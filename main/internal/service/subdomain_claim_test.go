package service

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 子域名前缀的占用与释放
 *
 * 规则：一个域名下的同一个子域名前缀同一时刻只归一个用户。
 * 建记录时若该前缀尚未被占就归当前用户并占一条配额；已被他人占则拒绝，
 * 域名所有者与管理员不受限。
 *
 * 判定只看前缀，不看记录类型与值——同前缀下的多值 A、不同线路都属于同一个占用者。
 * 建记录之前该前缀下已有记录（含云端直接创建的）时，同样视为已占。
 */

/* claimDomain 造域名及其所有者，返回域名实体 */
func claimDomain(t *testing.T, ownerName, domainName string) models.Domain {
	t.Helper()
	owner := models.User{Username: ownerName, Email: ownerName + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建所有者失败: %v", err)
	}
	account := models.Account{UserID: owner.ID, Type: "cloudflare", Name: ownerName + "-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: domainName, ThirdID: "zone-" + domainName}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	return domain
}

/* claimUser 造一个普通用户 */
func claimUser(t *testing.T, name string, level int) models.User {
	t.Helper()
	u := models.User{Username: name, Email: name + "@example.com", Level: level, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	return u
}

/* TestClaimGrantedWhenNoExistingRecord 该前缀下无任何记录时应可占用 */
func TestClaimGrantedWhenNoExistingRecord(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-a", "claim-a.example.com")
	user := claimUser(t, "claimer-a", 1)

	if got := CheckSubdomainClaim(&domain, "xxxx1", user.ID, 1); got != ClaimGranted {
		t.Errorf("无既有记录时应放行，实际 %v", got)
	}
}

/* TestClaimTakenByOtherAfterRegistered 登记后他人不得再占 */
func TestClaimTakenByOtherAfterRegistered(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-b", "claim-b.example.com")
	first := claimUser(t, "claimer-b1", 1)
	second := claimUser(t, "claimer-b2", 1)

	RegisterSubdomainClaim(&domain, "xxxx1", first.ID)

	if got := CheckSubdomainClaim(&domain, "xxxx1", second.ID, 1); got != ClaimTakenByOther {
		t.Errorf("已被他人占用时应拒绝，实际 %v", got)
	}
	// 占用者自己再次使用同一前缀应放行（多值 A 的追加）
	if got := CheckSubdomainClaim(&domain, "xxxx1", first.ID, 1); got != ClaimGranted {
		t.Errorf("占用者自己应可继续使用该前缀，实际 %v", got)
	}
}

/* TestClaimDeniedByExistingCloudRecord 建占用之前该前缀下已有记录时不得认领 */
func TestClaimDeniedByExistingCloudRecord(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-c", "claim-c.example.com")
	user := claimUser(t, "claimer-c", 1)

	// 模拟云端直接创建、尚未被任何面板用户认领的记录（uid=0）
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: "cloud-1", Name: "xxxx1", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("预置云端记录失败: %v", err)
	}

	if got := CheckSubdomainClaim(&domain, "xxxx1", user.ID, 1); got != ClaimDeniedByExisting {
		t.Errorf("该前缀下已有记录时应拒绝占用，实际 %v", got)
	}
}

/* TestClaimOwnerAndAdminUnrestricted 域名所有者与管理员不受占用限制 */
func TestClaimOwnerAndAdminUnrestricted(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-d", "claim-d.example.com")
	taker := claimUser(t, "claimer-d", 1)
	admin := claimUser(t, "admin-d", 2)

	RegisterSubdomainClaim(&domain, "xxxx1", taker.ID)

	// 管理员
	if got := CheckSubdomainClaim(&domain, "xxxx1", admin.ID, 2); got != ClaimGranted {
		t.Errorf("管理员不应受占用限制，实际 %v", got)
	}

	// 域名所有者
	owner := models.User{Username: "owner-d-owner", Email: "owner-d-owner@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建所有者用户失败: %v", err)
	}
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		t.Fatalf("读账户失败: %v", err)
	}
	if got := CheckSubdomainClaim(&domain, "xxxx1", account.UserID, 1); got != ClaimGranted {
		t.Errorf("域名所有者不应受占用限制，实际 %v", got)
	}
}

/* TestClaimIsCaseInsensitiveAndTrims 前缀判定不区分大小写与空白 */
func TestClaimIsCaseInsensitiveAndTrims(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-e", "claim-e.example.com")
	first := claimUser(t, "claimer-e1", 1)
	second := claimUser(t, "claimer-e2", 1)

	RegisterSubdomainClaim(&domain, "  XXXX1  ", first.ID)

	if got := CheckSubdomainClaim(&domain, "xxxx1", second.ID, 1); got != ClaimTakenByOther {
		t.Errorf("大小写与空白归一后应视为同一前缀，实际 %v", got)
	}
}

/* TestReleaseOnlyWhenPrefixEmpty 仅当该前缀下已无记录时才释放 */
func TestReleaseOnlyWhenPrefixEmpty(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-f", "claim-f.example.com")
	user := claimUser(t, "claimer-f", 1)

	RegisterSubdomainClaim(&domain, "xxxx1", user.ID)
	// 前缀下还有一条记录：不应释放
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: "r-keep", Name: "xxxx1", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("预置记录失败: %v", err)
	}
	ReleaseSubdomainClaimIfEmpty(domain.ID, "xxxx1")

	if n := CountSubdomainClaims(domain.ID, user.ID); n != 1 {
		t.Errorf("该前缀下仍有记录，占用不应被释放（应为 1，实际 %d）", n)
	}

	// 记录清空后应释放
	if err := database.DB.Where("did = ?", domain.ID).Delete(&models.DNSRecord{}).Error; err != nil {
		t.Fatalf("清空记录失败: %v", err)
	}
	ReleaseSubdomainClaimIfEmpty(domain.ID, "xxxx1")

	if n := CountSubdomainClaims(domain.ID, user.ID); n != 0 {
		t.Errorf("该前缀下已无记录，占用应被释放（应为 0，实际 %d）", n)
	}
}

/* TestClaimCountsPerPrefix 同一用户占两个前缀计两个配额 */
func TestClaimCountsPerPrefix(t *testing.T) {
	setupOptimizeTestDB(t)
	domain := claimDomain(t, "owner-g", "claim-g.example.com")
	user := claimUser(t, "claimer-g", 1)

	RegisterSubdomainClaim(&domain, "xxxx1", user.ID)
	RegisterSubdomainClaim(&domain, "xxxx2", user.ID)

	if n := CountSubdomainClaims(domain.ID, user.ID); n != 2 {
		t.Errorf("两个前缀应计两条占用，实际 %d", n)
	}

	// 同一前缀重复登记不增加计数
	RegisterSubdomainClaim(&domain, "xxxx1", user.ID)
	if n := CountSubdomainClaims(domain.ID, user.ID); n != 2 {
		t.Errorf("重复登记同一前缀不应增加占用，实际 %d", n)
	}
}

/* TestClaimsAreScopedByDomain 不同域名下同名前缀互不影响 */
func TestClaimsAreScopedByDomain(t *testing.T) {
	setupOptimizeTestDB(t)
	d1 := claimDomain(t, "owner-h1", "claim-h1.example.com")
	d2 := claimDomain(t, "owner-h2", "claim-h2.example.com")
	other := claimUser(t, "claimer-h", 1)

	RegisterSubdomainClaim(&d1, "xxxx1", other.ID)

	// 另一个域名下的同名前缀应仍可占用
	if got := CheckSubdomainClaim(&d2, "xxxx1", other.ID, 1); got != ClaimGranted {
		t.Errorf("不同域名下的同名前缀互不影响，实际 %v", got)
	}
}
