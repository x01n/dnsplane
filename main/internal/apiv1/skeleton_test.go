package apiv1

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
)

/*
 * setupV1TestDB 初始化临时数据库
 *
 * RequestTrace 中间件在请求结束时落库到 RequestDB，未初始化会触发 nil 解引用 panic，
 * 因此所有用到完整中间件链的用例都必须先建库
 */
func setupV1TestDB(t *testing.T) {
	t.Helper()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "apiv1.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/*
 * v1 骨架的端到端测试
 *
 * 覆盖三件正确性前提：认证拦截、响应格式转换、以及异步接口确已同步化。
 * 这些一旦破坏，任何模块的接口都会整体失效，因此放在骨架层锁定
 */

/* v1TestEngine 构造与生产同构的 v1 路由，但不挂载具体业务模块 */
func v1TestEngine(register func(*gin.RouterGroup)) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(middleware.RequestTrace())
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(AdaptResponse())
	if register != nil {
		register(v1)
	}
	return r
}

/* doRequest 发一次请求并返回响应记录器 */
func doRequest(r *gin.Engine, method, path string, body []byte, headers map[string]string) *http.Response {
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := newRecorder()
	r.ServeHTTP(w, req)
	return w.Result()
}

/* 断言用的最小响应结构 */
type v1Err struct {
	Code      int    `json:"code"`
	RequestID string `json:"request_id"`
	Message   string `json:"message"`
}

/*
 * 缺少认证头必须被拒，且走 v1 的响应格式
 *
 * 若这里返回的是控制台的 {code:-1,msg:...}，说明 v1 的认证中间件没生效
 */
func TestV1RejectsMissingCredentials(t *testing.T) {
	setupV1TestDB(t)
	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/ping", func(c *gin.Context) { OK(c, gin.H{"pong": true}) })
	})

	resp := doRequest(r, http.MethodPost, "/api/v1/ping", []byte("{}"), nil)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HTTP 状态 = %d，期望 401", resp.StatusCode)
	}

	var got v1Err
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("响应不是 v1 错误格式: %v", err)
	}
	if got.Code != CodeUnauthorized {
		t.Errorf("错误码 = %d，期望 %d", got.Code, CodeUnauthorized)
	}
	if got.Message == "" {
		t.Error("错误响应必须带 message")
	}
}

/*
 * 时间戳超出窗口必须被拒
 *
 * 签名正确但时间戳过期时应回 CodeTimestampInvalid 而非通用认证失败——
 * 调用方据此判断是时钟漂移还是密钥错误，两者的处置完全不同
 */
func TestV1RejectsStaleTimestamp(t *testing.T) {
	setupV1TestDB(t)
	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/ping", func(c *gin.Context) { OK(c, gin.H{"pong": true}) })
	})

	resp := doRequest(r, http.MethodPost, "/api/v1/ping", []byte("{}"), map[string]string{
		"X-API-UID":       "1",
		"X-API-Timestamp": "1", // 1970 年，远超 ±5 分钟
		"X-API-Sign":      "deadbeef",
	})

	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("HTTP 状态 = %d，期望 401", resp.StatusCode)
	}
	var got v1Err
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if got.Code != CodeTimestampInvalid {
		t.Errorf("错误码 = %d，期望 %d（时间戳过期应可区分于签名错误）", got.Code, CodeTimestampInvalid)
	}
}

/*
 * 中间件阶段写出的错误不得被 AdaptResponse 二次包装
 *
 * 认证失败发生在 AdaptResponse 之外，其响应已确认是 v1 格式；
 * 这条用例守住「缓冲为空时原样透出」的分支——若该分支失效，
 * 认证错误会被解析成空结构再重编码，message 与 code 都会丢
 */
func TestV1AuthErrorNotDoubleWrapped(t *testing.T) {
	setupV1TestDB(t)
	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/ping", func(c *gin.Context) { OK(c, gin.H{"pong": true}) })
	})

	resp := doRequest(r, http.MethodPost, "/api/v1/ping", []byte("{}"), map[string]string{
		"X-API-UID": "1",
	})
	var got v1Err
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("响应解析失败: %v", err)
	}
	if got.Code != CodeUnauthorized || got.Message != messageFor(CodeUnauthorized) {
		t.Errorf("认证错误被改写：code=%d message=%q", got.Code, got.Message)
	}
}

/*
 * 错误码到 HTTP 状态的映射必须稳定
 *
 * 客户端与网关都可能只看 HTTP 状态：把权限不足映射成 5xx 会让调用方
 * 误判为服务端故障而反复重试，把内部错误映射成 4xx 则会掩盖真实故障
 */
func TestErrorCodeHTTPStatusMapping(t *testing.T) {
	cases := []struct {
		code int
		want int
	}{
		{CodeUnauthorized, http.StatusUnauthorized},
		{CodeSignMismatch, http.StatusUnauthorized},
		{CodeModuleDenied, http.StatusBadRequest},
		{CodeDomainDenied, http.StatusBadRequest},
		{CodeReadOnly, http.StatusBadRequest},
		{CodeDomainNotFound, http.StatusNotFound},
		{CodeRecordNotFound, http.StatusNotFound},
		{CodeResourceConflict, http.StatusConflict},
		{CodeProviderError, http.StatusBadGateway},
		{CodeUpstreamTimeout, http.StatusGatewayTimeout},
		{CodeInternalError, http.StatusInternalServerError},
	}
	for _, c := range cases {
		if got := httpStatusFor(c.code); got != c.want {
			t.Errorf("码 %d 的 HTTP 状态 = %d，期望 %d", c.code, got, c.want)
		}
	}
}

/*
 * 每个已定义的错误码都必须有默认文案
 *
 * 缺少文案时 Fail 会回落到「请求处理失败」，调用方仍能拿到码，
 * 但排查时看不到任何线索，等于把可诊断性丢掉了
 */
func TestEveryCodeHasMessage(t *testing.T) {
	codes := []int{
		CodeUnauthorized, CodeTimestampInvalid, CodeSignMismatch, CodeAPIKeyMissing,
		CodeAccountDisabled, CodeUserNotFound,
		CodeParamInvalid, CodeParamMissing, CodeBodyUnreadable, CodePageOutOfRange, CodeUnsupportedType,
		CodeModuleDenied, CodeDomainDenied, CodeSubDomainDenied, CodeReadOnly, CodeAdminRequired, CodeOwnerRequired,
		CodeResourceNotFound, CodeDomainNotFound, CodeAccountNotFound, CodeRecordNotFound,
		CodeCertOrderNotFound, CodeTaskNotFound, CodeResourceConflict, CodeResourceLocked,
		CodeProviderError, CodeAccountConfigBad, CodeProviderUnsupported, CodeDNSWriteFailed,
		CodeCertIssueFailed, CodeUpstreamTimeout,
		CodeInternalError, CodeNotImplemented,
	}
	for _, code := range codes {
		if m := messageFor(code); m == "" || m == "请求处理失败" {
			t.Errorf("错误码 %d 缺少默认文案", code)
		}
	}
}

/*
 * 控制台错误文案到 v1 码值的映射
 *
 * 复用控制台 handler 时错误只能按文案反推，这条锁定最常见的几类，
 * 防止映射表在后续调整中退化成「全部归为参数错误」
 */
func TestMapConsoleError(t *testing.T) {
	cases := []struct {
		msg  string
		want int
	}{
		{"无权操作该域名", CodeDomainDenied},
		{"无权限，仅管理员可操作", CodeDomainDenied},
		{"该域名为只读权限，无法执行写操作", CodeReadOnly},
		{"域名不存在", CodeDomainNotFound},
		{"账户不存在", CodeAccountNotFound},
		{"任务不存在", CodeTaskNotFound},
		{"账户配置解析失败", CodeAccountConfigBad},
		{"当前服务商不支持该能力", CodeProviderUnsupported},
		{"参数解析失败", CodeParamInvalid},
	}
	for _, c := range cases {
		if got := mapConsoleError(c.msg); got != c.want {
			t.Errorf("%q 映射为 %d，期望 %d", c.msg, got, c.want)
		}
	}
}
