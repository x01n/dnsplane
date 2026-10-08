package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * respondEO 返回一个固定 JSON 应答的 EO 测试服务器
 *
 * 与 ESA 不同，EOClient.Request 走 https://<endpoint>/（无路径），
 * 因此必须用 NewTLSServer，否则请求到不了处理器、用例会因网络错误假性通过
 */
func respondEO(body string) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

/* newEOClientForTest 构造指向本地 TLS 测试服务器的 EO 客户端 */
func newEOClientForTest(server *httptest.Server) *EOClient {
	return &EOClient{
		secretID:  "test-id",
		secretKey: "test-key",
		endpoint:  strings.TrimPrefix(server.URL, "https://"),
		version:   "2022-09-01",
		client:    server.Client(),
	}
}

/*
 * TestCreateAccelerationDomainReturnsNoGuessedCname 断言创建接口不再返回本地拼装的 CNAME
 *
 * 旧实现不解析响应、直接返回 "<域名>.eo.dnse5.com"，把猜测值伪装成服务端结果。
 * 新签名只创建不带返回值，调用方必须自己查询真实 CNAME
 */
func TestCreateAccelerationDomainReturnsNoGuessedCname(t *testing.T) {
	server := respondEO(`{"Response":{"RequestId":"r1"}}`)
	defer server.Close()

	client := newEOClientForTest(server)

	if err := client.CreateAccelerationDomain(context.Background(), "zone1", "shop.example.com", "192.0.2.10"); err != nil {
		t.Fatalf("创建成功时不应报错: %v", err)
	}
}

/*
 * TestCreateAccelerationDomainPropagatesError 断言创建失败时错误透传
 */
func TestCreateAccelerationDomainPropagatesError(t *testing.T) {
	server := respondEO(`{"Response":{"Error":{"Code":"InvalidParameter","Message":"参数错误"},"RequestId":"r1"}}`)
	defer server.Close()

	client := newEOClientForTest(server)

	err := client.CreateAccelerationDomain(context.Background(), "zone1", "shop.example.com", "192.0.2.10")
	if err == nil {
		t.Fatal("服务端返回错误时应透传，不能静默成功")
	}
	if !strings.Contains(err.Error(), "InvalidParameter") {
		t.Errorf("错误信息应包含服务端错误码，实际 %q", err.Error())
	}
}

/*
 * TestDescribeAccelerationDomainsReportsMissing 断言查不到加速域名时报错而不是返回空值
 */
func TestDescribeAccelerationDomainsReportsMissing(t *testing.T) {
	server := respondEO(`{"Response":{"AccelerationDomains":[],"RequestId":"r1"}}`)
	defer server.Close()

	client := newEOClientForTest(server)

	cname, status, err := client.DescribeAccelerationDomains(context.Background(), "zone1", "shop.example.com")
	if err == nil {
		t.Fatalf("查不到加速域名时应报错，实际 cname=%q status=%q", cname, status)
	}
	if cname != "" || status != "" {
		t.Errorf("失败时不应返回任何值，实际 cname=%q status=%q", cname, status)
	}
	if !strings.Contains(err.Error(), "shop.example.com") {
		t.Errorf("错误信息应包含域名便于排查，实际 %q", err.Error())
	}
}

/*
 * TestDescribeAccelerationDomainsReturnsRealCname 断言命中时返回服务端给出的 CNAME 与状态
 */
func TestDescribeAccelerationDomainsReturnsRealCname(t *testing.T) {
	server := respondEO(`{"Response":{"AccelerationDomains":[{"Cname":"shop.example.com.eo.example.net","DomainStatus":"online"}],"RequestId":"r1"}}`)
	defer server.Close()

	client := newEOClientForTest(server)

	cname, status, err := client.DescribeAccelerationDomains(context.Background(), "zone1", "shop.example.com")
	if err != nil {
		t.Fatalf("命中时不应报错: %v", err)
	}
	if cname != "shop.example.com.eo.example.net" {
		t.Errorf("应返回服务端给出的真实 CNAME，实际 %q", cname)
	}
	if status != "online" {
		t.Errorf("状态应为 online，实际 %q", status)
	}
}
