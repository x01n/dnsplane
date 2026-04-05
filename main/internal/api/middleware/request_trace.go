package middleware

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"main/internal/database"
	"main/internal/logstore"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

const (
	RequestIDKey    = "request_id"
	ErrorIDKey      = "error_id"
	DBQueriesKey    = "db_queries"
	ErrorStackKey   = "error_stack"
	ExtraDataKey    = "extra_data"
	MaxBodySize     = 64 * 1024  // 64KB
	MaxResponseSize = 256 * 1024 // 256KB
)

// DBQuery 使用database包中的类型
type DBQuery = database.DBQuery

// generateID 生成短随机ID
func generateID(prefix string) string {
	b := make([]byte, 8)
	rand.Read(b)
	return prefix + hex.EncodeToString(b)
}

var requestIDSeq uint64

// GenerateRequestID 生成请求 ID（非加密随机，仅用于日志关联；避免每请求 crypto/rand 开销）
func GenerateRequestID() string {
	n := atomic.AddUint64(&requestIDSeq, 1)
	return "req_" + strconv.FormatInt(time.Now().UnixNano(), 10) + "_" + strconv.FormatUint(n, 10)
}

// GenerateErrorID 生成错误ID
func GenerateErrorID() string {
	return generateID("err_")
}

const maxRequestLogBodyLen = 8192

// RequestTrace 请求追踪中间件
func RequestTrace() gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/api") {
			c.Next()
			return
		}

		start := time.Now()
		requestID := GenerateRequestID()
		c.Set(RequestIDKey, requestID)
		c.Set(DBQueriesKey, &[]database.DBQuery{})
		c.Header("X-Request-ID", requestID)
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, MaxBodySize))
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// 收集请求头
		headers := make(map[string]string)
		for k, v := range c.Request.Header {
			if len(v) > 0 {
				headers[k] = v[0]
			}
		}
		headersJSON, _ := json.Marshal(headers)

		// 处理请求（不再包装 Writer 复制整段响应体，大幅降低 CPU 与延迟）
		c.Next()

		// 计算耗时
		duration := time.Since(start).Milliseconds()

		statusCode := c.Writer.Status()
		if statusCode == 0 {
			statusCode = http.StatusOK
		}

		// 获取用户信息
		userID := AuthUserID(c)
		username := c.GetString("username")

		// 判断是否错误
		isError := statusCode >= 400 || c.GetBool("has_error")
		errorID := ""
		errorMsg := ""
		errorStack := ""

		if isError {
			errorID = c.GetString(ErrorIDKey)
			if errorID == "" {
				errorID = GenerateErrorID()
			}
			c.Header("X-Error-ID", errorID)
			errorMsg = c.GetString("error_msg")
			errorStack = c.GetString(ErrorStackKey)
		}

		// 处理请求体 - 优先使用解密后的数据
		bodyStr := string(bodyBytes)
		if decryptedData := GetDecryptedData(c); decryptedData != nil {
			if jsonBytes, err := json.Marshal(decryptedData); err == nil {
				bodyStr = string(jsonBytes)
			}
		}
		if len(bodyStr) > maxRequestLogBodyLen {
			bodyStr = truncateString(bodyStr, maxRequestLogBodyLen)
		}

		// 成功响应不落库 response 正文（此前会缓冲整段响应，开销大）；错误时可带 handler 提供的原始结构
		responseStr := ""
		if isError {
			if originalResp := GetOriginalResponse(c); originalResp != nil {
				if jsonBytes, err := json.Marshal(originalResp); err == nil {
					responseStr = truncateString(string(jsonBytes), MaxResponseSize)
				}
			}
		}

		// 获取数据库查询记录
		var dbQueriesJSON string
		var dbQueryTime int64
		if queries, ok := c.Get(DBQueriesKey); ok {
			if q, ok := queries.(*[]database.DBQuery); ok && len(*q) > 0 {
				for _, query := range *q {
					dbQueryTime += query.Duration
				}
				data, _ := json.Marshal(*q)
				dbQueriesJSON = string(data)
			}
		}

		// 获取额外数据
		var extraJSON string
		if extra, ok := c.Get(ExtraDataKey); ok {
			data, _ := json.Marshal(extra)
			extraJSON = string(data)
		}

		// 保存请求日志
		log := models.RequestLog{
			RequestID:   requestID,
			ErrorID:     errorID,
			UserID:      userID,
			Username:    username,
			Method:      c.Request.Method,
			Path:        c.Request.URL.Path,
			Query:       c.Request.URL.RawQuery,
			Body:        bodyStr,
			Headers:     string(headersJSON),
			IP:          c.ClientIP(),
			UserAgent:   truncateString(c.Request.UserAgent(), 500),
			StatusCode:  statusCode,
			Response:    responseStr,
			Duration:    duration,
			IsError:     isError,
			ErrorMsg:    errorMsg,
			ErrorStack:  errorStack,
			DBQueries:   dbQueriesJSON,
			DBQueryTime: dbQueryTime,
			Extra:       extraJSON,
			CreatedAt:   time.Now(),
		}

		// 异步：先落库拿到自增 ID，再写入 Redis（与管理端 / 排障字段一致）
		go func(l models.RequestLog) {
			_ = database.RequestDB.Create(&l).Error
			if logstore.Store != nil && logstore.Store.IsRedis() {
				logstore.Store.SaveRequestLog(l)
			}
		}(log)
	}
}

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "...[truncated]"
}

// SetError 标记请求为错误状态，记录错误信息和调用堆栈
// 多次调用只保留第一次的 errorID 和堆栈，msg 会追加
func SetError(c *gin.Context, msg string) string {
	existingID := c.GetString(ErrorIDKey)
	if existingID != "" {
		// 已有错误，追加消息
		existing := c.GetString("error_msg")
		if existing != "" && existing != msg {
			c.Set("error_msg", existing+"; "+msg)
		}
		return existingID
	}

	errorID := GenerateErrorID()
	c.Set(ErrorIDKey, errorID)
	c.Set("has_error", true)
	c.Set("error_msg", msg)
	buf := make([]byte, 4096)
	n := runtime.Stack(buf, false)
	c.Set(ErrorStackKey, string(buf[:n]))
	return errorID
}
func GetRequestID(c *gin.Context) string {
	if id, exists := c.Get(RequestIDKey); exists {
		return id.(string)
	}
	return ""
}

// AddDBQuery 添加数据库查询记录
func AddDBQuery(c *gin.Context, sql string, duration time.Duration, rows int64, err error) {
	if queries, ok := c.Get(DBQueriesKey); ok {
		if q, ok := queries.(*[]database.DBQuery); ok {
			query := database.DBQuery{
				SQL:      truncateString(sql, 2000),
				Duration: duration.Milliseconds(),
				Rows:     rows,
			}
			if err != nil {
				query.Error = err.Error()
			}
			*q = append(*q, query)
		}
	}
}

// SetExtra 设置额外信息
func SetExtra(c *gin.Context, key string, value interface{}) {
	var extra map[string]interface{}
	if e, ok := c.Get(ExtraDataKey); ok {
		extra = e.(map[string]interface{})
	} else {
		extra = make(map[string]interface{})
	}
	extra[key] = value
	c.Set(ExtraDataKey, extra)
}
