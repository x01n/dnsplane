package apiv1

import (
	"github.com/gin-gonic/gin"

	"main/internal/api/middleware"
)

/*
 * V1 对外 API 的路由装配
 *
 * 挂在引擎根节点而非 /api 分组下：控制台的 /api 分组带 CSRF double-submit 校验，
 * 而 API Key 客户端没有 cookie，也无从取 csrf token——继承那条链会让所有
 * 写请求在 CSRF 处被拒。签名本身就是身份与完整性的凭据，无需再叠一层
 *
 * 中间件顺序与理由：
 *   1. RequestTrace   生成 X-Request-ID 并把请求/响应落库，必须最外层，
 *                     否则内层 panic 会让它 c.Next() 之后的落库代码不执行
 *   2. Recovery       挡在 RequestTrace 之内，panic 时先写错误上下文再让外层完成落库
 *   3. CORS           对外接口需要跨域，放在认证之前以便 OPTIONS 预检不被拦
 *   4. APIKeyAuth     签名校验与身份注入
 *   5. AuditLog       写操作落审计库，读 c.GetString("user_id")，须在认证之后
 */
func Setup(engine *gin.Engine) {
	v1 := engine.Group("/api/v1")

	v1.Use(middleware.RequestTrace())
	v1.Use(middleware.Recovery())
	v1.Use(middleware.CORS())
	v1.Use(APIKeyAuth())
	v1.Use(JSONBodyToDecryptedData())
	v1.Use(middleware.AuditLog())
	// 响应格式适配放在链末：它包裹后续所有写出，必须在认证与限流之后
	v1.Use(AdaptResponse())
	{
		registerDomainRoutes(v1)
		registerRecordRoutes(v1)
		registerAccountRoutes(v1)
		registerCertRoutes(v1)
		registerMonitorRoutes(v1)
		registerScheduleRoutes(v1)
		registerCloudflareRoutes(v1)
		registerSystemRoutes(v1)
		registerUserRoutes(v1)
		registerOptimizeIPRoutes(v1)
	}
}
