package main

import (
	"os/exec"
	"screenrec/internal/winhide"
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// StreamASR 常驻流式识别进程（Paraformer streaming）
type StreamASR struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	ready  bool
	app    *App
}

func NewStreamASR(app *App) *StreamASR {
	return &StreamASR{app: app}
}

func (s *StreamASR) startLocked() error {
	if s.ready && s.cmd != nil && s.cmd.ProcessState == nil {
		return nil
	}
	if s.app.python == "" {
		return fmt.Errorf("未找到 Python")
	}
	script := filepath.Join(s.app.assets, "asr_stream.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("缺少 asr_stream.py")
	}
	cmd := winhide.Command(s.app.python, script)
	cmd.Dir = s.app.assets
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
	s.cmd, s.stdin, s.reader, s.ready = cmd, stdin, bufio.NewReader(stdout), true
	return nil
}

// Warm 启动流式进程并等待模型加载完成（首帧前调用，避免第一次识别卡十几秒）
func (s *StreamASR) Warm() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return err
	}
	hdr := make([]byte, 4) // 长度 0 = ping
	if _, err := s.stdin.Write(hdr); err != nil {
		s.ready = false
		return err
	}
	line, err := s.reader.ReadString('\n')
	if err != nil {
		s.ready = false
		return err
	}
	if !strings.Contains(line, "\"ok\"") {
		return fmt.Errorf("流式识别预热异常: %s", strings.TrimSpace(line))
	}
	return nil
}

// Feed 送入一段 PCM，返回当前草稿与（可能的）整句结果
func (s *StreamASR) Feed(pcm []byte) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.startLocked(); err != nil {
		return "", "", err
	}
	hdr := make([]byte, 4)
	binary.LittleEndian.PutUint32(hdr, uint32(len(pcm)))
	if _, err := s.stdin.Write(hdr); err != nil {
		s.ready = false
		return "", "", err
	}
	if _, err := s.stdin.Write(pcm); err != nil {
		s.ready = false
		return "", "", err
	}
	type res struct {
		line string
		err  error
	}
	ch := make(chan res, 1)
	go func() {
		line, err := s.reader.ReadString('\n')
		ch <- res{line, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			s.ready = false
			return "", "", r.err
		}
		var v struct {
			Partial string `json:"partial"`
			Final   string `json:"final"`
		}
		if err := json.Unmarshal([]byte(r.line), &v); err != nil {
			return "", "", err
		}
		return v.Partial, v.Final, nil
	case <-time.After(20 * time.Second):
		return "", "", fmt.Errorf("流式识别超时")
	}
}

func (s *StreamASR) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stdin != nil {
		s.stdin.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		s.cmd.Process.Kill()
	}
	s.ready = false
}

// streamReady 流式模型是否就位
func (a *App) streamReady() bool {
	for _, f := range []string{"encoder.int8.onnx", "decoder.int8.onnx", "tokens.txt"} {
		if _, err := os.Stat(filepath.Join(a.assets, "models", "stream", f)); err != nil {
			return false
		}
	}
	return a.python != ""
}
