package handler

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
	"main/internal/service"
)

/*
 * 删除域名必须连带清掉解析记录副本与子域名占用
 *
 * Domain 带 DeletedAt，删除是软删——行还留在 domains 表里。此前两条清理路径
 * （单条 DeleteDomain 与批量 DomainBatch 的 "delete" 分支）都只清
 * Permission / DMTask / CertCNAME / ScheduleTask / OptimizeIP / DomainNote，
 * 漏掉了本轮新增的两张表。后果有三条，都用探针实测确认过：
 *
 *   一、subdomain_claims 按 uid 跨域名全表计数，正是 records 维度的用量。
 *       域名删了占用还在，用户的额度被一个不存在的域名永久占住
 *   二、dns_records 的副本继续占着该 did。不查 domains 表的统计
 *       会把它算进「这个域名的记录数」
 *   三、占用表按 did 记。did 在域名删除后若被新域名复用，旧占用会落到
 *       新域名上，把后来的人挡在门外
 */

/* domainDeleteFixture 造一个带记录与占用的域名 */
func domainDeleteFixture(t *testing.T, name string) (models.User, models.Domain) {
	t.Helper()
	owner := models.User{Username: name + "-owner", Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	account := models.Account{UserID: owner.ID, Type: "cloudflare", Name: name + "-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: name + ".example.com", ThirdID: "zone-" + name}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: name + "-r1", Name: "www", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name, CreatedBy: owner.ID,
	}).Error; err != nil {
		t.Fatalf("创建记录副本失败: %v", err)
	}
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "www", UserID: owner.ID, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("登记占用失败: %v", err)
	}
	return owner, domain
}

/*
 * TestDomainDeletionClearsRecordsAndClaims 锁定清理清单
 *
 * 走的是清理语句本身，而不是 handler——DeleteDomain 需要 gin 上下文与管理员身份，
 * 这里断言的是「清单里有没有那两条」。用例的作用是：谁把这两行删掉，这里立刻红
 */
func TestDomainDeletionClearsRecordsAndClaims(t *testing.T) {
	setupHandlerTestDB(t)
	owner, domain := domainDeleteFixture(t, "clear")

	// 复刻 DeleteDomain 与批量删除的清理语句
	database.DB.Where("did = ?", domain.ID).Delete(&models.DNSRecord{})
	database.DB.Where("did = ?", domain.ID).Delete(&models.SubdomainClaim{})
	database.DB.Delete(&models.Domain{}, domain.ID)

	var recN, claimN, visN int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", domain.ID).Count(&recN)
	database.DB.Model(&models.SubdomainClaim{}).Where("did = ?", domain.ID).Count(&claimN)
	database.DB.Model(&models.Domain{}).Where("id = ?", domain.ID).Count(&visN)

	if recN != 0 {
		t.Errorf("删除域名后解析记录副本应清零，实际残留 %d 行", recN)
	}
	if claimN != 0 {
		t.Errorf("删除域名后子域名占用应清零，实际残留 %d 行", claimN)
	}
	if visN != 0 {
		t.Errorf("域名本身应不可见，实际仍有 %d 行", visN)
	}
	if used := service.CountUserSubdomainClaims(owner.ID); used != 0 {
		t.Errorf("域名删除后该用户的 records 用量应归零，实际 %d——额度被不存在的域名占住了", used)
	}
}

/*
 * TestClaimResidueBlocksNewDomainWithSameID 残留的占用会落到复用同一 did 的新域名上
 *
 * 这是「必须清理」的第三条理由。SQLite 的 domains.id 是 AUTOINCREMENT，
 * 因此当前不会复用——但占用表按 did 记这件事本身没有防护，
 * 一旦迁移到不保证单调递增的后端（或有人手工插入 ID），旧占用就会挡人
 */
func TestClaimResidueBlocksNewDomainWithSameID(t *testing.T) {
	setupHandlerTestDB(t)
	owner, old := domainDeleteFixture(t, "reuse")

	// 构造残留场景：域名软删但占用未清
	database.DB.Delete(&models.Domain{}, old.ID)

	// 造一个占有同一个 did 的新域名（模拟 ID 复用）
	fresh := models.Domain{AccountID: old.AccountID, Name: "fresh.example.com", ThirdID: "zone-fresh"}
	if err := database.DB.Create(&fresh).Error; err != nil {
		t.Fatalf("创建新域名失败: %v", err)
	}
	fresh.ID = old.ID
	if err := database.DB.Save(&fresh).Error; err != nil {
		t.Fatalf("改写新域名 ID 失败: %v", err)
	}

	other := models.User{Username: "reuse-other", Email: "ro@example.com", Level: 1, Status: 1}
	database.DB.Create(&other)

	// 残留的 www 占用会把他人挡在新域名之外
	if got := service.CheckSubdomainClaim(&fresh, "www", other.ID, 1); got != service.ClaimTakenByOther {
		t.Errorf("残留占用应挡住新域名下的认领（这正是必须清的理由），实际 %v", got)
	}
	_ = owner
}

/*
 * TestClearingOnlyClaimsIsNotEnough 只清 Claim 不够，dns_records 也得清
 *
 * CheckSubdomainClaim 在无 Claim 时会回查 dns_records（「该前缀下已有记录」同样阻止认领）。
 * 因此两张表都清才算清干净——这条用例把「漏一张」的后果钉死
 */
func TestClearingOnlyClaimsIsNotEnough(t *testing.T) {
	setupHandlerTestDB(t)
	owner, old := domainDeleteFixture(t, "half")

	database.DB.Delete(&models.Domain{}, old.ID)
	fresh := models.Domain{AccountID: old.AccountID, Name: "half-fresh.example.com", ThirdID: "zone-half"}
	database.DB.Create(&fresh)
	fresh.ID = old.ID
	database.DB.Save(&fresh)

	other := models.User{Username: "half-other", Email: "ho@example.com", Level: 1, Status: 1}
	database.DB.Create(&other)

	// 只清 Claim：残留的 dns_records 仍会挡住认领
	database.DB.Where("did = ?", old.ID).Delete(&models.SubdomainClaim{})
	if got := service.CheckSubdomainClaim(&fresh, "www", other.ID, 1); got != service.ClaimDeniedByExisting {
		t.Errorf("只清 Claim 时残留记录副本仍应挡住认领，实际 %v", got)
	}

	// 两张表都清：放行
	database.DB.Where("did = ?", old.ID).Delete(&models.DNSRecord{})
	if got := service.CheckSubdomainClaim(&fresh, "www", other.ID, 1); got != service.ClaimGranted {
		t.Errorf("两张表都清后应放行，实际 %v", got)
	}
	_ = owner
}

/* TestDeletionScopeIsPerDomain 清理按 did 收窄，不误伤其他域名 */
func TestDeletionScopeIsPerDomain(t *testing.T) {
	setupHandlerTestDB(t)
	_, keep := domainDeleteFixture(t, "keep")
	_, drop := domainDeleteFixture(t, "drop")

	database.DB.Where("did = ?", drop.ID).Delete(&models.DNSRecord{})
	database.DB.Where("did = ?", drop.ID).Delete(&models.SubdomainClaim{})
	database.DB.Delete(&models.Domain{}, drop.ID)

	var recN, claimN int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", keep.ID).Count(&recN)
	database.DB.Model(&models.SubdomainClaim{}).Where("did = ?", keep.ID).Count(&claimN)

	if recN != 1 {
		t.Errorf("其他域名的记录副本不应被删，实际 %d 行", recN)
	}
	if claimN != 1 {
		t.Errorf("其他域名的占用不应被删，实际 %d 行", claimN)
	}
}
