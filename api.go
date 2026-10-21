package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, map[string]string{"error": msg})
}

func (a *App) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		serveWeb(w, "index.html", "text/html; charset=utf-8")
	})
	mux.HandleFunc("GET /app.js", func(w http.ResponseWriter, r *http.Request) {
		serveWeb(w, "app.js", "text/javascript; charset=utf-8")
	})
	mux.HandleFunc("GET /style.css", func(w http.ResponseWriter, r *http.Request) {
		serveWeb(w, "style.css", "text/css; charset=utf-8")
	})

	mux.HandleFunc("GET /api/state", a.handleState)
	mux.HandleFunc("GET /api/windows", a.handleWindows)
	mux.HandleFunc("POST /api/mic/test", a.handleMicTest)
	mux.HandleFunc("POST /api/overlay", a.handleOverlay)
	mux.HandleFunc("GET /api/settings", a.handleGetSettings)
	mux.HandleFunc("PUT /api/settings", a.handlePutSettings)
	mux.HandleFunc("GET /api/loopback", a.handleLoopback)
	mux.HandleFunc("POST /api/record/start", a.handleStart)
	mux.HandleFunc("POST /api/record/stop", a.handleStop)
	mux.HandleFunc("GET /api/live", a.handleLive)
	mux.HandleFunc("GET /api/recordings", a.handleList)
	mux.HandleFunc("GET /api/recordings/{id}", a.handleGet)
	mux.HandleFunc("PUT /api/recordings/{id}", a.handleRename)
	mux.HandleFunc("DELETE /api/recordings/{id}", a.handleDelete)
	mux.HandleFunc("POST /api/recordings/{id}/transcribe", a.handleRetranscribe)
	mux.HandleFunc("POST /api/recordings/{id}/diarize", a.handleDiarize)
	mux.HandleFunc("GET /api/recordings/{id}/media", a.handleMedia)
	mux.HandleFunc("GET /api/recordings/{id}/transcript", a.handleTranscript)
	mux.HandleFunc("GET /api/recordings/{id}/export", a.handleExport)
	mux.HandleFunc("POST /api/recordings/{id}/translate", a.handleTranslate)

	return logMiddleware(mux)
}

func (a *App) handleState(w http.ResponseWriter, r *http.Request) {
	devs := listDevices(a.ffmpeg)
	var audioDevs, videoDevs []Device
	for _, d := range devs {
		if d.Kind == "audio" {
			audioDevs = append(audioDevs, d)
		} else {
			videoDevs = append(videoDevs, d)
		}
	}
	writeJSON(w, 200, map[string]any{
		"ffmpeg":     a.ffmpeg,
		"ffprobe":    a.ffprobe,
		"python":     a.python,
		"assets":     a.assets,
		"screens":    probeScreens(a.ffmpeg),
		"audio":      audioDevs,
		"video":      videoDevs,
		"encoder":    detectEncoder(a.ffmpeg),
		"recording":  a.recorder.Current(),
		"modelReady": a.python != "",
		"overlay":    a.overlay.IsVisible(),
		"overlayHwnd": a.overlay.Hwnd(),
		"overlayPos": a.overlay.Pos(),
		"micUsers":   micUsers(),
		"settings":   a.settingsView(),
		"meeting":    a.meeting.Pending(),
	})
}

func (a *App) settingsView() map[string]any {
	return map[string]any{
		"consentAccepted": a.set.ConsentOK(),
		"meetingPrompt":   a.set.MeetingEnabled(),
		"mutedApps":       a.set.Muted(),
		"overlayHidden":   a.set.Hidden(),
		"autoStart":       autoStartEnabled(),
	}
}

func (a *App) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.settingsView())
}

// handleLoopback 列出可用于「录制电脑声音」的播放设备
func (a *App) handleLoopback(w http.ResponseWriter, r *http.Request) {
	devs, err := a.loopDeviceList()
	if err != nil {
		writeErr(w, 500, err.Error())
		return
	}
	type dev struct {
		Name      string `json:"name"`
		Bluetooth bool   `json:"bluetooth"`
	}
	out := make([]dev, 0, len(devs))
	for _, d := range devs {
		out = append(out, dev{Name: d, Bluetooth: looksBluetooth(d)})
	}
	writeJSON(w, 200, map[string]any{"devices": out})
}

// looksBluetooth 判断播放设备是不是蓝牙（蓝牙设备不支持 WASAPI 回环）
func looksBluetooth(name string) bool {
	l := strings.ToLower(name)
	for _, k := range []string{"蓝牙", "bluetooth", "airpods", "buds", "wh-", "wf-", "freebuds", "耳机 ("} {
		if strings.Contains(l, k) {
			return true
		}
	}
	return false
}

func (a *App) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var req struct {
		MeetingPrompt  *bool   `json:"meetingPrompt"`
		OverlayHidden  *bool   `json:"overlayHidden"`
		ResetConsent   *bool   `json:"resetConsent"`
		AcceptConsent  *bool   `json:"acceptConsent"`
		ResetOverlayPos *bool  `json:"resetOverlayPos"`
		Unmute         *string `json:"unmute"`
		AutoStart      *bool   `json:"autoStart"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if req.MeetingPrompt != nil {
		a.set.SetMeetingEnabled(*req.MeetingPrompt)
	}
	if req.OverlayHidden != nil {
		a.set.SetHidden(*req.OverlayHidden)
		if !*req.OverlayHidden {
			a.overlay.SetVisible(true)
		} else {
			a.overlay.Dismiss()
		}
	}
	if req.ResetConsent != nil && *req.ResetConsent {
		a.set.ResetConsent()
	}
	if req.AcceptConsent != nil && *req.AcceptConsent {
		a.set.AcceptConsent()
	}
	if req.ResetOverlayPos != nil && *req.ResetOverlayPos {
		a.set.ResetOverlayPos()
		a.overlay.ResetPos()
	}
	if req.Unmute != nil {
		a.set.UnmuteApp(*req.Unmute)
	}
	if req.AutoStart != nil {
		if err := setAutoStart(*req.AutoStart); err != nil {
			writeErr(w, 500, err.Error())
			return
		}
	}
	writeJSON(w, 200, a.settingsView())
}

// handleWindows 列出可录制的窗口
func (a *App) handleWindows(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, listWindows())
}

// handleOverlay 控制悬浮字幕条的显隐
func (a *App) handleOverlay(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Show  *bool   `json:"show"`
		Text  *string `json:"text"`
		Level *int    `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if req.Text != nil {
		lv := 0
		if req.Level != nil {
			lv = *req.Level
		}
		a.overlay.Text(*req.Text, "", lv)
	}
	if req.Show != nil {
		a.overlay.SetVisible(*req.Show)
		a.set.SetHidden(!*req.Show)
	}
	writeJSON(w, 200, map[string]any{"ok": true})
}

// handleMicTest 采集 1.2 秒麦克风音频并返回电平诊断
func (a *App) handleMicTest(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Mic string `json:"mic"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if r2 := a.recorder.Current(); r2 != nil && r2.Status == "recording" {
		writeErr(w, 400, "录制中无法检测，请先停止")
		return
	}
	info, err := a.sampleMic(req.Mic, 1200*time.Millisecond)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, info)
}

func (a *App) handleStart(w http.ResponseWriter, r *http.Request) {
	var opt StartOptions
	if err := json.NewDecoder(r.Body).Decode(&opt); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	rec, err := a.startRecording(opt)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, rec)
}

func (a *App) handleStop(w http.ResponseWriter, r *http.Request) {
	if err := a.stopRecording(); err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (a *App) handleLive(w http.ResponseWriter, r *http.Request) {
	rec := a.recorder.Current()
	if rec == nil || rec.Status != "recording" {
		writeJSON(w, 200, map[string]any{"recording": false})
		return
	}
	a.mu.RLock()
	segs := append([]Segment{}, rec.Segments...)
	started := rec.StartedAt
	level := rec.LiveLevel
	partial := rec.LivePartial
	a.mu.RUnlock()
	writeJSON(w, 200, map[string]any{
		"recording": true,
		"startedAt": started,
		"segments":  segs,
		"text":      joinText(segs),
		"partial":   partial,
		"level":     level,
	})
}

func (a *App) handleList(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, a.list())
}

func (a *App) handleGet(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "记录不存在")
		return
	}
	a.mu.RLock()
	cp := *rec
	a.mu.RUnlock()
	writeJSON(w, 200, &cp)
}

func (a *App) handleRename(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "记录不存在")
		return
	}
	var req struct {
		Title string `json:"title"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeErr(w, 400, "请求格式错误")
		return
	}
	if strings.TrimSpace(req.Title) != "" {
		a.mu.Lock()
		rec.Title = strings.TrimSpace(req.Title)
		a.mu.Unlock()
	}
	a.recorder.save(rec)
	writeJSON(w, 200, rec)
}

func (a *App) handleDelete(w http.ResponseWriter, r *http.Request) {
	if err := a.delete(r.PathValue("id")); err != nil {
		writeErr(w, 404, err.Error())
		return
	}
	writeJSON(w, 200, map[string]string{"status": "ok"})
}

func (a *App) handleRetranscribe(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "记录不存在")
		return
	}
	rec.Transcribed = false
	go a.autoTranscribe(rec)
	writeJSON(w, 200, map[string]string{"status": "started"})
}

// handleDiarize 启动说话人分离
func (a *App) handleDiarize(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		writeErr(w, 404, "记录不存在")
		return
	}
	a.mu.Lock()
	if rec.Diarizing {
		a.mu.Unlock()
		writeErr(w, 400, "正在分离中，请稍候")
		return
	}
	if len(rec.Segments) == 0 {
		a.mu.Unlock()
		writeErr(w, 400, "请先完成转写")
		return
	}
	rec.Diarizing = true
	a.mu.Unlock()
	go a.runDiarize(rec)
	writeJSON(w, 200, map[string]string{"status": "started"})
}

func (a *App) handleMedia(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	path := rec.File
	if len(rec.Parts) > 1 {
		idx := 0
		fmt.Sscanf(r.URL.Query().Get("part"), "%d", &idx)
		if idx < 0 || idx >= len(rec.Parts) {
			idx = 0
		}
		path = rec.Parts[idx]
	}
	f, err := os.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	ctype := "video/mp4"
	if strings.HasSuffix(path, ".m4a") {
		ctype = "audio/mp4"
	}
	w.Header().Set("Content-Type", ctype)
	http.ServeContent(w, r, filepath.Base(path), st.ModTime(), f)
}

func (a *App) handleTranscript(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.mu.RLock()
	segs := append([]Segment{}, rec.Segments...)
	title := rec.Title
	a.mu.RUnlock()

	format := r.URL.Query().Get("format")
	var body string
	var ext, ctype string
	if format == "srt" {
		body = toSRT(segs)
		ext, ctype = ".srt", "text/plain; charset=utf-8"
	} else {
		body = joinText(segs)
		ext, ctype = ".txt", "text/plain; charset=utf-8"
	}
	fn := fmt.Sprintf("%s%s", sanitize(title), ext)
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Content-Disposition", attachment(fn))
	w.Write([]byte(body))
}

// handleTranslate 给整条记录补译文（本地 MarianMT，离线）
func (a *App) handleTranslate(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Dir string `json:"dir"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	n, err := a.translateRecord(rec, req.Dir)
	if err != nil {
		writeErr(w, 400, err.Error())
		return
	}
	a.mu.RLock()
	dir := rec.TranslateDir
	a.mu.RUnlock()
	writeJSON(w, 200, map[string]any{"status": "ok", "count": n, "dir": dir})
}

// handleExport 导出会议纪要：Markdown 或 Word
// 查询参数：format=md|docx（默认 md）、math=1（做公式整理）
func (a *App) handleExport(w http.ResponseWriter, r *http.Request) {
	rec, ok := a.get(r.PathValue("id"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	a.mu.RLock()
	snap := *rec
	snap.Segments = append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()

	doMath := r.URL.Query().Get("math") == "1"
	base := sanitize(snap.Title) + "_" + snap.StartedAt.Format("20060102")

	if r.URL.Query().Get("format") == "docx" {
		data, err := buildDocx(&snap, doMath)
		if err != nil {
			writeErr(w, 500, "生成 Word 失败："+err.Error())
			return
		}
		w.Header().Set("Content-Type", "application/vnd.openxmlformats-officedocument.wordprocessingml.document")
		w.Header().Set("Content-Disposition", attachment(base+".docx"))
		w.Write(data)
		return
	}

	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", attachment(base+".md"))
	w.Write([]byte(buildMarkdown(&snap, doMath)))
}

// attachment 生成兼容中文文件名的 Content-Disposition（RFC 5987）
func attachment(name string) string {
	var ascii strings.Builder
	for _, r := range name {
		if r < 128 && r != '"' && r != '\\' {
			ascii.WriteRune(r)
		} else {
			ascii.WriteByte('_')
		}
	}
	// 纯中文标题会退化成一片下划线，给个能看懂的回退名
	fallback := strings.Trim(ascii.String(), "_.- ")
	if fallback == "" || fallback == ".md" || fallback == ".docx" {
		fallback = "notes" + filepath.Ext(name)
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", fallback, url.PathEscape(name))
}

func sanitize(s string) string {	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_", "<", "_", ">", "_", "|", "_")
	out := strings.TrimSpace(r.Replace(s))
	if out == "" {
		out = "转录"
	}
	return out
}

func toSRT(segs []Segment) string {
	var b strings.Builder
	for i, s := range segs {
		text := s.Text
		if s.Trans != "" {
			text += "\n" + s.Trans
		}
		fmt.Fprintf(&b, "%d\n%s --> %s\n%s\n\n", i+1, srtTime(s.Start), srtTime(s.End), text)
	}
	return b.String()
}

func srtTime(sec float64) string {
	if sec < 0 {
		sec = 0
	}
	ms := int(sec*1000 + 0.5)
	h := ms / 3600000
	ms %= 3600000
	m := ms / 60000
	ms %= 60000
	s := ms / 1000
	ms %= 1000
	return fmt.Sprintf("%02d:%02d:%02d,%03d", h, m, s, ms)
}
