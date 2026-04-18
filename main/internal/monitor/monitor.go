package monitor

import (
	"context"
	"encoding/json"
	"fmt"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/notify"
	"main/internal/utils"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

var Service *Monitor

// ResolveStatus 任务实时解析状态（供API查询）
type ResolveStatus struct {
	TaskID       uint            `json:"task_id"`
	MainValue    string          `json:"main_value"`
	MainHealth   bool            `json:"main_health"`
	BackupValues map[string]bool `json:"backup_values"` // value -> healthy
	LastCheck    time.Time       `json:"last_check"`
	LastError    string          `json:"last_error"`
}

// TaskState 任务切换状态信息（序列化到record_info）
type TaskState struct {
	BackupRecordIDs []string `json:"backup_record_ids"`
	OriginalValue   string   `json:"original_value"`
	DeletedRecord   struct {
		Type   string `json:"type"`
		Line   string `json:"line"`
		TTL    int    `json:"ttl"`
		MX     int    `json:"mx"`
		Remark string `json:"remark"`
	} `json:"deleted_record"`
	WasDeleted bool `json:"was_deleted"`
}

// Monitor 容灾监控服务
type Monitor struct {
	ctx             context.Context
	cancel          context.CancelFunc
	wg              sync.WaitGroup
	running         bool
	mu              sync.Mutex
	resolveStatuses sync.Map // taskID(uint) -> *ResolveStatus
	processing      sync.Map // taskID(uint) -> bool, 防止同一任务并发处理
}

// New 创建监控服务实例
func New() *Monitor {
	m := &Monitor{}
	Service = m
	return m
}

// Start 启动监控服务
func (m *Monitor) Start() {
	m.mu.Lock()
	if m.running {
		m.mu.Unlock()
		return
	}
	m.running = true
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.mu.Unlock()

	m.wg.Add(1)
	go m.run()
	logger.Info("[Monitor] 容灾监控服务已启动")
}

// Stop 停止监控服务
func (m *Monitor) Stop() {
	m.mu.Lock()
	if !m.running {
		m.mu.Unlock()
		return
	}
	m.running = false
	m.cancel()
	m.mu.Unlock()
	m.wg.Wait()
	logger.Info("[Monitor] 容灾监控服务已停止")
}

// run 主循环: 1秒ticker，扫描到期任务并异步执行
func (m *Monitor) run() {
	defer m.wg.Done()

	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	// 每60秒更新一次运行状态
	statusTicker := time.NewTicker(60 * time.Second)
	defer statusTicker.Stop()

	for {
		select {
		case <-m.ctx.Done():
			return
		case <-statusTicker.C:
			m.updateRunStatus()
		case <-ticker.C:
			m.dispatchTasks()
		}
	}
}

// updateRunStatus 更新系统运行状态
func (m *Monitor) updateRunStatus() {
	now := time.Now().Format("2006-01-02 15:04:05")
	database.DB.Where("`key` = ?", "run_time").Assign(models.SysConfig{Key: "run_time", Value: now}).FirstOrCreate(&models.SysConfig{})

	var countConfig models.SysConfig
	var count int64
	if err := database.DB.Where("`key` = ?", "run_count").First(&countConfig).Error; err == nil {
		fmt.Sscanf(countConfig.Value, "%d", &count)
	}
	count++
	database.DB.Where("`key` = ?", "run_count").Assign(models.SysConfig{Key: "run_count", Value: fmt.Sprintf("%d", count)}).FirstOrCreate(&models.SysConfig{})
}

// dispatchTasks 扫描到期任务并启动异步处理
func (m *Monitor) dispatchTasks() {
	var tasks []models.DMTask
	now := time.Now().Unix()
	database.DB.Where("active = ? AND check_next_time <= ?", true, now).Find(&tasks)

	for _, task := range tasks {
		// 检查是否正在处理中，防止同一任务并发执行
		if _, loaded := m.processing.LoadOrStore(task.ID, true); loaded {
			// 已在处理中，跳过本次调度，延后下次检查
			nextTime := now + int64(task.Frequency)
			database.DB.Model(&models.DMTask{}).Where("id = ?", task.ID).
				Update("check_next_time", nextTime)
			continue
		}

		// 更新 check_next_time
		nextTime := now + int64(task.Frequency)
		database.DB.Model(&models.DMTask{}).Where("id = ?", task.ID).
			Update("check_next_time", nextTime)
		// SafeGo 统一兜底 panic（安全审计 L-5）；t 本地化避免循环变量被下一轮覆盖
		t := task
		utils.SafeGoWithName(fmt.Sprintf("monitor-task-%d", t.ID), func() { m.processTaskAsync(t) })
	}
}

// processTaskAsync 异步处理单个监控任务
// 使用WaitGroup并发检测所有主源+备用源
func (m *Monitor) processTaskAsync(task models.DMTask) {
	// 处理完成后释放锁
	defer m.processing.Delete(task.ID)
	defer func() {
		if r := recover(); r != nil {
			logger.Error("[Monitor] Task %d panic: %v", task.ID, r)
		}
	}()

	ctx, cancel := context.WithTimeout(m.ctx, time.Duration(task.Timeout+10)*time.Second)
	defer cancel()

	checkStart := time.Now()

	// ============ 并发检测主源 ============
	mainCheckStart := time.Now()
	mainIPs := parseValues(task.MainValue)
	mainResults := make(map[string]*CheckResult)
	var mainWg sync.WaitGroup
	var mainMu sync.Mutex
	mainHealthy := true

	for _, ip := range mainIPs {
		mainWg.Add(1)
		go func(addr string) {
			defer mainWg.Done()
			result := m.checkAddress(ctx, task, addr)
			mainMu.Lock()
			mainResults[addr] = result
			if !result.Success {
				mainHealthy = false
			}
			mainMu.Unlock()
		}(ip)
	}

	// ============ 并发检测备用源 ============
	backupCheckStart := time.Now()
	backupVals := getBackupValues(task)
	backupResults := make(map[string]*CheckResult)
	var backupWg sync.WaitGroup
	var backupMu sync.Mutex
	backupHealthMap := make(map[string]bool)

	for _, bv := range backupVals {
		addrs := resolveToAddrs(bv)
		for _, addr := range addrs {
			backupWg.Add(1)
			go func(val, a string) {
				defer backupWg.Done()
				result := m.checkAddress(ctx, task, a)
				backupMu.Lock()
				backupResults[val] = result
				backupHealthMap[val] = result.Success
				backupMu.Unlock()
			}(bv, addr)
		}
	}

	mainWg.Wait()
	mainDuration := time.Since(mainCheckStart).Milliseconds()
	backupWg.Wait()
	backupDuration := time.Since(backupCheckStart).Milliseconds()

	duration := time.Since(checkStart).Milliseconds()

	// ============ 保存检测日志 ============
	errMsg := ""
	for addr, r := range mainResults {
		if !r.Success {
			errMsg = fmt.Sprintf("%s: %s", addr, r.Error)
			break
		}
	}

	backupHealthJSON, _ := json.Marshal(backupHealthMap)
	go func() {
		database.LogDB.Create(&models.DMCheckLog{
			TaskID:         task.ID,
			Success:        mainHealthy,
			Duration:       duration,
			Error:          truncate(errMsg, 500),
			MainHealth:     mainHealthy,
			BackupHealths:  string(backupHealthJSON),
			MainDuration:   mainDuration,
			BackupDuration: backupDuration,
			CreatedAt:      time.Now(),
		})
	}()

	// ============ 更新内存中的ResolveStatus ============
	status := &ResolveStatus{
		TaskID:       task.ID,
		MainValue:    task.MainValue,
		MainHealth:   mainHealthy,
		BackupValues: backupHealthMap,
		LastCheck:    time.Now(),
		LastError:    errMsg,
	}
	m.resolveStatuses.Store(task.ID, status)

	// ============ 决策: 切换/恢复 ============
	updates := map[string]interface{}{
		"check_time":  time.Now().Unix(),
		"main_health": mainHealthy,
		"last_error":  truncate(errMsg, 500),
	}

	if bh, err := json.Marshal(backupHealthMap); err == nil {
		updates["backup_health"] = string(bh)
	}

	if mainHealthy {
		// 主源健康
		if task.ErrCount > 0 {
			updates["err_count"] = 0
		}

		// 检查是否需要恢复
		if task.Status == 1 && task.AutoRestore {
			logger.Info("[Monitor] Task %d: 主源恢复健康，开始自动恢复", task.ID)
			if err := m.restoreRecord(task); err == nil {
				now := time.Now()
				updates["status"] = 0
				updates["switch_time"] = now.Unix()
				updates["recover_time"] = &now
				updates["err_count"] = 0
				m.logAction(task.ID, 2, "主源恢复健康，自动恢复")
				m.sendNotification(task, "recover", "")
			} else {
				logger.Error("[Monitor] Task %d: 恢复失败: %v", task.ID, err)
			}
		}
	} else {
		// 主源异常
		errCount := task.ErrCount + 1
		updates["err_count"] = errCount

		if errCount >= task.Cycle && task.Status == 0 {
			// 达到阈值，先发通知再尝试切换（通知不依赖切换是否成功）
			logger.Warn("[Monitor] Task %d: 连续失败%d次，达到阈值%d，开始切换", task.ID, errCount, task.Cycle)

			now := time.Now()
			updates["fault_time"] = &now

			switchErr := m.smartSwitch(task, true)
			if switchErr == nil {
				updates["status"] = 1
				updates["switch_time"] = now.Unix()
				m.logAction(task.ID, 1, fmt.Sprintf("连续失败%d次，已切换: %s", errCount, truncate(errMsg, 200)))
				logger.Info("[Monitor] Task %d: 切换成功，发送故障通知...", task.ID)
				m.sendNotification(task, "fault", errMsg)
			} else {
				logger.Error("[Monitor] Task %d: 切换失败: %v", task.ID, switchErr)
				m.logAction(task.ID, 1, fmt.Sprintf("切换失败: %v", switchErr))
				logger.Info("[Monitor] Task %d: 切换失败，发送紧急通知...", task.ID)
				m.sendNotification(task, "fault_switch_failed", fmt.Sprintf("%s\n切换失败: %v", errMsg, switchErr))
			}
		}
	}

	database.DB.Model(&models.DMTask{}).Where("id = ?", task.ID).Updates(updates)
}

// checkAddress 对单个地址执行健康检测
func (m *Monitor) checkAddress(ctx context.Context, task models.DMTask, addr string) *CheckResult {
	switch task.CheckType {
	case 0: // ping
		return CheckPing(ctx, addr, task.Timeout)
	case 1: // tcp
		port := task.TCPPort
		if port == 0 {
			port = 80
		}
		return CheckTCP(ctx, addr, port, task.Timeout)
	case 2: // http
		url := task.CheckURL
		if url == "" {
			url = "http://" + addr
		}
		if err := validateCheckURL(url); err != nil {
			return &CheckResult{Success: false, Error: err.Error()}
		}
		hostIP := ""
		if task.CDN {
			hostIP = addr
		}
		httpOpts := httpCheckOptionsFromTask(task, hostIP)
		return CheckHTTP(ctx, url, task.Timeout, httpOpts)
	case 3: // https
		url := task.CheckURL
		if url == "" {
			url = "https://" + addr
		}
		if err := validateCheckURL(url); err != nil {
			return &CheckResult{Success: false, Error: err.Error()}
		}
		hostIP := ""
		if task.CDN {
			hostIP = addr
		}
		httpOpts := httpCheckOptionsFromTask(task, hostIP)
		return CheckHTTP(ctx, url, task.Timeout, httpOpts)
	default:
		return &CheckResult{Success: false, Error: "unknown check type"}
	}
}

func httpCheckOptionsFromTask(task models.DMTask, hostIP string) *HTTPCheckOptions {
	useField := strings.TrimSpace(task.ProxyHost) != "" && task.ProxyPort > 0 &&
		(strings.EqualFold(strings.TrimSpace(task.ProxyType), "http") ||
			strings.EqualFold(strings.TrimSpace(task.ProxyType), "socks5"))
	return &HTTPCheckOptions{
		HostIP:        hostIP,
		MaxRedirects:  task.MaxRedirects,
		ExpectStatus:  task.ExpectStatus,
		ExpectKeyword: task.ExpectKeyword,
		ProxyType:     strings.TrimSpace(task.ProxyType),
		ProxyHost:     strings.TrimSpace(task.ProxyHost),
		ProxyPort:     task.ProxyPort,
		ProxyUsername: task.ProxyUsername,
		ProxyPassword:   task.ProxyPassword,
		UseEnvProxy:     task.UseProxy && !useField,
		InsecureSkipTLS: task.AllowInsecureTLS,
	}
}

// smartSwitch 智能切换: 根据平台能力选择最佳切换策略
func (m *Monitor) smartSwitch(task models.DMTask, toBackup bool) error {
	if task.DomainID == 0 {
		return fmt.Errorf("任务 %d 的域名ID为空，请检查任务配置", task.ID)
	}
	if task.RecordID == "" {
		return fmt.Errorf("任务 %d 的记录ID为空，请检查任务配置", task.ID)
	}

	var domain models.Domain
	if err := database.DB.First(&domain, task.DomainID).Error; err != nil {
		return fmt.Errorf("域名不存在(did=%d): %w", task.DomainID, err)
	}

	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return fmt.Errorf("账户不存在(aid=%d): %w", domain.AccountID, err)
	}

	var config map[string]string
	json.Unmarshal([]byte(account.Config), &config)

	provider, err := dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		return fmt.Errorf("获取DNS提供商失败: %w", err)
	}

	caps := GetPlatformCaps(account.Type)
	ctx := context.Background()

	var state TaskState
	if task.RecordInfo != "" {
		json.Unmarshal([]byte(task.RecordInfo), &state)
	}

	switch task.Type {
	case 0: // 暂停/恢复
		if toBackup {
			return m.disableRecord(ctx, provider, task, caps, &state)
		}
		return m.enableRecord(ctx, provider, task, &state)

	case 1: // 删除/重建
		if toBackup {
			record, getErr := provider.GetDomainRecordInfo(ctx, task.RecordID)
			if getErr == nil {
				state.OriginalValue = record.Value
				state.DeletedRecord.Type = record.Type
				state.DeletedRecord.Line = record.Line
				state.DeletedRecord.TTL = record.TTL
				state.DeletedRecord.MX = record.MX
				state.DeletedRecord.Remark = record.Remark
				state.WasDeleted = true
				m.saveTaskState(task.ID, state)
			}
			return provider.DeleteDomainRecord(ctx, task.RecordID)
		}
		return m.recreateRecord(ctx, provider, task, &state)

	case 2: // 切换备用值
		if toBackup {
			return m.switchToBackup(ctx, provider, task, caps, &state)
		}
		return m.switchToMain(ctx, provider, task, &state)
	}

	return fmt.Errorf("unknown task type: %d", task.Type)
}

// disableRecord 暂停记录（优先暂停，暂停不支持则删除）
func (m *Monitor) disableRecord(ctx context.Context, provider dns.Provider, task models.DMTask, caps PlatformCaps, state *TaskState) error {
	if caps.Pause {
		err := provider.SetDomainRecordStatus(ctx, task.RecordID, false)
		if err == nil {
			return nil
		}
		logger.Warn("[Monitor] Task %d: 暂停失败，尝试删除: %v", task.ID, err)
	}

	// 暂停不支持或失败，回退到删除
	record, getErr := provider.GetDomainRecordInfo(ctx, task.RecordID)
	if getErr != nil {
		return fmt.Errorf("获取记录信息失败: %w", getErr)
	}

	state.OriginalValue = record.Value
	state.DeletedRecord.Type = record.Type
	state.DeletedRecord.Line = record.Line
	state.DeletedRecord.TTL = record.TTL
	state.DeletedRecord.MX = record.MX
	state.DeletedRecord.Remark = record.Remark
	state.WasDeleted = true

	if delErr := provider.DeleteDomainRecord(ctx, task.RecordID); delErr != nil {
		return fmt.Errorf("删除记录失败: %w", delErr)
	}

	m.saveTaskState(task.ID, *state)
	logger.Info("[Monitor] Task %d: 通过删除方式禁用记录", task.ID)
	return nil
}

// enableRecord 恢复记录（启用或重建）
func (m *Monitor) enableRecord(ctx context.Context, provider dns.Provider, task models.DMTask, state *TaskState) error {
	if state.WasDeleted {
		return m.recreateRecord(ctx, provider, task, state)
	}
	return provider.SetDomainRecordStatus(ctx, task.RecordID, true)
}

// recreateRecord 重新创建被删除的记录
func (m *Monitor) recreateRecord(ctx context.Context, provider dns.Provider, task models.DMTask, state *TaskState) error {
	if !state.WasDeleted {
		return nil
	}

	line := state.DeletedRecord.Line
	if line == "" {
		line = "default"
	}
	ttl := state.DeletedRecord.TTL
	if ttl == 0 {
		ttl = 600
	}
	recordType := state.DeletedRecord.Type
	if recordType == "" {
		recordType = "A"
	}
	value := state.OriginalValue
	if value == "" {
		value = task.MainValue
	}

	newRecordID, err := provider.AddDomainRecord(ctx, task.RR, recordType, value, line, ttl, state.DeletedRecord.MX, nil, state.DeletedRecord.Remark)
	if err != nil {
		return fmt.Errorf("重建记录失败: %w", err)
	}

	database.DB.Model(&models.DMTask{}).Where("id = ?", task.ID).Update("record_id", newRecordID)
	state.WasDeleted = false
	m.saveTaskState(task.ID, *state)
	logger.Info("[Monitor] Task %d: 记录已重建, 新ID=%s", task.ID, newRecordID)
	return nil
}

// switchToBackup 切换到备用值
func (m *Monitor) switchToBackup(ctx context.Context, provider dns.Provider, task models.DMTask, caps PlatformCaps, state *TaskState) error {
	state.OriginalValue = task.MainValue

	record, err := provider.GetDomainRecordInfo(ctx, task.RecordID)
	if err != nil {
		return fmt.Errorf("获取主记录信息失败: %w", err)
	}

	backupVals := getBackupValues(task)
	if len(backupVals) == 0 {
		return fmt.Errorf("无备用值")
	}

	// 选择第一个健康的备用值
	selectedBackup := backupVals[0]
	for _, bv := range backupVals {
		if m.isBackupHealthy(task.ID, bv) {
			selectedBackup = bv
			break
		}
	}

	if isCNAME(selectedBackup) {
		// 备用值是CNAME，需要解析为IP并添加A记录
		resolvedIPs, err := resolveCNAME(selectedBackup, 5)
		if err != nil || len(resolvedIPs) == 0 {
			return fmt.Errorf("解析备用CNAME失败: %v", err)
		}
		logger.Info("[Monitor] Task %d: 解析CNAME %s -> %v", task.ID, selectedBackup, resolvedIPs)

		for _, ip := range resolvedIPs {
			recordID, addErr := provider.AddDomainRecord(ctx, record.Name, "A", ip, record.Line, record.TTL, 0, nil, "backup-"+selectedBackup)
			if addErr == nil {
				state.BackupRecordIDs = append(state.BackupRecordIDs, recordID)
			}
		}

		// 禁用主记录
		if caps.Pause {
			err = provider.SetDomainRecordStatus(ctx, task.RecordID, false)
			if err != nil {
				// 暂停失败，尝试删除
				state.DeletedRecord.Type = record.Type
				state.DeletedRecord.Line = record.Line
				state.DeletedRecord.TTL = record.TTL
				state.DeletedRecord.MX = record.MX
				state.DeletedRecord.Remark = record.Remark
				state.WasDeleted = true
				provider.DeleteDomainRecord(ctx, task.RecordID)
			}
		} else {
			// 不支持暂停，直接删除
			state.DeletedRecord.Type = record.Type
			state.DeletedRecord.Line = record.Line
			state.DeletedRecord.TTL = record.TTL
			state.DeletedRecord.MX = record.MX
			state.DeletedRecord.Remark = record.Remark
			state.WasDeleted = true
			provider.DeleteDomainRecord(ctx, task.RecordID)
		}

		m.saveTaskState(task.ID, *state)
		return nil
	}

	// 备用值是IP
	backupRecordType := GetRecordType(selectedBackup) // A 或 AAAA

	if record.Type == backupRecordType {
		// 类型一致（如 A→A），直接更新记录值
		err = provider.UpdateDomainRecord(ctx, task.RecordID, record.Name, record.Type, selectedBackup, record.Line, record.TTL, record.MX, nil, "")
		if err != nil {
			return fmt.Errorf("更新记录值失败: %w", err)
		}
	} else {
		// 类型不一致（如 CNAME→IP），需要禁用/删除主记录 + 添加新记录
		logger.Info("[Monitor] Task %d: 记录类型不匹配(%s→%s)，采用删除+新建方式切换", task.ID, record.Type, backupRecordType)

		// 保存主记录信息用于恢复
		state.DeletedRecord.Type = record.Type
		state.DeletedRecord.Line = record.Line
		state.DeletedRecord.TTL = record.TTL
		state.DeletedRecord.MX = record.MX
		state.DeletedRecord.Remark = record.Remark
		state.WasDeleted = true

		// 添加新的备用记录
		newRecordID, addErr := provider.AddDomainRecord(ctx, record.Name, backupRecordType, selectedBackup, record.Line, record.TTL, 0, nil, "backup")
		if addErr != nil {
			return fmt.Errorf("添加备用%s记录失败: %w", backupRecordType, addErr)
		}
		state.BackupRecordIDs = append(state.BackupRecordIDs, newRecordID)

		// 禁用或删除主记录
		if caps.Pause {
			if pauseErr := provider.SetDomainRecordStatus(ctx, task.RecordID, false); pauseErr != nil {
				// 暂停失败，尝试删除
				provider.DeleteDomainRecord(ctx, task.RecordID)
			}
		} else {
			provider.DeleteDomainRecord(ctx, task.RecordID)
		}
	}

	m.saveTaskState(task.ID, *state)
	return nil
}

// switchToMain 从备用切回主源
func (m *Monitor) switchToMain(ctx context.Context, provider dns.Provider, task models.DMTask, state *TaskState) error {
	// 清理备用记录
	for _, recordID := range state.BackupRecordIDs {
		if err := provider.DeleteDomainRecord(ctx, recordID); err != nil {
			logger.Warn("[Monitor] Task %d: 清理备用记录 %s 失败: %v", task.ID, recordID, err)
		}
	}
	state.BackupRecordIDs = nil

	// 如果主记录被删除了，重建
	if state.WasDeleted {
		return m.recreateRecord(ctx, provider, task, state)
	}

	// 尝试启用主记录
	err := provider.SetDomainRecordStatus(ctx, task.RecordID, true)
	if err == nil {
		m.saveTaskState(task.ID, *state)
		return nil
	}

	// 启用失败，尝试更新值回主源
	record, getErr := provider.GetDomainRecordInfo(ctx, task.RecordID)
	if getErr != nil {
		return fmt.Errorf("获取记录失败: %w", getErr)
	}

	originalValue := state.OriginalValue
	if originalValue == "" {
		originalValue = task.MainValue
	}

	err = provider.UpdateDomainRecord(ctx, task.RecordID, record.Name, record.Type, originalValue, record.Line, record.TTL, record.MX, nil, "")
	if err != nil {
		return fmt.Errorf("恢复主记录值失败: %w", err)
	}

	m.saveTaskState(task.ID, *state)
	return nil
}

// restoreRecord 恢复记录（供processTaskAsync和ManualSwitch调用）
func (m *Monitor) restoreRecord(task models.DMTask) error {
	return m.smartSwitch(task, false)
}

// ManualSwitch 手动切换主备
func (m *Monitor) ManualSwitch(taskID uint, toBackup bool) error {
	var task models.DMTask
	if err := database.DB.First(&task, taskID).Error; err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}

	err := m.smartSwitch(task, toBackup)
	if err != nil {
		return err
	}

	// 更新状态
	updates := map[string]interface{}{
		"switch_time": time.Now().Unix(),
	}
	if toBackup {
		now := time.Now()
		updates["status"] = 1
		updates["fault_time"] = &now
		m.logAction(taskID, 1, "手动切换到备用")
	} else {
		now := time.Now()
		updates["status"] = 0
		updates["recover_time"] = &now
		updates["err_count"] = 0
		m.logAction(taskID, 2, "手动恢复主源")
	}
	database.DB.Model(&models.DMTask{}).Where("id = ?", taskID).Updates(updates)
	return nil
}

// GetResolveStatuses 获取所有任务的实时解析状态
func (m *Monitor) GetResolveStatuses() map[uint]*ResolveStatus {
	result := make(map[uint]*ResolveStatus)
	m.resolveStatuses.Range(func(key, value interface{}) bool {
		if taskID, ok := key.(uint); ok {
			if status, ok := value.(*ResolveStatus); ok {
				result[taskID] = status
			}
		}
		return true
	})
	return result
}

// GetResolveStatus 获取单个任务的实时解析状态
func (m *Monitor) GetResolveStatus(taskID uint) *ResolveStatus {
	if v, ok := m.resolveStatuses.Load(taskID); ok {
		if status, ok := v.(*ResolveStatus); ok {
			return status
		}
	}
	return nil
}

// ==================== 通知 ====================

// sendNotification 发送通知
func (m *Monitor) sendNotification(task models.DMTask, eventType string, errMsg string) {
	if !task.NotifyEnabled {
		logger.Info("[Monitor] Task %d: 跳过通知（notify_enabled=false）", task.ID)
		return
	}

	logger.Info("[Monitor] Task %d: 准备发送通知 type=%s channels=%s", task.ID, eventType, task.NotifyChannels)

	// 获取域名信息
	var domain models.Domain
	if err := database.DB.First(&domain, task.DomainID).Error; err != nil {
		logger.Error("[Monitor] Task %d: 获取域名信息失败: %v", task.ID, err)
		return
	}

	fullDomain := task.RR + "." + domain.Name
	if task.RR == "@" {
		fullDomain = domain.Name
	}

	var title, content string
	switch eventType {
	case "fault":
		title = fmt.Sprintf("【容灾告警】%s 主源异常", fullDomain)
		content = fmt.Sprintf("域名: %s\n主源: %s\n错误: %s\n连续失败: %d次\n已自动切换到备用源\n时间: %s",
			fullDomain, task.MainValue, truncate(errMsg, 200),
			task.Cycle, time.Now().Format("2006-01-02 15:04:05"))
	case "fault_switch_failed":
		title = fmt.Sprintf("【紧急告警】%s 主源异常且切换失败", fullDomain)
		content = fmt.Sprintf("域名: %s\n主源: %s\n错误: %s\n连续失败: %d次\n⚠️ 自动切换失败，请立即手动处理！\n时间: %s",
			fullDomain, task.MainValue, truncate(errMsg, 300),
			task.Cycle, time.Now().Format("2006-01-02 15:04:05"))
	case "recover":
		title = fmt.Sprintf("【容灾恢复】%s 主源已恢复", fullDomain)
		content = fmt.Sprintf("域名: %s\n主源: %s\n已自动恢复到主源\n时间: %s",
			fullDomain, task.MainValue, time.Now().Format("2006-01-02 15:04:05"))
	default:
		return
	}

	// 构建通知管理器
	manager := m.buildNotifyManager(task)
	if manager == nil {
		logger.Warn("[Monitor] Task %d: 无可用通知渠道，请检查系统设置中的邮件/Telegram/Webhook等配置", task.ID)
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := manager.Send(ctx, title, content); err != nil {
		logger.Error("[Monitor] Task %d: 发送通知失败: %v", task.ID, err)
	} else {
		logger.Info("[Monitor] Task %d: 通知已发送 (%s)", task.ID, eventType)
	}
}

// buildNotifyManager 根据系统配置构建通知管理器
func (m *Monitor) buildNotifyManager(task models.DMTask) *notify.NotifyManager {
	// 加载系统通知配置
	var configs []models.SysConfig
	database.DB.Where("`key` LIKE ?", "mail_%").
		Or("`key` LIKE ?", "tgbot_%").
		Or("`key` LIKE ?", "webhook_%").
		Or("`key` LIKE ?", "discord_%").
		Or("`key` LIKE ?", "bark_%").
		Or("`key` LIKE ?", "wechat_%").
		Find(&configs)

	configMap := make(map[string]string)
	for _, c := range configs {
		configMap[c.Key] = c.Value
	}

	// 解析任务指定的通知渠道
	var channels []string
	if task.NotifyChannels != "" {
		json.Unmarshal([]byte(task.NotifyChannels), &channels)
	}

	manager := notify.NewManager()
	hasNotifier := false

	// 如果任务未指定渠道，使用全部已配置渠道
	useAll := len(channels) == 0

	// 邮件（前端可能传 "email" 或 "mail"，都要匹配）
	if (useAll || containsStr(channels, "mail") || containsStr(channels, "email")) && configMap["mail_host"] != "" && configMap["mail_recv"] != "" {
		port := 25
		if p, err := strconv.Atoi(configMap["mail_port"]); err == nil {
			port = p
		}
		useTLS := configMap["mail_secure"] == "ssl" || configMap["mail_tls"] == "1"
		mailTo := configMap["mail_recv"]
		if mailTo == "" {
			mailTo = configMap["mail_from"]
		}
		manager.AddNotifier(notify.NewEmailNotifier(notify.EmailConfig{
			Host:     configMap["mail_host"],
			Port:     port,
			Username: configMap["mail_user"],
			Password: configMap["mail_password"],
			From:     configMap["mail_from"],
			To:       mailTo,
			UseTLS:   useTLS,
		}))
		hasNotifier = true
	}

	// Telegram
	if (useAll || containsStr(channels, "telegram")) && configMap["tgbot_token"] != "" && configMap["tgbot_chatid"] != "" {
		manager.AddNotifier(notify.NewTelegramNotifier(notify.TelegramConfig{
			BotToken: configMap["tgbot_token"],
			ChatID:   configMap["tgbot_chatid"],
		}))
		hasNotifier = true
	}

	// Webhook
	if (useAll || containsStr(channels, "webhook")) && configMap["webhook_url"] != "" {
		manager.AddNotifier(notify.NewWebhookNotifier(notify.WebhookConfig{
			URL: configMap["webhook_url"],
		}))
		hasNotifier = true
	}

	// Discord
	if (useAll || containsStr(channels, "discord")) && configMap["discord_webhook"] != "" {
		manager.AddNotifier(notify.NewDiscordNotifier(notify.DiscordConfig{
			WebhookURL: configMap["discord_webhook"],
		}))
		hasNotifier = true
	}

	// Bark
	if (useAll || containsStr(channels, "bark")) && configMap["bark_url"] != "" && configMap["bark_key"] != "" {
		manager.AddNotifier(notify.NewBarkNotifier(notify.BarkConfig{
			ServerURL: configMap["bark_url"],
			DeviceKey: configMap["bark_key"],
		}))
		hasNotifier = true
	}

	// 企业微信
	if (useAll || containsStr(channels, "wechat")) && configMap["wechat_webhook"] != "" {
		manager.AddNotifier(notify.NewWechatWorkNotifier(notify.WechatWorkConfig{
			WebhookURL: configMap["wechat_webhook"],
		}))
		hasNotifier = true
	}

	if !hasNotifier {
		return nil
	}
	return manager
}

// ==================== 辅助函数 ====================

// saveTaskState 保存任务切换状态到数据库
func (m *Monitor) saveTaskState(taskID uint, state TaskState) {
	stateJSON, _ := json.Marshal(state)
	database.DB.Model(&models.DMTask{}).Where("id = ?", taskID).Update("record_info", string(stateJSON))
}

// logAction 记录容灾切换日志
func (m *Monitor) logAction(taskID uint, action int, errMsg string) {
	database.DB.Create(&models.DMLog{
		TaskID:    taskID,
		Action:    action,
		ErrMsg:    truncate(errMsg, 100),
		CreatedAt: time.Now(),
	})
}

// isBackupHealthy 检查备用值是否健康（从内存状态查询）
func (m *Monitor) isBackupHealthy(taskID uint, backupVal string) bool {
	if v, ok := m.resolveStatuses.Load(taskID); ok {
		if status, ok := v.(*ResolveStatus); ok {
			if healthy, exists := status.BackupValues[backupVal]; exists {
				return healthy
			}
		}
	}
	return true // 默认认为健康
}

// parseValues 解析逗号分隔的值列表
func parseValues(value string) []string {
	if value == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// getBackupValues 获取任务的所有备用值
func getBackupValues(task models.DMTask) []string {
	if task.BackupValues != "" {
		return parseValues(task.BackupValues)
	}
	if task.BackupValue != "" {
		return []string{task.BackupValue}
	}
	return nil
}

// resolveToAddrs 将值解析为可检测的地址
func resolveToAddrs(value string) []string {
	if net.ParseIP(value) != nil {
		return []string{value}
	}
	// 尝试DNS解析
	ips, err := resolveCNAME(value, 3)
	if err != nil || len(ips) == 0 {
		return []string{value}
	}
	return ips
}

// resolveCNAME 递归解析CNAME到IP地址
func resolveCNAME(cname string, maxDepth int) ([]string, error) {
	if maxDepth <= 0 {
		return nil, fmt.Errorf("max recursion depth reached")
	}

	ips, err := net.LookupIP(cname)
	if err != nil {
		return nil, err
	}

	var result []string
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			result = append(result, ipv4.String())
		}
	}

	if len(result) == 0 {
		cnames, err := net.LookupCNAME(cname)
		if err == nil && cnames != "" && cnames != cname {
			return resolveCNAME(strings.TrimSuffix(cnames, "."), maxDepth-1)
		}
	}

	return result, nil
}

// isCNAME 判断值是否为域名（非IP）
func isCNAME(value string) bool {
	if value == "" {
		return false
	}
	if net.ParseIP(value) != nil {
		return false
	}
	if strings.Contains(value, ",") {
		return false
	}
	return strings.Contains(value, ".")
}

// containsStr 检查字符串切片是否包含指定值
func containsStr(slice []string, val string) bool {
	for _, s := range slice {
		if s == val {
			return true
		}
	}
	return false
}

// truncate 截断字符串到指定长度
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}
