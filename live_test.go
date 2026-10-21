package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// TestLiveLoop 验证「PCM 文件 → 实时分段 → 识别文本」整条链路
func TestLiveLoop(t *testing.T) {
	python := findPython()
	if python == "" {
		t.Skip("未找到 Python，跳过")
	}
	assets, _ := filepath.Abs(".")
	wavPath := filepath.Join(assets, "test16k.wav")
	raw, err := os.ReadFile(wavPath)
	if err != nil {
		t.Skip("缺少测试音频 test16k.wav")
	}
	pcm := raw[44:] // 跳过标准 WAV 头
	// 末尾补 2 秒静音，触发流式识别的端点检测
	pcm = append(pcm, make([]byte, 32000*2)...)

	dir := t.TempDir()
	pcmPath := filepath.Join(dir, "a.pcm")
	if err := os.WriteFile(pcmPath, pcm, 0644); err != nil {
		t.Fatal(err)
	}

	app := &App{python: python, assets: assets, dataDir: dir, recordsDir: dir}
	app.asr = NewASR(app)
	app.stream = NewStreamASR(app)
	defer app.asr.Close()
	defer app.stream.Close()
	// 预热两个识别进程（首次加载模型较慢）
	if err := app.stream.Warm(); err != nil {
		t.Logf("流式引擎预热失败，将走分块回退: %v", err)
	}
	r := NewRecorder(app)

	rec := &Recording{ID: "test", Status: "recording", Segments: []Segment{}}
	stop := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		r.liveLoop(rec, pcmPath, stop)
	}()

	time.Sleep(18 * time.Second)
	close(stop)
	wg.Wait()

	r.mu.Lock()
	segs := append([]Segment{}, rec.Segments...)
	r.mu.Unlock()

	t.Logf("识别到 %d 段：%s", len(segs), joinText(segs))
	if len(segs) == 0 {
		t.Fatal("没有识别出任何文字")
	}
	for _, s := range segs {
		if s.Text == "" || s.End <= s.Start {
			t.Fatalf("分段异常: %+v", s)
		}
	}
}
