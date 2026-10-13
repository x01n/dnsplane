package apiv1

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
)

/*
 * API Key 签名认证
 *
 * 请求头：
 *   X-API-UID        站内用户数字 ID
 *   X-API-Timestamp  Unix 秒级时间戳
 *   X-API-Sign       HMAC-SHA256(uid "\n" timestamp "\n" METHOD "\n" PATH, api_key) 的 hex
 *
 * 待签串含方法与路径，与旧版 PHP 的 md5(uid+timestamp+apikey) 相比多绑定了
 * 「这一次调用要打到哪个接口」，同一个签名无法被挪用到另一条路径上
 *
 * 时间戳窗口 ±5 分钟，超出即判过期，防止抓包后重放
 */
const (
	apiKeyTimestampTolerance = 5 * time.Minute
	headerAPIUID             = "X-API-UID"
	headerAPITimestamp       = "X-API-Timestamp"
	headerAPISign            = "X-API-Sign"
)

/* APIKeyAuth 校验 API Key 签名并把身份写入上下文 */
func APIKeyAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		uid := strings.TrimSpace(c.GetHeader(headerAPIUID))
		timestamp := strings.TrimSpace(c.GetHeader(headerAPITimestamp))
		sign := strings.TrimSpace(c.GetHeader(headerAPISign))

		if uid == "" || timestamp == "" || sign == "" {
			Fail(c, CodeUnauthorized, "")
			c.Abort()
			return
		}

		apiUID, err := strconv.ParseUint(uid, 10, 32)
		if err != nil || apiUID == 0 {
			Failf(c, CodeUnauthorized, "无效的用户 ID: %s", uid)
			c.Abort()
			return
		}

		ts, err := strconv.ParseInt(timestamp, 10, 64)
		if err != nil {
			Fail(c, CodeTimestampInvalid, "")
			c.Abort()
			return
		}
		if math.Abs(float64(time.Now().Unix()-ts)) > apiKeyTimestampTolerance.Seconds() {
			Fail(c, CodeTimestampInvalid, "")
			c.Abort()
			return
		}

		var user models.User
		if err := database.WithContext(c).
			Where("id = ?", uint(apiUID)).First(&user).Error; err != nil {
			Fail(c, CodeUserNotFound, "")
			c.Abort()
			return
		}
		if !user.IsAPI {
			Fail(c, CodeAPIKeyMissing, "")
			c.Abort()
			return
		}
		if user.Status != 1 {
			Fail(c, CodeAccountDisabled, "")
			c.Abort()
			return
		}

		message := uid + "\n" + timestamp + "\n" + c.Request.Method + "\n" + c.Request.URL.Path
		expected := signWithKey(message, user.APIKey)
		// 常量时间比较，避免逐字节比较泄露签名前缀
		if !hmac.Equal([]byte(sign), []byte(expected)) {
			Fail(c, CodeSignMismatch, "")
			c.Abort()
			return
		}

		/*
		 * user_id 存字符串：各层 SQL 占位符与 GetString("user_id") 都按字符串取，
		 * 存 uint 会让 GetString 拿到空串（与控制台 Auth 中间件的既有约定一致）
		 */
		c.Set("user_id", strconv.FormatUint(uint64(user.ID), 10))
		c.Set("username", user.Username)
		c.Set("level", user.Level)
		c.Set("auth_type", "apikey")
		// 模块权限与控制台同一套判定，注入后 UserModuleAllowed 才读得到
		middleware.ApplyUserPermissionModules(c, user.Permissions)

		c.Next()
	}
}

/* signWithKey 计算 HMAC-SHA256 签名的十六进制串 */
func signWithKey(message, key string) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}

/*
 * JSONBodyToDecryptedData 把 JSON 体放进 decrypted_data，并保证请求体可被再次读取
 *
 * 控制台的 handler 一律经 middleware.BindDecryptedData 取参数，它优先读
 * decrypted_data、读不到才回落 ShouldBindJSON。对外的 JSON 请求直接写入该键，
 * 既让 v1 自己的 handler 与控制台 handler 用同一种取参方式，也避免两套读法分叉
 *
 * 读完必须立刻还原请求体：另有相当一批控制台 handler 绕开 BindDecryptedData
 * 直接调 c.ShouldBindJSON（证书账户与订单的新增、用户管理、系统配置更新、
 * 全部 Cloudflare 接口都属此类）。body 已被本中间件读到 EOF 时，它们拿到的是
 * io.EOF，字段全为零值且多数不报错——表现为「接口回成功但什么也没改」，
 * 比直接报错更难发现。还原方式与 RequestTrace 的既有做法一致，避免两处对
 * body 的处置分叉
 *
 * 读取不设长度上限：最外层的 RequestTrace 已按 MaxBodySize 截断过，
 * 此处再设限只会让改动后的可读范围比改动前（ShouldBindJSON 读全量）更小
 *
 * 只在 POST/PUT/PATCH 上动作；GET 无体，塞入空 map 反而会让 handler 误以为有入参
 */
func JSONBodyToDecryptedData() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodPost, http.MethodPut, http.MethodPatch:
		default:
			c.Next()
			return
		}

		body := map[string]interface{}{}
		if c.Request.Body != nil {
			raw, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(raw))
			// 空体或非 JSON 对象都不阻断：decrypted_data 留作空 map，
			// 由 handler 按缺参处理，报错更贴近实际
			if len(bytes.TrimSpace(raw)) > 0 {
				_ = json.Unmarshal(raw, &body)
			}
		}
		c.Set("decrypted_data", body)
		c.Next()
	}
}
