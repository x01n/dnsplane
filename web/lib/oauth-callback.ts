/**
 * OAuth / 旧版登录回调：从 URL fragment（优先）或 query 读取 token，避免 access token 出现在查询串（易进 Referer/日志）。
 * 读到 fragment 后会 replaceState 清除 hash。
 */
export function consumeOAuthTokensFromUrl(searchParams: URLSearchParams): {
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
  const access_token = searchParams.get('access_token') || searchParams.get('token')
  const refresh_token = searchParams.get('refresh_token')
  if (access_token && refresh_token) {
    return { access_token, refresh_token }
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
