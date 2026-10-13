package apiv1

import (
	"net/http"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
)

/*
 * v1 请求体与响应格式的地基用例
 *
 * 这两条锁定的是「复用控制台 handler」能否成立的前提：
 * 请求体必须可被读第二次，成功响应的文案不能在适配层被丢掉
 */

/* setupBodyTestDB 初始化临时库与缓存 */
func setupBodyTestDB(t *testing.T) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "body.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* seedBodyTestUser 造一个可用的 API 用户 */
func seedBodyTestUser(t *testing.T, name string) (models.User, string) {
	t.Helper()
	const key = "0123456789abcdef0123456789abcdef"
	u := models.User{Username: name, Email: name + "@example.com", Level: 2, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := database.DB.Model(&u).Updates(map[string]interface{}{"is_api": true, "api_key": key}).Error; err != nil {
		t.Fatalf("设置 API Key 失败: %v", err)
	}
	return u, key
}

/*
 * TestV1BodyReadableTwice 断言请求体被中间件读过之后，handler 仍能再读一次
 *
 * 这是复用控制台 handler 的前提：JSONBodyToDecryptedData 必须先把 body 放进
 * decrypted_data（BindDecryptedData 那条路径），同时把请求体还原回去
 * （c.ShouldBindJSON 那条路径）。缺任一半都会让相应的一批接口「成功但什么也没做」：
 * 后者拿到 io.EOF，字段全为零值。
 *
 * 探针 handler 刻意只用 c.ShouldBindJSON，用来单独验证还原这一半；
 * decrypted_data 的内容在同一次请求里一并断言，验证另一半
 */
func TestV1BodyReadableTwice(t *testing.T) {
	setupBodyTestDB(t)
	user, key := seedBodyTestUser(t, "body-probe")

	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/probe", func(c *gin.Context) {
			// 第一半：控制台 handler 常见的直接读体
			raw := map[string]interface{}{}
			bindErr := ""
			if err := c.ShouldBindJSON(&raw); err != nil {
				bindErr = err.Error()
			}
			// 第二半：BindDecryptedData 那条路径读到的内容
			decrypted := middleware.GetDecryptedData(c)
			OK(c, gin.H{
				"bind_error": bindErr,
				"name":       raw["name"],
				"count":      raw["count"],
				"nested":     raw["nested"],
				"decrypted":  decrypted["name"],
			})
		})
	})

	path := "/api/v1/probe"
	body := `{"name":"hello","count":3,"nested":{"k":"v"},"list":["a","b"]}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, user.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP = %d，期望 200", resp.StatusCode)
	}
	var got struct {
		Data struct {
			BindError string      `json:"bind_error"`
			Name      string      `json:"name"`
			Count     float64     `json:"count"`
			Nested    interface{} `json:"nested"`
			Decrypted string      `json:"decrypted"`
		} `json:"data"`
	}
	decodeBody(t, resp, &got)

	if got.Data.BindError != "" {
		t.Errorf("handler 再读请求体失败（%s）——中间件读完未还原 body", got.Data.BindError)
	}
	if got.Data.Name != "hello" {
		t.Errorf("直接读体应拿到 name=hello，实际 %q（EOF 时为空串）", got.Data.Name)
	}
	if got.Data.Count != 3 {
		t.Errorf("直接读体应拿到 count=3，实际 %v", got.Data.Count)
	}
	if got.Data.Nested == nil {
		t.Error("直接读体应保留嵌套对象，实际为 nil")
	}
	if got.Data.Decrypted != "hello" {
		t.Errorf("decrypted_data 应含 name=hello，实际 %q", got.Data.Decrypted)
	}
}

/*
 * TestV1BodyEmptyStillPassesThrough 断言空体与非 JSON 体不阻断请求
 *
 * 空体是正常场景（如只带路径参数的动作接口）。中间件必须放行，
 * 由 handler 自己按缺参报错，而不是在中间件层抛解析失败
 */
func TestV1BodyEmptyStillPassesThrough(t *testing.T) {
	setupBodyTestDB(t)
	user, key := seedBodyTestUser(t, "body-empty")

	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/probe", func(c *gin.Context) {
			decrypted := middleware.GetDecryptedData(c)
			OK(c, gin.H{"decrypted_nil": decrypted == nil, "size": len(decrypted)})
		})
	})

	for _, body := range []string{"", "not-json"} {
		path := "/api/v1/probe"
		resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, user.ID, key, http.MethodPost, path, body))
		if resp.StatusCode != http.StatusOK {
			t.Errorf("body=%q 时应放行，实际 HTTP=%d", body, resp.StatusCode)
		}
	}
}

/*
 * TestV1SuccessMsgKeptInMessage 断言控制台 SuccessMsg 的文案出现在 v1 响应里
 *
 * SuccessMsg 写出的是 {code:0, msg:"成功添加 3 条"}，没有 data 键。
 * 适配层若只取 data，调用方拿到的是 data:null，而这类计数文案恰恰是
 * 判断「部分失败」的唯一线索，丢了就无法复盘
 */
func TestV1SuccessMsgKeptInMessage(t *testing.T) {
	setupBodyTestDB(t)
	user, key := seedBodyTestUser(t, "msg-probe")

	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/msg", func(c *gin.Context) {
			middleware.SuccessMsg(c, "成功添加 3 条，失败 1 条")
		})
		v1.POST("/data", func(c *gin.Context) {
			middleware.SuccessResponse(c, gin.H{"id": 7})
		})
	})

	path := "/api/v1/msg"
	body := `{}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, user.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("HTTP = %d，期望 200", resp.StatusCode)
	}
	var got okBody
	decodeBody(t, resp, &got)
	if got.Code != 0 {
		t.Fatalf("code = %d，期望 0", got.Code)
	}
	if got.Message != "成功添加 3 条，失败 1 条" {
		t.Errorf("message = %q，期望保留 SuccessMsg 的原文", got.Message)
	}

	// 对照：数据型成功响应不应凭空多出 message 字段
	path2 := "/api/v1/data"
	body2 := `{}`
	resp2 := doRequest(r, http.MethodPost, path2, []byte(body2), signedHeaders(t, user.ID, key, http.MethodPost, path2, body2))

	var raw map[string]interface{}
	decodeBody(t, resp2, &raw)
	if _, exists := raw["message"]; exists {
		t.Errorf("数据型成功响应不应带 message 字段，实际 %v", raw)
	}
	data, ok := raw["data"].(map[string]interface{})
	if !ok {
		t.Fatalf("data 应为对象，实际 %v", raw["data"])
	}
	if data["id"] != float64(7) {
		t.Errorf("data.id = %v，期望 7", data["id"])
	}
}

/*
 * TestV1ConsoleErrorKeepsMessage 断言控制台错误响应的文案同样被保留
 *
 * 错误分支此前已带 message，这条防止本次改动把它改写成空串
 */
func TestV1ConsoleErrorKeepsMessage(t *testing.T) {
	setupBodyTestDB(t)
	user, key := seedBodyTestUser(t, "err-probe")

	r := v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/err", func(c *gin.Context) {
			middleware.ErrorResponse(c, "无权操作该域名")
		})
	})

	path := "/api/v1/err"
	body := `{}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, user.ID, key, http.MethodPost, path, body))

	var got errBody
	decodeBody(t, resp, &got)
	if got.Code != CodeDomainDenied {
		t.Errorf("code = %d，期望 %d", got.Code, CodeDomainDenied)
	}
	if got.Message != "无权操作该域名" {
		t.Errorf("message = %q，期望保留控制台原文", got.Message)
	}
}
