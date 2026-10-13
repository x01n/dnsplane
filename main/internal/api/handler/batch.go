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
	DomainID string           `json:"domain_id"`
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
	if !CheckFeature(c, "batch_ops") {
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
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

	/*
	 * 子域校验：本接口的文本模式一行是「rr value」、结构模式带 Name，
	 * 两者都直接落成写入目标，故从请求里取 rr 即可判定，无需回查云端
	 */
	targets := make([]string, 0, len(req.Items))
	for _, item := range req.Items {
		targets = append(targets, item.Name)
	}
	for _, line := range strings.Split(req.Records, "\n") {
		if fields := strings.Fields(strings.TrimSpace(line)); len(fields) >= 2 {
			targets = append(targets, fields[0])
		}
	}
	if !checkBatchRecordSubDomain(c, &domain, c.GetString("user_id"), c.GetInt("level"), batchRecordAuthReq{Targets: targets}) {
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
	batchUID := currentUIDUint(c)
	batchLevel := c.GetInt("level")
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
		deniedCount := 0

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
				// 子域名前缀占用：批量里逐条判，被占的跳过而不整批失败
				if service.CheckSubdomainClaim(&domain, name, batchUID, batchLevel) != service.ClaimGranted {
					deniedCount++
					continue
				}
				recordID, err := provider.AddDomainRecord(ctx, name, rType, value, defaultLine, defaultTTL, 0, nil, "")
				if err != nil {
					failCount++
				} else {
					successCount++
					service.RegisterSubdomainClaim(&domain, name, batchUID)
					upsertBatchLocalRecord(domain.ID, domainName, recordID, name, rType, value, defaultLine, defaultTTL, 0, "")
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
				if service.CheckSubdomainClaim(&domain, rec.Name, batchUID, batchLevel) != service.ClaimGranted {
					deniedCount++
					continue
				}
				recordID, err := provider.AddDomainRecord(ctx, rec.Name, rec.Type, rec.Value, line, ttl, rec.MX, nil, rec.Remark)
				if err != nil {
					failCount++
				} else {
					successCount++
					service.RegisterSubdomainClaim(&domain, rec.Name, batchUID)
					upsertBatchLocalRecord(domain.ID, domainName, recordID, rec.Name, rec.Type, rec.Value, line, ttl, rec.MX, rec.Remark)
				}
			}
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "batch_add_records", domainName,
			fmt.Sprintf("批量添加记录: 成功%d, 失败%d, 子域名被占%d", successCount, failCount, deniedCount))
	})

	middleware.SuccessMsg(c, "提交成功")
}

/*
 * upsertBatchLocalRecord 批量新增成功后回填本地副本
 *
 * 批量的文本模式与结构模式字段形态不同（前者一行 "rr value"，后者带 Name/Type），
 * 但落库的目标结构一致，故共用一个助手而不是在两处各写一份 Create
 */
func upsertBatchLocalRecord(domainID uint, domainName, recordID, name, recordType, value, line string, ttl, mx int, remark string) {
	service.UpsertLocalRecord(&models.DNSRecord{
		DomainID: domainID, RecordID: recordID, Name: name, Type: recordType,
		Value: value, Line: line, TTL: ttl, MX: mx,
		Remark: remark, Status: "enable", Domain: domainName,
	})
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
	DomainID  string   `json:"domain_id"`
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
	if !CheckFeature(c, "batch_ops") {
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
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

	/* 子域校验：请求里只有 record_id，真实 rr 由云端记录反查，见 batch_authz.go */
	if !checkBatchRecordSubDomain(c, &domain, c.GetString("user_id"), c.GetInt("level"), batchRecordAuthReq{RecordIDs: req.RecordIDs}) {
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
				if local, ok := service.LocalRecord(domain.ID, recordID); ok {
					local.Line, local.TTL = line, ttl
					service.UpsertLocalRecord(&local)
				}
			}
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "batch_edit_records", domainName, fmt.Sprintf("批量编辑记录: 成功%d, 失败%d", successCount, failCount))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type BatchActionRecordsRequest struct {
	DomainID  string   `json:"domain_id"`
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
	if !CheckFeature(c, "batch_ops") {
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
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

	/* 子域校验：同 batch/edit，批量启用/暂停/删除都可能落在未授权子域上 */
	if !checkBatchRecordSubDomain(c, &domain, c.GetString("user_id"), c.GetInt("level"), batchRecordAuthReq{RecordIDs: req.RecordIDs}) {
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
			status := ""
			switch action {
			case "enable", "open":
				opErr = provider.SetDomainRecordStatus(ctx, recordID, true)
				status = "enable"
			case "disable", "pause":
				opErr = provider.SetDomainRecordStatus(ctx, recordID, false)
				status = "disable"
			case "delete":
				opErr = provider.DeleteDomainRecord(ctx, recordID)
			}
			if opErr != nil {
				failCount++
				continue
			}
			successCount++
			if action == "delete" {
				/*
				 * 释放占用前先取前缀：本地副本删掉后就查不到它的 name 了，
				 * 与 DeleteRecord 同一条路径
				 *
				 * 不释放的话该前缀会被永久锁死（他人建不了同前缀的记录），
				 * 且持续占住创建者一条 records 额度
				 */
				releasedPrefix := ""
				if local, ok := service.LocalRecord(domain.ID, recordID); ok {
					releasedPrefix = local.Name
				}
				service.DeleteLocalRecord(domain.ID, recordID)
				if releasedPrefix != "" {
					service.ReleaseSubdomainClaimIfEmpty(domain.ID, releasedPrefix)
				}
			} else if local, ok := service.LocalRecord(domain.ID, recordID); ok {
				local.Status = status
				service.UpsertLocalRecord(&local)
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
	DomainID string `json:"domain_id"`
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
		req.DomainID = c.Param("id")
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
