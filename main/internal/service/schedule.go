package service

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"main/internal/database"
	"main/internal/dns"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/monitor"
	"main/internal/sysconfig"
)

type ScheduleRunner struct {
	running  bool
	stopCh   chan struct{}
	stopOnce sync.Once
	wg       sync.WaitGroup
	mu       sync.Mutex
}

func NewScheduleRunner() *ScheduleRunner {
	return &ScheduleRunner{}
}

func (r *ScheduleRunner) Start(ctx context.Context) {
	r.mu.Lock()
	if r.running {
		r.mu.Unlock()
		return
	}
	r.running = true
	r.stopCh = make(chan struct{})
	r.stopOnce = sync.Once{}
	r.mu.Unlock()
	r.wg.Add(1)
	go r.run(ctx)
	go func() {
		<-ctx.Done()
		r.Stop()
	}()
}

func (r *ScheduleRunner) Stop() {
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

func (r *ScheduleRunner) run(ctx context.Context) {
	defer r.wg.Done()
	select {
	case <-r.stopCh:
		return
	case <-ctx.Done():
		return
	case <-time.After(20 * time.Second):
	}
	r.executeDueTasks()
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.executeDueTasks()
		}
	}
}

func (r *ScheduleRunner) executeDueTasks() {
	defer func() {
		if err := recover(); err != nil {
			logger.Error("定时切换任务异常: %v", err)
		}
	}()
	var tasks []models.ScheduleTask
	now := time.Now().Unix()
	database.DB.Where("next_time > 0 AND next_time <= ? AND active = ?", now, true).Find(&tasks)
	if len(tasks) == 0 {
		return
	}
	logger.Info("开始执行定时切换任务，共 %d 个", len(tasks))
	for _, task := range tasks {
		if err := r.executeOne(&task); err != nil {
			logger.Error("定时切换任务 %d 执行失败: %v", task.ID, err)
		} else {
			logger.Info("定时切换任务 %d 执行成功", task.ID)
		}
	}
	r.setConfig("schedule_time", time.Now().Format("2006-01-02 15:04:05"))
}

func (r *ScheduleRunner) executeOne(task *models.ScheduleTask) error {
	var domain models.Domain
	if err := database.DB.First(&domain, task.DomainID).Error; err != nil {
		return fmt.Errorf("域名不存在: %w", err)
	}
	provider, err := getScheduleProvider(&domain)
	if err != nil {
		return err
	}
	database.DB.Model(task).Update("update_time", time.Now().Unix())
	fullDomain := task.RR + "." + domain.Name
	if task.RR == "@" {
		fullDomain = domain.Name
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	var recordInfo struct {
		Line   string `json:"Line"`
		TTL    int    `json:"TTL"`
		Type   string `json:"Type"`
		MX     int    `json:"MX"`
		Remark string `json:"Remark"`
	}
	if task.RecordInfo != "" {
		_ = json.Unmarshal([]byte(task.RecordInfo), &recordInfo)
	}
	switch task.SwitchType {
	case 1:
		if err := provider.SetDomainRecordStatus(ctx, task.RecordID, true); err != nil {
			r.addLog(fullDomain, "启用解析失败", err.Error())
			return err
		}
		r.addLog(fullDomain, "启用解析", "定时启用解析成功")
	case 2:
		if err := provider.SetDomainRecordStatus(ctx, task.RecordID, false); err != nil {
			r.addLog(fullDomain, "暂停解析失败", err.Error())
			return err
		}
		r.addLog(fullDomain, "暂停解析", "定时暂停解析成功")
	case 3:
		if err := provider.DeleteDomainRecord(ctx, task.RecordID); err != nil {
			r.addLog(fullDomain, "删除解析失败", err.Error())
			return err
		}
		r.addLog(fullDomain, "删除解析", "定时删除解析成功")
	default:
		line := recordInfo.Line
		if line == "" {
			line = dns.DefaultDNSLine(getAccountTypeByDomain(domain.AccountID))
		}
		if getAccountTypeByDomain(domain.AccountID) == "cloudflare" && task.Line != "" {
			line = task.Line
		}
		ttl := recordInfo.TTL
		if ttl <= 0 {
			ttl = 600
		}
		recordType := recordInfo.Type
		if recordType == "" {
			recordType = monitor.GetRecordType(task.Value)
		}
		if err := provider.UpdateDomainRecord(ctx, task.RecordID, task.RR, recordType, task.Value, line, ttl, recordInfo.MX, nil, recordInfo.Remark); err != nil {
			r.addLog(fullDomain, "修改解析失败", err.Error())
			return err
		}
		r.addLog(fullDomain, "修改解析", fmt.Sprintf("%s [%s] %s (线路:%s TTL:%d)", task.RR, recordType, task.Value, line, ttl))
	}
	r.updateNextTime(task)
	return nil
}

func (r *ScheduleRunner) updateNextTime(task *models.ScheduleTask) {
	updateScheduleNextTime(task)
}

func UpdateScheduleNextTime(task *models.ScheduleTask) {
	updateScheduleNextTime(task)
}

func updateScheduleNextTime(task *models.ScheduleTask) {
	now := time.Now()
	nextTime := int64(0)
	if task.Type == 1 {
		switch task.Cycle {
		case 2:
			day, _ := strconv.Atoi(strings.TrimSpace(task.SwitchDate))
			candidate := time.Date(now.Year(), now.Month(), day, parseHH(task.SwitchTime), parseMM(task.SwitchTime), 0, 0, now.Location())
			if !candidate.After(now) {
				candidate = candidate.AddDate(0, 1, 0)
			}
			nextTime = candidate.Unix()
		case 1:
			weekday, _ := strconv.Atoi(strings.TrimSpace(task.SwitchDate))
			candidate := nextWeekdayTime(now, weekday, task.SwitchTime)
			nextTime = candidate.Unix()
		default:
			candidate := time.Date(now.Year(), now.Month(), now.Day(), parseHH(task.SwitchTime), parseMM(task.SwitchTime), 0, 0, now.Location())
			if !candidate.After(now) {
				candidate = candidate.AddDate(0, 0, 1)
			}
			nextTime = candidate.Unix()
		}
	} else {
		candidate, err := time.ParseInLocation("2006-01-02 15:04:05", strings.TrimSpace(task.SwitchTime)+":00", now.Location())
		if err == nil && candidate.After(now) {
			nextTime = candidate.Unix()
		}
	}
	task.NextTime = nextTime
	database.DB.Model(task).Update("next_time", nextTime)
}

func nextWeekdayTime(now time.Time, weekday int, hhmm string) time.Time {
	target := time.Weekday(weekday)
	candidate := time.Date(now.Year(), now.Month(), now.Day(), parseHH(hhmm), parseMM(hhmm), 0, 0, now.Location())
	diff := (int(target) - int(candidate.Weekday()) + 7) % 7
	candidate = candidate.AddDate(0, 0, diff)
	if !candidate.After(now) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func parseHH(hhmm string) int {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) > 0 {
		v, _ := strconv.Atoi(parts[0])
		return v
	}
	return 0
}

func parseMM(hhmm string) int {
	parts := strings.Split(strings.TrimSpace(hhmm), ":")
	if len(parts) > 1 {
		v, _ := strconv.Atoi(parts[1])
		return v
	}
	return 0
}

func (r *ScheduleRunner) addLog(domain, action, data string) {
	if len(data) > 500 {
		data = data[:500]
	}
	database.LogDB.Create(&models.Log{UserID: 0, Username: "系统", Action: action, Domain: domain, Data: data, CreatedAt: time.Now()})
}

func (r *ScheduleRunner) setConfig(key, value string) {
	var cfg models.SysConfig
	if database.DB.Where("`key` = ?", key).First(&cfg).Error != nil {
		database.DB.Create(&models.SysConfig{Key: key, Value: value})
	} else {
		database.DB.Model(&cfg).Update("value", value)
	}
	sysconfig.Invalidate(key)
}

func getScheduleProvider(domain *models.Domain) (dns.Provider, error) {
	var account models.Account
	if err := database.DB.First(&account, domain.AccountID).Error; err != nil {
		return nil, fmt.Errorf("账户不存在: %w", err)
	}
	var configMap map[string]string
	if err := json.Unmarshal([]byte(account.Config), &configMap); err != nil {
		return nil, fmt.Errorf("账户配置解析失败: %w", err)
	}
	return dns.GetProvider(account.Type, configMap, domain.Name, domain.ThirdID)
}

func getAccountTypeByDomain(accountID uint) string {
	var account models.Account
	if err := database.DB.Select("type").First(&account, accountID).Error; err != nil {
		return ""
	}
	return account.Type
}
