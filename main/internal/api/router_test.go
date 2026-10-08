package api

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"

	"github.com/gin-gonic/gin"
)

// testStaticFS 构造与生产 embed 结构一致的内存文件系统（顶层 web 目录）
func testStaticFS() fstest.MapFS {
	return fstest.MapFS{
		"web/login/index.html": &fstest.MapFile{Data: []byte("<html>login</html>")},
		"web/dashboard/index.html": &fstest.MapFile{
			Data: []byte("<html>dashboard</html>"),
		},
		"web/forgot-password/index.html": &fstest.MapFile{
			Data: []byte("<html>forgot</html>"),
		},
		"web/forgot-totp/index.html": &fstest.MapFile{
			Data: []byte("<html>forgot totp</html>"),
		},
		"web/reset-password/index.html": &fstest.MapFile{
			Data: []byte("<html>reset</html>"),
		},
		"web/reset-totp/index.html": &fstest.MapFile{
			Data: []byte("<html>reset totp</html>"),
		},
		"web/_not-found/index.html": &fstest.MapFile{Data: []byte("<html>404</html>")},
		"web/login/index.txt":       &fstest.MapFile{Data: []byte(`1:"$Sreact.fragment"`)},
		"web/login/chunk.js":        &fstest.MapFile{Data: []byte("console.log(1)")},
		"web/login/other.js":        &fstest.MapFile{Data: []byte("console.log(2)")},
		"web/login/theme.css":       &fstest.MapFile{Data: []byte("body{margin:0}")},
	}
}

// newTestRouter 以测试模式构建路由，避免 gin 调试输出干扰断言
func newTestRouter(t *testing.T) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	return setupRouter(testStaticFS())
}

// doGet 发起一次请求并返回响应记录器
func doGet(r *gin.Engine, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

// setupRouterTestDB 初始化临时数据库与缓存。
// RequestTrace 会在 c.Next 返回后异步把请求日志落到 RequestDB，
// 不初始化会让该 goroutine 对 nil *gorm.DB 解引用、在测试结束时 panic
func setupRouterTestDB(t *testing.T) {
	t.Helper()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "router.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

// assertSecurityHeaders 断言响应携带 SecurityHeaders 中间件写入的全部安全头
func assertSecurityHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	want := map[string]string{
		"X-Content-Type-Options":  "nosniff",
		"X-Frame-Options":         "SAMEORIGIN",
		"X-XSS-Protection":        "1; mode=block",
		"Referrer-Policy":         "strict-origin-when-cross-origin",
		"Permissions-Policy":      "camera=(), microphone=(), geolocation=()",
		"Content-Security-Policy": "default-src 'self';",
	}
	for k, v := range want {
		got := w.Header().Get(k)
		if k == "Content-Security-Policy" {
			if !strings.HasPrefix(got, v) {
				t.Errorf("Content-Security-Policy = %q，期望以 %q 开头", got, v)
			}
			continue
		}
		if got != v {
			t.Errorf("%s = %q，期望 %q", k, got, v)
		}
	}
}

// captchaOrigins 是 CSP 为三家第三方验证码 SDK 放行的来源域
var captchaOrigins = []string{
	"https://challenges.cloudflare.com",
	"https://www.google.com",
	"https://js.hcaptcha.com",
}

// cspMustContain 断言 CSP 的某个取指令覆盖了全部验证码域，且未回落到 default-src
func cspMustContain(t *testing.T, csp, directive string) {
	t.Helper()
	idx := strings.Index(csp, directive+" ")
	if idx < 0 {
		t.Fatalf("CSP 缺少指令 %s：%s", directive, csp)
	}
	// 取指令段：从指令名到下一个分号
	rest := csp[idx+len(directive)+1:]
	if end := strings.Index(rest, ";"); end >= 0 {
		rest = rest[:end]
	} else {
		rest = strings.TrimSuffix(rest, ";")
	}
	for _, origin := range captchaOrigins {
		if !strings.Contains(rest, origin) {
			t.Errorf("CSP 的 %s 未放行 %s：%s", directive, origin, rest)
		}
	}
}

// TestCSPAllowsCaptchaOrigins 校验 CSP 为三家验证码 SDK 放行脚本、iframe 与请求来源
//
// 三家 SDK 均以 createElement('script') 加载外部脚本并渲染进跨源 iframe，
// 仅靠 'unsafe-inline' 无法授权外部 src；frame-src 缺失时还会回落 default-src 'self'
func TestCSPAllowsCaptchaOrigins(t *testing.T) {
	r := newTestRouter(t)
	w := doGet(r, "/login/", nil)

	csp := w.Header().Get("Content-Security-Policy")
	if csp == "" {
		t.Fatal("Content-Security-Policy 为空")
	}
	cspMustContain(t, csp, "script-src")
	cspMustContain(t, csp, "frame-src")
	cspMustContain(t, csp, "connect-src")

	// 未放行的第三方域必须仍在白名单之外
	for _, origin := range []string{"https://evil.example", "https://cdn.jsdelivr.net"} {
		if strings.Contains(csp, origin) {
			t.Errorf("CSP 不应包含 %s：%s", origin, csp)
		}
	}
}

// TestSecurityHeadersOnStaticAsset 校验静态资源（HTML/JS/CSS）同样带安全响应头
func TestSecurityHeadersOnStaticAsset(t *testing.T) {
	r := newTestRouter(t)

	for _, target := range []string{"/login/", "/login/chunk.js", "/login/theme.css"} {
		w := doGet(r, target, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d，期望 200", target, w.Code)
		}
		assertSecurityHeaders(t, w)
	}
}

// TestSecurityHeadersOnAPI 校验 /api 路由同样带安全响应头
func TestSecurityHeadersOnAPI(t *testing.T) {
	setupRouterTestDB(t)
	r := newTestRouter(t)

	targets := []string{
		"/api/csrf",
		"/api/auth/captcha",
		"/api/system/config",
		"/api/user/info",
		"/api/not/registered",
	}
	for _, target := range targets {
		w := doGet(r, target, nil)
		assertSecurityHeaders(t, w)
	}
}

// TestSecurityHeadersOnRedirectAndNotFound 校验 302 与 NoRoute 回落页也带安全响应头
func TestSecurityHeadersOnRedirectAndNotFound(t *testing.T) {
	r := newTestRouter(t)

	redirect := doGet(r, "/", nil)
	if redirect.Code != http.StatusFound {
		t.Fatalf("根路径状态码 = %d，期望 302", redirect.Code)
	}
	assertSecurityHeaders(t, redirect)

	missing := doGet(r, "/no/such/page", nil)
	if missing.Code != http.StatusOK {
		t.Fatalf("未命中路径状态码 = %d，期望回落 200", missing.Code)
	}
	assertSecurityHeaders(t, missing)
}

// TestHSTSRequiresHTTPS 校验 HSTS 仅在直连 TLS 或 X-Forwarded-Proto: https 时下发
func TestHSTSRequiresHTTPS(t *testing.T) {
	r := newTestRouter(t)

	plain := doGet(r, "/login/", nil)
	if got := plain.Header().Get("Strict-Transport-Security"); got != "" {
		t.Errorf("明文请求 Strict-Transport-Security = %q，期望不下发", got)
	}

	for _, scheme := range []string{"https", "HTTPS"} {
		w := doGet(r, "/login/", map[string]string{"X-Forwarded-Proto": scheme})
		if got := w.Header().Get("Strict-Transport-Security"); got != "max-age=31536000" {
			t.Errorf("X-Forwarded-Proto=%s 时 Strict-Transport-Security = %q，期望 %q",
				scheme, got, "max-age=31536000")
		}
	}
}

// probeRouteForPanic 是仅供安全头测试注册的 panic 路由路径
const probeRouteForPanic = "/__panic_probe"

// TestSecurityHeadersSurvivePanic 校验业务 panic 后 gin.Recovery 写出的 500 仍带安全响应头
func TestSecurityHeadersSurvivePanic(t *testing.T) {
	r := newTestRouter(t)
	r.GET(probeRouteForPanic, func(*gin.Context) { panic("security headers probe") })

	w := doGet(r, probeRouteForPanic, nil)
	if w.Code != http.StatusInternalServerError {
		t.Fatalf("panic 路由状态码 = %d，期望 500", w.Code)
	}
	assertSecurityHeaders(t, w)
}

// TestHTMLCacheControl HTML 必须回源校验，否则会引用已被替换的旧 chunk
func TestHTMLCacheControl(t *testing.T) {
	r := newTestRouter(t)
	w := doGet(r, "/login/index.html", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q，期望 %q", got, "no-cache")
	}
}

// TestRSCTxtCacheControl Next.js RSC payload 文件名不含内容哈希，与 HTML 同等对待
func TestRSCTxtCacheControl(t *testing.T) {
	r := newTestRouter(t)
	w := doGet(r, "/login/index.txt", nil)

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", w.Code)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("Cache-Control = %q，期望 %q", got, "no-cache")
	}
}

// TestHashedAssetCacheControl 带内容哈希的构建产物使用一年强缓存
func TestHashedAssetCacheControl(t *testing.T) {
	r := newTestRouter(t)
	for _, path := range []string{"/login/chunk.js", "/login/theme.css"} {
		w := doGet(r, path, nil)
		if w.Code != http.StatusOK {
			t.Fatalf("%s 状态码 = %d，期望 200", path, w.Code)
		}
		if got := w.Header().Get("Cache-Control"); got != "public, max-age=31536000" {
			t.Errorf("%s Cache-Control = %q，期望 %q", path, got, "public, max-age=31536000")
		}
	}
}

// TestETagFormat ETag 必须用双引号包裹且为强校验器（无 W/ 前缀）
func TestETagFormat(t *testing.T) {
	r := newTestRouter(t)
	w := doGet(r, "/login/index.html", nil)

	etag := w.Header().Get("ETag")
	if etag == "" {
		t.Fatal("ETag 为空")
	}
	if len(etag) < 2 || etag[0] != '"' || etag[len(etag)-1] != '"' {
		t.Errorf("ETag = %q，未用双引号包裹", etag)
	}
	if len(etag) >= 2 && etag[:2] == "W/" {
		t.Errorf("ETag = %q，期望强校验器而非弱校验器", etag)
	}
}

// TestConditionalRequestNotModified 携带匹配的 If-None-Match 时返回 304 且无响应体
func TestConditionalRequestNotModified(t *testing.T) {
	r := newTestRouter(t)

	first := doGet(r, "/login/index.html", nil)
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("首次响应未携带 ETag")
	}

	second := doGet(r, "/login/index.html", map[string]string{"If-None-Match": etag})
	if second.Code != http.StatusNotModified {
		t.Fatalf("状态码 = %d，期望 304", second.Code)
	}
	if body := second.Body.String(); body != "" {
		t.Errorf("304 响应体 = %q，期望空", body)
	}
	if got := second.Header().Get("ETag"); got != etag {
		t.Errorf("304 响应 ETag = %q，期望 %q", got, etag)
	}
	if got := second.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("304 响应 Cache-Control = %q，期望 %q", got, "no-cache")
	}
}

// TestConditionalRequestNonMatching 不匹配的 If-None-Match 必须回落到完整响应
func TestConditionalRequestNonMatching(t *testing.T) {
	r := newTestRouter(t)
	w := doGet(r, "/login/chunk.js", map[string]string{"If-None-Match": `"deadbeef"`})

	if w.Code != http.StatusOK {
		t.Fatalf("状态码 = %d，期望 200", w.Code)
	}
	if w.Body.Len() == 0 {
		t.Error("响应体为空，期望完整内容")
	}
}

// TestETagStableForSameContent 同一内容的 ETag 在多次请求间保持稳定
func TestETagStableForSameContent(t *testing.T) {
	r := newTestRouter(t)

	first := doGet(r, "/login/chunk.js", nil).Header().Get("ETag")
	second := doGet(r, "/login/chunk.js", nil).Header().Get("ETag")

	if first == "" || second == "" {
		t.Fatalf("ETag 为空: first=%q second=%q", first, second)
	}
	if first != second {
		t.Errorf("同一文件的 ETag 不稳定: %q != %q", first, second)
	}
}

// TestETagDiffersByContent 不同内容的 ETag 必须不同
func TestETagDiffersByContent(t *testing.T) {
	r := newTestRouter(t)

	jsETag := doGet(r, "/login/chunk.js", nil).Header().Get("ETag")
	otherETag := doGet(r, "/login/other.js", nil).Header().Get("ETag")
	htmlETag := doGet(r, "/login/index.html", nil).Header().Get("ETag")

	if jsETag == otherETag {
		t.Errorf("不同文件的 ETag 相同: %q", jsETag)
	}
	if jsETag == htmlETag {
		t.Errorf("JS 与 HTML 的 ETag 相同: %q", jsETag)
	}
}

// TestETagTracksContentChange 内容变更后 ETag 必须随之变化
func TestETagTracksContentChange(t *testing.T) {
	gin.SetMode(gin.TestMode)
	mfs := testStaticFS()
	r := setupRouter(mfs)

	before := doGet(r, "/login/chunk.js", nil).Header().Get("ETag")

	mfs["web/login/chunk.js"].Data = []byte("console.log(999)")
	after := doGet(r, "/login/chunk.js", nil).Header().Get("ETag")

	if before == after {
		t.Errorf("内容变更后 ETag 未变化: %q", before)
	}
}

// TestNoRouteBehaviors 静态服务的回落链与根路径跳转语义保持不变
func TestNoRouteBehaviors(t *testing.T) {
	r := newTestRouter(t)

	root := doGet(r, "/", nil)
	if root.Code != http.StatusFound || root.Header().Get("Location") != "/login" {
		t.Errorf("根路径 = %d Location=%q，期望 302 -> /login",
			root.Code, root.Header().Get("Location"))
	}

	missing := doGet(r, "/no/such/page", nil)
	if missing.Code != http.StatusOK {
		t.Errorf("未命中路径状态码 = %d，期望回落 _not-found 页面 200", missing.Code)
	}
	if got := missing.Header().Get("Cache-Control"); got != "no-cache" {
		t.Errorf("未命中页面 Cache-Control = %q，期望 %q", got, "no-cache")
	}

	dynamic := doGet(r, "/dashboard/", nil)
	if dynamic.Code != http.StatusOK {
		t.Errorf("目录路径状态码 = %d，期望 200", dynamic.Code)
	}
}
