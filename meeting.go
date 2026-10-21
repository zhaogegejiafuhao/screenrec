package main

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// MeetingWatcher 会议自动提醒
//
// 判断依据（按可靠度排序）：
//  1. 注册表 CapabilityAccessManager：哪个 exe 此刻正在占用麦克风（Windows 麦克风小图标的同一份数据）
//  2. 窗口标题兜底：明确出现「正在会议中 / 语音通话 / Zoom Meeting」等字样
//
// 只在命中「已知会议应用」时才提示，避免被语音输入、录音笔之类误触。
type MeetingWatcher struct {
	app *App

	mu          sync.Mutex
	pending     string // 待确认的提示文案，空表示无
	pendingKey  string // 去重用（应用名）
	hitCount    int    // 连续命中次数，用来做去抖
	lastHitKey  string
	snoozeUntil time.Time // 「忽略」后的静默期
	promptedAt  time.Time // 上次提示时间
	promptedKey string
}

func NewMeetingWatcher(app *App) *MeetingWatcher {
	return &MeetingWatcher{app: app}
}

// Pending 返回待展示的提示文案
func (m *MeetingWatcher) Pending() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pending
}

// PendingKey 返回当前提示对应的应用名（「不再提醒」用）
func (m *MeetingWatcher) PendingKey() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingKey
}

// LastPrompt 上一次提示的应用与时间（给 API 展示用）
func (m *MeetingWatcher) LastPrompt() (string, time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.promptedKey, m.promptedAt
}

// Clear 用户已响应，清掉提示
func (m *MeetingWatcher) Clear() {
	m.mu.Lock()
	m.pending = ""
	m.pendingKey = ""
	m.hitCount = 0
	m.mu.Unlock()
}

// Snooze 用户点了「忽略」：静默一段时间
func (m *MeetingWatcher) Snooze(d time.Duration) {
	m.mu.Lock()
	m.snoozeUntil = time.Now().Add(d)
	m.pending = ""
	m.pendingKey = ""
	m.hitCount = 0
	m.mu.Unlock()
}

// Mute 用户选择「不再提醒这个应用」
func (m *MeetingWatcher) Mute(name string) {
	m.app.set.MuteApp(name)
	m.Snooze(24 * time.Hour)
	log.Printf("会议提醒已关闭：%s", name)
}

// Loop 后台轮询
func (m *MeetingWatcher) Loop(stop <-chan struct{}) {
	ticker := time.NewTicker(4 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
			m.tick()
		}
	}
}

func (m *MeetingWatcher) tick() {
	if !m.app.set.MeetingEnabled() {
		m.Clear()
		return
	}
	// 正在录制时不提示
	if rec := m.app.recorder.Current(); rec != nil && rec.Status == "recording" {
		m.Clear()
		return
	}

	m.mu.Lock()
	snoozed := time.Now().Before(m.snoozeUntil)
	m.mu.Unlock()
	if snoozed {
		return
	}

	key, text := m.detect()
	log.Printf("[meeting] tick key=%q text=%q", key, text)
	if key == "" {
		m.mu.Lock()
		m.hitCount = 0
		m.lastHitKey = ""
		if m.pending != "" {
			m.pending = ""
			m.pendingKey = ""
		}
		m.mu.Unlock()
		return
	}
	if m.app.set.IsMuted(key) {
		return
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	// 去抖：同一个应用连续命中 2 次（约 8 秒）才提示，避免一闪而过的麦克风占用
	if m.lastHitKey == key {
		m.hitCount++
	} else {
		m.lastHitKey = key
		m.hitCount = 1
	}
	if m.hitCount < 2 {
		return
	}
	// 同一个应用 30 分钟内不重复打扰
	if m.promptedKey == key && time.Since(m.promptedAt) < 30*time.Minute {
		return
	}
	if m.pending != text {
		m.pending = text
		m.pendingKey = key
	}
	m.promptedKey = key
	m.promptedAt = time.Now()
}

// detect 返回 (应用名, 提示文案)；没有会议则返回空
func (m *MeetingWatcher) detect() (string, string) {
	users := micUsers()
	titleHit := hitMeetingTitle()

	for _, u := range users {
		if name, ok := meetingAppNames[u.Name]; ok {
			return name, fmt.Sprintf("「%s」正在通话，要开始录音转写吗？", name)
		}
		if browserExes[u.Name] && titleHit != "" {
			return titleHit, fmt.Sprintf("检测到「%s」正在开会，要开始录音转写吗？", titleHit)
		}
	}
	// 兜底：窗口标题明确表明在开会
	if titleHit != "" {
		return titleHit, fmt.Sprintf("检测到「%s」正在进行，要开始录音转写吗？", titleHit)
	}
	// 有进程占用麦克风但不在已知名单里：仅记录日志，不打扰
	if len(users) > 0 {
		log.Printf("[meeting] 未识别的麦克风占用: %s", filepath.Base(users[0].Exe))
	}
	return "", ""
}

// 明确的会议/通话标题关键词（保守，避免把文档标题里的「会议」也命中）
var meetingTitleKeys = []struct {
	key  string
	name string
}{
	{"正在会议中", "会议"},
	{"会议中 ·", "会议"},
	{"语音通话中", "语音通话"},
	{"视频通话中", "视频通话"},
	{"通话中 -", "通话"},
	{"腾讯会议", "腾讯会议"},
	{"Zoom Meeting", "Zoom"},
	{"zoom meeting", "Zoom"},
	{"Microsoft Teams", "Microsoft Teams"},
	{"钉钉会议", "钉钉会议"},
	{"飞书会议", "飞书会议"},
	{"企业微信会议", "企业微信会议"},
	{"正在共享屏幕", "会议"},
}

func hitMeetingTitle() string {
	for _, w := range listWindows() {
		t := strings.TrimSpace(w.Title)
		for _, k := range meetingTitleKeys {
			if strings.Contains(t, k.key) {
				return k.name
			}
		}
	}
	return ""
}
