package service

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"main/internal/database"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 优选IP 任务执行层
 */

/* 华为云单条记录聚合多值，因此每线路只写一条聚合记录 */
const optimizeHuaweiProvider = "huawei"

/* 记录值类型标识 v6，取自任务表 ip_type 字段的取值（v4 走 A 记录，无需额外常量） */
const optimizeIPTypeV6 = "v6"

/* 单次任务执行的记录操作上限，防止异常数据导致长时间占用 DNS 接口 */
const optimizeMaxRecordOps = 200

/* OptimizeRunResult 单次任务执行的记录变更统计 */
type OptimizeRunResult struct {
	Added   int // 新增记录数
	Changed int // 修改记录数
	Deleted int // 删除记录数
}

func (r OptimizeRunResult) String() string {
	return fmt.Sprintf("成功添加%d条记录，修改%d条记录，删除%d条记录", r.Added, r.Changed, r.Deleted)
}

/* OptimizeExecutor 优选IP 任务执行器，持有单次执行的统计计数 */
type OptimizeExecutor struct {
	// result 全局累计
	result OptimizeRunResult
}

/* NewOptimizeExecutor 创建优选IP 任务执行器 */
func NewOptimizeExecutor() *OptimizeExecutor {
	return &OptimizeExecutor{}
}

/* Run 执行单个优选任务并返回变更统计 */
func (e *OptimizeExecutor) Run(ctx context.Context, task *models.OptimizeIP) (OptimizeRunResult, error) {
	e.result = OptimizeRunResult{}

	ipTypes := make([]string, 0, 2)
	for _, t := range strings.Split(task.IPType, ",") {
		if t = strings.TrimSpace(t); t != "" {
			ipTypes = append(ipTypes, t)
		}
	}
	if len(ipTypes) == 0 {
		return e.result, fmt.Errorf("解析IP类型不能为空")
	}

	for _, ipType := range ipTypes {
		if err := e.runIPType(ctx, task, ipType); err != nil {
			return e.result, err
		}
	}
	return e.result, nil
}

/*
 * runIPType 处理单个 IP 类型
 * ip_type 为逗号分隔的多值（如 v4,v6），每个值都要独立取一次接口数据并写入对应记录类型
 */
func (e *OptimizeExecutor) runIPType(ctx context.Context, task *models.OptimizeIP, ipType string) error {
	var domain models.Domain
	if err := database.DB.First(&domain, task.DomainID).Error; err != nil {
		return fmt.Errorf("域名不存在（ID：%d）", task.DomainID)
	}
	providerType := domainProviderType(domain.AccountID)
	if providerType == "" {
		return fmt.Errorf("域名账号不存在（ID：%d）", domain.AccountID)
	}
	lineMapping := dns.DefaultLineMapping[providerType]
	if len(lineMapping) == 0 {
		return fmt.Errorf("不支持的DNS服务商")
	}

	info, err := GetOptimizeIPData(ctx, task.CDNType, ipType)
	if err != nil {
		return err
	}

	provider, err := getScheduleProvider(&domain)
	if err != nil {
		return fmt.Errorf("DNS模块不存在：%v", err)
	}
	records, err := listOptimizeRecords(ctx, provider, task.RR)
	if err != nil {
		return err
	}

	// type==1 时电信数据改写到默认线路，需先确认接口确实返回了 DEF
	taskType := task.Type
	if taskType == 1 {
		if def, ok := info["DEF"]; ok && len(def) > 0 {
			taskType = 0
		}
	}

	recordType := "A"
	if ipType == optimizeIPTypeV6 {
		recordType = "AAAA"
	}

	totalOps := 0
	for _, line := range optimizeLineKeys {
		ips := info[line]
		if len(ips) == 0 {
			continue
		}
		recordNum := task.RecordNum
		getIPs := ips
		if providerType == optimizeHuaweiProvider {
			// 华为云把多值塞进一条记录，先排序截断再拼成单值
			sorted := append([]string(nil), ips...)
			sort.Strings(sorted)
			if recordNum > 0 && recordNum < len(sorted) {
				sorted = sorted[:recordNum]
			}
			getIPs = []string{strings.Join(sorted, ",")}
			recordNum = 1
		}

		targetLine := line
		if taskType == 1 && line == "CT" {
			targetLine = "DEF"
		}
		lineName, ok := lineMapping[targetLine]
		if !ok {
			continue
		}

		ops, err := e.processDNSLine(ctx, provider, task, records, recordNum, getIPs, lineName, recordType)
		if err != nil {
			return err
		}
		totalOps += ops
		if totalOps > optimizeMaxRecordOps {
			return fmt.Errorf("单次任务记录变更超过 %d 次，已中止", optimizeMaxRecordOps)
		}
	}
	return nil
}

/*
 * processDNSLine 处理单个线路的解析记录
 * 先清理同线路的 CNAME，再按「改多删少、补足数量」的方式把 A/AAAA 记录收敛到目标 IP 集合
 */
func (e *OptimizeExecutor) processDNSLine(ctx context.Context, provider dns.Provider, task *models.OptimizeIP,
	records []dns.Record, recordNum int, getIPs []string, lineName, recordType string) (int, error) {

	ops := 0

	// 同线路的 CNAME 与 A/AAAA 互斥，先删除 CNAME
	for i := range records {
		if records[i].Line != lineName || records[i].Type != "CNAME" {
			continue
		}
		if err := provider.DeleteDomainRecord(ctx, records[i].ID); err != nil {
			return ops, fmt.Errorf("删除CNAME解析失败，%v", err)
		}
		ops++
	}

	existIPs := make([]string, 0, len(records))
	for i := range records {
		if records[i].Line != lineName || records[i].Type != recordType {
			continue
		}
		existIPs = append(existIPs, records[i].Value)
	}

	delIPs := stringSetDiff(existIPs, getIPs)
	addIPs := stringSetDiff(getIPs, existIPs)
	correctCount := len(existIPs) - len(delIPs)

	if len(delIPs) > 0 {
		for i := range records {
			if records[i].Line != lineName || records[i].Type != recordType {
				continue
			}
			if _, need := delIPs[records[i].Value]; !need {
				continue
			}
			// 待删记录优先用待增 IP 改写，改写不到才真正删除
			if len(addIPs) > 0 {
				addIP := popLast(addIPs)
				if err := provider.UpdateDomainRecord(ctx, records[i].ID, task.RR, recordType, addIP, lineName, task.TTL, 1, nil, ""); err != nil {
					return ops, fmt.Errorf("修改解析失败，%v", err)
				}
				e.result.Changed++
				correctCount++
			} else {
				if err := provider.DeleteDomainRecord(ctx, records[i].ID); err != nil {
					return ops, fmt.Errorf("删除解析失败，%v", err)
				}
				e.result.Deleted++
			}
			ops++
		}
	}

	if correctCount < recordNum && len(addIPs) > 0 {
		for addIP := range addIPs {
			if _, err := provider.AddDomainRecord(ctx, task.RR, recordType, addIP, lineName, task.TTL, 1, nil, ""); err != nil {
				return ops, fmt.Errorf("添加解析失败，%v", err)
			}
			e.result.Added++
			correctCount++
			ops++
			if correctCount >= recordNum {
				break
			}
		}
	}
	return ops, nil
}

/* 分页取记录时的上限，防止 provider 返回与分页数据不一致的 Total 时陷入长时间循环 */
const (
	optimizeRecordPageSize = 100 // 单页大小
	optimizeRecordMaxPage  = 20  // 最多 20 页 = 2000 条，远超单个主机记录的合理数量
)

/*
 * listOptimizeRecords 取该主机记录下的全部解析记录
 * 刻意不带记录类型过滤：processDNSLine 需要同时拿到同线路的 CNAME 才能删除它
 */
func listOptimizeRecords(ctx context.Context, provider dns.Provider, rr string) ([]dns.Record, error) {
	var all []dns.Record
	// 三重终止条件：页数上限、返回空页、累计条数达到 Total 或单页未取满
	for page := 1; page <= optimizeRecordMaxPage; page++ {
		res, err := provider.GetSubDomainRecords(ctx, rr, page, optimizeRecordPageSize, "", "")
		if err != nil {
			return nil, fmt.Errorf("获取记录列表失败，%v", err)
		}
		records, ok := res.Records.([]dns.Record)
		if !ok || len(records) == 0 {
			break
		}
		all = append(all, records...)
		if res.Total > 0 && len(all) >= res.Total {
			break
		}
		if len(records) < optimizeRecordPageSize {
			break
		}
	}
	return all, nil
}

/* stringSetDiff 计算 a 中不在 b 中的元素集合 */
func stringSetDiff(a, b []string) map[string]struct{} {
	other := make(map[string]struct{}, len(b))
	for _, v := range b {
		other[v] = struct{}{}
	}
	diff := make(map[string]struct{}, len(a))
	for _, v := range a {
		if _, ok := other[v]; !ok {
			diff[v] = struct{}{}
		}
	}
	return diff
}

/* popLast 取出集合并移除任意一个元素 */
func popLast(set map[string]struct{}) string {
	for v := range set {
		delete(set, v)
		return v
	}
	return ""
}

/* domainProviderType 返回域名所属账号的服务商类型代码 */
func domainProviderType(accountID uint) string {
	var account models.Account
	if err := database.DB.Select("type").First(&account, accountID).Error; err != nil {
		return ""
	}
	return account.Type
}

/*
 * OptimizeRunner 优选IP 后台调度器
 */
type OptimizeRunner struct {
	stopCh chan struct{}
}

var optimizeRunner *OptimizeRunner

/* StartOptimizeRunner 启动优选IP 定时调度，由 main 在任务上下文就绪后调用 */
func StartOptimizeRunner(ctx context.Context) {
	optimizeRunner = &OptimizeRunner{stopCh: make(chan struct{})}
	go optimizeRunner.run(ctx)
}

/* StopOptimizeRunner 停止优选IP 定时调度 */
func StopOptimizeRunner() {
	if optimizeRunner != nil {
		close(optimizeRunner.stopCh)
	}
}

func (r *OptimizeRunner) run(ctx context.Context) {
	// 首次检查延迟，避开启动阶段的数据库与缓存初始化
	select {
	case <-r.stopCh:
		return
	case <-ctx.Done():
		return
	case <-time.After(90 * time.Second):
	}
	RunOptimizeTasks(ctx)

	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunOptimizeTasks(ctx)
		}
	}
}

/*
 * RunOptimizeTasks 执行全部启用中的优选任务
 * 由调度器每分钟调用，实际是否执行由 optimize_ip_min 配置的间隔决定，返回本轮是否真正执行
 */
func RunOptimizeTasks(ctx context.Context) bool {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("优选IP任务异常: %v", err)
		}
	}()

	if !optimizeIntervalElapsed() {
		return false
	}

	var tasks []models.OptimizeIP
	database.DB.Where("active = ?", true).Find(&tasks)
	if len(tasks) == 0 {
		return false
	}
	logger.Info("开始执行IP优选任务，共获取到%d个待执行任务", len(tasks))

	executor := NewOptimizeExecutor()
	for i := range tasks {
		task := &tasks[i]
		// 每个任务重建执行器：统计是单任务维度的，沿用同一个实例会把上一个任务的计数累加进来
		executor = NewOptimizeExecutor()
		result, err := executor.Run(ctx, task)
		now := time.Now()
		if err != nil {
			database.DB.Model(&models.OptimizeIP{}).Where("id = ?", task.ID).
				Updates(map[string]interface{}{"status": 2, "err_msg": err.Error(), "update_time": now})
			logger.Error("优选任务%d执行失败：%v", task.ID, err)
			continue
		}
		database.DB.Model(&models.OptimizeIP{}).Where("id = ?", task.ID).
			Updates(map[string]interface{}{"status": 1, "err_msg": "", "update_time": now})
		logger.Info("优选任务%d执行成功：%s", task.ID, result.String())
	}

	// 执行时间只在一轮跑完后写入，失败的中断也不会造成重复执行
	setOptimizeRunTime(time.Now().Format("2006-01-02 15:04:05"))
	return true
}

/* RunOptimizeTask 手动执行单个优选任务 */
func RunOptimizeTask(ctx context.Context, task *models.OptimizeIP) (OptimizeRunResult, error) {
	executor := NewOptimizeExecutor()
	result, err := executor.Run(ctx, task)
	now := time.Now()
	if err != nil {
		database.DB.Model(&models.OptimizeIP{}).Where("id = ?", task.ID).
			Updates(map[string]interface{}{"status": 2, "err_msg": err.Error(), "update_time": now})
		return result, err
	}
	database.DB.Model(&models.OptimizeIP{}).Where("id = ?", task.ID).
		Updates(map[string]interface{}{"status": 1, "err_msg": "", "update_time": now})
	return result, nil
}

/*
 * optimizeIntervalElapsed 判断距上次执行是否已超过配置的间隔
 */
func optimizeIntervalElapsed() bool {
	minutes := 30
	if v := strings.TrimSpace(sysconfig.GetValue(OptimizeCfgMin)); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			minutes = n
		}
	}
	last := strings.TrimSpace(sysconfig.GetValue(OptimizeCfgTime))
	if last == "" {
		return true
	}
	lastTime, err := time.ParseInLocation("2006-01-02 15:04:05", last, time.Local)
	if err != nil {
		return true
	}
	return time.Since(lastTime) >= time.Duration(minutes)*time.Minute
}

/* setOptimizeRunTime 记录本轮执行时间，供间隔判断与 /status 使用 */
func setOptimizeRunTime(value string) {
	var cfg models.SysConfig
	if database.DB.Where("`key` = ?", OptimizeCfgTime).First(&cfg).Error != nil {
		database.DB.Create(&models.SysConfig{Key: OptimizeCfgTime, Value: value})
	} else {
		database.DB.Model(&cfg).Update("value", value)
	}
	sysconfig.Invalidate(OptimizeCfgTime)
}
