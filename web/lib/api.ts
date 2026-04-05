const API_BASE = '/api'

export interface ApiResponse<T = unknown> {
  code: number
  msg?: string
  data?: T
}

class ApiClient {
  private token: string | null = null

  constructor() {
    if (typeof window !== 'undefined') {
      this.token = localStorage.getItem('token')
    }
  }

  setToken(token: string | null) {
    this.token = token
    if (typeof window !== 'undefined') {
      if (token) {
        localStorage.setItem('token', token)
      } else {
        localStorage.removeItem('token')
      }
    }
  }

  getToken() {
    return this.token
  }

  private async request<T>(
    method: string,
    url: string,
    data?: unknown,
    options?: RequestInit
  ): Promise<ApiResponse<T>> {
    const headers: Record<string, string> = {
      'Content-Type': 'application/json',
    }

    if (this.token) {
      headers['Authorization'] = `Bearer ${this.token}`
    }

    const config: RequestInit = {
      method,
      headers,
      ...options,
    }

    if (data && method !== 'GET') {
      config.body = JSON.stringify(data)
    }

    const response = await fetch(`${API_BASE}${url}`, config)
    
    if (response.status === 401) {
      this.setToken(null)
      if (typeof window !== 'undefined' && !window.location.pathname.includes('/login')) {
        window.location.href = '/login'
      }
      throw new Error('未登录或登录已过期')
    }

    const result = await response.json()
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

  async post<T>(url: string, data?: unknown) {
    return this.request<T>('POST', url, data)
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

// Auth APIs
export const authApi = {
  login: (data: { username: string; password: string; captcha_id?: string; captcha?: string; totp_code?: string }) =>
    api.post<{ token: string; user: User }>('/login', data),
  logout: () => api.post('/logout'),
  getCaptcha: () => api.get<{ captcha_id: string; captcha_image: string }>('/auth/captcha'),
  getConfig: () => api.get<{ captcha_enabled: boolean; totp_enabled: boolean }>('/auth/config'),
  getUserInfo: () => api.get<User>('/user/info'),
  changePassword: (data: { old_password: string; new_password: string }) =>
    api.post('/user/password', data),
  getInstallStatus: () => api.get<{ installed: boolean }>('/install/status'),
  install: (data: { username: string; password: string }) => api.post('/install', data),
}

// Account APIs
export const accountApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string }) =>
    api.get<{ total: number; list: Account[] }>('/accounts', params),
  create: (data: Partial<Account>) => api.post<Account>('/accounts', data),
  update: (id: number, data: Partial<Account>) => api.put<Account>(`/accounts/${id}`, data),
  delete: (id: number) => api.delete(`/accounts/${id}`),
  check: (id: number) => api.post(`/accounts/${id}/check`),
  getDomainList: (id: number, params?: { keyword?: string; page?: number; page_size?: number }) =>
    api.get<{ total: number; list: DomainItem[] }>(`/accounts/${id}/domains`, params),
}

// Domain APIs
export const domainApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; aid?: number; status?: string }) =>
    api.get<{ total: number; list: Domain[] }>('/domains', params),
  create: (data: { aid: number; name: string; third_id: string; method?: number }) =>
    api.post<Domain>('/domains', data),
  sync: (data: { aid: number; domains: { name: string; id: string; record_count: number }[] }) =>
    api.post('/domains/sync', data),
  update: (id: number, data: Partial<Domain>) => api.put<Domain>(`/domains/${id}`, data),
  delete: (id: number) => api.delete(`/domains/${id}`),
  batchAction: (data: { ids: number[]; action: string; [key: string]: unknown }) =>
    api.post('/domains/batch', data),
  updateExpire: (id: number) => api.post(`/domains/${id}/update-expire`),
  batchUpdateExpire: (ids: number[]) => api.post('/domains/batch/update-expire', { ids }),
  getRecords: (id: number, params?: { page?: number; page_size?: number; keyword?: string; type?: string; line?: string }) =>
    api.get<{ total: number; list: DNSRecord[] }>(`/domains/${id}/records`, params),
  createRecord: (id: number, data: { name: string; type: string; value: string; line?: string; ttl?: number; mx?: number; remark?: string }) =>
    api.post<DNSRecord>(`/domains/${id}/records`, data),
  updateRecord: (domainId: number, recordId: string, data: { name: string; type: string; value: string; line?: string; ttl?: number; mx?: number; remark?: string }) =>
    api.put<DNSRecord>(`/domains/${domainId}/records/${recordId}`, data),
  deleteRecord: (domainId: number, recordId: string) =>
    api.delete(`/domains/${domainId}/records/${recordId}`),
  setRecordStatus: (domainId: number, recordId: string, status: string) =>
    api.post(`/domains/${domainId}/records/${recordId}/status`, { status }),
  getLines: (id: number) => api.get<RecordLine[]>(`/domains/${id}/lines`),
  batchAddRecords: (id: number, data: { records: string; type?: string; line?: string; ttl?: number }) =>
    api.post(`/domains/${id}/records/batch`, data),
  batchEditRecords: (id: number, data: { record_ids: string[]; action: string; [key: string]: unknown }) =>
    api.put(`/domains/${id}/records/batch`, data),
  batchActionRecords: (id: number, data: { record_ids: string[]; action: string }) =>
    api.post(`/domains/${id}/records/batch/action`, data),
  queryWhois: (id: number) => api.post<WhoisInfo>(`/domains/${id}/whois`),
  getLogs: (id: number, params?: { page?: number; page_size?: number }) =>
    api.get<{ total: number; list: DomainLog[] }>(`/domains/${id}/logs`, params),
  getQuickLoginURL: (id: number) => api.get<{ url: string }>(`/domains/${id}/loginurl`),
}

// Monitor APIs
export const monitorApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; status?: number }) =>
    api.get<{ total: number; list: MonitorTask[] }>('/monitor/tasks', params),
  create: (data: Partial<MonitorTask>) => api.post<MonitorTask>('/monitor/tasks', data),
  update: (id: number, data: Partial<MonitorTask>) => api.put<MonitorTask>(`/monitor/tasks/${id}`, data),
  delete: (id: number) => api.delete(`/monitor/tasks/${id}`),
  toggle: (id: number, active: boolean) => api.post(`/monitor/tasks/${id}/toggle`, { active }),
  switch: (id: number) => api.post(`/monitor/tasks/${id}/switch`),
  getLogs: (id: number, params?: { page?: number; page_size?: number; action?: number }) =>
    api.get<{ total: number; list: MonitorLog[] }>(`/monitor/tasks/${id}/logs`, params),
  getOverview: () => api.get<MonitorOverview>('/monitor/overview'),
  batchCreate: (data: { tasks: Partial<MonitorTask>[] }) => api.post('/monitor/tasks/batch', data),
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
  updateAccount: (id: number, data: Partial<CertAccount>) => api.put<CertAccount>(`/cert/accounts/${id}`, data),
  deleteAccount: (id: number) => api.delete(`/cert/accounts/${id}`),
  
  // Orders
  getOrders: (params?: { page?: number; page_size?: number; keyword?: string; aid?: number; status?: number }) =>
    api.get<{ total: number; list: CertOrder[] }>('/cert/orders', params),
  createOrder: (data: Partial<CertOrder> & { domains: string[] }) => api.post<CertOrder>('/cert/orders', data),
  processOrder: (id: number, reset?: boolean) => api.post(`/cert/orders/${id}/process`, { reset }),
  deleteOrder: (id: number) => api.delete(`/cert/orders/${id}`),
  getOrderLog: (id: number) => api.get<{ log: string }>(`/cert/orders/${id}/log`),
  getOrderDetail: (id: number) => api.get<CertOrder>(`/cert/orders/${id}/detail`),
  downloadOrder: (id: number, format: string) => api.get<{ content: string }>(`/cert/orders/${id}/download`, { format }),
  toggleOrderAuto: (id: number, is_auto: boolean) => api.post(`/cert/orders/${id}/auto`, { is_auto }),
  
  // Deploys
  getDeploys: (params?: { page?: number; page_size?: number; keyword?: string; aid?: number; status?: number }) =>
    api.get<{ total: number; list: CertDeploy[] }>('/cert/deploys', params),
  createDeploy: (data: { account_id: number; order_id: number; config?: Record<string, string>; remark?: string }) => 
    api.post<CertDeploy>('/cert/deploys', data),
  updateDeploy: (id: number, data: Partial<CertDeploy> | { account_id?: number; order_id?: number; config?: Record<string, string>; remark?: string; active?: boolean }) => 
    api.put<CertDeploy>(`/cert/deploys/${id}`, data),
  deleteDeploy: (id: number) => api.delete(`/cert/deploys/${id}`),
  processDeploy: (id: number, reset?: boolean) => api.post(`/cert/deploys/${id}/process`, { reset }),
  
  // CNAMEs
  getCNAMEs: (params?: { page?: number; page_size?: number }) =>
    api.get<{ total: number; list: CertCNAME[] }>('/cert/cnames', params),
  createCNAME: (data: Partial<CertCNAME>) => api.post<CertCNAME>('/cert/cnames', data),
  deleteCNAME: (id: number) => api.delete(`/cert/cnames/${id}`),
  verifyCNAME: (id: number) => api.post<{ status: number }>(`/cert/cnames/${id}/verify`),
  
  // Providers - 返回 {cert: {...}, deploy: {...}} 格式
  getProviders: () => api.get<{ cert: Record<string, CertProviderConfig>; deploy: Record<string, CertProviderConfig> }>('/cert/providers'),
}

// User APIs
export const userApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string }) =>
    api.get<{ total: number; list: User[] }>('/users', params),
  create: (data: Partial<User> & { password: string; permissions?: string[] }) =>
    api.post<User>('/users', data),
  update: (id: number, data: Partial<User> & { password?: string; permissions?: string[] }) =>
    api.put<User>(`/users/${id}`, data),
  delete: (id: number) => api.delete(`/users/${id}`),
  getPermissions: (id: number) => api.get<UserPermission[]>(`/users/${id}/permissions`),
  addPermission: (id: number, data: Partial<UserPermission>) => api.post(`/users/${id}/permissions`, data),
  updatePermission: (id: number, permId: number, data: Partial<UserPermission>) => 
    api.put(`/users/${id}/permissions/${permId}`, data),
  deletePermission: (id: number, permId: number) => api.delete(`/users/${id}/permissions/${permId}`),
  resetAPIKey: (id: number) => api.post<{ api_key: string }>(`/users/${id}/reset-apikey`),
  sendResetEmail: (id: number, type: 'password' | 'totp') => api.post(`/users/${id}/send-reset`, { type }),
  adminResetTOTP: (id: number) => api.post(`/users/${id}/reset-totp`),
}

// Log APIs
export const logApi = {
  list: (params?: { page?: number; page_size?: number; keyword?: string; domain?: string; uid?: number }) =>
    api.get<{ total: number; list: OperationLog[] }>('/logs', params),
}

// System APIs
export const systemApi = {
  getConfig: () => api.get<SystemConfig>('/system/config'),
  updateConfig: (data: Partial<SystemConfig>) => api.post('/system/config', data),
  testMail: () => api.post('/system/mail/test'),
  testTelegram: () => api.post('/system/telegram/test'),
  testWebhook: () => api.post('/system/webhook/test'),
  testProxy: (data: { server: string; port: number; type: string; user?: string; password?: string }) =>
    api.post('/system/proxy/test', data),
  clearCache: () => api.post('/system/cache/clear'),
  getTaskStatus: () => api.get<{ running: boolean; last_run: string; error?: string }>('/system/task/status'),
  getCronConfig: () => api.get<CronConfig>('/system/cron'),
  updateCronConfig: (data: CronConfig) => api.post('/system/cron', data),
  getDNSProviders: () => api.get<DNSProvider[]>('/dns/providers'),
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
  disable: () => api.post('/user/totp/disable'),
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

export interface MonitorTask {
  id: number
  did: number
  rr: string
  record_id: string
  type: number
  main_value: string
  backup_value?: string
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
  active: boolean
  record_info?: string
  domain?: string
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

export interface SystemConfig {
  captcha_enabled?: boolean
  captcha_type?: string
  mail_enabled?: boolean
  mail_type?: number
  mail_host?: string
  mail_port?: number
  mail_user?: string
  mail_password?: string
  mail_from?: string
  mail_recv?: string
  tgbot_enabled?: boolean
  tgbot_token?: string
  tgbot_chatid?: string
  webhook_enabled?: boolean
  webhook_url?: string
  proxy_enabled?: boolean
  proxy_server?: string
  proxy_port?: number
  proxy_type?: string
  proxy_user?: string
  proxy_password?: string
}

export interface CronConfig {
  type: number
  key?: string
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
  certorder_status_3: number
  certorder_status_5: number
  certorder_status_6: number
  certorder_status_7: number
  certdeploy_status_0: number
  certdeploy_status_1: number
  certdeploy_status_2: number
}
