package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"main/internal/cert"
	"main/internal/cert/deploy"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

func GetCertAccounts(c *gin.Context) {
	isDeploy := c.Query("deploy") == "1"
	var accounts []models.CertAccount

	query := database.DB
	if isDeploy {
		query = query.Where("is_deploy = ?", true)
	} else {
		query = query.Where("is_deploy = ?", false)
	}
	query.Find(&accounts)

	result := make([]gin.H, 0, len(accounts))
	for _, acc := range accounts {
		cfg, _ := cert.GetProviderConfig(acc.Type)
		result = append(result, gin.H{
			"id":         acc.ID,
			"type":       acc.Type,
			"type_name":  cfg.Name,
			"name":       acc.Name,
			"config":     acc.Config,
			"remark":     acc.Remark,
			"is_deploy":  acc.IsDeploy,
			"created_at": acc.CreatedAt,
		})
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

type CreateCertAccountRequest struct {
	Type     string                 `json:"type" binding:"required"`
	Name     string                 `json:"name" binding:"required"`
	Config   map[string]interface{} `json:"config" binding:"required"`
	Remark   string                 `json:"remark"`
	IsDeploy bool                   `json:"is_deploy"`
}

func CreateCertAccount(c *gin.Context) {
	var req CreateCertAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	account := models.CertAccount{
		Type:     req.Type,
		Name:     req.Name,
		Config:   string(configJSON),
		Remark:   req.Remark,
		IsDeploy: req.IsDeploy,
	}

	if err := database.DB.Create(&account).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": gin.H{"id": account.ID}})
}

func UpdateCertAccount(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var account models.CertAccount
	if err := database.DB.First(&account, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "账户不存在"})
		return
	}

	var req CreateCertAccountRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	updates := map[string]interface{}{
		"name":   req.Name,
		"config": string(configJSON),
		"remark": req.Remark,
	}

	database.DB.Model(&account).Updates(updates)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

func DeleteCertAccount(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Delete(&models.CertAccount{}, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

func GetCertOrders(c *gin.Context) {
	type OrderResult struct {
		models.CertOrder
		AccountName string `json:"account_name"`
	}
	var dbOrders []OrderResult

	database.DB.Table("cert_orders").
		Select("cert_orders.*, cert_accounts.name as account_name").
		Joins("LEFT JOIN cert_accounts ON cert_orders.aid = cert_accounts.id").
		Find(&dbOrders)

	type OrderResponse struct {
		OrderResult
		Domains []string `json:"domains"`
	}
	orders := make([]OrderResponse, len(dbOrders))
	for i := range dbOrders {
		orders[i].OrderResult = dbOrders[i]
		orders[i].Domains = []string{}
		var domains []models.CertDomain
		database.DB.Where("oid = ?", dbOrders[i].ID).Order("sort ASC").Find(&domains)
		for _, d := range domains {
			orders[i].Domains = append(orders[i].Domains, d.Domain)
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": orders})
}

type CreateCertOrderRequest struct {
	AccountID uint     `json:"account_id" binding:"required"`
	Domains   []string `json:"domains" binding:"required"`
	KeyType   string   `json:"key_type"`
	KeySize   string   `json:"key_size"`
	IsAuto    bool     `json:"is_auto"`
}

func CreateCertOrder(c *gin.Context) {
	var req CreateCertOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	if req.KeyType == "" {
		req.KeyType = "RSA"
	}
	if req.KeySize == "" {
		req.KeySize = "2048"
	}

	order := models.CertOrder{
		AccountID: req.AccountID,
		KeyType:   req.KeyType,
		KeySize:   req.KeySize,
		IsAuto:    req.IsAuto,
		Status:    0,
	}

	if err := database.DB.Create(&order).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建失败"})
		return
	}

	for i, domain := range req.Domains {
		certDomain := models.CertDomain{
			OrderID: order.ID,
			Domain:  domain,
			Sort:    i,
		}
		database.DB.Create(&certDomain)
	}

	// 异步处理证书申请
	if order.IsAuto {
		// 获取证书账户
		var account models.CertAccount
		if err := database.DB.First(&account, order.AccountID).Error; err == nil {
			// 解析账户配置
			var config map[string]interface{}
			if account.Config != "" {
				json.Unmarshal([]byte(account.Config), &config)
			}
			var ext map[string]interface{}
			if account.Ext != "" {
				json.Unmarshal([]byte(account.Ext), &ext)
			}

			// 获取证书提供商
			provider, err := cert.GetProvider(account.Type, config, ext)
			if err == nil && provider != nil {
				provider.SetLogger(func(msg string) {
					appendOrderLog(&order, msg)
				})

				// 更新状态为处理中
				order.Status = 1
				database.DB.Save(&order)

				go processCertOrderAsync(&order, provider, req.Domains, &account)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": gin.H{"id": order.ID}})
}

func ProcessCertOrder(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var order models.CertOrder
	if err := database.DB.First(&order, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "订单不存在"})
		return
	}

	// 获取订单的域名列表
	var certDomains []models.CertDomain
	database.DB.Where("oid = ?", order.ID).Order("sort ASC").Find(&certDomains)
	if len(certDomains) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "订单没有域名"})
		return
	}

	domains := make([]string, len(certDomains))
	for i, d := range certDomains {
		domains[i] = d.Domain
	}

	// 获取证书账户
	var account models.CertAccount
	if err := database.DB.First(&account, order.AccountID).Error; err != nil {
		appendOrderLog(&order, "错误: 证书账户不存在")
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "证书账户不存在"})
		return
	}

	appendOrderLog(&order, "开始处理证书申请...")
	appendOrderLog(&order, "域名: "+joinDomains(domains))
	appendOrderLog(&order, "证书账户: "+account.Name+" ("+account.Type+")")

	// 解析账户配置
	var config map[string]interface{}
	if account.Config != "" {
		json.Unmarshal([]byte(account.Config), &config)
	}
	var ext map[string]interface{}
	if account.Ext != "" {
		json.Unmarshal([]byte(account.Ext), &ext)
	}

	// 获取证书提供商
	provider, err := cert.GetProvider(account.Type, config, ext)
	if err != nil {
		appendOrderLog(&order, "错误: 获取证书提供商失败 - "+err.Error())
		order.Status = -1
		order.Error = err.Error()
		database.DB.Save(&order)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "获取证书提供商失败: " + err.Error()})
		return
	}

	if provider == nil {
		appendOrderLog(&order, "错误: 证书提供商未实现 - "+account.Type)
		order.Status = -1
		order.Error = "证书提供商未实现"
		database.DB.Save(&order)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "证书提供商未实现"})
		return
	}

	// 设置日志记录器
	provider.SetLogger(func(msg string) {
		appendOrderLog(&order, msg)
	})

	appendOrderLog(&order, "正在创建证书订单...")

	// 更新状态为处理中
	order.Status = 1
	database.DB.Save(&order)

	// 异步处理证书申请
	go processCertOrderAsync(&order, provider, domains, &account)

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "证书申请已开始处理"})
}

// TriggerCertOrderProcessing 供自动续期等后台任务调用，与 ProcessCertOrder 相同的 ACME 异步流程（无 HTTP）
func TriggerCertOrderProcessing(orderID uint) {
	var order models.CertOrder
	if err := database.DB.First(&order, orderID).Error; err != nil {
		logger.Error("[CertOrder] Trigger: 订单 %d 不存在: %v", orderID, err)
		return
	}

	var certDomains []models.CertDomain
	database.DB.Where("oid = ?", order.ID).Order("sort ASC").Find(&certDomains)
	if len(certDomains) == 0 {
		appendOrderLog(&order, "错误: 订单没有域名")
		return
	}
	domains := make([]string, len(certDomains))
	for i, d := range certDomains {
		domains[i] = d.Domain
	}

	var account models.CertAccount
	if err := database.DB.First(&account, order.AccountID).Error; err != nil {
		appendOrderLog(&order, "错误: 证书账户不存在")
		return
	}

	appendOrderLog(&order, "自动续期触发处理...")
	appendOrderLog(&order, "域名: "+joinDomains(domains))
	appendOrderLog(&order, "证书账户: "+account.Name+" ("+account.Type+")")

	var config map[string]interface{}
	if account.Config != "" {
		json.Unmarshal([]byte(account.Config), &config)
	}
	var ext map[string]interface{}
	if account.Ext != "" {
		json.Unmarshal([]byte(account.Ext), &ext)
	}

	provider, err := cert.GetProvider(account.Type, config, ext)
	if err != nil {
		appendOrderLog(&order, "错误: 获取证书提供商失败 - "+err.Error())
		order.Status = -1
		order.Error = err.Error()
		database.DB.Save(&order)
		return
	}
	if provider == nil {
		appendOrderLog(&order, "错误: 证书提供商未实现 - "+account.Type)
		order.Status = -1
		order.Error = "证书提供商未实现"
		database.DB.Save(&order)
		return
	}

	provider.SetLogger(func(msg string) {
		appendOrderLog(&order, msg)
	})
	appendOrderLog(&order, "正在创建证书订单...")
	order.Status = 1
	database.DB.Save(&order)
	go processCertOrderAsync(&order, provider, domains, &account)
}

func appendOrderLog(order *models.CertOrder, msg string) {
	timestamp := time.Now().Format("2006-01-02 15:04:05")
	logLine := "[" + timestamp + "] " + msg + "\n"
	order.Info += logLine
	database.DB.Model(order).Update("info", order.Info)
	// 同时输出到控制台
	fmt.Printf("[CertOrder %d] %s\n", order.ID, msg)
}

func joinDomains(domains []string) string {
	result := ""
	for i, d := range domains {
		if i > 0 {
			result += ", "
		}
		result += d
	}
	return result
}

func processCertOrderAsync(order *models.CertOrder, provider cert.Provider, domains []string, account *models.CertAccount) {
	ctx := context.Background()

	// 创建订单信息
	orderInfo := &cert.OrderInfo{}

	// 创建证书订单
	dnsRecords, err := provider.CreateOrder(ctx, domains, orderInfo, order.KeyType, order.KeySize)
	if err != nil {
		appendOrderLog(order, "错误: 创建证书订单失败 - "+err.Error())
		order.Status = -2
		order.Error = err.Error()
		database.DB.Save(order)
		return
	}

	appendOrderLog(order, "证书订单创建成功")

	// 保存订单信息
	orderInfoBytes, _ := json.Marshal(orderInfo)
	order.ProcessID = orderInfo.OrderURL

	// 保存DNS验证记录并尝试自动添加
	dnsInfo := ""
	addedRecords := 0
	for domainName, records := range dnsRecords {
		dnsInfo += "域名 " + domainName + " 需要添加以下DNS记录:\n"
		for _, r := range records {
			dnsInfo += "  记录名: " + r.Name + "\n"
			dnsInfo += "  记录类型: " + r.Type + "\n"
			dnsInfo += "  记录值: " + r.Value + "\n"
			appendOrderLog(order, "DNS验证记录 - "+r.Name+"."+domainName+" -> "+r.Value)

			// 尝试自动添加DNS记录
			if addDNSRecord(ctx, order, domainName, r.Name, r.Value) {
				addedRecords++
			}
		}
	}
	order.DNS = dnsInfo

	// 保存账户扩展信息（如账户密钥等）
	if account.Ext == "" {
		// 首次使用，保存账户信息
		extBytes, _ := json.Marshal(map[string]interface{}{
			"order_info": string(orderInfoBytes),
		})
		account.Ext = string(extBytes)
		database.DB.Save(account)
	}

	if addedRecords > 0 {
		appendOrderLog(order, fmt.Sprintf("已自动添加 %d 条DNS验证记录", addedRecords))
		appendOrderLog(order, "等待DNS记录生效...")

		// 等待DNS记录生效（30秒）
		time.Sleep(10 * time.Second)

		// 开始验证流程
		appendOrderLog(order, "开始DNS验证...")

		// 触发验证
		if err := provider.AuthOrder(ctx, domains, orderInfo); err != nil {
			appendOrderLog(order, "触发验证失败: "+err.Error())
			order.Status = -3
			order.Error = err.Error()
			database.DB.Save(order)
			return
		}

		appendOrderLog(order, "验证请求已发送，等待验证完成...")

		// 轮询验证状态（最多5分钟）
		maxAttempts := 10
		for i := 0; i < maxAttempts; i++ {
			time.Sleep(10 * time.Second)

			valid, err := provider.GetAuthStatus(ctx, domains, orderInfo)
			if err != nil {
				appendOrderLog(order, fmt.Sprintf("检查验证状态失败 (%d/%d): %s", i+1, maxAttempts, err.Error()))
				continue
			}

			if valid {
				appendOrderLog(order, "DNS验证成功!")
				break
			}

			if i == maxAttempts-1 {
				appendOrderLog(order, "验证超时，请检查DNS记录是否正确")
				order.Status = -4
				order.Error = "验证超时"
				database.DB.Save(order)
				return
			}

			appendOrderLog(order, fmt.Sprintf("等待验证完成 (%d/%d)...", i+1, maxAttempts))
		}

		// 签发证书
		appendOrderLog(order, "正在签发证书...")
		certResult, err := provider.FinalizeOrder(ctx, domains, orderInfo, order.KeyType, order.KeySize)
		if err != nil {
			appendOrderLog(order, "签发证书失败: "+err.Error())
			order.Status = -5
			order.Error = err.Error()
			database.DB.Save(order)
			return
		}

		// 保存证书
		order.FullChain = certResult.FullChain
		order.PrivateKey = certResult.PrivateKey
		order.Issuer = certResult.Issuer
		issueTime := time.Unix(certResult.ValidFrom, 0)
		expireTime := time.Unix(certResult.ValidTo, 0)
		order.IssueTime = &issueTime
		order.ExpireTime = &expireTime
		order.Status = 3 // 已签发 (3=issued)
		database.DB.Save(order)

		appendOrderLog(order, "证书签发成功!")
		appendOrderLog(order, fmt.Sprintf("颁发机构: %s", certResult.Issuer))
		appendOrderLog(order, fmt.Sprintf("有效期至: %s", expireTime.Format("2006-01-02")))
	} else {
		appendOrderLog(order, "无法自动添加DNS记录，请手动添加上述TXT记录后点击验证按钮")
		order.Status = 1 // 待验证
		database.DB.Save(order)
	}
}

// addDNSRecord 自动添加DNS TXT记录用于证书验证
func addDNSRecord(ctx context.Context, order *models.CertOrder, mainDomain, recordName, recordValue string) bool {
	appendOrderLog(order, fmt.Sprintf("尝试自动添加DNS记录: %s.%s", recordName, mainDomain))
	var domain models.Domain
	var finalRecordName = recordName
	if err := database.DB.Where("name = ?", mainDomain).First(&domain).Error; err != nil {
		found := false
		recordParts := strings.Split(recordName, ".")
		if len(recordParts) > 1 {
			// 提取 _acme-challenge 后面的部分作为子域名
			subdomainParts := recordParts[1:] // ["7725"]
			for i := 0; i < len(subdomainParts); i++ {
				testDomain := strings.Join(subdomainParts[i:], ".") + "." + mainDomain
				appendOrderLog(order, "尝试匹配域名: "+testDomain)
				if err := database.DB.Where("name = ?", testDomain).First(&domain).Error; err == nil {
					found = true
					// 调整记录名：只保留 _acme-challenge 和子域名中不属于托管域名的部分
					finalRecordName = strings.Join(recordParts[:i+1], ".")
					appendOrderLog(order, fmt.Sprintf("匹配成功，记录名调整为: %s", finalRecordName))
					break
				}
			}
		}
		if !found {
			var domains []models.Domain
			database.DB.Find(&domains)
			fullRecord := recordName + "." + mainDomain
			for _, d := range domains {
				if strings.HasSuffix(fullRecord, "."+d.Name) || strings.HasSuffix(fullRecord, d.Name) {
					domain = d
					found = true
					// 计算最终记录名
					suffix := "." + d.Name
					if strings.HasSuffix(fullRecord, suffix) {
						finalRecordName = strings.TrimSuffix(fullRecord, suffix)
					}
					appendOrderLog(order, fmt.Sprintf("模糊匹配成功: %s, 记录名: %s", d.Name, finalRecordName))
					break
				}
			}
		}

		if !found {
			appendOrderLog(order, "未找到托管域名: "+mainDomain)
			return false
		}
	}

	appendOrderLog(order, fmt.Sprintf("找到托管域名: %s (ID: %d)", domain.Name, domain.ID))

	// 获取域名关联的DNS账户
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		appendOrderLog(order, "获取DNS账户失败: "+err.Error())
		return false
	}

	// 解析账户配置
	var config map[string]string
	json.Unmarshal([]byte(account.Config), &config)

	// 获取DNS提供商
	provider, err := dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		appendOrderLog(order, "获取DNS提供商失败: "+err.Error())
		return false
	}

	// 添加TXT记录
	recordID, err := provider.AddDomainRecord(ctx, finalRecordName, "TXT", recordValue, "default", 600, 0, nil, "ACME验证记录")
	if err != nil {
		appendOrderLog(order, "添加DNS记录失败: "+err.Error())
		return false
	}

	appendOrderLog(order, fmt.Sprintf("DNS记录添加成功 (ID: %s)", recordID))
	return true
}

func DeleteCertOrder(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Where("oid = ?", id).Delete(&models.CertDomain{})
	database.DB.Delete(&models.CertOrder{}, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

func GetCertOrderLog(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var order models.CertOrder
	if err := database.DB.First(&order, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "订单不存在"})
		return
	}

	// 返回订单的日志信息，包括错误信息和DNS验证信息
	logInfo := ""
	if order.Error != "" {
		logInfo += "错误信息: " + order.Error + "\n"
	}
	if order.DNS != "" {
		logInfo += "DNS验证信息: " + order.DNS + "\n"
	}
	if order.Info != "" {
		logInfo += "详细信息: " + order.Info
	}
	if logInfo == "" {
		logInfo = "暂无日志信息"
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": logInfo})
}

func GetCertOrderDetail(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var order models.CertOrder
	if err := database.DB.First(&order, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "订单不存在"})
		return
	}

	// 获取域名列表
	var domains []models.CertDomain
	database.DB.Where("oid = ?", order.ID).Order("sort ASC").Find(&domains)
	domainList := make([]string, len(domains))
	for i, d := range domains {
		domainList[i] = d.Domain
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"id":          order.ID,
			"domains":     domainList,
			"key_type":    order.KeyType,
			"key_size":    order.KeySize,
			"status":      order.Status,
			"issuer":      order.Issuer,
			"issue_time":  order.IssueTime,
			"expire_time": order.ExpireTime,
			"is_auto":     order.IsAuto,
			"fullchain":   order.FullChain,
			"private_key": order.PrivateKey,
			"dns_info":    order.DNS,
		},
	})
}

func DownloadCertOrder(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	fileType := c.Query("type")

	var order models.CertOrder
	if err := database.DB.First(&order, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "订单不存在"})
		return
	}

	if order.FullChain == "" || order.PrivateKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "证书尚未签发"})
		return
	}

	// 获取主域名作为文件名
	var domain models.CertDomain
	database.DB.Where("oid = ?", order.ID).Order("sort ASC").First(&domain)
	filename := domain.Domain
	if filename == "" {
		filename = fmt.Sprintf("cert_%d", order.ID)
	}
	// 清理文件名中的特殊字符
	filename = strings.ReplaceAll(filename, "*", "_wildcard")
	filename = strings.ReplaceAll(filename, ".", "_")

	switch fileType {
	case "fullchain":
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_fullchain.pem", filename))
		c.Header("Content-Type", "application/x-pem-file")
		c.String(http.StatusOK, order.FullChain)
	case "key":
		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s_private.key", filename))
		c.Header("Content-Type", "application/x-pem-file")
		c.String(http.StatusOK, order.PrivateKey)
	default:
		buf := &bytes.Buffer{}
		zipWriter := zip.NewWriter(buf)

		fullchainFile, err := zipWriter.Create(fmt.Sprintf("%s_fullchain.pem", filename))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建压缩文件失败"})
			return
		}
		if _, err := fullchainFile.Write([]byte(order.FullChain)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "写入证书失败"})
			return
		}

		keyFile, err := zipWriter.Create(fmt.Sprintf("%s_private.key", filename))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建压缩文件失败"})
			return
		}
		if _, err := keyFile.Write([]byte(order.PrivateKey)); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "写入私钥失败"})
			return
		}

		if err := zipWriter.Close(); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "生成压缩包失败"})
			return
		}

		c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=%s.zip", filename))
		c.Header("Content-Type", "application/zip")
		c.Data(http.StatusOK, "application/zip", buf.Bytes())
	}
}

func ToggleCertOrderAuto(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var order models.CertOrder
	if err := database.DB.First(&order, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "订单不存在"})
		return
	}

	order.IsAuto = !order.IsAuto
	database.DB.Save(&order)

	status := "已关闭"
	if order.IsAuto {
		status = "已开启"
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "自动续期" + status, "data": order.IsAuto})
}

func GetCertDeploys(c *gin.Context) {
	type DeployResult struct {
		models.CertDeploy
		AccountName string `json:"account_name"`
	}
	var dbDeploys []DeployResult
	database.DB.Table("cert_deploys").
		Select("cert_deploys.*, cert_accounts.name as account_name").
		Joins("LEFT JOIN cert_accounts ON cert_deploys.aid = cert_accounts.id").
		Find(&dbDeploys)

	type DeployResponse struct {
		DeployResult
		OrderDomains []string `json:"order_domains"`
	}
	result := make([]DeployResponse, len(dbDeploys))
	for i := range dbDeploys {
		result[i].DeployResult = dbDeploys[i]
		result[i].OrderDomains = []string{}
		var domains []models.CertDomain
		database.DB.Where("oid = ?", dbDeploys[i].OrderID).Order("sort ASC").Find(&domains)
		for _, d := range domains {
			result[i].OrderDomains = append(result[i].OrderDomains, d.Domain)
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

func CreateCertDeploy(c *gin.Context) {
	var req struct {
		AccountID uint                   `json:"account_id" binding:"required"`
		OrderID   uint                   `json:"order_id" binding:"required"`
		Config    map[string]interface{} `json:"config"`
		Remark    string                 `json:"remark"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	deploy := models.CertDeploy{
		AccountID: req.AccountID,
		OrderID:   req.OrderID,
		Config:    string(configJSON),
		Remark:    req.Remark,
		Active:    true,
	}

	if err := database.DB.Create(&deploy).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": gin.H{"id": deploy.ID}})
}

func UpdateCertDeploy(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var deploy models.CertDeploy
	if err := database.DB.First(&deploy, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "部署任务不存在"})
		return
	}

	var req struct {
		Config map[string]interface{} `json:"config"`
		Remark string                 `json:"remark"`
		Active bool                   `json:"active"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	updates := map[string]interface{}{
		"config": string(configJSON),
		"remark": req.Remark,
		"active": req.Active,
	}

	database.DB.Model(&deploy).Updates(updates)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

func DeleteCertDeploy(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.DB.Delete(&models.CertDeploy{}, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

func ProcessCertDeploy(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var deployTask models.CertDeploy
	if err := database.DB.First(&deployTask, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "部署任务不存在"})
		return
	}

	var order models.CertOrder
	if err := database.DB.First(&order, deployTask.OrderID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "证书订单不存在"})
		return
	}

	if order.FullChain == "" || order.PrivateKey == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "证书尚未签发"})
		return
	}

	var account models.CertAccount
	if err := database.DB.First(&account, deployTask.AccountID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "部署账户不存在"})
		return
	}

	// 解析账户配置
	var accConfig map[string]interface{}
	if account.Config != "" {
		json.Unmarshal([]byte(account.Config), &accConfig)
	}

	// 获取部署提供商
	provider, err := deploy.GetProvider(account.Type, accConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "获取部署提供商失败: " + err.Error()})
		return
	}

	// 解析部署配置
	var deployConfig map[string]interface{}
	if deployTask.Config != "" {
		json.Unmarshal([]byte(deployTask.Config), &deployConfig)
	}
	if deployConfig == nil {
		deployConfig = map[string]interface{}{}
	}
	var domains []models.CertDomain
	database.DB.Where("oid = ?", order.ID).Order("sort ASC").Find(&domains)
	var domainList []string
	for _, d := range domains {
		domainList = append(domainList, d.Domain)
	}
	if _, ok := deployConfig["domainList"]; !ok {
		deployConfig["domainList"] = domainList
	}
	if _, ok := deployConfig["domains"]; !ok {
		deployConfig["domains"] = strings.Join(domainList, ",")
	}

	// 执行部署
	if err := provider.Deploy(context.Background(), order.FullChain, order.PrivateKey, deployConfig); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "部署失败: " + err.Error()})
		return
	}

	// 更新最后运行时间
	database.DB.Model(&deployTask).Update("updated_at", time.Now())

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "部署成功"})
}

func GetCertProviders(c *gin.Context) {
	certProviders := cert.GetCertProviderConfigs()
	deployProviders := cert.GetDeployProviderConfigs()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"cert":   certProviders,
			"deploy": deployProviders,
		},
	})
}
