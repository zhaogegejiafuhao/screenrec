package main

import (
	"strings"
	"testing"
)

func TestExtractActionItems(t *testing.T) {
	segs := []Segment{
		{Start: 1, End: 3, Text: "这个方案我们下周再确认一下。"},
		{Start: 4, End: 6, Text: "小张负责跟进合同，明天给回复。"},
		{Start: 7, End: 9, Text: "今天天气不错，大家辛苦了。"},
		{Start: 10, End: 12, Text: "需要把预算表发给我吗？"},
	}
	items := extractActionItems(segs)
	if len(items) != 2 {
		t.Fatalf("期望 2 条待办，实际 %d：%+v", len(items), items)
	}
	if items[0].Due != "下周" {
		t.Fatalf("时间识别错误：%q", items[0].Due)
	}
	if items[1].Owner != "小张" {
		t.Fatalf("负责人识别错误：%q", items[1].Owner)
	}
	if items[1].Due != "明天" {
		t.Fatalf("时间识别错误：%q", items[1].Due)
	}
	// 疑问句不该被当成待办
	for _, it := range items {
		if strings.Contains(it.Text, "？") || strings.Contains(it.Text, "?") {
			t.Fatalf("疑问句被误判为待办：%q", it.Text)
		}
	}
}

func TestGuessDueFormats(t *testing.T) {
	cases := map[string]string{
		"10月25日之前提交":  "10月25日",
		"下周三前给到":      "周三",
		"月底前完成验收":     "月底",
		"这件事不急":       "",
	}
	for in, want := range cases {
		if got := guessDue(in); got != want {
			t.Errorf("guessDue(%q) = %q，期望 %q", in, got, want)
		}
	}
}

func TestTopKeywords(t *testing.T) {
	text := "注意力机制的复杂度是O(n)的平方。注意力机制可以用transformer实现。" +
		"我们讨论注意力机制的优化，注意力机制很重要。"
	kws := topKeywords(text, 2)
	if len(kws) == 0 {
		t.Fatal("没有抽到关键词")
	}
	joined := strings.Join(kws, "|")
	if !strings.Contains(joined, "注意力") {
		t.Fatalf("关键词里应包含「注意力」，实际：%v", kws)
	}
}

func TestTopicBlocksSplitByGap(t *testing.T) {
	segs := []Segment{
		{Start: 0, End: 5, Text: "先说一下这周的进度安排"},
		{Start: 6, End: 10, Text: "前端已经做完了主要页面"},
		{Start: 200, End: 205, Text: "接下来说说明年的预算问题"},
		{Start: 206, End: 210, Text: "预算大概需要增加两成左右"},
	}
	blocks := topicBlocks(segs)
	if len(blocks) != 2 {
		t.Fatalf("长停顿应切成 2 块，实际 %d：%+v", len(blocks), blocks)
	}
}

func TestLocalSummarySections(t *testing.T) {
	app := &App{recordings: map[string]*Recording{}}
	rec := &Recording{
		Title: "周会",
		Segments: []Segment{
			{Start: 0, End: 4, Text: "这次会议主要讨论下季度的预算安排。"},
			{Start: 5, End: 9, Text: "我们决定把市场预算增加两成。"},
			{Start: 10, End: 14, Text: "小李负责跟进供应商报价，下周给结果。"},
			{Start: 15, End: 19, Text: "风险是时间太紧，可能来不及验收。"},
		},
	}
	out, err := app.localSummary(rec)
	if err != nil {
		t.Fatal(err)
	}
	for _, sec := range []string{"## 一句话总结", "## 议题", "## 结论", "## 待办", "## 风险与分歧"} {
		if !strings.Contains(out, sec) {
			t.Fatalf("缺少章节 %s：\n%s", sec, out)
		}
	}
	if !strings.Contains(out, "小李") {
		t.Fatalf("待办里应保留负责人：\n%s", out)
	}
	if !strings.Contains(out, "本地规则抽取") {
		t.Fatal("应标注是本地抽取")
	}
}

func TestExtractActionItemsEnglish(t *testing.T) {
	segs := []Segment{
		{Start: 1, End: 4, Text: "We need to send the report by tomorrow."},
		{Start: 5, End: 8, Text: "The weather is nice today."},
	}
	items := extractActionItems(segs)
	if len(items) != 1 {
		t.Fatalf("期望 1 条待办，实际 %d：%+v", len(items), items)
	}
	if items[0].Due != "明天" {
		t.Fatalf("英文时间识别错误：%q", items[0].Due)
	}
}

func TestCutSegments(t *testing.T) {
	segs := []Segment{
		{Start: 0, End: 2, Text: "前"},
		{Start: 5, End: 8, Text: "中"},
		{Start: 9, End: 12, Text: "后"},
	}
	out := cutSegments(segs, 4, 10)
	if len(out) != 2 {
		t.Fatalf("期望 2 段，实际 %d：%+v", len(out), out)
	}
	if out[0].Start != 1 || out[0].End != 4 {
		t.Fatalf("时间轴没有平移到 0 点：%+v", out[0])
	}
	if out[1].End != 6 {
		t.Fatalf("尾段应被裁到 6 秒：%+v", out[1])
	}
}

func TestNegatedActionNotTodo(t *testing.T) {
	items := extractActionItems([]Segment{{Start: 0, End: 2, Text: "不需要参考音频"}})
	if len(items) != 0 {
		t.Fatalf("否定句不该算待办：%+v", items)
	}
}
