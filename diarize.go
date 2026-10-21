package main

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// DiarSeg 一段说话人区间（speaker 从 0 开始）
type DiarSeg struct {
	Start   float64 `json:"start"`
	End     float64 `json:"end"`
	Speaker int     `json:"speaker"`
}

// Diarize 对媒体文件做说话人分离
func (a *App) Diarize(media string) ([]DiarSeg, error) {
	if a.python == "" || a.ffmpeg == "" {
		return nil, fmt.Errorf("缺少 Python 或 ffmpeg")
	}
	script := filepath.Join(a.assets, "diarize.py")
	if _, err := os.Stat(script); err != nil {
		return nil, fmt.Errorf("缺少 diarize.py")
	}
	if _, err := os.Stat(filepath.Join(a.assets, "models", "diar", "embedding.onnx")); err != nil {
		return nil, fmt.Errorf("缺少说话人模型（models/diar）")
	}
	tmpDir, err := os.MkdirTemp(a.dataDir, "diar-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)
	wav := filepath.Join(tmpDir, "audio16k.wav")

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.ffmpeg, "-y", "-hide_banner", "-loglevel", "error",
		"-i", media, "-vn", "-ac", "1", "-ar", "16000", "-c:a", "pcm_s16le", wav)
	cmd.Dir = filepath.Dir(a.ffmpeg)
	if out, err := cmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("提取音频失败: %v %s", err, firstN(out, 200))
	}

	ctx2, cancel2 := context.WithTimeout(context.Background(), 60*time.Minute)
	defer cancel2()
	c2 := exec.CommandContext(ctx2, a.python, script, wav)
	c2.Dir = a.assets
	out, err := c2.Output()
	if err != nil {
		return nil, fmt.Errorf("分离失败: %v", err)
	}
	var res struct {
		Segments []DiarSeg `json:"segments"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("解析分离结果失败: %v %s", err, strings.TrimSpace(string(out)))
	}
	return res.Segments, nil
}

// assignSpeakers 按时间重叠把说话人编号写到转写分段上（speaker 从 1 开始）
func assignSpeakers(segs []Segment, ds []DiarSeg) int {
	maxSpk := 0
	for i := range segs {
		best, bestOv := -1, 0.0
		for _, d := range ds {
			ov := math.Min(segs[i].End, d.End) - math.Max(segs[i].Start, d.Start)
			if ov > bestOv {
				bestOv, best = ov, d.Speaker
			}
		}
		if best >= 0 && bestOv > 0.05 {
			segs[i].Speaker = best + 1
			if best+1 > maxSpk {
				maxSpk = best + 1
			}
		}
	}
	return maxSpk
}

// runDiarize 后台执行分离并回写结果
func (a *App) runDiarize(rec *Recording) {
	a.mu.Lock()
	paths := append([]string{}, rec.Parts...)
	if len(paths) == 0 {
		paths = []string{rec.File}
	}
	segs := append([]Segment{}, rec.Segments...)
	a.mu.Unlock()

	media, cleanup, err := a.mergeMedia(paths)
	if err == nil {
		defer cleanup()
		var ds []DiarSeg
		ds, err = a.Diarize(media)
		if err == nil {
			n := assignSpeakers(segs, ds)
			a.mu.Lock()
			rec.Segments = segs
			rec.Speakers = n
			rec.Diarized = true
			rec.Diarizing = false
			if err != nil {
				rec.Error = ""
			}
			a.mu.Unlock()
			a.recorder.save(rec)
			return
		}
	}

	a.mu.Lock()
	rec.Diarizing = false
	rec.Error = "说话人分离失败: " + err.Error()
	a.mu.Unlock()
	a.recorder.save(rec)
}
