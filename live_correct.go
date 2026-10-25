package main

// 实时字幕的「先出后改」。
//
// 豆包的实时字幕又准又实时，是因为它用端到端强模型，解码时就带着上下文，
// 不存在「先出结果再修正」这一步。我们的链路是 ASR + 后处理，两步必然有延迟，
// 所以换成另一个思路：
//
//	字幕照常立刻显示 → 模型在后台把刚说完的那句纠错 → 前端静默替换
//
// 用户看到的效果是：字幕先出现，个别字过一两秒闪一下变对。
// 代价是持续占用模型——视频录制时可能和 CPU 编码抢资源，所以做成了可关的设置项。

import (
	"context"
	"log"
	"strings"
	"sync"
	"time"
)

// 单句纠错的提示词必须带示例：实测只写「修正同音字」时，4B 模型会保守到原样输出，
// 给出两个「错→对」和一个「不用改」的对照后才会真的动手。
const correctOnePrompt = `你去修正语音识别结果里的同音字错别字。只输出修正后的句子，不要解释、不要引号、不要换行。

付费转化的买点请小李补完 → 付费转化的埋点请小李补完
当陆夜改版本周五上线 → 登录页改版本周五上线
这个方案我觉得可以 → 这个方案我觉得可以

只改同音字和明显错字，不改写句子，拿不准就原样输出。`

// liveCorrector 录制期间的后台纠错队列
type liveCorrector struct {
	app   *App
	rec   *Recording
	spec  llmSpec
	ch    chan int
	done  chan struct{}
	once  sync.Once
	fixed int // 改过的句数，停止时写回记录
}

func newLiveCorrector(a *App, rec *Recording, spec llmSpec) *liveCorrector {
	lc := &liveCorrector{
		app:  a,
		rec:  rec,
		spec: spec,
		// 缓冲 16 句：模型慢的时候不阻塞录制线程，满了就丢最旧的
		ch:   make(chan int, 16),
		done: make(chan struct{}),
	}
	go lc.loop()
	return lc
}

// push 把新段落下标塞进队列。绝不阻塞——调用方正持有录制锁。
func (lc *liveCorrector) push(idx int) {
	if idx < 0 {
		return
	}
	select {
	case lc.ch <- idx:
	default:
		// 队列满说明模型跟不上说话速度：丢掉这一句，等停止后的全量纠错兜底
	}
}

func (lc *liveCorrector) close() {
	lc.once.Do(func() { close(lc.done) })
}

func (lc *liveCorrector) loop() {
	for {
		select {
		case <-lc.done:
			return
		case idx := <-lc.ch:
			lc.fixOne(idx)
		}
	}
}

func (lc *liveCorrector) fixOne(idx int) {
	a := lc.app
	a.mu.RLock()
	if idx >= len(lc.rec.Segments) || lc.rec.Status != "recording" {
		a.mu.RUnlock()
		return
	}
	orig := lc.rec.Segments[idx].Text
	a.mu.RUnlock()

	// 太短的一句（"对""嗯"）不值得跑一次模型
	if len([]rune(strings.TrimSpace(orig))) < 5 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	fixed, err := chatCompletionEx(ctx, lc.spec.base, lc.spec.key, lc.spec.model, []chatMessage{
		{Role: "system", Content: correctOnePrompt},
		{Role: "user", Content: orig},
	}, lc.spec.timeout, noThinkExtra(lc.spec))
	cancel()
	if err != nil {
		log.Printf("[livefix] 纠错失败（保留原文）：%v", err)
		return
	}
	fixed = strings.TrimSpace(fixed)
	fixed = strings.Trim(fixed, "\"“”`")
	if fixed == "" || fixed == orig {
		return
	}
	// 模型偶尔会输出多行解释，只要不是单句就放弃
	if strings.ContainsAny(fixed, "\n") {
		return
	}

	a.mu.Lock()
	// 期间可能已经被全量转写覆盖，确认还是原文才替换
	if idx < len(lc.rec.Segments) && lc.rec.Segments[idx].Text == orig {
		lc.rec.Segments[idx].Text = fixed
		lc.rec.LiveText = joinText(lc.rec.Segments)
		lc.fixed++
		a.mu.Unlock()
		log.Printf("[livefix] %q → %q", orig, fixed)
		return
	}
	a.mu.Unlock()
}

// startLiveFix 录制开始时在后台准备纠错器。
// 模型冷启动要一两分钟，所以不能阻塞录制开始——模型就绪前的字幕不纠错，
// 停止后的全量纠错会补上。
func (a *App) startLiveFix(rec *Recording) {
	if !a.set.AutoCorrect() || !a.set.LiveFixOn() {
		return
	}
	engine, base, key, model, localURL, localModel, localBin, localGGUF, gpuLayers := a.set.LLMEngineConfig()
	if engine == "off" {
		return
	}
	go func() {
		spec, err := a.pickModel(engine, base, key, model, localURL, localModel, localBin, localGGUF, gpuLayers)
		if err != nil {
			log.Printf("[livefix] 没有可用模型，实时纠错关闭：%v", err)
			return
		}
		// pickModel 可能跑了一两分钟，这期间录制可能已经结束
		a.mu.RLock()
		live := a.recorder.Current()
		ok := live != nil && live.ID == rec.ID && live.Status == "recording"
		a.mu.RUnlock()
		if !ok {
			return
		}
		lc := newLiveCorrector(a, rec, spec)
		a.liveMu.Lock()
		a.liveFix = lc
		a.liveMu.Unlock()
		log.Printf("[livefix] 实时纠错已就绪: %s", rec.ID)
	}()
}

// stopLiveFix 录制结束时收工
func (a *App) stopLiveFix() {
	a.liveMu.Lock()
	lc := a.liveFix
	a.liveFix = nil
	a.liveMu.Unlock()
	if lc != nil {
		lc.close()
		// 把实时改过的条数累加进记录，详情里能看到「已校对 N 处」
		if lc.fixed > 0 {
			a.mu.Lock()
			lc.rec.Corrected += lc.fixed
			a.mu.Unlock()
			a.recorder.save(lc.rec)
			log.Printf("[livefix] 实时纠错共改动 %d 句: %s", lc.fixed, lc.rec.ID)
		}
	}
}

// pushLiveFix 由实时转写线程调用（持有 a.mu）
func (a *App) pushLiveFix(idx int) {
	a.liveMu.Lock()
	lc := a.liveFix
	a.liveMu.Unlock()
	if lc == nil {
		return
	}
	lc.push(idx)
}
