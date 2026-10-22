package main

import (
	"os/exec"
	"screenrec/internal/winhide"
	"context"
	"fmt"
	"os"
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
	devBusy    bool

	screenMu    sync.Mutex
	screenCache []int
	screenAt    time.Time
	screenBusy  bool
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
// enumerateDevices 真正跑一次 ffmpeg 枚举（慢，1~2 秒）。调用方负责缓存。
func enumerateDevices(ff string) []Device {
	if ff == "" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	cmd := winhide.CommandContext(ctx, ff, "-hide_banner", "-f", "dshow", "-list_devices", "true", "-i", "dummy")
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
	return devs
}

// warmDeviceCache 启动时同步枚举一次，保证打开页面就能看到设备列表
func warmDeviceCache(ff string) {
	devs := enumerateDevices(ff)
	if devs == nil {
		return
	}
	devCacheMu.Lock()
	devCache, devCacheAt = devs, time.Now()
	devCacheMu.Unlock()
}

// listDevices 返回设备列表。缓存 3 分钟，过期后先返回旧列表、后台刷新——
// 插拔耳机最多 3 分钟内自动出现，而轮询不再被枚举拖住。
// （枚举要跑一遍 ffmpeg -list_devices，实测 1~2 秒，而 /api/state 每 1.2 秒就被调用一次。）
func listDevices(ff string) []Device {
	if ff == "" {
		return nil
	}
	devCacheMu.Lock()
	if devCache != nil && time.Since(devCacheAt) < 3*time.Minute {
		v := devCache
		devCacheMu.Unlock()
		return v
	}
	cached := devCache
	if devBusy {
		devCacheMu.Unlock()
		return cached
	}
	devBusy = true
	devCacheMu.Unlock()

	go func() {
		devs := enumerateDevices(ff)
		devCacheMu.Lock()
		if devs != nil {
			devCache, devCacheAt = devs, time.Now()
		}
		devBusy = false
		devCacheMu.Unlock()
	}()

	return cached
}

// enumerateScreens 并发试 4 个 ddagrab 序号，返回能出帧的那些
func enumerateScreens(ff string) []int {
	var out []int
	var wg sync.WaitGroup
	ok := make([]bool, 4)
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			defer cancel()
			cmd := winhide.CommandContext(ctx, ff, "-hide_banner", "-loglevel", "error",
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
	return out
}

// warmScreenCache 启动时同步探一次显示器
func warmScreenCache(ff string) {
	if ff == "" {
		return
	}
	out := enumerateScreens(ff)
	screenMu.Lock()
	screenCache, screenAt = out, time.Now()
	screenMu.Unlock()
}

// probeScreens 返回可用的显示器序号（ddagrab output_idx）。
//
// 探测要并发拉起 4 个 ffmpeg 各试一次 ddagrab，同步做会让 /api/state 卡一两秒；
// 而它就在 /api/state 里被前端每 1.2 秒调用一次。所以同样改成「先返回缓存、
// 后台探一次」，第一次调用时先假定只有主屏。
func probeScreens(ff string) []int {
	if ff == "" {
		return nil
	}
	screenMu.Lock()
	if screenCache != nil && time.Since(screenAt) < 10*time.Minute {
		v := screenCache
		screenMu.Unlock()
		return v
	}
	cached := screenCache
	if screenBusy {
		screenMu.Unlock()
		if cached == nil {
			return []int{0}
		}
		return cached
	}
	screenBusy = true
	screenMu.Unlock()

	go func() {
		out := enumerateScreens(ff)
		screenMu.Lock()
		screenCache, screenAt, screenBusy = out, time.Now(), false
		screenMu.Unlock()
	}()

	if cached != nil {
		return cached
	}
	return []int{0}
}

// probeDuration 读取媒体时长（毫秒）
func probeDuration(probe, path string) int64 {
	if probe == "" {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := winhide.CommandContext(ctx, probe, "-v", "error",
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
