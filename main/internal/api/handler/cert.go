package handler

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"main/internal/api/middleware"
	"main/internal/cert"
	"main/internal/cert/deploy"
	"main/internal/database"
	"main/internal/dbcache"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/net/idna"
	"gorm.io/gorm"
)

// certDomainsByOrderIDs 按订单 ID 批量加载证书域名，避免列表接口 N+1。
func certDomainsByOrderIDs(tx *gorm.DB, orderIDs []uint) map[uint][]string {
	out := make(map[uint][]string)
	if len(orderIDs) == 0 {
		return out
	}
	seen := make(map[uint]struct{}, len(orderIDs))
	uniq := make([]uint, 0, len(orderIDs))
	for _, id := range orderIDs {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out
	}
	var rows []models.CertDomain
	tx.Where("oid IN ?", uniq).Order("oid ASC, sort ASC").Find(&rows)
	for _, r := range rows {
		out[r.OrderID] = append(out[r.OrderID], r.Domain)
	}
	return out
}

type certAccountListRow struct {
	ID        uint      `json:"id"`
	Type      string    `json:"type"`
	TypeName  string    `json:"type_name"`
	Name      string    `json:"name"`
	Config    string    `json:"config"`
	Remark    string    `json:"remark"`
	IsDeploy  bool      `json:"is_deploy"`
	CreatedAt time.Time `json:"created_at"`
}

func GetCertAccounts(c *gin.Context) {
	isDeploy := c.Query("deploy") == "1"
	// 安全审计 R-1：列表必须按 UID 过滤；非管理员只看自己的，避免账户名/类型枚举泄露。
	// 注意 cacheKey 也按 UID 维度区分，防止管理员命中后污染普通用户视图。
	uid := currentUIDUint(c)
	cacheKey := dbcache.KeyCertAccountsList(isDeploy, isAdmin(c), uid)

	var rows []certAccountListRow
	if err := dbcache.GetOrSetJSON(c.Request.Context(), cacheKey, dbcache.DefaultTTL, func() (interface{}, error) {
		var accounts []models.CertAccount
		q := database.DB
		if isDeploy {
			q = q.Where("is_deploy = ?", true)
		} else {
			q = q.Where("is_deploy = ?", false)
		}
		q = scopeCertAccountQuery(c, q)
		if err := q.Find(&accounts).Error; err != nil {
			return nil, err
		}
		list := make([]certAccountListRow, 0, len(accounts))
		for _, acc := range accounts {
			cfg, _ := cert.GetProviderConfig(acc.Type)
			list = append(list, certAccountListRow{
				ID: acc.ID, Type: acc.Type, TypeName: cfg.Name,
				Name: acc.Name, Config: acc.Config, Remark: acc.Remark,
				IsDeploy: acc.IsDeploy, CreatedAt: acc.CreatedAt,
			})
		}
		return list, nil
	}, &rows); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "加载失败"})
		return
	}

	result := make([]gin.H, 0, len(rows))
	for _, acc := range rows {
		result = append(result, gin.H{
			"id":         acc.ID,
			"type":       acc.Type,
			"type_name":  acc.TypeName,
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
		// 安全审计 R-1：始终绑定到当前用户，防止注入第三方 UID
		UserID:   currentUIDUint(c),
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
	dbcache.BustCertAccountsList()

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": gin.H{"id": account.ID}})
}

func UpdateCertAccount(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	// 安全审计 R-1：先做 ownership 校验
	account, ok := requireCertAccountOwner(c, uint(id))
	if !ok {
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

	database.DB.Model(account).Updates(updates)
	dbcache.BustCertAccountsList()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

func DeleteCertAccount(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	// 安全审计 R-1
	if _, ok := requireCertAccountOwner(c, uint(id)); !ok {
		return
	}
	database.DB.Delete(&models.CertAccount{}, id)
	dbcache.BustCertAccountsList()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

func GetCertOrders(c *gin.Context) {
	type OrderResult struct {
		models.CertOrder
		AccountName string `json:"account_name"`
	}
	var dbOrders []OrderResult

	// 安全审计 R-1：按 UID 过滤，非管理员只看自己关联账户的订单
	q := database.DB.Table("cert_orders").
		Select("cert_orders.*, cert_accounts.name as account_name").
		Joins("LEFT JOIN cert_accounts ON cert_orders.aid = cert_accounts.id")
	if !isAdmin(c) {
		q = q.Where("cert_accounts.uid = ?", currentUIDUint(c))
	}
	q.Find(&dbOrders)

	type OrderResponse struct {
		OrderResult
		Domains []string `json:"domains"`
	}
	orderIDs := make([]uint, len(dbOrders))
	for i := range dbOrders {
		orderIDs[i] = dbOrders[i].ID
	}
	domainsByOID := certDomainsByOrderIDs(database.DB, orderIDs)

	orders := make([]OrderResponse, len(dbOrders))
	for i := range dbOrders {
		orders[i].OrderResult = dbOrders[i]
		dlist := domainsByOID[dbOrders[i].ID]
		if dlist == nil {
			dlist = []string{}
		}
		orders[i].Domains = dlist
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": orders})
}

type CreateCertOrderRequest struct {
	AccountID     uint     `json:"account_id" binding:"required"`
	Domains       []string `json:"domains" binding:"required"`
	KeyType       string   `json:"key_type"`
	KeySize       string   `json:"key_size"`
	IsAuto        bool     `json:"is_auto"`
	ChallengeType string   `json:"challenge_type"` // 空|dns-01|http-01（ACME 域名验证；通配符仅 dns-01）
}

func validateCertOrderDomains(domains []string) error {
	if len(domains) == 0 {
		return fmt.Errorf("至少需要一个域名或 IP")
	}
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			return fmt.Errorf("存在空的域名或 IP")
		}
		if net.ParseIP(d) != nil {
			continue
		}
		if len(d) > 253 {
			return fmt.Errorf("域名过长: %s", d)
		}
		if strings.ContainsAny(d, " \t\r\n") {
			return fmt.Errorf("非法标识: %s", d)
		}
	}
	return nil
}

func detectCertOrderKind(domains []string) string {
	var ipN, dnsN int
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		if net.ParseIP(d) != nil {
			ipN++
		} else {
			dnsN++
		}
	}
	if ipN > 0 && dnsN > 0 {
		return "mixed"
	}
	if ipN > 0 {
		return "ip"
	}
	return "dns"
}

func normalizeCertChallengeType(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "http-01", "http01", "http":
		return "http-01"
	case "dns-01", "dns01", "dns":
		return "dns-01"
	default:
		return ""
	}
}

func validateChallengeChoice(domains []string, orderKind, challengeType string) error {
	ct := normalizeCertChallengeType(challengeType)
	if ct == "" {
		return nil
	}
	if orderKind == "ip" && ct == "dns-01" {
		return fmt.Errorf("纯 IP 订单仅支持 HTTP-01 验证")
	}
	if ct != "http-01" {
		return nil
	}
	for _, d := range domains {
		d = strings.TrimSpace(d)
		if strings.HasPrefix(d, "*.") {
			return fmt.Errorf("通配符域名不能使用 HTTP-01，请改用 DNS-01")
		}
	}
	return nil
}

func certCountChallengeRecords(dnsRecords map[string][]cert.DNSRecord) (dnsLike, http01 int) {
	for _, records := range dnsRecords {
		for _, r := range records {
			if strings.EqualFold(r.Type, "HTTP-01") {
				http01++
			} else {
				dnsLike++
			}
		}
	}
	return
}

func CreateCertOrder(c *gin.Context) {
	var req CreateCertOrderRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// 安全审计 R-1：AccountID 必须属于当前用户，防止借他人账户签发
	if _, ok := requireCertAccountOwner(c, req.AccountID); !ok {
		return
	}

	normDomains := make([]string, len(req.Domains))
	for i, d := range req.Domains {
		normDomains[i] = strings.TrimSpace(d)
	}
	if err := validateCertOrderDomains(normDomains); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": err.Error()})
		return
	}

	if req.KeyType == "" {
		req.KeyType = "RSA"
	}
	if req.KeySize == "" {
		req.KeySize = "2048"
	}

	orderKind := detectCertOrderKind(normDomains)
	normChallenge := normalizeCertChallengeType(req.ChallengeType)
	if orderKind == "ip" {
		normChallenge = ""
	}
	if err := validateChallengeChoice(normDomains, orderKind, normChallenge); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": err.Error()})
		return
	}

	order := models.CertOrder{
		AccountID:     req.AccountID,
		KeyType:       req.KeyType,
		KeySize:       req.KeySize,
		IsAuto:        req.IsAuto,
		OrderKind:     orderKind,
		ChallengeType: normChallenge,
		Status:        0,
	}

	if err := database.DB.Create(&order).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建失败"})
		return
	}

	for i, domain := range normDomains {
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

				go processCertOrderAsync(&order, provider, normDomains, &account)
			}
		}
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": gin.H{"id": order.ID}})
}

func ProcessCertOrder(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	// 安全审计 R-1 + R-6：先做 ownership 校验，再做状态机原子化（CAS：仅当 status != 1 时占位）。
	// 如果 status 已是 1（处理中），CAS 受影响行数为 0，直接拒绝重复提交，
	// 避免攻击者短时间内对同一 orderID N 次 process 触发 N 个 ACME goroutine 烧 LE 配额。
	orderPtr, ok := requireCertOrderOwner(c, uint(id))
	if !ok {
		return
	}
	order := *orderPtr

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

	// 安全审计 R-6：CAS 状态占位，避免并发重复签发烧 Let's Encrypt 配额。
	// 仅当当前 status != 1 时才能切换到处理中；若已被并发请求抢占则直接拒绝。
	res := database.DB.Model(&models.CertOrder{}).
		Where("id = ? AND status != ?", order.ID, 1).
		Update("status", 1)
	if res.RowsAffected == 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "订单正在处理中，请勿重复提交"})
		return
	}
	order.Status = 1

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
	// 安全审计 R-6：自动续期路径同样走 CAS，防止与手动 ProcessCertOrder 重叠
	res := database.DB.Model(&models.CertOrder{}).
		Where("id = ? AND status != ?", order.ID, 1).
		Update("status", 1)
	if res.RowsAffected == 0 {
		logger.Warn("[Cert] 自动续期被并发抢占，跳过 (orderID=%d)", order.ID)
		return
	}
	order.Status = 1
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
	if t := strings.TrimSpace(order.ChallengeType); t != "" {
		orderInfo.PreferredChallenge = strings.ToLower(t)
	}

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

	useCNAME := false
	if pc, ok := cert.GetProviderConfig(account.Type); ok && pc.CNAME {
		useCNAME = true
	}

	dnsRecCount, http01Count := certCountChallengeRecords(dnsRecords)
	if http01Count > 0 && dnsRecCount > 0 {
		appendOrderLog(order, "本订单同时含 DNS 与 HTTP-01：请确保 TXT 与 IP:80 校验路径均已就绪后再继续验证")
	}

	// 保存DNS验证记录并尝试自动添加（HTTP-01 仅写入说明，不调用 DNS API）
	dnsInfo := ""
	addedRecords := 0
	for domainName, records := range dnsRecords {
		recs := records
		if d, ok := certFindDomainByNameIncludingPunycode(domainName); ok {
			var acc models.Account
			if database.DB.First(&acc, d.AccountID).Error == nil && acc.Type == "huawei" {
				recs = mergeHuaweiTXTRecordsForCert(recs)
				appendOrderLog(order, "华为云 DNS：已按 dnsmgr 规则合并同主机 TXT 验证记录")
			}
		}

		headerPrinted := false
		for _, r := range recs {
			if strings.EqualFold(r.Type, "HTTP-01") {
				path := "/.well-known/acme-challenge/" + r.Name
				dnsInfo += "标识 " + domainName + " [HTTP-01]:\n"
				dnsInfo += "  URL 路径: " + path + "\n"
				dnsInfo += "  响应正文（纯文本，仅以下内容）: " + r.Value + "\n\n"
				appendOrderLog(order, fmt.Sprintf("HTTP-01 - 在 %s 上响应 GET %s ，正文为 key authorization", domainName, path))
				continue
			}
			if !headerPrinted {
				dnsInfo += "域名 " + domainName + " 需要添加以下DNS记录:\n"
				headerPrinted = true
			}
			dnsInfo += "  记录名: " + r.Name + "\n"
			dnsInfo += "  记录类型: " + r.Type + "\n"
			dnsInfo += "  记录值: " + r.Value + "\n"
			appendOrderLog(order, "DNS验证记录 - "+r.Name+"."+domainName+" -> "+r.Value)

			recType := r.Type
			if recType == "" {
				recType = "TXT"
			}
			if addDNSRecord(ctx, order, domainName, r.Name, r.Value, recType, useCNAME) {
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

	runAutoAuth := addedRecords > 0 || (http01Count > 0 && dnsRecCount == 0 && order.IsAuto)

	if runAutoAuth {
		if addedRecords > 0 {
			appendOrderLog(order, fmt.Sprintf("已自动添加 %d 条DNS验证记录", addedRecords))
			appendOrderLog(order, "等待DNS记录生效...")
			time.Sleep(10 * time.Second)
		} else {
			appendOrderLog(order, "HTTP-01：将向 CA 发起验证，请确认已对公网开放 80 端口并能返回上述校验内容")
			time.Sleep(5 * time.Second)
		}

		appendOrderLog(order, "开始触发 CA 验证...")

		if err := provider.AuthOrder(ctx, domains, orderInfo); err != nil {
			appendOrderLog(order, "触发验证失败: "+err.Error())
			order.Status = -3
			order.Error = err.Error()
			database.DB.Save(order)
			return
		}

		appendOrderLog(order, "验证请求已发送，等待验证完成...")

		maxAttempts := 10
		for i := 0; i < maxAttempts; i++ {
			time.Sleep(10 * time.Second)

			valid, err := provider.GetAuthStatus(ctx, domains, orderInfo)
			if err != nil {
				appendOrderLog(order, fmt.Sprintf("检查验证状态失败 (%d/%d): %s", i+1, maxAttempts, err.Error()))
				continue
			}

			if valid {
				appendOrderLog(order, "授权验证成功")
				break
			}

			if i == maxAttempts-1 {
				appendOrderLog(order, "验证超时，请检查 DNS TXT 或 HTTP-01 是否已正确配置")
				order.Status = -4
				order.Error = "验证超时"
				database.DB.Save(order)
				return
			}

			appendOrderLog(order, fmt.Sprintf("等待验证完成 (%d/%d)...", i+1, maxAttempts))
		}

		appendOrderLog(order, "正在签发证书...")
		certResult, err := provider.FinalizeOrder(ctx, domains, orderInfo, order.KeyType, order.KeySize)
		if err != nil {
			appendOrderLog(order, "签发证书失败: "+err.Error())
			order.Status = -5
			order.Error = err.Error()
			database.DB.Save(order)
			return
		}

		order.FullChain = certResult.FullChain
		order.PrivateKey = certResult.PrivateKey
		order.Issuer = certResult.Issuer
		issueTime := time.Unix(certResult.ValidFrom, 0)
		expireTime := time.Unix(certResult.ValidTo, 0)
		order.IssueTime = &issueTime
		order.ExpireTime = &expireTime
		order.Status = 3
		database.DB.Save(order)

		appendOrderLog(order, "证书签发成功!")
		appendOrderLog(order, fmt.Sprintf("颁发机构: %s", certResult.Issuer))
		appendOrderLog(order, fmt.Sprintf("有效期至: %s", expireTime.Format("2006-01-02")))
	} else {
		appendOrderLog(order, "请手动完成验证（DNS TXT 或 HTTP-01，见上方说明）后再次点击处理")
		order.Status = 1
		database.DB.Save(order)
	}
}

// mergeHuaweiTXTRecordsForCert 对齐 dnsmgr CertDnsUtils::getHuaweiDnsRecords（同 RR 多条 TXT 合并为一条）
func mergeHuaweiTXTRecordsForCert(records []cert.DNSRecord) []cert.DNSRecord {
	var nonTXT []cert.DNSRecord
	byName := make(map[string][]string)
	var order []string
	for _, r := range records {
		if !strings.EqualFold(r.Type, "TXT") {
			nonTXT = append(nonTXT, r)
			continue
		}
		name := r.Name
		if _, ok := byName[name]; !ok {
			order = append(order, name)
		}
		byName[name] = append(byName[name], r.Value)
	}
	sort.Strings(order)
	out := append([]cert.DNSRecord{}, nonTXT...)
	for _, name := range order {
		vals := byName[name]
		merged := `"` + strings.Join(vals, `","`) + `"`
		out = append(out, cert.DNSRecord{Name: name, Type: "TXT", Value: merged})
	}
	return out
}

// certChallengeDomainForCNAME 与 dnsmgr CertDnsUtils 中 CNAME 代理匹配的域名一致
func certChallengeDomainForCNAME(mainDomain, recordName string) string {
	if recordName == "_acme-challenge" {
		return mainDomain
	}
	if strings.HasPrefix(recordName, "_acme-challenge.") {
		sub := strings.TrimPrefix(recordName, "_acme-challenge.")
		if sub == "" {
			return mainDomain
		}
		return sub + "." + mainDomain
	}
	return mainDomain
}

func certFindDomainByNameIncludingPunycode(name string) (models.Domain, bool) {
	var domain models.Domain
	if err := database.DB.Where("name = ?", name).First(&domain).Error; err == nil {
		return domain, true
	}
	if strings.HasPrefix(strings.ToLower(name), "xn--") {
		if u, err := idna.ToUnicode(name); err == nil && u != "" && u != name {
			if err := database.DB.Where("name = ?", u).First(&domain).Error; err == nil {
				return domain, true
			}
		}
	}
	return domain, false
}

// certResolveCNAMEProxy 将验证记录映射到 CNAME 代理托管区（cert_cnames + domains）
func certResolveCNAMEProxy(mainDomain, recordName string) (newMain, newRR string, ok bool) {
	ch := certChallengeDomainForCNAME(mainDomain, recordName)
	var rr, parent string
	row := database.DB.Table("cert_cnames AS c").
		Select("c.rr, d.name").
		Joins("INNER JOIN domains d ON d.id = c.did AND d.deleted_at IS NULL").
		Where("c.domain = ?", ch).
		Limit(1).
		Row()
	if err := row.Scan(&rr, &parent); err != nil || rr == "" || parent == "" {
		return mainDomain, recordName, false
	}
	return parent, rr, true
}

// addDNSRecord 自动添加 DNS 验证记录（对齐 dnsmgr CertDnsUtils::addDns：默认线路、TTL、IDN、CNAME 代理）
func addDNSRecord(ctx context.Context, order *models.CertOrder, mainDomain, recordName, recordValue, recordType string, useCNAME bool) bool {
	mainDom := mainDomain
	finalRecordName := recordName
	if useCNAME {
		if nm, rr, ok := certResolveCNAMEProxy(mainDomain, recordName); ok {
			appendOrderLog(order, fmt.Sprintf("CNAME代理: 验证域 %s → 在托管区 %s 添加记录名 %s", certChallengeDomainForCNAME(mainDomain, recordName), nm, rr))
			mainDom = nm
			finalRecordName = rr
		}
	}

	appendOrderLog(order, fmt.Sprintf("尝试自动添加DNS记录: %s.%s (%s)", finalRecordName, mainDom, recordType))

	var domain models.Domain
	var found bool
	domain, found = certFindDomainByNameIncludingPunycode(mainDom)
	if !found {
		recordParts := strings.Split(finalRecordName, ".")
		if len(recordParts) > 1 {
			subdomainParts := recordParts[1:]
			for i := 0; i < len(subdomainParts); i++ {
				testDomain := strings.Join(subdomainParts[i:], ".") + "." + mainDom
				appendOrderLog(order, "尝试匹配域名: "+testDomain)
				var d models.Domain
				var ok bool
				d, ok = certFindDomainByNameIncludingPunycode(testDomain)
				if ok {
					domain = d
					found = true
					finalRecordName = strings.Join(recordParts[:i+1], ".")
					appendOrderLog(order, fmt.Sprintf("匹配成功，记录名调整为: %s", finalRecordName))
					break
				}
			}
		}
	}
	if !found {
		var domains []models.Domain
		database.DB.Find(&domains)
		fullRecord := finalRecordName + "." + mainDom
		for _, d := range domains {
			if strings.HasSuffix(fullRecord, "."+d.Name) || strings.HasSuffix(fullRecord, d.Name) {
				domain = d
				found = true
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
		appendOrderLog(order, "未找到托管域名: "+mainDom)
		return false
	}

	appendOrderLog(order, fmt.Sprintf("找到托管域名: %s (ID: %d)", domain.Name, domain.ID))

	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		appendOrderLog(order, "获取DNS账户失败: "+err.Error())
		return false
	}

	var config map[string]string
	json.Unmarshal([]byte(account.Config), &config)

	provider, err := dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		appendOrderLog(order, "获取DNS提供商失败: "+err.Error())
		return false
	}

	line := dns.DefaultDNSLine(account.Type)
	ttl := 600
	if account.Type == "namesilo" {
		ttl = 3600
	}

	recordID, skipped, err := dns.EnsureChallengeRecord(ctx, provider, finalRecordName, recordType, recordValue, line, ttl, "ACME验证记录")
	if err != nil {
		appendOrderLog(order, "添加DNS记录失败: "+err.Error())
		return false
	}
	if skipped {
		appendOrderLog(order, fmt.Sprintf("DNS验证记录已存在（值相同），跳过写入: %s.%s", finalRecordName, domain.Name))
	} else {
		appendOrderLog(order, fmt.Sprintf("DNS记录添加成功 (ID: %s)", recordID))
	}
	return true
}

func DeleteCertOrder(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	// 安全审计 R-1
	if _, ok := requireCertOrderOwner(c, uint(id)); !ok {
		return
	}
	database.DB.Where("oid = ?", id).Delete(&models.CertDomain{})
	database.DB.Delete(&models.CertOrder{}, id)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

func GetCertOrderLog(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	// 安全审计 R-1
	orderPtr, ok := requireCertOrderOwner(c, uint(id))
	if !ok {
		return
	}
	order := *orderPtr

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

	// 安全审计 R-1
	orderPtr, ok := requireCertOrderOwner(c, uint(id))
	if !ok {
		return
	}
	order := *orderPtr

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
			"id":             order.ID,
			"domains":        domainList,
			"order_kind":     order.OrderKind,
			"challenge_type": order.ChallengeType,
			"key_type":       order.KeyType,
			"key_size":       order.KeySize,
			"status":         order.Status,
			"issuer":         order.Issuer,
			"issue_time":     order.IssueTime,
			"expire_time":    order.ExpireTime,
			"is_auto":        order.IsAuto,
			"fullchain":      order.FullChain,
			"private_key":    order.PrivateKey,
			"dns_info":       order.DNS,
		},
	})
}

func DownloadCertOrder(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	fileType := c.Query("type")

	// 安全审计 R-1（最关键修复）：原实现允许任何登录用户下载他人证书私钥
	orderPtr, ok := requireCertOrderOwner(c, uint(id))
	if !ok {
		return
	}
	order := *orderPtr

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
	filename = strings.ReplaceAll(filename, ":", "_")
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

	// 安全审计 R-1
	orderPtr, ok := requireCertOrderOwner(c, uint(id))
	if !ok {
		return
	}
	order := *orderPtr

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
	oidList := make([]uint, len(dbDeploys))
	for i := range dbDeploys {
		oidList[i] = dbDeploys[i].OrderID
	}
	domainsByOID := certDomainsByOrderIDs(database.DB, oidList)

	result := make([]DeployResponse, len(dbDeploys))
	for i := range dbDeploys {
		result[i].DeployResult = dbDeploys[i]
		dlist := domainsByOID[dbDeploys[i].OrderID]
		if dlist == nil {
			dlist = []string{}
		}
		result[i].OrderDomains = dlist
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

	// 安全审计 H-3：local 类型部署会以 DNSPlane 进程权限执行用户填写的 restart_cmd，
	// 等同于服务端 RCE。限制仅管理员可创建此类型；其他类型（SSH/CDN/...）保持原开放。
	if !checkLocalDeployAllowed(c, req.AccountID) {
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	deploy := models.CertDeploy{
		UserID:    currentUIDUint(c),
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

// checkLocalDeployAllowed 对 CertAccount.Type == "local" 的部署强制 checkAdmin；
// 返回 false 时已写入"权限不足"响应，handler 应直接 return。
func checkLocalDeployAllowed(c *gin.Context, accountID uint) bool {
	if isAdmin(c) {
		return true
	}
	var acc models.CertAccount
	if err := database.DB.Select("type").First(&acc, accountID).Error; err != nil {
		// 账户查不到会在后续业务逻辑里再次失败，这里直接放过让原路径报错
		return true
	}
	if acc.Type == "local" {
		middleware.ErrorResponse(c, "本地部署需要管理员权限，请改用 SSH 或其他远程部署方式")
		return false
	}
	return true
}

func UpdateCertDeploy(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var deploy models.CertDeploy
	if err := database.DB.First(&deploy, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "部署任务不存在"})
		return
	}

	// 安全审计 H-3：修改 local 类型部署也需要管理员
	if !checkLocalDeployAllowed(c, deploy.AccountID) {
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

	// 解析部署配置（须先于 GetProvider，以便解析 aliyun/tencent 等 product 子类型）
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

	provider, err := deploy.GetProvider(account.Type, accConfig, deployConfig)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "获取部署提供商失败: " + err.Error()})
		return
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
	deployProviders := deploy.MergeDeployProviderConfigsForAPI(cert.GetDeployProviderConfigs())
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"cert":   certProviders,
			"deploy": deployProviders,
		},
	})
}
