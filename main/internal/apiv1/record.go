package apiv1

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/handler"
)

/*
 * 解析记录模块的 v1 接口
 *
 * 写操作在此重写为同步：控制台的 CreateRecord / UpdateRecord / DeleteRecord /
 * SetRecordStatus 内部用 utils.SafeGo 把云端调用丢到后台并立即回「提交成功」，
 * 对程序化调用方而言拿不到成败与失败原因，无法据此决定后续动作
 */

/* registerRecordRoutes 注册记录模块路由 */
func registerRecordRoutes(v1 *gin.RouterGroup) {
	/*
	 * 记录族整组挂桥接：三个只读 c.Param("id") 的接口（weight / quickinfo / logs）
	 * 以及只读 c.Query 的 smartparse，都靠它拿到入参
	 */
	g := v1.Group("/records", paramAlias(map[string]string{"id": "domain_id"}))

	g.POST("/list", requireModuleMW("domain"), handler.GetRecords)
	// 写操作走本包的同步实现，见 record_sync.go
	/*
	 * 记录的两条创建路径都要挂：单条与批量
	 *
	 * 控制台只给单条挂了 records 配额，批量族没有——那是控制台的既有缺口，
	 * 不该复制到对外接口上。对外接口是程序化调用的入口，
	 * 缺一道门就等于给了绕过的确定性路径
	 */
	g.POST("/create", requireModuleMW("domain"), requireQuota("records"), CreateRecordSync)
	g.POST("/update", requireModuleMW("domain"), UpdateRecordSync)
	g.POST("/delete", requireModuleMW("domain"), DeleteRecordSync)
	g.POST("/status", requireModuleMW("domain"), SetRecordStatusSync)
	g.POST("/remark", requireModuleMW("domain"), UpdateRecordRemarkSync)

	g.POST("/batch/add", requireModuleMW("domain"), requireQuota("records"), handler.BatchAddRecords)
	g.POST("/batch/edit", requireModuleMW("domain"), handler.BatchEditRecords)
	g.POST("/batch/action", requireModuleMW("domain"), handler.BatchActionRecords)

	g.POST("/weight", requireModuleMW("domain"), handler.GetRecordWeight)
	g.POST("/weight/update", requireModuleMW("domain"), handler.UpdateRecordWeight)
	g.POST("/quickinfo", requireModuleMW("domain"), handler.GetRecordQuickInfo)
	g.POST("/logs", requireModuleMW("domain"), handler.GetRecordChangeLog)

	/*
	 * 记录族的查询与路径参数都叫 domain_id，但 smartparse 两者都不读：
	 * 它只认 c.Query("value")，而桥接的标量搬运正是靠 body→query 生效，
	 * 故这条单独用完整的 paramBridge（含 query 搬运）而非组级的 paramAlias
	 */
	v1.POST("/records/smartparse",
		requireModuleMW("domain"),
		paramBridge(map[string]string{"id": "domain_id"}),
		handler.SmartParseRecord)
}
