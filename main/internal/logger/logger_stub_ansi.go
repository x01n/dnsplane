//go:build !windows

package logger

func enableWindowsANSI() {
	// 非 Windows 无需 SetConsoleMode
}
