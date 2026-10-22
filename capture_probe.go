package main

// 采集画面健康检查。
//
// 为什么需要它：gdigrab 抓 GPU 合成（硬件加速）的窗口——Edge/Chrome、微信小程序、
// 播放器、游戏——会输出**纯黑帧**，但文件照常生成、声音也在，用户往往回放时才发现
// 整段画面全黑。这类问题没法靠 ffmpeg 返回值判断，只能看画面本身。
//
// 判据：YUV limited range 下纯黑的 Y=16，正常桌面普遍 >100（实测 Edge 黑窗口 16.03、
// 微信窗口 225.7），两者区分度极大，取 22 作阈值。

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"time"
)

// blackLuma 平均亮度低于此值判为黑屏
const blackLuma = 22.0

var lumaRe = regexp.MustCompile(`YAVG=([0-9.]+)`)

// probeLuma 抓几帧算平均亮度。inArgs 是采集侧参数（形如 -f gdigrab -framerate 10 -i xxx）。
// hw=true 表示输入是 ddagrab 的 D3D11 硬件帧，必须先 hwdownload 才能送进 signalstats
// （否则报 Function not implemented，预检会误判成黑屏）。
func probeLuma(ff string, inArgs []string, hw bool) (float64, bool) {
	if ff == "" {
		return 0, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	vf := "signalstats,metadata=print:key=lavfi.signalstats.YAVG"
	if hw {
		vf = "hwdownload,format=bgra," + vf
	}
	args := append([]string{"-hide_banner"}, inArgs...)
	args = append(args, "-frames:v", "3", "-vf", vf, "-f", "null", "-")

	cmd := exec.CommandContext(ctx, ff, args...)
	cmd.Dir = filepath.Dir(ff)
	out, _ := cmd.CombinedOutput()

	matches := lumaRe.FindAllStringSubmatch(string(out), -1)
	if len(matches) == 0 {
		return 0, false
	}
	sum, n := 0.0, 0
	for _, m := range matches {
		if v, err := strconv.ParseFloat(m[1], 64); err == nil {
			sum += v
			n++
		}
	}
	if n == 0 {
		return 0, false
	}
	return sum / float64(n), true
}

// capturePlan 实际要用的采集方式
type capturePlan struct {
	kind   string   // "ddagrab" / "gdigrab-desktop" / "gdigrab-window" / "ddagrab-crop"
	screen int      // ddagrab 的显示器序号
	crop   *winRect // ddagrab-crop 时裁到这块区域
	note   string   // 给用户看的说明（降级原因）
}

// cropFilter 生成「全屏采集后裁剪到窗口」的滤镜链。
//
// 两个坑：
//   - 窗口可能比屏幕还大（最大化窗口含边框，实测 1938x1038 在 1920x1080 屏上），
//     直接 crop 会报 "Invalid too big or non positive size" 让 ffmpeg 立刻退出，
//     所以宽高也要用 min() 夹到画面尺寸
//   - crop 表达式里的逗号是滤镜参数分隔符，必须转义成 \,
func (c capturePlan) cropFilter() string {
	if c.crop == nil {
		return "hwdownload,format=bgra,scale=trunc(iw/2)*2:trunc(ih/2)*2"
	}
	w := ((c.crop.Right - c.crop.Left) / 2) * 2
	h := ((c.crop.Bottom - c.crop.Top) / 2) * 2
	if w < 16 || h < 16 {
		return "hwdownload,format=bgra,scale=trunc(iw/2)*2:trunc(ih/2)*2"
	}
	x, y := c.crop.Left, c.crop.Top
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return fmt.Sprintf(
		"hwdownload,format=bgra,crop=min(%d\\,in_w):min(%d\\,in_h):min(%d\\,in_w-min(%d\\,in_w)):min(%d\\,in_h-min(%d\\,in_h)),scale=trunc(iw/2)*2:trunc(ih/2)*2",
		w, h, x, w, y, h)
}

// planCapture 决定用哪种方式采集，并在必要时降级。
//
// 顺序：
//  1. 按用户选的方式先试一把（抓 6 帧看亮度）
//  2. 画面是黑的就换另一种；窗口模式再黑就退到「全屏采集 + 裁剪到窗口」
//  3. 都黑则报错——通常是锁屏、受保护内容（Netflix 之类）或窗口已最小化
func (a *App) planCapture(opt StartOptions) (capturePlan, error) {
	ff := a.ffmpeg

	if opt.Mode == "window" {
		if !windowUsable(opt.Hwnd) {
			return capturePlan{}, fmt.Errorf("这个窗口已经关闭或最小化了，请重新选择")
		}
		// 先试直接抓窗口
		winArgs := []string{"-f", "gdigrab", "-framerate", "5",
			"-i", fmt.Sprintf("hwnd=0x%x", opt.Hwnd)}
		if luma, ok := probeLuma(ff, winArgs, false); !ok || luma >= blackLuma {
			return capturePlan{kind: "gdigrab-window"}, nil
		}

		// 黑屏：多半是 GPU 加速窗口，gdigrab 拿不到合成后的画面。
		// 改用 ddagrab 抓整屏再裁到窗口矩形——代价是窗口被遮挡时会录到别的窗口。
		rc, ok := windowScreenRect(opt.Hwnd)
		if !ok {
			return capturePlan{}, fmt.Errorf("这个窗口用了硬件加速，直接录制会得到黑屏，且无法读取窗口位置。请改用「全屏」录制")
		}
		crop := rc
		scr := opt.Screen
		scrArgs := []string{"-f", "lavfi", "-i", fmt.Sprintf("ddagrab=%d:framerate=5", scr)}
		if luma, ok2 := probeLuma(ff, scrArgs, true); ok2 && luma < blackLuma {
			return capturePlan{}, fmt.Errorf("屏幕采集拿到的也是黑屏：可能处于锁屏、息屏状态，或正在播放受保护内容")
		}
		return capturePlan{
			kind:   "ddagrab-crop",
			screen: scr,
			crop:   &crop,
			note:   "该窗口使用硬件加速，已改为全屏采集后裁剪（窗口被其它窗口遮挡时画面会不准）",
		}, nil
	}

	// 全屏模式直接用 ddagrab，不做预检：
	// 黑屏几乎都发生在「录窗口」场景（GPU 合成窗口抓不到），全屏采集本身很稳；
	// 而预检要额外等 1~2 秒（先建 D3D11 设备、等首帧），每次都付这个代价不值得。
	return capturePlan{kind: "ddagrab", screen: opt.Screen}, nil
}
