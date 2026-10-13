package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm/clause"

	"main/internal/database"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 云端解析记录的本地缓存
 *
 * 配额判定要在创建前知道「该域名现有多少条记录」，每次查云端会消耗服务商的
 * API 配额，且在配额紧张时连带影响正常的增删改。故本地留一份副本，
 * 由本服务定时拉取同步；创建前的判定只读本地，结论即时可得
 *
 * 同步是「以云端为准」的全量对账：
 *   云端有、本地无 → 新增（可能是在服务商控制台直接建的）
 *   本地有、云端无 → 删除（可能是在服务商控制台删的）
 *   两边都有但字段不同 → 更新
 * 只做差异写入，避免每次同步全表重写
 */

/* RecordSyncInterval 同步间隔，可在系统设置中用 record_sync_interval_min 覆盖 */
const (
	defaultRecordSyncMinutes = 30
	minRecordSyncMinutes     = 5
	/*
	 * 上界取 30 天
	 *
	 * 不只是防溢出：这个值直接进 time.Duration 运算，超过 int64 纳秒表示范围
	 * （约 153722867 分钟）就会回绕成负数，而负的间隔会让「是否到期」恒真，
	 * 把同步变成每轮 tick 都跑。30 天远小于回绕阈值，也远大于任何合理配置
	 */
	maxRecordSyncMinutes = 30 * 24 * 60
	recordSyncBatchSize  = 100
)

/* errRecordListTruncated 分页拉取触及页数上限，返回的记录不是云端全量 */
var errRecordListTruncated = errors.New("记录数超过分页上限，本轮只同步了前一部分")

var (
	// recordSyncFirstDelay 启动后首次同步的延迟，避开启动期的初始化与迁移；测试可覆盖为更短值
	recordSyncFirstDelay = 1 * time.Minute
	// recordSyncPollEvery 间隔到期的轮询周期；测试可覆盖为更短值
	recordSyncPollEvery = 1 * time.Minute
	// recordSyncIntervalOverrideNanos 仅测试使用：非零时以该纳秒数覆盖配置读到的间隔，
	// 便于把分钟级的间隔压到毫秒级，验证改配置后下一轮是否按新间隔发起
	recordSyncIntervalOverrideNanos atomic.Int64
)

/* 本轮同步状态落 SysConfig 的键，与 monitor 的 run_time、schedule 的 schedule_time 同构 */
const (
	recordSyncCfgLastTime = "record_sync_last_time"
	recordSyncCfgOK       = "record_sync_ok"
	recordSyncCfgFail     = "record_sync_fail"
	recordSyncCfgErr      = "record_sync_err"
	// recordSyncCfgTruncated 统计被截断的域名数，副本只落前 1 万条，不能与整轮成功混为一谈
	recordSyncCfgTruncated = "record_sync_truncated"
)

var (
	recordSyncer   *RecordSyncer
	recordSyncerMu sync.Mutex
)

/* RecordSyncer 记录同步器 */
type RecordSyncer struct {
	cancel   context.CancelFunc
	stopChan chan struct{}
	wg       sync.WaitGroup
	// lastRun 上轮同步结束的 UnixNano：持久化的时间戳只有秒精度，
	// 进程内到期判定用它，避免秒级截断让判定偏早
	lastRun atomic.Int64
}

/* StartRecordSync 启动定时同步，由 main 在任务上下文就绪后调用 */
func StartRecordSync(ctx context.Context) {
	recordSyncerMu.Lock()
	defer recordSyncerMu.Unlock()
	if recordSyncer != nil {
		return
	}
	// 自持一个可取消的上下文：关停时能打断正在进行的整轮同步，
	// 否则 wg.Wait 必须等整轮跑完（单域名最坏可达分钟级）
	syncCtx, cancel := context.WithCancel(ctx)
	s := &RecordSyncer{cancel: cancel, stopChan: make(chan struct{})}
	recordSyncer = s
	s.wg.Add(1)
	go s.run(syncCtx, s.stopChan)
	logger.Info("[RecordSync] 解析记录同步服务已启动")
}

/* StopRecordSync 停止同步服务 */
func StopRecordSync() {
	// 先摘指针再等待：等待期间不持有 recordSyncerMu，
	// 避免整段关停被一轮同步阻塞时连带锁住 StartRecordSync
	recordSyncerMu.Lock()
	s := recordSyncer
	recordSyncer = nil
	recordSyncerMu.Unlock()
	if s == nil {
		return
	}
	close(s.stopChan)
	s.cancel()
	s.wg.Wait()
	logger.Info("[RecordSync] 解析记录同步服务已停止")
}

func (s *RecordSyncer) run(ctx context.Context, stopChan chan struct{}) {
	defer s.wg.Done()

	// 启动后延迟首次同步，避开启动期的初始化与迁移
	firstDelay := time.NewTimer(currentRecordSyncFirstDelay())
	select {
	case <-stopChan:
		firstDelay.Stop()
		return
	case <-ctx.Done():
		firstDelay.Stop()
		return
	case <-firstDelay.C:
	}

	SyncAllDomainRecords(ctx)
	s.lastRun.Store(time.Now().UnixNano())

	// 固定短周期轮询、每轮重读配置判断是否到期（同 optimize 的调度范式）：
	// 间隔改动下个 tick 即生效，无需重启进程
	ticker := time.NewTicker(currentRecordSyncPollEvery())
	defer ticker.Stop()

	for {
		select {
		case <-stopChan:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			if !recordSyncDue(time.Now(), s.lastRun.Load()) {
				continue
			}
			SyncAllDomainRecords(ctx)
			s.lastRun.Store(time.Now().UnixNano())
		}
	}
}

/* currentRecordSyncFirstDelay 读取首轮延迟，配置非法时回落到内置默认 */
func currentRecordSyncFirstDelay() time.Duration {
	if recordSyncFirstDelay <= 0 {
		return 1 * time.Minute
	}
	return recordSyncFirstDelay
}

/* currentRecordSyncPollEvery 读取轮询周期，配置非法时回落到内置默认 */
func currentRecordSyncPollEvery() time.Duration {
	if recordSyncPollEvery <= 0 {
		return 1 * time.Minute
	}
	return recordSyncPollEvery
}

/*
 * recordSyncInterval 同步间隔，配置非法时回落到默认值
 *
 * 用 strconv.Atoi 而非 fmt.Sscanf：后者遇到 "5abc" 会解析出 5 且 err 为 nil，
 * 把明显的配置错误静默当成一个合法值。整数解析一旦接受前缀，
 * 就会把「管理员填错了」表现为「配置生效了但值不是他想要的那个」
 *
 * 上界校验同样是必需的：n 是管理员可控的输入，而 time.Duration(n)*time.Minute
 * 在 int64 纳秒上会回绕——实测 9223372036854775807 回绕成负 1 分钟，
 * 153722867280912931 回绕成正 52 秒。两者都不触发 n<min 的下界判断，
 * 前者让「距上轮是否到期」恒真、每轮 tick 都同步一次，
 * 后者让状态上报里的 interval_minutes 变成 0 或负数
 */
func recordSyncInterval() time.Duration {
	if nanos := recordSyncIntervalOverrideNanos.Load(); nanos > 0 {
		return time.Duration(nanos)
	}
	raw := strings.TrimSpace(sysconfig.GetValue("record_sync_interval_min"))
	if raw == "" {
		return defaultRecordSyncMinutes * time.Minute
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < minRecordSyncMinutes || n > maxRecordSyncMinutes {
		return defaultRecordSyncMinutes * time.Minute
	}
	return time.Duration(n) * time.Minute
}

/*
 * recordSyncDue 判断距上轮同步是否已到配置间隔
 *
 * 时间戳只在本轮同步跑完后更新，刷新页面或重启进程都不会前移它。
 * lastRunNanos 为进程内记录的上轮结束时刻（未同步过传 0），
 * 它优先于持久化时间戳：SysConfig 里的时间戳只有秒精度，
 * 秒级截断会让判定偏早，在短间隔下尤其明显
 */
func recordSyncDue(now time.Time, lastRunNanos int64) bool {
	if lastRunNanos > 0 {
		return now.UnixNano()-lastRunNanos >= int64(recordSyncInterval())
	}
	last := recordSyncLastTime()
	if last.IsZero() {
		return true
	}
	return now.Sub(last) >= recordSyncInterval()
}

/* recordSyncLastTime 读取上轮同步时间，未同步过或时间戳非法时返回零值 */
func recordSyncLastTime() time.Time {
	raw := strings.TrimSpace(sysconfig.GetValue(recordSyncCfgLastTime))
	if raw == "" {
		return time.Time{}
	}
	last, err := time.ParseInLocation("2006-01-02 15:04:05", raw, time.Local)
	if err != nil {
		return time.Time{}
	}
	return last
}

/*
 * recordSyncStatusSnapshot 读取记录同步的运行状态，供 API 暴露
 *
 * 副本同步失败此前只落一行日志，面板与管理员都不可见：副本冻结时
 * 记录列表仍读云端、看起来一切正常，配额判定却已失真
 */
func recordSyncStatusSnapshot() map[string]interface{} {
	last := strings.TrimSpace(sysconfig.GetValue(recordSyncCfgLastTime))
	interval := int(recordSyncInterval().Minutes())
	next := ""
	if t := recordSyncLastTime(); !t.IsZero() {
		next = t.Add(recordSyncInterval()).Format("2006-01-02 15:04:05")
	}
	return map[string]interface{}{
		"last_time":        last,
		"ok":               recordSyncIntValue(recordSyncCfgOK),
		"fail":             recordSyncIntValue(recordSyncCfgFail),
		"truncated":        recordSyncIntValue(recordSyncCfgTruncated),
		"last_error":       sysconfig.GetValue(recordSyncCfgErr),
		"interval_minutes": interval,
		"next_time":        next,
	}
}

/* recordSyncIntValue 读取状态里的计数值，未同步过时为空串，一律按 0 处理 */
func recordSyncIntValue(key string) int {
	raw := strings.TrimSpace(sysconfig.GetValue(key))
	if raw == "" {
		return 0
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return 0
	}
	return n
}

/* RecordSyncStatus 定时同步运行状态，供 API 暴露；服务未启动时返回 nil */
func RecordSyncStatus() map[string]interface{} {
	recordSyncerMu.Lock()
	started := recordSyncer != nil
	recordSyncerMu.Unlock()
	if !started {
		return nil
	}
	return recordSyncStatusSnapshot()
}

/*
 * setRecordSyncStatus 落盘本轮同步状态
 *
 * 计数值此前只随栈帧丢弃、失败只有一行日志，副本冻结时面板与管理员均不可见。
 * 写入 SysConfig 与 monitor 的 run_time、schedule 的 schedule_time 同构，
 * 不引入新的存储依赖
 */
func setRecordSyncStatus(okCount, failCount, truncatedCount int, lastErr string) {
	if database.DB == nil {
		return
	}
	if len(lastErr) > 200 {
		lastErr = lastErr[:200]
	}
	now := time.Now().Format("2006-01-02 15:04:05")
	sysconfig.SetValue(recordSyncCfgOK, fmt.Sprintf("%d", okCount))
	sysconfig.SetValue(recordSyncCfgFail, fmt.Sprintf("%d", failCount))
	sysconfig.SetValue(recordSyncCfgTruncated, fmt.Sprintf("%d", truncatedCount))
	sysconfig.SetValue(recordSyncCfgErr, lastErr)
	sysconfig.SetValue(recordSyncCfgLastTime, now)
}

/*
 * SyncAllDomainRecords 同步全部域名的解析记录
 *
 * 逐个域名处理而非并发拉取：DNS 服务商普遍按账户限流，并发会把配额瞬间打满，
 * 反而拖慢整体进度。单域名失败只记录日志并继续，不让一个异常账户阻断全量同步
 *
 * 本轮计数统一在退出时落盘：此前只随栈帧丢弃，副本冻结对面板与管理员均不可见
 */
func SyncAllDomainRecords(ctx context.Context) {
	okCount, failCount, truncatedCount := 0, 0, 0
	lastErr := ""
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[RecordSync] 同步任务异常: %v", r)
			lastErr = fmt.Sprintf("同步任务异常: %v", r)
		}
		setRecordSyncStatus(okCount, failCount, truncatedCount, lastErr)
	}()

	if database.DB == nil {
		return
	}

	var domains []models.Domain
	if err := database.DB.Where("deleted_at IS NULL").Find(&domains).Error; err != nil {
		logger.Error("[RecordSync] 查询域名失败: %v", err)
		lastErr = fmt.Sprintf("查询域名失败: %v", err)
		return
	}
	if len(domains) == 0 {
		logger.Info("[RecordSync] 本轮同步完成：暂无域名")
		return
	}

	for i := range domains {
		select {
		case <-ctx.Done():
			lastErr = "同步被中断"
			return
		default:
		}
		err := SyncDomainRecords(ctx, &domains[i])
		switch {
		case err == nil:
			okCount++
		case errors.Is(err, errRecordListTruncated):
			// 副本只落了前一部分，既不算纯成功也不算失败，单列一档
			truncatedCount++
			logger.Warn("[RecordSync] 域名 %s 同步不完整: %v", domains[i].Name, err)
			lastErr = fmt.Sprintf("%s: %v", domains[i].Name, err)
		default:
			failCount++
			logger.Warn("[RecordSync] 域名 %s 同步失败: %v", domains[i].Name, err)
			lastErr = fmt.Sprintf("%s: %v", domains[i].Name, err)
		}
	}
	logger.Info("[RecordSync] 本轮同步完成：成功 %d 个域名，失败 %d 个，不完整 %d 个",
		okCount, failCount, truncatedCount)
}

/*
 * SyncDomainRecords 同步单个域名的解析记录
 *
 * 分页拉取：多数服务商单页上限 100，域名记录数超过该值时只取首页会漏数据，
 * 而漏掉的记录会让配额判定偏松（本地少了记录，实际云端已存在）
 *
 * 分页被页数上限截断时只做增量、跳过删除段：截断的前若干页不是云端全量，
 * 拿它算删除集会把排在后面的记录从本地误删
 */
func SyncDomainRecords(ctx context.Context, domain *models.Domain) error {
	provider, err := providerForSync(domain)
	if err != nil {
		return err
	}

	remote, err := fetchAllRecords(ctx, provider, domain)
	if err != nil && !errors.Is(err, errRecordListTruncated) {
		return err
	}
	truncated := errors.Is(err, errRecordListTruncated)

	// 云端记录按 RecordID 建索引，便于与本地逐条对齐
	remoteByID := make(map[string]dns.Record, len(remote))
	for _, r := range remote {
		if r.ID != "" {
			remoteByID[r.ID] = r
		}
	}

	var local []models.DNSRecord
	if err := database.DB.Where("did = ?", domain.ID).Find(&local).Error; err != nil {
		return err
	}
	localByID := make(map[string]models.DNSRecord, len(local))
	for _, r := range local {
		localByID[r.RecordID] = r
	}

	now := time.Now()
	writeFail := 0
	// 新增与更新
	for id, r := range remoteByID {
		if lr, exists := localByID[id]; exists {
			if recordChanged(lr, r) {
				updates := map[string]interface{}{
					"name": r.Name, "type": r.Type, "value": r.Value, "line": r.Line,
					"ttl": r.TTL, "mx": r.MX, "weight": r.Weight,
					"remark": r.Remark, "synced_at": now,
				}
				// 远程未提供启用状态时保留本地值，不用空串覆盖
				if r.Status != "" {
					updates["status"] = r.Status
				}
				if err := database.DB.Model(&models.DNSRecord{}).Where("id = ?", lr.ID).Updates(updates).Error; err != nil {
					writeFail++
					logger.Warn("[RecordSync] 域名 %s 更新本地副本失败(记录 %s): %v", domain.Name, lr.RecordID, err)
				}
			} else {
				// 仅刷新同步时间，避免无谓的字段写入
				if err := database.DB.Model(&models.DNSRecord{}).Where("id = ?", lr.ID).Update("synced_at", now).Error; err != nil {
					writeFail++
					logger.Warn("[RecordSync] 域名 %s 刷新同步时间失败(记录 %s): %v", domain.Name, lr.RecordID, err)
				}
			}
			continue
		}
		/*
		 * 远程未提供启用状态时按 "enable" 落库，不留空串
		 *
		 * 留空会让「本地 status」这一列出现两种语义：本地写入路径
		 * （handler 与 v1 的回填）一律写 enable/disable，而对账拉来的可能为空。
		 * 下游按空串判断「是否暂停」时会把这类记录当成既非启用也非暂停
		 */
		status := r.Status
		if status == "" {
			status = "enable"
		}
		if err := database.DB.Create(&models.DNSRecord{
			DomainID: domain.ID, RecordID: r.ID, Name: r.Name, Type: r.Type,
			Value: r.Value, Line: r.Line, TTL: r.TTL, MX: r.MX, Weight: r.Weight,
			Status: status, Remark: r.Remark, Domain: domain.Name,
			SyncedAt: now,
		}).Error; err != nil {
			writeFail++
			logger.Warn("[RecordSync] 域名 %s 新增本地副本失败(记录 %s): %v", domain.Name, r.ID, err)
		}
	}

	/*
	 * 云端已不存在的本地副本要清掉，否则配额判定会偏严（本地多算了已删记录）
	 *
	 * 同时要释放该前缀的占用，否则对账会把占用变成「残留」：
	 * 通过面板建的记录被删除后，本地副本由写入路径即时清掉、占用随之释放；
	 * 但若这条记录是在**服务商控制台**删的，面板只能等对账发现，
	 * 而这里此前只删副本、不动占用——该前缀会被永久锁死，且持续占一条 records 额度
	 *
	 * 释放判据与删除路径同一个函数：该前缀下仍有记录（多值 A、别的记录）时不释放。
	 * 因此必须**先删副本再释放**，否则刚删掉的这条自己会被计入
	 */
	if !truncated {
		for id, lr := range localByID {
			if _, exists := remoteByID[id]; !exists {
				if err := database.DB.Delete(&models.DNSRecord{}, lr.ID).Error; err != nil {
					writeFail++
					logger.Warn("[RecordSync] 域名 %s 删除本地副本失败(记录 %s): %v", domain.Name, lr.RecordID, err)
					continue
				}
				ReleaseSubdomainClaimIfEmpty(domain.ID, lr.Name)
			}
		}
	}

	if writeFail > 0 {
		return fmt.Errorf("本地副本写入失败 %d 条", writeFail)
	}
	if truncated {
		return errRecordListTruncated
	}
	return nil
}

/*
 * fetchAllRecords 分页拉取域名下的全部记录
 *
 * 上限设为 100 页（约一万条），防止服务商返回异常的翻页游标导致死循环；
 * 达到上限时返回截断标记而不报错：调用方据此跳过删除段，
 * 使截断的后果限于「不更新」，而不是拿不完整的云端清单误删本地副本
 */
func fetchAllRecords(ctx context.Context, provider dns.Provider, domain *models.Domain) ([]dns.Record, error) {
	const maxPages = 100
	var all []dns.Record
	for page := 1; page <= maxPages; page++ {
		// 关停或上层取消时必须跳出：否则整轮同步要等所有域名跑完，
		// StopRecordSync 的 wg.Wait 随之被拖住
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		pr, err := provider.GetDomainRecords(ctx, page, recordSyncBatchSize, "", "", "", "", "", "")
		if err != nil {
			return nil, err
		}
		records, ok := pr.Records.([]dns.Record)
		if !ok {
			return nil, fmt.Errorf("记录列表类型异常")
		}
		all = append(all, records...)
		if len(records) < recordSyncBatchSize {
			return all, nil
		}
	}
	logger.Warn("[RecordSync] 域名 %s 记录数超过 %d 条，本轮只同步前 %d 条",
		domain.Name, maxPages*recordSyncBatchSize, maxPages*recordSyncBatchSize)
	return all, errRecordListTruncated
}

/* recordChanged 判断本地副本与云端记录是否有字段差异 */
func recordChanged(local models.DNSRecord, remote dns.Record) bool {
	return local.Name != remote.Name || local.Type != remote.Type ||
		local.Value != remote.Value || local.Line != remote.Line ||
		local.TTL != remote.TTL || local.MX != remote.MX ||
		remoteStatusDiffers(local, remote) || local.Remark != remote.Remark
}

/*
 * remoteStatusDiffers 远程记录的启用状态与本地是否不同
 *
 * 少数服务商的列表接口不返回启用状态（henet 与 jdcloud 的 dns.Record 构造里
 * 根本没有 Status 字段），dns.Record.Status 留空。若直接比较，
 * 空串永远不等于本地的 "enable"/"disable"，于是每轮对每条记录都判定为「已变更」
 * 并写一次库——SQLite 写放大，且写库失败会把整个域名计入失败数
 *
 * 判据：远程未提供该字段时不参与比较。这样既不再每轮空写，
 * 也不会把本地已有的状态覆盖成空串（新建分支与更新分支都已按此处理）
 */
func remoteStatusDiffers(local models.DNSRecord, remote dns.Record) bool {
	return remote.Status != "" && local.Status != remote.Status
}

/* providerForSync 取域名对应的 DNS 服务商实例 */
func providerForSync(domain *models.Domain) (dns.Provider, error) {
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return nil, fmt.Errorf("账户不存在: %w", err)
	}
	config := map[string]string{}
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		return nil, fmt.Errorf("账户配置解析失败: %w", err)
	}
	return dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
}

/*
 * UpsertLocalRecord 写入本地副本（新增或更新同一条云端记录）
 *
 * 不等下一轮定时同步：用户创建后立即再建一条同名记录时，配额判定都读本地，
 * 若不即时写入会放行一个本该被拒的请求
 *
 * 用 OnConflict 而非 Create：record_id 上有唯一索引，更新路径复用同一个函数时
 * 若直接 Create 会撞唯一约束而静默失败（错误被丢弃），本地副本停在旧值上
 */
func UpsertLocalRecord(rec *models.DNSRecord) {
	if database.DB == nil || rec == nil {
		return
	}
	rec.SyncedAt = time.Now()
	database.DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "did"}, {Name: "record_id"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"name", "type", "value", "line", "ttl", "mx", "weight",
			"status", "remark", "domain", "synced_at",
		}),
	}).Create(rec)
}

/* DeleteLocalRecord 删除成功后同步移除本地副本 */
func DeleteLocalRecord(domainID uint, recordID string) {
	if database.DB == nil {
		return
	}
	database.DB.Where("did = ? AND record_id = ?", domainID, recordID).Delete(&models.DNSRecord{})
}

/*
 * LocalRecord 读取本地副本里的一条记录
 *
 * 供写入路径在回填前拿到原行：更新类操作只改请求里显式给出的字段，
 * 其余字段必须保留本地已有值，否则会把它们清成零值
 */
func LocalRecord(domainID uint, recordID string) (models.DNSRecord, bool) {
	var rec models.DNSRecord
	if database.DB == nil {
		return rec, false
	}
	if err := database.DB.Where("did = ? AND record_id = ?", domainID, recordID).
		First(&rec).Error; err != nil {
		return rec, false
	}
	return rec, true
}
