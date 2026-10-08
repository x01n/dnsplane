package handler

import "testing"

/*
 * 系统设置保存时的代理键校验测试
 *
 * proxy_type 与 proxy_port 是代理配置里仅有的两个有确定值域的键：
 * 前者决定 httpx 的类型分派，后者决定代理地址是否可用。
 * 校验只忽略非法的那一个键，不拒绝整批 —— 前端保存时提交整个 config 对象，
 * 拒绝整批会让用户其余合法改动一并失败。
 */

/* TestProxyConfigRejectReason 逐键校验代理配置的放行与拦截 */
func TestProxyConfigRejectReason(t *testing.T) {
	cases := []struct {
		name          string
		key           string
		value         string
		globalProxyOn bool
		wantReject    bool
	}{
		{"代理类型留空跟随默认", "proxy_type", "", true, false},
		{"代理类型 http", "proxy_type", "http", true, false},
		{"代理类型 socks5", "proxy_type", "socks5", true, false},
		{"代理类型大小写与空白归一", "proxy_type", " SOCKS5 ", true, false},
		{"代理类型 https 被拦截", "proxy_type", "https", true, true},
		{"代理类型 socks 被拦截", "proxy_type", "socks", true, true},

		{"全局代理开启时端口合法", "proxy_port", "8080", true, false},
		{"全局代理开启时端口下界合法", "proxy_port", "1", true, false},
		{"全局代理开启时端口上界合法", "proxy_port", "65535", true, false},
		{"全局代理开启时端口为空被拦截", "proxy_port", "", true, true},
		{"全局代理开启时端口为 0 被拦截", "proxy_port", "0", true, true},
		{"全局代理开启时端口越界被拦截", "proxy_port", "65536", true, true},
		{"全局代理开启时端口非数字被拦截", "proxy_port", "abc", true, true},
		{"全局代理开启时端口为负被拦截", "proxy_port", "-1", true, true},

		{"全局代理关闭时端口草稿值放行", "proxy_port", "0", false, false},
		{"全局代理关闭时端口非数字放行", "proxy_port", "abc", false, false},

		{"其余键不受代理校验影响", "mail_port", "abc", true, false},
		{"代理地址本身不做枚举校验", "proxy_server", "127.0.0.1", true, false},
		{"代理开关本身不做枚举校验", "proxy_enabled", "whatever", true, false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := proxyConfigRejectReason(tc.key, tc.value, tc.globalProxyOn)
			if tc.wantReject && got == "" {
				t.Fatalf("%s=%q 应被拦截，实际放行", tc.key, tc.value)
			}
			if !tc.wantReject && got != "" {
				t.Fatalf("%s=%q 应放行，实际被拦截: %s", tc.key, tc.value, got)
			}
		})
	}
}
