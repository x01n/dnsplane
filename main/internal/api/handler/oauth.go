package handler

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
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

	// 该来源的登录开关（auth_oauth_<provider>_login）：关闭时在跳转授权页之前就拒绝，
	// 否则用户会被送到第三方完成授权、再在回调处失败，体验与安全边界都说不通
	// 拒绝时回跳登录页并带错误码，而不是回 JSON —— 入口是整页跳转到本地址的，
	// 直接写 JSON 会让用户停在白屏页面上，回不去也无法重试
	// 键名与前端 auth-control-settings.tsx 的 `auth_oauth_${provider.name}_login` 拼法一致
	if !handlerSwitchOn("auth_oauth_" + providerName + "_login") {
		oauthFailureRedirect(c, "", errCodeProviderLoginOff)
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

	redirectURI := oauthCallbackPageURL()
	authURL := provider.AuthorizeURL(state, redirectURI)

	c.Redirect(http.StatusTemporaryRedirect, authURL)
}

/*
 * handleOAuthCallback 消费 state 并完成授权码换会话
 *
 * 授权码只在第三方页面回跳时到达后端，因此校验 state、换取令牌、拉取用户信息的顺序在此固定，
 * 两条入口（旧回调地址与本页回跳）共用它，避免两边分支逐渐分叉。
 * state 是一次性的，只能在此消费一次，调用方需要模式信息时由返回值取，不得自行再消费。
 * expectProvider 非空时要求与 state 记录的提供商一致：旧回调地址把它写在路径上，
 * 两者不符说明该 state 被拿到了别的提供商分支，必须拒绝
 */
func handleOAuthCallback(c *gin.Context, expectProvider, code, state string) (oauthResult, oauthStateEntry) {
	var empty oauthStateEntry

	entry, exists := consumeOAuthState(state)
	if !exists {
		return oauthResult{code: errCodeInvalidState}, empty
	}
	if expectProvider != "" && entry.Provider != expectProvider {
		return oauthResult{code: errCodeInvalidState}, entry
	}

	provider, err := oauth.GetProvider(entry.Provider)
	if err != nil {
		return oauthResult{code: errCodeProviderError}, entry
	}

	ctx := c.Request.Context()
	tokenResult, err := provider.ExchangeToken(ctx, code, oauthCallbackPageURL())
	if err != nil {
		logger.Error("[OAuth] %s token exchange failed: %v", entry.Provider, err)
		return oauthResult{code: errCodeTokenFailed}, entry
	}

	userInfo, err := provider.GetUserInfo(ctx, tokenResult)
	if err != nil {
		logger.Error("[OAuth] %s get user info failed: %v", entry.Provider, err)
		return oauthResult{code: errCodeUserInfoFailed}, entry
	}

	if entry.Mode == "bind" && entry.BindUID > 0 {
		return applyOAuthBind(c, entry.Provider, userInfo, tokenResult, entry.BindUID), entry
	}

	result, user := applyOAuthLogin(c, entry.Provider, userInfo, tokenResult)
	if result.code != "" {
		return result, entry
	}
	if !establishSession(c, user, entry.Provider) {
		return oauthResult{code: errCodeTokenFailed}, entry
	}
	return oauthResult{}, entry
}

/*
 * oauthCallbackPageURL 第三方授权完成后回跳的前端页面地址
 *
 * 回跳落在前端页面而非后端接口：code 由页面读出后再调 /api/auth/oauth/exchange 换取会话，
 * token 因此不再经过任何重定向 URL（查询串与 fragment 都会留存在浏览器历史与代理日志里）。
 * 该地址同时用于授权请求与令牌交换，两处必须逐字一致，否则第三方会以 redirect_uri 不匹配拒绝
 */
func oauthCallbackPageURL() string {
	return getSiteURL() + "/oauth-callback"
}

/*
 * oauthFailureRedirect 失败回到发起登录的前端页面并带上错误码
 *
 * 错误码由登录页转成中文提示，因此默认落在登录页；绑定模式落回个人资料页，
 * 与绑定成功后的落点一致，用户不会在绑定中途被带到登录页
 */
func oauthFailureRedirect(c *gin.Context, mode, code string) {
	target := "/login?error=" + code
	if mode == "bind" {
		target = "/dashboard/profile?error=" + code
	}
	c.Redirect(http.StatusTemporaryRedirect, getSiteURL()+target)
}

// OAuthCallback 处理第三方回调（兼容路径：第三方后台仍配置旧回调地址时走这里，由后端直接用 code 换会话）
func OAuthCallback(c *gin.Context) {
	providerName := c.Param("provider")
	code := c.Query("code")
	state := c.Query("state")

	if code == "" || state == "" {
		oauthFailureRedirect(c, "", errCodeInvalidRequest)
		return
	}

	result, entry := handleOAuthCallback(c, providerName, code, state)
	if result.code != "" {
		oauthFailureRedirect(c, entry.Mode, result.code)
		return
	}

	if entry.Mode == "bind" && entry.BindUID > 0 {
		c.Redirect(http.StatusTemporaryRedirect, getSiteURL()+"/dashboard/profile?bind=success")
		return
	}
	// 会话已由 HttpOnly cookie 承载，页面只需跳进控制台
	c.Redirect(http.StatusFound, getSiteURL()+"/dashboard/")
}

/*
 * OAuthExchange 前端回调页用授权码换取登录会话
 *
 * 第三方统一回跳到前端页面 /oauth-callback，页面读出 code 与 state 后调本接口，
 * 由后端用 code 换 token 并下发 HttpOnly cookie。响应体只回业务结果，
 * 不含任何令牌明文——请求日志中间件会截取成功响应体入库，凭据一旦出现在 body 就会长期留存
 */
func OAuthExchange(c *gin.Context) {
	var req struct {
		Code  string `json:"code"`
		State string `json:"state"`
	}
	if err := middleware.BindJSONFlexible(c, &req); err != nil {
		middleware.ErrorResponseWithCode(c, -1, errCodeInvalidRequest)
		return
	}

	code := strings.TrimSpace(req.Code)
	state := strings.TrimSpace(req.State)
	if code == "" || state == "" {
		middleware.ErrorResponseWithCode(c, -1, errCodeInvalidRequest)
		return
	}

	// state 一次性消费，其条目已承载 provider 与登录/绑定模式，前端无需再传 provider
	result, entry := handleOAuthCallback(c, "", code, state)
	if result.code != "" {
		middleware.ErrorResponseWithCode(c, -1, result.code)
		return
	}

	if entry.Mode == "bind" && entry.BindUID > 0 {
		middleware.SuccessResponse(c, gin.H{"mode": "bind", "redirect": "/dashboard/profile?bind=success"})
		return
	}
	middleware.SuccessResponse(c, gin.H{"mode": "login", "redirect": "/dashboard/"})
}

/*
 * oauthResult 业务结果，code 为空表示成功
 *
 * 登录链路与绑定链路共用同一套结果码：Redirect 回调路径把它拼进查询串回跳，
 * exchange 路径把它放进 JSON 响应体，两处错误码因此天然一致
 */
type oauthResult struct {
	code string
}

/*
 * OAuth 回调错误码，与前端 web/lib/oauth-callback.ts 的 OAUTH_CALLBACK_ERRORS 逐字对应
 *
 * 抽成常量：这些字面量同时出现在重定向查询串与 JSON 响应体两条路径上，
 * 拼错任何一个都会让前端落到兜底文案，而编译期不会报错
 */
const (
	errCodeProviderLoginOff  = "provider_login_off"
	errCodeInvalidRequest    = "invalid_request"
	errCodeInvalidState      = "invalid_state"
	errCodeProviderError     = "provider_error"
	errCodeTokenFailed       = "token_failed"
	errCodeUserInfoFailed    = "userinfo_failed"
	errCodeRegisterDisabled  = "register_disabled"
	errCodeEmailNotWhitelist = "email_not_whitelisted"
	errCodeAccountDisabled   = "account_disabled"
	errCodeUserNotFound      = "user_not_found"
	errCodeCreateFailed      = "create_failed"
	errCodeAlreadyBoundOther = "already_bound_other"
)

// applyOAuthLogin 处理 OAuth 登录/注册，成功时返回用户，失败时结果码由调用方决定跳转或落进响应体
func applyOAuthLogin(c *gin.Context, providerName string, userInfo *oauth.UserInfo, tokenResult *oauth.TokenResult) (oauthResult, *models.User) {
	// 查找已绑定用户
	var binding models.UserOAuth
	if err := database.WithContext(c).Where("provider = ? AND provider_user_id = ?", providerName, userInfo.ID).First(&binding).Error; err == nil {
		// 已绑定 → 登录该用户
		var user models.User
		if err := database.WithContext(c).First(&user, binding.UserID).Error; err != nil {
			return oauthResult{code: errCodeUserNotFound}, nil
		}
		if user.Status != 1 {
			return oauthResult{code: errCodeAccountDisabled}, nil
		}

		// 更新 token
		database.WithContext(c).Model(&binding).Updates(map[string]interface{}{
			"access_token":    tokenResult.AccessToken,
			"refresh_token":   tokenResult.RefreshToken,
			"provider_name":   userInfo.Name,
			"provider_email":  userInfo.Email,
			"provider_avatar": userInfo.Avatar,
		})

		return oauthResult{}, &user
	}

	// 未绑定 → 检查是否开放注册：全局 register_enabled 与该来源的 auth_oauth_<provider>_register 都需放开
	var registerEnabled string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "register_enabled").Pluck("value", &registerEnabled)
	if registerEnabled != "true" {
		return oauthResult{code: errCodeRegisterDisabled}, nil
	}
	if !handlerSwitchOn("auth_oauth_" + providerName + "_register") {
		return oauthResult{code: errCodeRegisterDisabled}, nil
	}

	emailNorm := strings.ToLower(strings.TrimSpace(userInfo.Email))
	if !registerEmailPassesWhitelist(emailNorm) {
		return oauthResult{code: errCodeEmailNotWhitelist}, nil
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
		return oauthResult{code: errCodeCreateFailed}, nil
	}

	dbcache.BustUserList()
	// 创建绑定
	createOAuthBinding(c, user.ID, providerName, userInfo, tokenResult)

	logger.Info("[OAuth] 新用户注册: %s (via %s: %s)", username, providerName, userInfo.Name)
	service.Audit.LogUserAction(c, strconv.FormatUint(uint64(user.ID), 10), user.Username, "oauth_register", fmt.Sprintf("通过%s注册", providerName))

	return oauthResult{}, &user
}

// applyOAuthBind 处理已登录用户绑定 OAuth，成功时结果码为空
func applyOAuthBind(c *gin.Context, providerName string, userInfo *oauth.UserInfo, tokenResult *oauth.TokenResult, userID uint) oauthResult {
	// 检查此第三方账号是否已被其他用户绑定
	var existBind models.UserOAuth
	if database.WithContext(c).Where("provider = ? AND provider_user_id = ?", providerName, userInfo.ID).First(&existBind).Error == nil {
		if existBind.UserID != userID {
			return oauthResult{code: errCodeAlreadyBoundOther}
		}
		// 已绑定到当前用户，更新信息
		database.WithContext(c).Model(&existBind).Updates(map[string]interface{}{
			"access_token":    tokenResult.AccessToken,
			"refresh_token":   tokenResult.RefreshToken,
			"provider_name":   userInfo.Name,
			"provider_email":  userInfo.Email,
			"provider_avatar": userInfo.Avatar,
		})
		return oauthResult{}
	}

	// 创建绑定
	createOAuthBinding(c, userID, providerName, userInfo, tokenResult)
	return oauthResult{}
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

/*
 * establishSession 为登录成功的用户建立会话
 *
 * 只下发 HttpOnly cookie，函数不接受任何 URL 参数：此前调用方把 token 拼进 302 的 fragment，
 * 凭据会留在浏览器历史里；现在是否需要把 access token 交给前端，由回调页改用
 * POST /api/auth/refresh 自行取得（会话 cookie 已在，该接口不需要额外凭据）
 */
func establishSession(c *gin.Context, user *models.User, providerName string) bool {
	uid := strconv.FormatUint(uint64(user.ID), 10)

	tokenPair, err := middleware.GenerateTokenPair(uid, user.Username, user.Level)
	if err != nil {
		logger.Error("[OAuth] generate token failed: %v", err)
		return false
	}

	if err := middleware.SetAuthCookies(c, tokenPair); err != nil {
		logger.Error("[OAuth] set auth cookies failed: %v", err)
		return false
	}

	if rtClaims, _ := middleware.ParseToken(tokenPair.RefreshToken); rtClaims != nil {
		middleware.StoreRefreshJTI(uid, rtClaims.ID)
	}

	now := time.Now()
	database.WithContext(c).Model(user).Update("last_time", &now)

	service.Audit.LogUserAction(c, uid, user.Username, "oauth_login", fmt.Sprintf("%s登录成功", providerName))
	logger.Info("[OAuth] 登录成功: %s (via %s)", user.Username, providerName)
	return true
}

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

	redirectURI := oauthCallbackPageURL()
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
