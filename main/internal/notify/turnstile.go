package notify

import (
	"context"
)

/*
 * Cloudflare Turnstile 服务端校验
 *
 * 设置页明确声明：保存非空的 turnstile_secret_key 后，找回密码与重置二步验证页将要求
 * 用户完成 Turnstile。因此「未配置私钥即不校验」是既定语义，而不是可选的降级开关。
 * 私钥已配置时，token 缺失或校验失败一律拒绝，绝不放行——
 * 虚假的安全感比没有开关更危险，开关是否生效必须可观测
 *
 * 协议实现（表单参数、成功判据、失败文案）与 reCAPTCHA / hCaptcha 完全一致，
 * 统一由 captcha.go 的 verifyCaptchaAt 承担，本文件只保留 Turnstile 特有的端点与厂商名
 */

/* turnstileProvider 错误文案中展示的厂商名 */
const turnstileProvider = "Turnstile"

/* turnstileVerifyURL Cloudflare 站点校验端点 */
const turnstileVerifyURL = "https://challenges.cloudflare.com/turnstile/v0/siteverify"

/*
 * VerifyTurnstile 核验前端组件返回的 token
 *
 * @param ctx       请求上下文，随请求取消可立即中止校验
 * @param secretKey Turnstile 私有密钥，为空时返回错误（调用方应先判断是否需要校验）
 * @param token     前端组件返回的 token
 * @param remoteIP  用户 IP，可为空，供 Cloudflare 侧风控关联
 * @returns 校验通过返回 nil，否则返回可直接回显给用户的错误
 */
func VerifyTurnstile(ctx context.Context, secretKey, token, remoteIP string) error {
	return verifyCaptchaAt(ctx, turnstileVerifyURL, turnstileProvider, secretKey, token, remoteIP)
}

/*
 * verifyTurnstileAt 在指定端点核验 token，端点作为参数传入以便测试打桩
 *
 * @param ctx       请求上下文
 * @param endpoint  站点校验端点
 * @param secretKey Turnstile 私有密钥
 * @param token     前端组件返回的 token
 * @param remoteIP  用户 IP，可为空
 * @returns 校验通过返回 nil，否则返回可直接回显给用户的错误
 */
func verifyTurnstileAt(ctx context.Context, endpoint, secretKey, token, remoteIP string) error {
	return verifyCaptchaAt(ctx, endpoint, turnstileProvider, secretKey, token, remoteIP)
}
