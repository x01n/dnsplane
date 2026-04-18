package middleware

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
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

// maxSuccessResponseCapture 成功响应落库正文上限（仅截取前若干字节，便于管理端对照）
const maxSuccessResponseCapture = 16384

// respCaptureWriter 在不影响真实输出的前提下截取响应体片段供请求日志使用
type respCaptureWriter struct {
	gin.ResponseWriter
	buf *bytes.Buffer
	lim int
}

func (w *respCaptureWriter) Write(b []byte) (int, error) {
	if w.buf != nil && w.buf.Len() < w.lim {
		rest := w.lim - w.buf.Len()
		if rest > len(b) {
			rest = len(b)
		}
		if rest > 0 {
			_, _ = w.buf.Write(b[:rest])
		}
	}
	return w.ResponseWriter.Write(b)
}

func (w *respCaptureWriter) WriteString(s string) (int, error) {
	if w.buf != nil && w.buf.Len() < w.lim {
		rest := w.lim - w.buf.Len()
		if rest > len(s) {
			rest = len(s)
		}
		if rest > 0 {
			_, _ = w.buf.WriteString(s[:rest])
		}
	}
	return w.ResponseWriter.WriteString(s)
}

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
		unbindGinTrace := database.BindRequestGinForDBTrace(c)
		defer unbindGinTrace()
		c.Header("X-Request-ID", requestID)
		var bodyBytes []byte
		if c.Request.Body != nil {
			bodyBytes, _ = io.ReadAll(io.LimitReader(c.Request.Body, MaxBodySize))
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
		}

		// 收集请求头（含 Host；多值头合并，避免 HTTP/2 等场景下漏记）
		headers := make(map[string]string)
		for k, vals := range c.Request.Header {
			if len(vals) == 0 {
				continue
			}
			if len(vals) == 1 {
				headers[k] = vals[0]
			} else {
				headers[k] = strings.Join(vals, ", ")
			}
		}
		if h := c.Request.Host; h != "" {
			headers["Host"] = h
		}
		// 敏感 header 掩码：Authorization / Cookie / X-Refresh-Token 等不落库明文
		headersJSONStr := SanitizeHeadersForLog(headers)

		captureBuf := &bytes.Buffer{}
		c.Writer = &respCaptureWriter{ResponseWriter: c.Writer, buf: captureBuf, lim: maxSuccessResponseCapture}
		c.Next()
		capturedResp := captureBuf.String()

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

		// 处理请求体：解密明文优先；表单 URL 编码解析为 JSON 便于阅读；否则原始字节
		bodyStr := string(bodyBytes)
		if decryptedData := GetDecryptedData(c); decryptedData != nil {
			if jsonBytes, err := json.Marshal(decryptedData); err == nil {
				bodyStr = string(jsonBytes)
			}
		} else if len(bodyBytes) > 0 {
			ct := strings.ToLower(c.Request.Header.Get("Content-Type"))
			if strings.Contains(ct, "application/x-www-form-urlencoded") {
				if vals, err := url.ParseQuery(string(bodyBytes)); err == nil && len(vals) > 0 {
					if jsonBytes, err := json.Marshal(vals); err == nil {
						bodyStr = string(jsonBytes)
					}
				}
			}
		}
		// 敏感字段脱敏：password / token / access_key 等统一替换为 ***REDACTED***
		bodyStr = SanitizeBodyForLog(bodyStr, c.Request.Header.Get("Content-Type"))
		if len(bodyStr) > maxRequestLogBodyLen {
			bodyStr = truncateString(bodyStr, maxRequestLogBodyLen)
		}

		// 响应：EncryptedResponse/SuccessResponse 已写入 original_response，成功与失败都应优先落库（避免仅依赖 Writer 截取）
		responseStr := ""
		if originalResp := GetOriginalResponse(c); originalResp != nil {
			if jsonBytes, err := json.Marshal(originalResp); err == nil {
				responseStr = truncateString(string(jsonBytes), MaxResponseSize)
			}
		}
		if responseStr == "" {
			if isError && capturedResp != "" {
				responseStr = truncateString(capturedResp, MaxResponseSize)
			} else if !isError && capturedResp != "" {
				responseStr = truncateString(capturedResp, maxSuccessResponseCapture)
			}
		}
		// 响应体里可能回显 token（如登录 204、refresh），同样脱敏
		responseStr = SanitizeBodyForLog(responseStr, c.Writer.Header().Get("Content-Type"))

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
			Headers:     headersJSONStr,
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
