package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"math"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// LevelInfo 一次麦克风电平采样的结果
type LevelInfo struct {
	PeakDb float64 `json:"peakDb"`
	RmsDb  float64 `json:"rmsDb"`
	Level  int     `json:"level"` // 0-100，用于画音量条
	Ok     bool    `json:"ok"`
	Advice string  `json:"advice"`
}

var (
	encMu   sync.Mutex
	encName string
	encAt   time.Time
)

// detectEncoder 探测可用的 H.264 编码器（优先 NVENC，失败回退 libx264）
func detectEncoder(ff string) string {
	encMu.Lock()
	defer encMu.Unlock()
	if encName != "" && time.Since(encAt) < 5*time.Minute {
		return encName
	}
	encName = "libx264"
	if ff != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error",
			"-f", "lavfi", "-i", "testsrc=size=320x240:rate=5", "-t", "0.4",
			"-c:v", "h264_nvenc", "-f", "null", "-")
		cmd.Dir = filepath.Dir(ff)
		if err := cmd.Run(); err == nil {
			encName = "nvenc"
		}
	}
	encAt = time.Now()
	return encName
}

// sampleMic 采集一小段麦克风音频，返回电平诊断
func (a *App) sampleMic(mic string, dur time.Duration) (*LevelInfo, error) {
	if a.ffmpeg == "" {
		return nil, fmt.Errorf("未找到 ffmpeg")
	}
	if mic == "" {
		return nil, fmt.Errorf("请选择麦克风设备")
	}
	ctx, cancel := context.WithTimeout(context.Background(), dur+15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, a.ffmpeg, "-hide_banner", "-loglevel", "error",
		"-f", "dshow", "-i", "audio="+mic,
		"-t", fmt.Sprintf("%.2f", dur.Seconds()),
		"-f", "s16le", "-ar", "16000", "-ac", "1", "pipe:1")
	cmd.Dir = filepath.Dir(a.ffmpeg)
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("采集失败：%v", err)
	}
	if len(out) < 3200 {
		return nil, fmt.Errorf("采集数据过短，设备可能被占用")
	}

	n := len(out) / 2
	var peak float64
	var sum float64
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(out[i*2:])))
		av := math.Abs(v)
		if av > peak {
			peak = av
		}
		sum += v * v
	}
	rms := math.Sqrt(sum / float64(n))

	db := func(v float64) float64 {
		if v < 1 {
			v = 1
		}
		return 20 * math.Log10(v/32768.0)
	}
	peakDb, rmsDb := db(peak), db(rms)

	// 用 RMS 映射到 0-100 的音量条（-60dB→0，-10dB→100）
	level := int((rmsDb + 60) / 50 * 100)
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}

	advice := "电平正常，可以开始录制"
	ok := true
	switch {
	case peakDb < -60:
		advice = "完全没有信号：麦克风被静音了。打开「设置 → 系统 → 声音 → 输入」，取消静音并把音量调到 80 以上"
		ok = false
	case peakDb < -45:
		advice = "信号极弱（像没开增益）：打开「设置 → 系统 → 声音 → 输入」，把音量调到 80 以上；若是外接耳机麦，检查系统里是否选对了设备"
		ok = false
	case peakDb < -32:
		advice = "声音偏小，识别率会下降：建议调高输入音量，或靠近麦克风说话"
		ok = false
	case peakDb > -2:
		advice = "音量过大可能削波：建议把输入音量调低一些"
	}

	return &LevelInfo{PeakDb: round1(peakDb), RmsDb: round1(rmsDb), Level: level, Ok: ok, Advice: advice}, nil
}

func round1(f float64) float64 {
	return math.Round(f*10) / 10
}

// liveLevel 把一段 PCM 转成 0-100 的实时音量值
func liveLevel(pcm []byte) (int, float64) {
	if len(pcm) < 2 {
		return 0, -90
	}
	n := len(pcm) / 2
	var peak float64
	var sum float64
	step := 1
	if n > 8000 { // 大窗口抽样，避免每 700ms 全量遍历
		step = n / 8000
	}
	count := 0
	for i := 0; i < n; i += step {
		v := float64(int16(binary.LittleEndian.Uint16(pcm[i*2:])))
		av := math.Abs(v)
		if av > peak {
			peak = av
		}
		sum += v * v
		count++
	}
	rms := math.Sqrt(sum / float64(count))
	dbV := 20 * math.Log10(math.Max(rms, 1)/32768.0)
	lv := int((dbV + 60) / 50 * 100)
	if lv < 0 {
		lv = 0
	}
	if lv > 100 {
		lv = 100
	}
	return lv, math.Round(dbV*10) / 10
}
