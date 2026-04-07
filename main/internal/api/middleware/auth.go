package middleware

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/sysconfig"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
)

/*
 * Cookie 加密层
 * cookie 值 = AES-GCM 加密后的 access token (hex 编码)
 * 使用 JWT secret 的 SHA-256 摘要作为 AES-256 密钥
 * 无服务端状态，服务重启不影响已有 cookie
 * cookie 值完全无 JWT 特征，外部无法解密
 */

/* deriveKey 从 JWT secret 派生 AES-256 密钥 */
func deriveKey() []byte {
	cfg := config.Get()
	h := sha256.Sum256([]byte(cfg.JWT.Secret))
	return h[:]
}

/*
 * EncryptForCookie 将 access token 加密为 cookie 值
 * 功能：AES-GCM 加密，输出 hex(nonce + ciphertext)，完全无 JWT 特征
 */
func EncryptForCookie(accessToken string) (string, error) {
	block, err := aes.NewCipher(deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ciphertext := gcm.Seal(nonce, nonce, []byte(accessToken), nil)
	return hex.EncodeToString(ciphertext), nil
}

/*
 * decryptCookie 从 cookie 值解密出 access token
 * 功能：AES-GCM 解密，校验完整性，返回原始 token
 */
func DecryptCookie(cookieVal string) (string, bool) {
	data, err := hex.DecodeString(cookieVal)
	if err != nil {
		return "", false
	}
	block, err := aes.NewCipher(deriveKey())
	if err != nil {
		return "", false
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", false
	}
	nonceSize := gcm.NonceSize()
	if len(data) < nonceSize {
		return "", false
	}
	plaintext, err := gcm.Open(nil, data[:nonceSize], data[nonceSize:], nil)
	if err != nil {
		return "", false
	}
	return string(plaintext), true
}

const (
	AccessTokenExpire  = 15 * time.Minute   // Access Token 15分钟过期
	RefreshTokenExpire = 7 * 24 * time.Hour // Refresh Token 7天过期
	RefreshJTIPrefix   = "rtjti:"           // Cache 中 refresh token JTI 的前缀

	authUserModelCtxKey = "auth_user_model" // Auth 中间件注入的 *models.User，供 GetUserInfo 等复用，避免二次查库
)

// authUserCacheTTL 认证用户信息缓存；远程部署下略长可明显减少 SQLite/Redis 往返
const authUserCacheTTL = 30 * time.Second

type Claims struct {
	UserID    string `json:"user_id"`
	Username  string `json:"username"`
	Level     int    `json:"level"`
	TokenType string `json:"token_type"` // "access" or "refresh"
	jwt.RegisteredClaims
}

/* TokenPair 访问令牌 + 刷新令牌对 */
type TokenPair struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int64  `json:"expires_in"` // access token过期时间（秒）
}

/* extractBearerToken 从 Authorization 头中提取 Bearer Token */
func extractBearerToken(header string) string {
	parts := strings.SplitN(header, " ", 2)
	if len(parts) == 2 && parts[0] == "Bearer" {
		return parts[1]
	}
	return ""
}

func Auth() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 1. 从 HttpOnly Cookie 中提取复合会话凭证 (sid.verifier)
		cookieVal, cookieErr := c.Cookie("_t")
		if cookieErr != nil || cookieVal == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "未登录"})
			c.Abort()
			return
		}

		// 2. AES-GCM 解密 cookie，获取真实 access token
		realToken, valid := DecryptCookie(cookieVal)
		if !valid {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "会话已过期"})
			c.Abort()
			return
		}

		// 3. 从 Authorization Bearer 头中提取 token，与 session 中的 token 双重验证
		authHeader := c.GetHeader("Authorization")
		headerToken := extractBearerToken(authHeader)
		if headerToken == "" || headerToken != realToken {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "Token不一致"})
			c.Abort()
			return
		}

		claims, err := ParseToken(realToken)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "Token无效或已过期"})
			c.Abort()
			return
		}

		// 只允许access token访问API
		if claims.TokenType != "" && claims.TokenType != "access" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "无效的Token类型"})
			c.Abort()
			return
		}

		/*
		 * 从缓存获取用户信息（10 秒 TTL），减少每次请求的 DB 查询
		 * 权限变更（禁用/升降级）最多 10 秒后生效，属于可接受延迟
		 */
		user, err := getCachedAuthUser(claims.UserID)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "用户不存在"})
			c.Abort()
			return
		}

		if user.Status != 1 {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "账户已被禁用"})
			c.Abort()
			return
		}

		// user_id 存字符串，供 GetString("user_id") 与各层 SQL 占位符一致（勿存 uint，否则 GetString 为空）
		c.Set("user_id", strconv.FormatUint(uint64(user.ID), 10))
		c.Set("username", user.Username)
		c.Set("level", user.Level)
		c.Set(authUserModelCtxKey, user)
		ApplyUserPermissionModules(c, user.Permissions)

		// 检查token是否即将过期（剩余5分钟），提示前端刷新
		if claims.ExpiresAt != nil {
			remaining := time.Until(claims.ExpiresAt.Time)
			if remaining < 5*time.Minute && remaining > 0 {
				c.Header("X-Token-Expiring", "true")
			}
		}

		c.Next()
	}
}

// AuthCachedUser 返回 Auth 中间件已加载的用户模型（与认证缓存一致）；不存在则 ok=false
func AuthCachedUser(c *gin.Context) (user *models.User, ok bool) {
	v, exists := c.Get(authUserModelCtxKey)
	if !exists {
		return nil, false
	}
	user, ok = v.(*models.User)
	if !ok || user == nil {
		return nil, false
	}
	return user, true
}

// AuthUserID 从上下文读取当前登录用户 ID（与 Auth 中间件写入的字符串 user_id 对应）
func AuthUserID(c *gin.Context) uint {
	s := c.GetString("user_id")
	if s == "" {
		return 0
	}
	u, err := strconv.ParseUint(s, 10, 32)
	if err != nil {
		return 0
	}
	return uint(u)
}

/* GenerateTokenPair 生成 access token 和 refresh token 对 */
func GenerateTokenPair(userID string, username string, level int) (*TokenPair, error) {
	accessToken, err := generateAccessToken(userID, username, level)
	if err != nil {
		return nil, err
	}

	refreshToken, err := generateRefreshToken(userID, username, level)
	if err != nil {
		return nil, err
	}

	return &TokenPair{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(AccessTokenExpire.Seconds()),
	}, nil
}

/* generateAccessToken 生成短期 access token（15 分钟） */
func generateAccessToken(userID string, username string, level int) (string, error) {
	cfg := config.Get()
	claims := Claims{
		UserID:    userID,
		Username:  username,
		Level:     level,
		TokenType: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(AccessTokenExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        generateJTI(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWT.Secret))
}

/* generateRefreshToken 生成长期 refresh token（7 天） */
func generateRefreshToken(userID string, username string, level int) (string, error) {
	cfg := config.Get()
	claims := Claims{
		UserID:    userID,
		Username:  username,
		Level:     level,
		TokenType: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(RefreshTokenExpire)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			ID:        generateJTI(),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(cfg.JWT.Secret))
}

/* GenerateToken 兼容旧接口，生成单个 access token */
func GenerateToken(userID string, username string, level int) (string, error) {
	return generateAccessToken(userID, username, level)
}

/*
 * SetAuthCookies 统一设置 access token 和 refresh token 的 HttpOnly cookie
 * 功能：将两个 token 都用 AES-GCM 加密后写入独立的 HttpOnly cookie
 * _t = 加密后的 access token (短期, 15min)
 * _rt = 加密后的 refresh token (长期, 7天)
 */
func SetAuthCookies(c *gin.Context, tokenPair *TokenPair) error {
	atCookie, err := EncryptForCookie(tokenPair.AccessToken)
	if err != nil {
		return err
	}
	rtCookie, err := EncryptForCookie(tokenPair.RefreshToken)
	if err != nil {
		return err
	}

	secure := isSecureRequest(c)
	c.SetSameSite(http.SameSiteStrictMode)
	c.SetCookie("_t", atCookie, int(AccessTokenExpire.Seconds()), "/", "", secure, true)
	c.SetCookie("_rt", rtCookie, int(RefreshTokenExpire.Seconds()), "/api/auth/refresh", "", secure, true)
	return nil
}

/* ClearAuthCookies 清除所有认证 cookie */
func ClearAuthCookies(c *gin.Context) {
	secure := isSecureRequest(c)
	c.SetCookie("_t", "", -1, "/", "", secure, true)
	c.SetCookie("_rt", "", -1, "/api/auth/refresh", "", secure, true)
}

/*
 * isSecureRequest 判断当前请求是否通过 HTTPS
 * 功能：支持直连 HTTPS 和反向代理场景（X-Forwarded-Proto）
 */
func isSecureRequest(c *gin.Context) bool {
	if c.Request.TLS != nil {
		return true
	}
	return strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
}

/*
 * storeRefreshJTI 将 refresh token 的 JTI 存入 cache，用于轮转验证
 * 每个用户同一时间只有一个有效的 refresh token JTI
 */
func StoreRefreshJTI(userID, jti string) {
	if cache.C == nil {
		return
	}
	key := RefreshJTIPrefix + userID
	cache.C.Set(key, jti, RefreshTokenExpire)
}

/*
 * validateAndRevokeRefreshJTI 验证 refresh token 的 JTI 是否为当前有效值
 * 验证通过后立即删除（一次性使用），防止 token 重用攻击
 * 返回 true 表示 JTI 有效
 */
func validateAndRevokeRefreshJTI(userID, jti string) bool {
	if cache.C == nil {
		return true // 无缓存时跳过 JTI 检查
	}
	key := RefreshJTIPrefix + userID
	storedJTI, ok := cache.C.Get(key)
	if !ok {
		return true // 无记录时允许（兼容旧 token）
	}
	if storedJTI != jti {
		// JTI 不匹配：可能是 token 重用攻击，吊销该用户所有 refresh token
		cache.C.Delete(key)
		logger.Warn("[Auth] refresh token JTI 不匹配，可能存在 token 重用攻击 (user=%s)", userID)
		return false
	}
	// 验证通过，删除旧 JTI（新的会在生成新 token 后存入）
	cache.C.Delete(key)
	return true
}

/*
 * RefreshAccessToken 使用 refresh token 刷新 access token
 * 功能：验证 refresh token → 检查 JTI → 生成新的 token pair → 存储新 JTI
 * 安全特性：
 *   - JTI 轮转：每次刷新旧 refresh token 失效
 *   - 重用检测：如果旧 refresh token 被二次使用，吊销该用户全部 refresh token
 */
func RefreshAccessToken(refreshToken string) (*TokenPair, error) {
	claims, err := ParseToken(refreshToken)
	if err != nil {
		return nil, err
	}

	if claims.TokenType != "refresh" {
		return nil, jwt.ErrTokenInvalidClaims
	}

	// JTI 轮转验证
	if !validateAndRevokeRefreshJTI(claims.UserID, claims.ID) {
		return nil, jwt.ErrTokenInvalidClaims
	}

	// 从数据库获取最新用户信息
	var user models.User
	if err := database.DB.Where("id = ?", claims.UserID).First(&user).Error; err != nil {
		return nil, err
	}

	if user.Status != 1 {
		return nil, jwt.ErrTokenInvalidClaims
	}

	uidStr := strconv.FormatUint(uint64(user.ID), 10)
	// 生成新的 token pair（使用数据库中的最新 level）
	newPair, err := GenerateTokenPair(uidStr, user.Username, user.Level)
	if err != nil {
		return nil, err
	}

	// 解析新 refresh token 获取 JTI，存入 cache
	newClaims, _ := ParseToken(newPair.RefreshToken)
	if newClaims != nil {
		StoreRefreshJTI(uidStr, newClaims.ID)
	}

	return newPair, nil
}

func ParseToken(tokenString string) (*Claims, error) {
	cfg := config.Get()
	token, err := jwt.ParseWithClaims(tokenString, &Claims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(cfg.JWT.Secret), nil
	})

	if err != nil {
		return nil, err
	}

	if claims, ok := token.Claims.(*Claims); ok && token.Valid {
		return claims, nil
	}

	return nil, jwt.ErrSignatureInvalid
}

func generateJTI() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

/*
 * getCachedAuthUser 从缓存获取用户基本信息（authUserCacheTTL）
 * 功能：减少 Auth 中间件每次请求的 DB / Redis 往返；权限变更最多延迟 authUserCacheTTL 生效
 */
func getCachedAuthUser(userID string) (*models.User, error) {
	cacheKey := "auth_user:" + userID
	var user models.User
	if cache.C.GetJSON(cacheKey, &user) {
		return &user, nil
	}
	if err := database.DB.First(&user, "id = ?", userID).Error; err != nil {
		return nil, err
	}
	cache.C.SetJSON(cacheKey, user, authUserCacheTTL)
	return &user, nil
}

/*
 * InvalidateAuthUserCache 清除用户认证缓存
 * 功能：用户信息变更（禁用/升降级/删除）后调用，确保立即生效
 */
func InvalidateAuthUserCache(userIDs ...string) {
	for _, uid := range userIDs {
		cache.C.Delete("auth_user:" + uid)
	}
}

/*
 * SecurityHeaders HTTP 安全响应头中间件
 * 功能：添加标准安全头，防止点击劫持、XSS、MIME 嗅探等攻击
 */
func SecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "SAMEORIGIN")
		c.Header("X-XSS-Protection", "1; mode=block")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		/* 直连或反代 HTTPS 时提示浏览器仅走 HTTPS（不默认 includeSubDomains，避免误伤兄弟域名） */
		if isSecureRequest(c) {
			c.Header("Strict-Transport-Security", "max-age=31536000")
		}
		c.Next()
	}
}

/*
 * CORS 跨域中间件
 * 功能：仅允许系统配置的 site_url 对应域名作为合法跨域来源
 *       开发环境下额外允许 localhost 来源
 *       避免任意 Origin 回显导致的 CSRF 风险
 */
func CORS() gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := c.GetHeader("Origin")
		if origin != "" && isAllowedOrigin(c, origin) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Authorization, X-Refresh-Token, X-Secret-Token")
		c.Header("Access-Control-Expose-Headers", "X-Token-Expiring, X-Request-ID, X-Error-ID")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

/*
 * isAllowedOrigin 判断请求 Origin 是否在允许列表中
 * 功能：优先匹配系统 site_url；未配置时仅允许与本请求 Host 一致的 Origin（同域嵌入部署），
 *       不再对任意 Origin 回显（避免 Access-Control-Allow-Credentials 下的反射型风险）。
 *       localhost / 127.0.0.1（http/https）供本地开发。
 */
func isAllowedOrigin(c *gin.Context, origin string) bool {
	if origin == "" {
		return false
	}
	if strings.HasPrefix(origin, "http://localhost") || strings.HasPrefix(origin, "http://127.0.0.1") ||
		strings.HasPrefix(origin, "https://localhost") || strings.HasPrefix(origin, "https://127.0.0.1") {
		return true
	}

	siteURL := strings.TrimSpace(sysconfig.GetValue("site_url"))
	if siteURL != "" {
		siteURL = strings.TrimRight(siteURL, "/")
		parsed, err := url.Parse(siteURL)
		if err != nil {
			return false
		}
		allowedOrigin := parsed.Scheme + "://" + parsed.Host
		return origin == allowedOrigin
	}

	// 未配置 site_url：仅允许浏览器声明的来源主机与当前服务 Host 一致（含端口）
	reqHost := c.Request.Host
	if reqHost == "" {
		return false
	}
	op, err := url.Parse(origin)
	if err != nil || op.Host == "" {
		return false
	}
	return strings.EqualFold(op.Host, reqHost)
}
