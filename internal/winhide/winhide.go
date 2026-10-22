// Package winhide 提供一个统一的子进程启动封装：不弹控制台黑框。
//
// 为什么需要它：本程序编译成 windowsgui 子系统（双击运行没有控制台），
// 但 exec.Command 启动的 ffmpeg / python / powershell / reg 都是控制台程序，
// Windows 会给每个子进程新建一个黑框。一次「开始录制」会同时拉起 ffmpeg、
// 回环采集、识别 worker、屏幕探测（4 个并发），屏幕上能连弹五六个黑框。
//
// 只设 SysProcAttr.HideWindow 有时仍会闪一下（控制台先创建再隐藏），
// 加上 CREATE_NO_WINDOW 才是从根上不创建控制台。
package winhide

import (
	"context"
	"os/exec"
	"syscall"
)

const createNoWindow = 0x08000000

// apply 设置不创建控制台窗口
func apply(cmd *exec.Cmd) *exec.Cmd {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
	return cmd
}

// Command 等价于 exec.Command，但子进程不会弹黑框
func Command(name string, arg ...string) *exec.Cmd {
	return apply(exec.Command(name, arg...))
}

// CommandContext 等价于 exec.CommandContext，但子进程不会弹黑框
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return apply(exec.CommandContext(ctx, name, arg...))
}
