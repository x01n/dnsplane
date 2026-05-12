package handler

import (
	"encoding/json"
	"fmt"
	"main/internal/api/middleware"
	"main/internal/database"
	maindns "main/internal/dns"
	"main/internal/models"
	"main/internal/service"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// getCFEnhanceService 从账户配置构建 Cloudflare 增强服务
func getCFEnhanceService(accountID uint) (*service.EnhanceService, *models.Account, error) {
	var account models.Account
	if err := database.DB.First(&account, accountID).Error; err != nil {
		return nil, nil, fmt.Errorf("账户不存在")
	}

	if account.Type != "cloudflare" {
		return nil, nil, fmt.Errorf("该账户不是 Cloudflare 类型")
	}

	var config map[string]string
	if account.Config != "" {
		decrypted := account.Config // 假设已解密
		json.Unmarshal([]byte(decrypted), &config)
	}

	email := config["email"]
	apiKey := config["apikey"]
	proxy := config["proxy"] == "1"

	auth := service.DetectAuthMode(apiKey)

	// Tunnels API 必须使用 API Token
	if auth == 0 {
		return nil, nil, fmt.Errorf("Tunnel API 不支持 Global API Key，请使用 API Token")
	}

	svc := service.NewEnhanceService(email, apiKey, auth, proxy, config["account_id"])
	return svc, &account, nil
}

// getCFDomainContext 从域名获取 Cloudflare 上下文
func getCFDomainContext(domainID uint) (*service.EnhanceService, *models.Domain, *models.Account, error) {
	var domain models.Domain
	if err := database.DB.First(&domain, domainID).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("域名不存在")
	}

	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return nil, nil, nil, fmt.Errorf("账户不存在")
	}

	if account.Type != "cloudflare" {
		return nil, nil, nil, fmt.Errorf("该域名所属账户不是 Cloudflare 类型")
	}

	if domain.ThirdID == "" {
		return nil, nil, nil, fmt.Errorf("域名未绑定 Cloudflare Zone ID")
	}

	svc, _, err := getCFEnhanceService(domain.AccountID)
	if err != nil {
		return nil, nil, nil, err
	}

	return svc, &domain, &account, nil
}

func getCFDomainContextForRequest(c *gin.Context, domainID uint) (*service.EnhanceService, *models.Domain, *models.Account, error) {
	if !middleware.UserModuleAllowed(c, "domain") {
		return nil, nil, nil, fmt.Errorf("无权限访问该功能模块")
	}
	svc, domain, account, err := getCFDomainContext(domainID)
	if err != nil {
		return nil, nil, nil, err
	}
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		return nil, nil, nil, fmt.Errorf("无权限操作该域名")
	}
	return svc, domain, account, nil
}

func getCFAccountContextForRequest(c *gin.Context, accountID uint) (*service.EnhanceService, *models.Account, error) {
	if !middleware.UserModuleAllowed(c, "domain") {
		return nil, nil, fmt.Errorf("无权限访问该功能模块")
	}
	svc, account, err := getCFEnhanceService(accountID)
	if err != nil {
		return nil, nil, err
	}
	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		return nil, nil, fmt.Errorf("无权限操作该账户")
	}
	return svc, account, nil
}

func cfRequestBody(c *gin.Context) map[string]interface{} {
	body := map[string]interface{}{}
	if strings.Contains(strings.ToLower(c.GetHeader("Content-Type")), "application/json") {
		_ = c.ShouldBindJSON(&body)
	}
	return body
}

func cfString(body map[string]interface{}, c *gin.Context, key string) string {
	if v, ok := body[key]; ok {
		switch val := v.(type) {
		case string:
			return strings.TrimSpace(val)
		case nil:
			return ""
		default:
			return strings.TrimSpace(fmt.Sprint(val))
		}
	}
	if v, ok := c.GetPostForm(key); ok {
		return strings.TrimSpace(v)
	}
	return strings.TrimSpace(c.Query(key))
}

func cfStringDefault(body map[string]interface{}, c *gin.Context, key, fallback string) string {
	if value := cfString(body, c, key); value != "" {
		return value
	}
	return fallback
}

func cfStringList(body map[string]interface{}, c *gin.Context, key string) []string {
	if v, ok := body[key]; ok {
		switch val := v.(type) {
		case []interface{}:
			items := make([]string, 0, len(val))
			for _, item := range val {
				if s := strings.TrimSpace(fmt.Sprint(item)); s != "" {
					items = append(items, s)
				}
			}
			return items
		case []string:
			items := make([]string, 0, len(val))
			for _, item := range val {
				if s := strings.TrimSpace(item); s != "" {
					items = append(items, s)
				}
			}
			return items
		case string:
			return cfSplitList(val)
		}
	}
	if values, ok := c.GetPostFormArray(key); ok && len(values) > 0 {
		items := make([]string, 0, len(values))
		for _, item := range values {
			if s := strings.TrimSpace(item); s != "" {
				items = append(items, s)
			}
		}
		return items
	}
	if v, ok := c.GetPostForm(key); ok {
		return cfSplitList(v)
	}
	return cfSplitList(c.Query(key))
}

func cfSplitList(value string) []string {
	parts := strings.FieldsFunc(value, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		if s := strings.TrimSpace(part); s != "" {
			items = append(items, s)
		}
	}
	return items
}

func cfHostnamesText(body map[string]interface{}, c *gin.Context) []string {
	items := cfStringList(body, c, "hostnames")
	if len(items) == 0 {
		items = cfStringList(body, c, "hostname_ids")
	}
	return items
}

func cfAccountID(svc *service.EnhanceService, account *models.Account) (string, error) {
	var configMap map[string]string
	_ = json.Unmarshal([]byte(account.Config), &configMap)
	accountID := strings.TrimSpace(configMap["account_id"])
	if accountID != "" {
		return accountID, nil
	}
	accountID, err := svc.GetDefaultAccountID()
	if err != nil {
		return "", err
	}
	if accountID == "" {
		return "", fmt.Errorf("无法获取 Cloudflare Account ID")
	}
	return accountID, nil
}

func cfNormalizeHostname(hostname string) string {
	return strings.ToLower(strings.TrimRight(strings.TrimSpace(hostname), "."))
}

func cfHostnameForMatch(hostname string) string {
	return strings.TrimPrefix(cfNormalizeHostname(hostname), "*.")
}

func cfValidHostname(hostname string) bool {
	value := cfNormalizeHostname(hostname)
	if strings.HasPrefix(value, "*.") {
		value = strings.TrimPrefix(value, "*.")
	}
	if value == "" || len(value) > 253 || strings.ContainsAny(value, " /\\\t\r\n") || strings.Contains(value, "://") {
		return false
	}
	labels := strings.Split(value, ".")
	if len(labels) < 2 {
		return false
	}
	for _, label := range labels {
		if label == "" || len(label) > 63 || strings.HasPrefix(label, "-") || strings.HasSuffix(label, "-") {
			return false
		}
		for _, r := range label {
			if !(r >= 'a' && r <= 'z') && !(r >= '0' && r <= '9') && r != '-' {
				return false
			}
		}
	}
	return true
}

func cfMatchHostnameToDomainRecordName(hostname, domainName string, allowRelative bool) (string, bool) {
	hostname = cfHostnameForMatch(hostname)
	domainName = cfNormalizeHostname(domainName)
	if hostname == "" || domainName == "" {
		return "", false
	}
	if hostname == domainName {
		return "@", true
	}
	suffix := "." + domainName
	if strings.HasSuffix(hostname, suffix) {
		return hostname[:len(hostname)-len(suffix)], true
	}
	if allowRelative {
		if hostname == "@" {
			return "@", true
		}
		if !strings.Contains(hostname, ".") {
			return hostname, true
		}
	}
	return "", false
}

type cfTxtTargetDomain struct {
	ID            uint
	AccountID     uint `gorm:"column:aid"`
	Name          string
	AccountType   string `gorm:"column:account_type"`
	AccountName   string `gorm:"column:account_name"`
	AccountRemark string `gorm:"column:account_remark"`
}

func cfDNSProviderName(providerType string) string {
	if cfg, ok := maindns.GetProviderConfig(providerType); ok && cfg.Name != "" {
		return cfg.Name
	}
	if providerType != "" {
		return providerType
	}
	return "-"
}

func cfAccountDisplayName(name, remark string, accountID uint) string {
	name = strings.TrimSpace(name)
	remark = strings.TrimSpace(remark)
	if remark != "" && name != "" {
		return remark + " (" + name + ")"
	}
	if remark != "" {
		return remark
	}
	if name != "" {
		return name
	}
	return fmt.Sprintf("账户#%d", accountID)
}

func cfFormatTxtTargetCandidate(row cfTxtTargetDomain, recordName string, currentDomainID uint) gin.H {
	return gin.H{
		"domain_id":            row.ID,
		"domain_name":          row.Name,
		"record_name":          recordName,
		"account_id":           row.AccountID,
		"account_type":         row.AccountType,
		"account_type_name":    cfDNSProviderName(row.AccountType),
		"account_display_name": cfAccountDisplayName(row.AccountName, row.AccountRemark, row.AccountID),
		"is_current_domain":    row.ID == currentDomainID,
	}
}

func cfCandidateString(row gin.H, key string) string {
	if value, ok := row[key]; ok && value != nil {
		return fmt.Sprint(value)
	}
	return ""
}

func cfCandidateBool(row gin.H, key string) bool {
	value, _ := row[key].(bool)
	return value
}

func cfFindTxtRecordTargetDomains(c *gin.Context, currentDomain *models.Domain, currentAccount *models.Account, hostname string) []gin.H {
	var rows []cfTxtTargetDomain
	database.DB.Model(&models.Domain{}).
		Select("domains.id, domains.aid, domains.name, accounts.type AS account_type, accounts.name AS account_name, accounts.remark AS account_remark").
		Joins("JOIN accounts ON domains.aid = accounts.id").
		Scan(&rows)

	candidates := make([]gin.H, 0)
	bestLength := -1
	for _, row := range rows {
		if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(row.ID), 10)) {
			continue
		}
		recordName, ok := cfMatchHostnameToDomainRecordName(hostname, row.Name, false)
		if !ok {
			continue
		}
		matchedLength := len(cfNormalizeHostname(row.Name))
		if matchedLength > bestLength {
			bestLength = matchedLength
			candidates = candidates[:0]
		}
		if matchedLength == bestLength {
			candidates = append(candidates, cfFormatTxtTargetCandidate(row, recordName, currentDomain.ID))
		}
	}

	if len(candidates) == 0 {
		if recordName, ok := cfMatchHostnameToDomainRecordName(hostname, currentDomain.Name, true); ok {
			candidates = append(candidates, cfFormatTxtTargetCandidate(cfTxtTargetDomain{
				ID:            currentDomain.ID,
				AccountID:     currentDomain.AccountID,
				Name:          currentDomain.Name,
				AccountType:   currentAccount.Type,
				AccountName:   currentAccount.Name,
				AccountRemark: currentAccount.Remark,
			}, recordName, currentDomain.ID))
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		if cfCandidateBool(candidates[i], "is_current_domain") != cfCandidateBool(candidates[j], "is_current_domain") {
			return cfCandidateBool(candidates[i], "is_current_domain")
		}
		if cfCandidateString(candidates[i], "account_type_name") != cfCandidateString(candidates[j], "account_type_name") {
			return cfCandidateString(candidates[i], "account_type_name") < cfCandidateString(candidates[j], "account_type_name")
		}
		if cfCandidateString(candidates[i], "account_display_name") != cfCandidateString(candidates[j], "account_display_name") {
			return cfCandidateString(candidates[i], "account_display_name") < cfCandidateString(candidates[j], "account_display_name")
		}
		return cfCandidateString(candidates[i], "domain_name") < cfCandidateString(candidates[j], "domain_name")
	})
	return candidates
}

func cfFindBestMatchingDomain(accountID uint, hostname string) (*models.Domain, error) {
	var domains []models.Domain
	if err := database.DB.Where("aid = ?", accountID).Find(&domains).Error; err != nil {
		return nil, err
	}
	bestIndex := -1
	bestLength := -1
	for i := range domains {
		domainName := cfNormalizeHostname(domains[i].Name)
		if _, ok := cfMatchHostnameToDomainRecordName(hostname, domainName, false); ok && len(domainName) > bestLength {
			bestIndex = i
			bestLength = len(domainName)
		}
	}
	if bestIndex < 0 {
		return nil, nil
	}
	return &domains[bestIndex], nil
}

func cfResolveTunnel(accountID uint, tunnelID string) (string, uint, error) {
	tunnelID = strings.TrimSpace(tunnelID)
	if tunnelID == "" {
		return "", 0, fmt.Errorf("tunnel_id 不能为空")
	}
	var cfTunnel models.CloudflareTunnel
	if err := database.DB.Where("aid = ? AND tunnel_id = ?", accountID, tunnelID).First(&cfTunnel).Error; err == nil {
		return cfTunnel.TunnelID, cfTunnel.ID, nil
	}
	if localID, err := strconv.ParseUint(tunnelID, 10, 32); err == nil {
		if err := database.DB.Where("aid = ? AND id = ?", accountID, uint(localID)).First(&cfTunnel).Error; err == nil {
			return cfTunnel.TunnelID, cfTunnel.ID, nil
		}
	}
	return tunnelID, 0, nil
}

func cfMapString(row map[string]interface{}, key string) string {
	if value, ok := row[key]; ok && value != nil {
		return strings.TrimSpace(fmt.Sprint(value))
	}
	return ""
}

func cfAsIngressRule(item interface{}) (map[string]interface{}, bool) {
	switch row := item.(type) {
	case map[string]interface{}:
		return row, true
	case gin.H:
		return map[string]interface{}(row), true
	default:
		return nil, false
	}
}

func cfTunnelConfig(raw map[string]interface{}) map[string]interface{} {
	if config, ok := raw["config"].(map[string]interface{}); ok {
		return config
	}
	return raw
}

func cfCloneConfig(config map[string]interface{}) map[string]interface{} {
	var cloned map[string]interface{}
	data, _ := json.Marshal(config)
	_ = json.Unmarshal(data, &cloned)
	if cloned == nil {
		cloned = map[string]interface{}{}
	}
	return cloned
}

func cfTunnelIngress(config map[string]interface{}) []interface{} {
	if ingress, ok := config["ingress"].([]interface{}); ok {
		return append([]interface{}{}, ingress...)
	}
	return []interface{}{}
}

func cfIsFallbackIngressRule(item interface{}) bool {
	rule, ok := cfAsIngressRule(item)
	return ok && cfMapString(rule, "hostname") == "" && cfMapString(rule, "path") == ""
}

func cfFindFallbackIngressIndex(ingress []interface{}) int {
	for i, item := range ingress {
		if cfIsFallbackIngressRule(item) {
			return i
		}
	}
	return -1
}

func cfFindPublicHostnameIndex(ingress []interface{}, hostname, path string) int {
	for i, item := range ingress {
		rule, ok := cfAsIngressRule(item)
		if !ok {
			continue
		}
		if cfNormalizeHostname(cfMapString(rule, "hostname")) == cfNormalizeHostname(hostname) && cfMapString(rule, "path") == strings.TrimSpace(path) {
			return i
		}
	}
	return -1
}

func cfEnsureFallbackIngress(ingress []interface{}) []interface{} {
	rows := make([]interface{}, 0, len(ingress)+1)
	for _, item := range ingress {
		if rule, ok := cfAsIngressRule(item); ok {
			rows = append(rows, rule)
		}
	}
	if len(rows) == 0 || !cfIsFallbackIngressRule(rows[len(rows)-1]) {
		rows = append(rows, map[string]interface{}{"service": "http_status:404"})
	}
	return rows
}

func cfPublicHostnameRows(config map[string]interface{}) []gin.H {
	rows := make([]gin.H, 0)
	for _, item := range cfTunnelIngress(config) {
		rule, ok := cfAsIngressRule(item)
		if !ok {
			continue
		}
		hostname := cfMapString(rule, "hostname")
		if hostname == "" {
			continue
		}
		rows = append(rows, gin.H{
			"hostname": hostname,
			"path":     cfMapString(rule, "path"),
			"service":  cfMapString(rule, "service"),
		})
	}
	return rows
}

func cfRouteExists(items []interface{}, routeID string) bool {
	for _, item := range items {
		row, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id := cfMapString(row, "id")
		if id == "" {
			id = cfMapString(row, "hostname_route_id")
		}
		if id == routeID {
			return true
		}
	}
	return false
}

// ========== 自定义主机名 ==========

// GetCustomHostnames 获取自定义主机名列表
func GetCustomHostnames(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, _, _, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	page, _ := strconv.Atoi(cfStringDefault(body, c, "page", "1"))
	pageSize, _ := strconv.Atoi(cfStringDefault(body, c, "pageSize", "10"))

	zoneID := cfString(body, c, "zone_id")
	if zoneID == "" {
		// 从域名获取 zone_id
		var domain models.Domain
		database.DB.First(&domain, uint(id))
		zoneID = domain.ThirdID
	}

	items, total, err := svc.ListCustomHostnames(zoneID, page, pageSize)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	// 格式化数据
	rows := make([]gin.H, 0)
	for _, item := range items {
		row := formatCustomHostnameRow(item)
		rows = append(rows, row)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  0,
		"data":  rows,
		"total": total,
		"rows":  rows,
	})
}

// AddCustomHostname 添加自定义主机名
func AddCustomHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostname := cfString(body, c, "hostname")
	customOrigin := cfString(body, c, "custom_origin_server")
	sslMethod := cfStringDefault(body, c, "ssl_method", "txt")
	minTLSVersion := cfStringDefault(body, c, "min_tls_version", "1.2")

	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "主机名不能为空"})
		return
	}

	// 验证自定义 origin
	if customOrigin != "" {
		if strings.HasPrefix(customOrigin, "http://") || strings.HasPrefix(customOrigin, "https://") {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "自定义源不能包含 http:// 或 https:// 前缀"})
			return
		}
	}

	result, err := svc.CreateCustomHostname(domain.ThirdID, hostname, customOrigin, sslMethod, minTLSVersion)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	// 写入数据库
	cfHostname := models.CloudflareHostname{
		DomainID:           uint(id),
		Hostname:           hostname,
		CustomOriginServer: customOrigin,
		SSLMethod:          sslMethod,
		SSLMinTLSVersion:   minTLSVersion,
	}

	if idStr, ok := result["id"].(string); ok {
		cfHostname.HostnameID = idStr
	}
	if ssl, ok := result["ssl"].(map[string]interface{}); ok {
		if status, ok := ssl["status"].(string); ok {
			cfHostname.SSLStatus = status
		}
		if method, ok := ssl["method"].(string); ok {
			cfHostname.SSLMethod = method
		}
	}
	if status, ok := result["status"].(string); ok {
		cfHostname.VerificationStatus = status
	}

	database.DB.Create(&cfHostname)

	// 记录日志
	addCFLog(account.ID, domain.Name, "添加自定义主机名", hostname)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": result,
	})
}

// UpdateCustomHostname 更新自定义主机名
func UpdateCustomHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostnameID := cfString(body, c, "hostname_id")
	if hostnameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "hostname_id 不能为空"})
		return
	}

	updates := make(map[string]interface{})

	if customOrigin := cfString(body, c, "custom_origin_server"); customOrigin != "" {
		updates["custom_origin_server"] = customOrigin
	}

	if sslMethod := cfString(body, c, "ssl_method"); sslMethod != "" {
		updates["ssl"] = map[string]interface{}{
			"method": sslMethod,
			"settings": map[string]interface{}{
				"min_tls_version": cfStringDefault(body, c, "min_tls_version", "1.2"),
			},
		}
	}

	result, err := svc.UpdateCustomHostname(domain.ThirdID, hostnameID, updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	addCFLog(account.ID, domain.Name, "更新自定义主机名", hostnameID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "更新成功",
		"data": result,
	})
}

// DeleteCustomHostname 删除自定义主机名
func DeleteCustomHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostnameID := cfString(body, c, "hostname_id")
	if hostnameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "hostname_id 不能为空"})
		return
	}

	err = svc.DeleteCustomHostname(domain.ThirdID, hostnameID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	// 删除本地记录
	database.DB.Where("hostname_id = ?", hostnameID).Delete(&models.CloudflareHostname{})

	addCFLog(account.ID, domain.Name, "删除自定义主机名", hostnameID)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}

func BatchAddCustomHostnames(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostnames := cfHostnamesText(body, c)
	customOrigin := cfString(body, c, "custom_origin_server")
	sslMethod := cfStringDefault(body, c, "ssl_method", "txt")
	minTLSVersion := cfStringDefault(body, c, "min_tls_version", "1.2")
	if len(hostnames) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "主机名列表不能为空"})
		return
	}
	if customOrigin != "" && (strings.HasPrefix(customOrigin, "http://") || strings.HasPrefix(customOrigin, "https://")) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "自定义源不能包含 http:// 或 https:// 前缀"})
		return
	}

	success := 0
	failed := make([]gin.H, 0)
	for _, hostname := range hostnames {
		result, err := svc.CreateCustomHostname(domain.ThirdID, hostname, customOrigin, sslMethod, minTLSVersion)
		if err != nil {
			failed = append(failed, gin.H{"hostname": hostname, "msg": err.Error()})
			continue
		}
		cfHostname := models.CloudflareHostname{
			DomainID:           uint(id),
			Hostname:           hostname,
			CustomOriginServer: customOrigin,
			SSLMethod:          sslMethod,
			SSLMinTLSVersion:   minTLSVersion,
		}
		if hostnameID, ok := result["id"].(string); ok {
			cfHostname.HostnameID = hostnameID
		}
		if ssl, ok := result["ssl"].(map[string]interface{}); ok {
			if status, ok := ssl["status"].(string); ok {
				cfHostname.SSLStatus = status
			}
		}
		if status, ok := result["status"].(string); ok {
			cfHostname.VerificationStatus = status
		}
		database.DB.Create(&cfHostname)
		success++
	}

	addCFLog(account.ID, domain.Name, "批量添加自定义主机名", fmt.Sprintf("成功 %d 个，失败 %d 个", success, len(failed)))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": fmt.Sprintf("成功添加 %d 个自定义主机名", success), "data": gin.H{"success": success, "failed": failed}})
}

func BatchUpdateCustomHostnames(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostnameIDs := cfStringList(body, c, "hostname_ids")
	customOrigin := cfString(body, c, "custom_origin_server")
	sslMethod := cfString(body, c, "ssl_method")
	minTLSVersion := cfString(body, c, "min_tls_version")
	if len(hostnameIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "hostname_ids 不能为空"})
		return
	}
	if customOrigin != "" && (strings.HasPrefix(customOrigin, "http://") || strings.HasPrefix(customOrigin, "https://")) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "自定义源不能包含 http:// 或 https:// 前缀"})
		return
	}

	updates := make(map[string]interface{})
	updates["custom_origin_server"] = customOrigin
	if sslMethod != "" || minTLSVersion != "" {
		ssl := map[string]interface{}{}
		if sslMethod != "" {
			ssl["method"] = sslMethod
		}
		if minTLSVersion != "" {
			ssl["settings"] = map[string]interface{}{"min_tls_version": minTLSVersion}
		}
		updates["ssl"] = ssl
	}

	success := 0
	failed := make([]gin.H, 0)
	for _, hostnameID := range hostnameIDs {
		if _, err := svc.UpdateCustomHostname(domain.ThirdID, hostnameID, updates); err != nil {
			failed = append(failed, gin.H{"hostname_id": hostnameID, "msg": err.Error()})
			continue
		}
		database.DB.Model(&models.CloudflareHostname{}).Where("hostname_id = ?", hostnameID).Updates(map[string]interface{}{
			"custom_origin_server": customOrigin,
			"ssl_method":           sslMethod,
			"ssl_min_tls_version":  minTLSVersion,
		})
		success++
	}

	addCFLog(account.ID, domain.Name, "批量更新自定义主机名", fmt.Sprintf("成功 %d 个，失败 %d 个", success, len(failed)))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": fmt.Sprintf("成功更新 %d 个自定义主机名", success), "data": gin.H{"success": success, "failed": failed}})
}

func BatchDeleteCustomHostnames(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostnameIDs := cfStringList(body, c, "hostname_ids")
	if len(hostnameIDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "hostname_ids 不能为空"})
		return
	}

	success := 0
	failed := make([]gin.H, 0)
	for _, hostnameID := range hostnameIDs {
		if err := svc.DeleteCustomHostname(domain.ThirdID, hostnameID); err != nil {
			failed = append(failed, gin.H{"hostname_id": hostnameID, "msg": err.Error()})
			continue
		}
		database.DB.Where("hostname_id = ?", hostnameID).Delete(&models.CloudflareHostname{})
		success++
	}

	addCFLog(account.ID, domain.Name, "批量删除自定义主机名", fmt.Sprintf("成功 %d 个，失败 %d 个", success, len(failed)))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": fmt.Sprintf("成功删除 %d 个自定义主机名", success), "data": gin.H{"success": success, "failed": failed}})
}

func GetCustomHostnameTxtTargets(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	_, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error(), "data": gin.H{"candidates": []gin.H{}}})
		return
	}

	body := cfRequestBody(c)
	hostname := cfString(body, c, "hostname")
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 TXT 主机名", "data": gin.H{"candidates": []gin.H{}}})
		return
	}

	candidates := cfFindTxtRecordTargetDomains(c, domain, account, hostname)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"hostname":   hostname,
			"candidates": candidates,
		},
	})
}

// RefreshCustomHostname 刷新自定义主机名验证状态
func RefreshCustomHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, _, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	hostnameID := cfString(body, c, "hostname_id")
	if hostnameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "hostname_id 不能为空"})
		return
	}

	result, err := svc.GetCustomHostname(domain.ThirdID, hostnameID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	// 更新本地记录
	var cfHostname models.CloudflareHostname
	if err := database.DB.Where("hostname_id = ?", hostnameID).First(&cfHostname).Error; err == nil {
		if ssl, ok := result["ssl"].(map[string]interface{}); ok {
			if status, ok := ssl["status"].(string); ok {
				cfHostname.SSLStatus = status
			}
		}
		if status, ok := result["status"].(string); ok {
			cfHostname.VerificationStatus = status
		}
		database.DB.Save(&cfHostname)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": formatCustomHostnameRow(result),
	})
}

// ========== Fallback Origin ==========

// GetFallbackOrigin 获取 Fallback Origin
func GetFallbackOrigin(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, _, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	origin, err := svc.GetFallbackOrigin(domain.ThirdID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"origin": origin},
	})
}

// SetFallbackOrigin 设置 Fallback Origin
func SetFallbackOrigin(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	origin := cfString(body, c, "origin")
	if origin == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "origin 不能为空"})
		return
	}

	err = svc.UpdateFallbackOrigin(domain.ThirdID, origin)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	addCFLog(account.ID, domain.Name, "设置 Fallback Origin", origin)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "设置成功",
		"data": gin.H{"origin": origin},
	})
}

// DeleteFallbackOrigin 删除 Fallback Origin
func DeleteFallbackOrigin(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	err = svc.DeleteFallbackOrigin(domain.ThirdID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	addCFLog(account.ID, domain.Name, "删除 Fallback Origin", "")

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}

// ========== DCV Delegation ==========

// GetDcvDelegationUUID 获取 DCV 委派 UUID
func GetDcvDelegationUUID(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, _, err := getCFDomainContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	uuid, err := svc.GetDcvDelegationUUID(domain.ThirdID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"uuid": uuid},
	})
}

// ========== Tunnels ==========

// GetTunnels 获取 Tunnel 列表
func GetTunnels(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]

	if accountID == "" {
		accountID, err = svc.GetDefaultAccountID()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
			return
		}
	}

	items, err := svc.ListTunnels(accountID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	rows := make([]gin.H, 0)
	for _, item := range items {
		row := formatTunnelRow(item)
		rows = append(rows, row)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":       0,
		"data":       rows,
		"total":      len(rows),
		"rows":       rows,
		"account_id": accountID,
	})
}

// AddTunnel 添加 Tunnel
func AddTunnel(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	name := cfString(body, c, "name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "名称不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, err = svc.GetDefaultAccountID()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
			return
		}
	}

	result, err := svc.CreateTunnel(accountID, name)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	// 保存本地记录
	cfTunnel := models.CloudflareTunnel{
		AccountID: uint(id),
		Name:      name,
	}
	if tunnelID, ok := result["id"].(string); ok {
		cfTunnel.TunnelID = tunnelID
	}
	if status, ok := result["status"].(string); ok {
		cfTunnel.Status = status
	}

	database.DB.Create(&cfTunnel)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "创建成功",
		"data": result,
	})
}

// DeleteTunnel 删除 Tunnel
func DeleteTunnel(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	if tunnelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]

	var cfTunnel models.CloudflareTunnel
	if err := database.DB.Where("tunnel_id = ?", tunnelID).First(&cfTunnel).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "Tunnel 不存在"})
		return
	}

	if accountID == "" {
		accountID, err = svc.GetDefaultAccountID()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Account ID"})
			return
		}
	}

	err = svc.DeleteTunnel(accountID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	database.DB.Where("tunnel_id = ?", tunnelID).Delete(&models.CloudflareTunnel{})
	database.DB.Where("tid = ?", cfTunnel.ID).Delete(&models.CloudflareCIDRRoute{})
	database.DB.Where("tid = ?", cfTunnel.ID).Delete(&models.CloudflareHostnameRoute{})

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}

// GetTunnelToken 获取 Tunnel Token
func GetTunnelToken(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	if tunnelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
	}

	token, err := svc.GetTunnelToken(accountID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"token": token},
	})
}

// ========== CIDR Routes ==========

// GetCidrRoutes 获取 CIDR 路由列表
func GetCidrRoutes(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	items, err := svc.ListCidrRoutes(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	rows := make([]gin.H, 0)
	for _, item := range items {
		row := formatCidrRouteRow(item)
		rows = append(rows, row)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  0,
		"data":  rows,
		"total": len(rows),
		"rows":  rows,
	})
}

// AddCidrRoute 添加 CIDR 路由
func AddCidrRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	network := cfString(body, c, "network")
	comment := cfString(body, c, "comment")
	virtualNetworkID := cfString(body, c, "virtual_network_id")

	if tunnelID == "" || network == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 network 不能为空"})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, localTunnelID, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	result, err := svc.CreateCidrRoute(accountID, cfTunnelID, network, comment, virtualNetworkID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	route := models.CloudflareCIDRRoute{
		TunnelID:         localTunnelID,
		Network:          network,
		Comment:          comment,
		VirtualNetworkID: virtualNetworkID,
	}
	if routeID, ok := result["id"].(string); ok {
		route.RouteID = routeID
	}
	if localTunnelID > 0 {
		database.DB.Create(&route)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": result,
	})
}

// DeleteCidrRoute 删除 CIDR 路由
func DeleteCidrRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	routeID := cfString(body, c, "route_id")

	if tunnelID == "" || routeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 route_id 不能为空"})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	items, err := svc.ListCidrRoutes(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	if !cfRouteExists(items, routeID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "CIDR 路由不存在或不属于当前 Tunnel"})
		return
	}

	err = svc.DeleteCidrRoute(accountID, routeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	database.DB.Where("route_id = ?", routeID).Delete(&models.CloudflareCIDRRoute{})

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}

// ========== Hostname Routes ==========

// GetHostnameRoutes 获取主机名路由列表
func GetHostnameRoutes(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	items, err := svc.ListHostnameRoutes(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	rows := make([]gin.H, 0)
	for _, item := range items {
		row := formatHostnameRouteRow(item)
		rows = append(rows, row)
	}

	c.JSON(http.StatusOK, gin.H{
		"code":  0,
		"data":  rows,
		"total": len(rows),
		"rows":  rows,
	})
}

// AddHostnameRoute 添加主机名路由
func AddHostnameRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	hostname := cfString(body, c, "hostname")
	comment := cfString(body, c, "comment")

	if tunnelID == "" || hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 hostname 不能为空"})
		return
	}
	if !cfValidHostname(hostname) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "主机名格式不正确"})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, localTunnelID, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	result, err := svc.CreateHostnameRoute(accountID, cfTunnelID, hostname, comment)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	route := models.CloudflareHostnameRoute{
		TunnelID: localTunnelID,
		Hostname: hostname,
		Comment:  comment,
	}
	if routeID, ok := result["id"].(string); ok {
		route.RouteID = routeID
	}
	if localTunnelID > 0 {
		database.DB.Create(&route)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": result,
	})
}

// DeleteHostnameRoute 删除主机名路由
func DeleteHostnameRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	routeID := cfString(body, c, "route_id")

	if tunnelID == "" || routeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 route_id 不能为空"})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	items, err := svc.ListHostnameRoutes(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	if !cfRouteExists(items, routeID) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "主机名路由不存在或不属于当前 Tunnel"})
		return
	}

	err = svc.DeleteHostnameRoute(accountID, routeID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	database.DB.Where("route_id = ?", routeID).Delete(&models.CloudflareHostnameRoute{})

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "删除成功",
	})
}

func GetTunnelPublicHostnames(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error(), "total": 0, "rows": []gin.H{}, "data": []gin.H{}})
		return
	}
	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	if tunnelID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 tunnel_id", "total": 0, "rows": []gin.H{}, "data": []gin.H{}})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID", "total": 0, "rows": []gin.H{}, "data": []gin.H{}})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error(), "total": 0, "rows": []gin.H{}, "data": []gin.H{}})
		return
	}

	raw, err := svc.GetTunnelConfig(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error(), "total": 0, "rows": []gin.H{}, "data": []gin.H{}})
		return
	}
	rows := cfPublicHostnameRows(cfTunnelConfig(raw))
	for _, row := range rows {
		zone, _ := cfFindBestMatchingDomain(account.ID, cfCandidateString(row, "hostname"))
		if zone != nil {
			row["zone_name"] = zone.Name
			row["zone_id"] = zone.ThirdID
		} else {
			row["zone_name"] = ""
			row["zone_id"] = ""
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": rows, "total": len(rows), "rows": rows})
}

func SaveTunnelPublicHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	hostname := cfString(body, c, "hostname")
	serviceValue := cfString(body, c, "service")
	path := cfString(body, c, "path")
	if tunnelID == "" || hostname == "" || serviceValue == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "Tunnel、主机名、服务地址不能为空"})
		return
	}
	if !cfValidHostname(hostname) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "主机名格式不正确"})
		return
	}

	zone, err := cfFindBestMatchingDomain(account.ID, hostname)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	if zone == nil || zone.ThirdID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "未找到匹配的本地域名，请先在当前 Cloudflare 账户下导入该主机名所属主域"})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	raw, err := svc.GetTunnelConfig(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	config := cfTunnelConfig(raw)
	oldConfig := cfCloneConfig(config)
	ingress := cfTunnelIngress(config)
	rule := map[string]interface{}{"hostname": hostname, "service": serviceValue}
	if path != "" {
		rule["path"] = path
	}

	if existingIndex := cfFindPublicHostnameIndex(ingress, hostname, path); existingIndex >= 0 {
		next, _ := cfAsIngressRule(ingress[existingIndex])
		if next == nil {
			next = map[string]interface{}{}
		}
		for key, value := range rule {
			next[key] = value
		}
		if path == "" {
			delete(next, "path")
		}
		ingress[existingIndex] = next
	} else if fallbackIndex := cfFindFallbackIngressIndex(ingress); fallbackIndex >= 0 {
		ingress = append(ingress[:fallbackIndex], append([]interface{}{rule}, ingress[fallbackIndex:]...)...)
	} else {
		ingress = append(ingress, rule)
	}
	config["ingress"] = cfEnsureFallbackIngress(ingress)

	if err := svc.UpdateTunnelConfig(accountID, cfTunnelID, config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	dnsResult, err := svc.UpsertTunnelCnameRecord(zone.ThirdID, hostname, cfTunnelID)
	if err != nil {
		_ = svc.UpdateTunnelConfig(accountID, cfTunnelID, oldConfig)
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "Public Hostname 已回滚：" + err.Error()})
		return
	}

	addCFLog(account.ID, zone.Name, "配置 Tunnel 公网主机名", fmt.Sprintf("%s -> %s [%s]", hostname, serviceValue, dnsResult["action"]))
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "配置 Public Hostname 成功"})
}

func DeleteTunnelPublicHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFAccountContextForRequest(c, uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	body := cfRequestBody(c)
	tunnelID := cfString(body, c, "tunnel_id")
	hostname := cfString(body, c, "hostname")
	path := cfString(body, c, "path")
	if tunnelID == "" || hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "缺少 tunnel_id 或 hostname"})
		return
	}

	accountID, err := cfAccountID(svc, account)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "无法获取 Cloudflare Account ID"})
		return
	}
	cfTunnelID, _, err := cfResolveTunnel(account.ID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	raw, err := svc.GetTunnelConfig(accountID, cfTunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}
	config := cfTunnelConfig(raw)
	oldConfig := cfCloneConfig(config)
	nextIngress := make([]interface{}, 0)
	for _, item := range cfTunnelIngress(config) {
		rule, ok := cfAsIngressRule(item)
		if !ok {
			continue
		}
		match := cfNormalizeHostname(cfMapString(rule, "hostname")) == cfNormalizeHostname(hostname) && cfMapString(rule, "path") == path
		if !match {
			nextIngress = append(nextIngress, rule)
		}
	}
	config["ingress"] = cfEnsureFallbackIngress(nextIngress)
	if err := svc.UpdateTunnelConfig(accountID, cfTunnelID, config); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	zone, _ := cfFindBestMatchingDomain(account.ID, hostname)
	if zone != nil && zone.ThirdID != "" {
		if _, err := svc.DeleteTunnelCnameRecordIfMatch(zone.ThirdID, hostname, cfTunnelID); err != nil {
			_ = svc.UpdateTunnelConfig(accountID, cfTunnelID, oldConfig)
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "删除 Public Hostname 时已回滚：" + err.Error()})
			return
		}
	}

	addCFLog(account.ID, hostname, "删除 Tunnel 公网主机名", hostname)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除 Public Hostname 成功"})
}

// ========== 辅助函数 ==========

// formatCustomHostnameRow 格式化自定义主机名数据
func formatCustomHostnameRow(item interface{}) gin.H {
	row, _ := item.(map[string]interface{})
	result := gin.H{}

	if id, ok := row["id"].(string); ok {
		result["id"] = id
	}
	if hostname, ok := row["hostname"].(string); ok {
		result["hostname"] = hostname
	}
	if origin, ok := row["custom_origin_server"].(string); ok {
		result["custom_origin_server"] = origin
	}
	if status, ok := row["status"].(string); ok {
		result["status"] = status
	}

	if ssl, ok := row["ssl"].(map[string]interface{}); ok {
		result["ssl"] = ssl
		if s, ok := ssl["status"].(string); ok {
			result["ssl_status"] = s
		}
		if m, ok := ssl["method"].(string); ok {
			result["ssl_method"] = m
		}
		if settings, ok := ssl["settings"].(map[string]interface{}); ok {
			if tls, ok := settings["min_tls_version"].(string); ok {
				result["ssl_min_tls_version"] = tls
			}
		}
	}

	if created, ok := row["created_on"].(string); ok {
		result["created_on"] = created
		result["created_at"] = created
	}

	return result
}

// formatTunnelRow 格式化 Tunnel 数据
func formatTunnelRow(item interface{}) gin.H {
	row, _ := item.(map[string]interface{})
	result := gin.H{}

	if id, ok := row["id"].(string); ok {
		result["id"] = id
	}
	if name, ok := row["name"].(string); ok {
		result["name"] = name
	}
	if status, ok := row["status"].(string); ok {
		result["status"] = status
	}
	if createdAt, ok := row["created_at"].(string); ok {
		result["created_at"] = createdAt
	}
	if conns, ok := row["conns"].([]interface{}); ok {
		result["connection_count"] = len(conns)
	}

	return result
}

// formatCidrRouteRow 格式化 CIDR 路由数据
func formatCidrRouteRow(item interface{}) gin.H {
	row, _ := item.(map[string]interface{})
	result := gin.H{}

	if id, ok := row["id"].(string); ok {
		result["id"] = id
	}
	if network, ok := row["network"].(string); ok {
		result["network"] = network
	}
	if comment, ok := row["comment"].(string); ok {
		result["comment"] = comment
	}
	if tunnelID, ok := row["tunnel_id"].(string); ok {
		result["tunnel_id"] = tunnelID
	}
	if createdAt, ok := row["created_at"].(string); ok {
		result["created_at"] = createdAt
	}

	return result
}

// formatHostnameRouteRow 格式化主机名路由数据
func formatHostnameRouteRow(item interface{}) gin.H {
	row, _ := item.(map[string]interface{})
	result := gin.H{}

	if id, ok := row["id"].(string); ok {
		result["id"] = id
	}
	if hostname, ok := row["hostname"].(string); ok {
		result["hostname"] = hostname
	}
	if comment, ok := row["comment"].(string); ok {
		result["comment"] = comment
	}
	if tunnelID, ok := row["tunnel_id"].(string); ok {
		result["tunnel_id"] = tunnelID
	}
	if createdAt, ok := row["created_at"].(string); ok {
		result["created_at"] = createdAt
	}

	return result
}

// addCFLog 添加 Cloudflare 操作日志
func addCFLog(accountID uint, domain, action, data string) {
	log := models.Log{
		UserID: 0, // 从上下文获取
		Action: action,
		Entity: "cloudflare",
		Domain: domain,
		Data:   data,
	}
	if len(log.Data) > 500 {
		log.Data = log.Data[:500]
	}
	database.DB.Create(&log)
}

// GetDomainDefaultLine 获取域名默认线路
func GetDomainDefaultLine(c *gin.Context) {
	body := cfRequestBody(c)
	domainID := cfString(body, c, "domain_id")
	if domainID == "" {
		domainID = cfString(body, c, "domain")
	}
	if domainID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "domain_id 不能为空"})
		return
	}

	// Cloudflare 默认线路为 "0" (仅DNS)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"default_line": "0"},
	})
}
