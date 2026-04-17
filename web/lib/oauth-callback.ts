/**
 * OAuth / 旧版登录回调：仅从 URL fragment (#access_token=) 读取 token。
 *
 * 安全审计 H-4：删除查询串 (?token= / ?access_token=) 的 fallback 读取路径；
 * 查询串参数会进入 Referer/浏览器历史/代理日志，凭据永久泄露。
 * Fragment (#) 不会发送到服务器、不进 Referer，是 OAuth Implicit Flow 的唯一安全载体。
 * 命中后立刻 replaceState 清空 hash。
 *
 * 参数 _ 已弃用，保留签名避免全量改调用方；后续可清理。
 */
export function consumeOAuthTokensFromUrl(_: URLSearchParams): {
  access_token: string | null
  refresh_token: string | null
} {
  if (typeof window !== 'undefined') {
    const raw = window.location.hash
    if (raw && raw.length > 1) {
      const p = new URLSearchParams(raw.slice(1))
      const access_token = p.get('access_token')
      const refresh_token = p.get('refresh_token')
      if (access_token && refresh_token) {
        const path = window.location.pathname + window.location.search
        window.history.replaceState(null, '', path)
        return { access_token, refresh_token }
      }
    }
  }
  return { access_token: null, refresh_token: null }
}

export const OAUTH_CALLBACK_ERRORS: Record<string, string> = {
  register_disabled: '未开放新用户注册',
  email_not_whitelisted: '邮箱不在白名单',
  account_disabled: '账户已被禁用',
  already_bound_other: '该第三方账号已被其他用户绑定',
  user_not_found: '本地账号不存在，请重试或联系管理员',
  token_failed: '登录令牌获取失败',
  userinfo_failed: '获取第三方用户信息失败',
  user_info_failed: '获取第三方用户信息失败',
  invalid_state: '请求已过期，请重试',
  invalid_request: '请求参数无效',
  provider_error: '第三方登录配置错误',
  create_failed: '创建账户失败',
  login_failed: '登录失败，请重试',
}
