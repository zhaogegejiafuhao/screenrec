package main

import (
	"encoding/binary"
	"math"
	"testing"
)

func newLiveRec() *Recording { return &Recording{Segments: []Segment{}} }

func TestAppendLiveTrimsSameSourceOverlap(t *testing.T) {
	rec := newLiveRec()
	appendLive(rec, 0, 2.5, "今天我们来聊一下录音转写")
	appendLive(rec, 2.2, 4.7, "录音转写的实时字幕")
	if len(rec.Segments) != 2 {
		t.Fatalf("期望 2 段，实际 %d：%+v", len(rec.Segments), rec.Segments)
	}
	if rec.Segments[1].Text != "的实时字幕" {
		t.Fatalf("重叠部分没被裁掉：%q", rec.Segments[1].Text)
	}
	if rec.LiveText != "今天我们来聊一下录音转写"+"的实时字幕" {
		t.Fatalf("LiveText 拼接不对：%q", rec.LiveText)
	}
}

func TestAppendLiveDropsCrossSourceEcho(t *testing.T) {
	rec := newLiveRec()
	appendLiveSrc(rec, 1.0, 3.5, "This is a live caption test", "sys")
	// 外放串进麦克风的同一句话（识别结果略有出入）
	appendLiveSrc(rec, 1.3, 3.8, "this is a live caption test", "mic")
	if len(rec.Segments) != 1 {
		t.Fatalf("回声没被丢弃：%+v", rec.Segments)
	}
}

func TestAppendLiveKeepsDifferentSources(t *testing.T) {
	rec := newLiveRec()
	appendLiveSrc(rec, 1.0, 3.0, "对方在视频里说话", "sys")
	appendLiveSrc(rec, 3.2, 5.0, "我这边也回应了一句", "mic")
	if len(rec.Segments) != 2 {
		t.Fatalf("两路不同内容应该都保留：%+v", rec.Segments)
	}
	if rec.Segments[0].Source != "sys" || rec.Segments[1].Source != "mic" {
		t.Fatalf("来源标记不对：%+v", rec.Segments)
	}
}

func TestAppendLiveIgnoresOldDifferentSource(t *testing.T) {
	rec := newLiveRec()
	appendLiveSrc(rec, 0, 2, "很久以前的一句话", "sys")
	appendLiveSrc(rec, 60, 62, "很久以前的一句话", "mic")
	if len(rec.Segments) != 2 {
		t.Fatalf("相隔一分钟的相同文本不该当成回声：%+v", rec.Segments)
	}
}

func TestTextSimilar(t *testing.T) {
	if textSimilar("你好世界", "你好世界") != 1 {
		t.Fatal("相同文本相似度应为 1")
	}
	if textSimilar("今天天气不错", "完全不同的内容") > 0.1 {
		t.Fatal("不同文本相似度应接近 0")
	}
	if textSimilar("abc", "abc") != 1 {
		t.Fatal("短文本相同也应判 1")
	}
}

func TestDownmix48kStereo(t *testing.T) {
	const frames = 48000 // 1 秒
	raw := make([]byte, frames*4)
	for i := 0; i < frames; i++ {
		v := int16(8000 * math.Sin(2*math.Pi*440*float64(i)/48000))
		binary.LittleEndian.PutUint16(raw[i*4:], uint16(v))
		binary.LittleEndian.PutUint16(raw[i*4+2:], uint16(v))
	}
	out := downmix48kStereo(raw)
	if len(out) != 16000*2 {
		t.Fatalf("降采样后长度不对：%d，期望 %d", len(out), 16000*2)
	}
	// 幅度应基本保持（3 点均值对 440Hz@48k 的衰减很小）
	var sum float64
	n := len(out) / 2
	for i := 0; i < n; i++ {
		v := float64(int16(binary.LittleEndian.Uint16(out[i*2:])))
		sum += v * v
	}
	rms := math.Sqrt(sum / float64(n))
	if rms < 4000 || rms > 6000 {
		t.Fatalf("降采样后能量异常：rms=%.0f（原始正弦 rms 约 5657）", rms)
	}
}
