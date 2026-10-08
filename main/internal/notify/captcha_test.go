package notify

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

/*
 * 第三方验证码服务端校验测试（reCAPTCHA / hCaptcha，以及共用的 Turnstile）
 *
 * 端点通过 verifyCaptchaAt / captchaProvider 注入 httptest 打桩，全程不访问
 * www.google.com、api.hcaptcha.com 与 challenges.cloudflare.com。
 * 覆盖：成功、失败、无错误码、下划线错误码、响应畸形、端点不可达、
 * 私钥未配置、token 为空、类型不受支持，以及 captcha_type 到端点的分派表。
 */

/* captchaStub 记录请求体并按要求应答的测试服务，三家厂商校验协议一致故共用 */
type captchaStub struct {
	calls  int
	form   url.Values
	status int
	body   string
}

/* newCaptchaStub 启动打桩服务并返回其 URL，服务在用例结束时关闭 */
func newCaptchaStub(t *testing.T, status int, body string) (*captchaStub, string) {
	t.Helper()
	stub := &captchaStub{status: status, body: body}
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

/* TestCaptchaProviderDispatch captcha_type 到端点与厂商名的分派表必须精确 */
func TestCaptchaProviderDispatch(t *testing.T) {
	cases := []struct {
		captchaType string
		wantURL     string
		wantName    string
		wantOK      bool
	}{
		{"recaptcha", "https://www.google.com/recaptcha/api/siteverify", "reCAPTCHA", true},
		{"hcaptcha", "https://api.hcaptcha.com/siteverify", "hCaptcha", true},
		{" RECAPTCHA ", "https://www.google.com/recaptcha/api/siteverify", "reCAPTCHA", true},
		{"HCaptcha", "https://api.hcaptcha.com/siteverify", "hCaptcha", true},
		{"turnstile", "", "", false},
		{"image", "", "", false},
		{"behavioral", "", "", false},
		{"", "", "", false},
		{"unknown", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.captchaType, func(t *testing.T) {
			endpoint, name, ok := captchaProvider(c.captchaType)
			if ok != c.wantOK {
				t.Fatalf("ok = %v，期望 %v", ok, c.wantOK)
			}
			if endpoint != c.wantURL {
				t.Errorf("端点 = %q，期望 %q", endpoint, c.wantURL)
			}
			if name != c.wantName {
				t.Errorf("厂商名 = %q，期望 %q", name, c.wantName)
			}
			if got := IsThirdPartyCaptchaType(c.captchaType); got != c.wantOK {
				t.Errorf("IsThirdPartyCaptchaType = %v，期望 %v", got, c.wantOK)
			}
		})
	}
}

/* TestVerifyCaptchaSuccess 校验通过：请求参数齐全且返回 nil */
func TestVerifyCaptchaSuccess(t *testing.T) {
	stub, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":true,"challenge_ts":"2026-10-06T00:00:00Z","hostname":"example.com"}`)

	if err := verifyCaptchaAt(context.Background(), endpoint, "reCAPTCHA", "secret-key", "token-abc", "203.0.113.7"); err != nil {
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

/* TestVerifyCaptchaFailure 校验失败：success=false 必须返回错误，并带上厂商错误码 */
func TestVerifyCaptchaFailure(t *testing.T) {
	_, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":false,"error-codes":["invalid-input-response"]}`)

	err := verifyCaptchaAt(context.Background(), endpoint, "hCaptcha", "secret-key", "bad-token", "")
	if err == nil {
		t.Fatal("success=false 时应返回错误，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "invalid-input-response") {
		t.Errorf("错误信息应包含厂商错误码，实际 %q", err.Error())
	}
}

/* TestVerifyCaptchaNoErrorCodes success=false 且无错误码时仍须拒绝 */
func TestVerifyCaptchaNoErrorCodes(t *testing.T) {
	_, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":false}`)

	if err := verifyCaptchaAt(context.Background(), endpoint, "reCAPTCHA", "secret-key", "bad-token", ""); err == nil {
		t.Fatal("success=false 无错误码时应返回错误，实际返回 nil")
	}
}

/* TestVerifyCaptchaUnderscoreErrorCodes hCaptcha 的下划线错误码键同样要带出 */
func TestVerifyCaptchaUnderscoreErrorCodes(t *testing.T) {
	_, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":false,"error_codes":["invalid-input-response"]}`)

	err := verifyCaptchaAt(context.Background(), endpoint, "hCaptcha", "secret-key", "bad-token", "")
	if err == nil {
		t.Fatal("success=false 时应返回错误，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "invalid-input-response") {
		t.Errorf("下划线形式的错误码也应带出，实际 %q", err.Error())
	}
}

/* TestVerifyCaptchaMalformedResponse 响应非 JSON 时必须判定失败，不能误判为通过 */
func TestVerifyCaptchaMalformedResponse(t *testing.T) {
	_, endpoint := newCaptchaStub(t, http.StatusOK, `<html>502 Bad Gateway</html>`)

	if err := verifyCaptchaAt(context.Background(), endpoint, "reCAPTCHA", "secret-key", "token", ""); err == nil {
		t.Fatal("响应无法解析时应返回错误，实际返回 nil")
	}
}

/* TestVerifyCaptchaUnreachable 端点不可达时必须判定失败，不得降级放行 */
func TestVerifyCaptchaUnreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	deadEndpoint := srv.URL
	srv.Close() // 立即关闭，制造连接失败

	if err := verifyCaptchaAt(context.Background(), deadEndpoint, "reCAPTCHA", "secret-key", "token", ""); err == nil {
		t.Fatal("端点不可达时应返回错误，实际返回 nil")
	}
}

/* TestVerifyCaptchaUnconfiguredSecret 未配置私钥时不得放行，且错误文案要指明是哪一家 */
func TestVerifyCaptchaUnconfiguredSecret(t *testing.T) {
	stub, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":true}`)

	err := verifyCaptchaAt(context.Background(), endpoint, "hCaptcha", "   ", "token-abc", "")
	if err == nil {
		t.Fatal("私钥为空时应返回错误，实际返回 nil")
	}
	if !strings.Contains(err.Error(), "hCaptcha") {
		t.Errorf("错误信息应指明厂商，实际 %q", err.Error())
	}
	if stub.calls != 0 {
		t.Errorf("私钥为空时不应发起请求，实际 %d 次", stub.calls)
	}
}

/* TestVerifyCaptchaEmptyToken 已配置私钥但 token 为空时必须拒绝，这是开关「真实生效」的核心断言 */
func TestVerifyCaptchaEmptyToken(t *testing.T) {
	stub, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":true}`)

	if err := verifyCaptchaAt(context.Background(), endpoint, "reCAPTCHA", "secret-key", "", ""); err == nil {
		t.Fatal("token 为空时应返回错误，实际返回 nil")
	}
	if stub.calls != 0 {
		t.Errorf("token 为空时不应发起请求，实际 %d 次", stub.calls)
	}
}

/* TestVerifyCaptchaUnsupportedType 类型不受支持时必须报错，不能静默当作通过 */
func TestVerifyCaptchaUnsupportedType(t *testing.T) {
	for _, captchaType := range []string{"turnstile", "image", "behavioral", ""} {
		t.Run(captchaType, func(t *testing.T) {
			if err := VerifyCaptcha(context.Background(), captchaType, "secret-key", "token", ""); err == nil {
				t.Fatal("类型不受支持时应返回错误，实际返回 nil")
			}
		})
	}
}

/*
 * TestTurnstileErrorTextUnchanged Turnstile 迁移到共用实现后错误文案必须逐字不变
 *
 * VerifyTurnstile 是既有对外行为，重构只应消除重复，不应改动用户可见文案
 */
func TestTurnstileErrorTextUnchanged(t *testing.T) {
	stub, endpoint := newCaptchaStub(t, http.StatusOK, `{"success":true}`)

	err := verifyTurnstileAt(context.Background(), endpoint, "  ", "token", "")
	if err == nil {
		t.Fatal("私钥为空时应返回错误，实际返回 nil")
	}
	if err.Error() != "未配置 Turnstile 私有密钥" {
		t.Errorf("错误文案 = %q，期望 %q", err.Error(), "未配置 Turnstile 私有密钥")
	}
	if stub.calls != 0 {
		t.Errorf("私钥为空时不应发起请求，实际 %d 次", stub.calls)
	}
}
