package notify

import "strings"

/*
 * splitMailRecipients 解析逗号分隔的收件人列表
 * 供 Sendcloud 与阿里云 DirectMail 两个 API 后端共用，保证与 SMTP 路径一致的拆分与去空规则
 */
func splitMailRecipients(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if addr := strings.TrimSpace(p); addr != "" {
			out = append(out, addr)
		}
	}
	return out
}
