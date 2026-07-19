package notify

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

var allowedNotifySchemes = map[string]struct{}{
	"http":  {},
	"https": {},
}

func ValidateOutboundURL(rawURL string) error {
	raw := strings.TrimSpace(rawURL)
	if raw == "" {
		return fmt.Errorf("URL 为空")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("URL 解析失败: %w", err)
	}
	scheme := strings.ToLower(u.Scheme)
	if _, ok := allowedNotifySchemes[scheme]; !ok {
		return fmt.Errorf("不允许的协议 %q，仅支持 http/https", scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("URL 缺少主机部分")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
		return fmt.Errorf("拒绝指向内网域名 %q 的通知地址", host)
	}
	if ip := net.ParseIP(host); ip != nil {
		if isPrivateOrReservedIP(ip) {
			return fmt.Errorf("拒绝指向私网/保留地址 %q 的通知地址", host)
		}
	}
	return nil
}

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
	if v4 := ip.To4(); v4 != nil {
		switch v4[0] {
		case 0:
			return true
		case 100:
			if v4[1] >= 64 && v4[1] <= 127 {
				return true // 100.64.0.0/10 CGNAT
			}
		case 127:
			return true
		case 169:
			if v4[1] == 254 {
				return true 
			}
		case 224, 225, 226, 227, 228, 229, 230, 231, 232, 233, 234, 235, 236, 237, 238, 239:
			return true 
		case 240, 241, 242, 243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 254, 255:
			return true 
		}
	}
	return false
}

func SanitizeMailHeader(field, raw string) error {
	if strings.ContainsAny(raw, "\r\n\x00") {
		return fmt.Errorf("邮件头字段 %s 包含非法控制字符", field)
	}
	return nil
}
