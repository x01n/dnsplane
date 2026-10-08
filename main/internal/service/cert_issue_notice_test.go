package service

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"main/internal/cache"
	"main/internal/cert"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 证书签发通知构建测试
 */

/* setupCertNoticeTest 初始化内存 SQLite 与内存缓存，并返回时间基准 */
func setupCertNoticeTest(t *testing.T) time.Time {
	t.Helper()
	dir := t.TempDir()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(dir, "test.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
	return time.Now()
}

/* setCertNoticeConfig 写入 sysconfig 并校验读回 */
func setCertNoticeConfig(t *testing.T, key, value string) {
	t.Helper()
	var cfg models.SysConfig
	if err := database.DB.Where("`key` = ?", key).First(&cfg).Error; err != nil {
		if err := database.DB.Create(&models.SysConfig{Key: key, Value: value}).Error; err != nil {
			t.Fatalf("写入配置 %s 失败: %v", key, err)
		}
	} else if err := database.DB.Model(&cfg).Update("value", value).Error; err != nil {
		t.Fatalf("更新配置 %s 失败: %v", key, err)
	}
	sysconfig.Invalidate(key)
	if got := sysconfig.GetValue(key); got != value {
		t.Fatalf("配置 %s 读回不符: got=%q want=%q", key, got, value)
	}
}

func TestCertOrderNoticeTitle(t *testing.T) {
	cases := []struct {
		domains []string
		suffix  string
		want    string
	}{
		{[]string{"a.com"}, "SSL证书签发成功通知", "a.com域名SSL证书签发成功通知"},
		{[]string{"a.com", "b.com"}, "SSL证书签发成功通知", "a.com等2个域名SSL证书签发成功通知"},
		{[]string{"a.com", "b.com", "c.com"}, "SSL证书失败通知", "a.com等3个域名SSL证书失败通知"},
		{nil, "SSL证书签发成功通知", ""},
		{[]string{}, "SSL证书签发成功通知", ""},
	}
	for _, c := range cases {
		if got := certOrderNoticeTitle(c.domains, c.suffix); got != c.want {
			t.Errorf("domains=%v suffix=%q got=%q want=%q", c.domains, c.suffix, got, c.want)
		}
	}
}

func TestCertIssueStatusName(t *testing.T) {
	cases := []struct {
		status int
		want   string
	}{
		{0, "失败"},
		{-1, "购买证书失败"},
		{-2, "创建订单失败"},
		{-3, "添加DNS失败"},
		{-4, "验证DNS失败"},
		{-5, "验证订单失败"},
		{-6, "订单验证未通过"},
		{-7, "签发证书失败"},
		{-8, "处理失败"},
		{1, "处理失败"},
	}
	for _, c := range cases {
		if got := certIssueStatusName(c.status); got != c.want {
			t.Errorf("status=%d got=%q want=%q", c.status, got, c.want)
		}
	}
}

func TestBuildCertIssueSuccessMessage(t *testing.T) {
	setupCertNoticeTest(t)
	setCertNoticeConfig(t, "site_name", "测试站点")

	issue := time.Date(2026, 1, 2, 3, 4, 5, 0, time.Local)
	expire := time.Date(2026, 4, 2, 3, 4, 5, 0, time.Local)
	order := &models.CertOrder{
		ID: 12, AccountID: 3, Status: 3,
		IssueTime: &issue, ExpireTime: &expire,
		Issuer: "Let's Encrypt",
	}
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)

	title, content := buildCertIssueSuccessMessage(order, []string{"a.com"}, "Let's Encrypt", now)

	if title != "a.com域名SSL证书签发成功通知" {
		t.Errorf("标题不符: %q", title)
	}
	for _, want := range []string{
		"您的SSL证书已签发成功！",
		"证书账户： Let's Encrypt(3)",
		"证书域名： a.com",
		"签发时间： 2026-01-02 03:04:05",
		"到期时间： 2026-04-02 03:04:05",
		"颁发机构： Let's Encrypt",
		"测试站点",
		"2026-10-05 12:00:00",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("正文缺少 %q\n正文: %s", want, content)
		}
	}

	// 多域名应以「、」连接
	_, content = buildCertIssueSuccessMessage(order, []string{"a.com", "b.com"}, "Let's Encrypt", now)
	if !strings.Contains(content, "证书域名： a.com、b.com") {
		t.Errorf("多域名连接不符: %s", content)
	}

	// 时间缺失时留空而非 panic
	empty := &models.CertOrder{ID: 1, AccountID: 1, Status: 3}
	_, content = buildCertIssueSuccessMessage(empty, []string{"a.com"}, "未知", now)
	if !strings.Contains(content, "签发时间： \n") || !strings.Contains(content, "到期时间： \n") {
		t.Errorf("时间缺失时应留空: %s", content)
	}
}

func TestBuildCertIssueFailMessage(t *testing.T) {
	setupCertNoticeTest(t)
	setCertNoticeConfig(t, "site_name", "测试站点")

	now := time.Date(2026, 10, 5, 12, 0, 0, 0, time.Local)
	order := &models.CertOrder{
		ID: 12, AccountID: 3, Status: -5,
		Error: "验证订单失败: timeout",
	}

	title, content := buildCertIssueFailMessage(order, []string{"a.com", "b.com"}, "Let's Encrypt", now)

	if title != "a.com等2个域名SSL证书验证订单失败通知" {
		t.Errorf("标题不符: %q", title)
	}
	for _, want := range []string{
		"您的SSL证书验证订单失败！",
		"证书账户： Let's Encrypt(3)",
		"证书域名： a.com、b.com",
		"失败时间： 2026-10-05 12:00:00",
		"失败原因： 验证订单失败: timeout",
		"测试站点",
	} {
		if !strings.Contains(content, want) {
			t.Errorf("正文缺少 %q\n正文: %s", want, content)
		}
	}
}

func TestCertAccountTypeName(t *testing.T) {
	// cert 注册表命中的类型应取注册时的显示名（测试中现场注册，避免依赖具体子包是否被导入）
	const testType = "__test_cert_type__"
	const testName = "测试证书账户"
	cert.Register(testType, nil, cert.ProviderConfig{Type: testType, Name: testName})
	if got := certAccountTypeName(testType); got != testName {
		t.Errorf("注册表命中应取显示名，实际 %q", got)
	}
	// 注册了类型但未给显示名时回退为原始类型
	const bareType = "__test_bare_type__"
	cert.Register(bareType, nil, cert.ProviderConfig{Type: bareType})
	if got := certAccountTypeName(bareType); got != bareType {
		t.Errorf("无显示名应回退为原始类型，实际 %q", got)
	}
	// 未登记的类型回退为原始类型
	if got := certAccountTypeName("__not_registered__"); got != "__not_registered__" {
		t.Errorf("未登记类型应回退为原始值，实际 %q", got)
	}
	// 空类型回退为「未知」
	if got := certAccountTypeName(""); got != "未知" {
		t.Errorf("空类型应为「未知」，实际 %q", got)
	}
}

/* TestCertNoticeSiteName site_name 未配置时回退为 DNSPlane */
func TestCertNoticeSiteName(t *testing.T) {
	setupCertNoticeTest(t)
	setCertNoticeConfig(t, "site_name", "")
	if got := certNoticeSiteName(); got != "DNSPlane" {
		t.Errorf("未配置 site_name 应回退为 DNSPlane，实际 %q", got)
	}
	setCertNoticeConfig(t, "site_name", "我的站点")
	if got := certNoticeSiteName(); got != "我的站点" {
		t.Errorf("site_name 不符: %q", got)
	}
}

/* TestSendCertIssueNotificationSwitches 覆盖两个通知开关的默认值与关闭行为 */
func TestSendCertIssueNotificationSwitches(t *testing.T) {
	setupCertNoticeTest(t)

	// 成功通知默认关闭：显式开启才发送
	if sysconfigBoolExplicitOn("cert_issue_success_notice_enabled") {
		t.Error("证书签发成功通知未配置时应为关闭")
	}
	setCertNoticeConfig(t, "cert_issue_success_notice_enabled", "1")
	if !sysconfigBoolExplicitOn("cert_issue_success_notice_enabled") {
		t.Error("显式开启后应为开启")
	}

	// 失败通知默认开启，显式 false 才关闭
	if !sysconfigBoolDefaultTrue("cert_issue_notice_enabled") {
		t.Error("证书签发失败通知未配置时应为开启")
	}
	setCertNoticeConfig(t, "cert_issue_notice_enabled", "false")
	if sysconfigBoolDefaultTrue("cert_issue_notice_enabled") {
		t.Error("显式 false 后应关闭")
	}
}

/* TestSendCertIssueNotificationNoOrder 订单不存在时应静默返回，不 panic */
func TestSendCertIssueNotificationNoOrder(t *testing.T) {
	setupCertNoticeTest(t)
	setCertNoticeConfig(t, "cert_issue_success_notice_enabled", "1")
	setCertNoticeConfig(t, "cert_issue_notice_enabled", "true")

	SendCertIssueSuccessNotification(999999)
	SendCertIssueFailNotification(999999)
}

/* TestSendCertIssueNotificationNoDomains 订单无关联域名时应静默返回 */
func TestSendCertIssueNotificationNoDomains(t *testing.T) {
	setupCertNoticeTest(t)
	setCertNoticeConfig(t, "cert_issue_success_notice_enabled", "1")
	setCertNoticeConfig(t, "cert_issue_notice_enabled", "true")

	order := models.CertOrder{AccountID: 1, Status: 3}
	if err := database.DB.Create(&order).Error; err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	SendCertIssueSuccessNotification(order.ID)
	SendCertIssueFailNotification(order.ID)

	var count int64
	database.DB.Model(&models.CertDomain{}).Where("oid = ?", order.ID).Count(&count)
	if count != 0 {
		t.Errorf("不应写入域名记录，实际 %d", count)
	}
}

/* TestCertOrderDomainList 域名应按 sort 升序返回 */
func TestCertOrderDomainList(t *testing.T) {
	setupCertNoticeTest(t)
	order := models.CertOrder{AccountID: 1, Status: 3}
	if err := database.DB.Create(&order).Error; err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	for _, d := range []models.CertDomain{
		{OrderID: order.ID, Domain: "second.com", Sort: 1},
		{OrderID: order.ID, Domain: "first.com", Sort: 0},
		{OrderID: order.ID, Domain: "third.com", Sort: 2},
	} {
		if err := database.DB.Create(&d).Error; err != nil {
			t.Fatalf("创建域名失败: %v", err)
		}
	}
	got := certOrderDomainList(order.ID)
	want := []string{"first.com", "second.com", "third.com"}
	if len(got) != len(want) {
		t.Fatalf("域名数量不符: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("第 %d 个域名不符: got=%v want=%v", i, got, want)
			break
		}
	}
}
