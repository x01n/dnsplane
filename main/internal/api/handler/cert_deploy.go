package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"main/internal/api/middleware"
	"main/internal/cert/deploy"
	"main/internal/database"
	"main/internal/dbcache"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/service"
	"main/internal/utils"

	"github.com/gin-gonic/gin"
)

/*
 * DeployAccountList 获取部署账户列表
 * @route POST /deploy/accounts/list
 * 功能：返回部署账户列表，管理员看全部，普通用户只看自己的
 */
func DeployAccountList(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var accounts []models.CertAccount
	query := database.WithContext(c).Where("is_deploy = ?", true)
	if !isAdmin(c) {
		query = query.Where("uid = ?", currentUIDUint(c))
	}
	query.Find(&accounts)

	result := make([]gin.H, 0, len(accounts))
	for _, acc := range accounts {
		cfg, _ := deploy.GetDeployConfig(acc.Type)
		result = append(result, gin.H{
			"id":         acc.ID,
			"type":       acc.Type,
			"type_name":  cfg.Name,
			"class":      cfg.Class,
			"name":       acc.Name,
			"remark":     acc.Remark,
			"created_at": acc.CreatedAt,
		})
	}

	middleware.SuccessResponse(c, result)
}

/*
 * DeployAccountAdd 创建部署账户
 * @route POST /deploy/accounts/add
 * 功能：创建新的证书部署账户（SSH/CDN/云服务商等）
 */
func DeployAccountAdd(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		Type   string                 `json:"type"`
		Name   string                 `json:"name"`
		Config map[string]interface{} `json:"config"`
		Remark string                 `json:"remark"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	/* 套餐配额检查 */
	if !CheckQuota(c, currentUID(c), "accounts") {
		return
	}

	if req.Type == "" || req.Name == "" {
		middleware.ErrorResponse(c, "类型和名称不能为空")
		return
	}

	// 验证部署类型是否存在
	if _, ok := deploy.GetDeployConfig(req.Type); !ok {
		middleware.ErrorResponse(c, "不支持的部署类型: "+req.Type)
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	account := models.CertAccount{
		UserID:   currentUIDUint(c),
		Type:     req.Type,
		Name:     req.Name,
		Config:   string(configJSON),
		Remark:   req.Remark,
		IsDeploy: true,
	}

	if err := database.WithContext(c).Create(&account).Error; err != nil {
		middleware.ErrorResponse(c, "创建失败")
		return
	}

	dbcache.BustCertAccountsList()
	service.Audit.LogAction(c, "create_deploy_account", "", fmt.Sprintf("创建部署账户: %s (%s)", req.Name, req.Type))
	middleware.SuccessResponse(c, gin.H{"id": account.ID})
}

/*
 * DeployAccountEdit 更新部署账户
 * @route POST /deploy/accounts/edit
 * 功能：更新部署账户名称、配置、备注等信息
 */
func DeployAccountEdit(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID     string                 `json:"id"`
		Name   string                 `json:"name"`
		Config map[string]interface{} `json:"config"`
		Remark string                 `json:"remark"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.CertAccount
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	updates := map[string]interface{}{}
	if req.Name != "" {
		updates["name"] = req.Name
	}
	if req.Config != nil {
		configJSON, _ := json.Marshal(req.Config)
		updates["config"] = string(configJSON)
	}
	updates["remark"] = req.Remark

	database.WithContext(c).Model(&account).Updates(updates)
	dbcache.BustCertAccountsList()
	service.Audit.LogAction(c, "update_deploy_account", "", fmt.Sprintf("更新部署账户: %s", account.Name))
	middleware.SuccessMsg(c, "更新成功")
}

/*
 * DeployAccountDelete 删除部署账户
 * @route POST /deploy/accounts/delete
 * 功能：删除部署账户，有关联任务则拒绝
 */
func DeployAccountDelete(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	// 检查是否有关联的部署任务
	var taskCount int64
	database.WithContext(c).Model(&models.CertDeploy{}).Where("aid = ?", req.ID).Count(&taskCount)
	if taskCount > 0 {
		middleware.ErrorResponse(c, fmt.Sprintf("该账户下还有 %d 个部署任务，请先删除相关任务", taskCount))
		return
	}

	var account models.CertAccount
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	database.WithContext(c).Delete(&models.CertAccount{}, req.ID)
	dbcache.BustCertAccountsList()
	service.Audit.LogAction(c, "delete_deploy_account", "", fmt.Sprintf("删除部署账户: %s", account.Name))
	middleware.SuccessMsg(c, "删除成功")
}

/*
 * DeployAccountDetail 获取部署账户详情（含配置）
 * @route POST /deploy/accounts/detail
 * 功能：返回账户完整配置信息，用于编辑页面回显
 */
func DeployAccountDetail(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.CertAccount
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	var config map[string]interface{}
	if account.Config != "" {
		if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
			middleware.ErrorResponse(c, "账户配置解析失败")
			return
		}
	}

	cfg, _ := deploy.GetDeployConfig(account.Type)
	middleware.SuccessResponse(c, gin.H{
		"id":        account.ID,
		"type":      account.Type,
		"type_name": cfg.Name,
		"name":      account.Name,
		"config":    config,
		"remark":    account.Remark,
	})
}

/*
 * DeployAccountCheck 检测部署账户连通性
 * @route POST /deploy/accounts/check
 * 功能：异步检测账户连通性，立即返回“提交成功”
 */
func DeployAccountCheck(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.CertAccount
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	var accConfig map[string]interface{}
	if account.Config != "" {
		if err := json.Unmarshal([]byte(account.Config), &accConfig); err != nil {
			middleware.ErrorResponse(c, "账户配置解析失败")
			return
		}
	}

	provider, err := deploy.GetProvider(account.Type, accConfig)
	if err != nil {
		middleware.ErrorResponse(c, "获取部署提供商失败: "+err.Error())
		return
	}

	/* 异步执行连通性检测 */
	accountName := account.Name
	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := provider.Check(ctx); err != nil {
			logger.Warn("[DeployAccountCheck] 连接失败: %s - %v", accountName, err)
			return
		}
		logger.Info("[DeployAccountCheck] 连接成功: %s", accountName)
	})

	middleware.SuccessMsg(c, "提交成功")
}

/*
 * DeployList 获取部署任务列表
 * @route POST /deploy/list
 * 功能：查询部署任务，支持按账户/订单/状态筛选
 */
func DeployList(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		AID      string `json:"aid"`
		OID      string `json:"oid"`
		Status   *int   `json:"status"`
		Domain   string `json:"domain"`
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 50
	}
	if req.PageSize > 200 {
		req.PageSize = 200
	}

	type DeployResult struct {
		models.CertDeploy
		AccountName string `json:"account_name"`
		AccountType string `json:"account_type"`
	}

	query := database.WithContext(c).Table("cert_deploys").
		Select("cert_deploys.*, cert_accounts.name as account_name, cert_accounts.type as account_type").
		Joins("LEFT JOIN cert_accounts ON cert_deploys.aid = cert_accounts.id")

	if !isAdmin(c) {
		query = query.Where("cert_deploys.uid = ?", currentUIDUint(c))
	}

	if req.AID != "" {
		query = query.Where("cert_deploys.aid = ?", req.AID)
	}
	if req.OID != "" {
		query = query.Where("cert_deploys.oid = ?", req.OID)
	}
	if req.Status != nil {
		query = query.Where("cert_deploys.status = ?", *req.Status)
	}

	var total int64
	query.Count(&total)

	var dbDeploys []DeployResult
	query.Order("cert_deploys.id DESC").Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Find(&dbDeploys)

	/* 批量加载所有部署任务关联的域名，避免循环内逐条查询（N+1 优化） */
	orderIDSet := make(map[uint]struct{})
	for _, d := range dbDeploys {
		orderIDSet[d.OrderID] = struct{}{}
	}
	orderIDs := make([]uint, 0, len(orderIDSet))
	for oid := range orderIDSet {
		orderIDs = append(orderIDs, oid)
	}
	domainMap := make(map[uint][]string)
	if len(orderIDs) > 0 {
		var allDomains []models.CertDomain
		database.WithContext(c).Where("oid IN ?", orderIDs).Order("sort ASC").Find(&allDomains)
		for _, d := range allDomains {
			domainMap[d.OrderID] = append(domainMap[d.OrderID], d.Domain)
		}
	}

	result := make([]gin.H, len(dbDeploys))
	for i := range dbDeploys {
		cfg, _ := deploy.GetDeployConfig(dbDeploys[i].AccountType)
		domainList := domainMap[dbDeploys[i].OrderID]
		if domainList == nil {
			domainList = []string{}
		}

		result[i] = gin.H{
			"id":           dbDeploys[i].ID,
			"aid":          dbDeploys[i].AccountID,
			"oid":          dbDeploys[i].OrderID,
			"account_name": dbDeploys[i].AccountName,
			"account_type": dbDeploys[i].AccountType,
			"type_name":    cfg.Name,
			"remark":       dbDeploys[i].Remark,
			"status":       dbDeploys[i].Status,
			"error":        dbDeploys[i].Error,
			"active":       dbDeploys[i].Active,
			"retry":        dbDeploys[i].Retry,
			"is_lock":      dbDeploys[i].IsLock,
			"last_time":    dbDeploys[i].LastTime,
			"process_id":   dbDeploys[i].ProcessID,
			"created_at":   dbDeploys[i].CreatedAt,
			"domains":      domainList,
		}
	}

	middleware.SuccessResponse(c, gin.H{"total": total, "list": result})
}

/*
 * DeployDetail 获取部署任务详情
 * @route POST /deploy/detail
 * 功能：返回部署任务完整信息含配置和日志
 */
func DeployDetail(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var deployTask models.CertDeploy
	if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "部署任务不存在")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	var account models.CertAccount
	database.WithContext(c).First(&account, deployTask.AccountID)

	var config map[string]interface{}
	if deployTask.Config != "" {
		if err := json.Unmarshal([]byte(deployTask.Config), &config); err != nil {
			middleware.ErrorResponse(c, "部署任务配置解析失败")
			return
		}
	}

	domainList := getOrderDomainList(c, strconv.FormatUint(uint64(deployTask.OrderID), 10))

	cfg, _ := deploy.GetDeployConfig(account.Type)

	middleware.SuccessResponse(c, gin.H{
		"id":           deployTask.ID,
		"aid":          deployTask.AccountID,
		"oid":          deployTask.OrderID,
		"account_name": account.Name,
		"account_type": account.Type,
		"type_name":    cfg.Name,
		"config":       config,
		"remark":       deployTask.Remark,
		"status":       deployTask.Status,
		"active":       deployTask.Active,
		"domains":      domainList,
	})
}

/*
 * DeployAdd 创建部署任务
 * @route POST /deploy/add
 * 功能：创建证书部署任务，关联订单和部署账户
 */
func DeployAdd(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		AID    string                 `json:"aid"`
		OID    string                 `json:"oid"`
		Config map[string]interface{} `json:"config"`
		Remark string                 `json:"remark"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.AID == "" {
		middleware.ErrorResponse(c, "请选择部署账户")
		return
	}
	if req.OID == "" {
		middleware.ErrorResponse(c, "请选择证书订单")
		return
	}

	// 验证账户存在
	var account models.CertAccount
	if err := database.WithContext(c).First(&account, req.AID).Error; err != nil {
		middleware.ErrorResponse(c, "部署账户不存在")
		return
	}

	/* 非管理员只能使用自己的账户 */
	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限使用该部署账户")
		return
	}

	// 验证订单存在
	var order models.CertOrder
	if err := database.WithContext(c).First(&order, req.OID).Error; err != nil {
		middleware.ErrorResponse(c, "证书订单不存在")
		return
	}

	/* 非管理员只能关联自己的订单 */
	if !isAdmin(c) && !checkCertOrderAccess(c, strconv.FormatUint(uint64(order.ID), 10)) {
		middleware.ErrorResponse(c, "无权限使用该证书订单")
		return
	}

	aidU, err := strconv.ParseUint(req.AID, 10, 32)
	if err != nil {
		middleware.ErrorResponse(c, "无效的部署账户ID")
		return
	}
	oidU, err := strconv.ParseUint(req.OID, 10, 32)
	if err != nil {
		middleware.ErrorResponse(c, "无效的证书订单ID")
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	deployTask := models.CertDeploy{
		UserID:    currentUIDUint(c),
		AccountID: uint(aidU),
		OrderID:   uint(oidU),
		Config:    string(configJSON),
		Remark:    req.Remark,
		Active:    true,
	}

	if err := database.WithContext(c).Create(&deployTask).Error; err != nil {
		middleware.ErrorResponse(c, "创建失败")
		return
	}

	service.Audit.LogAction(c, "create_deploy_task", "", fmt.Sprintf("创建部署任务: 账户=%s, 订单=%s", account.Name, order.ID))
	middleware.SuccessResponse(c, gin.H{"id": deployTask.ID})
}

/*
 * DeployEdit 更新部署任务
 * @route POST /deploy/edit
 * 功能：修改部署任务的配置、关联订单、备注等
 */
func DeployEdit(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID     string                 `json:"id"`
		OID    string                 `json:"oid"`
		Config map[string]interface{} `json:"config"`
		Remark string                 `json:"remark"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var deployTask models.CertDeploy
	if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "部署任务不存在")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	updates := map[string]interface{}{}
	if req.OID != "" {
		/* 非管理员修改关联订单时校验订单访问权限 */
		if !isAdmin(c) && !checkCertOrderAccess(c, req.OID) {
			middleware.ErrorResponse(c, "无权限使用该证书订单")
			return
		}
		updates["oid"] = req.OID
	}
	if req.Config != nil {
		configJSON, _ := json.Marshal(req.Config)
		updates["config"] = string(configJSON)
	}
	updates["remark"] = req.Remark

	database.WithContext(c).Model(&deployTask).Updates(updates)
	service.Audit.LogAction(c, "update_deploy_task", "", fmt.Sprintf("更新部署任务: %s", req.ID))
	middleware.SuccessMsg(c, "更新成功")
}

/*
 * DeployDelete 删除部署任务
 * @route POST /deploy/delete
 * 功能：删除指定的部署任务
 */
func DeployDelete(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var deployTask models.CertDeploy
	if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "部署任务不存在")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	database.WithContext(c).Delete(&models.CertDeploy{}, req.ID)
	service.Audit.LogAction(c, "delete_deploy_task", "", fmt.Sprintf("删除部署任务: %s", req.ID))
	middleware.SuccessMsg(c, "删除成功")
}

/*
 * DeployToggle 启用/禁用部署任务
 * @route POST /deploy/toggle
 * 功能：切换部署任务的激活状态
 */
func DeployToggle(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID     string `json:"id"`
		Active bool   `json:"active"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var deployTask models.CertDeploy
	if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "部署任务不存在")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	database.WithContext(c).Model(&deployTask).Update("active", req.Active)
	status := "已停止"
	if req.Active {
		status = "已开启"
	}
	middleware.SuccessMsg(c, status)
}

/*
 * DeployProcess 手动执行部署
 * @route POST /deploy/process
 * 功能：异步执行证书部署到目标服务器
 */
func DeployProcess(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID    string `json:"id"`
		Reset bool   `json:"reset"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var deployTask models.CertDeploy
	if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "部署任务不存在")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	// 检查是否正在执行
	if deployTask.IsLock {
		middleware.ErrorResponse(c, "任务正在执行中，请稍后再试")
		return
	}

	if req.Reset {
		database.WithContext(c).Model(&deployTask).Updates(map[string]interface{}{
			"status":  0,
			"error":   "",
			"is_lock": false,
			"retry":   0,
		})
	}

	var order models.CertOrder
	if err := database.WithContext(c).First(&order, deployTask.OrderID).Error; err != nil {
		middleware.ErrorResponse(c, "证书订单不存在")
		return
	}

	if order.FullChain == "" || order.PrivateKey == "" {
		middleware.ErrorResponse(c, "证书尚未签发")
		return
	}

	var account models.CertAccount
	if err := database.WithContext(c).First(&account, deployTask.AccountID).Error; err != nil {
		middleware.ErrorResponse(c, "部署账户不存在")
		return
	}

	var accConfig map[string]interface{}
	if account.Config != "" {
		if err := json.Unmarshal([]byte(account.Config), &accConfig); err != nil {
			middleware.ErrorResponse(c, "部署账户配置解析失败")
			return
		}
	}

	var deployConfig map[string]interface{}
	if deployTask.Config != "" {
		if err := json.Unmarshal([]byte(deployTask.Config), &deployConfig); err != nil {
			middleware.ErrorResponse(c, "部署任务配置解析失败")
			return
		}
	}
	if deployConfig == nil {
		deployConfig = map[string]interface{}{}
	}

	provider, err := deploy.GetProvider(account.Type, accConfig, deployConfig)
	if err != nil {
		middleware.ErrorResponse(c, "获取部署提供商失败: "+err.Error())
		return
	}

	domainList := getOrderDomainList(c, strconv.FormatUint(uint64(order.ID), 10))
	if _, ok := deployConfig["domainList"]; !ok {
		deployConfig["domainList"] = domainList
	}
	if _, ok := deployConfig["domains"]; !ok {
		deployConfig["domains"] = strings.Join(domainList, ",")
	}

	/* 加锁并异步执行部署 */
	now := time.Now()
	database.DB.Model(&deployTask).Updates(map[string]interface{}{
		"status":      1,
		"is_lock":     true,
		"lock_time":   now,
		"log_content": "",
	})

	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	taskID := deployTask.ID
	fullChain := order.FullChain
	privateKey := order.PrivateKey
	issueTime := order.IssueTime

	utils.SafeGo(func() {
		var logBuilder strings.Builder
		provider.SetLogger(func(msg string) {
			logBuilder.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().Format("15:04:05"), msg))
		})

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		if err := provider.Deploy(ctx, fullChain, privateKey, deployConfig); err != nil {
			logBuilder.WriteString(fmt.Sprintf("[%s] 部署失败: %s\n", time.Now().Format("15:04:05"), err.Error()))
			database.DB.Model(&models.CertDeploy{}).Where("id = ?", taskID).Updates(map[string]interface{}{
				"status":      -1,
				"error":       err.Error(),
				"is_lock":     false,
				"log_content": logBuilder.String(),
				"last_time":   time.Now(),
			})
			service.Audit.LogActionDirect(userID, username, ip, ua, "process_deploy_failed", "", fmt.Sprintf("部署任务 %s 失败: %s", taskID, err.Error()))
			return
		}

		logBuilder.WriteString(fmt.Sprintf("[%s] 部署成功\n", time.Now().Format("15:04:05")))
		database.DB.Model(&models.CertDeploy{}).Where("id = ?", taskID).Updates(map[string]interface{}{
			"status":      2,
			"error":       "",
			"is_lock":     false,
			"log_content": logBuilder.String(),
			"last_time":   time.Now(),
			"issue_time":  issueTime,
		})
		service.Audit.LogActionDirect(userID, username, ip, ua, "process_deploy", "", fmt.Sprintf("手动执行部署任务: %s", taskID))
	})

	middleware.SuccessMsg(c, "提交成功")
}

/*
 * DeployReset 重置部署任务状态
 * @route POST /deploy/reset
 * 功能：重置失败的部署任务状态为待执行
 */
func DeployReset(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var deployTask models.CertDeploy
	if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "部署任务不存在")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	database.WithContext(c).Model(&deployTask).Updates(map[string]interface{}{
		"status":  0,
		"error":   "",
		"is_lock": false,
		"retry":   0,
	})
	middleware.SuccessMsg(c, "重置成功")
}

/*
 * DeployLog 获取部署实时日志
 * @route POST /deploy/log
 * 功能：返回部署任务的实时执行日志
 */
func DeployLog(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		ID        string `json:"id"`
		ProcessID string `json:"process_id"`
	}
	middleware.BindDecryptedData(c, &req)

	var deployTask models.CertDeploy
	if req.ID != "" {
		if err := database.WithContext(c).First(&deployTask, req.ID).Error; err != nil {
			middleware.ErrorResponse(c, "部署任务不存在")
			return
		}
	} else if req.ProcessID != "" {
		if err := database.WithContext(c).Where("process_id = ?", req.ProcessID).First(&deployTask).Error; err != nil {
			middleware.ErrorResponse(c, "部署任务不存在")
			return
		}
	} else {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	if !isAdmin(c) && deployTask.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	logContent := deployTask.LogContent
	if logContent == "" && deployTask.Info != "" {
		logContent = deployTask.Info
	}
	if logContent == "" {
		logContent = "暂无日志"
	}

	middleware.SuccessResponse(c, logContent)
}

/*
 * DeployBatch 批量操作部署任务
 * @route POST /deploy/batch
 * 功能：批量删除/重置/启用/禁用部署任务
 */
func DeployBatch(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	var req struct {
		Action string   `json:"action"`
		IDs    []string `json:"ids"`
		CertID string   `json:"certid"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.Action == "" {
		middleware.ErrorResponse(c, "缺少操作类型")
		return
	}

	if len(req.IDs) == 0 {
		middleware.ErrorResponse(c, "请选择要操作的任务")
		return
	}

	// 非管理员只能操作自己的任务
	if !isAdmin(c) {
		var ownCount int64
		database.WithContext(c).Model(&models.CertDeploy{}).Where("id IN ? AND uid = ?", req.IDs, currentUIDUint(c)).Count(&ownCount)
		if ownCount != int64(len(req.IDs)) {
			middleware.ErrorResponse(c, "无权限")
			return
		}
	}

	switch req.Action {
	case "delete":
		database.WithContext(c).Delete(&models.CertDeploy{}, req.IDs)
		service.Audit.LogAction(c, "batch_delete_deploy", "", fmt.Sprintf("批量删除 %d 个部署任务", len(req.IDs)))
		middleware.SuccessMsg(c, fmt.Sprintf("成功删除 %d 个任务", len(req.IDs)))
	case "reset":
		database.WithContext(c).Model(&models.CertDeploy{}).Where("id IN ?", req.IDs).Updates(map[string]interface{}{
			"status":  0,
			"error":   "",
			"is_lock": false,
			"retry":   0,
		})
		middleware.SuccessMsg(c, fmt.Sprintf("成功重置 %d 个任务", len(req.IDs)))
	case "open":
		database.WithContext(c).Model(&models.CertDeploy{}).Where("id IN ?", req.IDs).Update("active", true)
		middleware.SuccessMsg(c, fmt.Sprintf("成功开启 %d 个任务", len(req.IDs)))
	case "close":
		database.WithContext(c).Model(&models.CertDeploy{}).Where("id IN ?", req.IDs).Update("active", false)
		middleware.SuccessMsg(c, fmt.Sprintf("成功停止 %d 个任务", len(req.IDs)))
	case "changecert":
		if req.CertID == "" {
			middleware.ErrorResponse(c, "请选择要关联的证书")
			return
		}
		database.WithContext(c).Model(&models.CertDeploy{}).Where("id IN ?", req.IDs).Update("oid", req.CertID)
		middleware.SuccessMsg(c, fmt.Sprintf("成功修改 %d 个任务", len(req.IDs)))
	default:
		middleware.ErrorResponse(c, "不支持的操作")
	}
}

/*
 * DeployTypes 获取所有部署器类型
 * @route POST /deploy/types
 * 功能：返回所有已注册的部署提供商，按分类分组
 */
func DeployTypes(c *gin.Context) {
	if !requireUserModule(c, "deploy") {
		return
	}
	configs := deploy.GetAllDeployConfigs()

	grouped := map[int][]gin.H{
		deploy.ClassSelfHosted:   {},
		deploy.ClassCloudService: {},
		deploy.ClassServer:       {},
	}

	for _, cfg := range configs {
		item := gin.H{
			"type":        cfg.Type,
			"name":        cfg.Name,
			"icon":        cfg.Icon,
			"desc":        cfg.Desc,
			"note":        cfg.Note,
			"inputs":      cfg.Inputs,
			"task_inputs": cfg.TaskInputs,
			"task_note":   cfg.TaskNote,
		}
		grouped[cfg.Class] = append(grouped[cfg.Class], item)
	}

	middleware.SuccessResponse(c, gin.H{
		"class_names": deploy.ClassNames,
		"types":       grouped,
	})
}

// getOrderDomainList 返回证书订单下的域名列表（oid 为订单 ID 字符串）
func getOrderDomainList(c *gin.Context, orderIDStr string) []string {
	id, err := strconv.ParseUint(orderIDStr, 10, 32)
	if err != nil {
		return nil
	}
	var domains []models.CertDomain
	database.WithContext(c).Where("oid = ?", uint(id)).Order("sort ASC").Find(&domains)
	out := make([]string, 0, len(domains))
	for _, d := range domains {
		out = append(out, d.Domain)
	}
	return out
}

// checkCertOrderAccess 校验当前用户是否拥有该订单对应证书账户（cert_accounts.uid）
func checkCertOrderAccess(c *gin.Context, orderIDStr string) bool {
	id, err := strconv.ParseUint(orderIDStr, 10, 32)
	if err != nil {
		return false
	}
	var order models.CertOrder
	if err := database.WithContext(c).First(&order, uint(id)).Error; err != nil {
		return false
	}
	var account models.CertAccount
	if err := database.WithContext(c).First(&account, order.AccountID).Error; err != nil {
		return false
	}
	return account.UserID == currentUIDUint(c)
}
