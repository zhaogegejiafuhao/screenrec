package main

import (
	"encoding/binary"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// 静音判定的核心：读回环 PCM 末尾算 RMS。
// 判错的两个方向都很难受——误报会让正常录制弹警告，漏报会让用户录完才发现没录到对方。
func writePCM(t *testing.T, path string, samples []int16) {
	t.Helper()
	buf := make([]byte, len(samples)*2)
	for i, s := range samples {
		binary.LittleEndian.PutUint16(buf[i*2:], uint16(s))
	}
	if err := os.WriteFile(path, buf, 0644); err != nil {
		t.Fatal(err)
	}
}

func TestSysPCMLevelSilent(t *testing.T) {
	p := filepath.Join(t.TempDir(), "silent.pcm")
	writePCM(t, p, make([]int16, 48000*2)) // 1 秒 48k 立体声全 0
	rms, ok := sysPCMLevel(p, 48000, 2, 0.5)
	if !ok {
		t.Fatal("应该能读到数据")
	}
	if rms >= sysSilenceRMS {
		t.Errorf("全 0 应该判为静音，实际 RMS=%.1f", rms)
	}
}

func TestSysPCMLevelHasSound(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sound.pcm")
	// 模拟一段正弦波，幅度 3000
	n := 48000 * 2
	s := make([]int16, n)
	for i := 0; i < n; i++ {
		s[i] = int16(3000 * math.Sin(2*math.Pi*440*float64(i)/48000))
	}
	writePCM(t, p, s)
	rms, ok := sysPCMLevel(p, 48000, 2, 0.5)
	if !ok {
		t.Fatal("应该能读到数据")
	}
	if rms < sysSilenceRMS {
		t.Errorf("有明显声音却判成静音，RMS=%.1f", rms)
	}
}

// 极低电平的底噪不能算「有声音」，否则设备不支持回环时永远不提示
func TestSysPCMLevelFaintNoise(t *testing.T) {
	p := filepath.Join(t.TempDir(), "faint.pcm")
	s := make([]int16, 48000*2)
	for i := range s {
		if i%2 == 0 {
			s[i] = 8
		} else {
			s[i] = -8
		}
	}
	writePCM(t, p, s)
	rms, _ := sysPCMLevel(p, 48000, 2, 0.5)
	if rms >= sysSilenceRMS {
		t.Errorf("±8 的底噪不该算有声，RMS=%.1f", rms)
	}
}

func TestSysPCMLevelMissingFile(t *testing.T) {
	if _, ok := sysPCMLevel(filepath.Join(t.TempDir(), "nope.pcm"), 48000, 2, 0.5); ok {
		t.Error("文件不存在应该返回 ok=false，不能让调用方当成静音")
	}
}
