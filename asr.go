package main

import (
	"os/exec"
	"screenrec/internal/winhide"
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// ASR 封装本地识别：常驻 worker 做实时转写，批量走 asr.py（带 VAD 分段）
type ASR struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	ready  bool
	app    *App
}

func NewASR(app *App) *ASR {
	return &ASR{app: app}
}

// Start 启动常驻识别进程（加载模型约 2 秒）
func (a *ASR) Start() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.startLocked()
}

func (a *ASR) startLocked() error {
	if a.ready && a.cmd != nil && a.cmd.ProcessState == nil {
		return nil
	}
	if a.app.python == "" {
		return fmt.Errorf("未找到 Python 运行时")
	}
	script := filepath.Join(a.app.assets, "asr_worker.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("找不到 asr_worker.py: %s", script)
	}
	cmd := winhide.Command(a.app.python, script)
	cmd.Dir = a.app.assets
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	a.cmd, a.stdin, a.reader, a.ready = cmd, stdin, bufio.NewReader(stdout), true
	return nil
}

// Decode 解码一段 16k/16bit/mono PCM，返回文本
func (a *ASR) Decode(pcm []byte) (string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.startLocked(); err != nil {
		return "", err
	}
	if len(pcm) == 0 {
		return "", nil
	}
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, uint32(len(pcm)))
	if _, err := a.stdin.Write(hdr); err != nil {
		a.ready = false
		return "", err
	}
	if _, err := a.stdin.Write(pcm); err != nil {
		a.ready = false
		return "", err
	}
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := a.reader.ReadString('\n')
		ch <- res{line, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			a.ready = false
			return "", r.err
		}
		var v struct {
			Text string `json:"text"`
		}
		if err := json.Unmarshal([]byte(r.line), &v); err != nil {
			return "", err
		}
		return v.Text, nil
	case <-time.After(60 * time.Second):
		return "", fmt.Errorf("识别超时")
	}
}

// Batch 对媒体文件做完整转写（提取 16k 单声道 → VAD 分段 → 逐段识别）
func (a *ASR) Batch(mediaPath string) ([]Segment, error) {
	if a.app.python == "" || a.app.ffmpeg == "" {
		return nil, fmt.Errorf("缺少 Python 或 ffmpeg")
	}
	tmpDir, err := os.MkdirTemp(a.app.dataDir, "asr-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	wavPath := filepath.Join(tmpDir, "audio16k.wav")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.app.ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-i", mediaPath, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wavPath)
	cmd.Dir = filepath.Dir(a.app.ffmpeg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("提取音频失败: %v %s", err, firstN(out, 200))
	}

	script := filepath.Join(a.app.assets, "asr.py")
	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel2()
	c2 := winhide.CommandContext(ctx2, a.app.python, script, wavPath)
	c2.Dir = a.app.assets
	out, err := c2.Output()
	if err != nil {
		return nil, fmt.Errorf("识别失败: %v", err)
	}
	var result struct {
		Segments []Segment `json:"segments"`
		Text     string    `json:"text"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return nil, fmt.Errorf("解析识别结果失败: %v", err)
	}
	if len(result.Segments) == 0 && result.Text != "" {
		result.Segments = []Segment{{Start: 0, End: 0, Text: result.Text}}
	}
	return result.Segments, nil
}

// mergeMedia 把多个分段合成一个媒体文件；单段直接返回原路径
func (a *App) mergeMedia(paths []string) (string, func(), error) {
	if len(paths) == 0 {
		return "", func() {}, fmt.Errorf("没有可用的媒体")
	}
	if len(paths) == 1 {
		return paths[0], func() {}, nil
	}
	if a.ffmpeg == "" {
		return "", func() {}, fmt.Errorf("缺少 ffmpeg")
	}
	tmpDir, err := os.MkdirTemp(a.dataDir, "merge-")
	if err != nil {
		return "", func() {}, err
	}
	cleanup := func() { os.RemoveAll(tmpDir) }
	listPath := filepath.Join(tmpDir, "list.txt")
	var b strings.Builder
	for _, p := range paths {
		b.WriteString("file '" + strings.ReplaceAll(p, "'", "'\\''") + "'\n")
	}
	if err := os.WriteFile(listPath, []byte(b.String()), 0644); err != nil {
		cleanup()
		return "", func() {}, err
	}
	merged := filepath.Join(tmpDir, "merged.m4a")
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-f", "concat", "-safe", "0", "-i", listPath,
		"-vn", "-c:a", "aac", "-b:a", "128k", merged)
	cmd.Dir = filepath.Dir(a.ffmpeg)
	if out, err := cmd.CombinedOutput(); err != nil {
		cleanup()
		return "", func() {}, fmt.Errorf("合并分段失败: %v %s", err, firstN(out, 200))
	}
	return merged, cleanup, nil
}

// BatchMulti 对多个分段文件做整段转写（先用 concat 解复用器拼成一个音轨）
func (a *ASR) BatchMulti(paths []string) ([]Segment, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("没有可转写的媒体")
	}
	if len(paths) == 1 {
		return a.Batch(paths[0])
	}
	merged, cleanup, err := a.app.mergeMedia(paths)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return a.Batch(merged)
}

// Close 关闭常驻进程
func (a *ASR) Close() {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.stdin != nil {
		a.stdin.Close()
	}
	if a.cmd != nil && a.cmd.Process != nil {
		a.cmd.Process.Kill()
	}
	a.ready = false
}

// ---- WAV 工具 ----

// writeWav16k 把 16k/16bit/mono 的原始 PCM 包成 WAV
func writeWav16k(path string, pcm []byte) error {
	hdr := make([]byte, 44)
	const sr, ch, bits = 16000, 1, 16
	copy(hdr[0:4], "RIFF")
	binary.LittleEndian.PutUint32(hdr[4:8], uint32(36+len(pcm)))
	copy(hdr[8:12], "WAVE")
	copy(hdr[12:16], "fmt ")
	binary.LittleEndian.PutUint32(hdr[16:20], 16)
	binary.LittleEndian.PutUint16(hdr[20:22], 1)
	binary.LittleEndian.PutUint16(hdr[22:24], ch)
	binary.LittleEndian.PutUint32(hdr[24:28], sr)
	binary.LittleEndian.PutUint32(hdr[28:32], sr*ch*bits/8)
	binary.LittleEndian.PutUint16(hdr[32:34], ch*bits/8)
	binary.LittleEndian.PutUint16(hdr[34:36], bits)
	copy(hdr[36:40], "data")
	binary.LittleEndian.PutUint32(hdr[40:44], uint32(len(pcm)))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(hdr); err != nil {
		return err
	}
	_, err = f.Write(pcm)
	return err
}

// pcmRMS 粗略判断是否有声音（int16 小端）
func pcmRMS(pcm []byte) float64 {
	n := len(pcm) / 2
	if n == 0 {
		return 0
	}
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		sum += v * v
	}
	return math.Sqrt(sum / float64(n))
}

func firstN(b []byte, n int) string {
	s := string(b)
	if len(s) > n {
		s = s[:n] + "…"
	}
	return s
}
