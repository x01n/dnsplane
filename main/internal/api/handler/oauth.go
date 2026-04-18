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
	"main/internal/dbcache"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/oauth"
	"main/internal/service"

	"github.com/gin-gonic/gin"
)

// OAuth state 缓存（安全审计 M-4）：
//   原实现使用进程内 map + 懒清理，多实例部署下 state 丢失导致登录失败，
//   且攻击者大量触发 OAuthLogin 可造成内存爆表 DoS。
//   改用统一的 cache.C（Redis 或内存）+ TTL 自动过期，多实例安全且不积压。
const oauthStatePrefix = "oauth_state:"
const oauthStateTTL = 5 * time.Minute

type oauthStateEntry struct {
	Provider string `json:"provider"`
	BindUID  uint   `json:"bind_uid"` // >0 表示这是绑定操作（已登录用户绑定第三方账号）
	Mode     string `json:"mode"`     // "login" 或 "bind"
}

func generateState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// saveOAuthState 将 state 写入 cache；失败返回 error，调用方需拦截。
func saveOAuthState(state string, entry oauthStateEntry) error {
	if cache.C == nil {
		return fmt.Errorf("cache 未初始化")
	}
	return cache.C.SetJSON(oauthStatePrefix+state, entry, oauthStateTTL)
}

// consumeOAuthState 读取并立刻删除（一次性使用）；返回 ok=false 表示不存在或已过期。
func consumeOAuthState(state string) (oauthStateEntry, bool) {
	var entry oauthStateEntry
	if cache.C == nil {
		return entry, false
	}
	if !cache.C.GetJSON(oauthStatePrefix+state, &entry) {
		return entry, false
	}
	cache.C.Delete(oauthStatePrefix + state)
	return entry, true
}

// GetOAuthProviders 获取已启用的 OAuth 提供商列表（公开API，不需要认证）
func GetOAuthProviders(c *gin.Context) {
	providers := oauth.GetEnabledProviders()
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": providers})
}

// OAuthLogin 跳转到第三方 OAuth2 授权页面
func OAuthLogin(c *gin.Context) {
	providerName := c.Param("provider")
	if providerName == "" {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "msg": "missing provider"})
		return
	}

	provider, err := oauth.GetProvider(providerName)
	if err != nil {
		c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
		return
	}

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{Provider: providerName, Mode: "login"}); err != nil {
		logger.Error("保存 OAuth state 失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "msg": "oauth state 保存失败"})
		return
	}

	redirectURI := getOAuthCallbackURI(providerName)
	authURL := provider.AuthorizeURL(state, redirectURI)

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

func getOAuthCallbackURI(provider string) string {
	base := getSiteURL()
	if provider == "github" {
		return base + "/api/auth/github/callback"
	}
	return base + "/api/auth/oauth/" + provider + "/callback"
}

// OAuthCallback 处理 OAuth2 回调
func OAuthCallback(c *gin.Context) {
	providerName := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")
	siteURL := getSiteURL()

	if code == "" || state == "" {
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=invalid_request")
		return
	}

	// 验证 state（一次性使用；过期由 cache TTL 保证）
	entry, exists := consumeOAuthState(state)
	if !exists || entry.Provider != providerName {
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=invalid_state")
		return
	}

	// 获取 Provider
	provider, err := oauth.GetProvider(providerName)
	if err != nil {
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=provider_error")
		return
	}

	redirectURI := getOAuthCallbackURI(providerName)

	// 用 code 换 token
	ctx := c.Request.Context()
	tokenResult, err := provider.ExchangeToken(ctx, code, redirectURI)
	if err != nil {
		logger.Error("[OAuth] %s token exchange failed: %v", providerName, err)
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=token_failed")
		return
	}

	// 获取第三方用户信息
	userInfo, err := provider.GetUserInfo(ctx, tokenResult)
	if err != nil {
		logger.Error("[OAuth] %s get user info failed: %v", providerName, err)
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=userinfo_failed")
		return
	}

	// ========== 绑定模式 ==========
	if entry.Mode == "bind" && entry.BindUID > 0 {
		handleOAuthBind(c, providerName, userInfo, tokenResult, entry.BindUID, siteURL)
		return
	}

	// ========== 登录模式 ==========
	handleOAuthLogin(c, providerName, userInfo, tokenResult, siteURL)
}

// handleOAuthLogin 处理 OAuth 登录/注册
func handleOAuthLogin(c *gin.Context, providerName string, userInfo *oauth.UserInfo, tokenResult *oauth.TokenResult, siteURL string) {
	// 查找已绑定用户
	var binding models.UserOAuth
	if err := database.WithContext(c).Where("provider = ? AND provider_user_id = ?", providerName, userInfo.ID).First(&binding).Error; err == nil {
		// 已绑定 → 登录该用户
		var user models.User
		if err := database.WithContext(c).First(&user, binding.UserID).Error; err != nil {
			c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=user_not_found")
			return
		}
		if user.Status != 1 {
			c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=account_disabled")
			return
		}

		// 更新 token
		database.WithContext(c).Model(&binding).Updates(map[string]interface{}{
			"access_token":    tokenResult.AccessToken,
			"refresh_token":   tokenResult.RefreshToken,
			"provider_name":   userInfo.Name,
			"provider_email":  userInfo.Email,
			"provider_avatar": userInfo.Avatar,
		})

		loginSuccessRedirect(c, &user, providerName, siteURL)
		return
	}

	// 未绑定 → 检查是否开放注册
	var registerEnabled string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "register_enabled").Pluck("value", &registerEnabled)
	if registerEnabled != "true" {
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=register_disabled")
		return
	}

	emailNorm := strings.ToLower(strings.TrimSpace(userInfo.Email))
	if !registerEmailPassesWhitelist(emailNorm) {
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=email_not_whitelisted")
		return
	}

	// 自动创建用户 — 生成唯一用户名
	username := generateUniqueUsername(c, providerName, userInfo.Name, userInfo.ID)

	user := models.User{
		Username: username,
		Password: "",
		Email:    userInfo.Email,
		Level:    1,
		Status:   1,
		RegTime:  time.Now(),
	}
	if err := database.WithContext(c).Create(&user).Error; err != nil {
		logger.Error("[OAuth] create user failed: %v", err)
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=create_failed")
		return
	}

	dbcache.BustUserList()
	// 创建绑定
	createOAuthBinding(c, user.ID, providerName, userInfo, tokenResult)

	logger.Info("[OAuth] 新用户注册: %s (via %s: %s)", username, providerName, userInfo.Name)
	service.Audit.LogUserAction(c, strconv.FormatUint(uint64(user.ID), 10), user.Username, "oauth_register", fmt.Sprintf("通过%s注册", providerName))

	loginSuccessRedirect(c, &user, providerName, siteURL)
}

// handleOAuthBind 处理已登录用户绑定 OAuth
func handleOAuthBind(c *gin.Context, providerName string, userInfo *oauth.UserInfo, tokenResult *oauth.TokenResult, userID uint, siteURL string) {
	// 检查此第三方账号是否已被其他用户绑定
	var existBind models.UserOAuth
	if database.WithContext(c).Where("provider = ? AND provider_user_id = ?", providerName, userInfo.ID).First(&existBind).Error == nil {
		if existBind.UserID != userID {
			c.Redirect(http.StatusTemporaryRedirect, siteURL+"/dashboard/profile?error=already_bound_other")
			return
		}
		// 已绑定到当前用户，更新信息
		database.WithContext(c).Model(&existBind).Updates(map[string]interface{}{
			"access_token":    tokenResult.AccessToken,
			"refresh_token":   tokenResult.RefreshToken,
			"provider_name":   userInfo.Name,
			"provider_email":  userInfo.Email,
			"provider_avatar": userInfo.Avatar,
		})
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/dashboard/profile?bind=success")
		return
	}

	// 创建绑定
	createOAuthBinding(c, userID, providerName, userInfo, tokenResult)
	c.Redirect(http.StatusTemporaryRedirect, siteURL+"/dashboard/profile?bind=success")
}

// createOAuthBinding 创建 OAuth 绑定记录
func createOAuthBinding(c *gin.Context, userID uint, providerName string, userInfo *oauth.UserInfo, tokenResult *oauth.TokenResult) {
	binding := models.UserOAuth{
		UserID:         userID,
		Provider:       providerName,
		ProviderUserID: userInfo.ID,
		ProviderName:   userInfo.Name,
		ProviderEmail:  userInfo.Email,
		ProviderAvatar: userInfo.Avatar,
		AccessToken:    tokenResult.AccessToken,
		RefreshToken:   tokenResult.RefreshToken,
		CreatedAt:      time.Now(),
	}
	if tokenResult.ExpiresIn > 0 {
		t := time.Now().Add(time.Duration(tokenResult.ExpiresIn) * time.Second)
		binding.ExpiresAt = &t
	}
	database.WithContext(c).Create(&binding)
}

// loginSuccessRedirect 登录成功后重定向（HttpOnly _t/_rt + 前端用 hash 同步 localStorage，避免 token 出现在查询串）
func loginSuccessRedirect(c *gin.Context, user *models.User, providerName, siteURL string) {
	tokenPair, err := middleware.GenerateTokenPair(strconv.FormatUint(uint64(user.ID), 10), user.Username, user.Level)
	if err != nil {
		logger.Error("[OAuth] generate token failed: %v", err)
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=token_failed")
		return
	}

	if err := middleware.SetAuthCookies(c, tokenPair); err != nil {
		logger.Error("[OAuth] set auth cookies failed: %v", err)
		c.Redirect(http.StatusTemporaryRedirect, siteURL+"/login/?error=token_failed")
		return
	}

	if rtClaims, _ := middleware.ParseToken(tokenPair.RefreshToken); rtClaims != nil {
		middleware.StoreRefreshJTI(strconv.FormatUint(uint64(user.ID), 10), rtClaims.ID)
	}

	now := time.Now()
	database.WithContext(c).Model(user).Update("last_time", &now)

	service.Audit.LogUserAction(c, strconv.FormatUint(uint64(user.ID), 10), user.Username, "oauth_login", fmt.Sprintf("%s登录成功", providerName))
	logger.Info("[OAuth] 登录成功: %s (via %s)", user.Username, providerName)

	frag := fmt.Sprintf("access_token=%s&refresh_token=%s",
		url.QueryEscape(tokenPair.AccessToken),
		url.QueryEscape(tokenPair.RefreshToken))
	c.Redirect(http.StatusFound, siteURL+"/oauth-callback#"+frag)
}

// ==================== 绑定管理 API（需要认证）====================

// GetOAuthBindings 获取当前用户的所有 OAuth 绑定
func GetOAuthBindings(c *gin.Context) {
	userID := middleware.AuthUserID(c)
	var bindings []models.UserOAuth
	database.WithContext(c).Where("user_id = ?", userID).Find(&bindings)
	middleware.SuccessResponse(c, bindings)
}

// GetOAuthBindURL 获取绑定跳转 URL
func GetOAuthBindURL(c *gin.Context) {
	userID := middleware.AuthUserID(c)
	var req struct {
		Provider string `json:"provider"`
	}
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.Provider == "" {
		middleware.ErrorResponse(c, "请指定提供商")
		return
	}

	provider, err := oauth.GetProvider(req.Provider)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	state := generateState()
	if err := saveOAuthState(state, oauthStateEntry{
		Provider: req.Provider,
		BindUID:  userID,
		Mode:     "bind",
	}); err != nil {
		middleware.ErrorResponse(c, "oauth state 保存失败")
		return
	}

	redirectURI := getOAuthCallbackURI(req.Provider)
	authURL := provider.AuthorizeURL(state, redirectURI)

	middleware.SuccessResponse(c, gin.H{"url": authURL})
}

// UnbindOAuth 解绑 OAuth
func UnbindOAuth(c *gin.Context) {
	userID := middleware.AuthUserID(c)
	var req struct {
		Provider string `json:"provider"`
	}
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.Provider == "" {
		middleware.ErrorResponse(c, "请指定提供商")
		return
	}

	result := database.WithContext(c).Where("user_id = ? AND provider = ?", userID, req.Provider).Delete(&models.UserOAuth{})
	if result.RowsAffected == 0 {
		middleware.ErrorResponse(c, "未找到绑定记录")
		return
	}

	username := c.GetString("username")
	service.Audit.LogUserAction(c, strconv.FormatUint(uint64(userID), 10), username, "oauth_unbind", fmt.Sprintf("解绑%s", req.Provider))
	middleware.SuccessMsg(c, "解绑成功")
}

// AdminGetUserOAuthBindings 管理员查看用户的 OAuth 绑定
func AdminGetUserOAuthBindings(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}
	var req struct {
		UserID uint `json:"user_id"`
	}
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.UserID == 0 {
		middleware.ErrorResponse(c, "缺少用户ID")
		return
	}
	var bindings []models.UserOAuth
	database.WithContext(c).Where("user_id = ?", req.UserID).Find(&bindings)
	middleware.SuccessResponse(c, bindings)
}

// AdminUnbindOAuth 管理员解绑用户的 OAuth
func AdminUnbindOAuth(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}
	var req struct {
		ID uint `json:"id"` // UserOAuth ID
	}
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.ID == 0 {
		middleware.ErrorResponse(c, "缺少绑定ID")
		return
	}
	database.WithContext(c).Delete(&models.UserOAuth{}, req.ID)
	middleware.SuccessMsg(c, "解绑成功")
}

// generateUniqueUsername 生成唯一用户名，逐级尝试直到不冲突
func generateUniqueUsername(c *gin.Context, provider, name, id string) string {
	candidates := []string{}

	if name != "" {
		candidates = append(candidates, provider+"_"+name)
	}
	if id != "" {
		candidates = append(candidates, provider+"_"+id)
	}
	if name != "" && id != "" {
		candidates = append(candidates, fmt.Sprintf("%s_%s_%s", provider, name, id))
	}

	for _, candidate := range candidates {
		var existing models.User
		if database.WithContext(c).Unscoped().Where("username = ?", candidate).First(&existing).Error != nil {
			return candidate // 不存在，可用
		}
	}

	// 所有候选都冲突，加随机后缀
	b := make([]byte, 4)
	rand.Read(b)
	suffix := hex.EncodeToString(b)
	if name != "" {
		return fmt.Sprintf("%s_%s_%s", provider, name, suffix)
	}
	return fmt.Sprintf("%s_%s", provider, suffix)
}
