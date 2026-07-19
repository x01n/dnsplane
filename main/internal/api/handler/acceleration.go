package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"main/internal/database"
	maindns "main/internal/dns"
	"main/internal/api/middleware"
	"main/internal/models"
	"main/internal/service"
	"main/internal/sysconfig"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// GetAccelConfig 获取加速配置
func GetAccelConfig(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	keys := []string{
		"accel_cf_enabled", "accel_cf_prefer_domain", "accel_cf_zone_id", "accel_cf_domain_id",
		"accel_eo_enabled", "accel_eo_secret_id", "accel_eo_secret_key", "accel_eo_zone_id", "accel_eo_endpoint", "accel_eo_plan_id",
		"accel_esa_enabled", "accel_esa_access_key_id", "accel_esa_access_key_secret", "accel_esa_site_id", "accel_esa_region",
		"accel_strategy",
	}

	result := make(map[string]string)
	for _, k := range keys {
		result[k] = sysconfig.GetValue(k)
	}

	// 隐藏敏感信息
	if result["accel_eo_secret_key"] != "" {
		result["accel_eo_secret_key"] = "******"
	}
	if result["accel_esa_access_key_secret"] != "" {
		result["accel_esa_access_key_secret"] = "******"
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// UpdateAccelConfig 更新加速配置
func UpdateAccelConfig(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	var req map[string]interface{}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "参数错误"})
		return
	}

	allowedKeys := map[string]bool{
		"accel_cf_enabled": true, "accel_cf_prefer_domain": true, "accel_cf_zone_id": true, "accel_cf_domain_id": true,
		"accel_eo_enabled": true, "accel_eo_secret_id": true, "accel_eo_secret_key": true, "accel_eo_zone_id": true, "accel_eo_endpoint": true, "accel_eo_plan_id": true,
		"accel_esa_enabled": true, "accel_esa_access_key_id": true, "accel_esa_access_key_secret": true, "accel_esa_site_id": true, "accel_esa_region": true,
		"accel_strategy": true,
	}

	for k, v := range req {
		if !allowedKeys[k] {
			continue
		}
		val := fmt.Sprintf("%v", v)
		if val == "******" {
			continue
		}
		database.DB.Where("key = ?", k).Assign(models.SysConfig{Value: val}).FirstOrCreate(&models.SysConfig{Key: k})
		sysconfig.Invalidate(k)
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "保存成功"})
}

func loadAccelConfig() *service.AccelConfig {
	cfg := &service.AccelConfig{}
	cfg.CFEnabled = sysconfig.GetValue("accel_cf_enabled") == "true"
	cfg.CFPreferDomain = sysconfig.GetValue("accel_cf_prefer_domain")
	cfg.CFZoneID = sysconfig.GetValue("accel_cf_zone_id")
	if did := sysconfig.GetValue("accel_cf_domain_id"); did != "" {
		if v, err := strconv.ParseUint(did, 10, 32); err == nil {
			cfg.CFDomainID = uint(v)
		}
	}

	cfg.EOEnabled = sysconfig.GetValue("accel_eo_enabled") == "true"
	cfg.EOSecretID = sysconfig.GetValue("accel_eo_secret_id")
	cfg.EOSecretKey = sysconfig.GetValue("accel_eo_secret_key")
	cfg.EOZoneID = sysconfig.GetValue("accel_eo_zone_id")
	cfg.EOEndpoint = sysconfig.GetValue("accel_eo_endpoint")
	cfg.EOPlanID = sysconfig.GetValue("accel_eo_plan_id")

	cfg.ESAEnabled = sysconfig.GetValue("accel_esa_enabled") == "true"
	cfg.ESAAccessKeyID = sysconfig.GetValue("accel_esa_access_key_id")
	cfg.ESAAccessKeySecret = sysconfig.GetValue("accel_esa_access_key_secret")
	cfg.ESASiteID = sysconfig.GetValue("accel_esa_site_id")
	cfg.ESARegion = sysconfig.GetValue("accel_esa_region")

	cfg.Strategy = sysconfig.GetValue("accel_strategy")
	if cfg.Strategy == "" {
		cfg.Strategy = "mixed_cf_eo"
	}

	return cfg
}

// AccelerateRecord 一键加速 DNS 记录
func AccelerateRecord(c *gin.Context) {
	domainID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var req struct {
		RecordName  string `json:"record_name"`
		RecordType  string `json:"record_type"`
		Value       string `json:"value"`
		RecordValue string `json:"record_value"`
		Strategy    string `json:"strategy"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "参数错误"})
		return
	}

	if req.Value == "" && req.RecordValue != "" {
		req.Value = req.RecordValue
	}

	if req.RecordName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "record_name 不能为空"})
		return
	}

	var domain models.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "域名不存在"})
		return
	}

	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(domainID, 10)) {
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权限操作该域名"})
		return
	}

	cfg := loadAccelConfig()

	// 构建完整域名
	fqdn := req.RecordName + "." + domain.Name
	if req.RecordName == "@" {
		fqdn = domain.Name
	}

	// 确定源站地址
	originAddr := req.Value
	if originAddr == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "源站地址不能为空"})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	var results []service.AccelResult

	strategy := cfg.Strategy
	if req.Strategy != "" {
		strategy = req.Strategy
	}

	switch strategy {
	case "cf_only":
		results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
	case "eo_only":
		results = append(results, accelerateEO(ctx, cfg, fqdn, originAddr, "DEF"))
	case "esa_only":
		results = append(results, accelerateESA(ctx, cfg, fqdn, originAddr, "DEF"))
	case "mixed_cf_eo":
		results = append(results, accelerateEO(ctx, cfg, fqdn, originAddr, "CN"))
		results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
	case "mixed_cf_esa":
		results = append(results, accelerateESA(ctx, cfg, fqdn, originAddr, "CN"))
		results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
	default:
		results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
	}

	// 配置 DNS 解析
	dnsResults := configureDNSForAcceleration(ctx, cfg, &domain, req.RecordName, results)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "加速配置完成",
		"data": gin.H{
			"accel_results": results,
			"dns_results":   dnsResults,
		},
	})
}

func accelerateCF(ctx context.Context, cfg *service.AccelConfig, fqdn string, domain models.Domain) service.AccelResult {
	if !cfg.CFEnabled || cfg.CFPreferDomain == "" {
		return service.AccelResult{
			Platform: "cloudflare",
			Line:     "AB",
			LineName: "海外",
			Status:   "skipped",
			Msg:      "CF 加速未启用或未配置优选域名",
		}
	}

	// CF 加速通过自定义主机名接入，CNAME 到优选域名
	if cfg.CFDomainID > 0 && cfg.CFZoneID != "" {
		svc, _, _, err := getCFDomainContext(cfg.CFDomainID)
		if err == nil {
			_, _ = svc.CreateCustomHostname(cfg.CFZoneID, fqdn, "", "http", "1.0")
		}
	}

	return service.AccelResult{
		Platform: "cloudflare",
		Line:     "AB",
		LineName: "海外",
		CNAME:    cfg.CFPreferDomain,
		Status:   "success",
	}
}

func accelerateEO(ctx context.Context, cfg *service.AccelConfig, fqdn, originAddr, scope string) service.AccelResult {
	lineName := "默认"
	lineCode := "DEF"
	if scope == "CN" {
		lineName = "国内"
		lineCode = "CN"
	}

	if !cfg.EOEnabled || cfg.EOSecretID == "" || cfg.EOZoneID == "" {
		return service.AccelResult{
			Platform: "tencenteo",
			Line:     lineCode,
			LineName: lineName,
			Status:   "skipped",
			Msg:      "EO 加速未启用或未配置",
		}
	}

	client := service.NewEOClient(cfg.EOSecretID, cfg.EOSecretKey, cfg.EOEndpoint)

	// 先查询是否已存在
	cname, status, err := client.DescribeAccelerationDomains(ctx, cfg.EOZoneID, fqdn)
	if err == nil && cname != "" && (status == "online" || status == "process") {
		return service.AccelResult{
			Platform: "tencenteo",
			Line:     lineCode,
			LineName: lineName,
			CNAME:    cname,
			Status:   "success",
			Msg:      "加速域名已存在",
		}
	}

	// 创建加速域名
	cname, err = client.CreateAccelerationDomain(ctx, cfg.EOZoneID, fqdn, originAddr)
	if err != nil {
		if strings.Contains(err.Error(), "DomainAlreadyExists") || strings.Contains(err.Error(), "already exists") {
			// 已存在，尝试获取 CNAME
			cname2, _, err2 := client.DescribeAccelerationDomains(ctx, cfg.EOZoneID, fqdn)
			if err2 == nil && cname2 != "" {
				cname = cname2
			} else {
				cname = fqdn + ".eo.dnse5.com"
			}
			return service.AccelResult{
				Platform: "tencenteo",
				Line:     lineCode,
				LineName: lineName,
				CNAME:    cname,
				Status:   "success",
				Msg:      "加速域名已存在",
			}
		}
		return service.AccelResult{
			Platform: "tencenteo",
			Line:     lineCode,
			LineName: lineName,
			Status:   "error",
			Msg:      err.Error(),
		}
	}

	// 创建后查询实际 CNAME
	time.Sleep(1 * time.Second)
	if actualCname, _, err := client.DescribeAccelerationDomains(ctx, cfg.EOZoneID, fqdn); err == nil && actualCname != "" {
		cname = actualCname
	}

	return service.AccelResult{
		Platform: "tencenteo",
		Line:     lineCode,
		LineName: lineName,
		CNAME:    cname,
		Status:   "success",
	}
}

func accelerateESA(ctx context.Context, cfg *service.AccelConfig, fqdn, originAddr, scope string) service.AccelResult {
	lineName := "默认"
	lineCode := "DEF"
	if scope == "CN" {
		lineName = "国内"
		lineCode = "CN"
	}

	if !cfg.ESAEnabled || cfg.ESAAccessKeyID == "" || cfg.ESASiteID == "" {
		return service.AccelResult{
			Platform: "aliyunesa",
			Line:     lineCode,
			LineName: lineName,
			Status:   "skipped",
			Msg:      "ESA 加速未启用或未配置",
		}
	}

	client := service.NewESAClient(cfg.ESAAccessKeyID, cfg.ESAAccessKeySecret, cfg.ESARegion)

	// 先查询是否已存在
	existingCname, err := client.GetESARecordCname(ctx, cfg.ESASiteID, fqdn)
	if err == nil && existingCname != "" && existingCname != fqdn+".cnamezone.com" {
		return service.AccelResult{
			Platform: "aliyunesa",
			Line:     lineCode,
			LineName: lineName,
			CNAME:    existingCname,
			Status:   "success",
			Msg:      "加速记录已存在",
		}
	}

	// 确定记录类型
	recordType := "CNAME"
	if isIPv4(originAddr) {
		recordType = "A"
	} else if strings.Contains(originAddr, ":") {
		recordType = "AAAA"
	}

	_, cname, err := client.CreateESARecord(ctx, cfg.ESASiteID, fqdn, recordType, originAddr)
	if err != nil {
		if strings.Contains(err.Error(), "RecordAlreadyExists") || strings.Contains(err.Error(), "already exist") {
			cname2, _ := client.GetESARecordCname(ctx, cfg.ESASiteID, fqdn)
			if cname2 != "" {
				cname = cname2
			} else {
				cname = fqdn + ".cnamezone.com"
			}
			return service.AccelResult{
				Platform: "aliyunesa",
				Line:     lineCode,
				LineName: lineName,
				CNAME:    cname,
				Status:   "success",
				Msg:      "加速记录已存在",
			}
		}
		return service.AccelResult{
			Platform: "aliyunesa",
			Line:     lineCode,
			LineName: lineName,
			Status:   "error",
			Msg:      err.Error(),
		}
	}

	return service.AccelResult{
		Platform: "aliyunesa",
		Line:     lineCode,
		LineName: lineName,
		CNAME:    cname,
		Status:   "success",
	}
}

// configureDNSForAcceleration 根据加速结果配置 DNS 解析
func configureDNSForAcceleration(ctx context.Context, cfg *service.AccelConfig, domain *models.Domain, recordName string, accelResults []service.AccelResult) []gin.H {
	account := getAccountByDomain(domain)
	if account == nil {
		return []gin.H{{"status": "error", "msg": "获取域名账号失败"}}
	}

	providerType := account.Type
	lineMapping := maindns.DefaultLineMapping[providerType]
	if lineMapping == nil {
		return []gin.H{{"status": "error", "msg": "DNS 服务商不支持分线路解析"}}
	}

	var config map[string]string
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		return []gin.H{{"status": "error", "msg": "账户配置解析失败"}}
	}
	provider, err := maindns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		return []gin.H{{"status": "error", "msg": "初始化 DNS 服务商失败: " + err.Error()}}
	}

	var dnsResults []gin.H

	// TTL 探测列表
	ttlList := []int{1, 60, 600}
	if domain.MinTTL > 0 {
		ttlList = []int{domain.MinTTL}
	}

	for _, ar := range accelResults {
		if ar.Status != "success" || ar.CNAME == "" {
			continue
		}

		// 根据平台和策略确定要配置的 DNS 线路
		var dnsLines []struct {
			lineKey  string
			lineName string
		}

		switch {
		case ar.Line == "AB":
			// 海外线路
			if lineID, ok := lineMapping["AB"]; ok && lineID != "" {
				dnsLines = append(dnsLines, struct {
					lineKey  string
					lineName string
				}{"AB", "海外"})
			} else {
				// 不支持海外线路，使用默认
				dnsLines = append(dnsLines, struct {
					lineKey  string
					lineName string
				}{"DEF", "默认"})
			}
		case ar.Line == "CN":
			// 国内线路 - 配置电信、联通、移动
			for _, lk := range []string{"CT", "CU", "CM"} {
				if lineID, ok := lineMapping[lk]; ok && lineID != "" {
					name := map[string]string{"CT": "电信", "CU": "联通", "CM": "移动"}[lk]
					dnsLines = append(dnsLines, struct {
						lineKey  string
						lineName string
					}{lk, name})
				}
			}
			if len(dnsLines) == 0 {
				dnsLines = append(dnsLines, struct {
					lineKey  string
					lineName string
				}{"DEF", "默认"})
			}
		default:
			dnsLines = append(dnsLines, struct {
				lineKey  string
				lineName string
			}{"DEF", "默认"})
		}

		for _, dl := range dnsLines {
			lineID := lineMapping[dl.lineKey]

			// 清理同线路上与 CNAME 互斥的 A/AAAA 记录
			removeConflictingRecords(ctx, provider, recordName, lineID)

			_, skipped, usedTTL, err := maindns.EnsureChallengeRecordWithTTLProbe(
				ctx, provider, recordName, "CNAME", ar.CNAME, lineID, ttlList, "accel-"+ar.Platform,
			)
			if err != nil {
				dnsResults = append(dnsResults, gin.H{
					"platform":  ar.Platform,
					"line_name": dl.lineName,
					"cname":     ar.CNAME,
					"status":    "error",
					"msg":       err.Error(),
				})
				continue
			}

			if usedTTL > 0 && domain.MinTTL == 0 {
				database.DB.Model(domain).Update("min_ttl", usedTTL)
				domain.MinTTL = usedTTL
				ttlList = []int{usedTTL}
			}

			status := "added"
			if skipped {
				status = "exists"
			}
			dnsResults = append(dnsResults, gin.H{
				"platform":  ar.Platform,
				"line_name": dl.lineName,
				"cname":     ar.CNAME,
				"status":    status,
			})
		}
	}

	return dnsResults
}

// removeConflictingRecords 删除同线路上与 CNAME 冲突的 A/AAAA 记录
func removeConflictingRecords(ctx context.Context, provider maindns.Provider, subHost, line string) {
	pr, err := provider.GetSubDomainRecords(ctx, subHost, 1, 100, "", "")
	if err != nil {
		return
	}
	list, ok := pr.Records.([]maindns.Record)
	if !ok {
		return
	}
	for _, rec := range list {
		rt := strings.ToUpper(rec.Type)
		if rt != "A" && rt != "AAAA" {
			continue
		}
		if rec.Line == line || line == "" || rec.Line == "" {
			_ = provider.DeleteDomainRecord(ctx, rec.ID)
		}
	}
}

func getAccountByDomain(domain *models.Domain) *models.Account {
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return nil
	}
	return &account
}

func isIPv4(s string) bool {
	parts := strings.Split(s, ".")
	if len(parts) != 4 {
		return false
	}
	for _, p := range parts {
		if n, err := strconv.Atoi(p); err != nil || n < 0 || n > 255 {
			return false
		}
	}
	return true
}

// BatchAccelerateRecords 批量一键加速
func BatchAccelerateRecords(c *gin.Context) {
	domainID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var req struct {
		Records []struct {
			RecordName string `json:"record_name"`
			RecordType string `json:"record_type"`
			Value      string `json:"value"`
		} `json:"records"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "参数错误"})
		return
	}

	var domain models.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "域名不存在"})
		return
	}

	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(domainID, 10)) {
		c.JSON(http.StatusForbidden, gin.H{"code": 1, "msg": "无权限操作该域名"})
		return
	}

	cfg := loadAccelConfig()
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()

	var allResults []gin.H

	for _, rec := range req.Records {
		fqdn := rec.RecordName + "." + domain.Name
		if rec.RecordName == "@" {
			fqdn = domain.Name
		}
		originAddr := rec.Value

		var results []service.AccelResult

		switch cfg.Strategy {
		case "cf_only":
			results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
		case "eo_only":
			results = append(results, accelerateEO(ctx, cfg, fqdn, originAddr, "DEF"))
		case "esa_only":
			results = append(results, accelerateESA(ctx, cfg, fqdn, originAddr, "DEF"))
		case "mixed_cf_eo":
			results = append(results, accelerateEO(ctx, cfg, fqdn, originAddr, "CN"))
			results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
		case "mixed_cf_esa":
			results = append(results, accelerateESA(ctx, cfg, fqdn, originAddr, "CN"))
			results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
		default:
			results = append(results, accelerateCF(ctx, cfg, fqdn, domain))
		}

		dnsResults := configureDNSForAcceleration(ctx, cfg, &domain, rec.RecordName, results)

		allResults = append(allResults, gin.H{
			"fqdn":          fqdn,
			"accel_results": results,
			"dns_results":   dnsResults,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "批量加速配置完成",
		"data": allResults,
	})
}

// GetAccelStatus 获取域名的加速状态
func GetAccelStatus(c *gin.Context) {
	domainID, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var domain models.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "域名不存在"})
		return
	}

	cfg := loadAccelConfig()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"strategy":        cfg.Strategy,
			"cf_enabled":      cfg.CFEnabled,
			"cf_prefer_domain": cfg.CFPreferDomain,
			"eo_enabled":      cfg.EOEnabled,
			"eo_zone_id":      cfg.EOZoneID,
			"esa_enabled":     cfg.ESAEnabled,
			"esa_site_id":     cfg.ESASiteID,
		},
	})
}

// GetAccelPlatforms 获取当前已配置的加速平台可用状态
func GetAccelPlatforms(c *gin.Context) {
	cfg := loadAccelConfig()

	platforms := []gin.H{}

	// CF
	cfAvailable := cfg.CFEnabled && cfg.CFPreferDomain != ""
	cfInfo := gin.H{
		"platform":  "cloudflare",
		"enabled":   cfg.CFEnabled,
		"available": cfAvailable,
	}
	if cfAvailable {
		cfInfo["prefer_domain"] = cfg.CFPreferDomain
		if cfg.CFDomainID > 0 {
			var domain models.Domain
			if err := database.DB.First(&domain, cfg.CFDomainID).Error; err == nil {
				cfInfo["domain_name"] = domain.Name
			}
		}
	}
	platforms = append(platforms, cfInfo)

	// EO
	eoAvailable := cfg.EOEnabled && cfg.EOSecretID != "" && cfg.EOZoneID != ""
	eoInfo := gin.H{
		"platform":  "tencenteo",
		"enabled":   cfg.EOEnabled,
		"available": eoAvailable,
	}
	if eoAvailable {
		eoInfo["zone_id"] = cfg.EOZoneID
	}
	platforms = append(platforms, eoInfo)

	// ESA
	esaAvailable := cfg.ESAEnabled && cfg.ESAAccessKeyID != "" && cfg.ESASiteID != ""
	esaInfo := gin.H{
		"platform":  "aliyunesa",
		"enabled":   cfg.ESAEnabled,
		"available": esaAvailable,
	}
	if esaAvailable {
		esaInfo["site_id"] = cfg.ESASiteID
	}
	platforms = append(platforms, esaInfo)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"platforms": platforms,
			"strategy":  cfg.Strategy,
		},
	})
}

// ListEOZones 枚举腾讯云 EO 站点列表
func ListEOZones(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	var req struct {
		SecretID  string `json:"secret_id"`
		SecretKey string `json:"secret_key"`
		Endpoint  string `json:"endpoint"`
	}
	_ = c.ShouldBindJSON(&req)

	// 如果前端未传，复用系统已配置的账号
	if req.SecretID == "" {
		req.SecretID = sysconfig.GetValue("accel_eo_secret_id")
	}
	if req.SecretKey == "" {
		req.SecretKey = sysconfig.GetValue("accel_eo_secret_key")
	}
	if req.Endpoint == "" {
		req.Endpoint = sysconfig.GetValue("accel_eo_endpoint")
	}

	if req.SecretID == "" || req.SecretKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "请填写 SecretId 和 SecretKey"})
		return
	}

	client := service.NewEOClient(req.SecretID, req.SecretKey, req.Endpoint)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	zones, err := client.DescribeZones(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "查询站点失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"zones": zones}})
}

// ListESASites 枚举阿里云 ESA 站点列表
func ListESASites(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	var req struct {
		AccessKeyID     string `json:"access_key_id"`
		AccessKeySecret string `json:"access_key_secret"`
		Region          string `json:"region"`
	}
	_ = c.ShouldBindJSON(&req)

	// 如果前端未传，复用系统已配置的账号
	if req.AccessKeyID == "" {
		req.AccessKeyID = sysconfig.GetValue("accel_esa_access_key_id")
	}
	if req.AccessKeySecret == "" {
		req.AccessKeySecret = sysconfig.GetValue("accel_esa_access_key_secret")
	}
	if req.Region == "" {
		req.Region = sysconfig.GetValue("accel_esa_region")
	}

	if req.AccessKeyID == "" || req.AccessKeySecret == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "请填写 AccessKeyID 和 AccessKeySecret"})
		return
	}

	client := service.NewESAClient(req.AccessKeyID, req.AccessKeySecret, req.Region)
	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()

	sites, err := client.ListSites(ctx)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 1, "msg": "查询站点失败: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"sites": sites}})
}

// ListAccelAccounts 列出可复用到加速配置的已有账号
func ListAccelAccounts(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	var accounts []models.Account
	database.DB.Find(&accounts)

	type accountInfo struct {
		ID   uint   `json:"id"`
		Name string `json:"name"`
		Type string `json:"type"`
	}

	result := map[string][]accountInfo{
		"cloudflare": {},
		"dnspod":     {},
		"aliyun":     {},
	}

	for _, acc := range accounts {
		switch acc.Type {
		case "cloudflare":
			result["cloudflare"] = append(result["cloudflare"], accountInfo{ID: acc.ID, Name: acc.Name, Type: acc.Type})
		case "dnspod":
			result["dnspod"] = append(result["dnspod"], accountInfo{ID: acc.ID, Name: acc.Name, Type: acc.Type})
		case "aliyun":
			result["aliyun"] = append(result["aliyun"], accountInfo{ID: acc.ID, Name: acc.Name, Type: acc.Type})
		}
	}

	// CF 域名列表（用于选择 SaaS 回落域名）
	var cfDomains []struct {
		ID      uint   `json:"id"`
		Name    string `json:"name"`
		ThirdID string `json:"third_id"`
	}
	database.DB.Model(&models.Domain{}).
		Joins("JOIN accounts ON accounts.id = domains.aid").
		Where("accounts.type = ? AND domains.deleted_at IS NULL", "cloudflare").
		Select("domains.id, domains.name, domains.third_id").
		Scan(&cfDomains)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"accounts":   result,
			"cf_domains": cfDomains,
		},
	})
}

// ApplyAccountToAccel 将已有账号的密钥应用到加速配置
func ApplyAccountToAccel(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	var req struct {
		AccountID uint   `json:"account_id"`
		Platform  string `json:"platform"` // eo, esa, cf
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "参数错误"})
		return
	}

	var account models.Account
	if err := database.DB.First(&account, req.AccountID).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "账号不存在"})
		return
	}

	var config map[string]string
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": 1, "msg": "账号配置解析失败"})
		return
	}

	applied := map[string]string{}

	switch req.Platform {
	case "eo":
		if account.Type != "dnspod" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "EO 加速需要 DNSPod（腾讯云）类型的账号"})
			return
		}
		if v := config["SecretId"]; v != "" {
			sysconfig.SetValue("accel_eo_secret_id", v)
			applied["accel_eo_secret_id"] = v
		}
		if v := config["SecretKey"]; v != "" {
			sysconfig.SetValue("accel_eo_secret_key", v)
			applied["accel_eo_secret_key"] = "******"
		}
	case "esa":
		if account.Type != "aliyun" {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "ESA 加速需要阿里云类型的账号"})
			return
		}
		if v := config["AccessKeyId"]; v != "" {
			sysconfig.SetValue("accel_esa_access_key_id", v)
			applied["accel_esa_access_key_id"] = v
		}
		if v := config["AccessKeySecret"]; v != "" {
			sysconfig.SetValue("accel_esa_access_key_secret", v)
			applied["accel_esa_access_key_secret"] = "******"
		}
	default:
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "不支持的平台: " + req.Platform})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已从账号同步密钥", "data": applied})
}
