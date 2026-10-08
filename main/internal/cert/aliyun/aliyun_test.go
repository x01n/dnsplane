package aliyun

import (
	"context"
	"encoding/json"
	"main/internal/cert"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const (
	testAK     = "TESTAK"
	testSecret = "TESTSECRET"
)

func TestCanonicalQuery(t *testing.T) {
	params := map[string]string{
		"Action":           "UpdateInstance",
		"InstanceId":       "abc-123",
		"Domain":           "example.com",
		"KeyAlgorithm":     "RSA_2048",
		"AutoReissue":      "disable",
		"ContactIdList.1":  "99",
		"ValidateType":     "DNS",
		"Format":           "JSON",
		"Version":          "2020-04-07",
		"AccessKeyId":      testAK,
		"SignatureMethod":  "HMAC-SHA1",
		"Timestamp":        "2026-10-05T00:00:00Z",
		"SignatureVersion": "1.0",
		"SignatureNonce":   "abcd1234",
	}
	want := "AccessKeyId=TESTAK&Action=UpdateInstance&AutoReissue=disable&ContactIdList.1=99" +
		"&Domain=example.com&Format=JSON&InstanceId=abc-123&KeyAlgorithm=RSA_2048" +
		"&SignatureMethod=HMAC-SHA1&SignatureNonce=abcd1234&SignatureVersion=1.0" +
		"&Timestamp=2026-10-05T00%3A00%3A00Z&ValidateType=DNS&Version=2020-04-07"
	if got := canonicalQuery(params); got != want {
		t.Fatalf("规范查询串不一致\n got=%s\nwant=%s", got, want)
	}
}

func TestPercentEncode(t *testing.T) {
	got := percentEncode("测试 * ~ + & /")
	want := "%E6%B5%8B%E8%AF%95%20%2A%20~%20%2B%20%26%20%2F"
	if got != want {
		t.Fatalf("转义不一致\n got=%s\nwant=%s", got, want)
	}
}

/* newTestProvider 返回指向本地测试服务端的 Provider */
func newTestProvider(t *testing.T, handler http.HandlerFunc) (*Provider, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	p := &Provider{
		accessKeyID:     testAK,
		accessKeySecret: testSecret,
		client:          srv.Client(),
		apiURL:          srv.URL + "/",
	}
	return p, srv
}

/* parseForm 解析 POST 表单体 */
func parseForm(t *testing.T, r *http.Request) map[string]string {
	t.Helper()
	if err := r.ParseForm(); err != nil {
		t.Fatalf("解析表单失败: %v", err)
	}
	out := make(map[string]string, len(r.PostForm))
	for k := range r.PostForm {
		out[k] = r.PostForm.Get(k)
	}
	return out
}

/* submitSignature 复算签名，验证服务端收到的参数与 Signature 自洽 */
func submitSignature(t *testing.T, form map[string]string) string {
	t.Helper()
	sig := form["Signature"]
	delete(form, "Signature")
	if want := signedFormSignature(form, testSecret); want != sig {
		t.Fatalf("签名与提交参数不匹配\n got=%s\nwant=%s", sig, want)
	}
	return sig
}

func TestRequestSendsSignedForm(t *testing.T) {
	var form map[string]string
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		form = parseForm(t, r)
		_, _ = w.Write([]byte(`{"RequestId":"req-1"}`))
	})
	defer srv.Close()

	if _, err := p.request(context.Background(), map[string]string{
		"Action":     "UpdateInstance",
		"InstanceId": "abc-123",
	}, true); err != nil {
		t.Fatalf("请求失败: %v", err)
	}

	wantStatic := map[string]string{
		"Action":           "UpdateInstance",
		"InstanceId":       "abc-123",
		"Format":           "JSON",
		"Version":          defaultVersion,
		"AccessKeyId":      testAK,
		"SignatureMethod":  "HMAC-SHA1",
		"SignatureVersion": "1.0",
	}
	for k, v := range wantStatic {
		if form[k] != v {
			t.Fatalf("字段 %s 不符: got=%q want=%q", k, form[k], v)
		}
	}
	if len(form["SignatureNonce"]) != 8 {
		t.Fatalf("SignatureNonce 长度应为 8: %q", form["SignatureNonce"])
	}
	submitSignature(t, form)
}

func TestRequestSurfacesAPIMessage(t *testing.T) {
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"Message": "InvalidAccessKeyId.NotFound"})
	})
	defer srv.Close()

	_, err := p.request(context.Background(), map[string]string{"Action": "ListInstances"}, true)
	if err == nil || err.Error() != "InvalidAccessKeyId.NotFound" {
		t.Fatalf("未透出接口返回的 Message: %v", err)
	}
}

func TestRequestReturnsNilWhenReturnDataFalse(t *testing.T) {
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"RequestId":"req-1"}`))
	})
	defer srv.Close()

	data, err := p.request(context.Background(), map[string]string{"Action": "ApplyCertificate"}, false)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	if data != nil {
		t.Fatalf("returnData=false 时应返回 nil: %#v", data)
	}
}

func TestRegisterRequiresCredentials(t *testing.T) {
	p := &Provider{}
	if _, err := p.Register(context.Background()); err == nil {
		t.Fatal("缺少 AccessKey 时应报错")
	}
}

func TestCreateOrderFlowAgainstFakeAPI(t *testing.T) {
	var actions []string
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		form := parseForm(t, r)
		submitSignature(t, form)
		action := form["Action"]
		actions = append(actions, action)

		switch action {
		case "ListInstances":
			_, _ = w.Write([]byte(`{"InstanceList":[{"InstanceId":"inst-1"}]}`))
		case "ListContact":
			_, _ = w.Write([]byte(`{"ContactList":[{"ContactId":"77"}]}`))
		case "UpdateInstance", "ApplyCertificate":
			_, _ = w.Write([]byte(`{"RequestId":"ok"}`))
		case "GetTaskAttribute":
			_, _ = w.Write([]byte(`{"TaskStatus":"success"}`))
		case "GetInstanceDetail":
			_, _ = w.Write([]byte(`{"DomainValidationList":[{"Domain":"example.com","ValidationKey":"_dnsauth","RootDomain":"example.com","ValidationType":"TXT","ValidationValue":"v1"}]}`))
		default:
			t.Errorf("未预期的 Action: %s", action)
		}
	})
	defer srv.Close()

	order := &cert.OrderInfo{}
	records, err := p.CreateOrder(context.Background(), []string{"example.com"}, order, "RSA", "2048")
	if err != nil {
		t.Fatalf("创建订单失败: %v", err)
	}
	if order.OrderURL != "inst-1" {
		t.Fatalf("OrderURL 应为实例 ID: %q", order.OrderURL)
	}
	got := records["example.com"]
	if len(got) != 1 || got[0].Name != "_dnsauth" || got[0].Type != "TXT" || got[0].Value != "v1" {
		t.Fatalf("验证记录不符: %#v", got)
	}

	wantActions := []string{"ListInstances", "ListContact", "UpdateInstance", "ApplyCertificate", "GetTaskAttribute", "GetInstanceDetail"}
	if strings.Join(actions, ",") != strings.Join(wantActions, ",") {
		t.Fatalf("调用顺序不符: %v", actions)
	}
}

func TestCreateOrderSurfacesTaskFailure(t *testing.T) {
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		form := parseForm(t, r)
		switch form["Action"] {
		case "ListInstances":
			_, _ = w.Write([]byte(`{"InstanceList":[{"InstanceId":"inst-1"}]}`))
		case "ListContact":
			_, _ = w.Write([]byte(`{"ContactList":[{"ContactId":"77"}]}`))
		case "GetTaskAttribute":
			_, _ = w.Write([]byte(`{"TaskStatus":"failed","TaskMessage":"domain not allowed"}`))
		default:
			_, _ = w.Write([]byte(`{"RequestId":"ok"}`))
		}
	})
	defer srv.Close()

	_, err := p.CreateOrder(context.Background(), []string{"example.com"}, &cert.OrderInfo{}, "RSA", "2048")
	if err == nil || !strings.Contains(err.Error(), "domain not allowed") {
		t.Fatalf("未透出任务失败原因: %v", err)
	}
}

func TestGetAuthStatus(t *testing.T) {
	status := "normal"
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"Status": status})
	})
	defer srv.Close()

	ok, err := p.GetAuthStatus(context.Background(), nil, &cert.OrderInfo{OrderURL: "inst-1"})
	if err != nil || !ok {
		t.Fatalf("normal 应为验证通过: ok=%v err=%v", ok, err)
	}

	status = "closed"
	if _, err := p.GetAuthStatus(context.Background(), nil, &cert.OrderInfo{OrderURL: "inst-1"}); err == nil {
		t.Fatal("closed 应为审核失败")
	}

	status = "pending"
	ok, err = p.GetAuthStatus(context.Background(), nil, &cert.OrderInfo{OrderURL: "inst-1"})
	if err != nil || ok {
		t.Fatalf("pending 应继续等待: ok=%v err=%v", ok, err)
	}
}

func TestBuyCertRequiresInactiveInstance(t *testing.T) {
	p, srv := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"TotalCount":0,"InactiveCount":0}`))
	})
	defer srv.Close()

	if err := p.BuyCert(context.Background(), nil, &cert.OrderInfo{}); err == nil {
		t.Fatal("无待使用实例时应报错")
	}
}

func TestBuildDNSRecordsTrimsMainDomainSuffix(t *testing.T) {
	detail := map[string]interface{}{
		"DomainValidationList": []interface{}{
			map[string]interface{}{
				"Domain":          "example.com",
				"ValidationKey":   "_dnsauth",
				"RootDomain":      "example.com",
				"ValidationType":  "TXT",
				"ValidationValue": "v1",
			},
		},
	}
	got := buildDNSRecords(detail)["example.com"]
	if len(got) != 1 || got[0].Name != "_dnsauth" {
		t.Fatalf("记录名未裁掉 RootDomain 后缀：%#v", got)
	}
	if empty := buildDNSRecords(map[string]interface{}{}); len(empty) != 0 {
		t.Fatalf("无验证列表时应返回空映射: %#v", empty)
	}
}
