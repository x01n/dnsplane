package handler

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"main/internal/api/middleware"
	"main/internal/cache"
	"main/internal/database"
	"main/internal/dbcache"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/utils"
	"main/internal/verify"
	"math/big"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/mojocn/base64Captcha"
	"golang.org/x/crypto/bcrypt"
)

var store = base64Captcha.DefaultMemStore

// bcryptCost 密码哈希成本因子（安全审计 M-6：DefaultCost=10 → 12，匹配 OWASP 2024 建议）。
// 每提升 1 档，哈希耗时约翻倍；12 档在普通 CPU 上约 250ms，用户登录无感。
const bcryptCost = 12

// loginDelayJitter 登录失败分支的随机抖动延迟（安全审计 L-2 缓解）。
// 让"密码错误"、"TOTP 错"、"账户禁用"、"未找到用户"等路径耗时趋同，
// 降低攻击者通过响应时序推断账号是否存在 / 是否启用 TOTP 的可行性。
func loginDelayJitter() {
	n, err := rand.Int(rand.Reader, big.NewInt(100))
	if err != nil {
		n = big.NewInt(50)
	}
	time.Sleep(time.Duration(50+n.Int64()) * time.Millisecond)
}

// markTOTPCounterUsed 将一个 TOTP 时间窗口标记为已使用（安全审计 M-1）。
// 返回 true 表示成功占位（首次使用）；返回 false 表示该 counter 已被使用过（重放）。
//
// Key 用 sha256(secret) 的前 16 字符作为稳定指纹，不暴露原始 secret；
// TTL 设为 90s 覆盖 ±30s 验证窗口。
//
// 在缓存不可用时返回 true 作为服务降级（与 JTI 类似策略）。
func markTOTPCounterUsed(secret string, counter int64) bool {
	if cache.C == nil {
		return true
	}
	fp := sha256.Sum256([]byte(secret))
	key := "totp_used:" + hex.EncodeToString(fp[:8]) + ":" + strconv.FormatInt(counter, 10)
	if _, ok := cache.C.Get(key); ok {
		return false
	}
	if err := cache.C.Set(key, "1", 90*time.Second); err != nil {
		// 写缓存失败视为放行，避免 cache 抖动导致合法用户被拒
		return true
	}
	return true
}

// 登录暴力破解防护（安全审计 H-5）。
// 以 IP+用户名 双维度计数，窗口 15 分钟内失败次数超过阈值即拒绝。
// 成功登录后清除该 IP+用户名 的计数器。
const (
	loginFailWindow    = 15 * time.Minute
	loginFailThreshold = 5
)

// bucketLoginFailIP 将 IP 归一到 /24 (IPv4) 或 /64 (IPv6) 前缀。
//
// 安全审计 M-7：内存缓存模式下 login_fail 键原以原始 IP+用户名 为键，
// 攻击者用海量随机 IP 可不受控地撑爆 map。按子网前缀分桶后：
//   - IPv4 可能的不同桶 ≤ 2^24 = 16M（实际使用中远远小于）
//   - IPv6 按 /64 前缀归档，与单个终端 /64 段合并
// 对合法用户影响极小：同一子网下不同用户名仍有独立计数。
func bucketLoginFailIP(raw string) string {
	ip := net.ParseIP(strings.TrimSpace(raw))
	if ip == nil {
		return raw
	}
	if v4 := ip.To4(); v4 != nil {
		return fmt.Sprintf("%d.%d.%d.0/24", v4[0], v4[1], v4[2])
	}
	mask := net.CIDRMask(64, 128)
	return ip.Mask(mask).String() + "/64"
}

func loginFailKey(ip, username string) string {
	return "login_fail:" + bucketLoginFailIP(ip) + ":" + strings.ToLower(strings.TrimSpace(username))
}

// loginBlocked 返回 true 表示当前 IP+username 组合已被临时锁定。
//
// 读路径使用 cache.C.Get（纯文本），与 Incr 使用的计数格式兼容。
// 原使用 GetJSON 在 cache 类型切换后会反序列化失败。
func loginBlocked(ip, username string) bool {
	if cache.C == nil {
		return false
	}
	v, ok := cache.C.Get(loginFailKey(ip, username))
	if !ok {
		return false
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return false
	}
	return n >= loginFailThreshold
}

// noteLoginFailure 原子累加一次失败计数。
//
// 安全审计 H-2：原实现 GetJSON + 自增 + SetJSON 三步非原子，
// 并发失败请求下存在 lost-update，实际计数远低于阈值，锁定形同虚设。
// 改为 cache.C.Incr（Redis INCR 天然原子；memoryCache 实现走 mu.Lock），
// 保证单次自增不被覆盖。
func noteLoginFailure(ip, username string) {
	if cache.C == nil {
		return
	}
	_, _ = cache.C.Incr(loginFailKey(ip, username), loginFailWindow)
}

// clearLoginFailure 登录成功或验证码通过后清零计数。
func clearLoginFailure(ip, username string) {
	if cache.C == nil {
		return
	}
	cache.C.Delete(loginFailKey(ip, username))
}

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

	registerEnabled := GetSysConfigValue("register_enabled") == "true"
	passwordRegister := GetSysConfigValue("auth_password_register") == "true" || GetSysConfigValue("auth_password_register") == "1"
	magicLinkLogin := GetSysConfigValue("auth_magic_link_login") == "true" || GetSysConfigValue("auth_magic_link_login") == "1"
	turnstileSite := strings.TrimSpace(GetSysConfigValue("turnstile_site_key"))

	data := gin.H{
		"login_captcha":             needCaptcha,
		"captcha_enabled":           needCaptcha,
		"register_enabled":          registerEnabled,
		"password_register_enabled": passwordRegister,
		"magic_link_login_enabled":  magicLinkLogin,
	}
	if turnstileSite != "" {
		data["turnstile_site_key"] = turnstileSite
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": data,
	})
}

func Login(c *gin.Context) {
	var req LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	ip := c.ClientIP()

	// 暴力破解锁定检查（安全审计 H-5），失败 N 次后 TTL 内强制拒绝
	if loginBlocked(ip, req.Username) {
		logger.Warn("用户登录被锁定: IP=%s 用户名=%s", ip, req.Username)
		loginDelayJitter()
		c.JSON(http.StatusTooManyRequests, gin.H{"code": -1, "msg": "登录失败次数过多，请 15 分钟后再试"})
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
		noteLoginFailure(ip, req.Username)
		logger.Info("用户登录失败: 用户名或密码错误 - 用户名: %s", req.Username)
		loginDelayJitter()
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名或密码错误"})
		return
	}

	if user.Status != 1 {
		// 禁用账户路径也计入失败计数（安全审计 M-5），
		// 保持所有失败分支的锁定行为一致，避免通过失败计数覆盖缺失推断账户状态
		noteLoginFailure(ip, req.Username)
		logger.Info("用户登录失败: 账户已被禁用 - 用户名: %s", req.Username)
		loginDelayJitter()
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "账户已被禁用"})
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.Password), []byte(req.Password)); err != nil {
		noteLoginFailure(ip, req.Username)
		logger.Info("用户登录失败: 用户名或密码错误 - 用户名: %s", req.Username)
		loginDelayJitter()
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名或密码错误"})
		return
	}

	// 检查TOTP二步验证
	if user.TOTPOpen && user.TOTPSecret != "" {
		if req.TOTPCode == "" {
			c.JSON(http.StatusOK, gin.H{"code": 2, "msg": "需要进行二步验证"})
			return
		}
		// 安全审计 M-1：校验码 + 单次计数器防重放。
		// 同一 counter 已在 90s 内被使用过，直接拒绝重放。
		ok, counter := utils.VerifyTOTPCodeWithCounter(user.TOTPSecret, req.TOTPCode)
		if !ok {
			noteLoginFailure(ip, req.Username)
			logger.Info("用户登录失败: TOTP验证码错误 - 用户名: %s", req.Username)
			loginDelayJitter()
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码错误"})
			return
		}
		if !markTOTPCounterUsed(user.TOTPSecret, counter) {
			noteLoginFailure(ip, req.Username)
			logger.Warn("用户登录被拦截: TOTP 计数器 %d 已被使用，疑似重放 - 用户名: %s", counter, req.Username)
			loginDelayJitter()
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "验证码已使用，请等待下一个周期"})
			return
		}
	}

	// 登录成功后清除该 IP+用户名 的失败计数
	clearLoginFailure(ip, req.Username)

	tokenPair, err := middleware.GenerateTokenPair(strconv.FormatUint(uint64(user.ID), 10), user.Username, user.Level)
	if err != nil {
		logger.Error("生成用户Token失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "生成Token失败"})
		return
	}

	if err := middleware.SetAuthCookies(c, tokenPair); err != nil {
		logger.Error("设置登录Cookie失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "登录失败"})
		return
	}

	if rtClaims, _ := middleware.ParseToken(tokenPair.RefreshToken); rtClaims != nil {
		middleware.StoreRefreshJTI(strconv.FormatUint(uint64(user.ID), 10), rtClaims.ID)
	}

	now := time.Now()
	database.DB.Model(&user).Update("last_time", &now)

	logger.Info("用户登录成功: %s", req.Username)
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "登录成功",
		"data": gin.H{
			"token":         tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"expires_in":    tokenPair.ExpiresIn,
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
	middleware.ClearAuthCookies(c)
	middleware.SuccessMsg(c, "退出成功")
}

/*
 * RefreshToken 用 refresh token 换取新的 access/refresh 对（无需 Auth 中间件）
 * 凭证来源：JSON body.refresh_token，或 HttpOnly Cookie _rt（路径 /api/auth/refresh）
 */
func RefreshToken(c *gin.Context) {
	var body struct {
		RefreshToken string `json:"refresh_token"`
	}
	_ = c.ShouldBindJSON(&body)

	rt := strings.TrimSpace(body.RefreshToken)
	if rt == "" {
		if ck, err := c.Cookie("_rt"); err == nil && ck != "" {
			if dec, ok := middleware.DecryptCookie(ck); ok {
				rt = dec
			}
		}
	}
	if rt == "" {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "缺少 refresh token"})
		return
	}

	pair, err := middleware.RefreshAccessToken(rt)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "刷新失败或已失效"})
		return
	}
	if err := middleware.SetAuthCookies(c, pair); err != nil {
		logger.Error("刷新后设置 Cookie 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "刷新失败"})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "ok",
		"data": gin.H{
			"token":         pair.AccessToken,
			"refresh_token": pair.RefreshToken,
			"expires_in":    pair.ExpiresIn,
		},
	})
}

func GetUserInfo(c *gin.Context) {
	userID := middleware.AuthUserID(c)

	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		middleware.ErrorResponse(c, "用户不存在")
		return
	}

	middleware.SuccessResponse(c, gin.H{
		"id":        user.ID,
		"username":  user.Username,
		"email":     user.Email,
		"level":     user.Level,
		"is_api":    user.IsAPI,
		"totp_open": user.TOTPOpen,
		"reg_time":  user.RegTime,
		"last_time": user.LastTime,
	})
}

type ChangePasswordRequest struct {
	OldPassword string `json:"old_password" binding:"required"`
	NewPassword string `json:"new_password" binding:"required,min=8"`
}

func ChangePassword(c *gin.Context) {
	var req ChangePasswordRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	userID := middleware.AuthUserID(c)
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

	// 强密码复杂度校验（安全审计 H-4：min=8 + 大小写 + 数字）
	if msg := utils.ValidatePasswordStrength(req.NewPassword); msg != "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msg})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), bcryptCost)
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
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// 强密码复杂度校验（安全审计 H-4）
	if msg := utils.ValidatePasswordStrength(req.Password); msg != "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msg})
		return
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
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
	userID := middleware.AuthUserID(c)

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
	userID := middleware.AuthUserID(c)

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
	userID := middleware.AuthUserID(c)

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
	userID := middleware.AuthUserID(c)

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

// generateResetToken 生成发给用户的原始重置 Token（经邮件发送）。
func generateResetToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

// hashResetToken 将用户可见的重置 Token 映射为落库 SHA-256 指纹。
//
// 设计动机（对应安全审计 H-6）：
//   - GORM 的 Updates(map[...]interface{}) 不触发 BeforeSave 钩子，
//     导致 User.ResetToken 的 AES-GCM 加密被绕过，明文落库；
//   - 改为落库时存 sha256(token)，明文仅在邮件中存在；
//     数据库被读取也只能拿到不可逆摘要，攻击者无法用于后续 /reset 接口；
//   - 查询时对用户提交的 token 同样 SHA-256 后比较，无需可逆解密，天然避开 GORM 钩子坑。
func hashResetToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
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

// 安全审计 R-7：邮件发送 goroutine 限流。
//
// ForgotPassword/ForgotTOTP/RequestMagicLink/SendAuthCode 是公开接口，
// 即便 verify.AllowPublicEmailRequest 限了 IP/邮箱粒度，攻击者用大量邮箱+多 IP
// 仍可在短时间内堆积大量 SMTP goroutine（每个 dial 超时可达 12s）。
// 信号量将同时进行的邮件发送数限制在 32，超额请求阻塞排队，邮件库内存/FD 占用可控。
var mailSendSem = make(chan struct{}, 32)

// enqueueNotifyMail 异步发信，HTTP 立即返回；失败仅记日志（与公开接口模糊成功口径一致）
func enqueueNotifyMail(task string, cfg notify.EmailConfig, subject, body string) {
	ec := cfg
	utils.SafeGoWithName(task, func() {
		mailSendSem <- struct{}{}        // 信号量获取（达上限即阻塞）
		defer func() { <-mailSendSem }() // 释放
		n := notify.NewEmailNotifier(ec)
		if err := n.Send(context.Background(), subject, body); err != nil {
			logger.Error("%s: %v", task, err)
		}
	})
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
	email := strings.TrimSpace(req.Email)
	if !verify.AllowPublicEmailRequest(c.ClientIP(), email, "forgot_pw", 12, 5) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	var user models.User
	if err := database.DB.Where("email = ?", email).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	token := generateResetToken()
	expireTime := time.Now().Add(30 * time.Minute)
	// 落库指纹而非明文（详见 hashResetToken 注释）
	database.DB.Model(&user).Updates(map[string]interface{}{
		"reset_token":  hashResetToken(token),
		"reset_type":   "password",
		"reset_expire": expireTime,
	})

	siteURL := getSiteURL()
	resetLink := notify.BuildResetLink(siteURL, "password", token)
	subject, body := notify.RenderPasswordResetEmail(user.Username, resetLink, "30")
	ec := *emailConfig
	ec.To = email
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
	enqueueNotifyMail("mail_forgot_password", ec, subject, body)
}

// ResetPassword 通过Token重置密码（公开API）
func ResetPassword(c *gin.Context) {
	var req struct {
		Token    string `json:"token" binding:"required"`
		Password string `json:"password" binding:"required,min=8"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}

	// 查找用户（用 token 的 SHA-256 指纹匹配，与落库值保持一致）
	var user models.User
	if err := database.DB.Where("reset_token = ? AND reset_type = ?", hashResetToken(req.Token), "password").First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "无效的重置链接"})
		return
	}

	// 检查Token是否过期
	if user.ResetExpire == nil || user.ResetExpire.Before(time.Now()) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "重置链接已过期，请重新申请"})
		return
	}

	// 强密码复杂度校验（安全审计 H-4）
	if msg := utils.ValidatePasswordStrength(req.Password); msg != "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msg})
		return
	}

	// 加密新密码
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
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
	email := strings.TrimSpace(req.Email)
	if !verify.AllowPublicEmailRequest(c.ClientIP(), email, "forgot_totp", 12, 5) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	var user models.User
	if err := database.DB.Where("email = ?", email).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	if !user.TOTPOpen {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
		return
	}

	token := generateResetToken()
	expireTime := time.Now().Add(30 * time.Minute)
	database.DB.Model(&user).Updates(map[string]interface{}{
		"reset_token":  hashResetToken(token),
		"reset_type":   "totp",
		"reset_expire": expireTime,
	})

	siteURL := getSiteURL()
	resetLink := notify.BuildResetLink(siteURL, "totp", token)
	subject, body := notify.RenderTOTPResetEmail(user.Username, resetLink, "30")
	ec := *emailConfig
	ec.To = email
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "如果该邮箱已注册，您将收到重置邮件"})
	enqueueNotifyMail("mail_forgot_totp", ec, subject, body)
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
	if err := database.DB.Where("reset_token = ? AND reset_type = ?", hashResetToken(req.Token), "totp").First(&user).Error; err != nil {
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
	if !requireAdmin(c) {
		return
	}
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

	// 保存 Token 的 SHA-256 指纹（详见 hashResetToken 注释）
	database.DB.Model(&user).Updates(map[string]interface{}{
		"reset_token":  hashResetToken(token),
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
	if !requireAdmin(c) {
		return
	}
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

// authRegisterPolicy 与 GetAuthConfig 一致：是否开放注册、是否开放密码自助注册
func authRegisterPolicy() (registerEnabled, passwordRegister bool) {
	registerEnabled = GetSysConfigValue("register_enabled") == "true"
	passwordRegister = GetSysConfigValue("auth_password_register") == "true" || GetSysConfigValue("auth_password_register") == "1"
	return
}

const msgEmailNotInWhitelist = "邮箱不在白名单"

// registerEmailPassesWhitelist 与系统设置「邮箱域名白名单」一致：未启用时恒为 true；启用时邮箱须匹配至少一条规则（整邮相等，或域名/子域与规则匹配，规则可带 @ 前缀）。
func registerEmailPassesWhitelist(emailNorm string) bool {
	if GetSysConfigValue("auth_email_whitelist_enabled") != "true" {
		return true
	}
	emailNorm = strings.TrimSpace(strings.ToLower(emailNorm))
	if emailNorm == "" {
		return false
	}
	at := strings.LastIndex(emailNorm, "@")
	if at < 0 || at == len(emailNorm)-1 {
		return false
	}
	domain := emailNorm[at+1:]
	raw := strings.TrimSpace(GetSysConfigValue("auth_email_whitelist"))
	if raw == "" {
		return false
	}
	for _, line := range strings.Split(raw, "\n") {
		rule := strings.TrimSpace(strings.TrimRight(line, "\r"))
		if rule == "" {
			continue
		}
		rule = strings.ToLower(rule)
		if strings.Contains(rule, "@") {
			if emailNorm == rule {
				return true
			}
			continue
		}
		if strings.HasPrefix(rule, "@") {
			rule = rule[1:]
		}
		if rule == "" {
			continue
		}
		if domain == rule || strings.HasSuffix(domain, "."+rule) {
			return true
		}
	}
	return false
}

func requireSystemInstalled(c *gin.Context) bool {
	var count int64
	database.DB.Model(&models.User{}).Count(&count)
	if count == 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "系统未完成安装"})
		return false
	}
	return true
}

// msgMagicLinkIfEligible 无密码登录请求的模糊成功提示（用户不存在 / 未开邮件等统一口径）
const msgMagicLinkIfEligible = "若该邮箱已注册，您将收到登录链接"

// RequestMagicLink POST /api/auth/magic-link 发送一次性登录链接（已启用 TOTP 的账号在打开链接后需再验动态口令）
func RequestMagicLink(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "请输入有效的邮箱地址"})
		return
	}
	if !requireSystemInstalled(c) {
		return
	}
	magicOn := GetSysConfigValue("auth_magic_link_login") == "true" || GetSysConfigValue("auth_magic_link_login") == "1"
	if !magicOn {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "未启用无密码登录"})
		return
	}
	emailTrim := strings.TrimSpace(req.Email)
	if !verify.AllowPublicEmailRequest(c.ClientIP(), emailTrim, "magic_link", 15, 5) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msgMagicLinkIfEligible})
		return
	}
	var user models.User
	if err := database.DB.Where("email = ?", emailTrim).First(&user).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msgMagicLinkIfEligible})
		return
	}
	if user.Status != 1 {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msgMagicLinkIfEligible})
		return
	}
	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msgMagicLinkIfEligible})
		return
	}
	token := generateResetToken()
	if err := cache.C.SetJSON(quickLoginPrefix+token, &QuickLoginToken{
		Token:    token,
		UserID:   strconv.FormatUint(uint64(user.ID), 10),
		DomainID: "",
	}, 15*time.Minute); err != nil {
		logger.Error("缓存魔法登录令牌失败: %v", err)
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msgMagicLinkIfEligible})
		return
	}
	siteURL := getSiteURL()
	magicURL := notify.BuildMagicLoginLink(siteURL, token)
	subject, body := notify.RenderMagicLoginEmail(user.Username, magicURL, "15")
	ec := *emailConfig
	ec.To = user.Email
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": msgMagicLinkIfEligible})
	enqueueNotifyMail("mail_magic_link", ec, subject, body)
}

// MagicLinkVerifyTotp POST /api/auth/magic-link/totp 提交魔法登录第二步动态口令（与 QuickLogin 拆开的 TOTP 校验）
func MagicLinkVerifyTotp(c *gin.Context) {
	var req struct {
		PreauthToken string `json:"preauth_token" binding:"required"`
		TOTPCode     string `json:"totp_code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}
	if err := verify.CheckIPLimit(c.ClientIP(), 60); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	key := magicTotpPreauthPrefix + strings.TrimSpace(req.PreauthToken)
	var pre MagicTotpPreauth
	if !cache.C.GetJSON(key, &pre) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "会话已失效，请重新点击邮件中的登录链接"})
		return
	}
	var user models.User
	if err := database.DB.First(&user, pre.UserID).Error; err != nil {
		cache.C.Delete(key)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}
	if user.Status != 1 {
		cache.C.Delete(key)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "账户已被禁用"})
		return
	}
	if !user.TOTPOpen || user.TOTPSecret == "" {
		cache.C.Delete(key)
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "当前账号无需二步验证，请重新使用登录链接"})
		return
	}
	if !utils.VerifyTOTPCode(user.TOTPSecret, req.TOTPCode) {
		logger.Info("魔法登录 TOTP 校验失败: uid=%s ip=%s", pre.UserID, c.ClientIP())
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "动态口令错误"})
		return
	}
	cache.C.Delete(key)

	tokenPair, err := middleware.GenerateTokenPair(strconv.FormatUint(uint64(user.ID), 10), user.Username, user.Level)
	if err != nil {
		logger.Error("魔法登录 TOTP 通过后签发 Token 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "登录失败"})
		return
	}
	if err := middleware.SetAuthCookies(c, tokenPair); err != nil {
		logger.Error("魔法登录 TOTP 通过后设置 Cookie 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "登录失败"})
		return
	}
	if rtClaims, _ := middleware.ParseToken(tokenPair.RefreshToken); rtClaims != nil {
		middleware.StoreRefreshJTI(strconv.FormatUint(uint64(user.ID), 10), rtClaims.ID)
	}
	now := time.Now()
	database.DB.Model(&user).Update("last_time", &now)

	redirectTo := "/dashboard/"
	if pre.DomainID != "" && pre.DomainID != "0" {
		redirectTo = "/dashboard/domains/" + pre.DomainID
	}
	logger.Info("魔法登录完成(含TOTP): user=%s ip=%s", user.Username, c.ClientIP())
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"msg":  "登录成功",
		"data": gin.H{
			"token":         tokenPair.AccessToken,
			"refresh_token": tokenPair.RefreshToken,
			"expires_in":    tokenPair.ExpiresIn,
			"redirect":      redirectTo,
			"user": gin.H{
				"id":       user.ID,
				"username": user.Username,
				"level":    user.Level,
			},
		},
	})
}

// SendAuthCode POST /api/auth/send-code 公开：仅 scene=register 时发送邮箱验证码（绑定邮箱请用已登录接口）。
func SendAuthCode(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
		Scene string `json:"scene"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "请输入有效的邮箱地址"})
		return
	}
	scene := strings.ToLower(strings.TrimSpace(req.Scene))
	if scene == "" {
		scene = verify.SceneRegister
	}
	if scene != verify.SceneRegister {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "绑定邮箱验证码需登录后在个人中心发送"})
		return
	}
	if !requireSystemInstalled(c) {
		return
	}
	regOn, pwdReg := authRegisterPolicy()
	if !regOn {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "管理员已关闭注册"})
		return
	}
	if !pwdReg {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "管理员未开放密码自助注册"})
		return
	}
	emailNorm := strings.ToLower(strings.TrimSpace(req.Email))
	if !registerEmailPassesWhitelist(emailNorm) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msgEmailNotInWhitelist})
		return
	}
	if !verify.AllowPublicEmailRequest(c.ClientIP(), emailNorm, "reg_code", 24, 8) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "若符合注册条件，验证码将发送至邮箱"})
		return
	}
	var taken int64
	database.DB.Model(&models.User{}).Where("LOWER(TRIM(email)) = ? AND email != ''", emailNorm).Count(&taken)
	if taken > 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该邮箱已被注册"})
		return
	}
	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "若邮件服务已配置，验证码将发送至邮箱"})
		return
	}
	code, err := verify.Generate(emailNorm, verify.SceneRegister)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	subject, body := notify.RenderVerificationCodeEmail(code, verify.SceneRegister, "5")
	ec := *emailConfig
	ec.To = strings.TrimSpace(req.Email)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "验证码已发送"})
	enqueueNotifyMail("mail_reg_code", ec, subject, body)
}

// Register POST /api/auth/register 邮箱验证码 + 密码自助注册
func Register(c *gin.Context) {
	var req struct {
		Username string `json:"username" binding:"required"`
		Password string `json:"password" binding:"required,min=8"`
		Email    string `json:"email" binding:"required,email"`
		Code     string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}
	if !requireSystemInstalled(c) {
		return
	}
	regOn, pwdReg := authRegisterPolicy()
	if !regOn {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "管理员已关闭注册"})
		return
	}
	if !pwdReg {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "管理员未开放密码自助注册"})
		return
	}
	username := strings.TrimSpace(req.Username)
	if len(username) < 2 || len(username) > 64 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名长度为 2～64 个字符"})
		return
	}
	emailNorm := strings.ToLower(strings.TrimSpace(req.Email))
	if !registerEmailPassesWhitelist(emailNorm) {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msgEmailNotInWhitelist})
		return
	}
	if err := verify.Verify(emailNorm, verify.SceneRegister, req.Code); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	// 强密码复杂度校验（安全审计 H-4）
	if msg := utils.ValidatePasswordStrength(req.Password); msg != "" {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": msg})
		return
	}
	var cnt int64
	database.DB.Model(&models.User{}).Where("username = ?", username).Count(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户名已存在"})
		return
	}
	database.DB.Model(&models.User{}).Where("LOWER(TRIM(email)) = ? AND email != ''", emailNorm).Count(&cnt)
	if cnt > 0 {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该邮箱已被注册"})
		return
	}
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcryptCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "密码加密失败"})
		return
	}
	user := models.User{
		Username: username,
		Password: string(hashedPassword),
		Email:    strings.TrimSpace(req.Email),
		Level:    0,
		Status:   1,
		RegTime:  time.Now(),
	}
	if err := database.DB.Create(&user).Error; err != nil {
		logger.Error("自助注册创建用户失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "注册失败"})
		return
	}
	dbcache.BustUserList()
	logger.Info("用户自助注册成功: %s", username)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "注册成功"})
}

// SendBindEmailCode POST /api/user/bind-email/send-code 已登录：向新邮箱发送绑定验证码
func SendBindEmailCode(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "请输入有效的邮箱地址"})
		return
	}
	userID := middleware.AuthUserID(c)
	emailNorm := strings.ToLower(strings.TrimSpace(req.Email))
	var other models.User
	if err := database.DB.Where("LOWER(TRIM(email)) = ? AND email != '' AND id != ?", emailNorm, userID).First(&other).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该邮箱已被其他账号使用"})
		return
	}
	if !verify.AllowPublicEmailRequest(c.ClientIP(), emailNorm, "bind_mail", 30, 12) {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "若邮件服务已配置，您将收到验证码"})
		return
	}
	emailConfig, err := getEmailConfig()
	if err != nil || emailConfig == nil {
		c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "若邮件服务已配置，您将收到验证码"})
		return
	}
	code, err := verify.Generate(emailNorm, verify.SceneBindMail)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	subject, body := notify.RenderVerificationCodeEmail(code, verify.SceneBindMail, "5")
	ec := *emailConfig
	ec.To = strings.TrimSpace(req.Email)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "验证码已发送"})
	enqueueNotifyMail("mail_bind_email", ec, subject, body)
}

// BindEmail POST /api/user/bind-email 已登录：校验验证码并更新邮箱
func BindEmail(c *gin.Context) {
	var req struct {
		Email string `json:"email" binding:"required,email"`
		Code  string `json:"code" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "参数错误"})
		return
	}
	userID := middleware.AuthUserID(c)
	emailNorm := strings.ToLower(strings.TrimSpace(req.Email))
	if err := verify.Verify(emailNorm, verify.SceneBindMail, req.Code); err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}
	var other models.User
	if err := database.DB.Where("LOWER(TRIM(email)) = ? AND email != '' AND id != ?", emailNorm, userID).First(&other).Error; err == nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该邮箱已被其他账号使用"})
		return
	}
	var user models.User
	if err := database.DB.First(&user, userID).Error; err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}
	database.DB.Model(&user).Update("email", req.Email)
	dbcache.BustUserList()
	logger.Info("用户绑定邮箱成功: uid=%d", userID)
	c.JSON(http.StatusOK, gin.H{"code": 0, "msg": "邮箱绑定成功"})
}
