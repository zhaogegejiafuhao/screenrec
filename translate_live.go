package main

// 实时字幕用的翻译客户端：复用 translate.py --serve 常驻进程，
// 单次请求一行 JSON，避免每句都重新加载 300MB 模型。

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type liveMT struct {
	cmd *exec.Cmd
	in  io.WriteCloser
	out *bufio.Reader
}

// translateLive 翻译一句（串行调用，内部有超时与崩溃重启）
func (a *App) translateLive(text, dir string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return "", nil
	}
	if dir == "" || dir == "auto" {
		dir = detectDirection(text)
	}

	a.ltMu.Lock()
	defer a.ltMu.Unlock()

	if err := a.ltEnsure(); err != nil {
		return "", err
	}

	payload, _ := json.Marshal(map[string]any{"id": 1, "dir": dir, "texts": []string{text}})
	if _, err := a.lt.in.Write(append(payload, '\n')); err != nil {
		a.ltReset()
		return "", fmt.Errorf("翻译进程已退出：%v", err)
	}

	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	out := a.lt.out
	go func() {
		line, err := out.ReadString('\n')
		ch <- result{line, err}
	}()

	select {
	case r := <-ch:
		if r.err != nil {
			a.ltReset()
			return "", fmt.Errorf("翻译进程无响应：%v", r.err)
		}
		var resp struct {
			Texts []string `json:"texts"`
			Error string   `json:"error"`
		}
		if err := json.Unmarshal([]byte(r.line), &resp); err != nil {
			return "", fmt.Errorf("翻译响应解析失败：%v", err)
		}
		if resp.Error != "" {
			return "", fmt.Errorf("翻译出错：%s", resp.Error)
		}
		if len(resp.Texts) == 0 {
			return "", fmt.Errorf("翻译没有返回结果")
		}
		return resp.Texts[0], nil
	case <-time.After(90 * time.Second):
		a.ltReset()
		return "", fmt.Errorf("翻译超时（模型可能正在加载）")
	}
}

// ltEnsure 懒启动常驻翻译进程（首次约 20 秒加载模型）
func (a *App) ltEnsure() error {
	if a.lt != nil {
		return nil
	}
	if a.python == "" {
		return fmt.Errorf("未找到 Python，无法翻译")
	}
	script := filepath.Join(a.assets, "translate.py")
	if _, err := os.Stat(script); err != nil {
		return fmt.Errorf("缺少 translate.py")
	}
	modelDir := filepath.Join(a.assets, "models", "mt")
	if _, err := os.Stat(modelDir); err != nil {
		return fmt.Errorf("缺少翻译模型（models/mt）")
	}

	cmd := exec.Command(a.python, "-u", script, "--serve", "--models", modelDir)
	cmd.Dir = a.assets
	in, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	cmd.Stderr = io.Discard // 首次加载的警告不刷屏
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动翻译进程失败：%v", err)
	}
	a.lt = &liveMT{cmd: cmd, in: in, out: bufio.NewReaderSize(out, 1<<16)}
	return nil
}

func (a *App) ltReset() {
	if a.lt == nil {
		return
	}
	_ = a.lt.in.Close()
	if a.lt.cmd.Process != nil {
		_ = a.lt.cmd.Process.Kill()
	}
	a.lt = nil
}

// warmLiveTranslate 后台预热翻译进程，避免开启实时双语后的第一句等太久
func (a *App) warmLiveTranslate(dir string) {
	if dir == "" || dir == "auto" {
		dir = "en2zh"
	}
	text := "warm up the translation model"
	if dir == "zh2en" {
		text = "预热翻译模型"
	}
	_, _ = a.translateLive(text, dir)
}

// CloseLiveTranslate 退出时清理
func (a *App) CloseLiveTranslate() {
	a.ltMu.Lock()
	defer a.ltMu.Unlock()
	a.ltReset()
}
