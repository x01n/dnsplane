package apiv1

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/handler"
)

/*
 * registerCloudflareRoutes 注册 Cloudflare 模块路由
 *
 * 路径形态沿用 batch1 的「动作动词 + body 传参」，不保留控制台的 :id 路径段：
 *   1. CF 的 handler 只认路径参数（27 处 c.Param("id")），拿不到 body 里的标识；
 *      若保留 :id，签名必须按 /cloudflare/hostnames/add/7 逐条计算，路径随域名变化，
 *      而服务端路由表无法声明该形状，调用方更容易签错。统一到固定路径 + body 后，
 *      签名对象是常量，客户端只需实现一次
 *   2. 同一前缀下 /hostnames/list 与 /hostnames/add 已是静态段，再挂 /hostnames/:id
 *      会让 gin 路由树在「静态子段 + 参数子段」间反复回溯
 *
 * :id 的语义分两族，因此分两个子组各挂一个桥接：
 *   自定义主机名 / 回退源 / DCV → 域名 ID，对应 body 的 domain_id
 *   Tunnels 全族                → 账户 ID，对应 body 的 account_id
 *
 * 全部接口要求 domain 模块：与控制台侧栏一致（自定义主机名与 Tunnels 均挂在「域名管理」下）
 */
func registerCloudflareRoutes(v1 *gin.RouterGroup) {
	g := v1.Group("/cloudflare")

	// 自定义主机名、回退源与 DCV：:id 是域名 ID
	d := g.Group("", cfDomainBridge())
	d.POST("/hostnames/list", requireModuleMW("domain"), handler.GetCustomHostnames)
	d.POST("/hostnames/add", requireModuleMW("domain"), handler.AddCustomHostname)
	d.POST("/hostnames/update", requireModuleMW("domain"), handler.UpdateCustomHostname)
	d.POST("/hostnames/delete", requireModuleMW("domain"), handler.DeleteCustomHostname)
	d.POST("/hostnames/batch-add", requireModuleMW("domain"), handler.BatchAddCustomHostnames)
	d.POST("/hostnames/batch-update", requireModuleMW("domain"), handler.BatchUpdateCustomHostnames)
	d.POST("/hostnames/batch-delete", requireModuleMW("domain"), handler.BatchDeleteCustomHostnames)
	d.POST("/hostnames/txt-targets", requireModuleMW("domain"), handler.GetCustomHostnameTxtTargets)
	d.POST("/hostnames/refresh", requireModuleMW("domain"), handler.RefreshCustomHostname)
	d.POST("/hostnames/setup-validation", requireModuleMW("domain"), handler.SetupHostnameValidation)

	d.POST("/fallback/get", requireModuleMW("domain"), handler.GetFallbackOrigin)
	d.POST("/fallback/set", requireModuleMW("domain"), handler.SetFallbackOrigin)
	d.POST("/fallback/delete", requireModuleMW("domain"), handler.DeleteFallbackOrigin)
	d.POST("/dcv-delegation-uuid", requireModuleMW("domain"), handler.GetDcvDelegationUUID)
	// 该接口本就只读 body，不需要 :id 桥接
	d.POST("/default-line", requireModuleMW("domain"), handler.GetDomainDefaultLine)

	// Tunnel 全族：:id 是账户 ID
	t := g.Group("/tunnels", cfAccountBridge())
	t.POST("/list", requireModuleMW("domain"), handler.GetTunnels)
	t.POST("/add", requireModuleMW("domain"), handler.AddTunnel)
	t.POST("/delete", requireModuleMW("domain"), handler.DeleteTunnel)
	t.POST("/token", requireModuleMW("domain"), handler.GetTunnelToken)
	t.POST("/public-hostnames/list", requireModuleMW("domain"), handler.GetTunnelPublicHostnames)
	t.POST("/public-hostnames/save", requireModuleMW("domain"), handler.SaveTunnelPublicHostname)
	t.POST("/public-hostnames/delete", requireModuleMW("domain"), handler.DeleteTunnelPublicHostname)
	t.POST("/cidr/list", requireModuleMW("domain"), handler.GetCidrRoutes)
	t.POST("/cidr/add", requireModuleMW("domain"), handler.AddCidrRoute)
	t.POST("/cidr/delete", requireModuleMW("domain"), handler.DeleteCidrRoute)
	t.POST("/hostname-routes/list", requireModuleMW("domain"), handler.GetHostnameRoutes)
	t.POST("/hostname-routes/add", requireModuleMW("domain"), handler.AddHostnameRoute)
	t.POST("/hostname-routes/delete", requireModuleMW("domain"), handler.DeleteHostnameRoute)
}

/*
 * registerSystemRoutes 注册系统管理模块路由
 *
 * 一律限定管理员，与控制台侧栏一致：操作日志、请求日志、系统设置在侧栏均为 adminOnly。
 * 这些接口返回或改写全局数据，普通用户即便有 module 权限也不应触达他人的资源与凭据
 * （GetSystemConfig 会回 mail_password、tgbot_token、oauth_*_secret 等全部敏感项）
 *
 * 与控制台 system 段的对应关系与取舍：
 *   - 日志与请求日志的 body → query 由 systemParamBridge 搬运，控制台版本一律从
 *     c.Query 取参，v1 的 JSON body 它读不到
 *   - 通知渠道测试（mail/telegram/... 共 11 条）不迁：它们只读库中配置、不发任何入参，
 *     是给设置页「测试连接」按钮用的连通性检查，对程序化调用没有稳定语义，
 *     且每次调用都会真实外发一条消息
 *   - TestProxy 同为设置页的连通性检查，需提交明文代理口令，理由同上
 *   - GetCronConfig / UpdateCronConfig 不迁：本地定时器的开关，对外部调用方无意义
 *   - auth-control（登录注册控制）与 accel（一键加速平台）属 batch1/batch3 的职责范围
 */
func registerSystemRoutes(v1 *gin.RouterGroup) {
	g := v1.Group("/system", systemParamBridge())

	g.POST("/config/get", adminOnly, handler.GetSystemConfig)
	g.POST("/config/update", adminOnly, handler.UpdateSystemConfig)
	g.POST("/optimizeip/config/get", adminOnly, handler.GetOptimizeIPConfig)
	g.POST("/optimizeip/config/update", adminOnly, handler.UpdateOptimizeIPConfig)
	g.POST("/optimizeip/license", adminOnly, handler.QueryOptimizeIPLicense)
	g.POST("/cache/clear", adminOnly, handler.ClearCache)

	// 操作日志
	g.POST("/logs/list", adminOnly, handler.GetLogs)
	g.POST("/logs/detail", adminOnly, handler.GetLogDetail)
	g.POST("/logs/clean", adminOnly, handler.CleanOperationLogs)

	// 请求日志
	g.POST("/request-logs/list", adminOnly, handler.GetRequestLogs)
	g.POST("/request-logs/detail", adminOnly, handler.GetRequestByID)
	g.POST("/request-logs/error", adminOnly, handler.GetErrorByID)
	g.POST("/request-logs/stats", adminOnly, handler.GetRequestStats)
	g.POST("/request-logs/clean", adminOnly, handler.CleanRequestLogs)
}

/* registerUserRoutes 注册用户模块路由 */
func registerUserRoutes(v1 *gin.RouterGroup) {
	/*
	 * 用户管理族的 :id 在 body 里各有分工：
	 *   用户级操作（update / delete / api-key/reset）→ uid
	 *   域名授权族（permissions/*）              → uid 与 perm_id 两个路径参数
	 *
	 * 用户级三条单独挂（body 键是 uid），授权族留在组外单独挂——
	 * 它们需要两个别名的映射，与用户级不同
	 */
	g := v1.Group("/users", paramAlias(map[string]string{"id": "uid"}))

	// 自身的账户信息；重置 API Key 需管理员，故仅由 users/api-key/reset 提供
	g.POST("/me", handler.GetUserInfo)
	g.POST("/me/password", handler.ChangePassword)

	// 用户管理（管理员）
	g.POST("/list", adminOnly, handler.GetUsers)
	g.POST("/create", adminOnly, handler.CreateUser)
	g.POST("/update", adminOnly, handler.UpdateUser)
	g.POST("/delete", adminOnly, handler.DeleteUser)
	g.POST("/api-key/reset", adminOnly, handler.ResetAPIKey)

	/*
	 * 域名授权（管理员）
	 *
	 * 这四条读两个路径参数（id 与 permId），而组级桥接只回填 id，
	 * 故单独挂一个双别名的映射。body 键的取舍见 paramAlias 的说明：
	 * 用户 ID 用 uid、权限行 ID 用 perm_id
	 */
	v1.POST("/users/permissions/list", adminOnly,
		paramAlias(map[string]string{"id": "uid", "permId": "perm_id"}),
		handler.GetUserPermissions)
	v1.POST("/users/permissions/add", adminOnly,
		paramAlias(map[string]string{"id": "uid", "permId": "perm_id"}),
		handler.AddUserPermission)
	v1.POST("/users/permissions/update", adminOnly,
		paramAlias(map[string]string{"id": "uid", "permId": "perm_id"}),
		handler.UpdateUserPermission)
	v1.POST("/users/permissions/delete", adminOnly,
		paramAlias(map[string]string{"id": "uid", "permId": "perm_id"}),
		handler.DeleteUserPermission)
}

/* registerOptimizeIPRoutes 注册优选 IP 模块路由 */
func registerOptimizeIPRoutes(v1 *gin.RouterGroup) {
	/*
	 * 组级挂别名：update / toggle / delete / run 四条读 c.Param("id")
	 *
	 * 此前本组是裸 Group，四条因拿不到 id 而全部失效——实测（任务确实存在于库中）：
	 *   update id=1 -> 4006「任务不存在」（未改）
	 *   toggle id=1 -> code=0「设置成功」（库里 active 未变）
	 *   delete id=1 -> code=0「删除成功」（库里仍剩 1 行）
	 *   run    id=1 -> 4006「任务不存在」
	 *
	 * 后两条尤其危险：**报成功而什么都没做**。控制台的路径是
	 * POST /api/optimizeip/tasks/:id（router.go:331-334），v1 没有对应的路径段，
	 * 只能靠桥接把 body 的 id 回填成路径参数
	 */
	g := v1.Group("/optimizeip", paramAlias(map[string]string{"id": "id"}))

	g.POST("/tasks/list", requireModuleMW("domain"), handler.GetOptimizeIPList)
	g.POST("/tasks/create", requireModuleMW("domain"), handler.CreateOptimizeIPTask)
	g.POST("/tasks/update", requireModuleMW("domain"), handler.UpdateOptimizeIPTask)
	g.POST("/tasks/delete", requireModuleMW("domain"), handler.DeleteOptimizeIPTask)
	g.POST("/tasks/toggle", requireModuleMW("domain"), handler.ToggleOptimizeIPTask)
	g.POST("/tasks/run", requireModuleMW("domain"), handler.RunOptimizeIPTask)
	g.POST("/status", requireModuleMW("domain"), handler.GetOptimizeIPStatus)
}

/* adminOnly 管理员限定中间件 */
func adminOnly(c *gin.Context) {
	if !requireAdmin(c) {
		return
	}
	c.Next()
}
