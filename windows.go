package main

import (
	"os"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

// WindowInfo 一个可录制的顶层窗口
type WindowInfo struct {
	Hwnd   uint64 `json:"hwnd"`
	Title  string `json:"title"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	PID    uint32 `json:"pid"`
}

var (
	userWin                = syscall.NewLazyDLL("user32.dll")
	procEnumWindows        = userWin.NewProc("EnumWindows")
	procGetWindowTextW     = userWin.NewProc("GetWindowTextW")
	procGetWindowTextLenW  = userWin.NewProc("GetWindowTextLengthW")
	procIsWindowVisible    = userWin.NewProc("IsWindowVisible")
	procIsIconic           = userWin.NewProc("IsIconic")
	procGetWindowRect      = userWin.NewProc("GetWindowRect")
	procGetWindowThreadPID = userWin.NewProc("GetWindowThreadProcessId")
	procGetWindowLongW     = userWin.NewProc("GetWindowLongW")

	winMu    sync.Mutex
	winEnum  sync.Mutex
	winTemp  []WindowInfo
	winSelf  uint32
	enumOnce sync.Once
	enumCb   uintptr
)

type winRect struct {
	Left, Top, Right, Bottom int32
}

// windowScreenRect 取窗口在屏幕上的矩形（含标题栏边框）。
// 用于「窗口录不了时改用全屏采集 + 裁剪」的降级路径。
func windowScreenRect(hwnd uint64) (winRect, bool) {
	var rc winRect
	if hwnd == 0 {
		return rc, false
	}
	if r, _, _ := procGetWindowRect.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rc))); r == 0 {
		return rc, false
	}
	if rc.Right <= rc.Left || rc.Bottom <= rc.Top {
		return rc, false
	}
	return rc, true
}

// windowUsable 窗口是否还活着、没最小化
func windowUsable(hwnd uint64) bool {
	if hwnd == 0 {
		return false
	}
	if v, _, _ := procIsWindowVisible.Call(uintptr(hwnd)); v == 0 {
		return false
	}
	if v, _, _ := procIsIconic.Call(uintptr(hwnd)); v != 0 {
		return false
	}
	return true
}

const (
	gwlExStyle     = 0xFFFFFFEC // -20
	wsExToolWindow = 0x00000080
	wsExNoActivate = 0x08000000
)

// enumProc 是固定的窗口枚举回调。
// 注意：syscall.NewCallback 每次调用都会占用一个回调槽（上限 2000），
// 必须在 init 时创建一次并复用，否则会议轮询这种高频调用会在几小时后崩溃。
func enumProc(hwnd uintptr, _ uintptr) uintptr {
	if v, _, _ := procIsWindowVisible.Call(hwnd); v == 0 {
		return 1
	}
	ex, _, _ := procGetWindowLongW.Call(hwnd, uintptr(gwlExStyle))
	if ex&wsExToolWindow != 0 && ex&wsExNoActivate == 0 {
		return 1
	}
	n, _, _ := procGetWindowTextLenW.Call(hwnd)
	if n == 0 || n > 512 {
		return 1
	}
	buf := make([]uint16, n+2)
	if r, _, _ := procGetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf))); r == 0 {
		return 1
	}
	title := strings.TrimSpace(syscall.UTF16ToString(buf))
	if title == "" {
		return 1
	}
	var rc winRect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&rc))); r == 0 {
		return 1
	}
	w, h := int(rc.Right-rc.Left), int(rc.Bottom-rc.Top)
	if w < 100 || h < 80 {
		return 1
	}
	var pid uint32
	procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	if pid == winSelf {
		return 1
	}
	winMu.Lock()
	winTemp = append(winTemp, WindowInfo{
		Hwnd: uint64(hwnd), Title: title, Width: w, Height: h, PID: pid,
	})
	winMu.Unlock()
	return 1
}

// listWindows 枚举可见的顶层窗口（排除工具窗口、本程序自身、过小的窗口）
func listWindows() []WindowInfo {
	enumOnce.Do(func() {
		winSelf = uint32(os.Getpid())
		enumCb = syscall.NewCallback(enumProc)
	})

	winEnum.Lock()
	defer winEnum.Unlock()

	winMu.Lock()
	winTemp = winTemp[:0]
	winMu.Unlock()

	procEnumWindows.Call(enumCb, 0)

	winMu.Lock()
	out := append([]WindowInfo{}, winTemp...)
	winMu.Unlock()
	sort.Slice(out, func(i, j int) bool {
		if out[i].Title == out[j].Title {
			return out[i].Hwnd < out[j].Hwnd
		}
		return out[i].Title < out[j].Title
	})
	return out
}
