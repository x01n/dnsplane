package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"runtime"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/proxy"
)

const maxHTTPBodyPreview = 8192

// HTTPCheckOptions HTTP(S) 探测：代理、重定向上限、期望状态码与响应正文关键词
type HTTPCheckOptions struct {
	HostIP        string
	MaxRedirects  int // 0=默认跟随 3 次；-1=不跟随重定向（仅看首包）
	ExpectStatus  string
	ExpectKeyword string
	ProxyType     string // http / socks5
	ProxyHost     string
	ProxyPort     int
	ProxyUsername string
	ProxyPassword string
	/* UseEnvProxy: UseProxy=true 且未填代理主机时走 HTTP_PROXY 等环境变量 */
	UseEnvProxy bool
	/*
	 * InsecureSkipTLS: 是否跳过 HTTPS 证书校验（默认 false=校验）。
	 * 仅在探测自签或内网证书时手动开启；默认安全对应安全审计 H-3 的修复。
	 */
	InsecureSkipTLS bool
}

type CheckResult struct {
	Success     bool
	Duration    time.Duration
	Error       string
	StatusCode  int
	BodyPreview string
}

// CheckPing 跨平台ICMP Ping检测
// Windows: 使用 iphlpapi.dll 的 IcmpSendEcho（内核级API，最可靠）
// Linux:   使用 golang.org/x/net/icmp 原始套接字
func CheckPing(ctx context.Context, ip string, timeout int) *CheckResult {
	if runtime.GOOS == "windows" {
		return checkPingWindows(ctx, ip, timeout)
	}
	return checkPingICMP(ctx, ip, timeout)
}

// checkPingWindows 在 check_ping_windows.go（Windows）/ check_ping_stub.go（其它平台）中实现

// ==================== Linux/macOS: ICMP raw socket ====================

// checkPingICMP 使用 golang.org/x/net/icmp 实现（Linux/macOS）
func checkPingICMP(ctx context.Context, ip string, timeout int) *CheckResult {
	dst := resolveIPv4(ip)
	if dst == nil {
		return &CheckResult{Success: false, Error: fmt.Sprintf("resolve %s failed", ip)}
	}

	conn, err := icmp.ListenPacket("ip4:icmp", "0.0.0.0")
	if err != nil {
		return checkTCPFallback(ctx, ip, timeout)
	}
	defer conn.Close()

	deadline := time.Now().Add(time.Duration(timeout) * time.Second)
	conn.SetDeadline(deadline)

	id := rand.Intn(0xFFFF)
	seq := rand.Intn(0xFFFF)
	msg := icmp.Message{
		Type: ipv4.ICMPTypeEcho,
		Code: 0,
		Body: &icmp.Echo{ID: id, Seq: seq, Data: []byte("DNSPlane")},
	}
	msgBytes, _ := msg.Marshal(nil)

	start := time.Now()
	dstAddr := &net.IPAddr{IP: dst}

	if _, err := conn.WriteTo(msgBytes, dstAddr); err != nil {
		return &CheckResult{Success: false, Duration: time.Since(start), Error: fmt.Sprintf("send: %v", err)}
	}

	buf := make([]byte, 1500)
	for {
		select {
		case <-ctx.Done():
			return &CheckResult{Success: false, Duration: time.Since(start), Error: "cancelled"}
		default:
		}

		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				return &CheckResult{Success: false, Duration: time.Since(start), Error: "ping timeout"}
			}
			return &CheckResult{Success: false, Duration: time.Since(start), Error: fmt.Sprintf("read: %v", err)}
		}

		rm, err := icmp.ParseMessage(1, buf[:n])
		if err != nil {
			continue
		}

		if rm.Type == ipv4.ICMPTypeEchoReply {
			if echo, ok := rm.Body.(*icmp.Echo); ok {
				if echo.ID == id && echo.Seq == seq {
					return &CheckResult{Success: true, Duration: time.Since(start)}
				}
			}
		}

		if rm.Type == ipv4.ICMPTypeDestinationUnreachable {
			return &CheckResult{Success: false, Duration: time.Since(start), Error: "destination unreachable"}
		}

		if time.Now().After(deadline) {
			return &CheckResult{Success: false, Duration: time.Since(start), Error: "ping timeout"}
		}
	}
}

// ==================== 工具函数 ====================

// resolveIPv4 解析IP或域名为IPv4地址
func resolveIPv4(ip string) net.IP {
	dst := net.ParseIP(ip)
	if dst != nil {
		return dst.To4()
	}
	addrs, err := net.LookupIP(ip)
	if err != nil || len(addrs) == 0 {
		return nil
	}
	for _, addr := range addrs {
		if v4 := addr.To4(); v4 != nil {
			return v4
		}
	}
	return nil
}

// checkTCPFallback 当ICMP不可用时回退到TCP探测
func checkTCPFallback(ctx context.Context, host string, timeout int) *CheckResult {
	start := time.Now()
	dialer := net.Dialer{Timeout: time.Duration(timeout) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, "80"))
	duration := time.Since(start)
	if err != nil {
		return &CheckResult{Success: false, Duration: duration, Error: fmt.Sprintf("icmp unavailable, tcp80 failed: %v", err)}
	}
	conn.Close()
	return &CheckResult{Success: true, Duration: duration}
}

// ==================== TCP / HTTP 检测 ====================

func CheckTCP(ctx context.Context, host string, port int, timeout int) *CheckResult {
	start := time.Now()
	addr := fmt.Sprintf("%s:%d", host, port)
	dialer := net.Dialer{Timeout: time.Duration(timeout) * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", addr)
	duration := time.Since(start)
	if err != nil {
		return &CheckResult{Success: false, Duration: duration, Error: fmt.Sprintf("tcp connect failed: %v", err)}
	}
	conn.Close()
	return &CheckResult{Success: true, Duration: duration}
}

func rewriteAddrHostIP(addr string, hostIP string, rawURL string) string {
	if hostIP == "" {
		return addr
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		if strings.HasPrefix(strings.ToLower(rawURL), "https") {
			port = "443"
		} else {
			port = "80"
		}
		return net.JoinHostPort(hostIP, port)
	}
	return net.JoinHostPort(hostIP, port)
}

func matchExpectHTTPStatus(code int, expect string) bool {
	for _, p := range strings.Split(expect, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := strconv.Atoi(p); err == nil && n == code {
			return true
		}
	}
	return false
}

// CheckHTTP 执行 HTTP(S) GET 检测（支持 HTTP/SOCKS5 认证代理、重定向次数、状态码与正文关键词）
func CheckHTTP(ctx context.Context, rawURL string, timeout int, opts *HTTPCheckOptions) *CheckResult {
	start := time.Now()
	if opts == nil {
		opts = &HTTPCheckOptions{}
	}
	maxFollow := opts.MaxRedirects
	if maxFollow == 0 {
		maxFollow = 3
	}

	baseDialer := &net.Dialer{Timeout: time.Duration(timeout) * time.Second}
	// 默认开启 TLS 校验（安全审计 H-3）；仅当监控任务显式勾选"允许自签证书"时退让。
	tlsCfg := &tls.Config{InsecureSkipVerify: opts.InsecureSkipTLS}
	transport := &http.Transport{
		TLSClientConfig: tlsCfg,
		MaxIdleConns:    10,
		IdleConnTimeout: 30 * time.Second,
	}

	proxyHost := strings.TrimSpace(opts.ProxyHost)
	proxyPort := opts.ProxyPort
	proxyKind := strings.ToLower(strings.TrimSpace(opts.ProxyType))
	useFieldProxy := proxyHost != "" && proxyPort > 0 && (proxyKind == "http" || proxyKind == "socks5")

	if useFieldProxy && proxyKind == "http" {
		proxyAddr := net.JoinHostPort(proxyHost, strconv.Itoa(proxyPort))
		u := &url.URL{Scheme: "http", Host: proxyAddr}
		if opts.ProxyUsername != "" {
			u.User = url.UserPassword(opts.ProxyUsername, opts.ProxyPassword)
		}
		transport.Proxy = http.ProxyURL(u)
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			addr = rewriteAddrHostIP(addr, opts.HostIP, rawURL)
			return baseDialer.DialContext(ctx, network, addr)
		}
	} else if useFieldProxy && proxyKind == "socks5" {
		proxyAddr := net.JoinHostPort(proxyHost, strconv.Itoa(proxyPort))
		var auth *proxy.Auth
		if opts.ProxyUsername != "" {
			auth = &proxy.Auth{User: opts.ProxyUsername, Password: opts.ProxyPassword}
		}
		socksDialer, err := proxy.SOCKS5("tcp", proxyAddr, auth, baseDialer)
		if err != nil {
			return &CheckResult{Success: false, Duration: time.Since(start), Error: "socks5 代理: " + err.Error()}
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			target := rewriteAddrHostIP(addr, opts.HostIP, rawURL)
			type res struct {
				c   net.Conn
				err error
			}
			ch := make(chan res, 1)
			go func() {
				c, err := socksDialer.Dial(network, target)
				ch <- res{c, err}
			}()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case r := <-ch:
				return r.c, r.err
			}
		}
	} else {
		if opts.UseEnvProxy {
			transport.Proxy = http.ProxyFromEnvironment
		}
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			addr = rewriteAddrHostIP(addr, opts.HostIP, rawURL)
			return baseDialer.DialContext(ctx, network, addr)
		}
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   time.Duration(timeout) * time.Second,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if opts.MaxRedirects == -1 {
				return http.ErrUseLastResponse
			}
			if len(via) >= maxFollow {
				return fmt.Errorf("重定向过多(最多 %d 次)", maxFollow)
			}
			return nil
		},
	}

	req, err := http.NewRequestWithContext(ctx, "GET", rawURL, nil)
	if err != nil {
		return &CheckResult{Success: false, Duration: time.Since(start), Error: fmt.Sprintf("create request: %v", err)}
	}
	req.Header.Set("User-Agent", "DNSPlane-Monitor/1.0")

	resp, err := client.Do(req)
	duration := time.Since(start)
	if err != nil {
		return &CheckResult{Success: false, Duration: duration, Error: fmt.Sprintf("http 请求失败: %v", err)}
	}
	defer resp.Body.Close()

	limited := io.LimitReader(resp.Body, maxHTTPBodyPreview)
	bodyChunk, _ := io.ReadAll(limited)
	io.Copy(io.Discard, resp.Body)

	preview := string(bodyChunk)
	code := resp.StatusCode
	out := &CheckResult{
		Duration:    duration,
		StatusCode:  code,
		BodyPreview: preview,
	}

	ok := code >= 200 && code < 400
	if strings.TrimSpace(opts.ExpectStatus) != "" {
		ok = matchExpectHTTPStatus(code, opts.ExpectStatus)
	}
	if !ok {
		out.Error = fmt.Sprintf("HTTP 状态 %d", code)
		return out
	}

	if kw := strings.TrimSpace(opts.ExpectKeyword); kw != "" {
		if !strings.Contains(strings.ToLower(preview), strings.ToLower(kw)) {
			out.Error = fmt.Sprintf("响应正文未包含关键词 %q", kw)
			return out
		}
	}

	out.Success = true
	return out
}

// ==================== 其他工具 ====================

func ResolveDomain(domain string) ([]string, error) {
	ips, err := net.LookupIP(domain)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			result = append(result, ipv4.String())
		}
	}
	return result, nil
}

func IsIPAddress(s string) bool {
	return net.ParseIP(s) != nil
}

func GetRecordType(value string) string {
	ip := net.ParseIP(value)
	if ip == nil {
		return "CNAME"
	}
	if ip.To4() != nil {
		return "A"
	}
	return "AAAA"
}
