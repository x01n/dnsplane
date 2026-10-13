package middleware

import (
	"encoding/json"
	"strings"
	"testing"

	"main/internal/notify"
)

/*
 * 词干规则与例外表的边界
 *
 * sensitiveKeys 现在只放「词干规则够不着的例外键」——它曾是 64 键的清单，
 * 其中 46 键的键名本身就含词干（password、token、secret_key 等），
 * 词干规则上线后成了冗余定义。删掉后两张表的分工是：
 *   词干规则：兜住所有键名含 secret / password / token 这类词的键
 *   例外表  ：兜住键名不含词干、但值确实是凭据或个人信息的键
 *
 * 这批用例锁的是**这个分工不退化**：任何一批键从遮蔽范围里掉出去，这里会红
 */

/*
 * TestSensitiveKeysHasNoStemCoveredRows 例外表里不应再出现被词干覆盖的键
 *
 * 反向的约束：若有人把 password / token 这类键加回例外表，这里会红——
 * 那说明他没意识到词干规则已经兜住了，两处定义会再次分叉
 */
func TestSensitiveKeysHasNoStemCoveredRows(t *testing.T) {
	for k := range sensitiveKeys {
		n := normalizeKey(k)
		for _, stem := range credentialStems {
			if strings.Contains(n, stem) {
				t.Errorf("例外表里的 %q 已含词干 %q，属冗余定义——词干规则已兜住它。"+
					"该键应从 sensitiveKeys 移除", k, stem)
				break
			}
		}
	}
}

/* TestStemsCoverUnprefixedCredentials 词干规则单独就能兜住带前缀的凭据键 */
func TestStemsCoverUnprefixedCredentials(t *testing.T) {
	// 这些键刻意**不在**例外表里，全部靠词干命中
	for _, k := range []string{
		"password", "passwd", "new_password", "confirm_password",
		"secret", "jwt_secret", "secret_key", "secret_id", "secret_access_key",
		"token", "access_token", "refresh_token", "totp_secret", "api_token",
		"api_key", "apikey", "access_key", "accesskey", "accesskeysecret",
		"private_key", "privatekey",
		"proxy_password", "ssh_password", "smtp_password", "mail_password",
		"webhook_secret", "client_secret", "app_secret", "api_secret",
		"tke_secret", "access_secret",
		// 带前缀的真实键名——这正是词干规则存在的理由
		"wxwork_secret", "tgbot_token", "mail_api_key", "dingtalk_secret",
		"github_client_secret", "wxtpl_appsecret", "qqbot_appsecret",
	} {
		if _, inTable := normalizedSensitiveKeys[normalizeKey(k)]; inTable {
			continue // 在表里也无妨，只要判为敏感即可
		}
		if !isSensitiveKey(k) {
			t.Errorf("%q 既不在例外表里、词干规则也没兜住——它会明文落库", k)
		}
	}
}

/* TestExceptionTableStillCoversNonStemCredentials 例外表里的键仍必须被判为敏感 */
func TestExceptionTableStillCoversNonStemCredentials(t *testing.T) {
	for _, k := range []string{
		"pwd", "master_key", "totp", "totp_code", "verification_code",
		"captcha", "code", "eab_hmac_key", "eab_kid", "bark_key",
		"app_key", "appkey", "skey", "sp_key", "spkey",
		"public_key", "publickey", "email",
	} {
		if !isSensitiveKey(k) {
			t.Errorf("例外键 %q 未被判为敏感——它从遮蔽范围里掉出去了", k)
		}
	}
}

/* TestExceptionTableIsMinimal 例外表不应重新膨胀回清单 */
func TestExceptionTableIsMinimal(t *testing.T) {
	const limit = 20
	if n := len(sensitiveKeys); n > limit {
		t.Errorf("例外表有 %d 键，超过 %d——它应只放词干够不着的例外。"+
			"新增键前先确认它的键名里确实没有词干词", n, limit)
	}
}

/*
 * TestNotifyCredentialKeysAllCovered 通知渠道的真实键集合全覆盖
 *
 * 数据源是 notify.NotifyConfigKeys() 而非手抄清单：新增渠道后，
 * 它的配置键若没被覆盖，这条立刻红。上一版靠它抓出漏网的 bark_key
 */
func TestNotifyCredentialKeysAllCovered(t *testing.T) {
	var leaked []string
	for _, k := range notify.NotifyConfigKeys() {
		// 只查凭据类：_enabled / _method / _format 之类是行为开关
		n := normalizeKey(k)
		isCred := false
		for _, stem := range []string{"secret", "password", "passwd", "token", "key", "credential"} {
			if strings.Contains(n, stem) {
				isCred = true
				break
			}
		}
		if !isCred {
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

/* TestEndToEndRedactionAfterCleanup 端到端：清理后各类键仍被正确掩掉 */
func TestEndToEndRedactionAfterCleanup(t *testing.T) {
	cases := map[string]string{
		"词干-裸 password": `{"password":"p"}`,
		"词干-带前缀":        `{"wxwork_secret":"s"}`,
		"词干-access_key": `{"access_key_secret":"s"}`,
		"例外-pwd":        `{"pwd":"p"}`,
		"例外-master_key": `{"master_key":"k"}`,
		"例外-code":       `{"code":"123456"}`,
		"例外-bark_key":   `{"bark_key":"bk"}`,
		"例外-public_key": `{"public_key":"pk"}`,
		"例外-email":      `{"email":"a@b.com"}`,
		"嵌套-词干":         `{"cfg":{"client_secret":"s"}}`,
		"内嵌 JSON-词干":    `{"config":"{\"SecretKey\":\"s\"}"}`,
		"内嵌 JSON-例外":    `{"config":"{\"bark_key\":\"bk\"}"}`,
	}
	for name, in := range cases {
		out := SanitizeBodyForLog(in, "application/json")
		if !strings.Contains(out, redactedMark) {
			t.Errorf("%s 未被脱敏：%s -> %s", name, in, out)
		}
	}

	// 反向：非凭据键不能被掩
	clean := `{"domain":"a.example.com","ttl":600,"record_type":"A","line":"默认"}`
	out := SanitizeBodyForLog(clean, "application/json")
	if strings.Contains(out, redactedMark) {
		t.Errorf("非凭据键被误掩：%s", out)
	}
	for _, want := range []string{"a.example.com", "600", `"A"`} {
		if !strings.Contains(out, want) {
			t.Errorf("非凭据内容 %s 丢失：%s", want, out)
		}
	}
}

/* TestExceptionTableNormalizesSeparators 例外表的 _ / - 与大小写不敏感 */
func TestExceptionTableNormalizesSeparators(t *testing.T) {
	for _, k := range []string{"sp_key", "sp-key", "SP_KEY", "SpKey"} {
		if !isSensitiveKey(k) {
			t.Errorf("%q 是 sp_key 的分隔符/大小写变体，应判为敏感", k)
		}
	}
	body, _ := json.Marshal(map[string]string{"SP-KEY": "v"})
	if out := SanitizeBodyForLog(string(body), "application/json"); !strings.Contains(out, redactedMark) {
		t.Errorf("大小写变体未被脱敏：%s", out)
	}
}
