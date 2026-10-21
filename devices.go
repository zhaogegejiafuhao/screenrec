package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// Device 一个 DirectShow 采集设备
type Device struct {
	Name  string `json:"name"`
	Kind  string `json:"kind"` // audio / video
	Label string `json:"label"`
}

var (
	devCacheMu sync.Mutex
	devCache   []Device
	devCacheAt time.Time

	screenMu    sync.Mutex
	screenCache []int
	screenAt    time.Time
)

// findTool 依次在环境变量、固定目录、PATH 中查找可执行文件
func findTool(envKey string, candidates []string, name string) string {
	if p := os.Getenv(envKey); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, p := range candidates {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// exeDir 返回可执行文件所在目录（用于找随程序分发的 ffmpeg / python）
func exeDir() string {
	if exe, err := os.Executable(); err == nil {
		return filepath.Dir(exe)
	}
	return ""
}

func findFFmpeg() string {
	var cands []string
	if d := exeDir(); d != "" {
		cands = append(cands,
			filepath.Join(d, "ffmpeg", "bin", "ffmpeg"+exeSuffix()),
			filepath.Join(d, "ffmpeg"+exeSuffix()),
		)
	}
	cands = append(cands, `C:\ffmpeg\bin\ffmpeg.exe`)
	return findTool("SCREENREC_FFMPEG", cands, "ffmpeg")
}

func findFFprobe() string {
	var cands []string
	if d := exeDir(); d != "" {
		cands = append(cands,
			filepath.Join(d, "ffmpeg", "bin", "ffprobe"+exeSuffix()),
			filepath.Join(d, "ffprobe"+exeSuffix()),
		)
	}
	cands = append(cands, `C:\ffmpeg\bin\ffprobe.exe`)
	p := findTool("SCREENREC_FFPROBE", cands, "ffprobe")
	if p == "" {
		// 从 ffmpeg 同目录推导
		if ff := findFFmpeg(); ff != "" {
			cand := filepath.Join(filepath.Dir(ff), "ffprobe"+exeSuffix())
			if _, err := os.Stat(cand); err == nil {
				return cand
			}
		}
	}
	return p
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// findPython 依次尝试：环境变量 → exe 同目录的 runtime/ → PATH
func findPython() string {
	if p := os.Getenv("SCREENREC_PYTHON"); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	var cands []string
	if d := exeDir(); d != "" {
		cands = append(cands,
			filepath.Join(d, "runtime", "python"+exeSuffix()),
			filepath.Join(d, "python", "python"+exeSuffix()),
		)
	}
	for _, p := range cands {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	for _, name := range []string{"python", "python3"} {
		if p, err := exec.LookPath(name); err == nil {
			return p
		}
	}
	return ""
}

var dshowRe = regexp.MustCompile(`"([^"]+)"\s+\((audio|video)\)`)

// listDevices 枚举 DirectShow 设备（结果缓存 10 秒）
func listDevices(ff string) []Device {
	devCacheMu.Lock()
	defer devCacheMu.Unlock()
	if time.Since(devCacheAt) < 10*time.Second && devCache != nil {
		return devCache
	}
	if ff == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-f", "dshow", "-list_devices", "true", "-i", "dummy")
	cmd.Dir = filepath.Dir(ff)
	out, _ := cmd.CombinedOutput()
	var devs []Device
	seen := map[string]bool{}
	for _, m := range dshowRe.FindAllStringSubmatch(string(out), -1) {
		name, kind := m[1], m[2]
		if seen[name] {
			continue
		}
		seen[name] = true
		label := name
		if kind == "audio" {
			label = "🎤 " + name
		}
		devs = append(devs, Device{Name: name, Kind: kind, Label: label})
	}
	devCache, devCacheAt = devs, time.Now()
	return devs
}

// probeScreens 探测可用的显示器序号（ddagrab output_idx），结果缓存 60 秒
func probeScreens(ff string) []int {
	if ff == "" {
		return nil
	}
	screenMu.Lock()
	defer screenMu.Unlock()
	if time.Since(screenAt) < 60*time.Second && screenCache != nil {
		return screenCache
	}
	var out []int
	var wg sync.WaitGroup
	ok := make([]bool, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error",
				"-f", "lavfi", "-i", fmt.Sprintf("ddagrab=%d:framerate=5", idx),
				"-frames:v", "1", "-f", "null", "-")
			cmd.Dir = filepath.Dir(ff)
			ok[idx] = cmd.Run() == nil
		}(i)
	}
	wg.Wait()
	for i := 0; i < 4; i++ {
		if ok[i] {
			out = append(out, i)
		}
	}
	if len(out) == 0 {
		out = []int{0}
	}
	screenCache, screenAt = out, time.Now()
	return out
}

// probeDuration 读取媒体时长（毫秒）
func probeDuration(probe, path string) int64 {
	if probe == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, probe, "-v", "error",
		"-show_entries", "format=duration", "-of", "default=nw=1:nk=1", path)
	cmd.Dir = filepath.Dir(probe)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var sec float64
	fmt.Sscanf(strings.TrimSpace(string(out)), "%f", &sec)
	return int64(sec * 1000)
}
