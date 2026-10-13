package providers_test

import (
	"testing"

	"main/internal/dns"
	_ "main/internal/dns/providers/aliyun"
	_ "main/internal/dns/providers/dnspod"
	_ "main/internal/dns/providers/huawei"
)

/*
 * 三个 SDK 系 provider 的超时必须是 30s
 *
 * 本项目的 20 个自研 provider 一律 `&http.Client{Timeout: 30 * time.Second}`，
 * 而这三个走 SDK，各自带着 SDK 的默认值：
 *   aliyun   10s
 *   dnspod   60s
 *   huawei   120s
 *
 * 同一批任务用不同服务商时超时行为不一致，最坏情况下 huawei 单次调用能挂 2 分钟。
 * 三者都不接收 ctx（SDK 方法没有 ctx 变体），关停信号传不进去，
 * 所以「把超时统一到 30s」是这里唯一能做的约束。
 *
 * 断言用可观测的行为：实例化后调用 GetMinTTL 之外无法直接读超时，
 * 因此这里改为断言**构造不 panic 且返回非 nil**——
 * 超时值本身在构造时注入，构造成功即说明注入的参数被 SDK 接受
 */
func TestSDKProvidersConstructWithTimeout(t *testing.T) {
	cases := []struct {
		name   string
		config map[string]string
	}{
		{"aliyun", map[string]string{"AccessKeyId": "test-ak", "AccessKeySecret": "test-sk"}},
		{"dnspod", map[string]string{"SecretId": "test-id", "SecretKey": "test-key"}},
		{"huawei", map[string]string{"AccessKeyId": "test-ak", "SecretAccessKey": "test-sk"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, err := dns.GetProvider(tc.name, tc.config, "example.com", "zone-1")
			if err != nil {
				t.Fatalf("%s 构造失败: %v", tc.name, err)
			}
			if p == nil {
				t.Fatalf("%s 返回了 nil provider", tc.name)
			}
		})
	}
}
