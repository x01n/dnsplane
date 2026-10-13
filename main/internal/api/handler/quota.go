package handler

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"main/internal/service"
	"main/internal/sysconfig"
)

/*
 * 资源配额
 *
 * 上限来源三层，优先级从高到低：
 *   1. user_quotas       管理员对单个用户的覆盖
 *   2. plans + 订阅       用户当前套餐
 *   3. 系统默认           未订阅任何套餐时的兜底
 *
 * 取值约定：-1 不限、0 禁止、正数为上限。未显式配置的维度按「不限」处理——
 * 存量实例升级后不应因为新增了配额维度而突然无法创建资源
 */

/* Quota 一个用户的各维度上限 */
type Quota struct {
	MaxDomains  int `json:"max_domains"`
	MaxRecords  int `json:"max_records"`
	MaxAccounts int `json:"max_accounts"`
	// MaxCertAccounts 证书账户（含部署账户，两者同表）
	MaxCertAccounts int `json:"max_cert_accounts"`
	MaxMonitors     int `json:"max_monitors"`
	MaxCerts        int `json:"max_certs"`
	MaxSchedules    int `json:"max_schedules"`
	MaxDeploys      int `json:"max_deploys"`
	MaxOptimizeIP   int `json:"max_optimize_ip"`
}

/*
 * unlimitedQuota 全维度不限
 *
 * 用作「该维度未配置」的基线：解析 JSON 时先铺满 -1，再用配置里的实际值覆盖，
 * 这样新增维度时无需改动所有存量套餐记录
 */
func unlimitedQuota() Quota {
	return Quota{
		MaxDomains: -1, MaxRecords: -1, MaxAccounts: -1, MaxMonitors: -1,
		MaxCerts: -1, MaxSchedules: -1, MaxDeploys: -1, MaxOptimizeIP: -1,
	}
}

/* applyQuotaJSON 把 JSON 里的维度覆盖到基线之上，未出现的维度保持不限 */
func applyQuotaJSON(q *Quota, raw string) {
	if strings.TrimSpace(raw) == "" {
		return
	}
	var m map[string]int
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		return
	}
	set := func(dst *int, key string) {
		if v, ok := m[key]; ok {
			*dst = v
		}
	}
	set(&q.MaxDomains, "max_domains")
	set(&q.MaxRecords, "max_records")
	set(&q.MaxAccounts, "max_accounts")
	set(&q.MaxCertAccounts, "max_cert_accounts")
	set(&q.MaxMonitors, "max_monitors")
	set(&q.MaxCerts, "max_certs")
	set(&q.MaxSchedules, "max_schedules")
	set(&q.MaxDeploys, "max_deploys")
	set(&q.MaxOptimizeIP, "max_optimize_ip")
}

/*
 * planIncludedDomains 取用户套餐自带的域名清单
 *
 * 与 max_domains 是两回事：那个限「自己接入多少域名」，
 * 这个列出「套餐连同哪几个域名一起给用户」。只有生效中的订阅才算
 */
func planIncludedDomains(userID uint) []string {
	var sub models.UserSubscription
	if err := database.DB.Where("user_id = ? AND status = 1", userID).First(&sub).Error; err != nil {
		return nil
	}
	if !subscriptionEffective(&sub, nowFunc()) {
		return nil
	}
	var plan models.Plan
	if err := database.DB.First(&plan, "id = ?", sub.PlanID).Error; err != nil || plan.Status != 1 {
		return nil
	}
	raw := strings.TrimSpace(plan.IncludedDomains)
	if raw == "" {
		return nil
	}
	out := make([]string, 0, 4)
	for _, d := range strings.Split(raw, ",") {
		if v := strings.ToLower(strings.TrimSpace(d)); v != "" {
			out = append(out, v)
		}
	}
	return out
}

/*
 * userMayUseDomain 用户是否有权在该域名下建记录
 *
 * 三条通路：自有账户的域名、被单独授权的域名、套餐附带的域名
 */
func userMayUseDomain(userID uint, domainName string) bool {
	name := strings.ToLower(strings.TrimSpace(domainName))
	if name == "" {
		return false
	}
	var n int64
	database.DB.Table("domains").
		Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
		Where("domains.deleted_at IS NULL AND domains.name = ? AND accounts.uid = ?", name, userID).
		Count(&n)
	if n > 0 {
		return true
	}
	database.DB.Model(&models.Permission{}).
		Where("uid = ? AND domain = ?", userID, name).Count(&n)
	if n > 0 {
		return true
	}
	for _, d := range planIncludedDomains(userID) {
		if d == name {
			return true
		}
	}
	return false
}

/*
 * quotaForUser 解析用户的实际配额
 *
 * 管理员不受配额约束：管理员是配额的管理者，限制其创建会让系统在需要扩容时
 * 无法自行调整（如配额耗尽的实例无法再由管理员补建资源）
 */
func quotaForUser(userID uint, level int) Quota {
	q := unlimitedQuota()
	if level >= 2 {
		return q
	}

	// 第三层：系统默认
	applyQuotaJSON(&q, sysconfigGetQuotaDefault())

	// 第二层：套餐
	var sub models.UserSubscription
	if err := database.DB.Where("user_id = ? AND status = 1", userID).First(&sub).Error; err == nil {
		if sub.ExpireTime == nil || sub.ExpireTime.After(nowFunc()) {
			var plan models.Plan
			if err := database.DB.First(&plan, "id = ?", sub.PlanID).Error; err == nil && plan.Status == 1 {
				applyQuotaJSON(&q, plan.Quotas)
			}
		}
	}

	// 第一层：个体覆盖
	var uq models.UserQuota
	if err := database.DB.First(&uq, "user_id = ?", userID).Error; err == nil {
		applyQuotaJSON(&q, uq.Quotas)
	}

	return q
}

/*
 * CheckQuota 创建资源前的配额校验
 *
 * 返回 false 时已写出错误响应，调用方直接 return 即可。
 * resource 取 max_ 之后的维度名（domains / records / accounts / monitors /
 * certs / schedules / deploys / optimize_ip）
 */
func CheckQuota(c *gin.Context, resource string) bool {
	userID := currentUIDUint(c)
	if userID == 0 {
		return true
	}

	q := quotaForUser(userID, c.GetInt("level"))
	limit := quotaLimitOf(q, resource)
	if limit < 0 {
		return true
	}

	used := quotaUsed(c, userID, resource)
	if used < limit {
		return true
	}

	middleware.ErrorResponse(c, quotaExceededMessage(resource, used, limit))
	return false
}

/*
 * QuotaAllows 配额是否允许再建一个该维度的资源（不含错误输出）
 *
 * 供 v1 的路由中间件使用：那层的响应格式是 v1 的，不能复用 ErrorResponse
 */
func QuotaAllows(c *gin.Context, resource string) bool {
	userID := currentUIDUint(c)
	if userID == 0 {
		return true
	}
	q := quotaForUser(userID, c.GetInt("level"))
	limit := quotaLimitOf(q, resource)
	if limit < 0 {
		return true
	}
	return quotaUsed(c, userID, resource) < limit
}

/* QuotaUsageOf 某维度的当前用量与上限，供调用方组织提示语 */
func QuotaUsageOf(c *gin.Context, resource string) (used, limit int) {
	userID := currentUIDUint(c)
	if userID == 0 {
		return 0, -1
	}
	q := quotaForUser(userID, c.GetInt("level"))
	return quotaUsed(c, userID, resource), quotaLimitOf(q, resource)
}

/* QuotaLabelOf 维度的中文名 */
func QuotaLabelOf(resource string) string {
	if label, ok := resourceLabels[resource]; ok {
		return label
	}
	return resource
}

/* quotaLimitOf 取某维度的上限 */
func quotaLimitOf(q Quota, resource string) int {
	switch resource {
	case "domains":
		return q.MaxDomains
	case "records":
		return q.MaxRecords
	case "accounts":
		return q.MaxAccounts
	case "cert_accounts":
		return q.MaxCertAccounts
	case "monitors":
		return q.MaxMonitors
	case "certs":
		return q.MaxCerts
	case "schedules":
		return q.MaxSchedules
	case "deploys":
		return q.MaxDeploys
	case "optimize_ip":
		return q.MaxOptimizeIP
	}
	return -1
}

/*
 * quotaUsed 统计某维度已占用的数量
 *
 * 解析记录走本地副本表：云端查询会消耗服务商 API 配额，且创建前的判定要求即时，
 * 因此以本地同步来的副本为准。副本由定时任务维护，允许短暂滞后——
 * 判定偏松而非偏严，不会误拒正常创建
 */
func quotaUsed(c *gin.Context, userID uint, resource string) int {
	db := database.WithContext(c)
	var n int64
	switch resource {
	case "domains":
		// 计入自有账户下的域名与被单独授权的域名，两者都是该用户可用的额度
		db.Table("domains").
			Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
			Where("domains.deleted_at IS NULL AND (accounts.uid = ? OR domains.name IN (SELECT domain FROM permissions WHERE uid = ?))", userID, userID).
			Count(&n)
	case "records":
		/*
		 * 计该用户占用的**子域名前缀**数，不是记录条数
		 *
		 * 「占一个配额」指的是占用一个子域名：同前缀下的多值 A、不同线路
		 * 共享同一条额度。按记录条数计会让多值记录被重复收额度，
		 * 与占用语义不符
		 *
		 * 统计对象是 Claim 表而非 dns_records：后者是云端副本，
		 * 被定时对账整表覆盖，本地派生的 uid 在其中没有权威性
		 */
		return service.CountUserSubdomainClaims(userID)
	case "accounts":
		db.Table("accounts").Where("uid = ? AND deleted_at IS NULL", userID).Count(&n)
	case "cert_accounts":
		/*
		 * 证书账户与部署账户同表（cert_accounts），用 is_deploy 区分用途。
		 * 两者共用这一维度的额度：它们的形态完全一致（都是「某个服务商的连接配置」），
		 * 对用户而言「我有几个可用于证书与部署的账户」是一件事
		 */
		db.Table("cert_accounts").Where("uid = ?", userID).Count(&n)
	case "monitors":
		db.Table("dm_tasks").
			Joins("LEFT JOIN domains ON dm_tasks.did = domains.id").
			Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
			Where("accounts.uid = ?", userID).Count(&n)
	case "certs":
		db.Table("cert_orders").
			Joins("LEFT JOIN cert_accounts ON cert_orders.aid = cert_accounts.id").
			Where("cert_accounts.uid = ?", userID).Count(&n)
	case "schedules":
		db.Table("schedule_tasks").
			Joins("LEFT JOIN domains ON schedule_tasks.did = domains.id").
			Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
			Where("accounts.uid = ?", userID).Count(&n)
	case "deploys":
		db.Table("cert_deploys").Where("uid = ?", userID).Count(&n)
	case "optimize_ip":
		db.Table("optimize_ips").
			Joins("LEFT JOIN domains ON optimize_ips.did = domains.id").
			Joins("LEFT JOIN accounts ON domains.aid = accounts.id").
			Where("accounts.uid = ?", userID).Count(&n)
	}
	return int(n)
}

/* resourceLabels 各维度的中文名，用于错误提示 */
var resourceLabels = map[string]string{
	"domains":       "域名",
	"records":       "子域名",
	"accounts":      "DNS 账户",
	"cert_accounts": "证书账户",
	"monitors":      "容灾监控任务",
	"certs":         "SSL 证书",
	"schedules":     "定时切换任务",
	"deploys":       "证书部署任务",
	"optimize_ip":   "优选 IP 任务",
}

/* quotaExceededMessage 超限提示，带上当前用量与上限便于用户与管理员判断 */
func quotaExceededMessage(resource string, used, limit int) string {
	label := resourceLabels[resource]
	if label == "" {
		label = resource
	}
	return label + "配额已达上限（" + itoaInt(used) + "/" + itoaInt(limit) + "），请联系管理员调整"
}

/* sysconfigGetQuotaDefault 未订阅套餐时的系统默认配额，未配置则全维度不限 */
func sysconfigGetQuotaDefault() string {
	return sysconfig.GetValue("quota_default")
}

/* nowFunc 当前时间，抽出来便于测试替换 */
func nowFunc() time.Time {
	return time.Now()
}

/* itoaInt 整数转字符串 */
func itoaInt(v int) string {
	return strconv.Itoa(v)
}

/*
 * CheckFeature 功能开关检查
 *
 * 套餐的 features 里可以按功能维度开关（如禁用批量操作、关闭 API 访问）。
 * 未订阅套餐或该功能未出现时一律放行——与配额同理，存量实例不应因为
 * 新增了功能开关而被静默关掉既有能力
 */
func CheckFeature(c *gin.Context, feature string) bool {
	var features map[string]bool
	if !loadFeaturesForUser(c, &features) {
		return true
	}
	enabled, configured := features[feature]
	if !configured {
		return true
	}
	if enabled {
		return true
	}
	middleware.ErrorResponse(c, featureDisabledMessage(feature))
	return false
}

/* loadFeaturesForUser 取用户套餐里的功能开关，无套餐时返回 false */
func loadFeaturesForUser(c *gin.Context, out *map[string]bool) bool {
	userID := currentUIDUint(c)
	if userID == 0 || c.GetInt("level") >= 2 {
		return false
	}
	var sub models.UserSubscription
	if err := database.DB.Where("user_id = ? AND status = 1", userID).First(&sub).Error; err != nil {
		return false
	}
	if sub.ExpireTime != nil && !sub.ExpireTime.After(nowFunc()) {
		return false
	}
	var plan models.Plan
	if err := database.DB.First(&plan, "id = ?", sub.PlanID).Error; err != nil || plan.Status != 1 {
		return false
	}
	if strings.TrimSpace(plan.Features) == "" {
		return false
	}
	return json.Unmarshal([]byte(plan.Features), out) == nil
}

/* featureLabels 功能开关的中文名 */
var featureLabels = map[string]string{
	"ssl":           "SSL 证书",
	"monitor":       "容灾监控",
	"schedule":      "定时切换",
	"optimize_ip":   "优选 IP",
	"api_access":    "API 访问",
	"batch_ops":     "批量操作",
	"export_import": "导入导出",
}

/* featureDisabledMessage 功能被套餐关闭时的提示 */
func featureDisabledMessage(feature string) string {
	label := featureLabels[feature]
	if label == "" {
		label = feature
	}
	return label + "未包含在当前套餐内，请联系管理员开通"
}
