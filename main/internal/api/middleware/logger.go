package middleware

import (
	"fmt"
	"main/internal/logger"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

// 慢请求阈值
const slowRequestThreshold = 3 * time.Second

// ANSI 颜色代码
const (
	colorReset   = "\033[0m"
	colorRed     = "\033[31m"
	colorGreen   = "\033[32m"
	colorYellow  = "\033[33m"
	colorBlue    = "\033[34m"
	colorMagenta = "\033[35m"
	colorCyan    = "\033[36m"
	colorWhite   = "\033[37m"
	colorGray    = "\033[90m"
)

// 静态文件扩展名
var staticExtensions = []string{
	".html", ".css", ".js", ".map",
	".png", ".jpg", ".jpeg", ".gif", ".svg", ".ico", ".webp",
	".woff", ".woff2", ".ttf", ".eot",
	".txt", ".xml",
}

// 需要过滤的路径前缀
var filterPaths = []string{
	"/_next/",
	"/icons/",
	"/favicon",
	"/login",
	"/dashboard",
}

// shouldSkipLog 判断是否应该跳过日志记录
func shouldSkipLog(path string) bool {
	// API 请求始终记录日志
	if strings.HasPrefix(path, "/api/") {
		return false
	}

	// 检查路径前缀
	for _, prefix := range filterPaths {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}

	// 检查文件扩展名
	for _, ext := range staticExtensions {
		if strings.HasSuffix(path, ext) {
			return true
		}
	}

	return false
}

// getStatusColor 根据状态码获取颜色
func getStatusColor(status int) string {
	switch {
	case status >= 200 && status < 300:
		return colorGreen
	case status >= 300 && status < 400:
		return colorCyan
	case status >= 400 && status < 500:
		return colorYellow
	default:
		return colorRed
	}
}

// getMethodColor 根据请求方法获取颜色
func getMethodColor(method string) string {
	switch method {
	case "GET":
		return colorBlue
	case "POST":
		return colorGreen
	case "PUT":
		return colorYellow
	case "DELETE":
		return colorRed
	case "PATCH":
		return colorMagenta
	default:
		return colorWhite
	}
}

// extractModule 从API路径提取模块名
func extractModule(path string) string {
	// 移除 /api/ 前缀
	trimmed := strings.TrimPrefix(path, "/api/")
	trimmed = strings.TrimPrefix(trimmed, "v1/")

	// 按 / 分割
	parts := strings.Split(trimmed, "/")
	if len(parts) == 0 {
		return "api"
	}

	// 模块映射
	moduleMap := map[string]string{
		"users":        "user",
		"accounts":     "account",
		"domains":      "domain",
		"cert":         "cert",
		"monitor":      "monitor",
		"system":       "system",
		"logs":         "log",
		"login":        "auth",
		"auth":         "auth",
		"install":      "auth",
		"user":         "auth",
		"dashboard":    "dashboard",
		"dns":          "dns",
		"request-logs": "request-log",
		"quicklogin":   "auth",
		"logout":       "auth",
	}

	firstPart := parts[0]
	if module, ok := moduleMap[firstPart]; ok {
		return module
	}

	return firstPart
}

// formatLatency 格式化耗时显示
func formatLatency(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.2fµs", float64(d.Microseconds()))
	} else if d < time.Second {
		return fmt.Sprintf("%.2fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

// Logger 自定义日志中间件
// - 过滤静态文件和HEAD请求
// - 控制台彩色输出 + 日志文件结构化记录
// - 包含请求ID关联、错误标记、慢请求告警
func Logger() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 跳过 HEAD 请求
		if c.Request.Method == "HEAD" {
			c.Next()
			return
		}

		path := c.Request.URL.Path

		// 跳过静态资源请求
		if shouldSkipLog(path) {
			c.Next()
			return
		}

		start := time.Now()

		// 处理请求
		c.Next()

		// 计算耗时
		latency := time.Since(start)
		status := c.Writer.Status()
		method := c.Request.Method
		timeStr := time.Now().Format("15:04:05")
		latencyStr := formatLatency(latency)
		statusStr := fmt.Sprintf("%d", status)

		// ---- 控制台彩色输出 ----
		statusColor := getStatusColor(status)
		methodColor := getMethodColor(method)

		// 基础格式: 14:20:24 200 1.99ms POST /api/domains/list
		consoleLine := fmt.Sprintf("%s%s%s %s%-3s%s %s%10s%s %s%-6s%s %s%s%s",
			colorGray, timeStr, colorReset,
			statusColor, statusStr, colorReset,
			colorGray, latencyStr, colorReset,
			methodColor, method, colorReset,
			colorWhite, path, colorReset,
		)

		// 如果有错误，在末尾追加错误标识
		hasError := c.GetBool("has_error")
		errorID := c.GetString(ErrorIDKey)
		if hasError && errorID != "" {
			consoleLine += fmt.Sprintf(" %s[%s]%s", colorRed, errorID, colorReset)
		}

		fmt.Fprintln(os.Stdout, consoleLine)

		// ---- 日志文件结构化记录（仅写文件，避免与控制台重复）----
		if strings.HasPrefix(path, "/api") {
			reqID := c.GetString(RequestIDKey)
			clientIP := c.ClientIP()
			module := extractModule(path)

			if status >= 500 || hasError {
				// 服务器错误 → ERROR
				errMsg := c.GetString("error_msg")
				logger.FileError("[%s] %s %s %d %s ip=%s err=%s rid=%s",
					module, method, path, status, latencyStr, clientIP, errMsg, reqID)
			} else if status >= 400 {
				// 客户端错误 → WARN
				errMsg := c.GetString("error_msg")
				logger.FileWarn("[%s] %s %s %d %s ip=%s err=%s rid=%s",
					module, method, path, status, latencyStr, clientIP, errMsg, reqID)
			} else if latency >= slowRequestThreshold {
				// 慢请求 → WARN
				logger.FileWarn("[%s] SLOW %s %s %d %s ip=%s rid=%s",
					module, method, path, status, latencyStr, clientIP, reqID)
			}
			// 正常快速请求不写日志文件（控制台已输出，避免日志文件过大）
		}
	}
}
