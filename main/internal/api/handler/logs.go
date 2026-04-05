package handler

import (
	"strconv"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

type GetDomainLogsRequest struct {
	DomainID  string `json:"domain_id" binding:"required"`
	Page      int    `json:"page"`
	PageSize  int    `json:"page_size"`
	Keyword   string `json:"keyword"`
	StartDate string `json:"start_date"`
	EndDate   string `json:"end_date"`
}

/*
 * GetDomainLogs 获取域名操作日志
 * @route POST /domains/logs
 * 功能：优先从 DNS 服务商查询操作日志，失败时降级到本地日志数据库
 */
func GetDomainLogs(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req GetDomainLogsRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 200 {
		req.PageSize = 200
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限查看该域名日志")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}

	result, err := provider.GetDomainRecordLog(c.Request.Context(), req.Page, req.PageSize, req.Keyword, req.StartDate, req.EndDate)
	if err != nil {
		var localLogs []models.Log
		var total int64

		query := database.LogDB.Model(&models.Log{}).Where("domain = ?", domain.Name)
		if req.Keyword != "" {
			query = query.Where("data LIKE ?", "%"+req.Keyword+"%")
		}

		query.Count(&total)
		query.Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Order("id desc").Find(&localLogs)

		middleware.SuccessResponse(c, gin.H{"total": total, "list": localLogs, "source": "local"})
		return
	}

	middleware.SuccessResponse(c, gin.H{"total": result.Total, "list": result.Records, "source": "provider"})
}
