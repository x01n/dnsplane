package apiv1

import (
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/models"
)

/*
 * v1 的配额闸门
 *
 * 控制台有九处 CheckQuota，v1 此前一处都没有——管理员设的配额上限，
 * 经 /api/v1 调用可以完全绕过：控制台建第三条记录会被拒，改用 API Key
 * 走 v1 就能无限建下去。配额是资源约束而非接口约束，绕过路径的存在使它形同虚设。
 *
 * 这些用例锁住：超限拒绝、未超限放行、管理员不受限、以及拒的是正确维度
 */

/* seedQuotaUser 造一个带 API Key 的普通用户并设定其配额 */
func seedQuotaUser(t *testing.T, name string, quotas string) (models.User, string) {
	t.Helper()
	const key = "quota-gate-key-0123456789abcdef"
	u := models.User{Username: name, Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := database.DB.Model(&u).Updates(map[string]interface{}{"is_api": true, "api_key": key}).Error; err != nil {
		t.Fatalf("设置 API Key 失败: %v", err)
	}
	if err := database.DB.Create(&models.UserQuota{UserID: u.ID, Quotas: quotas}).Error; err != nil {
		t.Fatalf("设置个体配额失败: %v", err)
	}
	return u, key
}

/* seedOwnedDomain 给用户造一个自有账户与域名，返回域名 */
func seedOwnedDomain(t *testing.T, uid uint, name string) models.Domain {
	t.Helper()
	account := models.Account{UserID: uid, Type: "cloudflare", Name: name + "-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: name, ThirdID: "zone-" + name}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	return domain
}

/* quotaGateEngine 只挂配额门与认证的最小引擎 */
func quotaGateEngine() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	v1 := r.Group("/api/v1")
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(AdaptResponse())
	v1.POST("/domains/create", requireQuota("domains"), func(c *gin.Context) {
		OK(c, gin.H{"passed": true})
	})
	v1.POST("/records/create", requireQuota("records"), func(c *gin.Context) {
		OK(c, gin.H{"passed": true})
	})
	return r
}

/* callQuotaGate 发一次带签名的请求，返回解析后的错误体（成功时 code=0） */
func callQuotaGate(t *testing.T, r *gin.Engine, uid uint, key, path string) v1Err {
	t.Helper()
	const body = `{}`
	resp := doRequest(r, http.MethodPost, path, []byte(body),
		signedHeaders(t, uid, key, http.MethodPost, path, body))
	var got v1Err
	decodeBody(t, resp, &got)
	return got
}

/* TestQuotaGateBlocksWhenExceeded 已达上限时不得放行 */
func TestQuotaGateBlocksWhenExceeded(t *testing.T) {
	setupBodyTestDB(t)
	// max_domains=1，而该用户已有 1 个域名
	u, key := seedQuotaUser(t, "at-limit", `{"max_domains":1}`)
	seedOwnedDomain(t, u.ID, "first.example.com")

	r := quotaGateEngine()
	got := callQuotaGate(t, r, u.ID, key, "/api/v1/domains/create")

	if got.Code == 0 {
		t.Fatal("已达上限时应被拒绝，实际放行")
	}
	if got.Code != CodeResourceLocked {
		t.Errorf("拒绝应使用 %d（资源被锁），实际 %d msg=%q", CodeResourceLocked, got.Code, got.Message)
	}
}

/* TestQuotaGateAllowsBelowLimit 未达上限时正常放行 */
func TestQuotaGateAllowsBelowLimit(t *testing.T) {
	setupBodyTestDB(t)
	u, key := seedQuotaUser(t, "below-limit", `{"max_domains":2}`)
	seedOwnedDomain(t, u.ID, "only.example.com")

	r := quotaGateEngine()
	if got := callQuotaGate(t, r, u.ID, key, "/api/v1/domains/create"); got.Code != 0 {
		t.Errorf("未达上限应放行，实际 code=%d msg=%q", got.Code, got.Message)
	}
}

/* TestQuotaGateUnlimitedAllows 配额为 -1（不限）时永远放行 */
func TestQuotaGateUnlimitedAllows(t *testing.T) {
	setupBodyTestDB(t)
	u, key := seedQuotaUser(t, "unlimited", `{"max_domains":-1}`)
	for i := 0; i < 3; i++ {
		seedOwnedDomain(t, u.ID, "many.example.com")
	}

	r := quotaGateEngine()
	if got := callQuotaGate(t, r, u.ID, key, "/api/v1/domains/create"); got.Code != 0 {
		t.Errorf("不限配额应放行，实际 code=%d msg=%q", got.Code, got.Message)
	}
}

/* TestQuotaGateDimensionIsSpecific 只拒超限的维度，别的维度不受影响 */
func TestQuotaGateDimensionIsSpecific(t *testing.T) {
	setupBodyTestDB(t)
	// 域名额度用满，但记录额度还有
	u, key := seedQuotaUser(t, "mixed", `{"max_domains":1,"max_records":10}`)
	seedOwnedDomain(t, u.ID, "mixed.example.com")

	r := quotaGateEngine()
	if got := callQuotaGate(t, r, u.ID, key, "/api/v1/domains/create"); got.Code == 0 {
		t.Error("域名维度已达上限，应被拒")
	}
	if got := callQuotaGate(t, r, u.ID, key, "/api/v1/records/create"); got.Code != 0 {
		t.Errorf("记录维度未超限，不应被域名维度的额度牵连，实际 code=%d msg=%q", got.Code, got.Message)
	}
}

/* TestQuotaGateAdminUnlimited 管理员不受配额约束（quotaForUser 对 level>=2 直接返回不限） */
func TestQuotaGateAdminUnlimited(t *testing.T) {
	setupBodyTestDB(t)
	admin, key := seedAPIAdmin(t, "quota-admin")
	if err := database.DB.Create(&models.UserQuota{UserID: admin.ID, Quotas: `{"max_domains":0}`}).Error; err != nil {
		t.Fatalf("设置配额失败: %v", err)
	}

	r := quotaGateEngine()
	if got := callQuotaGate(t, r, admin.ID, key, "/api/v1/domains/create"); got.Code != 0 {
		t.Errorf("管理员不应受配额约束，实际 code=%d msg=%q", got.Code, got.Message)
	}
}

/*
 * TestQuotaGateAppliesToV1RoutesOnlyWhereNeeded 真实路由表上的门只挂在必要处
 *
 * 判据是「该接口复用的 handler 内部有没有 CheckQuota」：
 *   有（域名/账户/证书/监控/定时/优选 IP）→ 路由层不挂，由 handler 拦
 *   没有（记录的批量新增）              → 路由层必须挂，否则整条绕过
 *
 * 这条用例同时锁住「路由层不重复挂」——重复挂会让拒绝发生在 handler 校验之前，
 * 错误码与走控制台时不一致
 */
func TestQuotaGateAppliesToV1RoutesOnlyWhereNeeded(t *testing.T) {
	setupBodyTestDB(t)
	r := v1FullEngine(nil)

	// 域名创建：handler 内部有 CheckQuota，路由层不挂；超限仍应被 handler 拒
	u1, key1 := seedQuotaUser(t, "route-domain", `{"max_domains":1}`)
	seedOwnedDomain(t, u1.ID, "route1.example.com")
	got1 := callQuotaGate(t, r, u1.ID, key1, "/api/v1/domains/create")
	if got1.Code == 0 {
		t.Error("/api/v1/domains/create 在已达上限时被放行：handler 的配额门未生效")
	}

	/*
	 * 记录的批量新增：handler 内没有配额门，必须由路由层拦
	 *
	 * 配额门挂在路由中间件上，先于 handler 的一切校验执行——
	 * 所以即使 body 里没有 domain_id，也应先被配额拦下而不是报「缺少域名ID」。
	 * 这条顺序本身就是判据：若收到的是参数错误，说明门没挂在最前
	 */
	u2, key2 := seedQuotaUser(t, "route-batch", `{"max_records":0}`)
	got2 := callQuotaGate(t, r, u2.ID, key2, "/api/v1/records/batch/add")
	if got2.Code != CodeResourceLocked {
		t.Errorf("/api/v1/records/batch/add 应由路由层配额门拦下（期望 %d），实际 code=%d msg=%q",
			CodeResourceLocked, got2.Code, got2.Message)
	}
}
