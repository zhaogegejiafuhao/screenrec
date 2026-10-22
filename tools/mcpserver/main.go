// 录屏工坊的 MCP server：把本地录制、文稿、纪要、待办、剪辑暴露给
// Claude Desktop / Cursor / Cline 这类支持 Model Context Protocol 的客户端。
//
// 传输是 stdio（一行一个 JSON-RPC 消息），数据全部走本机 127.0.0.1 的 HTTP API，
// 不联网、不上传。用法见 README「MCP server」一节。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

var (
	baseURL   = "http://127.0.0.1:8790"
	httpCli   = &http.Client{Timeout: 120 * time.Second}
	serverVer = "0.7.0"
)

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

func main() {
	urlFlag := flag.String("url", "", "录屏工坊地址（默认 http://127.0.0.1:8790）")
	flag.Parse()
	if *urlFlag != "" {
		baseURL = strings.TrimRight(*urlFlag, "/")
	} else if v := os.Getenv("SCREENREC_URL"); v != "" {
		baseURL = strings.TrimRight(v, "/")
	}

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 8<<20)
	out := bufio.NewWriter(os.Stdout)
	defer out.Flush()

	for in.Scan() {
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		var req rpcReq
		if err := json.Unmarshal([]byte(line), &req); err != nil {
			writeResp(out, rpcResp{JSONRPC: "2.0", Error: &rpcError{Code: -32700, Message: "JSON 解析失败"}})
			continue
		}
		if len(req.ID) == 0 {
			continue // 通知（initialized 等）不需要回复
		}
		writeResp(out, handle(req))
	}
}

func writeResp(w *bufio.Writer, resp rpcResp) {
	resp.JSONRPC = "2.0"
	b, err := json.Marshal(resp)
	if err != nil {
		b, _ = json.Marshal(rpcResp{JSONRPC: "2.0", ID: resp.ID, Error: &rpcError{Code: -32603, Message: "内部错误"}})
	}
	w.Write(b)
	w.WriteByte('\n')
	w.Flush()
}

func handle(req rpcReq) rpcResp {
	switch req.Method {
	case "initialize":
		ver := "2024-11-05"
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		if json.Unmarshal(req.Params, &p) == nil && p.ProtocolVersion != "" {
			ver = p.ProtocolVersion
		}
		return rpcResp{ID: req.ID, Result: map[string]any{
			"protocolVersion": ver,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "screenrec",
				"version": serverVer,
			},
			"instructions": "查询本机的录屏/录音记录、转写文稿、AI 纪要、待办，也能裁剪片段。" +
				"第一次用先调 list_recordings 拿到 id。所有数据都在本机。",
		}}
	case "ping":
		return rpcResp{ID: req.ID, Result: map[string]any{}}
	case "tools/list":
		return rpcResp{ID: req.ID, Result: map[string]any{"tools": toolDefs()}}
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		if err := json.Unmarshal(req.Params, &p); err != nil {
			return rpcResp{ID: req.ID, Error: &rpcError{Code: -32602, Message: "参数格式错误"}}
		}
		text, isErr := callTool(p.Name, p.Arguments)
		res := map[string]any{
			"content": []map[string]any{{"type": "text", "text": text}},
		}
		if isErr {
			res["isError"] = true
		}
		return rpcResp{ID: req.ID, Result: res}
	default:
		return rpcResp{ID: req.ID, Error: &rpcError{Code: -32601, Message: "不支持的方法：" + req.Method}}
	}
}

func toolDefs() []map[string]any {
	idProp := map[string]any{"type": "string", "description": "记录 id，来自 list_recordings"}
	return []map[string]any{
		{
			"name":        "list_recordings",
			"description": "列出本机的录制记录（最新的在前），含标题、时长、是否已转写。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"limit": map[string]any{"type": "integer", "description": "最多返回几条，默认 20"},
				},
			},
		},
		{
			"name":        "get_transcript",
			"description": "取某条记录的转写文稿。默认带 [mm:ss] 时间戳和说话人标签。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":        idProp,
					"with_time": map[string]any{"type": "boolean", "description": "是否带时间戳，默认 true"},
				},
				"required": []string{"id"},
			},
		},
		{
			"name":        "search_transcripts",
			"description": "在本机所有转写文稿里搜关键词，返回命中的时间点和上下文。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"query": map[string]any{"type": "string", "description": "关键词"},
					"limit": map[string]any{"type": "integer", "description": "最多返回几条，默认 20"},
				},
				"required": []string{"query"},
			},
		},
		{
			"name":        "get_summary",
			"description": "取某条记录已有的会议纪要（Markdown）。没有则提示生成。",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": idProp},
				"required":   []string{"id"},
			},
		},
		{
			"name":        "generate_summary",
			"description": "为某条记录生成会议纪要。没配 API Key 时用本地规则抽取，也能出五段式结构。",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": idProp},
				"required":   []string{"id"},
			},
		},
		{
			"name":        "get_action_items",
			"description": "从某条记录里抽取结构化待办（事项、负责人、时间、出现位置）。",
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": map[string]any{"id": idProp},
				"required":   []string{"id"},
			},
		},
		{
			"name":        "clip_recording",
			"description": "把一条记录裁成片段，可选把字幕烧进画面。会创建一条新记录。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"id":    idProp,
					"start": map[string]any{"type": "number", "description": "起点（秒）"},
					"end":   map[string]any{"type": "number", "description": "终点（秒）"},
					"burn":  map[string]any{"type": "boolean", "description": "是否烧录字幕，默认 false"},
				},
				"required": []string{"id", "start", "end"},
			},
		},
		{
			"name":        "today_agenda",
			"description": "列出今天剩下的日程（需先在设置里开启日历并配好数据源）。",
			"inputSchema": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"refresh": map[string]any{"type": "boolean", "description": "是否先重新拉取日历，默认 false"},
				},
			},
		},
	}
}

func callTool(name string, args map[string]any) (string, bool) {
	switch name {
	case "list_recordings":
		return toolList(args)
	case "get_transcript":
		return toolTranscript(args)
	case "search_transcripts":
		return toolSearch(args)
	case "get_summary":
		return toolGetSummary(args)
	case "generate_summary":
		return toolGenSummary(args)
	case "get_action_items":
		return toolActions(args)
	case "clip_recording":
		return toolClip(args)
	case "today_agenda":
		return toolAgenda(args)
	}
	return "未知工具：" + name, true
}

type segment struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Text    string  `json:"text"`
	Trans   string  `json:"trans,omitempty"`
	Speaker int     `json:"speaker,omitempty"`
}

type recInfo struct {
	ID          string    `json:"id"`
	Title       string    `json:"title"`
	Mode        string    `json:"mode"`
	Status      string    `json:"status"`
	File        string    `json:"file"`
	StartedAt   time.Time `json:"startedAt"`
	DurationMs  int64     `json:"durationMs"`
	SizeBytes   int64     `json:"sizeBytes"`
	Transcribed bool      `json:"transcribed"`
	Summary     string    `json:"summary,omitempty"`
	SummaryMode string    `json:"summaryModel,omitempty"`
	Speakers    int       `json:"speakers"`
	ClipOf      string    `json:"clipOf,omitempty"`
	Segments    []segment `json:"segments"`
	Error       string    `json:"error,omitempty"`
}

func apiGet(path string) ([]byte, error) {
	resp, err := httpCli.Get(baseURL + path)
	if err != nil {
		return nil, fmt.Errorf("连不上录屏工坊（%s）。先双击 screenrec.exe 启动它。", baseURL)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("接口 %s 返回 %d：%s", path, resp.StatusCode, apiErr(body))
	}
	return body, nil
}

func apiPost(path string, payload any) ([]byte, error) {
	var buf bytes.Buffer
	if payload != nil {
		json.NewEncoder(&buf).Encode(payload)
	}
	resp, err := httpCli.Post(baseURL+path, "application/json", &buf)
	if err != nil {
		return nil, fmt.Errorf("连不上录屏工坊（%s）。先双击 screenrec.exe 启动它。", baseURL)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("接口 %s 返回 %d：%s", path, resp.StatusCode, apiErr(body))
	}
	return body, nil
}

func apiErr(body []byte) string {
	var e struct {
		Error string `json:"error"`
	}
	if json.Unmarshal(body, &e) == nil && e.Error != "" {
		return e.Error
	}
	s := string(body)
	if len([]rune(s)) > 200 {
		s = string([]rune(s)[:200]) + "…"
	}
	return s
}

func toolList(args map[string]any) (string, bool) {
	limit := intArg(args, "limit", 20)
	body, err := apiGet("/api/recordings")
	if err != nil {
		return err.Error(), true
	}
	var list []recInfo
	if err := json.Unmarshal(body, &list); err != nil {
		return "解析记录列表失败", true
	}
	if len(list) == 0 {
		return "本机还没有录制记录。", false
	}
	if len(list) > limit {
		list = list[:limit]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 条（显示前 %d 条）：\n\n", len(list), len(list))
	for _, r := range list {
		kind := "录屏"
		if r.Mode == "audio" {
			kind = "录音"
		}
		flags := []string{}
		if r.Transcribed {
			flags = append(flags, "已转写")
		}
		if r.Summary != "" {
			flags = append(flags, "有纪要")
		}
		if r.ClipOf != "" {
			flags = append(flags, "片段")
		}
		fmt.Fprintf(&b, "- %s · %s · %s · %s", r.ID, r.Title, kind, mmss(r.DurationMs))
		if len(flags) > 0 {
			fmt.Fprintf(&b, " · %s", strings.Join(flags, "/"))
		}
		b.WriteString("\n")
	}
	return b.String(), false
}

func toolTranscript(args map[string]any) (string, bool) {
	id := strArg(args, "id")
	if id == "" {
		return "缺少参数 id", true
	}
	body, err := apiGet("/api/recordings/" + url.PathEscape(id))
	if err != nil {
		return err.Error(), true
	}
	var r recInfo
	if err := json.Unmarshal(body, &r); err != nil {
		return "解析记录失败", true
	}
	if len(r.Segments) == 0 {
		return fmt.Sprintf("「%s」还没有转写内容。", r.Title), false
	}
	withTime := boolArg(args, "with_time", true)
	var b strings.Builder
	fmt.Fprintf(&b, "# %s（%s，%d 段）\n\n", r.Title, mmss(r.DurationMs), len(r.Segments))
	for _, s := range r.Segments {
		if withTime {
			fmt.Fprintf(&b, "[%s] ", mmss(int64(s.Start*1000)))
		}
		if s.Speaker > 0 {
			fmt.Fprintf(&b, "说话人%d：", s.Speaker)
		}
		b.WriteString(strings.TrimSpace(s.Text))
		b.WriteString("\n")
	}
	return b.String(), false
}

func toolSearch(args map[string]any) (string, bool) {
	q := strings.TrimSpace(strArg(args, "query"))
	if q == "" {
		return "缺少参数 query", true
	}
	limit := intArg(args, "limit", 20)

	// 优先走服务端索引（一次请求搞定，上百条记录也不用一条条拉详情）
	type idxHit struct {
		ID    string  `json:"id"`
		Title string  `json:"title"`
		Start float64 `json:"start"`
		Text  string  `json:"text"`
		Trans string  `json:"trans,omitempty"`
	}
	type idxResp struct {
		Count   int      `json:"count"`
		Indexed int      `json:"indexed"`
		Hits    []idxHit `json:"hits"`
	}
	body, err := apiGet("/api/search?q=" + url.QueryEscape(q) + "&limit=" + strconv.Itoa(limit))
	if err == nil {
		var out idxResp
		if json.Unmarshal(body, &out) == nil {
			if out.Count == 0 {
				return fmt.Sprintf("没有找到包含「%s」的内容（已索引 %d 条记录）。", q, out.Indexed), false
			}
			var b strings.Builder
			fmt.Fprintf(&b, "找到 %d 条：\n\n", out.Count)
			for _, h := range out.Hits {
				text := strings.TrimSpace(h.Text)
				if len([]rune(text)) > 120 {
					text = string([]rune(text)[:120]) + "…"
				}
				fmt.Fprintf(&b, "- %s · %s · %s\n  %s\n", h.Title, mmss(int64(h.Start*1000)), h.ID, text)
			}
			return b.String(), false
		}
	}

	// 老版本服务端没有 /api/search，退回逐条读取（只取前 40 条，避免太慢）
	listBody, err := apiGet("/api/recordings")
	if err != nil {
		return err.Error(), true
	}
	var list []recInfo
	if err := json.Unmarshal(listBody, &list); err != nil {
		return "解析记录列表失败", true
	}
	if len(list) > 40 {
		list = list[:40]
	}
	ql := strings.ToLower(q)
	type found struct {
		title string
		id    string
		at    float64
		text  string
	}
	var hits []found
	for _, item := range list {
		if len(hits) >= limit {
			break
		}
		rb, err := apiGet("/api/recordings/" + url.PathEscape(item.ID))
		if err != nil {
			continue
		}
		var r recInfo
		if json.Unmarshal(rb, &r) != nil {
			continue
		}
		for _, s := range r.Segments {
			if strings.Contains(strings.ToLower(s.Text), ql) {
				hits = append(hits, found{r.Title, r.ID, s.Start, strings.TrimSpace(s.Text)})
				if len(hits) >= limit {
					break
				}
			}
		}
	}
	if len(hits) == 0 {
		return fmt.Sprintf("没有找到包含「%s」的内容。", q), false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "找到 %d 条：\n\n", len(hits))
	for _, h := range hits {
		text := h.text
		if len([]rune(text)) > 120 {
			text = string([]rune(text)[:120]) + "…"
		}
		fmt.Fprintf(&b, "- %s · %s · %s\n  %s\n", h.title, mmss(int64(h.at*1000)), h.id, text)
	}
	return b.String(), false
}

func toolGetSummary(args map[string]any) (string, bool) {
	id := strArg(args, "id")
	body, err := apiGet("/api/recordings/" + url.PathEscape(id))
	if err != nil {
		return err.Error(), true
	}
	var r recInfo
	if err := json.Unmarshal(body, &r); err != nil {
		return "解析记录失败", true
	}
	if strings.TrimSpace(r.Summary) == "" {
		return fmt.Sprintf("「%s」还没有纪要。可以调用 generate_summary 生成。", r.Title), false
	}
	model := r.SummaryMode
	if model == "" {
		model = "未知"
	}
	return fmt.Sprintf("（模型：%s）\n\n%s", model, r.Summary), false
}

func toolGenSummary(args map[string]any) (string, bool) {
	id := strArg(args, "id")
	body, err := apiPost("/api/recordings/"+url.PathEscape(id)+"/summary", map[string]any{})
	if err != nil {
		return err.Error(), true
	}
	var r struct {
		Summary string `json:"summary"`
		Model   string `json:"model"`
	}
	if json.Unmarshal(body, &r) != nil || r.Summary == "" {
		return "生成失败：接口没有返回内容", true
	}
	return fmt.Sprintf("（模型：%s）\n\n%s", r.Model, r.Summary), false
}

func toolActions(args map[string]any) (string, bool) {
	id := strArg(args, "id")
	body, err := apiGet("/api/recordings/" + url.PathEscape(id) + "/actions")
	if err != nil {
		return err.Error(), true
	}
	var r struct {
		Items []struct {
			Text  string  `json:"text"`
			Owner string  `json:"owner"`
			Due   string  `json:"due"`
			At    float64 `json:"at"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "解析待办失败", true
	}
	if len(r.Items) == 0 {
		return "没有抽到明确待办。", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "共 %d 条待办：\n\n", len(r.Items))
	for _, it := range r.Items {
		owner := it.Owner
		if owner == "" {
			owner = "未指定"
		}
		due := it.Due
		if due == "" {
			due = "未指定"
		}
		fmt.Fprintf(&b, "- [ ] %s（负责人：%s / 时间：%s / %s）\n", it.Text, owner, due, mmss(int64(it.At*1000)))
	}
	return b.String(), false
}

func toolAgenda(args map[string]any) (string, bool) {
	if boolArg(args, "refresh", false) {
		if _, err := apiPost("/api/calendar/refresh", nil); err != nil {
			return err.Error(), true
		}
	}
	body, err := apiGet("/api/calendar")
	if err != nil {
		return err.Error(), true
	}
	var r struct {
		Enabled bool `json:"enabled"`
		Events  []struct {
			Title   string  `json:"title"`
			Start   string  `json:"start"`
			End     string  `json:"end"`
			InSec   int     `json:"inSec"`
			Fired   bool    `json:"fired"`
			JoinURL string  `json:"joinUrl"`
		} `json:"events"`
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &r); err != nil {
		return "解析日程失败", true
	}
	if !r.Enabled {
		return "日历自动录制还没开启：网页左下角「日历自动录制」里打开并填数据源（留空=本机 Outlook）。", false
	}
	if r.Error != "" {
		return "读取日历失败：" + r.Error, false
	}
	if len(r.Events) == 0 {
		return "今天剩下的时间里没有日程。", false
	}
	var b strings.Builder
	fmt.Fprintf(&b, "今天还剩 %d 条日程：\n\n", len(r.Events))
	for _, e := range r.Events {
		st, _ := time.Parse(time.RFC3339, e.Start)
		en, _ := time.Parse(time.RFC3339, e.End)
		mark := ""
		if e.InSec < 0 {
			mark = "（进行中）"
		} else if e.Fired {
			mark = "（已提醒）"
		}
		fmt.Fprintf(&b, "- %s–%s %s%s", st.Format("15:04"), en.Format("15:04"), e.Title, mark)
		if e.JoinURL != "" {
			fmt.Fprintf(&b, "\n  入会：%s", e.JoinURL)
		}
		b.WriteString("\n")
	}
	return b.String(), false
}

func toolClip(args map[string]any) (string, bool) {
	id := strArg(args, "id")
	start := numArg(args, "start", 0)
	end := numArg(args, "end", 0)
	burn := boolArg(args, "burn", false)
	body, err := apiPost("/api/recordings/"+url.PathEscape(id)+"/clip", map[string]any{
		"start": start, "end": end, "burn": burn, "reencode": burn,
	})
	if err != nil {
		return err.Error(), true
	}
	var r recInfo
	if err := json.Unmarshal(body, &r); err != nil {
		return "解析剪辑结果失败", true
	}
	return fmt.Sprintf("已生成片段：%s\nid：%s\n时长：%s\n路径：%s",
		r.Title, r.ID, mmss(r.DurationMs), r.File), false
}

func mmss(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	t := ms / 1000
	if t >= 3600 {
		return fmt.Sprintf("%d:%02d:%02d", t/3600, (t%3600)/60, t%60)
	}
	return fmt.Sprintf("%02d:%02d", t/60, t%60)
}

func strArg(args map[string]any, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func numArg(args map[string]any, key string, def float64) float64 {
	if v, ok := args[key].(float64); ok {
		return v
	}
	return def
}

func intArg(args map[string]any, key string, def int) int {
	if v, ok := args[key].(float64); ok && v > 0 {
		return int(v)
	}
	return def
}

func boolArg(args map[string]any, key string, def bool) bool {
	if v, ok := args[key].(bool); ok {
		return v
	}
	return def
}
