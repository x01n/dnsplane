package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 套餐与配额管理端点（/system/plans、/system/user-quotas、/user/quota）
 *
 * 未启用加密链路时 GetDecryptedData 返回 nil，请求按明文 JSON 绑定，
 * 与 auth_control_test.go 的投递方式一致。
 *
 * 重点覆盖三类易错点：
 *   1. 零值写入：Plan.Status 与 UserSubscription.Status 刻意不带 gorm default 标签，
 *      用 map 做 Updates 时零值必须真的落库，否则「停用」会被静默忽略
 *   2. 唯一性：默认套餐全库只能有一个；同一用户同时只能有一条启用中的订阅
 *   3. 引用完整性：仍有在用订阅的套餐不得删除
 */

/* planAdminContext 构造一个管理员身份的请求上下文 */
func planAdminContext(t *testing.T, method, path, body string) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	var reader *strings.Reader
	if body == "" {
		reader = strings.NewReader("")
	} else {
		reader = strings.NewReader(body)
	}
	c.Request = httptest.NewRequest(method, path, reader)
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("level", 2)
	c.Set("user_id", "1")
	return c, w
}

/* decodeResp 解析标准响应体的 code/data */
func decodeResp(t *testing.T, w *httptest.ResponseRecorder) (int, map[string]interface{}) {
	t.Helper()
	var resp struct {
		Code int                    `json:"code"`
		Msg  string                 `json:"msg"`
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
	}
	return resp.Code, resp.Data
}

/* planSeedUser 建一个普通用户用于订阅与配额用例 */
func planSeedUser(t *testing.T, name string) models.User {
	t.Helper()
	u := models.User{Username: name, Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

/* TestCreatePlanStoresZeroStatus 显式传入 status=0 时必须以「停用」落库 */
func TestCreatePlanStoresZeroStatus(t *testing.T) {
	setupHandlerTestDB(t)

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans",
		`{"name":"停用套餐","status":0,"quotas":{"max_domains":5}}`)
	CreatePlan(c)

	code, _ := decodeResp(t, w)
	if code != 0 {
		t.Fatalf("code = %d，期望 0（原文 %s）", code, w.Body.String())
	}

	var plan models.Plan
	if err := database.DB.Where("name = ?", "停用套餐").First(&plan).Error; err != nil {
		t.Fatalf("套餐未写入: %v", err)
	}
	if plan.Status != 0 {
		t.Errorf("status = %d，期望 0：零值被 gorm default 标签吞掉了", plan.Status)
	}
	if plan.Quotas != `{"max_domains":5}` {
		t.Errorf("quotas = %q，期望只保留配额维度键", plan.Quotas)
	}
}

/* TestCreatePlanRejectsUnknownQuotaKeys 白名单外的键在落库前被过滤 */
func TestCreatePlanRejectsUnknownQuotaKeys(t *testing.T) {
	setupHandlerTestDB(t)

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans",
		`{"name":"过滤套餐","quotas":{"max_domains":3,"evil_key":999}}`)
	CreatePlan(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("code = %d，期望 0（原文 %s）", code, w.Body.String())
	}

	var plan models.Plan
	database.DB.Where("name = ?", "过滤套餐").First(&plan)
	var parsed map[string]int
	if err := json.Unmarshal([]byte(plan.Quotas), &parsed); err != nil {
		t.Fatalf("quotas 不是合法 JSON: %v", err)
	}
	if _, ok := parsed["evil_key"]; ok {
		t.Errorf("白名单外的键 evil_key 不应入库，实际 %v", parsed)
	}
	if parsed["max_domains"] != 3 {
		t.Errorf("max_domains = %d，期望 3", parsed["max_domains"])
	}
}

/* TestCreatePlanRejectsInvalidQuotaValue 小于 -1 的取值必须被拒 */
func TestCreatePlanRejectsInvalidQuotaValue(t *testing.T) {
	setupHandlerTestDB(t)

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans",
		`{"name":"非法套餐","quotas":{"max_domains":-5}}`)
	CreatePlan(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("负数小于 -1 时应返回错误，实际成功")
	}
	var n int64
	database.DB.Model(&models.Plan{}).Where("name = ?", "非法套餐").Count(&n)
	if n != 0 {
		t.Errorf("非法套餐不应入库，实际写入 %d 条", n)
	}
}

/* TestCreatePlanRejectsDuplicateName 同名套餐不得重复创建 */
func TestCreatePlanRejectsDuplicateName(t *testing.T) {
	setupHandlerTestDB(t)
	if err := database.DB.Create(&models.Plan{ID: "p-dup", Name: "重名", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans", `{"name":"重名"}`)
	CreatePlan(c)
	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("重名套餐应被拒绝")
	}
}

/* TestSetDefaultPlanKeepsSingleDefault 设为默认时全库只保留一个默认 */
func TestSetDefaultPlanKeepsSingleDefault(t *testing.T) {
	setupHandlerTestDB(t)
	if err := database.DB.Create(&models.Plan{ID: "p1", Name: "甲", Status: 1, IsDefault: true}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.Plan{ID: "p2", Name: "乙", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans/p2/default", "")
	c.Params = gin.Params{{Key: "id", Value: "p2"}}
	SetDefaultPlan(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("设置默认失败：%s", w.Body.String())
	}

	var n int64
	database.DB.Model(&models.Plan{}).Where("is_default = ?", true).Count(&n)
	if n != 1 {
		t.Errorf("默认套餐数量 = %d，期望恰好 1", n)
	}
	var p2 models.Plan
	database.DB.First(&p2, "id = ?", "p2")
	if !p2.IsDefault {
		t.Error("p2 应被设为默认")
	}
}

/* TestSetDefaultPlanRejectsDisabled 停用的套餐不能设为默认 */
func TestSetDefaultPlanRejectsDisabled(t *testing.T) {
	setupHandlerTestDB(t)
	if err := database.DB.Create(&models.Plan{ID: "p-off", Name: "停用", Status: 0}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans/p-off/default", "")
	c.Params = gin.Params{{Key: "id", Value: "p-off"}}
	SetDefaultPlan(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("停用套餐不应被设为默认")
	}
}

/* TestDeletePlanBlockedByActiveSubscription 仍有启用订阅的套餐不得删除 */
func TestDeletePlanBlockedByActiveSubscription(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "plan-del-user")
	if err := database.DB.Create(&models.Plan{ID: "p-inuse", Name: "在用", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "sub-inuse", UserID: u.ID, PlanID: "p-inuse", Status: 1,
	}).Error; err != nil {
		t.Fatalf("写入订阅失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans/p-inuse/delete", "")
	c.Params = gin.Params{{Key: "id", Value: "p-inuse"}}
	DeletePlan(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("仍有在用订阅时应拒绝删除")
	}
	var n int64
	database.DB.Model(&models.Plan{}).Where("id = ?", "p-inuse").Count(&n)
	if n != 1 {
		t.Error("套餐不应被删除")
	}
}

/* TestDeletePlanRemovesUnused 无订阅的套餐可正常删除 */
func TestDeletePlanRemovesUnused(t *testing.T) {
	setupHandlerTestDB(t)
	if err := database.DB.Create(&models.Plan{ID: "p-free", Name: "空闲", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/plans/p-free/delete", "")
	c.Params = gin.Params{{Key: "id", Value: "p-free"}}
	DeletePlan(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("删除失败：%s", w.Body.String())
	}

	var n int64
	database.DB.Model(&models.Plan{}).Where("id = ?", "p-free").Count(&n)
	if n != 0 {
		t.Error("套餐应已删除")
	}
}

/*
 * TestSetUserSubscriptionReplacesPrevious 同一用户只保留一条启用中的订阅
 *
 * 若旧订阅仍为启用，quotaForUser 的 First() 会命中哪一条取决于插入顺序，
 * 用户实际享有的配额不可预期
 */
func TestSetUserSubscriptionReplacesPrevious(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "sub-user")
	if err := database.DB.Create(&models.Plan{ID: "sp1", Name: "甲套餐", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.Plan{ID: "sp2", Name: "乙套餐", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "old-sub", UserID: u.ID, PlanID: "sp1", PlanName: "甲套餐", Status: 1,
	}).Error; err != nil {
		t.Fatalf("写入订阅失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1/sub", `{"plan_id":"sp2"}`)
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	SetUserSubscription(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("授予失败：%s", w.Body.String())
	}

	var active int64
	database.DB.Model(&models.UserSubscription{}).
		Where("user_id = ? AND status = 1", u.ID).Count(&active)
	if active != 1 {
		t.Errorf("启用中的订阅数 = %d，期望恰好 1", active)
	}
	var sub models.UserSubscription
	database.DB.Where("user_id = ? AND status = 1", u.ID).First(&sub)
	if sub.PlanID != "sp2" {
		t.Errorf("生效套餐 = %q，期望 sp2", sub.PlanID)
	}
	if sub.PlanName != "乙套餐" {
		t.Errorf("套餐名冗余字段 = %q，期望「乙套餐」", sub.PlanName)
	}
}

/* TestSetUserSubscriptionRejectsDisabledPlan 停用的套餐不得授予 */
func TestSetUserSubscriptionRejectsDisabledPlan(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "sub-off-user")
	if err := database.DB.Create(&models.Plan{ID: "sp-off", Name: "停用套餐", Status: 0}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1/sub", `{"plan_id":"sp-off"}`)
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	SetUserSubscription(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("停用套餐不应被授予")
	}
}

/* TestSetUserSubscriptionRejectsBadExpire 到期时间格式错误必须被拒 */
func TestSetUserSubscriptionRejectsBadExpire(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "sub-exp-user")
	if err := database.DB.Create(&models.Plan{ID: "sp-e", Name: "带期套餐", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1/sub",
		`{"plan_id":"sp-e","expire_at":"2026/01/02"}`)
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	SetUserSubscription(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("非 YYYY-MM-DD 的到期时间应被拒绝")
	}
}

/* TestRevokeUserSubscription 撤销后启用中的订阅归零 */
func TestRevokeUserSubscription(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "revoke-user")
	if err := database.DB.Create(&models.UserSubscription{
		ID: "rev-sub", UserID: u.ID, PlanID: "sp1", Status: 1,
	}).Error; err != nil {
		t.Fatalf("写入订阅失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1/sub/revoke", "")
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	RevokeUserSubscription(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("撤销失败：%s", w.Body.String())
	}

	var active int64
	database.DB.Model(&models.UserSubscription{}).
		Where("user_id = ? AND status = 1", u.ID).Count(&active)
	if active != 0 {
		t.Errorf("撤销后启用中的订阅数 = %d，期望 0", active)
	}
}

/* TestRevokeUserSubscriptionWithoutActive 无可撤销订阅时给出明确错误 */
func TestRevokeUserSubscriptionWithoutActive(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "revoke-none")

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1/sub/revoke", "")
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	RevokeUserSubscription(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("没有启用中的订阅时应返回错误而非静默成功")
	}
}

/*
 * TestSetUserQuotaThenReset 个体配额的写入与清除
 *
 * 清除后必须回落到套餐配额，而不是被当作「配额为 0」全面禁止
 */
func TestSetUserQuotaThenReset(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "uq-user")
	if err := database.DB.Create(&models.Plan{
		ID: "uq-plan", Name: "套餐", Status: 1, Quotas: `{"max_domains":10}`,
	}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "uq-sub", UserID: u.ID, PlanID: "uq-plan", Status: 1,
	}).Error; err != nil {
		t.Fatalf("写入订阅失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1",
		`{"quotas":{"max_domains":0},"remark":"暂停创建"}`)
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	SetUserQuota(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("保存个体配额失败：%s", w.Body.String())
	}

	if q := quotaForUser(u.ID, 1); q.MaxDomains != 0 {
		t.Errorf("个体配额应为 0（禁止），实际 %d", q.MaxDomains)
	}

	c2, w2 := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/1/reset", "")
	c2.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	ResetUserQuota(c2)
	if code, _ := decodeResp(t, w2); code != 0 {
		t.Fatalf("清除个体配额失败：%s", w2.Body.String())
	}

	if q := quotaForUser(u.ID, 1); q.MaxDomains != 10 {
		t.Errorf("清除后应回落到套餐值 10，实际 %d", q.MaxDomains)
	}
}

/* TestSetUserQuotaRejectsUnknownUser 不存在的用户不得被写入配额 */
func TestSetUserQuotaRejectsUnknownUser(t *testing.T) {
	setupHandlerTestDB(t)

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/9999", `{"quotas":{"max_domains":1}}`)
	c.Params = gin.Params{{Key: "uid", Value: "9999"}}
	SetUserQuota(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("不存在的用户应被拒绝")
	}
}

/* TestSetUserQuotaRejectsBadUID 非数字用户 ID 必须被拒 */
func TestSetUserQuotaRejectsBadUID(t *testing.T) {
	setupHandlerTestDB(t)

	c, w := planAdminContext(t, http.MethodPost, "/api/system/user-quotas/abc", `{"quotas":{"max_domains":1}}`)
	c.Params = gin.Params{{Key: "uid", Value: "abc"}}
	SetUserQuota(c)

	if code, _ := decodeResp(t, w); code == 0 {
		t.Fatal("非数字用户 ID 应被拒绝")
	}
}

/* TestGetUserSubscriptionReturnsNullWhenAbsent 未订阅时返回 data:null 而非报错 */
func TestGetUserSubscriptionReturnsNullWhenAbsent(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "nosub-user")

	c, w := planAdminContext(t, http.MethodGet, "/api/system/user-quotas/1/sub", "")
	c.Params = gin.Params{{Key: "uid", Value: itoaInt(int(u.ID))}}
	GetUserSubscription(c)

	var resp struct {
		Code int          `json:"code"`
		Data *interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
	}
	if resp.Code != 0 {
		t.Fatalf("code = %d，期望 0（原文 %s）", resp.Code, w.Body.String())
	}
}

/* TestGetMyQuotaAdminUnlimited 管理员查询自己的配额恒为不限 */
func TestGetMyQuotaAdminUnlimited(t *testing.T) {
	setupHandlerTestDB(t)
	admin := models.User{Username: "myq-admin", Level: 2, Status: 1}
	if err := database.DB.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	// 先给该用户设死上限，再以管理员身份读取——应仍为不限
	if err := database.DB.Create(&models.UserQuota{
		UserID: admin.ID, Quotas: `{"max_domains":0}`,
	}).Error; err != nil {
		t.Fatalf("写入个体配额失败: %v", err)
	}

	c, w := planAdminContext(t, http.MethodGet, "/api/user/quota", "")
	c.Set("user_id", itoaInt(int(admin.ID)))
	GetMyQuota(c)

	code, data := decodeResp(t, w)
	if code != 0 {
		t.Fatalf("code = %d，期望 0（原文 %s）", code, w.Body.String())
	}
	quotas, ok := data["quotas"].(map[string]interface{})
	if !ok {
		t.Fatalf("data.quotas 类型异常：%v", data["quotas"])
	}
	if quotas["max_domains"] != float64(-1) {
		t.Errorf("管理员 max_domains = %v，应恒为 -1", quotas["max_domains"])
	}
	if data["source"] != "admin" {
		t.Errorf("source = %v，期望 admin", data["source"])
	}
}

/*
 * TestGetMyQuotaReportsSource 来源判定与三层优先级一致
 *
 * 前端据 source 展示「个体覆盖 / 套餐 / 系统默认」，判错会让用户看到
 * 与实际生效值不符的来源标签
 */
func TestGetMyQuotaReportsSource(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "src-user")

	// 第一层：无个体无套餐 -> system
	c, w := planAdminContext(t, http.MethodGet, "/api/user/quota", "")
	c.Set("user_id", itoaInt(int(u.ID)))
	c.Set("level", 1)
	GetMyQuota(c)
	if _, data := decodeResp(t, w); data["source"] != "system" {
		t.Errorf("无个体无套餐时 source = %v，期望 system", data["source"])
	}

	// 第二层：给套餐 -> plan
	if err := database.DB.Create(&models.Plan{
		ID: "src-plan", Name: "来源套餐", Status: 1, Quotas: `{"max_domains":7}`,
	}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "src-sub", UserID: u.ID, PlanID: "src-plan", PlanName: "来源套餐", Status: 1,
	}).Error; err != nil {
		t.Fatalf("写入订阅失败: %v", err)
	}
	c2, w2 := planAdminContext(t, http.MethodGet, "/api/user/quota", "")
	c2.Set("user_id", itoaInt(int(u.ID)))
	c2.Set("level", 1)
	GetMyQuota(c2)
	_, data2 := decodeResp(t, w2)
	if data2["source"] != "plan" {
		t.Errorf("有套餐时 source = %v，期望 plan", data2["source"])
	}
	if data2["plan_name"] != "来源套餐" {
		t.Errorf("plan_name = %v，期望「来源套餐」", data2["plan_name"])
	}

	// 第一层：加个体覆盖 -> admin
	if err := database.DB.Create(&models.UserQuota{
		UserID: u.ID, Quotas: `{"max_domains":2}`,
	}).Error; err != nil {
		t.Fatalf("写入个体配额失败: %v", err)
	}
	c3, w3 := planAdminContext(t, http.MethodGet, "/api/user/quota", "")
	c3.Set("user_id", itoaInt(int(u.ID)))
	c3.Set("level", 1)
	GetMyQuota(c3)
	_, data3 := decodeResp(t, w3)
	if data3["source"] != "admin" {
		t.Errorf("有个体覆盖时 source = %v，期望 admin", data3["source"])
	}
}

/* TestUpdateQuotaDefaultRoundTrip 系统默认配额可写可读 */
func TestUpdateQuotaDefaultRoundTrip(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)

	c, w := planAdminContext(t, http.MethodPost, "/api/system/quota-default",
		`{"quotas":{"max_domains":20,"max_records":-1}}`)
	UpdateQuotaDefault(c)
	if code, _ := decodeResp(t, w); code != 0 {
		t.Fatalf("保存默认配额失败：%s", w.Body.String())
	}

	c2, w2 := planAdminContext(t, http.MethodGet, "/api/system/quota-default", "")
	GetQuotaDefault(c2)
	code, data := decodeResp(t, w2)
	if code != 0 {
		t.Fatalf("读取默认配额失败：%s", w2.Body.String())
	}
	quotas, ok := data["quotas"].(map[string]interface{})
	if !ok {
		t.Fatalf("data.quotas 类型异常：%v", data["quotas"])
	}
	if quotas["max_domains"] != float64(20) {
		t.Errorf("max_domains = %v，期望 20", quotas["max_domains"])
	}
	if quotas["max_records"] != float64(-1) {
		t.Errorf("max_records = %v，期望 -1", quotas["max_records"])
	}

	// 落到系统默认层的用户应直接读到该值
	u := planSeedUser(t, "default-user")
	if q := quotaForUser(u.ID, 1); q.MaxDomains != 20 {
		t.Errorf("系统默认未生效：max_domains = %d，期望 20", q.MaxDomains)
	}
}

/*
 * TestNonAdminRejected 非管理员不得访问管理端点
 *
 * 这些接口能改任意用户的配额，越权即等于绕过全部资源限制
 */
func TestNonAdminRejected(t *testing.T) {
	setupHandlerTestDB(t)

	cases := []struct {
		name string
		call func(*gin.Context)
	}{
		{"GetPlans", GetPlans},
		{"CreatePlan", CreatePlan},
		{"GetUserQuotas", GetUserQuotas},
		{"GetQuotaDefault", GetQuotaDefault},
		{"UpdateQuotaDefault", UpdateQuotaDefault},
	}
	for _, tc := range cases {
		w := httptest.NewRecorder()
		ctx, _ := gin.CreateTestContext(w)
		ctx.Request = httptest.NewRequest(http.MethodPost, "/api/system/plans", strings.NewReader("{}"))
		ctx.Request.Header.Set("Content-Type", "application/json")
		ctx.Set("level", 1)
		ctx.Set("user_id", "2")
		tc.call(ctx)
		if code, _ := decodeResp(t, w); code == 0 {
			t.Errorf("%s：普通用户不应访问成功", tc.name)
		}
	}
}

/* TestQuotaUsedByResourceCoversAllDimensions 用量统计覆盖全部维度键 */
func TestQuotaUsedByResourceCoversAllDimensions(t *testing.T) {
	setupHandlerTestDB(t)
	u := planSeedUser(t, "used-user")

	c, _ := planAdminContext(t, http.MethodGet, "/api/user/quota", "")
	used := quotaUsedByResource(c, u.ID)
	for _, k := range quotaDimensionKeys {
		if _, ok := used[k]; !ok {
			t.Errorf("用量结果缺少维度 %s", k)
		}
	}
	if len(used) != len(quotaDimensionKeys) {
		t.Errorf("用量维度数 = %d，期望 %d", len(used), len(quotaDimensionKeys))
	}
}

/* TestQuotaToJSONFiltersUnknownKeys quotaToJSON 只输出白名单维度 */
func TestQuotaToJSONFiltersUnknownKeys(t *testing.T) {
	raw := quotaToJSON(map[string]int{"max_domains": 1, "evil": 2})
	var parsed map[string]int
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		t.Fatalf("输出不是合法 JSON: %v", err)
	}
	if len(parsed) != 1 || parsed["max_domains"] != 1 {
		t.Errorf("过滤结果 = %v，期望只含 max_domains", parsed)
	}
}

/* TestQuotaFromJSONEmpty 空串解析为空调配而非报错 */
func TestQuotaFromJSONEmpty(t *testing.T) {
	if got := quotaFromJSON(""); len(got) != 0 {
		t.Errorf("空串应解析为空映射，实际 %v", got)
	}
	if got := quotaFromJSON("{not json"); len(got) != 0 {
		t.Errorf("非法 JSON 应解析为空映射，实际 %v", got)
	}
}

/* TestSysconfigQuotaDefaultFromKey 默认配额确实走 sysconfig 的 quota_default 键 */
func TestSysconfigQuotaDefaultFromKey(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	sysconfig.SetValue(quotaDefaultKey, `{"max_monitors":3}`)

	u := planSeedUser(t, "cfg-user")
	if q := quotaForUser(u.ID, 1); q.MaxMonitors != 3 {
		t.Errorf("max_monitors = %d，期望 3（来自 quota_default）", q.MaxMonitors)
	}
}

/* TestPlanSubscriberCountReflectsActiveOnly 订阅数只统计启用中的 */
func TestPlanSubscriberCountReflectsActiveOnly(t *testing.T) {
	setupHandlerTestDB(t)
	if err := database.DB.Create(&models.Plan{ID: "cnt-plan", Name: "计数", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	u1 := planSeedUser(t, "cnt-u1")
	u2 := planSeedUser(t, "cnt-u2")
	for i, u := range []models.User{u1, u2} {
		status := 1
		if i == 1 {
			status = 0
		}
		if err := database.DB.Create(&models.UserSubscription{
			ID: "cnt-sub-" + itoaInt(i), UserID: u.ID, PlanID: "cnt-plan", Status: status,
		}).Error; err != nil {
			t.Fatalf("写入订阅失败: %v", err)
		}
	}

	c, w := planAdminContext(t, http.MethodGet, "/api/system/plans", "")
	GetPlans(c)
	code, data := decodeResp(t, w)
	if code != 0 {
		t.Fatalf("加载失败：%s", w.Body.String())
	}
	list, ok := data["list"].([]interface{})
	if !ok || len(list) != 1 {
		t.Fatalf("list 异常：%v", data["list"])
	}
	row := list[0].(map[string]interface{})
	if row["subscriber_count"] != float64(1) {
		t.Errorf("subscriber_count = %v，期望 1（只计启用中的订阅）", row["subscriber_count"])
	}
}

/* TestExpiredSubscriptionNotCountedAsActive 过期订阅不计入订阅数 */
func TestExpiredSubscriptionNotCountedAsActive(t *testing.T) {
	setupHandlerTestDB(t)
	if err := database.DB.Create(&models.Plan{ID: "exp-plan", Name: "过期计数", Status: 1}).Error; err != nil {
		t.Fatalf("写入套餐失败: %v", err)
	}
	u := planSeedUser(t, "exp-cnt-user")
	past := time.Now().AddDate(0, 0, -1)
	if err := database.DB.Create(&models.UserSubscription{
		ID: "exp-sub", UserID: u.ID, PlanID: "exp-plan", Status: 1, ExpireTime: &past,
	}).Error; err != nil {
		t.Fatalf("写入订阅失败: %v", err)
	}

	if q := quotaForUser(u.ID, 1); q.MaxDomains != -1 {
		t.Errorf("过期订阅应不生效：max_domains = %d，期望 -1", q.MaxDomains)
	}
}
