package main

import (
	"log"
	"math"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

func timeNowMs() int64 { return time.Now().UnixMilli() }

// 悬浮条：参照豆包「录音转写」的视觉规范
//   - 浅色卡片 #F9FAFB + 1px 描边，圆角 12
//   - 左侧 7 根 2px 声波竖条（豆包 asr-animate：高 4~22、圆角 4、色 #474746）
//   - 右侧按钮组：开始 / 结束 / 忽略 + 关闭 ×
//   - 默认整体鼠标穿透，只有按钮区域接收点击（WM_NCHITTEST 判定）
//   - 纯 syscall，无 cgo

var (
	u32 = syscall.NewLazyDLL("user32.dll")
	g32 = syscall.NewLazyDLL("gdi32.dll")
	k32 = syscall.NewLazyDLL("kernel32.dll")

	pRegisterClassExW = u32.NewProc("RegisterClassExW")
	pCreateWindowExW  = u32.NewProc("CreateWindowExW")
	pDefWindowProcW   = u32.NewProc("DefWindowProcW")
	pShowWindow       = u32.NewProc("ShowWindow")
	pUpdateWindow     = u32.NewProc("UpdateWindow")
	pGetMessageW      = u32.NewProc("GetMessageW")
	pTranslateMessage = u32.NewProc("TranslateMessage")
	pDispatchMessageW = u32.NewProc("DispatchMessageW")
	pPostMessageW     = u32.NewProc("PostMessageW")
	pInvalidateRect   = u32.NewProc("InvalidateRect")
	pSetLayeredAttrs  = u32.NewProc("SetLayeredWindowAttributes")
	pGetSystemMetrics = u32.NewProc("GetSystemMetrics")
	pBeginPaint       = u32.NewProc("BeginPaint")
	pEndPaint         = u32.NewProc("EndPaint")
	pFillRect         = u32.NewProc("FillRect")
	pFrameRect        = u32.NewProc("FrameRect")
	pDrawTextW        = u32.NewProc("DrawTextW")
	pRegisterHotKey   = u32.NewProc("RegisterHotKey")
	pSetTimer         = u32.NewProc("SetTimer")
	pKillTimer        = u32.NewProc("KillTimer")
	pGetClientRect    = u32.NewProc("GetClientRect")
	pScreenToClient   = u32.NewProc("ScreenToClient")
	pSetWindowRgn     = u32.NewProc("SetWindowRgn")
	pCreateRoundRgn   = g32.NewProc("CreateRoundRectRgn")
	pGetCursorPos     = u32.NewProc("GetCursorPos")
	pSetCursor        = u32.NewProc("SetCursor")
	pLoadCursorW      = u32.NewProc("LoadCursorW")
	pTrackMouseEvent  = u32.NewProc("TrackMouseEvent")
	pSetWindowPos     = u32.NewProc("SetWindowPos")

	gSetBkMode       = g32.NewProc("SetBkMode")
	gSetTextColor    = g32.NewProc("SetTextColor")
	gCreateFontW     = g32.NewProc("CreateFontW")
	gSelectObject    = g32.NewProc("SelectObject")
	gDeleteObject    = g32.NewProc("DeleteObject")
	gCreateSolidBrsh = g32.NewProc("CreateSolidBrush")
	gCreatePen       = g32.NewProc("CreatePen")
	gRoundRect       = g32.NewProc("RoundRect")
	gMoveToEx        = g32.NewProc("MoveToEx")
	gLineTo          = g32.NewProc("LineTo")
	gEllipse         = g32.NewProc("Ellipse")

	kGetModuleHandleW = k32.NewProc("GetModuleHandleW")
)

type wndClassExW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type winPoint struct{ X, Y int32 }

type winMsg struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      winPoint
	Private uint32
}

type paintStruct struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     winRect
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type trackMouseEvent struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

const (
	wsPopup     = 0x80000000
	wsExTopmost = 0x00000008
	wsExToolWin = 0x00000080
	wsExLayered = 0x00080000
	wsExNoAct   = 0x08000000

	wmDestroy     = 0x0002
	wmPaint       = 0x000F
	wmTimer       = 0x0113
	wmSetCursor   = 0x0020
	wmNCHitTest   = 0x0084
	wmNCLButtonDblClk = 0x00A3
	wmMouseMove   = 0x0200
	wmMouseLeave  = 0x02A3
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmMoving      = 0x0216
	wmExitSizeMove = 0x0232
	wmHotkey      = 0x0312

	swShowNoActivate = 4
	swHide           = 0
	lwaAlpha         = 2

	dtCenter    = 0x00000001
	dtVCenter   = 0x00000004
	dtWordBreak = 0x00000010
	dtSingle    = 0x00000020
	dtNoPrefix  = 0x00000800
	dtCalcRect  = 0x00000400

	transparentBk = 1
	htTransparent = -1
	htClient      = 1
	htCaption     = 2
	idcHand       = 32649
	idcSizeAll    = 32646
	tmeLeave      = 0x00000002

	// gripW 左侧拖动手柄宽度
	gripW = 26
)

// 豆包配色（COLORREF = 0x00BBGGRR）
const (
	colBg        = 0x00FBFAF9 // #F9FAFB
	colBorder    = 0x00EBE6E5 // #E5E6EB
	colText      = 0x0029231F // #1F2329
	colPrimary   = 0x00FF7033 // #3370FF
	colPrimaryHv = 0x00FF8B52 // #528BFF
	colDanger    = 0x00454AF5 // #F54A45
	colDangerHv  = 0x00666BF7 // #F76B66
	colGhost     = 0x00F5F3F2 // #F2F3F5
	colGhostHv   = 0x00EDEAE8 // #E8EAED
	colBar       = 0x00464747 // #474746
	colClose     = 0x009E958F // #8F959E
	colWhite     = 0x00FFFFFF
)

const (
	kindClose = iota
	kindPrimary
	kindDanger
	kindGhost
)

// ovState 悬浮条状态
type ovState int

const (
	stIdle ovState = iota
	stRecording
	stPrompt
	stConsent
)

// barPattern 豆包 asr-animate 的柱高比例（中间高两边低）
var barPattern = []float64{0.3, 0.6, 0.9, 1.0, 0.9, 0.6, 0.3}

type ovButton struct {
	ID    string
	Label string
	Kind  int
	Rect  winRect
}

// Overlay 悬浮控制条
type Overlay struct {
	mu        sync.Mutex
	hwnd      uintptr
	text      string
	partial   string
	level     float64
	target    float64
	visible   bool
	dismissed bool
	st        ovState
	prompt    string
	hover     string
	tracking  bool
	fontMain  uintptr
	fontBtn   uintptr
	app       *App
	onAction  func(string)
	onHotkey  func()
	w, h      int32
	sw, sh    int32 // 屏幕逻辑尺寸（DPI 感知后的物理像素）
	nid       notifyIconData
	taskbarMsg uint32
}

func NewOverlay() *Overlay { return &Overlay{} }

// OnAction 注册按钮回调：start / stop / ignore / close
func (o *Overlay) OnAction(f func(string)) {
	o.mu.Lock()
	o.onAction = f
	o.mu.Unlock()
}

// Text 更新字幕内容与电平
func (o *Overlay) Text(final, partial string, level int) {
	o.mu.Lock()
	changed := o.text != final || o.partial != partial
	o.text, o.partial = final, partial
	if level < 0 {
		level = 0
	}
	if level > 100 {
		level = 100
	}
	o.target = float64(level)
	hwnd := o.hwnd
	show := !o.dismissed
	o.mu.Unlock()

	if show && hwnd != 0 {
		pShowWindow.Call(hwnd, swShowNoActivate)
	}
	if changed && hwnd != 0 {
		pInvalidateRect.Call(hwnd, 0, 0)
	}
}

func (o *Overlay) setState(st ovState, prompt string) {
	o.mu.Lock()
	changed := o.st != st || o.prompt != prompt
	o.st, o.prompt = st, prompt
	hwnd := o.hwnd
	o.mu.Unlock()
	if changed && hwnd != 0 {
		pInvalidateRect.Call(hwnd, 0, 0)
	}
}

func (o *Overlay) SetIdle()              { o.setState(stIdle, "") }
func (o *Overlay) SetRecording()         { o.setState(stRecording, "") }
func (o *Overlay) SetPrompt(text string) { o.setState(stPrompt, text) }

// ShowConsent 显示录音授权确认（自绘，避免依赖被策略屏蔽的 MessageBox）
func (o *Overlay) ShowConsent(text string) {
	o.setState(stConsent, text)
	o.SetVisible(true)
}

// ConsentShowing 当前是否正在等待授权确认
func (o *Overlay) ConsentShowing() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.st == stConsent
}

// SetVisible 显示/隐藏（显示会清掉"已关闭"状态）
func (o *Overlay) SetVisible(v bool) {
	o.mu.Lock()
	o.visible = v
	if v {
		o.dismissed = false
	}
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 {
		return
	}
	if v {
		pShowWindow.Call(hwnd, swShowNoActivate)
		pInvalidateRect.Call(hwnd, 0, 0)
	} else {
		pShowWindow.Call(hwnd, swHide)
	}
}

// Dismiss 用户点了关闭：隐藏悬浮条
func (o *Overlay) Dismiss() {
	o.mu.Lock()
	o.dismissed = true
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd != 0 {
		pShowWindow.Call(hwnd, swHide)
	}
}

func (o *Overlay) IsDismissed() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.dismissed
}

// SetInitialHidden 必须在 Start 之前调用：决定启动时是否直接隐藏
func (o *Overlay) SetInitialHidden(v bool) {
	o.mu.Lock()
	o.dismissed = v
	o.visible = !v
	o.mu.Unlock()
}

// Hwnd 返回悬浮窗句柄（调试/截图用）
func (o *Overlay) Hwnd() uint64 {
	o.mu.Lock()
	defer o.mu.Unlock()
	return uint64(o.hwnd)
}

// clampIntoView 拖出屏幕时把窗口拉回来，保证至少 120px 可见
func (o *Overlay) clampIntoView() {
	o.mu.Lock()
	hwnd, w, h, sw, sh := o.hwnd, o.w, o.h, o.sw, o.sh
	o.mu.Unlock()
	if hwnd == 0 || sw == 0 {
		return
	}
	var wr winRect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); r == 0 {
		return
	}
	x, y, changed := wr.Left, wr.Top, false
	// 整条保持在屏幕内：按钮在右侧，只留一部分的话按钮会丢掉
	if x > sw-w {
		x, changed = sw-w, true
	}
	if x < 0 {
		x, changed = 0, true
	}
	if y < 0 {
		y, changed = 0, true
	}
	if y > sh-h {
		y, changed = sh-h, true
	}
	if changed {
		o.moveTo(x, y)
	}
}

// moveTo 把窗口移到屏幕坐标 (x,y)，保持置顶且不抢焦点
func (o *Overlay) moveTo(x, y int32) {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 {
		return
	}
	const hwndTopmost = ^uintptr(0) // -1 = HWND_TOPMOST
	const swpNoSize = 0x0001
	const swpNoActivate = 0x0010
	pSetWindowPos.Call(hwnd, hwndTopmost,
		uintptr(x), uintptr(y), 0, 0, uintptr(swpNoSize|swpNoActivate))
}

// savePos 记住当前窗口位置
func (o *Overlay) savePos() {
	o.mu.Lock()
	hwnd := o.hwnd
	o.mu.Unlock()
	if hwnd == 0 || o.app == nil {
		return
	}
	var wr winRect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); r != 0 {
		o.app.set.SetOverlayPos(wr.Left, wr.Top)
	}
}

// ResetPos 把悬浮条移回默认位置（屏幕底部居中）
func (o *Overlay) ResetPos() {
	o.mu.Lock()
	w, h, sw, sh := o.w, o.h, o.sw, o.sh
	o.mu.Unlock()
	if sw == 0 {
		return
	}
	o.moveTo((sw-w)/2, sh-h-108)
}

// Pos 返回悬浮窗屏幕矩形 [x,y,w,h]（拖动后位置会变）
func (o *Overlay) Pos() []int32 {
	o.mu.Lock()
	hwnd := o.hwnd
	w, h := o.w, o.h
	o.mu.Unlock()
	if hwnd == 0 {
		return nil
	}
	var wr winRect
	if r, _, _ := procGetWindowRect.Call(hwnd, uintptr(unsafe.Pointer(&wr))); r == 0 {
		return nil
	}
	return []int32{wr.Left, wr.Top, w, h}
}

// IsVisible 是否处于显示状态（含被关闭）
func (o *Overlay) IsVisible() bool {
	o.mu.Lock()
	defer o.mu.Unlock()
	return !o.dismissed
}

// Start 在独立 OS 线程里创建窗口并跑消息循环
func (o *Overlay) Start(app *App) {
	o.app = app
	o.onHotkey = func() { app.toggleRecordByHotkey() }
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		o.run()
	}()
}

func (o *Overlay) run() {
	hInst, _, _ := kGetModuleHandleW.Call(0)
	clsName := mustUTF16("ScreenRecOverlay")

	o.fontMain, _, _ = gCreateFontW.Call(
		uintptr(^uint32(0)-17), // -18
		0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(mustUTF16("Microsoft YaHei UI"))),
	)
	o.fontBtn, _, _ = gCreateFontW.Call(
		uintptr(^uint32(0)-14), // -15
		0, 0, 0, 500, 0, 0, 0, 1, 0, 0, 5, 0,
		uintptr(unsafe.Pointer(mustUTF16("Microsoft YaHei UI"))),
	)

	wc := wndClassExW{
		CbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		LpfnWndProc:   syscall.NewCallback(o.wndProc),
		HInstance:     hInst,
		LpszClassName: clsName,
	}
	if r, _, err := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		log.Printf("悬浮窗注册失败: %v", err)
		return
	}

	sw, _, _ := pGetSystemMetrics.Call(0)
	sh, _, _ := pGetSystemMetrics.Call(1)
	w := int32(float64(sw) * 0.56)
	if w > 880 {
		w = 880
	}
	if w < 640 {
		w = 640
	}
	h := int32(84)
	x := (int32(sw) - w) / 2
	y := int32(sh) - h - 108
	// 用户拖动过就沿用上次位置（越界则回落到默认位）
	if o.app != nil {
		if px, py, ok := o.app.set.OverlayPos(); ok {
			if px >= 0 && px <= int32(sw)-w && py >= 0 && py <= int32(sh)-h {
				x, y = px, py
			}
		}
	}

	exStyle := uintptr(wsExTopmost | wsExToolWin | wsExLayered | wsExNoAct)
	hwnd, _, err := pCreateWindowExW.Call(
		exStyle,
		uintptr(unsafe.Pointer(clsName)),
		uintptr(unsafe.Pointer(mustUTF16(""))),
		uintptr(wsPopup),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		0, 0, hInst, 0,
	)
	if hwnd == 0 {
		log.Printf("悬浮窗创建失败: %v", err)
		return
	}

	o.mu.Lock()
	o.hwnd = hwnd
	o.w, o.h = w, h
	o.sw, o.sh = int32(sw), int32(sh)
	o.mu.Unlock()

	rgn, _, _ := pCreateRoundRgn.Call(0, 0, uintptr(w+1), uintptr(h+1), 24, 24)
	pSetWindowRgn.Call(hwnd, rgn, 1)

	pSetLayeredAttrs.Call(hwnd, 0, 250, lwaAlpha)
	if r, _, e := pRegisterHotKey.Call(hwnd, 1, uintptr(0x0001|0x0002), 0x20); r == 0 {
		log.Printf("热键注册失败（可能被占用）: %v", e)
	}
	pSetTimer.Call(hwnd, 1, 50, 0)
	pUpdateWindow.Call(hwnd)

	// Explorer 重启（任务栏重建）后需要重新添加图标，先注册这个广播消息
	if r, _, _ := pRegisterWindowMsg.Call(uintptr(unsafe.Pointer(mustUTF16("TaskbarCreated")))); r != 0 {
		o.mu.Lock()
		o.taskbarMsg = uint32(r)
		o.mu.Unlock()
	}
	o.initTray(hwnd)

	// 创建完成即显示（除非用户上次手动关闭过）。
	// 不能依赖 watchOverlay 的首帧：它可能在窗口创建前就跑到 idle 分支，
	// 之后不再触发 show，表现为"悬浮条永远不出现"。
	o.mu.Lock()
	hidden := o.dismissed
	o.mu.Unlock()
	if !hidden {
		pShowWindow.Call(hwnd, swShowNoActivate)
		pInvalidateRect.Call(hwnd, 0, 0)
	}

	var m winMsg
	for {
		r, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// layout 依据当前状态计算按钮矩形（每次重算，保证绘制与命中判定一致）
func (o *Overlay) layout() []ovButton {
	o.mu.Lock()
	st, W, H := o.st, o.w, o.h
	o.mu.Unlock()
	if W == 0 || H == 0 {
		return nil
	}

	type spec struct {
		id    string
		label string
		kind  int
		w     int32
	}
	var specs []spec
	switch st {
	case stRecording:
		specs = []spec{{"stop", "结束", kindDanger, 62}}
	case stPrompt:
		specs = []spec{{"ignore", "忽略", kindGhost, 56}, {"start", "开始", kindPrimary, 62}}
	case stConsent:
		specs = []spec{{"deny", "取消", kindGhost, 56}, {"consent", "同意并开始", kindPrimary, 118}}
	default:
		specs = []spec{{"start", "开始", kindPrimary, 62}}
	}

	const btnH int32 = 30
	const pad int32 = 12
	cy := H / 2

	bs := make([]ovButton, 0, len(specs)+1)
	bs = append(bs, ovButton{ID: "close", Kind: kindClose,
		Rect: winRect{Left: W - pad - 30, Top: cy - 15, Right: W - pad, Bottom: cy + 15}})
	right := W - pad - 30 - 10
	for i := len(specs) - 1; i >= 0; i-- {
		sp := specs[i]
		bs = append(bs, ovButton{ID: sp.id, Label: sp.label, Kind: sp.kind,
			Rect: winRect{Left: right - sp.w, Top: cy - btnH/2, Right: right, Bottom: cy + btnH/2}})
		right -= sp.w + 8
	}
	return bs
}

func (o *Overlay) wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case wmPaint:
		o.paint(hwnd)
		return 0
	case wmTimer:
		o.animate()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmNCHitTest:
		// WM_NCHITTEST 的坐标是屏幕坐标，必须先转成客户区
		pt := winPoint{X: int32(int16(lParam & 0xFFFF)), Y: int32(int16((lParam >> 16) & 0xFFFF))}
		pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		// 左侧手柄：交给系统做非客户区拖动（HTCAPTION）
		if o.inGrip(pt.X, pt.Y) {
			return htCaption
		}
		for _, b := range o.layout() {
			if hitRect(b.Rect, pt.X, pt.Y) {
				return htClient
			}
		}
		return ^uintptr(0) // HTTRANSPARENT(-1)：其余区域鼠标穿透
	case wmMouseMove:
		mx := int32(int16(lParam & 0xFFFF))
		my := int32(int16((lParam >> 16) & 0xFFFF))
		hover := ""
		for _, b := range o.layout() {
			if hitRect(b.Rect, mx, my) {
				hover = b.ID
				break
			}
		}
		o.mu.Lock()
		changed := o.hover != hover
		o.hover = hover
		needTrack := hover != "" && !o.tracking
		if needTrack {
			o.tracking = true
		}
		o.mu.Unlock()
		if needTrack {
			tme := trackMouseEvent{CbSize: uint32(unsafe.Sizeof(trackMouseEvent{})), DwFlags: tmeLeave, HwndTrack: hwnd}
			pTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
		}
		if changed {
			pInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0
	case wmMouseLeave:
		o.mu.Lock()
		o.hover = ""
		o.tracking = false
		o.mu.Unlock()
		pInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case wmSetCursor:
		// 非客户区（手柄）不会走 WM_MOUSEMOVE，直接按当前光标位置判断
		var cp winPoint
		pGetCursorPos.Call(uintptr(unsafe.Pointer(&cp)))
		pScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&cp)))
		if o.inGrip(cp.X, cp.Y) {
			cur, _, _ := pLoadCursorW.Call(0, idcSizeAll)
			pSetCursor.Call(cur)
			return 1
		}
		o.mu.Lock()
		h := o.hover
		o.mu.Unlock()
		if h != "" {
			cur, _, _ := pLoadCursorW.Call(0, idcHand)
			pSetCursor.Call(cur)
			return 1
		}
		break
	case wmLButtonDown:
		return 0
	case wmLButtonUp:
		mx := int32(int16(lParam & 0xFFFF))
		my := int32(int16((lParam >> 16) & 0xFFFF))
		for _, b := range o.layout() {
			if hitRect(b.Rect, mx, my) {
				o.mu.Lock()
				fn := o.onAction
				o.mu.Unlock()
				if b.ID == "close" {
					o.Dismiss()
				}
				if fn != nil {
					go fn(b.ID)
				}
				return 0
			}
		}
		return 0
	case wmHotkey:
		if o.onHotkey != nil {
			go o.onHotkey()
		}
		return 0
	case wmNCLButtonDblClk:
		return 0 // 双击手柄不做最大化
	case wmExitSizeMove:
		// 拖动结束：夹到屏幕内并记住位置
		o.clampIntoView()
		o.savePos()
		return 0
	case wmDestroy:
		pKillTimer.Call(hwnd, 1)
		pPostMessageW.Call(0, 0x0012, 0, 0)
		return 0
	}
	if msg == wmTray {
		o.onTray(lParam)
		return 0
	}
	if o.taskbarMsg != 0 && msg == o.taskbarMsg {
		o.initTray(hwnd) // 任务栏重建后补上图标
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func hitRect(r winRect, x, y int32) bool {
	return x >= r.Left && x < r.Right && y >= r.Top && y < r.Bottom
}

// inGrip 是否落在左侧拖动手柄上（含状态指示区，方便抓取）
func (o *Overlay) inGrip(x, y int32) bool {
	o.mu.Lock()
	h := o.h
	o.mu.Unlock()
	return x >= 0 && x < gripW && y >= 0 && y < h
}

// animate 平滑电平，让声波跳动自然
func (o *Overlay) animate() {
	o.mu.Lock()
	o.level += (o.target - o.level) * 0.35
	if o.level < 0.5 {
		o.level = 0
	}
	o.mu.Unlock()
}

func (o *Overlay) paint(hwnd uintptr) {
	var ps paintStruct
	hdc, _, _ := pBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
	defer pEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))

	var rc winRect
	pGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
	W, H := rc.Right, rc.Bottom

	bg, _, _ := gCreateSolidBrsh.Call(colBg)
	pFillRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bg)
	gDeleteObject.Call(bg)
	bd, _, _ := gCreateSolidBrsh.Call(colBorder)
	pFrameRect.Call(hdc, uintptr(unsafe.Pointer(&rc)), bd)
	gDeleteObject.Call(bd)

	o.mu.Lock()
	text, partial, level := o.text, o.partial, o.level
	st, prompt, hover := o.st, o.prompt, o.hover
	o.mu.Unlock()

	// ---- 左侧拖动手柄：2×3 个圆点 ----
	{
		c := uint32(0x00D4CDC9) // #C9CDD4
		brush, _, _ := gCreateSolidBrsh.Call(uintptr(c))
		pen, _, _ := gCreatePen.Call(0, 1, uintptr(c))
		oldPen, _, _ := gSelectObject.Call(hdc, pen)
		oldBr, _, _ := gSelectObject.Call(hdc, brush)
		cy := H / 2
		for row := int32(-1); row <= 1; row++ {
			for col := int32(0); col <= 1; col++ {
				x := 6 + col*5
				y := cy + row*5
				gEllipse.Call(hdc, uintptr(x), uintptr(y-1), uintptr(x+3), uintptr(y+2))
			}
		}
		gSelectObject.Call(hdc, oldPen)
		gSelectObject.Call(hdc, oldBr)
		gDeleteObject.Call(pen)
		gDeleteObject.Call(brush)
	}

	// ---- 状态指示 ----
	barX := int32(32)
	if st == stIdle || level <= 1.5 {
		// 空闲：一个 8px 圆点（提醒态用橙色，空闲用豆包蓝）
		c := uint32(colPrimary)
		if st == stPrompt || st == stConsent {
			c = 0x000094F5 // #F59400 提醒橙
		}
		pen, _, _ := gCreatePen.Call(0, 1, uintptr(c))
		brush, _, _ := gCreateSolidBrsh.Call(uintptr(c))
		oldPen, _, _ := gSelectObject.Call(hdc, pen)
		oldBr, _, _ := gSelectObject.Call(hdc, brush)
		cy := H / 2
		gEllipse.Call(hdc, 32, uintptr(cy-4), 40, uintptr(cy+4))
		gSelectObject.Call(hdc, oldPen)
		gSelectObject.Call(hdc, oldBr)
		gDeleteObject.Call(pen)
		gDeleteObject.Call(brush)
		barX = 52
	} else {
		var phase float64
		if level > 2 {
			phase = float64(timeNowMs()%360) / 57.2957
		}
		for i, p := range barPattern {
			bh := 22 * (level / 100.0) * p
			if level > 2 {
				bh *= 1 + 0.18*math.Sin(phase+float64(i)*0.9)
			}
			if bh < 4 {
				bh = 4
			}
			if bh > 22 {
				bh = 22
			}
			top := H/2 - int32(bh/2)
			pen, _, _ := gCreatePen.Call(0, 1, colBar)
			brush, _, _ := gCreateSolidBrsh.Call(colBar)
			oldPen, _, _ := gSelectObject.Call(hdc, pen)
			oldBr, _, _ := gSelectObject.Call(hdc, brush)
			gRoundRect.Call(hdc, uintptr(barX), uintptr(top), uintptr(barX+2), uintptr(top+int32(bh)), 4, 4)
			gSelectObject.Call(hdc, oldPen)
			gSelectObject.Call(hdc, oldBr)
			gDeleteObject.Call(pen)
			gDeleteObject.Call(brush)
			barX += 4
		}
	}

	// ---- 按钮 ----
	bs := o.layout()
	textRight := W - 44
	for _, b := range bs {
		if b.Rect.Left-12 < textRight {
			textRight = b.Rect.Left - 12
		}
	}
	for _, b := range bs {
		o.drawButton(hdc, b, b.ID == hover)
	}

	// ---- 字幕文本 ----
	show := ""
	switch st {
	case stPrompt, stConsent:
		show = prompt
	default:
		show = text + partial
	}
	if show == "" {
		if st == stRecording {
			show = "正在听…"
		} else {
			show = "点「开始」或按 Ctrl+Alt+Space 说话"
		}
	}
	gSetBkMode.Call(hdc, transparentBk)
	if o.fontMain != 0 {
		gSelectObject.Call(hdc, o.fontMain)
	}
	gSetTextColor.Call(hdc, colText)

	tr := winRect{Left: barX + 14, Top: 8, Right: textRight, Bottom: H - 8}
	if tr.Right < tr.Left+60 {
		tr.Right = tr.Left + 60
	}
	measure := tr
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(mustUTF16(show))), uintptr(^uint32(0)),
		uintptr(unsafe.Pointer(&measure)), uintptr(dtCalcRect|dtWordBreak|dtNoPrefix))
	th := measure.Bottom - measure.Top
	top := (H - th) / 2
	if top < 6 {
		top = 6
	}
	tr.Top = top
	tr.Bottom = top + th

	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(mustUTF16(show))), uintptr(^uint32(0)),
		uintptr(unsafe.Pointer(&tr)), uintptr(dtCenter|dtWordBreak|dtNoPrefix))
}

func (o *Overlay) drawButton(hdc uintptr, b ovButton, hover bool) {
	if b.Kind == kindClose {
		cx := (b.Rect.Left + b.Rect.Right) / 2
		cy := (b.Rect.Top + b.Rect.Bottom) / 2
		c := uint32(colClose)
		if hover {
			c = colText
		}
		pen, _, _ := gCreatePen.Call(0, 1, uintptr(c))
		old, _, _ := gSelectObject.Call(hdc, pen)
		const r = 6
		gMoveToEx.Call(hdc, uintptr(cx-r), uintptr(cy-r), 0)
		gLineTo.Call(hdc, uintptr(cx+r), uintptr(cy+r))
		gMoveToEx.Call(hdc, uintptr(cx+r), uintptr(cy-r), 0)
		gLineTo.Call(hdc, uintptr(cx-r), uintptr(cy+r))
		gSelectObject.Call(hdc, old)
		gDeleteObject.Call(pen)
		return
	}

	bg, fg := uint32(colGhost), uint32(colText)
	switch b.Kind {
	case kindPrimary:
		bg, fg = colPrimary, colWhite
		if hover {
			bg = colPrimaryHv
		}
	case kindDanger:
		bg, fg = colDanger, colWhite
		if hover {
			bg = colDangerHv
		}
	default:
		if hover {
			bg = colGhostHv
		}
	}

	pen, _, _ := gCreatePen.Call(0, 1, uintptr(bg))
	brush, _, _ := gCreateSolidBrsh.Call(uintptr(bg))
	oldPen, _, _ := gSelectObject.Call(hdc, pen)
	oldBr, _, _ := gSelectObject.Call(hdc, brush)
	gRoundRect.Call(hdc, uintptr(b.Rect.Left), uintptr(b.Rect.Top),
		uintptr(b.Rect.Right), uintptr(b.Rect.Bottom), 16, 16)
	gSelectObject.Call(hdc, oldPen)
	gSelectObject.Call(hdc, oldBr)
	gDeleteObject.Call(pen)
	gDeleteObject.Call(brush)

	gSetBkMode.Call(hdc, transparentBk)
	gSetTextColor.Call(hdc, uintptr(fg))
	if o.fontBtn != 0 {
		gSelectObject.Call(hdc, o.fontBtn)
	}
	r := b.Rect
	pDrawTextW.Call(hdc, uintptr(unsafe.Pointer(mustUTF16(b.Label))), uintptr(^uint32(0)),
		uintptr(unsafe.Pointer(&r)), uintptr(dtCenter|dtVCenter|dtSingle|dtNoPrefix))
}

func mustUTF16(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}
