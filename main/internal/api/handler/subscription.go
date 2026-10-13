package handler

import (
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 订阅有效性的单一判据
 *
 * 「在用」这件事此前有四处各写一遍，口径互不一致，直接导致过期订阅既删不掉套餐
 * 也撤不掉订阅：
 *
 *   GetPlans        subscriber_count 只按 status=1 计数，不看 expire_time
 *   DeletePlan      同上，于是过期订阅把套餐钉死
 *   planSourceForUser  认为过期无效，返回"system"，前端因此显示「未订阅」
 *   前端撤销按钮      禁用条件是 plan_name 非空，而它正是被上一条判空的
 *
 * 管理员给用户授了带 expire_at 的套餐，到期后：套餐列表显示「仍有 1 个订阅在用」
 * 删除被禁，用户页显示「未订阅」撤销按钮被禁——那个套餐永久无法删除也无法撤销
 *
 * 这里收敛成一个判据：**过期即无效**。所以计数、归属、来源三处都读同一个函数，
 * 前端则改为「只要订阅记录还在就允许撤销」——撤销一条已过期的记录是合法的清理动作，
 * 用 plan_name 是否非空来禁用按钮是把展示口径误用成了操作条件
 */

/* subscriptionEffective 订阅是否仍在生效期内 */
func subscriptionEffective(sub *models.UserSubscription, now time.Time) bool {
	if sub == nil || sub.Status != 1 {
		return false
	}
	// ExpireTime 为空表示永久有效
	return sub.ExpireTime == nil || sub.ExpireTime.After(now)
}

/*
 * activeSubscriptionCount 统计套餐下仍在生效的订阅数
 *
 * 与 planSourceForUser 的套餐分支同一口径：过期的订阅不再占用套餐，
 * 管理员无需先撤销它才能删除套餐
 */
func activeSubscriptionCount(db *gorm.DB, planID string) int64 {
	var subs []models.UserSubscription
	if err := db.Where("plan_id = ? AND status = 1", planID).Find(&subs).Error; err != nil {
		return 0
	}
	now := nowFunc()
	var n int64
	for i := range subs {
		if subscriptionEffective(&subs[i], now) {
			n++
		}
	}
	return n
}

/*
 * hasSubscriptionRecord 该用户是否存在订阅记录（含已过期）
 *
 * 供前端的撤销按钮判断可否操作：撤销一条已过期的记录是合法清理，
 * 用 plan_name 是否非空来禁用按钮会把过期订阅困在无人可撤的状态里
 */
func hasSubscriptionRecord(c *gin.Context, userID uint) bool {
	var n int64
	database.WithContext(c).Model(&models.UserSubscription{}).
		Where("user_id = ? AND status = 1", userID).Count(&n)
	return n > 0
}
