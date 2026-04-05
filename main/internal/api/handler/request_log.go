package handler

import (
	"sync"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/logstore"
	"main/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

/* useRedisLogs 写入侧是否同步到 Redis（读取统一走 SQLite，避免 LRANGE 全量反序列化） */
func useRedisLogs() bool {
	return logstore.Store != nil && logstore.Store.IsRedis()
}

// 列表页不拉取 response/body 等大字段，降低 SQLite IO 与加密载荷体积
const requestLogListSelect = "id, request_id, error_id, user_id, username, method, path, ip, user_agent, status_code, duration, is_error, error_msg, created_at"

const requestLogRecentErrSelect = "id, request_id, path, method, status_code, error_msg, created_at"

const requestStatsCacheTTL = 60 * time.Second

var requestStatsCache struct {
	mu    sync.RWMutex
	until time.Time
	data  gin.H
}

func tryRequestStatsCache() (gin.H, bool) {
	requestStatsCache.mu.RLock()
	defer requestStatsCache.mu.RUnlock()
	if requestStatsCache.data != nil && time.Now().Before(requestStatsCache.until) {
		return requestStatsCache.data, true
	}
	return nil, false
}

func setRequestStatsCache(h gin.H) {
	requestStatsCache.mu.Lock()
	defer requestStatsCache.mu.Unlock()
	requestStatsCache.data = h
	requestStatsCache.until = time.Now().Add(requestStatsCacheTTL)
}

func invalidateRequestStatsCache() {
	requestStatsCache.mu.Lock()
	defer requestStatsCache.mu.Unlock()
	requestStatsCache.data = nil
	requestStatsCache.until = time.Time{}
}

type GetRequestLogsRequest struct {
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	Keyword   string `json:"keyword"`
	IsError   string `json:"is_error"`
	Method    string `json:"method"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

func applyRequestLogFilters(query *gorm.DB, req *GetRequestLogsRequest) *gorm.DB {
	if req.Keyword != "" {
		query = query.Where("request_id LIKE ? OR error_id LIKE ? OR path LIKE ? OR username LIKE ? OR ip LIKE ?",
			"%"+req.Keyword+"%", "%"+req.Keyword+"%", "%"+req.Keyword+"%", "%"+req.Keyword+"%", "%"+req.Keyword+"%")
	}
	if req.Method != "" {
		query = query.Where("method = ?", req.Method)
	}
	if req.IsError == "1" {
		query = query.Where("is_error = ?", true)
	} else if req.IsError == "0" {
		query = query.Where("is_error = ?", false)
	}
	if req.StartDate != "" {
		if t, err := time.Parse("2006-01-02", req.StartDate); err == nil {
			query = query.Where("created_at >= ?", t)
		}
	}
	if req.EndDate != "" {
		if t, err := time.Parse("2006-01-02", req.EndDate); err == nil {
			query = query.Where("created_at < ?", t.Add(24*time.Hour))
		}
	}
	return query
}

/*
 * GetRequestLogs 获取请求日志列表
 * @route POST /request-logs/list
 * 功能：分页查询 HTTP 请求日志，支持按方法/错误/日期筛选
 * 权限：仅管理员
 */
func GetRequestLogs(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req GetRequestLogsRequest
	_ = middleware.BindJSONFlexible(c, &req)

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 200 {
		req.PageSize = 200
	}

	query := database.RequestDB.Model(&models.RequestLog{})
	query = applyRequestLogFilters(query, &req)

	var total int64
	query.Count(&total)

	var logs []models.RequestLog
	query.Order("id DESC").Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).
		Select(requestLogListSelect).Find(&logs)

	middleware.SuccessResponse(c, gin.H{"total": total, "list": logs})
}

type GetRequestByIDRequest struct {
	RequestID string `json:"request_id" binding:"required"`
}

/*
 * GetRequestByID 根据请求 ID 查询日志详情
 * @route POST /request-logs/detail
 * 功能：根据 X-Request-ID 查询完整的请求日志
 * 权限：仅管理员
 */
func GetRequestByID(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req GetRequestByIDRequest
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.RequestID == "" {
		middleware.ErrorResponse(c, "请求ID不能为空")
		return
	}

	var log models.RequestLog
	if err := database.RequestDB.Where("request_id = ?", req.RequestID).First(&log).Error; err != nil {
		middleware.ErrorResponse(c, "请求记录不存在")
		return
	}

	middleware.SuccessResponse(c, log)
}

type GetErrorByIDRequest struct {
	ErrorID string `json:"error_id" binding:"required"`
}

/*
 * GetErrorByID 根据错误 ID 查询日志详情
 * @route POST /request-logs/error
 * 功能：根据 X-Error-ID 查询错误请求的完整日志
 * 权限：仅管理员
 */
func GetErrorByID(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req GetErrorByIDRequest
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ErrorID == "" {
		middleware.ErrorResponse(c, "错误ID不能为空")
		return
	}

	var log models.RequestLog
	if err := database.RequestDB.Where("error_id = ?", req.ErrorID).First(&log).Error; err != nil {
		middleware.ErrorResponse(c, "错误记录不存在")
		return
	}

	middleware.SuccessResponse(c, log)
}

/*
 * GetRequestStats 获取请求统计数据
 * @route POST /request-logs/stats
 * 功能：返回总请求数/成功数/错误数/平均响应时间等统计
 * 权限：仅管理员
 */
func GetRequestStats(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	if h, ok := tryRequestStatsCache(); ok {
		middleware.SuccessResponse(c, h)
		return
	}

	today := time.Now().Truncate(24 * time.Hour)

	type reqStats struct {
		Total      int64 `gorm:"column:total"`
		ErrorCnt   int64 `gorm:"column:error_cnt"`
		TodayCnt   int64 `gorm:"column:today_cnt"`
		TodayError int64 `gorm:"column:today_error"`
	}
	var rs reqStats
	database.RequestDB.Model(&models.RequestLog{}).Select(
		"COUNT(*) as total, "+
			"SUM(CASE WHEN is_error=1 THEN 1 ELSE 0 END) as error_cnt, "+
			"SUM(CASE WHEN created_at >= ? THEN 1 ELSE 0 END) as today_cnt, "+
			"SUM(CASE WHEN created_at >= ? AND is_error=1 THEN 1 ELSE 0 END) as today_error",
		today, today,
	).Scan(&rs)
	totalCount, errorCount, todayCount, todayErrorCount := rs.Total, rs.ErrorCnt, rs.TodayCnt, rs.TodayError

	var recentErrors []models.RequestLog
	database.RequestDB.Where("is_error = ?", true).Order("id DESC").Limit(5).
		Select(requestLogRecentErrSelect).Find(&recentErrors)

	h := gin.H{
		"total_count":       totalCount,
		"error_count":       errorCount,
		"today_count":       todayCount,
		"today_error_count": todayErrorCount,
		"recent_errors":     recentErrors,
	}
	setRequestStatsCache(h)
	middleware.SuccessResponse(c, h)
}

type CleanRequestLogsRequest struct {
	Days             int    `json:"days"`
	BeforeDate       string `json:"before_date"`
	SuccessKeepCount int    `json:"success_keep_count"`
	ErrorKeepCount   int    `json:"error_keep_count"`
}

func cleanRequestLogsSQLite(req *CleanRequestLogsRequest) int64 {
	var totalDeleted int64

	if req.BeforeDate != "" {
		cutoff, err := time.Parse("2006-01-02", req.BeforeDate)
		if err == nil {
			result := database.RequestDB.Where("created_at < ?", cutoff).Delete(&models.RequestLog{})
			totalDeleted += result.RowsAffected
		}
	} else if req.Days > 0 {
		cutoff := time.Now().AddDate(0, 0, -req.Days)
		result := database.RequestDB.Where("created_at < ?", cutoff).Delete(&models.RequestLog{})
		totalDeleted += result.RowsAffected
	}

	if req.SuccessKeepCount > 0 {
		var minKeepID uint
		database.RequestDB.Model(&models.RequestLog{}).
			Where("is_error = ?", false).
			Order("id DESC").
			Offset(req.SuccessKeepCount).
			Limit(1).
			Pluck("id", &minKeepID)

		if minKeepID > 0 {
			result := database.RequestDB.Where("is_error = ? AND id <= ?", false, minKeepID).Delete(&models.RequestLog{})
			totalDeleted += result.RowsAffected
		}
	}

	if req.ErrorKeepCount > 0 {
		var minKeepID uint
		database.RequestDB.Model(&models.RequestLog{}).
			Where("is_error = ?", true).
			Order("id DESC").
			Offset(req.ErrorKeepCount).
			Limit(1).
			Pluck("id", &minKeepID)

		if minKeepID > 0 {
			result := database.RequestDB.Where("is_error = ? AND id <= ?", true, minKeepID).Delete(&models.RequestLog{})
			totalDeleted += result.RowsAffected
		}
	}

	return totalDeleted
}

/*
 * CleanRequestLogs 清理请求日志
 * @route POST /request-logs/clean
 * 功能：按保留数量清理成功/错误请求日志
 * 权限：仅管理员
 */
func CleanRequestLogs(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req CleanRequestLogsRequest
	_ = middleware.BindJSONFlexible(c, &req)

	if useRedisLogs() {
		keepCount := req.SuccessKeepCount
		if keepCount <= 0 {
			keepCount = 2000
		}
		logstore.Store.CleanRequestLogs(keepCount)
	}

	deleted := cleanRequestLogsSQLite(&req)
	invalidateRequestStatsCache()
	middleware.SuccessResponse(c, gin.H{"msg": "清理完成", "deleted": deleted})
}
