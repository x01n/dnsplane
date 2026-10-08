package httpx

import (
	"net/http"
	"net/url"
	"path/filepath"
	"testing"

	"main/internal/cache"
	"main/internal/config"
	"main/internal/database"
	"main/internal/models"
	"main/internal/sysconfig"
)

/*
 * 全局出站代理配置测试
 *
 * 覆盖 proxy_enabled / proxy_server / proxy_port / proxy_type / proxy_user /
 * proxy_password 六个键到出站代理 URL 的全部决策分支，重点是非法配置的
 * 回退路径：地址写错时既不 panic，也不静默直连，须退回环境变量代理。
 *
 * 有意不覆盖「无任何代理配置时直连」：ProxyFromEnvironment 在进程内首次调用后
 * 会把环境快照进缓存，仅用 t.Setenv 无法重置，强行断言会把测试间的执行顺序
 * 变成隐式依赖
 */

/* setupProxyTestDB 初始化临时 SQLite 与内存缓存 */
func setupProxyTestDB(t *testing.T) {
	t.Helper()
	if err := database.Init(&config.DatabaseConfig{
		Driver:   "sqlite",
		FilePath: filepath.Join(t.TempDir(), "httpx.db"),
	}); err != nil {
		t.Fatalf("初始化测试数据库失败: %v", err)
	}
	cache.Init(&cache.Config{})
	t.Cleanup(func() {
		_ = cache.Close()
		_ = database.Close()
	})
}

/* clearProxyConfig 清空系统配置并同步失效缓存，保证用例之间互不影响 */
func clearProxyConfig(t *testing.T) {
	t.Helper()
	if err := database.DB.Where("1 = 1").Delete(&models.SysConfig{}).Error; err != nil {
		t.Fatalf("清空配置失败: %v", err)
	}
	// 内存缓存不随删库失效：不同步清理的话，前一个用例写入的键会被后一个用例读到
	sysconfig.Invalidate(
		"proxy_enabled", "proxy_server", "proxy_port",
		"proxy_type", "proxy_user", "proxy_password",
	)
}

/* seedProxyConfig 逐条写入系统配置，走 SetValue 以同步填充缓存 */
func seedProxyConfig(t *testing.T, kv map[string]string) {
	t.Helper()
	for k, v := range kv {
		sysconfig.SetValue(k, v)
	}
}

/* setupEnvProxy 固定环境变量代理与绕行名单，使回退分支的期望值可确定 */
func setupEnvProxy(t *testing.T) {
	t.Helper()
	t.Setenv("HTTP_PROXY", "http://env-proxy.test:3128")
	t.Setenv("HTTPS_PROXY", "http://env-proxy.test:3128")
	t.Setenv("NO_PROXY", "")
}

/* envFallback 计算给定请求在当前环境变量下的代理，作为回退分支的期望值 */
func envFallback(t *testing.T, req *http.Request) *url.URL {
	t.Helper()
	want, err := http.ProxyFromEnvironment(req)
	if err != nil {
		t.Fatalf("计算环境变量代理失败: %v", err)
	}
	return want
}

/* sameURL 判断两个可空的代理 URL 是否等价 */
func sameURL(a, b *url.URL) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return a.String() == b.String()
}

/* newProxyRequest 构造一个出站请求，仅用于驱动代理决策 */
func newProxyRequest(t *testing.T, target string) *http.Request {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, target, nil)
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	return req
}

/*
 * TestProxyForRequestDecisionTable 逐个验证代理决策分支
 *
 * wantScheme 为空表示该用例期望回退到环境变量代理，此时与
 * http.ProxyFromEnvironment 的实际结果逐字段比对
 */
func TestProxyForRequestDecisionTable(t *testing.T) {
	setupProxyTestDB(t)
	setupEnvProxy(t)

	cases := []struct {
		name       string
		cfg        map[string]string
		wantScheme string
		wantHost   string
		wantUser   string
		wantPass   string
	}{
		{
			name: "六键全未配置时回退环境变量",
			cfg:  nil,
		},
		{
			name: "开关显式关闭时回退环境变量",
			cfg: map[string]string{
				"proxy_enabled": "false",
				"proxy_server":  "127.0.0.1",
				"proxy_port":    "8080",
				"proxy_type":    "http",
			},
		},
		{
			name: "开关开启且地址端口合法时使用数据库代理",
			cfg: map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "127.0.0.1",
				"proxy_port":    "8080",
				"proxy_type":    "http",
			},
			wantScheme: "http",
			wantHost:   "127.0.0.1:8080",
		},
		{
			name: "socks5 类型使用 socks5 协议",
			cfg: map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "127.0.0.1",
				"proxy_port":    "1080",
				"proxy_type":    "socks5",
			},
			wantScheme: "socks5",
			wantHost:   "127.0.0.1:1080",
		},
		{
			name: "配置凭据时写入代理 URL 的用户信息",
			cfg: map[string]string{
				"proxy_enabled":  "true",
				"proxy_server":   "127.0.0.1",
				"proxy_port":     "8080",
				"proxy_type":     "http",
				"proxy_user":     "proxyuser",
				"proxy_password": "proxypass",
			},
			wantScheme: "http",
			wantHost:   "127.0.0.1:8080",
			wantUser:   "proxyuser",
			wantPass:   "proxypass",
		},
		{
			name: "开关开启但地址为空时回退环境变量",
			cfg: map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "",
				"proxy_port":    "8080",
			},
		},
		{
			name: "开关开启但端口非法时回退环境变量",
			cfg: map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "127.0.0.1",
				"proxy_port":    "0",
			},
		},
		{
			name: "未知代理类型按 http 处理",
			cfg: map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "127.0.0.1",
				"proxy_port":    "8080",
				"proxy_type":    "https",
			},
			wantScheme: "http",
			wantHost:   "127.0.0.1:8080",
		},
		{
			name: "地址含非法字符时回退环境变量",
			cfg: map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "127.0.0.1 8080",
				"proxy_port":    "8080",
			},
		},
	}

	req := newProxyRequest(t, "https://api.example.test/v1")

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			clearProxyConfig(t)
			seedProxyConfig(t, tc.cfg)

			got, err := proxyForRequest(req)
			if err != nil {
				t.Fatalf("解析代理失败: %v", err)
			}

			if tc.wantScheme == "" {
				if want := envFallback(t, req); !sameURL(got, want) {
					t.Fatalf("应回退环境变量代理 %v，实际 %v", want, got)
				}
				return
			}

			if got == nil {
				t.Fatalf("期望代理 %s://%s，实际回退到了环境变量", tc.wantScheme, tc.wantHost)
			}
			if got.Scheme != tc.wantScheme || got.Host != tc.wantHost {
				t.Fatalf("期望代理 %s://%s，实际 %s", tc.wantScheme, tc.wantHost, got)
			}
			assertUserInfo(t, got, tc.wantUser, tc.wantPass)
		})
	}
}

/*
 * TestProxyPortOutOfRangeFallsBack 端口越界与非法取值一律回退环境变量
 *
 * proxy_port 由前端以数字提交，后端仍须自证：越界值若被放进代理 URL，
 * 出站请求会以一个必然失败的代理地址发出，比直连更难排查
 */
func TestProxyPortOutOfRangeFallsBack(t *testing.T) {
	setupProxyTestDB(t)
	setupEnvProxy(t)

	req := newProxyRequest(t, "https://api.example.test/v1")

	for _, port := range []string{"", "0", "65536", "abc", "-1"} {
		t.Run("port="+port, func(t *testing.T) {
			clearProxyConfig(t)
			seedProxyConfig(t, map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  "127.0.0.1",
				"proxy_port":    port,
			})

			got, err := proxyForRequest(req)
			if err != nil {
				t.Fatalf("解析代理失败: %v", err)
			}
			if want := envFallback(t, req); !sameURL(got, want) {
				t.Fatalf("端口 %q 应回退环境变量代理 %v，实际 %v", port, want, got)
			}
		})
	}
}

/*
 * TestProxyServerInvalidCharsFallBack 地址含空格/下划线/换行时回退环境变量
 *
 * 这些字符会让代理地址无法被正确解析，此时既不能 panic，
 * 也不能把畸形地址交给 Transport —— 只能退回环境变量或直连
 */
func TestProxyServerInvalidCharsFallBack(t *testing.T) {
	setupProxyTestDB(t)
	setupEnvProxy(t)

	req := newProxyRequest(t, "https://api.example.test/v1")

	for _, host := range []string{"127.0.0.1 8080", "proxy_host", "127.0.0.1\n8080"} {
		t.Run("server="+host, func(t *testing.T) {
			clearProxyConfig(t)
			seedProxyConfig(t, map[string]string{
				"proxy_enabled": "true",
				"proxy_server":  host,
				"proxy_port":    "8080",
			})

			got, err := proxyForRequest(req)
			if err != nil {
				t.Fatalf("解析代理失败: %v", err)
			}
			if want := envFallback(t, req); !sameURL(got, want) {
				t.Fatalf("地址 %q 应回退环境变量代理 %v，实际 %v", host, want, got)
			}
		})
	}
}

/*
 * TestProxyIgnoresTargetScheme 代理选取不随目标 scheme 变化
 *
 * 标准库 Transport 只按目标 scheme 决定是否走代理与用何种方式对接，
 * 不看代理 URL 的 scheme。若此处按目标 scheme 分支，https 目标
 * 会拿到与 http 目标不同的代理，从而在实现层引入多余的差异
 */
func TestProxyIgnoresTargetScheme(t *testing.T) {
	setupProxyTestDB(t)
	setupEnvProxy(t)
	clearProxyConfig(t)
	seedProxyConfig(t, map[string]string{
		"proxy_enabled": "true",
		"proxy_server":  "127.0.0.1",
		"proxy_port":    "8080",
		"proxy_type":    "http",
	})

	httpsProxy, err := proxyForRequest(newProxyRequest(t, "https://api.example.test/v1"))
	if err != nil {
		t.Fatalf("解析 https 目标代理失败: %v", err)
	}
	httpProxy, err := proxyForRequest(newProxyRequest(t, "http://api.example.test/v1"))
	if err != nil {
		t.Fatalf("解析 http 目标代理失败: %v", err)
	}

	if !sameURL(httpsProxy, httpProxy) {
		t.Fatalf("不同 target scheme 得到了不同代理: https=%v http=%v", httpsProxy, httpProxy)
	}
}

/*
 * TestProxyConfigTakesEffectOnNextRequest 配置变更下一次请求即生效
 *
 * sysconfig.GetValue 带 60 秒 TTL 缓存，保存设置后若仍读到旧值，
 * 管理员会看到「改了没用」。此处验证写入路径同步刷新了缓存
 */
func TestProxyConfigTakesEffectOnNextRequest(t *testing.T) {
	setupProxyTestDB(t)
	setupEnvProxy(t)
	clearProxyConfig(t)
	seedProxyConfig(t, map[string]string{
		"proxy_enabled": "true",
		"proxy_server":  "127.0.0.1",
		"proxy_port":    "8080",
		"proxy_type":    "http",
	})

	req := newProxyRequest(t, "https://api.example.test/v1")

	before, err := proxyForRequest(req)
	if err != nil {
		t.Fatalf("解析代理失败: %v", err)
	}
	if before == nil || before.Host != "127.0.0.1:8080" {
		t.Fatalf("初始代理应为 127.0.0.1:8080，实际 %v", before)
	}

	sysconfig.SetValue("proxy_server", "10.0.0.9")

	after, err := proxyForRequest(req)
	if err != nil {
		t.Fatalf("解析代理失败: %v", err)
	}
	if after == nil || after.Host != "10.0.0.9:8080" {
		t.Fatalf("改动后的下一次请求应使用新地址 10.0.0.9:8080，实际 %v", after)
	}
}

/*
 * TestInstallWiresDefaultTransport 安装点把决策函数接到 DefaultTransport
 *
 * 安装必须是命令式的：若改用 init()，go test 进程虽执行 init 却不跑 main，
 * 测试里替换过的 DefaultTransport 会被无声覆盖
 */
func TestInstallWiresDefaultTransport(t *testing.T) {
	setupProxyTestDB(t)
	setupEnvProxy(t)
	clearProxyConfig(t)
	seedProxyConfig(t, map[string]string{
		"proxy_enabled": "true",
		"proxy_server":  "127.0.0.1",
		"proxy_port":    "8080",
		"proxy_type":    "http",
	})

	tr, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		t.Fatalf("DefaultTransport 类型异常: %T", http.DefaultTransport)
	}
	originalProxy := tr.Proxy
	t.Cleanup(func() { tr.Proxy = originalProxy })

	Install()

	got, err := tr.Proxy(newProxyRequest(t, "https://api.example.test/v1"))
	if err != nil {
		t.Fatalf("DefaultTransport.Proxy 解析失败: %v", err)
	}
	if got == nil || got.Host != "127.0.0.1:8080" {
		t.Fatalf("DefaultTransport 应使用数据库代理 127.0.0.1:8080，实际 %v", got)
	}

	// 全局代理关闭后应回退环境变量，而不是直连
	seedProxyConfig(t, map[string]string{"proxy_enabled": "false"})
	req := newProxyRequest(t, "https://api.example.test/v1")
	got, err = tr.Proxy(req)
	if err != nil {
		t.Fatalf("DefaultTransport.Proxy 解析失败: %v", err)
	}
	if want := envFallback(t, req); !sameURL(got, want) {
		t.Fatalf("开关关闭后应回退环境变量代理 %v，实际 %v", want, got)
	}
}

/*
 * TestExplicitOnDecisionTable 开关判据取值表
 *
 * 判据与 service 包的 sysconfigBoolExplicitOn 同源：仅 true/1/yes/on 为开。
 * 未配置一律关闭 —— 全局代理会改变全部出站流向，误开比误关代价大
 */
func TestExplicitOnDecisionTable(t *testing.T) {
	cases := map[string]bool{
		"":        false,
		"false":   false,
		"0":       false,
		"no":      false,
		"off":     false,
		"disable": false,
		"true":    true,
		"1":       true,
		"yes":     true,
		"on":      true,
		" TRUE ":  true,
		"On":      true,
	}
	for in, want := range cases {
		if got := ExplicitOn(in); got != want {
			t.Errorf("ExplicitOn(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

/*
 * TestValidPortDecisionTable 端口值域表
 *
 * 边界值 1 与 65535 均合法，越界与 0 非法
 */
func TestValidPortDecisionTable(t *testing.T) {
	cases := map[int]bool{
		-1:    false,
		0:     false,
		1:     true,
		8080:  true,
		65535: true,
		65536: false,
	}
	for in, want := range cases {
		if got := ValidPort(in); got != want {
			t.Errorf("ValidPort(%d) = %v, 期望 %v", in, got, want)
		}
	}
}

/* assertUserInfo 校验代理 URL 内嵌的凭据 */
func assertUserInfo(t *testing.T, got *url.URL, wantUser, wantPass string) {
	t.Helper()
	if wantUser == "" {
		if got.User != nil {
			t.Fatalf("未配置凭据时不应带用户信息，实际 %v", got.User)
		}
		return
	}
	if got.User == nil {
		t.Fatalf("期望代理凭据 %q，实际无用户信息", wantUser)
	}
	if got.User.Username() != wantUser {
		t.Fatalf("期望用户名 %q，实际 %q", wantUser, got.User.Username())
	}
	pass, _ := got.User.Password()
	if pass != wantPass {
		t.Fatalf("期望密码 %q，实际 %q", wantPass, pass)
	}
}
