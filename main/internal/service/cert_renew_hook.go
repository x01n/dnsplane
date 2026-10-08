package service

import "sync"

/*
 * 证书通知钩子
 * handler 包在签发流程的成功/失败分支调用这两个钩子，由 main 在启动时注入 service 侧实现。
 * 采用注入而非直接依赖，是为了让 api/handler 只面向抽象，不反向耦合具体通知实现。
 */
var (
	certIssueSuccessNotifyFn func(orderID uint)
	certIssueFailNotifyFn    func(orderID uint)
	certNotifyMu             sync.RWMutex
)

/* SetCertIssueNotifyHooks 注入证书签发成功/失败通知实现，nil 表示停用 */
func SetCertIssueNotifyHooks(success, fail func(orderID uint)) {
	certNotifyMu.Lock()
	defer certNotifyMu.Unlock()
	certIssueSuccessNotifyFn = success
	certIssueFailNotifyFn = fail
}

/* CertIssueSuccessNotify 触发证书签发成功通知，未注入时为空操作 */
func CertIssueSuccessNotify(orderID uint) {
	certNotifyMu.RLock()
	fn := certIssueSuccessNotifyFn
	certNotifyMu.RUnlock()
	if fn != nil {
		fn(orderID)
	}
}

/* CertIssueFailNotify 触发证书签发失败通知，未注入时为空操作 */
func CertIssueFailNotify(orderID uint) {
	certNotifyMu.RLock()
	fn := certIssueFailNotifyFn
	certNotifyMu.RUnlock()
	if fn != nil {
		fn(orderID)
	}
}

var CertRenewProcessStarter func(orderID uint)

func SetCertRenewProcessStarter(fn func(orderID uint)) {
	CertRenewProcessStarter = fn
}
