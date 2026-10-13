package handler

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
)

/*
 * 首页统计的按用户收窄
 *
 * 管理员看全站，普通用户只看自己名下的资源。此前一律返回全站计数，
 * 普通用户看到的「域名总数」是别人的域名加上去的，既泄露实例规模又与实际不符。
 *
 * 口径与 quotaUsed 一致：域名算自有账户下的加上被授权的，其余算自有账户下的。
 * 有意不复用 quotaUsed——那个函数是配额校验的伴生品，返回的是「用量/上限」
 * 语境下的数字，而首页要的是纯计数，且不应受 max_* 为 -1（不限）的影响
 */

/* ownerScopeDashboardStats 普通用户的首页统计 */
func ownerScopeDashboardStats(c *gin.Context) {
	userID := middleware.AuthUserID(c)

	var domains, tasks, certs, deploys int64

	// 域名：自有账户下的 + 被单独授权的（与 quotaUsed 的 domains 分支同口径）
	database.WithContext(c).Table("domains").
		Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
		Where("domains.deleted_at IS NULL AND (accounts.uid = ? OR domains.name IN (SELECT domain FROM permissions WHERE uid = ?))", userID, userID).
		Count(&domains)

	// 容灾监控任务
	database.WithContext(c).Table("dm_tasks").
		Joins("LEFT JOIN domains ON dm_tasks.did = domains.id").
		Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
		Where("accounts.uid = ?", userID).Count(&tasks)

	// 证书订单
	database.WithContext(c).Table("cert_orders").
		Joins("LEFT JOIN cert_accounts ON cert_orders.aid = cert_accounts.id").
		Where("cert_accounts.uid = ?", userID).Count(&certs)

	// 证书部署任务
	database.WithContext(c).Table("cert_deploys").Where("uid = ?", userID).Count(&deploys)

	middleware.SuccessResponse(c, gin.H{
		"domains":             domains,
		"tasks":               tasks,
		"certs":               certs,
		"deploys":             deploys,
		"dmonitor_active":     0,
		"dmonitor_status_0":   0,
		"dmonitor_status_1":   0,
		"dmonitor_state":      0,
		"certorder_status_3":  0,
		"certorder_status_5":  0,
		"certorder_status_6":  0,
		"certorder_status_7":  0,
		"certdeploy_status_0": 0,
		"certdeploy_status_1": 0,
		"certdeploy_status_2": 0,
	})
}
