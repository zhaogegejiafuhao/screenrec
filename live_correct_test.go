package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// 实时纠错的核心链路：新字幕入队 → 后台调模型 → 原地替换。
// 这里用 mock 的 OpenAI 兼容服务验证，不依赖真模型。
func TestLiveCorrectorReplacesSegment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Messages []chatMessage `json:"messages"`
		}
		json.NewDecoder(r.Body).Decode(&req)
		text := ""
		if len(req.Messages) > 0 {
			text = req.Messages[len(req.Messages)-1].Content
		}
		// 模拟模型：把「买点」改成「埋点」
		fixed := strings.ReplaceAll(text, "买点", "埋点")
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{
				{"message": map[string]string{"content": fixed}},
			},
		})
	}))
	defer srv.Close()

	rec := &Recording{
		ID:     "t1",
		Status: "recording",
		Segments: []Segment{
			{Start: 0, End: 3, Text: "付费转化的买点要在周四前补完。"},
		},
	}
	a := &App{}
	spec := llmSpec{base: srv.URL, model: "mock", timeout: 5 * time.Second}

	lc := newLiveCorrector(a, rec, spec)
	defer lc.close()
	lc.push(0)

	// 等后台改完
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		a.mu.RLock()
		got := rec.Segments[0].Text
		a.mu.RUnlock()
		if got == "付费转化的埋点要在周四前补完。" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Errorf("字幕没被纠正，当前：%q", rec.Segments[0].Text)
}

// 录制已经停止/段落被全量转写替换时，不能再拿旧结果覆盖
func TestLiveCorrectorSkipsWhenStopped(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "改过的"}}},
		})
	}))
	defer srv.Close()

	rec := &Recording{ID: "t2", Status: "done", Segments: []Segment{{Text: "原文内容够长了吧"}}}
	a := &App{}
	lc := newLiveCorrector(a, rec, llmSpec{base: srv.URL, model: "mock", timeout: 2 * time.Second})
	defer lc.close()
	lc.push(0)

	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(&calls) != 0 {
		t.Error("录制已结束就不该再调模型")
	}
	if rec.Segments[0].Text != "原文内容够长了吧" {
		t.Errorf("段落被意外改写：%q", rec.Segments[0].Text)
	}
}

// 太短的一句不值得跑模型（"对""嗯""好"）
func TestLiveCorrectorSkipsShortText(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "好"}}},
		})
	}))
	defer srv.Close()

	rec := &Recording{ID: "t3", Status: "recording", Segments: []Segment{{Text: "好"}}}
	a := &App{}
	lc := newLiveCorrector(a, rec, llmSpec{base: srv.URL, model: "mock", timeout: 2 * time.Second})
	defer lc.close()
	lc.push(0)

	time.Sleep(300 * time.Millisecond)
	if atomic.LoadInt32(&calls) != 0 {
		t.Error("三个字以下不该调模型")
	}
}

// 队列满时必须立刻丢弃，绝不能阻塞录制线程
func TestLiveCorrectorPushNeverBlocks(t *testing.T) {
	rec := &Recording{ID: "t4", Status: "recording"}
	a := &App{}
	lc := &liveCorrector{
		app:  a,
		rec:  rec,
		spec: llmSpec{},
		ch:   make(chan int, 2),
		done: make(chan struct{}),
	}
	// 不启动 loop，让队列塞满
	start := time.Now()
	for i := 0; i < 100; i++ {
		lc.push(i)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("push 阻塞了 %v，录制线程会被拖住", d)
	}
}
