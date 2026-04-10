'use client'

import { useState, useEffect, useMemo, useRef } from 'react'
import { useSearchParams, useRouter, usePathname } from 'next/navigation'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Badge } from '@/components/ui/badge'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Textarea } from '@/components/ui/textarea'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { toast } from 'sonner'
import { certApi, CertOrder, CertAccount, CertProviderConfig, ProviderConfigField } from '@/lib/api'
import {
  isDeployFieldVisible,
  mergeProviderFieldDefaults,
  resolveSelectFieldValue,
} from '@/lib/deploy-config-form'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { cn, formatDate } from '@/lib/utils'
import {
  certOrderDomainsLine,
  certOrderKindShort,
  formatCertOrderExpiryLine,
} from '@/lib/cert-order-display'
import { TableSkeleton } from '@/components/table-skeleton'
import { EmptyState } from '@/components/empty-state'
import { ShieldCheck, Plus, Search, RefreshCw, Download, Eye, Trash2, Play, FileText, CheckCircle, XCircle, Clock, AlertTriangle, Server, Rocket, Upload, Loader2, RotateCcw, KeyRound, Globe, Copy, Check, Info } from 'lucide-react'
import Link from 'next/link'

const CERT_STATUS_MAP: Record<number, { label: string; color: string; icon: React.ReactNode }> = {
  0: { label: '待申请', color: 'bg-gray-500', icon: <Clock className="h-4 w-4" /> },
  1: { label: '申请中', color: 'bg-blue-500', icon: <RefreshCw className="h-4 w-4 animate-spin" /> },
  2: { label: '待验证', color: 'bg-yellow-500', icon: <AlertTriangle className="h-4 w-4" /> },
  3: { label: '已签发', color: 'bg-green-500', icon: <CheckCircle className="h-4 w-4" /> },
  [-1]: { label: '创建失败', color: 'bg-red-500', icon: <XCircle className="h-4 w-4" /> },
  [-2]: { label: '订单创建失败', color: 'bg-red-500', icon: <XCircle className="h-4 w-4" /> },
  [-3]: { label: '验证失败', color: 'bg-red-500', icon: <XCircle className="h-4 w-4" /> },
  [-4]: { label: '验证超时', color: 'bg-orange-500', icon: <XCircle className="h-4 w-4" /> },
  [-5]: { label: '签发失败', color: 'bg-red-500', icon: <XCircle className="h-4 w-4" /> },
}

const KEY_TYPES = ['RSA', 'ECC']
const KEY_SIZES: Record<string, string[]> = {
  RSA: ['2048', '3072', '4096'],
  ECC: ['256', '384'],
}

const orderKindLabel = (kind?: string, challengeType?: string): string => {
  const ch = (challengeType || '').toLowerCase()
  switch (kind) {
    case 'ip':
      return 'IP (HTTP-01)'
    case 'mixed':
      return ch === 'http-01' ? '混合 (域名 HTTP-01)' : '混合'
    case 'dns':
      return ch === 'http-01' ? '域名 (HTTP-01)' : '域名 (DNS-01)'
    default:
      return '—'
  }
}

const orderKindBadgeClass = (kind?: string): string => {
  switch (kind) {
    case 'ip':
      return 'bg-sky-600 hover:bg-sky-600 text-white border-0'
    case 'mixed':
      return 'bg-violet-600 hover:bg-violet-600 text-white border-0'
    case 'dns':
      return 'bg-emerald-700/90 hover:bg-emerald-700/90 text-white border-0'
    default:
      return 'bg-muted text-muted-foreground'
  }
}

const getDaysUntilExpiry = (order: CertOrder): number | null => {
  if (order.end_day !== undefined) return order.end_day
  if (!order.expire_time) return null
  const now = new Date()
  const expiry = new Date(order.expire_time)
  const diffTime = expiry.getTime() - now.getTime()
  return Math.ceil(diffTime / (1000 * 60 * 60 * 24))
}

const getExpiryColor = (days: number | null): string => {
  if (days === null) return 'text-muted-foreground'
  if (days <= 0) return 'text-red-600 dark:text-red-400 font-bold'
  if (days <= 7) return 'text-red-500 dark:text-red-400'
  if (days <= 30) return 'text-orange-500 dark:text-orange-400'
  return 'text-green-600 dark:text-green-400'
}

/* 根据证书到期天数和状态返回表格行高亮 className */
const getCertRowClassName = (order: CertOrder): string => {
  if (order.status < 0) return 'bg-red-50/30 dark:bg-red-950/10'
  const days = getDaysUntilExpiry(order)
  if (days === null) return ''
  if (days <= 0) return 'bg-red-50/50 dark:bg-red-950/20'
  if (days <= 7) return 'bg-orange-50/50 dark:bg-orange-950/20'
  if (days <= 30) return 'bg-yellow-50/30 dark:bg-yellow-950/10'
  return ''
}

/** 单行域名预填时判断是否更像 IP（证书 IP 单证倾向 HTTP-01） */
function looksLikeSingleIpToken(s: string): boolean {
  const t = s.trim()
  if (!t || t.startsWith('*.')) return false
  if (/^\d{1,3}(\.\d{1,3}){3}$/.test(t)) return true
  if (t.includes(':') && /^[0-9a-fA-F:]+$/.test(t) && t.length >= 3) return true
  return false
}

export default function CertPage() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const certUrlPrefillDone = useRef(false)
  const [orders, setOrders] = useState<CertOrder[]>([])
  const [accounts, setAccounts] = useState<CertAccount[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('all')
  const [selectedOrders, setSelectedOrders] = useState<number[]>([])
  const [batchLoading, setBatchLoading] = useState(false)

  const [showAddDialog, setShowAddDialog] = useState(false)
  const [showDetailDialog, setShowDetailDialog] = useState(false)
  const [showLogDialog, setShowLogDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  const [showDeployDialog, setShowDeployDialog] = useState(false)
  
  const [selectedOrder, setSelectedOrder] = useState<CertOrder | null>(null)
  const [deployAccounts, setDeployAccounts] = useState<CertAccount[]>([])
  const [selectedDeployAccount, setSelectedDeployAccount] = useState<string>('')
  const [deployProviders, setDeployProviders] = useState<Record<string, CertProviderConfig>>({})
  const [quickDeployFormConfig, setQuickDeployFormConfig] = useState<Record<string, string>>({})
  const [quickDeployRemark, setQuickDeployRemark] = useState('')
  const [deploying, setDeploying] = useState(false)
  const [orderDetail, setOrderDetail] = useState<CertOrder | null>(null)
  const [orderLog, setOrderLog] = useState('')
  const [logLoading, setLogLoading] = useState(false)
  const [logCopied, setLogCopied] = useState(false)

  const [formData, setFormData] = useState({
    account_id: '',
    domains: '',
    key_type: 'RSA',
    key_size: '2048',
    is_auto: true,
    challenge_type: 'dns-01' as 'dns-01' | 'http-01',
  })
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    loadData()
  }, [])

  /* 域名管理跳转：/dashboard/cert?domain=example.com 或 ?domains=a.com,b.com */
  useEffect(() => {
    if (certUrlPrefillDone.current) return
    const raw = searchParams.get('domain') || searchParams.get('domains')
    if (!raw?.trim()) return
    certUrlPrefillDone.current = true
    const decoded = decodeURIComponent(raw.replace(/\+/g, ' '))
    const lines = decoded
      .split(/[\n,;]+/)
      .map((s) => s.trim())
      .filter(Boolean)
    const text = lines.join('\n')
    const singleIp = lines.length === 1 && looksLikeSingleIpToken(lines[0])
    setFormData((prev) => ({
      ...prev,
      domains: text,
      challenge_type: singleIp ? 'http-01' : prev.challenge_type,
    }))
    setShowAddDialog(true)
    router.replace(pathname, { scroll: false })
  }, [searchParams, router, pathname])

  const loadData = async () => {
    setLoading(true)
    try {
      const [ordersRes, accountsRes, deployAccountsRes, providersRes] = await Promise.all([
        certApi.getOrders(),
        certApi.getAccounts({ is_deploy: false }),
        certApi.getAccounts({ is_deploy: true }),
        certApi.getProviders(),
      ])
      if (ordersRes.code === 0 && ordersRes.data) {
        setOrders(Array.isArray(ordersRes.data) ? ordersRes.data : (ordersRes.data as { list: CertOrder[] }).list || [])
      }
      if (accountsRes.code === 0 && accountsRes.data) {
        setAccounts(Array.isArray(accountsRes.data) ? accountsRes.data : (accountsRes.data as { list: CertAccount[] }).list || [])
      }
      if (deployAccountsRes.code === 0 && deployAccountsRes.data) {
        setDeployAccounts(Array.isArray(deployAccountsRes.data) ? deployAccountsRes.data : (deployAccountsRes.data as { list: CertAccount[] }).list || [])
      }
      if (providersRes.code === 0 && providersRes.data) {
        setDeployProviders(providersRes.data.deploy || {})
      }
    } catch {
      toast.error('加载数据失败')
    } finally {
      setLoading(false)
    }
  }

  const handleAdd = () => {
    setFormData({
      account_id: '',
      domains: '',
      key_type: 'RSA',
      key_size: '2048',
      is_auto: true,
      challenge_type: 'dns-01',
    })
    setShowAddDialog(true)
  }

  const handleSubmit = async () => {
    if (!formData.account_id) {
      toast.error('请选择证书账户')
      return
    }
    const domainList = formData.domains.split('\n').map(d => d.trim()).filter(d => d)
    if (domainList.length === 0) {
      toast.error('请输入至少一个域名或 IP')
      return
    }
    if (formData.challenge_type === 'http-01' && domainList.some((d) => d.startsWith('*.'))) {
      toast.error('通配符域名不能使用 HTTP-01，请改用 DNS-01')
      return
    }

    setSubmitting(true)
    try {
      const res = await certApi.createOrder({
        account_id: Number(formData.account_id),
        domains: domainList,
        key_type: formData.key_type,
        key_size: formData.key_size,
        is_auto: formData.is_auto,
        challenge_type: formData.challenge_type,
      })
      if (res.code === 0) {
        toast.success('创建证书订单成功')
        setShowAddDialog(false)
        loadData()
      } else {
        toast.error(res.msg || '创建失败')
      }
    } catch {
      toast.error('创建失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleProcess = async (order: CertOrder) => {
    try {
      const res = await certApi.processOrder(order.id, true)
      if (res.code === 0) {
        toast.success(res.msg || '证书申请已开始处理')
        loadData()
      } else {
        toast.error(res.msg || '处理失败')
      }
    } catch {
      toast.error('处理失败')
    }
  }

  const handleBatchProcess = async () => {
    if (selectedOrders.length === 0) return
    setBatchLoading(true)
    try {
      let success = 0
      for (const id of selectedOrders) {
        try {
          const res = await certApi.processOrder(id, true)
          if (res.code === 0) success++
        } catch { /* ignore */ }
      }
      toast.success(`已提交 ${success} 个证书重新申请`)
      setSelectedOrders([])
      loadData()
    } finally {
      setBatchLoading(false)
    }
  }

  const handleBatchRenew = async () => {
    const autoOrders = selectedOrders.filter(id => {
      const order = orders.find(o => o.id === id)
      return order && order.is_auto
    })
    if (autoOrders.length === 0) {
      toast.error('所选订单中没有启用自动续期的订单')
      return
    }
    setBatchLoading(true)
    try {
      let success = 0
      for (const id of autoOrders) {
        try {
          const res = await certApi.processOrder(id, true)
          if (res.code === 0) success++
        } catch { /* ignore */ }
      }
      toast.success(`已提交 ${success}/${autoOrders.length} 个证书续期申请`)
      setSelectedOrders([])
      loadData()
    } finally {
      setBatchLoading(false)
    }
  }

  const [showBatchDeleteDialog, setShowBatchDeleteDialog] = useState(false)

  const handleBatchDelete = async () => {
    setShowBatchDeleteDialog(false)
    setBatchLoading(true)
    try {
      let success = 0
      for (const id of selectedOrders) {
        try {
          const res = await certApi.deleteOrder(id)
          if (res.code === 0) success++
        } catch { /* ignore */ }
      }
      toast.success(`已删除 ${success} 个证书订单`)
      setSelectedOrders([])
      loadData()
    } finally {
      setBatchLoading(false)
    }
  }

  const handleViewDetail = async (order: CertOrder) => {
    try {
      const res = await certApi.getOrderDetail(order.id)
      if (res.code === 0 && res.data) {
        // 确保数据格式正确
        const detail = res.data as CertOrder
        setOrderDetail(detail)
        setShowDetailDialog(true)
      } else {
        toast.error(res.msg || '获取详情失败')
      }
    } catch {
      toast.error('获取详情失败')
    }
  }

  const handleViewLog = async (order: CertOrder) => {
    setSelectedOrder(order)
    setOrderLog('')
    setLogCopied(false)
    setLogLoading(true)
    setShowLogDialog(true)
    try {
      const res = await certApi.getOrderLog(order.id)
      if (res.code === 0) {
        let logContent = ''
        if (typeof res.data === 'string') {
          logContent = res.data
        } else if (res.data && typeof res.data === 'object') {
          logContent = (res.data as { log?: string }).log || JSON.stringify(res.data, null, 2)
        } else {
          logContent = '暂无日志信息'
        }
        setOrderLog(logContent)
      } else {
        toast.error(res.msg || '获取日志失败')
        setShowLogDialog(false)
      }
    } catch {
      toast.error('获取日志失败')
      setShowLogDialog(false)
    } finally {
      setLogLoading(false)
    }
  }

  const handleCopyLog = async () => {
    if (!orderLog.trim()) {
      toast.error('暂无日志可复制')
      return
    }
    try {
      await navigator.clipboard.writeText(orderLog)
      setLogCopied(true)
      toast.success('已复制完整日志')
      window.setTimeout(() => setLogCopied(false), 2000)
    } catch {
      toast.error('复制失败，请手动选择文本')
    }
  }

  const handleDelete = (order: CertOrder) => {
    setSelectedOrder(order)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!selectedOrder) return
    try {
      const res = await certApi.deleteOrder(selectedOrder.id)
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
      setSelectedOrder(null)
    }
  }

  const handleToggleAuto = async (order: CertOrder) => {
    try {
      const res = await certApi.toggleOrderAuto(order.id, !order.is_auto)
      if (res.code === 0) {
        toast.success(res.msg || '操作成功')
        loadData()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    }
  }

  const quickDeployProvider = useMemo(() => {
    if (!selectedDeployAccount) return undefined
    const account = deployAccounts.find((a) => a.id.toString() === selectedDeployAccount)
    return account ? deployProviders[account.type] : undefined
  }, [selectedDeployAccount, deployAccounts, deployProviders])

  const quickDeployConfigFields = useMemo(
    () => quickDeployProvider?.deploy_config ?? [],
    [quickDeployProvider],
  )

  useEffect(() => {
    if (!selectedDeployAccount || !selectedOrder) {
      setQuickDeployFormConfig({})
      return
    }
    const account = deployAccounts.find((a) => a.id.toString() === selectedDeployAccount)
    if (!account) return
    const fields = deployProviders[account.type]?.deploy_config
    const doms = selectedOrder.domains ?? []
    const base: Record<string, string> = {}
    if (doms.length) {
      base.domains = doms.join('\n')
      base.domain = doms[0]
      base.sites = doms.join('\n')
    }
    setQuickDeployFormConfig(mergeProviderFieldDefaults(fields, base))
  }, [selectedDeployAccount, selectedOrder, deployAccounts, deployProviders])

  const renderQuickDeployField = (field: ProviderConfigField) => {
    const raw = quickDeployFormConfig[field.key]
    const value = raw ?? field.value ?? ''

    if (field.type === 'radio' && field.options) {
      const v = value || field.value || field.options[0]?.value || ''
      return (
        <RadioGroup
          value={v}
          onValueChange={(nv) =>
            setQuickDeployFormConfig((prev) => ({ ...prev, [field.key]: nv }))
          }
          className="flex flex-wrap gap-4"
        >
          {field.options.map((opt) => (
            <div key={opt.value} className="flex items-center space-x-2">
              <RadioGroupItem value={opt.value} id={`quick-deploy-${field.key}-${opt.value}`} />
              <Label htmlFor={`quick-deploy-${field.key}-${opt.value}`} className="font-normal cursor-pointer">
                {opt.label}
              </Label>
            </div>
          ))}
        </RadioGroup>
      )
    }

    if (field.type === 'select' && field.options) {
      const resolved = resolveSelectFieldValue(field, raw)
      return (
        <Select
          value={resolved}
          onValueChange={(v) =>
            setQuickDeployFormConfig((prev) => ({ ...prev, [field.key]: v }))
          }
        >
          <SelectTrigger>
            <SelectValue placeholder={field.placeholder || `请选择${field.name}`} />
          </SelectTrigger>
          <SelectContent>
            {field.options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      )
    }

    if (field.type === 'textarea') {
      const rows = field.key === 'domains' || field.key === 'sites' ? 6 : field.key === 'domain' ? 4 : 4
      return (
        <Textarea
          value={value}
          onChange={(e) =>
            setQuickDeployFormConfig((prev) => ({ ...prev, [field.key]: e.target.value }))
          }
          placeholder={field.placeholder}
          rows={rows}
          className="min-h-[96px] font-mono text-sm"
        />
      )
    }

    return (
      <Input
        type={field.type === 'password' ? 'password' : 'text'}
        value={value}
        onChange={(e) =>
          setQuickDeployFormConfig((prev) => ({ ...prev, [field.key]: e.target.value }))
        }
        placeholder={field.placeholder}
      />
    )
  }

  const handleQuickDeploy = (order: CertOrder) => {
    setSelectedOrder(order)
    setSelectedDeployAccount('')
    setQuickDeployFormConfig({})
    setQuickDeployRemark('')
    setShowDeployDialog(true)
  }

  const handleDeploy = async () => {
    if (!selectedOrder || !selectedDeployAccount) {
      toast.error('请选择部署账户')
      return
    }
    for (const field of quickDeployConfigFields) {
      if (!isDeployFieldVisible(field, quickDeployFormConfig)) continue
      const v = quickDeployFormConfig[field.key]
      if (field.required && (v === undefined || String(v).trim() === '')) {
        toast.error(`请填写${field.name}`)
        return
      }
    }
    setDeploying(true)
    try {
      const defaultRemark = `快速部署 - ${selectedOrder.domains?.join(', ') ?? ''}`
      const createRes = await certApi.createDeploy({
        account_id: Number(selectedDeployAccount),
        order_id: selectedOrder.id,
        config: quickDeployFormConfig,
        remark: quickDeployRemark.trim() || defaultRemark,
      })
      if (createRes.code !== 0) {
        toast.error(createRes.msg || '创建部署任务失败')
        return
      }
      
      // 立即执行部署（后端返回 data.id 为数字）
      const newId = createRes.data != null && typeof createRes.data === 'object' && 'id' in createRes.data
        ? Number((createRes.data as { id: number }).id)
        : NaN
      if (!Number.isNaN(newId)) {
        const processRes = await certApi.processDeploy(newId)
        if (processRes.code === 0) {
          toast.success('证书部署成功')
          setShowDeployDialog(false)
          setShowDetailDialog(false)
        } else {
          toast.error(processRes.msg || '部署执行失败')
        }
      }
    } catch {
      toast.error('部署失败')
    } finally {
      setDeploying(false)
    }
  }

  const handleDownload = async (order: CertOrder, format: string) => {
    try {
      const token = localStorage.getItem('token')
      const response = await fetch(`/api/cert/orders/${order.id}/download?type=${format}`, {
        credentials: 'same-origin',
        headers: {
          'Authorization': `Bearer ${token}`
        }
      })
      
      if (!response.ok) {
        if (response.status === 401) {
          toast.error('登录已过期，请重新登录')
          return
        }
        const data = await response.json()
        toast.error(data.msg || '下载失败')
        return
      }

      // 获取文件名
      const contentDisposition = response.headers.get('Content-Disposition')
      let filename = 'cert'
      if (contentDisposition) {
        const match = contentDisposition.match(/filename=([^;]+)/)
        if (match) {
          filename = match[1].replace(/"/g, '')
        }
      }

      // 创建blob并下载
      const blob = await response.blob()
      const url = window.URL.createObjectURL(blob)
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      window.URL.revokeObjectURL(url)
      document.body.removeChild(a)
    } catch {
      toast.error('下载失败')
    }
  }

  const filteredOrders = orders.filter(order => {
    const kindText = orderKindLabel(order.order_kind, order.challenge_type)
    const matchKeyword = !keyword ||
      (order.domains && order.domains.some(d => d.includes(keyword))) ||
      order.issuer?.includes(keyword) ||
      kindText.includes(keyword) ||
      (order.order_kind && order.order_kind.includes(keyword))
    const matchStatus = statusFilter === 'all' || 
      (statusFilter === 'error' && order.status < 0) ||
      (statusFilter === 'issued' && order.status === 3) ||
      (statusFilter === 'pending' && order.status >= 0 && order.status < 3) ||
      order.status.toString() === statusFilter
    return matchKeyword && matchStatus
  })

  // Stats
  const issuedCount = orders.filter(o => o.status === 3).length
  const errorCount = orders.filter(o => o.status < 0).length
  const pendingCount = orders.filter(o => o.status >= 0 && o.status < 3).length
  const expiringCount = orders.filter(o => o.end_day !== undefined && o.end_day > 0 && o.end_day <= 7).length

  const getStatusBadge = (status: number) => {
    const statusInfo = CERT_STATUS_MAP[status] || { label: '未知', color: 'bg-gray-500', icon: null }
    return (
      <Badge className={`${statusInfo.color} text-white flex items-center gap-1`}>
        {statusInfo.icon}
        {statusInfo.label}
      </Badge>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center shadow-lg shadow-emerald-500/20">
              <ShieldCheck className="h-5 w-5 text-white" />
            </div>
            证书管理
          </h1>
          <p className="text-muted-foreground mt-1">管理SSL证书订单，支持自动申请和续期</p>
        </div>
        <div className="flex gap-2">
          <Link href="/dashboard/accounts">
            <Button variant="outline">
              <Server className="h-4 w-4 mr-2" />
              账户管理
            </Button>
          </Link>
          <Button onClick={handleAdd} className="bg-gradient-to-r from-emerald-600 to-teal-600 hover:from-emerald-500 hover:to-teal-500">
            <Plus className="h-4 w-4 mr-2" />
            申请证书
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">已签发</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600 dark:text-green-400">{issuedCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">处理中</CardTitle>
            <Clock className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-blue-600 dark:text-blue-400">{pendingCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">即将过期</CardTitle>
            <AlertTriangle className="h-4 w-4 text-orange-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-orange-600 dark:text-orange-400">{expiringCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">签发失败</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600 dark:text-red-400">{errorCount}</div>
          </CardContent>
        </Card>
      </div>

      {/* Filters */}
      <Card>
        <CardHeader>
          <CardTitle>证书订单列表</CardTitle>
          <CardDescription>查看和管理所有SSL证书订单</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col sm:flex-row gap-4 mb-6">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索域名、IP、类型…"
                value={keyword}
                onChange={(e) => setKeyword(e.target.value)}
                className="pl-10"
              />
            </div>
            <Select value={statusFilter} onValueChange={setStatusFilter}>
              <SelectTrigger className="w-[180px]">
                <SelectValue placeholder="状态筛选" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="issued">已签发</SelectItem>
                <SelectItem value="pending">处理中</SelectItem>
                <SelectItem value="error">签发失败</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="outline" onClick={loadData}>
              <RefreshCw className="h-4 w-4 mr-2" />
              刷新
            </Button>
            {selectedOrders.length > 0 && (
              <div className="flex items-center gap-2 ml-4 pl-4 border-l">
                <span className="text-sm text-muted-foreground">已选 {selectedOrders.length} 项</span>
                <Button size="sm" variant="outline" onClick={handleBatchProcess} disabled={batchLoading}>
                  {batchLoading ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <Play className="h-3.5 w-3.5 mr-1" />}
                  {batchLoading ? '处理中...' : '重新申请'}
                </Button>
                <Button size="sm" variant="outline" onClick={handleBatchRenew} disabled={batchLoading} className="border-emerald-200 text-emerald-700 hover:bg-emerald-50 dark:border-emerald-800 dark:text-emerald-400 dark:hover:bg-emerald-950">
                  {batchLoading ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <RotateCcw className="h-3.5 w-3.5 mr-1" />}
                  {batchLoading ? '处理中...' : '批量续期'}
                </Button>
                <Button size="sm" variant="destructive" onClick={() => setShowBatchDeleteDialog(true)} disabled={batchLoading}>
                  {batchLoading ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <Trash2 className="h-3.5 w-3.5 mr-1" />}
                  {batchLoading ? '处理中...' : '批量删除'}
                </Button>
              </div>
            )}
          </div>

          {loading ? (
            <TableSkeleton rows={5} columns={9} />
          ) : filteredOrders.length === 0 ? (
            <EmptyState
              icon={ShieldCheck}
              title="暂无证书订单"
              description="还没有任何证书订单，请点击上方按钮申请"
            />
          ) : (
          <>
          <div className="md:hidden space-y-3">
            {filteredOrders.map((order) => {
              const days = getDaysUntilExpiry(order)
              return (
                <Card key={order.id} className={cn('overflow-hidden', getCertRowClassName(order))}>
                  <CardContent className="p-4 space-y-3">
                    <div className="flex items-start justify-between gap-2">
                      <Checkbox
                        checked={selectedOrders.includes(order.id)}
                        onCheckedChange={(checked) => {
                          setSelectedOrders(checked
                            ? [...selectedOrders, order.id]
                            : selectedOrders.filter((id) => id !== order.id))
                        }}
                        className="mt-1"
                      />
                      <div className="flex-1 min-w-0">
                        <div className="font-medium text-sm break-words">
                          {order.domains?.join(', ') || '—'}
                        </div>
                        {order.order_kind && (
                          <Badge className={cn('text-[10px] px-1.5 py-0', orderKindBadgeClass(order.order_kind))}>
                            {orderKindLabel(order.order_kind, order.challenge_type)}
                          </Badge>
                        )}
                        {getStatusBadge(order.status)}
                      </div>
                    </div>
                    <div className="grid grid-cols-2 gap-x-3 gap-y-1 text-xs text-muted-foreground">
                      <span>密钥</span>
                      <span className="text-right text-foreground">{order.key_type} {order.key_size}</span>
                      <span>颁发机构</span>
                      <span className="text-right text-foreground truncate">{order.issuer || '—'}</span>
                      <span>自动续期</span>
                      <span className="text-right">
                        <Switch
                          className="scale-90 origin-right"
                          checked={order.is_auto}
                          onCheckedChange={() => handleToggleAuto(order)}
                        />
                      </span>
                      {order.expire_time && (
                        <>
                          <span>有效期</span>
                          <span className={cn('text-right font-medium', getExpiryColor(days))}>
                            {new Date(order.expire_time).toLocaleDateString()}
                            {days != null ? ` · ${days <= 0 ? '已过期' : `余${days}天`}` : ''}
                          </span>
                        </>
                      )}
                    </div>
                    {order.status < 0 && order.error && (
                      <p className="text-xs text-destructive break-words border-t pt-2">
                        失败原因：{order.error}
                      </p>
                    )}
                    <div className="flex flex-wrap gap-2 pt-1 border-t">
                      {order.status === 3 && (
                        <>
                          <Button size="sm" variant="outline" className="min-h-10" onClick={() => handleViewDetail(order)}>详情</Button>
                          <Button size="sm" variant="outline" className="min-h-10" onClick={() => handleDownload(order, 'zip')}>下载</Button>
                        </>
                      )}
                      {order.status !== 3 && order.status >= 0 && (
                        <Button size="sm" variant="outline" className="min-h-10" onClick={() => handleProcess(order)}>处理</Button>
                      )}
                      <Button size="sm" variant="outline" className="min-h-10" onClick={() => handleViewLog(order)}>日志</Button>
                      <Button size="sm" variant="outline" className="min-h-10 text-destructive border-destructive/30" onClick={() => handleDelete(order)}>删除</Button>
                    </div>
                  </CardContent>
                </Card>
              )
            })}
          </div>
          <div className="hidden md:block rounded-md border overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">
                    <Checkbox 
                      checked={selectedOrders.length === filteredOrders.length && filteredOrders.length > 0}
                      onCheckedChange={(checked) => {
                        setSelectedOrders(checked ? filteredOrders.map(o => o.id) : [])
                      }}
                    />
                  </TableHead>
                  <TableHead>域名 / IP</TableHead>
                  <TableHead className="whitespace-nowrap w-[100px]">类型</TableHead>
                  <TableHead>密钥类型</TableHead>
                  <TableHead>颁发机构</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>有效期</TableHead>
                  <TableHead>自动续期</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredOrders.map((order) => (
                    <TableRow key={order.id} className={getCertRowClassName(order)}>
                      <TableCell>
                        <Checkbox 
                          checked={selectedOrders.includes(order.id)}
                          onCheckedChange={(checked) => {
                            setSelectedOrders(checked 
                              ? [...selectedOrders, order.id]
                              : selectedOrders.filter(id => id !== order.id)
                            )
                          }}
                        />
                      </TableCell>
                      <TableCell>
                        <div className="max-w-[200px]">
                          {order.domains?.slice(0, 2).map((d, i) => (
                            <div key={i} className="truncate text-sm font-mono">{d}</div>
                          ))}
                          {order.domains && order.domains.length > 2 && (
                            <div className="text-xs text-muted-foreground">等 {order.domains.length} 项</div>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge className={cn('text-[10px] px-1.5 py-0', orderKindBadgeClass(order.order_kind))}>
                          {orderKindLabel(order.order_kind, order.challenge_type)}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm">{order.key_type} {order.key_size}</span>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm">{order.issuer || '-'}</span>
                      </TableCell>
                      <TableCell>
                        <div className="space-y-1">
                          {getStatusBadge(order.status)}
                          {order.status < 0 && order.error && (
                            <p className="text-xs text-destructive max-w-[220px] break-words">{order.error}</p>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        {order.expire_time ? (() => {
                          const days = getDaysUntilExpiry(order)
                          return (
                            <div>
                              <div className="text-sm">{new Date(order.expire_time).toLocaleDateString()}</div>
                              {days !== null && (
                                <div className={`text-xs font-medium ${getExpiryColor(days)}`}>
                                  {days <= 0 ? '已过期' : `剩余 ${days} 天`}
                                </div>
                              )}
                            </div>
                          )
                        })() : (
                          <span className="text-muted-foreground">-</span>
                        )}
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Switch
                            checked={order.is_auto}
                            onCheckedChange={() => handleToggleAuto(order)}
                          />
                          <span className={`text-xs font-medium ${order.is_auto ? 'text-green-600 dark:text-green-400' : 'text-muted-foreground'}`}>
                            {order.is_auto ? '已开启' : '已关闭'}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          {order.status === 3 && (
                            <>
                              <Button size="sm" variant="ghost" onClick={() => handleViewDetail(order)} title="查看详情">
                                <Eye className="h-4 w-4" />
                              </Button>
                              <Button size="sm" variant="ghost" onClick={() => handleDownload(order, 'zip')} title="下载证书">
                                <Download className="h-4 w-4" />
                              </Button>
                            </>
                          )}
                          {order.status !== 3 && order.status >= 0 && (
                            <Button size="sm" variant="ghost" onClick={() => handleProcess(order)} title="处理证书">
                              <Play className="h-4 w-4" />
                            </Button>
                          )}
                          <Button size="sm" variant="ghost" onClick={() => handleViewLog(order)} title="查看日志">
                            <FileText className="h-4 w-4" />
                          </Button>
                          <Button size="sm" variant="ghost" className="text-red-500 hover:text-red-600" onClick={() => handleDelete(order)} title="删除">
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))}
              </TableBody>
            </Table>
          </div>
          </>
          )}
        </CardContent>
      </Card>

      {/* Add Order Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>申请SSL证书</DialogTitle>
            <DialogDescription>创建新的SSL证书申请订单</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>证书账户 *</Label>
              <Select value={formData.account_id} onValueChange={(v) => setFormData({ ...formData, account_id: v })}>
                <SelectTrigger>
                  <SelectValue placeholder="选择证书账户" />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((acc) => (
                    <SelectItem key={acc.id} value={acc.id.toString()}>
                      {acc.name} ({acc.type_name || acc.type})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>域名或公网 IP *</Label>
              <Textarea
                placeholder={'每行一条。域名示例：example.com 或 *.example.com\nIP 证书示例：203.0.113.10（需 Let\'s Encrypt 等支持 IP 的 ACME；验证为 HTTP-01，需开放 80 端口）'}
                value={formData.domains}
                onChange={(e) => setFormData({ ...formData, domains: e.target.value })}
                rows={5}
              />
              <p className="text-xs text-muted-foreground">
                默认 DNS-01：可在本系统托管的解析上自动添加 TXT。选 HTTP-01 时需在域名解析到的服务器上开放 80 端口，按订单说明放置校验文件。公网 IP 固定 HTTP-01。通配符仅支持 DNS-01。
              </p>
            </div>
            <div className="space-y-2">
              <Label>域名验证方式（ACME）</Label>
              <Select
                value={formData.challenge_type}
                onValueChange={(v) =>
                  setFormData({ ...formData, challenge_type: v as 'dns-01' | 'http-01' })
                }
              >
                <SelectTrigger>
                  <SelectValue />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="dns-01">DNS-01（TXT 记录，可自动写解析）</SelectItem>
                  <SelectItem value="http-01">HTTP-01（80 端口，自行配置 Web 服务器）</SelectItem>
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">
                订单中仅有公网 IP 时，此项会被忽略（固定 HTTP-01）。混合域名+IP 时：选 DNS-01 则域名为 TXT、IP 仍为 HTTP-01；选 HTTP-01 则仅作用于域名部分。
              </p>
            </div>
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>密钥类型</Label>
                <Select 
                  value={formData.key_type} 
                  onValueChange={(v) => setFormData({ 
                    ...formData, 
                    key_type: v,
                    key_size: KEY_SIZES[v][0]
                  })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {KEY_TYPES.map((t) => (
                      <SelectItem key={t} value={t}>{t}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>密钥长度</Label>
                <Select value={formData.key_size} onValueChange={(v) => setFormData({ ...formData, key_size: v })}>
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {KEY_SIZES[formData.key_type].map((s) => (
                      <SelectItem key={s} value={s}>{s}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>
            <div className="flex items-center space-x-2">
              <Switch
                checked={formData.is_auto}
                onCheckedChange={(checked) => setFormData({ ...formData, is_auto: checked })}
              />
              <Label>启用自动续期</Label>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddDialog(false)}>取消</Button>
            <Button onClick={handleSubmit} disabled={submitting}>
              {submitting ? '提交中...' : '提交申请'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Detail Dialog */}
      <Dialog open={showDetailDialog} onOpenChange={setShowDetailDialog}>
        <DialogContent className="max-w-2xl max-h-[85vh] flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-3">
              证书详情
              {orderDetail && getStatusBadge(orderDetail.status)}
            </DialogTitle>
            <DialogDescription>查看证书详细信息和内容</DialogDescription>
          </DialogHeader>
          {orderDetail && (() => {
            const daysLeft = getDaysUntilExpiry(orderDetail)
            return (
              <div className="flex-1 overflow-y-auto space-y-5 pr-2">
                {/* Domains (SAN) */}
                <div className="space-y-2">
                  <div className="flex items-center gap-1.5 text-xs text-muted-foreground uppercase tracking-wide font-medium">
                    <Globe className="h-3.5 w-3.5" />
                    证书域名 / IP (SAN)
                  </div>
                  <div className="flex flex-wrap items-center gap-2">
                    {orderDetail.order_kind && (
                      <Badge className={cn('text-[10px]', orderKindBadgeClass(orderDetail.order_kind))}>
                        {orderKindLabel(orderDetail.order_kind, orderDetail.challenge_type)}
                      </Badge>
                    )}
                    <div className="flex flex-wrap gap-1.5">
                    {orderDetail.domains?.map((d, i) => (
                      <Badge key={i} variant="secondary" className="font-mono text-xs px-2 py-0.5">
                        {i === 0 && <ShieldCheck className="h-3 w-3 mr-1 text-emerald-500" />}
                        {d}
                      </Badge>
                    ))}
                    {(!orderDetail.domains || orderDetail.domains.length === 0) && (
                      <span className="text-sm text-muted-foreground">-</span>
                    )}
                    </div>
                  </div>
                </div>

                {orderDetail.dns_info && orderDetail.status !== 3 && (
                  <div className="space-y-2">
                    <Label className="text-xs text-muted-foreground">验证说明（DNS / HTTP-01）</Label>
                    <Textarea
                      value={orderDetail.dns_info}
                      readOnly
                      className="font-mono text-xs min-h-[120px] resize-y bg-amber-50/50 dark:bg-amber-950/20 border-amber-200/60 dark:border-amber-900/50"
                    />
                  </div>
                )}

                {/* Info Grid */}
                <div className="grid grid-cols-2 gap-x-6 gap-y-3 bg-muted/40 rounded-lg p-4">
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground">颁发机构</span>
                    <p className="text-sm font-medium">{orderDetail.issuer || '-'}</p>
                  </div>
                  <div className="space-y-1">
                    <div className="flex items-center gap-1 text-xs text-muted-foreground">
                      <KeyRound className="h-3 w-3" />
                      密钥类型
                    </div>
                    <p className="text-sm font-medium">{orderDetail.key_type} {orderDetail.key_size}</p>
                  </div>
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground">签发时间</span>
                    <p className="text-sm font-medium">{orderDetail.issue_time ? new Date(orderDetail.issue_time).toLocaleString() : '-'}</p>
                  </div>
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground">过期时间</span>
                    <p className="text-sm font-medium">{orderDetail.expire_time ? new Date(orderDetail.expire_time).toLocaleString() : '-'}</p>
                  </div>
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground">剩余天数</span>
                    <p className={`text-sm font-bold ${getExpiryColor(daysLeft)}`}>
                      {daysLeft !== null ? (daysLeft <= 0 ? '已过期' : `${daysLeft} 天`) : '-'}
                    </p>
                  </div>
                  <div className="space-y-1">
                    <span className="text-xs text-muted-foreground">自动续期</span>
                    <p className="text-sm font-medium">
                      {orderDetail.is_auto ? (
                        <span className="text-green-600 dark:text-green-400">已启用</span>
                      ) : (
                        <span className="text-muted-foreground">未启用</span>
                      )}
                    </p>
                  </div>
                </div>

                {/* Certificate Content */}
                {orderDetail.fullchain && (
                  <div className="space-y-2">
                    <Label className="text-sm">证书内容 (fullchain.pem)</Label>
                    <Textarea
                      value={orderDetail.fullchain}
                      readOnly
                      className="font-mono text-xs h-32 resize-none bg-muted/30"
                    />
                  </div>
                )}
                {orderDetail.private_key && (
                  <div className="space-y-2">
                    <Label className="text-sm">私钥内容 (private.key)</Label>
                    <Textarea
                      value={orderDetail.private_key}
                      readOnly
                      className="font-mono text-xs h-32 resize-none bg-muted/30"
                    />
                  </div>
                )}
              </div>
            )
          })()}
          <DialogFooter className="mt-4 flex-wrap gap-2">
            <div className="flex gap-2 flex-1 flex-wrap">
              <Button variant="outline" size="sm" onClick={() => handleDownload(orderDetail!, 'fullchain')}>
                <Download className="h-3.5 w-3.5 mr-1.5" />
                下载证书
              </Button>
              <Button variant="outline" size="sm" onClick={() => handleDownload(orderDetail!, 'key')}>
                <KeyRound className="h-3.5 w-3.5 mr-1.5" />
                下载私钥
              </Button>
              <Button variant="outline" size="sm" onClick={() => handleDownload(orderDetail!, 'zip')}>
                <Download className="h-3.5 w-3.5 mr-1.5" />
                下载全部 (ZIP)
              </Button>
            </div>
            {deployAccounts.length > 0 && (
              <Button onClick={() => handleQuickDeploy(orderDetail!)} className="bg-gradient-to-r from-violet-600 to-purple-600 hover:from-violet-500 hover:to-purple-500">
                <Rocket className="h-4 w-4 mr-2" />
                一键部署
              </Button>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Quick Deploy Dialog */}
      <Dialog open={showDeployDialog} onOpenChange={setShowDeployDialog}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Rocket className="h-5 w-5 text-violet-500" />
              一键部署证书
            </DialogTitle>
            <DialogDescription>选择部署账户并填写与「部署管理」一致的部署参数，将证书部署到目标服务</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            {selectedOrder && (
              <div className="rounded-lg border bg-muted/40 p-4 space-y-3">
                <p className="text-xs font-medium text-muted-foreground uppercase tracking-wide">
                  即将部署的证书
                </p>
                <p className="text-sm font-semibold leading-snug break-all text-foreground">
                  {certOrderDomainsLine(selectedOrder, 6)}
                </p>
                <div className="grid gap-3 sm:grid-cols-2 text-sm">
                  <div className="space-y-0.5">
                    <span className="text-xs text-muted-foreground">订单号</span>
                    <p className="font-mono tabular-nums">#{selectedOrder.id}</p>
                  </div>
                  <div className="space-y-0.5">
                    <span className="text-xs text-muted-foreground">证书账户 / 类型</span>
                    <p className="break-all">
                      {selectedOrder.type_name || '—'}{' '}
                      <Badge variant="outline" className="ml-1 text-[10px] h-5 align-middle font-normal">
                        {certOrderKindShort(selectedOrder)}
                      </Badge>
                    </p>
                  </div>
                  <div className="space-y-0.5">
                    <span className="text-xs text-muted-foreground">密钥</span>
                    <p>
                      {[selectedOrder.key_type, selectedOrder.key_size].filter(Boolean).join(' ') || '默认'}
                    </p>
                  </div>
                  <div className="space-y-0.5">
                    <span className="text-xs text-muted-foreground">到期</span>
                    {(() => {
                      const exp = formatCertOrderExpiryLine(selectedOrder)
                      return (
                        <p
                          className={cn(
                            exp.text === '已过期' && 'text-destructive font-medium',
                            exp.urgent && exp.text !== '已过期' && 'text-amber-700 dark:text-amber-400 font-medium',
                          )}
                        >
                          {exp.text}
                          {selectedOrder.expire_time && (
                            <span className="block text-xs font-normal text-muted-foreground mt-0.5">
                              {formatDate(selectedOrder.expire_time, 'datetime')}
                            </span>
                          )}
                        </p>
                      )
                    })()}
                  </div>
                  <div className="space-y-0.5 sm:col-span-2">
                    <span className="text-xs text-muted-foreground">颁发机构</span>
                    <p className="break-all">{selectedOrder.issuer || '—'}</p>
                  </div>
                </div>
              </div>
            )}
            <div className="space-y-2">
              <Label>部署账户 *</Label>
              <Select
                value={selectedDeployAccount}
                onValueChange={setSelectedDeployAccount}
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择部署账户" />
                </SelectTrigger>
                <SelectContent>
                  {deployAccounts.map((acc) => (
                    <SelectItem key={acc.id} value={acc.id.toString()}>
                      <div className="flex items-center gap-2">
                        <Upload className="h-4 w-4 text-muted-foreground" />
                        {acc.name} ({acc.type_name || acc.type})
                      </div>
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <p className="text-xs text-muted-foreground">如需添加新的部署账户，请前往「部署账户」页面配置</p>
            </div>

            {quickDeployProvider?.note && (
              <p className="text-sm text-muted-foreground border-l-2 border-primary/40 pl-3 py-1">
                {quickDeployProvider.note}
              </p>
            )}
            {quickDeployProvider?.deploy_note && (
              <div className="flex gap-2 rounded-md border border-amber-200/80 bg-amber-50/80 dark:border-amber-900/50 dark:bg-amber-950/40 px-3 py-2 text-sm text-amber-900 dark:text-amber-100">
                <Info className="h-4 w-4 shrink-0 mt-0.5" />
                <span>{quickDeployProvider.deploy_note}</span>
              </div>
            )}

            {selectedDeployAccount &&
              quickDeployConfigFields
                .filter((field) => isDeployFieldVisible(field, quickDeployFormConfig))
                .map((field) => (
                  <div key={field.key} className="space-y-2">
                    <Label>
                      {field.name}
                      {field.required && <span className="text-destructive ml-1">*</span>}
                    </Label>
                    {renderQuickDeployField(field)}
                    {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
                  </div>
                ))}

            <div className="space-y-2">
              <Label>备注</Label>
              <Textarea
                placeholder="可选；留空则使用「快速部署 - 域名列表」"
                value={quickDeployRemark}
                onChange={(e) => setQuickDeployRemark(e.target.value)}
                rows={2}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDeployDialog(false)}>取消</Button>
            <Button onClick={handleDeploy} disabled={deploying || !selectedDeployAccount} className="bg-gradient-to-r from-violet-600 to-purple-600">
              {deploying ? (
                <>
                  <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                  部署中...
                </>
              ) : (
                <>
                  <Rocket className="h-4 w-4 mr-2" />
                  立即部署
                </>
              )}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Log Dialog */}
      <Dialog
        open={showLogDialog}
        onOpenChange={(open) => {
          setShowLogDialog(open)
          if (!open) setLogCopied(false)
        }}
      >
        <DialogContent className="max-w-3xl max-h-[90vh] flex flex-col gap-0 p-0 sm:rounded-lg overflow-hidden">
          <DialogHeader className="px-6 pt-6 pb-2 space-y-1 shrink-0">
            <DialogTitle className="text-lg">申请日志</DialogTitle>
            <DialogDescription className="line-clamp-2">
              {selectedOrder?.domains?.length
                ? `${selectedOrder.domains.slice(0, 3).join(' · ')}${selectedOrder.domains.length > 3 ? ` 等 ${selectedOrder.domains.length} 个域名` : ''}`
                : '查看证书申请与续期过程日志；重试会在原有日志后追加记录，不会清空历史。'}
            </DialogDescription>
          </DialogHeader>
          <div className="px-6 pb-2 shrink-0 flex items-center justify-between gap-2">
            <p className="text-xs text-muted-foreground">支持滚动查看长日志，可复制全文排查</p>
            <Button
              type="button"
              variant="outline"
              size="sm"
              className="shrink-0 h-8"
              disabled={logLoading || !orderLog.trim()}
              onClick={() => void handleCopyLog()}
            >
              {logCopied ? (
                <>
                  <Check className="h-3.5 w-3.5 mr-1.5 text-green-600" />
                  已复制
                </>
              ) : (
                <>
                  <Copy className="h-3.5 w-3.5 mr-1.5" />
                  复制全文
                </>
              )}
            </Button>
          </div>
          <div className="flex-1 min-h-[280px] max-h-[min(58vh,520px)] mx-6 mb-4 rounded-md border bg-muted/30 overflow-hidden">
            {logLoading ? (
              <div className="flex flex-col items-center justify-center gap-3 h-full min-h-[280px] text-muted-foreground">
                <Loader2 className="h-8 w-8 animate-spin" />
                <span className="text-sm">正在加载日志…</span>
              </div>
            ) : (
              <pre
                className="h-full max-h-[min(58vh,520px)] overflow-auto p-4 text-xs font-mono leading-relaxed whitespace-pre-wrap break-words text-foreground select-text"
                tabIndex={0}
              >
                {orderLog || '暂无日志'}
              </pre>
            )}
          </div>
          <DialogFooter className="px-6 py-4 border-t bg-muted/20 mt-auto shrink-0">
            <Button variant="outline" onClick={() => setShowLogDialog(false)}>
              关闭
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
              确定要删除该证书订单吗？此操作不可撤销。
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

      {/* 批量删除确认 */}
      <AlertDialog open={showBatchDeleteDialog} onOpenChange={setShowBatchDeleteDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认批量删除</AlertDialogTitle>
            <AlertDialogDescription>确定要删除选中的 {selectedOrders.length} 个证书订单吗？此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleBatchDelete} className="bg-red-600 hover:bg-red-700">确定删除</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
