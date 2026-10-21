// 测试辅助：移动光标并单击；或按 "key <VK码>" 发送一次按键
package main

import (
	"fmt"
	"os"
	"strconv"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32        = syscall.NewLazyDLL("user32.dll")
	pSetCursorPos = user32.NewProc("SetCursorPos")
	pGetCursorPos = user32.NewProc("GetCursorPos")
	pMouseEvent   = user32.NewProc("mouse_event")
	pGetSysMetric = user32.NewProc("GetSystemMetrics")
	pSetDpiAware  = user32.NewProc("SetProcessDpiAwarenessContext")
	pKeybdEvent   = user32.NewProc("keybd_event")
)

type point struct{ X, Y int32 }

func main() {
	// 必须声明 DPI 感知，否则坐标会被系统按缩放比虚拟化（实测 1920x1080 被报成 1536x864）
	pSetDpiAware.Call(uintptr(^uintptr(3)))

	// key 模式：向当前前台窗口发送一次按键（用于确认对话框）
	if len(os.Args) >= 3 && os.Args[1] == "key" {
		vk, _ := strconv.Atoi(os.Args[2])
		pKeybdEvent.Call(uintptr(vk), 0, 0, 0)
		time.Sleep(60 * time.Millisecond)
		pKeybdEvent.Call(uintptr(vk), 0, 2, 0) // KEYEVENTF_KEYUP
		fmt.Printf("key 0x%x sent\n", vk)
		return
	}

	// drag 模式：从 (x1,y1) 拖到 (x2,y2)
	if len(os.Args) >= 6 && os.Args[1] == "drag" {
		x1, _ := strconv.Atoi(os.Args[2])
		y1, _ := strconv.Atoi(os.Args[3])
		x2, _ := strconv.Atoi(os.Args[4])
		y2, _ := strconv.Atoi(os.Args[5])
		pSetCursorPos.Call(uintptr(x1), uintptr(y1))
		time.Sleep(300 * time.Millisecond)
		pMouseEvent.Call(0x0002, 0, 0, 0, 0) // left down
		time.Sleep(200 * time.Millisecond)
		const steps = 20
		for i := 1; i <= steps; i++ {
			x := x1 + (x2-x1)*i/steps
			y := y1 + (y2-y1)*i/steps
			pSetCursorPos.Call(uintptr(x), uintptr(y))
			time.Sleep(25 * time.Millisecond)
		}
		time.Sleep(200 * time.Millisecond)
		pMouseEvent.Call(0x0004, 0, 0, 0, 0) // left up
		fmt.Printf("dragged %d,%d -> %d,%d\n", x1, y1, x2, y2)
		return
	}

	// move 模式：只移动光标不点击
	if len(os.Args) >= 4 && os.Args[1] == "move" {
		x, _ := strconv.Atoi(os.Args[2])
		y, _ := strconv.Atoi(os.Args[3])
		pSetCursorPos.Call(uintptr(x), uintptr(y))
		fmt.Printf("moved %d,%d\n", x, y)
		return
	}

	// wheel 模式：滚轮滚动（正数向上）
	if len(os.Args) >= 3 && os.Args[1] == "wheel" {
		d, _ := strconv.Atoi(os.Args[2])
		pMouseEvent.Call(0x0800, 0, 0, uintptr(int32(d)*120), 0)
		fmt.Printf("wheel %d\n", d)
		return
	}

	if len(os.Args) < 3 {
		fmt.Println("usage: click <x> <y> | click key <vk> | click wheel <delta>")
		return
	}

	x, _ := strconv.Atoi(os.Args[1])
	y, _ := strconv.Atoi(os.Args[2])

	w, _, _ := pGetSysMetric.Call(0)
	h, _, _ := pGetSysMetric.Call(1)
	fmt.Printf("screen %dx%d\n", w, h)

	pSetCursorPos.Call(uintptr(x), uintptr(y))
	time.Sleep(200 * time.Millisecond)
	var p point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&p)))
	fmt.Printf("cursor now %d,%d\n", p.X, p.Y)

	const leftDown, leftUp = 0x0002, 0x0004
	pMouseEvent.Call(leftDown, 0, 0, 0, 0)
	time.Sleep(80 * time.Millisecond)
	pMouseEvent.Call(leftUp, 0, 0, 0, 0)
	fmt.Println("clicked")
}
