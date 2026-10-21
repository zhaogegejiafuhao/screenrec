package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestStreamFeed 只验证 StreamASR 客户端（不含 liveLoop），用于定位问题
func TestStreamFeed(t *testing.T) {
	if testing.Short() {
		t.Skip("慢测试，-short 下跳过")
	}
	python := findPython()
	if python == "" {
		t.Skip("未找到 Python")
	}
	assets, _ := filepath.Abs(".")
	raw, err := os.ReadFile(filepath.Join(assets, "test16k.wav"))
	if err != nil {
		t.Skip("缺少 test16k.wav")
	}
	pcm := append(append([]byte{}, raw[44:]...), make([]byte, 64000)...)

	app := &App{python: python, assets: assets}
	s := NewStreamASR(app)
	defer s.Close()
	if err := s.Warm(); err != nil {
		t.Fatalf("预热失败: %v", err)
	}

	var finals []string
	last := ""
	for i := 0; i < len(pcm); i += 32000 {
		end := i + 32000
		if end > len(pcm) {
			end = len(pcm)
		}
		partial, final, err := s.Feed(pcm[i:end])
		if err != nil {
			t.Fatalf("第 %d 段失败: %v", i/32000, err)
		}
		if partial != "" {
			last = partial
		}
		if final != "" {
			finals = append(finals, final)
		}
		time.Sleep(120 * time.Millisecond)
	}
	t.Logf("finals=%q lastPartial=%q", finals, last)
	if len(finals) == 0 {
		t.Fatal("没有收到整句结果")
	}
}
