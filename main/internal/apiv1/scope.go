package apiv1

import (
	"strings"

	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
)

/*
 * V1 的鉴权范围
 *
 * 控制台把鉴权散在各 handler 内部（requireAdmin / requireUserModule /
 * CheckDomainPermission 三种混用），对外接口需要一层统一、可枚举的范围声明，
 * 否则新增接口时很容易漏掉某一维度的校验，而且从路由表上看不出每个接口要什么权限。
 *
 * 四个维度相互独立，按需组合：
 *   模块：用户是否被授予该功能模块（permissions JSON 里的 key）
 *   域名：用户对该域名是否有任何权限（自有账户 或 permission 表授权）
 *   子域：授权是否覆盖本次操作的子域名
 *   只读：授权中 read_only 为真时拒绝写操作
 */

/* requireModule 要求用户具备某个功能模块的访问权限 */
func requireModule(c *gin.Context, module string) bool {
	if middleware.UserModuleAllowed(c, module) {
		return true
	}
	Failf(c, CodeModuleDenied, "无 %s 模块的访问权限", module)
	c.Abort()
	return false
}

/* requireAdmin 要求管理员身份 */
func requireAdmin(c *gin.Context) bool {
	if c.GetInt("level") >= 2 {
		return true
	}
	Fail(c, CodeAdminRequired, "")
	c.Abort()
	return false
}

/*
 * loadDomain 按域名 ID 取域名实体，顺带校验该域名是否存在
 *
 * 单独抽出是因为后续每一层校验都要用它，重复查库既浪费也会让错误分支分叉
 */
func loadDomain(c *gin.Context, domainID string) (*models.Domain, bool) {
	if strings.TrimSpace(domainID) == "" {
		Fail(c, CodeParamMissing, "缺少域名 ID")
		c.Abort()
		return nil, false
	}
	var domain models.Domain
	if err := database.WithContext(c).First(&domain, domainID).Error; err != nil {
		Failf(c, CodeDomainNotFound, "域名 ID %s 不存在", domainID)
		c.Abort()
		return nil, false
	}
	return &domain, true
}

/* requireDomain 要求用户对该域名有操作权限 */
func requireDomain(c *gin.Context, domain *models.Domain) bool {
	if middleware.CheckDomainPermission(c.GetString("user_id"), c.GetInt("level"), itoa(domain.ID)) {
		return true
	}
	Failf(c, CodeDomainDenied, "无域名 %s 的操作权限", domain.Name)
	c.Abort()
	return false
}

/* requireSubDomain 要求授权覆盖指定子域名，空子域视为不限制 */
func requireSubDomain(c *gin.Context, domain *models.Domain, subDomain string) bool {
	if middleware.CheckSubDomainPermission(c.GetString("user_id"), c.GetInt("level"), itoa(domain.ID), subDomain) {
		return true
	}
	Failf(c, CodeSubDomainDenied, "无子域名 %s.%s 的操作权限", subDomain, domain.Name)
	c.Abort()
	return false
}

/*
 * requireWritable 拒绝只读授权下的写操作
 *
 * 判定依据是当前用户在该域名上被授予的权限记录，而非上下文里的 perm_read_only。
 * 控制台的 Permission 中间件确已挂载（router.go:104），但 v1 刻意不依赖它：
 * 中间件从**请求**里取 domain_id（支持四种来源），v1 这一层从**已加载的域名实体**取，
 * 两者是不同的判据。自己查权限表，Permission() 的挂载位置或取参逻辑变化时
 * v1 的判定不会跟着失效
 */
func requireWritable(c *gin.Context, domain *models.Domain) bool {
	if c.GetInt("level") >= 2 {
		return true
	}
	perm, isOwner, allowed := domainPermissionOf(c.GetString("user_id"), itoa(domain.ID))
	if !allowed || isOwner || perm == nil {
		// 自有域名、管理员、或本就无授权（无授权会在 requireDomain 阶段被拒）都不受只读限制
		return true
	}
	if !perm.ReadOnly {
		return true
	}
	Failf(c, CodeReadOnly, "域名 %s 仅有只读权限，不能执行写操作", domain.Name)
	c.Abort()
	return false
}

/*
 * scopeDomain 组合：加载域名 → 校验域名权限 → 校验子域 → 写操作时校验只读
 *
 * 绝大多数涉及域名的接口都是这四步的排列，抽成一个入口避免各处顺序不一致。
 * needWrite 为真时才会施加只读限制，查询类接口不受影响
 */
func scopeDomain(c *gin.Context, domainID, subDomain string, needWrite bool) (*models.Domain, bool) {
	domain, ok := loadDomain(c, domainID)
	if !ok {
		return nil, false
	}
	if !requireDomain(c, domain) {
		return nil, false
	}
	if subDomain != "" && !requireSubDomain(c, domain, subDomain) {
		return nil, false
	}
	if needWrite && !requireWritable(c, domain) {
		return nil, false
	}
	return domain, true
}
