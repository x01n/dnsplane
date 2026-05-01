package handler

import (
	"strconv"
	"strings"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"main/internal/service"

	"github.com/gin-gonic/gin"
)

type scheduleTaskPayload struct {
	DomainID    uint   `json:"did"`
	RR          string `json:"rr"`
	RecordID    string `json:"recordid"`
	Type        int    `json:"type"`
	Cycle       int    `json:"cycle"`
	SwitchType  int    `json:"switchtype"`
	SwitchDate  string `json:"switchdate"`
	SwitchTime  string `json:"switchtime"`
	Value       string `json:"value"`
	Line        string `json:"line"`
	Remark      string `json:"remark"`
	RecordInfo  string `json:"recordinfo"`
	Active      *bool  `json:"active"`
}

type scheduleListRequest struct {
	Page     int    `json:"page" form:"page"`
	PageSize int    `json:"page_size" form:"page_size"`
	Keyword  string `json:"keyword" form:"keyword"`
	Type     string `json:"type" form:"type"`
}

type scheduleBatchActionRequest struct {
	IDs []uint `json:"ids"`
	Act string `json:"act"`
}

func GetScheduleTasks(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req scheduleListRequest
	_ = middleware.BindDecryptedData(c, &req)
	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	type row struct {
		models.ScheduleTask
		Domain string `json:"domain"`
	}
	q := database.WithContext(c).Table("schedule_tasks as a").
		Select("a.*, b.name as domain").
		Joins("join domains b on a.did = b.id")
	if kw := strings.TrimSpace(req.Keyword); kw != "" {
		like := "%" + kw + "%"
		q = q.Where("a.rr LIKE ? OR b.name LIKE ? OR a.remark LIKE ? OR a.value LIKE ? OR a.record_id = ?", like, like, like, like, kw)
	}
	if req.Type != "" {
		q = q.Where("a.type = ?", req.Type)
	}
	if !isAdmin(c) {
		sql, args := userAccessibleDomainIDs(currentUID(c))
		q = q.Where("a.did IN ("+sql+")", args...)
	}
	var total int64
	q.Count(&total)
	var list []row
	q.Order("a.id desc").Offset((req.Page-1)*req.PageSize).Limit(req.PageSize).Find(&list)
	middleware.SuccessResponse(c, gin.H{"total": total, "list": list})
}

func CreateScheduleTask(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req scheduleTaskPayload
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.DomainID == 0 || strings.TrimSpace(req.RR) == "" || strings.TrimSpace(req.RecordID) == "" {
		middleware.ErrorResponse(c, "必填项不能为空")
		return
	}
	if !ensureDomainWrite(c, strconv.FormatUint(uint64(req.DomainID), 10), req.RR) {
		return
	}
	var count int64
	database.WithContext(c).Model(&models.ScheduleTask{}).Where("record_id = ? AND switch_type = ? AND switch_time = ?", req.RecordID, req.SwitchType, req.SwitchTime).Count(&count)
	if count > 0 {
		middleware.ErrorResponse(c, "当前定时切换策略已存在")
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	task := models.ScheduleTask{DomainID: req.DomainID, RR: strings.TrimSpace(req.RR), RecordID: strings.TrimSpace(req.RecordID), Type: req.Type, Cycle: req.Cycle, SwitchType: req.SwitchType, SwitchDate: strings.TrimSpace(req.SwitchDate), SwitchTime: strings.TrimSpace(req.SwitchTime), Value: strings.TrimSpace(req.Value), Line: strings.TrimSpace(req.Line), Remark: strings.TrimSpace(req.Remark), RecordInfo: strings.TrimSpace(req.RecordInfo), AddTime: time.Now().Unix(), Active: active}
	if err := database.WithContext(c).Create(&task).Error; err != nil {
		middleware.ErrorResponse(c, "添加失败")
		return
	}
	sr := service.NewScheduleRunner()
	sr.Stop()
	service.UpdateScheduleNextTime(&task)
	middleware.SuccessResponse(c, gin.H{"id": task.ID})
}

func UpdateScheduleTask(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	id := c.Param("id")
	var task models.ScheduleTask
	if err := database.WithContext(c).First(&task, id).Error; err != nil {
		middleware.ErrorResponse(c, "策略不存在")
		return
	}
	if !ensureDomainWrite(c, strconv.FormatUint(uint64(task.DomainID), 10), task.RR) {
		return
	}
	var req scheduleTaskPayload
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if req.DomainID == 0 || strings.TrimSpace(req.RR) == "" || strings.TrimSpace(req.RecordID) == "" {
		middleware.ErrorResponse(c, "必填项不能为空")
		return
	}
	var count int64
	database.WithContext(c).Model(&models.ScheduleTask{}).Where("record_id = ? AND switch_type = ? AND switch_time = ? AND id <> ?", req.RecordID, req.SwitchType, req.SwitchTime, task.ID).Count(&count)
	if count > 0 {
		middleware.ErrorResponse(c, "当前定时切换策略已存在")
		return
	}
	updates := map[string]interface{}{"did": req.DomainID, "rr": strings.TrimSpace(req.RR), "record_id": strings.TrimSpace(req.RecordID), "type": req.Type, "cycle": req.Cycle, "switch_type": req.SwitchType, "switch_date": strings.TrimSpace(req.SwitchDate), "switch_time": strings.TrimSpace(req.SwitchTime), "value": strings.TrimSpace(req.Value), "line": strings.TrimSpace(req.Line), "remark": strings.TrimSpace(req.Remark), "record_info": strings.TrimSpace(req.RecordInfo)}
	if req.Active != nil {
		updates["active"] = *req.Active
	}
	if err := database.WithContext(c).Model(&task).Updates(updates).Error; err != nil {
		middleware.ErrorResponse(c, "修改失败")
		return
	}
	database.WithContext(c).First(&task, task.ID)
	service.UpdateScheduleNextTime(&task)
	middleware.SuccessMsg(c, "修改成功")
}

func DeleteScheduleTask(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	id := c.Param("id")
	var task models.ScheduleTask
	if err := database.WithContext(c).First(&task, id).Error; err != nil {
		middleware.ErrorResponse(c, "策略不存在")
		return
	}
	if !ensureDomainWrite(c, strconv.FormatUint(uint64(task.DomainID), 10), task.RR) {
		return
	}
	database.WithContext(c).Delete(&task)
	middleware.SuccessMsg(c, "删除成功")
}

func ToggleScheduleTask(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	id := c.Param("id")
	var task models.ScheduleTask
	if err := database.WithContext(c).First(&task, id).Error; err != nil {
		middleware.ErrorResponse(c, "策略不存在")
		return
	}
	if !ensureDomainWrite(c, strconv.FormatUint(uint64(task.DomainID), 10), task.RR) {
		return
	}
	active := !task.Active
	if v := c.Query("active"); v != "" {
		active = v == "1" || strings.EqualFold(v, "true")
	}
	database.WithContext(c).Model(&task).Update("active", active)
	middleware.SuccessResponse(c, gin.H{"active": active})
}

func BatchScheduleTaskAction(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req scheduleBatchActionRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if len(req.IDs) == 0 {
		middleware.ErrorResponse(c, "未选择任务")
		return
	}
	success := 0
	for _, id := range req.IDs {
		var task models.ScheduleTask
		if err := database.WithContext(c).First(&task, id).Error; err != nil {
			continue
		}
		if !ensureDomainWrite(c, strconv.FormatUint(uint64(task.DomainID), 10), task.RR) {
			continue
		}
		switch req.Act {
		case "delete":
			database.WithContext(c).Delete(&task)
			success++
		case "open":
			database.WithContext(c).Model(&task).Update("active", true)
			success++
		case "close":
			database.WithContext(c).Model(&task).Update("active", false)
			success++
		}
	}
	middleware.SuccessMsg(c, "成功操作"+strconv.Itoa(success)+"个定时切换策略")
}
