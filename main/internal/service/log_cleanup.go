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

func StartLogCleanup() {
	cfg := config.Get()
	if cfg == nil || !cfg.LogCleanup.Enable {
		return
	}

	logCleanupService = &LogCleanupService{
		stopChan: make(chan struct{}),
	}
	go logCleanupService.run()
}

// StopLogCleanup 停止日志清理服务
func StopLogCleanup() {
	if logCleanupService != nil {
		close(logCleanupService.stopChan)
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

	successKeep := cfg.LogCleanup.SuccessKeepCount
	errorKeep := cfg.LogCleanup.ErrorKeepCount

	if successKeep <= 0 {
		successKeep = 1000
	}
	if errorKeep <= 0 {
		errorKeep = 500
	}
	var successCount int64
	database.RequestDB.Model(&models.RequestLog{}).Where("is_error = ?", false).Count(&successCount)
	if successCount > int64(successKeep) {
		_ = successCount - int64(successKeep) // deleteCount (unused but computed)
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
			}
		} else {
		}
	}
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
