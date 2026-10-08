package api

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"main/internal/api/handler"
	"main/internal/database"
	"main/internal/models"
)

/*
 * OAuth 前端回调链路的路由级断言
 *
 * 改造后第三方统一回跳到前端页面 /oauth-callback，页面再调 POST /api/auth/oauth/exchange。
 * 这条链路对路由注册与 CSRF 白名单有硬依赖，任一环节失效都不会编译报错，
 * 只会表现为回调页永远拿不到会话，因此在此逐条锁定
 */

// TestOAuthExchangeRouteRegistered 前端回调页依赖的交换接口必须注册为 POST
func TestOAuthExchangeRouteRegistered(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	for _, rt := range r.Routes() {
		if rt.Method == http.MethodPost && rt.Path == "/api/auth/oauth/exchange" {
			return
		}
	}
	t.Fatalf("未注册 POST /api/auth/oauth/exchange，实际路由表：%v", r.Routes())
}

// TestOAuthExchangeExemptFromCSRF 回调页的 POST 必须落在 CSRF 白名单内
//
// 跨站回跳的页面无法携带 X-CSRF-Token（该头由本站脚本读写，第三方导航不参与），
// 若被 CSRF 中间件拦下，回调页拿到的是 403 而不是可转成中文提示的业务错误码
func TestOAuthExchangeExemptFromCSRF(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/oauth/exchange",
		strings.NewReader(`{"code":"probe-code","state":"probe-state"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusForbidden {
		t.Fatalf("交换接口被 CSRF 拦截（HTTP 403）：%s", w.Body.String())
	}
	// 探针用的 state 不存在，响应应停在业务层的 invalid_state 而非中间件层
	if !strings.Contains(w.Body.String(), "invalid_state") {
		t.Errorf("响应体 = %q，期望含 invalid_state", w.Body.String())
	}
}

// TestOAuthCallbackRouteKeepsWorking 旧回调路由必须保留，供第三方后台尚未更新回调地址时兜底
func TestOAuthCallbackRouteKeepsWorking(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/auth/oauth/github/callback?code=x&state=y", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// state 不存在 → 回跳登录页并带 invalid_state；关键是不能落到 NoRoute 的 HTML 404
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("状态码 = %d，期望 307 回跳（body=%s）", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	if !strings.Contains(loc, "error=invalid_state") {
		t.Errorf("Location = %q，期望含 error=invalid_state", loc)
	}
}

// TestOAuthAuthorizeURLPointsAtFrontendCallback 授权 URL 的 redirect_uri 必须指向前端回调页
//
// 旧实现为 github 生成 {site}/api/auth/github/callback，而路由表里从来没有这条规则，
// 授权完成后必然落到 NoRoute 并返回 HTML 404
func TestOAuthAuthorizeURLPointsAtFrontendCallback(t *testing.T) {
	setupRouterTestDB(t)
	seedRouterSysConfig(t, map[string]string{
		"oauth_github_client_id":     "router-test-client-id",
		"oauth_github_client_secret": "router-test-client-secret",
		"site_url":                   "https://dns.example.com",
	})

	r, api := newProbeRouter(t)
	api.GET("/auth/oauth/:provider/login", handler.OAuthLogin)

	w := doGet(r, "/api/auth/oauth/github/login", nil)
	if w.Code != http.StatusTemporaryRedirect {
		t.Fatalf("状态码 = %d，期望 307 跳转授权页（body=%s）", w.Code, w.Body.String())
	}
	loc := w.Header().Get("Location")
	authURL, err := url.Parse(loc)
	if err != nil {
		t.Fatalf("授权 URL 解析失败：%v（%q）", err, loc)
	}
	// redirect_uri 在授权 URL 里是编码过的，必须解码后再比对，否则断言只能靠子串碰运气
	redirectURI := authURL.Query().Get("redirect_uri")
	if redirectURI != "https://dns.example.com/oauth-callback" {
		t.Errorf("redirect_uri = %q，期望 https://dns.example.com/oauth-callback", redirectURI)
	}
	if strings.Contains(redirectURI, "/api/auth/github/callback") {
		t.Errorf("redirect_uri 仍指向不存在的后端路径：%q", redirectURI)
	}
}

// seedRouterSysConfig 写入系统配置，供路由级用例准备提供商凭据与站点地址
func seedRouterSysConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
			t.Fatalf("写入配置 %s 失败：%v", k, err)
		}
	}
}