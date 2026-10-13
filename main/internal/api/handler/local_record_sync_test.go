package handler

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
	"main/internal/service"
)

/*
 * 本地记录副本必须被写入路径即时回填
 *
 * 配额判定（quotaUsed 的 records 维度）读的是 dns_records 表，而它由定时同步
 * 每 30 分钟从云端拉一次。若创建路径不回填，用户在两次同步之间可以连续创建
 * 任意多条记录而不触发上限——判定窗口长达一整个同步周期。
 *
 * 这批用例锁的是「回填这件事真的发生」，而不是某个函数被调用过。
 * 走的是真实的 service 层函数，断言的是数据库里的行
 */

/* setupLocalRecordDB 建一个域名，返回其 ID 与名称 */
func setupLocalRecordDB(t *testing.T, name string) (uint, string) {
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
	return domain.ID, domain.Name
}

/* TestUpsertLocalRecordVisibleToQuota 回填后配额计数应立刻看到这条记录 */
func TestUpsertLocalRecordVisibleToQuota(t *testing.T) {
	setupHandlerTestDB(t)
	domainID, domainName := setupLocalRecordDB(t, "quota-visible")

	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID: domainID, RecordID: "r-new", Name: "www", Type: "A",
		Value: "192.0.2.1", Line: "default", TTL: 600, Status: "enable", Domain: domainName,
	})

	var n int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", domainID).Count(&n)
	if n != 1 {
		t.Errorf("回填后本地副本应有 1 条，实际 %d", n)
	}
}

/* TestUpsertLocalRecordUpdatesExisting 同一条云端记录二次写入应更新而不是新增或失败 */
func TestUpsertLocalRecordUpdatesExisting(t *testing.T) {
	setupHandlerTestDB(t)
	domainID, domainName := setupLocalRecordDB(t, "upsert-twice")

	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID: domainID, RecordID: "r1", Name: "www", Type: "A",
		Value: "192.0.2.1", TTL: 600, Status: "enable", Domain: domainName,
	})
	// 改记录值：同一个 record_id 再写一次
	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID: domainID, RecordID: "r1", Name: "www", Type: "A",
		Value: "192.0.2.99", TTL: 300, Status: "enable", Domain: domainName,
	})

	var rows []models.DNSRecord
	database.DB.Where("did = ?", domainID).Find(&rows)
	if len(rows) != 1 {
		t.Fatalf("同一条记录二次写入不应产生新行，实际 %d 行", len(rows))
	}
	if rows[0].Value != "192.0.2.99" || rows[0].TTL != 300 {
		t.Errorf("二次写入应更新内容，实际 value=%s ttl=%d", rows[0].Value, rows[0].TTL)
	}
}

/* TestDeleteLocalRecordRemovesRow 删除成功后本地副本应同步移除 */
func TestDeleteLocalRecordRemovesRow(t *testing.T) {
	setupHandlerTestDB(t)
	domainID, domainName := setupLocalRecordDB(t, "delete-local")

	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID: domainID, RecordID: "r-del", Name: "www", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domainName,
	})
	service.DeleteLocalRecord(domainID, "r-del")

	var n int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", domainID).Count(&n)
	if n != 0 {
		t.Errorf("删除后本地副本应为空，实际 %d 行", n)
	}
}

/* TestDeleteLocalRecordScopedByDomain 删除必须按域名限定，不能跨域名误删同 ID 记录 */
func TestDeleteLocalRecordScopedByDomain(t *testing.T) {
	setupHandlerTestDB(t)

	d1, n1 := setupLocalRecordDB(t, "scope-a")
	d2, n2 := setupLocalRecordDB(t, "scope-b")

	for _, x := range []struct {
		id   uint
		name string
	}{{d1, n1}, {d2, n2}} {
		service.UpsertLocalRecord(&models.DNSRecord{
			DomainID: x.id, RecordID: "same-id", Name: "www", Type: "A",
			Value: "192.0.2.1", Status: "enable", Domain: x.name,
		})
	}

	service.DeleteLocalRecord(d1, "same-id")

	var kept int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", d2).Count(&kept)
	if kept != 1 {
		t.Errorf("另一域名下同 record_id 的记录被误删，剩余 %d 行", kept)
	}
}

/* TestLocalRecordReturnsStoredRow 读回本地行，供更新路径保留未传字段 */
func TestLocalRecordReturnsStoredRow(t *testing.T) {
	setupHandlerTestDB(t)
	domainID, domainName := setupLocalRecordDB(t, "local-read")

	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID: domainID, RecordID: "r-read", Name: "mail", Type: "MX",
		Value: "mx.example.com", TTL: 1800, MX: 10, Status: "enable", Domain: domainName,
	})

	rec, ok := service.LocalRecord(domainID, "r-read")
	if !ok {
		t.Fatal("已写入的行应能被读回")
	}
	if rec.Type != "MX" || rec.TTL != 1800 || rec.MX != 10 {
		t.Errorf("读回的内容与写入不符：type=%s ttl=%d mx=%d", rec.Type, rec.TTL, rec.MX)
	}
}

/*
 * TestLocalRecordFallsBackWhenMissing 本地无该行时报告 not ok
 *
 * 这条原先叫 TestCurrentRecordMetaFallsBackWhenMissing，测的是
 * service.CurrentRecordMeta。该函数在 R40 被删（零调用的死代码），
 * 但它覆盖的场景——「本地查不到时调用方必须拿不到零值」——是真实的：
 * 调用方据此走回查云端，若拿到零值就会把云端记录覆盖成空。
 * 改为对仍在使用的 service.LocalRecord 断言同一件事
 */
func TestLocalRecordFallsBackWhenMissing(t *testing.T) {
	setupHandlerTestDB(t)
	domainID, _ := setupLocalRecordDB(t, "meta-missing")

	if rec, ok := service.LocalRecord(domainID, "not-there"); ok {
		t.Errorf("本地无该行时应报告 false，否则调用方会拿到零值去覆盖云端（拿到 %+v）", rec)
	}
}
