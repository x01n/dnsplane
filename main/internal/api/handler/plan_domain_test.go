package handler

import (
	"testing"

	"gorm.io/gorm"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 套餐绑定域名的权限下发
 *
 * 套餐可以绑定若干个域名，授予时把这些域名的访问权限一并下发，撤销时回收。
 *
 * 关键约束是**只回收套餐下发的那部分**：管理员手工授予的同域名权限必须保留，
 * 否则「撤销套餐」会变成一次越权删除。Permission.Source 就是为此存在的
 * ——空为手工授权，"plan" 为套餐下发
 */

/* seedPlanWithDomains 造一个绑定了域名的套餐 */
func seedPlanWithDomains(t *testing.T, id string, domains string) models.Plan {
	t.Helper()
	plan := models.Plan{
		ID: id, Name: "套餐-" + id, Status: 1,
		Quotas:          `{"max_domains":1,"max_records":10}`,
		IncludedDomains: normalizeDomainList(domains),
	}
	if err := database.DB.Create(&plan).Error; err != nil {
		t.Fatalf("创建套餐失败: %v", err)
	}
	return plan
}

/* grantPlanInTx 以事务方式下发套餐域名权限 */
func grantPlanInTx(t *testing.T, userID uint, plan *models.Plan) {
	t.Helper()
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		return refreshPlanDomainPermissions(tx, userID, plan, nil)
	}); err != nil {
		t.Fatalf("下发失败: %v", err)
	}
}

/* TestNormalizeDomainList 域名清单归一化：去重、去空白、转小写 */
func TestNormalizeDomainList(t *testing.T) {
	cases := []struct{ in, want string }{
		{"a.com,b.com", "a.com,b.com"},
		{"  A.COM , b.com  ", "a.com,b.com"},
		{"a.com,a.com", "a.com"},
		{"", ""},
		{" , , ", ""},
	}
	for _, tc := range cases {
		if got := normalizeDomainList(tc.in); got != tc.want {
			t.Errorf("normalizeDomainList(%q) = %q，期望 %q", tc.in, got, tc.want)
		}
	}
}

/* TestGrantPlanDomainsCreatesPermissions 授予套餐时下发绑定域名的权限 */
func TestGrantPlanDomainsCreatesPermissions(t *testing.T) {
	setupHandlerTestDB(t)
	plan := seedPlanWithDomains(t, "p-dom1", "a.example.com,b.example.com")
	uid := newTestUser(t, "plan-dom-user", 1)

	grantPlanInTx(t, uid, &plan)

	var perms []models.Permission
	database.DB.Where("uid = ? AND source = ?", uid, "plan").Find(&perms)
	if len(perms) != 2 {
		t.Fatalf("应下发 2 条域名权限，实际 %d 条", len(perms))
	}
	for _, p := range perms {
		if p.Domain != "a.example.com" && p.Domain != "b.example.com" {
			t.Errorf("下发了未绑定的域名：%s", p.Domain)
		}
	}
}

/* TestGrantPlanDomainsIsIdempotent 重复授予同一套餐不产生重复权限行 */
func TestGrantPlanDomainsIsIdempotent(t *testing.T) {
	setupHandlerTestDB(t)
	plan := seedPlanWithDomains(t, "p-dom2", "c.example.com")
	uid := newTestUser(t, "plan-idem", 1)

	grantPlanInTx(t, uid, &plan)
	grantPlanInTx(t, uid, &plan)

	var n int64
	database.DB.Model(&models.Permission{}).Where("uid = ? AND source = ?", uid, "plan").Count(&n)
	if n != 1 {
		t.Errorf("重复授予不应堆出重复权限行，实际 %d 条", n)
	}
}

/*
 * TestRevokePlanKeepsManualPermissions 撤销套餐不得删掉管理员手工授予的权限
 *
 * 这是本模块最要紧的一条：两条来源不同的权限行指向同一域名时，
 * 撤销只能删掉套餐那条
 */
func TestRevokePlanKeepsManualPermissions(t *testing.T) {
	setupHandlerTestDB(t)
	plan := seedPlanWithDomains(t, "p-dom3", "keep.example.com")
	uid := newTestUser(t, "plan-manual", 1)

	// 管理员手工授予同一域名
	if err := database.DB.Create(&models.Permission{
		UserID: uid, Domain: "keep.example.com", SubDomain: "", Source: "",
	}).Error; err != nil {
		t.Fatalf("创建手工授权失败: %v", err)
	}
	grantPlanInTx(t, uid, &plan)

	var before int64
	database.DB.Model(&models.Permission{}).Where("uid = ?", uid).Count(&before)
	if before != 2 {
		t.Fatalf("前置不成立：应有 2 条权限（手工 + 套餐），实际 %d", before)
	}

	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		return revokePlanDomains(tx, uid)
	}); err != nil {
		t.Fatalf("回收失败: %v", err)
	}

	var manual, planRows int64
	database.DB.Model(&models.Permission{}).Where("uid = ? AND source = ?", uid, "").Count(&manual)
	database.DB.Model(&models.Permission{}).Where("uid = ? AND source = ?", uid, "plan").Count(&planRows)

	if manual != 1 {
		t.Errorf("管理员手工授权被误删：剩余 %d 条，期望 1 条", manual)
	}
	if planRows != 0 {
		t.Errorf("套餐下发的权限应被回收：剩余 %d 条", planRows)
	}
}

/* TestRefreshPlanDomainsReplacesOldSet 改绑域名后，换掉的域名权限应被回收 */
func TestRefreshPlanDomainsReplacesOldSet(t *testing.T) {
	setupHandlerTestDB(t)
	old := seedPlanWithDomains(t, "p-dom4", "old.example.com")
	uid := newTestUser(t, "plan-swap", 1)
	grantPlanInTx(t, uid, &old)

	newPlan := models.Plan{ID: "p-dom4", IncludedDomains: "new.example.com"}
	grantPlanInTx(t, uid, &newPlan)

	var perms []models.Permission
	database.DB.Where("uid = ? AND source = ?", uid, "plan").Find(&perms)
	if len(perms) != 1 || perms[0].Domain != "new.example.com" {
		t.Errorf("换绑后应只剩新域名的权限，实际 %+v", perms)
	}
}

/* TestPlanDomainsReproduceSubscriptionExpiry 下发的权限继承订阅的到期时间 */
func TestPlanDomainsReproduceSubscriptionExpiry(t *testing.T) {
	setupHandlerTestDB(t)
	plan := seedPlanWithDomains(t, "p-dom5", "exp.example.com")
	uid := newTestUser(t, "plan-expire", 1)

	exp := futureTime()
	if err := database.DB.Transaction(func(tx *gorm.DB) error {
		return refreshPlanDomainPermissions(tx, uid, &plan, exp)
	}); err != nil {
		t.Fatalf("下发失败: %v", err)
	}

	var perm models.Permission
	if err := database.DB.Where("uid = ? AND source = ?", uid, "plan").First(&perm).Error; err != nil {
		t.Fatalf("读权限失败: %v", err)
	}
	if perm.ExpireTime == nil || !perm.ExpireTime.Equal(*exp) {
		t.Errorf("权限到期时间应继承订阅的到期时间，实际 %v", perm.ExpireTime)
	}
}

/* TestPlanWithoutDomainsGrantsNothing 未绑定域名的套餐不下发任何权限 */
func TestPlanWithoutDomainsGrantsNothing(t *testing.T) {
	setupHandlerTestDB(t)
	plan := seedPlanWithDomains(t, "p-dom6", "")
	uid := newTestUser(t, "plan-nodomain", 1)

	grantPlanInTx(t, uid, &plan)

	var n int64
	database.DB.Model(&models.Permission{}).Where("uid = ?", uid).Count(&n)
	if n != 0 {
		t.Errorf("未绑定域名时不应下发权限，实际 %d 条", n)
	}
}
