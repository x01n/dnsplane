package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/utils"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"golang.org/x/crypto/bcrypt"
)

var store = base64Captcha.DefaultMemStore

type LoginRequest struct {
	Username    string `json:"username" binding:"required"`
	Password    string `json:"password" binding:"required"`
	CaptchaID   string `json:"captcha_id"`
	CaptchaCode string `json:"captcha_code"`
	TOTPCode    string `json:"totp_code"`
}

func GetCaptcha(c *gin.Context) {
	driver := base64Captcha.NewDriverString(60, 200, 2, 2, 4, "23456789ABCDEFGHJKLMNPQRSTUVWXYZ", nil, nil, nil)
	captcha := base64Captcha.NewCaptcha(driver, store)
	id, b64s, _, err := captcha.Generate()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "生成验证码失败"})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"captcha_id":    id,
			"captcha_image": b64s,
		},
	})
}

func GetAuthConfig(c *gin.Context) {
	var captchaConfig models.SysConfig
	var needCaptcha bool = true
	if err := database.DB.Where("`key` = ?", "login_captcha").First(&captchaConfig).Error; err == nil {
		if captchaConfig.Value == "0" || captchaConfig.Value == "false" {
			needCaptcha = false
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"login_captcha": needCaptcha,
		},
	})
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// Check if captcha is enabled
	var captchaConfig models.SysConfig
	needCaptcha := true
	if err := database.DB.Where("`key` = ?", "login_captcha").First(&captchaConfig).Error; err == nil {
		if captchaConfig.Value == "0" || captchaConfig.Value == "false" {
			needCaptcha = false
		}
	}

	if needCaptcha {
		if req.CaptchaID == "" || req.CaptchaCode == "" {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码不能为空"})
			return
		}
		if !store.Verify(req.CaptchaID, req.CaptchaCode, true) {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码错误"})
			return
		}
	}

	var user models.User
	if err := database.DB.Where("username = ?", req.Username).First(&user).Error; err != nil {
		logger.Info("用户登录失败: 用户名或密码错误 - 用户名: %s", req.Username)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名或密码错误"})
		return
	}

	if user.Status != 1 {
		logger.Info("用户登录失败: 账户已被禁用 - 用户名: %s", req.Username)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "账户已被禁用"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		logger.Info("用户登录失败: 用户名或密码错误 - 用户名: %s", req.Username)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名或密码错误"})
		return
	}

	// 检查TOTP二步验证
	if user.TOTPOpen && user.TOTPSecret != "" {
		if req.TOTPCode == "" {
			c.JSON(http.StatusOK, gin.H{"code": 2, "msg": "需要进行二步验证"})
			return
		}
		if !utils.VerifyTOTPCode(user.TOTPSecret, req.TOTPCode) {
			logger.Info("用户登录失败: TOTP验证码错误 - 用户名: %s", req.Username)
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码错误"})
			return
		}
	}

	token, err := middleware.GenerateToken(strconv.FormatUint(uint64(user.ID), 10), user.Username, user.Level)
	if err != nil {
		logger.Error("生成用户Token失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "生成Token失败"})
		return
	}

	now := time.Now()
	database.DB.Model(&user).Update("last_time", &now)

	logger.Info("用户登录成功: %s", req.Username)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "登录成功",
		"data": gin.H{
			"token": token,
			"user": gin.H{
				"id":       user.ID,
				"username": user.Username,
				"level":    user.Level,
			},
		},
	})
}

func Logout(c *gin.Context) {
	username := c.GetString("username")
	logger.Info("用户退出登录: %s", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "退出成功"})
}

func GetUserInfo(c *gin.Context) {
	userID := c.GetUint("user_id")

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"id":        user.ID,
			"username":  user.Username,
			"level":     user.Level,
			"is_api":    user.IsAPI,
			"totp_open": user.TOTPOpen,
			"reg_time":  user.RegTime,
			"last_time": user.LastTime,
		},
	})
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=6"`
}

func ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	userID := c.GetUint("user_id")
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.OldPassword)); err != nil {
		logger.Info("用户修改密码失败: 原密码错误 - 用户ID: %d", userID)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "原密码错误"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("用户修改密码失败: 密码加密失败 - 用户ID: %d, 错误: %v", userID, err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "密码加密失败"})
		return
	}

	database.DB.Model(&user).Update("password", string(hashedPassword))

	logger.Info("用户修改密码成功: %d", userID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "密码修改成功"})
}

func Install(c *gin.Context) {
	var count int64
	database.DB.Model(&models.User{}).Count(&count)
	if count > 0 {
		logger.Warn("系统安装失败: 系统已安装")
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "系统已安装"})
		return
	}

	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		logger.Error("系统安装失败: 密码加密失败 - 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "密码加密失败"})
		return
	}

	user := models.User{
		Username: req.Username,
		Password: string(hashedPassword),
		Level:    1,
		Status:   1,
		RegTime:  time.Now(),
	}

	if err := database.DB.Create(&user).Error; err != nil {
		logger.Error("系统安装失败: 创建用户失败 - 错误: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "创建用户失败"})
		return
	}

	logger.Info("系统安装成功: %s", req.Username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "安装成功"})
}

func InstallStatus(c *gin.Context) {
	var count int64
	database.DB.Model(&models.User{}).Count(&count)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"installed": count > 0,
		},
	})
}

// GetTOTPStatus 获取TOTP状态
func GetTOTPStatus(c *gin.Context) {
	userID := c.GetUint("user_id")

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"totp_open": user.TOTPOpen,
		},
	})
}

// EnableTOTP 启用TOTP
func EnableTOTP(c *gin.Context) {
	userID := c.GetUint("user_id")

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	if user.TOTPOpen {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "TOTP已启用"})
		return
	}

	// 生成新密钥
	secret, err := utils.GenerateTOTPSecret()
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "生成密钥失败"})
		return
	}

	// 生成二维码URI
	issuer := "DNSPlane"
	var siteConfig models.SysConfig
	if err := database.DB.Where("`key` = ?", "site_name").First(&siteConfig).Error; err == nil && siteConfig.Value != "" {
		issuer = siteConfig.Value
	}

	uri := utils.GenerateTOTPURI(utils.TOTPConfig{
		Secret:  secret,
		Issuer:  issuer,
		Account: user.Username,
	})

	// 保存密钥（但不启用，等待验证后再启用）
	database.DB.Model(&user).Update("totp_secret", secret)

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"secret": secret,
			"uri":    uri,
		},
	})
}

// VerifyAndEnableTOTP 验证并启用TOTP
func VerifyAndEnableTOTP(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req struct {
		Code string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	if user.TOTPSecret == "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "请先生成TOTP密钥"})
		return
	}

	// 验证验证码
	if !utils.VerifyTOTPCode(user.TOTPSecret, req.Code) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码错误"})
		return
	}

	// 启用TOTP
	database.DB.Model(&user).Update("totp_open", true)

	logger.Info("用户%d启用了TOTP二步验证", userID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TOTP启用成功"})
}

// DisableTOTP 禁用TOTP
func DisableTOTP(c *gin.Context) {
	userID := c.GetUint("user_id")

	var req struct {
		Password string `json:"password" binding:"required"`
		Code     string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	// 验证密码
	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "密码错误"})
		return
	}

	// 验证TOTP验证码
	if user.TOTPOpen && user.TOTPSecret != "" {
		if !utils.VerifyTOTPCode(user.TOTPSecret, req.Code) {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码错误"})
			return
		}
	}

	// 禁用TOTP
	database.DB.Model(&user).Updates(map[string]interface{}{
		"totp_open":   false,
		"totp_secret": "",
	})

	logger.Info("用户%d禁用了TOTP二步验证", userID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "TOTP已禁用"})
}

// generateResetToken 生成重置Token
func generateResetToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// getEmailConfig 获取邮件配置
func getEmailConfig() (*notify.EmailConfig, error) {
	var configs []models.SysConfig
	database.DB.Where("`key` IN ?", []string{
		"mail_host", "mail_port", "mail_username", "mail_password", "mail_from", "mail_tls",
	}).Find(&configs)

	configMap := make(map[string]string)
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	if configMap["mail_host"] == "" {
		return nil, nil
	}

	port := 25
	if p, ok := configMap["mail_port"]; ok && p != "" {
		var portInt int
		json.Unmarshal([]byte(p), &portInt)
		if portInt > 0 {
			port = portInt
		}
	}

	useTLS := configMap["mail_tls"] == "1" || configMap["mail_tls"] == "true"

	return &notify.EmailConfig{
		Host:     configMap["mail_host"],
		Port:     port,
		Username: configMap["mail_username"],
		Password: configMap["mail_password"],
		From:     configMap["mail_from"],
		UseTLS:   useTLS,
	}, nil
}

// getSiteURL 获取站点URL
func getSiteURL() string {
	var config models.SysConfig
	if err := database.DB.Where("`key` = ?", "site_url").First(&config).Error; err == nil && config.Value != "" {
		return config.Value
	}
	return "http://localhost:8080"
}

// ForgotPassword 发送密码重置邮件（公开API）
func ForgotPassword(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "请输入有效的邮箱地址"})
		return
	}

	// 查找用户
	var user models.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		// 为了安全，即使用户不存在也返回成功
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	// 检查邮件配置
	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "邮件服务未配置，请联系管理员"})
		return
	}

	// 生成重置Token
	token := generateResetToken()
	expireTime := time.Now().Add(30 * time.Minute)

	// 保存Token
	database.DB.Model(&user).Updates(map[string]interface{}{
		"reset_token":  token,
		"reset_type":   "password",
		"reset_expire": expireTime,
	})

	// 发送邮件
	siteURL := getSiteURL()
	resetLink := notify.BuildResetLink(siteURL, "password", token)
	subject, body := notify.RenderPasswordResetEmail(user.Username, resetLink, "30")

	emailConfig.To = req.Email
	notifier := notify.NewEmailNotifier(*emailConfig)
	if err := notifier.Send(context.Background(), subject, body); err != nil {
		logger.Error("发送密码重置邮件失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "邮件发送失败，请稍后重试"})
		return
	}

	logger.Info("已发送密码重置邮件: %s", req.Email)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "重置邮件已发送，请查收"})
}

// ResetPassword 通过Token重置密码（公开API）
func ResetPassword(c *gin.Context) {
	var req struct {
		Token    string `json:"token" binding:"required"`
		Password string `json:"password" binding:"required,min=6"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// 查找用户
	var user models.User
	if err := database.DB.Where("reset_token = ? AND reset_type = ?", req.Token, "password").First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "无效的重置链接"})
		return
	}

	// 检查Token是否过期
	if user.ResetExpire == nil || user.ResetExpire.Before(time.Now()) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "重置链接已过期，请重新申请"})
		return
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "密码加密失败"})
		return
	}

	// 更新密码并清除Token
	database.DB.Model(&user).Updates(map[string]interface{}{
		"password":     string(hashedPassword),
		"reset_token":  "",
		"reset_type":   "",
		"reset_expire": nil,
	})

	logger.Info("用户%s通过邮件重置了密码", user.Username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "密码重置成功"})
}

// ForgotTOTP 发送TOTP重置邮件（公开API）
func ForgotTOTP(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "请输入有效的邮箱地址"})
		return
	}

	// 查找用户
	var user models.User
	if err := database.DB.Where("email = ?", req.Email).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	// 检查用户是否启用了TOTP
	if !user.TOTPOpen {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	// 检查邮件配置
	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "邮件服务未配置，请联系管理员"})
		return
	}

	// 生成重置Token
	token := generateResetToken()
	expireTime := time.Now().Add(30 * time.Minute)

	// 保存Token
	database.DB.Model(&user).Updates(map[string]interface{}{
		"reset_token":  token,
		"reset_type":   "totp",
		"reset_expire": expireTime,
	})

	// 发送邮件
	siteURL := getSiteURL()
	resetLink := notify.BuildResetLink(siteURL, "totp", token)
	subject, body := notify.RenderTOTPResetEmail(user.Username, resetLink, "30")

	emailConfig.To = req.Email
	notifier := notify.NewEmailNotifier(*emailConfig)
	if err := notifier.Send(context.Background(), subject, body); err != nil {
		logger.Error("发送TOTP重置邮件失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "邮件发送失败，请稍后重试"})
		return
	}

	logger.Info("已发送TOTP重置邮件: %s", req.Email)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "重置邮件已发送，请查收"})
}

// ResetTOTP 通过Token重置TOTP（公开API）
func ResetTOTP(c *gin.Context) {
	var req struct {
		Token string `json:"token" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// 查找用户
	var user models.User
	if err := database.DB.Where("reset_token = ? AND reset_type = ?", req.Token, "totp").First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "无效的重置链接"})
		return
	}

	// 检查Token是否过期
	if user.ResetExpire == nil || user.ResetExpire.Before(time.Now()) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "重置链接已过期，请重新申请"})
		return
	}

	// 关闭TOTP并清除Token
	database.DB.Model(&user).Updates(map[string]interface{}{
		"totp_open":    false,
		"totp_secret":  "",
		"reset_token":  "",
		"reset_type":   "",
		"reset_expire": nil,
	})

	logger.Info("用户%s通过邮件重置了TOTP", user.Username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "二步验证已关闭，请重新登录"})
}

// AdminSendResetEmail 管理员发送重置邮件
func AdminSendResetEmail(c *gin.Context) {
	userID, _ := c.Params.Get("id")

	var req struct {
		Type string `json:"type" binding:"required,oneof=password totp"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// 查找目标用户
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	// 检查用户邮箱
	if user.Email == "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该用户未绑定邮箱"})
		return
	}

	// 检查邮件配置
	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "邮件服务未配置"})
		return
	}

	// 生成重置Token
	token := generateResetToken()
	expireTime := time.Now().Add(60 * time.Minute) // 管理员发送的链接有效期更长

	// 保存Token
	database.DB.Model(&user).Updates(map[string]interface{}{
		"reset_token":  token,
		"reset_type":   req.Type,
		"reset_expire": expireTime,
	})

	// 发送邮件
	siteURL := getSiteURL()
	resetLink := notify.BuildResetLink(siteURL, req.Type, token)
	subject, body := notify.RenderAdminResetEmail(user.Username, req.Type, resetLink, "60")

	emailConfig.To = user.Email
	notifier := notify.NewEmailNotifier(*emailConfig)
	if err := notifier.Send(context.Background(), subject, body); err != nil {
		logger.Error("管理员发送重置邮件失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "邮件发送失败"})
		return
	}

	adminUsername := c.GetString("username")
	logger.Info("管理员%s为用户%s发送了%s重置邮件", adminUsername, user.Username, req.Type)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "重置邮件已发送"})
}

// AdminResetTOTP 管理员直接重置用户TOTP
func AdminResetTOTP(c *gin.Context) {
	userID, _ := c.Params.Get("id")

	// 查找目标用户
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	if !user.TOTPOpen {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该用户未启用二步验证"})
		return
	}

	// 直接关闭TOTP
	database.DB.Model(&user).Updates(map[string]interface{}{
		"totp_open":   false,
		"totp_secret": "",
	})

	adminUsername := c.GetString("username")
	logger.Info("管理员%s重置了用户%s的TOTP", adminUsername, user.Username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "已重置用户的二步验证"})
}
