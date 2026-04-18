package handler

import (
	"context"
	"net"
	"net/http"
	"net/url"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/service"

	"github.com/gin-gonic/gin"
)

// getParam 从解密数据或query参数中读取值（支持GET和POST双模式）
func getParam(c *gin.Context, key string) string {
	data := middleware.GetDecryptedData(c)
	if data != nil {
		if v, ok := data[key]; ok {
			switch val := v.(type) {
			case string:
				return val
			case float64:
				return strconv.FormatFloat(val, 'f', -1, 64)
			case bool:
				return strconv.FormatBool(val)
			}
		}
	}
	return c.Query(key)
}

// GetDashboardStats 获取仪表盘统计数据
func GetDashboardStats(c *gin.Context) {
	now := time.Now()
	expireSoon := now.AddDate(0, 0, 7)

	type dashRow struct {
		Domains           int64 `gorm:"column:domains"`
		Tasks             int64 `gorm:"column:tasks"`
		Certs             int64 `gorm:"column:certs"`
		Deploys           int64 `gorm:"column:deploys"`
		DmonitorActive    int64 `gorm:"column:dmonitor_active"`
		DmonitorStatus0   int64 `gorm:"column:dmonitor_status_0"`
		DmonitorStatus1   int64 `gorm:"column:dmonitor_status_1"`
		OptimizeipActive  int64 `gorm:"column:optimizeip_active"`
		OptimizeipStatus1 int64 `gorm:"column:optimizeip_status_1"`
		OptimizeipStatus2 int64 `gorm:"column:optimizeip_status_2"`
		CertorderStatus3  int64 `gorm:"column:certorder_status_3"`
		CertorderStatus5  int64 `gorm:"column:certorder_status_5"`
		CertorderStatus6  int64 `gorm:"column:certorder_status_6"`
		CertorderStatus7  int64 `gorm:"column:certorder_status_7"`
		CertdeployStatus0 int64 `gorm:"column:certdeploy_status_0"`
		CertdeployStatus1 int64 `gorm:"column:certdeploy_status_1"`
		CertdeployStatus2 int64 `gorm:"column:certdeploy_status_2"`
	}
	var dr dashRow
	var runTime string
	// 并行两条只读 SQL；不使用 WithContext（避免注入 gin_context，防止回调并发写 db_queries 竞态）
	reqCtx := c.Request.Context()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		database.DB.WithContext(reqCtx).Raw(`
SELECT
  (SELECT COUNT(*) FROM domains WHERE deleted_at IS NULL) AS domains,
  (SELECT COUNT(*) FROM dm_tasks) AS tasks,
  (SELECT COUNT(*) FROM cert_orders) AS certs,
  (SELECT COUNT(*) FROM cert_deploys) AS deploys,
  (SELECT COUNT(*) FROM dm_tasks WHERE active = 1) AS dmonitor_active,
  (SELECT COUNT(*) FROM dm_tasks WHERE status = 0) AS dmonitor_status_0,
  (SELECT COUNT(*) FROM dm_tasks WHERE status = 1) AS dmonitor_status_1,
  (SELECT COUNT(*) FROM optimize_ips WHERE active = 1) AS optimizeip_active,
  (SELECT COUNT(*) FROM optimize_ips WHERE status = 1) AS optimizeip_status_1,
  (SELECT COUNT(*) FROM optimize_ips WHERE status = 2) AS optimizeip_status_2,
  (SELECT COUNT(*) FROM cert_orders WHERE status = 3) AS certorder_status_3,
  (SELECT COUNT(*) FROM cert_orders WHERE status < 0) AS certorder_status_5,
  (SELECT COUNT(*) FROM cert_orders WHERE expire_time IS NOT NULL AND expire_time < ? AND expire_time >= ?) AS certorder_status_6,
  (SELECT COUNT(*) FROM cert_orders WHERE expire_time IS NOT NULL AND expire_time < ?) AS certorder_status_7,
  (SELECT COUNT(*) FROM cert_deploys WHERE status = 0) AS certdeploy_status_0,
  (SELECT COUNT(*) FROM cert_deploys WHERE status = 1) AS certdeploy_status_1,
  (SELECT COUNT(*) FROM cert_deploys WHERE status = -1) AS certdeploy_status_2
`, expireSoon, now, now).Scan(&dr)
	}()
	go func() {
		defer wg.Done()
		database.DB.WithContext(reqCtx).Model(&models.SysConfig{}).Where("`key` = ?", "run_time").Pluck("value", &runTime)
	}()
	wg.Wait()

	var dmonitorState int64 = 0
	if runTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", runTime); err == nil {
			if time.Since(t).Seconds() < 10 {
				dmonitorState = 1
			}
		}
	}

	middleware.SuccessResponse(c, gin.H{
		"domains":             dr.Domains,
		"tasks":               dr.Tasks,
		"certs":               dr.Certs,
		"deploys":             dr.Deploys,
		"dmonitor_active":     dr.DmonitorActive,
		"dmonitor_status_0":   dr.DmonitorStatus0,
		"dmonitor_status_1":   dr.DmonitorStatus1,
		"dmonitor_state":      dmonitorState,
		"optimizeip_active":   dr.OptimizeipActive,
		"optimizeip_status_1": dr.OptimizeipStatus1,
		"optimizeip_status_2": dr.OptimizeipStatus2,
		"certorder_status_3":  dr.CertorderStatus3,
		"certorder_status_5":  dr.CertorderStatus5,
		"certorder_status_6":  dr.CertorderStatus6,
		"certorder_status_7":  dr.CertorderStatus7,
		"certdeploy_status_0": dr.CertdeployStatus0,
		"certdeploy_status_1": dr.CertdeployStatus1,
		"certdeploy_status_2": dr.CertdeployStatus2,
	})
}

// TestMailNotification 测试邮件通知
func TestMailNotification(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var configs []models.SysConfig
	database.WithContext(c).Where("`key` IN ?", []string{
		"mail_host", "mail_port", "mail_user", "mail_password",
		"mail_from", "mail_from_name", "mail_recv",
		"mail_secure", "mail_auth", "mail_tls", "site_name",
	}).Find(&configs)

	configMap := make(map[string]string)
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	mailHost := configMap["mail_host"]
	mailTo := configMap["mail_recv"]
	if mailTo == "" {
		mailTo = configMap["mail_from"]
	}
	if mailHost == "" || mailTo == "" {
		middleware.ErrorResponse(c, "请先配置邮箱设置")
		return
	}

	mailPort, _ := strconv.Atoi(configMap["mail_port"])
	if mailPort == 0 {
		mailPort = 25
	}

	// 兼容旧配置
	secure := configMap["mail_secure"]
	if secure == "" && (configMap["mail_tls"] == "1" || configMap["mail_tls"] == "true") {
		secure = "ssl"
	}

	authType := configMap["mail_auth"]
	if authType == "" {
		authType = "plain"
	}

	siteName := configMap["site_name"]
	if siteName == "" {
		siteName = "DNSPlane"
	}

	config := notify.EmailConfig{
		Host:     mailHost,
		Port:     mailPort,
		Username: configMap["mail_user"],
		Password: configMap["mail_password"],
		From:     configMap["mail_from"],
		FromName: configMap["mail_from_name"],
		To:       mailTo,
		Secure:   secure,
		AuthType: authType,
	}

	notifier := notify.NewEmailNotifier(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// 使用美化的测试邮件模板
	subject, body := notify.RenderTestEmail(siteName)
	if err := notifier.Send(ctx, subject, body); err != nil {
		middleware.ErrorResponse(c, "邮件发送失败："+err.Error())
		return
	}

	middleware.SuccessMsg(c, "邮件发送成功")
}

// TestTelegramNotification 测试Telegram通知
func TestTelegramNotification(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var botToken, chatID string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "tgbot_token").Pluck("value", &botToken)
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "tgbot_chatid").Pluck("value", &chatID)

	if botToken == "" || chatID == "" {
		middleware.ErrorResponse(c, "请先配置Telegram设置")
		return
	}

	config := notify.TelegramConfig{
		BotToken: botToken,
		ChatID:   chatID,
	}

	notifier := notify.NewTelegramNotifier(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, "消息发送测试", "这是一封测试消息！\n\n来自：DNSPlane"); err != nil {
		middleware.ErrorResponse(c, "消息发送失败："+err.Error())
		return
	}

	middleware.SuccessMsg(c, "消息发送成功")
}

// TestWebhookNotification 测试Webhook通知
func TestWebhookNotification(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var webhookURL string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "webhook_url").Pluck("value", &webhookURL)

	if webhookURL == "" {
		middleware.ErrorResponse(c, "请先配置Webhook设置")
		return
	}

	config := notify.WebhookConfig{
		URL:    webhookURL,
		Method: "POST",
	}

	notifier := notify.NewWebhookNotifier(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, "消息发送测试", "这是一封测试消息！\n来自：DNSPlane"); err != nil {
		middleware.ErrorResponse(c, "消息发送失败："+err.Error())
		return
	}

	middleware.SuccessMsg(c, "消息发送成功")
}

// ClearCache 清除缓存
func ClearCache(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	// 强制GC回收内存
	runtime.GC()

	service.Audit.LogAction(c, "clear_cache", "", "清除系统缓存")

	middleware.SuccessMsg(c, "缓存已清除，内存已回收")
}

// TestProxy 测试代理服务器
func TestProxy(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var req struct {
		Type string `json:"type" binding:"required"` // http, socks5
		Host string `json:"host" binding:"required"`
		Port int    `json:"port" binding:"required"`
		User string `json:"user"`
		Pass string `json:"pass"`
	}

	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.Type == "" || req.Host == "" || req.Port == 0 {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	// 拒绝指向私网/回环/链路本地的代理目标（安全审计 M-6），
	// 防止管理员误触发 SSRF 穿入云厂商 IMDS 或内网服务
	if ip := net.ParseIP(req.Host); ip != nil {
		if ip.IsLoopback() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsPrivate() {
			middleware.ErrorResponse(c, "代理地址不能指向内网或保留地址")
			return
		}
	} else {
		lower := strings.ToLower(strings.TrimSpace(req.Host))
		if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
			strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
			middleware.ErrorResponse(c, "代理地址不能指向内网域名")
			return
		}
	}

	// 构建代理URL
	var proxyURL string
	if req.User != "" {
		proxyURL = req.Type + "://" + url.QueryEscape(req.User) + ":" + url.QueryEscape(req.Pass) + "@" + req.Host + ":" + strconv.Itoa(req.Port)
	} else {
		proxyURL = req.Type + "://" + req.Host + ":" + strconv.Itoa(req.Port)
	}

	parsedURL, err := url.Parse(proxyURL)
	if err != nil {
		middleware.ErrorResponse(c, "代理地址格式错误")
		return
	}

	// 创建带代理的HTTP客户端
	transport := &http.Transport{
		Proxy: http.ProxyURL(parsedURL),
		DialContext: (&net.Dialer{
			Timeout:   10 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   15 * time.Second,
	}

	// 测试访问
	start := time.Now()
	resp, err := client.Get("https://www.google.com/generate_204")
	latency := time.Since(start).Milliseconds()

	if err != nil {
		// 尝试国内网站
		start = time.Now()
		resp, err = client.Get("https://www.baidu.com")
		latency = time.Since(start).Milliseconds()
		if err != nil {
			middleware.ErrorResponse(c, "代理连接失败: "+err.Error())
			return
		}
	}
	defer resp.Body.Close()

	middleware.EncryptedResponse(c, gin.H{
		"code": 0,
		"msg":  "代理连接成功",
		"data": gin.H{
			"latency": latency,
			"status":  resp.StatusCode,
		},
	})
}

// GetTaskStatus 获取后台任务状态
func GetTaskStatus(c *gin.Context) {

	// 获取各任务统计
	var scheduleCount, optimizeCount, certAutoCount, domainNoticeCount int64
	var scheduleActiveCount, optimizeActiveCount int64

	database.WithContext(c).Model(&models.ScheduleTask{}).Count(&scheduleCount)
	database.WithContext(c).Model(&models.ScheduleTask{}).Where("active = ?", true).Count(&scheduleActiveCount)

	database.WithContext(c).Model(&models.OptimizeIP{}).Count(&optimizeCount)
	database.WithContext(c).Model(&models.OptimizeIP{}).Where("active = ?", true).Count(&optimizeActiveCount)

	database.WithContext(c).Model(&models.CertOrder{}).Where("is_auto = ?", true).Count(&certAutoCount)
	database.WithContext(c).Model(&models.Domain{}).Where("is_notice = ?", true).Count(&domainNoticeCount)

	// 获取最近执行时间
	var scheduleTime, optimizeTime string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "schedule_time").Pluck("value", &scheduleTime)
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "optimize_time").Pluck("value", &optimizeTime)

	middleware.SuccessResponse(c, gin.H{
		"schedule": gin.H{
			"total":     scheduleCount,
			"active":    scheduleActiveCount,
			"last_time": scheduleTime,
		},
		"optimize": gin.H{
			"total":     optimizeCount,
			"active":    optimizeActiveCount,
			"last_time": optimizeTime,
		},
		"cert_auto":     certAutoCount,
		"domain_notice": domainNoticeCount,
	})
}

// GetCronConfig 获取定时任务配置
func GetCronConfig(c *gin.Context) {

	// 从数据库读取cron配置
	configKeys := []string{"cron_schedule", "cron_optimize", "cron_cert", "cron_expire"}
	configs := make(map[string]string)

	for _, key := range configKeys {
		var value string
		database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", key).Pluck("value", &value)
		configs[key] = value
	}

	// 设置默认值
	if configs["cron_schedule"] == "" {
		configs["cron_schedule"] = "*/1 * * * *" // 每分钟
	}
	if configs["cron_optimize"] == "" {
		configs["cron_optimize"] = "*/30 * * * *" // 每30分钟
	}
	if configs["cron_cert"] == "" {
		configs["cron_cert"] = "0 * * * *" // 每小时
	}
	if configs["cron_expire"] == "" {
		configs["cron_expire"] = "0 8 * * *" // 每天8点
	}

	middleware.SuccessResponse(c, configs)
}

// TestDiscordNotification 测试Discord通知
func TestDiscordNotification(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var webhookURL string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "discord_webhook").Pluck("value", &webhookURL)

	if webhookURL == "" {
		middleware.ErrorResponse(c, "请先配置Discord Webhook URL")
		return
	}

	config := notify.DiscordConfig{
		WebhookURL: webhookURL,
	}

	notifier := notify.NewDiscordNotifier(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, "🔔 DNSPlane 测试通知", "这是一条测试消息，如果您收到此消息，说明Discord通知配置正确！"); err != nil {
		middleware.ErrorResponse(c, "消息发送失败："+err.Error())
		return
	}

	middleware.SuccessMsg(c, "消息发送成功")
}

// TestBarkNotification 测试Bark推送
func TestBarkNotification(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var serverURL, deviceKey string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "bark_server").Pluck("value", &serverURL)
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "bark_key").Pluck("value", &deviceKey)

	if deviceKey == "" {
		middleware.ErrorResponse(c, "请先配置Bark Device Key")
		return
	}

	config := notify.BarkConfig{
		ServerURL: serverURL,
		DeviceKey: deviceKey,
	}

	notifier := notify.NewBarkNotifier(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, "DNSPlane测试", "这是一条测试消息"); err != nil {
		middleware.ErrorResponse(c, "推送发送失败："+err.Error())
		return
	}

	middleware.SuccessMsg(c, "推送发送成功")
}

// TestWechatNotification 测试企业微信通知
func TestWechatNotification(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var webhookURL string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "wechat_webhook").Pluck("value", &webhookURL)

	if webhookURL == "" {
		middleware.ErrorResponse(c, "请先配置企业微信Webhook URL")
		return
	}

	config := notify.WechatWorkConfig{
		WebhookURL: webhookURL,
	}

	notifier := notify.NewWechatWorkNotifier(config)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if err := notifier.Send(ctx, "DNSPlane 测试通知", "这是一条测试消息，如果您收到此消息，说明企业微信通知配置正确！"); err != nil {
		middleware.ErrorResponse(c, "消息发送失败："+err.Error())
		return
	}

	middleware.SuccessMsg(c, "消息发送成功")
}

// GetSystemInfo 获取系统信息
func GetSystemInfo(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	var m runtime.MemStats
	runtime.ReadMemStats(&m)

	// DB file sizes
	dataDBSize := getFileSize("data/data.db")
	logsDBSize := getFileSize("data/logs.db")
	requestDBSize := getFileSize("data/request_logs.db")

	middleware.SuccessResponse(c, gin.H{
		"version":         "1.0.0",
		"go_version":      runtime.Version(),
		"os":              runtime.GOOS,
		"arch":            runtime.GOARCH,
		"goroutines":      runtime.NumGoroutine(),
		"memory_alloc":    m.Alloc,
		"memory_sys":      m.Sys,
		"data_db_size":    dataDBSize,
		"logs_db_size":    logsDBSize,
		"request_db_size": requestDBSize,
		"num_cpu":         runtime.NumCPU(),
		"db_maintenance":  database.GetMaintenanceStats(),
	})
}

func getFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}

// UpdateCronConfig 更新定时任务配置
func UpdateCronConfig(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	var req struct {
		CronSchedule string `json:"cron_schedule"`
		CronOptimize string `json:"cron_optimize"`
		CronCert     string `json:"cron_cert"`
		CronExpire   string `json:"cron_expire"`
	}

	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	// 更新配置
	updateConfig := func(key, value string) {
		if value == "" {
			return
		}
		var config models.SysConfig
		result := database.WithContext(c).Where("`key` = ?", key).First(&config)
		if result.Error != nil {
			database.WithContext(c).Create(&models.SysConfig{Key: key, Value: value})
		} else {
			database.WithContext(c).Model(&config).Update("value", value)
		}
	}

	updateConfig("cron_schedule", req.CronSchedule)
	updateConfig("cron_optimize", req.CronOptimize)
	updateConfig("cron_cert", req.CronCert)
	updateConfig("cron_expire", req.CronExpire)

	service.Audit.LogAction(c, "update_cron_config", "", "更新定时任务配置")

	middleware.SuccessMsg(c, "配置已更新")
}
