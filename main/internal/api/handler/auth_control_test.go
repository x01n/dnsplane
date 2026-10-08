package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/oauth"
)

/*
 * 登录注册控制端点（/system/auth-control）测试
 *
 * 该端点是前端 auth-control-settings.tsx 的唯一数据来源，键集合必须与前端
 * 逐个对应（前端保存时提交整个 config 对象，多一个不可写键就会整批失败）。
 *
 * 未启用加密链路时 GetDecryptedData 返回 nil，请求按明文 JSON 绑定，
 * 与其它 handler 的取数方式一致，故测试直接投递 JSON body。
 */

/*
 * TestGetAuthControlReturnsOnlyWritableKeys 只返回白名单内的键
 *
 * 关键安全断言：turnstile_secret_key / oauth_*_client_secret / site_url 等
 * 虽同为 auth% 或相邻前缀，但不得出现在返回体里
 */
func TestGetAuthControlReturnsOnlyWritableKeys(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedAuthControlConfig(t, map[string]string{
		"auth_password_login": "true",
		"auth_email_verify":   "false",
		"register_enabled":    "true",
		// 以下均不得出现在返回体
		"site_url":             "https://evil.example.com",
		"turnstile_secret_key": "0x4AAAA-secret",
		"turnstile_site_key":   "0x4AAAA-site",
		// 以下键虽以 auth_ 开头，但均为不可写项
		"auth_password_reset_dummy": "x",
	})

	// 一个真实启用的 provider，用于验证其开关可读、而其凭据不可读
	seedAuthControlConfig(t, map[string]string{
		"oauth_github_client_id":     "gh-id",
		"oauth_github_client_secret": "gh-secret",
	})
	providers := oauth.GetEnabledProviders()
	if len(providers) == 0 {
		t.Fatal("写入凭据后应有已启用 provider")
	}
	provider := providers[0].Name
	seedAuthControlConfig(t, map[string]string{
		"auth_oauth_" + provider + "_login": "true",
	})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/auth-control", nil)
	c.Set("level", 2)
	GetAuthControl(c)

	var resp struct {
		Code int               `json:"code"`
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("code = %d，期望 0", resp.Code)
	}

	mustHave := []string{"auth_password_login", "auth_email_verify", "register_enabled", "auth_oauth_" + provider + "_login"}
	for _, k := range mustHave {
		if _, ok := resp.Data[k]; !ok {
			t.Errorf("返回体缺少可写键 %s", k)
		}
	}

	mustNotHave := []string{
		"turnstile_secret_key", "turnstile_site_key",
		"oauth_github_client_secret", "oauth_github_client_id",
		"github_client_secret", "github_client_id",
		"site_url", "auth_password_reset_dummy",
	}
	for _, k := range mustNotHave {
		if v, ok := resp.Data[k]; ok {
			t.Errorf("返回体泄露了不可写键 %s = %q", k, v)
		}
	}
}

/*
 * TestGetAuthControlOmitsUnsetKeys 未配置的键不得补空串
 *
 * 前端 getBool 只在 undefined 时用默认值，补空串会让默认开启的开关显示成关闭
 */
func TestGetAuthControlOmitsUnsetKeys(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedAuthControlConfig(t, map[string]string{"auth_password_login": "true"})

	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/system/auth-control", nil)
	c.Set("level", 2)
	GetAuthControl(c)

	var resp struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if _, ok := resp.Data["auth_email_verify"]; ok {
		t.Error("未配置的 auth_email_verify 不应出现在返回体（补空串会让前端默认值失效）")
	}
}

/* TestUpdateAuthControlWritesAndInvalidates 写入成功且缓存被失效 */
func TestUpdateAuthControlWritesAndInvalidates(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedAuthControlConfig(t, map[string]string{"auth_password_login": "true"})

	// 先热一次缓存，确保失效是必需的
	if got := GetSysConfigValue("auth_password_login"); got != "true" {
		t.Fatalf("预热缓存读到 %q，期望 true", got)
	}

	c, w := authControlAdminReq(t, `{"auth_password_login":"false","auth_email_verify":"false"}`)
	UpdateAuthControl(c)

	if msg := respMsg(t, w); !strings.Contains(msg, "更新成功") {
		t.Fatalf("期望更新成功，实际 %q", msg)
	}
	if got := GetSysConfigValue("auth_password_login"); got != "false" {
		t.Errorf("写入后应读到 false，实际 %q —— 缓存未失效", got)
	}
	if got := GetSysConfigValue("auth_email_verify"); got != "false" {
		t.Errorf("新键应被创建并读到 false，实际 %q", got)
	}
}

/*
 * TestUpdateAuthControlRejectsForeignKeys 白名单外的键整批拒绝且不落库
 *
 * 「整批拒绝」是核心：若先写一部分再报错，前端会看到失败但库已被改了一半
 */
func TestUpdateAuthControlRejectsForeignKeys(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedAuthControlConfig(t, map[string]string{"auth_password_login": "true"})

	cases := []struct {
		name string
		body string
	}{
		{"含 turnstile_secret_key", `{"auth_password_login":"false","turnstile_secret_key":"x"}`},
		{"含 site_url", `{"auth_password_login":"false","site_url":"https://evil.example.com"}`},
		{"含 oauth 凭据键", `{"auth_password_login":"false","oauth_github_client_id":"x"}`},
		{"含未知 auth 键", `{"auth_password_login":"false","auth_not_a_real_key":"x"}`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := authControlAdminReq(t, tc.body)
			UpdateAuthControl(c)

			if msg := respMsg(t, w); !strings.Contains(msg, "不支持修改的配置项") {
				t.Errorf("应拒绝，实际 msg = %q", msg)
			}
			// 同批内的可写键也不得落库
			if got := GetSysConfigValue("auth_password_login"); got != "true" {
				t.Errorf("整批被拒时可写键也不得变化，实际 %q", got)
			}
		})
	}
}

/* TestUpdateAuthControlRejectsNonAdmin 普通用户不得读写 */
func TestUpdateAuthControlRejectsNonAdmin(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)

	for _, level := range []int{0, 1} {
		t.Run("level", func(t *testing.T) {
			c, w := authControlAdminReq(t, `{"auth_password_login":"false"}`)
			c.Set("level", level)
			UpdateAuthControl(c)
			if msg := respMsg(t, w); !strings.Contains(msg, "权限不足") {
				t.Errorf("level=%d 应被拒，实际 msg = %q", level, msg)
			}
		})
	}
}

/* TestUpdateAuthControlRejectsEmptyBody 空请求体返回参数错误，不做无意义写入 */
func TestUpdateAuthControlRejectsEmptyBody(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)

	c, w := authControlAdminReq(t, `{}`)
	UpdateAuthControl(c)
	if msg := respMsg(t, w); !strings.Contains(msg, "参数错误") {
		t.Errorf("空请求体应返回参数错误，实际 %q", msg)
	}
}

/*
 * TestAuthControlKeyListMatchesFrontend 键集合与前端 auth-control-settings.tsx 逐项对齐
 *
 * 前端读取的固定键共七个，另有动态的 auth_oauth_<provider>_login|register。
 * 少一个 → 前端开关保存时报错；多一个 → 读到的值前端用不上
 */
func TestAuthControlKeyListMatchesFrontend(t *testing.T) {
	want := []string{
		"auth_password_login",
		"auth_password_register",
		"auth_email_verify",
		"auth_email_whitelist_enabled",
		"auth_email_whitelist",
		"register_enabled",
	}
	have := make(map[string]bool)
	for _, k := range authControlFixedKeys() {
		have[k] = true
	}
	for _, k := range want {
		if !have[k] {
			t.Errorf("前端读取的键 %s 不在后端可写清单内", k)
		}
	}
	if !have["auth_magic_link_login"] {
		t.Error("auth_magic_link_login 由 GetAuthConfig 下发、属本页面语义范围，应在清单内")
	}
}

/*
 * TestIsWritableAuthControlKeyOAuthDynamic OAuth 开关按 provider 动态放行
 *
 * 用 oauth 包真实返回的 provider 名，而非硬编码字符串：
 * 一旦键名拼法与 oauth.GetEnabledProviders 分叉，本用例立即失败
 */
func TestIsWritableAuthControlKeyOAuthDynamic(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedAuthControlConfig(t, map[string]string{
		"oauth_github_client_id":     "id",
		"oauth_github_client_secret": "secret",
	})

	providers := oauth.GetEnabledProviders()
	if len(providers) == 0 {
		t.Fatal("写入凭据后应有已启用 provider")
	}
	name := providers[0].Name

	if !isWritableAuthControlKey("auth_oauth_" + name + "_login") {
		t.Errorf("auth_oauth_%s_login 应可写", name)
	}
	if !isWritableAuthControlKey("auth_oauth_" + name + "_register") {
		t.Errorf("auth_oauth_%s_register 应可写", name)
	}
	if isWritableAuthControlKey("auth_oauth_" + name + "_password") {
		t.Errorf("auth_oauth_%s_password 不是已知后缀，不应可写", name)
	}
	if isWritableAuthControlKey("auth_oauth_not_a_provider_login") {
		t.Error("未启用的 provider 不应可写")
	}
}

/* TestAuthControlRoundTrip 读→改→读的完整往返，模拟前端保存流程 */
func TestAuthControlRoundTrip(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedAuthControlConfig(t, map[string]string{
		"auth_password_login": "true",
		"auth_email_verify":   "true",
	})

	// 读
	w := httptest.NewRecorder()
	c1, _ := gin.CreateTestContext(w)
	c1.Request = httptest.NewRequest(http.MethodGet, "/api/system/auth-control", nil)
	c1.Set("level", 2)
	GetAuthControl(c1)

	var got struct {
		Data map[string]string `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}

	// 改：前端提交整个 config 对象，这里回投读到的全部键
	body := `{"auth_password_login":"false"}`
	for k := range got.Data {
		if k != "auth_password_login" {
			body = strings.TrimSuffix(body, "}") + `,"` + k + `":"` + got.Data[k] + `"}`
		}
	}
	c2, w2 := authControlAdminReq(t, body)
	UpdateAuthControl(c2)
	if msg := respMsg(t, w2); !strings.Contains(msg, "更新成功") {
		t.Fatalf("回投读到的全部键应能保存成功，实际 %q", msg)
	}

	// 再读
	if got := GetSysConfigValue("auth_password_login"); got != "false" {
		t.Errorf("往返后 auth_password_login 应为 false，实际 %q", got)
	}
}
