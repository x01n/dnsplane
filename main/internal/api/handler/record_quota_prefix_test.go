package handler

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
	"main/internal/service"
)

/*
 * records 配额的计量单位是**子域名前缀**，不是记录条数
 *
 * 用户要求：「这个域名添加了一个 xxxx1 的子域名，那么就会占用一个配额」——
 * 「占一个配额」指的是占用一个子域名。同前缀下的多值 A、不同线路共享同一条额度。
 *
 * 按记录条数计会让多值记录被重复收额度，与占用语义不符；
 * 而且统计对象必须是 Claim 表——dns_records 是云端副本，被对账整表覆盖，
 * 本地派生的 uid 在其中没有权威性
 */

/* quotaPrefixUser 造一个带个体配额的用户 */
func quotaPrefixUser(t *testing.T, name string, quotas string) models.User {
	t.Helper()
	u := models.User{Username: name, Email: name + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&u).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	if err := database.DB.Create(&models.UserQuota{UserID: u.ID, Quotas: quotas}).Error; err != nil {
		t.Fatalf("设置个体配额失败: %v", err)
	}
	return u
}

/* quotaPrefixDomain 给用户造一个自有账户与域名 */
func quotaPrefixDomain(t *testing.T, uid uint, name string) models.Domain {
	t.Helper()
	account := models.Account{UserID: uid, Type: "cloudflare", Name: name + "-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: name, ThirdID: "zone-" + name}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	return domain
}

/* seedClaim 登记一条子域名占用 */
func seedClaim(t *testing.T, domain models.Domain, prefix string, uid uint) {
	t.Helper()
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: prefix, UserID: uid, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("登记占用失败: %v", err)
	}
}

/* TestRecordQuotaCountsPrefixesNotRows 同一前缀的多条记录只计一个配额 */
func TestRecordQuotaCountsPrefixesNotRows(t *testing.T) {
	setupHandlerTestDB(t)
	user := quotaPrefixUser(t, "prefix-count", `{"max_records":5}`)
	domain := quotaPrefixDomain(t, user.ID, "prefix.example.com")

	// 一个前缀下 3 条记录（多值 A），并登记一条占用
	for _, id := range []string{"r1", "r2", "r3"} {
		if err := database.DB.Create(&models.DNSRecord{
			DomainID: domain.ID, RecordID: id, Name: "www", Type: "A",
			Value: "192.0.2." + id[len(id)-1:], Status: "enable", Domain: domain.Name,
		}).Error; err != nil {
			t.Fatalf("预置记录失败: %v", err)
		}
	}
	seedClaim(t, domain, "www", user.ID)

	c := batchAuthzContext(t, user.ID)
	used, limit := QuotaUsageOf(c, "records")
	if used != 1 {
		t.Errorf("一个前缀下 3 条记录应只计 1 个配额，实际 %d", used)
	}
	if limit != 5 {
		t.Errorf("限额应为 5，实际 %d", limit)
	}
	if !QuotaAllows(c, "records") {
		t.Error("用量 1 < 限额 5，应放行")
	}
}

/* TestRecordQuotaCountsEachPrefix 两个前缀计两个配额 */
func TestRecordQuotaCountsEachPrefix(t *testing.T) {
	setupHandlerTestDB(t)
	user := quotaPrefixUser(t, "two-prefix", `{"max_records":5}`)
	domain := quotaPrefixDomain(t, user.ID, "two.example.com")

	seedClaim(t, domain, "www", user.ID)
	seedClaim(t, domain, "blog", user.ID)

	c := batchAuthzContext(t, user.ID)
	if used, _ := QuotaUsageOf(c, "records"); used != 2 {
		t.Errorf("两个前缀应计 2 个配额，实际 %d", used)
	}
}

/* TestRecordQuotaIgnoresClaimsOfOthers 他人的占用不计入自己的用量 */
func TestRecordQuotaIgnoresClaimsOfOthers(t *testing.T) {
	setupHandlerTestDB(t)
	me := quotaPrefixUser(t, "me-claims", `{"max_records":5}`)
	other := quotaPrefixUser(t, "other-claims", `{"max_records":5}`)
	domain := quotaPrefixDomain(t, other.ID, "shared.example.com")

	seedClaim(t, domain, "taken", other.ID)

	c := batchAuthzContext(t, me.ID)
	if used, _ := QuotaUsageOf(c, "records"); used != 0 {
		t.Errorf("他人占用的前缀不应计入我的用量，实际 %d", used)
	}
}

/* TestRecordQuotaIgnoresRowsWithoutClaim 无 Claim 的历史记录不计任何人的用量 */
func TestRecordQuotaIgnoresRowsWithoutClaim(t *testing.T) {
	setupHandlerTestDB(t)
	user := quotaPrefixUser(t, "legacy-rows", `{"max_records":5}`)
	domain := quotaPrefixDomain(t, user.ID, "legacy.example.com")

	// 本功能上线前创建的记录：dns_records 里有行，但没有 Claim
	for _, id := range []string{"old1", "old2"} {
		if err := database.DB.Create(&models.DNSRecord{
			DomainID: domain.ID, RecordID: id, Name: id, Type: "A",
			Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
		}).Error; err != nil {
			t.Fatalf("预置历史记录失败: %v", err)
		}
	}

	c := batchAuthzContext(t, user.ID)
	if used, _ := QuotaUsageOf(c, "records"); used != 0 {
		t.Errorf("无占用的历史记录不应计配额（它们不是通过面板配额建的），实际 %d", used)
	}
}

/*
 * TestLegacyRowsStillBlockClaim 历史记录虽不计配额，仍须阻止他人认领
 *
 * 这是用户明确要求的：「如果这个子域名在创建之前就有其他记录，也会阻止占用」。
 * 该判定查的是 dns_records，不依赖 Claim 表，所以两者互不影响
 */
func TestLegacyRowsStillBlockClaim(t *testing.T) {
	setupHandlerTestDB(t)
	owner := quotaPrefixUser(t, "legacy-owner", `{"max_records":5}`)
	other := quotaPrefixUser(t, "legacy-other", `{"max_records":5}`)
	domain := quotaPrefixDomain(t, owner.ID, "block.example.com")

	// 云端直接创建的记录，没有 Claim
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: "cloud-1", Name: "xxxx1", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("预置云端记录失败: %v", err)
	}

	if got := service.CheckSubdomainClaim(&domain, "xxxx1", other.ID, 1); got != service.ClaimDeniedByExisting {
		t.Errorf("该前缀下已有记录时应阻止认领，实际 %v", got)
	}
}

/* TestRecordQuotaBlocksAtPrefixLimit 前缀数达上限时应拒绝 */
func TestRecordQuotaBlocksAtPrefixLimit(t *testing.T) {
	setupHandlerTestDB(t)
	user := quotaPrefixUser(t, "at-prefix-limit", `{"max_records":2}`)
	domain := quotaPrefixDomain(t, user.ID, "limit.example.com")

	seedClaim(t, domain, "one", user.ID)
	seedClaim(t, domain, "two", user.ID)

	c := batchAuthzContext(t, user.ID)
	if QuotaAllows(c, "records") {
		t.Error("已占 2 个前缀而限额为 2，应拒绝再占")
	}
	if used, limit := QuotaUsageOf(c, "records"); used != 2 || limit != 2 {
		t.Errorf("用量与限额应为 2/2，实际 %d/%d", used, limit)
	}
}
