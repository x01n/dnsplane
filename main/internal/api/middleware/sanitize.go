package middleware

import (
	"encoding/json"
	"net/url"
	"regexp"
	"strings"
)

// 本文件提供请求日志脱敏工具：RequestTrace/AuditLog 记录 body 与 headers 之前调用。
// 命中关键字的字段值替换为固定掩码，保留字段结构便于排障。

const redactedMark = "***REDACTED***"

// 敏感关键字列表，匹配 JSON key 或表单字段名；不区分大小写、不区分 _/- 间隔。
// 注意：宁可多掩几个也不要漏（密码、token、私钥是硬性要求）。
var sensitiveKeys = map[string]struct{}{
	"password":          {},
	"passwd":            {},
	"pwd":               {},
	"new_password":      {},
	"old_password":      {},
	"confirm_password":  {},
	"secret":            {},
	"jwt_secret":        {},
	"master_key":        {},
	"token":             {},
	"access_token":      {},
	"refresh_token":     {},
	"magic_link_token":  {},
	"reset_token":       {},
	"api_key":           {},
	"apikey":            {},
	"access_key":        {},
	"secret_key":        {},
	"access_key_id":     {},
	"access_key_secret": {},
	"accesskey":         {},
	"secretkey":         {},
	"accesskeyid":       {},
	"accesskeysecret":   {},
	"totp":              {},
	"totp_secret":       {},
	"totp_code":         {},
	"verification_code": {},
	"captcha":           {},
	"code":              {},
	"private_key":       {},
	"privatekey":        {},
	"proxy_password":    {},
	"ssh_password":      {},
	"eab_hmac_key":      {},
	"eab_kid":           {},
	"webhook_secret":    {},
	"smtp_password":     {},
	"mail_password":     {},
}

var sensitiveHeaders = map[string]struct{}{
	"authorization":   {},
	"cookie":          {},
	"set-cookie":      {},
	"x-refresh-token": {},
	"x-secret-token":  {},
	"x-csrf-token":    {},
	"proxy-authorization": {},
}

// 匹配 config JSON 里可能内嵌的 long key/secret（尽量避免误伤普通文本）
var longCredentialRe = regexp.MustCompile(`(?i)(access[_-]?key[_-]?(id|secret)?|secret[_-]?key)`)

func normalizeKey(k string) string {
	k = strings.ToLower(k)
	k = strings.ReplaceAll(k, "-", "")
	k = strings.ReplaceAll(k, "_", "")
	return k
}

// isSensitiveKey 判断 JSON key / form 字段是否属于敏感列表。
func isSensitiveKey(k string) bool {
	normalized := normalizeKey(k)
	for key := range sensitiveKeys {
		if normalized == normalizeKey(key) {
			return true
		}
	}
	if longCredentialRe.MatchString(k) {
		return true
	}
	return false
}

// SanitizeBodyForLog 接收请求/响应的原始 body 字符串，返回脱敏后的版本。
// 识别 JSON（对象/数组递归）与 x-www-form-urlencoded；其他类型不修改原文。
func SanitizeBodyForLog(body string, contentType string) string {
	if body == "" {
		return body
	}
	trimmed := strings.TrimSpace(body)
	ct := strings.ToLower(contentType)

	// JSON 分支：尝试解析为 any，递归脱敏后序列化回写
	if strings.HasPrefix(trimmed, "{") || strings.HasPrefix(trimmed, "[") || strings.Contains(ct, "application/json") {
		var v any
		if err := json.Unmarshal([]byte(trimmed), &v); err == nil {
			redactJSON(&v)
			if out, err := json.Marshal(v); err == nil {
				return string(out)
			}
		}
	}

	// Form 分支
	if strings.Contains(ct, "application/x-www-form-urlencoded") {
		if vals, err := url.ParseQuery(trimmed); err == nil {
			for k := range vals {
				if isSensitiveKey(k) {
					vals.Set(k, redactedMark)
				}
			}
			return vals.Encode()
		}
	}

	return body
}

func redactJSON(v *any) {
	switch node := (*v).(type) {
	case map[string]any:
		for k, val := range node {
			if isSensitiveKey(k) {
				node[k] = redactedMark
				continue
			}
			redactJSON(&val)
			node[k] = val
		}
	case []any:
		for i := range node {
			redactJSON(&node[i])
		}
	}
}

// SanitizeHeadersForLog 将敏感 header 值替换为掩码后返回 JSON 字符串。
// 输入为 map[string]string，保留 key 与大小写以便排障。
func SanitizeHeadersForLog(headers map[string]string) string {
	if len(headers) == 0 {
		return "{}"
	}
	out := make(map[string]string, len(headers))
	for k, v := range headers {
		if _, ok := sensitiveHeaders[strings.ToLower(k)]; ok {
			out[k] = redactedMark
			continue
		}
		out[k] = v
	}
	b, _ := json.Marshal(out)
	return string(b)
}
