package main

// 系统托盘：右键菜单提供「打开界面 / 开始录音 / 显示悬浮条 / 复位 / 退出」。
// 纯 syscall 实现，不引入任何依赖。

import (
	"log"
	"syscall"
	"unsafe"
)

var (
	shell32              = syscall.NewLazyDLL("shell32.dll")
	pShellNotifyIconW    = shell32.NewProc("Shell_NotifyIconW")
	pLoadIconW           = u32.NewProc("LoadIconW")
	pCreatePopupMenu     = u32.NewProc("CreatePopupMenu")
	pAppendMenuW         = u32.NewProc("AppendMenuW")
	pTrackPopupMenu      = u32.NewProc("TrackPopupMenu")
	pDestroyMenu         = u32.NewProc("DestroyMenu")
	pSetForegroundWindow = u32.NewProc("SetForegroundWindow")
	pRegisterWindowMsg   = u32.NewProc("RegisterWindowMessageW")
)

const (
	nimAdd    = 0
	nimDelete = 2

	nifMessage = 0x1
	nifIcon    = 0x2
	nifTip     = 0x4

	wmTray     = 0x8001 // WM_APP + 1
	mfString   = 0x0000
	mfSep      = 0x0800
	tpmRight   = 0x0002
	tpmReturn  = 0x0100
	idiAppIcon = 32512

	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmContextMenu   = 0x007B

	cmdOpen    = 1
	cmdRecord  = 2
	cmdToggle  = 3
	cmdReset   = 4
	cmdQuit    = 9
	keySetVal  = 0x0002
)

// notifyIconData NOTIFYICONDATAW 的完整布局（64 位对齐由 Go 自动处理）
type notifyIconData struct {
	CbSize           uint32
	HWnd             uintptr
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            uintptr
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     uintptr
}

// initTray 添加托盘图标（Explorer 重启后会再次调用）
func (o *Overlay) initTray(hwnd uintptr) {
	nid := notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(notifyIconData{})),
		HWnd:             hwnd,
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: wmTray,
	}
	icon, _, _ := pLoadIconW.Call(0, idiAppIcon)
	nid.HIcon = icon
	// copy 会自动按目标长度截断，别再手动切片（短字符串会越界 panic）
	copy(nid.SzTip[:], syscall.StringToUTF16("录屏工坊 · Ctrl+Alt+Space 开始/停止"))

	o.mu.Lock()
	o.nid = nid
	o.mu.Unlock()

	if r, _, err := pShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&nid))); r == 0 {
		log.Printf("[tray] 添加托盘图标失败: %v", err)
	} else {
		log.Printf("[tray] 托盘图标已添加")
	}
}

// onTray 处理托盘图标的鼠标事件
func (o *Overlay) onTray(lParam uintptr) {
	switch uint32(lParam & 0xFFFF) {
	case wmRButtonUp, wmContextMenu:
		o.showTrayMenu()
	case wmLButtonDblClk:
		if o.app != nil && o.app.url != "" {
			go openBrowser(o.app.url)
		}
	}
}

func (o *Overlay) trayRecording() bool {
	if o.app == nil {
		return false
	}
	rec := o.app.recorder.Current()
	return rec != nil && rec.Status == "recording"
}

func (o *Overlay) showTrayMenu() {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 {
		return
	}

	h, _, _ := pCreatePopupMenu.Call()
	add := func(id uintptr, text string) {
		pAppendMenuW.Call(h, mfString, id, uintptr(unsafe.Pointer(mustUTF16(text))))
	}
	sep := func() { pAppendMenuW.Call(h, mfSep, 0, 0) }

	add(cmdOpen, "打开界面")
	if o.trayRecording() {
		add(cmdRecord, "结束录音    Ctrl+Alt+Space")
	} else {
		add(cmdRecord, "开始录音    Ctrl+Alt+Space")
	}
	sep()
	if o.IsDismissed() {
		add(cmdToggle, "显示悬浮字幕")
	} else {
		add(cmdToggle, "隐藏悬浮字幕")
	}
	add(cmdReset, "复位悬浮条位置")
	sep()
	add(cmdQuit, "退出")

	var pt winPoint
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// 菜单弹出前必须置前，否则点空白处不会消失
	pSetForegroundWindow.Call(hwnd)
	cmd, _, _ := pTrackPopupMenu.Call(h, tpmRight|tpmReturn,
		uintptr(pt.X), uintptr(pt.Y), 0, hwnd, 0)
	pDestroyMenu.Call(h)
	pPostMessageW.Call(hwnd, 0, 0, 0) // WM_NULL，让菜单正确关闭

	if cmd != 0 {
		o.trayCommand(int(cmd))
	}
}

func (o *Overlay) trayCommand(cmd int) {
	if o.app == nil {
		return
	}
	switch cmd {
	case cmdOpen:
		go openBrowser(o.app.url)
	case cmdRecord:
		go o.app.toggleRecordByHotkey()
	case cmdToggle:
		if o.IsDismissed() {
			o.Show()
		} else {
			o.Dismiss()
		}
	case cmdReset:
		o.ResetPos()
	case cmdQuit:
		o.app.RequestQuit()
	}
}

// Show 重新显示悬浮条（清掉「用户关掉了」的状态）
func (o *Overlay) Show() {
	o.mu.Lock()
	o.dismissed = false
	o.mu.Unlock()
	o.SetVisible(true)
	if o.app != nil {
		o.app.set.SetHidden(false)
	}
}

// removeTray 退出前移除图标，避免托盘残留幽灵图标
func (o *Overlay) removeTray() {
	o.mu.Lock()
	nid := o.nid
	o.mu.Unlock()
	if nid.HWnd != 0 {
		pShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&nid)))
	}
}
