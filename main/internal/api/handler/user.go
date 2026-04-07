package handler

import (
	"crypto/rand"
	"encoding/hex"
	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dbcache"
	"main/internal/models"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"
)

// userPublicRow 用户列表对外字段（不读 password 等大字段）
type userPublicRow struct {
	ID          uint       `json:"id"`
	Username    string     `json:"username"`
	Email       string     `json:"email"`
	Level       int        `json:"level"`
	IsAPI       bool       `json:"is_api"`
	APIKey      string     `json:"api_key,omitempty"`
	Status      int        `json:"status"`
	Permissions string     `json:"permissions"`
	TOTPOpen    bool       `json:"totp_open"`
	RegTime     time.Time  `json:"reg_time"`
	LastTime    *time.Time `json:"last_time"`
}

// generateAPIKey 生成随机API Key
func generateAPIKey() string {
	bytes := make([]byte, 16)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func GetUsers(c *gin.Context) {
	type usersCachePayload struct {
		Total int             `json:"total"`
		List  []userPublicRow `json:"list"`
	}
	var payload usersCachePayload
	if err := dbcache.GetOrSetJSON(c.Request.Context(), dbcache.KeyUsersAdminFullList(), dbcache.DefaultTTL, func() (interface{}, error) {
		var users []models.User
		if err := database.DB.Select(
			"id", "username", "email", "level", "is_api", "api_key", "status", "permissions", "totp_open", "reg_time", "last_time",
		).Find(&users).Error; err != nil {
			return nil, err
		}
		list := make([]userPublicRow, 0, len(users))
		for _, u := range users {
			list = append(list, userPublicRow{
				ID: u.ID, Username: u.Username, Email: u.Email, Level: u.Level, IsAPI: u.IsAPI, APIKey: u.APIKey,
				Status: u.Status, Permissions: u.Permissions, TOTPOpen: u.TOTPOpen, RegTime: u.RegTime, LastTime: u.LastTime,
			})
		}
		return usersCachePayload{Total: len(list), List: list}, nil
	}, &payload); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "加载失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"total": payload.Total, "list": payload.List}})
}

type CreateUserRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required,min=6"`
	Email       string `json:"email"`
	Level       int    `json:"level"`
	IsAPI       bool   `json:"is_api"`
	Permissions string `json:"permissions"`
}

func CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	var count int64
	database.DB.Model(&models.User{}).Where("username = ?", req.Username).Count(&count)
	if count > 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名已存在"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "密码加密失败"})
		return
	}

	user := models.User{
		Username:    req.Username,
		Password:    string(hashedPassword),
		Email:       req.Email,
		Level:       req.Level,
		IsAPI:       req.IsAPI,
		Permissions: req.Permissions,
		Status:      1,
		RegTime:     time.Now(),
	}

	// 如果启用API，自动生成API Key
	if req.IsAPI {
		user.APIKey = generateAPIKey()
	}

	if err := database.DB.Create(&user).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建失败"})
		return
	}
	dbcache.BustUserList()

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "创建成功", "data": gin.H{"id": user.ID, "api_key": user.APIKey}})
}

func UpdateUser(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var user models.User
	if err := database.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	var req struct {
		Password    string `json:"password"`
		Email       string `json:"email"`
		Level       int    `json:"level"`
		IsAPI       bool   `json:"is_api"`
		Status      int    `json:"status"`
		Permissions string `json:"permissions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	updates := map[string]interface{}{
		"email":       req.Email,
		"level":       req.Level,
		"is_api":      req.IsAPI,
		"status":      req.Status,
		"permissions": req.Permissions,
	}

	if req.Password != "" {
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
		updates["password"] = string(hashedPassword)
	}

	// 如果启用API且当前没有API Key，自动生成
	if req.IsAPI && user.APIKey == "" {
		updates["api_key"] = generateAPIKey()
	}
	// 如果禁用API，清除API Key
	if !req.IsAPI {
		updates["api_key"] = ""
	}

	database.DB.Model(&user).Updates(updates)

	// 获取更新后的用户信息
	database.DB.First(&user, id)
	dbcache.BustUserList()

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功", "data": gin.H{"api_key": user.APIKey}})
}

func DeleteUser(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	currentUserID := middleware.AuthUserID(c)
	if uint(id) == currentUserID {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "不能删除自己"})
		return
	}

	// 同时删除用户的权限
	database.DB.Where("uid = ?", id).Delete(&models.Permission{})
	database.DB.Delete(&models.User{}, id)
	dbcache.BustUserList()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// GetUserPermissions 获取用户权限列表
func GetUserPermissions(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var perms []models.Permission
	database.DB.Where("uid = ?", id).Find(&perms)

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": perms})
}

// AddUserPermission 添加用户权限
func AddUserPermission(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var req struct {
		DomainID   uint       `json:"did" binding:"required"`
		Domain     string     `json:"domain" binding:"required"`
		SubDomain  string     `json:"sub"`
		ReadOnly   bool       `json:"read_only"`
		ExpireTime *time.Time `json:"expire_time"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	perm := models.Permission{
		UserID:     uint(id),
		DomainID:   req.DomainID,
		Domain:     req.Domain,
		SubDomain:  req.SubDomain,
		ReadOnly:   req.ReadOnly,
		ExpireTime: req.ExpireTime,
		CreatedAt:  time.Now(),
	}

	if err := database.DB.Create(&perm).Error; err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "添加失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "添加成功", "data": gin.H{"id": perm.ID}})
}

// UpdateUserPermission 更新用户权限
func UpdateUserPermission(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	permId, _ := strconv.ParseUint(c.Param("permId"), 10, 32)

	var perm models.Permission
	if err := database.DB.Where("id = ? AND uid = ?", permId, id).First(&perm).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "权限不存在"})
		return
	}

	var req struct {
		SubDomain  string     `json:"sub"`
		ReadOnly   bool       `json:"read_only"`
		ExpireTime *time.Time `json:"expire_time"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	updates := map[string]interface{}{
		"sub":         req.SubDomain,
		"read_only":   req.ReadOnly,
		"expire_time": req.ExpireTime,
	}

	database.DB.Model(&perm).Updates(updates)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}

// DeleteUserPermission 删除用户权限
func DeleteUserPermission(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	permId, _ := strconv.ParseUint(c.Param("permId"), 10, 32)

	result := database.DB.Where("id = ? AND uid = ?", permId, id).Delete(&models.Permission{})
	if result.RowsAffected == 0 {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "权限不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// ResetAPIKey 重新生成API Key
func ResetAPIKey(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var user models.User
	if err := database.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	if !user.IsAPI {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "用户未启用API访问"})
		return
	}

	newKey := generateAPIKey()
	database.DB.Model(&user).Update("api_key", newKey)
	dbcache.BustUserList()

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "重新生成成功", "data": gin.H{"api_key": newKey}})
}

// applyOperationLogFilters 与前端日志页筛选一致；action 为前端下拉分类（如 account、domain）时按前缀匹配。
func applyOperationLogFilters(db *gorm.DB, keyword, entity, actionFilter, userID, domain string) *gorm.DB {
	q := db
	if keyword != "" {
		kw := "%" + keyword + "%"
		q = q.Where("domain LIKE ? OR action LIKE ? OR data LIKE ? OR username LIKE ?",
			kw, kw, kw, kw)
	}
	if entity != "" {
		q = q.Where("entity = ?", entity)
	}
	if actionFilter != "" {
		switch actionFilter {
		case "login":
			q = q.Where("action = ?", "login")
		case "account":
			q = q.Where("action LIKE ?", "accounts_%")
		case "domain":
			q = q.Where("action LIKE ? OR action IN ?", "domains_%", []string{"delete_domain", "update_domain"})
		case "record":
			q = q.Where("action LIKE ?", "domains_records_%")
		case "monitor":
			q = q.Where("action LIKE ?", "monitor_%")
		case "cert":
			q = q.Where("action LIKE ? AND action NOT LIKE ? AND action NOT LIKE ?",
				"cert_%", "cert_deploy%", "cert_deploy-account%")
		case "deploy":
			q = q.Where("action LIKE ? OR action LIKE ? OR action IN ?",
				"cert_deploy%", "cert_deploy-account%",
				[]string{"process_deploy", "create_cert_deploy", "delete_cert_deploy", "batch_delete_deploy"})
		case "user":
			q = q.Where("action LIKE ? OR action LIKE ?", "users_%", "user_%")
		case "system":
			q = q.Where("action LIKE ? OR action IN ?", "system_%",
				[]string{"update_system_config", "clear_cache", "clean_logs", "update_cron_config"})
		case "totp":
			q = q.Where("action LIKE ? OR action IN ?", "user_totp%",
				[]string{"enable_totp", "disable_totp", "reset_totp"})
		default:
			q = q.Where("action = ?", actionFilter)
		}
	}
	if userID != "" {
		q = q.Where("uid = ?", userID)
	}
	if domain != "" {
		q = q.Where("domain LIKE ?", "%"+domain+"%")
	}
	return q
}

func GetLogs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
	keyword := c.Query("keyword")
	entity := c.Query("entity")
	action := c.Query("action")
	userID := c.Query("user_id")
	if userID == "" {
		userID = c.Query("uid")
	}
	domain := c.Query("domain")

	var logs []models.Log
	var total int64

	if database.LogDB == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
			"total": 0, "list": []models.Log{}, "records": []models.Log{},
			"stats": gin.H{"today_count": 0, "distinct_users": 0, "distinct_domains": 0},
		}})
		return
	}

	base := applyOperationLogFilters(database.LogDB.Model(&models.Log{}), keyword, entity, action, userID, domain)
	base.Count(&total)
	base.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	var todayCount int64
	applyOperationLogFilters(database.LogDB.Model(&models.Log{}), keyword, entity, action, userID, domain).
		Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).
		Count(&todayCount)

	var uids []uint
	applyOperationLogFilters(database.LogDB.Model(&models.Log{}), keyword, entity, action, userID, domain).
		Where("uid > 0").
		Distinct("uid").
		Pluck("uid", &uids)
	distinctUsers := int64(len(uids))

	var doms []string
	applyOperationLogFilters(database.LogDB.Model(&models.Log{}), keyword, entity, action, userID, domain).
		Where("domain != ?", "").
		Distinct("domain").
		Pluck("domain", &doms)
	distinctDomains := int64(len(doms))

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total":   total,
			"list":    logs,
			"records": logs,
			"stats": gin.H{
				"today_count":       todayCount,
				"distinct_users":    distinctUsers,
				"distinct_domains": distinctDomains,
			},
		},
	})
}

// GetLogDetail 获取日志详情
func GetLogDetail(c *gin.Context) {
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	if database.LogDB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "msg": "日志库未初始化"})
		return
	}
	var log models.Log
	if err := database.LogDB.First(&log, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "日志不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": log,
	})
}

func GetSystemConfig(c *gin.Context) {
	var configs []models.SysConfig
	database.DB.Find(&configs)

	result := make(map[string]string)
	for _, cfg := range configs {
		result[cfg.Key] = cfg.Value
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

func UpdateSystemConfig(c *gin.Context) {
	var req map[string]string
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	for key, value := range req {
		database.DB.Where("key = ?", key).Assign(models.SysConfig{Key: key, Value: value}).FirstOrCreate(&models.SysConfig{})
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}
