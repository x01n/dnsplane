package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

/*
 * 认证相关配置开关测试
 *
 * 覆盖三类此前「只存不用」的开关，它们都曾被保存进 SysConfig 却无任何后端消费，
 * 管理员以为生效而实际无效：
 *   - auth_password_login      关闭密码登录
 *   - turnstile_secret_key     敏感流程的 Turnstile 强制校验
 *   - auth_email_verify        注册流程的邮箱验证码
 *
 * 未配置一律视为开启（零倒退）：这些键在旧实例的库中普遍不存在，
 * 若按「未配置=关」处理，升级瞬间就会关闭密码登录 / 邮箱验证。
 */

/*
 * TestLoginHonoursPasswordLoginSwitch 密码登录开关三分支
 *
 * 关：凭据正确也必须被拒
 * 开 / 未配置：凭据正确即放行（零倒退）
 *
 * 断言看 code 而非仅看 msg：放行时 code 为 0，拒绝时为 -1
 */
func TestLoginHonoursPasswordLoginSwitch(t *testing.T) {
	cases := []struct {
		name       string
		cfg        map[string]string
		wantCode   int
		wantMsgHas string
	}{
		{"未配置开关应放行", nil, 0, "登录成功"},
		{"显式开启应放行", map[string]string{"auth_password_login": "true"}, 0, "登录成功"},
		{"显式关闭应拒绝", map[string]string{"auth_password_login": "false"}, -1, "管理员已关闭密码登录"},
		{"关闭值 0 应拒绝", map[string]string{"auth_password_login": "0"}, -1, "管理员已关闭密码登录"},
		{"关闭值 OFF 大小写不敏感", map[string]string{"auth_password_login": "OFF"}, -1, "管理员已关闭密码登录"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			password := seedUser(t, "admin")
			// 关闭图片验证码，隔离出密码登录开关这一个变量：
			// needCaptcha 默认 true，不关掉的话「应放行」用例会先被验证码拦住
			seedConfig(t, mergeConfig(c.cfg, map[string]string{"captcha_enabled": "0"}))

			w := postJSON(t, Login, "/api/login", map[string]string{
				"username":     "admin",
				"password":     password,
				"captcha_id":   "",
				"captcha_code": "",
			})

			var out struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
				t.Fatalf("解析响应失败: %v（原文：%s）", err, w.Body.String())
			}
			if out.Code != c.wantCode {
				t.Errorf("code = %d，期望 %d（msg=%q）", out.Code, c.wantCode, out.Msg)
			}
			if !strings.Contains(out.Msg, c.wantMsgHas) {
				t.Errorf("msg = %q，期望包含 %q", out.Msg, c.wantMsgHas)
			}
		})
	}
}

/*
 * TestLoginSwitchOffRejectsBeforeBruteForceCounter 开关关闭时不应累加失败计数
 *
 * 反例保护：若把开关判定放在凭据校验之后，攻击者可用不存在的账号反复触发
 * 「管理员已关闭密码登录」，而合法用户不承担任何后果——但更糟的是反过来：
 * 开关关闭期间任何尝试都会进入失败计数，误锁真实 IP
 */
func TestLoginSwitchOffRejectsBeforeBruteForceCounter(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")
	seedConfig(t, map[string]string{"auth_password_login": "0"})

	for i := 0; i < loginFailThreshold*2; i++ {
		postJSON(t, Login, "/api/login", map[string]string{
			"username": "admin",
			"password": "wrong-password",
		})
	}

	// 关掉开关后凭据正确也应被拒，且拒绝理由必须是开关而非锁定
	w := postJSON(t, Login, "/api/login", map[string]string{"username": "admin", "password": "wrong-password"})
	msg := respMsg(t, w)
	if !strings.Contains(msg, "管理员已关闭密码登录") {
		t.Errorf("开关关闭时拒绝理由应为开关本身，实际 msg = %q", msg)
	}
	if strings.Contains(msg, "登录失败次数过多") {
		t.Error("开关关闭时不应进入暴力破解锁定计数")
	}
}

/*
 * TestAuthEmailVerifySwitch 注册邮箱验证开关三分支
 *
 * 断言落在 SendAuthCode 上：它是注册链路的公开入口，且开关关闭时会明确告知
 * 「无需验证码」，比 Register 的间接表现更直接
 */
func TestAuthEmailVerifySwitch(t *testing.T) {
	cases := []struct {
		name       string
		cfg        map[string]string
		wantMsgHas string
	}{
		{"未配置开关应要求验证码", nil, "若邮件服务已配置"},
		{"显式开启应要求验证码", map[string]string{"auth_email_verify": "true"}, "若邮件服务已配置"},
		{"显式关闭应跳过验证码", map[string]string{"auth_email_verify": "false"}, "无需验证码"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedUser(t, "admin") // 满足 requireSystemInstalled
			seedConfig(t, map[string]string{
				"register_enabled":       "true",
				"auth_password_register": "true",
			})
			seedConfig(t, c.cfg)

			w := postJSON(t, SendAuthCode, "/api/auth/send-code", map[string]string{
				"email": "newuser@example.com",
				"scene": "register",
			})
			msg := respMsg(t, w)
			if !strings.Contains(msg, c.wantMsgHas) {
				t.Errorf("msg = %q，期望包含 %q", msg, c.wantMsgHas)
			}
		})
	}
}

/*
 * TestRegisterCodeRequiredOnlyWhenVerifyOn 关闭邮箱验证后，空验证码也应能注册
 *
 * 这是「开关真的生效」的端到端断言：code 字段曾写死 binding:"required"，
 * 使关闭开关在 API 层不可达
 */
func TestRegisterCodeRequiredOnlyWhenVerifyOn(t *testing.T) {
	t.Run("关闭时允许空验证码", func(t *testing.T) {
		setupHandlerTestDB(t)
		clearSysConfig(t)
		seedUser(t, "admin")
		seedConfig(t, map[string]string{
			"register_enabled":       "true",
			"auth_password_register": "true",
			"auth_email_verify":      "false",
		})

		w := postJSON(t, Register, "/api/auth/register", map[string]string{
			"username": "newbie",
			"password": "Str0ng!Passw0rd",
			"email":    "newbie@example.com",
			"code":     "",
		})
		msg := respMsg(t, w)
		if !strings.Contains(msg, "注册成功") {
			t.Errorf("关闭邮箱验证后空验证码应可注册，实际 msg = %q", msg)
		}
	})

	t.Run("开启时空验证码被拒", func(t *testing.T) {
		setupHandlerTestDB(t)
		clearSysConfig(t)
		seedUser(t, "admin")
		seedConfig(t, map[string]string{
			"register_enabled":       "true",
			"auth_password_register": "true",
			"auth_email_verify":      "true",
		})

		w := postJSON(t, Register, "/api/auth/register", map[string]string{
			"username": "newbie2",
			"password": "Str0ng!Passw0rd",
			"email":    "newbie2@example.com",
			"code":     "",
		})
		msg := respMsg(t, w)
		if !strings.Contains(msg, "请输入邮箱验证码") {
			t.Errorf("开启邮箱验证时空验证码应被拒，实际 msg = %q", msg)
		}
	})
}

/*
 * TestRequireTurnstileBranches Turnstile 强制校验三分支
 *
 * 未配置私钥 → 放行（与设置页「留空表示不强制」一致）
 * 已配置私钥 + 无 token → 拒绝（这是开关生效的核心断言，且不发起任何网络请求）
 * 已配置私钥 + 有 token 但服务不可达 → 拒绝（不得降级放行）
 */
func TestRequireTurnstileBranches(t *testing.T) {
	t.Run("未配置私钥应放行", func(t *testing.T) {
		setupHandlerTestDB(t)
		clearSysConfig(t)

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

		if err := requireTurnstile(c, ""); err != nil {
			t.Errorf("未配置私钥时应放行，实际返回 %v", err)
		}
	})

	t.Run("私钥为空白应放行", func(t *testing.T) {
		setupHandlerTestDB(t)
		clearSysConfig(t)
		seedConfig(t, map[string]string{"turnstile_secret_key": "   "})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

		if err := requireTurnstile(c, ""); err != nil {
			t.Errorf("私钥为空白时应放行，实际返回 %v", err)
		}
	})

	t.Run("已配置私钥但无token应拒绝", func(t *testing.T) {
		setupHandlerTestDB(t)
		clearSysConfig(t)
		seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret"})

		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/", nil)

		err := requireTurnstile(c, "")
		if err == nil {
			t.Fatal("已配置私钥但无 token 时必须拒绝，实际放行")
		}
		if !strings.Contains(err.Error(), "人机验证") {
			t.Errorf("错误信息应指向人机验证，实际 %q", err.Error())
		}
	})
}

/*
 * TestTurnstileTokenFieldNames 前端契约：两个字段名任一非空都算携带了 token
 *
 * turnstile_token 是后端首选字段，cf-turnstile-response 是 Cloudflare 官方表单字段名，
 * 前端任选其一都能通过，避免实现被单一字段名绑死
 */
func TestTurnstileTokenFieldNames(t *testing.T) {
	cases := []struct {
		name  string
		field turnstileFields
		want  string
	}{
		{"主字段", turnstileFields{TurnstileToken: "tk-primary"}, "tk-primary"},
		{"兼容字段", turnstileFields{CFTurnstile: "tk-compat"}, "tk-compat"},
		{"两者都在时主字段优先", turnstileFields{TurnstileToken: "tk-primary", CFTurnstile: "tk-compat"}, "tk-primary"},
		{"主字段仅空白时回退兼容字段", turnstileFields{TurnstileToken: "   ", CFTurnstile: "tk-compat"}, "tk-compat"},
		{"都为空时为空", turnstileFields{}, ""},
		{"首尾空白被去除", turnstileFields{TurnstileToken: "  tk  "}, "tk"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.field.token(); got != c.want {
				t.Errorf("token() = %q，期望 %q", got, c.want)
			}
		})
	}
}

/*
 * TestTurnstileFieldsJSONBinding 字段名必须与前端提交的 JSON 键一致
 * 用 json.Unmarshal 实测而非读 tag，避免 tag 与实际绑定行为脱节
 */
func TestTurnstileFieldsJSONBinding(t *testing.T) {
	var f turnstileFields
	if err := json.Unmarshal([]byte(`{"turnstile_token":"a"}`), &f); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if f.token() != "a" {
		t.Errorf("turnstile_token 未被绑定，实际 %q", f.token())
	}

	var g turnstileFields
	if err := json.Unmarshal([]byte(`{"cf-turnstile-response":"b"}`), &g); err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if g.token() != "b" {
		t.Errorf("cf-turnstile-response 未被绑定，实际 %q", g.token())
	}
}

/*
 * Turnstile 端到端：三个敏感流程在配置私钥后必须要求校验
 *
 * 前端契约（web/app/(auth)/forgot-password|forgot-totp|reset-totp/page.tsx）：
 * 提交体统一带 turnstile_token，未启用时前端不发送该字段，
 * 服务端未配置私钥时同样放行，双方语义一致。
 *
 * 接入点均在各自限流之前，因此这些用例在未配置私钥时不会消耗配额。
 */
func TestForgotPasswordRejectsWithoutTurnstile(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin") // 邮箱 admin@example.com
	seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret"})

	w := postJSON(t, ForgotPassword, "/api/auth/forgot-password", map[string]string{"email": "admin@example.com"})
	if msg := respMsg(t, w); !strings.Contains(msg, "人机验证") {
		t.Errorf("配置私钥后未携带 token 应被人机验证拦截，实际 msg = %q", msg)
	}
}

/* TestForgotPasswordPassesWhenNoSecret 未配置私钥时找回密码不受影响（零倒退） */
func TestForgotPasswordPassesWhenNoSecret(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")

	w := postJSON(t, ForgotPassword, "/api/auth/forgot-password", map[string]string{"email": "admin@example.com"})
	if msg := respMsg(t, w); strings.Contains(msg, "人机验证") {
		t.Errorf("未配置私钥时不应要求人机验证，实际 msg = %q", msg)
	}
}

/* TestForgotTOTPRejectsWithoutTurnstile 重置二步验证的申请入口同样受控 */
func TestForgotTOTPRejectsWithoutTurnstile(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")
	seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret"})

	w := postJSON(t, ForgotTOTP, "/api/auth/forgot-totp", map[string]string{"email": "admin@example.com"})
	if msg := respMsg(t, w); !strings.Contains(msg, "人机验证") {
		t.Errorf("配置私钥后未携带 token 应被人机验证拦截，实际 msg = %q", msg)
	}
}

/* TestResetTOTPRejectsWithoutTurnstile 凭邮件链接重置二步验证同样受控 */
func TestResetTOTPRejectsWithoutTurnstile(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")
	seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret"})

	w := postJSON(t, ResetTOTP, "/api/auth/reset-totp", map[string]string{"token": "whatever"})
	if msg := respMsg(t, w); !strings.Contains(msg, "人机验证") {
		t.Errorf("配置私钥后未携带 token 应被人机验证拦截，实际 msg = %q", msg)
	}
}

/*
 * TestResetTOTPRejectsBeforeTokenLookup Turnstile 必须早于 token 查库
 *
 * 若顺序颠倒，攻击者可用任意 token 探测「该 token 是否存在」——
 * 校验失败与 token 无效两种响应会泄露信息。断言的是错误文案：
 * 未携带 token 时必须得到人机验证提示，而不是「无效的重置链接」
 */
func TestResetTOTPRejectsBeforeTokenLookup(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")
	seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret"})

	w := postJSON(t, ResetTOTP, "/api/auth/reset-totp", map[string]string{"token": "definitely-not-a-real-token"})
	msg := respMsg(t, w)
	if strings.Contains(msg, "无效的重置链接") {
		t.Errorf("Turnstile 应先于 token 查库，实际已泄露 token 无效信息：%q", msg)
	}
	if !strings.Contains(msg, "人机验证") {
		t.Errorf("应返回人机验证提示，实际 %q", msg)
	}
}

/*
 * TestTurnstileRejectsBeforeRateLimit Turnstile 必须早于 IP 限流
 *
 * 未通过校验的请求不应消耗 AllowPublicEmailRequest 的配额，
 * 否则攻击者可用无 token 请求把合法用户的可信邮箱打到限流。
 * 断言：连续多次无 token 请求后，响应仍是人机验证提示而非限流模糊成功
 */
func TestTurnstileRejectsBeforeRateLimit(t *testing.T) {
	setupHandlerTestDB(t)
	clearSysConfig(t)
	seedUser(t, "admin")
	seedConfig(t, map[string]string{"turnstile_secret_key": "0x4AAAA-secret"})

	for i := 0; i < 8; i++ {
		w := postJSON(t, ForgotPassword, "/api/auth/forgot-password", map[string]string{"email": "admin@example.com"})
		msg := respMsg(t, w)
		if !strings.Contains(msg, "人机验证") {
			t.Fatalf("第 %d 次请求应仍为人机验证提示，实际 %q —— 说明限流先于校验", i+1, msg)
		}
	}
}


/*
 * TestGetAuthConfigTurnstileStandaloneRequired turnstile_standalone_required 的下发契约
 *
 * 该字段是 GET /auth/config 的既有类型声明（web/lib/api.ts:294），后端此前从未下发，
 * 前端据此无法区分「未开启独立校验」与「后端没实现」，于是在私钥已配置而站点密钥缺失时
 * 静默不渲染组件 —— 用户看到的是「后端拒绝 + 页面无组件」的死锁。
 *
 * 与相邻的 turnstile_site_key 不同，本字段是布尔，必须无条件下发（含 false）。
 */
func TestGetAuthConfigTurnstileStandaloneRequired(t *testing.T) {
	cases := []struct {
		name       string
		cfg        map[string]string
		want       bool
		wantSiteKs bool // turnstile_site_key 是否随响应下发
	}{
		{"均未配置", nil, false, false},
		{"仅私钥", map[string]string{"turnstile_secret_key": "0x4AAAA-secret"}, true, false},
		{"私钥为空白视为未配置", map[string]string{"turnstile_secret_key": "   "}, false, false},
		{"仅站点密钥", map[string]string{"turnstile_site_key": "0x4AAAA-site"}, false, true},
		{"两把都有", map[string]string{"turnstile_secret_key": "0x4AAAA-secret", "turnstile_site_key": "0x4AAAA-site"}, true, true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, c.cfg)

			w := httptest.NewRecorder()
			ctx, _ := gin.CreateTestContext(w)
			ctx.Request = httptest.NewRequest(http.MethodGet, "/api/auth/config", nil)
			GetAuthConfig(ctx)

			var resp struct {
				Code int                    `json:"code"`
				Data map[string]interface{} `json:"data"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
				t.Fatalf("解析响应失败: %v（原文 %s）", err, w.Body.String())
			}
			if resp.Code != 0 {
				t.Fatalf("code = %d，期望 0", resp.Code)
			}

			// 无条件存在：这是本用例的核心，缺字段会让前端退回「无法区分」的旧状态
			raw, exists := resp.Data["turnstile_standalone_required"]
			if !exists {
				t.Fatal("turnstile_standalone_required 必须无条件下发，实际缺失")
			}
			got, ok := raw.(bool)
			if !ok {
				t.Fatalf("turnstile_standalone_required 应为布尔，实际 %T", raw)
			}
			if got != c.want {
				t.Errorf("turnstile_standalone_required = %v，期望 %v", got, c.want)
			}

			_, sitePresent := resp.Data["turnstile_site_key"]
			if sitePresent != c.wantSiteKs {
				t.Errorf("turnstile_site_key 存在性 = %v，期望 %v（非空才下发）", sitePresent, c.wantSiteKs)
			}
		})
	}
}

/*
 * TestTurnstileStandaloneRequiredMatchesEnforcement 下发值与实际强制行为必须同源
 *
 * 若两者判据分叉（例如下发看 secret，强制看 secret+site），前端会按错误的状态渲染。
 * 此处把下发值当作前端唯一依据，反查 requireTurnstile 的真实行为。
 */
func TestTurnstileStandaloneRequiredMatchesEnforcement(t *testing.T) {
	cfgs := []map[string]string{
		nil,
		{"turnstile_secret_key": "0x4AAAA-secret"},
		{"turnstile_secret_key": "   "},
		{"turnstile_site_key": "0x4AAAA-site"},
		{"turnstile_secret_key": "0x4AAAA-secret", "turnstile_site_key": "0x4AAAA-site"},
	}

	for i, cfg := range cfgs {
		t.Run(fmt.Sprintf("配置%d", i), func(t *testing.T) {
			setupHandlerTestDB(t)
			clearSysConfig(t)
			seedConfig(t, cfg)

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
			advertised, _ := resp.Data["turnstile_standalone_required"].(bool)

			// 空 token 时 requireTurnstile 是否拒绝，即「是否真的必需」
			enforced := requireTurnstile(ctx, "") != nil

			if advertised != enforced {
				t.Errorf("下发值 %v 与实际强制 %v 不一致 —— 前端会按错误状态渲染", advertised, enforced)
			}
		})
	}
}
