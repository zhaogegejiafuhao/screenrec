package main

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 需要本地翻译模型（models/mt）与 Python 环境；没有就跳过
func TestLiveTranslateWorker(t *testing.T) {
	python := findPython()
	if python == "" {
		t.Skip("没有可用的 Python")
	}
	abs, _ := filepath.Abs(".")
	if _, err := filepath.Glob(filepath.Join(abs, "models", "mt", "en-zh", "*.bin")); err != nil {
		t.Skip("缺少翻译模型")
	}

	app := &App{python: python, assets: abs}
	defer app.CloseLiveTranslate()

	start := time.Now()
	out, err := app.translateLive("The meeting will start at three tomorrow afternoon.", "en2zh")
	if err != nil {
		t.Fatalf("首次翻译失败: %v", err)
	}
	t.Logf("首次（含加载）耗时 %.1fs -> %s", time.Since(start).Seconds(), out)
	if !strings.Contains(out, "会议") {
		t.Errorf("翻译结果异常: %q", out)
	}

	start = time.Now()
	out2, err := app.translateLive("Please prepare the quarterly report.", "en2zh")
	if err != nil {
		t.Fatalf("第二次翻译失败: %v", err)
	}
	warm := time.Since(start).Seconds()
	t.Logf("热态耗时 %.2fs -> %s", warm, out2)
	if warm > 5 {
		t.Errorf("热态太慢：%.2fs", warm)
	}
	if !strings.Contains(out2, "报告") {
		t.Errorf("翻译结果异常: %q", out2)
	}
}
