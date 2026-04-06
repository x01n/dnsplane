package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"main/internal/api/middleware"
	"main/internal/cache"
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/* quicklogin token 缓存前缀，存储在 cache 层，自动 TTL 过期 */
const quickLoginPrefix = "ql:"

/* magicTotpPreauthPrefix 邮件链接已验证，待补 TOTP 的中间态（不签发 JWT） */
const magicTotpPreauthPrefix = "mtp:"

// MagicTotpPreauth 魔法登录第二步：已通过一次性链接校验，待提交 TOTP
type MagicTotpPreauth struct {
	UserID   string `json:"user_id"`
	DomainID string `json:"domain_id"`
}

/*
 * QuickLoginToken 快速登录令牌结构
 * 功能：存储于 cache 层，携带用户和域名信息，5 分钟自动过期
 */
type QuickLoginToken struct {
	Token    string `json:"token"`
	UserID   string `json:"user_id"`
	DomainID string `json:"domain_id"`
}

/* generateToken 生成 64 位随机 hex token */
func generateToken() string {
	bytes := make([]byte, 32)
	rand.Read(bytes)
	return hex.EncodeToString(bytes)
}

type GetQuickLoginURLRequest struct {
	DomainID string `json:"domain_id" binding:"required"`
}

/*
 * GetQuickLoginURL 生成快速登录链接
 * @route POST /domains/loginurl
 * 功能：为指定域名生成一次性快速登录 URL，token 存入 cache 层（5 分钟过期）
 */
func GetQuickLoginURL(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req GetQuickLoginURLRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	userID := c.GetString("user_id")

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(userID, c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	token := generateToken()
	expireAt := time.Now().Add(5 * time.Minute)

	cache.C.SetJSON(quickLoginPrefix+token, &QuickLoginToken{
		Token:    token,
		UserID:   userID,
		DomainID: req.DomainID,
	}, 5*time.Minute)

	scheme := "http"
	if c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https") {
		scheme = "https"
	}
	host := c.Request.Host

	url := scheme + "://" + host + "/api/quicklogin?token=" + token

	middleware.SuccessResponse(c, gin.H{"url": url, "token": token, "expire_at": expireAt})
}

// checkRateLimit 基于 cache.Incr 的固定窗口计数限流；失败时放行避免影响登录
func checkRateLimit(key string, max int, window time.Duration) bool {
	n, err := cache.C.Incr("rl:"+key, window)
	if err != nil {
		return true
	}
	return int(n) <= max
}

/*
 * QuickLogin 快速登录验证
 * @route GET /quicklogin?token=xxx
 * 功能：验证一次性 token → 查找用户 → 生成 JWT → 设置 HttpOnly cookie → 重定向到域名管理页
 * 安全：IP 限流（30次/小时）+ token 一次性使用后立即删除 + JWT 双 cookie 认证
 */
func QuickLogin(c *gin.Context) {
	/* IP 限流：每个 IP 每小时最多 30 次 */
	if !checkRateLimit("quicklogin_"+c.ClientIP(), 30, time.Hour) {
		c.JSON(http.StatusTooManyRequests, gin.H{"code": -1, "msg": "请求过于频繁，请稍后再试"})
		return
	}

	token := c.Query("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "token不能为空"})
		return
	}

	var loginToken QuickLoginToken
	if !cache.C.GetJSON(quickLoginPrefix+token, &loginToken) {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "msg": "token无效或已过期"})
		return
	}

	/* 一次性使用，立即删除 */
	cache.C.Delete(quickLoginPrefix + token)

	var user models.User
	if err := database.WithContext(c).First(&user, loginToken.UserID).Error; err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "msg": "用户不存在"})
		return
	}

	if user.Status != 1 {
		c.JSON(http.StatusUnauthorized, gin.H{"code": -1, "msg": "账户已被禁用"})
		return
	}

	/* 已启用 TOTP：不签发会话，写入中间态并跳转前端补验动态口令 */
	if user.TOTPOpen && user.TOTPSecret != "" {
		preToken := generateToken()
		if err := cache.C.SetJSON(magicTotpPreauthPrefix+preToken, &MagicTotpPreauth{
			UserID:   strconv.FormatUint(uint64(user.ID), 10),
			DomainID: loginToken.DomainID,
		}, 10*time.Minute); err != nil {
			logger.Error("缓存魔法登录 TOTP 中间态失败: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "登录失败"})
			return
		}
		base := strings.TrimSuffix(getSiteURL(), "/")
		next := fmt.Sprintf("%s/magic-login/totp/?preauth=%s", base, url.QueryEscape(preToken))
		c.Redirect(http.StatusFound, next)
		return
	}

	/* 生成 JWT token pair 并设置 HttpOnly cookie，实现真正的登录 */
	tokenPair, err := middleware.GenerateTokenPair(strconv.FormatUint(uint64(user.ID), 10), user.Username, user.Level)
	if err != nil {
		logger.Error("快速登录生成Token失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "登录失败"})
		return
	}

	if err := middleware.SetAuthCookies(c, tokenPair); err != nil {
		logger.Error("快速登录设置Cookie失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "登录失败"})
		return
	}

	/* 存储 refresh token JTI 用于轮转验证 */
	if rtClaims, _ := middleware.ParseToken(tokenPair.RefreshToken); rtClaims != nil {
		middleware.StoreRefreshJTI(strconv.FormatUint(uint64(user.ID), 10), rtClaims.ID)
	}

	/* 更新最后登录时间 */
	now := time.Now()
	database.WithContext(c).Model(&user).Update("last_time", &now)

	logger.Info("快速登录成功: user=%s domain=%s ip=%s", user.Username, loginToken.DomainID, c.ClientIP())

	redirectTo := "/dashboard/"
	if loginToken.DomainID != "" && loginToken.DomainID != "0" {
		redirectTo = "/dashboard/domains/" + loginToken.DomainID
	}
	c.Redirect(http.StatusFound, redirectTo)
}
