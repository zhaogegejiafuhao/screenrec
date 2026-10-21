package main

import (
	"strings"
	"syscall"
	"unsafe"
)

// 通过注册表读取「哪个进程正在占用麦克风」
// 路径：HKCU\Software\Microsoft\Windows\CurrentVersion\
//        CapabilityAccessManager\ConsentStore\microphone\NonPackaged\<把 \ 换成 # 的 exe 路径>
// 值：LastUsedTimeStart / LastUsedTimeStop（QWORD，FILETIME）
// 判定：Start != 0 且 (Stop == 0 或 Stop < Start) => 该进程此刻正在采集麦克风
// 这正是 Windows 任务栏"麦克风正在使用"小图标的同一份数据

var (
	advapi            = syscall.NewLazyDLL("advapi32.dll")
	pRegOpenKeyExW    = advapi.NewProc("RegOpenKeyExW")
	pRegEnumKeyExW    = advapi.NewProc("RegEnumKeyExW")
	pRegQueryValueExW = advapi.NewProc("RegQueryValueExW")
	pRegCloseKey      = advapi.NewProc("RegCloseKey")
)

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	regSZ           = 1
	regDword        = 4
	regTypeQword    = 11
	micConsentPath  = `SOFTWARE\Microsoft\Windows\CurrentVersion\CapabilityAccessManager\ConsentStore\microphone\NonPackaged`
)

func regOpen(root uintptr, path string) uintptr {
	var h uintptr
	r, _, _ := pRegOpenKeyExW.Call(root, uintptr(unsafe.Pointer(mustUTF16(path))), 0, keyRead,
		uintptr(unsafe.Pointer(&h)))
	if r != 0 {
		return 0
	}
	return h
}

func regEnumKey(h uintptr, index uint32) string {
	buf := make([]uint16, 512)
	n := uint32(len(buf))
	r, _, _ := pRegEnumKeyExW.Call(h, uintptr(index), uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&n)), 0, 0, 0, 0)
	if r != 0 {
		return ""
	}
	return syscall.UTF16ToString(buf[:n])
}

func regQword(h uintptr, name string) uint64 {
	var data uint64
	sz := uint32(8)
	var typ uint32
	r, _, _ := pRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(mustUTF16(name))), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&data)), uintptr(unsafe.Pointer(&sz)))
	if r != 0 || typ != regTypeQword {
		return 0
	}
	return data
}

func regString(h uintptr, name string) string {
	buf := make([]uint16, 1024)
	sz := uint32(len(buf) * 2)
	var typ uint32
	r, _, _ := pRegQueryValueExW.Call(h, uintptr(unsafe.Pointer(mustUTF16(name))), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&sz)))
	if r != 0 || (typ != regSZ && typ != 2) {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// MicUser 一个正在占用麦克风的进程
type MicUser struct {
	Exe  string // 完整路径
	Name string // 文件名（小写）
}

// micUsers 返回当前正在采集麦克风的进程列表（不含本程序自己拉起的 ffmpeg）
func micUsers() []MicUser {
	root := regOpen(hkeyCurrentUser, micConsentPath)
	if root == 0 {
		return nil
	}
	defer pRegCloseKey.Call(root)

	var out []MicUser
	for i := uint32(0); i < 512; i++ {
		name := regEnumKey(root, i)
		if name == "" {
			break
		}
		sub := regOpen(root, name)
		if sub == 0 {
			continue
		}
		start := regQword(sub, "LastUsedTimeStart")
		stop := regQword(sub, "LastUsedTimeStop")
		pRegCloseKey.Call(sub)
		if start == 0 {
			continue
		}
		if stop != 0 && stop > start {
			continue // 已经停止使用
		}
		exe := strings.ReplaceAll(name, "#", `\`)
		base := strings.ToLower(exe)
		if idx := strings.LastIndexAny(base, `\/`); idx >= 0 {
			base = base[idx+1:]
		}
		// 我们自己的采集进程，以及本程序，需要排除
		if base == "ffmpeg.exe" || base == "ffprobe.exe" || base == "screenrec.exe" || base == "python.exe" {
			continue
		}
		out = append(out, MicUser{Exe: exe, Name: base})
	}
	return out
}

// 已知的会议/通话类应用（文件名 => 展示名）
var meetingAppNames = map[string]string{
	"wemeetapp.exe": "腾讯会议",
	"wemeet.exe":    "腾讯会议",
	"voov.exe":      "腾讯会议",
	"dingtalk.exe":  "钉钉会议",
	"feishu.exe":    "飞书会议",
	"lark.exe":      "飞书会议",
	"wxwork.exe":    "企业微信会议",
	"wechat.exe":    "微信通话",
	"weixin.exe":    "微信通话",
	"qq.exe":        "QQ 通话",
	"zoom.exe":      "Zoom",
	"ms-teams.exe":  "Microsoft Teams",
	"teams.exe":     "Microsoft Teams",
	"discord.exe":   "Discord",
	"tencentmeeting.exe": "腾讯会议",
}

var browserExes = map[string]bool{
	"chrome.exe":   true,
	"msedge.exe":   true,
	"firefox.exe":  true,
	"brave.exe":    true,
	"opera.exe":    true,
	"360se.exe":    true,
	"360chrome.exe": true,
}
