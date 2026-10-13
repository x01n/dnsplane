package apiv1

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/handler"
)

/* registerAccountRoutes 注册 DNS 账户模块路由 */
func registerAccountRoutes(v1 *gin.RouterGroup) {
	g := v1.Group("/accounts")

	g.POST("/list", requireModuleMW("domain"), handler.GetAccounts)
	g.POST("/detail", requireModuleMW("domain"), handler.GetAccountDetail)
	g.POST("/create", requireModuleMW("domain"), handler.CreateAccount)
	g.POST("/update", requireModuleMW("domain"), handler.UpdateAccount)
	g.POST("/delete", requireModuleMW("domain"), handler.DeleteAccount)
	g.POST("/check", requireModuleMW("domain"), handler.CheckAccount)
}

/* registerCertRoutes 注册证书模块路由 */
func registerCertRoutes(v1 *gin.RouterGroup) {
	/*
	 * 证书账户与订单的 :id 即 body 的 account_id / order_id，
	 * 子组各自回填；部署账户三条只读 body，不需要桥接
	 */
	g := v1.Group("/cert")

	// 证书账户：:id 是账户 ID
	acct := g.Group("/accounts", paramAlias(map[string]string{"id": "account_id"}))
	acct.POST("/list", requireModuleMW("cert"), handler.GetCertAccounts)
	acct.POST("/create", requireModuleMW("cert"), handler.CreateCertAccount)
	acct.POST("/update", requireModuleMW("cert"), handler.UpdateCertAccount)
	acct.POST("/delete", requireModuleMW("cert"), handler.DeleteCertAccount)
	g.POST("/providers", requireModuleMW("cert"), handler.GetCertProviders)

	// 证书订单：:id 是订单 ID
	order := g.Group("/orders", paramAlias(map[string]string{"id": "order_id"}))
	order.POST("/list", requireModuleMW("cert"), handler.GetCertOrders)
	order.POST("/create", requireModuleMW("cert"), handler.CreateCertOrder)
	order.POST("/process", requireModuleMW("cert"), handler.ProcessCertOrder)
	order.POST("/delete", requireModuleMW("cert"), handler.DeleteCertOrder)
	order.POST("/log", requireModuleMW("cert"), handler.GetCertOrderLog)
	order.POST("/detail", requireModuleMW("cert"), handler.GetCertOrderDetail)
	order.POST("/download", requireModuleMW("cert"), handler.DownloadCertOrder)
	order.POST("/auto", requireModuleMW("cert"), handler.ToggleCertOrderAuto)

	// 证书部署：:id 是部署任务 ID
	dep := g.Group("/deploys", paramAlias(map[string]string{"id": "deploy_id"}))
	dep.POST("/list", requireModuleMW("deploy"), handler.GetCertDeploys)
	dep.POST("/create", requireModuleMW("deploy"), handler.CreateCertDeploy)
	dep.POST("/update", requireModuleMW("deploy"), handler.UpdateCertDeploy)
	dep.POST("/delete", requireModuleMW("deploy"), handler.DeleteCertDeploy)
	dep.POST("/process", requireModuleMW("deploy"), handler.ProcessCertDeploy)

	/*
	 * 部署账户三条只读 body 里的 id 字段（BindDecryptedData 走 decrypted_data，
	 * 不受 body 被消费的影响），与整组的 :id 语义不同，故留在组外
	 */
	g.POST("/deploy-accounts/list", requireModuleMW("deploy"), handler.DeployAccountList)
	g.POST("/deploy-accounts/detail", requireModuleMW("deploy"), handler.DeployAccountDetail)
	g.POST("/deploy-accounts/create", requireModuleMW("deploy"), handler.DeployAccountAdd)
	g.POST("/deploy-accounts/update", requireModuleMW("deploy"), handler.DeployAccountEdit)
	g.POST("/deploy-accounts/delete", requireModuleMW("deploy"), handler.DeployAccountDelete)
	g.POST("/deploy-accounts/check", requireModuleMW("deploy"), handler.DeployAccountCheck)
	g.POST("/deploy-types", requireModuleMW("deploy"), handler.DeployTypes)
}

/* registerMonitorRoutes 注册容灾监控模块路由 */
func registerMonitorRoutes(v1 *gin.RouterGroup) {
	g := v1.Group("/monitor")

	g.POST("/tasks/list", requireModuleMW("monitor"), handler.GetMonitorTasks)
	g.POST("/tasks/create", requireModuleMW("monitor"), handler.CreateMonitorTask)
	g.POST("/tasks/update", requireModuleMW("monitor"), handler.UpdateMonitorTask)
	g.POST("/tasks/delete", requireModuleMW("monitor"), handler.DeleteMonitorTask)
	g.POST("/tasks/toggle", requireModuleMW("monitor"), handler.ToggleMonitorTask)
	g.POST("/tasks/switch", requireModuleMW("monitor"), handler.SwitchMonitorTask)
	g.POST("/tasks/logs", requireModuleMW("monitor"), handler.GetMonitorLogs)
	g.POST("/tasks/history", requireModuleMW("monitor"), handler.GetMonitorHistory)
	g.POST("/tasks/uptime", requireModuleMW("monitor"), handler.GetMonitorUptime)
	g.POST("/tasks/resolve-status", requireModuleMW("monitor"), handler.GetResolveStatus)
	g.POST("/tasks/batch", requireModuleMW("monitor"), handler.BatchCreateMonitorTasks)
	g.POST("/tasks/auto-create", requireModuleMW("monitor"), handler.AutoCreateMonitorTask)
	g.POST("/overview", requireModuleMW("monitor"), handler.GetMonitorOverview)
	g.POST("/status", requireModuleMW("monitor"), handler.GetMonitorStatus)
	g.POST("/lookup", requireModuleMW("monitor"), handler.LookupRecord)
}

/* registerScheduleRoutes 注册定时切换模块路由 */
func registerScheduleRoutes(v1 *gin.RouterGroup) {
	g := v1.Group("/schedule")

	// 定时任务的 :id 在 body 里就叫 id，不存在语义分歧
	task := g.Group("/tasks", paramAlias(map[string]string{"id": "id"}))

	g.POST("/tasks/list", requireModuleMW("domain"), handler.GetScheduleTasks)
	g.POST("/tasks/create", requireModuleMW("domain"), handler.CreateScheduleTask)
	task.POST("/update", requireModuleMW("domain"), handler.UpdateScheduleTask)
	task.POST("/delete", requireModuleMW("domain"), handler.DeleteScheduleTask)
	task.POST("/toggle", requireModuleMW("domain"), handler.ToggleScheduleTask)
	g.POST("/tasks/batch", requireModuleMW("domain"), handler.BatchScheduleTaskAction)
}
