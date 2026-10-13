//go:build windows

package monitor

import (
	"context"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

// checkPingWindows 使用 Windows 原生 IcmpSendEcho API
func checkPingWindows(ctx context.Context, ip string, timeout int) *CheckResult {
	dst := resolveIPv4(ip)
	if dst == nil {
		return &CheckResult{Success: false, Error: fmt.Sprintf("resolve %s failed", ip)}
	}

	iphlpapi := syscall.NewLazyDLL("iphlpapi.dll")
	procCreate := iphlpapi.NewProc("IcmpCreateFile")
	procSendEcho := iphlpapi.NewProc("IcmpSendEcho")
	procClose := iphlpapi.NewProc("IcmpCloseHandle")

	handle, _, err := procCreate.Call()
	invalidHandle := ^uintptr(0)
	if handle == invalidHandle {
		return &CheckResult{Success: false, Error: fmt.Sprintf("IcmpCreateFile: %v", err)}
	}
	defer procClose.Call(handle)

	destAddr := uint32(dst[0]) | uint32(dst[1])<<8 | uint32(dst[2])<<16 | uint32(dst[3])<<24
	sendData := []byte("DNSPlane")
	replyBuf := make([]byte, 256)

	timeoutMs := uint32(timeout * 1000)
	if timeoutMs == 0 {
		timeoutMs = 5000
	}

	start := time.Now()

	type pingResult struct {
		ret uintptr
	}
	resultCh := make(chan pingResult, 1)

	go func() {
		ret, _, _ := procSendEcho.Call(
			handle,
			uintptr(destAddr),
			uintptr(unsafe.Pointer(&sendData[0])),
			uintptr(len(sendData)),
			0,
			uintptr(unsafe.Pointer(&replyBuf[0])),
			uintptr(len(replyBuf)),
			uintptr(timeoutMs),
		)
		resultCh <- pingResult{ret: ret}
	}()

	select {
	case <-ctx.Done():
		return &CheckResult{Success: false, Duration: time.Since(start), Error: "cancelled"}
	case res := <-resultCh:
		duration := time.Since(start)
		if res.ret == 0 {
			return &CheckResult{Success: false, Duration: duration, Error: "ping timeout"}
		}
		status := *(*uint32)(unsafe.Pointer(&replyBuf[4]))
		rtt := *(*uint32)(unsafe.Pointer(&replyBuf[8]))
		if status != 0 {
			return &CheckResult{Success: false, Duration: duration, Error: windowsICMPStatusText(status)}
		}
		return &CheckResult{Success: true, Duration: time.Duration(rtt) * time.Millisecond}
	}
}

func windowsICMPStatusText(status uint32) string {
	switch status {
	case 0:
		return "success"
	case 11002:
		return "destination net unreachable"
	case 11003:
		return "destination host unreachable"
	case 11010:
		return "request timed out"
	case 11013:
		return "time exceeded"
	default:
		return fmt.Sprintf("icmp error (status=%d)", status)
	}
}
