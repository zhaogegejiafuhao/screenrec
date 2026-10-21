package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

func tailText(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		s = "…" + s[len(s)-n:]
	}
	return s
}

// Segment 一条带时间戳的转写结果
type Segment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Trans   string  `json:"trans,omitempty"`   // 译文（双语字幕）
	Speaker int     `json:"speaker,omitempty"` // 1 起，0 表示未区分
}

// Recording 一次录制
type Recording struct {
	ID          string     `json:"id"`
	Title       string     `json:"title"`
	Mode        string     `json:"mode"` // screen / audio
	File        string     `json:"file"`
	Parts       []string   `json:"parts,omitempty"`
	PCMFile     string     `json:"pcmFile,omitempty"`
	StartedAt   time.Time  `json:"startedAt"`
	EndedAt     *time.Time `json:"endedAt,omitempty"`
	DurationMs  int64      `json:"durationMs"`
	Status      string     `json:"status"` // recording / done / error
	Error       string     `json:"error,omitempty"`
	Screen      int        `json:"screen"`
	Mic         string     `json:"mic"`
	FPS         int        `json:"fps"`
	CRF         int        `json:"crf"`
	GainDb      int        `json:"gainDb"`
	SystemAudio bool       `json:"systemAudio"`
	SysDevice   string     `json:"sysDevice,omitempty"`
	SysWarning  string     `json:"sysWarning,omitempty"`
	SysFile     string     `json:"-"`
	SysLeadMs   int64      `json:"-"`
	Segments    []Segment  `json:"segments"`
	LiveText    string     `json:"liveText"`
	LivePartial string     `json:"livePartial"`
	LiveTrans   string     `json:"liveTrans,omitempty"`
	LiveDir     string     `json:"-"`
	LiveLevel   int        `json:"liveLevel"`
	Encoder     string     `json:"encoder,omitempty"`
	Transcribed bool       `json:"transcribed"`
	Transcribing bool      `json:"transcribing"`
	Diarized    bool       `json:"diarized"`
	Diarizing   bool       `json:"diarizing"`
	Speakers    int        `json:"speakers"`
	TranslateDir string    `json:"translateDir,omitempty"` // en2zh / zh2en
	Summary      string    `json:"summary,omitempty"`      // AI 纪要（Markdown）
	SummaryModel string    `json:"summaryModel,omitempty"`
	SummaryAt    *time.Time `json:"summaryAt,omitempty"`
	SizeBytes    int64     `json:"sizeBytes"`
	Pattern     string     `json:"-"` // 分段录制时的文件名模板
}

// Recorder 管理 ffmpeg 录制进程
type Recorder struct {
	mu       sync.Mutex
	saveMu   sync.Mutex
	app      *App
	cur      *Recording
	cmd      *exec.Cmd
	stdin    io.WriteCloser
	liveStop chan struct{}
	doneCh   chan struct{}
	stopping bool
	loop     *LoopbackProc
}

func NewRecorder(app *App) *Recorder {
	return &Recorder{app: app}
}

// StartOptions 录制参数
type StartOptions struct {
	Mode       string `json:"mode"` // screen / window / audio
	Screen     int    `json:"screen"`
	Mic        string `json:"mic"`
	FPS        int    `json:"fps"`
	CRF        int    `json:"crf"`
	GainDb     int    `json:"gainDb"`     // 麦克风增益补偿（dB），0 表示不处理
	Hwnd       uint64 `json:"hwnd"`       // 窗口录制时的窗口句柄
	SegmentMin int    `json:"segmentMin"` // 每段分钟数，0 表示不分段
	// 同时录制「电脑正在播放的声音」（WASAPI 回环），与麦克风实时混音
	SystemAudio bool   `json:"systemAudio"`
	SysDevice   string `json:"sysDevice"`
}

func (r *Recorder) Current() *Recording {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cur
}

// Start 启动录制
func (r *Recorder) Start(opt StartOptions) (*Recording, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.cur != nil && r.cur.Status == "recording" {
		return nil, fmt.Errorf("正在录制中")
	}
	if r.app.ffmpeg == "" {
		return nil, fmt.Errorf("未找到 ffmpeg")
	}
	if opt.Mode == "" {
		opt.Mode = "screen"
	}
	if opt.Mode != "screen" && opt.Mode != "window" && opt.Mode != "audio" {
		opt.Mode = "screen"
	}
	if opt.FPS <= 0 {
		opt.FPS = 30
	}
	if opt.CRF <= 0 {
		opt.CRF = 23
	}
	if strings.TrimSpace(opt.Mic) == "" {
		return nil, fmt.Errorf("请选择麦克风设备")
	}
	if opt.Mode == "window" && opt.Hwnd == 0 {
		return nil, fmt.Errorf("请选择要录制的窗口")
	}
	if opt.GainDb < 0 {
		opt.GainDb = 0
	}
	if opt.GainDb > 30 {
		opt.GainDb = 30
	}

	if opt.SegmentMin < 0 {
		opt.SegmentMin = 0
	}
	if opt.SegmentMin > 120 {
		opt.SegmentMin = 120
	}

	now := time.Now()
	id := fmt.Sprintf("r%s", now.Format("20060102-150405"))
	ext := ".mp4"
	if opt.Mode == "audio" {
		ext = ".m4a"
	}
	outFile := filepath.Join(r.app.recordsDir, id+ext)
	pattern := ""
	if opt.Mode != "audio" && opt.SegmentMin > 0 {
		pattern = filepath.Join(r.app.recordsDir, id+"-%03d.mp4")
		outFile = pattern
	}
	pcmFile := filepath.Join(r.app.recordsDir, id+".pcm")

	// 编码器：有视频时优先试硬件，失败自动回退 CPU
	encoders := []string{"libx264"}
	if opt.Mode != "audio" {
		enc := detectEncoder(r.app.ffmpeg)
		encoders = []string{enc}
		if enc != "libx264" {
			encoders = append(encoders, "libx264")
		}
	}
	// 系统声音模式下不做编码器重试：stdin 被 PCM 占用，重试会导致音频流错位
	if opt.SystemAudio {
		encoders = encoders[:1]
	}

	// 先启动系统声音采集（写裸 PCM 文件），拿到 python 侧的真实起始时间
	sysFile := filepath.Join(r.app.recordsDir, id+".sys.pcm")
	var lb *LoopbackProc
	if opt.SystemAudio {
		l, err := r.app.startLoopback(opt.SysDevice, sysFile)
		if err != nil {
			return nil, err
		}
		lb = l
	}

	var cmd *exec.Cmd
	var stdin io.WriteCloser
	var waitCh chan error
	var startedEnc, lastErr string
	var ffStart time.Time
	var leadMs int64
	for _, e := range encoders {
		args := buildRecordArgs(opt, outFile, pcmFile, e)
		c := exec.Command(r.app.ffmpeg, args...)
		c.Dir = filepath.Dir(r.app.ffmpeg)
		in, err := c.StdinPipe()
		if err != nil {
			if lb != nil {
				lb.Stop()
			}
			return nil, err
		}
		var errBuf strings.Builder
		c.Stdout = os.Stderr
		c.Stderr = io.MultiWriter(os.Stderr, &errBuf)
		ffStart = time.Now()
		if err := c.Start(); err != nil {
			lastErr = err.Error()
			continue
		}
		wch := make(chan error, 1)
		go func() { wch <- c.Wait() }()
		// 观察 1.2 秒：若 ffmpeg 立刻退出，说明参数或设备有问题
		select {
		case err := <-wch:
			os.Remove(outFile)
			os.Remove(pcmFile)
			lastErr = fmt.Sprintf("%v\n%s", err, tailText(errBuf.String(), 400))
			continue
		case <-time.After(1200 * time.Millisecond):
		}
		cmd, stdin, waitCh, startedEnc = c, in, wch, e
		if lb != nil {
			leadMs = ffStart.UnixMilli() - lb.startMs
			if leadMs < 0 {
				leadMs = 0
			}
		}
		break
	}
	if cmd == nil {
		if lb != nil {
			lb.Stop()
		}
		return nil, fmt.Errorf("录制启动失败：%s", lastErr)
	}

	rec := &Recording{
		ID:        id,
		Title:     defaultTitle(opt.Mode, now),
		Mode:      opt.Mode,
		File:      outFile,
		PCMFile:   pcmFile,
		StartedAt: now,
		Status:    "recording",
		Screen:    opt.Screen,
		Mic:       opt.Mic,
		FPS:       opt.FPS,
		CRF:       opt.CRF,
		GainDb:    opt.GainDb,
		Encoder:   startedEnc,
		SystemAudio: opt.SystemAudio,
		SysDevice: opt.SysDevice,
		SysFile:    sysFile,
		SysLeadMs:  leadMs,
		Segments:  []Segment{},
		Pattern:   pattern,
	}
	r.cur, r.cmd, r.stdin = rec, cmd, stdin
	r.loop = lb
	r.app.mu.Lock()
	r.app.lastOpt = opt
	r.app.mu.Unlock()
	r.stopping = false
	r.liveStop = make(chan struct{})
	r.doneCh = make(chan struct{})
	r.save(rec)

	go r.waitExit(rec, waitCh)
	go r.liveLoop(rec, pcmFile, r.liveStop)
	return rec, nil
}

func defaultTitle(mode string, t time.Time) string {
	kind := "录屏"
	if mode == "audio" {
		kind = "录音"
	}
	return fmt.Sprintf("%s %s", kind, t.Format("01-02 15:04"))
}

// buildRecordArgs 组装 ffmpeg 参数（mp4/m4a + PCM 双输出）
func buildRecordArgs(opt StartOptions, outFile, pcmFile, enc string) []string {
	// 麦克风增益：低电平麦克风用 volume 提升，再用 alimiter 防削波
	gain := []string{}
	if opt.GainDb > 0 {
		gain = []string{"-af", fmt.Sprintf("volume=%ddB,alimiter=limit=0.95", opt.GainDb)}
	}

	args := []string{"-y", "-hide_banner", "-loglevel", "warning"}
	if opt.Mode == "screen" {
		args = append(args,
			"-thread_queue_size", "512",
			"-f", "lavfi", "-i", fmt.Sprintf("ddagrab=%d:framerate=%d", opt.Screen, opt.FPS),
		)
	} else if opt.Mode == "window" {
		args = append(args,
			"-thread_queue_size", "512",
			"-f", "gdigrab", "-framerate", fmt.Sprint(opt.FPS),
			"-i", fmt.Sprintf("hwnd=0x%x", opt.Hwnd),
		)
	}
	args = append(args,
		"-thread_queue_size", "512",
		"-f", "dshow", "-i", "audio="+opt.Mic,
	)

	if opt.Mode != "audio" {
		args = append(args, "-map", "0:v")
		if opt.Mode == "screen" {
			args = append(args, "-vf", "hwdownload,format=bgra,scale=trunc(iw/2)*2:trunc(ih/2)*2")
		} else {
			args = append(args, "-vf", "format=yuv420p,scale=trunc(iw/2)*2:trunc(ih/2)*2")
		}
		if enc == "nvenc" {
			args = append(args, "-c:v", "h264_nvenc", "-preset", "p5", "-rc", "vbr",
				"-cq", fmt.Sprint(opt.CRF), "-b:v", "0")
		} else {
			args = append(args, "-c:v", "libx264", "-preset", "veryfast",
				"-crf", fmt.Sprint(opt.CRF))
		}
		args = append(args, "-pix_fmt", "yuv420p", "-g", fmt.Sprint(opt.FPS*2))
		args = append(args, "-map", "1:a")
		args = append(args, gain...)
		args = append(args, "-c:a", "aac", "-b:a", "160k", "-ar", "44100", "-ac", "2")
		if opt.SegmentMin > 0 {
			args = append(args,
				"-f", "segment",
				"-segment_time", fmt.Sprint(opt.SegmentMin*60),
				"-reset_timestamps", "1",
				"-segment_format", "mp4",
				outFile,
			)
		} else {
			args = append(args, "-movflags", "+faststart", outFile)
		}
	} else {
		args = append(args, "-map", "0:a")
		args = append(args, gain...)
		args = append(args,
			"-c:a", "aac", "-b:a", "160k", "-ar", "44100", "-ac", "2",
			"-movflags", "+faststart",
			outFile,
		)
	}

	audioInput := "0:a"
	if opt.Mode != "audio" {
		audioInput = "1:a"
	}
	args = append(args, "-map", audioInput)
	args = append(args, gain...)
	args = append(args, "-f", "s16le", "-ar", "16000", "-ac", "1",
		"-flush_packets", "1", pcmFile)
	return args
}



// Stop 停止录制（发送 q 让 ffmpeg 正常收尾）
func (r *Recorder) Stop() error {
	r.mu.Lock()
	if r.cur == nil || r.cur.Status != "recording" {
		r.mu.Unlock()
		return fmt.Errorf("当前没有在录制")
	}
	r.stopping = true
	stdin := r.stdin
	cmd := r.cmd
	done := r.doneCh
	r.mu.Unlock()

	if stdin != nil {
		fmt.Fprint(stdin, "q\n")
	}
	select {
	case <-done:
	case <-time.After(12 * time.Second):
		if cmd != nil && cmd.Process != nil {
			cmd.Process.Kill()
		}
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}
	return nil
}

func (r *Recorder) waitExit(rec *Recording, waitCh chan error) {
	err := <-waitCh
	defer close(r.doneCh)

	r.mu.Lock()
	if r.liveStop != nil {
		close(r.liveStop)
		r.liveStop = nil
	}
	silentLoop := false
	if r.loop != nil {
		silentLoop = r.loop.IsSilent()
		r.loop.Stop()
		r.loop = nil
	}
	stopping := r.stopping
	r.mu.Unlock()
	if silentLoop {
		rec.SysWarning = "系统声音全程静音：该播放设备可能不支持回环（蓝牙耳机常见），建议换成内置扬声器或有线耳机"
	}

	now := time.Now()
	r.app.mu.Lock()
	rec.EndedAt = &now
	rec.Status = "done"
	if err != nil && !stopping {
		rec.Status = "error"
		rec.Error = err.Error()
	}
	r.app.mu.Unlock()

	// 分段录制：把实际生成的文件收集起来并按序号排序
	parts := globParts(rec.Pattern)
	if len(parts) == 0 {
		if _, e := os.Stat(rec.File); e == nil {
			parts = []string{rec.File}
		}
	}
	var size, dur int64
	for _, p := range parts {
		if st, e := os.Stat(p); e == nil {
			size += st.Size()
		}
		dur += probeDuration(r.app.ffprobe, p)
	}

	r.app.mu.Lock()
	rec.Parts = parts
	if len(parts) > 0 {
		rec.File = parts[0]
	}
	rec.SizeBytes = size
	rec.DurationMs = dur
	if rec.DurationMs == 0 {
		rec.DurationMs = int64(now.Sub(rec.StartedAt).Milliseconds())
	}
	rec.LiveLevel = 0
	r.app.mu.Unlock()
	r.save(rec)

	// 混音 + 整段转写放后台，别让停止按钮等它们
	multiPart := len(parts) > 1
	go func() {
		// 有系统声音：先混进成片，再转写（这样转写也能听到对方说话）
		if rec.SystemAudio && rec.SysFile != "" {
			if multiPart {
				rec.SysWarning = "分段录制暂不与系统声音混音，本条只有麦克风声音"
			} else if err := r.app.mixSystemAudio(rec.File, rec.SysFile, rec.SysLeadMs, rec.GainDb); err != nil {
				rec.SysWarning = "系统声音混音失败：" + err.Error()
			}
			os.Remove(rec.SysFile)
			rec.SysFile = ""
			r.save(rec)
		}
		r.app.autoTranscribe(rec)
	}()
}

// globParts 把 %03d 模板展开成实际文件列表
func globParts(pattern string) []string {
	if pattern == "" {
		return nil
	}
	cand := strings.Replace(pattern, "%03d", "*", 1)
	files, err := filepath.Glob(cand)
	if err != nil {
		return nil
	}
	sort.Strings(files)
	return files
}

// liveLoop 边录边转写：每 2.5 秒切一刀，带 0.3 秒重叠
func (r *Recorder) liveLoop(rec *Recording, pcmPath string, stop chan struct{}) {
	const sr = 16000
	const bytesPerSec = sr * 2
	chunkBytes := int64(bytesPerSec * 2.5)
	overlapBytes := int64(bytesPerSec * 0.3)
	liveOn, liveDirCfg := false, "auto"
	if r.app.set != nil {
		liveOn, liveDirCfg = r.app.set.LiveTransConfig()
	}

	var f *os.File
	for i := 0; i < 40; i++ {
		ff, err := os.Open(pcmPath)
		if err == nil {
			f = ff
			break
		}
		select {
		case <-stop:
			return
		case <-time.After(150 * time.Millisecond):
		}
	}
	if f == nil {
		return
	}
	defer f.Close()

	consumed := int64(0)
	useStream := r.app.streamReady() && r.app.stream != nil
	uttStart := int64(0)
	lastPartial := ""
	tick := 700 * time.Millisecond
	if useStream {
		tick = 200 * time.Millisecond
	}
	ticker := time.NewTicker(tick)
	defer ticker.Stop()

	readAt := func(off, n int64) []byte {
		b := make([]byte, n)
		if _, err := f.ReadAt(b, off); err != nil {
			return nil
		}
		return b
	}

	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		st, err := f.Stat()
		if err != nil {
			continue
		}
		size := st.Size()

		// 实时音量：取末尾 0.2 秒
		if size >= 6400 {
			if lb := readAt(size-6400, 6400); lb != nil {
				lv, _ := liveLevel(lb)
				r.app.mu.Lock()
				if rec.Status == "recording" {
					rec.LiveLevel = lv
				}
				r.app.mu.Unlock()
			}
		}

		if useStream {
			avail := size - consumed
			if avail < 3200 { // 至少 0.1 秒
				continue
			}
			if avail > int64(bytesPerSec) { // 单次最多喂 1 秒，避免积压导致延迟
				avail = int64(bytesPerSec)
			}
			if avail%2 != 0 {
				avail--
			}
			buf := readAt(consumed, avail)
			if buf == nil {
				continue
			}
			consumed += avail

			partial, final, err := r.app.stream.Feed(buf)
			if err != nil {
				useStream = false // 流式进程异常，退回 2.5 秒分块
				uttStart = consumed
				continue
			}
			if final != "" {
				// 断句后立刻用 SenseVoice 精校这一句（更准且带标点）
				seg := readAt(uttStart, consumed-uttStart)
				text := final
				if seg != nil {
					if t2, e := r.app.asr.Decode(seg); e == nil && strings.TrimSpace(t2) != "" {
						text = t2
					}
				}
				r.app.mu.Lock()
				if rec.Status == "recording" {
					appendLive(rec, float64(uttStart)/bytesPerSec, float64(consumed)/bytesPerSec, text)
					rec.LivePartial = ""
				}
				r.app.mu.Unlock()
				r.translateAsync(rec, text, liveOn, liveDirCfg)
				uttStart = consumed
				lastPartial = ""
			} else if partial != "" && partial != lastPartial {
				lastPartial = partial
				r.app.mu.Lock()
				if rec.Status == "recording" {
					rec.LivePartial = partial
				}
				r.app.mu.Unlock()
			}
			continue
		}

		// ---- 回退路径：2.5 秒分块 + 0.3 秒重叠 ----
		if size-consumed < chunkBytes {
			continue
		}
		start := consumed - overlapBytes
		if start < 0 {
			start = 0
		}
		n := consumed + chunkBytes - start
		buf := readAt(start, n)
		if buf == nil {
			continue
		}
		consumed += chunkBytes
		if pcmRMS(buf) < 260 {
			continue
		}
		text, err := r.app.asr.Decode(buf)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		startSec := float64(start) / bytesPerSec
		endSec := startSec + float64(n)/bytesPerSec
		r.app.mu.Lock()
		if rec.Status == "recording" {
			appendLive(rec, startSec, endSec, text)
		}
		r.app.mu.Unlock()
		r.translateAsync(rec, text, liveOn, liveDirCfg)
	}
}

// appendLive 追加实时转写，裁掉与上一条重叠的文字（调用方需持锁）
func appendLive(rec *Recording, start, end float64, text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}
	if n := len(rec.Segments); n > 0 {
		prev := []rune(rec.Segments[n-1].Text)
		cur := []rune(text)
		maxL := len(prev)
		if maxL > 12 {
			maxL = 12
		}
		for l := maxL; l >= 3; l-- {
			if len(cur) >= l && string(prev[len(prev)-l:]) == string(cur[:l]) {
				text = strings.TrimSpace(string(cur[l:]))
				break
			}
		}
	}
	if text == "" {
		return
	}
	rec.Segments = append(rec.Segments, Segment{Start: round2(start), End: round2(end), Text: text})
	rec.LiveText = joinText(rec.Segments)
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}

// translateAsync 实时字幕：断句后异步翻译，结果写回 rec.LiveTrans
func (r *Recorder) translateAsync(rec *Recording, text string, on bool, cfgDir string) {
	if !on || strings.TrimSpace(text) == "" {
		return
	}
	go func() {
		r.app.mu.RLock()
		dir := rec.LiveDir
		alive := rec.Status == "recording"
		r.app.mu.RUnlock()
		if !alive {
			return
		}
		if dir == "" {
			dir = cfgDir
		}
		tr, err := r.app.translateLive(text, dir)
		if err != nil || strings.TrimSpace(tr) == "" {
			return
		}
		// 方向一旦确定就固定下来，避免中英混说时反复切换模型
		if dir == "auto" {
			dir = detectDirection(text)
		}

		r.app.mu.Lock()
		if rec.Status == "recording" {
			rec.LiveTrans = joinSentence(rec.LiveTrans, tr)
			rec.LiveDir = dir
			if n := len([]rune(rec.LiveTrans)); n > 600 {
				rec.LiveTrans = "…" + string([]rune(rec.LiveTrans)[n-600:])
			}
		}
		trans := rec.LiveTrans
		r.app.mu.Unlock()
		if r.app.overlay != nil {
			r.app.overlay.SetTrans(trans)
		}
	}()
}

func joinText(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
	}
	return b.String()
}

func (r *Recorder) save(rec *Recording) {
	r.app.mu.Lock()
	b, err := json.MarshalIndent(rec, "", "  ")
	r.app.mu.Unlock()
	if err != nil {
		return
	}
	r.saveMu.Lock()
	defer r.saveMu.Unlock()
	os.WriteFile(filepath.Join(r.app.recordsDir, rec.ID+".json"), b, 0644)
}
