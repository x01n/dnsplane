package handler

import (
	"context"
	"fmt"
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

/*
 * buildChannelForTest 按渠道注册表装配指定渠道，供各测试端点复用
 *
 * 与生产装载的唯一差异是不读取该渠道的启用开关（*_enabled）：
 * 测试端点的用途是验证凭据是否可用，与渠道是否启用是两件事 ——
 * 渠道被关闭时仍应能测出配置本身有没有问题。
 * 实现上把开关键排除在 IN 查询之外，ChannelEnabled 对缺键判定为启用，
 * 从而复用渠道自己的构造逻辑而不必为测试另写一份（那正是各端点参数缺失的来源）
 *
 * @param c gin 上下文
 * @param t 渠道标识
 * @returns 通知器、渠道适配器与错误
 */
func buildChannelForTest(c *gin.Context, t notify.NotifyType) (notify.Notifier, notify.Channel, error) {
	ch, ok := notify.LookupChannel(t)
	if !ok {
		return nil, nil, fmt.Errorf("通知渠道 %s 未注册", t)
	}

	var configs []models.SysConfig
	if err := database.WithContext(c).Where("`key` IN ?", ch.ConfigKeys()).Find(&configs).Error; err != nil {
		return nil, ch, fmt.Errorf("读取通知配置失败：%w", err)
	}
	configMap := make(map[string]string, len(configs))
	for _, cfg := range configs {
		configMap[cfg.Key] = cfg.Value
	}

	notifier, ok := ch.Build(configMap)
	if !ok {
		return nil, ch, fmt.Errorf("请先完成%s配置", ch.Label())
	}
	return notifier, ch, nil
}

/* siteNameOrDefault 读取站点名，未配置时回退 DNSPlane */
func siteNameOrDefault(c *gin.Context) string {
	var siteName string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "site_name").Pluck("value", &siteName)
	if siteName == "" {
		return "DNSPlane"
	}
	return siteName
}

/*
 * testChannelNotification 各渠道测试端点的统一实现
 * 渠道的构造与错误提示均由注册表给出，新增渠道时只需注册并加一条路由
 * @param c gin 上下文
 * @param t 渠道标识
 */
func testChannelNotification(c *gin.Context, t notify.NotifyType) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "权限不足，仅管理员可操作")
		return
	}

	notifier, ch, err := buildChannelForTest(c, t)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	subject := "DNSPlane 测试通知"
	content := fmt.Sprintf("这是一条测试消息，如果您收到此消息，说明%s通知配置正确！", ch.Label())
	if t == notify.NotifyEmail {
		// 邮件渠道用美化模板，模板本身是 HTML；mailContentAdapter 判定首字符为 '<' 时原样透传
		subject, content = notify.RenderTestEmail(siteNameOrDefault(c))
	}

	if err := notifier.Send(ctx, subject, content); err != nil {
		middleware.ErrorResponse(c, "消息发送失败："+err.Error())
		return
	}
	middleware.SuccessMsg(c, "消息发送成功")
}

// GetDashboardStats 获取仪表盘统计数据
/*
 * GetDashboardStats 首页统计
 *
 * 管理员看全站，普通用户只看自己名下的资源
 *
 * 此前一律返回全站计数：普通用户打开首页，卡片上的「域名总数」是全站的，
 * 既泄露实例规模，又与他实际拥有的数量不符——同一个数字对两类用户
 * 意味着不同的事。同文件的 GetSystemInfo 早已按 level 门控，
 * 这一路漏了
 */
func GetDashboardStats(c *gin.Context) {
	/*
	 * 普通用户走按操作者收窄的统计，与配额口径一致（详见 quotaUsed）
	 *
	 * 不复用校验用的配额函数：那些返回的是「上限」相关的结果，
	 * 首页要的是实际计数，且不受 max_* 为 -1（不限）的影响
	 */
	if c.GetInt("level") < 2 {
		ownerScopeDashboardStats(c)
		return
	}
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
		"certorder_status_3":  dr.CertorderStatus3,
		"certorder_status_5":  dr.CertorderStatus5,
		"certorder_status_6":  dr.CertorderStatus6,
		"certorder_status_7":  dr.CertorderStatus7,
		"certdeploy_status_0": dr.CertdeployStatus0,
		"certdeploy_status_1": dr.CertdeployStatus1,
		"certdeploy_status_2": dr.CertdeployStatus2,
	})
}

/* TestMailNotification 测试邮件通知 */
func TestMailNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyEmail)
}

/* TestTelegramNotification 测试Telegram通知 */
func TestTelegramNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyTelegram)
}

/* TestWebhookNotification 测试Webhook通知 */
func TestWebhookNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyWebhook)
}

/* TestCustomWebhookNotification 测试自定义Webhook通知 */
func TestCustomWebhookNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyCustomWebhook)
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
	if reason := proxyTargetRejectReason(req.Host); reason != "" {
		middleware.ErrorResponse(c, reason)
		return
	}

	// 构建代理URL
	parsedURL, err := url.Parse(buildProxyURL(req.Type, req.Host, req.User, req.Pass, req.Port))
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

/*
 * buildProxyURL 拼装代理 URL，IPv6 字面量补方括号后与端口拼接
 *
 * IPv6 字面量不加方括号会被 net/url 误判：socks5 分支把 "::1:1080" 整段当作 host
 * 从而静默拨错地址，http 分支直接报 invalid port。
 * 判据用「含冒号且未以 [ 开头」而非 net.ParseIP(...).To4() == nil —— 后者对
 * IPv4-mapped 地址 ::ffff:127.0.0.1 返回 false，会漏掉这类同样需要方括号的输入
 *
 * @param kind 代理类型，如 http / socks5
 * @param host 主机名或 IP 字面量，允许前端已自带方括号
 * @param user 代理用户名，留空时 URL 不带凭据段
 * @param pass 代理密码
 * @param port 代理端口
 * @returns 可直接交给 url.Parse 的代理 URL
 */
func buildProxyURL(kind, host, user, pass string, port int) string {
	if strings.Contains(host, ":") && !strings.HasPrefix(host, "[") {
		host = "[" + host + "]"
	}
	authority := host + ":" + strconv.Itoa(port)
	if user != "" {
		return kind + "://" + url.QueryEscape(user) + ":" + url.QueryEscape(pass) + "@" + authority
	}
	return kind + "://" + authority
}

func proxyTargetRejectReason(host string) string {
	lower := strings.ToLower(strings.TrimSpace(host))
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".internal") || strings.HasSuffix(lower, ".local") {
		return "代理地址不能指向内网域名"
	}
	return ""
}

/*
 * GetTaskStatus 获取后台任务状态
 *
 * 限定管理员：返回的是全站运营信息（定时任务数、证书续期数、到期域名数、
 * 解析记录同步状态与最近一次错误）。同族的 GetSystemConfig 与 UpdateSystemConfig
 * 都有 requireAdmin，这两个端点没有——属同族缺一个校验的形态
 *
 * 前端侧栏把「系统设置」标为 adminOnly 只是隐藏入口，不是路由守卫：
 * 非管理员直接访问该路径仍会进页面并发起这个请求，后端是唯一可靠的边界
 */
func GetTaskStatus(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	// 获取各任务统计
	var scheduleCount, certAutoCount, domainNoticeCount int64
	var scheduleActiveCount int64

	database.WithContext(c).Model(&models.ScheduleTask{}).Count(&scheduleCount)
	database.WithContext(c).Model(&models.ScheduleTask{}).Where("active = ?", true).Count(&scheduleActiveCount)

	database.WithContext(c).Model(&models.CertOrder{}).Where("is_auto = ?", true).Count(&certAutoCount)
	database.WithContext(c).Model(&models.Domain{}).Where("is_notice = ?", true).Count(&domainNoticeCount)

	// 获取最近执行时间
	var scheduleTime string
	database.WithContext(c).Model(&models.SysConfig{}).Where("`key` = ?", "schedule_time").Pluck("value", &scheduleTime)

	middleware.SuccessResponse(c, gin.H{
		"schedule": gin.H{
			"total":     scheduleCount,
			"active":    scheduleActiveCount,
			"last_time": scheduleTime,
		},
		"cert_auto":     certAutoCount,
		"domain_notice": domainNoticeCount,
		// 记录副本同步状态：此前只有一行日志，失败不可见，副本冻结无法察觉
		"record_sync": service.RecordSyncStatus(),
	})
}

/*
 * GetCronConfig 获取定时任务配置
 *
 * 限定管理员，理由同 GetTaskStatus：三个调度表达式属于运维配置，
 * 泄露即暴露系统的调度节奏。同族的 UpdateCronConfig 已有 requireAdmin，
 * 只读的那一侧漏了
 */
func GetCronConfig(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}

	// 从数据库读取cron配置
	configKeys := []string{"cron_schedule", "cron_cert", "cron_expire"}
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
	if configs["cron_cert"] == "" {
		configs["cron_cert"] = "0 * * * *" // 每小时
	}
	if configs["cron_expire"] == "" {
		configs["cron_expire"] = "0 8 * * *" // 每天8点
	}

	middleware.SuccessResponse(c, configs)
}

/* TestDiscordNotification 测试Discord通知 */
func TestDiscordNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyDiscord)
}

/* TestBarkNotification 测试Bark推送 */
func TestBarkNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyBark)
}

/* TestWechatNotification 测试企业微信通知 */
func TestWechatNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyWechat)
}

/* TestDingtalkNotification 测试钉钉通知 */
func TestDingtalkNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyDingTalk)
}

/* TestFeishuNotification 测试飞书通知 */
func TestFeishuNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyFeishu)
}

/* TestWxWorkAppNotification 测试企业微信应用消息 */
func TestWxWorkAppNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyWxWorkApp)
}

/* TestWxTplNotification 测试公众号模板消息 */
func TestWxTplNotification(c *gin.Context) {
	testChannelNotification(c, notify.NotifyWxTpl)
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
	updateConfig("cron_cert", req.CronCert)
	updateConfig("cron_expire", req.CronExpire)

	service.Audit.LogAction(c, "update_cron_config", "", "更新定时任务配置")

	middleware.SuccessMsg(c, "配置已更新")
}
