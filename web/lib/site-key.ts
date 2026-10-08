/**
 * 第三方验证码站点密钥（site key）的格式校验
 *
 * @param s 待校验的站点密钥；来源为后端配置或用户输入，接受任意类型
 * @returns 是否为可安全交给第三方 SDK 的密钥
 */
export function isValidSiteKey(s: unknown): s is string {
  return typeof s === 'string' && s.length > 0 && s.length <= 128 && /^[A-Za-z0-9_-]+$/.test(s)
}