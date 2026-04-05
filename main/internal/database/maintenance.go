package database

import (
	"database/sql"
	"fmt"
	"main/internal/logger"
	"main/internal/models"
	"sync"
	"time"

	"gorm.io/gorm"
)

// MaintenanceConfig 数据库维护配置
// 可在系统设置中通过 SysConfig 表动态配置（key 前缀: maint_）
type MaintenanceConfig struct {
	OperationLogDays   int           // 操作日志保留天数，默认90
	CertLogDays        int           // 证书日志保留天数，默认60
	MonitorLogDays     int           // 容灾切换日志保留天数，默认60
	CheckLogHours      int           // 监控检查日志保留小时数，默认72
	RequestLogDays     int           // 请求日志保留天数，默认7
	RequestSuccessKeep int           // 保留成功请求日志条数，默认2000
	RequestErrorKeep   int           // 保留错误请求日志条数，默认1000
	VacuumIntervalH    int           // VACUUM执行间隔(小时)，默认24
	VacuumInterval     time.Duration `json:"-"` // 计算得出
}

// SysConfig 表中的 key 名映射
const (
	KeyOperationLogDays   = "maint_operation_log_days"
	KeyCertLogDays        = "maint_cert_log_days"
	KeyMonitorLogDays     = "maint_monitor_log_days"
	KeyCheckLogHours      = "maint_check_log_hours"
	KeyRequestLogDays     = "maint_request_log_days"
	KeyRequestSuccessKeep = "maint_request_success_keep"
	KeyRequestErrorKeep   = "maint_request_error_keep"
	KeyVacuumIntervalH    = "maint_vacuum_interval_h"
)

// MaintenanceConfigKeys 所有维护配置key列表（供系统设置页面使用）
var MaintenanceConfigKeys = []string{
	KeyOperationLogDays, KeyCertLogDays, KeyMonitorLogDays, KeyCheckLogHours,
	KeyRequestLogDays, KeyRequestSuccessKeep, KeyRequestErrorKeep, KeyVacuumIntervalH,
}

// DefaultMaintenanceConfig 返回默认配置
func DefaultMaintenanceConfig() MaintenanceConfig {
	return MaintenanceConfig{
		OperationLogDays:   90,
		CertLogDays:        60,
		MonitorLogDays:     60,
		CheckLogHours:      72,
		RequestLogDays:     7,
		RequestSuccessKeep: 2000,
		RequestErrorKeep:   1000,
		VacuumIntervalH:    24,
		VacuumInterval:     24 * time.Hour,
	}
}

// LoadMaintenanceConfig 从 SysConfig 数据库表加载维护配置（未配置的用默认值）
func LoadMaintenanceConfig() MaintenanceConfig {
	cfg := DefaultMaintenanceConfig()
	if DB == nil {
		return cfg
	}

	var configs []models.SysConfig
	DB.Where("`key` LIKE ?", "maint_%").Find(&configs)

	for _, c := range configs {
		var val int
		if _, err := fmt.Sscanf(c.Value, "%d", &val); err != nil || val <= 0 {
			continue
		}
		switch c.Key {
		case KeyOperationLogDays:
			cfg.OperationLogDays = val
		case KeyCertLogDays:
			cfg.CertLogDays = val
		case KeyMonitorLogDays:
			cfg.MonitorLogDays = val
		case KeyCheckLogHours:
			cfg.CheckLogHours = val
		case KeyRequestLogDays:
			cfg.RequestLogDays = val
		case KeyRequestSuccessKeep:
			cfg.RequestSuccessKeep = val
		case KeyRequestErrorKeep:
			cfg.RequestErrorKeep = val
		case KeyVacuumIntervalH:
			cfg.VacuumIntervalH = val
		}
	}

	cfg.VacuumInterval = time.Duration(cfg.VacuumIntervalH) * time.Hour
	return cfg
}

// MaintenanceService 数据库维护服务
type MaintenanceService struct {
	config     MaintenanceConfig
	stopChan   chan struct{}
	wg         sync.WaitGroup
	lastVacuum time.Time
}

var maintService *MaintenanceService

// StartMaintenance 启动数据库维护服务
func StartMaintenance(cfg MaintenanceConfig) {
	if maintService != nil {
		return
	}

	// 启动前先清理无效数据
	cleanInvalidTasks()

	// 用传入的配置兜底，然后尝试从数据库加载覆盖
	dbCfg := LoadMaintenanceConfig()
	cfg = dbCfg
	if cfg.VacuumInterval <= 0 {
		cfg.VacuumInterval = 24 * time.Hour
	}

	maintService = &MaintenanceService{
		config:   cfg,
		stopChan: make(chan struct{}),
	}
	maintService.wg.Add(1)
	go maintService.run()
	logger.Info("[Maintenance] 数据库维护服务已启动 (清理间隔=1h, VACUUM间隔=%dh)", cfg.VacuumIntervalH)
}

// cleanInvalidTasks 启动时清理无效的监控任务（DomainID=0 或 RecordID为空）
func cleanInvalidTasks() {
	if DB == nil {
		return
	}

	// 禁用无效任务（did=0 或 record_id 为空）并记录
	var invalidCount int64
	DB.Model(&models.DMTask{}).
		Where("(did = 0 OR did IS NULL OR record_id = '' OR record_id IS NULL) AND active = ?", true).
		Count(&invalidCount)

	if invalidCount > 0 {
		DB.Model(&models.DMTask{}).
			Where("did = 0 OR did IS NULL OR record_id = '' OR record_id IS NULL").
			Update("active", false)
		logger.Warn("[Maintenance] 发现 %d 个无效监控任务（域名ID或记录ID为空），已自动禁用", invalidCount)
	}
}

// StopMaintenance 停止数据库维护服务
func StopMaintenance() {
	if maintService != nil {
		close(maintService.stopChan)
		maintService.wg.Wait()
		maintService = nil
		logger.Info("[Maintenance] 数据库维护服务已停止")
	}
}

func (m *MaintenanceService) run() {
	defer m.wg.Done()

	// 启动后延迟30秒执行首次维护，避免启动期IO压力
	select {
	case <-m.stopChan:
		return
	case <-time.After(30 * time.Second):
	}

	m.runCleanup()
	m.runVacuumAndOptimize()

	// 每小时执行轻量清理
	ticker := time.NewTicker(1 * time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-m.stopChan:
			return
		case <-ticker.C:
			m.runCleanup()
			vacuumInterval := m.config.VacuumInterval
			if vacuumInterval <= 0 {
				vacuumInterval = 24 * time.Hour
			}
			if time.Since(m.lastVacuum) >= vacuumInterval {
				m.runVacuumAndOptimize()
			}
		}
	}
}

// ==================== 清理逻辑 ====================

func (m *MaintenanceService) runCleanup() {
	// 每次清理前重新从数据库加载配置（管理员可能已修改）
	m.config = LoadMaintenanceConfig()

	// 操作日志 (LogDB)
	if LogDB != nil {
		cutoff := time.Now().AddDate(0, 0, -m.config.OperationLogDays)
		if r := LogDB.Where("created_at < ?", cutoff).Delete(&models.Log{}); r.RowsAffected > 0 {
			logger.Info("[Maintenance] 清理操作日志 %d 条 (保留%d天)", r.RowsAffected, m.config.OperationLogDays)
		}
	}

	// 证书日志 (LogDB)
	if LogDB != nil {
		cutoff := time.Now().AddDate(0, 0, -m.config.CertLogDays)
		if r := LogDB.Where("created_at < ?", cutoff).Delete(&models.CertLog{}); r.RowsAffected > 0 {
			logger.Info("[Maintenance] 清理证书日志 %d 条 (保留%d天)", r.RowsAffected, m.config.CertLogDays)
		}
	}

	// 监控检查日志 — 高频数据，按小时清理 (LogDB)
	if LogDB != nil {
		cutoff := time.Now().Add(-time.Duration(m.config.CheckLogHours) * time.Hour)
		if r := LogDB.Where("created_at < ?", cutoff).Delete(&models.DMCheckLog{}); r.RowsAffected > 0 {
			logger.Info("[Maintenance] 清理监控检查日志 %d 条 (保留%dh)", r.RowsAffected, m.config.CheckLogHours)
		}
	}

	// 容灾切换日志 (主DB)
	if DB != nil {
		cutoff := time.Now().AddDate(0, 0, -m.config.MonitorLogDays)
		if r := DB.Where("created_at < ?", cutoff).Delete(&models.DMLog{}); r.RowsAffected > 0 {
			logger.Info("[Maintenance] 清理容灾切换日志 %d 条 (保留%d天)", r.RowsAffected, m.config.MonitorLogDays)
		}
	}

	// 请求日志 (RequestDB)
	if RequestDB != nil {
		// 按天数清理
		cutoff := time.Now().AddDate(0, 0, -m.config.RequestLogDays)
		if r := RequestDB.Where("created_at < ?", cutoff).Delete(&models.RequestLog{}); r.RowsAffected > 0 {
			logger.Info("[Maintenance] 清理过期请求日志 %d 条 (保留%d天)", r.RowsAffected, m.config.RequestLogDays)
		}

		// 按条数清理成功日志
		trimRequestLogs(RequestDB, false, m.config.RequestSuccessKeep, "成功")
		// 按条数清理错误日志
		trimRequestLogs(RequestDB, true, m.config.RequestErrorKeep, "错误")
	}
}

// trimRequestLogs 按条数限制清理请求日志
func trimRequestLogs(db *gorm.DB, isError bool, keepCount int, label string) {
	var count int64
	db.Model(&models.RequestLog{}).Where("is_error = ?", isError).Count(&count)
	if count <= int64(keepCount) {
		return
	}
	var minKeepID uint
	db.Model(&models.RequestLog{}).
		Where("is_error = ?", isError).
		Order("id DESC").
		Offset(keepCount).
		Limit(1).
		Pluck("id", &minKeepID)
	if minKeepID > 0 {
		if r := db.Where("is_error = ? AND id <= ?", isError, minKeepID).Delete(&models.RequestLog{}); r.RowsAffected > 0 {
			logger.Info("[Maintenance] 清理多余%s请求日志 %d 条 (保留%d条)", label, r.RowsAffected, keepCount)
		}
	}
}

// ==================== VACUUM 与优化 ====================

func (m *MaintenanceService) runVacuumAndOptimize() {
	logger.Debug("[Maintenance] 开始数据库压缩与优化...")
	start := time.Now()

	optimizeGormDB(DB, "主数据库")
	optimizeGormDB(LogDB, "日志数据库")
	optimizeGormDB(RequestDB, "请求日志数据库")

	m.lastVacuum = time.Now()
	invalidateMaintenanceStatsCache()
	logger.Debug("[Maintenance] 数据库压缩与优化完成, 耗时 %v", time.Since(start))
}

// optimizeGormDB 对GORM数据库执行 WAL checkpoint + ANALYZE + optimize + VACUUM
func optimizeGormDB(gormDB *gorm.DB, name string) {
	if gormDB == nil {
		return
	}
	sqlDB, err := gormDB.DB()
	if err != nil {
		logger.Warn("[Maintenance] %s: 获取SQL连接失败: %v", name, err)
		return
	}

	sizeBefore := getDBSize(sqlDB)

	// 1. WAL Checkpoint — 将WAL日志合并到主数据库文件
	sqlDB.Exec("PRAGMA wal_checkpoint(TRUNCATE)")

	// 2. ANALYZE — 更新查询优化器的统计信息
	sqlDB.Exec("ANALYZE")

	// 3. PRAGMA optimize — SQLite自动优化需要维护的索引
	sqlDB.Exec("PRAGMA optimize")

	// 4. VACUUM — 压缩数据库文件，回收已删除数据的空间
	if _, err := sqlDB.Exec("VACUUM"); err != nil {
		logger.Warn("[Maintenance] %s: VACUUM 失败: %v", name, err)
	}

	// 5. VACUUM 可能重置 journal_mode，恢复WAL模式
	sqlDB.Exec("PRAGMA journal_mode=WAL")

	sizeAfter := getDBSize(sqlDB)
	saved := sizeBefore - sizeAfter
	if saved > 1024 { // >1KB才报告
		logger.Debug("[Maintenance] %s: %s → %s (节省 %s)", name, formatBytes(sizeBefore), formatBytes(sizeAfter), formatBytes(saved))
	} else {
		logger.Debug("[Maintenance] %s: %s (已是最优)", name, formatBytes(sizeAfter))
	}
}

// ==================== 统计与工具 ====================

var (
	maintenanceStatsMu   sync.RWMutex
	maintenanceStatsSnap map[string]interface{}
	maintenanceStatsAt   time.Time
	maintenanceStatsTTL  = 25 * time.Second
)

func invalidateMaintenanceStatsCache() {
	maintenanceStatsMu.Lock()
	maintenanceStatsSnap = nil
	maintenanceStatsMu.Unlock()
}

// GetMaintenanceStats 获取数据库维护统计（供API调用，短时缓存避免系统信息页对每表 COUNT）
func GetMaintenanceStats() map[string]interface{} {
	maintenanceStatsMu.RLock()
	if maintenanceStatsSnap != nil && time.Since(maintenanceStatsAt) < maintenanceStatsTTL {
		out := maintenanceStatsSnap
		maintenanceStatsMu.RUnlock()
		return out
	}
	maintenanceStatsMu.RUnlock()

	maintenanceStatsMu.Lock()
	defer maintenanceStatsMu.Unlock()
	if maintenanceStatsSnap != nil && time.Since(maintenanceStatsAt) < maintenanceStatsTTL {
		return maintenanceStatsSnap
	}
	maintenanceStatsSnap = buildMaintenanceStats()
	maintenanceStatsAt = time.Now()
	return maintenanceStatsSnap
}

func buildMaintenanceStats() map[string]interface{} {
	stats := map[string]interface{}{}

	if DB != nil {
		stats["main_db"] = gormDBStats(DB, "主数据库")
	}
	if LogDB != nil {
		stats["log_db"] = gormDBStats(LogDB, "日志数据库")
	}
	if RequestDB != nil {
		stats["request_db"] = gormDBStats(RequestDB, "请求日志数据库")
	}

	if maintService != nil {
		cfg := maintService.config
		stats["config"] = map[string]interface{}{
			"operation_log_days":   cfg.OperationLogDays,
			"cert_log_days":        cfg.CertLogDays,
			"monitor_log_days":     cfg.MonitorLogDays,
			"check_log_hours":      cfg.CheckLogHours,
			"request_log_days":     cfg.RequestLogDays,
			"request_success_keep": cfg.RequestSuccessKeep,
			"request_error_keep":   cfg.RequestErrorKeep,
			"vacuum_interval_h":    cfg.VacuumIntervalH,
		}
		if !maintService.lastVacuum.IsZero() {
			stats["last_vacuum"] = maintService.lastVacuum.Format("2006-01-02 15:04:05")
			stats["next_vacuum"] = maintService.lastVacuum.Add(cfg.VacuumInterval).Format("2006-01-02 15:04:05")
		}
	}
	return stats
}

func gormDBStats(gormDB *gorm.DB, name string) map[string]interface{} {
	sqlDB, err := gormDB.DB()
	if err != nil {
		return map[string]interface{}{"name": name, "error": err.Error()}
	}
	info := map[string]interface{}{
		"name":      name,
		"size":      getDBSize(sqlDB),
		"size_text": formatBytes(getDBSize(sqlDB)),
	}

	// 空闲页
	var freePages, pageSize int64
	sqlDB.QueryRow("PRAGMA freelist_count").Scan(&freePages)
	sqlDB.QueryRow("PRAGMA page_size").Scan(&pageSize)
	info["reclaimable"] = freePages * pageSize
	info["reclaimable_text"] = formatBytes(freePages * pageSize)

	// 表行数：用 ANALYZE 产生的 sqlite_stat1，避免对每个表执行 COUNT(*)
	tables := map[string]int64{}
	trows, err := sqlDB.Query(`SELECT tbl, stat FROM sqlite_stat1 WHERE idx IS NULL ORDER BY tbl`)
	if err == nil {
		defer trows.Close()
		for trows.Next() {
			var tbl, statStr string
			if trows.Scan(&tbl, &statStr) != nil {
				continue
			}
			var n int64
			_, _ = fmt.Sscanf(statStr, "%d", &n)
			tables[tbl] = n
		}
	}
	info["tables"] = tables
	info["tables_approx"] = true
	return info
}

func getDBSize(sqlDB *sql.DB) int64 {
	var pageCount, pageSize int64
	sqlDB.QueryRow("PRAGMA page_count").Scan(&pageCount)
	sqlDB.QueryRow("PRAGMA page_size").Scan(&pageSize)
	return pageCount * pageSize
}

func formatBytes(b int64) string {
	switch {
	case b < 1024:
		return fmt.Sprintf("%dB", b)
	case b < 1024*1024:
		return fmt.Sprintf("%.1fKB", float64(b)/1024)
	case b < 1024*1024*1024:
		return fmt.Sprintf("%.2fMB", float64(b)/1024/1024)
	default:
		return fmt.Sprintf("%.2fGB", float64(b)/1024/1024/1024)
	}
}
