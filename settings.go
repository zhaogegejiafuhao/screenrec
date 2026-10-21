package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"unsafe"
)

// Settings 应用级配置（持久化到 data/settings.json）
type Settings struct {
	mu   sync.Mutex
	path string

	// 首次录音前的合规确认是否已通过
	ConsentAccepted bool `json:"consentAccepted"`
	// 会议自动提醒（默认开启，故用 Off 取反存储）
	MeetingPromptOff bool `json:"meetingPromptOff"`
	// 用户选择"不再提醒"的应用名
	MutedApps []string `json:"mutedApps"`
	// 用户手动关掉悬浮条后记住状态
	OverlayHidden bool `json:"overlayHidden"`
	// 悬浮条被拖动后的位置（左上角屏幕坐标）
	OverlayPosSet bool  `json:"overlayPosSet"`
	OverlayX      int32 `json:"overlayX"`
	OverlayY      int32 `json:"overlayY"`
	// 关闭了会议提醒的会议应用
	AcceptedCount int `json:"acceptedCount"`
}

func loadSettings(dataDir string) *Settings {
	s := &Settings{path: filepath.Join(dataDir, "settings.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(b, s)
	}
	return s
}

func (s *Settings) save() {
	s.mu.Lock()
	b, _ := json.MarshalIndent(s, "", "  ")
	path := s.path
	s.mu.Unlock()
	if b != nil {
		os.WriteFile(path, b, 0644)
	}
}

func (s *Settings) ConsentOK() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ConsentAccepted
}

func (s *Settings) AcceptConsent() {
	s.mu.Lock()
	s.ConsentAccepted = true
	s.mu.Unlock()
	s.save()
}

func (s *Settings) MeetingEnabled() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return !s.MeetingPromptOff
}

func (s *Settings) SetMeetingEnabled(on bool) {
	s.mu.Lock()
	s.MeetingPromptOff = !on
	s.mu.Unlock()
	s.save()
}

func (s *Settings) MuteApp(name string) {
	s.mu.Lock()
	for _, m := range s.MutedApps {
		if m == name {
			s.mu.Unlock()
			s.save()
			return
		}
	}
	s.MutedApps = append(s.MutedApps, name)
	s.mu.Unlock()
	s.save()
}

func (s *Settings) IsMuted(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, m := range s.MutedApps {
		if m == name {
			return true
		}
	}
	return false
}

func (s *Settings) ResetConsent() {
	s.mu.Lock()
	s.ConsentAccepted = false
	s.mu.Unlock()
	s.save()
}

func (s *Settings) UnmuteApp(name string) {
	s.mu.Lock()
	out := s.MutedApps[:0]
	for _, m := range s.MutedApps {
		if m != name {
			out = append(out, m)
		}
	}
	s.MutedApps = out
	s.mu.Unlock()
	s.save()
}

// Muted 返回静音名单副本
func (s *Settings) Muted() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string{}, s.MutedApps...)
}

func (s *Settings) Hidden() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.OverlayHidden
}

func (s *Settings) SetHidden(v bool) {
	s.mu.Lock()
	s.OverlayHidden = v
	s.mu.Unlock()
	s.save()
}

// OverlayPos 返回记住的悬浮条位置
func (s *Settings) OverlayPos() (int32, int32, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.OverlayX, s.OverlayY, s.OverlayPosSet
}

// SetOverlayPos 记住悬浮条位置（拖动结束后调用）
func (s *Settings) SetOverlayPos(x, y int32) {
	s.mu.Lock()
	if s.OverlayPosSet && s.OverlayX == x && s.OverlayY == y {
		s.mu.Unlock()
		return
	}
	s.OverlayX, s.OverlayY, s.OverlayPosSet = x, y, true
	s.mu.Unlock()
	s.save()
}

// ResetOverlayPos 复位到默认位置
func (s *Settings) ResetOverlayPos() {
	s.mu.Lock()
	s.OverlayPosSet = false
	s.mu.Unlock()
	s.save()
}

// ---- 原生对话框 ----
// 说明：部分机器（组策略/安全软件）会把 MessageBox 静默返回 IDOK 而不显示，
// 因此录音授权确认不依赖原生对话框，改为自绘在悬浮条上（见 Overlay.ShowConsent）。

var pMessageBoxW = u32.NewProc("MessageBoxW")

const (
	mbSetForeground = 0x00010000
	mbTopMost       = 0x00040000
	mbIconInfo      = 0x00000040
	mbYesNo         = 0x00000004
)

func messageBox(title, text string, flags uintptr) int {
	r, _, _ := pMessageBoxW.Call(0,
		uintptr(unsafe.Pointer(mustUTF16(text))),
		uintptr(unsafe.Pointer(mustUTF16(title))),
		flags|mbSetForeground|mbTopMost)
	return int(r)
}

// confirmDialog 通用二次确认（用于删除等不可逆操作）
func confirmDialog(title, text string) bool {
	return messageBox(title, text, mbYesNo|mbIconInfo) == 6 // IDYES
}
