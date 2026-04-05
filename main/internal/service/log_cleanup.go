package service

import (
	"time"

	"main/internal/config"
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
)

// LogCleanupService 请求日志清理服务
type LogCleanupService struct {
	stopChan chan struct{}
}

var logCleanupService *LogCleanupService

// StartLogCleanup 启动日志清理服务
func StartLogCleanup() {
	cfg := config.Get()
	if cfg == nil || !cfg.LogCleanup.Enable {
		return
	}

	logCleanupService = &LogCleanupService{
		stopChan: make(chan struct{}),
	}

	go logCleanupService.run()
	logger.Info("请求日志清理服务已启动")
}

// StopLogCleanup 停止日志清理服务
func StopLogCleanup() {
	if logCleanupService != nil {
		close(logCleanupService.stopChan)
		logger.Info("请求日志清理服务已停止")
	}
}

func (s *LogCleanupService) run() {
	// 启动时立即执行一次清理
	s.cleanup()

	cfg := config.Get()
	interval := time.Duration(cfg.LogCleanup.CleanupInterval) * time.Hour
	if interval < time.Hour {
		interval = time.Hour
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.stopChan:
			return
		case <-ticker.C:
			s.cleanup()
		}
	}
}

func (s *LogCleanupService) cleanup() {
	cfg := config.Get()
	if cfg == nil {
		return
	}

	// 注：DMCheckLog 清理已由 database.MaintenanceService 统一处理，此处不再重复

	successKeep := cfg.LogCleanup.SuccessKeepCount
	errorKeep := cfg.LogCleanup.ErrorKeepCount

	if successKeep <= 0 {
		successKeep = 1000
	}
	if errorKeep <= 0 {
		errorKeep = 500
	}

	// 清理成功日志（保留最新的N条）
	var successCount int64
	database.RequestDB.Model(&models.RequestLog{}).Where("is_error = ?", false).Count(&successCount)
	if successCount > int64(successKeep) {
		deleteCount := successCount - int64(successKeep)
		// 获取要保留的最小ID
		var minKeepID uint
		database.RequestDB.Model(&models.RequestLog{}).
			Where("is_error = ?", false).
			Order("id DESC").
			Offset(successKeep).
			Limit(1).
			Pluck("id", &minKeepID)

		if minKeepID > 0 {
			result := database.RequestDB.Where("is_error = ? AND id <= ?", false, minKeepID).Delete(&models.RequestLog{})
			if result.RowsAffected > 0 {
				logger.Info("清理成功请求日志 %d 条", result.RowsAffected)
			}
		} else {
			logger.Info("计划清理成功日志 %d 条", deleteCount)
		}
	}

	// 清理错误日志（保留最新的N条）
	var errorCount int64
	database.RequestDB.Model(&models.RequestLog{}).Where("is_error = ?", true).Count(&errorCount)
	if errorCount > int64(errorKeep) {
		// 获取要保留的最小ID
		var minKeepID uint
		database.RequestDB.Model(&models.RequestLog{}).
			Where("is_error = ?", true).
			Order("id DESC").
			Offset(errorKeep).
			Limit(1).
			Pluck("id", &minKeepID)

		if minKeepID > 0 {
			result := database.RequestDB.Where("is_error = ? AND id <= ?", true, minKeepID).Delete(&models.RequestLog{})
			if result.RowsAffected > 0 {
				logger.Info("清理错误请求日志 %d 条", result.RowsAffected)
			}
		}
	}
}

// CleanupRequestLogs 手动清理请求日志
func CleanupRequestLogs(successKeep, errorKeep int) (int64, int64) {
	var successDeleted, errorDeleted int64

	if successKeep > 0 {
		var minKeepID uint
		database.RequestDB.Model(&models.RequestLog{}).
			Where("is_error = ?", false).
			Order("id DESC").
			Offset(successKeep).
			Limit(1).
			Pluck("id", &minKeepID)

		if minKeepID > 0 {
			result := database.RequestDB.Where("is_error = ? AND id <= ?", false, minKeepID).Delete(&models.RequestLog{})
			successDeleted = result.RowsAffected
		}
	}

	if errorKeep > 0 {
		var minKeepID uint
		database.RequestDB.Model(&models.RequestLog{}).
			Where("is_error = ?", true).
			Order("id DESC").
			Offset(errorKeep).
			Limit(1).
			Pluck("id", &minKeepID)

		if minKeepID > 0 {
			result := database.RequestDB.Where("is_error = ? AND id <= ?", true, minKeepID).Delete(&models.RequestLog{})
			errorDeleted = result.RowsAffected
		}
	}

	return successDeleted, errorDeleted
}
