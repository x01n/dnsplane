package handler

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 证书账户的配额维度
 *
 * 此前两条创建路径的口径是错的：
 *   CreateCertAccount（控制台 /cert/accounts）  **完全没有配额检查**
 *   DeployAccountAdd（v1 /deploy-accounts/create）CheckQuota("accounts")
 *
 * 而 accounts 维度统计的是 **DNS 账户**（accounts 表），两者写的是
 * cert_accounts 表——维度错配，等于管理员设的「DNS 账户」上限
 * 既拦不住证书账户，也拦不住部署账户。
 *
 * 新增 max_cert_accounts 维度，两条路径共用：证书账户与部署账户同表、
 * 形态一致（都是「某服务商的连接配置」），对用户而言是一件事
 */

/* TestCertAccountsDimensionCountsBothKinds cert_accounts 维度同时计入两种账户 */
func TestCertAccountsDimensionCountsBothKinds(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaPrefixUser(t, "cert-dim", `{"max_cert_accounts":5}`)

	for _, acc := range []models.CertAccount{
		{UserID: u.ID, Type: "letsencrypt", Name: "sign-acc", IsDeploy: false},
		{UserID: u.ID, Type: "ssh", Name: "deploy-acc", IsDeploy: true},
	} {
		if err := database.DB.Create(&acc).Error; err != nil {
			t.Fatalf("预置账户失败: %v", err)
		}
	}

	c := batchAuthzContext(t, u.ID)
	if used, _ := QuotaUsageOf(c, "cert_accounts"); used != 2 {
		t.Errorf("证书账户与部署账户应共用这一维度，共 2 个，实际 %d", used)
	}
}

/* TestCertAccountsDimensionDoesNotCountDNSAccounts 该维度不得统计 DNS 账户 */
func TestCertAccountsDimensionDoesNotCountDNSAccounts(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaPrefixUser(t, "cert-vs-dns", `{"max_cert_accounts":5,"max_accounts":5}`)

	// 造 3 个 DNS 账户
	for _, name := range []string{"dns1", "dns2", "dns3"} {
		if err := database.DB.Create(&models.Account{
			UserID: u.ID, Type: "cloudflare", Name: name,
		}).Error; err != nil {
			t.Fatalf("预置 DNS 账户失败: %v", err)
		}
	}

	c := batchAuthzContext(t, u.ID)
	if used, _ := QuotaUsageOf(c, "cert_accounts"); used != 0 {
		t.Errorf("DNS 账户不应计入证书账户维度（这正是修复前的错配），实际 %d", used)
	}
	if used, _ := QuotaUsageOf(c, "accounts"); used != 3 {
		t.Errorf("DNS 账户应为 3，实际 %d", used)
	}
}

/* TestCertAccountsQuotaBlocksAtLimit 达上限时拒绝 */
func TestCertAccountsQuotaBlocksAtLimit(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaPrefixUser(t, "cert-limit", `{"max_cert_accounts":1}`)

	if err := database.DB.Create(&models.CertAccount{
		UserID: u.ID, Type: "letsencrypt", Name: "only-one",
	}).Error; err != nil {
		t.Fatalf("预置失败: %v", err)
	}

	c := batchAuthzContext(t, u.ID)
	if QuotaAllows(c, "cert_accounts") {
		t.Error("已有 1 个而限额为 1，应拒绝")
	}
}

/* TestCertAccountsQuotaAllowsWhenUnlimited -1 表示不限 */
func TestCertAccountsQuotaAllowsWhenUnlimited(t *testing.T) {
	setupHandlerTestDB(t)
	u := quotaPrefixUser(t, "cert-unlimited", `{"max_cert_accounts":-1}`)

	for i := 0; i < 5; i++ {
		if err := database.DB.Create(&models.CertAccount{
			UserID: u.ID, Type: "letsencrypt", Name: "acc" + string(rune('a'+i)),
		}).Error; err != nil {
			t.Fatalf("预置失败: %v", err)
		}
	}

	c := batchAuthzContext(t, u.ID)
	if !QuotaAllows(c, "cert_accounts") {
		t.Error("-1 表示不限，应放行")
	}
}
