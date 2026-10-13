package handler

import (
	"encoding/json"
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 个体覆盖的边界：只有显式声明的维度才算覆盖
 *
 * 「设置个体配额」弹窗此前用 GetUserQuotas 返回的**生效值**预填表单，
 * 而生效值是「个体覆盖 > 套餐 > 系统默认」三层合并的结果。管理员改一个维度再保存，
 * 会把预填的其余维度一并写成个体覆盖——此后调整套餐，该用户永远停在旧值，
 * 而管理员并不知道自己覆盖过这些维度。
 *
 * 修法是后端额外暴露 overlays（只含真正被覆盖的维度），前端据此预填。
 * 这些用例锁住 overlays 的口径
 */

/* TestOverlayQuotaMapOnlyExplicitKeys 未在 JSON 里出现的维度不得出现在覆盖集里 */
func TestOverlayQuotaMapOnlyExplicitKeys(t *testing.T) {
	got := overlayQuotaMap(`{"max_domains":5}`)

	if v, ok := got["max_domains"]; !ok || v != 5 {
		t.Errorf("显式声明的维度应被保留，实际 %v", got)
	}
	if _, ok := got["max_records"]; ok {
		t.Errorf("未声明的维度不应出现在覆盖集里，实际 %v", got)
	}
	if len(got) != 1 {
		t.Errorf("覆盖集应只含 1 个维度，实际 %d 个：%v", len(got), got)
	}
}

/* TestOverlayQuotaMapDistinguishesZeroFromAbsent 声明为 0 与未声明是两回事 */
func TestOverlayQuotaMapDistinguishesZeroFromAbsent(t *testing.T) {
	got := overlayQuotaMap(`{"max_domains":0}`)

	v, ok := got["max_domains"]
	if !ok {
		t.Fatal("显式声明为 0 的维度应被保留：0 表示禁止创建，不是「未覆盖」")
	}
	if v != 0 {
		t.Errorf("值应为 0，实际 %d", v)
	}
}

/* TestOverlayQuotaMapEmptyInput 空配置与坏 JSON 都返回空集，不返回 nil */
func TestOverlayQuotaMapEmptyInput(t *testing.T) {
	for _, raw := range []string{"", "   ", "not json", "[]"} {
		got := overlayQuotaMap(raw)
		if got == nil {
			t.Errorf("输入 %q 时应返回空集而非 nil，否则前端需要额外判空", raw)
		}
		if len(got) != 0 {
			t.Errorf("输入 %q 时应为空集，实际 %v", raw, got)
		}
	}
}

/* TestOverlayQuotaMapIgnoresUnknownKeys 未知键不得混入覆盖集 */
func TestOverlayQuotaMapIgnoresUnknownKeys(t *testing.T) {
	got := overlayQuotaMap(`{"max_domains":5,"not_a_dimension":99}`)

	if _, ok := got["not_a_dimension"]; ok {
		t.Errorf("未知键不应进入覆盖集，实际 %v", got)
	}
}

/*
 * TestOverlaysExcludePlanContributedDimensions 覆盖集必须排除套餐贡献的维度
 *
 * 这是整个修复的关键区分：生效值含套餐贡献的维度，用它会误导前端把套餐值固化下来
 */
func TestOverlaysExcludePlanContributedDimensions(t *testing.T) {
	setupHandlerTestDB(t)

	// 套餐给 max_domains=5，个体覆盖只给 max_records=100
	plan := models.Plan{ID: "p-overlay", Name: "基础版", Status: 1, Quotas: `{"max_domains":5}`}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建套餐失败: %v", err)
	}
	uid := newTestUser(t, "overlay-user", 1)
	if err := database.DB.Create(&models.UserSubscription{
		ID: "sub-overlay", UserID: uid, PlanID: plan.ID, PlanName: plan.Name, Status: 1,
	}).Error; err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	if err := database.DB.Create(&models.UserQuota{
		UserID: uid, Quotas: `{"max_records":100}`,
	}).Error; err != nil {
		t.Fatalf("创建个体覆盖失败: %v", err)
	}

	effective := quotaToMap(quotaForUser(uid, 1))
	if effective["max_domains"] != 5 {
		t.Fatalf("前提不成立：生效值里 max_domains 应来自套餐的 5，实际 %d", effective["max_domains"])
	}
	if effective["max_records"] != 100 {
		t.Fatalf("前提不成立：生效值里 max_records 应为个体覆盖的 100，实际 %d", effective["max_records"])
	}

	// 覆盖集的口径：只读个体覆盖那一层的原始 JSON
	overlays := overlayQuotaMap(`{"max_records":100}`)
	if _, ok := overlays["max_domains"]; ok {
		t.Errorf("max_domains 来自套餐，不应出现在覆盖集里：%v", overlays)
	}
	if overlays["max_records"] != 100 {
		t.Errorf("max_records 是个体覆盖，应在覆盖集里：%v", overlays)
	}
	if _, err := json.Marshal(overlays); err != nil {
		t.Errorf("覆盖集应可序列化: %v", err)
	}
}
