'use client'

import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { toast } from 'sonner'
import { logApi, userApi, OperationLog, User } from '@/lib/api'
import { ScrollText, Search, RefreshCw, Eye, Calendar, User as UserIcon, Globe, Activity, Trash2 } from 'lucide-react'
import { TableSkeleton } from '@/components/table-skeleton'
import { EmptyState } from '@/components/empty-state'
import { formatDate } from '@/lib/utils'

const ACTION_COLORS: Record<string, string> = {
  login: 'bg-green-500',
  logout: 'bg-gray-500',
  create: 'bg-blue-500',
  update: 'bg-yellow-500',
  delete: 'bg-red-500',
  add: 'bg-blue-500',
  edit: 'bg-yellow-500',
  del: 'bg-red-500',
  sync: 'bg-purple-500',
  process: 'bg-cyan-500',
  deploy: 'bg-violet-500',
  toggle: 'bg-orange-500',
  switch: 'bg-pink-500',
  reset: 'bg-amber-500',
  batch: 'bg-indigo-500',
  check: 'bg-teal-500',
  enable: 'bg-emerald-500',
  disable: 'bg-slate-500',
  clean: 'bg-rose-500',
  clear: 'bg-rose-500',
  test: 'bg-sky-500',
  set: 'bg-yellow-500',
  send: 'bg-blue-400',
  auto: 'bg-violet-400',
  download: 'bg-cyan-400',
  verify: 'bg-teal-400',
}

const ACTION_NAMES: Record<string, string> = {
  // ==================== 认证 ====================
  'login': '用户登录',
  'logout': '用户登出',
  'github_login': 'GitHub登录',
  'enable_totp': '启用二步验证',
  'disable_totp': '禁用二步验证',
  'reset_password': '重置密码',
  'reset_totp': '重置二步验证',

  // ==================== DNS账户管理 ====================
  'create_account': '创建DNS账户',
  'update_account': '更新DNS账户',
  'delete_account': '删除DNS账户',
  'check_account': '检测DNS账户',
  // 自动审计路径格式
  'accounts_create': '创建DNS账户',
  'accounts_update': '更新DNS账户',
  'accounts_delete': '删除DNS账户',
  'accounts_check': '检测DNS账户',
  'accounts_domains': '获取账户域名',

  // ==================== 域名管理 ====================
  'delete_domain': '删除域名',
  'update_domain': '更新域名',
  'domains_create': '添加域名',
  'domains_sync': '同步域名',
  'domains_delete': '删除域名',
  'domains_update': '更新域名',
  'domains_batch': '批量操作域名',
  'domains_update-expire': '更新域名到期时间',
  'domains_batch_update-expire': '批量更新到期时间',
  'domains_loginurl': '获取快捷登录',

  // ==================== DNS记录管理 ====================
  'add_record': '添加DNS记录',
  'update_record': '更新DNS记录',
  'delete_record': '删除DNS记录',
  'set_record_status': '修改记录状态',
  'batch_add_records': '批量添加记录',
  'batch_edit_records': '批量编辑记录',
  'batch_action_records': '批量操作记录',
  'domains_records_create': '添加DNS记录',
  'domains_records_update': '更新DNS记录',
  'domains_records_delete': '删除DNS记录',
  'domains_records_status': '修改记录状态',
  'domains_records_batch_add': '批量添加记录',
  'domains_records_batch_edit': '批量编辑记录',
  'domains_records_batch_action': '批量操作记录',

  // ==================== 容灾监控 ====================
  'create_monitor_task': '创建监控任务',
  'update_monitor_task': '更新监控任务',
  'delete_monitor_task': '删除监控任务',
  'toggle_monitor_task': '切换监控开关',
  'switch_monitor_task': '手动切换监控',
  'batch_create_monitor_tasks': '批量创建监控',
  'auto_create_monitor': '自动创建监控',
  'monitor_tasks_create': '创建监控任务',
  'monitor_tasks_update': '更新监控任务',
  'monitor_tasks_delete': '删除监控任务',
  'monitor_tasks_toggle': '切换监控开关',
  'monitor_tasks_switch': '手动切换监控',
  'monitor_tasks_batch': '批量创建监控',
  'monitor_tasks_auto-create': '自动创建监控',
  'monitor_tasks_lookup': '解析域名记录',
  'monitor_tasks_resolve-status': '查询解析状态',

  // ==================== 证书账户 ====================
  'create_cert_account': '创建证书账户',
  'update_cert_account': '更新证书账户',
  'delete_cert_account': '删除证书账户',
  'cert_accounts_create': '创建证书账户',
  'cert_accounts_update': '更新证书账户',
  'cert_accounts_delete': '删除证书账户',

  // ==================== 证书订单 ====================
  'create_cert_order': '创建证书订单',
  'delete_cert_order': '删除证书订单',
  'cert_orders_create': '创建证书订单',
  'cert_orders_process': '申请/续期证书',
  'cert_orders_delete': '删除证书订单',
  'cert_orders_download': '下载证书',
  'cert_orders_auto': '切换自动续期',

  // ==================== 证书部署账户 ====================
  'create_deploy_account': '创建部署账户',
  'update_deploy_account': '更新部署账户',
  'delete_deploy_account': '删除部署账户',
  'cert_deploy-account_add': '创建部署账户',
  'cert_deploy-account_edit': '编辑部署账户',
  'cert_deploy-account_delete': '删除部署账户',
  'cert_deploy-account_check': '检测部署账户',

  // ==================== 证书部署任务 ====================
  'create_deploy_task': '创建部署任务',
  'update_deploy_task': '更新部署任务',
  'delete_deploy_task': '删除部署任务',
  'process_deploy': '执行部署',
  'create_cert_deploy': '创建部署任务',
  'delete_cert_deploy': '删除部署任务',
  'batch_delete_deploy': '批量删除部署',
  'cert_deploy_add': '创建部署任务',
  'cert_deploy_edit': '编辑部署任务',
  'cert_deploy_delete': '删除部署任务',
  'cert_deploy_toggle': '切换部署开关',
  'cert_deploy_process': '执行部署',
  'cert_deploy_reset': '重置部署任务',
  'cert_deploy_batch': '批量操作部署',

  // ==================== CNAME代理 ====================
  'create_cert_cname': '创建CNAME代理',
  'delete_cert_cname': '删除CNAME代理',
  'cert_cnames_create': '创建CNAME代理',
  'cert_cnames_delete': '删除CNAME代理',
  'cert_cnames_verify': '验证CNAME代理',

  // ==================== 用户管理 ====================
  'create_user': '创建用户',
  'update_user': '更新用户',
  'delete_user': '删除用户',
  'reset_apikey': '重置API Key',
  'reset_user_password': '重置用户密码',
  'add_user_permission': '添加用户权限',
  'delete_user_permission': '删除用户权限',
  'users_create': '创建用户',
  'users_update': '更新用户',
  'users_delete': '删除用户',
  'users_permissions_add': '添加用户权限',
  'users_permissions_update': '更新用户权限',
  'users_permissions_delete': '删除用户权限',
  'users_reset-apikey': '重置API Key',
  'users_send-reset': '发送重置邮件',
  'users_reset-totp': '重置二步验证',
  'users_reset-password': '重置用户密码',

  // ==================== 系统管理 ====================
  'update_system_config': '更新系统配置',
  'update_cron_config': '更新定时任务配置',
  'clear_cache': '清除缓存',
  'clean_logs': '清空日志',
  'system_config_update': '更新系统配置',
  'system_cache_clear': '清除缓存',
  'system_cron': '更新定时任务',
  'system_mail_test': '测试邮件通知',
  'system_telegram_test': '测试Telegram通知',
  'system_webhook_test': '测试Webhook通知',
  'system_discord_test': '测试Discord通知',
  'system_bark_test': '测试Bark推送',
  'system_wechat_test': '测试企业微信通知',
  'system_proxy_test': '测试代理连接',
  'system_info': '查看系统信息',
  'logs_clean': '清空操作日志',
  'request-logs_clean': '清空请求日志',

  // ==================== TOTP ====================
  'user_totp_enable': '启用二步验证',
  'user_totp_verify': '验证二步验证',
  'user_totp_disable': '禁用二步验证',
  'user_password': '修改密码',

  // ==================== 只读操作（旧日志兼容） ====================
  'user_info': '查看用户信息',
  'dashboard_stats': '查看仪表盘',
  'system_config_get': '查看系统配置',
  'system_task_status': '查看任务状态',
  'system_cron_get': '查看定时配置',
  'dns_providers': '查看DNS提供商',
  'user_totp_status': '查看二步验证状态',
  'accounts_list': '查看账户列表',
  'accounts_detail': '查看账户详情',
  'domains_list': '查看域名列表',
  'domains_records_list': '查看解析记录',
  'domains_records_lines': '查看线路列表',
  'domains_whois': '查询WHOIS',
  'domains_logs': '查看域名日志',
  'cert_accounts_list': '查看证书账户',
  'cert_orders_list': '查看证书订单',
  'cert_orders_detail': '查看订单详情',
  'cert_orders_log': '查看订单日志',
  'cert_deploy-account_list': '查看部署账户',
  'cert_deploy-account_detail': '查看部署账户详情',
  'cert_deploy_list': '查看部署任务',
  'cert_deploy_detail': '查看部署详情',
  'cert_deploy_log': '查看部署日志',
  'cert_deploy-types': '查看部署类型',
  'cert_cnames_list': '查看CNAME列表',
  'cert_providers': '查看证书提供商',
  'monitor_tasks_list': '查看监控任务',
  'monitor_tasks_logs': '查看监控日志',
  'monitor_tasks_history': '查看监控历史',
  'monitor_tasks_uptime': '查看可用率',
  'monitor_overview': '查看监控概览',
  'monitor_status': '查看监控状态',
  'users_list': '查看用户列表',
  'users_permissions_list': '查看用户权限',
  'logs_list': '查看操作日志',
  'logs_detail': '查看日志详情',
  'request-logs_list': '查看请求日志',
  'request-logs_stats': '查看请求统计',
  'request-logs_request': '查看请求详情',
  'request-logs_error': '查看错误详情',
}

const ACTION_OPTIONS = [
  { value: 'all', label: '全部操作' },
  { value: 'login', label: '用户登录' },
  { value: 'account', label: 'DNS账户' },
  { value: 'domain', label: '域名管理' },
  { value: 'record', label: 'DNS记录' },
  { value: 'monitor', label: '容灾监控' },
  { value: 'cert', label: '证书管理' },
  { value: 'deploy', label: '证书部署' },
  { value: 'user', label: '用户管理' },
  { value: 'system', label: '系统操作' },
  { value: 'totp', label: '二步验证' },
]

export default function LogsPage() {
  const [logs, setLogs] = useState<OperationLog[]>([])
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [domain, setDomain] = useState('')
  const [userId, setUserId] = useState<string>('all')
  const [actionFilter, setActionFilter] = useState<string>('all')
  const [page, setPage] = useState(1)
  const [total, setTotal] = useState(0)
  const pageSize = 20

  const [showDetailDialog, setShowDetailDialog] = useState(false)
  const [selectedLog, setSelectedLog] = useState<OperationLog | null>(null)
  const [cleanDialogOpen, setCleanDialogOpen] = useState(false)

  useEffect(() => {
    loadUsers()
  }, [])

  useEffect(() => {
    loadData()
  // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [page, userId, actionFilter])

  const loadUsers = async () => {
    try {
      const res = await userApi.list()
      if (res.code === 0 && res.data) {
        const d = res.data as User[] | { list?: User[] }
        setUsers(Array.isArray(d) ? d : d.list || [])
      }
    } catch {
      /* 忽略加载失败 */
    }
  }

  const handleCleanLogs = async () => {
    setCleanDialogOpen(false)
    try {
      const res = await logApi.clean(0)
      if (res.code === 0) {
        toast.success(res.msg || '日志已清空')
        loadData()
      } else {
        toast.error(res.msg || '清空失败')
      }
    } catch {
      toast.error('清空失败')
    }
  }

  const loadData = async () => {
    setLoading(true)
    try {
      const params: Record<string, string | number | undefined> = {
        page,
        page_size: pageSize,
      }
      if (keyword) params.keyword = keyword
      if (domain) params.domain = domain
      if (userId && userId !== 'all') params.uid = userId
      if (actionFilter && actionFilter !== 'all') params.action = actionFilter

      const res = await logApi.list(params)
      if (res.code === 0 && res.data) {
        const data = res.data as { total: number; list: OperationLog[] }
        setLogs(data.list || [])
        setTotal(data.total || 0)
      }
    } catch {
      toast.error('加载日志失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSearch = () => {
    setPage(1)
    loadData()
  }

  const handleViewDetail = (log: OperationLog) => {
    setSelectedLog(log)
    setShowDetailDialog(true)
  }

  const getActionBadge = (action: string) => {
    // 尝试从 action 中提取关键动词来匹配颜色
    const lowerAction = action.toLowerCase()
    let color = 'bg-gray-500'
    // 按优先级匹配颜色
    const colorKeywords = [
      'delete', 'clean', 'clear', 'login', 'create', 'add', 'update', 'edit',
      'toggle', 'switch', 'reset', 'batch', 'check', 'verify', 'enable',
      'disable', 'process', 'deploy', 'sync', 'test', 'send', 'download',
      'auto', 'set',
    ]
    for (const keyword of colorKeywords) {
      if (lowerAction.includes(keyword)) {
        color = ACTION_COLORS[keyword] || 'bg-gray-500'
        break
      }
    }
    const label = ACTION_NAMES[action] || action
    return (
      <Badge className={`${color} text-white`}>
        {label}
      </Badge>
    )
  }

  const formatLogData = (data: string | undefined) => {
    if (!data) return null
    try {
      const parsed = JSON.parse(data)
      return JSON.stringify(parsed, null, 2)
    } catch {
      return data
    }
  }

  const totalPages = Math.ceil(total / pageSize)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-amber-500 to-orange-600 flex items-center justify-center shadow-lg shadow-amber-500/20">
              <ScrollText className="h-5 w-5 text-white" />
            </div>
            操作日志
          </h1>
          <p className="text-muted-foreground mt-1">查看系统操作记录和审计日志</p>
        </div>
        <Button variant="outline" onClick={() => setCleanDialogOpen(true)}>
          <Trash2 className="h-4 w-4 mr-2" />
          清空日志
        </Button>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">总日志数</CardTitle>
            <ScrollText className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{total}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">今日操作</CardTitle>
            <Calendar className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">
              {logs.filter(l => {
                const today = new Date().toDateString()
                return new Date(l.created_at).toDateString() === today
              }).length}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">活跃用户</CardTitle>
            <UserIcon className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600 dark:text-green-400">
              {new Set(logs.map(l => l.uid)).size}
            </div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">涉及域名</CardTitle>
            <Globe className="h-4 w-4 text-purple-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-purple-600 dark:text-purple-400">
              {new Set(logs.filter(l => l.domain).map(l => l.domain)).size}
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Logs List */}
      <Card>
        <CardHeader>
          <CardTitle>日志列表</CardTitle>
          <CardDescription>查看所有系统操作记录</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col sm:flex-row gap-4 mb-6">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索操作内容..."
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                onKeyDown={(e) => e.key === 'Enter' && handleSearch()}
                className="pl-10"
              />
            </div>
            <Input
              placeholder="筛选域名..."
              value={domain}
              onChange={(e) => setDomain(e.target.value)}
              className="w-[180px]"
            />
            <Select value={userId} onValueChange={(v) => { setUserId(v); setPage(1) }}>
              <SelectTrigger className="w-[150px]">
                <SelectValue placeholder="选择用户" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部用户</SelectItem>
                {users.map((user) => (
                  <SelectItem key={user.id} value={user.id.toString()}>
                    {user.username}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={actionFilter} onValueChange={(v) => { setActionFilter(v); setPage(1) }}>
              <SelectTrigger className="w-[150px]">
                <SelectValue placeholder="操作类型" />
              </SelectTrigger>
              <SelectContent>
                {ACTION_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value}>
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Button variant="outline" onClick={handleSearch}>
              <Search className="h-4 w-4 mr-2" />
              搜索
            </Button>
            <Button variant="outline" onClick={loadData}>
              <RefreshCw className="h-4 w-4 mr-2" />
              刷新
            </Button>
          </div>

          {loading ? (
            <TableSkeleton rows={5} columns={6} />
          ) : logs.length === 0 ? (
            <EmptyState
              icon={ScrollText}
              title="暂无日志记录"
              description="还没有任何操作日志"
            />
          ) : (
          <div className="rounded-md border overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[80px]">ID</TableHead>
                  <TableHead>用户</TableHead>
                  <TableHead>操作</TableHead>
                  <TableHead>域名</TableHead>
                  <TableHead>时间</TableHead>
                  <TableHead className="text-right">详情</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {logs.map((log) => (
                    <TableRow key={log.id}>
                      <TableCell>
                        <span className="text-muted-foreground">#{log.id}</span>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <div className="h-6 w-6 rounded-full bg-gradient-to-br from-indigo-500 to-blue-500 flex items-center justify-center text-white text-xs font-medium">
                            {(log.username || 'U').charAt(0).toUpperCase()}
                          </div>
                          <span className="text-sm">{log.username || `用户 #${log.uid}`}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        {getActionBadge(log.action)}
                      </TableCell>
                      <TableCell>
                        {log.domain ? (
                          <span className="text-sm font-mono bg-muted px-2 py-0.5 rounded">{log.domain}</span>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <span className="text-sm text-muted-foreground">{formatDate(log.created_at)}</span>
                      </TableCell>
                      <TableCell className="text-right">
                        <Button size="sm" variant="ghost" onClick={() => handleViewDetail(log)}>
                          <Eye className="h-4 w-4" />
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </div>
          )}

          {/* Pagination */}
          {totalPages > 1 && (
            <div className="flex flex-wrap items-center justify-between gap-2 mt-4">
              <div className="text-sm text-muted-foreground">
                共 {total} 条记录，第 {page} / {totalPages} 页
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page === 1}
                  onClick={() => setPage(page - 1)}
                >
                  上一页
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page === totalPages}
                  onClick={() => setPage(page + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* Detail Dialog */}
      <Dialog open={showDetailDialog} onOpenChange={setShowDetailDialog}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Activity className="h-5 w-5" />
              日志详情
            </DialogTitle>
            <DialogDescription>查看操作日志的详细信息</DialogDescription>
          </DialogHeader>
          {selectedLog && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <label className="text-sm text-muted-foreground">日志 ID</label>
                  <p className="font-mono">#{selectedLog.id}</p>
                </div>
                <div className="space-y-1">
                  <label className="text-sm text-muted-foreground">操作用户</label>
                  <p>{selectedLog.username || `用户 #${selectedLog.uid}`}</p>
                </div>
                <div className="space-y-1">
                  <label className="text-sm text-muted-foreground">操作类型</label>
                  <div>{getActionBadge(selectedLog.action)}</div>
                </div>
                <div className="space-y-1">
                  <label className="text-sm text-muted-foreground">操作时间</label>
                  <p>{formatDate(selectedLog.created_at)}</p>
                </div>
                {selectedLog.domain && (
                  <div className="space-y-1 col-span-2">
                    <label className="text-sm text-muted-foreground">关联域名</label>
                    <p className="font-mono bg-muted px-2 py-1 rounded inline-block">{selectedLog.domain}</p>
                  </div>
                )}
              </div>
              {selectedLog.data && (
                <div className="space-y-2">
                  <label className="text-sm text-muted-foreground">操作数据</label>
                  <pre className="p-4 bg-muted rounded-lg overflow-auto text-xs max-h-[300px] font-mono">
                    {formatLogData(selectedLog.data)}
                  </pre>
                </div>
              )}
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDetailDialog(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      <AlertDialog open={cleanDialogOpen} onOpenChange={setCleanDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认清空日志</AlertDialogTitle>
            <AlertDialogDescription>确定要清空所有操作日志吗？此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleCleanLogs} className="bg-red-600 hover:bg-red-700">确定清空</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
