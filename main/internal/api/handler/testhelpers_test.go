package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
	"main/internal/oauth"
)

/*
 * handler 包测试助手集中定义
 *
 * 这里放「与环境打交道」的通用助手：建库、写种子数据、拼请求上下文。
 * 它们此前散落在六个 _test.go 里，而同包所有测试文件共用一个命名空间，
 * 谁再加一个同名助手都会编译期撞车（本包已发生过 setupHandlerTestDB 重复定义）。
 * 集中后新增助手前只需看这一处即可确认是否重名
 *
 * 有意未搬入的领域专用助手，随各自用例留在原文件：
 *   qqbot_test.go              callQQBotWebhook / signQQBotBody / initQQBotTestEnv /
 *                              setQQBotTestConfig / setupQQBotTest
 *   captcha_test.go            stubCaptchaUpstream / getAuthConfigData / assertStringField
 *   notify_test_endpoint_test.go  notifierConfigField / notifierConfigMapField / notifierConfigValue
 *
 * 新增助手前请连带上表一起查重
 */

/* setupHandlerTestDB 初始化临时 SQLite、内存缓存与 gin 测试模式 */
func setupHandlerTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "handler.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* seedOAuthTestDB 初始化临时库与缓存（OAuth 用例不建用户） */
func seedOAuthTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "oauth.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* clearSysConfig 清空全部系统配置，保证用例之间互不影响 */
func clearSysConfig(t *testing.T) {
	t.Helper()
	if err := database.DB.Where("1 = 1").Delete(&models.SysConfig{}).Error; err != nil {
		t.Fatalf("清空配置失败: %v", err)
	}
}

/* seedConfig 逐条写入系统配置 */
func seedConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
			t.Fatalf("写入配置 %s 失败: %v", k, err)
		}
	}
}

/* seedAuthControlConfig 写入若干配置项（登录注册控制用例的种子） */
func seedAuthControlConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		if err := database.DB.Create(&models.SysConfig{Key: k, Value: v}).Error; err != nil {
			t.Fatalf("写入配置 %s 失败: %v", k, err)
		}
	}
}

/* seedUser 写入一个可登录用户，返回其明文密码 */
func seedUser(t *testing.T, username string) string {
	t.Helper()
	const password = "Str0ng!Passw0rd"
	hashed, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		t.Fatalf("生成密码哈希失败: %v", err)
	}
	if err := database.DB.Create(&models.User{
		Username: username,
		Password: string(hashed),
		Email:    username + "@example.com",
		Level:    1,
		Status:   1,
		RegTime:  time.Now(),
	}).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return password
}

/*
 * seedProvider 写入一个提供商的凭据配置使其进入「已启用」集合，返回其 Name()
 *
 * GetEnabledProviders 要求提供商有 client_id 等凭据才会返回，
 * 因此不能只写开关键——那样列表为空，用例会 Skip 而非真正断言
 */
func seedProvider(t *testing.T, name string) string {
	t.Helper()
	seedConfig(t, map[string]string{
		"oauth_" + name + "_client_id":     "test-client-id",
		"oauth_" + name + "_client_secret": "test-client-secret",
	})
	providers := oauth.GetEnabledProviders()
	for _, p := range providers {
		if p.Name == name {
			return p.Name
		}
	}
	t.Fatalf("写入凭据后 oauth 包仍未返回提供商 %s，实际列表 %v", name, providers)
	return ""
}

/* postJSON 构造并执行一次 JSON 请求，返回响应记录器 */
func postJSON(t *testing.T, handler gin.HandlerFunc, path string, body interface{}) *httptest.ResponseRecorder {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("序列化请求体失败: %v", err)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw)))
	c.Request.Header.Set("Content-Type", "application/json")
	handler(c)
	return w
}

/* respMsg 取出响应中的 msg 字段 */
func respMsg(t *testing.T, w *httptest.ResponseRecorder) string {
	t.Helper()
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	return out.Msg
}

/* getWithParam 构造带路径参数的 GET 请求并执行 */
func getWithParam(t *testing.T, handler gin.HandlerFunc, path, paramKey, paramVal string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, path, nil)
	c.Params = gin.Params{{Key: paramKey, Value: paramVal}}
	handler(c)
	return w
}

/* newTestContext 构造仅带请求上下文的 gin.Context */
func newTestContext() (*gin.Context, *httptest.ResponseRecorder) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/test", nil)
	c.Request = c.Request.WithContext(context.Background())
	return c, w
}

/* newCaptchaContext 构造一个带请求的最小 gin 上下文 */
func newCaptchaContext(t *testing.T) *gin.Context {
	t.Helper()
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
	return c
}

/* authControlAdminReq 构造带管理员身份的请求上下文（level=2 即管理员，见 checkAdmin） */
func authControlAdminReq(t *testing.T, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/system/auth-control", strings.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("level", 2)
	return c, w
}

/* mergeConfig 返回两个配置 map 的并集，后者优先，不改动入参；nil 入参视为空 map */
func mergeConfig(a, b map[string]string) map[string]string {
	out := make(map[string]string, len(a)+len(b))
	for k, v := range a {
		out[k] = v
	}
	for k, v := range b {
		out[k] = v
	}
	return out
}
