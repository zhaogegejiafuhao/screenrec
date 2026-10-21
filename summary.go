package main

// AI 纪要：把转写内容交给 OpenAI 兼容接口（DeepSeek / 通义 / Kimi / 本地 Ollama），
// 生成「总结 / 议题 / 结论 / 待办 / 风险」。只发文本，不发音频，且需要用户自己填 Key。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
	url := strings.TrimRight(base, "/") + "/chat/completions"
	payload, _ := json.Marshal(map[string]any{
		"model":       model,
		"messages":    messages,
		"temperature": 0.3,
		"max_tokens":  1800,
		"stream":      false,
	})

	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("接口地址无效：%v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}

	resp, err := (&http.Client{Timeout: 180 * time.Second}).Do(req)
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

// summarizeRecord 生成纪要并写回记录
func (a *App) summarizeRecord(rec *Recording) (string, error) {
	base, key, model := a.set.LLM()
	if key == "" {
		return "", fmt.Errorf("还没有配置 API Key：网页左下角「AI 纪要设置」里填一个")
	}

	a.mu.RLock()
	snap := *rec
	snap.Segments = append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()

	if len(snap.Segments) == 0 {
		return "", fmt.Errorf("这条记录还没有转写内容")
	}

	input, truncated := buildSummaryInput(&snap, 14000)
	ctx, cancel := context.WithTimeout(context.Background(), 180*time.Second)
	defer cancel()

	content, err := chatCompletion(ctx, base, key, model, []chatMessage{
		{Role: "system", Content: summaryPrompt},
		{Role: "user", Content: input},
	})
	if err != nil {
		return "", err
	}
	if truncated {
		content += "\n\n> 注：转写内容过长，纪要仅基于前一部分生成。"
	}

	now := time.Now()
	a.mu.Lock()
	rec.Summary = content
	rec.SummaryModel = model
	rec.SummaryAt = &now
	a.mu.Unlock()
	a.recorder.save(rec)

	return content, nil
}
