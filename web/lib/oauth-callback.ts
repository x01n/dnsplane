/**
 * 第三方登录失败原因的中文文案
 *
 * 键名与后端 oauth.go 的错误码常量逐字对应：这些码既可能出现在回调页的 JSON 响应里，
 * 也可能出现在回跳登录页的 ?error= 查询串里，两处共用同一张表
 */
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
  provider_login_off: '管理员已关闭该来源的登录',
  invalid_request: '请求参数无效',
  access_denied: '已取消第三方授权',
  provider_error: '第三方登录配置错误',
  create_failed: '创建账户失败',
  login_failed: '登录失败，请重试',
}
