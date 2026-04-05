//go:build !windows

package monitor

import "context"

// checkPingWindows 仅在 Windows 实现；非 Windows 构建需满足符号以通过编译（运行路径由 CheckPing 的 runtime.GOOS 分支保证不会进入此处）。
func checkPingWindows(ctx context.Context, ip string, timeout int) *CheckResult {
	return checkPingICMP(ctx, ip, timeout)
}
