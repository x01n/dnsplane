//go:build windows

package logger

import (
	"syscall"
	"unsafe"
)

// enableWindowsANSI 启用 Windows 终端 ANSI 颜色（虚拟终端处理）
func enableWindowsANSI() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")
	getStdHandle := kernel32.NewProc("GetStdHandle")

	const (
		stdOutputHandle       = ^uintptr(0) - 11 + 1 // -11
		enableVirtualTerminal = 0x0004
	)

	handle, _, _ := getStdHandle.Call(stdOutputHandle)
	var mode uint32
	getConsoleMode.Call(handle, uintptr(unsafe.Pointer(&mode)))
	setConsoleMode.Call(handle, uintptr(mode|enableVirtualTerminal))
}
