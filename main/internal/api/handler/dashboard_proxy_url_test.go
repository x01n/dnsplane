package handler

import (
	"net/url"
	"strings"
	"testing"
)

/*
 * 代理 URL 构造测试
 *
 * IPv6 字面量不加方括号时 net/url 会误判：socks5 分支把 "::1:1080" 整段当作 host,
 * 静默拨向错误地址；http 分支直接报 invalid port。两者都只在 IPv6 输入下出现，
 * 而 M-6 放宽 IP 字面量拦截之前，回环 IPv6 走不到这段构造逻辑
 *
 * 断言一律走 url.Parse 的真实解析结果，不比对拼接出来的原始字符串
 */

/* TestBuildProxyURLIPv6Literal IPv6 字面量补方括号后 host 与 port 分界正确 */
func TestBuildProxyURLIPv6Literal(t *testing.T) {
	cases := []struct {
		name         string
		kind         string
		host         string
		wantHost     string
		wantHostname string
	}{
		{"回环地址 socks5", "socks5", "::1", "[::1]:1080", "::1"},
		{"回环地址 http", "http", "::1", "[::1]:1080", "::1"},
		{"IPv4 映射地址 socks5", "socks5", "::ffff:127.0.0.1", "[::ffff:127.0.0.1]:1080", "::ffff:127.0.0.1"},
		{"IPv4 映射地址 http", "http", "::ffff:127.0.0.1", "[::ffff:127.0.0.1]:1080", "::ffff:127.0.0.1"},
		{"文档用 IPv6 地址 http", "http", "2001:db8::1", "[2001:db8::1]:1080", "2001:db8::1"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildProxyURL(tc.kind, tc.host, "", "", 1080)
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("host=%q 应能解析，实际报错: %v（原文：%s）", tc.host, err, raw)
			}
			if u.Scheme != tc.kind {
				t.Fatalf("scheme 应为 %q，实际 %q", tc.kind, u.Scheme)
			}
			if u.Host != tc.wantHost {
				t.Fatalf("host=%q 的 u.Host 应为 %q，实际 %q（原文：%s）", tc.host, tc.wantHost, u.Host, raw)
			}
			if u.Port() != "1080" {
				t.Fatalf("host=%q 的 u.Port() 应为 1080，实际 %q（原文：%s）", tc.host, u.Port(), raw)
			}
			if u.Hostname() != tc.wantHostname {
				t.Fatalf("host=%q 的 u.Hostname() 应为 %q，实际 %q（原文：%s）", tc.host, tc.wantHostname, u.Hostname(), raw)
			}
			if u.User != nil {
				t.Fatalf("无凭据时 u.User 应为空，实际 %v（原文：%s）", u.User, raw)
			}
		})
	}
}

/* TestBuildProxyURLNonIPv6Unchanged IPv4 字面量与域名的构造结果不受修复影响 */
func TestBuildProxyURLNonIPv6Unchanged(t *testing.T) {
	cases := []struct {
		name         string
		kind         string
		host         string
		wantHost     string
		wantHostname string
	}{
		{"IPv4 字面量 http", "http", "127.0.0.1", "127.0.0.1:1080", "127.0.0.1"},
		{"IPv4 字面量 socks5", "socks5", "127.0.0.1", "127.0.0.1:1080", "127.0.0.1"},
		{"域名 http", "http", "example.com", "example.com:1080", "example.com"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildProxyURL(tc.kind, tc.host, "", "", 1080)
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("host=%q 应能解析，实际报错: %v（原文：%s）", tc.host, err, raw)
			}
			if u.Host != tc.wantHost {
				t.Fatalf("host=%q 的 u.Host 应为 %q，实际 %q（原文：%s）", tc.host, tc.wantHost, u.Host, raw)
			}
			if u.Port() != "1080" {
				t.Fatalf("host=%q 的 u.Port() 应为 1080，实际 %q（原文：%s）", tc.host, u.Port(), raw)
			}
			if u.Hostname() != tc.wantHostname {
				t.Fatalf("host=%q 的 u.Hostname() 应为 %q，实际 %q（原文：%s）", tc.host, tc.wantHostname, u.Hostname(), raw)
			}
		})
	}
}

/* TestBuildProxyURLBracketedHostNotWrapped 已带方括号的 IPv6 字面量不重复补括号 */
func TestBuildProxyURLBracketedHostNotWrapped(t *testing.T) {
	raw := buildProxyURL("http", "[::1]", "", "", 1080)
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("已带方括号的 host 应能解析，实际报错: %v（原文：%s）", err, raw)
	}
	if u.Host != "[::1]:1080" {
		t.Fatalf("u.Host 应为 %q，实际 %q（原文：%s）", "[::1]:1080", u.Host, raw)
	}
	if u.Port() != "1080" {
		t.Fatalf("u.Port() 应为 1080，实际 %q（原文：%s）", u.Port(), raw)
	}
	if n := strings.Count(raw, "["); n != 1 {
		t.Fatalf("方括号左括号应只出现 1 次，实际 %d 次（原文：%s）", n, raw)
	}
	if n := strings.Count(raw, "]"); n != 1 {
		t.Fatalf("方括号右括号应只出现 1 次，实际 %d 次（原文：%s）", n, raw)
	}
}

/* TestBuildProxyURLWithCredentials 有凭据分支与无凭据分支共用同一套 host 处理 */
func TestBuildProxyURLWithCredentials(t *testing.T) {
	cases := []struct {
		name     string
		kind     string
		host     string
		wantHost string
	}{
		{"IPv6 字面量带凭据", "socks5", "::1", "[::1]:1080"},
		{"IPv4 字面量带凭据", "http", "127.0.0.1", "127.0.0.1:1080"},
		{"域名带凭据", "http", "example.com", "example.com:1080"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw := buildProxyURL(tc.kind, tc.host, "alice", "p@ss:w/ord", 1080)
			u, err := url.Parse(raw)
			if err != nil {
				t.Fatalf("host=%q 应能解析，实际报错: %v（原文：%s）", tc.host, err, raw)
			}
			if u.Host != tc.wantHost {
				t.Fatalf("host=%q 的 u.Host 应为 %q，实际 %q（原文：%s）", tc.host, tc.wantHost, u.Host, raw)
			}
			if u.Port() != "1080" {
				t.Fatalf("host=%q 的 u.Port() 应为 1080，实际 %q（原文：%s）", tc.host, u.Port(), raw)
			}
			if u.User == nil {
				t.Fatalf("带凭据时 u.User 不应为空（原文：%s）", raw)
			}
			if u.User.Username() != "alice" {
				t.Fatalf("用户名应为 alice，实际 %q（原文：%s）", u.User.Username(), raw)
			}
			pass, ok := u.User.Password()
			if !ok {
				t.Fatalf("密码应存在（原文：%s）", raw)
			}
			if pass != "p@ss:w/ord" {
				t.Fatalf("密码应为 %q，实际 %q（原文：%s）", "p@ss:w/ord", pass, raw)
			}
		})
	}
}
