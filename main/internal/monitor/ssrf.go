package monitor

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

// 安全审计 M-3：监控任务的 CheckURL 来源于可添加域名的普通用户，未做任何过滤即发起出站请求，
// 可被用作 SSRF 跳板访问云厂商 IMDS、内网服务、Kubernetes API 等。
// validateCheckURL 对协议、主机做白名单校验，拒绝指向环回 / 私网 / 链路本地地址的 URL。

// 允许的协议
var allowedCheckURLSchemes = map[string]struct{}{
	"http":  {},
	"https": {},
}

// validateCheckURL 解析 rawURL 并拒绝一切明显的 SSRF 目标。合法则返回 nil。
//
// 校验规则：
//   - scheme 必须为 http/https（禁止 file:// gopher:// 等）
//   - host 必须非空
//   - 若 host 是 IP（或可被解析为纯数字 IP），必须是公网地址
//   - 保留的 DNS 域名（localhost、内网 TLD）同样拒绝
//
// 注意：本函数不做 DNS 解析（避免慢解析 + TOCTOU 漂移）；check.go 里的 Dialer
//       仍会做实际连接，若目标解析到私网，buildInsecureAddrGuard 会在连接阶段二次拦截。
func validateCheckURL(rawURL string) error {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return nil // 空 URL 走默认生成路径，由调用方拼接 addr:port
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL 解析失败: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if _, ok := allowedCheckURLSchemes[scheme]; !ok {
		return fmt.Errorf("不允许的协议 %q，仅支持 http/https", scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机部分")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
		return fmt.Errorf("拒绝指向内网域名 %q 的探测任务", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrReservedIP(ip) {
			return fmt.Errorf("拒绝指向私网/保留地址 %q 的探测任务", host)
		}
	}
	return nil
}

// isPrivateOrReservedIP 判断 IP 是否属于私网、回环、链路本地、组播或未分配段。
func isPrivateOrReservedIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsInterfaceLocalMulticast() {
		return true
	}
	if ip.IsPrivate() {
		return true
	}
	// IPv4 额外保留段（CGNAT / 基准地址 / 测试网等）
	if v4 := ip.To4(); v4 != nil {
		switch v4[0] {
		case 0:
			return true // 0.0.0.0/8
		case 100:
			if v4[1] >= 64 && v4[1] <= 127 {
				return true // 100.64.0.0/10 CGNAT
			}
		case 127:
			return true // 127.0.0.0/8
		case 169:
			if v4[1] == 254 {
				return true // 169.254.0.0/16 link-local（含云厂商 IMDS）
			}
		case 224, 225, 226, 227, 228, 229, 230, 231, 232, 233, 234, 235, 236, 237, 238, 239:
			return true // 224.0.0.0/4 组播
		case 240, 241, 242, 243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 254, 255:
			return true // 240.0.0.0/4 保留 / 广播
		}
	}
	return false
}
