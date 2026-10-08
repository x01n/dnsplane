package handler

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"main/internal/cache"
)

/*
 * 第三方验证码守卫测试（reCAPTCHA / hCaptcha）
 *
 * requireCaptcha 经 notify.VerifyCaptcha 出站，端点由 captcha_type 决定，
 * 生产代码里没有可注入的测试钩子。此处替换 http.DefaultTransport 拦截出站请求，
 * 既不打真实厂商端点，又能断言「请求究竟发往哪一家」与「有没有发请求」——
 * 后者是「未配置即放行」「token 缺失即拒绝」两条语义的直接证据
 */

/* stubRoundTripper 拦截出站请求，记录 URL 与表单并返回预置响应 */
type stubRoundTripper struct {
	urls   []string
	forms  []url.Values
	status int
	body   string
	fail   bool
}

/* RoundTrip 实现 http.RoundTripper，不产生任何真实网络流量 */
func (s *stubRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	s.urls = append(s.urls, req.URL.String())
	if req.Body != nil {
		raw, err := io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		form, err := url.ParseQuery(string(raw))
		if err != nil {
			return nil, err
		}
		s.forms = append(s.forms, form)
	}
	if s.fail {
		return nil, errors.New("stub transport 连接失败")
	}
	return &http.Response{
		StatusCode: s.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(s.body)),
		Request:    req,
	}, nil
}

/* stubCaptchaUpstream 用打桩传输替换全局 DefaultTransport，用例结束自动还原 */
func stubCaptchaUpstream(t *testing.T, status int, body string) *stubRoundTripper {
	t.Helper()
	stub := &stubRoundTripper{status: status, body: body}
	original := http.DefaultTransport
	http.DefaultTransport = stub
	t.Cleanup(func() { http.DefaultTransport = original })
	return stub
}

/*
 * TestRequireCaptchaDispatchByType captcha_type 决定「打给谁」与「要不要打」
 *
 * 不仅断言分派函数，而是断言真实出站请求的 URL——
 * 分派表与出站行为脱节时（例如传了 URL 却用错变量），只有后者能发现
 */
func TestRequireCaptchaDispatchByType(t *testing.T) {
	cases := []struct {
		name        string
		captchaType string
		secret      string
		token       string
		wantURL     string
	}{
		{"recaptcha 打 Google 端点", "recaptcha", "sec", "tk", "https://www.google.com/recaptcha/api/siteverify"},
		{"hcaptcha 打 hCaptcha 端点", "hcaptcha", "sec", "tk", "https://api.hcaptcha.com/siteverify"},
		{"大写与空白仍可识别", " HCaptcha ", "sec", "tk", "https://api.hcaptcha.com/siteverify"},
		{"图形验证码不校验", "image", "sec", "tk", ""},
		{"行为验证码不校验", "behavioral", "sec", "tk", ""},
		{"turnstile 不走此入口", "turnstile", "sec", "tk", ""},
		{"类型未配置不校验", "", "sec", "tk", ""},
		{"未知类型不校验", "geetest", "sec", "tk", ""},
		{"私钥未配置放行", "recaptcha", "", "tk", ""},
		{"私钥为空白放行", "recaptcha", "   ", "tk", ""},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, map[string]string{
				"captcha_type":       c.captchaType,
				"captcha_secret_key": c.secret,
			})
			stub := stubCaptchaUpstream(t, http.StatusOK, `{"success":true}`)

			err := requireCaptcha(newCaptchaContext(t), c.token)
			if err != nil {
				t.Fatalf("应通过校验，实际返回 %v", err)
			}
			if c.wantURL == "" {
				if len(stub.urls) != 0 {
					t.Fatalf("不应发起任何校验请求，实际 %v", stub.urls)
				}
				return
			}
			if len(stub.urls) != 1 {
				t.Fatalf("应发起 1 次校验请求，实际 %v", stub.urls)
			}
			if stub.urls[0] != c.wantURL {
				t.Errorf("校验端点 = %q，期望 %q", stub.urls[0], c.wantURL)
			}
			if got := stub.forms[0].Get("secret"); got != strings.TrimSpace(c.secret) {
				t.Errorf("secret = %q，期望 %q", got, strings.TrimSpace(c.secret))
			}
			if got := stub.forms[0].Get("response"); got != c.token {
				t.Errorf("response = %q，期望 %q", got, c.token)
			}
		})
	}
}

/*
 * TestRequireCaptchaRejectBranches 已配置私钥时的失败路径必须一律拒绝
 *
 * 反例覆盖：上游 success=false、上游返回非 JSON、上游不可达、token 缺失。
 * 其中 token 缺失不应发起请求——既省一次出站，也避免用无效 token 消耗上游配额
 */
func TestRequireCaptchaRejectBranches(t *testing.T) {
	cases := []struct {
		name        string
		token       string
		status      int
		body        string
		fail        bool
		wantURLs    int
		wantErrText string
	}{
		{"上游判定失败", "tk", http.StatusOK, `{"success":false,"error-codes":["invalid-input-response"]}`, false, 1, "invalid-input-response"},
		{"上游返回非 JSON", "tk", http.StatusOK, `<html>502</html>`, false, 1, "解析失败"},
		{"上游不可达", "tk", 0, "", true, 1, "不可达"},
		{"token 缺失", "", http.StatusOK, `{"success":true}`, false, 0, "人机验证"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, map[string]string{
				"captcha_type":       "recaptcha",
				"captcha_secret_key": "sec",
			})
			stub := stubCaptchaUpstream(t, c.status, c.body)
			stub.fail = c.fail

			err := requireCaptcha(newCaptchaContext(t), c.token)
			if err == nil {
				t.Fatal("应拒绝，实际放行")
			}
			if !strings.Contains(err.Error(), c.wantErrText) {
				t.Errorf("错误信息应包含 %q，实际 %q", c.wantErrText, err.Error())
			}
			if len(stub.urls) != c.wantURLs {
				t.Errorf("出站请求数 = %d，期望 %d（%v）", len(stub.urls), c.wantURLs, stub.urls)
			}
		})
	}
}

/*
 * TestCaptchaTokenFieldNames 前端契约：三个字段名任一非空都算携带了 token
 *
 * captcha_token 是后端统一字段名，g-recaptcha-response / h-captcha-response
 * 是两家厂商的官方表单字段名（经官方文档核实）
 */
func TestCaptchaTokenFieldNames(t *testing.T) {
	cases := []struct {
		name  string
		field captchaTokenFields
		want  string
	}{
		{"统一字段", captchaTokenFields{CaptchaToken: "tk-main"}, "tk-main"},
		{"reCAPTCHA 官方字段", captchaTokenFields{Recaptcha: "tk-rc"}, "tk-rc"},
		{"hCaptcha 官方字段", captchaTokenFields{Hcaptcha: "tk-hc"}, "tk-hc"},
		{"统一字段优先", captchaTokenFields{CaptchaToken: "tk-main", Recaptcha: "tk-rc", Hcaptcha: "tk-hc"}, "tk-main"},
		{"统一字段仅空白时回退", captchaTokenFields{CaptchaToken: "   ", Hcaptcha: "tk-hc"}, "tk-hc"},
		{"都为空时为空", captchaTokenFields{}, ""},
		{"首尾空白被去除", captchaTokenFields{CaptchaToken: "  tk  "}, "tk"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.field.thirdPartyToken(); got != c.want {
				t.Errorf("thirdPartyToken() = %q，期望 %q", got, c.want)
			}
		})
	}
}

/*
 * TestCaptchaTokenFieldsJSONBinding 字段名必须与前端提交的 JSON 键一致
 *
 * 第三个断言是反例：cf-turnstile-response 属于 turnstileFields，
 * 若被本结构一并接收，Turnstile 的 token 会被送到 Google/hCaptcha 端点，必然校验失败
 */
func TestCaptchaTokenFieldsJSONBinding(t *testing.T) {
	for _, key := range []string{"captcha_token", "g-recaptcha-response", "h-captcha-response"} {
		var f captchaTokenFields
		if err := json.Unmarshal([]byte(`{"`+key+`":"v"}`), &f); err != nil {
			t.Fatalf("解析 %s 失败: %v", key, err)
		}
		if f.thirdPartyToken() != "v" {
			t.Errorf("%s 未被绑定，实际 %q", key, f.thirdPartyToken())
		}
	}

	var g captchaTokenFields
	if err := json.Unmarshal([]byte(`{"cf-turnstile-response":"v"}`), &g); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if g.thirdPartyToken() != "" {
		t.Errorf("cf-turnstile-response 不应被第三方字段接收，实际 %q", g.thirdPartyToken())
	}
}

/*
 * TestForgotPasswordThirdPartyCaptcha 找回密码接入第三方校验的端到端三分支
 *
 * 未配置私钥 → 不要求人机验证（零倒退）
 * 已配置私钥 + 无 token → 拦截
 * 已配置私钥 + token 但上游判定失败 → 拦截
 * 已配置私钥 + 上游通过 → 不再被人机验证拦截
 */
func TestForgotPasswordThirdPartyCaptcha(t *testing.T) {
	cases := []struct {
		name      string
		secret    string
		token     string
		body      string
		wantBlock bool
	}{
		{"未配置私钥时放行", "", "", `{"success":true}`, false},
		{"私钥为空白时放行", "   ", "", `{"success":true}`, false},
		{"配置私钥后无 token 被拦", "sec", "", `{"success":true}`, true},
		{"配置私钥后上游判定失败被拦", "sec", "tk", `{"success":false,"error-codes":["invalid-input-response"]}`, true},
		{"配置私钥后上游通过放行", "sec", "tk", `{"success":true}`, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedUser(t, "admin") // 邮箱 admin@example.com
			seedConfig(t, map[string]string{
				"captcha_type":       "recaptcha",
				"captcha_secret_key": c.secret,
			})
			stubCaptchaUpstream(t, http.StatusOK, c.body)

			payload := map[string]string{"email": "admin@example.com"}
			if c.token != "" {
				payload["captcha_token"] = c.token
			}
			w := postJSON(t, ForgotPassword, "/api/auth/forgot-password", payload)

			blocked := strings.Contains(respMsg(t, w), "人机验证")
			if blocked != c.wantBlock {
				t.Errorf("被人机验证拦截 = %v，期望 %v（msg=%q）", blocked, c.wantBlock, respMsg(t, w))
			}
		})
	}
}

/* TestForgotTOTPThirdPartyCaptcha 重置二步验证的申请入口同样受第三方校验保护 */
func TestForgotTOTPThirdPartyCaptcha(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")
	seedConfig(t, map[string]string{"captcha_type": "hcaptcha", "captcha_secret_key": "sec"})
	stubCaptchaUpstream(t, http.StatusOK, `{"success":true}`)

	w := postJSON(t, ForgotTOTP, "/api/auth/forgot-totp", map[string]string{"email": "admin@example.com"})
	if msg := respMsg(t, w); !strings.Contains(msg, "人机验证") {
		t.Errorf("配置私钥后未携带 token 应被人机验证拦截，实际 msg = %q", msg)
	}
}

/* TestResetTOTPThirdPartyCaptcha 凭邮件链接重置二步验证同样受第三方校验保护 */
func TestResetTOTPThirdPartyCaptcha(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedConfig(t, map[string]string{"captcha_type": "hcaptcha", "captcha_secret_key": "sec"})
	stubCaptchaUpstream(t, http.StatusOK, `{"success":true}`)

	w := postJSON(t, ResetTOTP, "/api/auth/reset-totp", map[string]string{"token": "whatever"})
	if msg := respMsg(t, w); !strings.Contains(msg, "人机验证") {
		t.Errorf("配置私钥后未携带 token 应被人机验证拦截，实际 msg = %q", msg)
	}
}

/*
 * TestResetTOTPThirdPartyRejectsBeforeTokenLookup 第三方校验必须同样早于 token 查库
 *
 * 若顺序颠倒，攻击者可用任意 token 探测「该 token 是否存在」——
 * 断言的是错误文案：未携带 token 时必须得到人机验证提示，而不是「无效的重置链接」
 */
func TestResetTOTPThirdPartyRejectsBeforeTokenLookup(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedConfig(t, map[string]string{"captcha_type": "recaptcha", "captcha_secret_key": "sec"})
	stubCaptchaUpstream(t, http.StatusOK, `{"success":true}`)

	w := postJSON(t, ResetTOTP, "/api/auth/reset-totp", map[string]string{"token": "definitely-not-a-real-token"})
	msg := respMsg(t, w)
	if strings.Contains(msg, "无效的重置链接") {
		t.Errorf("第三方校验应先于 token 查库，实际已泄露 token 无效信息：%q", msg)
	}
	if !strings.Contains(msg, "人机验证") {
		t.Errorf("应返回人机验证提示，实际 %q", msg)
	}
}

/*
 * TestSensitiveCaptchaBothSwitches 两个开关同时配置时必须都通过
 *
 * Turnstile 与第三方验证码是两套独立配置，任一未通过都不得放行；
 * 只携带其中一家的 token 时，另一家的守卫必须拦下且不产生跨厂商出站请求
 */
func TestSensitiveCaptchaBothSwitches(t *testing.T) {
	cases := []struct {
		name           string
		turnstileToken string
		captchaToken   string
		wantErr        bool
	}{
		{"两个 token 都带", "ts", "rc", false},
		{"只带 Turnstile token", "ts", "", true},
		{"只带第三方 token", "", "rc", true},
		{"都不带", "", "", true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, map[string]string{
				"turnstile_secret_key": "0x4AAAA-secret",
				"captcha_type":         "recaptcha",
				"captcha_secret_key":   "sec",
			})
			stub := stubCaptchaUpstream(t, http.StatusOK, `{"success":true}`)

			err := requireSensitiveCaptcha(newCaptchaContext(t), c.turnstileToken, c.captchaToken)
			if (err != nil) != c.wantErr {
				t.Fatalf("拒绝 = %v，期望 %v（err=%v）", err != nil, c.wantErr, err)
			}
			// Turnstile 先于第三方：缺 Turnstile token 时它已拦下，不应惊动 Google；
			// 两者都带时各校验一次
			wantCalls := 0
			if c.turnstileToken != "" {
				wantCalls = 1
				if c.captchaToken != "" {
					wantCalls = 2
				}
			}
			if len(stub.urls) != wantCalls {
				t.Errorf("出站请求数 = %d，期望 %d（%v）", len(stub.urls), wantCalls, stub.urls)
			}
		})
	}
}

/*
 * TestSensitiveCaptchaTurnstileOnlyUnchanged 回归：只配置 Turnstile 时行为与改动前一致
 *
 * captcha_type 停留在 turnstile 时，不得因为引入了第三方校验而多发一次出站请求
 */
func TestSensitiveCaptchaTurnstileOnlyUnchanged(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret", "captcha_type": "turnstile"})
	stub := stubCaptchaUpstream(t, http.StatusOK, `{"success":true}`)

	if err := requireSensitiveCaptcha(newCaptchaContext(t), "ts", ""); err != nil {
		t.Fatalf("Turnstile 通过后应放行，实际 %v", err)
	}
	if len(stub.urls) != 1 {
		t.Fatalf("应只发起 1 次 Turnstile 校验，实际 %v", stub.urls)
	}
	if stub.urls[0] != "https://challenges.cloudflare.com/turnstile/v0/siteverify" {
		t.Errorf("端点 = %q，期望 Turnstile 站点校验端点", stub.urls[0])
	}
}

/*
 * TestTurnstileStandaloneSignalUnaffectedByCaptchaType 回归：下发信号仍只看 Turnstile 私钥
 *
 * captcha_type 切到 recaptcha 不应改变 turnstile_standalone_required——
 * 前端据此决定是否渲染 Turnstile 组件，两者分叉会造成「后端要求但前端不渲染」的死锁
 */
func TestTurnstileStandaloneSignalUnaffectedByCaptchaType(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedConfig(t, map[string]string{
		"turnstile_secret_key": "0x4AAAA-secret",
		"captcha_type":         "recaptcha",
		"captcha_secret_key":   "sec",
	})

	w := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(w)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	GetAuthConfig(ctx)

	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if v, _ := resp.Data["turnstile_standalone_required"].(bool); !v {
		t.Errorf("turnstile_standalone_required = %v，期望 true（判据只看 Turnstile 私钥）", resp.Data["turnstile_standalone_required"])
	}
	if v, _ := resp.Data["captcha_type"].(string); v != "recaptcha" {
		t.Errorf("captcha_type = %v，期望 recaptcha", resp.Data["captcha_type"])
	}
}

/*
 * TestLoginCaptchaDispatchByType 登录流程按 captcha_type 分派到各自的校验
 *
 * 这是本轮修复的核心：改动前 else 分支把 image / turnstile / recaptcha / hcaptcha
 * 一律送进 base64Captcha，管理员选中任一第三方类型后登录恒返回「验证码不能为空」。
 *
 * 断言用「是否越过验证码关卡」而非「登录是否成功」：
 * 越过后的响应是「用户名或密码错误」（故意用错密码，避免依赖 token 签发链路），
 * 未越过则必然带着该类型专属的验证码文案
 */
func TestLoginCaptchaDispatchByType(t *testing.T) {
	cases := []struct {
		name       string
		config     map[string]string
		payload    map[string]string
		wantURL    string // 空表示不应产生任何出站请求
		wantMsg    string // 期望响应中出现的文案
		wantPassed bool   // 是否已越过验证码关卡
		upstream   string // 上游应答，空则用 success=true
	}{
		{
			name:       "图形验证码：正确凭据越过",
			config:     map[string]string{"captcha_type": "image"},
			payload:    map[string]string{"captcha_id": "img-ok", "captcha_code": "ABCD"},
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:    "图形验证码：空凭据被拒",
			config:  map[string]string{"captcha_type": "image"},
			payload: map[string]string{},
			wantMsg: "验证码不能为空",
		},
		{
			name:    "图形验证码：错误凭据被拒",
			config:  map[string]string{"captcha_type": "image"},
			payload: map[string]string{"captcha_id": "img-bad", "captcha_code": "WRONG"},
			wantMsg: "验证码错误",
		},
		{
			name:       "captcha_type 未配置时仍走图形验证码",
			config:     map[string]string{},
			payload:    map[string]string{"captcha_id": "img-ok", "captcha_code": "ABCD"},
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:       "行为验证码：有效 verify_token 越过",
			config:     map[string]string{"captcha_type": "behavioral"},
			payload:    map[string]string{"verify_token": "vt-ok"},
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:    "行为验证码：无效 verify_token 被拒",
			config:  map[string]string{"captcha_type": "behavioral"},
			payload: map[string]string{"verify_token": "vt-bad"},
			wantMsg: "请先完成验证码",
		},
		{
			name:       "Turnstile：已配置私钥且校验通过则越过",
			config:     map[string]string{"captcha_type": "turnstile", "turnstile_secret_key": "0x4AAAA-secret"},
			payload:    map[string]string{"turnstile_token": "ts-token"},
			wantURL:    "https://challenges.cloudflare.com/turnstile/v0/siteverify",
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:     "Turnstile：已配置私钥但上游判定失败则被拒",
			config:   map[string]string{"captcha_type": "turnstile", "turnstile_secret_key": "0x4AAAA-secret"},
			payload:  map[string]string{"turnstile_token": "ts-token"},
			upstream: `{"success":false,"error-codes":["invalid-input-response"]}`,
			wantURL:  "https://challenges.cloudflare.com/turnstile/v0/siteverify",
			wantMsg:  "invalid-input-response",
		},
		{
			name:    "Turnstile：已配置私钥但未携带 token 则被拒",
			config:  map[string]string{"captcha_type": "turnstile", "turnstile_secret_key": "0x4AAAA-secret"},
			payload: map[string]string{},
			wantMsg: "人机验证",
		},
		{
			name:       "Turnstile：未配置私钥时放行（零倒退）",
			config:     map[string]string{"captcha_type": "turnstile"},
			payload:    map[string]string{},
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:       "reCAPTCHA：已配置私钥且校验通过则越过",
			config:     map[string]string{"captcha_type": "recaptcha", "captcha_secret_key": "sec"},
			payload:    map[string]string{"captcha_token": "rc-token"},
			wantURL:    "https://www.google.com/recaptcha/api/siteverify",
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:    "reCAPTCHA：已配置私钥但未携带 token 则被拒（改动前是「验证码不能为空」）",
			config:  map[string]string{"captcha_type": "recaptcha", "captcha_secret_key": "sec"},
			payload: map[string]string{},
			wantMsg: "人机验证",
		},
		{
			name:     "reCAPTCHA：已配置私钥但上游判定失败则被拒",
			config:   map[string]string{"captcha_type": "recaptcha", "captcha_secret_key": "sec"},
			payload:  map[string]string{"captcha_token": "rc-token"},
			upstream: `{"success":false,"error-codes":["invalid-input-response"]}`,
			wantURL:  "https://www.google.com/recaptcha/api/siteverify",
			wantMsg:  "invalid-input-response",
		},
		{
			name:       "reCAPTCHA：未配置私钥时放行（零倒退）",
			config:     map[string]string{"captcha_type": "recaptcha"},
			payload:    map[string]string{},
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:       "hCaptcha：已配置私钥且校验通过则越过",
			config:     map[string]string{"captcha_type": "hcaptcha", "captcha_secret_key": "sec"},
			payload:    map[string]string{"h-captcha-response": "hc-token"},
			wantURL:    "https://api.hcaptcha.com/siteverify",
			wantMsg:    "用户名或密码错误",
			wantPassed: true,
		},
		{
			name:    "hCaptcha：已配置私钥但未携带 token 则被拒",
			config:  map[string]string{"captcha_type": "hcaptcha", "captcha_secret_key": "sec"},
			payload: map[string]string{},
			wantMsg: "人机验证",
		},
		{
			name:    "跨类型 token 不被接受：turnstile 类型不认第三方 token",
			config:  map[string]string{"captcha_type": "turnstile", "turnstile_secret_key": "0x4AAAA-secret"},
			payload: map[string]string{"captcha_token": "rc-token"},
			wantMsg: "人机验证",
		},
		{
			name:    "跨类型 token 不被接受：reCAPTCHA 类型不认 Turnstile token",
			config:  map[string]string{"captcha_type": "recaptcha", "captcha_secret_key": "sec"},
			payload: map[string]string{"turnstile_token": "ts-token"},
			wantMsg: "人机验证",
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			password := seedUser(t, "admin")
			seedConfig(t, c.config)

			// 图形验证码凭据写入 store；行为验证码令牌写入缓存
			_ = store.Set("img-ok", "ABCD")
			if err := cache.C.Set(captchaVerifyKeyPrefix+"vt-ok", "1", captchaVerifyTTL); err != nil {
				t.Fatalf("写入 verify_token 失败: %v", err)
			}

			body := c.upstream
			if body == "" {
				body = `{"success":true}`
			}
			stub := stubCaptchaUpstream(t, http.StatusOK, body)

			payload := map[string]string{"username": "admin", "password": password + "-wrong"}
			for k, v := range c.payload {
				payload[k] = v
			}
			w := postJSON(t, Login, "/api/auth/login", payload)
			msg := respMsg(t, w)

			if !strings.Contains(msg, c.wantMsg) {
				t.Errorf("响应文案 = %q，期望包含 %q", msg, c.wantMsg)
			}
			if strings.Contains(msg, "验证码不能为空") && c.wantPassed {
				t.Errorf("仍被图形验证码分支拦下，说明未按 captcha_type 分派：%q", msg)
			}
			if c.wantURL == "" {
				if len(stub.urls) != 0 {
					t.Errorf("不应发起校验请求，实际 %v", stub.urls)
				}
				return
			}
			if len(stub.urls) != 1 || stub.urls[0] != c.wantURL {
				t.Errorf("校验请求 = %v，期望恰好一次且端点 %q", stub.urls, c.wantURL)
			}
		})
	}
}

/*
 * TestLoginRequestTokenFieldsJSONBinding 登录请求体的 token 字段名必须与前端一致
 *
 * 三个字段名分别对应：后端统一字段 captcha_token、两家厂商官方字段名、Turnstile 字段名。
 * 任一写错都会让前端提交的 token 落空，表现为「组件明明通过了却提示请先完成人机验证」
 */
func TestLoginRequestTokenFieldsJSONBinding(t *testing.T) {
	cases := []struct {
		json            string
		wantTurnstile   string
		wantThirdParty  string
		wantCaptchaID   string
		wantCaptchaCode string
	}{
		{json: `{"captcha_id":"i","captcha_code":"c","turnstile_token":"ts"}`, wantTurnstile: "ts", wantCaptchaID: "i", wantCaptchaCode: "c"},
		{json: `{"turnstile_token":"ts"}`, wantTurnstile: "ts"},
		{json: `{"cf-turnstile-response":"cf"}`, wantTurnstile: "cf"},
		{json: `{"captcha_token":"ct"}`, wantThirdParty: "ct"},
		{json: `{"g-recaptcha-response":"gr"}`, wantThirdParty: "gr"},
		{json: `{"h-captcha-response":"hc"}`, wantThirdParty: "hc"},
	}

	for _, c := range cases {
		t.Run(c.json, func(t *testing.T) {
			var req LoginRequest
			if err := json.Unmarshal([]byte(c.json), &req); err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if got := req.turnstileFields.token(); got != c.wantTurnstile {
				t.Errorf("turnstile token = %q，期望 %q", got, c.wantTurnstile)
			}
			if got := req.captchaTokenFields.thirdPartyToken(); got != c.wantThirdParty {
				t.Errorf("第三方 token = %q，期望 %q", got, c.wantThirdParty)
			}
			if req.CaptchaID != c.wantCaptchaID {
				t.Errorf("captcha_id = %q，期望 %q", req.CaptchaID, c.wantCaptchaID)
			}
			if req.CaptchaCode != c.wantCaptchaCode {
				t.Errorf("captcha_code = %q，期望 %q", req.CaptchaCode, c.wantCaptchaCode)
			}
		})
	}
}

/*
 * TestGetAuthConfigSiteKeyDelivery site key 的下发时机与按类型收起
 *
 * 两个站点密钥的读取时机不同：
 *   - turnstile_site_key 与 captcha_type 无关：它还兼作敏感流程（找回密码等）的组件密钥，
 *     那里不看 captcha_type，只看 turnstile_secret_key 是否非空
 *   - captcha_site_key 按类型收起：只在 captcha_type 是 recaptcha / hcaptcha 时读取，
 *     否则前端在 turnstile 类型下会同时看到它，可能拿 reCAPTCHA 的 key 去初始化 Turnstile
 *
 * 三个 key 均非空才下发，缺失不代表未启用，而是未配置站点密钥
 */
func TestGetAuthConfigSiteKeyDelivery(t *testing.T) {
	cases := []struct {
		name            string
		config          map[string]string
		wantCaptchaSite string
		wantTurnstile   string
	}{
		{"都未配置则都不下发", map[string]string{}, "", ""},
		{"reCAPTCHA 类型下主验证码站点密钥下发", map[string]string{"captcha_type": "recaptcha", "captcha_site_key": "6Lc-main"}, "6Lc-main", ""},
		{"hCaptcha 类型下主验证码站点密钥下发", map[string]string{"captcha_type": "hcaptcha", "captcha_site_key": "6Lc-main"}, "6Lc-main", ""},
		{"主验证码站点密钥为空白视为未配置", map[string]string{"captcha_type": "recaptcha", "captcha_site_key": "   "}, "", ""},
		{"turnstile 类型下不下发主验证码站点密钥——避免拿错 key", map[string]string{"captcha_type": "turnstile", "captcha_site_key": "6Lc-main"}, "", ""},
		{"image 类型下不下发主验证码站点密钥", map[string]string{"captcha_type": "image", "captcha_site_key": "6Lc-main"}, "", ""},
		{"行为验证码类型下不下发主验证码站点密钥", map[string]string{"captcha_type": "behavioral", "captcha_site_key": "6Lc-main"}, "", ""},
		{"类型未配置时默认 image，不下发主验证码站点密钥", map[string]string{"captcha_site_key": "6Lc-main"}, "", ""},
		{"Turnstile 站点密钥与类型无关，非空即下发", map[string]string{"captcha_type": "image", "turnstile_site_key": "0x4AAAA-site"}, "", "0x4AAAA-site"},
		{"两把都配且类型为第三方时都下发", map[string]string{"captcha_type": "recaptcha", "captcha_site_key": "6Lc-main", "turnstile_site_key": "0x4AAAA-site"}, "6Lc-main", "0x4AAAA-site"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, c.config)

			got := getAuthConfigData(t)
			assertStringField(t, got, "captcha_site_key", c.wantCaptchaSite)
			assertStringField(t, got, "turnstile_site_key", c.wantTurnstile)
		})
	}
}

/*
 * TestGetAuthConfigCaptchaTypeTurnstile 登录流程的 Turnstile 信号
 *
 * 该字段必须无条件下发（含 false），否则前端无法区分「登录不用 Turnstile」与
 * 「后端没实现该信号」；判据只看 captcha_type，与敏感流程的
 * turnstile_standalone_required（只看私钥）互不影响
 */
func TestGetAuthConfigCaptchaTypeTurnstile(t *testing.T) {
	cases := []struct {
		name           string
		config         map[string]string
		wantLogin      bool
		wantStandalone bool
	}{
		{"登录用 Turnstile 且敏感流程也开", map[string]string{"captcha_type": "turnstile", "turnstile_secret_key": "0x4AAAA-secret"}, true, true},
		{"登录用 Turnstile 但敏感流程未开", map[string]string{"captcha_type": "turnstile"}, true, false},
		{"登录不用 Turnstile 但敏感流程开", map[string]string{"captcha_type": "image", "turnstile_secret_key": "0x4AAAA-secret"}, false, true},
		{"两者都关", map[string]string{"captcha_type": "image"}, false, false},
		{"类型未配置时默认 image", map[string]string{}, false, false},
		{"第三方类型不影响 Turnstile 信号", map[string]string{"captcha_type": "recaptcha", "turnstile_secret_key": "0x4AAAA-secret"}, false, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, c.config)

			got := getAuthConfigData(t)
			// 三个信号都是布尔，必须存在且取值正确
			for _, key := range []string{"captcha_type_turnstile", "turnstile_standalone_required"} {
				if _, ok := got[key]; !ok {
					t.Errorf("响应缺少布尔信号 %s —— 前端无法区分「未开启」与「后端没实现」", key)
				}
			}
			if v, _ := got["captcha_type_turnstile"].(bool); v != c.wantLogin {
				t.Errorf("captcha_type_turnstile = %v，期望 %v", got["captcha_type_turnstile"], c.wantLogin)
			}
			if v, _ := got["turnstile_standalone_required"].(bool); v != c.wantStandalone {
				t.Errorf("turnstile_standalone_required = %v，期望 %v", got["turnstile_standalone_required"], c.wantStandalone)
			}
		})
	}
}

/* getAuthConfigData 调用 GetAuthConfig 并取出 data 段 */
func getAuthConfigData(t *testing.T) map[string]interface{} {
	t.Helper()
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
	GetAuthConfig(c)

	var resp struct {
		Data map[string]interface{} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
	}
	return resp.Data
}

/* assertStringField 断言字符串字段的存在性与取值，空期望值要求字段不存在 */
func assertStringField(t *testing.T, data map[string]interface{}, key, want string) {
	t.Helper()
	got, ok := data[key]
	if want == "" {
		if ok {
			t.Errorf("%s 不应下发（未配置时的语义是「缺失」），实际 %v", key, got)
		}
		return
	}
	if !ok {
		t.Errorf("%s 未下发，期望 %q", key, want)
		return
	}
	if s, _ := got.(string); s != want {
		t.Errorf("%s = %q，期望 %q", key, s, want)
	}
}
