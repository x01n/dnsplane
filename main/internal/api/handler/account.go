package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dbcache"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/service"
	"main/internal/utils"

	"github.com/gin-gonic/gin"
)

/* checkAdmin 检查当前请求用户是否为管理员（level >= 2） */
func checkAdmin(c *gin.Context) bool {
	level, exists := c.Get("level")
	if !exists {
		return false
	}
	// 支持int和float64类型（JWT解析可能返回float64）
	switch v := level.(type) {
	case int:
		return v >= 2
	case float64:
		return int(v) >= 2
	}
	return false
}

/* isAdmin 返回当前用户是否管理员 */
func isAdmin(c *gin.Context) bool {
	return c.GetInt("level") >= 2
}

/* currentUID 返回当前请求的用户 ID */
func currentUID(c *gin.Context) string {
	return c.GetString("user_id")
}

func currentUIDUint(c *gin.Context) uint {
	u, _ := strconv.ParseUint(c.GetString("user_id"), 10, 32)
	return uint(u)
}

/* requireUserModule 非管理员须拥有 users.permissions 中的模块 key（未配置 permissions 时放行） */
func requireUserModule(c *gin.Context, module string) bool {
	if !middleware.UserModuleAllowed(c, module) {
		middleware.ErrorResponse(c, "无权限访问该功能模块")
		return false
	}
	return true
}

// accountsListCache DNS 账户列表 API 缓存结构（与 SuccessResponse 字段一致）
type accountsListCache struct {
	Total int                `json:"total"`
	List  []accountCacheItem `json:"list"`
}

type accountCacheItem struct {
	ID        uint      `json:"id"`
	UID       uint      `json:"uid"`
	Type      string    `json:"type"`
	TypeName  string    `json:"type_name"`
	Name      string    `json:"name"`
	Config    string    `json:"config"`
	Remark    string    `json:"remark"`
	CreatedAt time.Time `json:"created_at"`
}

/*
 * GetAccounts 获取 DNS 账户列表
 * @route POST /accounts/list
 * 功能：管理员看全部账户，普通用户只看自己的账户，隐藏敏感配置
 */
func GetAccounts(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	uid := currentUID(c)
	admin := isAdmin(c)
	cacheKey := dbcache.KeyAccountsUser(uid)
	if admin {
		cacheKey = dbcache.KeyAccountsAdmin()
	}

	var payload accountsListCache
	if err := dbcache.GetOrSetJSON(c.Request.Context(), cacheKey, dbcache.DefaultTTL, func() (interface{}, error) {
		var accounts []models.Account
		if admin {
			database.WithContext(c).Find(&accounts)
		} else {
			database.WithContext(c).Where("uid = ?", uid).Find(&accounts)
		}
		list := make([]accountCacheItem, 0, len(accounts))
		for _, acc := range accounts {
			cfg, _ := dns.GetProviderConfig(acc.Type)
			list = append(list, accountCacheItem{
				ID: acc.ID, UID: acc.UserID, Type: acc.Type, TypeName: cfg.Name,
				Name: acc.Name, Config: acc.Config, Remark: acc.Remark, CreatedAt: acc.CreatedAt,
			})
		}
		return accountsListCache{Total: len(accounts), List: list}, nil
	}, &payload); err != nil {
		middleware.ErrorResponse(c, "加载账户列表失败")
		return
	}

	result := make([]gin.H, 0, len(payload.List))
	for _, row := range payload.List {
		result = append(result, gin.H{
			"id": row.ID, "uid": row.UID, "type": row.Type, "type_name": row.TypeName,
			"name": row.Name, "config": row.Config, "remark": row.Remark, "created_at": row.CreatedAt,
		})
	}
	middleware.SuccessResponse(c, gin.H{"total": payload.Total, "list": result})
}

/*
 * GetAccountDetail 获取 DNS 账户详情（含配置）
 * @route POST /accounts/detail
 * 功能：返回账户完整配置信息，用于编辑页面回显
 */
func GetAccountDetail(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req struct {
		ID string `json:"id"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.Account
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	// 非管理员只能查看自己的账户
	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	cfg, _ := dns.GetProviderConfig(account.Type)
	middleware.SuccessResponse(c, gin.H{
		"id":         account.ID,
		"uid":        account.UserID,
		"type":       account.Type,
		"type_name":  cfg.Name,
		"name":       account.Name,
		"config":     account.Config,
		"remark":     account.Remark,
		"created_at": account.CreatedAt,
	})
}

type CreateAccountRequest struct {
	Type   string            `json:"type" binding:"required"`
	Name   string            `json:"name" binding:"required"`
	Config map[string]string `json:"config" binding:"required"`
	Remark string            `json:"remark"`
}

/*
 * CreateAccount 创建 DNS 账户
 * @route POST /accounts/create
 * 功能：创建新的 DNS 服务商账户，存储加密后的 API 密钥配置
 */
func CreateAccount(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req CreateAccountRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.Type == "" || req.Name == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	/* 套餐配额检查 */
	if !CheckQuota(c, currentUID(c), "accounts") {
		return
	}

	if _, ok := dns.GetProviderConfig(req.Type); !ok {
		logger.Warn("创建DNS账户失败: 不支持的DNS类型 - 类型: %s", req.Type)
		middleware.ErrorResponse(c, "不支持的DNS类型")
		return
	}

	uid := currentUID(c)
	var count int64
	database.WithContext(c).Model(&models.Account{}).Where("type = ? AND name = ? AND uid = ?", req.Type, req.Name, uid).Count(&count)
	if count > 0 {
		middleware.ErrorResponse(c, "同类型同名账户已存在")
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	account := models.Account{
		UserID: currentUIDUint(c),
		Type:   req.Type,
		Name:   req.Name,
		Config: string(configJSON),
		Remark: req.Remark,
	}

	if err := database.WithContext(c).Create(&account).Error; err != nil {
		logger.Error("创建DNS账户失败: %v", err)
		middleware.ErrorResponse(c, "创建失败")
		return
	}

	logger.Info("创建DNS账户成功: %s (ID: %d)", req.Name, account.ID)
	dbcache.BustAccounts(uid)
	service.Audit.LogAction(c, "create_account", "", fmt.Sprintf("创建DNS账户: %s (%s)", req.Name, req.Type))
	middleware.SuccessResponse(c, gin.H{"id": account.ID})
}

type UpdateAccountRequest struct {
	ID     string            `json:"id"`
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Config map[string]string `json:"config"`
	Remark string            `json:"remark"`
}

/*
 * UpdateAccount 更新 DNS 账户
 * @route POST /accounts/update
 * 功能：更新 DNS 账户名称、配置、备注等信息
 */
func UpdateAccount(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req UpdateAccountRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.Account
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	configJSON, _ := json.Marshal(req.Config)
	updates := map[string]interface{}{
		"name":   req.Name,
		"config": string(configJSON),
		"remark": req.Remark,
	}

	database.WithContext(c).Model(&account).Updates(updates)
	dbcache.BustAccounts(strconv.FormatUint(uint64(account.UserID), 10))
	logger.Info("更新DNS账户成功: %s (ID: %s)", req.Name, req.ID)
	service.Audit.LogAction(c, "update_account", "", fmt.Sprintf("更新DNS账户: %s (ID: %s)", req.Name, req.ID))
	middleware.SuccessMsg(c, "更新成功")
}

type DeleteAccountRequest struct {
	ID string `json:"id"`
}

/*
 * DeleteAccount 删除 DNS 账户
 * @route POST /accounts/delete
 * 功能：删除账户前检查是否有关联域名，有则拒绝删除
 */
func DeleteAccount(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req DeleteAccountRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		req.ID = c.Param("id")
	}
	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var count int64
	database.WithContext(c).Model(&models.Domain{}).Where("aid = ?", req.ID).Count(&count)
	if count > 0 {
		logger.Warn("删除DNS账户失败: 账户下存在域名，无法删除 - 账户ID: %s", req.ID)
		middleware.ErrorResponse(c, "该账户下存在域名，无法删除")
		return
	}

	var account models.Account
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	ownerUID := strconv.FormatUint(uint64(account.UserID), 10)
	database.WithContext(c).Delete(&models.Account{}, req.ID)
	dbcache.BustAccounts(ownerUID)
	logger.Info("删除DNS账户成功: 账户ID %s", req.ID)
	service.Audit.LogAction(c, "delete_account", "", fmt.Sprintf("删除DNS账户: ID %s", req.ID))
	middleware.SuccessMsg(c, "删除成功")
}

type CheckAccountRequest struct {
	ID string `json:"id"`
}

/*
 * CheckAccount 检测 DNS 账户连接
 * @route POST /accounts/check
 * 功能：异步检测账户连通性，立即返回“提交成功”，后台执行
 */
func CheckAccount(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req CheckAccountRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		req.ID = c.Param("id")
	}
	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.Account
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	var config map[string]string
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		middleware.ErrorResponse(c, "账户配置解析失败")
		return
	}

	provider, err := dns.GetProvider(account.Type, config, "", "")
	if err != nil {
		logger.Error("检测DNS账户连接失败: %v", err)
		middleware.ErrorResponse(c, err.Error())
		return
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	accountName := account.Name
	accountID := req.ID

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := provider.Check(ctx); err != nil {
			logger.Warn("DNS账户连接失败: %s (ID: %s) - 错误: %v", accountName, accountID, err)
			service.Audit.LogActionDirect(userID, username, ip, ua, "check_account_failed", "", fmt.Sprintf("检测DNS账户连接失败: %s (ID: %s) - %s", accountName, accountID, err.Error()))
			return
		}
		logger.Info("DNS账户连接成功: %s (ID: %s)", accountName, accountID)
		service.Audit.LogActionDirect(userID, username, ip, ua, "check_account", "", fmt.Sprintf("检测DNS账户连接: %s (ID: %s)", accountName, accountID))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type DNSProviderResponse struct {
	Type   string            `json:"type"`
	Name   string            `json:"name"`
	Icon   string            `json:"icon"`
	Config []dns.ConfigField `json:"config"`
	Add    bool              `json:"add"`
}

/*
 * GetDNSProviders 获取支持的 DNS 服务商列表
 * @route POST /accounts/providers
 * 功能：返回所有已注册的 DNS provider 类型、名称、配置字段
 */
func GetDNSProviders(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	providersMap := dns.GetAllProviderConfigs()
	providers := make([]DNSProviderResponse, 0, len(providersMap))
	for _, cfg := range providersMap {
		providers = append(providers, DNSProviderResponse{
			Type:   cfg.Type,
			Name:   cfg.Name,
			Icon:   cfg.Icon,
			Config: cfg.Config,
			Add:    cfg.Features.Add,
		})
	}

	sort.Slice(providers, func(i, j int) bool {
		return providers[i].Type < providers[j].Type
	})

	middleware.SuccessResponse(c, providers)
}
