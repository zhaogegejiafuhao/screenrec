package main

import (
	"strings"
	"testing"
)

// 融合纪要靠「候选列表」把规则的高召回喂给模型，输入格式错了模型就抓瞎，
// 所以候选的时间戳、序号、截断标记都要盯住。
func TestBuildFusionInput(t *testing.T) {
	rec := &Recording{Title: "评审会", Segments: []Segment{
		{Start: 12, End: 15, Text: "那我们开始吧"},
		{Start: 30, End: 36, Text: "埋点周四之前补一下"},
	}}
	cands := []ActionItem{
		{Text: "埋点周四之前补一下", At: 30},
		{Text: "文案周五之前给设计师确认", At: 42},
	}

	input, truncated := buildFusionInput(rec, cands, 10000)
	if truncated {
		t.Error("内容很短，不该被截断")
	}

	for _, want := range []string{"标题：评审会", "[00:12] 那我们开始吧", "[00:30] 埋点周四之前补一下"} {
		if !strings.Contains(input, want) {
			t.Errorf("输入里应包含 %q，实际：\n%s", want, input)
		}
	}
	// 候选必须带序号和时间戳，模型才能回到原文上下文
	if !strings.Contains(input, "1. [00:30] 埋点周四之前补一下") {
		t.Errorf("候选没带上序号和时间戳：\n%s", input)
	}
	if !strings.Contains(input, "候选待办（规则抽取，含噪声，请甄别）") {
		t.Error("候选段标题缺失，模型不知道这是带噪声的候选")
	}
}

func TestBuildFusionInputEmptyCandidates(t *testing.T) {
	rec := &Recording{Title: "t", Segments: []Segment{{Start: 0, End: 2, Text: "随便聊聊"}}}
	input, _ := buildFusionInput(rec, nil, 10000)
	if !strings.Contains(input, "规则没有抽到任何候选") {
		t.Errorf("没有候选时要明确告诉模型自行判断：\n%s", input)
	}
}

func TestBuildFusionInputTruncates(t *testing.T) {
	var segs []Segment
	for i := 0; i < 50; i++ {
		segs = append(segs, Segment{Start: float64(i * 10), End: float64(i*10 + 8), Text: "这里是一段比较长的会议发言内容，用来把输入撑到超过上限。"})
	}
	rec := &Recording{Title: "长会", Segments: segs}
	_, truncated := buildFusionInput(rec, nil, 200)
	if !truncated {
		t.Error("超过 maxRunes 应该标记截断")
	}
}

// 融合的提示词要说清两件事：候选里混着噪声（要甄别）、以及可以补充漏掉的待办。
// 少任何一句，模型要么照单全收（等于规则）要么只用模型自己抽的（等于不用候选）。
func TestFusionPrompt(t *testing.T) {
	for _, want := range []string{"混有不少噪声", "补上", "不超过 25 字", "## 待办"} {
		if !strings.Contains(fusionPrompt, want) {
			t.Errorf("融合提示词应包含 %q", want)
		}
	}
}
