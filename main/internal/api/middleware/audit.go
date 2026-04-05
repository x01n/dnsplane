package middleware

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"main/internal/database"
	"main/internal/models"
	"main/internal/utils"

	"github.com/gin-gonic/gin"
)

/*
 * AuditLog 审计日志中间件
 * 功能：自动记录所有写操作(POST/PUT/DELETE)的请求信息
 *       无需每个 handler 手动调用 service.Audit
 */
func AuditLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		// 记录开始时间
		start := time.Now()

		// 执行 handler
		c.Next()

		// 只记录写操作
		method := c.Request.Method
		if method != "POST" && method != "PUT" && method != "DELETE" {
			return
		}

		// 跳过登录/认证相关接口（避免记录密码等敏感数据）
		path := c.Request.URL.Path
		if shouldSkipAudit(path) {
			return
		}

		// 获取用户信息
		userID := c.GetString("user_id")
		username, _ := c.Get("username")
		usernameStr, _ := username.(string)
		if userID == "" {
			return // 未认证的请求不记录
		}

		// 提取操作名称
		action := deriveAction(path, method)

		// 提取域名信息（如果有）
		domain := ""
		if d, exists := c.Get("perm_domain"); exists {
			domain, _ = d.(string)
		}

		// 获取响应状态
		statusCode := c.Writer.Status()
		duration := time.Since(start).Milliseconds()

		// 构建日志数据
		data := action
		if duration > 0 {
			data += " (" + time.Duration(duration*int64(time.Millisecond)).String() + ")"
		}
		if statusCode >= 400 {
			data += fmt.Sprintf(" [HTTP %dxx]", statusCode/100)
		}

		uidUint, _ := strconv.ParseUint(userID, 10, 32)

		// 异步写入数据库（不阻塞响应）
		utils.SafeGo(func() {
			log := models.Log{
				UserID:    uint(uidUint),
				Username:  usernameStr,
				Action:    action,
				Domain:    domain,
				Data:      truncate(data, 500),
				IP:        c.ClientIP(),
				UserAgent: truncate(c.Request.UserAgent(), 200),
				CreatedAt: time.Now(),
			}
			database.LogDB.Create(&log)
		})
	}
}

/*
 * 审计跳过规则预编译
 * 功能：使用 map 和切片在 init 阶段预编译，避免每次请求线性扫描
 */
var (
	auditSkipPrefixes = []string{
		"/api/login",
		"/api/auth/",
		"/api/install",
	}
	auditSkipSuffixes = []string{
		"/list", "/detail", "/get", "/stats", "/status",
		"/info", "/log", "/history", "/uptime", "/overview",
		"/lines", "/providers", "/whois", "/logs",
		"/request", "/error",
	}
	auditSkipExact = map[string]struct{}{
		"/api/dashboard/stats":    {},
		"/api/system/config/get":  {},
		"/api/system/task/status": {},
		"/api/system/cron/get":    {},
		"/api/dns/providers":      {},
		"/api/user/info":          {},
		"/api/user/totp/status":   {},
		"/api/cert/providers":     {},
		"/api/cert/deploy-types":  {},
		"/api/monitor/overview":   {},
		"/api/monitor/status":     {},
	}
)

/*
 * shouldSkipAudit 判断是否应该跳过审计记录
 * 功能：精确匹配使用 map O(1) 查找，前缀/后缀使用预编译切片
 */
func shouldSkipAudit(path string) bool {
	// O(1) 精确匹配
	if _, ok := auditSkipExact[path]; ok {
		return true
	}
	// 前缀匹配
	for _, prefix := range auditSkipPrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	// 后缀匹配
	for _, suffix := range auditSkipSuffixes {
		if strings.HasSuffix(path, suffix) {
			return true
		}
	}
	return false
}

/* deriveAction 从路由路径推导操作名称 */
func deriveAction(path, method string) string {
	// 去除 /api/ 前缀
	path = strings.TrimPrefix(path, "/api/")
	path = strings.TrimPrefix(path, "v1/")

	// 将路径转为操作名称：accounts/create -> accounts_create
	action := strings.ReplaceAll(path, "/", "_")
	action = strings.TrimSuffix(action, "_")

	// 对于 RESTful 风格的 v1 路由，加上方法前缀
	if method == "PUT" {
		action = "update_" + action
	} else if method == "DELETE" {
		action = "delete_" + action
	}

	return truncate(action, 40)
}

/* truncate 截断字符串到指定长度 */
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
