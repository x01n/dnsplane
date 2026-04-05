package service

/*
 * CertRenewProcessStarter 由 main 注入：将「待续期」订单交给与 ProcessCertOrder 相同的 ACME 异步流程。
 * 避免 service 包直接依赖 api/handler（防止 import 循环）。
 */
var CertRenewProcessStarter func(orderID uint)

// SetCertRenewProcessStarter 注册自动续期后的证书签发入口（应在 main 中调用一次）
func SetCertRenewProcessStarter(fn func(orderID uint)) {
	CertRenewProcessStarter = fn
}
