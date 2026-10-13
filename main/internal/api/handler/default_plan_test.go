package handler

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 默认套餐必须真的被新用户拿到
 *
 * 在接通消费之前，SetDefaultPlan 写下的 is_default 标记没有任何读取点：
 * quotaForUser 的三层取值（个体覆盖 → 套餐 → 系统默认）里不含默认套餐，
 * 注册链路也不读它。界面文案却写着「默认套餐会在新用户注册时自动参考」。
 *
 * 这几个用例锁住「标记 → 订阅」这一步真实发生，以及不该发生的三种情况
 */

/* seedDefaultPlan 造一个启用中的默认套餐 */
func seedDefaultPlan(t *testing.T, id, name string, status int) models.Plan {
	t.Helper()
	plan := models.Plan{ID: id, Name: name, Status: status, IsDefault: true, Quotas: `{"max_domains":5}`}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建默认套餐失败: %v", err)
	}
	return plan
}

/* newTestUser 造一个普通用户并返回其 ID */
func newTestUser(t *testing.T, name string, level int) uint {
	t.Helper()
	u := models.User{Username: name, Email: name + "@example.com", Level: level, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u.ID
}

/* TestGrantDefaultPlanCreatesSubscription 有默认套餐时，新用户应拿到一条启用中的订阅 */
func TestGrantDefaultPlanCreatesSubscription(t *testing.T) {
	setupHandlerTestDB(t)
	plan := seedDefaultPlan(t, "p-default", "基础版", 1)
	uid := newTestUser(t, "newcomer", 1)

	grantDefaultPlanIfAny(uid)

	var sub models.UserSubscription
	if err := database.DB.Where("user_id = ? AND status = 1", uid).First(&sub).Error; err != nil {
		t.Fatalf("新用户没有拿到默认套餐: %v", err)
	}
	if sub.PlanID != plan.ID {
		t.Errorf("订阅指向的套餐应为 %s，实际 %s", plan.ID, sub.PlanID)
	}
	if sub.ExpireTime != nil {
		t.Errorf("默认套餐应为永久（ExpireTime 为空），实际 %v", sub.ExpireTime)
	}
}

/* TestGrantDefaultPlanSkipsWhenPlanDisabled 默认套餐被停用时不应授予 */
func TestGrantDefaultPlanSkipsWhenPlanDisabled(t *testing.T) {
	setupHandlerTestDB(t)
	seedDefaultPlan(t, "p-off", "已停用", 0)
	uid := newTestUser(t, "skipped", 1)

	grantDefaultPlanIfAny(uid)

	var n int64
	database.DB.Model(&models.UserSubscription{}).Where("user_id = ?", uid).Count(&n)
	if n != 0 {
		t.Errorf("停用的默认套餐不应被授予，实际落了 %d 条订阅", n)
	}
}

/* TestGrantDefaultPlanNoopWhenNoDefault 没有默认套餐时是静默无操作，不是错误 */
func TestGrantDefaultPlanNoopWhenNoDefault(t *testing.T) {
	setupHandlerTestDB(t)
	uid := newTestUser(t, "no-default", 1)

	grantDefaultPlanIfAny(uid)

	var n int64
	database.DB.Model(&models.UserSubscription{}).Where("user_id = ?", uid).Count(&n)
	if n != 0 {
		t.Errorf("无默认套餐时不应产生订阅，实际 %d 条", n)
	}
}

/*
 * TestDefaultPlanFeedsQuotaForUser 默认套餐的配额必须真的进入 quotaForUser 的套餐层
 *
 * 前面的用例只证明订阅落库了；这条证明「落库」确实改变了配额判定——
 * 否则等于订阅写了但没人看，与本缺陷的原始形态一模一样
 */
func TestDefaultPlanFeedsQuotaForUser(t *testing.T) {
	setupHandlerTestDB(t)
	seedDefaultPlan(t, "p-quota", "限量版", 1)
	uid := newTestUser(t, "limited", 1)

	before := quotaForUser(uid, 1).MaxDomains
	if before != -1 {
		t.Fatalf("授予前 max_domains 应为不限(-1)，实际 %d，用例前提不成立", before)
	}

	grantDefaultPlanIfAny(uid)

	after := quotaForUser(uid, 1).MaxDomains
	if after != 5 {
		t.Errorf("默认套餐的 max_domains=5 应生效，实际 %d", after)
	}
}
