package service

import (
	"strings"

	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"

	"gorm.io/gorm"
)

/*
 * 子域名前缀的占用与释放
 *
 * 规则：一个域名下的同一个子域名前缀同一时刻只归一个用户。
 *   建记录时若该前缀尚未被占 → 归当前用户，占一条配额
 *   该前缀已被自己占         → 放行，不再重复计
 *   该前缀已被他人占         → 拒绝，除域名所有者与管理员
 *
 * 判定只看前缀，不看记录类型与值：同前缀下的多值 A、不同线路的记录
 * 都属于同一个占用者。创建之前该前缀下已有云端记录（uid=0）时，
 * 同样视为已被占用——「以前就有的记录」不能被后来的用户认领
 */

/* NormalizeRecordPrefix 归一记录名到子域名前缀 */
func NormalizeRecordPrefix(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}

/*
 * ClaimOutcome 一次占用判定的结果
 *
 * ByOwner 与 ByAdmin 分开返回，是为了让调用方能给出不同的提示语：
 * 「该子域名已被他人占用」与「你对这个域名没有权限」是两回事
 */
type ClaimOutcome int

const (
	ClaimGranted          ClaimOutcome = iota // 可以占用（含自己已占）
	ClaimTakenByOther                         // 已被他人占用
	ClaimDeniedByExisting                     // 建记录前该前缀下已有非本人记录
)

/* claimable 判断当前用户是否不受占用限制：域名所有者与管理员 */
func claimable(domain *models.Domain, userID uint, level int) bool {
	if level >= 2 {
		return true
	}
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return false
	}
	return account.UserID == userID
}

/*
 * CheckSubdomainClaim 判定当前用户能否占用该子域名前缀
 *
 * 调用点在云端写入**之前**：占用是排他性的，先写后判会让云端出现
 * 一条马上要被删掉的记录，且并发下两个用户可能同时通过判定
 */
func CheckSubdomainClaim(domain *models.Domain, prefix string, userID uint, level int) ClaimOutcome {
	if database.DB == nil || domain == nil {
		return ClaimGranted
	}
	prefix = NormalizeRecordPrefix(prefix)
	if prefix == "" {
		// 记录名为空是「@」（根域），也参与占用：根域同时只能有一个使用者
		prefix = "@"
	}
	if claimable(domain, userID, level) {
		return ClaimGranted
	}

	var claim models.SubdomainClaim
	err := database.DB.Where("did = ? AND prefix = ?", domain.ID, prefix).First(&claim).Error
	if err == nil {
		if claim.UserID == userID {
			return ClaimGranted
		}
		return ClaimTakenByOther
	}
	if err != gorm.ErrRecordNotFound {
		// 查库失败时不放行：占用判定失败会退化成「谁都能建」，
		// 与排他性的目标正好相反
		logger.Warn("[SubdomainClaim] 查询占用失败: domain=%d prefix=%s err=%v", domain.ID, prefix, err)
		return ClaimDeniedByExisting
	}

	// 无占用记录，但该前缀下可能已有云端直接创建的历史记录，同样视为已占
	var n int64
	database.DB.Model(&models.DNSRecord{}).
		Where("did = ? AND LOWER(name) = ?", domain.ID, prefix).Count(&n)
	if n > 0 {
		return ClaimDeniedByExisting
	}
	return ClaimGranted
}

/*
 * RegisterSubdomainClaim 云端写入成功后登记占用
 *
 * 已存在同前缀的记录时不覆盖占用者：多值 A 由同一人追加第二条时，
 * 占用关系不该被重新分配
 */
func RegisterSubdomainClaim(domain *models.Domain, prefix string, userID uint) {
	if database.DB == nil || domain == nil || userID == 0 {
		return
	}
	prefix = NormalizeRecordPrefix(prefix)
	if prefix == "" {
		prefix = "@"
	}
	var claim models.SubdomainClaim
	err := database.DB.Where("did = ? AND prefix = ?", domain.ID, prefix).First(&claim).Error
	if err == nil {
		return // 已被认领，占用者不变
	}
	if err != gorm.ErrRecordNotFound {
		logger.Warn("[SubdomainClaim] 登记前查询失败: domain=%d prefix=%s err=%v", domain.ID, prefix, err)
		return
	}
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: prefix, UserID: userID, Domain: domain.Name,
	}).Error; err != nil {
		// 唯一索引会把并发下的重复登记挡掉，此时另一条已经写入，不影响正确性
		logger.Warn("[SubdomainClaim] 登记占用失败: domain=%d prefix=%s err=%v", domain.ID, prefix, err)
	}
}

/*
 * ReleaseSubdomainClaimIfEmpty 该前缀下已无记录时释放占用
 *
 * 只在删除路径调用。仍有其他记录（多值、不同线路）时不释放，
 * 否则同一个人删掉两条 A 中的一条就会把自己的占用一起放掉
 */
func ReleaseSubdomainClaimIfEmpty(domainID uint, prefix string) {
	if database.DB == nil || domainID == 0 {
		return
	}
	prefix = NormalizeRecordPrefix(prefix)
	if prefix == "" {
		prefix = "@"
	}
	var n int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ? AND LOWER(name) = ?", domainID, prefix).Count(&n)
	if n > 0 {
		return
	}
	if err := database.DB.Where("did = ? AND prefix = ?", domainID, prefix).
		Delete(&models.SubdomainClaim{}).Error; err != nil {
		logger.Warn("[SubdomainClaim] 释放占用失败: domain=%d prefix=%s err=%v", domainID, prefix, err)
	}
}

/* CountSubdomainClaims 该用户在该域名下占用的前缀数 */
func CountSubdomainClaims(domainID, userID uint) int {
	if database.DB == nil {
		return 0
	}
	var n int64
	database.DB.Model(&models.SubdomainClaim{}).
		Where("did = ? AND uid = ?", domainID, userID).Count(&n)
	return int(n)
}

/*
 * CountUserSubdomainClaims 该用户占用的子域名前缀总数（跨域名）
 *
 * 这就是 records 维度的用量：一个前缀占一条额度，同前缀下的多值 A、
 * 不同线路共享同一条。按 Claim 统计而非按记录条数，是因为「占一个配额」
 * 指的是占用一个子域名，不是写一条记录
 *
 * 历史记录（云端直接创建、或本功能上线前创建）没有 Claim，因此不计入任何人的
 * 用量——它们不是通过面板配额建的。但它们仍会阻止他人认领，那条判定查的是
 * dns_records，不依赖本表
 */
func CountUserSubdomainClaims(userID uint) int {
	if database.DB == nil || userID == 0 {
		return 0
	}
	var n int64
	database.DB.Model(&models.SubdomainClaim{}).Where("uid = ?", userID).Count(&n)
	return int(n)
}
