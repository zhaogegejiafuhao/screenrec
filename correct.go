package main

// 转写纠错：用大模型修同音字与明显错别字。
//
// 为什么需要：SenseVoice 在嘈杂环境、口音、专业术语上会出同音错——
// 实测「埋点」被识别成「买点」、「登录页」被识别成「当陆夜」。这类错误
// 靠换更大的 ASR 模型也只是缓解，而模型读过上下文就知道「登录页改版」是个
// 产品术语，能把它改回来。
//
// 三条硬约束：只改错字、不改写句子、不增删内容。行数对不上就整批放弃，
// 宁可保留原文也不让模型"润色"用户的会议记录。

import (
	"context"
	"fmt"
	"log"
	"strings"
)

const correctPrompt = `你是语音识别结果的后处理器。输入是会议转写的若干行，每行以「序号|」开头。

任务：只修正**同音字**和**明显的错别字**，例如：
- 「买点」「埋点」→ 埋点（产品术语）
- 「当陆夜」→ 登录页
- 「接口文当」→ 接口文档

严格要求：
1. 不许改写句子、不许润色、不许调整语序
2. 不许增加或删除内容，不许合并或拆分行
3. 拿不准的地方保持原样——宁可留错也不许猜
4. 专有名词、人名、数字、时间不要动

输出格式：与输入**完全相同的行数**，每行「序号|修正后的文本」。不要输出任何解释、不要加代码块标记。`

// noThinkExtra 给本地 llama-server 的请求关掉思考模式。
//
// Qwen3 默认先输出一大段思考，纠错这种简单任务会被思考吃光 max_tokens，
// 实测直接返回空字符串（表现为「模型没改」）。云端接口不认识这个参数，只对本地加。
func noThinkExtra(spec llmSpec) map[string]any {
	ex := map[string]any{}
	for k, v := range spec.extra {
		ex[k] = v
	}
	if strings.Contains(spec.base, "127.0.0.1") || strings.Contains(spec.base, "localhost") {
		ex["chat_template_kwargs"] = map[string]any{"enable_thinking": false}
	}
	return ex
}

// correctSegments 修正转写里的同音字。返回改动条数；0 表示没改或没跑成。
func (a *App) correctSegments(rec *Recording, spec llmSpec) int {
	a.mu.RLock()
	segs := append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()
	if len(segs) == 0 {
		return 0
	}

	a.mu.Lock()
	rec.Correcting = true
	a.mu.Unlock()

	changed := 0
	// 每批 30 行：小模型一次喂太多会漏行、乱行，太少则往返次数多；
	// 实测 30 行在 4B 模型上还没出现漏行，而一批约 3~5 秒
	const batch = 30
	for i := 0; i < len(segs); i += batch {
		end := i + batch
		if end > len(segs) {
			end = len(segs)
		}
		part := segs[i:end]

		var b strings.Builder
		for j, s := range part {
			fmt.Fprintf(&b, "%d|%s\n", j+1, strings.TrimSpace(s.Text))
		}

		ctx, cancel := context.WithTimeout(context.Background(), spec.timeout)
		content, err := chatCompletionEx(ctx, spec.base, spec.key, spec.model, []chatMessage{
			{Role: "system", Content: correctPrompt},
			{Role: "user", Content: b.String()},
		}, spec.timeout, noThinkExtra(spec))
		cancel()
		if err != nil {
			log.Printf("[correct] 第 %d 批纠错失败（保留原文）：%v", i/batch+1, err)
			continue
		}

		fixed, ok := parseCorrected(content, len(part))
		if !ok {
			// 行数对不上就直接放弃这一批——错位的文本比错字更糟
			log.Printf("[correct] 第 %d 批输出行数不符，跳过", i/batch+1)
			continue
		}
		for j := range part {
			if fixed[j] != "" && fixed[j] != strings.TrimSpace(part[j].Text) {
				part[j].Text = fixed[j]
				changed++
			}
		}
	}

	a.mu.Lock()
	rec.Correcting = false
	a.mu.Unlock()
	if changed == 0 {
		a.recorder.save(rec)
		return 0
	}
	a.mu.Lock()
	rec.Segments = segs
	rec.LiveText = joinText(segs)
	rec.Corrected = changed
	a.mu.Unlock()
	a.recorder.save(rec)
	a.RebuildSearchIndex(rec.ID)
	return changed
}

// parseCorrected 解析「序号|文本」格式，要求行数与段数严格一致
func parseCorrected(out string, want int) ([]string, bool) {
	lines := make([]string, want)
	seen := 0
	for _, raw := range strings.Split(out, "\n") {
		ln := strings.TrimSpace(raw)
		if ln == "" {
			continue
		}
		// 容忍模型加上的 Markdown 代码块符号和列表符号
		ln = strings.TrimPrefix(ln, "- ")
		ln = strings.TrimSpace(strings.Trim(ln, "`"))
		pipe := strings.IndexAny(ln, "|｜")
		if pipe <= 0 {
			continue
		}
		var idx int
		if _, err := fmt.Sscanf(strings.TrimSpace(ln[:pipe]), "%d", &idx); err != nil {
			continue
		}
		if idx < 1 || idx > want {
			continue
		}
		// 全角分隔符占 3 字节，别按 1 字节切，否则首字会被吃掉
		sepLen := 1
		if strings.HasPrefix(ln[pipe:], "｜") {
			sepLen = len("｜")
		}
		text := strings.TrimSpace(ln[pipe+sepLen:])
		// 去掉模型可能加的首尾引号
		text = strings.Trim(text, "\"“”")
		if text == "" {
			continue
		}
		lines[idx-1] = text
		seen++
	}
	// 必须每一行都有结果，缺一行就整批不用
	if seen != want {
		return nil, false
	}
	return lines, true
}

// healSegments 给 segments 做纠错前的快照判断：全是空文本就别浪费模型时间
func healable(segs []Segment) bool {
	for _, s := range segs {
		if strings.TrimSpace(s.Text) != "" {
			return true
		}
	}
	return false
}
