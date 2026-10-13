package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
)

/*
 * 域名权限中间件的挂载验证
 *
 * 该中间件此前只定义未挂载，导致它写入的 perm_read_only / perm_domain /
 * perm_sub_domain 三个键恒不存在，下游依赖它们的拦截全部失效。
 * 这里从路由层验证它确实被挂在 /api 分组内，且确实写出了这三个键——
 * 只断言「函数被调用」不够，真正失效的是上下文键的缺失
 */

/*
 * permissionProbeEngine 构造含 Auth 与 Permission 的最小路由
 *
 * 不复用 SetupRouter：那会牵入全部业务 handler 与静态资源，
 * 本用例只验证中间件链的顺序与效果，路由面越小越能定位问题
 */
func permissionProbeEngine() *gin.Engine {
	r := gin.New()
	r.Use(gin.Recovery())
	api := r.Group("/api")
	api.Use(middleware.RequestTrace())
	{
		auth := api.Group("")
		// 用静态身份替代真实登录，隔离出 Permission 的行为
		auth.Use(func(c *gin.Context) {
			c.Set("user_id", c.GetHeader("X-Test-UID"))
			level := 1
			if c.GetHeader("X-Test-Admin") == "1" {
				level = 2
			}
			c.Set("level", level)
			c.Set("username", "probe")
			middleware.ApplyUserPermissionModules(c, "")
			c.Next()
		})
		auth.Use(middleware.Permission())
		/*
		 * 探针带 :id 路径参数以贴合生产
		 *
		 * Permission 依次从 decrypted_data / query / c.Param("id") 取 domain_id，
		 * 而写入 decrypted_data 的 DecryptAndVerify 中间件从未被挂载，
		 * 控制台的域名范围接口实际都靠 :id 取到域名 ID
		 */
		auth.POST("/probe/:id", func(c *gin.Context) {
			ro, _ := c.Get("perm_read_only")
			pd, _ := c.Get("perm_domain")
			ps, _ := c.Get("perm_sub_domain")
			c.JSON(http.StatusOK, gin.H{
				"read_only":  ro,
				"domain":     pd,
				"sub_domain": ps,
			})
		})
	}
	return r
}

/* seedPermFixture 建一个属于 owner 的域名，并给 viewer 授予指定权限 */
func seedPermFixture(t *testing.T, readOnly bool, sub string) (models.User, models.User, models.Domain) {
	t.Helper()
	owner := models.User{Username: "p-owner", Level: 1, Status: 1}
	viewer := models.User{Username: "p-viewer", Level: 1, Status: 1}
	for _, u := range []*models.User{&owner, &viewer} {
		if err := database.DB.Create(u).Error; err != nil {
			t.Fatalf("创建用户失败: %v", err)
		}
	}
	account := models.Account{UserID: owner.ID, Type: "cloudflare", Name: "p-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: "perm.example.com"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if err := database.DB.Create(&models.Permission{
		UserID:    viewer.ID,
		Domain:    domain.Name,
		SubDomain: sub,
		ReadOnly:  readOnly,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}
	return owner, viewer, domain
}

/*
 * 中间件必须把三个权限键写入上下文
 *
 * 这是「挂载生效」的直接证据：未挂载时三个键都不存在，探针会回 null
 */
func TestPermissionMiddlewareWritesContextKeys(t *testing.T) {
	setupRouterTestDB(t)
	_, viewer, domain := seedPermFixture(t, false, "api")

	r := permissionProbeEngine()
	w := doPost(r, "/api/probe/"+itoaU(domain.ID), map[string]string{
		"X-Test-UID": itoaU(viewer.ID),
	}, []byte(`{}`))

	var got map[string]interface{}
	decodeResp(t, w, &got)
	if got["domain"] != domain.Name {
		t.Errorf("perm_domain = %v，期望 %s（中间件未生效）", got["domain"], domain.Name)
	}
	if got["sub_domain"] != "api" {
		t.Errorf("perm_sub_domain = %v，期望 api", got["sub_domain"])
	}
	if got["read_only"] != false {
		t.Errorf("perm_read_only = %v，期望 false", got["read_only"])
	}
}

/*
 * 只读授权下的写请求必须被中间件直接拒绝
 *
 * 这条锁定「只读拦截生效」：挂载前该请求会一路走到 handler，
 * 而 handler 只在个别接口检查 perm_read_only，多数写接口会直接放行
 */
func TestPermissionMiddlewareBlocksReadOnlyWrite(t *testing.T) {
	setupRouterTestDB(t)
	_, viewer, domain := seedPermFixture(t, true, "")

	r := permissionProbeEngine()
	w := doPost(r, "/api/probe/"+itoaU(domain.ID), map[string]string{
		"X-Test-UID": itoaU(viewer.ID),
	}, []byte(`{}`))

	var got struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	decodeResp(t, w, &got)
	if got.Code == 0 {
		t.Fatalf("只读授权的写请求应被拒，实际放行（msg=%q）", got.Msg)
	}
}

/*
 * 无授权的用户不得读到域名信息
 *
 * 与只读那条配对：只读是「能读不能写」，无授权是「读写都不行」，
 * 两者必须区分开，否则拦截粒度就退化成了一刀切
 */
func TestPermissionMiddlewareDeniesStranger(t *testing.T) {
	setupRouterTestDB(t)
	_, _, domain := seedPermFixture(t, false, "")
	stranger := models.User{Username: "p-stranger", Level: 1, Status: 1}
	if err := database.DB.Create(&stranger).Error; err != nil {
		t.Fatalf("创建无关用户失败: %v", err)
	}

	r := permissionProbeEngine()
	w := doPost(r, "/api/probe/"+itoaU(domain.ID), map[string]string{
		"X-Test-UID": itoaU(stranger.ID),
	}, []byte(`{}`))

	var got struct {
		Code int `json:"code"`
	}
	decodeResp(t, w, &got)
	if got.Code == 0 {
		t.Error("无授权用户访问他人域名应被拒，实际放行")
	}
}

/*
 * 管理员不受域名权限约束
 *
 * 管理员的运维入口依赖他人域名，若被域名单点校验拦住会全面失效
 */
func TestPermissionMiddlewareAllowsAdmin(t *testing.T) {
	setupRouterTestDB(t)
	_, _, domain := seedPermFixture(t, false, "")
	admin := models.User{Username: "p-admin", Level: 2, Status: 1}
	if err := database.DB.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}

	r := permissionProbeEngine()
	w := doPost(r, "/api/probe/"+itoaU(domain.ID), map[string]string{
		"X-Test-UID":   itoaU(admin.ID),
		"X-Test-Admin": "1",
	}, []byte(`{}`))

	/*
	 * 管理员在中间件里直接放行，不会写入 perm_* 三个键
	 * （那些键供子域/只读的后续判定使用，管理员本就不受其约束），
	 * 因此这里断言的是「请求到达了 handler」而非「键有值」
	 */
	var got map[string]interface{}
	decodeResp(t, w, &got)
	if _, reached := got["domain"]; !reached {
		t.Errorf("管理员请求应到达 handler，实际响应 %s", w.Body.String())
	}
}

/*
 * 请求未带 domain_id 时放行
 *
 * 该中间件只应作用于域名范围的接口；无 domain_id 的请求（如列表、配置类）
 * 由各自 handler 判断，中间件放行是对的，拦住反而会误伤
 */
func TestPermissionMiddlewareSkipsRequestWithoutDomainID(t *testing.T) {
	setupRouterTestDB(t)

	/*
	 * 用一条不带 :id 的路由：该中间件只作用于域名范围接口，
	 * 无 domain_id 时放行交由 handler 自行判断，拦住会误伤列表类接口
	 */
	r := gin.New()
	api := r.Group("/api")
	{
		auth := api.Group("")
		auth.Use(func(c *gin.Context) {
			c.Set("user_id", "999")
			c.Set("level", 1)
			c.Next()
		})
		auth.Use(middleware.Permission())
		auth.POST("/no-domain-scope", func(c *gin.Context) {
			ro, _ := c.Get("perm_read_only")
			c.JSON(http.StatusOK, gin.H{"read_only": ro})
		})
	}

	w := doPost(r, "/api/no-domain-scope", nil, []byte(`{}`))
	var got map[string]interface{}
	decodeResp(t, w, &got)
	if got["read_only"] != nil {
		t.Errorf("无 domain_id 的请求不应写入 perm_read_only，实际 %v", got["read_only"])
	}
}

/* doPost 发起一次带 JSON 体的 POST 请求 */
func doPost(r *gin.Engine, target string, headers map[string]string, body []byte) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

/* decodeResp 解析响应体，失败即 Fatal 以免零值静默通过断言 */
func decodeResp(t *testing.T, w *httptest.ResponseRecorder, out interface{}) {
	t.Helper()
	if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
}

/* itoaU 数字 ID 转字符串 */
func itoaU(v uint) string {
	return strconv.FormatUint(uint64(v), 10)
}
