package notify

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

/*
 * 通知器出站 URL SSRF 防护（安全审计 R-5）。
 *
 * Webhook / Bark / Discord / WeChat 等通知器都允许管理员填写任意 URL。
 * 缺乏校验时可被填入：
 *   - http://127.0.0.1:6379  → 内网 Redis 命令注入面
 *   - http://169.254.169.254/... → 云厂商 IMDS 凭据回显
 *   - http://internal-svc/admin   → 内网服务枚举
 *   - file:// gopher:// 等异常协议
 *
 * 一旦叠加 R-2（SystemConfig 无鉴权）即可由普通用户配置，威胁面进一步放大。
 *
 * 校验规则：
 *   1. 协议白名单：仅允许 http / https
 *   2. host 必须非空
 *   3. host 是 IP 则拒绝私网 / 回环 / 链路本地 / 组播 / 保留段
 *   4. host 是保留 DNS 域名（localhost / *.localhost / *.internal / *.local）拒绝
 */

var allowedNotifySchemes = map[string]struct{}{
	"http":  {},
	"https": {},
}

// ValidateOutboundURL 出站 URL 校验，合法返回 nil。
// 不做 DNS 解析（避免 TOCTOU 漂移与慢解析阻塞）；最终连接由调用栈决定。
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

// isPrivateOrReservedIP 与 monitor/ssrf.go 同等语义；本包独立维护一份避免循环依赖。
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
				return true // 169.254.0.0/16 link-local（含云厂商 IMDS）
			}
		case 224, 225, 226, 227, 228, 229, 230, 231, 232, 233, 234, 235, 236, 237, 238, 239:
			return true // 组播
		case 240, 241, 242, 243, 244, 245, 246, 247, 248, 249, 250, 251, 252, 253, 254, 255:
			return true // 保留 / 广播
		}
	}
	return false
}

// SanitizeMailHeader 校验邮件头字段，防 CRLF 注入（安全审计 R-4）。
//
// 攻击场景：From / FromName / To / Subject 等字段允许 \r\n 时，可附加
// "Bcc: victim@x" / "Subject: phishing" 把已认证 SMTP 通道用作钓鱼跳板。
// 一旦字段中包含任何 CR/LF/NUL 控制字符即拒绝。
func SanitizeMailHeader(field, raw string) error {
	if strings.ContainsAny(raw, "\r\n\x00") {
		return fmt.Errorf("邮件头字段 %s 包含非法控制字符", field)
	}
	return nil
}
