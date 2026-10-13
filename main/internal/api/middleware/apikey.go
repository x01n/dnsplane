package middleware

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"main/internal/database"
	"main/internal/models"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	/* APIKeyTimestampTolerance 请求时间戳最大允许偏差（±5 分钟） */
	APIKeyTimestampTolerance = 5 * time.Minute
)

/*
 * JSONBodyToDecryptedData 将 JSON body 存入 decrypted_data
 * 功能：API Key 认证路由复用 handler 层的 decrypted_data 读取逻辑
 */
func JSONBodyToDecryptedData() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "POST" {
			var body map[string]interface{}
			if err := c.ShouldBindJSON(&body); err == nil && body != nil {
				c.Set("decrypted_data", body)
			}
		}
		c.Next()
	}
}

/*
 * APIKeyAuth API Key HMAC-SHA256 签名认证中间件
 * 功能：验证 X-API-UID / X-API-Timestamp / X-API-Sign 三个请求头
 * 安全：时间戳防重放（±5分钟）+ HMAC 常量时间比较防时序攻击
 */
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := c.GetHeader("X-API-UID")
		timestamp := c.GetHeader("X-API-Timestamp")
		sign := c.GetHeader("X-API-Sign")

		/* 1. 检查必要的认证头 */
		if uid == "" || timestamp == "" || sign == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "缺少API认证头"})
			c.Abort()
			return
		}

		uid = strings.TrimSpace(uid)
		apiUID, err := strconv.ParseUint(uid, 10, 32)
		if err != nil || apiUID == 0 {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "无效的用户ID"})
			c.Abort()
			return
		}

		/* 3. 校验时间戳在 ±5 分钟内，防重放攻击 */
		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "无效的时间戳"})
			c.Abort()
			return
		}

		diff := math.Abs(float64(time.Now().Unix() - ts))
		if diff > APIKeyTimestampTolerance.Seconds() {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "请求已过期"})
			c.Abort()
			return
		}

		/* 4. 查询 API 用户（X-API-UID 为站内用户数字 ID） */
		var user models.User
		if err := database.WithContext(c).
			Where("id = ? AND is_api = ? AND status = ?", uint(apiUID), true, 1).
			First(&user).Error; err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "API用户不存在或未启用"})
			c.Abort()
			return
		}

		/* 5. HMAC-SHA256 签名验证（常量时间比较，防时序攻击） */
		message := uid + "\n" + timestamp + "\n" + c.Request.Method + "\n" + c.Request.URL.Path
		expectedSign := computeHMACSHA256(message, user.APIKey)

		if !hmac.Equal([]byte(sign), []byte(expectedSign)) {
			c.JSON(http.StatusUnauthorized, gin.H{"code": 401, "msg": "签名验证失败"})
			c.Abort()
			return
		}
		c.Set("user_id", strconv.FormatUint(uint64(user.ID), 10))
		c.Set("username", user.Username)
		c.Set("level", user.Level)

		c.Next()
	}
}

/* computeHMACSHA256 计算 HMAC-SHA256 签名 */
func computeHMACSHA256(message, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
