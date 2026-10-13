package service

import (
	"context"
	"testing"

	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
)

/*
 * 对账删除云端已消失的记录时，必须一并释放该前缀的占用
 *
 * 三条删除路径里，前两条（控制台的 DeleteRecord、v1 的 DeleteRecordSync）
 * 都会在删掉本地副本后调 ReleaseSubdomainClaimIfEmpty；对账这条此前只删副本。
 *
 * 触发场景是明确的：用户在**服务商控制台**把记录删了，面板无法即时得知，
 * 只能等对账发现。而这里不动占用的话，该前缀会被永久锁死——
 * 后来的人建不了同前缀的记录，且该用户持续被占着一条 records 额度。
 */

/* syncClaimProvider 返回固定记录集的对账用服务商 */
func newSyncClaimProvider(records []dns.Record) *syncFixedProvider {
	return &syncFixedProvider{records: records}
}

/* seedSyncClaimCase 建域名、本地副本与占用，返回域名 */
func seedSyncClaimCase(t *testing.T, tag, prefix string, remoteIDs []string) models.Domain {
	t.Helper()
	domain := setupSyncTestDomain(t)

	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: tag + "-r1", Name: prefix, Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("预置本地副本失败: %v", err)
	}
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: prefix, UserID: 1, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("预置占用失败: %v", err)
	}
	_ = remoteIDs
	return domain
}

/* claimCount 该域名下某前缀的占用行数 */
func claimCount(t *testing.T, domainID uint, prefix string) int64 {
	t.Helper()
	var n int64
	database.DB.Model(&models.SubdomainClaim{}).
		Where("did = ? AND prefix = ?", domainID, prefix).Count(&n)
	return n
}

/*
 * TestSyncReleasesClaimWhenRemoteRecordDeleted 云端已无该记录时释放占用
 *
 * 这是本轮修复的核心：服务商控制台删除后，对账是面板唯一的发现途径
 */
func TestSyncReleasesClaimWhenRemoteRecordDeleted(t *testing.T) {
	setupOptimizeTestDB(t)

	// 云端返回空清单：那条记录已在服务商侧被删
	registerSyncTestProvider(t, newSyncClaimProvider(nil))
	domain := seedSyncClaimCase(t, "sync-claim-gone", "gone", nil)

	if n := claimCount(t, domain.ID, "gone"); n != 1 {
		t.Fatalf("前置：占用应存在，实际 %d 行", n)
	}

	if err := SyncDomainRecords(context.Background(), &domain); err != nil {
		t.Fatalf("对账不应失败: %v", err)
	}

	if n := claimCount(t, domain.ID, "gone"); n != 0 {
		t.Errorf("云端已无该记录，占用应释放，实际残留 %d 行——该前缀被永久锁死并占一条额度", n)
	}
	var recN int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", domain.ID).Count(&recN)
	if recN != 0 {
		t.Errorf("本地副本应已删除，实际残留 %d 行", recN)
	}
}

/*
 * TestSyncKeepsClaimWhenSiblingRecordRemains 同前缀下还有云端记录时不释放
 *
 * 多值 A 的场景：云端只删了其中一条，另一条还在，占用必须保留。
 * 这条同时验证「先删副本再释放」这个顺序是对的——若不先删，
 * 刚删掉的这条会被 ReleaseSubdomainClaimIfEmpty 的计数算进去，占用永远放不掉
 */
func TestSyncKeepsClaimWhenSiblingRecordRemains(t *testing.T) {
	setupOptimizeTestDB(t)

	registerSyncTestProvider(t, newSyncClaimProvider([]dns.Record{
		{ID: "keep-r", Name: "multi", Type: "A", Value: "192.0.2.9", Status: "enable"},
	}))
	domain := setupSyncTestDomain(t)

	// 本地有两行同前缀记录与一条占用
	for _, id := range []string{"multi-r1", "multi-r2"} {
		if err := database.DB.Create(&models.DNSRecord{
			DomainID: domain.ID, RecordID: id, Name: "multi", Type: "A",
			Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
		}).Error; err != nil {
			t.Fatalf("预置本地副本失败: %v", err)
		}
	}
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "multi", UserID: 1, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("预置占用失败: %v", err)
	}

	if err := SyncDomainRecords(context.Background(), &domain); err != nil {
		t.Fatalf("对账不应失败: %v", err)
	}

	// multi-r1/r2 都不在云端清单里 → 两行副本被删；但云端新增了 keep-r（同为 multi 前缀）
	var recN int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", domain.ID).Count(&recN)
	if recN != 1 {
		t.Fatalf("应只剩云端那条，实际 %d 行", recN)
	}
	if n := claimCount(t, domain.ID, "multi"); n != 1 {
		t.Errorf("该前缀下仍有云端记录，占用应保留，实际 %d 行", n)
	}
}

/*
 * TestSyncDoesNotReleaseClaimOfOtherPrefix 释放按前缀收窄，不误伤其他前缀
 */
func TestSyncDoesNotReleaseClaimOfOtherPrefix(t *testing.T) {
	setupOptimizeTestDB(t)

	registerSyncTestProvider(t, newSyncClaimProvider(nil))
	domain := setupSyncTestDomain(t)

	for _, p := range []string{"alpha", "beta"} {
		database.DB.Create(&models.DNSRecord{
			DomainID: domain.ID, RecordID: p + "-r", Name: p, Type: "A",
			Value: "192.0.2.1", Status: "enable", Domain: domain.Name,
		})
		database.DB.Create(&models.SubdomainClaim{
			DomainID: domain.ID, Prefix: p, UserID: 1, Domain: domain.Name,
		})
	}

	// 只有 alpha 在云端被删（把 beta 保留在远端清单里）
	registerSyncTestProvider(t, newSyncClaimProvider([]dns.Record{
		{ID: "beta-r", Name: "beta", Type: "A", Value: "192.0.2.1", Status: "enable"},
	}))

	if err := SyncDomainRecords(context.Background(), &domain); err != nil {
		t.Fatalf("对账不应失败: %v", err)
	}

	if n := claimCount(t, domain.ID, "alpha"); n != 0 {
		t.Errorf("alpha 前缀的占用应释放，实际 %d 行", n)
	}
	if n := claimCount(t, domain.ID, "beta"); n != 1 {
		t.Errorf("beta 前缀仍被占用，不应释放，实际 %d 行", n)
	}
}
