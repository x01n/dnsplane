package httpx

import (
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"main/internal/logger"
	"main/internal/sysconfig"
)

func ExplicitOn(v string) bool {
	return sysconfig.ExplicitOn(v)
}

/* Enabled 报告数据库中的全局代理开关是否开启 */
func Enabled() bool {
	return ExplicitOn(sysconfig.GetValue("proxy_enabled"))
}

/* ValidPort 代理端口值域：1..65535，其余（含 0 与未配置）一律无效 */
func ValidPort(port int) bool {
	return port >= 1 && port <= 65535
}

/*
 * Install 把 DB 全局代理接入 http.DefaultTransport.Proxy
 *
 * 必须在 cache.Init 之后调用：proxyForRequest 经 sysconfig 读缓存。
 * 不用 init() —— go test 进程同样执行 init 但不跑 main，测试中被替换过的
 * DefaultTransport 会被这里的赋值覆盖，且 init 期 cache.C 尚未就绪会 panic
 */
func Install() {
	if tr, ok := http.DefaultTransport.(*http.Transport); ok {
		tr.Proxy = proxyForRequest
	}
}

/* proxyForRequest 解析本次请求应使用的代理；配置缺失或非法时回退环境变量 */
func proxyForRequest(req *http.Request) (*url.URL, error) {
	if !Enabled() {
		return http.ProxyFromEnvironment(req)
	}

	host := strings.TrimSpace(sysconfig.GetValue("proxy_server"))
	port, err := strconv.Atoi(strings.TrimSpace(sysconfig.GetValue("proxy_port")))
	if !validProxyHost(host) || err != nil || !ValidPort(port) {
		return http.ProxyFromEnvironment(req)
	}

	// 标准库 transport 只按目标 scheme 分支，不看代理 URL 的 scheme，
	// 故 proxy_type 无需为 https 单列分支；未知值按 http 处理并留痕
	kind := strings.ToLower(strings.TrimSpace(sysconfig.GetValue("proxy_type")))
	scheme := "http"
	switch kind {
	case "", "http":
	case "socks5":
		scheme = "socks5"
	default:
		logger.Warn("系统配置 proxy_type=%q 不在支持范围内，按 http 处理（支持 http / socks5）", kind)
	}

	u := &url.URL{Scheme: scheme, Host: net.JoinHostPort(host, strconv.Itoa(port))}
	if user := sysconfig.GetValue("proxy_user"); user != "" {
		u.User = url.UserPassword(user, sysconfig.GetValue("proxy_password"))
	}
	return u, nil
}

/* validProxyHost 代理主机名合法性：非空，且不含空格、下划线、换行
 * 这些字符会让代理 URL 无法被正确解析，此时回退环境变量而不是 panic */
func validProxyHost(host string) bool {
	return host != "" && !strings.ContainsAny(host, " _\r\n")
}
