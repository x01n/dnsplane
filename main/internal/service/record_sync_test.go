package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"gorm.io/gorm"

	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
	"main/internal/sysconfig"
)

/* syncTestProviderName 测试用服务商名，注册进全局注册表供 providerForSync 取用 */
const syncTestProviderName = "sync-test-fake"

/* registerSyncTestProvider 把测试服务商注册进全局注册表，返回注册实例 */
func registerSyncTestProvider(t *testing.T, prov dns.Provider) {
	t.Helper()
	dns.Register(syncTestProviderName, func(config map[string]string, domain, domainID string) dns.Provider {
		return prov
	}, dns.ProviderConfig{Type: syncTestProviderName, Name: "记录同步测试服务商"})
}

/* setupSyncTestDomain 落库一个账户与域名，使 providerForSync 能取到服务商实例 */
func setupSyncTestDomain(t *testing.T) models.Domain {
	t.Helper()
	account := models.Account{UserID: 1, Type: syncTestProviderName, Name: "sync-test", Config: "{}"}
	if err := database.DB.Create(&account).Error; err != nil {
		t.Fatalf("创建测试账户失败: %v", err)
	}
	domain := models.Domain{AccountID: account.ID, Name: "sync-test.example.com", ThirdID: "zone-1"}
	if err := database.DB.Create(&domain).Error; err != nil {
		t.Fatalf("创建测试域名失败: %v", err)
	}
	return domain
}

/* readSysConfigFromDB 直接读库断言落盘值，绕开 sysconfig 的 60 秒缓存 */
func readSysConfigFromDB(t *testing.T, key string) string {
	t.Helper()
	var cfg models.SysConfig
	if err := database.DB.Where("`key` = ?", key).First(&cfg).Error; err != nil {
		return ""
	}
	return cfg.Value
}

/* syncBlockingProvider 记录拉取会一直阻塞，直到上下文被取消或测试主动放行 */
type syncBlockingProvider struct {
	fakeProvider
	started chan struct{}
	release chan struct{}
	once    sync.Once
	mu      sync.Mutex
	calls   int
}

func (p *syncBlockingProvider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	p.mu.Lock()
	p.calls++
	p.mu.Unlock()
	p.once.Do(func() { close(p.started) })
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-p.release:
		return &dns.PageResult{Records: []dns.Record{}}, nil
	}
}

func (p *syncBlockingProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

/*
 * TestStopRecordSyncInterruptsInFlightSync 断言关停能打断正在进行的整轮同步
 *
 * 旧实现的 StopRecordSync 持锁 wg.Wait，而 run 里的同步上下文不可取消：
 * 拉取被服务商拖住时关停必须等整轮跑完，进程退出随之被拖住
 */
func TestStopRecordSyncInterruptsInFlightSync(t *testing.T) {
	setupOptimizeTestDB(t)

	prov := &syncBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	registerSyncTestProvider(t, prov)
	setupSyncTestDomain(t)

	prevDelay := recordSyncFirstDelay
	recordSyncFirstDelay = 10 * time.Millisecond
	t.Cleanup(func() { recordSyncFirstDelay = prevDelay })

	// 清理顺序为后注册先执行：先放行阻塞的拉取，再停服务，
	// 避免用例失败时清理本身卡死
	t.Cleanup(StopRecordSync)
	t.Cleanup(func() {
		select {
		case <-prov.release:
		default:
			close(prov.release)
		}
	})

	StartRecordSync(context.Background())
	select {
	case <-prov.started:
	case <-time.After(5 * time.Second):
		t.Fatal("首轮同步未在 5 秒内发起拉取")
	}

	done := make(chan struct{})
	start := time.Now()
	go func() {
		StopRecordSync()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("StopRecordSync 未能在 3 秒内返回：关停仍被整轮同步阻塞")
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("停止耗时 %v，超过 3 秒上限", elapsed)
	}

	callsAfterStop := prov.callCount()
	time.Sleep(100 * time.Millisecond)
	if got := prov.callCount(); got != callsAfterStop {
		t.Fatalf("停止后仍在拉取记录：停止时 %d 次，停止后 %d 次", callsAfterStop, got)
	}
}

/* syncCancelAfterFirstProvider 首页返回满页后立刻取消上下文，用于验证翻页会响应取消 */
type syncCancelAfterFirstProvider struct {
	fakeProvider
	cancel context.CancelFunc
	mu     sync.Mutex
	calls  int
}

func (p *syncCancelAfterFirstProvider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	p.mu.Lock()
	p.calls++
	first := p.calls == 1
	p.mu.Unlock()
	records := make([]dns.Record, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		records = append(records, dns.Record{ID: fmt.Sprintf("p%d-%d", page, i), Type: "A", Value: "10.0.0.1"})
	}
	if first {
		p.cancel()
	}
	return &dns.PageResult{Total: pageSize, Records: records}, nil
}

func (p *syncCancelAfterFirstProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.calls
}

/*
 * TestFetchAllRecordsStopsWhenContextCanceled 断言上下文取消后不再继续翻页
 *
 * 旧实现的分页循环没有任何中断检查，取消后仍会把 100 页全部拉完，
 * 使上层关停无法打断同步
 */
func TestFetchAllRecordsStopsWhenContextCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	prov := &syncCancelAfterFirstProvider{cancel: cancel}

	_, err := fetchAllRecords(ctx, prov, &models.Domain{Name: "example.com"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消后应返回 context.Canceled，实际 %v", err)
	}
	if got := prov.callCount(); got != 1 {
		t.Fatalf("取消后不应继续翻页，实际调用 %d 次", got)
	}
}

/* syncPagingProvider 每页都返回满页，模拟记录数超过页数上限的域名 */
type syncPagingProvider struct {
	fakeProvider
}

func (p *syncPagingProvider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	records := make([]dns.Record, 0, pageSize)
	for i := 0; i < pageSize; i++ {
		records = append(records, dns.Record{
			ID:    fmt.Sprintf("p%d-%d", page, i),
			Name:  fmt.Sprintf("host%d", i),
			Type:  "A",
			Value: fmt.Sprintf("10.0.%d.%d", page, i%256),
		})
	}
	return &dns.PageResult{Total: pageSize, Records: records}, nil
}

/* localRecordIDs 读本地副本的记录 ID 集合 */
func localRecordIDs(t *testing.T, domainID uint) map[string]bool {
	t.Helper()
	var rows []models.DNSRecord
	if err := database.DB.Where("did = ?", domainID).Find(&rows).Error; err != nil {
		t.Fatalf("查询本地副本失败: %v", err)
	}
	out := make(map[string]bool, len(rows))
	for _, r := range rows {
		out[r.RecordID] = true
	}
	return out
}

/*
 * TestSyncDomainRecordsKeepsLocalWhenRemoteTruncated 断言云端清单被截断时不删除本地副本
 *
 * 旧实现把截断的前 100 页当成云端全量，排在 1 万条之外的记录会被从本地误删，
 * 配额判定随之偏严，且本地副本与云端永久不一致
 */
func TestSyncDomainRecordsKeepsLocalWhenRemoteTruncated(t *testing.T) {
	setupOptimizeTestDB(t)

	prov := &syncPagingProvider{}
	registerSyncTestProvider(t, prov)
	domain := setupSyncTestDomain(t)

	for _, id := range []string{"p1-0", "cloud-deleted"} {
		if err := database.DB.Create(&models.DNSRecord{
			DomainID: domain.ID, RecordID: id, Name: id, Type: "A", Value: "192.0.2.1", Domain: domain.Name,
		}).Error; err != nil {
			t.Fatalf("预置本地副本失败: %v", err)
		}
	}

	err := SyncDomainRecords(context.Background(), &domain)
	if !errors.Is(err, errRecordListTruncated) {
		t.Fatalf("截断轮次应返回截断标记，实际 %v", err)
	}

	ids := localRecordIDs(t, domain.ID)
	if !ids["cloud-deleted"] {
		t.Fatal("云端清单被截断时不应删除本地记录：截断集不能当作全量")
	}
	if !ids["p100-0"] {
		t.Fatal("截断轮次仍应写入云端已有的记录")
	}
}

/* syncFixedProvider 每次拉取返回固定的记录集，模拟一个能一次拉完的小域名 */
type syncFixedProvider struct {
	fakeProvider
	records []dns.Record
	mu      sync.Mutex
	rounds  int
}

func (p *syncFixedProvider) GetDomainRecords(ctx context.Context, page, pageSize int, keyword, subDomain, value, recordType, line, status string) (*dns.PageResult, error) {
	if page == 1 {
		p.mu.Lock()
		p.rounds++
		p.mu.Unlock()
		return &dns.PageResult{Total: len(p.records), Records: p.records}, nil
	}
	return &dns.PageResult{Total: len(p.records), Records: []dns.Record{}}, nil
}

/* rounds 返回拉取到的同步轮数，一轮只拉一次首页 */
func (p *syncFixedProvider) roundCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.rounds
}

/*
 * TestSyncAllDomainRecordsRecordsStatusAndWriteFailures 断言本轮计数落盘且写库失败计入失败数
 *
 * 旧实现的成败计数只随栈帧丢弃、只剩一行日志，副本冻结时面板与管理员都察觉不到；
 * 写库失败（如 VACUUM 期间的 SQLITE_BUSY）同样被丢弃，也不计入失败数
 */
func TestSyncAllDomainRecordsRecordsStatusAndWriteFailures(t *testing.T) {
	setupOptimizeTestDB(t)

	prov := &syncFixedProvider{records: []dns.Record{{ID: "r1", Name: "www", Type: "A", Value: "192.0.2.10"}}}
	registerSyncTestProvider(t, prov)
	domain := setupSyncTestDomain(t)

	// 注入 dns_records 表的 Create 失败，复现 SQLite 写锁竞争时写语句返回错误的真实情形；
	// 必须按表名过滤，否则会把状态写回 SysConfig 的插入一并拦下，用例就测不到目标行为
	const cbName = "test:record_sync_fail_create"
	database.DB.Callback().Create().Before("gorm:create").Register(cbName, func(tx *gorm.DB) {
		if tx.Statement.Table != "dns_records" {
			return
		}
		tx.AddError(errors.New("database is locked (5) (SQLITE_BUSY)"))
	})
	t.Cleanup(func() { database.DB.Callback().Create().Remove(cbName) })

	SyncAllDomainRecords(context.Background())

	if got := readSysConfigFromDB(t, recordSyncCfgFail); got != "1" {
		t.Fatalf("写库失败应计入失败数，record_sync_fail=%q", got)
	}
	if got := readSysConfigFromDB(t, recordSyncCfgOK); got != "0" {
		t.Fatalf("写库失败的域名不应计入成功，record_sync_ok=%q", got)
	}
	if got := readSysConfigFromDB(t, recordSyncCfgLastTime); got == "" {
		t.Fatal("本轮同步应落盘时间戳")
	}
	if got := readSysConfigFromDB(t, recordSyncCfgErr); !strings.Contains(got, domain.Name) {
		t.Fatalf("最近失败原因应含域名，实际 %q", got)
	}
}

/*
 * TestSyncAllDomainRecordsCountsSuccess 断言全部成功时计数为成功且无失败记录
 */
func TestSyncAllDomainRecordsCountsSuccess(t *testing.T) {
	setupOptimizeTestDB(t)

	prov := &syncFixedProvider{records: []dns.Record{{ID: "r1", Name: "www", Type: "A", Value: "192.0.2.10"}}}
	registerSyncTestProvider(t, prov)
	domain := setupSyncTestDomain(t)

	SyncAllDomainRecords(context.Background())

	if got := readSysConfigFromDB(t, recordSyncCfgOK); got != "1" {
		t.Fatalf("record_sync_ok 应为 1，实际 %q", got)
	}
	if got := readSysConfigFromDB(t, recordSyncCfgFail); got != "0" {
		t.Fatalf("record_sync_fail 应为 0，实际 %q", got)
	}
	if ids := localRecordIDs(t, domain.ID); !ids["r1"] {
		t.Fatal("同步成功应写入本地副本")
	}
}

/*
 * TestRecordSyncDueFollowsConfigChange 断言到期判定按配置做时间算术，同一时刻只改配置结果就变
 *
 * 旧实现的 ticker 周期由启动时的首读值永久固定、且没有任何到期判定，间隔改动只在重启后生效
 */
func TestRecordSyncDueFollowsConfigChange(t *testing.T) {
	setupOptimizeTestDB(t)

	now := time.Now()
	format := func(d time.Duration) string { return now.Add(d).Format("2006-01-02 15:04:05") }

	sysconfig.SetValue("record_sync_interval_min", "5")
	sysconfig.SetValue(recordSyncCfgLastTime, format(-6*time.Minute))
	if !recordSyncDue(now, 0) {
		t.Fatal("间隔 5 分钟、上轮 6 分钟前，应判定到期")
	}

	// 同一时刻只改配置：若到期只由固定的 ticker 周期决定，这里不会出现差异
	sysconfig.SetValue("record_sync_interval_min", "60")
	if recordSyncDue(now, 0) {
		t.Fatal("间隔改为 60 分钟后，同一时间戳不应再判定到期")
	}

	sysconfig.SetValue("record_sync_interval_min", "5")
	sysconfig.SetValue(recordSyncCfgLastTime, format(-4*time.Minute))
	if recordSyncDue(now, 0) {
		t.Fatal("间隔 5 分钟、上轮 4 分钟前，不应判定到期")
	}

	sysconfig.SetValue(recordSyncCfgLastTime, "")
	if !recordSyncDue(now, 0) {
		t.Fatal("没有上轮时间戳时应判定到期")
	}
}

/*
 * TestRecordSyncRunPicksUpConfigChange 断言长期运行中改配置后，下一轮同步按新间隔真正发起
 *
 * 本用例驱动的是 run 的定时循环本身（含固定轮询周期与到期判定），
 * 旧实现没有到期判定、ticker 周期又由首读值固定，用例无法通过
 */
func TestRecordSyncRunPicksUpConfigChange(t *testing.T) {
	setupOptimizeTestDB(t)

	prov := &syncFixedProvider{records: []dns.Record{{ID: "r1", Name: "www", Type: "A", Value: "192.0.2.10"}}}
	registerSyncTestProvider(t, prov)
	setupSyncTestDomain(t)

	prevDelay := recordSyncFirstDelay
	recordSyncFirstDelay = 10 * time.Millisecond
	t.Cleanup(func() { recordSyncFirstDelay = prevDelay })
	prevPoll := recordSyncPollEvery
	recordSyncPollEvery = 100 * time.Millisecond
	t.Cleanup(func() { recordSyncPollEvery = prevPoll })
	t.Cleanup(StopRecordSync)

	recordSyncIntervalOverrideNanos.Store(int64(120 * time.Millisecond))
	t.Cleanup(func() { recordSyncIntervalOverrideNanos.Store(0) })

	StartRecordSync(context.Background())
	waitForRounds(t, prov, 1, 5*time.Second)

	// 间隔改为 2 倍后，同一时间窗口内的同步轮数应明显减少（旧实现无到期判定，这里会打满）
	recordSyncIntervalOverrideNanos.Store(int64(240 * time.Millisecond))
	waitForRounds(t, prov, 2, 5*time.Second)
	before := prov.roundCount()
	time.Sleep(600 * time.Millisecond)
	delta := prov.roundCount() - before
	if delta > 3 {
		t.Fatalf("间隔拉长后 600ms 内仍跑了 %d 轮，说明配置改动未生效", delta)
	}
	if delta == 0 {
		t.Fatalf("间隔拉长后 600ms 内一轮都没跑，疑似同步循环已停")
	}
}

/* waitForRounds 等同步循环在 provider 侧累计发起指定轮数，超时即失败 */
func waitForRounds(t *testing.T, prov *syncFixedProvider, want int, limit time.Duration) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if prov.roundCount() >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%v 内未完成 %d 轮同步，实际 %d 轮", limit, want, prov.roundCount())
}

/*
 * TestRecordSyncIntervalBounds 断言间隔配置的边界处理
 */
func TestRecordSyncIntervalBounds(t *testing.T) {
	setupOptimizeTestDB(t)

	cases := map[string]time.Duration{
		"":      30 * time.Minute,
		"abc":   30 * time.Minute,
		"3":     30 * time.Minute,
		" 5 ":   5 * time.Minute,
		"60":    60 * time.Minute,
		"43200": 43200 * time.Minute,
	}
	for raw, want := range cases {
		sysconfig.SetValue("record_sync_interval_min", raw)
		if got := recordSyncInterval(); got != want {
			t.Errorf("配置 %q 时 recordSyncInterval()=%v，期望 %v", raw, got, want)
		}
	}
}

/*
 * TestRecordSyncStatusOnlyAfterStart 断言状态只在服务启动后暴露
 */
func TestRecordSyncStatusOnlyAfterStart(t *testing.T) {
	setupOptimizeTestDB(t)

	StopRecordSync()
	if got := RecordSyncStatus(); got != nil {
		t.Fatalf("服务未启动时不应暴露状态，实际 %v", got)
	}

	prov := &syncBlockingProvider{started: make(chan struct{}), release: make(chan struct{})}
	registerSyncTestProvider(t, prov)
	setupSyncTestDomain(t)

	t.Cleanup(StopRecordSync)
	t.Cleanup(func() {
		select {
		case <-prov.release:
		default:
			close(prov.release)
		}
	})
	StartRecordSync(context.Background())

	got := RecordSyncStatus()
	if got == nil {
		t.Fatal("服务启动后应暴露状态")
	}
	for _, key := range []string{"last_time", "ok", "fail", "truncated", "last_error", "interval_minutes", "next_time"} {
		if _, ok := got[key]; !ok {
			t.Errorf("状态缺少字段 %s", key)
		}
	}
}

/*
 * 配置解析的值域与格式
 *
 * 原实现用 fmt.Sscanf 解析，「5abc」得到 5 且 err 为 nil——明显的配置错误
 * 被静默当成一个合法值。改 Atoi 后这类输入一律回落默认值。
 *
 * 同时补上界：time.Duration(n)*time.Minute 在 n 极大时按 int64 纳秒回绕，
 * 实测 9223372036854775807 得 -1m、153722867280912931 得 52s。
 * 回绕成负数会让「距上轮是否到期」恒真，把同步变成每轮 tick 都跑
 */
func TestRecordSyncIntervalRejectsDirtyAndOverflow(t *testing.T) {
	setupOptimizeTestDB(t)

	cases := []struct {
		raw  string
		want time.Duration
		note string
	}{
		{"5", 5 * time.Minute, "合法下限"},
		{" 5 ", 5 * time.Minute, "两侧空白"},
		{"43200", 43200 * time.Minute, "合法上限（30 天）"},
		{"", defaultRecordSyncMinutes * time.Minute, "空值"},
		{"abc", defaultRecordSyncMinutes * time.Minute, "非数字"},
		{"4", defaultRecordSyncMinutes * time.Minute, "低于下限"},
		{"43201", defaultRecordSyncMinutes * time.Minute, "高于上限"},
		{"5abc", defaultRecordSyncMinutes * time.Minute, "脏输入：旧实现会静默取 5"},
		{"-5", defaultRecordSyncMinutes * time.Minute, "负数"},
		{"9223372036854775807", defaultRecordSyncMinutes * time.Minute, "回绕边界：旧实现得 -1m"},
		{"153722867280912931", defaultRecordSyncMinutes * time.Minute, "回绕边界：旧实现得 52s"},
		{"99999999999999999999", defaultRecordSyncMinutes * time.Minute, "超出 int64"},
	}

	for _, tc := range cases {
		t.Run(tc.raw, func(t *testing.T) {
			sysconfig.SetValue("record_sync_interval_min", tc.raw)
			got := recordSyncInterval()
			if got != tc.want {
				t.Errorf("输入 %q（%s）：期望 %v，实际 %v", tc.raw, tc.note, tc.want, got)
			}
			if got <= 0 {
				t.Errorf("输入 %q 得到非正间隔 %v，会让「是否到期」恒真", tc.raw, got)
			}
		})
	}
}

/*
 * 远程未提供启用状态时不得判定为「已变更」
 *
 * henet 与 jdcloud 的列表接口不返回启用状态（它们的 dns.Record 构造里
 * 根本没有 Status 字段）。若直接比较，空串永远不等于本地的 enable/disable，
 * 每轮对每条记录都判定为已变更并写一次库
 */
func TestRecordChangedIgnoresEmptyRemoteStatus(t *testing.T) {
	local := models.DNSRecord{Name: "www", Type: "A", Value: "192.0.2.1", Status: "enable"}
	remote := dns.Record{Name: "www", Type: "A", Value: "192.0.2.1", Status: ""}

	if recordChanged(local, remote) {
		t.Error("远程未提供启用状态时不应判定为已变更：会导致每轮对每条记录空写一次库")
	}

	// 远程明确给出不同的状态时必须判为已变更
	remoteOff := dns.Record{Name: "www", Type: "A", Value: "192.0.2.1", Status: "disable"}
	if !recordChanged(local, remoteOff) {
		t.Error("远程明确给出 disable 而本地是 enable，应判定为已变更")
	}

	// 远程给出相同状态时不构成变更
	remoteSame := dns.Record{Name: "www", Type: "A", Value: "192.0.2.1", Status: "enable"}
	if recordChanged(local, remoteSame) {
		t.Error("字段完全一致时不应判定为已变更")
	}
}
