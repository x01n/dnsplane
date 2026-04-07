package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
	"main/internal/monitor"
	"main/internal/service"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// 监控图表单任务在时间窗口内可能产生极大量检查点，限制返回条数避免拖慢接口与加密响应
const maxMonitorHistoryPoints = 4000

// userAccessibleDomainIDs 返回子查询 SQL：当前用户可见的域名 ID（自有 DNS 账户或委派权限）
func userAccessibleDomainIDs(userID string) (string, []interface{}) {
	uid, err := strconv.ParseUint(userID, 10, 32)
	if err != nil {
		return "SELECT 0 WHERE 0", nil
	}
	u := uint(uid)
	now := time.Now()
	sql := `SELECT id FROM domains WHERE deleted_at IS NULL AND (
		aid IN (SELECT id FROM accounts WHERE uid = ? AND deleted_at IS NULL)
		OR name IN (SELECT domain FROM permissions WHERE uid = ? AND (expire_time IS NULL OR expire_time > ?))
	)`
	return sql, []interface{}{u, u, now}
}

func requireMonitorModule(c *gin.Context) bool {
	if !middleware.UserModuleAllowed(c, "monitor") {
		middleware.ErrorResponse(c, "无权限访问容灾监控模块")
		return false
	}
	return true
}

func monitorTaskAccessible(c *gin.Context, task *models.DMTask) bool {
	if isAdmin(c) {
		return true
	}
	return middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(task.DomainID), 10))
}

/* requireMonitorWrite 写操作：模块 + 域名 + 非只读委派 + 子域 */
func requireMonitorWrite(c *gin.Context, domainID uint, rr string) bool {
	if !requireMonitorModule(c) {
		return false
	}
	did := strconv.FormatUint(uint64(domainID), 10)
	if !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), did) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return false
	}
	if ro, ok := c.Get("perm_read_only"); ok {
		if v, ok := ro.(bool); ok && v {
			middleware.ErrorResponse(c, "该域名为只读权限，无法执行写操作")
			return false
		}
	}
	if rr != "" && !middleware.CheckSubDomainPermission(currentUID(c), c.GetInt("level"), did, rr) {
		middleware.ErrorResponse(c, "无权限操作该子域名")
		return false
	}
	return true
}

func requireMonitorTaskByID(c *gin.Context, taskID uint) (*models.DMTask, bool) {
	var task models.DMTask
	if err := database.WithContext(c).First(&task, taskID).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return nil, false
	}
	if !requireMonitorModule(c) {
		return nil, false
	}
	if !monitorTaskAccessible(c, &task) {
		middleware.ErrorResponse(c, "无权限查看该监控任务")
		return nil, false
	}
	return &task, true
}

func dmTasksScoped(c *gin.Context) *gorm.DB {
	q := database.WithContext(c).Model(&models.DMTask{})
	if !isAdmin(c) {
		subSQL, subArgs := userAccessibleDomainIDs(currentUID(c))
		q = q.Where("did IN ("+subSQL+")", subArgs...)
	}
	return q
}

func buildMonitorTasksListQuery(c *gin.Context, keyword string) *gorm.DB {
	query := database.WithContext(c).Table("dm_tasks").
		Select("dm_tasks.*, domains.name as domain").
		Joins("LEFT JOIN domains ON dm_tasks.did = domains.id")

	if !isAdmin(c) {
		subSQL, subArgs := userAccessibleDomainIDs(currentUID(c))
		query = query.Where("dm_tasks.did IN ("+subSQL+")", subArgs...)
	}

	if keyword != "" {
		query = query.Where("domains.name LIKE ? OR dm_tasks.rr LIKE ? OR dm_tasks.main_value LIKE ?",
			"%"+keyword+"%", "%"+keyword+"%", "%"+keyword+"%")
	}
	return query
}

func GetMonitorTasks(c *gin.Context) {
	if !requireMonitorModule(c) {
		return
	}

	var req struct {
		Page     int    `json:"page"`
		PageSize int    `json:"page_size"`
		Keyword  string `json:"keyword"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 200
	}
	if req.PageSize > 500 {
		req.PageSize = 500
	}

	var tasks []struct {
		models.DMTask
		Domain string `json:"domain"`
	}

	var total int64
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buildMonitorTasksListQuery(c, req.Keyword).Count(&total)
	}()
	go func() {
		defer wg.Done()
		buildMonitorTasksListQuery(c, req.Keyword).
			Order("dm_tasks.id DESC").
			Offset((req.Page - 1) * req.PageSize).
			Limit(req.PageSize).
			Find(&tasks)
	}()
	wg.Wait()

	middleware.SuccessResponse(c, gin.H{"total": total, "list": tasks})
}

type CreateMonitorTaskRequest struct {
	DomainID      uint   `json:"domain_id" binding:"required"`
	RR            string `json:"rr" binding:"required"`
	RecordID      string `json:"record_id" binding:"required"`
	Type          int    `json:"type"` // 0:暂停/恢复 1:删除/恢复 2:切换备用 3:条件开启
	MainValue     string `json:"main_value" binding:"required"`
	BackupValue   string `json:"backup_value"` // 备用IP或域名
	CheckType     int    `json:"check_type"`   // 0:ping 1:tcp 2:http 3:https
	CheckURL      string `json:"check_url"`
	TCPPort       int    `json:"tcp_port"`
	ExpectStatus  string `json:"expect_status"`
	ExpectKeyword string `json:"expect_keyword"`
	MaxRedirects  int    `json:"max_redirects"`
	ProxyType     string `json:"proxy_type"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     int    `json:"proxy_port"`
	ProxyUsername string `json:"proxy_username"`
	ProxyPassword string `json:"proxy_password"`
	Frequency     int    `json:"frequency" binding:"required"` // 检测间隔(秒)
	Cycle         int    `json:"cycle"`                        // 连续失败次数阈值
	Timeout       int    `json:"timeout"`                      // 超时时间(秒)
	UseProxy      bool   `json:"use_proxy"`
	CDN           bool   `json:"cdn"`
	Remark        string `json:"remark"`
	RecordInfo    string `json:"record_info"` // 原记录信息JSON
}

type BatchMonitorRequest struct {
	DomainID      uint   `json:"domain_id" binding:"required"`
	RR            string `json:"rr" binding:"required"`
	Type          int    `json:"type"`
	BackupValue   string `json:"backup_value"`
	CheckType     int    `json:"check_type"`
	CheckURL      string `json:"check_url"`
	TCPPort       int    `json:"tcp_port"`
	ExpectStatus  string `json:"expect_status"`
	ExpectKeyword string `json:"expect_keyword"`
	MaxRedirects  int    `json:"max_redirects"`
	ProxyType     string `json:"proxy_type"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     int    `json:"proxy_port"`
	ProxyUsername string `json:"proxy_username"`
	ProxyPassword string `json:"proxy_password"`
	Frequency     int    `json:"frequency" binding:"required"`
	Cycle         int    `json:"cycle"`
	Timeout       int    `json:"timeout"`
	UseProxy      bool   `json:"use_proxy"`
	CDN           bool   `json:"cdn"`
	Remark        string `json:"remark"`
}

func CreateMonitorTask(c *gin.Context) {
	var req CreateMonitorTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if !requireMonitorWrite(c, req.DomainID, req.RR) {
		return
	}

	if req.Cycle == 0 {
		req.Cycle = 3
	}
	if req.Timeout == 0 {
		req.Timeout = 2
	}

	task := models.DMTask{
		DomainID:      req.DomainID,
		RR:            req.RR,
		RecordID:      req.RecordID,
		Type:          req.Type,
		MainValue:     req.MainValue,
		BackupValue:   req.BackupValue,
		CheckType:     req.CheckType,
		CheckURL:      req.CheckURL,
		TCPPort:       req.TCPPort,
		ExpectStatus:  req.ExpectStatus,
		ExpectKeyword: req.ExpectKeyword,
		MaxRedirects:  req.MaxRedirects,
		ProxyType:     req.ProxyType,
		ProxyHost:     req.ProxyHost,
		ProxyPort:     req.ProxyPort,
		ProxyUsername: req.ProxyUsername,
		ProxyPassword: req.ProxyPassword,
		Frequency:     req.Frequency,
		Cycle:         req.Cycle,
		Timeout:       req.Timeout,
		UseProxy:      req.UseProxy,
		CDN:           req.CDN,
		Remark:        req.Remark,
		RecordInfo:    req.RecordInfo,
		AddTime:       time.Now().Unix(),
		Active:        true,
	}

	// 使用Select("*")确保零值字段(如check_type=0, type=0)也正确写入
	if err := database.WithContext(c).Select("*").Create(&task).Error; err != nil {
		middleware.ErrorResponse(c, "创建失败")
		return
	}

	service.Audit.LogAction(c, "create_monitor_task", "", fmt.Sprintf("创建监控任务: %s, ID=%d", req.RR, task.ID))
	middleware.SuccessResponse(c, gin.H{"id": task.ID})
}

type UpdateMonitorTaskRequest struct {
	ID            uint   `json:"id"`
	DomainID      uint   `json:"domain_id"`
	RR            string `json:"rr"`
	RecordID      string `json:"record_id"`
	Type          int    `json:"type"`
	MainValue     string `json:"main_value"`
	BackupValue   string `json:"backup_value"`
	CheckType     int    `json:"check_type"`
	CheckURL      string `json:"check_url"`
	TCPPort       int    `json:"tcp_port"`
	ExpectStatus  string `json:"expect_status"`
	ExpectKeyword string `json:"expect_keyword"`
	MaxRedirects  int    `json:"max_redirects"`
	ProxyType     string `json:"proxy_type"`
	ProxyHost     string `json:"proxy_host"`
	ProxyPort     int    `json:"proxy_port"`
	ProxyUsername string `json:"proxy_username"`
	ProxyPassword string `json:"proxy_password"`
	Frequency     int    `json:"frequency"`
	Cycle         int    `json:"cycle"`
	Timeout       int    `json:"timeout"`
	UseProxy      bool   `json:"use_proxy"`
	CDN           bool   `json:"cdn"`
	AutoRestore   bool   `json:"auto_restore"`
	NotifyEnabled bool   `json:"notify_enabled"`
	Remark        string `json:"remark"`
}

func UpdateMonitorTask(c *gin.Context) {
	var req UpdateMonitorTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.ID = uint(u)
		}
	}
	if req.ID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var task models.DMTask
	if err := database.WithContext(c).First(&task, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return
	}

	if !requireMonitorWrite(c, task.DomainID, task.RR) {
		return
	}
	newDid, newRR := task.DomainID, task.RR
	if req.DomainID > 0 {
		newDid = req.DomainID
	}
	if req.RR != "" {
		newRR = req.RR
	}
	if newDid != task.DomainID || newRR != task.RR {
		if !requireMonitorWrite(c, newDid, newRR) {
			return
		}
	}

	updates := map[string]interface{}{}

	// 关键字段：只在有值时更新，防止零值覆盖
	if req.DomainID > 0 {
		updates["did"] = req.DomainID
	}
	if req.RR != "" {
		updates["rr"] = req.RR
	}
	if req.RecordID != "" {
		updates["record_id"] = req.RecordID
	}
	if req.MainValue != "" {
		updates["main_value"] = req.MainValue
	}

	// 普通字段：始终更新（零值有意义）
	updates["type"] = req.Type
	updates["backup_value"] = req.BackupValue
	updates["backup_values"] = req.BackupValue
	updates["check_type"] = req.CheckType
	updates["check_url"] = req.CheckURL
	updates["tcp_port"] = req.TCPPort
	updates["expect_status"] = req.ExpectStatus
	updates["expect_keyword"] = req.ExpectKeyword
	updates["max_redirects"] = req.MaxRedirects
	updates["proxy_type"] = req.ProxyType
	updates["proxy_host"] = req.ProxyHost
	updates["proxy_port"] = req.ProxyPort
	updates["proxy_username"] = req.ProxyUsername
	updates["proxy_password"] = req.ProxyPassword
	updates["frequency"] = req.Frequency
	updates["cycle"] = req.Cycle
	updates["timeout"] = req.Timeout
	updates["use_proxy"] = req.UseProxy
	updates["cdn"] = req.CDN
	updates["auto_restore"] = req.AutoRestore
	updates["notify_enabled"] = req.NotifyEnabled
	updates["remark"] = req.Remark

	database.WithContext(c).Model(&task).Updates(updates)
	service.Audit.LogAction(c, "update_monitor_task", "", fmt.Sprintf("更新监控任务: %d", req.ID))
	middleware.SuccessMsg(c, "更新成功")
}

type DeleteMonitorTaskRequest struct {
	ID uint `json:"id"`
}

func DeleteMonitorTask(c *gin.Context) {
	var req DeleteMonitorTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.ID = uint(u)
		}
	}
	if req.ID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var task models.DMTask
	if err := database.WithContext(c).First(&task, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return
	}
	if !requireMonitorWrite(c, task.DomainID, task.RR) {
		return
	}

	database.WithContext(c).Delete(&models.DMTask{}, req.ID)
	database.WithContext(c).Where("task_id = ?", req.ID).Delete(&models.DMLog{})
	service.Audit.LogAction(c, "delete_monitor_task", "", fmt.Sprintf("删除监控任务: %d", req.ID))
	middleware.SuccessMsg(c, "删除成功")
}

type ToggleMonitorTaskRequest struct {
	ID     uint `json:"id" form:"id"`
	Active bool `json:"active" form:"active"`
}

func ToggleMonitorTask(c *gin.Context) {
	var req ToggleMonitorTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.ID = uint(u)
		}
	}
	if req.ID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	var task models.DMTask
	if err := database.WithContext(c).First(&task, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return
	}
	if !requireMonitorWrite(c, task.DomainID, task.RR) {
		return
	}

	database.WithContext(c).Model(&models.DMTask{}).Where("id = ?", req.ID).Update("active", req.Active)
	service.Audit.LogAction(c, "toggle_monitor_task", "", fmt.Sprintf("切换监控任务状态: %d, active=%v", req.ID, req.Active))
	middleware.SuccessMsg(c, "操作成功")
}

type SwitchMonitorTaskRequest struct {
	ID       uint  `json:"id" form:"id"`
	ToBackup *bool `json:"to_backup" form:"to_backup"` // 指针类型区分"未传"和"传了false"
}

func SwitchMonitorTask(c *gin.Context) {
	var req SwitchMonitorTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.ID = uint(u)
		}
	}
	if req.ID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	if monitor.Service == nil {
		middleware.ErrorResponse(c, "监控服务未启动")
		return
	}

	var loaded models.DMTask
	if err := database.WithContext(c).First(&loaded, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return
	}
	if !requireMonitorWrite(c, loaded.DomainID, loaded.RR) {
		return
	}

	// 如果前端未指定方向，根据当前任务状态自动判断
	var toBackup bool
	if req.ToBackup != nil {
		toBackup = *req.ToBackup
	} else {
		toBackup = loaded.Status == 0 // 当前正常 → 切到备用
	}

	if err := monitor.Service.ManualSwitch(req.ID, toBackup); err != nil {
		middleware.ErrorResponse(c, "切换失败: "+err.Error())
		return
	}

	action := "切换到备用"
	if !toBackup {
		action = "恢复主源"
	}
	service.Audit.LogAction(c, "switch_monitor_task", "", fmt.Sprintf("手动%s: 任务%d", action, req.ID))
	middleware.SuccessMsg(c, action+"成功")
}

type GetMonitorLogsRequest struct {
	ID       uint `json:"id" form:"id"`
	Page     int  `json:"page" form:"page"`
	PageSize int  `json:"page_size" form:"page_size"`
}

func GetMonitorLogs(c *gin.Context) {
	var req GetMonitorLogsRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.ID = uint(u)
		}
	}
	if req.ID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	if _, ok := requireMonitorTaskByID(c, req.ID); !ok {
		return
	}

	page := req.Page
	pageSize := req.PageSize
	if page <= 0 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}

	var logs []models.DMLog
	var total int64

	database.WithContext(c).Model(&models.DMLog{}).Where("task_id = ?", req.ID).Count(&total)
	database.WithContext(c).Where("task_id = ?", req.ID).Order("id DESC").
		Offset((page - 1) * pageSize).Limit(pageSize).Find(&logs)

	middleware.SuccessResponse(c, gin.H{"total": total, "list": logs})
}

func GetMonitorOverview(c *gin.Context) {
	if !requireMonitorModule(c) {
		return
	}

	yesterday := time.Now().Add(-24 * time.Hour)
	dmLogsInScope := func() *gorm.DB {
		q := database.WithContext(c).Model(&models.DMLog{}).Where("created_at >= ?", yesterday)
		if !isAdmin(c) {
			subSQL, subArgs := userAccessibleDomainIDs(currentUID(c))
			q = q.Where("task_id IN (SELECT id FROM dm_tasks WHERE did IN ("+subSQL+"))", subArgs...)
		}
		return q
	}
	scopedCheck := func() *gorm.DB {
		q := database.LogDB.Model(&models.DMCheckLog{}).Where("created_at >= ?", yesterday)
		if !isAdmin(c) {
			subSQL, subArgs := userAccessibleDomainIDs(currentUID(c))
			q = q.Where("task_id IN (SELECT id FROM dm_tasks WHERE did IN ("+subSQL+"))", subArgs...)
		}
		return q
	}

	var taskCount, activeCount, healthyCount, faultyCount int64
	var switchCount, failCount int64
	var totalChecks, successChecks int64
	var sysCfgs []models.SysConfig

	var wg sync.WaitGroup
	wg.Add(9)
	go func() {
		defer wg.Done()
		dmTasksScoped(c).Count(&taskCount)
	}()
	go func() {
		defer wg.Done()
		dmTasksScoped(c).Where("active = ?", true).Count(&activeCount)
	}()
	go func() {
		defer wg.Done()
		dmTasksScoped(c).Where("active = ? AND status = ?", true, 0).Count(&healthyCount)
	}()
	go func() {
		defer wg.Done()
		dmTasksScoped(c).Where("status = ?", 1).Count(&faultyCount)
	}()
	go func() {
		defer wg.Done()
		dmLogsInScope().Count(&switchCount)
	}()
	go func() {
		defer wg.Done()
		dmLogsInScope().Where("action = ?", 1).Count(&failCount)
	}()
	go func() {
		defer wg.Done()
		scopedCheck().Count(&totalChecks)
	}()
	go func() {
		defer wg.Done()
		scopedCheck().Where("success = ?", true).Count(&successChecks)
	}()
	go func() {
		defer wg.Done()
		database.WithContext(c).Model(&models.SysConfig{}).Where("`key` IN ?", []string{"run_time", "run_count"}).Find(&sysCfgs)
	}()
	wg.Wait()

	avgUptime := float64(0)
	if totalChecks > 0 {
		avgUptime = math.Round(float64(successChecks)/float64(totalChecks)*10000) / 100
	}

	var runTime, runCountStr string
	var runCount int64
	for _, row := range sysCfgs {
		switch row.Key {
		case "run_time":
			runTime = row.Value
		case "run_count":
			runCountStr = row.Value
		}
	}
	if runCountStr != "" {
		fmt.Sscanf(runCountStr, "%d", &runCount)
	}

	var runState int = 0
	if runTime != "" {
		if t, err := time.Parse("2006-01-02 15:04:05", runTime); err == nil {
			if time.Since(t).Seconds() < 15 {
				runState = 1
			}
		}
	}

	middleware.SuccessResponse(c, gin.H{
		"task_count":    taskCount,
		"active_count":  activeCount,
		"healthy_count": healthyCount,
		"faulty_count":  faultyCount,
		"switch_count":  switchCount,
		"fail_count":    failCount,
		"avg_uptime":    avgUptime,
		"run_time":      runTime,
		"run_count":     runCount,
		"run_state":     runState,
	})
}

func BatchCreateMonitorTasks(c *gin.Context) {
	var req BatchMonitorRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if !requireMonitorWrite(c, req.DomainID, req.RR) {
		return
	}

	if req.Cycle == 0 {
		req.Cycle = 3
	}
	if req.Timeout == 0 {
		req.Timeout = 2
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	provider, err := getDNSProviderByDomain(&domain)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	ctx := c.Request.Context()
	result, err := provider.GetSubDomainRecords(ctx, req.RR, 1, 100, "", "")
	if err != nil {
		middleware.ErrorResponse(c, "获取解析记录失败: "+err.Error())
		return
	}

	records, ok := result.Records.([]dns.Record)
	if !ok {
		middleware.ErrorResponse(c, "解析记录格式错误")
		return
	}

	var createdCount int
	var existCount int

	for _, record := range records {
		if record.Type != "A" && record.Type != "AAAA" {
			continue
		}

		var existTask models.DMTask
		if err := database.WithContext(c).Where("record_id = ?", record.ID).First(&existTask).Error; err == nil {
			existCount++
			continue
		}

		recordInfo := map[string]interface{}{
			"type":   record.Type,
			"line":   record.Line,
			"ttl":    record.TTL,
			"mx":     record.MX,
			"remark": record.Remark,
		}
		recordInfoJSON, _ := json.Marshal(recordInfo)

		task := models.DMTask{
			DomainID:      req.DomainID,
			RR:            req.RR,
			RecordID:      record.ID,
			Type:          req.Type,
			MainValue:     record.Value,
			BackupValue:   req.BackupValue,
			CheckType:     req.CheckType,
			CheckURL:      req.CheckURL,
			TCPPort:       req.TCPPort,
			ExpectStatus:  req.ExpectStatus,
			ExpectKeyword: req.ExpectKeyword,
			MaxRedirects:  req.MaxRedirects,
			ProxyType:     req.ProxyType,
			ProxyHost:     req.ProxyHost,
			ProxyPort:     req.ProxyPort,
			ProxyUsername: req.ProxyUsername,
			ProxyPassword: req.ProxyPassword,
			Frequency:     req.Frequency,
			Cycle:         req.Cycle,
			Timeout:       req.Timeout,
			UseProxy:      req.UseProxy,
			CDN:           req.CDN,
			Remark:        req.Remark,
			RecordInfo:    string(recordInfoJSON),
			AddTime:       time.Now().Unix(),
			Active:        true,
		}

		if err := database.WithContext(c).Select("*").Create(&task).Error; err == nil {
			createdCount++
		}
	}

	service.Audit.LogAction(c, "batch_create_monitor_tasks", "", fmt.Sprintf("批量创建监控任务: %s, 创建%d个, 已存在%d个", req.RR, createdCount, existCount))
	middleware.SuccessResponse(c, gin.H{"created": createdCount, "existed": existCount})
}

func GetMonitorStatus(c *gin.Context) {
	if !requireMonitorModule(c) {
		return
	}

	var config models.SysConfig
	database.WithContext(c).Where("key = ?", "monitor_run_time").First(&config)

	runState := 0
	if config.Value != "" {
		runTime, err := time.Parse("2006-01-02 15:04:05", config.Value)
		if err == nil && time.Since(runTime) < 10*time.Second {
			runState = 1
		}
	}

	middleware.SuccessResponse(c, gin.H{"running": runState == 1, "run_time": config.Value})
}

func GetMonitorHistory(c *gin.Context) {
	var req struct {
		TaskID uint   `json:"task_id" form:"task_id"`
		Period string `json:"period" form:"period"` // "1h", "24h", "7d", "30d"
	}
	middleware.BindDecryptedData(c, &req)

	if req.TaskID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.TaskID = uint(u)
		}
	}
	if req.TaskID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	if _, ok := requireMonitorTaskByID(c, req.TaskID); !ok {
		return
	}

	if database.LogDB == nil {
		middleware.SuccessResponse(c, []models.DMCheckLog{})
		return
	}

	var since time.Time
	switch req.Period {
	case "1h":
		since = time.Now().Add(-time.Hour)
	case "7d":
		since = time.Now().AddDate(0, 0, -7)
	case "30d":
		since = time.Now().AddDate(0, 0, -30)
	default:
		since = time.Now().AddDate(0, 0, -1) // 24h default
	}

	var logs []models.DMCheckLog
	database.LogDB.Where("task_id = ? AND created_at > ?", req.TaskID, since).
		Order("created_at DESC").Limit(maxMonitorHistoryPoints).Find(&logs)
	slices.Reverse(logs)

	middleware.SuccessResponse(c, logs)
}

func GetMonitorUptime(c *gin.Context) {
	var req struct {
		TaskID uint `json:"task_id" form:"task_id"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.TaskID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.TaskID = uint(u)
		}
	}
	if req.TaskID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	if _, ok := requireMonitorTaskByID(c, req.TaskID); !ok {
		return
	}

	if database.LogDB == nil {
		empty := gin.H{
			"24h": gin.H{"total": int64(0), "success": int64(0), "uptime": float64(0), "avg_duration": float64(0)},
			"7d":  gin.H{"total": int64(0), "success": int64(0), "uptime": float64(0), "avg_duration": float64(0)},
			"30d": gin.H{"total": int64(0), "success": int64(0), "uptime": float64(0), "avg_duration": float64(0)},
		}
		middleware.SuccessResponse(c, empty)
		return
	}

	// Calculate uptime for different periods
	periods := map[string]time.Time{
		"24h": time.Now().AddDate(0, 0, -1),
		"7d":  time.Now().AddDate(0, 0, -7),
		"30d": time.Now().AddDate(0, 0, -30),
	}

	result := make(map[string]interface{})
	for name, since := range periods {
		var total, success int64
		database.LogDB.Model(&models.DMCheckLog{}).Where("task_id = ? AND created_at > ?", req.TaskID, since).Count(&total)
		database.LogDB.Model(&models.DMCheckLog{}).Where("task_id = ? AND created_at > ? AND success = ?", req.TaskID, since, true).Count(&success)

		uptime := float64(0)
		if total > 0 {
			uptime = float64(success) / float64(total) * 100
		}

		var avgDuration float64
		database.LogDB.Model(&models.DMCheckLog{}).Where("task_id = ? AND created_at > ? AND success = ?", req.TaskID, since, true).
			Select("COALESCE(AVG(duration), 0)").Row().Scan(&avgDuration)

		result[name] = gin.H{
			"total":        total,
			"success":      success,
			"uptime":       math.Round(uptime*100) / 100,
			"avg_duration": math.Round(avgDuration*100) / 100,
		}
	}

	middleware.SuccessResponse(c, result)
}

// LookupRecord 查询子域名DNS记录
func LookupRecord(c *gin.Context) {
	var req struct {
		DomainID  uint   `json:"domain_id"`
		SubDomain string `json:"sub_domain"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.DomainID == 0 || req.SubDomain == "" {
		middleware.ErrorResponse(c, "请填写域名和子域名")
		return
	}

	var domain models.Domain
	if err := database.DB.First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	if !requireMonitorModule(c) {
		return
	}
	didStr := strconv.FormatUint(uint64(req.DomainID), 10)
	if !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), didStr) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}
	if !middleware.CheckSubDomainPermission(currentUID(c), c.GetInt("level"), didStr, req.SubDomain) {
		middleware.ErrorResponse(c, "无权限操作该子域名")
		return
	}

	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	var config map[string]string
	json.Unmarshal([]byte(account.Config), &config)

	provider, err := dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	result, err := provider.GetDomainRecords(context.Background(), 1, 100, req.SubDomain, "", "", "", "", "")
	if err != nil {
		middleware.ErrorResponse(c, "获取记录失败: "+err.Error())
		return
	}

	middleware.SuccessResponse(c, gin.H{
		"domain":       domain.Name,
		"account_type": account.Type,
		"records":      result.Records,
	})
}

// AutoCreateRecord 智能创建时的记录信息
// 需要兼容多种JSON字段格式：
//   - dns.Record 标准格式: id, type, value, line (小写)
//   - 部分前端格式: RecordId, Type, Value, Line (首字母大写)
//   - 兼容格式: record_id (snake_case)
type AutoCreateRecord struct {
	// 标准 dns.Record 格式 (json:"id" 等)
	ID       string `json:"id"`
	TypeStd  string `json:"type"`
	ValueStd string `json:"value"`
	LineStd  string `json:"line"`
	NameStd  string `json:"name"`
	// PascalCase 格式 (部分前端可能使用)
	RecordId string `json:"RecordId"`
	TypePC   string `json:"Type"`
	ValuePC  string `json:"Value"`
	LinePC   string `json:"Line"`
	LineName string `json:"LineName"`
	// snake_case 格式
	RecordID string `json:"record_id"`
}

func (r AutoCreateRecord) GetRecordID() string {
	if r.ID != "" {
		return r.ID
	}
	if r.RecordId != "" {
		return r.RecordId
	}
	return r.RecordID
}

func (r AutoCreateRecord) GetType() string {
	if r.TypeStd != "" {
		return r.TypeStd
	}
	return r.TypePC
}

func (r AutoCreateRecord) GetValue() string {
	if r.ValueStd != "" {
		return r.ValueStd
	}
	return r.ValuePC
}

func (r AutoCreateRecord) GetLine() string {
	if r.LineName != "" {
		return r.LineName
	}
	if r.LineStd != "" {
		return r.LineStd
	}
	return r.LinePC
}

func (r AutoCreateRecord) GetName() string {
	return r.NameStd
}

// AutoCreateMonitorTask 智能创建监控（支持批量）
func AutoCreateMonitorTask(c *gin.Context) {
	var req struct {
		DomainID  uint               `json:"domain_id"`
		SubDomain string             `json:"sub_domain"`
		Records   []AutoCreateRecord `json:"records"`
		// 单记录模式（向后兼容）
		RecordID    string `json:"record_id"`
		RecordType  string `json:"record_type"`
		RecordValue string `json:"record_value"`
		RecordLine  string `json:"record_line"`
		// 通用参数
		BackupValue    string   `json:"backup_value"`
		BackupValues   string   `json:"backup_values"`
		Type           int      `json:"type"`
		Strategy       int      `json:"strategy"`
		CheckType      int      `json:"check_type"`
		CheckURL       string   `json:"check_url"`
		TCPPort        int      `json:"tcp_port"`
		ExpectStatus   string   `json:"expect_status"`
		ExpectKeyword  string   `json:"expect_keyword"`
		MaxRedirects   int      `json:"max_redirects"`
		UseProxy       bool     `json:"use_proxy"`
		ProxyType      string   `json:"proxy_type"`
		ProxyHost      string   `json:"proxy_host"`
		ProxyPort      int      `json:"proxy_port"`
		ProxyUsername  string   `json:"proxy_username"`
		ProxyPassword  string   `json:"proxy_password"`
		Frequency      int      `json:"frequency"`
		Cycle          int      `json:"cycle"`
		Timeout        int      `json:"timeout"`
		AutoRestore    bool     `json:"auto_restore"`
		NotifyEnabled  bool     `json:"notify_enabled"`
		NotifyChannels []string `json:"notify_channels"`
		Remark         string   `json:"remark"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.DomainID == 0 || req.SubDomain == "" {
		middleware.ErrorResponse(c, "缺少必要参数")
		return
	}

	if !requireMonitorWrite(c, req.DomainID, req.SubDomain) {
		return
	}

	if req.Frequency <= 0 {
		req.Frequency = 60
	}
	if req.Cycle <= 0 {
		req.Cycle = 3
	}
	if req.Timeout <= 0 {
		req.Timeout = 5
	}

	// 处理策略字段（兼容 type 和 strategy）
	taskType := req.Type
	if req.Strategy > 0 {
		taskType = req.Strategy
	}

	// 处理备用值（兼容 backup_value 和 backup_values）
	backupVal := req.BackupValues
	if backupVal == "" {
		backupVal = req.BackupValue
	}

	// 处理通知渠道
	notifyChannelsJSON := ""
	if len(req.NotifyChannels) > 0 {
		b, _ := json.Marshal(req.NotifyChannels)
		notifyChannelsJSON = string(b)
	}

	// 构建记录列表：优先使用 records 数组，回退到单记录字段
	type recordItem struct {
		ID    string
		RType string
		Value string
		Line  string
	}
	var recordList []recordItem

	if len(req.Records) > 0 {
		for _, r := range req.Records {
			recordList = append(recordList, recordItem{
				ID:    r.GetRecordID(),
				RType: r.GetType(),
				Value: r.GetValue(),
				Line:  r.GetLine(),
			})
		}
	} else if req.RecordID != "" {
		recordList = append(recordList, recordItem{
			ID:    req.RecordID,
			RType: req.RecordType,
			Value: req.RecordValue,
			Line:  req.RecordLine,
		})
	}

	if len(recordList) == 0 {
		middleware.ErrorResponse(c, "请选择至少一条DNS记录")
		return
	}

	var createdIDs []uint
	var skippedEmpty int
	for _, rec := range recordList {
		// 跳过记录ID为空的记录（数据不完整）
		if rec.ID == "" {
			skippedEmpty++
			continue
		}

		// 检查是否已存在
		var existTask models.DMTask
		if err := database.DB.Where("record_id = ? AND did = ?", rec.ID, req.DomainID).First(&existTask).Error; err == nil {
			continue // 已存在，跳过
		}

		// 判断备用值类型
		backupType := "ip"
		if backupVal != "" && !IsIPValue(backupVal) {
			backupType = "cname"
		}

		task := models.DMTask{
			DomainID:       req.DomainID,
			RR:             req.SubDomain,
			RecordID:       rec.ID,
			RecordType:     rec.RType,
			RecordLine:     rec.Line,
			Type:           taskType,
			MainValue:      rec.Value,
			BackupValues:   backupVal,
			BackupValue:    backupVal,
			BackupType:     backupType,
			CheckType:      req.CheckType,
			CheckURL:       req.CheckURL,
			TCPPort:        req.TCPPort,
			ExpectStatus:   req.ExpectStatus,
			ExpectKeyword:  req.ExpectKeyword,
			MaxRedirects:   req.MaxRedirects,
			UseProxy:       req.UseProxy,
			ProxyType:      req.ProxyType,
			ProxyHost:      req.ProxyHost,
			ProxyPort:      req.ProxyPort,
			ProxyUsername:  req.ProxyUsername,
			ProxyPassword:  req.ProxyPassword,
			Frequency:      req.Frequency,
			Cycle:          req.Cycle,
			Timeout:        req.Timeout,
			AutoRestore:    req.AutoRestore,
			NotifyEnabled:  req.NotifyEnabled,
			NotifyChannels: notifyChannelsJSON,
			Remark:         req.Remark,
			Active:         true,
			MainHealth:     true,
			AddTime:        time.Now().Unix(),
			CheckNextTime:  time.Now().Unix(),
		}

		// 使用Select("*")确保零值字段(如check_type=0, type=0)也正确写入
		if err := database.DB.Select("*").Create(&task).Error; err == nil {
			createdIDs = append(createdIDs, task.ID)
		}
	}

	service.Audit.LogAction(c, "auto_create_monitor", "", fmt.Sprintf("智能创建监控: %s, 创建%d个任务", req.SubDomain, len(createdIDs)))
	middleware.SuccessResponse(c, gin.H{"ids": createdIDs, "created": len(createdIDs)})
}

// IsIPValue 判断值是否为IP地址（支持逗号分隔的多值）
func IsIPValue(value string) bool {
	if value == "" {
		return false
	}
	// 处理多行/逗号分隔
	for _, line := range strings.Split(value, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		for _, part := range strings.Split(line, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			if net.ParseIP(part) == nil {
				return false // 含有非IP
			}
		}
	}
	return true
}

// GetResolveStatus 获取各解析实时状态
func GetResolveStatus(c *gin.Context) {
	var req struct {
		TaskID uint `json:"task_id" form:"task_id"`
	}
	middleware.BindDecryptedData(c, &req)

	if req.TaskID == 0 {
		if p := c.Param("id"); p != "" {
			u, _ := strconv.ParseUint(p, 10, 32)
			req.TaskID = uint(u)
		}
	}
	if req.TaskID == 0 {
		middleware.ErrorResponse(c, "缺少任务ID")
		return
	}

	if _, ok := requireMonitorTaskByID(c, req.TaskID); !ok {
		return
	}

	if monitor.Service == nil {
		middleware.SuccessResponse(c, []interface{}{})
		return
	}

	statuses := monitor.Service.GetResolveStatus(req.TaskID)
	middleware.SuccessResponse(c, statuses)
}
