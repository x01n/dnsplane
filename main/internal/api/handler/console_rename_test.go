package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/database"
	"main/internal/dns/dnstest"
	"main/internal/models"
	"main/internal/service"
)

/*
 * 控制台 UpdateRecord 的改名：两侧授权与占用迁移
 *
 * 两个约束：
 *   一、改名要**两侧都授权**。只校验 req.Name（新名）的话，持有 new 前缀授权的
 *       用户可以改 old 前缀下他人的记录，把它从他自己的前缀下劫走。
 *       探针实测确认过修复前放行：只授权 hijack 的用户改掉 victim 下的记录，
 *       接口回「提交成功」
 *   二、改名要迁移占用。旧前缀不释放会把它永久锁死并持续占一条 records 额度
 *
 * 控制台把写逻辑放在 utils.SafeGo 里，接口只回「提交成功」，因此**占用迁移
 * 与云端写入无法在这个层面断言**——那部分由 v1 的同名用例覆盖（它已同步化）。
 * 这里断言的是同步阶段的鉴权，以及占用迁移所依赖的旧名取值逻辑
 */

/* consoleRenameFixture 造拥有者、域名、账户、云端记录与本地副本 */
func consoleRenameFixture(t *testing.T, tag, oldName string) (models.User, models.Domain, string) {
	t.Helper()
	dnstest.Register()
	dnstest.Reset()

	owner := models.User{Username: tag + "-own", Email: tag + "@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建拥有者失败: %v", err)
	}
	account := models.Account{UserID: owner.ID, Type: "memory", Name: tag + "-acc", Config: "{}"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: tag + ".example.com", ThirdID: tag + "-zone"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}

	recordID := dnstest.For(domain.ThirdID).Put("", oldName, "A", "192.0.2.1")
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: domain.ID, RecordID: recordID, Name: oldName, Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: domain.Name, CreatedBy: owner.ID,
	}).Error; err != nil {
		t.Fatalf("创建本地副本失败: %v", err)
	}
	return owner, domain, recordID
}

/* callConsoleUpdate 以指定用户身份调 UpdateRecord */
func callConsoleUpdate(t *testing.T, u models.User, domainID uint, recordID, newName string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]interface{}{
		"domain_id": itoaUint(domainID),
		"record_id": recordID,
		"name":      newName,
		"type":      "A",
		"value":     "192.0.2.1",
	})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/api/domains/records/update", bytes.NewReader(body))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Set("user_id", itoaUint(u.ID))
	c.Set("username", u.Username)
	c.Set("level", u.Level)
	UpdateRecord(c)
	return w
}

/* consoleResp 取响应里的 code 与 msg */
func consoleResp(t *testing.T, w *httptest.ResponseRecorder) (int, string) {
	t.Helper()
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
	}
	return out.Code, out.Msg
}

/*
 * TestConsoleRenameDeniedWhenOldSubdomainNotGranted 只有新前缀授权时改名必须拒绝
 *
 * 这是修复前放行的那个场景。攻击者只被授予 hijack 前缀，目标记录在 victim 前缀下
 */
func TestConsoleRenameDeniedWhenOldSubdomainNotGranted(t *testing.T) {
	setupHandlerTestDB(t)
	owner, domain, recordID := consoleRenameFixture(t, "con-deny", "victim")
	_ = owner

	attacker := models.User{Username: "con-atk", Email: "ca@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&attacker).Error; err != nil {
		t.Fatalf("创建攻击者失败: %v", err)
	}
	// 只授权 hijack，没有 victim
	if err := database.DB.Create(&models.Permission{
		UserID: attacker.ID, Domain: domain.Name, SubDomain: "hijack", ReadOnly: false,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}

	w := callConsoleUpdate(t, attacker, domain.ID, recordID, "hijack")
	if code, msg := consoleResp(t, w); code == 0 {
		t.Errorf("只授权新前缀时改他人前缀下的记录必须拒绝，实际放行：%s", msg)
	}
}

/*
 * TestConsoleRenameAllowedWhenBothSubdomainsGranted 两侧都授权时放行
 *
 * 与上一条配对：同一场景，只把 victim 也授权给该用户
 */
func TestConsoleRenameAllowedWhenBothSubdomainsGranted(t *testing.T) {
	setupHandlerTestDB(t)
	_, domain, recordID := consoleRenameFixture(t, "con-allow", "victim")

	user := models.User{Username: "con-both", Email: "cb@example.com", Level: 1, Status: 1}
	database.DB.Create(&user)
	if err := database.DB.Create(&models.Permission{
		UserID: user.ID, Domain: domain.Name, SubDomain: "hijack", ReadOnly: false,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}
	// victim 单独再授一条
	if err := database.DB.Create(&models.Permission{
		UserID: user.ID, Domain: domain.Name, SubDomain: "victim", ReadOnly: false,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}

	w := callConsoleUpdate(t, user, domain.ID, recordID, "hijack")
	if code, msg := consoleResp(t, w); code != 0 {
		t.Errorf("两侧都授权时应放行，实际 code=%d msg=%s", code, msg)
	}
}

/*
 * TestConsoleRenameToSameNameSkipsOldNameCheck 同名编辑不触发旧名校验
 *
 * 改值、改 TTL 时 oldName == req.Name，不该为旧名再查一次权限——
 * 否则「只授权域名但不限定前缀」之外的授权形态会被这条新校验误伤
 */
func TestConsoleRenameToSameNameSkipsOldNameCheck(t *testing.T) {
	setupHandlerTestDB(t)
	_, domain, recordID := consoleRenameFixture(t, "con-same", "same")

	user := models.User{Username: "con-same-u", Email: "cs@example.com", Level: 1, Status: 1}
	database.DB.Create(&user)
	if err := database.DB.Create(&models.Permission{
		UserID: user.ID, Domain: domain.Name, SubDomain: "same", ReadOnly: false,
	}).Error; err != nil {
		t.Fatalf("创建授权失败: %v", err)
	}

	w := callConsoleUpdate(t, user, domain.ID, recordID, "same")
	if code, msg := consoleResp(t, w); code != 0 {
		t.Errorf("同名前缀内的编辑应放行，实际 code=%d msg=%s", code, msg)
	}
}

/*
 * TestConsoleRenameOwnerAlwaysAllowed 域名所有者不受子域授权限制
 *
 * 与第一条配对：同一场景，把操作者换成拥有者
 */
func TestConsoleRenameOwnerAlwaysAllowed(t *testing.T) {
	setupHandlerTestDB(t)
	owner, domain, recordID := consoleRenameFixture(t, "con-own", "victim")

	w := callConsoleUpdate(t, owner, domain.ID, recordID, "hijack")
	if code, msg := consoleResp(t, w); code != 0 {
		t.Errorf("域名所有者应放行，实际 code=%d msg=%s", code, msg)
	}
}

/*
 * TestConsoleRenameOldNameComesFromLocalCopy 旧名取自本地副本
 *
 * 占用迁移与旧名校验都依赖 oldName，控制台只能从本地副本取（接口同步阶段
 * 不去回查云端）。这条锁定「副本里的旧名确实能被取到」这个前提——
 * 若哪天 LocalRecord 的查询条件变了，改名逻辑会静默退化成「不迁移、不校验旧名」
 */
func TestConsoleRenameOldNameComesFromLocalCopy(t *testing.T) {
	setupHandlerTestDB(t)
	_, domain, recordID := consoleRenameFixture(t, "con-local", "before")

	local, ok := service.LocalRecord(domain.ID, recordID)
	if !ok {
		t.Fatal("本地副本应存在，改名逻辑取不到旧名就会跳过校验与迁移")
	}
	if local.Name != "before" {
		t.Errorf("本地副本里的旧名应为 before，实际 %q", local.Name)
	}
	// 归一化后与请求里的新名可比
	if service.NormalizeRecordPrefix(local.Name) == service.NormalizeRecordPrefix("after") {
		t.Error("归一化后旧名与新名不应相等，否则改名不会被识别")
	}
}
