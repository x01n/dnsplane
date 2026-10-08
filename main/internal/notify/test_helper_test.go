package notify

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

/*
 * 渠道 HTTP 契约测试的共享辅助。
 * 被测算渠道的出站地址是固定的公网域名（须通过 ValidateOutboundURL 的 SSRF 校验），
 * 故不能直接把域名替换成 httptest 的 127.0.0.1 地址。此处的做法是在 Transport 层
 * 改写目标为主机，保持请求 URL、方法、头与请求体不变，使校验与传输各走各的路径。
 */

/* rewriteTransport 把出站请求重定向到测试服务端，保留原始 Path/Query/Header/Body */
type rewriteTransport struct {
	target *url.URL
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = t.target.Scheme
	cloned.URL.Host = t.target.Host
	cloned.Host = t.target.Host
	return http.DefaultTransport.RoundTrip(cloned)
}

/* newRewriteClient 构造把全部出站请求导向 srv 的 HTTP 客户端 */
func newRewriteClient(t *testing.T, srv *httptest.Server) *http.Client {
	t.Helper()
	u, err := url.Parse(srv.URL)
	if err != nil {
		t.Fatalf("解析测试服务端地址失败: %v", err)
	}
	return &http.Client{
		Transport: &rewriteTransport{target: u},
		Timeout:   30 * time.Second,
	}
}
