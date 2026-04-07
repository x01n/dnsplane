package middleware

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"main/internal/logger"
	"main/internal/utils"

	"github.com/gin-gonic/gin"
)

// DecryptAndVerify 解密并验证签名的中间件
func DecryptAndVerify() gin.HandlerFunc {
	return func(c *gin.Context) {
		var encReq struct {
			Key  string `json:"key"`
			IV   string `json:"iv"`
			Data string `json:"data"`
		}

		if err := c.ShouldBindJSON(&encReq); err != nil || encReq.Key == "" || encReq.IV == "" || encReq.Data == "" {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "请求格式错误"})
			c.Abort()
			return
		}

		payload := &utils.EncryptedPayload{
			Key:  encReq.Key,
			IV:   encReq.IV,
			Data: encReq.Data,
		}

		result, err := utils.HybridDecryptWithKey(payload)
		if err != nil {
			logger.Warn("请求解密失败: %v", err)
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "请求解密失败"})
			c.Abort()
			return
		}

		c.Set("aes_key", result.AESKey)

		// 从Authorization header获取token用于派生签名密钥
		var signKey []byte
		if authHeader := c.GetHeader("Authorization"); authHeader != "" {
			if parts := splitAuthHeader(authHeader); len(parts) == 2 && parts[0] == "Bearer" {
				accessToken := parts[1]
				refreshToken := c.GetHeader("X-Refresh-Token")
				secretToken := c.GetHeader("X-Secret-Token")
				if refreshToken != "" && secretToken != "" {
					signKey = utils.DeriveSignKey(refreshToken, accessToken, secretToken)
				}
			}
		}

		// 解析并验证签名请求
		_, dataMap, err := utils.ParseSignedRequestWithKey(result.Plaintext, signKey)
		if err != nil {
			logger.Warn("请求签名验证失败: %v", err)
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": err.Error()})
			c.Abort()
			return
		}

		c.Set("decrypted_data", dataMap)
		c.Next()
	}
}

func splitAuthHeader(header string) []string {
	for i := 0; i < len(header); i++ {
		if header[i] == ' ' {
			return []string{header[:i], header[i+1:]}
		}
	}
	return []string{header}
}

// GetDecryptedData 从context获取解密后的数据
func GetDecryptedData(c *gin.Context) map[string]interface{} {
	if data, exists := c.Get("decrypted_data"); exists {
		if m, ok := data.(map[string]interface{}); ok {
			return m
		}
	}
	return nil
}

// GetAESKey 从context获取AES密钥
func GetAESKey(c *gin.Context) []byte {
	if key, exists := c.Get("aes_key"); exists {
		if k, ok := key.([]byte); ok {
			return k
		}
	}
	return nil
}

// BindDecryptedData 将解密数据绑定到结构体；无加密时：GET/HEAD 用 Query，其余用 JSON body（与前端 GET+Query、POST+JSON 一致）
func BindDecryptedData(c *gin.Context, v interface{}) error {
	data := GetDecryptedData(c)
	if data == nil {
		if c.Request.Method == http.MethodGet || c.Request.Method == http.MethodHead {
			return c.ShouldBindQuery(v)
		}
		if err := c.ShouldBindJSON(v); err != nil {
			// POST 无 body（如 /accounts/:id/delete）时 ShouldBindJSON 报 EOF，路径参数由 handler 补全
			if errors.Is(err, io.EOF) {
				return nil
			}
			if msg := strings.ToLower(err.Error()); strings.Contains(msg, "unexpected end of json input") {
				return nil
			}
			return err
		}
		return nil
	}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	return json.Unmarshal(jsonBytes, v)
}

// BindJSONFlexible 优先绑定 DecryptAndVerify 放入上下文的明文；否则按普通 JSON body 绑定（便于未启用加密链路的客户端）
func BindJSONFlexible(c *gin.Context, v interface{}) error {
	if data := GetDecryptedData(c); data != nil {
		jsonBytes, err := json.Marshal(data)
		if err != nil {
			return err
		}
		return json.Unmarshal(jsonBytes, v)
	}
	return c.ShouldBindJSON(v)
}

// EncryptedResponse 发送加密并混淆的响应
func EncryptedResponse(c *gin.Context, data interface{}) {
	// 保存原始响应数据用于日志记录
	c.Set("original_response", data)

	aesKey := GetAESKey(c)
	if aesKey == nil {
		c.JSON(http.StatusOK, data)
		return
	}

	encrypted, err := utils.EncryptAndObfuscate(data, aesKey)
	if err != nil {
		c.JSON(http.StatusOK, data)
		return
	}
	c.JSON(http.StatusOK, encrypted)
}

// GetOriginalResponse 获取原始响应数据
func GetOriginalResponse(c *gin.Context) interface{} {
	if data, exists := c.Get("original_response"); exists {
		return data
	}
	return nil
}

// SuccessResponse 成功响应
func SuccessResponse(c *gin.Context, data interface{}) {
	resp := gin.H{"code": 0, "data": data}
	EncryptedResponse(c, resp)
}

// SuccessMsg 成功消息响应
func SuccessMsg(c *gin.Context, msg string) {
	resp := gin.H{"code": 0, "msg": msg}
	EncryptedResponse(c, resp)
}

// ErrorResponse 错误响应（自动记录错误上下文和堆栈）
func ErrorResponse(c *gin.Context, msg string) {
	SetError(c, msg)
	resp := gin.H{"code": -1, "msg": msg}
	EncryptedResponse(c, resp)
}

// ErrorResponseWithCode 带错误码的错误响应（自动记录错误上下文和堆栈）
func ErrorResponseWithCode(c *gin.Context, code int, msg string) {
	SetError(c, msg)
	resp := gin.H{"code": code, "msg": msg}
	EncryptedResponse(c, resp)
}
