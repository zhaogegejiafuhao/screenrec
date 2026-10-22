package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

//go:embed web/index.html web/app.js web/style.css
var webFS embed.FS

func serveWeb(w http.ResponseWriter, name, ctype string) {
	b, err := webFS.ReadFile("web/" + name)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Write(b)
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (sw *statusWriter) WriteHeader(code int) {
	sw.status = code
	sw.ResponseWriter.WriteHeader(code)
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		sw := &statusWriter{ResponseWriter: w, status: 200}
		next.ServeHTTP(sw, r)
		if r.URL.Path != "/" && !hasPrefix(r.URL.Path, "/app.js") && !hasPrefix(r.URL.Path, "/style.css") {
			log.Printf("%s %s -> %d (%s)", r.Method, r.URL.Path, sw.status, time.Since(start).Round(time.Millisecond))
		}
	})
}

func hasPrefix(s, p string) bool {
	return len(s) >= len(p) && s[:len(p)] == p
}

func main() {	setupDPI()
	addr := flag.String("addr", "127.0.0.1", "监听地址")
	port := flag.Int("port", 8790, "监听端口")
	dataDir := flag.String("data", "data", "数据目录（录制文件与转写结果）")
	assets := flag.String("assets", ".", "资源目录（asr.py/models/pylibs 所在目录）")
	open := flag.Bool("open", true, "启动后自动打开浏览器")
	flag.Parse()

	// 相对路径按 exe 所在目录解析：开机自启/开始菜单启动时工作目录可能是 System32，
	// 用相对路径会把 data 建到系统目录里。
	if exe, err := os.Executable(); err == nil {
		exeDir := filepath.Dir(exe)
		if !filepath.IsAbs(*dataDir) {
			*dataDir = filepath.Join(exeDir, *dataDir)
		}
		if !filepath.IsAbs(*assets) {
			*assets = filepath.Join(exeDir, *assets)
		}
	}

	// 日志同时写文件：GUI 版（windowsgui）没有控制台，出问题只能翻日志。
	// 不能用 io.MultiWriter——stderr 句柄无效时它会直接返回，文件那条也写不进去。
	if err := os.MkdirAll(*dataDir, 0755); err == nil {
		if f, err := os.OpenFile(filepath.Join(*dataDir, "app.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644); err == nil {
			log.SetOutput(teeWriter{file: f})
		}
	}

	url := fmt.Sprintf("http://%s:%d", *addr, *port)

	// 单实例互斥：同一台机器只允许一个录制进程，
	// 第二个实例会把已运行实例的悬浮条唤到最前，然后自己退出。
	if !acquireSingleInstance(singleInstanceName) {
		notifyExisting(url)
		openBrowser(url)
		log.Printf("已有实例在运行，已为你打开 %s", url)
		return
	}

	app, err := NewApp(*dataDir, *assets)
	if err != nil {
		log.Fatalf("初始化失败: %v", err)
	}
	app.url = url

	// 上次开着实时双语的话，启动时就后台加载翻译模型
	if on, dir := app.set.LiveTransConfig(); on {
		go app.warmLiveTranslate(dir)
	}

	// 预热识别进程，避免第一段实时字幕卡顿
	go func() {
		if app.python != "" {
			if err := app.asr.Start(); err != nil {
				log.Printf("识别引擎预热失败: %v", err)
			} else {
				log.Printf("识别引擎就绪")
			}
		}
	}()
	defer app.asr.Close()
	defer app.stream.Close()

	app.overlay.SetInitialHidden(app.set.Hidden())
	app.overlay.Start(app)
	go watchOverlay(app)

	stopMeeting := make(chan struct{})
	go app.meeting.Loop(stopMeeting)
	defer close(stopMeeting)

	stopCalendar := make(chan struct{})
	go app.calendar.Loop(stopCalendar)
	defer close(stopCalendar)

	srv := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", *addr, *port),
		Handler: app.routes(),
	}
	if *open {
		go openBrowser(url)
	}

	idle := make(chan struct{})
	go func() {
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
		select {
		case <-sig:
		case <-app.quit:
		}
		app.overlay.removeTray()
		app.recorder.Stop()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(ctx)
		close(idle)
	}()

	log.Printf("录屏工坊已启动: %s", url)
	log.Printf("ffmpeg: %s", app.ffmpeg)
	log.Printf("识别引擎: %s", app.python)
	log.Printf("数据目录: %s", filepath.Join(app.dataDir, "recordings"))
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatalf("服务异常退出: %v", err)
	}
	<-idle
	log.Println("已退出")
}

// watchOverlay 把录制状态、实时文本、会议提醒同步到悬浮条
func watchOverlay(app *App) {
	ticker := time.NewTicker(150 * time.Millisecond)
	defer ticker.Stop()
	prev := ""
	for range ticker.C {
		rec := app.recorder.Current()

		if rec != nil && rec.Status == "recording" {
			if prev != "rec" {
				app.overlay.SetRecording()
				app.overlay.SetVisible(true)
				prev = "rec"
			}
			if !app.overlay.IsDismissed() {
				app.mu.RLock()
				text, partial, level, trans := rec.LiveText, rec.LivePartial, rec.LiveLevel, rec.LiveTrans
				if rec.LiveSysLevel > level {
					level = rec.LiveSysLevel // 只看视频不说话时，电平条跟着电脑声音跳
				}
				app.mu.RUnlock()
				app.overlay.SetTrans(trans)
				app.overlay.Text(tailRunes(text, 90), partial, level)
			}
			continue
		}

		// 授权确认未完成时保持该状态，不被空闲态覆盖
		if app.overlay.ConsentShowing() {
			if prev != "consent" {
				app.overlay.SetVisible(true)
				prev = "consent"
			}
			continue
		}

		// 日历提醒优先于会议提醒：它是提前约好的，比事后检测更可信
		if p := app.calendar.Pending(); p != "" {
			key := "cal:" + p
			if prev != key {
				app.overlay.SetPrompt(p)
				app.overlay.SetVisible(true)
				prev = key
			}
			continue
		}

		// 会议提醒优先于空闲态
		if p := app.meeting.Pending(); p != "" {
			key := "prompt:" + p
			if prev != key {
				app.overlay.SetPrompt(p)
				app.overlay.SetVisible(true)
				prev = key
			}
			continue
		}

		if prev != "idle" {
			app.overlay.SetIdle()
			app.overlay.SetTrans("")
			app.overlay.Text("", "", 0)
			if !app.overlay.IsDismissed() {
				app.overlay.SetVisible(true)
			}
			prev = "idle"
		}
	}
}

// tailRunes 只保留末尾若干字符，避免字幕条被长文撑爆
func tailRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}

// ---- 单实例互斥 ----

const singleInstanceName = "Local\\ScreenRecStudio_SingleInstance_v1"

// acquireSingleInstance 用命名互斥量保证同机只有一个实例。
// 返回 false 表示已有实例在运行。
func acquireSingleInstance(name string) bool {
	h, _, err := k32.NewProc("CreateMutexW").Call(0, 0, uintptr(unsafe.Pointer(mustUTF16(name))))
	if h == 0 {
		return true // 创建失败不阻塞启动
	}
	if e, ok := err.(syscall.Errno); ok && e == 183 { // ERROR_ALREADY_EXISTS
		return false
	}
	return true
}

// notifyExisting 唤醒已运行实例的悬浮条
func notifyExisting(base string) {
	body := strings.NewReader(`{"show":true}`)
	req, err := http.NewRequest("POST", base+"/api/overlay", body)
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	c := &http.Client{Timeout: 1500 * time.Millisecond}
	if resp, err := c.Do(req); err == nil {
		resp.Body.Close()
	}
}

// setupDPI 声明进程 DPI 感知，否则窗口坐标/尺寸会被系统缩放（
// 这也会让 gdigrab 抓到的窗口尺寸变成 125% 的奇数像素）
func setupDPI() {
	if r, _, _ := u32.NewProc("SetProcessDpiAwarenessContext").Call(uintptr(^uintptr(3))); r != 0 {
		return
	}
	u32.NewProc("SetProcessDPIAware").Call()
}

func openBrowser(url string) {
	var cmd string
	var args []string
	switch runtime.GOOS {
	case "windows":
		cmd, args = "cmd", []string{"/c", "start", url}
	case "darwin":
		cmd, args = "open", []string{url}
	default:
		cmd, args = "xdg-open", []string{url}
	}
	exec.Command(cmd, args...).Run()
}

// teeWriter 日志双写：stderr 写失败（GUI 版没有控制台）不影响文件写入
type teeWriter struct{ file *os.File }

func (t teeWriter) Write(p []byte) (int, error) {
	_, _ = os.Stderr.Write(p)
	return t.file.Write(p)
}
