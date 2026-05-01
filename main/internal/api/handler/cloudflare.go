package handler

import (
	"encoding/json"
	"fmt"
	"main/internal/database"
	"main/internal/models"
	"main/internal/service"
	"net/http"
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

// ========== 自定义主机名 ==========

// GetCustomHostnames 获取自定义主机名列表
func GetCustomHostnames(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, _, _, err := getCFDomainContext(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	page, _ := strconv.Atoi(c.DefaultPostForm("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultPostForm("pageSize", "10"))

	zoneID, _ := c.GetPostForm("zone_id")
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
		"total": total,
		"rows":  rows,
	})
}

// AddCustomHostname 添加自定义主机名
func AddCustomHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, account, err := getCFDomainContext(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	hostname := c.PostForm("hostname")
	customOrigin := c.PostForm("custom_origin_server")
	sslMethod := c.DefaultPostForm("ssl_method", "txt")
	minTLSVersion := c.DefaultPostForm("min_tls_version", "1.2")

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

	svc, domain, account, err := getCFDomainContext(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	hostnameID := c.PostForm("hostname_id")
	if hostnameID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "hostname_id 不能为空"})
		return
	}

	updates := make(map[string]interface{})

	if customOrigin := c.PostForm("custom_origin_server"); customOrigin != "" {
		updates["custom_origin_server"] = customOrigin
	}

	if sslMethod := c.PostForm("ssl_method"); sslMethod != "" {
		updates["ssl"] = map[string]interface{}{
			"method": sslMethod,
			"settings": map[string]interface{}{
				"min_tls_version": c.DefaultPostForm("min_tls_version", "1.2"),
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

	svc, domain, account, err := getCFDomainContext(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	hostnameID := c.PostForm("hostname_id")
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

// RefreshCustomHostname 刷新自定义主机名验证状态
func RefreshCustomHostname(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, domain, _, err := getCFDomainContext(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	hostnameID := c.PostForm("hostname_id")
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

	svc, domain, _, err := getCFDomainContext(uint(id))
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

	svc, domain, account, err := getCFDomainContext(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	origin := c.PostForm("origin")
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

	svc, domain, account, err := getCFDomainContext(uint(id))
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

	svc, domain, _, err := getCFDomainContext(uint(id))
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

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	accountID := account.Config
	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	if accountID == "" {
		accountID = configMap["account_id"]
	}

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
		"total":      len(rows),
		"rows":       rows,
		"account_id": accountID,
	})
}

// AddTunnel 添加 Tunnel
func AddTunnel(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	name := c.PostForm("name")
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

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	tunnelID := c.PostForm("tunnel_id")
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

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	tunnelID := c.PostForm("tunnel_id")
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
	tunnelID := c.PostForm("tunnel_id")

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
	}

	cfTunnel := models.CloudflareTunnel{}
	if tunnelID != "" {
		database.DB.Where("id = ?", tunnelID).First(&cfTunnel)
	}

	items, err := svc.ListCidrRoutes(accountID, cfTunnel.TunnelID)
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
		"total": len(rows),
		"rows":  rows,
	})
}

// AddCidrRoute 添加 CIDR 路由
func AddCidrRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	tunnelID := c.PostForm("tunnel_id")
	network := c.PostForm("network")
	comment := c.PostForm("comment")

	if tunnelID == "" || network == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 network 不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
	}

	var cfTunnel models.CloudflareTunnel
	if err := database.DB.Where("id = ?", tunnelID).First(&cfTunnel).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "Tunnel 不存在"})
		return
	}

	result, err := svc.CreateCidrRoute(accountID, cfTunnel.TunnelID, network, comment, "")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	// 保存本地记录
	route := models.CloudflareCIDRRoute{
		TunnelID: cfTunnel.ID,
		Network:  network,
		Comment:  comment,
	}
	if routeID, ok := result["id"].(string); ok {
		route.RouteID = routeID
	}
	database.DB.Create(&route)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": result,
	})
}

// DeleteCidrRoute 删除 CIDR 路由
func DeleteCidrRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	tunnelID := c.PostForm("tunnel_id")
	routeID := c.PostForm("route_id")

	if tunnelID == "" || routeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 route_id 不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
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
	tunnelID := c.PostForm("tunnel_id")

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
	}

	cfTunnel := models.CloudflareTunnel{}
	if tunnelID != "" {
		database.DB.Where("id = ?", tunnelID).First(&cfTunnel)
	}

	items, err := svc.ListHostnameRoutes(accountID, cfTunnel.TunnelID)
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
		"total": len(rows),
		"rows":  rows,
	})
}

// AddHostnameRoute 添加主机名路由
func AddHostnameRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	tunnelID := c.PostForm("tunnel_id")
	hostname := c.PostForm("hostname")
	comment := c.PostForm("comment")

	if tunnelID == "" || hostname == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 hostname 不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
	}

	var cfTunnel models.CloudflareTunnel
	if err := database.DB.Where("id = ?", tunnelID).First(&cfTunnel).Error; err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "Tunnel 不存在"})
		return
	}

	result, err := svc.CreateHostnameRoute(accountID, cfTunnel.TunnelID, hostname, comment)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	route := models.CloudflareHostnameRoute{
		TunnelID: cfTunnel.ID,
		Hostname: hostname,
		Comment:  comment,
	}
	if routeID, ok := result["id"].(string); ok {
		route.RouteID = routeID
	}
	database.DB.Create(&route)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "添加成功",
		"data": result,
	})
}

// DeleteHostnameRoute 删除主机名路由
func DeleteHostnameRoute(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	svc, account, err := getCFEnhanceService(uint(id))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": err.Error()})
		return
	}

	tunnelID := c.PostForm("tunnel_id")
	routeID := c.PostForm("route_id")

	if tunnelID == "" || routeID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": 1, "msg": "tunnel_id 和 route_id 不能为空"})
		return
	}

	var configMap map[string]string
	json.Unmarshal([]byte(account.Config), &configMap)
	accountID := configMap["account_id"]
	if accountID == "" {
		accountID, _ = svc.GetDefaultAccountID()
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
	domainID := c.PostForm("domain_id")
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
