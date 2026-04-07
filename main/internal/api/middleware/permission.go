package middleware

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"main/internal/database"
	"main/internal/models"

	"github.com/gin-gonic/gin"
)

/* activeDelegatedPermissionsByUID 返回用户在某域名下未过期的委派权限行（不含账户所有者逻辑） */
func activeDelegatedPermissionsByUID(userIDStr, domainName string) []models.Permission {
	var list []models.Permission
	database.DB.Where("uid = ? AND domain = ?", userIDStr, domainName).Find(&list)
	now := time.Now()
	out := make([]models.Permission, 0, len(list))
	for _, p := range list {
		if p.ExpireTime != nil && p.ExpireTime.Before(now) {
			continue
		}
		out = append(out, p)
	}
	return out
}

/* subGrantCoversRR 权限行中的 sub 配置是否覆盖请求中的主机记录名 rr（通配或列表命中） */
func subGrantCoversRR(grant, rr string) bool {
	rr = strings.TrimSpace(rr)
	g := strings.TrimSpace(grant)
	if g == "" || g == "*" {
		return true
	}
	if rr == "" {
		return false
	}
	for _, sub := range strings.Split(g, ",") {
		if strings.TrimSpace(sub) == rr {
			return true
		}
	}
	return false
}

/* mergePermissionRows 将同一域名下多条权限合并为一条有效策略（列表请求无具体 rr 时使用） */
func mergePermissionRows(rows []models.Permission) models.Permission {
	hasWild := false
	readOnly := false
	set := make(map[string]struct{})
	for _, p := range rows {
		s := strings.TrimSpace(p.SubDomain)
		if s == "" || s == "*" {
			hasWild = true
		}
		if p.ReadOnly {
			readOnly = true
		}
		for _, x := range strings.Split(s, ",") {
			x = strings.TrimSpace(x)
			if x != "" && x != "*" {
				set[x] = struct{}{}
			}
		}
	}
	var m models.Permission
	m.UserID = rows[0].UserID
	m.DomainID = rows[0].DomainID
	m.Domain = rows[0].Domain
	m.ReadOnly = readOnly
	if hasWild {
		m.SubDomain = "*"
	} else {
		keys := make([]string, 0, len(set))
		for k := range set {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		m.SubDomain = strings.Join(keys, ",")
	}
	return m
}

/*
 * pickDelegatedPermission 在多条委派权限中选出当前请求应对应的一条，或合并为一条
 * 请求中带具体 rr 时优先匹配列表/通配；仅 domain_id 时合并子域列表供列表接口过滤
 */
func pickDelegatedPermission(c *gin.Context, userID, domainName string) (*models.Permission, bool) {
	rows := activeDelegatedPermissionsByUID(userID, domainName)
	if len(rows) == 0 {
		return nil, false
	}
	if len(rows) == 1 {
		p := rows[0]
		return &p, true
	}
	rr := strings.TrimSpace(getSubDomainFromRequest(c))
	if rr != "" {
		var wild *models.Permission
		for i := range rows {
			p := &rows[i]
			g := strings.TrimSpace(p.SubDomain)
			if g == "" || g == "*" {
				if wild == nil {
					wild = p
				}
				continue
			}
			if subGrantCoversRR(g, rr) {
				return p, true
			}
		}
		if wild != nil {
			return wild, true
		}
		return nil, false
	}
	m := mergePermissionRows(rows)
	return &m, true
}

/*
 * Permission 域名权限检查中间件
 * 功能：从 GET query 或 POST 解密数据中读取 domain_id，校验用户域名访问权限
 *       管理员(level>=2)直接放行，普通用户检查域名+子域名权限
 */
func Permission() gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := c.GetString("user_id")
		userLevel := c.GetInt("level")

		// 管理员直接放行
		if userLevel >= 2 {
			c.Next()
			return
		}

		domainID := getDomainIDFromRequest(c)
		if domainID == "" {
			// 没有 domain_id 参数的请求直接放行（由具体 handler 判断）
			c.Next()
			return
		}

		// 查找域名
		var domain models.Domain
		if err := database.WithContext(c).Where("id = ?", domainID).First(&domain).Error; err != nil {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "域名不存在"})
			c.Abort()
			return
		}

		// 检查域名所属账户是否为当前用户自己添加的
		var account models.Account
		isAccountOwner := false
		uidNum, _ := strconv.ParseUint(userID, 10, 32)
		if database.WithContext(c).Where("id = ?", domain.AccountID).First(&account).Error == nil && account.UserID == uint(uidNum) {
			isAccountOwner = true
		}

		if isAccountOwner {
			// 用户自有账户的域名：完全开放，无子域名限制
			c.Set("perm_read_only", false)
			c.Set("perm_domain", domain.Name)
			c.Set("perm_sub_domain", "*")
			c.Next()
			return
		}

		perm, ok := pickDelegatedPermission(c, userID, domain.Name)
		if !ok || perm == nil {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "无权限操作该域名"})
			c.Abort()
			return
		}

		// 检查只读权限（写操作拦截；POST 中含仅查询类接口如 whois）
		if perm.ReadOnly && isDomainScopedWriteRequest(c) {
			c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "该域名为只读权限，无法执行写操作"})
			c.Abort()
			return
		}

		// 子域二次校验（请求带 rr 且权限为有限列表时）
		if perm.SubDomain != "" && perm.SubDomain != "*" {
			subDomain := strings.TrimSpace(getSubDomainFromRequest(c))
			if subDomain != "" {
				if !subGrantCoversRR(perm.SubDomain, subDomain) {
					c.JSON(http.StatusOK, gin.H{"code": -1, "msg": "无权限操作该子域名"})
					c.Abort()
					return
				}
			}
		}

		c.Set("perm_read_only", perm.ReadOnly)
		c.Set("perm_domain", domain.Name)
		c.Set("perm_sub_domain", perm.SubDomain)

		c.Next()
	}
}

/* getDomainIDFromRequest 从请求中提取域名 ID（支持解密数据/query 参数） */
func getDomainIDFromRequest(c *gin.Context) string {
	// 1. 从解密后的数据中读取
	if data, exists := c.Get("decrypted_data"); exists {
		if m, ok := data.(map[string]interface{}); ok {
			if id, ok := m["domain_id"]; ok {
				return parseStringID(id)
			}
			if id, ok := m["did"]; ok {
				return parseStringID(id)
			}
		}
	}

	// 2. 从 GET query 参数读取
	if idStr := c.Query("domain_id"); idStr != "" {
		return idStr
	}

	// 3. 从 URL 路径参数读取
	if idStr := c.Param("id"); idStr != "" {
		return idStr
	}

	return ""
}

/* getSubDomainFromRequest 从请求中提取子域名 */
func getSubDomainFromRequest(c *gin.Context) string {
	// 从解密数据中读取
	if data, exists := c.Get("decrypted_data"); exists {
		if m, ok := data.(map[string]interface{}); ok {
			if rr, ok := m["name"].(string); ok {
				return rr
			}
			if rr, ok := m["rr"].(string); ok {
				return rr
			}
		}
	}

	// 从 query 参数读取
	if rr := c.Query("name"); rr != "" {
		return rr
	}
	if rr := c.Query("rr"); rr != "" {
		return rr
	}

	return ""
}

/*
 * isDomainScopedWriteRequest 在已解析出 domain_id 的上下文中判断是否应视为「写操作」
 * API 仅暴露 GET/POST：POST 默认视为写；例外为纯查询类 POST（如 WHOIS）
 */
func isDomainScopedWriteRequest(c *gin.Context) bool {
	method := c.Request.Method
	switch method {
	case "PUT", "DELETE", "PATCH":
		return true
	case "POST":
		p := c.Request.URL.Path
		if strings.HasSuffix(p, "/whois") {
			return false
		}
		return true
	default:
		return false
	}
}

/* parseStringID 将 interface{} 转换为 string ID（兼容 string/float64） */
func parseStringID(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		return fmt.Sprintf("%v", val)
	case int:
		return fmt.Sprintf("%d", val)
	case uint:
		return fmt.Sprintf("%d", val)
	}
	if v != nil {
		return fmt.Sprintf("%v", v)
	}
	return ""
}

/*
 * getDomainPermission 域名权限查询公共逻辑
 * 功能：查询域名→检查账户归属→委派权限合并
 * 返回 perm（nil 表示是账户所有者）、allowed 标志
 */
func getDomainPermission(userID string, domainID string) (perm *models.Permission, isOwner bool, allowed bool) {
	var domain models.Domain
	if err := database.DB.Where("id = ?", domainID).First(&domain).Error; err != nil {
		return nil, false, false
	}

	var account models.Account
	uidNum, _ := strconv.ParseUint(userID, 10, 32)
	if database.DB.Where("id = ?", domain.AccountID).First(&account).Error == nil && account.UserID == uint(uidNum) {
		return nil, true, true
	}

	rows := activeDelegatedPermissionsByUID(userID, domain.Name)
	if len(rows) == 0 {
		return nil, false, false
	}
	m := mergePermissionRows(rows)
	return &m, false, true
}

/* CheckDomainPermission 检查用户是否有域名权限（供 handler 直接调用） */
func CheckDomainPermission(userID string, userLevel int, domainID string) bool {
	if userLevel >= 2 {
		return true
	}
	_, _, allowed := getDomainPermission(userID, domainID)
	return allowed
}

/* CheckSubDomainPermission 检查用户是否有子域名权限 */
func CheckSubDomainPermission(userID string, userLevel int, domainID string, subDomain string) bool {
	if userLevel >= 2 {
		return true
	}
	perm, isOwner, allowed := getDomainPermission(userID, domainID)
	if !allowed {
		return false
	}
	if isOwner || perm == nil {
		return true
	}
	sd := strings.TrimSpace(subDomain)
	if sd == "" {
		return true
	}
	return subGrantCoversRR(perm.SubDomain, sd)
}

/*
 * ApplyUserPermissionModules 将用户功能权限 JSON 写入上下文
 * - permissions 列为空或解析失败：视为未配置，UserModuleAllowed 对非管理员一律放行（兼容旧库）
 * - 已配置 JSON（含 []）：仅允许列表中的模块 key
 */
func ApplyUserPermissionModules(c *gin.Context, permissionsJSON string) {
	s := strings.TrimSpace(permissionsJSON)
	if s == "" {
		c.Set("perm_modules_configured", false)
		return
	}
	var arr []string
	if err := json.Unmarshal([]byte(s), &arr); err != nil {
		c.Set("perm_modules_configured", false)
		return
	}
	c.Set("perm_modules_configured", true)
	c.Set("perm_modules", arr)
}

/* UserModuleAllowed 非管理员是否拥有某功能模块（如 monitor、cert、deploy、domain） */
func UserModuleAllowed(c *gin.Context, module string) bool {
	if c.GetInt("level") >= 2 {
		return true
	}
	v, ok := c.Get("perm_modules_configured")
	configured, _ := v.(bool)
	if !ok || !configured {
		return true
	}
	listRaw, _ := c.Get("perm_modules")
	list, _ := listRaw.([]string)
	for _, m := range list {
		if m == module {
			return true
		}
	}
	return false
}
