package service

import (
	"testing"
)

/*
 * 证书签发通知钩子的注入与空操作语义测试
 * handler 侧在签发流程调用钩子，未注入（如单元测试、未启动 main）时必须安全空操作
 */

/* TestCertIssueHooksNoopWhenNotInjected 未注入时应静默返回，不 panic */
func TestCertIssueHooksNoopWhenNotInjected(t *testing.T) {
	SetCertIssueNotifyHooks(nil, nil)
	CertIssueSuccessNotify(1)
	CertIssueFailNotify(1)
}

/* TestCertIssueHooksDispatch 注入后应按顺序转发订单 ID */
func TestCertIssueHooksDispatch(t *testing.T) {
	var gotSuccess, gotFail []uint
	SetCertIssueNotifyHooks(
		func(id uint) { gotSuccess = append(gotSuccess, id) },
		func(id uint) { gotFail = append(gotFail, id) },
	)
	t.Cleanup(func() { SetCertIssueNotifyHooks(nil, nil) })

	CertIssueSuccessNotify(11)
	CertIssueFailNotify(22)
	CertIssueSuccessNotify(33)

	if len(gotSuccess) != 2 || gotSuccess[0] != 11 || gotSuccess[1] != 33 {
		t.Errorf("成功钩子转发不符: %v", gotSuccess)
	}
	if len(gotFail) != 1 || gotFail[0] != 22 {
		t.Errorf("失败钩子转发不符: %v", gotFail)
	}
}

/* TestCertIssueHooksResetToNil 重新注入 nil 后应恢复空操作 */
func TestCertIssueHooksResetToNil(t *testing.T) {
	called := 0
	SetCertIssueNotifyHooks(func(uint) { called++ }, func(uint) { called++ })
	SetCertIssueNotifyHooks(nil, nil)

	CertIssueSuccessNotify(1)
	CertIssueFailNotify(1)
	if called != 0 {
		t.Errorf("重置为 nil 后不应再回调，实际调用 %d 次", called)
	}
}
