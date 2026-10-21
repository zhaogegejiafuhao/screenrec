package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

// App 应用上下文
type App struct {
	dataDir    string
	recordsDir string
	assets     string
	ffmpeg     string
	ffprobe    string
	python     string
	mu         sync.RWMutex
	recorder   *Recorder
	asr        *ASR
	stream     *StreamASR
	overlay    *Overlay
	set        *Settings
	meeting    *MeetingWatcher
	loopMu     sync.Mutex
	loopDevs   []string
	loopAt     time.Time
	lastOpt    StartOptions
	recordings map[string]*Recording
	url        string
	quit       chan struct{}
}

// RequestQuit 请求退出（托盘菜单用）
func (a *App) RequestQuit() {
	select {
	case <-a.quit:
	default:
		close(a.quit)
	}
}

func NewApp(dataDir, assets string) (*App, error) {
	abs, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, err
	}
	records := filepath.Join(abs, "recordings")
	if err := os.MkdirAll(records, 0755); err != nil {
		return nil, err
	}
	assetsAbs, err := filepath.Abs(assets)
	if err != nil {
		return nil, err
	}
	a := &App{
		dataDir:    abs,
		recordsDir: records,
		assets:     assetsAbs,
		ffmpeg:     findFFmpeg(),
		ffprobe:    findFFprobe(),
		python:     findPython(),
		recordings: map[string]*Recording{},
		quit:       make(chan struct{}),
	}
	a.set = loadSettings(abs)
	a.asr = NewASR(a)
	a.stream = NewStreamASR(a)
	a.overlay = NewOverlay()
	a.recorder = NewRecorder(a)
	a.meeting = NewMeetingWatcher(a)
	a.overlay.OnAction(a.overlayAction)
	a.loadRecordings()
	return a, nil
}

// startRecording 所有启动录制的入口都走这里（网页 / 热键 / 悬浮条），
// 统一做合规确认与状态检查。
func (a *App) startRecording(opt StartOptions) (*Recording, error) {
	// 未确认授权时，把确认条画在悬浮条上（Win32 MessageBox 在部分机器上
	// 会被组策略静默返回，实测直接返回 IDOK 而不显示，不能作为同意凭据）
	if !a.set.ConsentOK() {
		a.overlay.ShowConsent("录音前请确认：已取得在场人员的录音同意，内容仅用于合法用途")
		return nil, errConsentNeeded
	}

	rec, err := a.recorder.Start(opt)
	if err != nil {
		return nil, err
	}
	a.put(rec)
	a.meeting.Clear()
	a.overlay.SetRecording()
	a.overlay.SetVisible(true)
	return rec, nil
}

var errConsentNeeded = fmt.Errorf("请先在悬浮条上确认录音授权")

// stopRecording 停止录制（悬浮条 / 热键 / 网页共用）
func (a *App) stopRecording() error {
	err := a.recorder.Stop()
	a.overlay.SetIdle()
	return err
}

// requestStart 悬浮条/热键触发的开始：先看授权，再真正开始
func (a *App) requestStart() {
	opt := a.lastOptions()
	if !a.set.ConsentOK() {
		a.overlay.ShowConsent("录音前请确认：已取得在场人员的录音同意，内容仅用于合法用途")
		return
	}
	if _, err := a.startRecording(opt); err != nil {
		a.flashPrompt("启动失败：" + err.Error())
	}
}

// flashPrompt 短暂显示一条提示，4 秒后回到空闲态
func (a *App) flashPrompt(msg string) {
	a.overlay.SetVisible(true)
	a.overlay.SetPrompt(msg)
	go func() {
		time.Sleep(4 * time.Second)
		a.overlay.SetIdle()
	}()
}

// overlayAction 悬浮条按钮回调
func (a *App) overlayAction(id string) {
	switch id {
	case "start":
		a.requestStart()
	case "consent":
		a.set.AcceptConsent()
		a.overlay.SetIdle()
		if _, err := a.startRecording(a.lastOptions()); err != nil {
			a.flashPrompt("启动失败：" + err.Error())
		}
	case "deny":
		a.overlay.SetIdle()
	case "stop":
		a.stopRecording()
	case "ignore":
		a.meeting.Snooze(30 * time.Minute)
		a.overlay.SetIdle()
	case "mute":
		if p := a.meeting.PendingKey(); p != "" {
			a.meeting.Mute(p)
		}
		a.overlay.SetIdle()
	case "close":
		// 关闭时如果正有一条会议提醒，一并静默，避免关闭后立刻又弹回来
		if a.meeting.Pending() != "" {
			a.meeting.Snooze(30 * time.Minute)
		}
		a.set.SetHidden(true)
	}
}

func (a *App) loadRecordings() {
	entries, _ := os.ReadDir(a.recordsDir)
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".json" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(a.recordsDir, e.Name()))
		if err != nil {
			continue
		}
		var r Recording
		if json.Unmarshal(b, &r) != nil || r.ID == "" {
			continue
		}
		if r.Status == "recording" {
			r.Status = "error"
			r.Error = "程序退出导致录制中断"
		}
		r.Diarizing = false
		a.recordings[r.ID] = &r
	}
}

func (a *App) get(id string) (*Recording, bool) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r, ok := a.recordings[id]
	return r, ok
}

func (a *App) put(r *Recording) {
	a.mu.Lock()
	a.recordings[r.ID] = r
	a.mu.Unlock()
}

func (a *App) list() []*Recording {
	a.mu.RLock()
	defer a.mu.RUnlock()
	out := make([]*Recording, 0, len(a.recordings))
	for _, r := range a.recordings {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out
}

func (a *App) delete(id string) error {
	a.mu.Lock()
	r, ok := a.recordings[id]
	if ok {
		delete(a.recordings, id)
	}
	a.mu.Unlock()
	if !ok {
		return fmt.Errorf("记录不存在")
	}
	os.Remove(r.File)
	for _, p := range r.Parts {
		os.Remove(p)
	}
	os.Remove(r.PCMFile)
	os.Remove(filepath.Join(a.recordsDir, id+".json"))
	return nil
}

// lastOptions 返回上次使用的录制参数（热键启动时用），没有则给一套默认值
func (a *App) lastOptions() StartOptions {
	a.mu.RLock()
	opt := a.lastOpt
	a.mu.RUnlock()
	if opt.Mode == "" {
		opt.Mode = "screen"
	}
	if opt.FPS == 0 {
		opt.FPS = 30
	}
	if opt.CRF == 0 {
		opt.CRF = 23
	}
	if opt.Mic == "" {
		for _, d := range listDevices(a.ffmpeg) {
			if d.Kind == "audio" {
				opt.Mic = d.Name
				break
			}
		}
	}
	return opt
}

// toggleRecordByHotkey 全局热键：开始/停止录音（复用上次参数）
func (a *App) toggleRecordByHotkey() {
	if rec := a.recorder.Current(); rec != nil && rec.Status == "recording" {
		a.stopRecording()
		return
	}
	a.requestStart()
}

// autoTranscribe 录制结束后自动整段转写（比实时更准）
func (a *App) autoTranscribe(rec *Recording) {
	if a.python == "" || a.ffmpeg == "" {
		return
	}
	a.mu.Lock()
	rec.Transcribing = true
	a.mu.Unlock()
	a.recorder.save(rec)

	a.mu.Lock()
	paths := append([]string{}, rec.Parts...)
	a.mu.Unlock()
	if len(paths) == 0 {
		paths = []string{rec.File}
	}
	segs, err := a.asr.BatchMulti(paths)

	a.mu.Lock()
	rec.Transcribing = false
	if err != nil {
		rec.Error = "转写失败: " + err.Error()
	} else if len(segs) > 0 {
		rec.Segments = segs
		rec.LiveText = joinText(segs)
		rec.Transcribed = true
		rec.Error = ""
	} else if len(rec.Segments) > 0 {
		// 整段精校没识别出内容，但实时通道有结果：保留实时结果，避免"越修越少"
		rec.Error = ""
	} else {
		rec.Transcribed = true
		rec.Error = ""
	}
	a.mu.Unlock()
	a.recorder.save(rec)

	// 转写成功后删除中间 PCM，节省空间
	if err == nil && rec.PCMFile != "" {
		os.Remove(rec.PCMFile)
		a.mu.Lock()
		rec.PCMFile = ""
		a.mu.Unlock()
		a.recorder.save(rec)
	}
}
