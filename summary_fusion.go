package main

// 融合纪要：规则抽候选（召回高）+ 模型甄别改写（精确高）。
//
// 为什么要这么做：评测数据是这么说的——规则抽取召回 0.61 但精确只有 0.23
// （十条里抽中九条、混进去七条噪声），小模型反过来精确 0.39、召回 0.45。
// 两个模型的失败原因不同：规则输在「分不清哪些话是待办」，模型输在「读长文时会漏」。
// 那就让规则负责「别漏」，把候选连同原文一起交给模型负责「别抽错」。

import (
	"context"
	"fmt"
	"strings"
	"time"
)

const fusionPrompt = `你是一名严谨的会议纪要助手。输入包含一段会议转写，以及一份「候选待办」列表。

候选待办由关键词规则自动抽取，**以不漏为目标，因此混有不少噪声**——
闲聊、陈述事实、已经做完的事、别人的事，都不是待办，应当剔除。

你要做两件事：
1. 甄别候选：只保留真正的行动项（有人要做、而且还没做的事）。
   如果转写里存在明确的行动项但候选列表漏掉了，请补上。
2. 把保留的每条改写成不超过 25 字的祈使句任务，保留关键数字、人名、产品名与时间点。

只依据给出的转写内容整理，不要编造任何信息。输出 Markdown，结构固定：

## 一句话总结
（不超过 60 字）

## 议题
- 逐条列出讨论的主题

## 结论
- 已达成的决定或共识

## 待办
- [ ] 事项 — 负责人（未提及写「未指定」）/ 时间（未提及写「未指定」）

## 风险与分歧
- 未解决的问题、不同意见（没有就写「无明显分歧」）

要求：
- 待办一条一行、不加解释；重复的要合并
- 用中文输出；原文是英文时仍用中文总结，但专有名词保留原文
- 信息不足的部分直接写「原文未提及」，不要推测`

// maxFusionCands 候选超过这个数就只取前面的：4B 模型处理不了太长的列表，
// 而且规则抽取本来就是宁滥勿缺，后面的边际价值很低
const maxFusionCands = 40

// fusionSummary 走「规则候选 + 模型改写」生成纪要
func (a *App) fusionSummary(rec *Recording, spec llmSpec) (string, error) {
	a.mu.RLock()
	snap := *rec
	snap.Segments = append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()

	if len(snap.Segments) == 0 {
		return "", fmt.Errorf("这条记录还没有转写内容")
	}

	cands := extractActionItems(snap.Segments)
	total := len(cands)
	if total > maxFusionCands {
		cands = cands[:maxFusionCands]
	}
	input, truncated := buildFusionInput(&snap, cands, 12000)

	ctx, cancel := context.WithTimeout(context.Background(), spec.timeout)
	defer cancel()

	content, err := chatCompletionEx(ctx, spec.base, spec.key, spec.model, []chatMessage{
		{Role: "system", Content: fusionPrompt},
		{Role: "user", Content: input},
	}, spec.timeout, spec.extra)
	if err != nil {
		return "", err
	}
	if truncated {
		content += "\n\n> 注：转写内容过长，纪要仅基于前一部分生成。"
	}
	content += fmt.Sprintf("\n\n> 待办来自规则抽取的 %d 条候选，经 %s 甄别改写。", total, spec.label)

	now := time.Now()
	a.mu.Lock()
	rec.Summary = content
	rec.SummaryModel = spec.label + "（规则候选+甄别）"
	rec.SummaryAt = &now
	a.mu.Unlock()
	a.recorder.save(rec)

	return content, nil
}

// buildFusionInput 转写原文在前、规则候选在后。
// 候选带上时间戳，方便模型回到原文上下文判断这句话到底是不是待办。
func buildFusionInput(rec *Recording, cands []ActionItem, maxRunes int) (string, bool) {
	var b strings.Builder
	fmt.Fprintf(&b, "标题：%s\n\n转写：\n", rec.Title)
	truncated := false
	count := 0
	for _, s := range rec.Segments {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		label := ""
		if rec.Speakers > 0 && s.Speaker > 0 {
			label = fmt.Sprintf("说话人%d: ", s.Speaker)
		}
		line := fmt.Sprintf("[%s] %s%s\n", clock(s.Start), label, text)
		n := len([]rune(line))
		if count+n > maxRunes {
			truncated = true
			break
		}
		b.WriteString(line)
		count += n
	}

	b.WriteString("\n候选待办（规则抽取，含噪声，请甄别）：\n")
	if len(cands) == 0 {
		b.WriteString("（规则没有抽到任何候选，请完全依据转写自行判断）\n")
	}
	for i, c := range cands {
		fmt.Fprintf(&b, "%d. [%s] %s\n", i+1, clock(c.At), c.Text)
	}
	return b.String(), truncated
}
