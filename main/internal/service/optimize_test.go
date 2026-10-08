package service

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
	"main/internal/sysconfig"
)

/* fakeProvider 仅实现优选逻辑用到的记录读写，其余接口方法返回零值，避免测试触网 */
type fakeProvider struct {
	records []dns.Record
	nextID  int
}

func (f *fakeProvider) GetError() string { return "" }

func (f *fakeProvider) Check(ctx context.Context) error { return nil }

func (f *fakeProvider) GetDomainList(ctx context.Context, keyword string, page, pageSize int) (*dns.PageResult, error) {
	return nil, nil
}

func (f *fakeProvider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	return nil, nil
}

func (f *fakeProvider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	return &dns.PageResult{Total: len(f.records), Records: f.records}, nil
}

func (f *fakeProvider) GetDomainRecordInfo(ctx context.Context, recordID string) (*dns.Record, error) {
	return nil, nil
}

func (f *fakeProvider) AddDomainRecord(ctx context.Context, name, recordType, value, line string, ttl, mx int, weight *int, remark string) (string, error) {
	f.nextID++
	rec := dns.Record{ID: fmt.Sprintf("new-%d", f.nextID), Name: name, Type: recordType, Value: value, Line: line, TTL: ttl}
	f.records = append(f.records, rec)
	return rec.ID, nil
}

func (f *fakeProvider) UpdateDomainRecord(ctx context.Context, recordID, name, recordType, value, line string, ttl, mx int, weight *int, remark string) error {
	for i := range f.records {
		if f.records[i].ID == recordID {
			f.records[i].Value = value
			f.records[i].Type = recordType
			f.records[i].Line = line
			f.records[i].TTL = ttl
			return nil
		}
	}
	return fmt.Errorf("记录 %s 不存在", recordID)
}

func (f *fakeProvider) UpdateDomainRecordRemark(ctx context.Context, recordID, remark string) error {
	return nil
}

func (f *fakeProvider) DeleteDomainRecord(ctx context.Context, recordID string) error {
	for i := range f.records {
		if f.records[i].ID == recordID {
			f.records = append(f.records[:i], f.records[i+1:]...)
			return nil
		}
	}
	return fmt.Errorf("记录 %s 不存在", recordID)
}

func (f *fakeProvider) SetDomainRecordStatus(ctx context.Context, recordID string, enable bool) error {
	return nil
}

func (f *fakeProvider) GetDomainRecordLog(ctx context.Context, page, pageSize int, keyword, startDate, endDate string) (*dns.PageResult, error) {
	return nil, nil
}

func (f *fakeProvider) GetRecordLine(ctx context.Context) ([]dns.RecordLine, error) { return nil, nil }

func (f *fakeProvider) GetMinTTL() int { return 600 }

func (f *fakeProvider) AddDomain(ctx context.Context, domain string) error { return nil }

/* values 返回当前记录值列表，便于断言 */
func (f *fakeProvider) values() []string {
	out := make([]string, 0, len(f.records))
	for _, r := range f.records {
		out = append(out, r.Value)
	}
	return out
}

/*
 * pagingProvider 每页都返回满页且 Total 恒为 0，模拟 provider 分页数据与 Total 不一致的情况
 * 用于验证 listOptimizeRecords 的页数上限是真终止条件
 */
type pagingProvider struct {
	fakeProvider
	pages int
}

func (p *pagingProvider) GetSubDomainRecords(ctx context.Context, subDomain string, page, pageSize int, recordType, line string) (*dns.PageResult, error) {
	p.pages = page
	records := make([]dns.Record, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		records = append(records, dns.Record{
			ID:   fmt.Sprintf("p%d-%d", page, i),
			Type: "A", Value: fmt.Sprintf("10.0.%d.%d", page, i), Line: "telecom",
		})
	}
	return &dns.PageResult{Total: 0, Records: records}, nil
}

/* setupOptimizeTestDB 为依赖 sysconfig 的用例准备临时 sqlite 库与内存缓存 */
func setupOptimizeTestDB(t *testing.T) {
	t.Helper()
	cache.Init(&cache.Config{}) // 未启用 Redis 时使用内存缓存
	cfg := &config.Config{}
	cfg.Database.Driver = "sqlite"
	cfg.Database.FilePath = t.TempDir() + "/optimize.db"
	if err := database.Init(&cfg.Database); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	t.Cleanup(func() {
		database.Close()
		cache.Close()
	})
}

func TestWetestOptimizePath(t *testing.T) {
	cases := map[int]string{
		1: "get_cloudflare_ip",
		2: "get_cloudfront_ip",
		3: "get_gcore_ip",
		4: "get_edgeone_ip",
		0: "",
		9: "",
	}
	for cdnType, want := range cases {
		if got := wetestOptimizePath(cdnType); got != want {
			t.Errorf("wetestOptimizePath(%d)=%q, 期望 %q", cdnType, got, want)
		}
	}
}

func TestNormalizeOptimizeInfo(t *testing.T) {
	info := map[string]interface{}{
		"DEF": []interface{}{map[string]interface{}{"ip": "1.1.1.1"}},
		"CT":  []interface{}{map[string]interface{}{"ip": "2.2.2.2"}, map[string]interface{}{"ip": "3.3.3.3"}},
		"CM":  []interface{}{map[string]interface{}{"ip": "4.4.4.4"}},
		// CU 缺失；额外未知线路应被忽略
		"AB": []interface{}{map[string]interface{}{"ip": "5.5.5.5"}},
	}
	got := normalizeOptimizeInfo(info)

	if len(got["CT"]) != 2 || got["CT"][0] != "2.2.2.2" || got["CT"][1] != "3.3.3.3" {
		t.Errorf("CT 线路解析异常: %v", got["CT"])
	}
	if len(got["DEF"]) != 1 || got["DEF"][0] != "1.1.1.1" {
		t.Errorf("DEF 线路解析异常: %v", got["DEF"])
	}
	if _, ok := got["CU"]; ok {
		t.Errorf("CU 线路缺失时不应写入结果: %v", got["CU"])
	}
	if _, ok := got["AB"]; ok {
		t.Errorf("未知线路 AB 不应出现在结果中")
	}
}

func TestNormalizeOptimizeInfoInvalidShape(t *testing.T) {
	if got := normalizeOptimizeInfo("not-an-object"); len(got) != 0 {
		t.Errorf("非法 info 应返回空映射，实际 %v", got)
	}
	if got := normalizeOptimizeInfo(nil); len(got) != 0 {
		t.Errorf("nil info 应返回空映射，实际 %v", got)
	}
}

func TestProcessDNSLineAddsUpToRecordNum(t *testing.T) {
	provider := &fakeProvider{}
	executor := NewOptimizeExecutor()
	task := &models.OptimizeIP{RR: "www", TTL: 600}

	ops, err := executor.processDNSLine(context.Background(), provider, task, nil, 2,
		[]string{"1.1.1.1", "2.2.2.2", "3.3.3.3"}, "telecom", "A")
	if err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if executor.result.Added != 2 || executor.result.Changed != 0 || executor.result.Deleted != 0 {
		t.Fatalf("统计异常: %+v", executor.result)
	}
	if ops != 2 {
		t.Errorf("操作数=%d, 期望 2", ops)
	}
	if len(provider.records) != 2 {
		t.Errorf("实际写入 %d 条记录, 期望 2 条", len(provider.records))
	}
	for _, r := range provider.records {
		if r.Line != "telecom" || r.Type != "A" || r.TTL != 600 {
			t.Errorf("记录字段异常: %+v", r)
		}
	}
}

func TestProcessDNSLineNoChangeWhenIPsMatch(t *testing.T) {
	provider := &fakeProvider{records: []dns.Record{
		{ID: "r1", Type: "A", Value: "1.1.1.1", Line: "telecom"},
		{ID: "r2", Type: "A", Value: "2.2.2.2", Line: "telecom"},
	}}
	executor := NewOptimizeExecutor()
	task := &models.OptimizeIP{RR: "www", TTL: 600}

	if _, err := executor.processDNSLine(context.Background(), provider, task, provider.records, 2,
		[]string{"1.1.1.1", "2.2.2.2"}, "telecom", "A"); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if executor.result.Added != 0 || executor.result.Changed != 0 || executor.result.Deleted != 0 {
		t.Errorf("IP 完全一致时不应产生变更: %+v", executor.result)
	}
	if len(provider.records) != 2 {
		t.Errorf("记录数被改动: %d", len(provider.records))
	}
}

func TestProcessDNSLineRewritesThenDeletes(t *testing.T) {
	provider := &fakeProvider{records: []dns.Record{
		{ID: "r1", Type: "A", Value: "1.1.1.1", Line: "telecom"},
		{ID: "r2", Type: "A", Value: "9.9.9.9", Line: "telecom"},
		{ID: "r3", Type: "A", Value: "8.8.8.8", Line: "telecom"},
	}}
	executor := NewOptimizeExecutor()
	task := &models.OptimizeIP{RR: "www", TTL: 600}

	// 目标 2 条，既有 3 条且其中 2 条不在目标内：一条改写成待增 IP，一条删除
	if _, err := executor.processDNSLine(context.Background(), provider, task, provider.records, 2,
		[]string{"1.1.1.1", "2.2.2.2"}, "telecom", "A"); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	if executor.result.Changed != 1 || executor.result.Deleted != 1 || executor.result.Added != 0 {
		t.Fatalf("统计异常: %+v", executor.result)
	}
	values := provider.values()
	if len(values) != 2 {
		t.Fatalf("记录数=%d, 期望 2: %v", len(values), values)
	}
	found := map[string]bool{}
	for _, v := range values {
		found[v] = true
	}
	if !found["1.1.1.1"] || !found["2.2.2.2"] {
		t.Errorf("最终记录值异常: %v", values)
	}
}

func TestProcessDNSLineDeletesCNAMEOnSameLine(t *testing.T) {
	provider := &fakeProvider{records: []dns.Record{
		{ID: "c1", Type: "CNAME", Value: "example.cf.com", Line: "telecom"},
		{ID: "a1", Type: "A", Value: "1.1.1.1", Line: "telecom"},
		// 其它线路的记录不应被触碰
		{ID: "a2", Type: "A", Value: "7.7.7.7", Line: "unicom"},
	}}
	executor := NewOptimizeExecutor()
	task := &models.OptimizeIP{RR: "www", TTL: 600}

	if _, err := executor.processDNSLine(context.Background(), provider, task, provider.records, 1,
		[]string{"1.1.1.1"}, "telecom", "A"); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	for _, r := range provider.records {
		if r.Type == "CNAME" {
			t.Errorf("同线路 CNAME 未被删除: %+v", r)
		}
	}
	if len(provider.records) != 2 {
		t.Errorf("记录数=%d, 期望保留 2 条（其它线路不受影响）", len(provider.records))
	}
}

func TestProcessDNSLineSkipsOtherIPType(t *testing.T) {
	provider := &fakeProvider{records: []dns.Record{
		{ID: "a1", Type: "A", Value: "1.1.1.1", Line: "telecom"},
	}}
	executor := NewOptimizeExecutor()
	task := &models.OptimizeIP{RR: "www", TTL: 600}

	// 处理 AAAA 时不应动到同线路的 A 记录
	if _, err := executor.processDNSLine(context.Background(), provider, task, provider.records, 1,
		[]string{"2001:db8::1"}, "telecom", "AAAA"); err != nil {
		t.Fatalf("执行失败: %v", err)
	}
	keptA := false
	for _, r := range provider.records {
		if r.Type == "A" && r.Value == "1.1.1.1" {
			keptA = true
		}
	}
	if !keptA {
		t.Errorf("处理 AAAA 时误删了 A 记录: %+v", provider.records)
	}
	if executor.result.Added != 1 {
		t.Errorf("应新增 1 条 AAAA: %+v", executor.result)
	}
}

func TestStringSetDiffAndPopLast(t *testing.T) {
	diff := stringSetDiff([]string{"a", "b", "c"}, []string{"b"})
	if len(diff) != 2 {
		t.Fatalf("差集大小=%d, 期望 2", len(diff))
	}
	if _, ok := diff["b"]; ok {
		t.Errorf("b 不应出现在差集中")
	}
	first := popLast(diff)
	if first == "" {
		t.Errorf("popLast 应返回非空元素")
	}
	if len(diff) != 1 {
		t.Errorf("popLast 应移除元素, 剩余 %d", len(diff))
	}
	if popLast(map[string]struct{}{}) != "" {
		t.Errorf("空集合 popLast 应返回空串")
	}
}

func TestOptimizeExecutorRejectsEmptyIPType(t *testing.T) {
	executor := NewOptimizeExecutor()
	if _, err := executor.Run(context.Background(), &models.OptimizeIP{IPType: " , "}); err == nil {
		t.Fatal("空的 ip_type 应当报错")
	}
}

/* TestListOptimizeRecordsPagingLimit Total 恒为 0 且每页满页时必须停在页数上限，不能无限循环 */
func TestListOptimizeRecordsPagingLimit(t *testing.T) {
	provider := &pagingProvider{}
	records, err := listOptimizeRecords(context.Background(), provider, "www")
	if err != nil {
		t.Fatalf("取记录失败: %v", err)
	}
	if provider.pages != optimizeRecordMaxPage {
		t.Errorf("请求页数=%d, 期望停在页数上限 %d", provider.pages, optimizeRecordMaxPage)
	}
	if len(records) != optimizeRecordMaxPage*optimizeRecordPageSize {
		t.Errorf("累计记录数=%d, 期望 %d", len(records), optimizeRecordMaxPage*optimizeRecordPageSize)
	}
}

/* TestListOptimizeRecordsStopsAtTotal Total 小于单页容量时应只请求一次 */
func TestListOptimizeRecordsStopsAtTotal(t *testing.T) {
	provider := &fakeProvider{records: []dns.Record{
		{ID: "r1", Type: "A", Value: "1.1.1.1", Line: "telecom"},
		{ID: "r2", Type: "A", Value: "2.2.2.2", Line: "telecom"},
	}}
	records, err := listOptimizeRecords(context.Background(), provider, "www")
	if err != nil {
		t.Fatalf("取记录失败: %v", err)
	}
	if len(records) != 2 {
		t.Errorf("记录数=%d, 期望 2", len(records))
	}
}

func TestGetOptimizeLicenseXingpingcnRequiresNoKey(t *testing.T) {
	setupOptimizeTestDB(t)

	_, err := GetOptimizeLicense(context.Background(), OptimizeAPIXingpingcn, "any")
	if err == nil || !strings.Contains(err.Error(), "无需密钥") {
		t.Fatalf("期望返回无需密钥提示, 实际: %v", err)
	}
}

func TestGetOptimizeIPDataXingpingcnRejectsIPv6(t *testing.T) {
	setupOptimizeTestDB(t)

	// 需先切到 xingpingcn 数据源，否则默认走 wetest 分支
	sysconfig.SetValue(OptimizeCfgAPI, "2")

	_, err := GetOptimizeIPData(context.Background(), 1, "v6")
	if err == nil || !strings.Contains(err.Error(), "暂不支持IPv6") {
		t.Fatalf("期望返回不支持 IPv6, 实际: %v", err)
	}
}

func TestGetOptimizeIPDataXingpingcnRejectsPrivateProxy(t *testing.T) {
	setupOptimizeTestDB(t)

	sysconfig.SetValue(OptimizeCfgAPI, "2")
	sysconfig.SetValue(OptimizeCfgProxy, "http://192.168.1.10:8080")

	_, err := GetOptimizeIPData(context.Background(), 1, "v4")
	if err == nil || !strings.Contains(err.Error(), "无效的代理地址配置") {
		t.Fatalf("指向内网的代理前缀应被拒绝, 实际: %v", err)
	}
}

func TestOptimizeHTTPRejectsPrivateTarget(t *testing.T) {
	if _, err := optimizeHTTPGet(context.Background(), "http://127.0.0.1:8080/x.json"); err == nil {
		t.Fatal("回环地址应被 SSRF 校验拒绝")
	}
	if _, err := optimizeHTTPPostJSON(context.Background(), "http://10.0.0.1/api", []byte("{}")); err == nil {
		t.Fatal("私网地址应被 SSRF 校验拒绝")
	}
	if _, err := optimizeHTTPGet(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("非 http/https 协议应被 SSRF 校验拒绝")
	}
}

func TestOptimizeAPIConfigFallback(t *testing.T) {
	setupOptimizeTestDB(t)

	sysconfig.SetValue(OptimizeCfgAPI, "")
	if got := optimizeAPIConfig(); got != OptimizeAPIWetest {
		t.Errorf("空配置应回落到 wetest, 实际 %d", got)
	}
	sysconfig.SetValue(OptimizeCfgAPI, "not-a-number")
	if got := optimizeAPIConfig(); got != OptimizeAPIWetest {
		t.Errorf("非法配置应回落到 wetest, 实际 %d", got)
	}
	sysconfig.SetValue(OptimizeCfgAPI, "2")
	if got := optimizeAPIConfig(); got != OptimizeAPIXingpingcn {
		t.Errorf("配置为 2 时应使用 xingpingcn, 实际 %d", got)
	}
}

func TestOptimizeIntervalElapsed(t *testing.T) {
	setupOptimizeTestDB(t)

	// 从未执行过：允许立即执行
	sysconfig.SetValue(OptimizeCfgTime, "")
	if !optimizeIntervalElapsed() {
		t.Error("无执行记录时应允许执行")
	}

	// 刚执行过且间隔 30 分钟：不允许重复执行
	sysconfig.SetValue(OptimizeCfgMin, "30")
	setOptimizeRunTime(time.Now().Format("2006-01-02 15:04:05"))
	if optimizeIntervalElapsed() {
		t.Error("刚执行过且未到间隔时不应重复执行")
	}

	// 间隔配置为 10 分钟，上次执行时间为过去：允许执行
	sysconfig.SetValue(OptimizeCfgMin, "10")
	sysconfig.SetValue(OptimizeCfgTime, "2020-01-01 00:00:00")
	if !optimizeIntervalElapsed() {
		t.Error("超过间隔后应允许执行")
	}
}
