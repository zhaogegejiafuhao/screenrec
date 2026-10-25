package main

import (
	"os/exec"
	"screenrec/internal/winhide"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 本地小模型纪要引擎。
//
// 思路：不自己实现推理，直接对接本机 llama.cpp 的 llama-server（OpenAI 兼容接口），
// 这样代码里只要换个 base URL 就能在「云端 API」和「本机模型」之间切换，
// 也不用把几 GB 的模型塞进发布包——没有模型就走规则抽取，想用再下载。
//
// 若用户填了 llama-server.exe 与 .gguf 的路径，程序生成纪要时会自动把服务拉起来，
// 自己退出时再把它关掉，不让后台留个吃显存的进程。

type localLLM struct {
	mu    sync.Mutex
	cmd   *exec.Cmd
	port  string
	ready bool
	at    time.Time
}

// localLLMUp 探测本地推理服务是否在（800ms 内没回就算不在）
func (a *App) localLLMUp(base string) bool {
	u := strings.TrimRight(base, "/")
	if u == "" {
		return false
	}
	cli := &http.Client{Timeout: 800 * time.Millisecond}
	resp, err := cli.Get(u + "/models")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == 200
}

// portOf 从 http://127.0.0.1:8080/v1 里取出 8080
func portOf(base string) string {
	u, err := url.Parse(base)
	if err != nil || u.Port() == "" {
		u2, err2 := url.Parse("http://" + base)
		if err2 != nil || u2.Port() == "" {
			return "8080"
		}
		return u2.Port()
	}
	return u.Port()
}

// ensureLocalLLM 保证本地推理服务可用：已在不动，没在就尝试拉起。
// 返回错误时调用方应退回规则抽取，不要卡住用户。
func (a *App) ensureLocalLLM(bin, gguf, base string, gpuLayers int) error {
	if base == "" {
		return fmt.Errorf("没填本地模型地址")
	}
	if a.localLLMUp(base) {
		return nil
	}
	if bin == "" || gguf == "" {
		return fmt.Errorf("本地模型服务没启动，也没填 llama-server.exe / .gguf 路径")
	}
	if _, err := os.Stat(bin); err != nil {
		return fmt.Errorf("找不到 llama-server.exe：%s", bin)
	}
	if _, err := os.Stat(gguf); err != nil {
		return fmt.Errorf("找不到模型文件：%s", gguf)
	}

	a.llm.mu.Lock()
	defer a.llm.mu.Unlock()
	if a.llm.cmd != nil {
		// 刚拉起过，说明还在加载模型：2.5GB 的 gguf 从机械盘读出来实测要 85 秒，
		// 原来只等 60 秒，冷启动必超时。给到 180 秒，同时盯着进程有没有提前死掉。
		deadline := time.Now().Add(180 * time.Second)
		for time.Now().Before(deadline) {
			if a.localLLMUp(base) {
				return nil
			}
			if a.llm.cmd.ProcessState != nil {
				a.llm.cmd = nil
				return fmt.Errorf("本地模型服务已退出，看 llama-server.log")
			}
			time.Sleep(time.Second)
		}
		return fmt.Errorf("本地模型服务启动超时（180s）：模型可能太大或磁盘太慢，看 llama-server.log")
	}
	return a.startLocked(bin, gguf, base, gpuLayers)
}

// startLocked 拉起 llama-server 并等它就绪（调用方持锁）
func (a *App) startLocked(bin, gguf, base string, gpuLayers int) error {
	port := portOf(base)
	ngl := gpuLayers
	if ngl == 0 {
		ngl = -1
	}
	args := []string{
		"-m", gguf,
		"--host", "127.0.0.1",
		"--port", port,
		"-c", "4096",
		"-ngl", fmt.Sprintf("%d", ngl),
		"-t", "8",
		"--alias", "local",
	}
	cmd := winhide.Command(bin, args...)
	logFile := filepath.Join(a.dataDir, "llama-server.log")
	if f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
		cmd.Stdout = f
		cmd.Stderr = f
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动本地模型失败：%v", err)
	}
	a.llm.cmd = cmd
	a.llm.port = port
	a.llm.at = time.Now()
	log.Printf("[llm] 已拉起本地模型服务 pid=%d port=%s gpu层=%d", cmd.Process.Pid, port, ngl)

	// 模型加载 + CUDA 初始化要一会儿：2.5GB gguf 在机械盘上实测 85 秒，给 180 秒
	deadline := time.Now().Add(180 * time.Second)
	for time.Now().Before(deadline) {
		if a.localLLMUp(base) {
			a.llm.ready = true
			log.Printf("[llm] 本地模型就绪")
			return nil
		}
		if cmd.ProcessState != nil {
			return fmt.Errorf("本地模型服务已退出，看 %s", logFile)
		}
		time.Sleep(time.Second)
	}
	return fmt.Errorf("本地模型服务启动超时（180s），看 %s", logFile)
}

// StopLocalLLM 退出时收掉自己拉起的模型服务
func (a *App) StopLocalLLM() {
	a.llm.mu.Lock()
	defer a.llm.mu.Unlock()
	if a.llm.cmd == nil || a.llm.cmd.Process == nil {
		return
	}
	_ = a.llm.cmd.Process.Kill()
	_, _ = a.llm.cmd.Process.Wait()
	a.llm.cmd = nil
	a.llm.ready = false
	log.Printf("[llm] 已关闭本地模型服务")
}

// portOpen 备用：直接探端口（llama-server 的 /models 有时要更久才有响应）
func portOpen(addr string) bool {
	c, err := net.DialTimeout("tcp", addr, 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}
