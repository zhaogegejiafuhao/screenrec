package main

import (
	"os/exec"
	"screenrec/internal/winhide"
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// 系统声音（电脑正在播放的声音）采集。
//
// ffmpeg 在 Windows 上没有 WASAPI 回环输入，所以用 Python + soundcard 采，
// 直接写成裸 s16le PCM 文件；停止后再由 ffmpeg 与麦克风音轨混音。
//
// 为什么不在录制过程中实时混音：试过把 PCM 通过 stdin 喂给 ffmpeg 的 pipe:0，
// 音频一旦供不上，整条 filter 图连同视频一起卡死（实测录屏模式输出卡在 3.5 秒、
// 停止超时、mp4 缺 moov）。改成"先分开录，事后再混"，彻底解耦，
// 代价是实时字幕只有麦克风的声音。

const (
	loopRate = 48000
	loopCh   = 2

	// SysProcessDevice 是「进程级回环」的伪设备名：录所有应用，不挑输出设备
	SysProcessDevice = "__process__"
)

// LoopbackProc 一个运行中的回环采集进程
type LoopbackProc struct {
	cmd     *exec.Cmd
	file    string
	startMs int64 // 采集真正开始的时刻（python 侧）
	errMu   sync.Mutex
	errBuf  strings.Builder
	silent  bool
}

// startLoopback 启动采集，等到 READY 再返回
func (a *App) startLoopback(device, outPath string) (*LoopbackProc, error) {
	if a.python == "" {
		return nil, fmt.Errorf("未找到 Python，无法采集系统声音")
	}
	var script string
	var args []string
	if device == SysProcessDevice {
		// 进程级回环：抓「除自己以外所有进程」的音频，与输出设备无关（蓝牙也能录）
		script = filepath.Join(a.assets, "loopback_proc.py")
		if _, err := os.Stat(script); err != nil {
			return nil, fmt.Errorf("缺少 loopback_proc.py")
		}
		args = []string{"-u", script,
			"--exclude-self",
			"--rate", fmt.Sprint(loopRate),
			"--channels", fmt.Sprint(loopCh),
			"--out", outPath,
		}
	} else {
		script = filepath.Join(a.assets, "loopback.py")
		if _, err := os.Stat(script); err != nil {
			return nil, fmt.Errorf("缺少 loopback.py")
		}
		args = []string{"-u", script,
			"--device", device,
			"--rate", fmt.Sprint(loopRate),
			"--channels", fmt.Sprint(loopCh),
			"--out", outPath,
			"--libs", filepath.Join(a.assets, "pylibs"),
		}
	}
	cmd := winhide.Command(a.python, args...)
	cmd.Dir = a.assets
	errPipe, err := cmd.StderrPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动采集进程失败: %v", err)
	}

	lb := &LoopbackProc{cmd: cmd, file: outPath}
	ready := make(chan int64, 1)
	fail := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(errPipe)
		sent := false
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			switch {
			case strings.HasPrefix(line, "READY "):
				var ms int64
				fmt.Sscanf(line, "READY %d", &ms)
				if !sent {
					sent = true
					ready <- ms
				}
			case strings.HasPrefix(line, "LOOPBACK_FAIL"), strings.HasPrefix(line, "LOOPBACK_ERR"):
				if !sent {
					sent = true
					fail <- line
				}
			case strings.HasPrefix(line, "LOOPBACK_SILENT"):
				lb.errMu.Lock()
				lb.silent = true
				lb.errMu.Unlock()
			case strings.Contains(line, "data discontinuity"):
				// 采集抖动，忽略：不影响后续混音
			default:
				lb.errMu.Lock()
				lb.errBuf.WriteString(line + "\n")
				lb.errMu.Unlock()
				log.Printf("[loopback] %s", line)
			}
		}
	}()

	select {
	case ms := <-ready:
		lb.startMs = ms
		return lb, nil
	case msg := <-fail:
		cmd.Process.Kill()
		return nil, fmt.Errorf("系统声音采集失败: %s", msg)
	case <-time.After(10 * time.Second):
		cmd.Process.Kill()
		return nil, fmt.Errorf("系统声音采集启动超时（设备可能不支持回环）")
	}
}

// IsSilent 采集过程中是否出现过「连续 3 秒全 0」
func (l *LoopbackProc) IsSilent() bool {
	l.errMu.Lock()
	defer l.errMu.Unlock()
	return l.silent
}

func (l *LoopbackProc) Stop() {
	if l == nil {
		return
	}
	if l.cmd.Process != nil {
		l.cmd.Process.Kill()
	}
	done := make(chan struct{})
	go func() { l.cmd.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
	}
}

// mixSystemAudio 把系统声音混进已录好的媒体文件（视频直接复制，不重编码）。
// leadMs：python 比 ffmpeg 早启动的时间，需要从系统声音里裁掉。
func (a *App) mixSystemAudio(src, sysPCM string, leadMs int64, gainDb int) error {
	st, err := os.Stat(sysPCM)
	if err != nil || st.Size() < loopRate*loopCh*2/4 {
		return fmt.Errorf("系统声音文件为空")
	}
	tmp := src + ".mix" + filepath.Ext(src)
	trim := ""
	if leadMs > 20 {
		trim = fmt.Sprintf("atrim=start=%.3f,asetpts=PTS-STARTPTS,", float64(leadMs)/1000.0)
	}

	micChain := "[0:a]anull"
	if gainDb > 0 {
		micChain = fmt.Sprintf("[0:a]volume=%ddB,alimiter=limit=0.95", gainDb)
	}
	filter := fmt.Sprintf("%s[a0];[1:a]%svolume=1.0[a1];[a0][a1]amix=inputs=2:duration=first:dropout_transition=0:normalize=0,alimiter=limit=0.95[a]",
		micChain, trim)

	args := []string{"-y", "-hide_banner", "-loglevel", "error",
		"-i", src,
		"-f", "s16le", "-ar", fmt.Sprint(loopRate), "-ac", fmt.Sprint(loopCh), "-i", sysPCM,
		"-filter_complex", filter,
		"-map", "0:v?", "-map", "[a]",
		"-c:v", "copy", "-c:a", "aac", "-b:a", "160k",
		"-movflags", "+faststart", tmp,
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.ffmpeg, args...)
	cmd.Dir = filepath.Dir(a.ffmpeg)
	if out, err := cmd.CombinedOutput(); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("混音失败: %v %s", err, tailText(string(out), 300))
	}
	if err := os.Rename(tmp, src); err != nil {
		return fmt.Errorf("替换原文件失败: %v", err)
	}
	log.Printf("[mix] 已把系统声音混入 %s", filepath.Base(src))
	return nil
}

// loopDeviceList 带缓存地枚举播放设备（每次起 python 约 1 秒，不宜频繁调）
func (a *App) loopDeviceList() ([]string, error) {
	a.loopMu.Lock()
	if time.Since(a.loopAt) < 5*time.Minute && a.loopDevs != nil {
		devs := append([]string{}, a.loopDevs...)
		a.loopMu.Unlock()
		return devs, nil
	}
	a.loopMu.Unlock()

	devs, err := a.loopDevices()
	if err != nil {
		return nil, err
	}
	a.loopMu.Lock()
	a.loopDevs = append([]string{}, devs...)
	a.loopAt = time.Now()
	a.loopMu.Unlock()
	return devs, nil
}

// loopDevices 列出可用于回环采集的播放设备
func (a *App) loopDevices() ([]string, error) {
	if a.python == "" {
		return nil, fmt.Errorf("未找到 Python")
	}
	code := `
import sys, json
sys.path.insert(0, sys.argv[1])
try:
    import soundcard as sc
    names = [sp.name for sp in sc.all_speakers()]
    print(json.dumps({"devices": names}, ensure_ascii=False))
except Exception as e:
    print(json.dumps({"devices": [], "error": str(e)}, ensure_ascii=False))
`
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := winhide.CommandContext(ctx, a.python, "-c", code, filepath.Join(a.assets, "pylibs"))
	cmd.Dir = a.assets
	out, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("枚举播放设备失败: %v", err)
	}
	var res struct {
		Devices []string `json:"devices"`
		Error   string   `json:"error"`
	}
	if err := json.Unmarshal(out, &res); err != nil {
		return nil, fmt.Errorf("解析设备列表失败")
	}
	if res.Error != "" {
		return nil, fmt.Errorf("%s", res.Error)
	}
	return res.Devices, nil
}
