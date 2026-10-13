package handler

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"main/internal/service"
	"main/internal/sysconfig"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

/*
 * 优选IP 接口层
 */

/* optimizeIPConfigKeys 允许读写的配置键 */
var optimizeIPConfigKeys = []string{
	service.OptimizeCfgAPI,
	service.OptimizeCfgKey,
	service.OptimizeCfgProxy,
	service.OptimizeCfgMin,
}

/* GetOptimizeIPConfig 读取优选IP 配置 */
func GetOptimizeIPConfig(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	result := make(map[string]string, len(optimizeIPConfigKeys))
	for _, k := range optimizeIPConfigKeys {
		result[k] = sysconfig.GetValue(k)
	}
	if result[service.OptimizeCfgKey] == "" {
		result[service.OptimizeCfgKey] = "o1zrmHAF"
	}
	if result[service.OptimizeCfgMin] == "" {
		result[service.OptimizeCfgMin] = "30"
	}
	middleware.SuccessResponse(c, result)
}

/* UpdateOptimizeIPConfig 保存优选IP 配置 */
func UpdateOptimizeIPConfig(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var req map[string]interface{}
	if len(middleware.GetDecryptedData(c)) > 0 {
		req = middleware.GetDecryptedData(c)
	} else if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	allowed := make(map[string]bool, len(optimizeIPConfigKeys))
	for _, k := range optimizeIPConfigKeys {
		allowed[k] = true
	}

	for key, raw := range req {
		if !allowed[key] {
			continue
		}
		value := fmt.Sprintf("%v", raw)
		if key == service.OptimizeCfgMin {
			n, err := strconv.Atoi(strings.TrimSpace(value))
			if err != nil || n < 10 {
				middleware.ErrorResponse(c, "自动更新时间间隔不能小于10分钟")
				return
			}
		}
		sysconfig.SetValue(key, value)
	}
	middleware.SuccessMsg(c, "保存成功")
}

/* QueryOptimizeIPLicense 查询接口剩余可用次数 */
func QueryOptimizeIPLicense(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var req struct {
		API int    `json:"optimize_ip_api"`
		Key string `json:"optimize_ip_key"`
	}
	if len(middleware.GetDecryptedData(c)) > 0 {
		_ = middleware.BindDecryptedData(c, &req)
	} else if err := c.ShouldBindJSON(&req); err != nil {
		middleware.ErrorResponse(c, "参数错误")
		return
	}
	if strings.TrimSpace(req.Key) == "" {
		middleware.ErrorResponse(c, "参数不能为空")
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 15*time.Second)
	defer cancel()
	remain, err := service.GetOptimizeLicense(ctx, req.API, strings.TrimSpace(req.Key))
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}
	middleware.SuccessMsg(c, "当前积分余额："+remain)
}

/* optimizeIPListRequest 优选IP 列表查询参数 */
type optimizeIPListRequest struct {
	Type      int    `json:"type"`      // 搜索字段：1=域名 2=备注
	Keyword   string `json:"kw"`        // 搜索关键字
	Status    string `json:"status"`    // 上次更新结果：1=成功 2=失败
	Offset    int    `json:"offset"`    // 起始偏移
	Limit     int    `json:"limit"`     // 每页数量
	SortName  string `json:"sortName"`  // 排序字段
	SortOrder string `json:"sortOrder"` // 排序方向 asc/desc
}

/* optimizeIPRow 列表行，内嵌任务并附带域名名称 */
type optimizeIPRow struct {
	models.OptimizeIP
	Domain string `json:"domain"`
}

/* GetOptimizeIPList 优选IP 任务列表 */
func GetOptimizeIPList(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var req optimizeIPListRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数错误")
		return
	}
	if req.Limit <= 0 {
		req.Limit = 20
	}
	if req.Limit > 200 {
		req.Limit = 200
	}
	if req.Offset < 0 {
		req.Offset = 0
	}

	// 计数与取数各自独立构造查询：Count 会污染链式条件，复用同一 *gorm.DB 会让后续 Select 带上聚合上下文
	buildQuery := func() *gorm.DB {
		q := database.WithContext(c).Table("optimize_ips AS A").Joins("JOIN domains B ON A.did = B.id")
		kw := strings.TrimSpace(req.Keyword)
		if kw != "" {
			like := "%" + kw + "%"
			switch req.Type {
			case 1:
				q = q.Where("A.rr LIKE ? OR B.name LIKE ?", like, like)
			case 2:
				q = q.Where("A.remark LIKE ?", like)
			}
		}
		if strings.TrimSpace(req.Status) != "" {
			if n, err := strconv.Atoi(strings.TrimSpace(req.Status)); err == nil {
				q = q.Where("A.status = ?", n)
			}
		}
		return q
	}

	var total int64
	buildQuery().Count(&total)

	allowedSort := map[string]string{
		"id": "A.id", "rr": "A.rr", "cdn_type": "A.cdn_type", "recordnum": "A.record_num",
		"ip_type": "A.ip_type", "active": "A.active", "updatetime": "A.update_time", "status": "A.status",
	}
	orderBy := "A.id DESC"
	if col, ok := allowedSort[strings.TrimSpace(req.SortName)]; ok {
		dir := "DESC"
		if strings.EqualFold(req.SortOrder, "asc") {
			dir = "ASC"
		}
		orderBy = col + " " + dir
	}

	var rows []optimizeIPRow
	if err := buildQuery().Select("A.*, B.name AS domain").Order(orderBy).
		Offset(req.Offset).Limit(req.Limit).Scan(&rows).Error; err != nil {
		middleware.ErrorResponse(c, "查询失败")
		return
	}
	middleware.SuccessResponse(c, gin.H{"total": total, "list": rows})
}

/* optimizeIPTaskRequest 任务新增/编辑参数 */
type optimizeIPTaskRequest struct {
	DomainID  uint   `json:"did"`
	RR        string `json:"rr"`
	Type      int    `json:"type"`
	IPType    string `json:"ip_type"`
	CDNType   int    `json:"cdn_type"`
	RecordNum int    `json:"recordnum"`
	TTL       int    `json:"ttl"`
	Remark    string `json:"remark"`
}

/* validateOptimizeIPTask 校验任务参数，返回空串表示通过 */
func validateOptimizeIPTask(req *optimizeIPTaskRequest, excludeID uint) string {
	req.RR = strings.TrimSpace(req.RR)
	req.IPType = strings.TrimSpace(req.IPType)
	req.Remark = strings.TrimSpace(req.Remark)

	if req.DomainID == 0 || req.RR == "" || req.IPType == "" || req.RecordNum == 0 || req.TTL == 0 {
		return "必填项不能为空"
	}
	if req.RecordNum < 1 {
		return "解析数量不能少于1个"
	}
	if req.RecordNum > 50 {
		return "解析数量不能超过50个"
	}
	var count int64
	query := database.DB.Model(&models.OptimizeIP{}).Where("did = ? AND rr = ?", req.DomainID, req.RR)
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	query.Count(&count)
	if count > 0 {
		return "当前域名的优选IP任务已存在"
	}
	return ""
}

/* CreateOptimizeIPTask 新增优选任务 */
func CreateOptimizeIPTask(c *gin.Context) {
	/* 配额校验：优选 IP 任务数 */
	if !CheckQuota(c, "optimize_ip") {
		return
	}
	if !requireAdmin(c) {
		return
	}
	var req optimizeIPTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数错误")
		return
	}
	if msg := validateOptimizeIPTask(&req, 0); msg != "" {
		middleware.ErrorResponse(c, msg)
		return
	}

	task := models.OptimizeIP{
		DomainID:  req.DomainID,
		RR:        req.RR,
		Type:      req.Type,
		IPType:    req.IPType,
		CDNType:   req.CDNType,
		RecordNum: req.RecordNum,
		TTL:       req.TTL,
		Remark:    req.Remark,
		AddTime:   time.Now(),
		Active:    true,
	}
	if err := database.WithContext(c).Create(&task).Error; err != nil {
		middleware.ErrorResponse(c, "添加失败")
		return
	}
	middleware.SuccessMsg(c, "添加成功")
}

/* UpdateOptimizeIPTask 修改优选任务 */
func UpdateOptimizeIPTask(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var task models.OptimizeIP
	if err := database.WithContext(c).First(&task, id).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return
	}

	var req optimizeIPTaskRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数错误")
		return
	}
	if msg := validateOptimizeIPTask(&req, uint(id)); msg != "" {
		middleware.ErrorResponse(c, msg)
		return
	}

	database.WithContext(c).Model(&models.OptimizeIP{}).Where("id = ?", id).Updates(map[string]interface{}{
		"did":        req.DomainID,
		"rr":         req.RR,
		"type":       req.Type,
		"ip_type":    req.IPType,
		"cdn_type":   req.CDNType,
		"record_num": req.RecordNum,
		"ttl":        req.TTL,
		"remark":     req.Remark,
	})
	middleware.SuccessMsg(c, "修改成功")
}

/* DeleteOptimizeIPTask 删除优选任务 */
func DeleteOptimizeIPTask(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	database.WithContext(c).Where("id = ?", id).Delete(&models.OptimizeIP{})
	middleware.SuccessMsg(c, "删除成功")
}

/* ToggleOptimizeIPTask 启用/停用优选任务 */
func ToggleOptimizeIPTask(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var req struct {
		Active *bool `json:"active"`
	}
	if err := middleware.BindDecryptedData(c, &req); err != nil || req.Active == nil {
		middleware.ErrorResponse(c, "参数错误")
		return
	}
	database.WithContext(c).Model(&models.OptimizeIP{}).Where("id = ?", id).Update("active", *req.Active)
	middleware.SuccessMsg(c, "设置成功")
}

/* RunOptimizeIPTask 手动执行优选任务 */
func RunOptimizeIPTask(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
	var task models.OptimizeIP
	if err := database.WithContext(c).First(&task, id).Error; err != nil {
		middleware.ErrorResponse(c, "任务不存在")
		return
	}

	// 单次执行可能涉及多次 DNS 接口调用，超时放宽到 2 分钟
	ctx, cancel := context.WithTimeout(c.Request.Context(), 120*time.Second)
	defer cancel()
	result, err := service.RunOptimizeTask(ctx, &task)
	if err != nil {
		middleware.ErrorResponse(c, "优选任务执行失败："+err.Error())
		return
	}
	middleware.SuccessMsg(c, "优选任务执行成功："+result.String())
}

/*
 * GetOptimizeIPStatus 优选IP 运行状态
 */
func GetOptimizeIPStatus(c *gin.Context) {
	// 取启用中任务里最近一次更新时间
	var tasks []models.OptimizeIP
	database.DB.Where("active = ?", true).Order("update_time DESC").Limit(1).Find(&tasks)

	var latest *time.Time
	if len(tasks) > 0 {
		latest = tasks[0].UpdateTime
	}
	c.String(http.StatusOK, optimizeRunState(latest, time.Now()))
}

func optimizeRunState(latest *time.Time, now time.Time) string {
	if latest != nil && latest.Add(time.Hour).After(now) {
		return "ok"
	}
	return "error"
}
