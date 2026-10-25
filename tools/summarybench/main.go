// Command summarybench 给 AI 纪要打分：同一批带标准答案的会议转写，
// 分别交给「规则抽取 / 本机模型 / 云端 API」三个引擎，算待办抽取的准确率
// 和摘要覆盖度，输出一张对比表。
//
// 用法（先双击 screenrec.exe 启动服务）：
//
//	go run ./tools/summarybench -data D:\xxx\data -gold testdata\summary\gold.json
//	go run ./tools/summarybench -engines rules,local          # 只测这两个
//	go run ./tools/summarybench -out bench.md -json bench.json
//
// 打分的几个口径（都写在 score 里，改口径就改那里）：
//   - 待办：预测条目和标准答案做字符二元组 Jaccard，≥0.45 算命中，一个标准答案只认一条
//   - 负责人 / 时间：只在命中的条目上比，标准答案为空的跳过
//   - 议题 / 决定召回：标准答案里的关键词有没有出现在生成的纪要里
//   - ROUGE-L：按字符的最长公共子序列 F1，衡量摘要和参考摘要的重合度
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
)

type goldCase struct {
	ID        string        `json:"id"`
	Title     string        `json:"title"`
	Reference string        `json:"reference"`
	Topics    []string      `json:"topics"`
	Decisions []string      `json:"decisions"`
	Actions   []goldAction  `json:"actions"`
	Segments  []goldSegment `json:"segments"`
}

type goldAction struct {
	Owner string `json:"owner"`
	Task  string `json:"task"`
	Due   string `json:"due"`
}

type goldSegment struct {
	Start float64 `json:"start"`
	End   float64 `json:"end"`
	Text  string  `json:"text"`
}

type goldSet struct {
	Cases []goldCase `json:"cases"`
}

type benchAction struct {
	Text  string `json:"text"`
	Owner string `json:"owner"`
	Due   string `json:"due"`
	At    string `json:"at"`
}

type engineResult struct {
	Engine string  `json:"engine"`
	OK     bool    `json:"ok"`
	Err    string  `json:"error,omitempty"`
	MS     int64   `json:"ms"`
	Prec   float64 `json:"precision"`
	Rec    float64 `json:"recall"`
	F1     float64 `json:"f1"`
	Owner  float64 `json:"ownerAcc"`
	Due    float64 `json:"dueAcc"`
	Topic  float64 `json:"topicRecall"`
	Deci   float64 `json:"decisionRecall"`
	RougeL float64 `json:"rougeL"`
	PredN  int     `json:"predActions"`
	GoldN  int     `json:"goldActions"`
	Sample string  `json:"sample,omitempty"`
}

func main() {
	api := flag.String("api", "http://127.0.0.1:8790", "录屏工坊地址")
	data := flag.String("data", "data", "数据目录（会往 recordings 里写测试用例）")
	gold := flag.String("gold", "tools/summarybench/testdata/gold.json", "标准答案集")
	engines := flag.String("engines", "rules,local,cloud", "要测的引擎，逗号分隔")
	out := flag.String("out", "", "Markdown 报告输出路径（默认打到控制台）")
	js := flag.String("json", "", "原始结果 JSON 输出路径")
	thr := flag.Float64("thr", 0.5, "待办命中的相似度阈值")
	repeat := flag.Int("repeat", 1, "每条用例每个引擎跑几次取平均（小模型输出有波动，建议 3）")
	flag.Parse()

	set, err := loadGold(*gold)
	if err != nil {
		fmt.Println("读取标准答案失败:", err)
		os.Exit(1)
	}
	if err := ping(*api); err != nil {
		fmt.Println("连不上录屏工坊:", err)
		os.Exit(1)
	}

	absData, _ := filepath.Abs(*data)
	recDir := filepath.Join(absData, "recordings")
	if err := os.MkdirAll(recDir, 0755); err != nil {
		fmt.Println("打不开数据目录:", err)
		os.Exit(1)
	}
	// 清掉上一轮自己建的用例，避免越攒越多
	if olds, err := filepath.Glob(filepath.Join(recDir, "rbench-*.json")); err == nil {
		for _, p := range olds {
			os.Remove(p)
		}
	}
	for _, c := range set.Cases {
		if err := writeCase(recDir, c); err != nil {
			fmt.Println("写测试用例失败:", err)
			os.Exit(1)
		}
	}

	// 评测会临时改用户的「AI 纪要引擎」设置，跑完必须还原——
	// 否则用户下次用会发现引擎被停在 off/rules 上，甚至以为功能坏了
	origEngine := getEngine(*api)
	defer func() {
		if origEngine != "" {
			_ = setEngine(*api, origEngine)
		}
	}()

	var rows [][]engineResult // 每个 case 一行，列是引擎
	for _, c := range set.Cases {
		var row []engineResult
		for _, eng := range strings.Split(*engines, ",") {
			eng = strings.TrimSpace(eng)
			if eng == "" {
				continue
			}
			row = append(row, runCaseAvg(*api, eng, c, *thr, *repeat))
		}
		rows = append(rows, row)
	}

	report := render(set, rows, strings.Split(*engines, ","), *repeat)
	if *out != "" {
		if err := os.WriteFile(*out, []byte(report), 0644); err == nil {
			fmt.Println("已写出报告:", *out)
		}
	}
	if *js != "" {
		flat := []engineResult{}
		for _, r := range rows {
			flat = append(flat, r...)
		}
		if b, err := json.MarshalIndent(flat, "", "  "); err == nil {
			os.WriteFile(*js, b, 0644)
		}
	}
	fmt.Println(report)
}

func loadGold(p string) (*goldSet, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	var s goldSet
	if err := json.Unmarshal(b, &s); err != nil {
		return nil, err
	}
	if len(s.Cases) == 0 {
		return nil, fmt.Errorf("标准答案集里没有用例")
	}
	return &s, nil
}

func ping(api string) error {
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Get(api + "/api/state")
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// writeCase 把标准答案里的转写写成一条记录文件，App 读不到内存时会自动从磁盘补
func writeCase(dir string, c goldCase) error {
	segs := make([]map[string]any, 0, len(c.Segments))
	for _, s := range c.Segments {
		segs = append(segs, map[string]any{"start": s.Start, "end": s.End, "text": s.Text})
	}
	rec := map[string]any{
		"id":          "rbench-" + c.ID,
		"title":       c.Title,
		"mode":        "audio",
		"file":        "",
		"status":      "done",
		"startedAt":   time.Now().Format(time.RFC3339),
		"durationMs":  0,
		"segments":    segs,
		"transcribed": true,
	}
	b, _ := json.MarshalIndent(rec, "", "  ")
	return os.WriteFile(filepath.Join(dir, "rbench-"+c.ID+".json"), b, 0644)
}

// runCaseAvg 跑 repeat 次取平均。
// 为什么要重复：4B 小模型即使 temperature=0.3，同一份输入两次生成的措辞也可能差很多，
// 单次采样的 F1 波动能到 ±0.2，只看一次容易得出反的结论。
func runCaseAvg(api, engine string, c goldCase, thr float64, repeat int) engineResult {
	if repeat < 1 {
		repeat = 1
	}
	first := runCase(api, engine, c, thr)
	if repeat == 1 || !first.OK {
		return first
	}
	acc := first
	n := 1
	for i := 1; i < repeat; i++ {
		r := runCase(api, engine, c, thr)
		if !r.OK {
			continue
		}
		acc.MS += r.MS
		acc.Prec += r.Prec
		acc.Rec += r.Rec
		acc.F1 += r.F1
		acc.Owner += r.Owner
		acc.Due += r.Due
		acc.Topic += r.Topic
		acc.Deci += r.Deci
		acc.RougeL += r.RougeL
		acc.PredN += r.PredN
		n++
	}
	k := float64(n)
	acc.MS = acc.MS / int64(n)
	acc.Prec /= k
	acc.Rec /= k
	acc.F1 /= k
	acc.Owner /= k
	acc.Due /= k
	acc.Topic /= k
	acc.Deci /= k
	acc.RougeL /= k
	acc.PredN = acc.PredN / n
	acc.Sample = first.Sample
	return acc
}

func runCase(api, engine string, c goldCase, thr float64) engineResult {
	res := engineResult{Engine: engine, GoldN: len(c.Actions)}
	// 评测里的 rules 对应后端的 off（不跑模型，纯规则抽取）
	real := engine
	if engine == "rules" {
		real = "off"
	}
	if err := setEngine(api, real); err != nil {
		res.Err = err.Error()
		return res
	}
	id := "rbench-" + c.ID
	start := time.Now()
	body, err := post(api, "/api/recordings/"+id+"/summary", nil)
	if err != nil {
		res.Err = err.Error()
		return res
	}
	res.MS = time.Since(start).Milliseconds()

	var rec struct {
		Summary      string `json:"summary"`
		SummaryModel string `json:"summaryModel"`
	}
	json.Unmarshal(body, &rec)

	var preds []benchAction
	if engine == "rules" {
		// 规则抽取有独立的待办接口，直接用它
		ab, err := get(api, "/api/recordings/"+id+"/actions")
		if err == nil {
			var out struct {
				Items []benchAction `json:"items"`
			}
			json.Unmarshal(ab, &out)
			preds = out.Items
		}
	}
	if len(preds) == 0 {
		preds = parseActions(rec.Summary)
	}

	p, r, f1, ownerAcc, dueAcc := scoreActions(c.Actions, preds, thr)
	res.Prec, res.Rec, res.F1, res.Owner, res.Due = p, r, f1, ownerAcc, dueAcc
	res.Topic = keywordRecall(c.Topics, rec.Summary)
	res.Deci = keywordRecall(c.Decisions, rec.Summary)
	res.RougeL = rougeL(c.Reference, rec.Summary)
	res.PredN = len(preds)
	res.OK = true
	if len([]rune(rec.Summary)) > 240 {
		res.Sample = string([]rune(rec.Summary)[:240]) + "…"
	} else {
		res.Sample = rec.Summary
	}
	return res
}

// getEngine 读当前引擎设置（读不到返回空串）
func getEngine(api string) string {
	resp, err := http.Get(strings.TrimRight(api, "/") + "/api/settings")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	var m map[string]any
	if json.NewDecoder(resp.Body).Decode(&m) != nil {
		return ""
	}
	if v, ok := m["llmEngine"].(string); ok {
		return v
	}
	return ""
}

func setEngine(api, engine string) error {
	_, err := put(api, "/api/settings", map[string]any{"llmEngine": engine})
	return err
}

/* ---------- 打分 ---------- */

func scoreActions(golds []goldAction, preds []benchAction, thr float64) (p, r, f1, ownerAcc, dueAcc float64) {
	if len(golds) == 0 {
		return 0, 0, 0, 0, 0
	}
	used := make([]bool, len(preds))
	hit := 0
	ownerTot, ownerOK, dueTot, dueOK := 0, 0, 0, 0
	for _, g := range golds {
		best, bestIdx := 0.0, -1
		for i, pd := range preds {
			if used[i] {
				continue
			}
			s := sim(g.Task, pd.Text)
			if s > best {
				best, bestIdx = s, i
			}
		}
		if bestIdx >= 0 && best >= thr {
			used[bestIdx] = true
			hit++
			if g.Owner != "" {
				ownerTot++
				if strings.Contains(preds[bestIdx].Owner, g.Owner) || strings.Contains(norm(g.Owner), norm(preds[bestIdx].Owner)) {
					ownerOK++
				}
			}
			if g.Due != "" {
				dueTot++
				if strings.Contains(norm(preds[bestIdx].Due), norm(g.Due)) {
					dueOK++
				}
			}
		}
	}
	p = float64(hit) / float64(len(preds))
	r = float64(hit) / float64(len(golds))
	if p+r > 0 {
		f1 = 2 * p * r / (p + r)
	}
	if len(preds) == 0 {
		p = 0
	}
	if ownerTot > 0 {
		ownerAcc = float64(ownerOK) / float64(ownerTot)
	}
	if dueTot > 0 {
		dueAcc = float64(dueOK) / float64(dueTot)
	}
	return
}

func keywordRecall(keys []string, text string) float64 {
	if len(keys) == 0 {
		return 0
	}
	n := norm(text)
	ok := 0
	for _, k := range keys {
		if strings.Contains(n, norm(k)) {
			ok++
		}
	}
	return float64(ok) / float64(len(keys))
}

// norm 归一化：小写、去空白与标点，中文不动
func norm(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsSpace(r) {
			continue
		}
		if unicode.IsPunct(r) {
			continue
		}
		b.WriteRune(unicode.ToLower(r))
	}
	return b.String()
}

// sim 字符二元组「包含度」：交集 / 较短一方的长度。
//
// 为什么不用 Jaccard：规则抽取给的是一整句原话（二十几个字），模型给的是凝练后的
// 任务（几个字），Jaccard 会被长句的分母压到接近 0，等于变相惩罚规则抽取。
// 用包含度衡量「短的那句是不是基本被长的那句说到了」，两边才公平。
func sim(a, b string) float64 {
	ga, gb := bigrams(norm(a)), bigrams(norm(b))
	if len(ga) == 0 || len(gb) == 0 {
		if norm(a) == norm(b) {
			return 1
		}
		return 0
	}
	inter := 0
	for k := range ga {
		if gb[k] {
			inter++
		}
	}
	short := len(ga)
	if len(gb) < short {
		short = len(gb)
	}
	return float64(inter) / float64(short)
}

func bigrams(s string) map[string]bool {
	m := map[string]bool{}
	rs := []rune(s)
	for i := 0; i+1 < len(rs); i++ {
		m[string(rs[i:i+2])] = true
	}
	return m
}

// rougeL 字符级最长公共子序列的 F1
func rougeL(ref, cand string) float64 {
	a, b := []rune(norm(ref)), []rune(norm(cand))
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	dp := make([][]int, len(a)+1)
	for i := range dp {
		dp[i] = make([]int, len(b)+1)
	}
	for i := 1; i <= len(a); i++ {
		for j := 1; j <= len(b); j++ {
			if a[i-1] == b[j-1] {
				dp[i][j] = dp[i-1][j-1] + 1
			} else if dp[i-1][j] > dp[i][j-1] {
				dp[i][j] = dp[i-1][j]
			} else {
				dp[i][j] = dp[i][j-1]
			}
		}
	}
	lcs := float64(dp[len(a)][len(b)])
	pr := lcs / float64(len(b))
	re := lcs / float64(len(a))
	if pr+re == 0 {
		return 0
	}
	return 2 * pr * re / (pr + re)
}

// parseActions 从纪要 Markdown 里把待办条目抠出来。
// 模型常见写法是「- [ ] 任务 — 负责人 / 时间」，任务和元信息要拆开，
// 否则负责人、时间会被算进任务文本里，相似度被拉低。
func parseActions(md string) []benchAction {
	var out []benchAction
	inTodo := false
	for _, ln := range strings.Split(md, "\n") {
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "#") {
			inTodo = strings.Contains(trimmed, "待办") || strings.Contains(trimmed, "行动") ||
				strings.Contains(trimmed, "Action") || strings.Contains(trimmed, "TODO")
			continue
		}
		if !strings.HasPrefix(trimmed, "-") && !strings.HasPrefix(trimmed, "*") &&
			!strings.HasPrefix(trimmed, "•") && !listNum(trimmed) {
			continue
		}
		if !inTodo {
			continue
		}
		text := strings.TrimSpace(strings.TrimLeft(trimmed, "-*• "))
		text = strings.TrimLeft(text, "0123456789. ")
		text = strings.TrimSpace(strings.TrimPrefix(text, "[ ]"))
		text = strings.TrimSpace(strings.TrimPrefix(text, "[x]"))
		if text == "" {
			continue
		}
		owner, due := "", ""
		for _, sep := range []string{"—", "–", " | ", "｜"} {
			if i := strings.Index(text, sep); i > 0 {
				meta := strings.TrimSpace(text[i+len(sep):])
				text = strings.TrimSpace(text[:i])
				for _, s2 := range []string{"/", "，", ","} {
					if j := strings.Index(meta, s2); j > 0 {
						owner = strings.TrimSpace(meta[:j])
						due = strings.TrimSpace(meta[j+len(s2):])
						break
					}
				}
				if owner == "" {
					owner = meta
				}
				break
			}
		}
		for _, w := range []string{"未指定", "无", "-", "待定"} {
			if owner == w {
				owner = ""
			}
			if due == w {
				due = ""
			}
		}
		// 没有显式分隔符时，试着从右往左把「负责人/时间」尾巴一段段摘出来。
		// 小模型常写成「补全埋点，小李负责，周四前」而不是 prompt 要求的「— 负责人 / 时间」，
		// 不摘掉的话这段尾巴会把文本相似度压到阈值以下，明明是正确答案却判不命中。
		if owner == "" && due == "" {
			for round := 0; round < 4; round++ {
				i := strings.LastIndex(text, "，")
				if i <= 0 {
					break
				}
				tail := strings.TrimSpace(text[i+len("，"):])
				head := strings.TrimSpace(text[:i])
				if head == "" || !looksLikeMeta(tail) || len([]rune(tail)) > 14 {
					break
				}
				text = head
				o, d := guessOwner(tail), guessDue(tail)
				if o != "" && owner == "" {
					owner = o
				}
				if d != "" && due == "" {
					due = d
				}
				if o == "" && d == "" && owner == "" {
					owner = tail
				}
			}
		}
		for _, w := range []string{"未指定", "无", "-", "待定"} {
			if owner == w {
				owner = ""
			}
			if due == w {
				due = ""
			}
		}
		if owner == "" {
			owner = guessOwner(text)
		}
		if due == "" {
			due = guessDue(text)
		}
		out = append(out, benchAction{Text: text, Owner: owner, Due: due})
	}
	return out
}

// looksLikeMeta 判断一段尾巴像不像「负责人/时间」元数据而不是任务内容。
// 判据：出现负责人用词或时间词。宁可错判成任务（漏摘），也别把「补齐了才有数据看」
// 这种真·任务尾巴切掉。
func looksLikeMeta(s string) bool {
	for _, w := range []string{"负责", "跟进", "处理", "来做", "来过", "确认"} {
		if strings.Contains(s, w) {
			return true
		}
	}
	for _, w := range []string{"本周", "下周", "月底", "年底", "明天", "后天", "今天",
		"上午", "下午", "周一", "周二", "周三", "周四", "周五", "周六", "周日",
		"之前", "以前", "截止", "deadline", "by "} {
		if strings.Contains(s, w) {
			return true
		}
	}
	// 纯日期/时刻：10月25日、3 号、Q3
	for _, r := range s {
		if r >= '0' && r <= '9' {
			return true
		}
	}
	return false
}

func listNum(s string) bool {
	if len(s) < 2 {
		return false
	}
	if s[0] < '0' || s[0] > '9' {
		return false
	}
	tail := s[1:]
	for _, p := range []string{".", "、", ")", "．"} {
		if strings.HasPrefix(tail, p) {
			return true
		}
	}
	return false
}

// guessOwner / guessDue 只在评测里用：从一句话里抠负责人和时间
func guessOwner(s string) string {
	for _, kw := range []string{"负责", "跟进", "准备", "补", "更新", "出一版", "出"} {
		if i := strings.Index(s, kw); i >= 0 {
			head := s[:i]
			for _, w := range []string{"，", "。", "：", ":", " ", "(", "（", "、"} {
				if j := strings.LastIndex(head, w); j >= 0 {
					head = head[j+1:]
				}
			}
			if len([]rune(head)) <= 6 && len(head) > 0 {
				return head
			}
		}
	}
	return ""
}

func guessDue(s string) string {
	for _, kw := range []string{"本周五", "本周三", "本周", "下周一", "下周二", "下周", "周四", "月底", "今天", "明天", "下个版本", "下个季度"} {
		if strings.Contains(s, kw) {
			return kw
		}
	}
	return ""
}

/* ---------- HTTP ---------- */

func get(api, path string) ([]byte, error) {
	resp, err := http.Get(api + path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func post(api, path string, payload any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		b, _ := json.Marshal(payload)
		body = strings.NewReader(string(b))
	} else {
		body = strings.NewReader("{}")
	}
	resp, err := http.Post(api+path, "application/json", body)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	return b, nil
}

func put(api, path string, payload any) ([]byte, error) {
	b, _ := json.Marshal(payload)
	req, err := http.NewRequest("PUT", api+path, strings.NewReader(string(b)))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

/* ---------- 报告 ---------- */

func render(set *goldSet, rows [][]engineResult, engines []string, repeat int) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# AI 纪要引擎对比（%s）\n\n", time.Now().Format("2006-01-02 15:04"))
	fmt.Fprintf(&b, "标准答案集：%s（%d 条会议，共 %d 条待办）\n\n", "tools/summarybench/testdata/gold.json", len(set.Cases), countActions(set))

	fmt.Fprintf(&b, "| 会议 | 引擎 | 待办 P | 待办 R | 待办 F1 | 负责人 | 时间 | 议题召回 | 决定召回 | ROUGE-L | 耗时 |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for i, row := range rows {
		title := ""
		if i < len(set.Cases) {
			title = set.Cases[i].Title
		}
		for _, r := range row {
			if !r.OK {
				fmt.Fprintf(&b, "| %s | %s | — | — | — | — | — | — | — | — | 失败：%s |\n", title, r.Engine, r.Err)
				continue
			}
			fmt.Fprintf(&b, "| %s | %s | %.2f | %.2f | %.2f | %.0f%% | %.0f%% | %.0f%% | %.0f%% | %.2f | %.1fs |\n",
				title, r.Engine, r.Prec, r.Rec, r.F1, r.Owner*100, r.Due*100, r.Topic*100, r.Deci*100, r.RougeL, float64(r.MS)/1000)
		}
	}

	// 汇总：每个引擎的平均
	fmt.Fprintf(&b, "\n## 汇总（各会议平均）\n\n")
	fmt.Fprintf(&b, "| 引擎 | 待办 F1 | 精确率 | 召回率 | 负责人 | 时间 | 议题召回 | ROUGE-L | 平均耗时 | 成功 |\n")
	fmt.Fprintf(&b, "| --- | --- | --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, eng := range engines {
		eng = strings.TrimSpace(eng)
		if eng == "" {
			continue
		}
		var f1, p, r, ow, du, tp, rl, ms float64
		n, ok := 0, 0
		for _, row := range rows {
			for _, x := range row {
				if x.Engine != eng {
					continue
				}
				n++
				if !x.OK {
					continue
				}
				ok++
				f1 += x.F1
				p += x.Prec
				r += x.Rec
				ow += x.Owner
				du += x.Due
				tp += x.Topic
				rl += x.RougeL
				ms += float64(x.MS)
			}
		}
		if n == 0 {
			continue
		}
		if ok == 0 {
			fmt.Fprintf(&b, "| %s | 全部失败 | | | | | | | | 0/%d |\n", eng, n)
			continue
		}
		k := float64(ok)
		fmt.Fprintf(&b, "| %s | %.2f | %.2f | %.2f | %.0f%% | %.0f%% | %.0f%% | %.2f | %.1fs | %d/%d |\n",
			eng, f1/k, p/k, r/k, ow/k*100, du/k*100, tp/k*100, rl/k, ms/k/1000, ok, n)
	}

	fmt.Fprintf(&b, "\n指标口径：待办命中=字符二元组包含度≥阈值（默认 0.5，交集/较短一方），一个标准答案只认一条预测；\n")
	fmt.Fprintf(&b, "解析时会先把「，小李负责，周四前」这类逗号元数据尾巴摘掉再比对——小模型常不按 prompt 要求的\n")
	fmt.Fprintf(&b, "「— 负责人 / 时间」格式输出，不摘的话同义改写会被误判成不命中；\n")
	fmt.Fprintf(&b, "精确率低说明模型在编待办（幻觉），召回率低说明漏待办；ROUGE-L 是和人工参考摘要的字符级 LCS F1。\n")
	return b.String()
}

func countActions(s *goldSet) int {
	n := 0
	for _, c := range s.Cases {
		n += len(c.Actions)
	}
	return n
}
