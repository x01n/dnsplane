package api

import (
	"embed"
	"io/fs"
	"main/internal/api/handler"
	"main/internal/api/middleware"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

func SetupRouter(staticFS embed.FS) *gin.Engine {
	r := gin.New()
	r.RedirectTrailingSlash = false
	r.RedirectFixedPath = false
	r.Use(gin.Recovery())
	r.Use(middleware.Logger())
	// CORS 仅作用于 /api：静态与页面路由不再跑 Origin 校验与 Expose-Headers，减少每请求开销
	// 业务接口仅注册 GET（查询）与 POST（创建、更新、删除统一 POST；删除路径为 .../delete）
	api := r.Group("/api")
	api.Use(middleware.CORS())
	// 请求追踪与落库（含 X-Request-ID、body/headers、db_queries）；须在 CORS 之后以便 OPTIONS 不记日志
	api.Use(middleware.RequestTrace())
	// CSRF double-submit cookie：GET 自动下发 _csrf cookie，写请求校验 X-CSRF-Token
	api.Use(middleware.CSRF())
	// 操作审计写入 LogDB（与 service.Audit、GetLogs 一致）
	api.Use(middleware.AuditLog())
	{
		// CSRF token 获取端点（前端初始化时拉一次即可）
		api.GET("/csrf", middleware.IssueCSRFHandler)

		api.POST("/login", handler.Login)
		api.GET("/auth/captcha", handler.GetCaptcha)
		api.GET("/auth/config", handler.GetAuthConfig)
		api.POST("/install", handler.Install)
		api.GET("/install/status", handler.InstallStatus)

		// 公开的密码/TOTP重置接口（无需认证）
		api.POST("/auth/forgot-password", handler.ForgotPassword)
		api.POST("/auth/reset-password", handler.ResetPassword)
		api.POST("/auth/forgot-totp", handler.ForgotTOTP)
		api.POST("/auth/reset-totp", handler.ResetTOTP)
		api.GET("/auth/oauth/providers", handler.GetOAuthProviders)
		api.GET("/auth/oauth/:provider/login", handler.OAuthLogin)
		api.GET("/auth/oauth/:provider/callback", handler.OAuthCallback)
		api.POST("/auth/refresh", handler.RefreshToken)
		api.POST("/auth/send-code", handler.SendAuthCode)
		api.POST("/auth/register", handler.Register)
		api.POST("/auth/magic-link", handler.RequestMagicLink)
		api.POST("/auth/magic-link/totp", handler.MagicLinkVerifyTotp)

		auth := api.Group("")
		auth.Use(middleware.Auth())
		{
			auth.GET("/user/info", handler.GetUserInfo)
			auth.POST("/user/password", handler.ChangePassword)
			auth.POST("/user/bind-email/send-code", handler.SendBindEmailCode)
			auth.POST("/user/bind-email", handler.BindEmail)
			auth.POST("/logout", handler.Logout)

			auth.GET("/accounts", handler.GetAccounts)
			auth.POST("/accounts", handler.CreateAccount)
			auth.POST("/accounts/:id/check", handler.CheckAccount)
			auth.POST("/accounts/:id/delete", handler.DeleteAccount)
			auth.POST("/accounts/:id", handler.UpdateAccount)
			auth.GET("/accounts/:id/domains", handler.GetAccountDomainList)

			auth.GET("/domains", handler.GetDomains)
			auth.GET("/domains/:id", handler.GetDomainDetail)
			auth.POST("/domains", handler.CreateDomain)
			auth.POST("/domains/sync", handler.SyncDomains)
			auth.POST("/domains/batch", handler.BatchDomainAction)
			auth.POST("/domains/:id/update-expire", handler.UpdateDomainExpire)
			auth.POST("/domains/batch/update-expire", handler.BatchUpdateDomainExpire)
			auth.POST("/domains/:id/delete", handler.DeleteDomain)
			auth.POST("/domains/:id", handler.UpdateDomain)

			auth.GET("/domains/:id/records", handler.GetRecords)
			auth.POST("/domains/:id/records", handler.CreateRecord)
			auth.POST("/domains/:id/records/batch", handler.BatchAddRecords)
			auth.POST("/domains/:id/records/batch/edit", handler.BatchEditRecords)
			auth.POST("/domains/:id/records/batch/action", handler.BatchActionRecords)
			auth.POST("/domains/:id/records/:recordId/status", handler.SetRecordStatus)
			auth.POST("/domains/:id/records/:recordId/delete", handler.DeleteRecord)
			auth.POST("/domains/:id/records/:recordId", handler.UpdateRecord)
			auth.GET("/domains/:id/lines", handler.GetRecordLines)
			auth.POST("/domains/:id/whois", handler.QueryWhois)

			auth.GET("/monitor/tasks", handler.GetMonitorTasks)
			auth.POST("/monitor/tasks", handler.CreateMonitorTask)
			auth.POST("/monitor/tasks/:id/delete", handler.DeleteMonitorTask)
			auth.POST("/monitor/tasks/:id", handler.UpdateMonitorTask)
			auth.POST("/monitor/tasks/:id/toggle", handler.ToggleMonitorTask)
			auth.POST("/monitor/tasks/:id/switch", handler.SwitchMonitorTask)
			auth.GET("/monitor/tasks/:id/logs", handler.GetMonitorLogs)
			auth.GET("/monitor/tasks/:id/history", handler.GetMonitorHistory)
			auth.GET("/monitor/tasks/:id/uptime", handler.GetMonitorUptime)
			auth.GET("/monitor/tasks/:id/resolve-status", handler.GetResolveStatus)
			auth.GET("/monitor/overview", handler.GetMonitorOverview)
			auth.POST("/monitor/tasks/batch", handler.BatchCreateMonitorTasks)
			auth.POST("/monitor/tasks/auto-create", handler.AutoCreateMonitorTask)
			auth.POST("/monitor/lookup", handler.LookupRecord)
			auth.GET("/monitor/status", handler.GetMonitorStatus)

			auth.GET("/cert/accounts", handler.GetCertAccounts)
			auth.POST("/cert/accounts", handler.CreateCertAccount)
			auth.POST("/cert/accounts/:id/delete", handler.DeleteCertAccount)
			auth.POST("/cert/accounts/:id", handler.UpdateCertAccount)

			auth.GET("/cert/orders", handler.GetCertOrders)
			auth.POST("/cert/orders", handler.CreateCertOrder)
			auth.POST("/cert/orders/:id/process", handler.ProcessCertOrder)
			auth.POST("/cert/orders/:id/delete", handler.DeleteCertOrder)
			auth.GET("/cert/orders/:id/log", handler.GetCertOrderLog)
			auth.GET("/cert/orders/:id/detail", handler.GetCertOrderDetail)
			auth.GET("/cert/orders/:id/download", handler.DownloadCertOrder)
			auth.POST("/cert/orders/:id/auto", handler.ToggleCertOrderAuto)

			auth.GET("/cert/deploys", handler.GetCertDeploys)
			auth.POST("/cert/deploys", handler.CreateCertDeploy)
			auth.POST("/cert/deploys/:id/delete", handler.DeleteCertDeploy)
			auth.POST("/cert/deploys/:id", handler.UpdateCertDeploy)
			auth.POST("/cert/deploys/:id/process", handler.ProcessCertDeploy)

			auth.GET("/users", handler.GetUsers)
			auth.POST("/users", handler.CreateUser)
			auth.POST("/users/:id/delete", handler.DeleteUser)
			auth.POST("/users/:id", handler.UpdateUser)
			auth.GET("/users/:id/permissions", handler.GetUserPermissions)
			auth.POST("/users/:id/permissions", handler.AddUserPermission)
			auth.POST("/users/:id/permissions/:permId/delete", handler.DeleteUserPermission)
			auth.POST("/users/:id/permissions/:permId", handler.UpdateUserPermission)
			auth.POST("/users/:id/reset-apikey", handler.ResetAPIKey)
			auth.POST("/users/:id/send-reset", handler.AdminSendResetEmail)
			auth.POST("/users/:id/reset-totp", handler.AdminResetTOTP)

			auth.GET("/logs", handler.GetLogs)
			auth.GET("/logs/:id", handler.GetLogDetail)

			auth.GET("/system/config", handler.GetSystemConfig)
			auth.POST("/system/config", handler.UpdateSystemConfig)

			auth.GET("/dns/providers", handler.GetDNSProviders)
			auth.GET("/cert/providers", handler.GetCertProviders)

			auth.GET("/dashboard/stats", handler.GetDashboardStats)
			auth.GET("/dashboard/system/info", handler.GetSystemInfo)
			auth.POST("/system/mail/test", handler.TestMailNotification)
			auth.POST("/system/telegram/test", handler.TestTelegramNotification)
			auth.POST("/system/webhook/test", handler.TestWebhookNotification)
			auth.POST("/system/cache/clear", handler.ClearCache)

			auth.POST("/system/proxy/test", handler.TestProxy)
			auth.GET("/system/task/status", handler.GetTaskStatus)
			auth.GET("/system/cron", handler.GetCronConfig)
			auth.POST("/system/cron", handler.UpdateCronConfig)

			auth.GET("/domains/:id/logs", handler.GetDomainLogs)
			auth.GET("/domains/:id/loginurl", handler.GetQuickLoginURL)

			auth.GET("/cert/cnames", handler.GetCertCNAMEs)
			auth.POST("/cert/cnames", handler.CreateCertCNAME)
			auth.POST("/cert/cnames/:id/delete", handler.DeleteCertCNAME)
			auth.POST("/cert/cnames/:id/verify", handler.VerifyCertCNAME)

			// TOTP二步验证
			auth.GET("/user/totp/status", handler.GetTOTPStatus)
			auth.POST("/user/totp/enable", handler.EnableTOTP)
			auth.POST("/user/totp/verify", handler.VerifyAndEnableTOTP)
			auth.POST("/user/totp/disable", handler.DisableTOTP)

			auth.GET("/user/oauth/bindings", handler.GetOAuthBindings)
			auth.POST("/user/oauth/bind-url", handler.GetOAuthBindURL)
			auth.POST("/user/oauth/unbind", handler.UnbindOAuth)

			auth.POST("/request-logs/list", handler.GetRequestLogs)
			auth.POST("/request-logs/detail", handler.GetRequestByID)
			auth.POST("/request-logs/error", handler.GetErrorByID)
			auth.POST("/request-logs/stats", handler.GetRequestStats)
			auth.POST("/request-logs/clean", handler.CleanRequestLogs)
		}

		api.GET("/quicklogin", handler.QuickLogin)
	}

	subFS, err := fs.Sub(staticFS, "web")
	if err != nil {
		panic("创建内嵌静态文件子系统失败: " + err.Error())
	}

	// 获取Content-Type
	getContentType := func(filePath string) string {
		switch {
		case strings.HasSuffix(filePath, ".html"):
			return "text/html; charset=utf-8"
		case strings.HasSuffix(filePath, ".css"):
			return "text/css; charset=utf-8"
		case strings.HasSuffix(filePath, ".js"):
			return "application/javascript; charset=utf-8"
		case strings.HasSuffix(filePath, ".json"):
			return "application/json; charset=utf-8"
		case strings.HasSuffix(filePath, ".svg"):
			return "image/svg+xml"
		case strings.HasSuffix(filePath, ".png"):
			return "image/png"
		case strings.HasSuffix(filePath, ".jpg"), strings.HasSuffix(filePath, ".jpeg"):
			return "image/jpeg"
		case strings.HasSuffix(filePath, ".gif"):
			return "image/gif"
		case strings.HasSuffix(filePath, ".ico"):
			return "image/x-icon"
		case strings.HasSuffix(filePath, ".woff2"):
			return "font/woff2"
		case strings.HasSuffix(filePath, ".woff"):
			return "font/woff"
		case strings.HasSuffix(filePath, ".ttf"):
			return "font/ttf"
		case strings.HasSuffix(filePath, ".map"):
			return "application/json"
		default:
			return "application/octet-stream"
		}
	}

	// 尝试读取并服务文件
	tryServeFile := func(c *gin.Context, filePath string) bool {
		data, err := fs.ReadFile(subFS, filePath)
		if err != nil {
			return false
		}
		contentType := getContentType(filePath)
		if !strings.HasSuffix(filePath, ".html") {
			c.Header("Cache-Control", "public, max-age=31536000")
		}
		c.Data(http.StatusOK, contentType, data)
		return true
	}

	r.GET("/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/login/")
	})

	r.NoRoute(func(c *gin.Context) {
		path := c.Request.URL.Path
		filePath := strings.TrimPrefix(path, "/")
		if tryServeFile(c, filePath) {
			return
		}
		if strings.HasSuffix(path, "/") {
			if tryServeFile(c, filePath+"index.html") {
				return
			}
		}
		if tryServeFile(c, filePath+"/index.html") {
			return
		}

		// 处理动态路由: /dashboard/domains/[id]/ -> /dashboard/domains/_/
		if strings.HasPrefix(path, "/dashboard/domains/") && path != "/dashboard/domains/" {
			if tryServeFile(c, "dashboard/domains/_/index.html") {
				return
			}
		}
		// 处理动态路由: /dashboard/monitor/[id]/ -> /dashboard/monitor/_/
		if strings.HasPrefix(path, "/dashboard/monitor/") && path != "/dashboard/monitor/" &&
			!strings.HasPrefix(path, "/dashboard/monitor/add") && !strings.HasPrefix(path, "/dashboard/monitor/batch") {
			if tryServeFile(c, "dashboard/monitor/_/index.html") {
				return
			}
		}
		// 处理密码重置页面路由
		if strings.HasPrefix(path, "/forgot-password") {
			if tryServeFile(c, "forgot-password/index.html") {
				return
			}
		}
		if strings.HasPrefix(path, "/reset-password") {
			if tryServeFile(c, "reset-password/index.html") {
				return
			}
		}
		if strings.HasPrefix(path, "/reset-totp") {
			if tryServeFile(c, "reset-totp/index.html") {
				return
			}
		}

		if tryServeFile(c, "_not-found/index.html") {
			return
		}

		c.String(http.StatusNotFound, "404 Not Found")
	})

	return r
}
