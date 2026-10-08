package service

import (
	"testing"

	"main/internal/httpx"
)

/*
 * ExplicitOn 口径同源断言
 *
 * 同一「仅 true/1/yes/on 为开」判据此前在 httpx 与 service 两处各写一遍，
 * 任一处漏改都会让全局代理开关与证书通知开关的取值口径分叉。
 * 现已收敛到 sysconfig.ExplicitOn，本用例以「同一输入串喂两侧、结果必须一致」
 * 锁定同源关系，防止日后有人就地改回内联实现
 */
func TestExplicitOnParityWithHTTPX(t *testing.T) {
	setupCertNoticeTest(t)

	const key = "explicit_on_parity_probe_key"
	inputs := []string{"", "true", "1", "yes", "on", "TRUE", " On ", "false", "0", "no", "off", "disable", "2"}

	// 未配置状态：service 侧读库得空串，httpx 侧直接传空串
	if got, want := sysconfigBoolExplicitOn(key), httpx.ExplicitOn(""); got != want {
		t.Fatalf("未配置时口径分叉: service=%v httpx=%v", got, want)
	}

	for _, in := range inputs {
		setCertNoticeConfig(t, key, in)
		if got, want := sysconfigBoolExplicitOn(key), httpx.ExplicitOn(in); got != want {
			t.Errorf("输入 %q 口径分叉: service.sysconfigBoolExplicitOn=%v httpx.ExplicitOn=%v", in, got, want)
		}
	}
}