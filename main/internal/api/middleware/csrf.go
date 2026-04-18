package middleware

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// CSRF 采用 double-submit cookie 模式（无服务端状态）：
//   - Cookie 名：_csrf，值为 32 字节随机 hex；非 HttpOnly 以便前端 JS 读取
//   - 请求头：X-CSRF-Token，前端每次非 GET 请求必须回显 cookie 值
//   - 中间件使用 subtle.ConstantTimeCompare 比较两者一致
//   - SameSite=Strict + Secure(HTTPS) 双重防御
//
// GET/HEAD/OPTIONS 视为安全方法不校验，同时按需写入 cookie 让前端拿到 token。
// OAuth 回调、magic-link verify 等跨站返回场景会被跳过（详见 csrfSkipPrefixes）。

const (
	csrfCookie = "_csrf"
	csrfHeader = "X-CSRF-Token"
)

// 这些前缀不参与 CSRF 校验：
//   - 跨站第三方回调（OAuth provider 的 302 回跳天然没法带自定义 header）
//   - 未认证即可调用的公开接口（登录本身不具备 CSRF 价值，因为攻击者无法借此执行受害者的已授权操作）
var csrfSkipPrefixes = []string{
	"/api/auth/oauth/", // OAuth provider callback
	"/api/quicklogin",  // 域名快速登录跳转
}

// 完全放行的精确路径（无认证即可调用的）
var csrfSkipExact = map[string]struct{}{
	"/api/csrf":                   {}, // 发 token 自身
	"/api/login":                  {}, // 登录
	"/api/install":                {}, // 首次安装
	"/api/install/status":         {},
	"/api/auth/captcha":           {},
	"/api/auth/config":            {},
	"/api/auth/forgot-password":   {},
	"/api/auth/reset-password":    {},
	"/api/auth/forgot-totp":       {},
	"/api/auth/reset-totp":        {},
	"/api/auth/refresh":           {}, // refresh 自带一次性 JTI，单独保护
	"/api/auth/send-code":         {},
	"/api/auth/register":          {},
	"/api/auth/magic-link":        {},
	"/api/auth/magic-link/totp":   {},
	"/api/auth/oauth/providers":   {},
}

func newCSRFToken() string {
	b := make([]byte, 32)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// issueCSRFCookie 向响应写入 _csrf cookie（非 HttpOnly）。
func issueCSRFCookie(c *gin.Context, token string) {
	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteStrictMode)
	// 非 HttpOnly：前端必须能读出来回传到 header；SameSite=Strict 抵御主流 CSRF 场景
	c.SetCookie(csrfCookie, token, 7*24*3600, "/", "", secure, false)
}

// CSRF 返回中间件实例。
func CSRF() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// 非 /api 路由不参与（router.go 已限定 /api 组使用此中间件）
		if !strings.HasPrefix(path, "/api") {
			c.Next()
			return
		}

		method := c.Request.Method
		existing, _ := c.Cookie(csrfCookie)

		// 安全方法：补发 cookie 后放行
		if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
			if existing == "" {
				issueCSRFCookie(c, newCSRFToken())
			}
			c.Next()
			return
		}

		// 白名单放行
		if _, ok := csrfSkipExact[path]; ok {
			c.Next()
			return
		}
		for _, p := range csrfSkipPrefixes {
			if strings.HasPrefix(path, p) {
				c.Next()
				return
			}
		}

		headerTok := c.GetHeader(csrfHeader)
		if existing == "" || headerTok == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "CSRF token 缺失"})
			return
		}
		if subtle.ConstantTimeCompare([]byte(existing), []byte(headerTok)) != 1 {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": 403, "msg": "CSRF token 不匹配"})
			return
		}
		c.Next()
	}
}

// IssueCSRFHandler 提供给路由使用：返回当前 token 并确保 cookie 已写入。
// GET /api/csrf → {"code":0,"data":{"token":"..."}}
func IssueCSRFHandler(c *gin.Context) {
	tok, _ := c.Cookie(csrfCookie)
	if tok == "" {
		tok = newCSRFToken()
		issueCSRFCookie(c, tok)
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{"token": tok},
	})
}
