package apiv1

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/dns/dnstest"
	"main/internal/models"
	"main/internal/service"
)

/*
 * v1 改名（UpdateRecordSync）的两个新增约束
 *
 * 一、req.Name 是写入云端的**新名**，此前从未被校验。resolveRecordScope 校验的是
 *     original.Name（该记录当前的名字），于是持有 old 子域授权的用户可以把记录
 *     改名到任意子域
 *
 * 二、改名要迁移子域名前缀的占用：新前缀走 CheckSubdomainClaim，旧前缀走
 *     ReleaseSubdomainClaimIfEmpty。此前两处都没有，改名会把旧前缀锁死并占配额
 *
 * 用 dnstest 的内存服务商走完真实的 dns.GetProvider 装配链，不做桩替换——
 * 判定逻辑（CheckSubdomainClaim 会回查 dns_records）依赖真实 SQL，用桩会失去覆盖
 */

/* seedRenameCase 建拥有者、域名、账户、云端记录与本地副本，返回所需标识 */
func seedRenameCase(t *testing.T, tag, oldName string) (models.User, models.Domain, string, string) {
	t.Helper()
	dnstest.Register()

	owner := seedV1User(t, tag+"-owner")
	// Config 必须是非空的合法 JSON：providerForDomain 会 unmarshal 它，空串直接判配置无法解析
	account := models.Account{UserID: owner.ID, Type: "memory", Name: tag + "-acc", Config: "{}"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: tag + ".example.com", ThirdID: tag + "-zone"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}

	recordID := dnstest.For(domain.ThirdID).Put("", oldName, "A", "192.0.2.1")

	// 本地副本：控制台与 v1 的写路径都会回填它，改名判定要读它的旧名
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: recordID, Name: oldName, Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name, CreatedBy: owner.ID,
	}).Error; err != nil {
		t.Fatalf("创建本地副本失败: %v", err)
	}

	const key = "aaaabbbbccccddddaaaabbbbccccdddd"
	if err := database.DB.Model(&owner).Updates(map[string]interface{}{"is_api": true, "api_key": key}).Error; err != nil {
		t.Fatalf("写入 API Key 失败: %v", err)
	}
	return owner, domain, recordID, key
}

/* renameEngine 只挂 UpdateRecordSync 的路由 */
func renameEngine() *gin.Engine {
	return v1TestEngine(func(v1 *gin.RouterGroup) {
		v1.POST("/records/update", UpdateRecordSync)
	})
}

/* renameBody 拼一次改名请求体 */
func renameBody(domainID uint, recordID, newName string) string {
	b, _ := json.Marshal(map[string]interface{}{
		"domain_id": itoa(domainID),
		"record_id": recordID,
		"name":      newName,
		"type":      "A",
		"value":     "192.0.2.1",
	})
	return string(b)
}

/* callRename 发一次改名请求并解出 v1 错误结构 */
func callRename(t *testing.T, r *gin.Engine, uid uint, key, body string) v1Err {
	t.Helper()
	path := "/api/v1/records/update"
	resp := doRequest(r, http.MethodPost, path, []byte(body), signedHeaders(t, uid, key, http.MethodPost, path, body))
	var got v1Err
	decodeBody(t, resp, &got)
	return got
}

/*
 * TestRenameDeniedWhenNewSubdomainNotGranted 改名到未授权子域必须拒绝
 *
 * 授权覆盖 old.example.com 这一条前缀，改名目标是 other。此前没有任何校验，
 * 调用方可以把记录搬到任意子域
 */
func TestRenameDeniedWhenNewSubdomainNotGranted(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	owner, domain, recordID, _ := seedRenameCase(t, "ren-deny", "old")

	delegated := seedV1User(t, "ren-deny-user")
	// 只授权 old 这一个前缀
	seedDelegatedPermission(t, delegated.ID, domain.Name, false, "old")
	const key = "bbbbccccddddaaaabbbbccccddddaaaa"
	if err := database.DB.Model(&delegated).Updates(map[string]interface{}{"is_api": true, "api_key": key}).Error; err != nil {
		t.Fatalf("写入 API Key 失败: %v", err)
	}
	_ = owner

	r := renameEngine()
	got := callRename(t, r, delegated.ID, key, renameBody(domain.ID, recordID, "other"))

	if got.Code != CodeSubDomainDenied {
		t.Errorf("改名到未授权子域应回 CodeSubDomainDenied(%d)，实际 code=%d message=%q",
			CodeSubDomainDenied, got.Code, got.Message)
	}
	// 被拒后云端记录不得变更
	rec, ok := dnstest.For(domain.ThirdID).Get(recordID)
	if !ok || rec.Name != "old" {
		t.Errorf("被拒的改名不应改动云端记录，实际 name=%q", rec.Name)
	}
}

/* TestRenameAllowedWithinGrantedSubdomain 授权覆盖的新名照常放行 */
func TestRenameAllowedWithinGrantedSubdomain(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	owner, domain, recordID, _ := seedRenameCase(t, "ren-allow", "old")

	delegated := seedV1User(t, "ren-allow-user")
	// 授权整个域名（sub 为空即不限定前缀）
	seedDelegatedPermission(t, delegated.ID, domain.Name, false, "")
	const key = "ccccddddaaaabbbbccccddddaaaabbbb"
	database.DB.Model(&delegated).Updates(map[string]interface{}{"is_api": true, "api_key": key})
	_ = owner

	r := renameEngine()
	got := callRename(t, r, delegated.ID, key, renameBody(domain.ID, recordID, "moved"))

	if got.Code != 0 {
		t.Fatalf("授权范围内的改名应成功，实际 code=%d message=%q", got.Code, got.Message)
	}
	rec, _ := dnstest.For(domain.ThirdID).Get(recordID)
	if rec.Name != "moved" {
		t.Errorf("云端记录名应已更新为 moved，实际 %q", rec.Name)
	}
}

/*
 * TestRenameMovesSubdomainClaim 改名后占用迁到新前缀，旧前缀释放
 *
 * 这是本轮新增的核心行为：此前改名会让旧前缀被永久锁死并占住一条配额
 */
func TestRenameMovesSubdomainClaim(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	owner, domain, recordID, key := seedRenameCase(t, "ren-move", "old")

	// 改名前的占用：旧前缀归拥有者
	service.RegisterSubdomainClaim(&domain, "old", owner.ID)

	r := renameEngine()
	got := callRename(t, r, owner.ID, key, renameBody(domain.ID, recordID, "fresh"))
	if got.Code != 0 {
		t.Fatalf("拥有者改名应成功，实际 code=%d message=%q", got.Code, got.Message)
	}

	var oldClaim, newClaim int64
	database.DB.Model(&models.SubdomainClaim{}).Where("did = ? AND prefix = ?", domain.ID, "old").Count(&oldClaim)
	database.DB.Model(&models.SubdomainClaim{}).Where("did = ? AND prefix = ?", domain.ID, "fresh").Count(&newClaim)

	if newClaim != 1 {
		t.Errorf("新前缀应被登记占用，实际 %d 行", newClaim)
	}
	if oldClaim != 0 {
		t.Errorf("旧前缀下已无记录，占用应释放，实际残留 %d 行", oldClaim)
	}
	if n := service.CountUserSubdomainClaims(owner.ID); n != 1 {
		t.Errorf("改名后该用户的占用数应仍为 1（不是 2），实际 %d", n)
	}
}

/*
 * TestRenameKeepsClaimWhenOldPrefixStillHasRows 多值 A 改掉一条时旧前缀占用保留
 *
 * 旧前缀下还有别的记录时不能释放，否则另一个人可以抢走剩下的那条记录所在的前缀。
 * 这条是「按既有判据迁移」与「直接搬移」两种方案的分歧点
 */
func TestRenameKeepsClaimWhenOldPrefixStillHasRows(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	owner, domain, recordID, key := seedRenameCase(t, "ren-keep", "multi")

	// 同前缀下再有一条记录（多值 A）
	extraID := dnstest.For(domain.ThirdID).Put("", "multi", "A", "192.0.2.2")
	database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: extraID, Name: "multi", Type: "A",
		Value: "192.0.2.2", Status: "enable", Domain: domain.Name, CreatedBy: owner.ID,
	})
	service.RegisterSubdomainClaim(&domain, "multi", owner.ID)

	r := renameEngine()
	got := callRename(t, r, owner.ID, key, renameBody(domain.ID, recordID, "solo"))
	if got.Code != 0 {
		t.Fatalf("改名应成功，实际 code=%d message=%q", got.Code, got.Message)
	}

	var oldClaim int64
	database.DB.Model(&models.SubdomainClaim{}).Where("did = ? AND prefix = ?", domain.ID, "multi").Count(&oldClaim)
	if oldClaim != 1 {
		t.Errorf("旧前缀下仍有记录，占用应保留，实际 %d 行", oldClaim)
	}
	if n := service.CountUserSubdomainClaims(owner.ID); n != 2 {
		t.Errorf("应同时持有 multi 与 solo 两个占用，实际 %d", n)
	}
}

/*
 * TestRenameDeniedWhenNewPrefixClaimedByOther 改名到他人已占的前缀必须拒绝
 *
 * 判据与建记录时一致：CheckSubdomainClaim 给出的 ClaimTakenByOther 也拦改名
 *
 * 改名者必须是**非所有者**的普通用户：域名所有者与管理员本就不受占用限制
 *（用户明确要求「除了域名所有者和管理员」），拿拥有者测这条会恒放行
 */
func TestRenameDeniedWhenNewPrefixClaimedByOther(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	_, domain, recordID, _ := seedRenameCase(t, "ren-taken", "mine")

	// 一个对该域名有写权限、但不是所有者的用户
	delegated := seedV1User(t, "ren-taken-delegated")
	seedDelegatedPermission(t, delegated.ID, domain.Name, false, "")
	const key = "ddddaaaabbbbccccddddaaaabbbbcccc"
	database.DB.Model(&delegated).Updates(map[string]interface{}{"is_api": true, "api_key": key})

	other := seedV1User(t, "ren-taken-other")
	// 他人先占了目标前缀，且该前缀下已有记录
	database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "taken", UserID: other.ID, Domain: domain.Name,
	})
	database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: "taken-r", Name: "taken", Type: "A",
		Value: "192.0.2.9", Status: "enable", Domain: domain.Name, CreatedBy: other.ID,
	})

	r := renameEngine()
	got := callRename(t, r, delegated.ID, key, renameBody(domain.ID, recordID, "taken"))
	if got.Code != CodeSubDomainDenied {
		t.Errorf("改名到他人已占前缀应回 CodeSubDomainDenied(%d)，实际 code=%d message=%q",
			CodeSubDomainDenied, got.Code, got.Message)
	}

	rec, _ := dnstest.For(domain.ThirdID).Get(recordID)
	if rec.Name != "mine" {
		t.Errorf("被拒的改名不应改动云端记录，实际 name=%q", rec.Name)
	}
}

/*
 * TestRenameByOwnerBypassesClaim 域名所有者不受占用限制
 *
 * 与上一条配对：同一场景、同一个目标前缀，只是改名者换成了所有者。
 * 两条一起锁定「谁能绕过占用」这条边界，缺一条都会让边界无从判断
 */
func TestRenameByOwnerBypassesClaim(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	owner, domain, recordID, key := seedRenameCase(t, "ren-owner", "mine")

	other := seedV1User(t, "ren-owner-other")
	database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "taken", UserID: other.ID, Domain: domain.Name,
	})

	r := renameEngine()
	got := callRename(t, r, owner.ID, key, renameBody(domain.ID, recordID, "taken"))
	if got.Code != 0 {
		t.Errorf("域名所有者不受占用限制，改名应成功，实际 code=%d message=%q", got.Code, got.Message)
	}
}

/*
 * TestRenameToSameNameDoesNotTouchClaims 同名前缀内的常规编辑不参与占用判定
 *
 * 改值、改 TTL 时 req.Name 与原值相同，不应触发任何 Claim 迁移
 */
func TestRenameToSameNameDoesNotTouchClaims(t *testing.T) {
	setupV1TestDB(t)
	dnstest.Reset()
	owner, domain, recordID, key := seedRenameCase(t, "ren-same", "same")

	other := seedV1User(t, "ren-same-other")
	// 同一个前缀被别人占了：同前缀内的编辑不该因此被拒（占用判据只管改名）
	database.DB.Create(&models.SubdomainClaim{
		DomainID: domain.ID, Prefix: "same", UserID: other.ID, Domain: domain.Name,
	})

	r := renameEngine()
	got := callRename(t, r, owner.ID, key, renameBody(domain.ID, recordID, "same"))
	if got.Code != 0 {
		t.Errorf("同名编辑不应被占用判定拦截，实际 code=%d message=%q", got.Code, got.Message)
	}
}
