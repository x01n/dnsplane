package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/dns/dnstest"
	"main/internal/models"
	"main/internal/service"
)

/*
 * 批量操作删除记录时必须释放子域名占用
 *
 * 四条删除路径里，控制台的单条 DeleteRecord、v1 的 DeleteRecordSync、
 * 以及对账 SyncDomainRecords 都会在删掉本地副本后调
 * ReleaseSubdomainClaimIfEmpty；批量操作这条此前只删副本。
 *
 * 不释放的后果：该前缀被永久锁死（他人建不了同前缀的记录），
 * 且持续占住创建者一条 records 额度——而面板上已看不到任何对应记录
 */

/* batchDeleteFixture 造域名、本地副本与占用 */
func batchDeleteFixture(t *testing.T, tag, prefix string) (models.Domain, string) {
	t.Helper()
	owner := models.User{Username: tag + "-own", Email: tag + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	account := models.Account{UserID: owner.ID, Type: "cloudflare", Name: tag + "-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: tag + ".example.com", ThirdID: tag + "-zone"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}

	recordID := tag + "-r1"
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: recordID, Name: prefix, Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name, CreatedBy: owner.ID,
	}).Error; err != nil {
		t.Fatalf("创建本地副本失败: %v", err)
	}
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: prefix, UserID: owner.ID, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("登记占用失败: %v", err)
	}
	return domain, recordID
}

/* batchClaimRows 该域名下某前缀的占用行数 */
func batchClaimRows(t *testing.T, domainID uint, prefix string) int64 {
	t.Helper()
	var n int64
	database.DB.Model(&models.SubdomainClaim{}).
		Where("did = ? AND prefix = ?", domainID, prefix).Count(&n)
	return n
}

/*
 * TestBatchDeleteReleasesClaim 复刻批量删除的清理段，断言占用被释放
 *
 * 走的是清理语句本身而非 handler——BatchActionRecords 需要 gin 上下文、
 * 模块权限与真实 provider，且写入在 SafeGo 内不可断言。
 * 用例的作用是：谁把释放那几行删掉，这里立刻红
 */
func TestBatchDeleteReleasesClaim(t *testing.T) {
	setupHandlerTestDB(t)
	domain, recordID := batchDeleteFixture(t, "batch-del", "gone")

	if n := batchClaimRows(t, domain.ID, "gone"); n != 1 {
		t.Fatalf("前置：占用应存在，实际 %d 行", n)
	}

	// 复刻 batch.go 中 action == "delete" 的清理段
	releasedPrefix := ""
	if local, ok := service.LocalRecord(domain.ID, recordID); ok {
		releasedPrefix = local.Name
	}
	service.DeleteLocalRecord(domain.ID, recordID)
	if releasedPrefix != "" {
		service.ReleaseSubdomainClaimIfEmpty(domain.ID, releasedPrefix)
	}

	if n := batchClaimRows(t, domain.ID, "gone"); n != 0 {
		t.Errorf("批量删除后占用应释放，实际残留 %d 行——该前缀被永久锁死并占一条额度", n)
	}
	var recN int64
	database.DB.Model(&models.DNSRecord{}).Where("did = ?", domain.ID).Count(&recN)
	if recN != 0 {
		t.Errorf("本地副本应已删除，实际残留 %d 行", recN)
	}
}

/*
 * TestBatchDeleteKeepsClaimWhenSiblingRemains 多值 A 只删一条时占用保留
 *
 * 释放判据复用 ReleaseSubdomainClaimIfEmpty：该前缀下仍有记录就不释放
 */
func TestBatchDeleteKeepsClaimWhenSiblingRemains(t *testing.T) {
	setupHandlerTestDB(t)
	domain, recordID := batchDeleteFixture(t, "batch-keep", "multi")

	// 同前缀下再有一条
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: "batch-keep-r2", Name: "multi", Type: "A",
		Value: "192.0.2.2", Status: "enable", Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("创建第二条记录失败: %v", err)
	}

	releasedPrefix := ""
	if local, ok := service.LocalRecord(domain.ID, recordID); ok {
		releasedPrefix = local.Name
	}
	service.DeleteLocalRecord(domain.ID, recordID)
	if releasedPrefix != "" {
		service.ReleaseSubdomainClaimIfEmpty(domain.ID, releasedPrefix)
	}

	if n := batchClaimRows(t, domain.ID, "multi"); n != 1 {
		t.Errorf("同前缀下仍有记录，占用应保留，实际 %d 行", n)
	}
}

/*
 * TestBatchActionDeleteReleasesClaim 端到端：真的调 BatchActionRecords 走完删除
 *
 * 第一条用例复刻清理语句、断言的是「语句对不对」；这一条反过来断言
 * 「handler 真的调用了它」。写入在 SafeGo 内是异步的，用轮询等结果——
 * 断言的是数据库里的行，不是某个函数被调用过
 *
 * 前置用 dnstest 的内存服务商，使 provider 构造与云端删除都能真实走通
 */
func TestBatchActionDeleteReleasesClaim(t *testing.T) {
	setupHandlerTestDB(t)
	dnstest.Register()
	dnstest.Reset()

	owner := models.User{Username: "batch-e2e", Email: "be@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建用户失败: %v", err)
	}
	account := models.Account{UserID: owner.ID, Type: "memory", Name: "batch-e2e-acc", Config: "{}"}
	database.DB.Create(&account)
	domain := models.Domain{AccountID: account.ID, Name: "batch-e2e.example.com", ThirdID: "batch-e2e-zone"}
	database.DB.Create(&domain)

	recordID := dnstest.For(domain.ThirdID).Put("", "gone", "A", "192.0.2.1")
	database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: recordID, Name: "gone", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name, CreatedBy: owner.ID,
	})
	database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "gone", UserID: owner.ID, Domain: domain.Name,
	})

	body, _ := json.Marshal(map[string]interface{}{
		"domain_id":  itoaUint(domain.ID),
		"record_ids": []string{recordID},
		"action":     "delete",
	})
	w := batchActionAs(t, body, owner)
	if code, msg := consoleResp(t, w); code != 0 {
		t.Fatalf("接口应回提交成功，实际 code=%d msg=%s", code, msg)
	}

	// 异步写入，轮询等结果
	var n int64
	for i := 0; i < 100; i++ {
		database.DB.Model(&models.SubdomainClaim{}).
			Where("did = ? AND prefix = ?", domain.ID, "gone").Count(&n)
		if n == 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if n != 0 {
		t.Errorf("批量删除后占用应释放，实际残留 %d 行——handler 没调用释放", n)
	}
	if cnt := dnstest.For(domain.ThirdID).Count(); cnt != 0 {
		t.Errorf("云端记录应已删除，实际 %d 条", cnt)
	}
}

/*
 * TestBatchDeleteClaimsPerPrefixOnly 删除按 record_id 收窄，不误伤其他前缀
 */
func TestBatchDeleteClaimsPerPrefixOnly(t *testing.T) {
	setupHandlerTestDB(t)
	domain, recordID := batchDeleteFixture(t, "batch-scope", "alpha")

	// 另一个前缀，不在本次删除范围内
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: "batch-scope-other", Name: "beta", Type: "A",
		Value: "192.0.2.3", Status: "enable", Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("创建另一前缀记录失败: %v", err)
	}
	if err := database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "beta", UserID: 1, Domain: domain.Name,
	}).Error; err != nil {
		t.Fatalf("登记 beta 占用失败: %v", err)
	}

	releasedPrefix := ""
	if local, ok := service.LocalRecord(domain.ID, recordID); ok {
		releasedPrefix = local.Name
	}
	service.DeleteLocalRecord(domain.ID, recordID)
	if releasedPrefix != "" {
		service.ReleaseSubdomainClaimIfEmpty(domain.ID, releasedPrefix)
	}

	if n := batchClaimRows(t, domain.ID, "alpha"); n != 0 {
		t.Errorf("alpha 的占用应释放，实际 %d 行", n)
	}
	if n := batchClaimRows(t, domain.ID, "beta"); n != 1 {
		t.Errorf("beta 未被删除，占用不应受影响，实际 %d 行", n)
	}
}

/*
 * batchActionAs 以指定用户身份调 BatchActionRecords
 *
 * 不复用 postJSON：那个助手不设置身份上下文，而本 handler 需要 user_id / level
 * 才能走完权限校验。控制台用例里身份相关的助手本就随各自领域留在原文件
 */
func batchActionAs(t *testing.T, body []byte, u models.User) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/domains/records/batch-action", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", itoaUint(u.ID))
	c.Set("username", u.Username)
	c.Set("level", u.Level)
	BatchActionRecords(c)
	return w
}
