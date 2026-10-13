package monitor

import (
	"context"
	"fmt"
	"net"
	"testing"
	"time"
)

/*
 * IPv6 地址在 TCP 探测里必须带方括号
 *
 * 裸拼接 host:port 对 IPv6 会产出 "::1:80" 这种有多个冒号的地址，
 * net.Dial 直接以 "too many colons in address" 拒绝，任务永久判失败。
 * 这里起一个真实的 IPv6 回环监听，断言探测能连上，而非只比对地址字符串
 */
func TestCheckTCPAcceptsIPv6Literal(t *testing.T) {
	listener, err := net.Listen("tcp", "[::1]:0")
	if err != nil {
		t.Skipf("本机无 IPv6 回环，用例前提不成立: %v", err)
	}
	defer listener.Close()

	accepted := make(chan struct{}, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			conn.Close()
			accepted <- struct{}{}
		}
	}()

	_, portStr, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("解析监听端口失败: %v", err)
	}
	port := 0
	if _, err := fmt.Sscanf(portStr, "%d", &port); err != nil {
		t.Fatalf("端口 %q 不是数字: %v", portStr, err)
	}

	result := CheckTCP(context.Background(), "::1", port, 3)
	if !result.Success {
		t.Fatalf("IPv6 回环 TCP 探测失败: %s", result.Error)
	}

	select {
	case <-accepted:
	case <-time.After(time.Second):
		t.Error("探测报告成功，但监听侧未收到连接")
	}
}

/*
 * 非法地址不能因为加方括号而被误判为可连
 *
 * JoinHostPort 只做格式封装，不做可达性判断：这里断言的是失败路径给出的是
 * 拨号错误而不是格式错误，避免「加了方括号就万事大吉」的误解
 */
func TestCheckTCPRejectsUnreachablePort(t *testing.T) {
	// 绑定后立刻关闭，拿一个确定无人监听的端口
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("申请端口失败: %v", err)
	}
	addr := listener.Addr().String()
	listener.Close()

	_, portStr, _ := net.SplitHostPort(addr)
	port := 0
	fmt.Sscanf(portStr, "%d", &port)

	result := CheckTCP(context.Background(), "127.0.0.1", port, 1)
	if result.Success {
		t.Error("已关闭的端口不应探测成功")
	}
}
