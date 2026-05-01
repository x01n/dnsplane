package handler

import (
	"context"
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

	"github.com/gin-gonic/gin"
)

type domainExtListRequest struct {
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"page_size" form:"page_size"`
	Keyword  string `json:"keyword" form:"keyword"`
	Sub      string `json:"sub" form:"sub"`
	Value    string `json:"value" form:"value"`
	Type     string `json:"type" form:"type"`
	Line     string `json:"line" form:"line"`
	Status   string `json:"status" form:"status"`
}

type addDomainAliasRequest struct {
	Name string `json:"name" binding:"required"`
}

type updateRecordWeightRequest struct {
	DomainID string `json:"domain_id"`
	RecordID string `json:"record_id" binding:"required"`
	Weight   int    `json:"weight"`
}

func loadDomainForAction(c *gin.Context) (*models.Domain, bool) {
	id := c.Param("id")
	if id == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return nil, false
	}
	var domain models.Domain
	if err := database.WithContext(c).First(&domain, id).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return nil, false
	}
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), id) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return nil, false
	}
	return &domain, true
}

func ensureDomainWrite(c *gin.Context, domainID string, subDomain string) bool {
	if !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), domainID) {
		middleware.ErrorResponse(c, "无权操作该域名")
		return false
	}
	if readOnly, exists := c.Get("perm_read_only"); exists {
		if ro, ok := readOnly.(bool); ok && ro {
			middleware.ErrorResponse(c, "您对该域名仅有只读权限")
			return false
		}
	}
	if subDomain != "" && !middleware.CheckSubDomainPermission(currentUID(c), c.GetInt("level"), domainID, subDomain) {
		middleware.ErrorResponse(c, "无权操作该子域名")
		return false
	}
	return true
}

func providerFeatureByDomain(domain *models.Domain) (dns.ProviderFeatures, bool) {
	var account models.Account
	if err := database.DB.Where("id = ?", domain.AccountID).First(&account).Error; err != nil {
		return dns.ProviderFeatures{}, false
	}
	cfg, ok := dns.GetProviderConfig(account.Type)
	if !ok {
		return dns.ProviderFeatures{}, false
	}
	return cfg.Features, true
}

func GetDomainAliases(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	domain, ok := loadDomainForAction(c)
	if !ok {
		return
	}
	var aliases []models.DomainAlias
	database.WithContext(c).Where("did = ?", domain.ID).Order("id ASC").Find(&aliases)
	middleware.SuccessResponse(c, gin.H{"list": aliases})
}

func AddDomainAlias(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	domain, ok := loadDomainForAction(c)
	if !ok {
		return
	}
	if !ensureDomainWrite(c, strconv.FormatUint(uint64(domain.ID), 10), "") {
		return
	}
	var req addDomainAliasRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		middleware.ErrorResponse(c, "别名不能为空")
		return
	}
	var count int64
	database.WithContext(c).Model(&models.DomainAlias{}).Where("did = ? AND alias = ?", domain.ID, name).Count(&count)
	if count > 0 {
		middleware.ErrorResponse(c, "别名已存在")
		return
	}
	alias := models.DomainAlias{DomainID: domain.ID, Name: name}
	if err := database.WithContext(c).Create(&alias).Error; err != nil {
		middleware.ErrorResponse(c, "添加失败")
		return
	}
	service.Audit.LogAction(c, "add_domain_alias", domain.Name, "添加域名别名: "+name)
	middleware.SuccessResponse(c, alias)
}

func DeleteDomainAlias(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	aliasID := c.Param("aliasId")
	if aliasID == "" {
		aliasID = c.Param("id")
	}
	if aliasID == "" {
		middleware.ErrorResponse(c, "缺少别名ID")
		return
	}
	var alias models.DomainAlias
	if err := database.WithContext(c).First(&alias, aliasID).Error; err != nil {
		middleware.ErrorResponse(c, "别名不存在")
		return
	}
	var domain models.Domain
	if err := database.WithContext(c).First(&domain, alias.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}
	if !ensureDomainWrite(c, strconv.FormatUint(uint64(domain.ID), 10), "") {
		return
	}
	if err := database.WithContext(c).Delete(&alias).Error; err != nil {
		middleware.ErrorResponse(c, "删除失败")
		return
	}
	service.Audit.LogAction(c, "delete_domain_alias", domain.Name, "删除域名别名: "+alias.Name)
	middleware.SuccessMsg(c, "删除成功")
}

func GetRecordWeight(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	domain, ok := loadDomainForAction(c)
	if !ok {
		return
	}
	sub := strings.TrimSpace(c.Query("sub"))
	if sub == "" {
		sub = strings.TrimSpace(c.Query("name"))
	}
	if sub == "" {
		middleware.ErrorResponse(c, "缺少子域名")
		return
	}
	provider := getProviderByDomain(c, domain)
	if provider == nil {
		return
	}
	result, err := provider.GetSubDomainRecords(c.Request.Context(), sub, 1, 500, "", "")
	if err != nil {
		middleware.ErrorResponse(c, "获取记录失败: "+err.Error())
		return
	}
	records, _ := result.Records.([]dns.Record)
	items := make([]gin.H, 0)
	for _, r := range records {
		if r.Type != "A" && r.Type != "AAAA" {
			continue
		}
		items = append(items, gin.H{
			"record_id": r.ID,
			"name":      r.Name,
			"type":      r.Type,
			"value":     r.Value,
			"line":      r.Line,
			"ttl":       r.TTL,
			"weight":    r.Weight,
			"status":    r.Status,
		})
	}
	middleware.SuccessResponse(c, gin.H{"list": items})
}

func UpdateRecordWeight(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req updateRecordWeightRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		middleware.ErrorResponse(c, "缺少记录ID")
		return
	}
	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}
	if !ensureDomainWrite(c, req.DomainID, "") {
		return
	}
	features, ok := providerFeatureByDomain(&domain)
	if !ok || !features.Weight {
		middleware.ErrorResponse(c, "当前服务商不支持权重")
		return
	}
	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	record, err := provider.GetDomainRecordInfo(ctx, req.RecordID)
	if err != nil {
		middleware.ErrorResponse(c, "获取记录详情失败: "+err.Error())
		return
	}
	weight := req.Weight
	if err := provider.UpdateDomainRecord(ctx, req.RecordID, record.Name, record.Type, record.Value, record.Line, record.TTL, record.MX, &weight, record.Remark); err != nil {
		middleware.ErrorResponse(c, "更新权重失败: "+err.Error())
		return
	}
	service.Audit.LogAction(c, "update_record_weight", domain.Name, fmt.Sprintf("更新记录权重: %s -> %d", req.RecordID, req.Weight))
	middleware.SuccessMsg(c, "更新成功")
}

func SmartParseRecord(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	value := strings.TrimSpace(c.Query("value"))
	if value == "" {
		value = strings.TrimSpace(c.Query("domain"))
	}
	if value == "" {
		middleware.ErrorResponse(c, "缺少待解析值")
		return
	}
	recordType := "TXT"
	if ip := net.ParseIP(value); ip != nil {
		if ip.To4() != nil {
			recordType = "A"
		} else {
			recordType = "AAAA"
		}
	} else if strings.Contains(value, ".") && !strings.ContainsAny(value, " /\\") {
		recordType = "CNAME"
	}
	middleware.SuccessResponse(c, gin.H{"type": recordType, "value": value})
}

func GetRecordQuickInfo(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	domain, ok := loadDomainForAction(c)
	if !ok {
		return
	}
	provider := getProviderByDomain(c, domain)
	if provider == nil {
		return
	}
	lines, err := provider.GetRecordLine(c.Request.Context())
	if err != nil {
		middleware.ErrorResponse(c, "获取线路失败: "+err.Error())
		return
	}
	features, _ := providerFeatureByDomain(domain)
	middleware.SuccessResponse(c, gin.H{
		"lines":            lines,
		"min_ttl":          provider.GetMinTTL(),
		"supports_weight":  features.Weight,
		"supports_remark":  features.Remark,
		"supports_log":     features.Log,
		"supports_status":  features.Status,
	})
}

func GetRecordChangeLog(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	domain, ok := loadDomainForAction(c)
	if !ok {
		return
	}
	provider := getProviderByDomain(c, domain)
	if provider == nil {
		return
	}
	page, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page", "1")))
	pageSize, _ := strconv.Atoi(strings.TrimSpace(c.DefaultQuery("page_size", "20")))
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	result, err := provider.GetDomainRecordLog(c.Request.Context(), page, pageSize, c.Query("keyword"), c.Query("start_date"), c.Query("end_date"))
	if err != nil || result == nil {
		middleware.SuccessResponse(c, gin.H{"total": 0, "list": []interface{}{}})
		return
	}
	middleware.SuccessResponse(c, gin.H{"total": result.Total, "list": result.Records})
}
