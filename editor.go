package main

// 片段导出：把一条录制裁成一段，可选把字幕烧进画面。
// 剪辑是"录完能直接发出去"的最后一公里，之前只能整条导出。

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ClipOptions 片段参数（秒）
type ClipOptions struct {
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Burn     bool    `json:"burn"`     // 把字幕烧进画面
	Reencode bool    `json:"reencode"` // 精确到帧（默认优先流拷贝，快但只能切在关键帧）
	Title    string  `json:"title"`
}

// clipRecording 生成片段并作为一条新记录返回
func (a *App) clipRecording(id string, opt ClipOptions) (*Recording, error) {
	if a.ffmpeg == "" {
		return nil, fmt.Errorf("未找到 ffmpeg")
	}
	src, ok := a.get(id)
	if !ok {
		return nil, fmt.Errorf("记录不存在")
	}
	a.mu.RLock()
	dur := float64(src.DurationMs) / 1000
	parts := append([]string{}, src.Parts...)
	segs := append([]Segment{}, src.Segments...)
	mode := src.Mode
	a.mu.RUnlock()
	if dur <= 0 {
		dur = float64(probeDuration(a.ffprobe, src.File)) / 1000
	}
	if opt.End <= 0 || opt.End > dur+0.5 {
		opt.End = dur
	}
	if opt.Start < 0 {
		opt.Start = 0
	}
	if opt.End-opt.Start < 0.5 {
		return nil, fmt.Errorf("片段太短（至少 0.5 秒）")
	}
	if mode == "audio" {
		opt.Burn = false // 录音没有画面
	}

	input := src.File
	if len(parts) > 1 {
		merged, cleanup, err := a.mergeMedia(parts)
		if err != nil {
			return nil, err
		}
		defer cleanup()
		input = merged
	}
	if _, err := os.Stat(input); err != nil {
		return nil, fmt.Errorf("源文件不存在：%s", filepath.Base(input))
	}

	now := time.Now()
	clipID := "c" + now.Format("20060102-150405")
	out := filepath.Join(a.recordsDir, clipID+".mp4")
	cut := opt.End - opt.Start

	// 字幕文件放临时目录，ffmpeg 的 cwd 也切过去：
	// 这样 filter 里只写相对文件名，省掉 Windows 盘符冒号的转义地狱。
	tmpDir, err := os.MkdirTemp(a.dataDir, "clip-")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tmpDir)

	args := []string{"-y", "-hide_banner", "-loglevel", "error"}
	args = append(args, "-ss", fmt.Sprintf("%.3f", opt.Start))
	args = append(args, "-t", fmt.Sprintf("%.3f", cut))
	args = append(args, "-i", input)

	if opt.Burn {
		clipSegs := cutSegments(segs, opt.Start, opt.End)
		if len(clipSegs) == 0 {
			return nil, fmt.Errorf("这段时间没有字幕，无法烧录（可先转写）")
		}
		f, err := os.Create(filepath.Join(tmpDir, "clip.srt"))
		if err != nil {
			return nil, err
		}
		if _, err := f.WriteString(toSRT(clipSegs)); err != nil {
			f.Close()
			return nil, err
		}
		f.Close()
		style := "FontName=Microsoft YaHei,FontSize=20,PrimaryColour=&H00FFFFFF," +
			"OutlineColour=&H00000000,BackColour=&H80000000,BorderStyle=1,Outline=1,Shadow=0,MarginV=28"
		args = append(args, "-vf", "subtitles=clip.srt:force_style='"+style+"'")
	}

	if opt.Burn || opt.Reencode {
		args = append(args,
			"-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
			"-pix_fmt", "yuv420p",
			"-c:a", "aac", "-b:a", "160k",
			"-movflags", "+faststart",
		)
	} else {
		args = append(args, "-c", "copy", "-avoid_negative_ts", "make_zero",
			"-movflags", "+faststart")
	}
	args = append(args, out)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.ffmpeg, args...)
	cmd.Dir = tmpDir
	if outBytes, err := cmd.CombinedOutput(); err != nil {
		os.Remove(out)
		return nil, fmt.Errorf("剪辑失败：%v %s", err, tailText(string(outBytes), 300))
	}
	st, err := os.Stat(out)
	if err != nil || st.Size() < 1024 {
		os.Remove(out)
		return nil, fmt.Errorf("剪辑输出为空")
	}

	title := strings.TrimSpace(opt.Title)
	if title == "" {
		title = fmt.Sprintf("%s · 片段 %s", src.Title, now.Format("15:04"))
	}
	clipSegs := cutSegments(segs, opt.Start, opt.End)
	done := now
	rec := &Recording{
		ID:           clipID,
		Title:        title,
		Mode:         mode,
		File:         out,
		StartedAt:    now,
		EndedAt:      &done,
		DurationMs:   int64(cut * 1000),
		Status:       "done",
		Screen:       src.Screen,
		Mic:          src.Mic,
		FPS:          src.FPS,
		CRF:          src.CRF,
		GainDb:       src.GainDb,
		SystemAudio:  src.SystemAudio,
		Encoder:      src.Encoder,
		Segments:     clipSegs,
		LiveText:     joinText(clipSegs),
		Transcribed:  len(clipSegs) > 0,
		SizeBytes:    st.Size(),
		ClipOf:       src.ID,
		ClipStartSec: round2(opt.Start),
		ClipBurned:   opt.Burn,
	}
	a.put(rec)
	a.recorder.save(rec)
	return rec, nil
}

// cutSegments 截取时间窗内的字幕，并把时间轴移到 0 点
func cutSegments(segs []Segment, start, end float64) []Segment {
	out := make([]Segment, 0, len(segs))
	for _, s := range segs {
		ns := s.Start - start
		ne := s.End - start
		if ne <= 0 || ns >= end-start {
			continue
		}
		if ns < 0 {
			ns = 0
		}
		if ne > end-start {
			ne = end - start
		}
		if ne-ns < 0.08 {
			continue
		}
		c := s
		c.Start = round2(ns)
		c.End = round2(ne)
		out = append(out, c)
	}
	return out
}
