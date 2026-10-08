package handler

import (
	"encoding/json"
	"fmt"
	"main/internal/api/middleware"
	"main/internal/cache"
	"main/internal/database"
	maindns "main/internal/dns"
	"main/internal/models"
	"main/internal/service"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// cfCacheKey 生成 Cloudflare 缓存 key
func cfCacheKey(handler string, id string) string {
	return "cf:" + handler + ":" + id
}

// cfCacheDelete 删除指定前缀的缓存
func cfCacheDelete(handler string, id string) {
	if cache.C == nil {
		return
	}
	_ = cache.C.Delete(cfCacheKey(handler, id))
}

// cfCacheDeletePrefix 删除指定前缀的所有缓存
func cfCacheDeletePrefix(handler string) {
	if cache.C == nil {
		return
	}
	_ = cache.C.DeletePrefix("cf:" + handler + ":")
}

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
		decrypted := account.Config
		json.Unmarshal([]byte(decrypted), &config)
	}

	email := config["email"]
	apiKey := config["apikey"]

	auth := 0
	if config["auth"] == "1" {
		auth = 1
	} else {
		auth = service.DetectAuthMode(apiKey)
	}

	svc := service.NewEnhanceService(email, apiKey, auth, config["account_id"])
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
	/*
	 * 该入口只服务 Tunnel 及其配套路由（公网主机名、CIDR、Hostname Route），
	 * 与 PHP getCloudflareAccountContext 的 requireTunnelApiToken=true 同义：
	 * Tunnels 的接口只认 API 令牌，Global API Key 会被 Cloudflare 拒绝，
	 * 提前拦下可避免用户拿到一串难懂的远端报错
	 *
	 * PHP 还在 service 层用 assertTunnelSupported 逐方法重复校验，Go 侧只保留这一处：
	 * 本函数是全部隧道入口的唯一出口，逐方法重复不增加覆盖面
	 */
	if !svc.IsApiTokenAuth() {
		return nil, nil, fmt.Errorf("Cloudflare Tunnels 仅支持 API 令牌认证，请将当前账户的认证方式切换为 API令牌")
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

/*
 * cfValidateCustomOrigin 校验自定义主机名的源站，返回空串表示通过
 *
 * 规则对齐 PHP 版 validateCustomOrigin，顺序也保持一致，让前端按序展示首个错误。
 * 不复用 cfValidHostname：后者按 CF 主机名建模——允许 *. 通配、强制至少两个 label、
 * 只认 a-z0-9 与连字符，而源站是探源域名，单 label（内网源站）与含下划线的记录名
 * 都应放行；反过来说通配符在源站上是非法的，cfValidHostname 却会放行。
 * 这里用的是 checkDomain 的同款判据：允许 _ 与 *.、首字符不得为点或连字符、
 * 末字符不得为点、长度不超过 253
 */
func cfValidateCustomOrigin(origin string) string {
	if origin == "" {
		return ""
	}
	if strings.Contains(origin, "://") {
		return "自定义源站不支持填写 http:// 或 https://"
	}
	if strings.Contains(origin, "*") {
		return "自定义源站不支持通配符"
	}
	if strings.Contains(origin, "/") {
		return "自定义源站格式不正确"
	}
	if cfOriginHasPortSuffix(origin) {
		return "自定义源站不支持端口"
	}
	if net.ParseIP(origin) != nil {
		return "自定义源站不支持 IP 地址，请填写域名"
	}
	if !cfValidOriginDomain(origin) {
		return "自定义源站格式不正确"
	}
	return ""
}

/* cfValidateSSLParams 校验自定义主机名的证书验证方式与最低 TLS 版本，返回空串表示通过 */
func cfValidateSSLParams(sslMethod, minTLSVersion string) string {
	if sslMethod != "txt" && sslMethod != "http" {
		return "证书验证方法无效"
	}
	if !cfValidMinTLSVersion(minTLSVersion) {
		return "最低 TLS 版本无效"
	}
	return ""
}

/* cfValidMinTLSVersion 判断最低 TLS 版本是否在 Cloudflare 接受的四档之内 */
func cfValidMinTLSVersion(version string) bool {
	switch version {
	case "1.0", "1.1", "1.2", "1.3":
		return true
	}
	return false
}

/*
 * cfOriginHasPortSuffix 判断源站末尾是否为「IPv4 形式的 host + 纯数字端口」
 *
 * 只认含点的 host：IPv6 字面量本身由冒号分隔，若把末尾的纯数字段一律当端口，
 * 2001:db8::1 会被误判成端口而不是 IP。PHP 用 FILTER_VALIDATE_IP 先于端口规则命中，
 * 这里让端口规则同样只覆盖带点形式，行为与之等价
 */
func cfOriginHasPortSuffix(origin string) bool {
	idx := strings.LastIndex(origin, ":")
	if idx < 0 || idx == len(origin)-1 {
		return false
	}
	if !strings.Contains(origin[:idx], ".") {
		return false
	}
	port := origin[idx+1:]
	if len(port) > 5 {
		return false
	}
	for _, r := range port {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

/* cfValidOriginDomain 校验源站域名格式，对应 PHP 的 checkDomain */
func cfValidOriginDomain(origin string) bool {
	if len(origin) < 4 || len(origin) > 253 {
		return false
	}
	if !strings.Contains(origin, ".") {
		return false
	}
	if strings.HasPrefix(origin, ".") || strings.HasPrefix(origin, "-") || strings.HasSuffix(origin, ".") {
		return false
	}
	for _, r := range origin {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_':
		default:
			return false
		}
	}
	return true
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

	// 检查缓存
	cacheKey := cfCacheKey("hostnames", fmt.Sprintf("%d:%s:%d:%d", id, zoneID, page, pageSize))
	if cache.C != nil {
		var cached gin.H
		if cache.C.GetJSON(cacheKey, &cached) {
			c.JSON(http.StatusOK, cached)
			return
		}
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

	result := gin.H{
		"code":  0,
		"data":  rows,
		"total": total,
		"rows":  rows,
	}

	// 写入缓存 2min
	if cache.C != nil {
		_ = cache.C.SetJSON(cacheKey, result, 2*time.Minute)
	}

	c.JSON(http.StatusOK, result)
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

	if msg := cfValidateSSLParams(sslMethod, minTLSVersion); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
		return
	}
	if msg := cfValidateCustomOrigin(customOrigin); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
		return
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

	// 清除 hostnames 缓存
	cfCacheDeletePrefix("hostnames")

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

	// 单条更新对齐 PHP hostnames_update：源站与 SSL 参数都会下发，因此按必填档校验
	if msg := cfValidateSSLParams(cfStringDefault(body, c, "ssl_method", "txt"), cfStringDefault(body, c, "min_tls_version", "1.0")); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
		return
	}
	if msg := cfValidateCustomOrigin(cfString(body, c, "custom_origin_server")); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
		return
	}

	if customOrigin := cfString(body, c, "custom_origin_server"); customOrigin != "" {
		updates["custom_origin_server"] = customOrigin
	}

	if sslMethod := cfString(body, c, "ssl_method"); sslMethod != "" {
		sslPayload := map[string]interface{}{
			"method": sslMethod,
			"type":   "dv",
		}
		minTLS := cfStringDefault(body, c, "min_tls_version", "1.0")
		if minTLS != "" {
			sslPayload["settings"] = map[string]interface{}{
				"min_tls_version": minTLS,
			}
		}
		updates["ssl"] = sslPayload
	}

	result, err := svc.UpdateCustomHostname(domain.ThirdID, hostnameID, updates)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	addCFLog(account.ID, domain.Name, "更新自定义主机名", hostnameID)

	// 清除 hostnames 缓存
	cfCacheDeletePrefix("hostnames")

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

	// 清除 hostnames 缓存
	cfCacheDeletePrefix("hostnames")

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
	if msg := cfValidateSSLParams(sslMethod, minTLSVersion); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
		return
	}
	if msg := cfValidateCustomOrigin(customOrigin); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
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
	// 清除 hostnames 缓存
	cfCacheDeletePrefix("hostnames")
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
	// 批量更新允许留空表示「保持不变」，因此仅对显式传入的值校验
	if sslMethod != "" && sslMethod != "txt" && sslMethod != "http" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "证书验证方法无效"})
		return
	}
	if minTLSVersion != "" && !cfValidMinTLSVersion(minTLSVersion) {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "最低 TLS 版本无效"})
		return
	}
	if msg := cfValidateCustomOrigin(customOrigin); msg != "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": msg})
		return
	}

	updates := make(map[string]interface{})
	updates["custom_origin_server"] = customOrigin
	if sslMethod != "" || minTLSVersion != "" {
		ssl := map[string]interface{}{
			"type": "dv",
		}
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
	// 清除 hostnames 缓存
	cfCacheDeletePrefix("hostnames")
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
	// 清除 hostnames 缓存
	cfCacheDeletePrefix("hostnames")
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

// SetupHostnameValidation 一键配置 custom hostname 验证所需的 DNS 记录
func SetupHostnameValidation(c *gin.Context) {
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

	// 从 Cloudflare 获取最新验证信息
	result, err := svc.GetCustomHostname(domain.ThirdID, hostnameID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "获取主机名信息失败: " + err.Error()})
		return
	}

	hostname, _ := result["hostname"].(string)
	if hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "主机名为空"})
		return
	}

	// 如果 SSL 已过期或错误状态，先 PATCH SSL 触发重新验证
	sslStatus := ""
	sslMethod := "http"
	if ssl, ok := result["ssl"].(map[string]interface{}); ok {
		sslStatus, _ = ssl["status"].(string)
		if m, _ := ssl["method"].(string); m != "" {
			sslMethod = m
		}
	}
	if sslStatus == "expired" || sslStatus == "deleted" || sslStatus == "validation_timed_out" {
		updates := map[string]interface{}{
			"ssl": map[string]interface{}{
				"method": sslMethod,
				"type":   "dv",
			},
		}
		newResult, patchErr := svc.UpdateCustomHostname(domain.ThirdID, hostnameID, updates)
		if patchErr != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "重新激活 SSL 失败: " + patchErr.Error()})
			return
		}
		result = newResult
	}

	type dnsTask struct {
		RecordName string
		RecordType string
		Value      string
		Purpose    string
	}

	var tasks []dnsTask

	// 收集 ownership_verification TXT 记录
	if ov, ok := result["ownership_verification"].(map[string]interface{}); ok {
		if ovType, _ := ov["type"].(string); strings.EqualFold(ovType, "txt") {
			if name, _ := ov["name"].(string); name != "" {
				if value, _ := ov["value"].(string); value != "" {
					tasks = append(tasks, dnsTask{
						RecordName: name,
						RecordType: "TXT",
						Value:      value,
						Purpose:    "ownership_verification",
					})
				}
			}
		}
	}

	// 收集 SSL validation_records
	if ssl, ok := result["ssl"].(map[string]interface{}); ok {
		if valRecords, ok := ssl["validation_records"].([]interface{}); ok {
			for _, vr := range valRecords {
				rec, ok := vr.(map[string]interface{})
				if !ok {
					continue
				}
				if txtName, _ := rec["txt_name"].(string); txtName != "" {
					if txtValue, _ := rec["txt_value"].(string); txtValue != "" {
						tasks = append(tasks, dnsTask{
							RecordName: txtName,
							RecordType: "TXT",
							Value:      txtValue,
							Purpose:    "ssl_validation",
						})
					}
				}
				if cnameName, _ := rec["cname"].(string); cnameName != "" {
					if cnameTarget, _ := rec["cname_target"].(string); cnameTarget != "" {
						tasks = append(tasks, dnsTask{
							RecordName: cnameName,
							RecordType: "CNAME",
							Value:      cnameTarget,
							Purpose:    "ssl_validation",
						})
					}
				}
			}
		}
	}

	// 如果没有单独的 validation records，添加 DCV delegation CNAME
	if len(tasks) == 0 || sslStatus == "expired" || sslStatus == "deleted" {
		dcvUuid, dcvErr := svc.GetDcvDelegationUUID(domain.ThirdID)
		if dcvErr == nil && dcvUuid != "" {
			tasks = append(tasks, dnsTask{
				RecordName: "_acme-challenge." + hostname,
				RecordType: "CNAME",
				Value:      hostname + "." + dcvUuid + ".dcv.cloudflare.com",
				Purpose:    "dcv_delegation",
			})
		}
	}

	if len(tasks) == 0 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "无需配置验证记录（可能已验证通过或验证信息为空）", "data": gin.H{"added": 0, "results": []gin.H{}}})
		return
	}

	// 对每条验证记录尝试找到面板中匹配的域名并添加
	var results []gin.H
	added := 0
	for _, task := range tasks {
		recordFQDN := task.RecordName
		targetDomain, recordName := cfMatchFQDNToDomain(recordFQDN)
		if targetDomain == nil {
			results = append(results, gin.H{
				"fqdn":    recordFQDN,
				"type":    task.RecordType,
				"purpose": task.Purpose,
				"status":  "skipped",
				"msg":     "未找到匹配的托管域名",
			})
			continue
		}

		provider, err := getDNSProviderByDomain(targetDomain)
		if err != nil {
			results = append(results, gin.H{
				"fqdn":    recordFQDN,
				"type":    task.RecordType,
				"purpose": task.Purpose,
				"status":  "error",
				"msg":     "获取DNS服务商失败: " + err.Error(),
			})
			continue
		}

		ctx := c.Request.Context()

		// 确定 TTL 列表：如果域名已保存 MinTTL，直接使用；否则按 1→60→600 探测
		ttlList := []int{1, 60, 600}
		if targetDomain.MinTTL > 0 {
			ttlList = []int{targetDomain.MinTTL}
		}

		recordID, skipped, usedTTL, addErr := maindns.EnsureChallengeRecordWithTTLProbe(ctx, provider, recordName, task.RecordType, task.Value, "", ttlList, "cf-hostname-validation")
		if addErr != nil {
			results = append(results, gin.H{
				"fqdn":    recordFQDN,
				"type":    task.RecordType,
				"purpose": task.Purpose,
				"status":  "error",
				"msg":     addErr.Error(),
			})
			continue
		}

		// 如果探测到新的 MinTTL，保存到域名
		if usedTTL > 0 && targetDomain.MinTTL == 0 {
			database.DB.Model(targetDomain).Update("min_ttl", usedTTL)
		}

		status := "added"
		if skipped {
			status = "exists"
		} else {
			added++
		}
		results = append(results, gin.H{
			"fqdn":      recordFQDN,
			"type":      task.RecordType,
			"purpose":   task.Purpose,
			"status":    status,
			"record_id": recordID,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  fmt.Sprintf("配置完成，新增 %d 条记录", added),
		"data": gin.H{
			"added":   added,
			"results": results,
		},
	})
}

// cfMatchFQDNToDomain 根据 FQDN 找到面板中最匹配的域名和记录名
func cfMatchFQDNToDomain(fqdn string) (*models.Domain, string) {
	fqdn = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(fqdn)), ".")

	var domains []models.Domain
	database.DB.Find(&domains)

	var bestDomain *models.Domain
	bestLength := 0

	for i := range domains {
		domainName := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domains[i].Name)), ".")
		if fqdn == domainName || strings.HasSuffix(fqdn, "."+domainName) {
			if len(domainName) > bestLength {
				bestDomain = &domains[i]
				bestLength = len(domainName)
			}
		}
	}

	if bestDomain == nil {
		return nil, ""
	}

	domainName := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(bestDomain.Name)), ".")
	recordName := "@"
	if fqdn != domainName {
		recordName = strings.TrimSuffix(fqdn, "."+domainName)
	}
	return bestDomain, recordName
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

	// 检查缓存
	cacheKey := cfCacheKey("fallback", strconv.FormatUint(id, 10))
	if cache.C != nil {
		var cached gin.H
		if cache.C.GetJSON(cacheKey, &cached) {
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	origin, err := svc.GetFallbackOrigin(domain.ThirdID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	result := gin.H{
		"code": 0,
		"data": gin.H{"origin": origin},
	}

	// 写入缓存 60s
	if cache.C != nil {
		_ = cache.C.SetJSON(cacheKey, result, 60*time.Second)
	}

	c.JSON(http.StatusOK, result)
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

	// 清除缓存
	cfCacheDelete("fallback", strconv.FormatUint(id, 10))

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

	// 清除缓存
	cfCacheDelete("fallback", strconv.FormatUint(id, 10))

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

	// 检查缓存
	cacheKey := cfCacheKey("dcv", strconv.FormatUint(id, 10))
	if cache.C != nil {
		var cached gin.H
		if cache.C.GetJSON(cacheKey, &cached) {
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	uuid, err := svc.GetDcvDelegationUUID(domain.ThirdID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	result := gin.H{
		"code": 0,
		"data": gin.H{"uuid": uuid},
	}

	// 写入缓存 60s
	if cache.C != nil {
		_ = cache.C.SetJSON(cacheKey, result, 60*time.Second)
	}

	c.JSON(http.StatusOK, result)
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

	// 检查缓存
	cacheKey := cfCacheKey("tunnels", strconv.FormatUint(id, 10))
	if cache.C != nil {
		var cached gin.H
		if cache.C.GetJSON(cacheKey, &cached) {
			c.JSON(http.StatusOK, cached)
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

	result := gin.H{
		"code":       0,
		"data":       rows,
		"total":      len(rows),
		"rows":       rows,
		"account_id": accountID,
	}

	// 写入缓存 2min
	if cache.C != nil {
		_ = cache.C.SetJSON(cacheKey, result, 2*time.Minute)
	}

	c.JSON(http.StatusOK, result)
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

	// 清除 Tunnel 列表缓存
	cfCacheDelete("tunnels", strconv.FormatUint(id, 10))

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

	// 清除 Tunnel 列表缓存和 Token 缓存
	cfCacheDelete("tunnels", strconv.FormatUint(id, 10))
	cfCacheDelete("tunnel_token", fmt.Sprintf("%d:%s", id, tunnelID))

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

	// 检查缓存
	cacheKey := cfCacheKey("tunnel_token", fmt.Sprintf("%d:%s", id, tunnelID))
	if cache.C != nil {
		var cached gin.H
		if cache.C.GetJSON(cacheKey, &cached) {
			c.JSON(http.StatusOK, cached)
			return
		}
	}

	token, err := svc.GetTunnelToken(accountID, tunnelID)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	result := gin.H{
		"code": 0,
		"data": gin.H{"token": token},
	}

	// 写入缓存 60s
	if cache.C != nil {
		_ = cache.C.SetJSON(cacheKey, result, 60*time.Second)
	}

	c.JSON(http.StatusOK, result)
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
	if sni, ok := row["custom_origin_sni"].(string); ok {
		result["custom_origin_sni"] = sni
	}
	if status, ok := row["status"].(string); ok {
		result["status"] = status
	}
	if verStatus, ok := row["verification_status"].(string); ok {
		result["verification_status"] = verStatus
	}

	if ssl, ok := row["ssl"].(map[string]interface{}); ok {
		result["ssl"] = ssl
		if s, ok := ssl["status"].(string); ok {
			result["ssl_status"] = s
		}
		if m, ok := ssl["method"].(string); ok {
			result["ssl_method"] = m
		}
		if t, ok := ssl["type"].(string); ok {
			result["ssl_type"] = t
		}
		if ca, ok := ssl["certificate_authority"].(string); ok {
			result["ssl_certificate_authority"] = ca
		}
		if settings, ok := ssl["settings"].(map[string]interface{}); ok {
			if tls, ok := settings["min_tls_version"].(string); ok {
				result["ssl_min_tls_version"] = tls
			}
		}
		if valRecords, ok := ssl["validation_records"].([]interface{}); ok {
			result["ssl_validation_records"] = valRecords
		}
		if valErrors, ok := ssl["validation_errors"].([]interface{}); ok {
			result["validation_errors"] = valErrors
		}
		if expiry, ok := ssl["expires_on"].(string); ok {
			result["ssl_expires_on"] = expiry
		}
	}

	if ov, ok := row["ownership_verification"].(map[string]interface{}); ok {
		result["ownership_verification"] = ov
	}
	if ovHTTP, ok := row["ownership_verification_http"].(map[string]interface{}); ok {
		result["ownership_verification_http"] = ovHTTP
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
	} else {
		result["status"] = "unknown"
	}
	if createdAt, ok := row["created_at"].(string); ok {
		result["created_at"] = createdAt
	}
	if deletedAt, ok := row["deleted_at"].(string); ok {
		result["deleted_at"] = deletedAt
	}
	if connsActiveAt, ok := row["conns_active_at"].(string); ok {
		result["conns_active_at"] = connsActiveAt
	}
	if connections, ok := row["connections"].([]interface{}); ok {
		result["connections"] = connections
		result["connection_count"] = len(connections)
	} else {
		result["connections"] = []interface{}{}
		result["connection_count"] = 0
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

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"default_line": cfDefaultLine(c, domainID)},
	})
}

// cfDefaultLine 解析域名对应的默认线路，供自定义主机名的验证记录写入使用
// 入参既接受面板域名 ID，也接受域名本身；查不到时统一回退 "0"（Cloudflare 仅 DNS）
func cfDefaultLine(c *gin.Context, domainID string) string {
	const fallback = "0"

	if !middleware.UserModuleAllowed(c, "domain") {
		return fallback
	}

	var domain models.Domain
	query := database.WithContext(c)
	if id, err := strconv.ParseUint(strings.TrimSpace(domainID), 10, 32); err == nil {
		query = query.Where("id = ?", uint(id))
	} else {
		query = query.Where("name = ?", strings.TrimSpace(domainID))
	}
	if err := query.First(&domain).Error; err != nil {
		return fallback
	}

	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		return fallback
	}

	// 服务商线路列表的第一条即默认线路；Cloudflare 恒为 "0"（仅 DNS），无需调用 API
	if provider, err := getDNSProviderByDomain(&domain); err == nil && provider != nil {
		if lines, lerr := provider.GetRecordLine(c.Request.Context()); lerr == nil && len(lines) > 0 {
			if line := strings.TrimSpace(lines[0].ID); line != "" {
				return line
			}
		}
	}

	// 服务商不可用或未声明线路时，回退到静态映射，仍然失败才用 fallback
	var account models.Account
	if err := database.WithContext(c).Where("id = ?", domain.AccountID).First(&account).Error; err != nil {
		return fallback
	}
	if line := strings.TrimSpace(maindns.DefaultDNSLine(account.Type)); line != "" {
		return line
	}
	return fallback
}
