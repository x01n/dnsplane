package logstore

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"main/internal/cache"
	"main/internal/logger"
	"main/internal/models"
)

const (
	requestLogKey        = "app:request_logs"
	systemLogKey         = "app:system_logs"
	maxLogSize           = 10000 // 最大保留条数
	requestStatsCacheKey = "logstore:req_stats"
	requestStatsCacheTTL = 2 * time.Minute
)

// requestLogLite 统计扫描用：避免每条反序列化整棵 RequestLog（含大 body）
type requestLogLite struct {
	IsError   bool      `json:"is_error"`
	CreatedAt time.Time `json:"created_at"`
}

var (
	requestStatsComputeMu sync.Mutex
	saveLogTrimCounter    atomic.Uint64
)

/* Store 日志存储（使用 Cache 的 List 操作，Redis 可用时用 Redis，否则回退到内存） */
var Store *LogStore

type LogStore struct {
	c cache.Cache
}

/* Init 初始化日志存储 */
func Init() {
	Store = &LogStore{c: cache.C}
	if cache.C.IsRedis() {
		logger.Info("[LogStore] 使用 Redis 存储请求日志和系统日志")
	} else {
		logger.Info("[LogStore] 使用内存存储请求日志和系统日志（建议配置 Redis）")
	}
}

/* IsRedis 是否使用 Redis 后端 */
func (s *LogStore) IsRedis() bool {
	return s.c.IsRedis()
}

// ==================== 请求日志 ====================

/* SaveRequestLog 保存请求日志 */
func (s *LogStore) SaveRequestLog(log models.RequestLog) {
	data, err := json.Marshal(log)
	if err != nil {
		logger.Error("[LogStore] 序列化请求日志失败: %v", err)
		return
	}
	if err := s.c.LPush(requestLogKey, string(data)); err != nil {
		logger.Error("[LogStore] 保存请求日志失败: %v", err)
		return
	}
	// 降低热路径 Redis QPS：每 64 次写入再检查长度并裁剪
	if saveLogTrimCounter.Add(1)%64 == 0 {
		if length, _ := s.c.LLen(requestLogKey); length > maxLogSize+500 {
			_ = s.c.LTrim(requestLogKey, 0, maxLogSize-1)
			_ = s.c.Delete(requestStatsCacheKey)
		}
	}
}

func (s *LogStore) invalidateRequestStatsCache() {
	_ = s.c.Delete(requestStatsCacheKey)
}

/* QueryRequestLogs 查询请求日志（支持分页和过滤） */
func (s *LogStore) QueryRequestLogs(page, pageSize int, keyword, method, isError, startDate, endDate string) ([]models.RequestLog, int64) {
	// 获取全部数据（Redis List 不支持条件过滤，需要拉取后在内存过滤）
	total, _ := s.c.LLen(requestLogKey)
	if total == 0 {
		return nil, 0
	}

	// 无筛选条件时直接分页取
	hasFilter := keyword != "" || method != "" || isError != "" || startDate != "" || endDate != ""

	if !hasFilter {
		start := int64((page - 1) * pageSize)
		stop := start + int64(pageSize) - 1
		items, err := s.c.LRange(requestLogKey, start, stop)
		if err != nil {
			return nil, 0
		}
		logs := parseRequestLogs(items)
		return logs, total
	}

	// 有筛选条件：需要拉取全部，内存过滤
	items, err := s.c.LRange(requestLogKey, 0, total-1)
	if err != nil {
		return nil, 0
	}

	allLogs := parseRequestLogs(items)
	filtered := filterRequestLogs(allLogs, keyword, method, isError, startDate, endDate)
	filteredTotal := int64(len(filtered))

	// 分页
	start := (page - 1) * pageSize
	if int64(start) >= filteredTotal {
		return nil, filteredTotal
	}
	end := start + pageSize
	if int64(end) > filteredTotal {
		end = int(filteredTotal)
	}
	return filtered[start:end], filteredTotal
}

/* GetRequestByID 根据 request_id 查找 */
func (s *LogStore) GetRequestByID(requestID string) (*models.RequestLog, error) {
	// 从最近的日志中查找（最多搜索 maxLogSize 条）
	total, _ := s.c.LLen(requestLogKey)
	if total == 0 {
		return nil, fmt.Errorf("请求记录不存在")
	}
	batchSize := int64(500)
	for offset := int64(0); offset < total; offset += batchSize {
		end := offset + batchSize - 1
		if end >= total {
			end = total - 1
		}
		items, err := s.c.LRange(requestLogKey, offset, end)
		if err != nil {
			continue
		}
		for _, item := range items {
			var log models.RequestLog
			if json.Unmarshal([]byte(item), &log) == nil && log.RequestID == requestID {
				return &log, nil
			}
		}
	}
	return nil, fmt.Errorf("请求记录不存在")
}

/* GetErrorByID 根据 error_id 查找 */
func (s *LogStore) GetErrorByID(errorID string) (*models.RequestLog, error) {
	total, _ := s.c.LLen(requestLogKey)
	if total == 0 {
		return nil, fmt.Errorf("错误记录不存在")
	}
	batchSize := int64(500)
	for offset := int64(0); offset < total; offset += batchSize {
		end := offset + batchSize - 1
		if end >= total {
			end = total - 1
		}
		items, err := s.c.LRange(requestLogKey, offset, end)
		if err != nil {
			continue
		}
		for _, item := range items {
			var log models.RequestLog
			if json.Unmarshal([]byte(item), &log) == nil && log.ErrorID == errorID {
				return &log, nil
			}
		}
	}
	return nil, fmt.Errorf("错误记录不存在")
}

/* requestStatsResult 请求统计缓存结构 */
type requestStatsResult struct {
	Total        int64               `json:"total"`
	ErrorCnt     int64               `json:"error_cnt"`
	TodayCnt     int64               `json:"today_cnt"`
	TodayError   int64               `json:"today_error"`
	RecentErrors []models.RequestLog `json:"recent_errors"`
}

/*
 * GetRequestStats 获取请求统计（总数/错误数/今日/最近错误）
 * 缓存未命中时全表扫描成本高：互斥合并并发重算 + 2min 缓存 + 轻量 JSON 解析
 */
func (s *LogStore) GetRequestStats() (totalCount, errorCount, todayCount, todayErrorCount int64, recentErrors []models.RequestLog) {
	var cached requestStatsResult
	if s.c.GetJSON(requestStatsCacheKey, &cached) {
		return cached.Total, cached.ErrorCnt, cached.TodayCnt, cached.TodayError, cached.RecentErrors
	}

	requestStatsComputeMu.Lock()
	defer requestStatsComputeMu.Unlock()

	if s.c.GetJSON(requestStatsCacheKey, &cached) {
		return cached.Total, cached.ErrorCnt, cached.TodayCnt, cached.TodayError, cached.RecentErrors
	}

	total, _ := s.c.LLen(requestLogKey)
	if total == 0 {
		return
	}
	totalCount = total
	today := time.Now().Truncate(24 * time.Hour)

	batchSize := int64(500)
	for offset := int64(0); offset < total; offset += batchSize {
		end := offset + batchSize - 1
		if end >= total {
			end = total - 1
		}
		items, _ := s.c.LRange(requestLogKey, offset, end)
		for _, item := range items {
			var lite requestLogLite
			if json.Unmarshal([]byte(item), &lite) != nil {
				continue
			}
			if lite.IsError {
				errorCount++
				if len(recentErrors) < 5 {
					var full models.RequestLog
					if json.Unmarshal([]byte(item), &full) == nil {
						recentErrors = append(recentErrors, full)
					}
				}
			}
			if lite.CreatedAt.After(today) || lite.CreatedAt.Equal(today) {
				todayCount++
				if lite.IsError {
					todayErrorCount++
				}
			}
		}
	}

	_ = s.c.SetJSON(requestStatsCacheKey, requestStatsResult{
		Total: totalCount, ErrorCnt: errorCount,
		TodayCnt: todayCount, TodayError: todayErrorCount,
		RecentErrors: recentErrors,
	}, requestStatsCacheTTL)
	return
}

/* CleanRequestLogs 清理请求日志（按条数截断） */
func (s *LogStore) CleanRequestLogs(keepCount int) int64 {
	if keepCount <= 0 {
		keepCount = maxLogSize
	}
	total, _ := s.c.LLen(requestLogKey)
	if total <= int64(keepCount) {
		return 0
	}
	deleted := total - int64(keepCount)
	s.c.LTrim(requestLogKey, 0, int64(keepCount)-1)
	s.invalidateRequestStatsCache()
	return deleted
}

// ==================== 系统日志（操作日志） ====================

/* SaveSystemLog 保存系统日志 */
func (s *LogStore) SaveSystemLog(log models.Log) {
	data, err := json.Marshal(log)
	if err != nil {
		return
	}
	if err := s.c.LPush(systemLogKey, string(data)); err != nil {
		logger.Error("[LogStore] 保存系统日志失败: %v", err)
		return
	}
	if length, _ := s.c.LLen(systemLogKey); length > maxLogSize+500 {
		s.c.LTrim(systemLogKey, 0, maxLogSize-1)
	}
}

/* QuerySystemLogs 查询系统日志（支持分页和过滤） */
func (s *LogStore) QuerySystemLogs(page, pageSize int, keyword, action, domain string) ([]models.Log, int64) {
	total, _ := s.c.LLen(systemLogKey)
	if total == 0 {
		return nil, 0
	}

	hasFilter := keyword != "" || action != "" || domain != ""

	if !hasFilter {
		start := int64((page - 1) * pageSize)
		stop := start + int64(pageSize) - 1
		items, err := s.c.LRange(systemLogKey, start, stop)
		if err != nil {
			return nil, 0
		}
		logs := parseSystemLogs(items)
		return logs, total
	}

	items, err := s.c.LRange(systemLogKey, 0, total-1)
	if err != nil {
		return nil, 0
	}
	allLogs := parseSystemLogs(items)
	filtered := filterSystemLogs(allLogs, keyword, action, domain)
	filteredTotal := int64(len(filtered))

	start := (page - 1) * pageSize
	if int64(start) >= filteredTotal {
		return nil, filteredTotal
	}
	end := start + pageSize
	if int64(end) > filteredTotal {
		end = int(filteredTotal)
	}
	return filtered[start:end], filteredTotal
}

/* CleanSystemLogs 清理系统日志（按天数截断） */
func (s *LogStore) CleanSystemLogs(keepDays int) int64 {
	total, _ := s.c.LLen(systemLogKey)
	if total == 0 {
		return 0
	}
	cutoff := time.Now().AddDate(0, 0, -keepDays)

	// 从尾部开始找到需要保留的边界
	items, _ := s.c.LRange(systemLogKey, 0, total-1)
	keepIdx := int64(0)
	for i, item := range items {
		var log models.Log
		if json.Unmarshal([]byte(item), &log) == nil && log.CreatedAt.Before(cutoff) {
			keepIdx = int64(i)
			break
		}
		keepIdx = int64(i) + 1
	}
	if keepIdx >= total {
		return 0
	}
	deleted := total - keepIdx
	s.c.LTrim(systemLogKey, 0, keepIdx-1)
	return deleted
}

// ==================== 工具函数 ====================

func parseRequestLogs(items []string) []models.RequestLog {
	logs := make([]models.RequestLog, 0, len(items))
	for _, item := range items {
		var log models.RequestLog
		if json.Unmarshal([]byte(item), &log) == nil {
			logs = append(logs, log)
		}
	}
	return logs
}

func filterRequestLogs(logs []models.RequestLog, keyword, method, isError, startDate, endDate string) []models.RequestLog {
	filtered := make([]models.RequestLog, 0)
	kw := strings.ToLower(keyword)

	var startTime, endTime time.Time
	if startDate != "" {
		startTime, _ = time.Parse("2006-01-02", startDate)
	}
	if endDate != "" {
		t, _ := time.Parse("2006-01-02", endDate)
		endTime = t.Add(24 * time.Hour)
	}

	for _, log := range logs {
		if kw != "" {
			match := strings.Contains(strings.ToLower(log.RequestID), kw) ||
				strings.Contains(strings.ToLower(log.ErrorID), kw) ||
				strings.Contains(strings.ToLower(log.Path), kw) ||
				strings.Contains(strings.ToLower(log.Username), kw) ||
				strings.Contains(strings.ToLower(log.IP), kw)
			if !match {
				continue
			}
		}
		if method != "" && log.Method != method {
			continue
		}
		if isError == "1" && !log.IsError {
			continue
		}
		if isError == "0" && log.IsError {
			continue
		}
		if !startTime.IsZero() && log.CreatedAt.Before(startTime) {
			continue
		}
		if !endTime.IsZero() && log.CreatedAt.After(endTime) {
			continue
		}
		filtered = append(filtered, log)
	}
	return filtered
}

func parseSystemLogs(items []string) []models.Log {
	logs := make([]models.Log, 0, len(items))
	for _, item := range items {
		var log models.Log
		if json.Unmarshal([]byte(item), &log) == nil {
			logs = append(logs, log)
		}
	}
	return logs
}

func filterSystemLogs(logs []models.Log, keyword, action, domain string) []models.Log {
	filtered := make([]models.Log, 0)
	kw := strings.ToLower(keyword)
	for _, log := range logs {
		if kw != "" {
			match := strings.Contains(strings.ToLower(log.Username), kw) ||
				strings.Contains(strings.ToLower(log.Data), kw) ||
				strings.Contains(strings.ToLower(log.Domain), kw) ||
				strings.Contains(strings.ToLower(log.Action), kw)
			if !match {
				continue
			}
		}
		if action != "" && log.Action != action {
			continue
		}
		if domain != "" && !strings.Contains(strings.ToLower(log.Domain), strings.ToLower(domain)) {
			continue
		}
		filtered = append(filtered, log)
	}
	return filtered
}
