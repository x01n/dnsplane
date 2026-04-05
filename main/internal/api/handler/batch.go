package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
	"main/internal/service"
	"main/internal/utils"
	"main/internal/whois"

	"github.com/gin-gonic/gin"
)

type BatchRecordReq struct {
	Name   string `json:"name" binding:"required"`
	Type   string `json:"type" binding:"required"`
	Value  string `json:"value" binding:"required"`
	Line   string `json:"line"`
	TTL    int    `json:"ttl"`
	MX     int    `json:"mx"`
	Remark string `json:"remark"`
}

type BatchAddRecordsRequest struct {
	DomainID string           `json:"domain_id" binding:"required"`
	Records  string           `json:"records"`
	Type     string           `json:"type"`
	Line     string           `json:"line"`
	TTL      int              `json:"ttl"`
	Items    []BatchRecordReq `json:"items"`
}

/*
 * BatchAddRecords 批量添加解析记录
 * @route POST /domains/records/batch-add
 * 功能：支持文本模式和结构化模式批量添加 DNS 记录，异步执行
 */
func BatchAddRecords(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req BatchAddRecordsRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	/* 套餐功能检查：批量操作 */
	if !CheckFeature(c, c.GetString("user_id"), "batch_ops") {
		return
	}

	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(c.GetString("user_id"), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	provider, err := getDNSProviderByDomain(&domain)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name
	records := req.Records
	items := req.Items
	defaultTTL := req.TTL
	defaultLine := req.Line
	recordType := req.Type

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		if defaultTTL == 0 {
			defaultTTL = 600
		}
		if defaultLine == "" {
			defaultLine = "default"
		}

		successCount := 0
		failCount := 0

		if records != "" {
			lines := strings.Split(strings.TrimSpace(records), "\n")
			for _, line := range lines {
				line = strings.TrimSpace(line)
				if line == "" {
					continue
				}
				parts := strings.Fields(line)
				if len(parts) < 2 {
					failCount++
					continue
				}
				name := parts[0]
				value := parts[1]
				rType := recordType
				if rType == "" || rType == "auto" {
					rType = detectRecordType(value)
				}
				if _, err := provider.AddDomainRecord(ctx, name, rType, value, defaultLine, defaultTTL, 0, nil, ""); err != nil {
					failCount++
				} else {
					successCount++
				}
			}
		} else {
			for _, rec := range items {
				ttl := rec.TTL
				if ttl == 0 {
					ttl = 600
				}
				line := rec.Line
				if line == "" {
					line = "default"
				}
				if _, err := provider.AddDomainRecord(ctx, rec.Name, rec.Type, rec.Value, line, ttl, rec.MX, nil, rec.Remark); err != nil {
					failCount++
				} else {
					successCount++
				}
			}
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "batch_add_records", domainName, fmt.Sprintf("批量添加记录: 成功%d, 失败%d", successCount, failCount))
	})

	middleware.SuccessMsg(c, "提交成功")
}

/* detectRecordType 根据值自动检测记录类型（A/AAAA/CNAME） */
func detectRecordType(value string) string {
	if ip := net.ParseIP(value); ip != nil {
		if ip.To4() != nil {
			return "A"
		}
		return "AAAA"
	}
	/* 包含冒号可能是缩写 IPv6 */
	if strings.Contains(value, ":") {
		return "AAAA"
	}
	return "CNAME"
}

type BatchEditRecordsRequest struct {
	DomainID  string   `json:"domain_id" binding:"required"`
	RecordIDs []string `json:"record_ids" binding:"required"`
	TTL       *int     `json:"ttl"`
	Line      *string  `json:"line"`
}

/*
 * BatchEditRecords 批量编辑解析记录
 * @route POST /domains/records/batch-edit
 * 功能：异步批量修改 TTL/线路等属性，120 秒超时保护
 */
func BatchEditRecords(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req BatchEditRecordsRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	/* 套餐功能检查：批量操作 */
	if !CheckFeature(c, c.GetString("user_id"), "batch_ops") {
		return
	}

	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(c.GetString("user_id"), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	provider, err := getDNSProviderByDomain(&domain)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name
	recordIDs := req.RecordIDs
	ttlPtr := req.TTL
	linePtr := req.Line

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		successCount := 0
		failCount := 0

		for _, recordID := range recordIDs {
			record, err := provider.GetDomainRecordInfo(ctx, recordID)
			if err != nil {
				failCount++
				continue
			}
			ttl := record.TTL
			if ttlPtr != nil {
				ttl = *ttlPtr
			}
			line := record.Line
			if linePtr != nil {
				line = *linePtr
			}
			if err = provider.UpdateDomainRecord(ctx, recordID, record.Name, record.Type, record.Value, line, ttl, record.MX, nil, record.Remark); err != nil {
				failCount++
			} else {
				successCount++
			}
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "batch_edit_records", domainName, fmt.Sprintf("批量编辑记录: 成功%d, 失败%d", successCount, failCount))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type BatchActionRecordsRequest struct {
	DomainID  string   `json:"domain_id" binding:"required"`
	RecordIDs []string `json:"record_ids" binding:"required"`
	Action    string   `json:"action" binding:"required"`
}

/*
 * BatchActionRecords 批量记录操作
 * @route POST /domains/records/batch-action
 * 功能：异步批量启用/暂停/删除解析记录，120 秒超时保护
 */
func BatchActionRecords(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req BatchActionRecordsRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	/* 套餐功能检查：批量操作 */
	if !CheckFeature(c, c.GetString("user_id"), "batch_ops") {
		return
	}

	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(c.GetString("user_id"), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	provider, err := getDNSProviderByDomain(&domain)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name
	recordIDs := req.RecordIDs
	action := req.Action

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		successCount := 0
		failCount := 0

		for _, recordID := range recordIDs {
			var opErr error
			switch action {
			case "enable", "open":
				opErr = provider.SetDomainRecordStatus(ctx, recordID, true)
			case "disable", "pause":
				opErr = provider.SetDomainRecordStatus(ctx, recordID, false)
			case "delete":
				opErr = provider.DeleteDomainRecord(ctx, recordID)
			}
			if opErr != nil {
				failCount++
			} else {
				successCount++
			}
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "batch_action_records", domainName, fmt.Sprintf("批量操作记录: action=%s, 成功%d, 失败%d", action, successCount, failCount))
	})

	middleware.SuccessMsg(c, "提交成功")
}

/* getDNSProviderByDomain 根据域名模型获取 DNS provider（不依赖 gin.Context） */
func getDNSProviderByDomain(domain *models.Domain) (dns.Provider, error) {
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return nil, err
	}

	var configMap map[string]string
	if err := json.Unmarshal([]byte(account.Config), &configMap); err != nil {
		return nil, err
	}

	return dns.GetProvider(account.Type, configMap, domain.Name, domain.ThirdID)
}

type QueryWhoisRequest struct {
	DomainID string `json:"domain_id" binding:"required"`
}

/*
 * QueryWhois 查询域名 WHOIS 信息
 * @route POST /domains/whois
 * 功能：查询域名注册商、到期时间等 WHOIS 信息
 */
func QueryWhois(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req QueryWhoisRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(c.GetString("user_id"), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()

	info, err := whois.Query(ctx, domain.Name)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	if info.ExpiryDate != nil {
		domain.ExpireTime = info.ExpiryDate
		domain.CheckTime = timePtr(time.Now())
		database.WithContext(c).Save(&domain)
	}

	middleware.SuccessResponse(c, info)
}

/* timePtr 将 time.Time 转为指针（用于可选时间字段赋值） */
func timePtr(t time.Time) *time.Time {
	return &t
}
