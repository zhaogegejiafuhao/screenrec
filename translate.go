package main

// 本地翻译：调用 translate.py（Helsinki-NLP opus-mt，MarianMT）把每句译文写回 Segment.Trans。
// 全程离线，模型放在 assets/models/mt。

import (
	"screenrec/internal/winhide"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"screenrec/internal/mathnorm"
)

// detectDirection 自动判断翻译方向：中文占比高就中译英，否则英译中
func detectDirection(text string) string {
	var cjk, latin int
	for _, r := range text {
		switch {
		case unicode.Is(unicode.Han, r):
			cjk++
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
			latin++
		}
	}
	if cjk == 0 {
		return "en2zh"
	}
	if latin == 0 {
		return "zh2en"
	}
	if cjk >= latin/2 {
		return "zh2en"
	}
	return "en2zh"
}

func directionName(dir string) string {
	if dir == "zh2en" {
		return "中 → 英"
	}
	return "英 → 中"
}

// formulaOnly 判断是否「纯公式句」：拉丁字母很少且带数学符号
func formulaOnly(s string) bool {
	letters := 0
	for _, r := range s {
		if r < 128 && unicode.IsLetter(r) {
			letters++
		}
	}
	return letters <= 8 && strings.ContainsAny(s, "²³^∫√")
}

// translateRecord 给整条记录补译文；返回翻译了多少句
func (a *App) translateRecord(rec *Recording, dir string) (int, error) {
	if a.python == "" {
		return 0, fmt.Errorf("未找到 Python，无法翻译")
	}

	a.mu.RLock()
	segs := append([]Segment{}, rec.Segments...)
	a.mu.RUnlock()
	if len(segs) == 0 {
		return 0, fmt.Errorf("这条记录还没有转写内容")
	}

	if dir == "" || dir == "auto" {
		raw := make([]string, len(segs))
		for i, s := range segs {
			raw[i] = s.Text
		}
		dir = detectDirection(strings.Join(raw, ""))
	}
	if dir != "en2zh" && dir != "zh2en" {
		return 0, fmt.Errorf("不支持的翻译方向：%s", dir)
	}

	texts := make([]string, len(segs))
	for i, s := range segs {
		// 英译中先把口语公式变符号（x squared → x²），否则模型会译成「正方」
		if dir == "en2zh" {
			texts[i] = mathnorm.Normalize(s.Text)
		} else {
			texts[i] = s.Text
		}
	}

	script := filepath.Join(a.assets, "translate.py")
	if _, err := os.Stat(script); err != nil {
		return 0, fmt.Errorf("缺少 translate.py")
	}
	modelDir := filepath.Join(a.assets, "models", "mt")
	if _, err := os.Stat(modelDir); err != nil {
		return 0, fmt.Errorf("缺少翻译模型（models/mt）")
	}

	tmp, err := os.MkdirTemp(a.dataDir, "mt-")
	if err != nil {
		return 0, err
	}
	defer os.RemoveAll(tmp)

	inPath := filepath.Join(tmp, "in.json")
	outPath := filepath.Join(tmp, "out.json")
	payload, _ := json.Marshal(map[string]any{"texts": texts})
	if err := os.WriteFile(inPath, payload, 0644); err != nil {
		return 0, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.python, script,
		"--dir", dir,
		"--models", modelDir,
		"--in", inPath,
		"--out", outPath,
	)
	cmd.Dir = a.assets
	// 首次加载模型约 5~15 秒，输出只用于报错
	out, err := cmd.CombinedOutput()
	if err != nil {
		return 0, fmt.Errorf("翻译失败：%v %s", err, firstN(out, 300))
	}

	raw, err := os.ReadFile(outPath)
	if err != nil {
		return 0, fmt.Errorf("翻译没有产出结果：%v", err)
	}
	var resp struct {
		Texts   []string `json:"texts"`
		Seconds float64  `json:"seconds"`
		Error   string   `json:"error"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return 0, fmt.Errorf("解析翻译结果失败：%v", err)
	}
	if resp.Error != "" {
		return 0, fmt.Errorf("翻译出错：%s", resp.Error)
	}
	if len(resp.Texts) != len(segs) {
		return 0, fmt.Errorf("译文数量不匹配（%d/%d）", len(resp.Texts), len(segs))
	}

	a.mu.Lock()
	for i := range rec.Segments {
		rec.Segments[i].Trans = resp.Texts[i]
		// 纯公式句（x² + y² = c² / O(n²)）翻译模型会把上标压平成 x2，
		// 与其被改坏，不如保留规范化后的原文
		if formulaOnly(texts[i]) {
			rec.Segments[i].Trans = texts[i]
		}
	}
	rec.TranslateDir = dir
	a.mu.Unlock()
	a.recorder.save(rec)

	return len(resp.Texts), nil
}
