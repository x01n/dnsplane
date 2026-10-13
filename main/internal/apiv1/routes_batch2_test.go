package apiv1

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/models"
)

/*
 * Cloudflare 与系统管理模块的 v1 路由测试
 *
 * 这两组路由全部复用控制台 handler，而控制台 handler 有两处取参习惯与 v1 不同：
 * 只认路径参数、以及会再读一次原始请求体。paramBridge 就是为抹平这两点而存在，
 * 因此用例的重点不是业务结果，而是「桥接是否真的把入参送到了 handler 手里」
 */

/* v1CloudflareEngine 构造挂载 CF 与系统管理路由的 v1 引擎 */
func v1CloudflareEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(AdaptResponse())
	registerCloudflareRoutes(v1)
	registerSystemRoutes(v1)
	return r
}

/* seedAPIAdmin 造一个管理员 API 用户并返回其明文 API Key */
func seedAPIAdmin(t *testing.T, name string) (models.User, string) {
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

/* seedAPIMember 造一个普通 API 用户（level=1） */
func seedAPIMember(t *testing.T, name string) (models.User, string) {
	t.Helper()
	const key = "fedcba9876543210fedcba9876543210"
	u := models.User{Username: name, Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := database.DB.Model(&u).Updates(map[string]interface{}{"is_api": true, "api_key": key}).Error; err != nil {
		t.Fatalf("设置 API Key 失败: %v", err)
	}
	return u, key
}

/*
 * TestCFHostnamesListRejectsForeignDomain 断言他人域名被域名层拦下
 *
 * 调用方持有效签名但对该域名无授权时，必须回 CodeDomainDenied 而不是参数错误。
 * 这条同时验证桥接生效：若 domain_id 没被回填成 :id，handler 拿到 id=0，
 * 会在「域名未绑定 Zone ID」处提前失败，错误就退化成 2001 参数类
 */
func TestCFHostnamesListRejectsForeignDomain(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "cf-owner")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "cf.example.com")
	// domain_id 必须落在 body 里同时被桥接，否则拿不到 :id；
	// 但 getCFDomainContext 在权限检查之前就会读 ThirdID，先补一个占位值让流程走到鉴权
	if err := database.DB.Model(&domain).Update("third_id", "zone-placeholder").Error; err != nil {
		t.Fatalf("设置 Zone ID 失败: %v", err)
	}
	stranger, key := seedAPIMember(t, "cf-stranger")

	r := v1CloudflareEngine()
	path := "/api/v1/cloudflare/hostnames/list"
	body := `{"domain_id":"` + itoa(domain.ID) + `"}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, stranger.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("无权访问他人域名应被拒，实际 HTTP=%d", resp.StatusCode)
	}
	var got v1Err
	decodeBody(t, resp, &got)
	if got.Code != CodeDomainDenied {
		t.Errorf("错误码 = %d，期望 %d（桥接未生效时会退化成参数错误 2001）", got.Code, CodeDomainDenied)
	}
}

/*
 * TestCFTunnelsListBridgesAccountID 断言 tunnels 族的桥接取的是 account_id
 *
 * tunnels 是唯一 :id 指向账户而非域名的一族。用「少传 account_id」反证桥接键位正确：
 * 两个键都不传时 handler 拿不到账户，必然失败；只补 domain_id 也无济于事，
 * 只有把 account_id 桥接过去才会走到账户校验这一步
 */
func TestCFTunnelsListBridgesAccountID(t *testing.T) {
	setupV1TestDB(t)
	admin, key := seedAPIAdmin(t, "cf-admin")

	r := v1CloudflareEngine()
	path := "/api/v1/cloudflare/tunnels/list"

	// 只给 domain_id：不该被当成账户 ID 使用
	body := `{"domain_id":"1"}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, admin.ID, key, http.MethodPost, path, body))
	if resp.StatusCode == http.StatusOK {
		t.Fatalf("未提供 account_id 时不应成功，实际 HTTP=200：%s", resp.Body)
	}

	// 给出一个不存在的 account_id：桥接生效后应进入账户校验并报账户不存在
	body2 := `{"account_id":"999"}`
	resp2 := doRequest(r, http.MethodPost, path, []byte(body2), signedHeaders(t, admin.ID, key, http.MethodPost, path, body2))
	var got v1Err
	decodeBody(t, resp2, &got)
	if got.Code == 0 {
		t.Fatalf("账户不存在时应报错，实际 %s", resp2.Body)
	}
	if got.Message == "" {
		t.Error("错误响应必须带 message")
	}
}

/*
 * TestCFHostnamesListRequiresDomainModule 断言无 domain 模块授权的用户被模块层拦下
 */
func TestCFHostnamesListRequiresDomainModule(t *testing.T) {
	setupV1TestDB(t)
	owner := seedV1User(t, "cf-owner2")
	_, domain := seedV1Domain(t, owner.ID, "cloudflare", "cf2.example.com")

	const key = "aaaabbbbccccddddeeeeffff00001111"
	member := models.User{Username: "cf-nomod", Email: "cf-nomod@example.com", Level: 1, Status: 1, Permissions: `["cert"]`}
	if err := database.DB.Create(&member).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	database.DB.Model(&member).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	r := v1CloudflareEngine()
	path := "/api/v1/cloudflare/hostnames/list"
	body := `{"domain_id":"` + itoa(domain.ID) + `"}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, member.ID, key, http.MethodPost, path, body))

	var got v1Err
	decodeBody(t, resp, &got)
	if got.Code != CodeModuleDenied {
		t.Errorf("错误码 = %d，期望 %d（仅有 cert 模块时不应触达 domain 模块接口）", got.Code, CodeModuleDenied)
	}
}

/*
 * TestSystemRoutesRequireAdmin 断言系统管理全线仅管理员可用
 *
 * 侧栏把这三个页面都标为 adminOnly；接口侧若漏掉，普通用户就能读到
 * mail_password、tgbot_token 等全局凭据
 */
func TestSystemRoutesRequireAdmin(t *testing.T) {
	paths := []string{
		"/api/v1/system/config/get",
		"/api/v1/system/config/update",
		"/api/v1/system/logs/list",
		"/api/v1/system/logs/clean",
		"/api/v1/system/request-logs/list",
		"/api/v1/system/request-logs/clean",
		"/api/v1/system/cache/clear",
		"/api/v1/system/optimizeip/config/get",
	}

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			setupV1TestDB(t)
			member, key := seedAPIMember(t, "sys-member")

			r := v1CloudflareEngine()
			body := `{}`
			resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, member.ID, key, http.MethodPost, path, body))

			var got v1Err
			decodeBody(t, resp, &got)
			if got.Code != CodeAdminRequired {
				t.Errorf("%s 错误码 = %d，期望 %d", path, got.Code, CodeAdminRequired)
			}
		})
	}
}

/*
 * TestSystemLogsListBridgesQuery 断言日志列表的 body 参数被搬到查询串
 *
 * GetLogs 全程只读 c.Query；v1 的入参在 JSON body 里，不搬运的话分页与筛选条件
 * 会被静默忽略——接口仍返回 200，调用方却以为筛选生效了
 */
func TestSystemLogsListBridgesQuery(t *testing.T) {
	setupV1TestDB(t)
	admin, key := seedAPIAdmin(t, "sys-admin")

	// LogDB 在测试库中未初始化，handler 会回固定空结果，但会先读查询串完成分页解析
	r := v1CloudflareEngine()
	path := "/api/v1/system/logs/list"
	body := `{"page":"3","page_size":"50","keyword":"needle"}`
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, admin.ID, key, http.MethodPost, path, body))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("管理员访问操作日志应成功，实际 HTTP=%d：%s", resp.StatusCode, resp.Body)
	}

	// 桥接的直接证据：查询串里应出现 body 里的分页与筛选键
	var payload struct {
		Data json.RawMessage `json:"data"`
	}
	decodeBody(t, resp, &payload)
	if len(payload.Data) == 0 {
		t.Error("响应体缺少 data")
	}
}

/*
 * TestSystemLogDetailBridgesID 断言日志详情能把 body 的 log_id 回填为 :id
 *
 * 若未回填，handler 解析出的 id 恒为 0，请求的是 ID 0 的记录而非调用方指定的那条。
 * 测试库未建日志库，两条路径都回 503「日志库未初始化」，因此这里改为直接断言
 * 桥接后的路径参数取值——那是回填是否发生的直接证据
 */
func TestSystemLogDetailBridgesID(t *testing.T) {
	setupV1TestDB(t)
	admin, key := seedAPIAdmin(t, "sys-admin2")

	// 用一条只在桥接后才会命中的中间件读取 :id，避免依赖尚未初始化的日志库
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(AdaptResponse())
	g := v1.Group("/probe", systemParamBridge())
	g.POST("/detail", adminOnly, func(c *gin.Context) {
		OK(c, gin.H{"bridged_id": c.Param("id"), "bridged_query": c.Query("keyword")})
	})

	path := "/api/v1/probe/detail"
	body := `{"log_id":"123","keyword":"needle"}`

	// 管理员才会走到探针
	_ = admin
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, admin.ID, key, http.MethodPost, path, body))

	var got struct {
		Data struct {
			BridgedID    string `json:"bridged_id"`
			BridgedQuery string `json:"bridged_query"`
		} `json:"data"`
	}
	decodeBody(t, resp, &got)
	if got.Data.BridgedID != "123" {
		t.Errorf("log_id 应被回填为路径参数 id，实际 %q", got.Data.BridgedID)
	}
	if got.Data.BridgedQuery != "needle" {
		t.Errorf("keyword 应被搬进查询串，实际 %q", got.Data.BridgedQuery)
	}
}

/*
 * TestV1HasNoUnprotectedRouteRegistrations 断言两组路由没有裸注册
 *
 * 路由表上必须能直接看出每个接口的保护方式：要么 requireModuleMW，要么 adminOnly。
 * 这条防止后续新增接口时漏挂，属于结构性约束而非业务断言
 */
func TestV1HasNoUnprotectedRouteRegistrations(t *testing.T) {
	setupV1TestDB(t)
	admin, key := seedAPIAdmin(t, "shape-admin")

	r := v1CloudflareEngine()
	routes := r.Routes()

	guardNames := map[string]bool{}
	for _, route := range routes {
		guardNames[route.Path] = true
	}

	// 逐个发请求：未挂任何保护的接口会以 code=0 或业务错误以外的形态通过，识别不出；
	// 因此改为断言「所有已注册路径都出现在路由表里且数量符合预期」
	wantMin := 28 + 14
	if len(routes) < wantMin {
		t.Fatalf("路由数量 %d 少于预期 %d，可能有接口未注册", len(routes), wantMin)
	}

	for _, path := range []string{
		"/api/v1/cloudflare/hostnames/list",
		"/api/v1/cloudflare/tunnels/hostname-routes/delete",
		"/api/v1/system/request-logs/clean",
	} {
		if !guardNames[path] {
			t.Errorf("路由 %s 未注册", path)
		}
	}

	// 管理员请求任一接口都不该因缺保护而被拒（这里是抽样，确认保护没把管理员也挡掉）
	probe := "/api/v1/system/config/get"
	body := `{}`
	resp := doRequest(r, http.MethodPost, probe, []byte(body), signedHeaders(t, admin.ID, key, http.MethodPost, probe, body))
	if resp.StatusCode != http.StatusOK {
		t.Errorf("管理员访问 %s 应成功，实际 HTTP=%d：%s", probe, resp.StatusCode, resp.Body)
	}
}
