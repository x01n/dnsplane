package handler

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/*
 * 批量记录接口的子域归属校验
 *
 * 单条接口（CreateRecord / UpdateRecord）在建写前一律走 CheckSubDomainPermission，
 * 批量族只走 CheckDomainPermission——域级校验回答得了「这个域名归不归你管」，
 * 回答不了「这条记录归不归你管」。结果是收到 rr 级委派的用户可以把记录
 * 增删改到该域名下任意子域。
 *
 * 本文件锁住三件事：
 *   域级授权不足以写未授权子域
 *   授权范围内的子域不能被误拒（否则是把越权修成了不可用）
 *   上游取不到记录时必须判「无法判定」并拒绝，不能放行
 */

/* fakeRecordProvider 只实现批量校验所需方法的 Provider 桩 */
type fakeRecordProvider struct {
	dns.Provider
	records []dns.Record
	err     error
}

func (f *fakeRecordProvider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	// 只有第一页有数据，让分页聚合能在第二页正确收尾
	if page > 1 {
		return &dns.PageResult{Records: []dns.Record{}}, nil
	}
	return &dns.PageResult{Records: f.records}, nil
}

func (f *fakeRecordProvider) GetError() string { return "" }

var errFakeUpstream = errors.New("上游不可达")

/* seedSubDelegatedUser 造一个持有 rr 级委派的普通用户与该域名 */
func seedSubDelegatedUser(t *testing.T, grantedRR string) (models.User, models.Domain) {
	t.Helper()

	owner := models.User{Username: "sub-owner", Email: "sub-owner@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&owner).Error; err != nil {
		t.Fatalf("创建域主失败: %v", err)
	}
	delegate := models.User{Username: "sub-delegate", Email: "sub-delegate@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&delegate).Error; err != nil {
		t.Fatalf("创建被授权用户失败: %v", err)
	}

	account := models.Account{UserID: owner.ID, Type: "cloudflare", Name: "sub-acc"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: "sub.example.com", ThirdID: "zone-sub"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建域名失败: %v", err)
	}
	perm := models.Permission{UserID: delegate.ID, Domain: domain.Name, DomainID: domain.ID, SubDomain: grantedRR}
	if err := database.DB.Create(&perm).Error; err != nil {
		t.Fatalf("创建委派权限失败: %v", err)
	}
	return delegate, domain
}

/* batchAuthzContext 构造批量校验所需的上下文 */
func batchAuthzContext(t *testing.T, uid uint) *gin.Context {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodPost, "/", strings.NewReader("{}"))
	c.Set("user_id", itoaUint(uid))
	c.Set("level", 1)
	return c
}

/* TestBatchSubDomainScopeRejectsWriteOutsideGrant 只授权 www 时，写入 blog 必须被拒 */
func TestBatchSubDomainScopeRejectsWriteOutsideGrant(t *testing.T) {
	setupCertTestDB(t)

	delegate, domain := seedSubDelegatedUser(t, "www")
	c := batchAuthzContext(t, delegate.ID)

	if checkBatchRecordSubDomain(c, &domain, itoaUint(delegate.ID), 1,
		batchRecordAuthReq{Targets: []string{"www", "blog"}}) {
		t.Error("仅授权 www 的用户往 blog 写入，应被拒绝")
	}
}

/* TestBatchSubDomainScopeAllowsWriteInsideGrant 授权内的子域必须放行 */
func TestBatchSubDomainScopeAllowsWriteInsideGrant(t *testing.T) {
	setupCertTestDB(t)

	delegate, domain := seedSubDelegatedUser(t, "www")
	c := batchAuthzContext(t, delegate.ID)

	if !checkBatchRecordSubDomain(c, &domain, itoaUint(delegate.ID), 1,
		batchRecordAuthReq{Targets: []string{"www"}}) {
		t.Error("授权内的子域被误拒，等于把越权修成了不可用")
	}
}

/* TestBatchSubDomainScopeRejectsStranger 无任何授权的用户必须被拒 */
func TestBatchSubDomainScopeRejectsStranger(t *testing.T) {
	setupCertTestDB(t)

	_, domain := seedSubDelegatedUser(t, "www")
	stranger := models.User{Username: "sub-stranger", Email: "sub-stranger@example.com", Level: 1, Status: 1}
	if err := database.DB.Create(&stranger).Error; err != nil {
		t.Fatalf("创建陌生用户失败: %v", err)
	}

	c := batchAuthzContext(t, stranger.ID)
	if checkBatchRecordSubDomain(c, &domain, itoaUint(stranger.ID), 1,
		batchRecordAuthReq{Targets: []string{"www"}}) {
		t.Error("无授权的用户即使目标落在别人的授权范围内，也必须被拒")
	}
}

/*
 * TestBatchSubDomainScopeRejectsForeignRecordID 批量操作里混入不属于本域名的 record_id 必须被拒
 *
 * 判定依据取自云端记录的真实 rr，而不是请求里的字段：请求里的 record_id 是
 * 攻击者可控的，按请求字段反查 rr 等于让攻击者自证清白。这里直接对一个
 * 明确不在授权范围内的 record_id 走一遍判定链
 */
func TestBatchSubDomainScopeRejectsForeignRecordID(t *testing.T) {
	setupCertTestDB(t)

	delegate, domain := seedSubDelegatedUser(t, "www")
	c := batchAuthzContext(t, delegate.ID)

	// 该域名下只有 blog 一条记录，而调用方只被授权 www
	idx := cloudRecordRRIndex(context.Background(), &fakeRecordProvider{records: []dns.Record{
		{ID: "r-foreign", Name: "blog", Type: "A", Value: "192.0.2.9"},
	}})
	if idx == nil {
		t.Fatal("索引构建失败，后续判定无从谈起")
	}
	rr, known := idx["r-foreign"]
	if !known {
		t.Fatal("索引应包含云端返回的记录")
	}
	if middleware.CheckSubDomainPermission(itoaUint(delegate.ID), 1, itoaUint(domain.ID), rr) {
		t.Error("blog 不在授权范围内，子域判定不应通过")
	}

	// 同一套判据下，授权内的记录必须通过，确认拒绝来自子域而非「一律拒绝」
	idx2 := cloudRecordRRIndex(context.Background(), &fakeRecordProvider{records: []dns.Record{
		{ID: "r-own", Name: "www", Type: "A", Value: "192.0.2.10"},
	}})
	if !middleware.CheckSubDomainPermission(itoaUint(delegate.ID), 1, itoaUint(domain.ID), idx2["r-own"]) {
		t.Error("www 在授权范围内，子域判定应通过")
	}
	_ = c
}

/* TestCloudRecordRRIndexReturnsNilOnError 上游取不到记录时索引为 nil，供调用方判为「无法判定」 */
func TestCloudRecordRRIndexReturnsNilOnError(t *testing.T) {
	provider := &fakeRecordProvider{err: errFakeUpstream}

	if idx := cloudRecordRRIndex(context.Background(), provider); idx != nil {
		t.Errorf("上游失败时应返回 nil 让调用方拒绝，实际返回 %v", idx)
	}
}

/* TestCloudRecordRRIndexIndexesByRecordID 索引的键是 record_id、值是真实 rr */
func TestCloudRecordRRIndexIndexesByRecordID(t *testing.T) {
	provider := &fakeRecordProvider{records: []dns.Record{
		{ID: "r1", Name: "www", Type: "A", Value: "192.0.2.1"},
		{ID: "r2", Name: "blog", Type: "A", Value: "192.0.2.2"},
	}}

	idx := cloudRecordRRIndex(context.Background(), provider)
	if idx == nil {
		t.Fatal("正常返回时索引不应为 nil")
	}
	if idx["r1"] != "www" || idx["r2"] != "blog" {
		t.Errorf("索引内容与云端记录不符：%v", idx)
	}
}
