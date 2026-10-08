package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

/*
 * newESAClientForTest 构造指向本地 TLS 测试服务器的 ESA 客户端
 *
 * 客户端类型与字段都在本包内，可直接注入 endpoint 与 client。
 * Request 固定以 https 发起，因此必须用 NewTLSServer，否则请求根本到不了
 * 处理器，用例会因为网络错误假性通过而测不到响应解析分支
 */
func newESAClientForTest(server *httptest.Server) *ESAClient {
	return &ESAClient{
		accessKeyID:     "test-key",
		accessKeySecret: "test-secret",
		endpoint:        strings.TrimPrefix(server.URL, "https://"),
		version:         "2024-09-10",
		client:          server.Client(),
	}
}

/* respondESA 返回一个固定 JSON 应答的测试服务器 */
func respondESA(body string) *httptest.Server {
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
}

/*
 * TestGetESARecordCnameReportsNotFound 断言查不到记录时返回错误而不是伪造的默认值
 *
 * 旧实现会在记录不存在时返回 "<域名>.cnamezone.com" 且 err 为 nil，
 * 把「没找到」伪装成「找到了」，调用方据此把猜出来的地址写进 DNS
 */
func TestGetESARecordCnameReportsNotFound(t *testing.T) {
	server := respondESA(`{"RequestId":"r1","Records":[],"TotalCount":0}`)
	defer server.Close()

	client := newESAClientForTest(server)

	cname, err := client.GetESARecordCname(context.Background(), "site1", "shop.example.com")
	if err == nil {
		t.Fatalf("记录不存在时应返回错误，实际 cname=%q", cname)
	}
	if cname != "" {
		t.Errorf("记录不存在时不应返回任何 CNAME，实际 %q", cname)
	}
	if strings.Contains(err.Error(), "http") {
		t.Fatalf("请求未到达测试服务器，用例测不到解析分支：%v", err)
	}
}

/*
 * TestGetESARecordCnameReturnsRealValue 断言命中记录时返回服务端给出的真实 CNAME
 */
func TestGetESARecordCnameReturnsRealValue(t *testing.T) {
	server := respondESA(`{"RequestId":"r1","Records":[{"RecordName":"shop.example.com","RecordCname":"shop.example.com.eo.example.net"}],"TotalCount":1}`)
	defer server.Close()

	client := newESAClientForTest(server)

	cname, err := client.GetESARecordCname(context.Background(), "site1", "shop.example.com")
	if err != nil {
		t.Fatalf("命中记录时不应报错: %v", err)
	}
	if cname != "shop.example.com.eo.example.net" {
		t.Errorf("应返回服务端给出的真实 CNAME，实际 %q", cname)
	}
}

/*
 * TestGetESARecordCnameRejectsMissingCnameField 断言记录存在但缺少 CNAME 字段时报错
 *
 * 记录已存在却没有 RecordCname，属于服务端未给出可用于解析的地址，
 * 同样不能靠本地拼字符串补一个出来
 */
func TestGetESARecordCnameRejectsMissingCnameField(t *testing.T) {
	server := respondESA(`{"RequestId":"r1","Records":[{"RecordName":"shop.example.com"}],"TotalCount":1}`)
	defer server.Close()

	client := newESAClientForTest(server)

	cname, err := client.GetESARecordCname(context.Background(), "site1", "shop.example.com")
	if err == nil {
		t.Fatalf("缺少 RecordCname 时应返回错误，实际 cname=%q", cname)
	}
	if cname != "" {
		t.Errorf("缺少 RecordCname 时不应返回任何 CNAME，实际 %q", cname)
	}
}

/*
 * TestCreateESARecordRejectsMissingCname 断言创建成功但响应无 CNAME 时视为失败
 *
 * 旧的默认值 "<域名>.cnamezone.com" 是本地拼装的猜测值，
 * 写进 DNS 后会指向一个云端未必存在的地址，因此必须报错而非回填
 */
func TestCreateESARecordRejectsMissingCname(t *testing.T) {
	server := respondESA(`{"RequestId":"r1","RecordId":"12345"}`)
	defer server.Close()

	client := newESAClientForTest(server)

	recordID, cname, err := client.CreateESARecord(context.Background(), "site1", "shop.example.com", "CNAME", "origin.example.com")
	if err == nil {
		t.Fatalf("响应缺少 RecordCname 时应返回错误，实际 recordID=%q cname=%q", recordID, cname)
	}
	if cname != "" {
		t.Errorf("失败时不应返回 CNAME，实际 %q", cname)
	}
}

/*
 * TestCreateESARecordReturnsServerCname 断言创建成功时返回服务端给出的 CNAME 与记录 ID
 */
func TestCreateESARecordReturnsServerCname(t *testing.T) {
	server := respondESA(`{"RequestId":"r1","RecordId":"12345","RecordCname":"shop.example.com.eo.example.net"}`)
	defer server.Close()

	client := newESAClientForTest(server)

	recordID, cname, err := client.CreateESARecord(context.Background(), "site1", "shop.example.com", "CNAME", "origin.example.com")
	if err != nil {
		t.Fatalf("创建成功时不应报错: %v", err)
	}
	if recordID != "12345" {
		t.Errorf("记录 ID 应为 12345，实际 %q", recordID)
	}
	if cname != "shop.example.com.eo.example.net" {
		t.Errorf("应返回服务端给出的真实 CNAME，实际 %q", cname)
	}
}
