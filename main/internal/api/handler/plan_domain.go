package handler

import (
	"strings"
	"time"

	"gorm.io/gorm"

	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
)

/*
 * 套餐绑定域名的权限下发
 *
 * 套餐可以绑定若干个域名，授予套餐时把这些域名的访问权限一并下发给用户，
 * 撤销时只回收套餐下发的那部分。
 *
 * 为什么不复用 AddUserPermission：那条路径是管理员手工授权，写入的行
 * Source 为空。若套餐也走它，撤销时就分不清「这条是套餐给的还是管理员单独给的」，
 * 只能整片删掉，会把管理员的手工授权一并削掉。
 *
 * 权限行的形态与手工授权一致（SubDomain 为空表示整个域名），
 * 差别只在 Source 标记，因此下游的权限判定逻辑无需改动
 */

/*
 * normalizeDomainList 归一化域名清单为逗号分隔的小写串
 *
 * 授予时要按名单匹配域名，大小写与空白不一致会让匹配失败；
 * 在写入侧统一，读取侧就不必反复 Trim/Lower
 */
func normalizeDomainList(raw string) string {
	parts := make([]string, 0, 4)
	seen := map[string]bool{}
	for _, d := range strings.Split(raw, ",") {
		v := strings.ToLower(strings.TrimSpace(d))
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		parts = append(parts, v)
	}
	return strings.Join(parts, ",")
}

/* planIncludedDomainList 解析套餐绑定的域名清单 */
func planIncludedDomainList(plan *models.Plan) []string {
	if plan == nil {
		return nil
	}
	raw := strings.TrimSpace(plan.IncludedDomains)
	if raw == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, d := range strings.Split(raw, ",") {
		if v := strings.ToLower(strings.TrimSpace(d)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

/*
 * grantPlanDomains 在授予套餐的事务内下发域名权限
 *
 * 已存在同域名同来源的行则跳过，避免同一套餐反复授予时堆出重复权限行。
 * 管理员手工授予过同一域名时也不覆盖——两条来源不同的行并存，
 * 撤销套餐只会删掉 Source='plan' 的那条
 */
func grantPlanDomains(tx *gorm.DB, userID uint, plan *models.Plan, expire *time.Time) error {
	for _, name := range planIncludedDomainList(plan) {
		var existing int64
		if err := tx.Model(&models.Permission{}).
			Where("uid = ? AND domain = ? AND source = ?", userID, name, "plan").
			Count(&existing).Error; err != nil {
			return err
		}
		if existing > 0 {
			continue
		}

		// 记录域名的本地 ID（若该域名在面板中存在），便于权限页展示
		var domainID uint
		var domain models.Domain
		if err := tx.Where("name = ? AND deleted_at IS NULL", name).First(&domain).Error; err == nil {
			domainID = domain.ID
		}

		perm := models.Permission{
			UserID:     userID,
			DomainID:   domainID,
			Domain:     name,
			SubDomain:  "",
			ReadOnly:   false,
			ExpireTime: expire,
			Source:     "plan",
			CreatedAt:  time.Now(),
		}
		if err := tx.Create(&perm).Error; err != nil {
			return err
		}
	}
	return nil
}

/*
 * revokePlanDomains 撤销套餐时回收套餐下发的权限
 *
 * 只删 Source='plan' 的行：管理员手工授予的同域名权限必须保留，
 * 否则「撤销套餐」会变成一次越权删除
 */
func revokePlanDomains(tx *gorm.DB, userID uint) error {
	return tx.Where("uid = ? AND source = ?", userID, "plan").
		Delete(&models.Permission{}).Error
}

/* refreshPlanDomainPermissions 套餐变更后重新下发（先回收再下发） */
func refreshPlanDomainPermissions(tx *gorm.DB, userID uint, plan *models.Plan, expire *time.Time) error {
	if err := revokePlanDomains(tx, userID); err != nil {
		return err
	}
	return grantPlanDomains(tx, userID, plan, expire)
}

/* expireStalePlanPermissions 清理已过期订阅留下的套餐权限 */
func expireStalePlanPermissions(userID uint) {
	if database.DB == nil {
		return
	}
	if err := database.DB.Where("uid = ? AND source = ?", userID, "plan").
		Delete(&models.Permission{}).Error; err != nil {
		logger.Warn("[Plan] 回收失效套餐权限失败: uid=%d err=%v", userID, err)
	}
}
