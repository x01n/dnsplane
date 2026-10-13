package handler

import (
	"main/internal/api/middleware"
	"main/internal/database"
	"main/internal/models"
	"net/http"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

/*
 * cert_authz.go：cert.go 中所有 handler 的 ownership 校验集中实现。
 *
 * 安全审计 R-1：cert.go 整套接口（账户/订单/下载）此前完全无鉴权，任意已登录普通用户
 * 可调 GET /api/cert/orders 列出全站订单后用 GET /api/cert/orders/:id/download?type=key
 * 拖走他人证书私钥；POST /process 也可强行触发别人的 ACME 续签烧 LE 配额。
 *
 * 模型关系：
 *   CertOrder.AccountID -> CertAccount.ID
 *   CertAccount.UserID  -> User.ID
 * 因此订单的属主 = 关联 CertAccount 的 UserID。
 *
 * 校验语义：
 *   - 管理员（isAdmin）放行所有
 *   - 否则要求资源属主等于当前 UID
 */

// requireCertAccountOwner 校验当前用户对 certAccountID 是否有写权限。
// 失败时已写响应，handler 应直接 return false 出口。
func requireCertAccountOwner(c *gin.Context, accountID uint) (*models.CertAccount, bool) {
	var acc models.CertAccount
	if err := database.DB.First(&acc, accountID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "账户不存在"})
		return nil, false
	}
	if !isAdmin(c) && acc.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权操作该证书账户")
		return nil, false
	}
	return &acc, true
}

// requireCertOrderOwner 校验当前用户对 certOrderID 是否有写/读权限（通过关联账户 UID）。
func requireCertOrderOwner(c *gin.Context, orderID uint) (*models.CertOrder, bool) {
	var order models.CertOrder
	if err := database.DB.First(&order, orderID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "订单不存在"})
		return nil, false
	}
	if isAdmin(c) {
		return &order, true
	}
	var acc models.CertAccount
	if err := database.DB.Select("id", "uid").First(&acc, order.AccountID).Error; err != nil {
		middleware.ErrorResponse(c, "订单关联的证书账户已失效")
		return nil, false
	}
	if acc.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权操作该证书订单")
		return nil, false
	}
	return &order, true
}

// scopeCertAccountQuery 给 CertAccount 列表查询补 UID 过滤；管理员看全量。
func scopeCertAccountQuery(c *gin.Context, q *gorm.DB) *gorm.DB {
	if isAdmin(c) {
		return q
	}
	return q.Where("uid = ?", currentUIDUint(c))
}

// scopeCertOrderQuery 通过子查询限定订单关联的账户必须属于当前用户。
func scopeCertOrderQuery(c *gin.Context, q *gorm.DB) *gorm.DB {
	if isAdmin(c) {
		return q
	}
	return q.Where(
		"aid IN (?)",
		database.DB.Model(&models.CertAccount{}).Select("id").Where("uid = ?", currentUIDUint(c)),
	)
}

// requireCertDeployOwner 校验当前用户对 certDeployID 是否有写权限。
//
// 不复用 requireCertOrderOwner：后者的所有权链是 CertOrder.aid → CertAccount.uid，
// 而 CertDeploy 自带 uid 列（创建时写 currentUIDUint），且 CertDeploy.aid 指向
// **部署账户**，与 CertOrder.aid（证书账户）是不同实体，走订单链会判错属主。
//
// 安全审计：部署任务的 config 里含外送目标地址，越权修改等于把他人私钥改送到自己机器；
// 越权触发 process 更会直接把 order.FullChain + order.PrivateKey 外送。
// 修复前创建/更新/删除/执行四个入口均无校验，仅有 checkLocalDeployAllowed 拦 local 类型，
// 而后者只防「借进程权限执行命令」，改用 ssh 类型即可绕开。
func requireCertDeployOwner(c *gin.Context, deployID uint) (*models.CertDeploy, bool) {
	var deploy models.CertDeploy
	if err := database.DB.First(&deploy, deployID).Error; err != nil {
		c.JSON(http.StatusNotFound, gin.H{"code": -1, "msg": "部署任务不存在"})
		return nil, false
	}
	if !isAdmin(c) && deploy.UserID != currentUIDUint(c) {
		middleware.ErrorResponse(c, "无权操作该部署任务")
		return nil, false
	}
	return &deploy, true
}

// scopeCertDeployQuery 给 CertDeploy 列表查询补 UID 过滤；管理员看全量。
func scopeCertDeployQuery(c *gin.Context, q *gorm.DB) *gorm.DB {
	if isAdmin(c) {
		return q
	}
	return q.Where("cert_deploys.uid = ?", currentUIDUint(c))
}
