package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/internal/cert/deploy"
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/sysconfig"
	"main/internal/whois"
)

/* 部署重试间隔（秒），参考 dnsmgr 的指数退避 */
var deployRetryIntervals = []int{60, 300, 600, 1800, 3600}

/* TaskRunner 后台任务管理器，负责证书续期、部署执行、到期通知等定时任务 */
type TaskRunner struct {
	notifyManager    *notify.NotifyManager
	running          bool
	stopCh           chan struct{}
	wg               sync.WaitGroup
	certTaskInterval time.Duration
	expireInterval   time.Duration
	stopOnce         sync.Once // 防止 Stop 被多次调用
	mu               sync.Mutex
}

/* NewTaskRunner 创建后台任务管理器实例 */
func NewTaskRunner() *TaskRunner {
	return &TaskRunner{
		notifyManager:    notify.NewManager(),
		certTaskInterval: 5 * time.Minute, // 证书/部署任务每5分钟检查
		expireInterval:   24 * time.Hour,  // 域名到期每天检查
	}
}

func (r *TaskRunner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.stopOnce = sync.Once{} // 重置 stopOnce
	r.mu.Unlock()
	r.loadNotifyConfig()
	r.wg.Add(2)
	go r.runCertTask(ctx)
	go r.runExpireNotice(ctx)

	// 监听退出信号
	go func() {
		<-ctx.Done()
		r.Stop()
	}()
}

func (r *TaskRunner) Stop() {
	r.stopOnce.Do(func() {
		r.mu.Lock()
		if !r.running {
			r.mu.Unlock()
			return
		}
		close(r.stopCh)
		r.mu.Unlock()
		r.wg.Wait()
		r.mu.Lock()
		r.running = false
		r.mu.Unlock()
	})
}

func (r *TaskRunner) loadNotifyConfig() {
	r.notifyManager = notify.NewManager()
	notify.LoadNotifiersWithGetter(r.notifyManager, sysconfig.GetValue)
}

func (r *TaskRunner) runCertTask(ctx context.Context) {
	defer r.wg.Done()
	select {
	case <-r.stopCh:
		return
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	r.executeCertTask()

	ticker := time.NewTicker(r.certTaskInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.executeCertTask()
		}
	}
}

func (r *TaskRunner) runExpireNotice(ctx context.Context) {
	defer r.wg.Done()
	select {
	case <-r.stopCh:
		return
	case <-ctx.Done():
		return
	case <-time.After(60 * time.Second):
	}
	r.executeExpireNotice()

	ticker := time.NewTicker(r.expireInterval)
	defer ticker.Stop()

	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.executeExpireNotice()
		}
	}
}

func (r *TaskRunner) executeCertTask() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("证书任务异常: %v", err)
		}
	}()
	r.checkCertRenewal()
	r.executeDeployTasks()
	r.retryFailedDeployTasks()
	r.releaseExpiredLocks()
}

func (r *TaskRunner) checkCertRenewal() {
	renewDays := 30
	if v := sysconfig.GetValue("cert_expire_days"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			renewDays = d
		}
	}

	var orders []models.CertOrder
	expiryThreshold := time.Now().AddDate(0, 0, renewDays)
	database.DB.Where("is_auto = ? AND status = ? AND expire_time IS NOT NULL AND expire_time < ?",
		true, 3, expiryThreshold).Find(&orders)

	for _, order := range orders {
		if order.IsLock {
			continue
		}

		// 检查重试次数
		if order.Retry >= 5 {
			if renewFailNoticeCooldownPassed(&order) {
				var names []string
				database.DB.Model(&models.CertDomain{}).Where("oid = ?", order.ID).Pluck("domain", &names)
				SendCertRenewFailNotification(order.ID, names, "自动续期已连续尝试5次仍未成功，系统已暂停调度，请登录处理")
				now := time.Now()
				database.DB.Model(&models.CertOrder{}).Where("id = ?", order.ID).Update("renew_fail_notice_at", now)
			}
			continue
		}

		// 检查重试间隔
		if order.RetryTime != nil {
			interval := getRetryInterval(order.Retry, []int{300, 600, 1800, 3600, 7200})
			if time.Since(*order.RetryTime) < time.Duration(interval)*time.Second {
				continue
			}
		}

		daysLeft := int(time.Until(*order.ExpireTime).Hours() / 24)
		if daysLeft < 0 {
			logger.Info("证书订单 %d 已过期 %d 天，触发自动续期", order.ID, -daysLeft)
		} else {
			logger.Info("证书订单 %d 即将过期(剩余%d天)，触发自动续期", order.ID, daysLeft)
		}
		now := time.Now()
		database.DB.Model(&order).Updates(map[string]interface{}{
			"status":           0,
			"is_lock":          false,
			"is_send":          false,
			"expire_notice_at": nil,
			"retry":            order.Retry + 1,
			"retry_time":       now,
		})

		r.addCertLog(order.ID, "auto_renew", fmt.Sprintf("开始自动续期（第%d次）", order.Retry+1))

		if CertRenewProcessStarter != nil {
			CertRenewProcessStarter(order.ID)
		} else {
			logger.Error("[AutoRenew] 未注册 CertRenewProcessStarter，订单 %d 无法执行 ACME，请在 main 中注册 handler.TriggerCertOrderProcessing", order.ID)
		}
	}
}

func (r *TaskRunner) executeDeployTasks() {
	var deploys []models.CertDeploy
	database.DB.Where("active = ? AND is_lock = ? AND (status = ? OR status = ?)",
		true, false, 0, 2).Find(&deploys)

	for _, deployTask := range deploys {
		var order models.CertOrder
		if err := database.DB.First(&order, deployTask.OrderID).Error; err != nil {
			continue
		}
		if order.Status != 3 || order.FullChain == "" || order.PrivateKey == "" {
			continue
		}
		if deployTask.Status == 2 {
			if deployTask.IssueTime != nil && order.IssueTime != nil {
				if !order.IssueTime.After(*deployTask.IssueTime) {
					continue // 证书未更新，不需要重新部署
				}
				logger.Info("部署任务 %d: 检测到证书已更新，开始重新部署", deployTask.ID)
			} else {
				continue
			}
		}
		r.executeSingleDeploy(&deployTask, &order)
	}
}

func (r *TaskRunner) retryFailedDeployTasks() {
	var deploys []models.CertDeploy
	database.DB.Where("active = ? AND status = ? AND is_lock = ?",
		true, -1, false).Find(&deploys)

	now := time.Now()
	for _, deployTask := range deploys {
		maxRetry := deployTask.MaxRetry
		if maxRetry == 0 {
			maxRetry = 5 // 默认最大重试5次
		}

		// 超过最大重试次数，不再重试
		if deployTask.Retry >= maxRetry {
			continue
		}
		if deployTask.RetryTime != nil {
			interval := getRetryInterval(deployTask.Retry, deployRetryIntervals)
			if now.Sub(*deployTask.RetryTime) < time.Duration(interval)*time.Second {
				continue
			}
		}

		var order models.CertOrder
		if err := database.DB.First(&order, deployTask.OrderID).Error; err != nil {
			continue
		}

		if order.FullChain == "" || order.PrivateKey == "" {
			continue
		}

		logger.Info("部署任务 %d: 开始第 %d 次重试", deployTask.ID, deployTask.Retry+1)
		r.executeSingleDeploy(&deployTask, &order)
	}
}

func (r *TaskRunner) executeSingleDeploy(deployTask *models.CertDeploy, order *models.CertOrder) {
	// 加锁防止并发执行
	now := time.Now()
	result := database.DB.Model(deployTask).Where("is_lock = ?", false).Updates(map[string]interface{}{
		"is_lock":   true,
		"lock_time": now,
		"status":    1, // 执行中
	})
	if result.RowsAffected == 0 {
		return // 已被其他实例锁定
	}

	// 获取部署账户
	var account models.CertAccount
	if err := database.DB.First(&account, deployTask.AccountID).Error; err != nil {
		r.updateDeployFailed(deployTask, "部署账户不存在")
		return
	}

	// 解析账户配置
	var accConfig map[string]interface{}
	if account.Config != "" {
		if err := json.Unmarshal([]byte(account.Config), &accConfig); err != nil {
			r.updateDeployFailed(deployTask, "账户配置解析失败: "+err.Error())
			return
		}
	}

	// 解析部署配置
	var deployConfig map[string]interface{}
	if deployTask.Config != "" {
		if err := json.Unmarshal([]byte(deployTask.Config), &deployConfig); err != nil {
			r.updateDeployFailed(deployTask, "部署配置解析失败: "+err.Error())
			return
		}
	}
	if deployConfig == nil {
		deployConfig = map[string]interface{}{}
	}

	// 获取部署提供商（传入任务配置以解析子产品类型）
	provider, err := deploy.GetProvider(account.Type, accConfig, deployConfig)
	if err != nil {
		r.updateDeployFailed(deployTask, "获取部署提供商失败: "+err.Error())
		return
	}

	// 获取域名列表
	var domains []models.CertDomain
	database.DB.Where("oid = ?", order.ID).Order("sort ASC").Find(&domains)
	domainList := make([]string, 0, len(domains))
	for _, d := range domains {
		domainList = append(domainList, d.Domain)
	}
	if _, ok := deployConfig["domainList"]; !ok {
		deployConfig["domainList"] = domainList
	}
	if _, ok := deployConfig["domains"]; !ok {
		deployConfig["domains"] = strings.Join(domainList, ",")
	}
	var logBuilder strings.Builder
	provider.SetLogger(func(msg string) {
		logBuilder.WriteString(fmt.Sprintf("[%s] %s\n", time.Now().Format("2006-01-02 15:04:05"), msg))
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := provider.Deploy(ctx, order.FullChain, order.PrivateKey, deployConfig); err != nil {
		logBuilder.WriteString(fmt.Sprintf("[%s] 部署失败: %s\n", time.Now().Format("2006-01-02 15:04:05"), err.Error()))
		r.updateDeployFailedWithLog(deployTask, err.Error(), logBuilder.String())
		logger.Error("部署任务 %d 执行失败: %v", deployTask.ID, err)
		maxRetry := deployTask.MaxRetry
		if maxRetry == 0 {
			maxRetry = 5
		}
		if deployTask.Retry+1 >= maxRetry {
			r.sendDeployNotification(deployTask, domainList, false, err.Error())
		}
		return
	}
	logBuilder.WriteString(fmt.Sprintf("[%s] 部署成功\n", time.Now().Format("2006-01-02 15:04:05")))
	deployNow := time.Now()
	database.DB.Model(deployTask).Updates(map[string]interface{}{
		"status":      2,
		"error":       "",
		"is_lock":     false,
		"retry":       0,
		"log_content": logBuilder.String(),
		"last_time":   deployNow,
		"issue_time":  order.IssueTime,
	})

	logger.Info("部署任务 %d 执行成功", deployTask.ID)

	r.sendDeployNotification(deployTask, domainList, true, "")

	// 记录审计日志
	database.LogDB.Create(&models.Log{
		UserID:    0,
		Username:  "系统",
		Action:    "auto_deploy",
		Domain:    strings.Join(domainList, ","),
		Data:      fmt.Sprintf("自动部署成功: 账户=%s, 订单=%d", account.Name, order.ID),
		CreatedAt: time.Now(),
	})
}

/* updateDeployFailed 更新部署任务为失败状态 */
func (r *TaskRunner) updateDeployFailed(deployTask *models.CertDeploy, errMsg string) {
	now := time.Now()
	database.DB.Model(deployTask).Updates(map[string]interface{}{
		"status":     -1,
		"error":      errMsg,
		"is_lock":    false,
		"retry":      deployTask.Retry + 1,
		"retry_time": now,
		"last_time":  now,
	})
}

/* updateDeployFailedWithLog 更新部署任务为失败状态（含执行日志） */
func (r *TaskRunner) updateDeployFailedWithLog(deployTask *models.CertDeploy, errMsg, logContent string) {
	now := time.Now()
	database.DB.Model(deployTask).Updates(map[string]interface{}{
		"status":      -1,
		"error":       errMsg,
		"is_lock":     false,
		"retry":       deployTask.Retry + 1,
		"retry_time":  now,
		"last_time":   now,
		"log_content": logContent,
	})
}

func (r *TaskRunner) releaseExpiredLocks() {
	lockTimeout := time.Now().Add(-10 * time.Minute)

	/* 部署任务锁 */
	if result := database.DB.Model(&models.CertDeploy{}).
		Where("is_lock = ? AND lock_time < ?", true, lockTimeout).
		Updates(map[string]interface{}{
			"is_lock": false,
			"status":  -1,
			"error":   "执行超时，锁已自动释放",
		}); result.RowsAffected > 0 {
		logger.Warn("释放 %d 个超时的部署锁", result.RowsAffected)
	}

	orderLockTimeout := time.Now().Add(-15 * time.Minute)
	if result := database.DB.Model(&models.CertOrder{}).
		Where("is_lock = ? AND lock_time < ? AND status = ?", true, orderLockTimeout, 1).
		Updates(map[string]interface{}{
			"is_lock": false,
			"status":  -6,
			"error":   "申请超时，锁已自动释放",
		}); result.RowsAffected > 0 {
		logger.Warn("释放 %d 个超时的证书订单锁", result.RowsAffected)
	}
}

func (r *TaskRunner) sendDeployNotification(deployTask *models.CertDeploy, domains []string, success bool, errMsg string) {
	if success {
		if !sysconfigBoolExplicitOn("cert_deploy_success_notice_enabled") {
			return
		}
	} else {
		if !sysconfigBoolDefaultTrue("cert_deploy_notice_enabled") {
			return
		}
	}
	domainStr := strings.Join(domains, ", ")
	var title, content string
	if success {
		title = "【部署成功】" + domainStr
		content = fmt.Sprintf("证书部署成功\n域名: %s\n部署时间: %s", domainStr, time.Now().Format("2006-01-02 15:04:05"))
	} else {
		title = "【部署失败】" + domainStr
		content = fmt.Sprintf("证书部署失败（已达最大重试次数）\n域名: %s\n错误信息: %s\n时间: %s",
			domainStr, errMsg, time.Now().Format("2006-01-02 15:04:05"))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := r.notifyManager.Send(ctx, title, content); err != nil {
		logger.Error("发送部署通知失败: %v", err)
	}
}

func SendCertRenewFailNotification(orderID uint, domainNames []string, errSummary string) {
	if !sysconfigBoolDefaultTrue("cert_renew_fail_notice_enabled") {
		return
	}
	mgr := notify.NewManager()
	notify.LoadNotifiersWithGetter(mgr, sysconfig.GetValue)
	domainStr := strings.Join(domainNames, ", ")
	if domainStr == "" {
		domainStr = "(无关联域名)"
	}
	title := fmt.Sprintf("【证书续期失败】订单 #%d", orderID)
	content := fmt.Sprintf("自动续期或 ACME 流程失败\n订单ID: %d\n域名: %s\n摘要: %s\n时间: %s",
		orderID, domainStr, errSummary, time.Now().Format("2006-01-02 15:04:05"))
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := mgr.Send(ctx, title, content); err != nil {
		logger.Error("发送证书续期失败通知: %v", err)
	}
}

func MaybeNotifyCertAutoRenewACMEFailure(orderID uint, errSummary string) {
	if !sysconfigBoolDefaultTrue("cert_renew_fail_notice_enabled") {
		return
	}
	var order models.CertOrder
	if database.DB.First(&order, orderID).Error != nil {
		return
	}
	if !order.IsAuto || order.Status >= 0 {
		return
	}
	if order.Retry < 1 {
		return
	}
	if order.RenewFailNoticeAt != nil && time.Since(*order.RenewFailNoticeAt) < 24*time.Hour {
		return
	}
	var names []string
	database.DB.Model(&models.CertDomain{}).Where("oid = ?", orderID).Pluck("domain", &names)
	summary := strings.TrimSpace(errSummary)
	if summary == "" {
		summary = "ACME 处理失败"
	}
	SendCertRenewFailNotification(orderID, names, summary)
	now := time.Now()
	database.DB.Model(&models.CertOrder{}).Where("id = ?", orderID).Update("renew_fail_notice_at", now)
}

func renewFailNoticeCooldownPassed(order *models.CertOrder) bool {
	if order.RenewFailNoticeAt == nil {
		return true
	}
	return time.Since(*order.RenewFailNoticeAt) >= 24*time.Hour
}

/* sysconfigBoolDefaultTrue 未配置时视为 true；显式 false/0/off/no 则关闭（兼容旧库） */
func sysconfigBoolDefaultTrue(key string) bool {
	v := strings.TrimSpace(strings.ToLower(sysconfig.GetValue(key)))
	if v == "" {
		return true
	}
	return v != "false" && v != "0" && v != "no" && v != "off"
}

/* sysconfigBoolExplicitOn 未配置视为 false；仅 true/1/yes/on 为开 */
func sysconfigBoolExplicitOn(key string) bool {
	v := strings.TrimSpace(strings.ToLower(sysconfig.GetValue(key)))
	return v == "true" || v == "1" || v == "yes" || v == "on"
}

/* certExpireNoticeLeadDays 证书到期推送提前天数：优先 cert_expire_notice_days，≤0 或未配则用 cert_expire_days，再默认 30 */
func certExpireNoticeLeadDays() int {
	if v := sysconfig.GetValue("cert_expire_notice_days"); v != "" {
		if d, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && d > 0 {
			return d
		}
	}
	if v := sysconfig.GetValue("cert_expire_days"); v != "" {
		if d, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && d > 0 {
			return d
		}
	}
	return 30
}

/* certExpireNoticeIntervalDays 同一订单两次证书到期推送的最小间隔（天），默认 1；≤0 或未配视为 1 */
func certExpireNoticeIntervalDays() int {
	if v := sysconfig.GetValue("cert_expire_notice_interval_days"); v != "" {
		if d, err := strconv.Atoi(strings.TrimSpace(v)); err == nil && d > 0 {
			return d
		}
	}
	return 1
}

/* getRetryInterval 获取指数退避重试间隔（秒） */
func getRetryInterval(retry int, intervals []int) int {
	if retry < 0 {
		retry = 0
	}
	if retry >= len(intervals) {
		return intervals[len(intervals)-1]
	}
	return intervals[retry]
}

/* executeExpireNotice 执行域名和证书到期检查通知 */
func (r *TaskRunner) executeExpireNotice() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("域名到期通知任务异常: %v", err)
		}
	}()

	// 执行域名到期检查
	r.checkDomainExpire()

	// 执行证书到期检查
	r.checkCertExpire()
}

/* checkDomainExpire 检查域名到期并发送通知 */
func (r *TaskRunner) checkDomainExpire() {
	domainNoticeOn := sysconfigBoolDefaultTrue("domain_expire_notice_enabled")

	/* 使用 sysconfig 缓存层读取配置，避免每次定时任务直查 DB */
	notifyDays := 30
	if v := sysconfig.GetValue("domain_expire_days"); v != "" {
		if d, err := strconv.Atoi(v); err == nil && d > 0 {
			notifyDays = d
		}
	}

	var domains []models.Domain
	database.DB.Where("is_notice = ?", true).Find(&domains)

	now := time.Now()
	whoisCtx, whoisCancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer whoisCancel()

	for _, domain := range domains {
		/* WHOIS 自动刷新：无到期时间或距上次检查超过7天时查询 */
		needWhois := domain.ExpireTime == nil
		if !needWhois && domain.CheckTime != nil {
			needWhois = int(now.Sub(*domain.CheckTime).Hours()/24) >= 7
		}
		if !needWhois && domain.CheckTime == nil {
			needWhois = true
		}
		if needWhois {
			info, err := whois.Query(whoisCtx, domain.Name)
			if err != nil {
				logger.Warn("[DomainExpire] WHOIS 查询失败 %s: %v", domain.Name, err)
			} else {
				updates := map[string]interface{}{"check_time": now}
				if info.ExpiryDate != nil {
					updates["expire_time"] = info.ExpiryDate
					domain.ExpireTime = info.ExpiryDate
				}
				database.DB.Model(&domain).Updates(updates)
			}
		}

		if domain.ExpireTime == nil {
			continue
		}

		daysUntilExpiry := int(domain.ExpireTime.Sub(now).Hours() / 24)

		if !domainNoticeOn {
			continue
		}

		// 只通知即将过期的域名
		if daysUntilExpiry > notifyDays {
			continue
		}

		// 检查是否已通知过（每天最多通知一次）
		if domain.NoticeTime != nil {
			daysSinceNotice := int(now.Sub(*domain.NoticeTime).Hours() / 24)
			if daysSinceNotice < 1 {
				continue
			}
		}

		// 发送通知
		var title, content string
		if daysUntilExpiry <= 0 {
			title = "【域名已过期】" + domain.Name
			content = "您的域名 " + domain.Name + " 已过期，请尽快续费！"
		} else if daysUntilExpiry <= 7 {
			title = "【域名即将过期】" + domain.Name
			content = "您的域名 " + domain.Name + " 将在 " + formatInt(daysUntilExpiry) + " 天后过期，请及时续费！"
		} else {
			title = "【域名到期提醒】" + domain.Name
			content = "您的域名 " + domain.Name + " 将在 " + domain.ExpireTime.Format("2006-01-02") + " 过期（剩余 " + formatInt(daysUntilExpiry) + " 天），请注意续费。"
		}

		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.notifyManager.Send(ctx, title, content); err != nil {
				logger.Error("发送域名到期通知失败 %s: %v", domain.Name, err)
			} else {
				database.DB.Model(&domain).Update("notice_time", now)
				logger.Info("发送域名到期通知成功 %s (%d天)", domain.Name, daysUntilExpiry)
			}
		}()
	}
}

/* checkCertExpire 检查证书到期并发送通知 */
func (r *TaskRunner) checkCertExpire() {
	if !sysconfigBoolDefaultTrue("cert_expire_notice_enabled") {
		return
	}

	notifyDays := certExpireNoticeLeadDays()
	intervalDays := certExpireNoticeIntervalDays()
	now := time.Now()
	expiryThreshold := now.AddDate(0, 0, notifyDays)
	database.DB.Model(&models.CertOrder{}).
		Where("status = ? AND is_send = ? AND expire_notice_at IS NULL", 3, true).
		Update("expire_notice_at", now)

	var orders []models.CertOrder
	database.DB.Where("status = ? AND expire_time IS NOT NULL AND expire_time < ?",
		3, expiryThreshold).Find(&orders)

	for _, order := range orders {
		if order.ExpireTime == nil {
			continue
		}

		if order.ExpireNoticeAt != nil {
			daysSince := int(now.Sub(*order.ExpireNoticeAt).Hours() / 24)
			if daysSince < intervalDays {
				continue
			}
		}

		daysUntilExpiry := int(time.Until(*order.ExpireTime).Hours() / 24)
		var domains []models.CertDomain
		database.DB.Where("oid = ?", order.ID).Find(&domains)
		var domainList string
		for i, d := range domains {
			if i > 0 {
				domainList += ", "
			}
			domainList += d.Domain
		}

		var title, content string
		if daysUntilExpiry <= 0 {
			title = "【证书已过期】" + domainList
			content = fmt.Sprintf("您的SSL证书（%s）已于 %s 过期，请立即续期或检查自动续期任务。",
				domainList, order.ExpireTime.Format("2006-01-02"))
		} else if daysUntilExpiry <= 7 {
			title = "【证书即将过期】" + domainList
			content = fmt.Sprintf("您的SSL证书（%s）将在 %d 天后过期，请及时续期！", domainList, daysUntilExpiry)
		} else {
			title = "【证书到期提醒】" + domainList
			content = fmt.Sprintf("您的SSL证书（%s）将在 %s 过期（剩余 %d 天），请注意续期。",
				domainList, order.ExpireTime.Format("2006-01-02"), daysUntilExpiry)
		}

		func() {
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := r.notifyManager.Send(ctx, title, content); err != nil {
				logger.Error("发送证书到期通知失败: %v", err)
			} else {
				database.DB.Model(&order).Updates(map[string]interface{}{
					"is_send":          true,
					"expire_notice_at": now,
				})
				logger.Info("发送证书到期通知成功: %s (%d天)", domainList, daysUntilExpiry)
			}
		}()
	}
}

func (r *TaskRunner) addCertLog(orderID uint, action, data string) {
	database.LogDB.Create(&models.CertLog{
		OrderID:   orderID,
		Type:      action,
		Content:   data,
		CreatedAt: time.Now(),
	})
}

func formatInt(n int) string {
	return strconv.Itoa(n)
}

func (r *TaskRunner) ReloadNotifyConfig() {
	r.loadNotifyConfig()
}

func (r *TaskRunner) GetConfig(key string) string {
	return sysconfig.GetValue(key)
}

func (r *TaskRunner) SetConfig(key, value string) {
	var config models.SysConfig
	result := database.DB.Where("`key` = ?", key).First(&config)
	if result.Error != nil {
		database.DB.Create(&models.SysConfig{Key: key, Value: value})
	} else {
		database.DB.Model(&config).Update("value", value)
	}
	sysconfig.Invalidate(key)
}

func (r *TaskRunner) GetStatus() map[string]interface{} {
	var certOrderCount, domainNoticeCount int64
	database.DB.Model(&models.CertOrder{}).Where("is_auto = ?", true).Count(&certOrderCount)
	database.DB.Model(&models.Domain{}).Where("is_notice = ?", true).Count(&domainNoticeCount)
	var deployStats struct {
		Active int64 `gorm:"column:active_cnt"`
		Failed int64 `gorm:"column:failed_cnt"`
	}
	database.DB.Model(&models.CertDeploy{}).Select(
		"SUM(CASE WHEN active=1 THEN 1 ELSE 0 END) as active_cnt, " +
			"SUM(CASE WHEN active=1 AND status=-1 THEN 1 ELSE 0 END) as failed_cnt",
	).Scan(&deployStats)

	return map[string]interface{}{
		"running":         r.running,
		"cert_auto_count": certOrderCount,
		"notice_count":    domainNoticeCount,
		"deploy_active":   deployStats.Active,
		"deploy_failed":   deployStats.Failed,
	}
}
