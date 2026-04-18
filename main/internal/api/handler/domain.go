package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/dns"
	"main/internal/models"
	"main/internal/service"
	"main/internal/utils"
	"sync"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// domainWithAccountRow 域名列表 JOIN 账户后的扫描结构
type domainWithAccountRow struct {
	models.Domain
	AccountName string `json:"account_name"`
	AccountType string `json:"account_type"`
}

/*
 * getProviderByDomain 根据域名获取 DNS provider
 * 功能：统一"加载 account → 解析 config → 创建 provider"的重复模式
 * 返回 provider 和 domain，出错时直接写入 HTTP 响应并返回 nil
 */
func getProviderByDomain(c *gin.Context, domain *models.Domain) dns.Provider {
	var account models.Account
	if err := database.WithContext(c).Where("id = ?", domain.AccountID).First(&account).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return nil
	}
	var config map[string]string
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		middleware.ErrorResponse(c, "账户配置解析失败")
		return nil
	}
	provider, err := dns.GetProvider(account.Type, config, domain.Name, domain.ThirdID)
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return nil
	}
	return provider
}

/* normalizeDNSListStatusForAPI 将前端/通用状态转为多数云 DNS API 使用的 ENABLE/DISABLE，空则不过滤 */
func normalizeDNSListStatusForAPI(status string) string {
	s := strings.TrimSpace(strings.ToLower(status))
	if s == "" {
		return ""
	}
	if s == "1" || s == "enable" || s == "on" {
		return "ENABLE"
	}
	if s == "0" || s == "disable" || s == "off" {
		return "DISABLE"
	}
	u := strings.ToUpper(strings.TrimSpace(status))
	if u == "ENABLE" || u == "PAUSE" || u == "DISABLE" {
		if u == "PAUSE" {
			return "DISABLE"
		}
		return u
	}
	return ""
}

type GetDomainsRequest struct {
	Keyword  string `json:"keyword"`
	AID      string `json:"aid"`
	Page     int    `json:"page"`
	PageSize int    `json:"page_size"`
}

func buildDomainsListQuery(c *gin.Context, req *GetDomainsRequest) *gorm.DB {
	userID := c.GetString("user_id")
	userLevel := c.GetInt("level")

	q := database.WithContext(c).Table("domains").
		Select("domains.*, accounts.name as account_name, accounts.type as account_type").
		Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
		Where("domains.deleted_at IS NULL")

	if userLevel < 2 {
		q = q.Where(
			"domains.aid IN (SELECT id FROM accounts WHERE uid = ? AND deleted_at IS NULL) OR domains.name IN (SELECT domain FROM permissions WHERE uid = ?)",
			userID, userID,
		)
	}
	if req.Keyword != "" {
		q = q.Where("domains.name LIKE ?", "%"+req.Keyword+"%")
	}
	if req.AID != "" {
		q = q.Where("domains.aid = ?", req.AID)
	}
	return q
}

/*
 * GetDomains 获取域名列表
 * @route POST /domains/list
 * 功能：分页查询用户可见的域名列表，支持关键词搜索和按账户筛选
 */
func GetDomains(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req GetDomainsRequest
	middleware.BindDecryptedData(c, &req)

	if req.Page <= 0 {
		req.Page = 1
	}
	if req.PageSize <= 0 {
		req.PageSize = 20
	}
	if req.PageSize > 200 {
		req.PageSize = 200
	}

	userID := c.GetString("user_id")
	userLevel := c.GetInt("level")

	var total int64
	var domains []domainWithAccountRow
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		buildDomainsListQuery(c, &req).Count(&total)
	}()
	go func() {
		defer wg.Done()
		buildDomainsListQuery(c, &req).Order("domains.id DESC").Offset((req.Page - 1) * req.PageSize).Limit(req.PageSize).Find(&domains)
	}()
	wg.Wait()

	domainIDs := make([]uint, 0, len(domains))
	domainNames := make([]string, 0, len(domains))
	for _, d := range domains {
		domainIDs = append(domainIDs, d.ID)
		domainNames = append(domainNames, d.Name)
	}
	noteMap := make(map[uint]string)
	permMap := make(map[string]string)
	var notes []models.DomainNote
	var perms []models.Permission
	var wgNotes sync.WaitGroup
	if len(domainIDs) > 0 {
		wgNotes.Add(1)
		go func() {
			defer wgNotes.Done()
			uidN, _ := strconv.ParseUint(userID, 10, 32)
			database.WithContext(c).Where("uid = ? AND did IN ?", uint(uidN), domainIDs).Find(&notes)
		}()
	}
	if userLevel < 2 && len(domainNames) > 0 {
		wgNotes.Add(1)
		go func() {
			defer wgNotes.Done()
			database.WithContext(c).Where("uid = ? AND domain IN ?", userID, domainNames).Find(&perms)
		}()
	}
	wgNotes.Wait()
	for _, n := range notes {
		noteMap[n.DomainID] = n.Remark
	}
	for _, p := range perms {
		permMap[p.Domain] = p.SubDomain
	}

	// 转换为前端期望的格式
	result := make([]gin.H, 0, len(domains))
	for _, d := range domains {
		// 获取账户类型名称
		typeName := ""
		if cfg, ok := dns.GetProviderConfig(d.AccountType); ok {
			typeName = cfg.Name
		}
		// 用户独立备注优先，否则显示域名全局备注
		remark := d.Remark
		if userNote, ok := noteMap[d.ID]; ok {
			remark = userNote
		}
		item := gin.H{
			"id":           d.ID,
			"aid":          d.AccountID,
			"name":         d.Name,
			"third_id":     d.ThirdID,
			"is_hide":      d.IsHide,
			"is_sso":       d.IsSSO,
			"record_count": d.RecordCount,
			"remark":       remark,
			"is_notice":    d.IsNotice,
			"expire_time":  d.ExpireTime,
			"created_at":   d.CreatedAt,
			"account_name": d.AccountName,
			"account_type": d.AccountType,
			"type_name":    typeName,
		}
		// 非管理员：附加子域名权限信息
		if sub, ok := permMap[d.Name]; ok {
			item["perm_sub"] = sub
		}
		result = append(result, item)
	}

	middleware.SuccessResponse(c, gin.H{"total": total, "list": result})
}

/*
 * GetDomainDetail 获取单个域名详情
 * @route GET /domains/:id（路径 id）；兼容 body/query 中的 id
 * 功能：根据域名ID返回域名信息，避免前端拉取全量列表查找单条记录
 */
func GetDomainDetail(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req struct {
		ID string `json:"id" form:"id"`
	}
	middleware.BindDecryptedData(c, &req)
	if req.ID == "" {
		req.ID = c.Param("id")
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限查看该域名")
		return
	}

	var account models.Account
	accountName := ""
	accountType := ""
	typeName := ""
	if err := database.WithContext(c).Where("id = ?", domain.AccountID).First(&account).Error; err == nil {
		accountName = account.Name
		accountType = account.Type
		if cfg, ok := dns.GetProviderConfig(account.Type); ok {
			typeName = cfg.Name
		}
	}

	middleware.SuccessResponse(c, gin.H{
		"id":           domain.ID,
		"aid":          domain.AccountID,
		"name":         domain.Name,
		"third_id":     domain.ThirdID,
		"record_count": domain.RecordCount,
		"remark":       domain.Remark,
		"expire_time":  domain.ExpireTime,
		"account_name": accountName,
		"account_type": accountType,
		"type_name":    typeName,
	})
}

type CreateDomainRequest struct {
	AccountID string `json:"account_id" binding:"required"`
	Name      string `json:"name" binding:"required"`
}

/*
 * CreateDomain 添加域名
 * @route POST /domains/create
 * 功能：管理员通过 DNS 账户添加域名，自动获取第三方平台域名 ID
 */
func CreateDomain(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req CreateDomainRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	/* 套餐配额检查 */
	if !CheckQuota(c, currentUID(c), "domains") {
		return
	}

	if req.AccountID == "" || req.Name == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	var account models.Account
	if err := database.WithContext(c).Where("id = ?", req.AccountID).First(&account).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	var config map[string]string
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		middleware.ErrorResponse(c, "账户配置解析失败")
		return
	}

	provider, err := dns.GetProvider(account.Type, config, req.Name, "")
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
	result, err := provider.GetDomainList(ctx, req.Name, 1, 100)
	if err != nil {
		middleware.ErrorResponse(c, "获取域名失败: "+err.Error())
		return
	}

	domainList, ok := result.Records.([]dns.DomainInfo)
	if !ok {
		middleware.ErrorResponse(c, "域名列表格式异常")
		return
	}
	var thirdID string
	for _, d := range domainList {
		if d.Name == req.Name {
			thirdID = d.ID
			break
		}
	}

	if thirdID == "" {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	aid, err := strconv.ParseUint(req.AccountID, 10, 32)
	if err != nil {
		middleware.ErrorResponse(c, "无效的账户ID")
		return
	}

	domain := models.Domain{
		AccountID: uint(aid),
		Name:      req.Name,
		ThirdID:   thirdID,
	}

	if err := database.WithContext(c).Create(&domain).Error; err != nil {
		middleware.ErrorResponse(c, "创建失败")
		return
	}

	middleware.SuccessResponse(c, gin.H{"id": domain.ID})
}

type SyncDomainsRequest struct {
	AccountID string `json:"aid" binding:"required"`
	Domains   []struct {
		Name        string      `json:"name"`
		ID          interface{} `json:"id"` // 可能是 string 或 float64
		RecordCount int         `json:"record_count"`
	} `json:"domains"`
}

/* getSyncDomainID 从 interface{} 提取域名 ID 字符串（兼容 string/float64 类型） */
func getSyncDomainID(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%v", val)
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", val)
	}
}

/*
 * SyncDomains 同步域名列表
 * @route POST /domains/sync
 * 功能：管理员从 DNS 账户同步域名，自动创建/删除本地域名记录
 */
func SyncDomains(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	// 与 BindDecryptedData 一致：加密链路用 decrypted_data；否则直接解析 JSON body（明文 API 客户端）
	var rawData map[string]interface{}
	if d := middleware.GetDecryptedData(c); d != nil {
		rawData = d
	} else {
		if err := c.ShouldBindJSON(&rawData); err != nil {
			middleware.ErrorResponse(c, "参数解析失败")
			return
		}
	}
	if rawData == nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	accountID := ""
	if v, ok := rawData["aid"].(string); ok {
		accountID = v
	} else if v, ok := rawData["aid"].(float64); ok {
		accountID = fmt.Sprintf("%.0f", v)
	} else if v, ok := rawData["account_id"].(string); ok {
		accountID = v
	} else if v, ok := rawData["account_id"].(float64); ok {
		accountID = fmt.Sprintf("%.0f", v)
	}
	if accountID == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	var account models.Account
	if err := database.WithContext(c).Where("id = ?", accountID).First(&account).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	// 提取 domains 数组
	rawDomains, ok := rawData["domains"].([]interface{})
	if !ok || len(rawDomains) == 0 {
		middleware.ErrorResponse(c, "没有要同步的域名")
		return
	}

	/* 预加载该账户下所有已有域名，避免循环内逐条查询（N+1 优化） */
	var existingDomains []models.Domain
	database.WithContext(c).Where("aid = ?", accountID).Find(&existingDomains)
	existingMap := make(map[string]*models.Domain, len(existingDomains))
	for i := range existingDomains {
		existingMap[existingDomains[i].Name] = &existingDomains[i]
	}

	added := 0
	updated := 0
	for _, item := range rawDomains {
		dm, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name, _ := dm["name"].(string)
		domainID := getSyncDomainID(dm["id"])
		recordCount := 0
		if v, ok := dm["record_count"].(float64); ok {
			recordCount = int(v)
		}
		if name == "" {
			continue
		}

		if existing, ok := existingMap[name]; ok {
			// 域名已存在，更新缺失的 ThirdID 和 RecordCount
			updates := map[string]interface{}{}
			if existing.ThirdID == "" && domainID != "" {
				updates["third_id"] = domainID
			}
			if recordCount > 0 {
				updates["record_count"] = recordCount
			}
			if len(updates) > 0 {
				database.WithContext(c).Model(existing).Updates(updates)
				updated++
			}
		} else {
			// 域名不存在，创建
			aidU, err := strconv.ParseUint(accountID, 10, 32)
			if err != nil {
				continue
			}
			domain := models.Domain{
				AccountID:   uint(aidU),
				Name:        name,
				ThirdID:     domainID,
				RecordCount: recordCount,
			}
			database.WithContext(c).Create(&domain)
			added++
		}
	}

	middleware.SuccessResponse(c, gin.H{"added": added, "updated": updated})
}

type DeleteDomainRequest struct {
	ID string `json:"id"`
}

/*
 * DeleteDomain 删除域名
 * @route POST /domains/delete
 * 功能：管理员删除域名及其关联的监控任务、优化 IP、定时任务
 */
func DeleteDomain(c *gin.Context) {
	if !checkAdmin(c) {
		middleware.ErrorResponse(c, "无权限，仅管理员可操作")
		return
	}

	var req DeleteDomainRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		req.ID = c.Param("id")
	}
	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	// 查找域名信息（用于清理关联数据和审计日志）
	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	// 清理关联数据
	database.WithContext(c).Where("domain = ?", domain.Name).Delete(&models.Permission{})
	/* 清理监控任务及其日志 */
	var taskIDs []string
	database.WithContext(c).Model(&models.DMTask{}).Where("did = ?", req.ID).Pluck("id", &taskIDs)
	if len(taskIDs) > 0 {
		database.WithContext(c).Where("task_id IN ?", taskIDs).Delete(&models.DMLog{})
		database.LogDB.Where("task_id IN ?", taskIDs).Delete(&models.DMCheckLog{})
	}
	database.WithContext(c).Where("did = ?", req.ID).Delete(&models.DMTask{})
	database.WithContext(c).Where("did = ?", req.ID).Delete(&models.CertCNAME{})
	database.WithContext(c).Where("did = ?", req.ID).Delete(&models.ScheduleTask{})
	database.WithContext(c).Where("did = ?", req.ID).Delete(&models.OptimizeIP{})
	database.WithContext(c).Where("did = ?", req.ID).Delete(&models.DomainNote{})

	database.WithContext(c).Delete(&models.Domain{}, req.ID)

	service.Audit.LogAction(c, "delete_domain", domain.Name, fmt.Sprintf("删除域名: %s", domain.Name))
	middleware.SuccessMsg(c, "删除成功")
}

type GetRecordsRequest struct {
	DomainID   string `json:"domain_id" form:"domain_id"`
	Page       int    `json:"page" form:"page"`
	PageSize   int    `json:"page_size" form:"page_size"`
	Keyword    string `json:"keyword" form:"keyword"`
	RecordType string `json:"type" form:"type"`
	Line       string `json:"line" form:"line"`
	Status     string `json:"status" form:"status"`       // 1/0、enable/disable，或 ENABLE/DISABLE
	SubDomain  string `json:"subdomain" form:"subdomain"` // 主机记录（前缀）筛选，与 dnsmgr subdomain 一致
	Value      string `json:"value" form:"value"`         // 记录值模糊
}

/*
 * GetRecords 获取域名解析记录
 * @route POST /domains/records
 * 功能：从 DNS 服务商查询域名解析记录，支持子域名权限过滤和分页
 */
func GetRecords(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req GetRecordsRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限查看该域名记录")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
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

	// 获取子域名权限限制（由 Permission 中间件设置）
	permSubDomain, _ := c.Get("perm_sub_domain")
	subDomainFilter := ""
	if v, ok := permSubDomain.(string); ok {
		subDomainFilter = v
	}
	var allowedSubs []string
	if subDomainFilter != "" && subDomainFilter != "*" {
		for _, s := range strings.Split(subDomainFilter, ",") {
			s = strings.TrimSpace(s)
			if s != "" {
				allowedSubs = append(allowedSubs, s)
			}
		}
	}

	// 转换为前端期望的格式
	type RecordItem struct {
		RecordId string `json:"RecordId"`
		Name     string `json:"Name"`
		Type     string `json:"Type"`
		Value    string `json:"Value"`
		Line     string `json:"Line"`
		LineName string `json:"LineName,omitempty"`
		TTL      int    `json:"TTL"`
		MX       int    `json:"MX,omitempty"`
		Weight   int    `json:"Weight,omitempty"`
		Status   string `json:"Status"`
		Remark   string `json:"Remark,omitempty"`
	}

	lineNameByID := map[string]string{}
	if lines, lerr := provider.GetRecordLine(c.Request.Context()); lerr == nil {
		for _, ln := range lines {
			lineNameByID[ln.ID] = ln.Name
		}
	}

	statusAPI := normalizeDNSListStatusForAPI(req.Status)

	mapOne := func(r dns.Record) RecordItem {
		status := "1"
		if r.Status == "disable" {
			status = "0"
		}
		ln := lineNameByID[r.Line]
		if ln == "" {
			ln = r.Line
		}
		return RecordItem{
			RecordId: r.ID, Name: r.Name, Type: r.Type, Value: r.Value,
			Line: r.Line, LineName: ln, TTL: r.TTL, MX: r.MX, Weight: r.Weight,
			Status: status, Remark: r.Remark,
		}
	}

	mapRecords := func(records []dns.Record) []RecordItem {
		items := make([]RecordItem, 0, len(records))
		for _, r := range records {
			items = append(items, mapOne(r))
		}
		return items
	}

	/* 子域多路合并后的本地精筛（keyword 已由各次 API 传入；此处不再按 keyword 缩窄，避免与服务商语义重复） */
	filterLocal := func(items []RecordItem) []RecordItem {
		if len(items) == 0 {
			return items
		}
		out := make([]RecordItem, 0, len(items))
		for _, it := range items {
			if v := strings.TrimSpace(req.SubDomain); v != "" && !strings.EqualFold(it.Name, v) {
				continue
			}
			if v := strings.TrimSpace(req.Value); v != "" {
				if !strings.Contains(strings.ToLower(it.Value), strings.ToLower(v)) {
					continue
				}
			}
			if v := strings.TrimSpace(req.Line); v != "" && it.Line != v {
				continue
			}
			if want, ok := recordItemStatusWant(req.Status); ok && it.Status != want {
				continue
			}
			if v := strings.TrimSpace(req.RecordType); v != "" && !strings.EqualFold(it.Type, v) {
				continue
			}
			out = append(out, it)
		}
		return out
	}

	var list []RecordItem
	var total int
	ctx := c.Request.Context()

	if len(allowedSubs) > 0 {
		const perSubLimit = 500
		for _, sub := range allowedSubs {
			subResult, subErr := provider.GetDomainRecords(ctx, 1, perSubLimit, req.Keyword, sub, req.Value, req.RecordType, req.Line, statusAPI)
			if subErr != nil {
				continue
			}
			if records, ok := subResult.Records.([]dns.Record); ok {
				list = append(list, mapRecords(records)...)
			}
		}
		list = filterLocal(list)
		total = len(list)
		start := (page - 1) * pageSize
		if start < len(list) {
			end := start + pageSize
			if end > len(list) {
				end = len(list)
			}
			list = list[start:end]
		} else {
			list = nil
		}
	} else {
		subForAPI := strings.TrimSpace(req.SubDomain)
		result, err := provider.GetDomainRecords(ctx, page, pageSize, req.Keyword, subForAPI, req.Value, req.RecordType, req.Line, statusAPI)
		if err != nil {
			middleware.ErrorResponse(c, "获取记录失败: "+err.Error())
			return
		}

		if result.Total > 0 {
			database.WithContext(c).Model(&domain).Update("record_count", result.Total)
		}

		if records, ok := result.Records.([]dns.Record); ok {
			list = mapRecords(records)
		}
		total = result.Total
	}

	middleware.SuccessResponse(c, gin.H{"total": total, "list": list})
}

/* recordItemStatusWant 解析筛选状态，返回值 "1"/"0"；无效应过滤时返回 false */
func recordItemStatusWant(raw string) (string, bool) {
	s := strings.TrimSpace(strings.ToLower(raw))
	if s == "" {
		return "", false
	}
	if s == "1" || s == "enable" || s == "on" {
		return "1", true
	}
	if s == "0" || s == "disable" || s == "off" {
		return "0", true
	}
	if u := strings.ToUpper(strings.TrimSpace(raw)); u == "ENABLE" {
		return "1", true
	}
	if u := strings.ToUpper(strings.TrimSpace(raw)); u == "DISABLE" || u == "PAUSE" {
		return "0", true
	}
	return "", false
}

type CreateRecordRequest struct {
	DomainID string `json:"domain_id"`
	Name     string `json:"name" binding:"required"`
	Type     string `json:"type" binding:"required"`
	Value    string `json:"value" binding:"required"`
	Line     string `json:"line"`
	TTL      int    `json:"ttl"`
	MX       int    `json:"mx"`
	Weight   *int   `json:"weight"`
	Remark   string `json:"remark"`
}

/*
 * CreateRecord 添加解析记录
 * @route POST /domains/records/create
 * 功能：异步添加 DNS 解析记录，提取上下文后立即返回，后台执行
 */
func CreateRecord(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req CreateRecordRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 子域名权限校验：非管理员检查是否有权操作该子域名 */
	if !middleware.CheckSubDomainPermission(c.GetString("user_id"), c.GetInt("level"), req.DomainID, req.Name) {
		middleware.ErrorResponse(c, "无权操作该子域名")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}

	if req.Line == "" {
		req.Line = "default"
	}
	if req.TTL == 0 {
		req.TTL = 600
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		_, err := provider.AddDomainRecord(ctx, req.Name, req.Type, req.Value, req.Line, req.TTL, req.MX, req.Weight, req.Remark)
		if err != nil {
			service.Audit.LogActionDirect(userID, username, ip, ua, "add_record_failed", domainName, fmt.Sprintf("%s.%s -> %s 失败: %s", req.Name, domainName, req.Value, err.Error()))
			return
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "add_record", domainName, fmt.Sprintf("%s.%s -> %s", req.Name, domainName, req.Value))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type UpdateRecordRequest struct {
	DomainID string `json:"domain_id"`
	RecordID string `json:"record_id"`
	Name     string `json:"name" binding:"required"`
	Type     string `json:"type" binding:"required"`
	Value    string `json:"value" binding:"required"`
	Line     string `json:"line"`
	TTL      int    `json:"ttl"`
	MX       int    `json:"mx"`
	Weight   *int   `json:"weight"`
	Remark   string `json:"remark"`
}

/*
 * UpdateRecord 修改解析记录
 * @route POST /domains/records/update
 * 功能：异步更新 DNS 解析记录值/线路/TTL 等属性
 */
func UpdateRecord(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req UpdateRecordRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if req.DomainID == "" || req.RecordID == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 子域名权限校验：非管理员检查是否有权操作该子域名 */
	if !middleware.CheckSubDomainPermission(c.GetString("user_id"), c.GetInt("level"), req.DomainID, req.Name) {
		middleware.ErrorResponse(c, "无权操作该子域名")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := provider.UpdateDomainRecord(ctx, req.RecordID, req.Name, req.Type, req.Value, req.Line, req.TTL, req.MX, req.Weight, req.Remark); err != nil {
			service.Audit.LogActionDirect(userID, username, ip, ua, "update_record_failed", domainName, fmt.Sprintf("%s.%s -> %s 失败: %s", req.Name, domainName, req.Value, err.Error()))
			return
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "update_record", domainName, fmt.Sprintf("%s.%s -> %s", req.Name, domainName, req.Value))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type DeleteRecordRequest struct {
	DomainID string `json:"domain_id"`
	RecordID string `json:"record_id"`
}

/*
 * DeleteRecord 删除解析记录
 * @route POST /domains/records/delete
 * 功能：异步删除指定 DNS 解析记录
 */
func DeleteRecord(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req DeleteRecordRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if req.DomainID == "" || req.RecordID == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 域名权限校验：检查用户是否有该域名的操作权限 */
	if !middleware.CheckDomainPermission(c.GetString("user_id"), c.GetInt("level"), req.DomainID) {
		middleware.ErrorResponse(c, "无权操作该域名")
		return
	}
	/* 只读权限检查 */
	if readOnly, exists := c.Get("perm_read_only"); exists && readOnly.(bool) {
		middleware.ErrorResponse(c, "您对该域名仅有只读权限")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}

	/* 提取上下文信息后异步执行 */
	userID := c.GetString("user_id")
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name
	recordID := req.RecordID

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := provider.DeleteDomainRecord(ctx, recordID); err != nil {
			service.Audit.LogActionDirect(userID, username, ip, ua, "delete_record_failed", domainName, fmt.Sprintf("删除记录ID: %s 失败: %s", recordID, err.Error()))
			return
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "delete_record", domainName, fmt.Sprintf("删除记录ID: %s", recordID))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type SetRecordStatusRequest struct {
	DomainID string `json:"domain_id"`
	RecordID string `json:"record_id"`
	Enable   bool   `json:"enable"`
}

/*
 * SetRecordStatus 设置解析记录状态
 * @route POST /domains/records/status
 * 功能：异步启用/暂停指定 DNS 解析记录
 */
func SetRecordStatus(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req SetRecordStatusRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.RecordID == "" {
		req.RecordID = c.Param("recordId")
	}
	if req.DomainID == "" || req.RecordID == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	// 安全审计 R-3：原实现遗漏权限校验，与 CreateRecord/UpdateRecord/DeleteRecord 不一致；
	// 拥有任意域名读权限的用户可对全站任何记录调启停 → DoS。补齐与同文件其他 mutation 一致的检查。
	userID := c.GetString("user_id")
	userLevel := c.GetInt("level")
	if !middleware.CheckDomainPermission(userID, userLevel, req.DomainID) {
		middleware.ErrorResponse(c, "无权操作该域名")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}

	/* 提取上下文信息后异步执行 */
	username := c.GetString("username")
	ip := c.ClientIP()
	ua := c.Request.UserAgent()
	domainName := domain.Name
	recordID := req.RecordID
	enable := req.Enable
	status := "启用"
	if !enable {
		status = "暂停"
	}

	utils.SafeGo(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := provider.SetDomainRecordStatus(ctx, recordID, enable); err != nil {
			service.Audit.LogActionDirect(userID, username, ip, ua, "set_record_status_failed", domainName, fmt.Sprintf("%s记录: %s 失败: %s", status, recordID, err.Error()))
			return
		}
		service.Audit.LogActionDirect(userID, username, ip, ua, "set_record_status", domainName, fmt.Sprintf("%s记录: %s", status, recordID))
	})

	middleware.SuccessMsg(c, "提交成功")
}

type GetRecordLinesRequest struct {
	DomainID string `json:"domain_id" form:"domain_id"`
}

/*
 * GetRecordLines 获取解析线路列表
 * @route POST /domains/records/lines
 * 功能：查询 DNS 服务商支持的解析线路（默认/电信/联通等）
 */
func GetRecordLines(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req GetRecordLinesRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.DomainID == "" {
		req.DomainID = c.Param("id")
	}
	if req.DomainID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.DomainID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 非管理员校验域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	provider := getProviderByDomain(c, &domain)
	if provider == nil {
		return
	}

	lines, err := provider.GetRecordLine(c.Request.Context())
	if err != nil {
		middleware.ErrorResponse(c, "获取失败: "+err.Error())
		return
	}

	middleware.SuccessResponse(c, lines)
}

type GetAccountDomainListRequest struct {
	ID      string `json:"id" form:"id"`
	Keyword string `json:"keyword" form:"keyword"`
}

/*
 * GetAccountDomainList 获取 DNS 账户下的域名列表
 * @route POST /domains/account-list
 * 功能：从 DNS 服务商获取账户下所有域名，标记已导入的域名
 */
func GetAccountDomainList(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req GetAccountDomainListRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		req.ID = c.Param("id")
	}
	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少账户ID")
		return
	}

	var account models.Account
	if err := database.WithContext(c).First(&account, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "账户不存在")
		return
	}

	/* 非管理员只能查看自己的账户 */
	if !isAdmin(c) && account.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权限")
		return
	}

	var config map[string]string
	if err := json.Unmarshal([]byte(account.Config), &config); err != nil {
		middleware.ErrorResponse(c, "账户配置解析失败")
		return
	}

	provider, err := dns.GetProvider(account.Type, config, "", "")
	if err != nil {
		middleware.ErrorResponse(c, err.Error())
		return
	}

	result, err := provider.GetDomainList(c.Request.Context(), req.Keyword, 1, 500)
	if err != nil {
		middleware.ErrorResponse(c, "获取域名列表失败: "+err.Error())
		return
	}

	domainList, ok := result.Records.([]dns.DomainInfo)
	if !ok {
		middleware.ErrorResponse(c, "域名列表格式异常")
		return
	}

	var existingDomains []models.Domain
	database.WithContext(c).Where("aid = ?", req.ID).Find(&existingDomains)
	existingMap := make(map[string]bool)
	for _, d := range existingDomains {
		existingMap[d.Name] = true
	}

	type DomainItem struct {
		DomainId    string `json:"DomainId"`
		Domain      string `json:"Domain"`
		RecordCount int    `json:"RecordCount"`
		Disabled    bool   `json:"disabled"`
	}

	items := make([]DomainItem, 0, len(domainList))
	for _, d := range domainList {
		items = append(items, DomainItem{
			DomainId:    d.ID,
			Domain:      d.Name,
			RecordCount: d.RecordCount,
			Disabled:    existingMap[d.Name],
		})
	}

	middleware.SuccessResponse(c, gin.H{"total": len(items), "list": items})
}

type UpdateDomainRequest struct {
	ID         string     `json:"id"`
	IsHide     *bool      `json:"is_hide"`
	IsSSO      *bool      `json:"is_sso"`
	IsNotice   *bool      `json:"is_notice"`
	Remark     *string    `json:"remark"`
	ExpireTime *time.Time `json:"expire_time"`
}

/*
 * UpdateDomain 更新域名信息
 * @route POST /domains/update
 * 功能：更新域名的隐藏/SSO/通知开关、备注、到期时间等属性
 */
func UpdateDomain(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req UpdateDomainRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		req.ID = c.Param("id")
	}
	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	/* 全局属性修改需要管理员或域名权限 */
	if !isAdmin(c) && !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), strconv.FormatUint(uint64(domain.ID), 10)) {
		middleware.ErrorResponse(c, "无权限操作该域名")
		return
	}

	// 域名全局属性
	updates := make(map[string]interface{})
	if req.IsHide != nil {
		updates["is_hide"] = *req.IsHide
	}
	if req.IsSSO != nil {
		updates["is_sso"] = *req.IsSSO
	}
	if req.IsNotice != nil {
		updates["is_notice"] = *req.IsNotice
	}
	if req.ExpireTime != nil {
		updates["expire_time"] = *req.ExpireTime
	}

	if len(updates) > 0 {
		database.WithContext(c).Model(&domain).Updates(updates)
	}

	// 备注按用户独立存储
	if req.Remark != nil {
		uidN, _ := strconv.ParseUint(c.GetString("user_id"), 10, 32)
		var note models.DomainNote
		err := database.WithContext(c).Where("uid = ? AND did = ?", uint(uidN), domain.ID).First(&note).Error
		if err != nil {
			// 不存在则创建
			note = models.DomainNote{
				UserID:   uint(uidN),
				DomainID: domain.ID,
				Remark:   *req.Remark,
			}
			database.WithContext(c).Create(&note)
		} else {
			database.WithContext(c).Model(&note).Update("remark", *req.Remark)
		}
	}

	service.Audit.LogAction(c, "update_domain", domain.Name, fmt.Sprintf("更新域名配置: %s", domain.Name))
	middleware.SuccessMsg(c, "修改域名配置成功！")
}

type BatchDomainActionRequest struct {
	IDs      []string `json:"ids" binding:"required"`
	Action   string   `json:"action" binding:"required"`
	IsNotice *bool    `json:"is_notice"`
	Remark   *string  `json:"remark"`
}

/*
 * BatchDomainAction 批量域名操作
 * @route POST /domains/batch
 * 功能：批量删除/设置通知/设置备注/更新到期时间，使用 IN 查询优化
 */
func BatchDomainAction(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req BatchDomainActionRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if len(req.IDs) == 0 || req.Action == "" {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	/* 删除操作仅管理员可执行，其它操作需要域名权限 */
	if req.Action == "delete" {
		if !checkAdmin(c) {
			middleware.ErrorResponse(c, "无权限，仅管理员可删除域名")
			return
		}
	} else if !isAdmin(c) {
		/* 非管理员：检查所有目标域名是否在用户权限范围内 */
		for _, id := range req.IDs {
			if !middleware.CheckDomainPermission(currentUID(c), c.GetInt("level"), id) {
				middleware.ErrorResponse(c, "无权限操作部分域名")
				return
			}
		}
	}

	success := len(req.IDs)
	switch req.Action {
	case "delete":
		/* 查出待删除域名名称，用于清理按 domain 名称关联的 Permission */
		var domainNames []string
		database.WithContext(c).Model(&models.Domain{}).Where("id IN ?", req.IDs).Pluck("name", &domainNames)
		if len(domainNames) > 0 {
			database.WithContext(c).Where("domain IN ?", domainNames).Delete(&models.Permission{})
		}
		/* 清理监控任务及其日志 */
		var batchTaskIDs []string
		database.WithContext(c).Model(&models.DMTask{}).Where("did IN ?", req.IDs).Pluck("id", &batchTaskIDs)
		if len(batchTaskIDs) > 0 {
			database.WithContext(c).Where("task_id IN ?", batchTaskIDs).Delete(&models.DMLog{})
			database.LogDB.Where("task_id IN ?", batchTaskIDs).Delete(&models.DMCheckLog{})
		}
		database.WithContext(c).Where("id IN ?", req.IDs).Delete(&models.Domain{})
		database.WithContext(c).Where("did IN ?", req.IDs).Delete(&models.DMTask{})
		database.WithContext(c).Where("did IN ?", req.IDs).Delete(&models.OptimizeIP{})
		database.WithContext(c).Where("did IN ?", req.IDs).Delete(&models.ScheduleTask{})
		database.WithContext(c).Where("did IN ?", req.IDs).Delete(&models.CertCNAME{})
		database.WithContext(c).Where("did IN ?", req.IDs).Delete(&models.DomainNote{})
	case "set_notice":
		if req.IsNotice != nil {
			database.WithContext(c).Model(&models.Domain{}).Where("id IN ?", req.IDs).Update("is_notice", *req.IsNotice)
		} else {
			success = 0
		}
	case "set_remark":
		if req.Remark != nil {
			database.WithContext(c).Model(&models.Domain{}).Where("id IN ?", req.IDs).Update("remark", *req.Remark)
		} else {
			success = 0
		}
	case "update_expire":
		database.WithContext(c).Model(&models.Domain{}).Where("id IN ?", req.IDs).Update("check_status", 0)
	default:
		middleware.ErrorResponse(c, "不支持的操作")
		return
	}

	middleware.SuccessMsg(c, fmt.Sprintf("成功操作%d个域名", success))
}

type UpdateDomainExpireRequest struct {
	ID string `json:"id" binding:"required"`
}

/*
 * UpdateDomainExpire 更新单个域名到期时间
 * @route POST /domains/expire
 * 功能：通过 WHOIS 查询更新域名的到期时间
 */
func UpdateDomainExpire(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req UpdateDomainExpireRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if req.ID == "" {
		middleware.ErrorResponse(c, "缺少域名ID")
		return
	}

	var domain models.Domain
	if err := database.WithContext(c).First(&domain, req.ID).Error; err != nil {
		middleware.ErrorResponse(c, "域名不存在")
		return
	}

	database.WithContext(c).Model(&domain).Update("check_status", 0)
	middleware.SuccessMsg(c, "已提交更新请求，请稍后刷新")
}

type BatchUpdateDomainExpireRequest struct {
	IDs []string `json:"ids" binding:"required"`
}

/*
 * BatchUpdateDomainExpire 批量更新域名到期时间
 * @route POST /domains/expire/batch
 * 功能：异步通过 WHOIS 批量查询并更新域名到期时间
 */
func BatchUpdateDomainExpire(c *gin.Context) {
	if !requireUserModule(c, "domain") {
		return
	}
	var req BatchUpdateDomainExpireRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}

	if len(req.IDs) == 0 {
		middleware.ErrorResponse(c, "参数错误")
		return
	}

	count := database.WithContext(c).Model(&models.Domain{}).Where("id IN ?", req.IDs).Update("check_status", 0).RowsAffected
	middleware.SuccessMsg(c, fmt.Sprintf("已提交%d个域名，约%d分钟后刷新完成", count, (count/5)+1))
}
