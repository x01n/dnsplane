package service


var CertRenewProcessStarter func(orderID uint)

func SetCertRenewProcessStarter(fn func(orderID uint)) {
	CertRenewProcessStarter = fn
}
