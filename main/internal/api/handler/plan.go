package handler

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"gorm.io/gorm"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/logger"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 套餐与配额的接口层
 *
 * 读取侧复用 quota.go 的 quotaForUser：它已实现「个体 > 套餐 > 系统默认」三层优先级，
 * 这里只负责把结果与来源一并吐给前端，不重复实现优先级逻辑
 *
 * 写入侧分两类：
 *   plans / user_quotas：管理员维护配额模板与个体覆盖
 *   user_subscriptions：管理员授予或撤销套餐
 */

/* quotaDimensionKeys 配额维度的 JSON 键，与 applyQuotaJSON 的读取键保持一一对应 */
var quotaDimensionKeys = []string{
	"max_domains",
	"max_records",
	"max_accounts",
	"max_cert_accounts",
	"max_monitors",
	"max_certs",
	"max_schedules",
	"max_deploys",
	"max_optimize_ip",
}

/* quotaFromJSON 把 JSON 字符串解成维度键→上限的映射，空串返回空映射而非 nil */
func quotaFromJSON(raw string) map[string]int {
	out := make(map[string]int, len(quotaDimensionKeys))
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	for _, k := range quotaDimensionKeys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

/*
 * quotaToJSON 把维度键→上限写回 JSON 字符串
 *
 * 只输出配额维度键，前端传来的多余字段在这里被过滤掉——
 * 与 UpdateOptimizeIPConfig 的 allowlist 同思路，避免越权写入任意键
 */
func quotaToJSON(m map[string]int) string {
	filtered := make(map[string]int, len(quotaDimensionKeys))
	for _, k := range quotaDimensionKeys {
		if v, ok := m[k]; ok {
			filtered[k] = v
		}
	}
	b, err := json.Marshal(filtered)
	if err != nil {
		return "{}"
	}
	return string(b)
}

/* quotaToMap 把 Quota 结构体摊成维度键→上限，供前端统一渲染 */
func quotaToMap(q Quota) map[string]int {
	return map[string]int{
		"max_domains":       q.MaxDomains,
		"max_records":       q.MaxRecords,
		"max_accounts":      q.MaxAccounts,
		"max_cert_accounts": q.MaxCertAccounts,
		"max_monitors":      q.MaxMonitors,
		"max_certs":         q.MaxCerts,
		"max_schedules":     q.MaxSchedules,
		"max_deploys":       q.MaxDeploys,
		"max_optimize_ip":   q.MaxOptimizeIP,
	}
}

/*
 * overlayQuotaMap 解析个体覆盖的原文，只保留**真正被覆盖**的维度
 *
 * 与 quotaToMap 的区别：后者把零值也铺满八个维度（那是「有效值」的语义），
 * 这里只保留 JSON 里显式出现的键——「未声明」与「声明为 0」是两回事，
 * 前者应回落上层，后者表示禁止创建
 */
func overlayQuotaMap(raw string) map[string]int {
	out := map[string]int{}
	if strings.TrimSpace(raw) == "" {
		return out
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return out
	}
	for _, k := range quotaDimensionKeys {
		if v, ok := m[k]; ok {
			out[k] = v
		}
	}
	return out
}

/* quotaUsedByResource 返回各维度的当前用量，与 quotaUsed 的统计口径一致 */
func quotaUsedByResource(c *gin.Context, userID uint) map[string]int {
	used := make(map[string]int, len(quotaDimensionKeys))
	/*
	 * 逐维度列举，而不是遍历 quotaDimensionKeys
	 *
	 * 两者的差异是刻意的：quotaDimensionKeys 是 JSON 的键名（带 max_ 前缀），
	 * 而 quotaUsed 收的是维度名（不带前缀）。要遍历就得在这里剥前缀，
	 * 但那样会把「维度名」与「JSON 键名」的对应关系隐式化——
	 * 新增维度时漏改一处不会报错。显式列举让漏改在编译期就暴露
	 */
	for _, r := range []string{"domains", "records", "accounts", "cert_accounts", "monitors", "certs", "schedules", "deploys", "optimize_ip"} {
		used["max_"+r] = quotaUsed(c, userID, r)
	}
	return used
}

/* validatePlanQuotas 校验各维度取值落入约定范围：-1 不限、0 禁止、正数为上限 */
func validatePlanQuotas(m map[string]int) bool {
	for _, v := range m {
		if v < -1 {
			return false
		}
	}
	return true
}

/* planRow 套餐列表的一行 */
type planRow struct {
	models.Plan
	SubscriberCount int64 `json:"subscriber_count"`
}

/*
 * GetPlans 套餐列表
 *
 * 附带每个套餐的在用订阅数：删除套餐前需要知道会波及多少用户，
 * 前端据此决定是否给出二次确认
 */
func GetPlans(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var plans []models.Plan
	if err := database.WithContext(c).Order("sort ASC, created_at ASC").Find(&plans).Error; err != nil {
		middleware.ErrorResponse(c, "加载失败")
		return
	}

	rows := make([]planRow, 0, len(plans))
	for _, p := range plans {
		rows = append(rows, planRow{
			Plan:            p,
			SubscriberCount: activeSubscriptionCount(database.WithContext(c), p.ID),
		})
	}

	middleware.SuccessResponse(c, gin.H{"total": len(rows), "list": rows})
}

/* planRequest 创建与修改套餐的入参 */
type planRequest struct {
	// IncludedDomains 套餐绑定的域名，逗号分隔；授予套餐时一并下发访问权限
	IncludedDomains string          `json:"included_domains"`
	Name            string          `json:"name"`
	Description     string          `json:"description"`
	Status          *int            `json:"status"`
	Sort            *int            `json:"sort"`
	Quotas          map[string]int  `json:"quotas"`
	Features        map[string]bool `json:"features"`
}

/* CreatePlan 新建套餐 */
func CreatePlan(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var req planRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		middleware.ErrorResponse(c, "套餐名称不能为空")
		return
	}
	if !validatePlanQuotas(req.Quotas) {
		middleware.ErrorResponse(c, "配额取值不合法：-1 不限、0 禁止、正数为上限")
		return
	}

	var dup models.Plan
	if database.WithContext(c).Where("name = ?", name).First(&dup).Error == nil {
		middleware.ErrorResponse(c, "套餐名称已存在")
		return
	}

	features := "{}"
	if len(req.Features) > 0 {
		if b, err := json.Marshal(req.Features); err == nil {
			features = string(b)
		}
	}

	plan := models.Plan{
		ID:          uuid.New().String(),
		Name:        name,
		Description: strings.TrimSpace(req.Description),
		IsDefault:   false,
		Sort:        0,
		// Status 显式赋 1：模型刻意不带 gorm default 标签，零值会被当成「停用」写入
		Status:   1,
		Quotas:   quotaToJSON(req.Quotas),
		Features: features,
		// 域名清单归一化后再存：授予时要按它匹配域名，大小写与空白必须一致
		IncludedDomains: normalizeDomainList(req.IncludedDomains),
	}
	if req.Sort != nil {
		plan.Sort = *req.Sort
	}
	// 显式传入 0 时按「停用」入库，与模型的零值语义一致
	if req.Status != nil && *req.Status == 0 {
		plan.Status = 0
	}

	if err := database.WithContext(c).Create(&plan).Error; err != nil {
		middleware.ErrorResponse(c, "创建失败")
		return
	}
	middleware.SuccessResponse(c, plan)
}

/* UpdatePlan 修改套餐 */
func UpdatePlan(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	if id == "" {
		middleware.ErrorResponse(c, "缺少套餐 ID")
		return
	}
	var plan models.Plan
	if err := database.WithContext(c).First(&plan, "id = ?", id).Error; err != nil {
		middleware.ErrorResponse(c, "套餐不存在")
		return
	}
	var req planRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if !validatePlanQuotas(req.Quotas) {
		middleware.ErrorResponse(c, "配额取值不合法：-1 不限、0 禁止、正数为上限")
		return
	}

	updates := map[string]interface{}{}
	if name := strings.TrimSpace(req.Name); name != "" && name != plan.Name {
		var dup models.Plan
		if database.WithContext(c).Where("name = ? AND id <> ?", name, plan.ID).First(&dup).Error == nil {
			middleware.ErrorResponse(c, "套餐名称已存在")
			return
		}
		updates["name"] = name
	}
	updates["description"] = strings.TrimSpace(req.Description)
	if req.Sort != nil {
		updates["sort"] = *req.Sort
	}
	if req.Status != nil {
		updates["status"] = *req.Status
	}
	updates["quotas"] = quotaToJSON(req.Quotas)
	if len(req.Features) > 0 {
		if b, err := json.Marshal(req.Features); err == nil {
			updates["features"] = string(b)
		}
	}

	if req.IncludedDomains != "" || req.Name != "" {
		updates["included_domains"] = normalizeDomainList(req.IncludedDomains)
	}

	if err := database.WithContext(c).Model(&plan).Updates(updates).Error; err != nil {
		middleware.ErrorResponse(c, "保存失败")
		return
	}

	/*
	 * 改了绑定域名后，已授予该套餐的用户权限必须跟着变
	 *
	 * 否则「在套餐里加一个域名」对存量用户不生效，而「删一个域名」会让
	 * 他们继续持有已解绑域名的权限。同事务内先回收再下发，
	 * 只动 Source='plan' 的行
	 */
	if _, changed := updates["included_domains"]; changed {
		var plan2 models.Plan
		if err := database.WithContext(c).First(&plan2, "id = ?", plan.ID).Error; err == nil {
			var subs []models.UserSubscription
			database.WithContext(c).Where("plan_id = ? AND status = 1", plan2.ID).Find(&subs)
			now := nowFunc()
			for i := range subs {
				sub := subs[i]
				if !subscriptionEffective(&sub, now) {
					continue
				}
				uid := sub.UserID
				expire := sub.ExpireTime
				if err := database.WithContext(c).Transaction(func(tx *gorm.DB) error {
					return refreshPlanDomainPermissions(tx, uid, &plan2, expire)
				}); err != nil {
					logger.Warn("[Plan] 套餐变更后同步域名权限失败: uid=%d plan=%s err=%v", uid, plan2.ID, err)
				}
			}
		}
	}
	middleware.SuccessMsg(c, "保存成功")
}

/*
 * DeletePlan 删除套餐
 *
 * 仍有启用中的订阅时不删：订阅表冗余了 plan_name，删掉套餐会让这些用户
 * 的当前权益无从解读，也会让 quotaForUser 回落到系统默认而不自知
 */
func DeletePlan(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	var plan models.Plan
	if err := database.WithContext(c).First(&plan, "id = ?", id).Error; err != nil {
		middleware.ErrorResponse(c, "套餐不存在")
		return
	}
	/*
	 * 占用判定与列表口径一致：过期订阅不算在用
	 *
	 * 此前这里只按 status=1 计数，与 planSourceForUser 的「过期即无效」冲突，
	 * 过期订阅会把套餐钉死——列表显示「仍有 1 个订阅在用」而用户页显示「未订阅」，
	 * 两边都没有可撤销的入口
	 */
	if n := activeSubscriptionCount(database.WithContext(c), plan.ID); n > 0 {
		middleware.ErrorResponse(c, "该套餐仍有 "+itoaInt(int(n))+" 个订阅在用，请先撤销订阅")
		return
	}
	if err := database.WithContext(c).Delete(&models.Plan{}, "id = ?", plan.ID).Error; err != nil {
		middleware.ErrorResponse(c, "删除失败")
		return
	}
	middleware.SuccessMsg(c, "删除成功")
}

/*
 * SetDefaultPlan 设为默认套餐
 *
 * 默认套餐由业务代码授予新用户，全库只能有一个：先清空再置位，
 * 放在同一事务里避免中途失败留下两个默认
 */
func SetDefaultPlan(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	id := strings.TrimSpace(c.Param("id"))
	var plan models.Plan
	if err := database.WithContext(c).First(&plan, "id = ?", id).Error; err != nil {
		middleware.ErrorResponse(c, "套餐不存在")
		return
	}
	if plan.Status != 1 {
		middleware.ErrorResponse(c, "停用的套餐不能设为默认")
		return
	}
	err := database.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Plan{}).Where("is_default = ?", true).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&models.Plan{}).Where("id = ?", plan.ID).
			Update("is_default", true).Error
	})
	if err != nil {
		middleware.ErrorResponse(c, "设置失败")
		return
	}
	middleware.SuccessMsg(c, "已设为默认套餐")
}

/* userQuotaRow 用户配额列表的一行：生效值、来源、用量三者并排展示 */
type userQuotaRow struct {
	UserID   uint   `json:"user_id"`
	Username string `json:"username"`
	Level    int    `json:"level"`
	Source   string `json:"source"` // admin 个体覆盖 / plan 套餐 / system 系统默认
	PlanID   string `json:"plan_id"`
	PlanName string `json:"plan_name"`
	// HasSubscription 是否存在订阅记录（含已过期）：过期时 PlanName 为空，
	// 前端撤销按钮需据此判断，否则过期订阅既显示「未订阅」又无法撤销
	HasSubscription bool `json:"has_subscription"`
	/*
	 * Overlays 是**只属于个体覆盖**的那部分维度
	 *
	 * Quotas 是三层合并后的生效值，前端拿它预填「设置个体配额」表单时，
	 * 会把套餐或系统默认的值一并写上，保存后它们就固化成个体覆盖——
	 * 管理员此后调整套餐，该用户永远停在旧值，而他并不知道自己覆盖过这些维度。
	 * 这里把「哪些维度真的是个体覆盖」单独给出，前端只预填这些
	 */
	Overlays  map[string]int `json:"overlays"`
	Quotas    map[string]int `json:"quotas"`
	Used      map[string]int `json:"used"`
	Remark    string         `json:"remark"`
	UpdatedAt *string        `json:"updated_at"`
}

/*
 * planSourceForUser 取用户配额的来源与套餐信息
 *
 * 与 quotaForUser 的三层判定保持一致：个体覆盖优先，其次套餐，最后系统默认。
 * 返回的 planID/planName 只在确实命中了启用中的套餐时才非空
 */
func planSourceForUser(c *gin.Context, userID uint) (source, planID, planName string) {
	var uq models.UserQuota
	if err := database.WithContext(c).First(&uq, "user_id = ?", userID).Error; err == nil {
		return "admin", "", ""
	}
	var sub models.UserSubscription
	if err := database.WithContext(c).Where("user_id = ? AND status = 1", userID).First(&sub).Error; err == nil {
		if sub.ExpireTime == nil || sub.ExpireTime.After(nowFunc()) {
			var plan models.Plan
			if err := database.WithContext(c).First(&plan, "id = ?", sub.PlanID).Error; err == nil && plan.Status == 1 {
				return "plan", plan.ID, sub.PlanName
			}
		}
	}
	return "system", "", ""
}

/* GetUserQuotas 列出全部用户的配额、来源与用量 */
func GetUserQuotas(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var users []models.User
	if err := database.WithContext(c).Select("id", "username", "level").
		Order("id ASC").Find(&users).Error; err != nil {
		middleware.ErrorResponse(c, "加载失败")
		return
	}

	rows := make([]userQuotaRow, 0, len(users))
	for _, u := range users {
		source, planID, planName := planSourceForUser(c, u.ID)
		row := userQuotaRow{
			UserID:   u.ID,
			Username: u.Username,
			Level:    u.Level,
			Source:   source,
			PlanID:   planID,
			PlanName: planName,
			// 有过期订阅时 planName 为空，前端撤销按钮不能据此禁用
			HasSubscription: hasSubscriptionRecord(c, u.ID),
			Quotas:          quotaToMap(quotaForUser(u.ID, u.Level)),
			Used:            quotaUsedByResource(c, u.ID),
		}
		var uq models.UserQuota
		if err := database.WithContext(c).First(&uq, "user_id = ?", u.ID).Error; err == nil {
			row.Remark = uq.Remark
			t := uq.UpdatedAt.Format("2006-01-02 15:04:05")
			row.UpdatedAt = &t
			row.Overlays = overlayQuotaMap(uq.Quotas)
		}
		if row.Overlays == nil {
			row.Overlays = map[string]int{}
		}
		rows = append(rows, row)
	}
	middleware.SuccessResponse(c, gin.H{"total": len(rows), "list": rows})
}

/* userQuotaRequest 设置个体配额或授予套餐的入参 */
type userQuotaRequest struct {
	Quotas   map[string]int `json:"quotas"`
	Remark   string         `json:"remark"`
	PlanID   string         `json:"plan_id"`
	ExpireAt string         `json:"expire_at"` // 空表示永久
}

/* targetUserIDUint 解析路径里的用户 ID */
func targetUserIDUint(c *gin.Context) (uint, bool) {
	raw := strings.TrimSpace(c.Param("uid"))
	if raw == "" {
		middleware.ErrorResponse(c, "缺少用户 ID")
		return 0, false
	}
	id, err := strconv.ParseUint(raw, 10, 32)
	if err != nil || id == 0 {
		middleware.ErrorResponse(c, "用户 ID 不合法")
		return 0, false
	}
	return uint(id), true
}

/* ensureUserExists 校验目标用户存在 */
func ensureUserExists(c *gin.Context, userID uint) bool {
	var n int64
	database.WithContext(c).Model(&models.User{}).Where("id = ?", userID).Count(&n)
	if n == 0 {
		middleware.ErrorResponse(c, "用户不存在")
		return false
	}
	return true
}

/* SetUserQuota 设置某用户的个体配额，优先级高于套餐 */
func SetUserQuota(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	userID, ok := targetUserIDUint(c)
	if !ok || !ensureUserExists(c, userID) {
		return
	}
	var req userQuotaRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if !validatePlanQuotas(req.Quotas) {
		middleware.ErrorResponse(c, "配额取值不合法：-1 不限、0 禁止、正数为上限")
		return
	}

	quota := models.UserQuota{
		UserID: userID,
		Quotas: quotaToJSON(req.Quotas),
		Remark: strings.TrimSpace(req.Remark),
	}
	// UserQuota 以 user_id 为主键，重复设置覆盖既有记录
	if err := database.WithContext(c).Where("user_id = ?", userID).
		Assign(map[string]interface{}{"quotas": quota.Quotas, "remark": quota.Remark}).
		FirstOrCreate(&quota).Error; err != nil {
		middleware.ErrorResponse(c, "保存失败")
		return
	}
	middleware.SuccessMsg(c, "保存成功")
}

/* ResetUserQuota 清除个体配额，回落到套餐或系统默认 */
func ResetUserQuota(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	userID, ok := targetUserIDUint(c)
	if !ok || !ensureUserExists(c, userID) {
		return
	}
	if err := database.WithContext(c).Delete(&models.UserQuota{}, "user_id = ?", userID).Error; err != nil {
		middleware.ErrorResponse(c, "重置失败")
		return
	}
	middleware.SuccessMsg(c, "已回落到套餐配额")
}

/*
 * GetUserSubscription 查某用户当前的套餐订阅
 *
 * 未订阅时返回 null 而非报错：前端要据此渲染「未订阅」状态
 */
func GetUserSubscription(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	userID, ok := targetUserIDUint(c)
	if !ok || !ensureUserExists(c, userID) {
		return
	}
	var sub models.UserSubscription
	if err := database.WithContext(c).Where("user_id = ? AND status = 1", userID).
		Order("created_at DESC").First(&sub).Error; err != nil {
		middleware.SuccessResponse(c, nil)
		return
	}
	middleware.SuccessResponse(c, sub)
}

/*
 * SetUserSubscription 授予或变更套餐
 *
 * 同一用户同时只保留一条启用中的订阅：先把既有的置为停用再写新的，
 * 否则 quotaForUser 的 First() 会命中哪一条取决于插入顺序，不可预期
 */
func SetUserSubscription(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	userID, ok := targetUserIDUint(c)
	if !ok || !ensureUserExists(c, userID) {
		return
	}
	var req userQuotaRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	planID := strings.TrimSpace(req.PlanID)
	if planID == "" {
		middleware.ErrorResponse(c, "请选择套餐")
		return
	}
	var plan models.Plan
	if err := database.WithContext(c).First(&plan, "id = ?", planID).Error; err != nil {
		middleware.ErrorResponse(c, "套餐不存在")
		return
	}
	if plan.Status != 1 {
		middleware.ErrorResponse(c, "该套餐已停用，无法授予")
		return
	}

	var expire *time.Time
	if s := strings.TrimSpace(req.ExpireAt); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			middleware.ErrorResponse(c, "到期时间格式应为 YYYY-MM-DD")
			return
		}
		expire = &t
	}

	sub := models.UserSubscription{
		ID:        uuid.New().String(),
		UserID:    userID,
		PlanID:    plan.ID,
		PlanName:  plan.Name,
		Status:    1,
		AutoRenew: false,
		StartTime: nowFunc(),
		// ExpireTime 为空表示永久
		ExpireTime: expire,
	}
	err := database.WithContext(c).Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.UserSubscription{}).
			Where("user_id = ? AND status = 1", userID).
			Update("status", 0).Error; err != nil {
			return err
		}
		if err := tx.Create(&sub).Error; err != nil {
			return err
		}
		/*
		 * 套餐绑定的域名权限在同事务内下发
		 *
		 * 与订阅一荣俱荣：订阅落库而权限失败会让用户「有套餐但用不了域名」，
		 * 反过来权限落库而订阅失败会让「撤销套餐」回收不到这批权限。
		 * 先回收旧的套餐权限再下发，避免换套餐时残留上一个套餐的域名
		 */
		return refreshPlanDomainPermissions(tx, userID, &plan, expire)
	})
	if err != nil {
		middleware.ErrorResponse(c, "授予失败")
		return
	}
	msg := "已授予套餐「" + plan.Name + "」"
	if n := len(planIncludedDomainList(&plan)); n > 0 {
		msg += "，已下发 " + itoaInt(n) + " 个绑定域名"
	}
	middleware.SuccessMsg(c, msg)
}

/* RevokeUserSubscription 撤销套餐，回落到系统默认配额 */
func RevokeUserSubscription(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	userID, ok := targetUserIDUint(c)
	if !ok || !ensureUserExists(c, userID) {
		return
	}
	var affected int64
	err := database.WithContext(c).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&models.UserSubscription{}).
			Where("user_id = ? AND status = 1", userID).Update("status", 0)
		if res.Error != nil {
			return res.Error
		}
		affected = res.RowsAffected
		if affected == 0 {
			return nil
		}
		// 只回收套餐下发的域名权限，管理员手工授予的保留
		return revokePlanDomains(tx, userID)
	})
	if err != nil {
		middleware.ErrorResponse(c, "撤销失败")
		return
	}
	if affected == 0 {
		middleware.ErrorResponse(c, "该用户当前没有启用中的套餐")
		return
	}
	middleware.SuccessMsg(c, "已撤销套餐")
}

/*
 * GetMyQuota 当前用户查询自己的配额与用量
 *
 * 管理员的 quotaForUser 恒为不限，这里照样原样返回：前端据 source 为 "admin"
 * 显示「管理员不受配额限制」，无需在前端再判一次 level
 */
func GetMyQuota(c *gin.Context) {
	userID := currentUIDUint(c)
	if userID == 0 {
		middleware.ErrorResponse(c, "未登录")
		return
	}
	level := c.GetInt("level")
	source, planID, planName := planSourceForUser(c, userID)
	if level >= 2 {
		source, planID, planName = "admin", "", ""
	}
	middleware.SuccessResponse(c, gin.H{
		"source":    source,
		"plan_id":   planID,
		"plan_name": planName,
		"quotas":    quotaToMap(quotaForUser(userID, level)),
		"used":      quotaUsedByResource(c, userID),
	})
}

/* quotaDefaultKeys 系统默认配额的配置键 */
const quotaDefaultKey = "quota_default"

/* GetQuotaDefault 读取系统默认配额（未订阅套餐时的兜底） */
func GetQuotaDefault(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	raw := sysconfig.GetValue(quotaDefaultKey)
	middleware.SuccessResponse(c, gin.H{
		"quotas": quotaFromJSON(raw),
		"limits": quotaToMap(unlimitedQuota()),
	})
}

/* UpdateQuotaDefault 保存系统默认配额 */
func UpdateQuotaDefault(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	var req userQuotaRequest
	if err := middleware.BindDecryptedData(c, &req); err != nil {
		middleware.ErrorResponse(c, "参数解析失败")
		return
	}
	if !validatePlanQuotas(req.Quotas) {
		middleware.ErrorResponse(c, "配额取值不合法：-1 不限、0 禁止、正数为上限")
		return
	}
	sysconfig.SetValue(quotaDefaultKey, quotaToJSON(req.Quotas))
	middleware.SuccessMsg(c, "保存成功")
}
