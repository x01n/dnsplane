package middleware

import (
	"strings"
	"testing"
)

/*
 * 请求日志的脱敏必须覆盖两层
 *
 * 一、字符串里内嵌的 JSON
 *
 * 面板把服务商凭证以一个 JSON **字符串**存进 config 列，提交时整个 config
 * 是 body 里的一个字符串值。外层键是 "config"——不在敏感键表里，
 * 只看键名的话，它内部的 SecretKey 会原样落进请求日志长期留存。
 *
 * 二、个人信息
 *
 * 邮箱出现在用户资料、通知收件人、白名单三处，属个人信息，
 * 与密码同级处理
 */

/* TestSanitizeRecursesIntoNestedJSON 内嵌 JSON 里的凭证要被脱敏 */
func TestSanitizeRecursesIntoNestedJSON(t *testing.T) {
	in := `{"config":"{\"SecretId\":\"AK123\",\"SecretKey\":\"SK456\"}"}`
	out := SanitizeBodyForLog(in, "application/json")

	if strings.Contains(out, "AK123") || strings.Contains(out, "SK456") {
		t.Errorf("内嵌 config 里的凭证未被脱敏：%s", out)
	}
	if !strings.Contains(out, redactedMark) {
		t.Errorf("应出现脱敏标记：%s", out)
	}
}

/* TestSanitizeKeepsNonSensitiveNestedJSON 内嵌 JSON 里没有敏感键时不该改动格式 */
func TestSanitizeKeepsNonSensitiveNestedJSON(t *testing.T) {
	in := `{"config":"{\"region\":\"cn-hangzhou\",\"endpoint\":\"https://x\"}"}`
	out := SanitizeBodyForLog(in, "application/json")

	// 没有敏感键就不回写，原文里的转义与键序保持不变
	if !strings.Contains(out, `cn-hangzhou`) {
		t.Errorf("非敏感内容不应被改动或丢弃：%s", out)
	}
	if strings.Contains(out, redactedMark) {
		t.Errorf("无敏感键时不该出现脱敏标记：%s", out)
	}
}

/* TestSanitizeRedactsEmail 邮箱作为个人信息被脱敏 */
func TestSanitizeRedactsEmail(t *testing.T) {
	out := SanitizeBodyForLog(`{"email":"someone@example.com","name":"x"}`, "application/json")

	if strings.Contains(out, "someone@example.com") {
		t.Errorf("邮箱未被脱敏：%s", out)
	}
	if !strings.Contains(out, `"name":"x"`) {
		t.Errorf("非敏感字段不应受影响：%s", out)
	}
}

/* TestSanitizeRedactsProviderCredentials 各服务商的凭证键名都被覆盖 */
func TestSanitizeRedactsProviderCredentials(t *testing.T) {
	in := `{"SecretId":"a","SecretKey":"b","api_password":"c","app_secret":"d","sp_key":"e","opentoken":"f"}`
	out := SanitizeBodyForLog(in, "application/json")

	for _, secret := range []string{`"a"`, `"b"`, `"c"`, `"d"`, `"e"`, `"f"`} {
		if strings.Contains(out, secret) {
			t.Errorf("凭证值 %s 未被脱敏：%s", secret, out)
		}
	}
}

/* TestSanitizeNonJSONStringUntouched 非 JSON 的普通字符串不做多余解析 */
func TestSanitizeNonJSONStringUntouched(t *testing.T) {
	in := `{"note":"这并不是 JSON"}`

	if out := SanitizeBodyForLog(in, "application/json"); !strings.Contains(out, "这并不是 JSON") {
		t.Errorf("普通文本不应被改动：%s", out)
	}
}
