package apiv1

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/handler"
)

/*
 * 域名模块的 v1 接口
 *
 * 与控制台的实现共享同一份业务逻辑：直接复用 handler 包里的同名函数，
 * 只把认证与响应格式换成 v1 的。这些 handler 内部依赖的上下文键
 * （user_id / username / level / decrypted_data）均由 APIKeyAuth 与
 * JSONBodyToDecryptedData 注入，因此无需改动即可在 v1 下运行
 *
 * 不复用的一类：内部使用 utils.SafeGo 异步执行的写接口。对外的程序化调用
 * 需要拿到真实结果与失败原因，「提交成功」再后台执行会让调用方无法判断成败，
 * 那些接口在本包内重写为同步实现
 */
func registerDomainRoutes(v1 *gin.RouterGroup) {
	/*
	 * 域名的 :id 在 body 里叫 domain_id，整组回填
	 *
	 * 不复用控制台 handler 是刻意的：它们一律只读 c.Param("id")，
	 * 而 v1 的路由表没有路径参数，不回填就会拿到空串或零值
	 */
	g := v1.Group("/domains", paramAlias(map[string]string{"id": "domain_id"}))

	g.POST("/list", requireModuleMW("domain"), handler.GetDomains)
	g.POST("/detail", requireModuleMW("domain"), handler.GetDomainDetail)
	g.POST("/create", requireModuleMW("domain"), handler.CreateDomain)
	g.POST("/sync", requireModuleMW("domain"), handler.SyncDomains)
	g.POST("/delete", requireModuleMW("domain"), handler.DeleteDomain)
	g.POST("/update", requireModuleMW("domain"), handler.UpdateDomain)
	// 批量接入域名：不挂 requireQuota。控制台的 BatchDomainAction 也没有配额门，
	// 批量是「先提交解析再逐个接入」的两段式，在这里拦会在解析阶段就整体失败
	g.POST("/batch", requireModuleMW("domain"), handler.BatchDomainAction)

	g.POST("/lines", requireModuleMW("domain"), handler.GetRecordLines)
	g.POST("/whois", requireModuleMW("domain"), handler.QueryWhois)
	g.POST("/aliases", requireModuleMW("domain"), handler.GetDomainAliases)
	g.POST("/aliases/add", requireModuleMW("domain"), handler.AddDomainAlias)

	g.POST("/categories", requireModuleMW("domain"), handler.GetDomainCategories)
	g.POST("/categories/create", requireModuleMW("domain"), handler.CreateDomainCategory)
	g.POST("/categories/set", requireModuleMW("domain"), handler.SetDomainCategory)

	g.POST("/groups", requireModuleMW("domain"), handler.GetRecordGroups)
	g.POST("/groups/change", requireModuleMW("domain"), handler.ChangeRecordGroup)

	/*
	 * 以下三条的 body 键与整组不同名，单独挂各自的桥接
	 *
	 * 不能留在 g 组内：组级桥接会把 body 里的 domain_id 回填到 id 上，
	 * 而这三条读的分别是 aliasId 与 body 里那个就叫 id 的分类主键
	 */
	v1.POST("/domains/aliases/delete",
		requireModuleMW("domain"),
		paramAlias(map[string]string{"aliasId": "alias_id"}),
		handler.DeleteDomainAlias)

	v1.POST("/domains/categories/update",
		requireModuleMW("domain"),
		paramAlias(map[string]string{"id": "id"}),
		handler.UpdateDomainCategory)

	v1.POST("/domains/categories/delete",
		requireModuleMW("domain"),
		paramAlias(map[string]string{"id": "id"}),
		handler.DeleteDomainCategory)
}

/* requireModuleMW 把模块校验包装成中间件，让路由表上能直接看出每个接口的范围 */
func requireModuleMW(module string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if !requireModule(c, module) {
			return
		}
		c.Next()
	}
}
