package apiv1

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/handler"
)

/*
 * V1 的配额闸门
 *
 * 控制台有九处 CheckQuota（accounts/domains/records/monitors/certs/schedules/
 * deploys/optimize_ip 八个维度 + 证书部署账户复用 accounts），而 v1 此前**一处都没有**。
 * 排查后发现真正敞口的只有一处：解析记录的**批量新增**。
 * 控制台只给单条 CreateRecord 挂了 records 配额，批量族没有——
 * 批量是一次请求建多条，缺这道门等于绕过。
 *
 * 其余创建接口（域名、DNS 账户、证书账户/订单/部署、监控、定时、优选 IP）
 * 的配额门在控制台 handler 内部，v1 复用的正是那些 handler，
 * 因此**不需要也不应在路由层重复挂**：重复挂会让拒绝发生在 handler 校验之前，
 * 错误码由路由层给出而不是 AdaptResponse 按文案映射的那一个，
 * 同一个接口经两个入口调用时错误码不一致。
 *
 * 判据是「这个 handler 内部有没有 CheckQuota」——有就不挂，没有才挂
 */

/*
 * requireQuota 配额闸门中间件
 *
 * 复用 handler.CheckQuota 的判定逻辑（三层优先级：个体覆盖 > 套餐 > 系统默认），
 * 但把错误输出的 shape 换成 v1 格式
 *
 * dimension 取 max_ 之后的维度名：domains / records / accounts / monitors /
 * certs / schedules / deploys / optimize_ip
 */
func requireQuota(dimension string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if handler.QuotaAllows(c, dimension) {
			c.Next()
			return
		}
		used, limit := handler.QuotaUsageOf(c, dimension)
		Failf(c, CodeResourceLocked, "%s已达上限（%d/%d），请先删除部分资源或联系管理员调整配额",
			handler.QuotaLabelOf(dimension), used, limit)
		c.Abort()
	}
}
