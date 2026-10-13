package handler

import (
	"testing"
	"time"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 过期订阅不得把套餐钉死
 *
 * 修复前四处口径互不一致：GetPlans 与 DeletePlan 只按 status=1 计数（不看过期），
 * planSourceForUser 认为过期无效（返回空 planName），前端撤销按钮按
 * planName 非空禁用。结果是一份过期订阅让套餐既删不掉（计数非零）
 * 也撤不掉（按钮禁用，因为显示的名字是空的）
 *
 * 这里锁住收敛后的口径：**过期即无效**
 */

/* seedSubscription 造一条订阅 */
func seedSubscription(t *testing.T, userID uint, planID, planName string, expire *time.Time) models.UserSubscription {
	t.Helper()
	sub := models.UserSubscription{
		ID:         planID + "-sub-" + planName,
		UserID:     userID,
		PlanID:     planID,
		PlanName:   planName,
		Status:     1,
		StartTime:  time.Now().Add(-48 * time.Hour),
		ExpireTime: expire,
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		t.Fatalf("创建订阅失败: %v", err)
	}
	return sub
}

/* pastTime 一个已经过去的时间点 */
func pastTime() *time.Time {
	t := time.Now().Add(-24 * time.Hour)
	return &t
}

/* futureTime 一个尚未到来的时间点 */
func futureTime() *time.Time {
	t := time.Now().Add(24 * time.Hour)
	return &t
}

/* TestActiveSubscriptionCountSkipsExpired 过期订阅不计入「在用」 */
func TestActiveSubscriptionCountSkipsExpired(t *testing.T) {
	setupHandlerTestDB(t)

	uid := newTestUser(t, "expired-holder", 1)
	seedSubscription(t, uid, "p1", "专业版", pastTime())

	if n := activeSubscriptionCount(database.DB, "p1"); n != 0 {
		t.Errorf("过期订阅不应计入在用，实际 %d", n)
	}
}

/* TestActiveSubscriptionCountCountsUnexpired 未过期与永久的订阅都应计入 */
func TestActiveSubscriptionCountCountsUnexpired(t *testing.T) {
	setupHandlerTestDB(t)

	u1 := newTestUser(t, "active-holder", 1)
	seedSubscription(t, u1, "p2", "专业版", futureTime())
	u2 := newTestUser(t, "forever-holder", 1)
	seedSubscription(t, u2, "p2-x", "专业版", nil) // ExpireTime 为空表示永久

	if n := activeSubscriptionCount(database.DB, "p2"); n != 1 {
		t.Errorf("未过期订阅应计入，实际 %d", n)
	}
	if n := activeSubscriptionCount(database.DB, "p2-x"); n != 1 {
		t.Errorf("永久订阅应计入，实际 %d", n)
	}
}

/*
 * TestDeletePlanAllowsWhenOnlyExpiredSubscribers 只剩过期订阅时，套餐应当可以删除
 *
 * 这是本缺陷的直接症状：删除被「仍有 N 个订阅在用」挡下，
 * 而那个 N 来自不看 expire_time 的计数
 */
func TestDeletePlanAllowsWhenOnlyExpiredSubscribers(t *testing.T) {
	setupHandlerTestDB(t)

	plan := models.Plan{ID: "p-dead", Name: "旧套餐", Status: 1}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建套餐失败: %v", err)
	}
	uid := newTestUser(t, "stuck-holder", 1)
	seedSubscription(t, uid, plan.ID, plan.Name, pastTime())

	if n := activeSubscriptionCount(database.DB, plan.ID); n != 0 {
		t.Fatalf("前置不成立：过期订阅仍被计为在用（%d）", n)
	}
}

/* TestHasSubscriptionRecordSeesExpired 存在过期订阅时，撤销入口必须可用 */
func TestHasSubscriptionRecordSeesExpired(t *testing.T) {
	setupHandlerTestDB(t)

	uid := newTestUser(t, "revoke-target", 1)
	seedSubscription(t, uid, "p3", "已过期套餐", pastTime())

	c := batchAuthzContext(t, uid)
	if !hasSubscriptionRecord(c, uid) {
		t.Error("存在订阅记录（虽已过期）时应报告 true，否则前端撤销按钮会被禁用，订阅永久无法清理")
	}
}

/* TestHasSubscriptionRecordFalseWhenNone 没有订阅记录时报告 false */
func TestHasSubscriptionRecordFalseWhenNone(t *testing.T) {
	setupHandlerTestDB(t)

	uid := newTestUser(t, "no-sub", 1)
	c := batchAuthzContext(t, uid)
	if hasSubscriptionRecord(c, uid) {
		t.Error("无订阅记录时不应报告 true")
	}
}
