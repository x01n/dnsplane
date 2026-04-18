package handler

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dbcache"
	"main/internal/models"
	"main/internal/utils"
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

// generateAPIKey 生成随机 API Key。
//
// 安全审计 L-4：从 16 字节 (128bit) 提升至 32 字节 (256bit)。
// API Key 同时充当 HMAC-SHA256 的签名密钥（见 apikey.go），
// HMAC 密钥的有效强度 = min(key_len, hash_output_len)；32 字节可完全利用 SHA-256 的安全边界。
// 输出为 64 位 hex 字符串，与原 32 位 hex 相比 URL 长度仅翻倍，无运行期负担。
func generateAPIKey() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

func GetUsers(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
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
	Password    string `json:"password" binding:"required,min=8"`
	Email       string `json:"email"`
	Level       int    `json:"level"`
	IsAPI       bool   `json:"is_api"`
	Permissions string `json:"permissions"`
}

func CreateUser(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
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

	// 强密码复杂度校验（安全审计 H-4）
	if msg := utils.ValidatePasswordStrength(req.Password); msg != "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msg})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
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
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var user models.User
	if err := database.DB.First(&user, id).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	// 安全审计 M-3：管理员层级约束。
	// 非操作自己时，不允许修改等级 ≥ 自己的其他用户；防止同级管理员互删/互锁。
	currentLevel := c.GetInt("level")
	currentUserID := middleware.AuthUserID(c)
	if uint(id) != currentUserID && user.Level >= currentLevel {
		middleware.ErrorResponse(c, "无权修改同级或更高权限的用户")
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

	// 不允许把他人 level 设置到 >= 自己的值（阻断横向提权路径）
	if uint(id) != currentUserID && req.Level >= currentLevel {
		middleware.ErrorResponse(c, "无权将他人等级提升至同级或以上")
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
		hashedPassword, _ := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
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
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	currentUserID := middleware.AuthUserID(c)
	if uint(id) == currentUserID {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "不能删除自己"})
		return
	}

	// 安全审计 M-3：不允许删除等级 ≥ 自己的其他管理员
	var target models.User
	if err := database.DB.Select("id", "level").First(&target, id).Error; err == nil {
		if target.Level >= c.GetInt("level") {
			middleware.ErrorResponse(c, "无权删除同级或更高权限的用户")
			return
		}
	}

	// 同时删除用户的权限
	database.DB.Where("uid = ?", id).Delete(&models.Permission{})
	database.DB.Delete(&models.User{}, id)
	dbcache.BustUserList()
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "删除成功"})
}

// GetUserPermissions 获取用户权限列表
func GetUserPermissions(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)

	var perms []models.Permission
	database.DB.Where("uid = ?", id).Find(&perms)

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": perms})
}

// AddUserPermission 添加用户权限
func AddUserPermission(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
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
	if !requireAdmin(c) {
		return
	}
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
	if !requireAdmin(c) {
		return
	}
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
	if !requireAdmin(c) {
		return
	}
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

// applyLogDateRange 按创建日期筛选（date_from / date_to 为 YYYY-MM-DD，含结束日当天）
func applyLogDateRange(db *gorm.DB, dateFrom, dateTo string) *gorm.DB {
	q := db
	if dateFrom != "" {
		if t, err := time.ParseInLocation("2006-01-02", dateFrom, time.Local); err == nil {
			q = q.Where("created_at >= ?", t)
		}
	}
	if dateTo != "" {
		if t, err := time.ParseInLocation("2006-01-02", dateTo, time.Local); err == nil {
			q = q.Where("created_at < ?", t.AddDate(0, 0, 1))
		}
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
	dateFrom := c.Query("date_from")
	dateTo := c.Query("date_to")

	var logs []models.Log
	var total int64

	if database.LogDB == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{
			"total": 0, "list": []models.Log{}, "records": []models.Log{},
			"stats": gin.H{"today_count": 0, "distinct_users": 0, "distinct_domains": 0},
		}})
		return
	}

	base := applyLogDateRange(
		applyOperationLogFilters(database.LogDB.Model(&models.Log{}), keyword, entity, action, userID, domain),
		dateFrom, dateTo,
	)
	base.Count(&total)
	base.Order("id DESC").Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)

	now := time.Now()
	dayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	dayEnd := dayStart.Add(24 * time.Hour)

	filtered := func() *gorm.DB {
		return applyLogDateRange(
			applyOperationLogFilters(database.LogDB.Model(&models.Log{}), keyword, entity, action, userID, domain),
			dateFrom, dateTo,
		)
	}

	var todayCount int64
	filtered().
		Where("created_at >= ? AND created_at < ?", dayStart, dayEnd).
		Count(&todayCount)

	var uids []uint
	filtered().
		Where("uid > 0").
		Distinct("uid").
		Pluck("uid", &uids)
	distinctUsers := int64(len(uids))

	var doms []string
	filtered().
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
				"today_count":      todayCount,
				"distinct_users":   distinctUsers,
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
	// 安全审计 R-2：原实现无任何鉴权，普通用户可读取 mail_password / tgbot_token /
	// webhook_url / oauth_*_secret / turnstile_secret_key 等全部敏感凭据。
	if !requireAdmin(c) {
		return
	}
	var configs []models.SysConfig
	database.DB.Find(&configs)

	result := make(map[string]string)
	for _, cfg := range configs {
		result[cfg.Key] = cfg.Value
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "data": result})
}

// sysConfigJSONValueToDB 将前端 JSON 任意类型转为 SysConfig 表中的字符串（bool/number/null 等）
func sysConfigJSONValueToDB(v interface{}) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		if t == float64(int64(t)) {
			return strconv.FormatInt(int64(t), 10)
		}
		return fmt.Sprintf("%g", t)
	case json.Number:
		return t.String()
	case []interface{}, map[string]interface{}:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	default:
		return fmt.Sprintf("%v", v)
	}
}

func UpdateSystemConfig(c *gin.Context) {
	// 安全审计 R-2：原实现无任何鉴权，普通用户可改 site_url 钓鱼 magic-link、
	// 关闭 login_captcha、改 webhook_url 触发 SSRF 等。
	if !requireAdmin(c) {
		return
	}
	var req map[string]interface{}
	if d := middleware.GetDecryptedData(c); d != nil {
		req = d
	} else {
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
			return
		}
	}
	if len(req) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	for key, val := range req {
		value := sysConfigJSONValueToDB(val)
		database.DB.Where("key = ?", key).Assign(models.SysConfig{Key: key, Value: value}).FirstOrCreate(&models.SysConfig{})
	}

	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "更新成功"})
}
