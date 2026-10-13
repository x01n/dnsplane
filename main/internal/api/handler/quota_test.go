package handler

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 配额的解析与判定
 *
 * 重点验证三层优先级的覆盖顺序与「未配置即不限」的兼容约定——
 * 后者若失效，存量实例升级后会因为新增配额维度而突然无法创建资源
 */

/* quotaTestUser 建一个普通用户 */
func quotaTestUser(t *testing.T, name string) models.User {
	t.Helper()
	u := models.User{Username: name, Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

/*
 * 未配置任何配额时全维度不限
 *
 * 这是兼容性底线：升级后既没有套餐也没有个体覆盖的实例，
 * 所有创建操作必须照常可用
 */
func TestQuotaDefaultsToUnlimited(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaTestUser(t, "q-default")

	q := quotaForUser(u.ID, 1)
	for name, v := range map[string]int{
		"domains": q.MaxDomains, "records": q.MaxRecords, "accounts": q.MaxAccounts,
		"monitors": q.MaxMonitors, "certs": q.MaxCerts, "schedules": q.MaxSchedules,
		"deploys": q.MaxDeploys, "optimize_ip": q.MaxOptimizeIP,
	} {
		if v != -1 {
			t.Errorf("维度 %s = %d，未配置时应为 -1（不限）", name, v)
		}
	}
}

/*
 * 管理员不受配额约束
 *
 * 管理员是配额的管理者：若管理员也受限，配额耗尽的实例将无法再由管理员
 * 补建任何资源来排障
 */
func TestQuotaAdminUnlimited(t *testing.T) {
	setupHandlerTestDB(t)

	// 先经个体配额给该用户设死上限，再以管理员身份读取——应仍为不限
	admin := models.User{Username: "q-admin", Level: 2, Status: 1}
	if err := database.DB.Create(&admin).Error; err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	if err := database.DB.Create(&models.UserQuota{
		UserID: admin.ID, Quotas: `{"max_domains":0}`,
	}).Error; err != nil {
		t.Fatalf("写入个体配额失败: %v", err)
	}

	if q := quotaForUser(admin.ID, 2); q.MaxDomains != -1 {
		t.Errorf("管理员配额 = %d，应恒为 -1（不受限）", q.MaxDomains)
	}
}

/* 个体配额覆盖套餐配额 */
func TestQuotaUserOverrideBeatsPlan(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaTestUser(t, "q-override")

	plan := models.Plan{
		ID: "plan-a", Name: "基础", Status: 1,
		Quotas: `{"max_domains":5,"max_records":10}`,
	}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "sub-a", UserID: u.ID, PlanID: plan.ID, Status: 1,
	}).Error; err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	// 未加个体覆盖时应取套餐值
	if q := quotaForUser(u.ID, 1); q.MaxDomains != 5 || q.MaxRecords != 10 {
		t.Fatalf("套餐配额未生效：domains=%d records=%d", q.MaxDomains, q.MaxRecords)
	}

	// 个体覆盖只改一个维度，另一维度应保持套餐值
	if err := database.DB.Create(&models.UserQuota{
		UserID: u.ID, Quotas: `{"max_domains":2}`,
	}).Error; err != nil {
		t.Fatalf("写入个体配额失败: %v", err)
	}

	q := quotaForUser(u.ID, 1)
	if q.MaxDomains != 2 {
		t.Errorf("个体覆盖未生效：domains=%d，期望 2", q.MaxDomains)
	}
	if q.MaxRecords != 10 {
		t.Errorf("未覆盖的维度应保持套餐值：records=%d，期望 10", q.MaxRecords)
	}
}

/*
 * 过期订阅回落为不限
 *
 * 订阅过期后不应继续享有套餐配额，但也不该被当作「配额为 0」而全面禁止——
 * 回落到系统默认（未配置即不限）才是可预期的行为
 */
func TestQuotaExpiredSubscriptionIgnored(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaTestUser(t, "q-expired")

	plan := models.Plan{ID: "plan-b", Name: "已过期", Status: 1, Quotas: `{"max_domains":1}`}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建套餐失败: %v", err)
	}
	past := nowFunc().AddDate(0, 0, -1)
	if err := database.DB.Create(&models.UserSubscription{
		ID: "sub-b", UserID: u.ID, PlanID: plan.ID, Status: 1, ExpireTime: &past,
	}).Error; err != nil {
		t.Fatalf("创建过期订阅失败: %v", err)
	}

	if q := quotaForUser(u.ID, 1); q.MaxDomains != -1 {
		t.Errorf("过期订阅应被忽略：domains=%d，期望 -1", q.MaxDomains)
	}
}

/*
 * 停用的套餐不生效
 *
 * 与过期订阅同理：套餐被停用（status=0）后其配额不再生效，
 * 否则管理员停用套餐想收回权限时，用户仍照旧享有
 */
func TestQuotaDisabledPlanIgnored(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaTestUser(t, "q-disabled")

	plan := models.Plan{ID: "plan-c", Name: "已停用", Status: 0, Quotas: `{"max_domains":1}`}
	/*
	 * Select("*") 确保 Status:0 真正落库
	 *
	 * 模型现已去掉 gorm:"default:1" 标签（见 models/quota.go 的说明），普通 Create
	 * 也能写入零值；这里保留 Select("*") 是为了让本用例不受将来重新引入 default
	 * 标签的影响——那条注释记录的正是这个坑：带 default 的字段会被 GORM 跳过，
	 * 传 0 存成 1，管理员停用套餐的意图丢失
	 */
	if err := database.DB.Select("*").Create(&plan).Error; err != nil {
		t.Fatalf("创建套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "sub-c", UserID: u.ID, PlanID: plan.ID, Status: 1,
	}).Error; err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	if q := quotaForUser(u.ID, 1); q.MaxDomains != -1 {
		t.Errorf("停用套餐应被忽略：domains=%d，期望 -1", q.MaxDomains)
	}
}

/*
 * 新增维度不破坏存量套餐
 *
 * 存量套餐的 JSON 里没有 max_deploys 这类新键，解析时应保持「不限」而非归零，
 * 否则加一个维度就会让所有老套餐用户被静默禁止使用该功能
 */
func TestQuotaMissingDimensionStaysUnlimited(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaTestUser(t, "q-legacy-plan")

	// 模拟遗留套餐：只有旧维度
	plan := models.Plan{
		ID: "plan-legacy", Name: "遗留", Status: 1,
		Quotas: `{"max_domains":-1,"max_subdomains":-1,"max_monitors":-1,"max_certs":-1,"max_schedules":-1,"max_accounts":-1}`,
	}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建遗留套餐失败: %v", err)
	}
	if err := database.DB.Create(&models.UserSubscription{
		ID: "sub-legacy", UserID: u.ID, PlanID: plan.ID, Status: 1,
	}).Error; err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}

	q := quotaForUser(u.ID, 1)
	if q.MaxDeploys != -1 {
		t.Errorf("遗留套餐未声明 max_deploys，应为 -1（不限），实际 %d", q.MaxDeploys)
	}
	if q.MaxRecords != -1 {
		t.Errorf("遗留套餐未声明 max_records，应为 -1（不限），实际 %d", q.MaxRecords)
	}
}

/* applyQuotaJSON 的覆盖语义 */
func TestApplyQuotaJSON(t *testing.T) {
	q := unlimitedQuota()
	applyQuotaJSON(&q, `{"max_domains":3,"max_records":0}`)

	if q.MaxDomains != 3 {
		t.Errorf("max_domains = %d，期望 3", q.MaxDomains)
	}
	if q.MaxRecords != 0 {
		t.Errorf("max_records = %d，期望 0（禁止创建）", q.MaxRecords)
	}
	if q.MaxAccounts != -1 {
		t.Errorf("未声明的维度应为 -1，实际 %d", q.MaxAccounts)
	}

	// 非法 JSON 不应改变已有值
	q2 := Quota{MaxDomains: 7}
	applyQuotaJSON(&q2, `{not json`)
	if q2.MaxDomains != 7 {
		t.Errorf("非法 JSON 不应改动配额，实际 %d", q2.MaxDomains)
	}
}

/* 超限提示要带上当前用量与上限，便于用户与管理员判断 */
func TestQuotaExceededMessage(t *testing.T) {
	msg := quotaExceededMessage("records", 2, 2)
	if msg == "" {
		t.Fatal("超限提示不应为空")
	}
	for _, want := range []string{"子域名", "2/2"} {
		if !containsStr(msg, want) {
			t.Errorf("提示 %q 应包含 %q", msg, want)
		}
	}
}

/* containsStr 子串判定，避免为非测试代码引入 strings 依赖 */
func containsStr(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
