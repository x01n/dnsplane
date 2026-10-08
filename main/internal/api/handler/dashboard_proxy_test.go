package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * 代理目标校验测试
 *
 * TestProxy 曾对 IP 字面量一并拒绝（loopback / private / link-local），
 * 导致按前端 placeholder 填 127.0.0.1:1080 的本机代理（Clash / v2ray 最常见的部署）
 * 能保存、能生效，却永远通不过「测试代理连接」。现只保留域名侧拦截。
 *
 * 校验与出站请求已解耦为纯函数 proxyTargetRejectReason，因此这里只验证校验决策，
 * 不发起任何真实连接
 */

/* TestProxyTargetLoopbackAllowed 回环与私网 IP 字面量一律放行 */
func TestProxyTargetLoopbackAllowed(t *testing.T) {
	hosts := []string{
		"127.0.0.1",
		"10.0.0.1",
		"192.168.1.1",
		"172.16.0.1",
		"::1",
	}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			if reason := proxyTargetRejectReason(host); reason != "" {
				t.Fatalf("host=%q 应放行，实际被拦截: %s", host, reason)
			}
		})
	}
}

/* TestProxyTargetInternalDomainRejected 解析后才指向内网的域名仍被拦截 */
func TestProxyTargetInternalDomainRejected(t *testing.T) {
	hosts := []string{
		"localhost",
		"foo.local",
		"bar.internal",
	}
	for _, host := range hosts {
		t.Run(host, func(t *testing.T) {
			if reason := proxyTargetRejectReason(host); reason == "" {
				t.Fatalf("host=%q 应被拦截，实际放行", host)
			}
		})
	}
}

/* TestProxyTargetPublicHostAllowed 公网主机名不受影响 */
func TestProxyTargetPublicHostAllowed(t *testing.T) {
	if reason := proxyTargetRejectReason("example.com"); reason != "" {
		t.Fatalf("公网主机名应放行，实际被拦截: %s", reason)
	}
}

/*
 * TestProxyEndpointGuardBranches 端到端断言 TestProxy 守卫区各分支
 *
 * 无 aes_key 时 middleware.ErrorResponse 走明文输出，可直接解析响应体。
 * 用例只喂停在守卫区的输入，不会发起真实出站连接
 *
 * 三个必填缺失用例的实际响应是「参数解析失败」而非「参数错误」：
 * DecryptAndVerify 与 JSONBodyToDecryptedData 两个会写入 decrypted_data 的中间件
 * 在本仓均无挂载点，BindDecryptedData 恒走 ShouldBindJSON 明文分支，
 * 请求在 binding:"required" 阶段就被拦下，走不到 TestProxy 手写的「参数错误」
 */
func TestProxyEndpointGuardBranches(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		msg  string
	}{
		{"端口缺失停在必填校验", `{"type":"socks5","host":"127.0.0.1"}`, "参数解析失败"},
		{"类型缺失停在必填校验", `{"host":"127.0.0.1","port":1080}`, "参数解析失败"},
		{"地址缺失停在必填校验", `{"type":"socks5","port":1080}`, "参数解析失败"},
		{"内网域名被拦截", `{"type":"socks5","host":"localhost","port":1080}`, "代理地址不能指向内网域名"},
		{"内网域名大小写与空白归一后仍被拦截", `{"type":"socks5","host":"  Foo.LOCAL  ","port":1080}`, "代理地址不能指向内网域名"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, w := newTestContext()
			c.Set("level", 2)
			c.Request = httptest.NewRequest(http.MethodPost, "/api/system/proxy/test", strings.NewReader(tc.raw))
			c.Request.Header.Set("Content-Type", "application/json")

			TestProxy(c)

			var body struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
			}
			if body.Msg != tc.msg {
				t.Fatalf("响应不符，期望 %q，实际响应：%s", tc.msg, w.Body.String())
			}
		})
	}
}
