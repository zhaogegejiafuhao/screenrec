package main

// 本地纪要：不接大模型也能出一份结构化的"能看"的纪要。
// 做法是规则抽取（分块 + 关键词 + 结论/待办/风险句式），质量不如 LLM，
// 但胜在零配置、离线、瞬时完成，且能把待办抽成结构化数据。

import (
	"fmt"
	"sort"
	"strings"
)

// ActionItem 一条待办
type ActionItem struct {
	Text  string  `json:"text"`
	Owner string  `json:"owner,omitempty"`
	Due   string  `json:"due,omitempty"`
	At    float64 `json:"at"` // 出现在原片第几秒
}

var stopWords = []string{
	"我们", "你们", "他们", "这个", "那个", "就是", "然后", "可以", "什么", "没有",
	"一个", "现在", "因为", "所以", "但是", "如果", "这样", "时候", "还是", "应该",
	"知道", "觉得", "其实", "不是", "的话", "自己", "大家", "这里", "那里", "还有",
	"可能", "需要", "一下", "两个", "这些", "那些", "已经", "开始", "问题", "东西",
	"对吧", "是的", "嗯嗯", "好的", "那么", "怎么", "为什么", "是不是", "有没有",
}

var enStop = map[string]bool{
	"the": true, "and": true, "that": true, "this": true, "with": true, "for": true,
	"you": true, "are": true, "was": true, "have": true, "but": true, "not": true,
	"they": true, "what": true, "your": true, "will": true, "from": true, "can": true,
	"just": true, "like": true, "about": true, "there": true, "would": true,
}

// localSummary 规则抽取式纪要
func (a *App) localSummary(rec *Recording) (string, error) {
	a.mu.RLock()
	segs := append([]Segment{}, rec.Segments...)
	title := rec.Title
	a.mu.RUnlock()
	if len(segs) == 0 {
		return "", fmt.Errorf("这条记录还没有转写内容")
	}

	blocks := topicBlocks(segs)
	var b strings.Builder
	fmt.Fprintf(&b, "## 一句话总结\n%s\n\n", oneLine(segs))

	b.WriteString("## 议题\n")
	shown := 0
	for _, blk := range blocks {
		if shown >= 8 {
			break
		}
		first := firstSentence(blk.text, 46)
		if first == "" {
			continue
		}
		kws := topKeywords(blk.text, 2)
		if len(kws) > 0 {
			fmt.Fprintf(&b, "- [%s] %s（关键词：%s）\n", clock(blk.start), first, strings.Join(kws, "、"))
		} else {
			fmt.Fprintf(&b, "- [%s] %s\n", clock(blk.start), first)
		}
		shown++
	}
	if shown == 0 {
		b.WriteString("- 原文太短，没有可归纳的议题\n")
	}
	b.WriteString("\n")

	concl := matchSentences(segs, []string{"决定", "确定", "定了", "结论", "达成", "同意", "就这么", "统一", "敲定"})
	b.WriteString("## 结论\n")
	if len(concl) == 0 {
		b.WriteString("- 原文未提及明确结论\n")
	} else {
		for i, c := range concl {
			if i >= 6 {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s\n", clock(c.at), c.text)
		}
	}
	b.WriteString("\n")

	items := extractActionItems(segs)
	b.WriteString("## 待办\n")
	if len(items) == 0 {
		b.WriteString("- 原文未提及明确待办\n")
	} else {
		for i, it := range items {
			if i >= 10 {
				break
			}
			owner, due := it.Owner, it.Due
			if owner == "" {
				owner = "未指定"
			}
			if due == "" {
				due = "未指定"
			}
			fmt.Fprintf(&b, "- [ ] %s — 负责人：%s / 时间：%s（%s）\n", it.Text, owner, due, clock(it.At))
		}
	}
	b.WriteString("\n")

	risks := matchSentences(segs, []string{"风险", "担心", "延期", "来不及", "不够", "冲突", "分歧", "问题", "卡住", "阻塞"})
	b.WriteString("## 风险与分歧\n")
	if len(risks) == 0 {
		b.WriteString("- 无明显分歧\n")
	} else {
		for i, c := range risks {
			if i >= 6 {
				break
			}
			fmt.Fprintf(&b, "- [%s] %s\n", clock(c.at), c.text)
		}
	}

	fmt.Fprintf(&b, "\n> 本地规则抽取（未调用大模型），标题：%s。配好 API Key 后点「重新生成纪要」可获得更好的结果。\n", title)
	return b.String(), nil
}

type block struct {
	start float64
	text  string
}

// topicBlocks 按停顿（>90 秒）和说话人变化切议题块
func topicBlocks(segs []Segment) []block {
	var out []block
	var cur strings.Builder
	start := 0.0
	lastEnd := -1.0
	lastSpk := 0
	flush := func() {
		t := strings.TrimSpace(cur.String())
		if len([]rune(t)) >= 8 {
			out = append(out, block{start: start, text: t})
		}
		cur.Reset()
	}
	for i, s := range segs {
		text := strings.TrimSpace(s.Text)
		if text == "" {
			continue
		}
		gap := s.Start - lastEnd
		spkChanged := s.Speaker != 0 && lastSpk != 0 && s.Speaker != lastSpk
		if i > 0 && (gap > 90 || spkChanged) {
			flush()
			start = s.Start
		}
		if cur.Len() == 0 {
			start = s.Start
		}
		if cur.Len() > 0 {
			cur.WriteString("。")
		}
		cur.WriteString(text)
		lastEnd = s.End
		if s.Speaker != 0 {
			lastSpk = s.Speaker
		}
	}
	flush()
	return out
}

// oneLine 取信息量最大的一句做总结
func oneLine(segs []Segment) string {
	best, bestScore := "", -1.0
	keys := topKeywords(allText(segs), 6)
	for _, s := range segs {
		text := strings.TrimSpace(s.Text)
		n := len([]rune(text))
		if n < 10 || n > 90 {
			continue
		}
		score := float64(n) * 0.05
		for _, k := range keys {
			if strings.Contains(text, k) {
				score += 1.5
			}
		}
		for _, m := range []string{"决定", "结论", "总结", "主要是", "目标是", "讨论"} {
			if strings.Contains(text, m) {
				score += 2
			}
		}
		if score > bestScore {
			best, bestScore = text, score
		}
	}
	if best == "" {
		best = firstSentence(allText(segs), 60)
	}
	return strings.TrimSpace(best)
}

func allText(segs []Segment) string {
	var b strings.Builder
	for _, s := range segs {
		b.WriteString(s.Text)
		b.WriteString("。")
	}
	return b.String()
}

func firstSentence(s string, max int) string {
	for _, part := range splitSentences(s) {
		part = trimPunct(part)
		if part == "" {
			continue
		}
		r := []rune(part)
		if len(r) > max {
			return string(r[:max]) + "…"
		}
		return part
	}
	return ""
}

// splitSentences 切句并保留句末标点（判断疑问句要用到）
func splitSentences(s string) []string {
	var out []string
	var cur []rune
	for _, r := range s {
		cur = append(cur, r)
		switch r {
		case '。', '！', '？', '!', '?', '；', ';', '\n', '\r':
			if t := strings.TrimSpace(string(cur)); t != "" {
				out = append(out, t)
			}
			cur = cur[:0]
		}
	}
	if t := strings.TrimSpace(string(cur)); t != "" {
		out = append(out, t)
	}
	return out
}

// trimPunct 去掉句末标点，用于展示
func trimPunct(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "。！？!?；;，,、 \t")
}

// isQuestion 疑问句不算待办
func isQuestion(s string) bool {
	s = strings.TrimSpace(s)
	for _, tail := range []string{"？", "?", "吗？", "呢？", "吗", "呢"} {
		if strings.HasSuffix(s, tail) {
			return true
		}
	}
	return false
}

type hit struct {
	text string
	at   float64
}

// matchSentences 找出包含任一关键词的句子
func matchSentences(segs []Segment, words []string) []hit {
	var out []hit
	for _, s := range segs {
		for _, sent := range splitSentences(s.Text) {
			if isQuestion(sent) {
				continue
			}
			sent = trimPunct(sent)
			if len([]rune(sent)) < 6 {
				continue
			}
			for _, w := range words {
				if strings.Contains(sent, w) {
					if len([]rune(sent)) > 70 {
						sent = string([]rune(sent)[:70]) + "…"
					}
					out = append(out, hit{text: sent, at: s.Start})
					break
				}
			}
		}
	}
	return out
}

// extractActionItems 用句式规则抽待办，并尽量识别负责人和时间
func extractActionItems(segs []Segment) []ActionItem {
	markers := []string{"要", "需要", "得", "负责", "跟进", "确认", "安排", "记得", "别忘",
		"明天", "后天", "下周", "本周", "月底", "之前", "尽快", "马上", "回头",
		"need to", "should", "must", "follow up", "deadline", "action item", "assign", "by tomorrow"}
	var out []ActionItem
	seen := map[string]bool{}
	for _, s := range segs {
		for _, sent := range splitSentences(s.Text) {
			if isQuestion(sent) {
				continue
			}
			sent = trimPunct(sent)
			n := len([]rune(sent))
			if n < 6 || n > 90 {
				continue
			}
			low := strings.ToLower(sent)
			// 「不需要 / 不用 / 没必要」不是待办
			neg := false
			for _, n := range []string{"不需要", "不用", "没必要", "无需", "不必", "don't need", "no need"} {
				if strings.Contains(low, n) {
					neg = true
					break
				}
			}
			if neg {
				continue
			}
			matched := false
			for _, m := range markers {
				if strings.Contains(low, m) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
			key := string([]rune(sent)[:minInt(n, 18)])
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, ActionItem{
				Text:  sent,
				Owner: guessOwner(sent),
				Due:   guessDue(sent),
				At:    s.Start,
			})
		}
	}
	return out
}

func guessOwner(s string) string {
	for _, suffix := range []string{"负责", "来跟", "跟进", "来处理", "去确认", "来确认", "来做"} {
		if i := strings.Index(s, suffix); i > 0 {
			r := []rune(s[:i])
			if len(r) >= 2 && len(r) <= 5 {
				return string(r)
			}
		}
	}
	return ""
}

func guessDue(s string) string {
	low := strings.ToLower(s)
	for _, en := range []struct{ k, v string }{
		{"tomorrow", "明天"}, {"today", "今天"}, {"next week", "下周"},
		{"next month", "下个月"}, {"this week", "本周"}, {"end of month", "月底"},
		{"friday", "周五"}, {"monday", "周一"}, {"tuesday", "周二"},
		{"wednesday", "周三"}, {"thursday", "周四"},
	} {
		if strings.Contains(low, en.k) {
			return en.v
		}
	}
	cands := []string{"今天", "明天", "后天", "周一", "周二", "周三", "周四", "周五", "周六", "周日", "本周", "下周", "月底", "下个月"}
	for _, c := range cands {
		if strings.Contains(s, c) {
			return c
		}
	}
	// 10月25日 / 25号 / 3点
	if i := strings.Index(s, "月"); i > 0 {
		r := []rune(s[:i])
		start := i
		for j := len(r) - 1; j >= 0 && j >= len(r)-2; j-- {
			if r[j] >= '0' && r[j] <= '9' {
				start = j
			}
		}
		if start < i {
			end := i + 1
			rr := []rune(s)
			for end < len(rr) && rr[end] >= '0' && rr[end] <= '9' {
				end++
			}
			if end < len(rr) && (rr[end] == '日' || rr[end] == '号') {
				return string(rr[start : end+1])
			}
		}
	}
	return ""
}

// topKeywords 频次 + 长度加权的 n-gram 关键词；英文文本改走整词统计
func topKeywords(text string, n int) []string {
	if asciiRatio(text) > 0.6 {
		return topEnglishWords(text, n)
	}
	runes := []rune(text)
	cnt := map[string]int{}
	for size := 2; size <= 4; size++ {
		for i := 0; i+size <= len(runes); i++ {
			gram := string(runes[i : i+size])
			if !isKeyword(gram) {
				continue
			}
			cnt[gram]++
		}
	}
	type kv struct {
		w string
		s float64
	}
	var list []kv
	for w, c := range cnt {
		if c < 2 {
			continue
		}
		list = append(list, kv{w, float64(c) * (1 + 0.6*float64(len([]rune(w))-2))})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].s == list[j].s {
			return len(list[i].w) > len(list[j].w)
		}
		return list[i].s > list[j].s
	})
	var out []string
	for _, it := range list {
		dup := false
		for _, prev := range out {
			if strings.Contains(prev, it.w) || strings.Contains(it.w, prev) {
				dup = true
				break
			}
		}
		if dup {
			continue
		}
		out = append(out, it.w)
		if len(out) >= n {
			break
		}
	}
	return out
}

func isKeyword(s string) bool {
	for _, w := range stopWords {
		if s == w {
			return false
		}
	}
	// 含虚词的 n-gram 基本都是跨词边界的碎片（"擎的测试"、"要参考音"）
	for _, bad := range "的了是在和与就都也很我你他她它要不没这个那个" {
		if strings.ContainsRune(s, bad) {
			return false
		}
	}
	for _, r := range s {
		if r < 0x4E00 || r > 0x9FFF { // 只收中文，避免把英文碎片当关键词
			return false
		}
	}
	return true
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func asciiRatio(s string) float64 {
	var letters, ascii int
	for _, r := range s {
		if r < 128 {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
				letters++
				ascii++
			}
			continue
		}
		letters++
	}
	if letters == 0 {
		return 0
	}
	return float64(ascii) / float64(letters)
}

// topEnglishWords 英文按整词统计，避免把字母碎片当关键词
func topEnglishWords(text string, n int) []string {
	fields := strings.FieldsFunc(strings.ToLower(text), func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-'
	})
	cnt := map[string]int{}
	for _, w := range fields {
		if len(w) < 4 || enStop[w] {
			continue
		}
		cnt[w]++
	}
	type kv struct {
		w string
		s float64
	}
	var list []kv
	for w, c := range cnt {
		if c < 2 {
			continue
		}
		list = append(list, kv{w, float64(c) * (1 + 0.05*float64(len(w)))})
	}
	sort.Slice(list, func(i, j int) bool { return list[i].s > list[j].s })
	var out []string
	for _, it := range list {
		out = append(out, it.w)
		if len(out) >= n {
			break
		}
	}
	return out
}
