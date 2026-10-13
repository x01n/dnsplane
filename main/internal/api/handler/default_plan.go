package handler

import (
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"

	"github.com/google/uuid"
)

/*
 * 默认套餐的消费
 *
 * 「默认套餐」此前只是个标记：SetDefaultPlan 会正确地把 is_default 置位，
 * 但注册链路与建号链路都不读它，quotaForUser 的三层取值
 * （个体覆盖 → 套餐 → 系统默认）里也没有它这一层。
 * 于是管理员设了默认套餐、界面文案也写着「新用户注册时自动参考」，
 * 实际新用户拿到的仍是 sysconfig 的 quota_default
 *
 * 这里把标记接成真实行为：建号时若有启用中的默认套餐，就为这个用户落一条订阅。
 * 与 SetUserSubscription 保持同一形态（status=1、StartTime 为建号时刻、
 * ExpireTime 为空表示永久），区别只是 PlanID 由标记决定而非管理员指定
 */

/*
 * grantDefaultPlanIfAny 给新建的用户授予默认套餐
 *
 * 失败只记日志、不改调用方的返回值：套餐是附加的约束，建号本身已经成功，
 * 因套餐缺失而让注册整体失败，比新用户暂时不受配额约束更糟。
 * 管理员可在用户管理页补授，或把套餐重新设为默认后再建号
 */
func grantDefaultPlanIfAny(userID uint) {
	var plan models.Plan
	err := database.DB.
		Where("is_default = ? AND status = ?", true, 1).
		First(&plan).Error
	if err != nil {
		// 未配置默认套餐（含查询无结果）是正常状态，不是错误
		return
	}
	if plan.Status != 1 {
		// 默认套餐被停用时不授予：停用语义是「不再可用」，与它是否为默认无关
		return
	}

	sub := models.UserSubscription{
		ID:        uuid.New().String(),
		UserID:    userID,
		PlanID:    plan.ID,
		PlanName:  plan.Name,
		Status:    1,
		AutoRenew: false,
		StartTime: nowFunc(),
		// ExpireTime 为空表示永久
	}
	if err := database.DB.Create(&sub).Error; err != nil {
		// 不向调用方传播：建号已成功，套餐缺失由管理员后续处理
		logger.Warn("授予默认套餐失败: user_id=%d plan_id=%s err=%v", userID, plan.ID, err)
	}
}
