package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"main/internal/database"
	"main/internal/models"
)

/*
 * OAuth 交换接口（POST /api/auth/oauth/exchange）行为测试
 *
 * 第三方授权完成后回跳到前端页面 /oauth-callback，页面读出 code 与 state 后调该接口换会话。
 * 这里用本地 httptest 服务充当第三方令牌与用户信息端点，custom 提供商的 token_url /
 * userinfo_url 全部来自系统配置，因此不必给生产代码开测试钩子就能跑完整链路
 *
 * 两条硬约束由本文件锁定：
 *   - 响应体不得出现任何令牌明文（请求日志中间件会截取成功响应体入库）
 *   - state 一次性消费，重放必须失败
 */

/* mockOAuthUpstream 启动一个假第三方：POST /token 发令牌，GET /userinfo 返回用户 */
func mockOAuthUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"mock-access-token","refresh_token":"mock-refresh-token","expires_in":3600,"token_type":"Bearer"}`))
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"sub":"mock-user-1","name":"Mock User","email":"mock@example.com","picture":"https://example.com/avatar.png"}`))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

/* seedMockCustomProvider 写入指向假第三方的 custom 提供商配置 */
func seedMockCustomProvider(t *testing.T, srv *httptest.Server) {
	t.Helper()
	seedConfig(t, map[string]string{
		"oauth_custom_client_id":     "mock-client-id",
		"oauth_custom_client_secret": "mock-client-secret",
		"oauth_custom_name":          "MockOAuth",
		"oauth_custom_authorize_url": srv.URL + "/authorize",
		"oauth_custom_token_url":     srv.URL + "/token",
		"oauth_custom_userinfo_url":  srv.URL + "/userinfo",
	})
}

/* seedBoundUser 建一个已与指定第三方账号绑定的用户 */
func seedBoundUser(t *testing.T, provider, providerUserID string) models.User {
	t.Helper()
	user := models.User{
		Username: "bound-oauth-user",
		Email:    "bound@example.com",
		Level:    1,
		Status:   1,
		RegTime:  time.Now(),
	}
	if err := database.DB.Create(&user).Error; err != nil {
		t.Fatalf("创建用户失败：%v", err)
	}
	if err := database.DB.Create(&models.UserOAuth{
		UserID:         user.ID,
		Provider:       provider,
		ProviderUserID: providerUserID,
	}).Error; err != nil {
		t.Fatalf("创建绑定失败：%v", err)
	}
	return user
}

/* exchangeResp 交换接口的响应结构 */
type exchangeResp struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		Mode     string `json:"mode"`
		Redirect string `json:"redirect"`
	} `json:"data"`
}

/* decodeExchangeResp 解析交换接口响应 */
func decodeExchangeResp(t *testing.T, w *httptest.ResponseRecorder) exchangeResp {
	t.Helper()
	var out exchangeResp
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败：%v（原文：%s）", err, w.Body.String())
	}
	return out
}

/* TestOAuthExchangeRejectsMissingParams 缺少 code 或 state 时必须回 invalid_request */
func TestOAuthExchangeRejectsMissingParams(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)

	cases := []struct {
		name string
		body map[string]string
	}{
		{"两者都缺", map[string]string{}},
		{"缺 state", map[string]string{"code": "c"}},
		{"缺 code", map[string]string{"state": "s"}},
		{"空白字符不算有效值", map[string]string{"code": "  ", "state": "  "}},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange", c.body)
			got := decodeExchangeResp(t, w)
			if got.Code != -1 || got.Msg != "invalid_request" {
				t.Errorf("响应 = %+v，期望 code=-1 msg=invalid_request", got)
			}
		})
	}
}

/* TestOAuthExchangeRejectsUnknownState 未知 state 回 invalid_state */
func TestOAuthExchangeRejectsUnknownState(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)

	w := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "c", "state": "never-issued"})
	got := decodeExchangeResp(t, w)
	if got.Code != -1 || got.Msg != "invalid_state" {
		t.Errorf("响应 = %+v，期望 code=-1 msg=invalid_state", got)
	}
}

/* TestOAuthExchangeStateIsSingleUse state 一次性消费，第二次调用必须失败 */
func TestOAuthExchangeStateIsSingleUse(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	srv := mockOAuthUpstream(t)
	seedMockCustomProvider(t, srv)
	seedBoundUser(t, "custom", "mock-user-1")

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: "custom", Mode: "login"}); err != nil {
		t.Fatalf("写入 state 失败：%v", err)
	}

	first := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "mock-code", "state": state})
	if got := decodeExchangeResp(t, first); got.Code != 0 {
		t.Fatalf("首次交换应成功，实际 %+v（原文：%s）", got, first.Body.String())
	}

	second := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "mock-code", "state": state})
	got := decodeExchangeResp(t, second)
	if got.Code != -1 || got.Msg != "invalid_state" {
		t.Errorf("重放响应 = %+v，期望 code=-1 msg=invalid_state", got)
	}
}

/* TestOAuthExchangeLoginSuccess 已绑定用户走登录分支：下发 cookie，响应体不含令牌明文 */
func TestOAuthExchangeLoginSuccess(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	srv := mockOAuthUpstream(t)
	seedMockCustomProvider(t, srv)
	user := seedBoundUser(t, "custom", "mock-user-1")

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: "custom", Mode: "login"}); err != nil {
		t.Fatalf("写入 state 失败：%v", err)
	}

	w := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "mock-code", "state": state})

	got := decodeExchangeResp(t, w)
	if got.Code != 0 {
		t.Fatalf("响应 = %+v，期望成功（原文：%s）", got, w.Body.String())
	}
	if got.Data.Mode != "login" {
		t.Errorf("mode = %q，期望 login", got.Data.Mode)
	}
	if got.Data.Redirect != "/dashboard/" {
		t.Errorf("redirect = %q，期望 /dashboard/", got.Data.Redirect)
	}

	// 凭据绝不能进响应体：RequestTrace 会把成功响应体截取进 request_logs
	raw := w.Body.String()
	for _, leaked := range []string{"mock-access-token", "mock-refresh-token"} {
		if strings.Contains(raw, leaked) {
			t.Errorf("响应体泄露令牌 %q：%s", leaked, raw)
		}
	}

	cookies := w.Result().Cookies()
	names := map[string]bool{}
	for _, ck := range cookies {
		names[ck.Name] = true
		if !ck.HttpOnly {
			t.Errorf("cookie %s 未设置 HttpOnly", ck.Name)
		}
	}
	if !names["_t"] || !names["_rt"] {
		t.Errorf("cookie 集合 = %v，期望同时含 _t 与 _rt", names)
	}

	// 第三方令牌应被写回绑定记录，供后续调用第三方接口使用
	var binding models.UserOAuth
	if err := database.DB.Where("user_id = ? AND provider = ?", user.ID, "custom").First(&binding).Error; err != nil {
		t.Fatalf("查询绑定失败：%v", err)
	}
	if binding.AccessToken != "mock-access-token" {
		t.Errorf("绑定记录 AccessToken = %q，期望 mock-access-token", binding.AccessToken)
	}
}

/* TestOAuthExchangeBindSuccess 绑定模式回 bind 信号与个人资料页落点 */
func TestOAuthExchangeBindSuccess(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	srv := mockOAuthUpstream(t)
	seedMockCustomProvider(t, srv)

	user := models.User{
		Username: "bind-target", Email: "bind@example.com", Level: 1, Status: 1, RegTime: time.Now(),
	}
	if err := database.DB.Create(&user).Error; err != nil {
		t.Fatalf("创建用户失败：%v", err)
	}

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: "custom", Mode: "bind", BindUID: user.ID}); err != nil {
		t.Fatalf("写入 state 失败：%v", err)
	}

	w := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "mock-code", "state": state})

	got := decodeExchangeResp(t, w)
	if got.Code != 0 {
		t.Fatalf("响应 = %+v，期望成功（原文：%s）", got, w.Body.String())
	}
	if got.Data.Mode != "bind" || got.Data.Redirect != "/dashboard/profile?bind=success" {
		t.Errorf("响应 data = %+v，期望 bind 与 /dashboard/profile?bind=success", got.Data)
	}
	// 绑定不建立新会话，不应下发认证 cookie
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "_t" || ck.Name == "_rt" {
			t.Errorf("绑定流程不应下发 %s cookie", ck.Name)
		}
	}

	var count int64
	database.DB.Model(&models.UserOAuth{}).Where("user_id = ? AND provider = ?", user.ID, "custom").Count(&count)
	if count != 1 {
		t.Errorf("绑定记录数 = %d，期望 1", count)
	}
}

/* TestOAuthExchangeBindRejectsOtherUserAccount 第三方账号已被他人绑定时回 already_bound_other */
func TestOAuthExchangeBindRejectsOtherUserAccount(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	srv := mockOAuthUpstream(t)
	seedMockCustomProvider(t, srv)
	seedBoundUser(t, "custom", "mock-user-1")

	another := models.User{
		Username: "another-user", Email: "another@example.com", Level: 1, Status: 1, RegTime: time.Now(),
	}
	if err := database.DB.Create(&another).Error; err != nil {
		t.Fatalf("创建用户失败：%v", err)
	}

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: "custom", Mode: "bind", BindUID: another.ID}); err != nil {
		t.Fatalf("写入 state 失败：%v", err)
	}

	w := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "mock-code", "state": state})
	got := decodeExchangeResp(t, w)
	if got.Code != -1 || got.Msg != "already_bound_other" {
		t.Errorf("响应 = %+v，期望 code=-1 msg=already_bound_other", got)
	}
}

/* TestOAuthExchangeTokenFailure token 端点报错时回 token_failed 且不下发 cookie */
func TestOAuthExchangeTokenFailure(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()
	seedConfig(t, map[string]string{
		"oauth_custom_client_id":     "mock-client-id",
		"oauth_custom_client_secret": "mock-client-secret",
		"oauth_custom_token_url":     srv.URL,
		"oauth_custom_userinfo_url":  srv.URL,
	})

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: "custom", Mode: "login"}); err != nil {
		t.Fatalf("写入 state 失败：%v", err)
	}

	w := postJSON(t, OAuthExchange, "/api/auth/oauth/exchange",
		map[string]string{"code": "mock-code", "state": state})
	got := decodeExchangeResp(t, w)
	if got.Code != -1 || got.Msg != "token_failed" {
		t.Errorf("响应 = %+v，期望 code=-1 msg=token_failed", got)
	}
	for _, ck := range w.Result().Cookies() {
		if ck.Name == "_t" || ck.Name == "_rt" {
			t.Errorf("令牌交换失败时不应下发 %s cookie", ck.Name)
		}
	}
}

/*
 * TestOAuthCallbackRejectsProviderMismatch 兼容路径的 provider 必须与 state 记录一致
 *
 * 旧回调地址带 provider 路径参数，state 里也记着 provider，两者不符说明有人把 state
 * 拿到了别的提供商分支上，必须拒绝而不是按路径参数继续
 */
func TestOAuthCallbackRejectsProviderMismatch(t *testing.T) {
	seedOAuthTestDB(t)
	clearSysConfig(t)
	srv := mockOAuthUpstream(t)
	seedMockCustomProvider(t, srv)

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: "custom", Mode: "login"}); err != nil {
		t.Fatalf("写入 state 失败：%v", err)
	}

	path := "/api/auth/oauth/github/callback?code=mock-code&state=" + url.QueryEscape(state)
	w := getWithParam(t, OAuthCallback, path, "provider", "github")

	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("状态码 = %d，期望 307 回跳", w.Code)
	}
	if loc := w.Header().Get("Location"); !strings.Contains(loc, "error=invalid_state") {
		t.Errorf("Location = %q，期望含 error=invalid_state", loc)
	}
}
