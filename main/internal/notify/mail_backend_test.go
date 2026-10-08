package notify

/*
 * 邮件 API 后端的 HTTP 契约测试
 * 用本地 httptest 服务端接收请求，校验 URL、请求体字段与响应判定分支，
 * 不使用任何外部网络
 */

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestSendcloudNotifierPostsExpectedFields(t *testing.T) {
	var gotPath string
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = r.ParseForm()
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":true,"statusCode":200,"message":"success"}`))
	}))
	defer srv.Close()

	n := NewSendcloudNotifier(SendcloudConfig{
		APIUser: "apiuser", APIKey: "apikey",
		From: "noreply@example.com", FromName: "DNSPlane",
		To: "a@example.com, b@example.com",
	})
	n.apiURL = srv.URL + "/apiv2/mail/send"

	if err := n.Send(context.Background(), "主题", "<b>正文</b>"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	if gotPath != "/apiv2/mail/send" {
		t.Fatalf("请求路径不符: %s", gotPath)
	}
	want := map[string]string{
		"apiUser":  "apiuser",
		"apiKey":   "apikey",
		"from":     "noreply@example.com",
		"fromName": "DNSPlane",
		"to":       "a@example.com,b@example.com",
		"subject":  "主题",
		"html":     "<b>正文</b>",
	}
	for k, v := range want {
		if gotForm.Get(k) != v {
			t.Fatalf("字段 %s 不符: got=%q want=%q", k, gotForm.Get(k), v)
		}
	}
}

func TestSendcloudNotifierSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"result":false,"statusCode":400,"message":["apiUser 不合法"]}`))
	}))
	defer srv.Close()

	n := NewSendcloudNotifier(SendcloudConfig{
		APIUser: "u", APIKey: "k", From: "f@example.com", To: "t@example.com",
	})
	n.apiURL = srv.URL

	err := n.Send(context.Background(), "s", "c")
	if err == nil {
		t.Fatal("statusCode 非 200 时应返回错误")
	}
	if !strings.Contains(err.Error(), "apiUser 不合法") {
		t.Fatalf("错误信息未透出 message 字段: %v", err)
	}
}

func TestSendcloudNotifierRequiresCredentials(t *testing.T) {
	n := NewSendcloudNotifier(SendcloudConfig{From: "f@example.com", To: "t@example.com"})
	if err := n.Send(context.Background(), "s", "c"); err == nil {
		t.Fatal("缺少 apiUser/apiKey 时应返回错误")
	}
}

func TestSendcloudNotifierRejectsHeaderInjection(t *testing.T) {
	n := NewSendcloudNotifier(SendcloudConfig{
		APIUser: "u", APIKey: "k", From: "f@example.com", To: "t@example.com",
	})
	if err := n.Send(context.Background(), "bad\r\nBcc: x@example.com", "c"); err == nil {
		t.Fatal("主题含 CRLF 时应拒绝发送")
	}
}

func TestAliyunDirectMailNotifierPostsSignedForm(t *testing.T) {
	var gotForm url.Values
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotForm = r.PostForm
		_, _ = w.Write([]byte(`{"RequestId":"req-1","EnvId":"env-1"}`))
	}))
	defer srv.Close()

	n := NewAliyunDirectMailNotifier(AliyunDirectMailConfig{
		AccessKeyID: "TESTAK", AccessKeySecret: "TESTSECRET",
		From: "noreply@example.com", FromName: "DNSPlane",
		To: "a@example.com,b@example.com",
	})
	n.apiURL = srv.URL + "/"
	// httptest 默认是 http，改走明文以便本地校验
	n.client = srv.Client()

	if err := n.Send(context.Background(), "主题", "<b>正文</b>"); err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	want := map[string]string{
		"Action":           "SingleSendMail",
		"AccountName":      "noreply@example.com",
		"ReplyToAddress":   "false",
		"AddressType":      "1",
		"ToAddress":        "a@example.com,b@example.com",
		"FromAlias":        "DNSPlane",
		"Subject":          "主题",
		"HtmlBody":         "<b>正文</b>",
		"Format":           "JSON",
		"Version":          aliyunDirectMailVersion,
		"AccessKeyId":      "TESTAK",
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
	}
	for k, v := range want {
		if gotForm.Get(k) != v {
			t.Fatalf("字段 %s 不符: got=%q want=%q", k, gotForm.Get(k), v)
		}
	}
	if gotForm.Get("Signature") == "" {
		t.Fatal("请求缺少 Signature")
	}
	if gotForm.Get("Timestamp") == "" || gotForm.Get("SignatureNonce") == "" {
		t.Fatal("请求缺少 Timestamp 或 SignatureNonce")
	}

	// 服务端按收到的参数（去掉 Signature）重算签名，验证与实际提交一致
	received := map[string]string{}
	for k := range gotForm {
		if k == "Signature" {
			continue
		}
		received[k] = gotForm.Get(k)
	}
	if wantSig := aliyunRPCSignature(received, "TESTSECRET", "POST"); wantSig != gotForm.Get("Signature") {
		t.Fatalf("签名与提交参数不匹配: got=%s want=%s", gotForm.Get("Signature"), wantSig)
	}
}

func TestAliyunDirectMailNotifierSurfacesAPIError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"Message": "The specified account is invalid"})
	}))
	defer srv.Close()

	n := NewAliyunDirectMailNotifier(AliyunDirectMailConfig{
		AccessKeyID: "ak", AccessKeySecret: "sk", From: "f@example.com", To: "t@example.com",
	})
	n.apiURL = srv.URL + "/"
	n.client = srv.Client()

	err := n.Send(context.Background(), "s", "c")
	if err == nil || !strings.Contains(err.Error(), "The specified account is invalid") {
		t.Fatalf("未透出 DirectMail 错误信息: %v", err)
	}
}
