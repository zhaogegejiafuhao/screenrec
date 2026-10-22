package main

// AI 纪要：把转写内容交给 OpenAI 兼容接口（DeepSeek / 通义 / Kimi / 本地 llama.cpp），
// 生成「总结 / 议题 / 结论 / 待办 / 风险」。只发文本，不发音频。
// 云端要用户自己填 Key；本机模型不用联网；两者都没有时退到规则抽取。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

const summaryPrompt = `你是一名严谨的会议纪要助手。请只依据给出的转写内容整理，不要编造任何信息。
输出 Markdown，结构固定：

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
- 保留关键数字、人名、产品名、时间点
- 用中文输出；原文是英文时仍用中文总结，但专有名词保留原文
- 信息不足的部分直接写「原文未提及」，不要推测`

// buildSummaryInput 把分段转写成带时间戳的文本，过长时截断
func buildSummaryInput(rec *Recording, maxRunes int) (string, bool) {
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
	if truncated {
		b.WriteString("\n（转写过长，以上为前一部分）\n")
	}
	return b.String(), truncated
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// chatCompletion 调用 OpenAI 兼容的 /chat/completions
func chatCompletion(ctx context.Context, base, key, model string, messages []chatMessage) (string, error) {
	return chatCompletionEx(ctx, base, key, model, messages, 180*time.Second, nil)
}

// chatCompletionEx 同上，但可指定超时和额外的请求字段（本地模型要用）
func chatCompletionEx(ctx context.Context, base, key, model string, messages []chatMessage, timeout time.Duration, extra map[string]any) (string, error) {
	url := strings.TrimRight(base, "/") + "/chat/completions"
	payloadMap := map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": 0.3,
		"max_tokens":  1800,
		"stream":      false,
	}
	for k, v := range extra {
		payloadMap[k] = v
	}
	payload, _ := json.Marshal(payloadMap)

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("接口地址无效：%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	if timeout <= 0 {
		timeout = 180 * time.Second
	}
	resp, err := (&http.Client{Timeout: timeout}).Do(req)
	if err != nil {
		return "", fmt.Errorf("连不上接口：%v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		switch resp.StatusCode {
		case 401:
			return "", fmt.Errorf("API Key 无效或已过期（401）")
		case 403:
			return "", fmt.Errorf("没有权限（403），检查 Key 的可用模型")
		case 404:
			return "", fmt.Errorf("接口地址不对（404）：应形如 https://api.deepseek.com/v1")
		case 429:
			return "", fmt.Errorf("请求太频繁或额度用完（429）")
		}
		return "", fmt.Errorf("接口返回 %d：%s", resp.StatusCode, firstN(body, 300))
	}

	var out struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("解析响应失败：%v", err)
	}
	if out.Error != nil && out.Error.Message != "" {
		return "", fmt.Errorf("接口报错：%s", out.Error.Message)
	}
	if len(out.Choices) == 0 || strings.TrimSpace(out.Choices[0].Message.Content) == "" {
		return "", fmt.Errorf("接口没有返回内容")
	}
	return strings.TrimSpace(out.Choices[0].Message.Content), nil
}

// summarizeRecord 生成纪要并写回记录。
//
// 引擎优先级由设置决定：
//   - cloud：只用云端 API（没 Key 直接报错，不偷偷改用别的）
//   - local：只用本机 llama.cpp（没起来就尝试拉起，拉不起来报错）
//   - off  ：不联网、不跑模型，纯规则抽取
//   - fusion：规则抽候选 + 模型甄别改写（评测下来待办最准，见 docs/summary-bench.md）
//   - auto ：等价于 fusion——有模型走融合，没有模型才退规则
//

// llmSpec 一个可用的模型后端
type llmSpec struct {
	base    string
	key     string
	model   string
	timeout time.Duration
	extra   map[string]any
	label   string
}

// errNoModel 没有可用模型后端（既没拉起本地、也没填 Key）
var errNoModel = fmt.Errorf("没有可用的模型后端")

// pickModel 按引擎设置挑后端：local/fusion 优先本机，cloud 只用云端，
// auto 是「本机能用就用，否则云端，都没有就规则」。
func (a *App) pickModel(engine, base, key, model, localURL, localModel, localBin, localGGUF string, gpuLayers int) (llmSpec, error) {
	useLocal := false
	switch engine {
	case "local", "fusion":
		useLocal = true
	case "cloud":
		useLocal = false
	default: // auto
		useLocal = localBin != "" || localGGUF != "" || a.localLLMUp(localURL)
	}

	var localErr error
	if useLocal {
		if err := a.ensureLocalLLM(localBin, localGGUF, localURL, gpuLayers); err == nil {
			return llmSpec{localURL, "", localModel, localTimeout, localChatExtra, "本地模型 " + localModel}, nil
		} else {
			localErr = err
		}
	}

	if key != "" {
		return llmSpec{base, key, model, 180 * time.Second, nil, model}, nil
	}
	if engine == "cloud" {
		return llmSpec{}, fmt.Errorf("选了云端纪要但没填 API Key")
	}
	if localErr != nil {
		return llmSpec{}, localErr
	}
	return llmSpec{}, errNoModel
}

func (a *App) summarizeRecord(rec *Recording) (string, error) {
	engine, base, key, model, localURL, localModel, localBin, localGGUF, gpuLayers := a.set.LLMEngineConfig()

	// 融合是评测里最强的路线（F1 0.56 vs 规则 0.33 / 纯模型 0.39），
	// 且耗时和纯模型一样，所以 auto 有模型就用融合，没有才退规则
	if engine == "auto" {
		engine = "fusion"
	}

	if engine == "off" {
		return a.writeLocalSummary(rec)
	}

	spec, err := a.pickModel(engine, base, key, model, localURL, localModel, localBin, localGGUF, gpuLayers)
	if err != nil {
		// 融合的前半段本来就是规则抽取，没模型时退化成规则即可，不必报错
		if engine == "fusion" {
			log.Printf("[llm] 融合纪要没有可用模型，退回规则抽取：%v", err)
			return a.writeLocalSummary(rec)
		}
		return a.writeLocalSummary(rec)
	}

	if engine == "fusion" {
		return a.fusionSummary(rec, spec)
	}
	return a.modelSummaryWith(rec, spec)
}

// localTimeout 本地 4B 模型生成一段纪要的时间；CPU 上可能更久，给足 10 分钟
const localTimeout = 600 * time.Second

// localChatExtra 关掉 Qwen3 的思考过程：纪要要的是结果，不要长篇推理
var localChatExtra = map[string]any{
	"chat_template_kwargs": map[string]any{"enable_thinking": false},
}

func (a *App) modelSummaryWith(rec *Recording, spec llmSpec) (string, error) {
	a.mu.RLock()
	snap := *rec
	snap.Segments = append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()

	if len(snap.Segments) == 0 {
		return "", fmt.Errorf("这条记录还没有转写内容")
	}

	input, truncated := buildSummaryInput(&snap, 14000)
	ctx, cancel := context.WithTimeout(context.Background(), spec.timeout)
	defer cancel()

	content, err := chatCompletionEx(ctx, spec.base, spec.key, spec.model, []chatMessage{
		{Role: "system", Content: summaryPrompt},
		{Role: "user", Content: input},
	}, spec.timeout, spec.extra)
	if err != nil {
		return "", err
	}
	if truncated {
		content += "\n\n> 注：转写内容过长，纪要仅基于前一部分生成。"
	}

	now := time.Now()
	a.mu.Lock()
	rec.Summary = content
	rec.SummaryModel = spec.label
	rec.SummaryAt = &now
	a.mu.Unlock()
	a.recorder.save(rec)
	return content, nil
}

// writeLocalSummary 规则抽取版纪要，写回记录
func (a *App) writeLocalSummary(rec *Recording) (string, error) {
	content, err := a.localSummary(rec)
	if err != nil {
		return "", err
	}
	now := time.Now()
	a.mu.Lock()
	rec.Summary = content
	rec.SummaryModel = "本地抽取（规则）"
	rec.SummaryAt = &now
	a.mu.Unlock()
	a.recorder.save(rec)
	return content, nil
}
