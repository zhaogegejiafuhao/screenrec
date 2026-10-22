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

	// GPT-SoVITS 有两代接口，参数名不一样：
	//   api_v2.py（v2pro 版自带）走 POST /tts，字段叫 text_lang / ref_audio_path
	//   老 api.py 走 POST /，字段叫 text_language / refer_wav_path
	// 先试新的，不行再退回老的，两边都能用。
	base = strings.TrimRight(base, "/")
	attempts := []struct {
		url     string
		payload map[string]any
	}{
		{
			url: base + "/tts",
			payload: map[string]any{
				"text":            text,
				"text_lang":       lang,
				"ref_audio_path":  ref,
				"prompt_text":     prompt,
				"prompt_lang":     lang,
				"top_k":           15,
				"top_p":           1,
				"temperature":     1,
				"speed":           1.0,
				"media_type":      "wav",
				"text_split_method": "cut5",
			},
		},
		{
			url: base + "/",
			payload: map[string]any{
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
			},
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	cli := &http.Client{Timeout: 10 * time.Minute}

	var audio []byte
	var lastErr error
	for _, at := range attempts {
		payload, _ := json.Marshal(at.payload)
		req, err := http.NewRequestWithContext(ctx, "POST", at.url, bytes.NewReader(payload))
		if err != nil {
			lastErr = fmt.Errorf("接口地址无效：%v", err)
			continue
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := cli.Do(req)
		if err != nil {
			lastErr = fmt.Errorf("连不上 GPT-SoVITS：%v（先启动 api_v2.py）", err)
			continue
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
		resp.Body.Close()
		if resp.StatusCode != 200 {
			lastErr = fmt.Errorf("合成失败（%d）：%s", resp.StatusCode, firstN(body, 200))
			continue
		}
		if len(body) < 1000 {
			lastErr = fmt.Errorf("返回内容不像音频（%d 字节）", len(body))
			continue
		}
		audio = body
		lastErr = nil
		break
	}
	if lastErr != nil {
		return "", lastErr
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
