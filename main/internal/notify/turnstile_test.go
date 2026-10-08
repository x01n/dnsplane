package notify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

/*
 * Cloudflare Turnstile 服务端校验测试
 *
 * 覆盖三分支：私钥未配置、私钥已配置且校验通过、私钥已配置且校验失败。
 * 端点通过 verifyTurnstileAt 注入 httptest 打桩，全程不访问 challenges.cloudflare.com
 */

/* turnstileStub 记录请求体并按要求应答的测试服务 */
type turnstileStub struct {
	calls  int
	form   url.Values
	status int
	body   string
}

func newTurnstileStub(t *testing.T, status int, body string) (*turnstileStub, string) {
	t.Helper()
	stub := &turnstileStub{status: status, body: body}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		stub.calls++
		if err := r.ParseForm(); err != nil {
			t.Errorf("解析表单失败: %v", err)
		}
		stub.form = r.PostForm
		if ct := r.Header.Get("Content-Type"); ct != "application/x-www-form-urlencoded" {
			t.Errorf("Content-Type = %q，期望 application/x-www-form-urlencoded", ct)
		}
		w.WriteHeader(stub.status)
		_, _ = w.Write([]byte(stub.body))
	}))
	t.Cleanup(srv.Close)
	return stub, srv.URL
}

/* TestVerifyTurnstileSuccess 校验通过：请求参数齐全且返回 nil */
func TestVerifyTurnstileSuccess(t *testing.T) {
	stub, url := newTurnstileStub(t, http.StatusOK, `{"success":true,"error-codes":[],"challenge_ts":"2026-10-06T00:00:00Z"}`)

	if err := verifyTurnstileAt(context.Background(), url, "secret-key", "token-abc", "203.0.113.7"); err != nil {
		t.Fatalf("校验应通过，实际返回 %v", err)
	}
	if stub.calls != 1 {
		t.Fatalf("应发生 1 次校验请求，实际 %d 次", stub.calls)
	}
	if got := stub.form.Get("secret"); got != "secret-key" {
		t.Errorf("secret = %q，期望 %q", got, "secret-key")
	}
	if got := stub.form.Get("response"); got != "token-abc" {
		t.Errorf("response = %q，期望 %q", got, "token-abc")
	}
	if got := stub.form.Get("remoteip"); got != "203.0.113.7" {
		t.Errorf("remoteip = %q，期望 %q", got, "203.0.113.7")
	}
}

/* TestVerifyTurnstileFailure 校验失败：success=false 必须返回错误，并带上 Cloudflare 的错误码 */
func TestVerifyTurnstileFailure(t *testing.T) {
	_, url := newTurnstileStub(t, http.StatusOK, `{"success":false,"error-codes":["invalid-input-response"]}`)

	err := verifyTurnstileAt(context.Background(), url, "secret-key", "bad-token", "")
	if err == nil {
		t.Fatal("success=false 时应返回错误，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "invalid-input-response") {
		t.Errorf("错误信息应包含 Cloudflare 错误码，实际 %q", err.Error())
	}
}

/* TestVerifyTurnstileNoErrorCodes success=false 且无错误码时仍须拒绝 */
func TestVerifyTurnstileNoErrorCodes(t *testing.T) {
	_, url := newTurnstileStub(t, http.StatusOK, `{"success":false}`)

	if err := verifyTurnstileAt(context.Background(), url, "secret-key", "bad-token", ""); err == nil {
		t.Fatal("success=false 无错误码时应返回错误，实际返回 nil")
	}
}

/* TestVerifyTurnstileUnconfiguredSecret 未配置私钥时不得放行，避免调用点漏判开关 */
func TestVerifyTurnstileUnconfiguredSecret(t *testing.T) {
	stub, url := newTurnstileStub(t, http.StatusOK, `{"success":true}`)

	if err := verifyTurnstileAt(context.Background(), url, "   ", "token-abc", ""); err == nil {
		t.Fatal("私钥为空时应返回错误，实际返回 nil")
	}
	if stub.calls != 0 {
		t.Errorf("私钥为空时不应发起请求，实际 %d 次", stub.calls)
	}
}

/* TestVerifyTurnstileEmptyToken 已配置私钥但 token 为空时必须拒绝，这是开关「真实生效」的核心断言 */
func TestVerifyTurnstileEmptyToken(t *testing.T) {
	stub, url := newTurnstileStub(t, http.StatusOK, `{"success":true}`)

	if err := verifyTurnstileAt(context.Background(), url, "secret-key", "", ""); err == nil {
		t.Fatal("token 为空时应返回错误，实际返回 nil")
	}
	if stub.calls != 0 {
		t.Errorf("token 为空时不应发起请求，实际 %d 次", stub.calls)
	}
}

/* TestVerifyTurnstileMalformedResponse 响应非 JSON 时必须判定失败，不能误判为通过 */
func TestVerifyTurnstileMalformedResponse(t *testing.T) {
	_, url := newTurnstileStub(t, http.StatusOK, `<html>502 Bad Gateway</html>`)

	if err := verifyTurnstileAt(context.Background(), url, "secret-key", "token", ""); err == nil {
		t.Fatal("响应无法解析时应返回错误，实际返回 nil")
	}
}

/* TestVerifyTurnstileUnreachable 端点不可达时必须判定失败，不得降级放行 */
func TestVerifyTurnstileUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadURL := srv.URL
	srv.Close() // 立即关闭，制造连接失败

	if err := verifyTurnstileAt(context.Background(), deadURL, "secret-key", "token", ""); err == nil {
		t.Fatal("端点不可达时应返回错误，实际返回 nil")
	}
}

/* TestVerifyTurnstileIgnoresExtraResponseFields 响应含额外字段时仍能解析通过 */
func TestVerifyTurnstileIgnoresExtraResponseFields(t *testing.T) {
	payload, _ := json.Marshal(map[string]interface{}{
		"success":      true,
		"error-codes":  []string{},
		"challenge_ts": "2026-10-06T00:00:00Z",
		"hostname":     "example.com",
		"cdata":        "extra",
	})
	_, url := newTurnstileStub(t, http.StatusOK, string(payload))

	if err := verifyTurnstileAt(context.Background(), url, "secret-key", "token", ""); err != nil {
		t.Fatalf("含额外字段的响应应能解析通过，实际返回 %v", err)
	}
}

/* TestVerifyTurnstileTrimsInputs 首尾空白不应影响校验，也不应让空白 token 蒙混过关 */
func TestVerifyTurnstileTrimsInputs(t *testing.T) {
	stub, url := newTurnstileStub(t, http.StatusOK, `{"success":true}`)

	if err := verifyTurnstileAt(context.Background(), url, " secret-key ", " token-abc ", ""); err != nil {
		t.Fatalf("首尾空白不应导致失败，实际返回 %v", err)
	}
	if got := stub.form.Get("secret"); got != "secret-key" {
		t.Errorf("secret 未去除空白: %q", got)
	}
	if got := stub.form.Get("response"); got != "token-abc" {
		t.Errorf("response 未去除空白: %q", got)
	}
}
