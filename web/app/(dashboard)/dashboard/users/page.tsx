'use client'

import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { toast } from 'sonner'
import { userApi, domainApi, quotaApi, User, UserPermission, Domain, Plan, QuotaMap, QuotaOverlayForm, UserQuotaRow, QuotaSource } from '@/lib/api'
import { Users, Plus, Search, RefreshCw, Trash2, Edit, Shield, ShieldCheck, Key, Copy, CheckCircle, XCircle, MoreHorizontal, Lock, Unlock, RotateCcw, FolderKey, Globe, Mail, ShieldOff, Send, AlertCircle, Gauge } from 'lucide-react'
import { TableSkeleton } from '@/components/table-skeleton'
import { EmptyState } from '@/components/empty-state'
import Link from 'next/link'
import { formatDate, cn } from '@/lib/utils'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'

/**
 * 等级徽章映射：后端只以 level >= 2 判定管理员，level 0（自助注册）
 * 与 1（第三方登录自动创建）在权限上等价，故共用普通用户样式；
 * 缺少对应 key 时徽章会落到「未知」兜底分支
 */
const USER_LEVELS: Record<number, { label: string; color: string }> = {
  0: { label: '普通用户', color: 'bg-gray-500' },
  1: { label: '普通用户', color: 'bg-gray-500' },
  2: { label: '管理员', color: 'bg-blue-500' },
}

const PERMISSIONS = [
  { key: 'domain', label: '域名管理' },
  { key: 'monitor', label: '容灾监控' },
  { key: 'cert', label: '证书管理' },
  { key: 'deploy', label: '部署管理' },
  { key: 'user', label: '用户管理' },
  { key: 'log', label: '操作日志' },
  { key: 'system', label: '系统设置' },
]

/**
 * 配额维度：key 与后端 applyQuotaJSON 读取的 JSON 键一一对应
 * 顺序与 quotaDimensionKeys 保持一致，便于两处对照
 */
const QUOTA_DIMENSIONS = [
  { key: 'max_domains', label: '域名' },
  { key: 'max_records', label: '子域名' },
  { key: 'max_accounts', label: 'DNS 账户' },
  { key: 'max_cert_accounts', label: '证书账户' },
  { key: 'max_monitors', label: '容灾监控' },
  { key: 'max_certs', label: 'SSL 证书' },
  { key: 'max_schedules', label: '定时切换' },
  { key: 'max_deploys', label: '证书部署' },
  { key: 'max_optimize_ip', label: '优选 IP' },
] as const

/** 配额来源徽章文案，与后端 planSourceForUser 的三个返回值对应 */
const QUOTA_SOURCE_LABELS: Record<QuotaSource, string> = {
  admin: '个体覆盖',
  plan: '套餐',
  system: '系统默认',
}

/** 上限展示：-1 显示「不限」而非负数，避免误读成「倒欠」 */
function formatQuotaLimit(limit: number): string {
  return limit < 0 ? '不限' : String(limit)
}

export default function UsersPage() {
  const [users, setUsers] = useState<User[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  /** 加载失败提示；非空时列表区显示错误态而非空状态 */
  const [loadError, setLoadError] = useState('')

  // 配额与套餐
  const [quotaRows, setQuotaRows] = useState<UserQuotaRow[]>([])
  const [quotaLoading, setQuotaLoading] = useState(true)
  const [quotaError, setQuotaError] = useState('')
  const [showQuotaDialog, setShowQuotaDialog] = useState(false)
  const [quotaTarget, setQuotaTarget] = useState<UserQuotaRow | null>(null)
  /** 弹窗内编辑中的配额；-1 不限、0 禁止、正数为上限 */
  const [quotaForm, setQuotaForm] = useState<QuotaOverlayForm>({})
  const [quotaRemark, setQuotaRemark] = useState('')
  const [showDefaultQuotaDialog, setShowDefaultQuotaDialog] = useState(false)
  const [defaultQuotaForm, setDefaultQuotaForm] = useState<QuotaMap>({})
  const [plans, setPlans] = useState<Plan[]>([])
  /** 弹窗内选中的套餐 id；空表示不授予 */
  const [quotaPlanId, setQuotaPlanId] = useState('')

  const [showAddDialog, setShowAddDialog] = useState(false)
  const [showEditDialog, setShowEditDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showPermDialog, setShowPermDialog] = useState(false)
  
  const [selectedUser, setSelectedUser] = useState<User | null>(null)
  const [userPermissions, setUserPermissions] = useState<UserPermission[]>([])
  const [allDomains, setAllDomains] = useState<Domain[]>([])
  const [permLoading, setPermLoading] = useState(false)

  const [formData, setFormData] = useState({
    username: '',
    password: '',
    email: '',
    level: '1',
    status: 1,
    is_api: false,
    permissions: [] as string[],
  })
  const [submitting, setSubmitting] = useState(false)

  const [permFormData, setPermFormData] = useState({
    did: '',
    domain: '',
    sub: '',
    read_only: false,
    expire_time: '',
  })

  useEffect(() => {
    loadData()
    loadQuotaData()
  }, [])

  /** 加载配额列表；与用户列表分开加载，配额接口失败不影响用户管理主流程 */
  const loadQuotaData = async () => {
    setQuotaLoading(true)
    try {
      const [quotaRes, planRes] = await Promise.all([
        quotaApi.listUserQuotas(),
        quotaApi.listPlans(),
      ])
      if (quotaRes.code === 0 && quotaRes.data) {
        setQuotaRows(quotaRes.data.list || [])
        setQuotaError('')
      } else {
        setQuotaError(quotaRes.msg || '加载配额失败')
      }
      // 套餐列表只取启用中的：停用套餐后端会拒绝授予
      if (planRes.code === 0 && planRes.data) {
        setPlans((planRes.data.list || []).filter((p) => p.status === 1))
      }
    } catch {
      setQuotaError('加载配额失败')
    } finally {
      setQuotaLoading(false)
    }
  }

  /**
   * 打开个体配额弹窗
   *
   * 表单只预填 overlays（真正属于个体覆盖的维度），未覆盖的维度留空
   *
   * 此前用 row.quotas 预填——那是三层合并后的生效值，其中包含套餐与系统默认
   * 贡献的维度。管理员改一个维度再保存，会把预填的其余维度一并写成个体覆盖，
   * 此后调整套餐时该用户永远停在旧值，而管理员并不知道自己覆盖过它们
   */
  const openQuotaDialog = (row: UserQuotaRow) => {
    setQuotaTarget(row)
    const form: QuotaOverlayForm = {}
    QUOTA_DIMENSIONS.forEach((d) => {
      const overlay = row.overlays?.[d.key]
      if (overlay !== undefined) {
        form[d.key] = overlay
      }
    })
    setQuotaForm(form)
    setQuotaRemark(row.remark || '')
    setQuotaPlanId(row.plan_id || '')
    setShowQuotaDialog(true)
  }

  /* 授予套餐：与保存个体配额分开，两者优先级不同，混在一次请求里会掩盖意图 */
  const handleGrantPlan = async () => {
    if (!quotaTarget || !quotaPlanId) {
      toast.error('请先选择套餐')
      return
    }
    setSubmitting(true)
    try {
      const res = await quotaApi.setUserSubscription(quotaTarget.user_id, { plan_id: quotaPlanId })
      if (res.code === 0) {
        toast.success(res.msg || '已授予套餐')
        loadQuotaData()
      } else {
        toast.error(res.msg || '授予失败')
      }
    } catch {
      toast.error('授予失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleRevokePlan = async () => {
    if (!quotaTarget) return
    setSubmitting(true)
    try {
      const res = await quotaApi.revokeUserSubscription(quotaTarget.user_id)
      if (res.code === 0) {
        toast.success('已撤销套餐')
        setQuotaPlanId('')
        loadQuotaData()
      } else {
        toast.error(res.msg || '撤销失败')
      }
    } catch {
      toast.error('撤销失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleSaveQuota = async () => {
    if (!quotaTarget) return
    setSubmitting(true)
    try {
      /*
       * 只提交被显式填写的维度
       *
       * 未填写的维度若也提交，后端会按原样落库，等于把该维度固化成个体覆盖——
       * 与弹窗里「留空表示继承」的语义正好相反
       */
      const overlays: QuotaMap = {}
      Object.entries(quotaForm).forEach(([k, v]) => {
        if (v !== undefined && v !== null) overlays[k] = v
      })
      const res = await quotaApi.setUserQuota(quotaTarget.user_id, {
        quotas: overlays,
        remark: quotaRemark,
      })
      if (res.code === 0) {
        toast.success('配额已保存')
        setShowQuotaDialog(false)
        loadQuotaData()
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleResetQuota = async () => {
    if (!quotaTarget) return
    setSubmitting(true)
    try {
      const res = await quotaApi.resetUserQuota(quotaTarget.user_id)
      if (res.code === 0) {
        toast.success('已回落到套餐配额')
        setShowQuotaDialog(false)
        loadQuotaData()
      } else {
        toast.error(res.msg || '重置失败')
      }
    } catch {
      toast.error('重置失败')
    } finally {
      setSubmitting(false)
    }
  }

  const openDefaultQuotaDialog = async () => {
    try {
      const res = await quotaApi.getQuotaDefault()
      const existing = res.code === 0 && res.data ? res.data.quotas : {}
      const form: QuotaMap = {}
      QUOTA_DIMENSIONS.forEach((d) => {
        form[d.key] = existing[d.key] ?? -1
      })
      setDefaultQuotaForm(form)
      setShowDefaultQuotaDialog(true)
    } catch {
      toast.error('读取系统默认配额失败')
    }
  }

  const handleSaveDefaultQuota = async () => {
    setSubmitting(true)
    try {
      const res = await quotaApi.updateQuotaDefault({ quotas: defaultQuotaForm })
      if (res.code === 0) {
        toast.success('系统默认配额已保存')
        setShowDefaultQuotaDialog(false)
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存失败')
    } finally {
      setSubmitting(false)
    }
  }

  const loadData = async () => {
    setLoading(true)
    try {
      const res = await userApi.list()
      if (res.code === 0 && res.data) {
        // 后端返回 { total, list } 格式
        const data = res.data as { total: number; list: User[] }
        setUsers(data.list || [])
        setLoadError('')
      } else {
        setLoadError(res.msg || '加载用户列表失败')
        toast.error(res.msg || '加载用户列表失败')
      }
    } catch (error) {
      console.error('Failed to load users:', error)
      setLoadError('加载用户列表失败')
      toast.error('加载用户列表失败')
    } finally {
      setLoading(false)
    }
  }

  const handleAdd = () => {
    setFormData({
      username: '',
      password: '',
      email: '',
      level: '1',
      status: 1,
      is_api: false,
      permissions: [],
    })
    setShowAddDialog(true)
  }

  const handleEdit = (user: User) => {
    setSelectedUser(user)
    setFormData({
      username: user.username,
      password: '',
      email: user.email || '',
      /** 存量 level=0 在下拉里无对应项会让 Radix Select 渲染成空白，归一到 1 仅影响表单展示 */
      level: String(Math.max(1, user.level)),
      status: user.status,
      is_api: user.is_api,
      permissions: user.permissions || [],
    })
    setShowEditDialog(true)
  }

  const handleSubmit = async (isEdit: boolean) => {
    if (!formData.username) {
      toast.error('请输入用户名')
      return
    }
    if (!isEdit && !formData.password) {
      toast.error('请输入密码')
      return
    }

    setSubmitting(true)
    try {
      const data: Partial<User> & { password?: string; permissions?: string[] } = {
        username: formData.username,
        email: formData.email,
        level: parseInt(formData.level),
        status: formData.status,
        is_api: formData.is_api,
        permissions: formData.permissions,
      }
      if (formData.password) {
        data.password = formData.password
      }

      let res
      if (isEdit && selectedUser) {
        res = await userApi.update(selectedUser.id, data)
      } else {
        res = await userApi.create(data as Partial<User> & { password: string })
      }

      if (res.code === 0) {
        toast.success(isEdit ? '修改成功' : '创建成功')
        setShowAddDialog(false)
        setShowEditDialog(false)
        loadData()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = (user: User) => {
    setSelectedUser(user)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!selectedUser) return
    try {
      const res = await userApi.delete(selectedUser.id)
      if (res.code === 0) {
        toast.success('删除成功')
        loadData()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    } finally {
      setShowDeleteDialog(false)
      setSelectedUser(null)
    }
  }

  const handleToggleStatus = async (user: User) => {
    try {
      const newStatus = user.status === 1 ? 0 : 1
      const res = await userApi.update(user.id, { status: newStatus })
      if (res.code === 0) {
        toast.success(newStatus === 1 ? '已启用' : '已禁用')
        loadData()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    }
  }

  const handleCopyApiKey = (apiKey: string) => {
    navigator.clipboard.writeText(apiKey)
    toast.success('API Key 已复制到剪贴板')
  }

  const handleResetApiKey = async (user: User) => {
    if (!user.is_api) {
      toast.error('该用户未启用API访问')
      return
    }
    try {
      const res = await userApi.resetAPIKey(user.id)
      if (res.code === 0) {
        const newKey = res.data?.api_key
        toast.success(`API Key 已重置${newKey ? `: ${newKey.substring(0, 8)}...` : ''}`)
        loadData()
      } else {
        toast.error(res.msg || '重置API Key失败')
      }
    } catch {
      toast.error('重置API Key失败')
    }
  }

  // 发送重置邮件
  const handleSendResetEmail = async (user: User, type: 'password' | 'totp') => {
    if (!user.email) {
      toast.error('该用户未绑定邮箱')
      return
    }
    try {
      const res = await userApi.sendResetEmail(user.id, type)
      if (res.code === 0) {
        toast.success(`${type === 'password' ? '密码' : 'TOTP'}重置邮件已发送`)
      } else {
        toast.error(res.msg || '发送失败')
      }
    } catch {
      toast.error('发送失败')
    }
  }

  // 管理员直接重置用户TOTP
  const handleAdminResetTOTP = async (user: User) => {
    if (!user.totp_open) {
      toast.error('该用户未启用二步验证')
      return
    }
    try {
      const res = await userApi.adminResetTOTP(user.id)
      if (res.code === 0) {
        toast.success('已重置用户的二步验证')
        loadData()
      } else {
        toast.error(res.msg || '重置失败')
      }
    } catch {
      toast.error('重置失败')
    }
  }

  // 打开权限管理弹窗
  const handleOpenPermissions = async (user: User) => {
    setSelectedUser(user)
    setShowPermDialog(true)
    setPermLoading(true)
    try {
      // 加载用户权限和所有域名
      const [permRes, domainRes] = await Promise.all([
        userApi.getPermissions(user.id),
        domainApi.list({ page_size: 1000 }),
      ])
      if (permRes.code === 0 && permRes.data) {
        setUserPermissions(Array.isArray(permRes.data) ? permRes.data : [])
      }
      if (domainRes.code === 0 && domainRes.data) {
        setAllDomains(domainRes.data.list || [])
      }
    } catch {
      toast.error('加载权限数据失败')
    } finally {
      setPermLoading(false)
    }
  }

  // 添加权限
  const handleAddPermission = async () => {
    if (!selectedUser || !permFormData.did) {
      toast.error('请选择域名')
      return
    }
    const domain = allDomains.find(d => d.id.toString() === permFormData.did)
    if (!domain) return

    try {
      const res = await userApi.addPermission(selectedUser.id, {
        did: parseInt(permFormData.did),
        domain: domain.name,
        sub: permFormData.sub,
        read_only: permFormData.read_only,
        expire_time: permFormData.expire_time || undefined,
      })
      if (res.code === 0) {
        toast.success('添加权限成功')
        // 重新加载权限
        const permRes = await userApi.getPermissions(selectedUser.id)
        if (permRes.code === 0 && permRes.data) {
          setUserPermissions(Array.isArray(permRes.data) ? permRes.data : [])
        }
        setPermFormData({ did: '', domain: '', sub: '', read_only: false, expire_time: '' })
      } else {
        toast.error(res.msg || '添加失败')
      }
    } catch {
      toast.error('添加失败')
    }
  }

  // 删除权限
  const handleDeletePermission = async (permId: number) => {
    if (!selectedUser) return
    try {
      const res = await userApi.deletePermission(selectedUser.id, permId)
      if (res.code === 0) {
        toast.success('删除成功')
        setUserPermissions(userPermissions.filter(p => p.id !== permId))
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  const filteredUsers = users.filter(user => {
    return !keyword || user.username.includes(keyword)
  })

  const togglePermission = (perm: string) => {
    setFormData(prev => ({
      ...prev,
      permissions: prev.permissions.includes(perm)
        ? prev.permissions.filter(p => p !== perm)
        : [...prev.permissions, perm]
    }))
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-indigo-500 to-blue-600 flex items-center justify-center shadow-lg shadow-indigo-500/20">
              <Users className="h-5 w-5 text-white" />
            </div>
            用户管理
          </h1>
          <p className="text-muted-foreground mt-1">管理系统用户和权限配置</p>
        </div>
        <Button onClick={handleAdd} className="bg-gradient-to-r from-indigo-600 to-blue-600 hover:from-indigo-500 hover:to-blue-500">
          <Plus className="h-4 w-4 mr-2" />
          添加用户
        </Button>
      </div>

      {/* Stats */}
      <div className="grid gap-4 md:grid-cols-3">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">总用户数</CardTitle>
            <Users className="h-4 w-4 text-muted-foreground" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold">{users.length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">管理员</CardTitle>
            <ShieldCheck className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-blue-600">{users.filter(u => u.level >= 2).length}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">API用户</CardTitle>
            <Key className="h-4 w-4 text-purple-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-purple-600">{users.filter(u => u.is_api).length}</div>
          </CardContent>
        </Card>
      </div>

      {/* User List */}
      <Card>
        <CardHeader>
          <CardTitle>用户列表</CardTitle>
          <CardDescription>查看和管理所有系统用户</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col sm:flex-row gap-4 mb-6">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索用户名..."
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                className="pl-10"
              />
            </div>
            <Button variant="outline" onClick={loadData}>
              <RefreshCw className="h-4 w-4 mr-2" />
              刷新
            </Button>
          </div>

          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>用户名</TableHead>
                  <TableHead>邮箱</TableHead>
                  <TableHead>角色</TableHead>
                  <TableHead>类型</TableHead>
                  <TableHead>API Key</TableHead>
                  <TableHead>二步验证</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>注册时间</TableHead>
                  <TableHead>最后登录</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={10} className="text-center py-8">
                      <RefreshCw className="h-6 w-6 animate-spin mx-auto mb-2 text-muted-foreground" />
                      <span className="text-muted-foreground">加载中...</span>
                    </TableCell>
                  </TableRow>
                ) : loadError ? (
                  <TableRow>
                    <TableCell colSpan={10} className="text-center py-8">
                      <AlertCircle className="h-12 w-12 mx-auto mb-2 text-destructive/60" />
                      <p className="text-muted-foreground mb-3">{loadError}</p>
                      <Button variant="outline" size="sm" onClick={loadData}>
                        重试
                      </Button>
                    </TableCell>
                  </TableRow>
                ) : filteredUsers.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={10} className="text-center py-8">
                      <Users className="h-12 w-12 mx-auto mb-2 text-muted-foreground/50" />
                      <p className="text-muted-foreground">暂无用户</p>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredUsers.map((user) => (
                    <TableRow key={user.id}>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <div className="h-8 w-8 rounded-full bg-gradient-to-br from-indigo-500 to-blue-500 flex items-center justify-center text-white text-sm font-medium">
                            {user.username.charAt(0).toUpperCase()}
                          </div>
                          <span className="font-medium">{user.username}</span>
                        </div>
                      </TableCell>
                      <TableCell>
                        {user.email ? (
                          <span className="text-sm">{user.email}</span>
                        ) : (
                          <span className="text-muted-foreground text-sm">-</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <Badge className={`${USER_LEVELS[user.level]?.color || 'bg-gray-500'} text-white`}>
                          {user.level >= 2 && <Shield className="h-3 w-3 mr-1" />}
                          {USER_LEVELS[user.level]?.label || '未知'}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        {user.is_api ? (
                          <Badge variant="outline" className="text-purple-500 border-purple-500">
                            <Key className="h-3 w-3 mr-1" />
                            API
                          </Badge>
                        ) : (
                          <Badge variant="outline">普通</Badge>
                        )}
                      </TableCell>
                      <TableCell>
                        {user.api_key ? (
                          <div className="flex items-center gap-1">
                            <code className="text-xs bg-muted px-1 py-0.5 rounded">
                              {user.api_key.substring(0, 8)}...
                            </code>
                            <Button size="sm" variant="ghost" className="h-6 w-6 p-0" onClick={() => handleCopyApiKey(user.api_key!)} aria-label="复制 API Key">
                              <Copy className="h-3 w-3" />
                            </Button>
                          </div>
                        ) : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </TableCell>
                      <TableCell>
                        {user.totp_open ? (
                          <CheckCircle className="h-4 w-4 text-green-500" />
                        ) : (
                          <XCircle className="h-4 w-4 text-muted-foreground" />
                        )}
                      </TableCell>
                      <TableCell>
                        <Switch
                          checked={user.status === 1}
                          onCheckedChange={() => handleToggleStatus(user)}
                        />
                      </TableCell>
                      <TableCell>
                        <span className="text-sm text-muted-foreground">{formatDate(user.reg_time)}</span>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm text-muted-foreground">{user.last_time ? formatDate(user.last_time) : '-'}</span>
                      </TableCell>
                      <TableCell className="text-right">
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="sm" className="h-8 w-8 p-0" aria-label="更多操作">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end">
                            <DropdownMenuItem onClick={() => handleEdit(user)}>
                              <Edit className="h-4 w-4 mr-2" />
                              编辑用户
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => handleOpenPermissions(user)}>
                              <FolderKey className="h-4 w-4 mr-2" />
                              域名权限
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => handleToggleStatus(user)}>
                              {user.status === 1 ? (
                                <>
                                  <Lock className="h-4 w-4 mr-2" />
                                  禁用用户
                                </>
                              ) : (
                                <>
                                  <Unlock className="h-4 w-4 mr-2" />
                                  启用用户
                                </>
                              )}
                            </DropdownMenuItem>
                            {user.is_api && (
                              <>
                                <DropdownMenuItem onClick={() => user.api_key && handleCopyApiKey(user.api_key)}>
                                  <Copy className="h-4 w-4 mr-2" />
                                  复制 API Key
                                </DropdownMenuItem>
                                <DropdownMenuItem onClick={() => handleResetApiKey(user)}>
                                  <RotateCcw className="h-4 w-4 mr-2" />
                                  重置 API Key
                                </DropdownMenuItem>
                              </>
                            )}
                            {user.email && (
                              <>
                                <DropdownMenuSeparator />
                                <DropdownMenuItem onClick={() => handleSendResetEmail(user, 'password')}>
                                  <Send className="h-4 w-4 mr-2" />
                                  发送密码重置邮件
                                </DropdownMenuItem>
                                {user.totp_open && (
                                  <DropdownMenuItem onClick={() => handleSendResetEmail(user, 'totp')}>
                                    <Mail className="h-4 w-4 mr-2" />
                                    发送TOTP重置邮件
                                  </DropdownMenuItem>
                                )}
                              </>
                            )}
                            {user.totp_open && (
                              <DropdownMenuItem onClick={() => handleAdminResetTOTP(user)}>
                                <ShieldOff className="h-4 w-4 mr-2" />
                                直接关闭二步验证
                              </DropdownMenuItem>
                            )}
                            <DropdownMenuSeparator />
                            <DropdownMenuItem 
                              className="text-red-600" 
                              onClick={() => handleDelete(user)}
                              disabled={user.level >= 2 && users.filter(u => u.level >= 2).length === 1}
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              删除用户
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* 配额与套餐 */}
      <Card>
        <CardHeader className="flex flex-row items-start justify-between space-y-0">
          <div>
            <CardTitle>配额与套餐</CardTitle>
            <CardDescription>
              三层优先级：个体覆盖 &gt; 套餐订阅 &gt; 系统默认。-1 不限、0 禁止、正数为上限
            </CardDescription>
          </div>
          <Button variant="outline" size="sm" onClick={loadQuotaData} disabled={quotaLoading}>
            <RefreshCw className={cn('h-4 w-4 mr-2', quotaLoading && 'animate-spin')} />
            刷新
          </Button>
        </CardHeader>
        <CardContent className="space-y-4">
          {quotaError ? (
            <EmptyState
              icon={AlertCircle}
              title="加载失败"
              description={quotaError}
              action="重试"
              onAction={loadQuotaData}
            />
          ) : quotaLoading ? (
            <TableSkeleton rows={4} columns={5} />
          ) : (
            <>
              <div className="rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>用户</TableHead>
                      <TableHead>配额来源</TableHead>
                      <TableHead>套餐</TableHead>
                      <TableHead>已用 / 上限</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {quotaRows.length === 0 ? (
                      <TableRow>
                        <TableCell colSpan={5} className="text-center py-8 text-muted-foreground">
                          暂无可管理的配额记录
                        </TableCell>
                      </TableRow>
                    ) : (
                      quotaRows.map((row) => (
                        <TableRow key={row.user_id}>
                          <TableCell className="font-medium">
                            {row.username}
                            {row.level >= 2 && (
                              <Badge className="ml-2 bg-blue-500 text-white">管理员不受限</Badge>
                            )}
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline">{QUOTA_SOURCE_LABELS[row.source] || row.source}</Badge>
                            {row.remark && (
                              <span className="ml-2 text-xs text-muted-foreground">{row.remark}</span>
                            )}
                          </TableCell>
                          <TableCell className="text-muted-foreground">{row.plan_name || '-'}</TableCell>
                          <TableCell className="text-xs">
                            {QUOTA_DIMENSIONS.map((d) => {
                              const used = row.used[d.key] ?? 0
                              const limit = row.quotas[d.key]
                              if (limit === undefined) return null
                              const atLimit = limit >= 0 && used >= limit
                              return (
                                <span
                                  key={d.key}
                                  className={cn(
                                    'inline-block mr-3 mb-1',
                                    atLimit ? 'text-destructive font-medium' : 'text-muted-foreground'
                                  )}
                                >
                                  {d.label} {used}/{formatQuotaLimit(limit)}
                                </span>
                              )
                            })}
                          </TableCell>
                          <TableCell className="text-right">
                            <Button
                              variant="outline"
                              size="sm"
                              onClick={() => openQuotaDialog(row)}
                              disabled={row.level >= 2}
                              aria-label={`设置 ${row.username} 的配额`}
                            >
                              {row.level >= 2 ? '不受限' : '设置配额'}
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))
                    )}
                  </TableBody>
                </Table>
              </div>
              <div className="flex flex-wrap items-center gap-2">
                <Button variant="outline" size="sm" onClick={openDefaultQuotaDialog}>
                  系统默认配额
                </Button>
                <Link href="/dashboard/plans" className="text-sm text-primary hover:underline">
                  管理套餐 →
                </Link>
              </div>
            </>
          )}
        </CardContent>
      </Card>

      {/* Add Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>添加用户</DialogTitle>
            <DialogDescription>创建新的系统用户</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>用户名 *</Label>
              <Input
                placeholder="输入用户名"
                value={formData.username}
                onChange={(e) => setFormData({ ...formData, username: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>密码 *</Label>
              <Input
                type="password"
                placeholder="输入密码"
                value={formData.password}
                onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>邮箱</Label>
              <Input
                type="email"
                placeholder="输入邮箱（用于重置密码）"
                value={formData.email}
                onChange={(e) => setFormData({ ...formData, email: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>角色</Label>
              <Select value={formData.level} onValueChange={(v) => setFormData({ ...formData, level: v })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">普通用户</SelectItem>
                  <SelectItem value="2">管理员</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center space-x-2">
              <Switch
                checked={formData.is_api}
                onCheckedChange={(checked) => setFormData({ ...formData, is_api: checked })}
              />
              <Label>启用 API 访问</Label>
            </div>
            {formData.level === '1' && (
              <div className="space-y-2">
                <Label>权限配置</Label>
                <div className="grid grid-cols-2 gap-2">
                  {PERMISSIONS.map((perm) => (
                    <div key={perm.key} className="flex items-center space-x-2">
                      <Checkbox
                        id={perm.key}
                        checked={formData.permissions.includes(perm.key)}
                        onCheckedChange={() => togglePermission(perm.key)}
                      />
                      <label htmlFor={perm.key} className="text-sm">{perm.label}</label>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddDialog(false)}>取消</Button>
            <Button onClick={() => handleSubmit(false)} disabled={submitting}>
              {submitting ? '创建中...' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog open={showEditDialog} onOpenChange={setShowEditDialog}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>编辑用户</DialogTitle>
            <DialogDescription>修改用户信息和权限</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>用户名 *</Label>
              <Input
                placeholder="输入用户名"
                value={formData.username}
                onChange={(e) => setFormData({ ...formData, username: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>新密码</Label>
              <Input
                type="password"
                placeholder="留空则不修改密码"
                value={formData.password}
                onChange={(e) => setFormData({ ...formData, password: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>邮箱</Label>
              <Input
                type="email"
                placeholder="输入邮箱（用于重置密码）"
                value={formData.email}
                onChange={(e) => setFormData({ ...formData, email: e.target.value })}
              />
            </div>
            <div className="space-y-2">
              <Label>角色</Label>
              <Select value={formData.level} onValueChange={(v) => setFormData({ ...formData, level: v })}>
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="1">普通用户</SelectItem>
                  <SelectItem value="2">管理员</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="flex items-center space-x-2">
              <Switch
                checked={formData.is_api}
                onCheckedChange={(checked) => setFormData({ ...formData, is_api: checked })}
              />
              <Label>启用 API 访问</Label>
            </div>
            {formData.level === '1' && (
              <div className="space-y-2">
                <Label>权限配置</Label>
                <div className="grid grid-cols-2 gap-2">
                  {PERMISSIONS.map((perm) => (
                    <div key={perm.key} className="flex items-center space-x-2">
                      <Checkbox
                        id={`edit-${perm.key}`}
                        checked={formData.permissions.includes(perm.key)}
                        onCheckedChange={() => togglePermission(perm.key)}
                      />
                      <label htmlFor={`edit-${perm.key}`} className="text-sm">{perm.label}</label>
                    </div>
                  ))}
                </div>
              </div>
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowEditDialog(false)}>取消</Button>
            <Button onClick={() => handleSubmit(true)} disabled={submitting}>
              {submitting ? '保存中...' : '保存'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Dialog */}
      <AlertDialog open={showDeleteDialog} onOpenChange={setShowDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除用户 &ldquo;{selectedUser?.username}&rdquo; 吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={confirmDelete} className="bg-red-600 hover:bg-red-700">
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* Permission Dialog */}
      <Dialog open={showPermDialog} onOpenChange={setShowPermDialog}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>域名权限管理</DialogTitle>
            <DialogDescription>管理用户 {selectedUser?.username} 的域名访问权限</DialogDescription>
          </DialogHeader>
          
          {permLoading ? (
            <div className="flex items-center justify-center py-8">
              <RefreshCw className="h-6 w-6 animate-spin" />
            </div>
          ) : (
            <div className="space-y-6">
              {/* 添加权限 */}
              <div className="space-y-4 p-4 border rounded-lg bg-muted/50">
                <Label className="font-medium">添加新权限</Label>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>域名 *</Label>
                    <Select value={permFormData.did} onValueChange={(v) => setPermFormData({ ...permFormData, did: v })}>
                      <SelectTrigger>
                        <SelectValue placeholder="选择域名" />
                      </SelectTrigger>
                      <SelectContent>
                        {allDomains.map((domain) => (
                          <SelectItem key={domain.id} value={domain.id.toString()}>
                            <div className="flex items-center gap-2">
                              <Globe className="h-4 w-4 text-muted-foreground" />
                              {domain.name}
                            </div>
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>子域名限制</Label>
                    <Input
                      placeholder="留空表示所有子域名"
                      value={permFormData.sub}
                      onChange={(e) => setPermFormData({ ...permFormData, sub: e.target.value })}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>过期时间</Label>
                    <Input
                      type="date"
                      value={permFormData.expire_time}
                      onChange={(e) => setPermFormData({ ...permFormData, expire_time: e.target.value })}
                    />
                  </div>
                  <div className="flex items-center space-x-2 mt-6">
                    <Checkbox
                      checked={permFormData.read_only}
                      onCheckedChange={(checked) => setPermFormData({ ...permFormData, read_only: !!checked })}
                    />
                    <Label>只读权限</Label>
                  </div>
                </div>
                <Button onClick={handleAddPermission} className="w-full">
                  <Plus className="h-4 w-4 mr-2" />
                  添加权限
                </Button>
              </div>

              {/* 权限列表 */}
              <div className="space-y-2">
                <Label className="font-medium">已分配权限 ({userPermissions.length})</Label>
                {userPermissions.length === 0 ? (
                  <div className="text-center py-8 text-muted-foreground border rounded-lg">
                    <FolderKey className="h-8 w-8 mx-auto mb-2 opacity-50" />
                    <p>暂无权限，请添加域名权限</p>
                  </div>
                ) : (
                  <div className="space-y-2">
                    {userPermissions.map((perm) => (
                      <div key={perm.id} className="flex items-center justify-between p-3 border rounded-lg">
                        <div className="flex items-center gap-3">
                          <Globe className="h-4 w-4 text-blue-500" />
                          <div>
                            <div className="font-medium">{perm.domain}</div>
                            <div className="text-xs text-muted-foreground">
                              {perm.sub ? `子域名: ${perm.sub}` : '所有子域名'}
                              {perm.read_only && <Badge variant="secondary" className="ml-2">只读</Badge>}
                              {perm.expire_time && <span className="ml-2">过期: {formatDate(perm.expire_time)}</span>}
                            </div>
                          </div>
                        </div>
                        <Button 
                          variant="ghost" 
                          size="sm" 
                          className="text-red-500 hover:text-red-600"
                          onClick={() => handleDeletePermission(perm.id)}
                        >
                          <Trash2 className="h-4 w-4" />
                        </Button>
                      </div>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}
          
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowPermDialog(false)}>关闭</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 个体配额 Dialog */}
      <Dialog open={showQuotaDialog} onOpenChange={setShowQuotaDialog}>
        <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Gauge className="h-5 w-5" />
              个体配额 - {quotaTarget?.username}
            </DialogTitle>
            <DialogDescription>
              表单只显示该用户被单独覆盖的维度，其余留空表示继承套餐或系统默认。
              留 -1 表示不限、0 表示禁止创建。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            {QUOTA_DIMENSIONS.map((d) => {
              /*
               * 空值表示「未覆盖」：该维度回落到套餐或系统默认
               *
               * 与 -1（不限）是两回事。此前表单恒为有值，未覆盖的维度也被写满，
               * 保存即把它们固化成个体覆盖
               */
              const overlay = quotaForm[d.key]
              const inherited = overlay === undefined
              return (
                <div key={d.key} className="flex items-center gap-3">
                  <Label className="w-24 shrink-0 text-sm">{d.label}</Label>
                  <Input
                    type="number"
                    min={-1}
                    value={inherited ? '' : overlay}
                    placeholder="继承"
                    onChange={(e) => {
                      const raw = e.target.value
                      const next = { ...quotaForm }
                      if (raw === '') {
                        delete next[d.key]
                      } else {
                        next[d.key] = Number(raw)
                      }
                      setQuotaForm(next)
                    }}
                    className="w-28"
                  />
                  {inherited ? (
                    <span className="text-xs text-muted-foreground">
                      继承（当前 {quotaTarget?.quotas[d.key] ?? -1}）
                    </span>
                  ) : overlay < 0 ? (
                    <span className="text-xs text-muted-foreground">不限</span>
                  ) : (
                    <span className="text-xs text-muted-foreground">个体覆盖</span>
                  )}
                </div>
              )
            })}
            <div className="space-y-2 pt-2">
              <Label className="text-sm">备注</Label>
              <Input
                value={quotaRemark}
                onChange={(e) => setQuotaRemark(e.target.value)}
                placeholder="可选，例如「临时提额」"
              />
            </div>

            <div className="space-y-2 pt-2 border-t">
              <Label className="text-sm">套餐订阅</Label>
              <p className="text-xs text-muted-foreground">
                个体配额优先于套餐；撤销套餐后优先回落到个体配额，再回落系统默认
              </p>
              <div className="flex flex-wrap items-center gap-2">
                <Select value={quotaPlanId} onValueChange={setQuotaPlanId}>
                  <SelectTrigger className="w-full sm:w-56" aria-label="选择套餐">
                    <SelectValue placeholder="未订阅套餐" />
                  </SelectTrigger>
                  <SelectContent>
                    {plans.length === 0 ? (
                      <SelectItem value="__none__" disabled>
                        暂无启用中的套餐
                      </SelectItem>
                    ) : (
                      plans.map((p) => (
                        <SelectItem key={p.id} value={p.id}>
                          {p.name}
                        </SelectItem>
                      ))
                    )}
                  </SelectContent>
                </Select>
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleGrantPlan}
                  disabled={submitting || !quotaPlanId}
                >
                  授予 / 变更
                </Button>
                {/*
                  撤销按钮的可用条件看 has_subscription 而非 plan_name

                  plan_name 由 planSourceForUser 给出，它在订阅过期时返回空串——
                  用展示字段决定能否操作，会让过期订阅恰好落进「显示未订阅、按钮禁用」
                  的缝里，那条订阅就再也撤不掉了。有订阅记录就该允许撤销
                */}
                <Button
                  variant="outline"
                  size="sm"
                  onClick={handleRevokePlan}
                  disabled={submitting || !quotaTarget?.has_subscription}
                >
                  撤销套餐
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                当前套餐：{quotaTarget?.plan_name || (quotaTarget?.has_subscription ? '已过期' : '未订阅')}
              </p>
            </div>
          </div>
          <DialogFooter className="flex justify-between sm:justify-between">
            <Button variant="outline" onClick={handleResetQuota} disabled={submitting}>
              清除个体配额
            </Button>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setShowQuotaDialog(false)} disabled={submitting}>
                取消
              </Button>
              <Button onClick={handleSaveQuota} disabled={submitting}>
                {submitting && <RefreshCw className="h-4 w-4 mr-2 animate-spin" />}
                保存
              </Button>
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 系统默认配额 Dialog */}
      <Dialog open={showDefaultQuotaDialog} onOpenChange={setShowDefaultQuotaDialog}>
        <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Gauge className="h-5 w-5" />
              系统默认配额
            </DialogTitle>
            <DialogDescription>
              用户既无个体配额也未订阅套餐时的兜底上限。留 -1 表示不限。
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            {QUOTA_DIMENSIONS.map((d) => (
              <div key={d.key} className="flex items-center gap-3">
                <Label className="w-24 shrink-0 text-sm">{d.label}</Label>
                <Input
                  type="number"
                  min={-1}
                  value={defaultQuotaForm[d.key] ?? -1}
                  onChange={(e) => {
                    const raw = e.target.value
                    setDefaultQuotaForm({ ...defaultQuotaForm, [d.key]: raw === '' ? -1 : Number(raw) })
                  }}
                  className="w-28"
                />
                {(defaultQuotaForm[d.key] ?? -1) < 0 && (
                  <span className="text-xs text-muted-foreground">不限</span>
                )}
              </div>
            ))}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDefaultQuotaDialog(false)} disabled={submitting}>
              取消
            </Button>
            <Button onClick={handleSaveDefaultQuota} disabled={submitting}>
              {submitting && <RefreshCw className="h-4 w-4 mr-2 animate-spin" />}
              保存
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
