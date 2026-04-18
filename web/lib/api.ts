const API_BASE = '/api'

/** 找回密码 / 魔法链接 / 注册验证码等 POST 的客户端等待上限（毫秒）；后端已异步发信 */
export const publicMailPostTimeoutMs = 12_000

export interface ApiResponse<T = unknown> {
  code: number
  msg?: string
  data?: T
}

/** 读取 document.cookie 中指定 name 的值；SSR 下返回空串 */
function readCookie(name: string): string {
  if (typeof document === 'undefined') return ''
  const prefix = name + '='
  for (const part of document.cookie.split(';')) {
    const seg = part.trim()
    if (seg.startsWith(prefix)) return decodeURIComponent(seg.slice(prefix.length))
  }
  return ''
}

class ApiClient {
  private token: string | null = null
  /** 并发 401 时合并为单次 refresh，避免 JTI 轮转导致多次刷新互相踩掉 */
  private refreshInFlight: Promise<boolean> | null = null
  /** CSRF 令牌预取合并，避免首屏多个并发请求各自 bootstrap */
  private csrfInFlight: Promise<string> | null = null

  constructor() {
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('token')
    }
  }

  /**
   * 获取 CSRF 令牌：优先读 cookie；不存在则 GET /api/csrf bootstrap。
   * 后端 double-submit cookie 策略要求请求头与 _csrf cookie 同值。
   */
  private async ensureCSRFToken(): Promise<string> {
    let tok = readCookie('_csrf')
    if (tok) return tok
    if (this.csrfInFlight) return this.csrfInFlight
    this.csrfInFlight = (async () => {
      try {
        const res = await fetch(`${API_BASE}/csrf`, {
          method: 'GET',
          credentials: 'include',
        })
        if (res.ok) {
          const j = (await res.json()) as ApiResponse<{ token: string }>
          if (j.code === 0 && j.data?.token) return j.data.token
        }
      } catch {
        /* 网络错误时返回空值，让本次请求按原逻辑去失败，下次会重试 */
      } finally {
        this.csrfInFlight = null
      }
      return readCookie('_csrf')
    })()
    tok = await this.csrfInFlight
    return tok
  }

  setToken(token: string | null) {
    this.token = token
    if (typeof window !== 'undefined') {
      if (token) {
        localStorage.setItem('token', token)
      } else {
        localStorage.removeItem('token')
        // 清理历史版本残留的 refresh_token（安全审计 M-1：该字段已不再写入 localStorage）
        localStorage.removeItem('refresh_token')
      }
    }
  }

  /**
   * 与 OAuth/注册一致：access 给 Authorization，refresh 仅靠 HttpOnly Cookie `_rt` 下发。
   * 安全审计 M-1：refresh_token 曾同时写入 localStorage，XSS 场景可被一次性窃取；
   * 改为只存服务端 HttpOnly Cookie，前端不再持有明文。
   */
  setTokens(tokens: { token: string; refresh_token?: string }) {
    this.token = tokens.token
    if (typeof window !== 'undefined') {
      localStorage.setItem('token', tokens.token)
      // 显式移除历史残留
      localStorage.removeItem('refresh_token')
    }
  }

  /** 内存中的 token 可能与 localStorage 脱节（构建时无 window、HMR、多标签页等），请求前对齐 */
  private syncTokenFromStorage() {
    if (typeof window === 'undefined') return
    if (!this.token) {
      const t = localStorage.getItem('token')
      if (t) this.token = t
    }
  }

  getToken() {
    this.syncTokenFromStorage()
    return this.token
  }

  /**
   * 调用 POST /auth/refresh（body 可带 refresh_token；Cookie _rt 同路径也会自动带上）
   * 不走 request()，避免与 401 处理互相递归
   */
  private tryRefreshSession(): Promise<boolean> {
    if (this.refreshInFlight) {
      return this.refreshInFlight
    }
    this.refreshInFlight = (async () => {
      try {
        // refresh token 仅走 HttpOnly Cookie `_rt`（安全审计 M-1），不再从 localStorage 读明文
        const res = await fetch(`${API_BASE}/auth/refresh`, {
          method: 'POST',
          headers: { 'Content-Type': 'application/json' },
          credentials: 'include',
          body: JSON.stringify({}),
        })
        if (!res.ok) {
          return false
        }
        const result = (await res.json()) as ApiResponse<{
          token: string
          refresh_token: string
          expires_in?: number
        }>
        if (result.code !== 0 || !result.data?.token || !result.data?.refresh_token) {
          return false
        }
        this.setTokens({
          token: result.data.token,
          refresh_token: result.data.refresh_token,
        })
        return true
      } catch {
        return false
      } finally {
        this.refreshInFlight = null
      }
    })()
    return this.refreshInFlight
  }

  private pathForAuthPolicy(fullUrl: string) {
    const q = fullUrl.indexOf('?')
    return q === -1 ? fullUrl : fullUrl.slice(0, q)
  }

  private async request<T>(
    method: string,
    url: string,
    data?: unknown,
    options?: RequestInit,
    _retryAfterRefresh = false
  ): Promise<ApiResponse<T>> {
    this.syncTokenFromStorage()

    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    }

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    // 非 GET 请求带上 double-submit CSRF 令牌，与服务端 _csrf cookie 同值
    if (method !== 'GET' && method !== 'HEAD') {
      const csrf = await this.ensureCSRFToken()
      if (csrf) headers['X-CSRF-Token'] = csrf
    }

    const config: RequestInit = {
      method,
      headers,
      credentials: 'include',
      ...options,
    }

    if (data && method !== 'GET') {
      config.body = JSON.stringify(data)
    }

    const t0 = typeof performance !== 'undefined' ? performance.now() : 0
    const response = await fetch(`${API_BASE}${url}`, config)

    if (response.status === 401) {
      const path = this.pathForAuthPolicy(url)
      const noRefresh =
        _retryAfterRefresh ||
        path === '/auth/refresh' ||
        path === '/login' ||
        path === '/install' ||
        path === '/install/status'

      if (!noRefresh) {
        const refreshed = await this.tryRefreshSession()
        if (refreshed) {
          return this.request<T>(method, url, data, options, true)
        }
      }

      this.setToken(null)
      if (typeof window !== 'undefined' && !window.location.pathname.includes('/login')) {
        window.location.href = '/login'
      }
      throw new Error('未登录或登录已过期')
    }

    let result: ApiResponse<T>
    try {
      result = (await response.json()) as ApiResponse<T>
    } catch {
      throw new Error('服务器返回了无效的响应格式')
    }
    if (
      typeof process !== 'undefined' &&
      process.env.NODE_ENV === 'development' &&
      typeof performance !== 'undefined'
    ) {
      console.debug(`[api] ${method} ${url} ${(performance.now() - t0).toFixed(1)}ms`)
    }
    return result
  }

  async get<T>(url: string, params?: Record<string, string | number | boolean | undefined>) {
    let queryString = ''
    if (params) {
      const searchParams = new URLSearchParams()
      Object.entries(params).forEach(([key, value]) => {
        if (value !== undefined && value !== '') {
          searchParams.append(key, String(value))
        }
      })
      queryString = searchParams.toString()
      if (queryString) {
        queryString = '?' + queryString
      }
    }
    return this.request<T>('GET', url + queryString)
  }

  /** 可选 timeoutMs：公开发信类接口短超时，避免等待过久（与后端异步发信配合） */
  async post<T>(
    url: string,
    data?: unknown,
    options?: RequestInit & { timeoutMs?: number }
  ) {
    const { timeoutMs, ...rest } = options || {}
    const init: RequestInit = { ...rest }
    if (typeof timeoutMs === 'number' && timeoutMs > 0 && !init.signal) {
      init.signal = AbortSignal.timeout(timeoutMs)
    }
    return this.request<T>('POST', url, data, init)
  }

  async put<T>(url: string, data?: unknown) {
    return this.request<T>('PUT', url, data)
  }

  async delete<T>(url: string, data?: unknown) {
    return this.request<T>('DELETE', url, data)
  }
}

export const api = new ApiClient()

/** 公开重置类接口等项目内约定名；当前后端为明文 JSON body */
export function encryptedPost<T = unknown>(
  path: string,
  data?: Record<string, unknown>
): Promise<ApiResponse<T>> {
  return api.post<T>(path, data)
}

/** GET /auth/config 与 handler.GetAuthConfig 对齐 */
export interface AuthPublicConfig {
  /** 与 login_captcha 同义，便于前端兼容 */
  captcha_enabled?: boolean
  login_captcha?: boolean
  register_enabled?: boolean
  /** 是否允许邮箱+密码自助注册（需服务端实现对应接口） */
  password_register_enabled?: boolean
  /** 无密码邮箱魔法链接登录（服务端 auth_magic_link_login） */
  magic_link_login_enabled?: boolean
  totp_enabled?: boolean
  captcha_type?: string
  captcha_site_key?: string
  turnstile_site_key?: string
  turnstile_standalone_required?: boolean
}

// Auth APIs
export const authApi = {
  /**
   * 登录 JSON 与 handler.LoginRequest 一致：captcha_id、captcha_code、totp_code
   * 前端可传 captcha 作为 captcha_code 的别名。
   */
  login: (data: {
    username: string
    password: string
    captcha_id?: string
    /** 图形验证码内容，将序列化为 captcha_code */
    captcha?: string
    captcha_code?: string
    totp_code?: string
  }) => {
    const body: Record<string, string> = {
      username: data.username,
      password: data.password,
    }
    if (data.captcha_id) body.captcha_id = data.captcha_id
    const code = data.captcha_code ?? data.captcha
    if (code !== undefined && code !== '') body.captcha_code = code
    if (data.totp_code) body.totp_code = data.totp_code
    return api.post<{ token: string; refresh_token: string; expires_in?: number; user: User }>(
      '/login',
      body,
    )
  },
  logout: () => api.post('/logout'),
  getCaptcha: () => api.get<{ captcha_id: string; captcha_image: string }>('/auth/captcha'),
  getConfig: () => api.get<AuthPublicConfig>('/auth/config'),
  getUserInfo: () => api.get<User>('/user/info'),
  changePassword: (data: { old_password: string; new_password: string }) =>
    api.post('/user/password', data),
  getInstallStatus: () => api.get<{ installed: boolean }>('/install/status'),
  install: (data: { username: string; password: string }) => api.post('/install', data),
  /** 仅 body.email，与 handler.ForgotPassword 一致 */
  forgotPassword: (data: { email: string }) =>
    api.post('/auth/forgot-password', data, { timeoutMs: publicMailPostTimeoutMs }),
  /** 仅 body.email，与 handler.ForgotTOTP 一致 */
  forgotTotp: (data: { email: string }) =>
    api.post('/auth/forgot-totp', data, { timeoutMs: publicMailPostTimeoutMs }),
  /**
   * 邮箱验证码：register → POST /auth/send-code；bindmail → POST /user/bind-email/send-code（需登录）
   */
  sendCode: (email: string, scene: string) =>
    scene === 'bindmail'
      ? api.post('/user/bind-email/send-code', { email }, { timeoutMs: publicMailPostTimeoutMs })
      : api.post('/auth/send-code', { email, scene }, { timeoutMs: publicMailPostTimeoutMs }),
  /** 绑定邮箱（POST /user/bind-email，需登录） */
  bindEmail: (email: string, code: string) =>
    api.post('/user/bind-email', { email, code }),
  register: (data: { username: string; password: string; email: string; code: string }) =>
    api.post('/auth/register', data),
  requestMagicLink: (email: string) =>
    api.post('/auth/magic-link', { email }, { timeoutMs: publicMailPostTimeoutMs }),
  /** 魔法链接第二步：preauth 来自邮件链接经 /api/quicklogin 跳转后的 query */
  verifyMagicLinkTotp: (preauth_token: string, totp_code: string) =>
    api.post<{
      token: string
      refresh_token: string
      expires_in?: number
      redirect?: string
      user: User
    }>('/auth/magic-link/totp', { preauth_token, totp_code }),
  // 行为/拼图验证码（mojocn/base64Captcha + go-captcha 系列）
  // 验证 answer 形态依赖具体验证码类型（点选/拖拽/旋转/算术），统一为 unknown
  getBehavioralCaptcha: () =>
    api.get<{ captcha_id: string; image_base64: string; thumb_base64?: string }>(
      '/auth/captcha/behavioral'
    ),
  verifyBehavioralCaptcha: (data: {
    captcha_id: string
    answer: unknown
    captcha_type?: string
  }) => api.post<{ verify_token: string }>('/auth/captcha/behavioral/verify', data),
  getGoCaptcha: () =>
    api.get<{ captcha_id: string; image_base64: string; thumb_base64?: string }>(
      '/auth/captcha/go'
    ),
  verifyGoCaptcha: (data: {
    captcha_id: string
    answer: unknown
    captcha_type?: string
  }) => api.post<{ verify_token: string }>('/auth/captcha/go/verify', data),
}

// 系统访问控制配置（白名单/限流/CAPTCHA 触发条件）
// 兼容历史命名：get/update 与 getConfig/updateConfig 同义
export const authControlApi = {
  get: () => api.get<Record<string, unknown>>('/system/auth-control'),
  update: (data: Record<string, unknown>) =>
    api.post('/system/auth-control', data),
  getConfig: () => api.get<Record<string, unknown>>('/system/auth-control'),
  updateConfig: (data: Record<string, unknown>) =>
    api.post('/system/auth-control', data),
}

/** 系统任务/Cron 状态信息（开放结构，schedule/optimize 等子任务字段动态注入） */
export interface TaskStatus {
  running: boolean
  last_run?: string
  next_run?: string
  error?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

// Account APIs
export const accountApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string }) =>
    api.get<{ total: number; list: Account[] }>('/accounts', params),
  create: (data: Partial<Account>) => api.post<Account>('/accounts', data),
  update: (id: number | string, data: Partial<Account>) => api.post<Account>(`/accounts/${id}`, data),
  delete: (id: number | string) => api.post(`/accounts/${id}/delete`, {}),
  check: (id: number | string) => api.post(`/accounts/${id}/check`),
  getDomainList: (id: number | string, params?: { keyword?: string; page?: number; page_size?: number }) =>
    api.get<{ total: number; list: DomainItem[] }>(`/accounts/${id}/domains`, params),
}

// Domain APIs
export const domainApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; aid?: number; status?: string }) =>
    api.get<{ total: number; list: Domain[] }>('/domains', params),
  detail: (id: number | string) => api.get<Domain>(`/domains/${id}`),
  create: (data: { aid: number; name: string; third_id: string; method?: number }) =>
    api.post<Domain>('/domains', data),
  sync: (data: { aid: number; domains: { name: string; id: string; record_count: number }[] }) =>
    api.post('/domains/sync', data),
  update: (id: number | string, data: Partial<Domain>) => api.post<Domain>(`/domains/${id}`, data),
  delete: (id: number | string) => api.post(`/domains/${id}/delete`, {}),
  /** 后端 BatchDomainActionRequest.ids 为 []string，此处统一转字符串 */
  batchAction: (data: {
    ids: (number | string)[]
    action: string
    is_notice?: boolean
    remark?: string
  }) =>
    api.post('/domains/batch', {
      ...data,
      ids: data.ids.map((id) => String(id)),
    }),
  updateExpire: (id: number | string) => api.post(`/domains/${id}/update-expire`),
  batchUpdateExpire: (ids: number[]) =>
    api.post('/domains/batch/update-expire', { ids: ids.map((id) => String(id)) }),
  getRecords: (id: number | string, params?: { page?: number; page_size?: number; keyword?: string; type?: string; line?: string }) =>
    api.get<{ total: number; list: DNSRecord[] }>(`/domains/${id}/records`, params),
  createRecord: (id: number | string, data: { name: string; type: string; value: string; line?: string; ttl?: number; mx?: number; remark?: string }) =>
    api.post<DNSRecord>(`/domains/${id}/records`, data),
  updateRecord: (domainId: number | string, recordId: string, data: { name: string; type: string; value: string; line?: string; ttl?: number; mx?: number; remark?: string }) =>
    api.post<DNSRecord>(`/domains/${domainId}/records/${recordId}`, data),
  deleteRecord: (domainId: number | string, recordId: string) =>
    api.post(`/domains/${domainId}/records/${recordId}/delete`, {}),
  setRecordStatus: (domainId: number | string, recordId: string, enable: boolean) =>
    api.post(`/domains/${domainId}/records/${recordId}/status`, { enable }),
  getLines: (id: number | string) => api.get<RecordLine[]>(`/domains/${id}/lines`),
  batchAddRecords: (id: number | string, data: { records: string; type?: string; line?: string; ttl?: number }) =>
    api.post(`/domains/${id}/records/batch`, data),
  batchEditRecords: (id: number | string, data: { record_ids: string[]; action: string; [key: string]: unknown }) =>
    api.post(`/domains/${id}/records/batch/edit`, data),
  batchActionRecords: (id: number | string, data: { record_ids: string[]; action: string }) =>
    api.post(`/domains/${id}/records/batch/action`, data),
  queryWhois: (id: number | string) => api.post<WhoisInfo>(`/domains/${id}/whois`),
  getLogs: (id: number | string, params?: { page?: number; page_size?: number }) =>
    api.get<{ total: number; list: DomainLog[] }>(`/domains/${id}/logs`, params),
  getQuickLoginURL: (id: number | string) => api.get<{ url: string }>(`/domains/${id}/loginurl`),
}

// Monitor APIs
export const monitorApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; status?: number }) =>
    api.get<{ total: number; list: MonitorTask[] }>('/monitor/tasks', params),
  create: (data: Partial<MonitorTask>) => api.post<MonitorTask>('/monitor/tasks', data),
  update: (id: number | string, data: Partial<MonitorTask>) => api.post<MonitorTask>(`/monitor/tasks/${id}`, data),
  delete: (id: number | string) => api.post(`/monitor/tasks/${id}/delete`, {}),
  toggle: (id: number | string, active: boolean) => api.post(`/monitor/tasks/${id}/toggle`, { active }),
  switch: (id: number | string) => api.post(`/monitor/tasks/${id}/switch`, {}),
  getLogs: (id: number | string, params?: { page?: number; page_size?: number; action?: number }) =>
    api.get<{ total: number; list: MonitorLog[] }>(`/monitor/tasks/${id}/logs`, params),
  /** 探测历史（用于列表迷你条、详情图表），数据来自 LogDB 的 dm_check_logs */
  getHistory: (id: number | string, period: '1h' | '24h' | '7d' | '30d' = '24h') =>
    api.get<MonitorCheckPoint[]>(`/monitor/tasks/${id}/history`, { period }),
  getUptime: (id: number | string) =>
    api.get<Record<string, { total: number; success: number; uptime: number; avg_duration: number }>>(
      `/monitor/tasks/${id}/uptime`
    ),
  getResolveStatus: (id: number | string) => api.get<unknown[]>(`/monitor/tasks/${id}/resolve-status`),
  lookup: (domainId: number | string, subDomain: string) =>
    api.post<{ domain: string; account_type: string; records: unknown[] }>('/monitor/lookup', {
      domain_id: domainId,
      sub_domain: subDomain,
    }),
  getOverview: () => api.get<MonitorOverview>('/monitor/overview'),
  batchCreate: (data: { tasks: Partial<MonitorTask>[] }) => api.post('/monitor/tasks/batch', data),
  /** 智能创建（lookup 选记录后批量建任务），与 handler.AutoCreateMonitorTask 一致 */
  autoCreate: (data: Record<string, unknown>) =>
    api.post<{ ids: number[]; created: number }>('/monitor/tasks/auto-create', data),
  getStatus: () => api.get<{ running: boolean; last_run: string }>('/monitor/status'),
}

// Cert APIs
export const certApi = {
  // Accounts
  getAccounts: (params?: { page?: number; page_size?: number; is_deploy?: boolean }) => {
    const query: Record<string, string | number> = {}
    if (params?.page) query.page = params.page
    if (params?.page_size) query.page_size = params.page_size
    if (params?.is_deploy !== undefined) query.deploy = params.is_deploy ? '1' : '0'
    return api.get<CertAccount[]>('/cert/accounts', query)
  },
  createAccount: (data: Partial<CertAccount>) => api.post<CertAccount>('/cert/accounts', data),
  updateAccount: (id: number | string, data: Partial<CertAccount>) => api.post<CertAccount>(`/cert/accounts/${id}`, data),
  deleteAccount: (id: number | string) => api.post(`/cert/accounts/${id}/delete`, {}),
  
  // Orders
  getOrders: (params?: { page?: number; page_size?: number; keyword?: string; aid?: number; status?: number }) =>
    api.get<{ total: number; list: CertOrder[] }>('/cert/orders', params),
  createOrder: (data: {
    account_id: number
    domains: string[]
    key_type?: string
    key_size?: string
    is_auto?: boolean
    /** ACME：dns-01 | http-01；通配符仅 dns-01；纯 IP 无需指定 */
    challenge_type?: string
  }) => api.post<CertOrder>('/cert/orders', data),
  processOrder: (id: number | string, reset?: boolean) => api.post(`/cert/orders/${id}/process`, { reset }),
  deleteOrder: (id: number | string) => api.post(`/cert/orders/${id}/delete`, {}),
  getOrderLog: (id: number | string) => api.get<{ log: string }>(`/cert/orders/${id}/log`),
  getOrderDetail: (id: number | string) => api.get<CertOrder>(`/cert/orders/${id}/detail`),
  downloadOrder: (id: number | string, format: string) => api.get<{ content: string }>(`/cert/orders/${id}/download`, { format }),
  toggleOrderAuto: (id: number | string, is_auto: boolean) => api.post(`/cert/orders/${id}/auto`, { is_auto }),
  
  // Deploys
  getDeploys: (params?: { page?: number; page_size?: number; keyword?: string; aid?: number; status?: number }) =>
    api.get<{ total: number; list: CertDeploy[] }>('/cert/deploys', params),
  createDeploy: (data: { account_id: number; order_id: number; config?: Record<string, string>; remark?: string }) => 
    api.post<CertDeploy>('/cert/deploys', data),
  updateDeploy: (id: number | string, data: Partial<CertDeploy> | { account_id?: number; order_id?: number; config?: Record<string, string>; remark?: string; active?: boolean }) => 
    api.post<CertDeploy>(`/cert/deploys/${id}`, data),
  deleteDeploy: (id: number | string) => api.post(`/cert/deploys/${id}/delete`, {}),
  processDeploy: (id: number | string, reset?: boolean) => api.post(`/cert/deploys/${id}/process`, { reset }),
  
  // CNAMEs
  getCNAMEs: (params?: { page?: number; page_size?: number }) =>
    api.get<{ total: number; list: CertCNAME[] }>('/cert/cnames', params),
  createCNAME: (data: Partial<CertCNAME>) => api.post<CertCNAME>('/cert/cnames', data),
  deleteCNAME: (id: number | string) => api.post(`/cert/cnames/${id}/delete`, {}),
  verifyCNAME: (id: number | string) => api.post<{ status: number }>(`/cert/cnames/${id}/verify`),
  
  // Providers - 返回 {cert: {...}, deploy: {...}} 格式
  getProviders: () => api.get<{ cert: Record<string, CertProviderConfig>; deploy: Record<string, CertProviderConfig> }>('/cert/providers'),
}

// User APIs
export const userApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string }) =>
    api.get<{ total: number; list: User[] }>('/users', params),
  create: (data: Partial<User> & { password: string; permissions?: string[] }) =>
    api.post<User>('/users', data),
  update: (id: number | string, data: Partial<User> & { password?: string; permissions?: string[] }) =>
    api.post<User>(`/users/${id}`, data),
  delete: (id: number | string) => api.post(`/users/${id}/delete`, {}),
  getPermissions: (id: number | string) => api.get<UserPermission[]>(`/users/${id}/permissions`),
  addPermission: (id: number | string, data: Partial<UserPermission>) => api.post(`/users/${id}/permissions`, data),
  updatePermission: (id: number | string, permId: number | string, data: Partial<UserPermission>) => 
    api.post(`/users/${id}/permissions/${permId}`, data),
  deletePermission: (id: number | string, permId: number | string) =>
    api.post(`/users/${id}/permissions/${permId}/delete`, {}),
  resetAPIKey: (id: number | string) => api.post<{ api_key: string }>(`/users/${id}/reset-apikey`),
  sendResetEmail: (id: number | string, type: 'password' | 'totp') => api.post(`/users/${id}/send-reset`, { type }),
  adminResetTOTP: (id: number | string) => api.post(`/users/${id}/reset-totp`),
}

/** GET /logs 返回；兼容旧版 records 字段 */
export interface OperationLogListData {
  total: number
  list?: OperationLog[]
  records?: OperationLog[]
  stats?: OperationLogStats
}

export interface OperationLogStats {
  today_count: number
  distinct_users: number
  distinct_domains: number
}

// Log APIs
export const logApi = {
  list: (params?: {
    page?: number
    page_size?: number
    keyword?: string
    domain?: string
    uid?: string | number
    user_id?: string | number
    action?: string
    entity?: string
    /** 含当日，格式 YYYY-MM-DD，与 date_to 均为可选 */
    date_from?: string
    date_to?: string
  }) => api.get<OperationLogListData>('/logs', params),
  /** 清理旧操作日志，days 表示保留多少天 */
  clean: (days: number) => api.post<{ deleted?: number }>('/logs/clean', { days }),
}

export interface SystemInfo {
  version?: string
  go_version?: string
  os?: string
  arch?: string
  num_cpu?: number
  goroutines?: number
  memory_alloc?: number
  memory_sys?: number
  data_db_size?: number
  logs_db_size?: number
  request_db_size?: number
  db_maintenance?: {
    last_vacuum?: string
    next_vacuum?: string
    main_db?: { size_text?: string }
    [key: string]: unknown
  }
}

// System APIs
export const systemApi = {
  getConfig: () => api.get<SystemConfig>('/system/config'),
  updateConfig: (data: Partial<SystemConfig>) => api.post('/system/config', data),
  testMail: () => api.post('/system/mail/test'),
  testTelegram: () => api.post('/system/telegram/test'),
  testWebhook: () => api.post('/system/webhook/test'),
  testDiscord: () => api.post('/system/discord/test'),
  testBark: () => api.post('/system/bark/test'),
  testWechat: () => api.post('/system/wechat/test'),
  /** 与 handler.TestProxy JSON 一致：host、pass（非 server/password） */
  testProxy: (data: {
    host: string
    port: number
    type: string
    user?: string
    pass?: string
  }) => api.post<{ latency?: number; status?: number }>('/system/proxy/test', data),
  clearCache: () => api.post('/system/cache/clear'),
  getTaskStatus: () => api.get<{ running: boolean; last_run: string; error?: string }>('/system/task/status'),
  getCronConfig: () => api.get<CronConfig>('/system/cron'),
  updateCronConfig: (data: CronConfig) => api.post('/system/cron', data),
  getDNSProviders: () => api.get<DNSProvider[]>('/dns/providers'),
  getSystemInfo: () => api.get<SystemInfo>('/dashboard/system/info'),
}

// Dashboard APIs
export const dashboardApi = {
  getStats: () => api.get<DashboardStats>('/dashboard/stats'),
}

// TOTP APIs
export const totpApi = {
  getStatus: () => api.get<{ enabled: boolean }>('/user/totp/status'),
  enable: () => api.post<{ secret: string; qrcode: string; uri?: string }>('/user/totp/enable'),
  verify: (code: string) => api.post('/user/totp/verify', { code }),
  /** 关闭 TOTP（需提供密码 + 6 位动态码二次确认） */
  disable: (password?: string, code?: string) =>
    api.post('/user/totp/disable', { password, code }),
  /** 重新生成 TOTP 恢复码列表（需提供密码 + 动态码二次确认） */
  regenerateRecovery: (password?: string, code?: string) =>
    api.post<{ recovery_codes: string[] }>('/user/totp/recovery/regenerate', { password, code }),
}

// OAuth（账户绑定；登录跳转使用 /api/auth/oauth/:provider/login）
export const oauthApi = {
  getProviders: () => api.get<OAuthProvider[]>('/auth/oauth/providers'),
  getBindings: () => api.get<OAuthBinding[]>('/user/oauth/bindings'),
  getBindURL: (provider: string) =>
    api.post<{ url: string }>('/user/oauth/bind-url', { provider }),
  unbind: (provider: string) => api.post('/user/oauth/unbind', { provider }),
}

// 请求日志（管理员）
export const requestLogApi = {
  getLogs: (params: {
    page?: number
    page_size?: number
    keyword?: string
    is_error?: string
    method?: string
    start_date?: string
    end_date?: string
  }) => api.post<{ total: number; list: RequestLog[] }>('/request-logs/list', params),
  getStats: () => api.post<RequestLogStats>('/request-logs/stats', {}),
  getByRequestId: (request_id: string) =>
    api.post<RequestLog>('/request-logs/detail', { request_id }),
  getByErrorId: (error_id: string) =>
    api.post<RequestLog>('/request-logs/error', { error_id }),
  cleanLogs: (days: number) =>
    api.post<{ msg?: string; deleted?: number }>('/request-logs/clean', { days }),
}

// Types
export interface OAuthProvider {
  name: string
  display_name: string
  icon: string
  enabled: boolean
}

export interface OAuthBinding {
  id: number
  user_id: number
  provider: string
  provider_user_id: string
  provider_name: string
  provider_email: string
  provider_avatar?: string
  expires_at?: string
  created_at: string
  updated_at: string
}

export interface RequestLog {
  id: number
  request_id: string
  error_id?: string
  user_id: number
  username?: string
  method: string
  path: string
  query?: string
  body?: string
  headers?: string
  ip: string
  user_agent?: string
  status_code: number
  response?: string
  duration: number
  is_error: boolean
  error_msg?: string
  error_stack?: string
  db_queries?: string
  db_query_time?: number
  extra?: string
  created_at: string
}

export interface RequestLogStats {
  total_count: number
  error_count: number
  today_count: number
  today_error_count: number
  recent_errors?: RequestLog[]
}

export interface User {
  id: number
  username: string
  email?: string
  is_api: boolean
  api_key?: string
  level: number
  status: number
  totp_open: boolean
  reg_time: string
  last_time?: string
  permissions?: string[]
}

export interface UserPermission {
  id: number
  uid: number
  did: number
  domain: string
  sub?: string
  read_only: boolean
  expire_time?: string
  created_at: string
}

export interface Account {
  id: number
  type: string
  name: string
  config?: string
  remark?: string
  created_at: string
  type_name?: string
  icon?: string
}

export interface Domain {
  id: number
  aid: number
  name: string
  third_id: string
  is_hide: boolean
  is_sso: boolean
  record_count: number
  remark?: string
  is_notice: boolean
  reg_time?: string
  expire_time?: string
  check_time?: string
  notice_time?: string
  check_status: number
  created_at: string
  account_type?: string
  account_name?: string
  type_name?: string
  icon?: string
  perm_sub?: string
}

export interface DomainItem {
  Domain: string
  DomainId: string
  RecordCount: number
  disabled?: boolean
}

export interface DNSRecord {
  RecordId: string
  Name: string
  Type: string
  Value: string
  Line: string
  LineName?: string
  TTL: number
  MX?: number
  Weight?: number
  Status: string
  Remark?: string
}

export interface RecordLine {
  id: string
  name: string
  parent?: string
}

export interface WhoisInfo {
  domain: string
  registrar?: string
  creation_date?: string
  expiration_date?: string
  /** 后端返回字段别名（部分接口走 created_date / expiry_date 命名） */
  created_date?: string
  expiry_date?: string
  updated_date?: string
  name_servers?: string[]
  status?: string[]
}

export interface DomainLog {
  id: number
  action: string
  data: string
  created_at: string
}

/** 容灾监控单次探测结果（与后端 DMCheckLog 一致） */
export interface MonitorCheckPoint {
  id: number
  task_id: number
  success: boolean
  duration: number
  error?: string
  main_health?: boolean
  backup_healths?: string
  main_duration?: number
  backup_duration?: number
  created_at: string
}

export interface MonitorTask {
  id: number
  did: number
  rr: string
  record_id: string
  record_type?: string
  record_line?: string
  type: number
  main_value: string
  backup_value?: string
  /** 多备用值，逗号分隔；非空时优先于 backup_value（与后端 DMTask.BackupValues 对齐） */
  backup_values?: string
  backup_type?: string
  check_type: number
  check_url?: string
  tcp_port?: number
  frequency: number
  cycle: number
  timeout: number
  remark?: string
  use_proxy: boolean
  cdn: boolean
  add_time: number
  check_time: number
  check_next_time: number
  switch_time: number
  err_count: number
  status: number
  /** 主源最新一次健康状态（DMTask.MainHealth） */
  main_health?: boolean
  /** 备用源健康状态汇总：JSON 数组字符串如 '[true,false]'（DMCheckLog 关联，运行时注入） */
  backup_health?: string
  active: boolean
  record_info?: string
  domain?: string
  expect_status?: string
  expect_keyword?: string
  max_redirects?: number
  proxy_type?: string
  proxy_host?: string
  proxy_port?: number
  proxy_username?: string
  proxy_password?: string
  notify_enabled?: boolean
  notify_channels?: string
  auto_restore?: boolean
  allow_insecure_tls?: boolean
  /** 故障/恢复时间戳与最新错误（运行时聚合字段） */
  fault_time?: number
  recover_time?: number
  last_error?: string
}

export interface MonitorLog {
  id: number
  task_id: number
  action: number
  err_msg?: string
  created_at: string
}

export interface MonitorOverview {
  run_count: number
  run_time: string
  run_state: number
  run_error?: string
  switch_count: number
  fail_count: number
  /** 任务计数与健康汇总（前端面板字段） */
  task_count?: number
  active_count?: number
  healthy_count?: number
  faulty_count?: number
  avg_uptime?: number
}

export interface CertAccount {
  id: number
  type: string
  name: string
  config?: string
  remark?: string
  is_deploy: boolean
  created_at: string
  type_name?: string
  icon?: string
}

export interface CertOrder {
  id: number
  aid: number
  order_kind?: string
  /** ACME 域名验证：dns-01 | http-01；空表示默认 DNS-01 */
  challenge_type?: string
  key_type: string
  key_size: string
  process_id?: string
  issue_time?: string
  expire_time?: string
  issuer?: string
  status: number
  error?: string
  is_auto: boolean
  retry: number
  fullchain?: string
  private_key?: string
  created_at: string
  domains?: string[]
  dns_info?: string
  type_name?: string
  icon?: string
  end_day?: number
}

export interface CertDeploy {
  id: number
  aid: number
  oid: number
  issue_time?: string
  config?: string
  remark?: string
  last_time?: string
  process_id?: string
  status: number
  error?: string
  active: boolean
  created_at: string
  type_name?: string
  icon?: string
  domains?: string[]
  cert_type_name?: string
}

export interface CertCNAME {
  id: number
  domain: string
  did: number
  rr: string
  status: number
  created_at: string
  cname_domain?: string
  host?: string
  record?: string
}

export interface CertProvider {
  type: string
  name: string
  icon?: string
  config: ProviderConfigField[]
  /** 部署方式说明（账户表单 / 列表展示） */
  note?: string
  /** 仅部署任务侧：无表单项时的提示 */
  deploy_note?: string
  max_domains?: number
  wildcard?: boolean
  cname?: boolean
  is_deploy?: boolean
}

// 后端API返回的证书/部署提供商配置格式
export interface CertProviderConfig {
  type: string
  name: string
  icon?: string
  note?: string
  config: ProviderConfigField[]
  is_deploy?: boolean
  cname?: boolean
  deploy_config?: ProviderConfigField[]
  deploy_note?: string
}

export interface DNSProvider {
  type: string
  name: string
  icon?: string
  config: ProviderConfigField[]
  add?: boolean
}

export interface ProviderConfigField {
  name: string       // 显示名称
  key: string        // 配置键名
  label?: string     // 标签（兼容旧代码）
  type: string
  required?: boolean
  placeholder?: string
  options?: { value: string; label: string }[]
  value?: string     // 默认值
  note?: string      // 备注
  show?: string      // 条件显示
}

export interface OperationLog {
  id: number
  uid: number
  action: string
  domain?: string
  data?: string
  created_at: string
  username?: string
}

/*
 * SystemConfig：系统设置键值袋。
 * 后端 SysConfig 表是 (key, value) 的开放结构，前端 settings 页大量字段
 * 只在需要时才接入。这里既列出已知键以提供 IDE 提示，也开放索引签名
 * 兼容未来追加项；新增字段无需先改这里再改 settings 页。
 */
export interface SystemConfig {
  // 验证码
  captcha_enabled?: boolean
  captcha_type?: string
  captcha_site_key?: string
  captcha_secret_key?: string
  turnstile_site_key?: string
  turnstile_secret_key?: string
  login_captcha?: boolean
  // 站点
  site_name?: string
  site_url?: string
  // 证书提醒
  cert_expire_days?: number
  cert_expire_notice_enabled?: boolean
  cert_expire_notice_days?: string | number
  cert_expire_notice_interval_days?: string | number
  // 邮件
  mail_enabled?: boolean
  mail_type?: number
  mail_host?: string
  mail_port?: number
  mail_secure?: string
  mail_auth?: string
  mail_user?: string
  mail_username?: string
  mail_password?: string
  mail_from?: string
  mail_from_name?: string
  mail_recv?: string
  mail_subject_template?: string
  mail_body_template?: string
  // Telegram
  tgbot_enabled?: boolean
  tgbot_token?: string
  tgbot_chatid?: string
  // Webhook 通用
  webhook_enabled?: boolean
  webhook_url?: string
  // Discord
  discord_enabled?: boolean
  discord_webhook?: string
  // Bark
  bark_enabled?: boolean
  bark_server?: string
  bark_key?: string
  // 企业微信
  wechat_enabled?: boolean
  wechat_webhook?: string
  // 代理
  proxy_enabled?: boolean
  proxy_server?: string
  proxy_port?: number
  proxy_type?: string
  proxy_user?: string
  proxy_password?: string
  // GitHub OAuth（旧版）
  github_mode?: string
  github_client_id?: string
  github_client_secret?: string
  github_app_id?: string
  github_app_private_key?: string
  // OAuth 服务商
  oauth_google_client_id?: string
  oauth_google_client_secret?: string
  oauth_wechat_app_id?: string
  oauth_wechat_app_secret?: string
  oauth_dingtalk_app_key?: string
  oauth_dingtalk_app_secret?: string
  oauth_custom_name?: string
  oauth_custom_client_id?: string
  oauth_custom_client_secret?: string
  oauth_custom_authorize_url?: string
  oauth_custom_token_url?: string
  oauth_custom_userinfo_url?: string
  oauth_custom_scopes?: string
  // 域名过期提醒
  domain_expire_notice_enabled?: boolean
  domain_expire_days?: number | string
  // 证书部署/续期通知
  cert_deploy_notice_enabled?: boolean
  cert_deploy_success_notice_enabled?: boolean
  cert_renew_fail_notice_enabled?: boolean
  // 注册开关
  register_enabled?: boolean
  password_register_enabled?: boolean
  magic_link_login_enabled?: boolean
  // 任意未列出键值（与后端 SysConfig 开放结构一致）
  // 用 any 而非 unknown 以方便 React JSX 直接使用 value={config.foo}
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

export interface CronConfig {
  type?: number
  key?: string
  /** Cron 任务调度表达式分项配置 */
  cron_schedule?: string
  cron_optimize?: string
  cron_cert?: string
  cron_expire?: string
  // eslint-disable-next-line @typescript-eslint/no-explicit-any
  [key: string]: any
}

export interface DashboardStats {
  domains: number
  tasks: number
  certs: number
  deploys: number
  dmonitor_state: number
  dmonitor_active: number
  dmonitor_status_0: number
  dmonitor_status_1: number
  optimizeip_active?: number
  optimizeip_status_1?: number
  optimizeip_status_2?: number
  certorder_status_3: number
  certorder_status_5: number
  certorder_status_6: number
  certorder_status_7: number
  certdeploy_status_0: number
  certdeploy_status_1: number
  certdeploy_status_2: number
}
