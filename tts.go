package main

// 声音克隆：把转写文本交给本机 GPT-SoVITS 服务合成语音。
// 只发文本和参考音频路径，音频生成完全在你自己的机器上完成。

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const ttsMaxRunes = 800

// synthesizeSpeech 生成配音并返回 wav 路径；text 为空时用整条转写
func (a *App) synthesizeSpeech(rec *Recording, text string) (string, error) {
	base, ref, prompt, lang := a.set.TTSConfig()
	if base == "" {
		return "", fmt.Errorf("还没有配置 GPT-SoVITS 地址（网页左下角「配音设置」）")
	}
	if ref == "" {
		return "", fmt.Errorf("还没有配置参考音频路径")
	}
	if _, err := os.Stat(ref); err != nil {
		return "", fmt.Errorf("参考音频不存在：%s", ref)
	}

	if strings.TrimSpace(text) == "" {
		a.mu.RLock()
		text = joinText(rec.Segments)
		a.mu.RUnlock()
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return "", fmt.Errorf("这条记录还没有可朗读的文本")
	}
	truncated := false
	if rs := []rune(text); len(rs) > ttsMaxRunes {
		text = string(rs[:ttsMaxRunes]) + "。"
		truncated = true
	}

	payload, _ := json.Marshal(map[string]any{
		"text":            text,
		"text_language":   lang,
		"refer_wav_path":  ref,
		"prompt_text":     prompt,
		"prompt_language": lang,
		"top_k":           15,
		"top_p":           1,
		"temperature":     1,
		"speed":           1.0,
		"if_sr":           false,
		"cut_punc":        "，。！？",
	})
	url := strings.TrimRight(base, "/") + "/"

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("接口地址无效：%v", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := (&http.Client{Timeout: 10 * time.Minute}).Do(req)
	if err != nil {
		return "", fmt.Errorf("连不上 GPT-SoVITS：%v（先双击「启动API.bat」）", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("合成失败（%d）：%s", resp.StatusCode, firstN(body, 200))
	}
	audio, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return "", err
	}
	if len(audio) < 1000 {
		return "", fmt.Errorf("返回内容不像音频（%d 字节）", len(audio))
	}

	out := filepath.Join(a.recordsDir, rec.ID+"-tts.wav")
	if err := os.WriteFile(out, audio, 0644); err != nil {
		return "", err
	}
	if truncated {
		return out, fmt.Errorf("文本超过 %d 字，只合成了前 %d 字", ttsMaxRunes, ttsMaxRunes)
	}
	return out, nil
}
