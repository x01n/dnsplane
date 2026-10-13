package apiv1

import (
	"testing"

	"main/internal/database"
	"main/internal/models"
)

/*
 * 配额按操作者各自计算
 *
 * 每个用户各自持一份额度，互不牵连：
 *   A 的额度用满不影响 B 能建多少
 *   B 在 A 授权给他的域名下建资源，计 B 自己的额度
 *
 * 这条语义决定了路由层配额门的取舍：v1 复用的控制台 handler 内部已有
 * CheckQuota，且它同样以 currentUIDUint(c)（操作者）判定，
 * 因此只有「handler 内没有配额门」的接口才需要在路由层补挂
 */

/* TestQuotaIsPerOperator 一个用户的额度用满不影响另一个用户 */
func TestQuotaIsPerOperator(t *testing.T) {
	setupBodyTestDB(t)
	r := quotaGateEngine()

	// A 的额度禁止创建
	ua, keyA := seedQuotaUser(t, "operator-a", `{"max_domains":0}`)
	gotA := callQuotaGate(t, r, ua.ID, keyA, "/api/v1/domains/create")
	if gotA.Code == 0 {
		t.Errorf("A 的 max_domains=0 应拒绝创建，实际放行")
	}

	// B 的额度充裕，不应被 A 的状态牵连
	ub, keyB := seedQuotaUser(t, "operator-b", `{"max_domains":10}`)
	gotB := callQuotaGate(t, r, ub.ID, keyB, "/api/v1/domains/create")
	if gotB.Code != 0 {
		t.Errorf("B 的额度独立于 A，不应被牵连，实际 code=%d msg=%q", gotB.Code, gotB.Message)
	}
}

/*
 * TestQuotaUsedCountsOnlyOwnResources records 维度的用量只算自有账户下的资源
 *
 * 统计口径是 `accounts.uid = 操作者`，所以没有自有域名的用户用量恒为 0——
 * 即使别的用户把域名授权给他，那些域名下的记录也不计入他的用量
 */
func TestQuotaUsedCountsOnlyOwnResources(t *testing.T) {
	setupBodyTestDB(t)

	// 甲：自有账户 + 域名 + 1 条本地记录
	owner, _ := seedQuotaUser(t, "quota-owner", `{"max_records":99}`)
	ownerDomain := seedOwnedDomain(t, owner.ID, "owner.example.com")
	if err := database.DB.Create(&models.DNSRecord{
		DomainID: ownerDomain.ID, RecordID: "r1", Name: "www", Type: "A",
		Value: "192.0.2.1", Status: "enable", Domain: ownerDomain.Name,
	}).Error; err != nil {
		t.Fatalf("预置记录失败: %v", err)
	}

	// 乙：无自有域名。限额设为 0 也不应因甲的资源而被计为超限之外的任何数
	other, keyOther := seedQuotaUser(t, "quota-other", `{"max_records":0}`)
	r := quotaGateEngine()

	// 乙的用量为 0、限额为 0 → 判定为「无余量」，应被拒
	got := callQuotaGate(t, r, other.ID, keyOther, "/api/v1/records/create")
	if got.Code != CodeResourceLocked {
		t.Errorf("乙的限额为 0 时应被拒（期望 %d），实际 code=%d msg=%q",
			CodeResourceLocked, got.Code, got.Message)
	}

	// 给丙一个正数限额：若他的用量被错算成甲的 1 条，限额 1 会刚好卡住
	other2, keyOther2 := seedQuotaUser(t, "quota-other2", `{"max_records":1}`)
	got2 := callQuotaGate(t, r, other2.ID, keyOther2, "/api/v1/records/create")
	if got2.Code == CodeResourceLocked {
		t.Errorf("无自有记录的用户用量应为 0，限额 1 不应被拒：%q", got2.Message)
	}
}
