package middleware

import (
	"strings"
	"testing"

	"main/internal/notify"
)

/*
 * 凭据键的脱敏覆盖
 *
 * 此前是「整串等值匹配」加一条只覆盖 access_key / secret_key 的正则，
 * 够不着带前缀的键名——wxwork_secret、tgbot_token、mail_api_key、
 * github_client_secret 等 22 个真实凭据键全部漏过，明文落进请求日志
 *
 * 改按词干子串匹配后，覆盖不再依赖人工同步：新增服务商或通知渠道时，
 * 只要键名里带 secret / token 这类词就自动落进遮蔽范围
 */

/* TestSanitizeCoversAllNotifyCredentialKeys 通知渠道的凭证键一个都不能漏 */
func TestSanitizeCoversAllNotifyCredentialKeys(t *testing.T) {
	/*
	 * 数据源用 notify 的真实键集合而非手抄清单
	 *
	 * 手抄清单会随渠道增删而失真，而这份是运行期真值：
	 * 新增一个渠道后，它的配置键若没被脱敏覆盖，这条用例立刻红
	 */
	var leaked []string
	for _, k := range notify.NotifyConfigKeys() {
		if !credentialLike(k) {
			continue
		}
		if !isSensitiveKey(k) {
			leaked = append(leaked, k)
		}
	}
	if len(leaked) > 0 {
		t.Errorf("通知渠道的凭据键未被脱敏：%v", leaked)
	}
}

/* credentialLike 判断键名在语义上是否属于凭据（与脱敏实现无关的独立判据） */
func credentialLike(k string) bool {
	n := normalizeKey(k)
	for _, stem := range []string{"secret", "password", "passwd", "token", "key", "credential"} {
		if strings.Contains(n, stem) {
			return true
		}
	}
	// _enabled / _type / _method / _format 之类是行为开关，不是凭据
	return false
}

/* TestSanitizeRedactsPrefixedCredentialKeys 带前缀的凭据键必须被掩 */
func TestSanitizeRedactsPrefixedCredentialKeys(t *testing.T) {
	cases := map[string]string{
		"tgbot_token":                `{"tgbot_token":"123:ABC"}`,
		"dingtalk_secret":            `{"dingtalk_secret":"SECx"}`,
		"feishu_secret":              `{"feishu_secret":"SECy"}`,
		"wxwork_secret":              `{"wxwork_secret":"SECz"}`,
		"wxtpl_appsecret":            `{"wxtpl_appsecret":"SECw"}`,
		"wechat_apptoken":            `{"wechat_apptoken":"AT-abc"}`,
		"qqbot_appsecret":            `{"qqbot_appsecret":"SECq"}`,
		"mail_api_key":               `{"mail_api_key":"KEY-m"}`,
		"github_client_secret":       `{"github_client_secret":"SECg"}`,
		"github_app_private_key":     `{"github_app_private_key":"-----BEGIN RSA-----"}`,
		"oauth_google_client_secret": `{"oauth_google_client_secret":"SECo"}`,
		"oauth_custom_client_secret": `{"oauth_custom_client_secret":"SECc"}`,
		"accel_eo_secret_key":        `{"accel_eo_secret_key":"SECe"}`,
	}
	for name, in := range cases {
		out := SanitizeBodyForLog(in, "application/json")
		if !strings.Contains(out, redactedMark) {
			t.Errorf("%s 未被脱敏：%s", name, out)
		}
	}
}

/*
 * TestSanitizeStemMatchingAcceptedCollateral 记录词干匹配的已知误伤
 *
 * 这四个键本身不是凭据（一个是 ID、一个是开放的授权页地址、两个是开关），
 * 掩掉只影响日志排查便利，不影响功能。用例固化这一取舍：
 * 若哪天有人把它们从遮蔽范围里救出来，这里会红，提醒确认是有意为之
 */
func TestSanitizeStemMatchingAcceptedCollateral(t *testing.T) {
	for _, k := range []string{
		"accel_eo_secret_id",
		"accel_esa_access_key_id",
		"oauth_custom_token_url",
		"password_register_enabled",
	} {
		if !isSensitiveKey(k) {
			t.Errorf("%s 预期被词干规则掩掉（已知取舍），实际未掩", k)
		}
	}
}

/* TestSanitizeLeavesNonCredentialKeysReadable 非凭据键不受词干规则影响 */
func TestSanitizeLeavesNonCredentialKeysReadable(t *testing.T) {
	in := `{"domain":"a.example.com","record_type":"A","line":"默认","ttl":600,"did":7,"status":"ENABLE"}`
	out := SanitizeBodyForLog(in, "application/json")

	for _, want := range []string{"a.example.com", `"A"`, "默认", "600", "ENABLE"} {
		if !strings.Contains(out, want) {
			t.Errorf("非凭据键 %s 被误掩：%s", want, out)
		}
	}
	if strings.Contains(out, redactedMark) {
		t.Errorf("非凭据键不该出现掩码：%s", out)
	}
}

/* TestSanitizeNormalizesSeparators 词干匹配对 _ 与 - 的分隔不敏感 */
func TestSanitizeNormalizesSeparators(t *testing.T) {
	for _, in := range []string{
		`{"client-secret":"v"}`,
		`{"client_secret":"v"}`,
		`{"CLIENT_SECRET":"v"}`,
		`{"ClientSecret":"v"}`,
	} {
		if out := SanitizeBodyForLog(in, "application/json"); !strings.Contains(out, redactedMark) {
			t.Errorf("分隔符/大小写变体未被脱敏：%s -> %s", in, out)
		}
	}
}
