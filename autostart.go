package main

// 开机自启：写 HKCU\Software\Microsoft\Windows\CurrentVersion\Run。
// 用 HKCU 而不是 HKLM，不需要管理员权限。

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	pRegCreateKeyExW = advapi.NewProc("RegCreateKeyExW")
	pRegSetValueExW  = advapi.NewProc("RegSetValueExW")
	pRegDeleteValueW = advapi.NewProc("RegDeleteValueW")
)

const (
	runKeyPath    = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName  = "ScreenRecStudio"
	autoStartArgs = ` -open`
)

// setAutoStart 打开/关闭开机自启
func setAutoStart(on bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}

	var h uintptr
	r, _, _ := pRegCreateKeyExW.Call(hkeyCurrentUser,
		uintptr(unsafe.Pointer(mustUTF16(runKeyPath))),
		0, 0, 0, keySetVal, 0,
		uintptr(unsafe.Pointer(&h)), 0)
	if r != 0 {
		return fmt.Errorf("打开注册表失败（错误码 %d）", r)
	}
	defer pRegCloseKey.Call(h)

	if !on {
		pRegDeleteValueW.Call(h, uintptr(unsafe.Pointer(mustUTF16(runValueName))))
		return nil
	}

	val, _ := syscall.UTF16FromString(`"` + exe + `"` + autoStartArgs)
	r, _, _ = pRegSetValueExW.Call(h,
		uintptr(unsafe.Pointer(mustUTF16(runValueName))),
		0, regSZ,
		uintptr(unsafe.Pointer(&val[0])),
		uintptr(len(val)*2))
	if r != 0 {
		return fmt.Errorf("写入启动项失败（错误码 %d）", r)
	}
	return nil
}

// autoStartEnabled 读注册表判断当前是否已开启
func autoStartEnabled() bool {
	h := regOpen(hkeyCurrentUser, runKeyPath)
	if h == 0 {
		return false
	}
	defer pRegCloseKey.Call(h)
	buf := make([]uint16, 512)
	n := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ := pRegQueryValueExW.Call(h,
		uintptr(unsafe.Pointer(mustUTF16(runValueName))), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)))
	return r == 0 && typ == regSZ
}
