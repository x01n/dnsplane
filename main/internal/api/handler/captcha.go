package handler

import (
	"strings"

	"github.com/gin-gonic/gin"

	"main/internal/notify"
)

/*
 * 第三方人机验证守卫（Cloudflare Turnstile / Google reCAPTCHA / hCaptcha）
 *
 * 三类校验的业务语义完全相同：未配置私钥即放行，已配置则 token 缺失或校验失败一律拒绝。
 * 差别只在「从哪取私钥、走哪家端点」，因此这里只写一次守卫，
 * 由 requireCaptcha 按 captcha_type 分派，避免同一段判断在三个流程里各写一遍
 */

/*
 * turnstileFields 公开表单共用的 Turnstile 提交字段
 *
 * 同时接受后端字段名 turnstile_token 与 Cloudflare 官方表单字段名 cf-turnstile-response，
 * 前端任选其一都能通过，避免实现被单一字段名绑死
 */
type turnstileFields struct {
	TurnstileToken string `json:"turnstile_token"`
	CFTurnstile    string `json:"cf-turnstile-response"`
}

/* token 返回两个字段中任一非空值，优先 turnstile_token */
func (f turnstileFields) token() string {
	if v := strings.TrimSpace(f.TurnstileToken); v != "" {
		return v
	}
	return strings.TrimSpace(f.CFTurnstile)
}

/*
 * captchaTokenFields 公开表单共用的第三方验证码提交字段
 *
 * 同时接受后端统一字段名 captcha_token 与两家厂商的官方表单字段名，
 * 前端任选其一都能通过，避免实现被单一字段名绑死
 *
 * 不含 cf-turnstile-response：Turnstile 由 turnstileFields 单独承载，
 * 两者混用会把 reCAPTCHA 的 token 送到 Cloudflare 端点，或反之
 *
 * 三家组件（components/auth-captcha-fields.tsx）当前统一走 SDK 回调 + onChange
 * 上抛 token，并未使用官方字段名，这些别名是为表单直投式提交预留的
 */
type captchaTokenFields struct {
	CaptchaToken string `json:"captcha_token"`
	Recaptcha    string `json:"g-recaptcha-response"`
	Hcaptcha     string `json:"h-captcha-response"`
}

/* thirdPartyToken 返回各字段中首个非空值，按声明顺序优先 */
func (f captchaTokenFields) thirdPartyToken() string {
	for _, v := range []string{f.CaptchaToken, f.Recaptcha, f.Hcaptcha} {
		if s := strings.TrimSpace(v); s != "" {
			return s
		}
	}
	return ""
}

/*
 * requireCaptcha 按 captcha_type 强制校验第三方人机验证
 *
 * 分派规则：
 *   - recaptcha / hcaptcha：登录验证码使用的通用键 captcha_secret_key 是私钥来源，
 *     非空即强制校验，与 Turnstile 同语义
 *   - 其余类型（image / behavioral / 未设 captcha_type / 未知取值）：一律不校验
 *
 * 「类型配错」同样放行是刻意选择：例如 captcha_type=recaptcha 而管理员只填了
 * turnstile_secret_key 时，用后者去请求 Google 端点必然失败，此时若拒绝，
 * 管理员唯一的补救路径（进设置页改回受支持的类型）会在保存前就被自己挡住；
 * 未知取值同理，不应为一个尚未配置的 captcha_type 引入强制校验
 *
 * @param c     请求上下文，用于传递 ctx 与用户 IP
 * @param token 前端提交的验证码 token
 * @returns 通过返回 nil，否则返回可直接回显给用户的错误
 */
func requireCaptcha(c *gin.Context, token string) error {
	captchaType := strings.TrimSpace(GetSysConfigValue("captcha_type"))
	if !notify.IsThirdPartyCaptchaType(captchaType) {
		return nil
	}
	secret := strings.TrimSpace(GetSysConfigValue("captcha_secret_key"))
	if secret == "" {
		return nil
	}
	return notify.VerifyCaptcha(c.Request.Context(), captchaType, secret, token, c.ClientIP())
}

/*
 * requireTurnstile 在已配置私有密钥时强制校验 Turnstile
 *
 * 未配置私有密钥 → 放行，与设置页「留空表示不强制敏感操作 Turnstile」一致
 * 已配置私有密钥 → token 缺失或校验失败一律拒绝，不提供任何绕过路径
 *
 * 接入点：ForgotPassword / ForgotTOTP / ResetTOTP，均置于各自限流之前。
 * 校验失败不应消耗 IP+邮箱 的配额，否则攻击者可用无 token 请求把合法用户打到限流。
 *
 * 前端契约：三个页面提交体统一带 turnstile_token（未启用时不发送该字段），
 * 与这里的「未配置私钥即放行」语义一致
 *
 * @param c     请求上下文，用于传递 ctx 与用户 IP
 * @param token 前端提交的 Turnstile token
 * @returns 通过返回 nil，否则返回可直接回显给用户的错误
 */
func requireTurnstile(c *gin.Context, token string) error {
	secret := strings.TrimSpace(GetSysConfigValue("turnstile_secret_key"))
	if secret == "" {
		return nil
	}
	return notify.VerifyTurnstile(c.Request.Context(), secret, token, c.ClientIP())
}

/*
 * requireSensitiveCaptcha 敏感流程（找回密码 / 重置二步验证）的人机验证入口
 *
 * Turnstile 与第三方验证码是两个独立开关，可能被同时配置，
 * 因此两者都不为空时要求「都通过」，任一方拒绝即整体拒绝
 *
 * @param c                 请求上下文
 * @param turnstileToken    前端提交的 Turnstile token
 * @param captchaToken      前端提交的第三方验证码 token
 * @returns 通过返回 nil，否则返回可直接回显给用户的错误
 */
func requireSensitiveCaptcha(c *gin.Context, turnstileToken, captchaToken string) error {
	if err := requireTurnstile(c, turnstileToken); err != nil {
		return err
	}
	return requireCaptcha(c, captchaToken)
}
