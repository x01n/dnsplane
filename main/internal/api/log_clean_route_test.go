package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * 清空操作日志的路由回归用例
 *
 * 该 POST 此前未注册，被 NoRoute 逐级回落到静态页，前端解析 HTML 失败后提示「清空失败」。
 * 这里从路由表与真实请求两条路径锁定它必须落在 /api 的 JSON 语义上
 */

// TestOperationLogCleanRouteRegistered 路由表必须存在 POST /api/logs/clean
func TestOperationLogCleanRouteRegistered(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	for _, rt := range r.Routes() {
		if rt.Method == http.MethodPost && rt.Path == "/api/logs/clean" {
			return
		}
	}
	t.Fatal("路由表中不存在 POST /api/logs/clean")
}

// TestOperationLogCleanRouteReturnsJSON 越过 CSRF 后必须由鉴权中间件回答 JSON 401，
// 而不是 NoRoute 回落的静态页 HTML
func TestOperationLogCleanRouteReturnsJSON(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	// double-submit：cookie 与同名请求头同值
	const token = "0123456789abcdef0123456789abcdef"
	req := httptest.NewRequest(http.MethodPost, "/api/logs/clean", strings.NewReader(`{"days":0}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-CSRF-Token", token)
	req.AddCookie(&http.Cookie{Name: "_csrf", Value: token})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Fatalf("状态码 = %d，期望 401（未登录）", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Errorf("Content-Type = %q，期望 JSON", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, `"code":401`) {
		t.Errorf("响应体 = %q，期望鉴权中间件的 JSON 401", body)
	}
}

// TestOperationLogCleanRequiresCSRFToken 缺少 CSRF 令牌时必须在到达 handler 前被拦下
func TestOperationLogCleanRequiresCSRFToken(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	req := httptest.NewRequest(http.MethodPost, "/api/logs/clean", strings.NewReader(`{"days":0}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusForbidden {
		t.Fatalf("状态码 = %d，期望 403", w.Code)
	}
	if body := w.Body.String(); !strings.Contains(body, "CSRF") {
		t.Errorf("响应体 = %q，期望 CSRF 校验失败的说明", body)
	}
}
