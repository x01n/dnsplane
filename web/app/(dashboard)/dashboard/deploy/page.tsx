'use client'

import { useState, useEffect, useMemo } from 'react'
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
import { certApi, CertDeploy, CertAccount, CertOrder, CertProviderConfig, ProviderConfigField } from '@/lib/api'
import {
  certOrderDomainsLine,
  certOrderKindShort,
  compareIssuedCertOrders,
  formatCertOrderExpiryLine,
} from '@/lib/cert-order-display'
import { CertOrderSelectItem } from '@/components/cert-order-select'
import {
  evaluateDeployFieldShow,
  isDeployFieldVisible,
  mergeProviderFieldDefaults,
  resolveSelectFieldValue,
} from '@/lib/deploy-config-form'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'
import { Upload, Plus, Search, RefreshCw, Trash2, Play, CheckCircle, XCircle, Clock, Server, Settings, Info } from 'lucide-react'
import Link from 'next/link'

const DEPLOY_STATUS_MAP: Record<number, { label: string; color: string; icon: React.ReactNode }> = {
  0: { label: '待部署', color: 'bg-gray-500', icon: <Clock className="h-4 w-4" /> },
  1: { label: '部署中', color: 'bg-blue-500', icon: <RefreshCw className="h-4 w-4 animate-spin" /> },
  2: { label: '已部署', color: 'bg-green-500', icon: <CheckCircle className="h-4 w-4" /> },
  [-1]: { label: '部署失败', color: 'bg-red-500', icon: <XCircle className="h-4 w-4" /> },
}

export default function DeployPage() {
  const [deploys, setDeploys] = useState<CertDeploy[]>([])
  const [accounts, setAccounts] = useState<CertAccount[]>([])
  const [orders, setOrders] = useState<CertOrder[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState<string>('all')
  const [selectedDeploys, setSelectedDeploys] = useState<number[]>([])

  const [showAddDialog, setShowAddDialog] = useState(false)
  const [showEditDialog, setShowEditDialog] = useState(false)
  const [showDeleteDialog, setShowDeleteDialog] = useState(false)
  
  const [selectedDeploy, setSelectedDeploy] = useState<CertDeploy | null>(null)
  const [providers, setProviders] = useState<Record<string, CertProviderConfig>>({})

  const [formData, setFormData] = useState({
    account_id: '',
    order_id: '',
    config: {} as Record<string, string>,
    remark: '',
  })
  const [submitting, setSubmitting] = useState(false)

  useEffect(() => {
    loadData()
  }, [])

  const loadData = async () => {
    setLoading(true)
    try {
      const [deploysRes, accountsRes, ordersRes, providersRes] = await Promise.all([
        certApi.getDeploys(),
        certApi.getAccounts({ is_deploy: true }),
        certApi.getOrders(),
        certApi.getProviders(),
      ])
      if (deploysRes.code === 0 && deploysRes.data) {
        setDeploys(Array.isArray(deploysRes.data) ? deploysRes.data : (deploysRes.data as { list: CertDeploy[] }).list || [])
      }
      if (accountsRes.code === 0 && accountsRes.data) {
        setAccounts(Array.isArray(accountsRes.data) ? accountsRes.data : (accountsRes.data as { list: CertAccount[] }).list || [])
      }
      if (ordersRes.code === 0 && ordersRes.data) {
        setOrders(Array.isArray(ordersRes.data) ? ordersRes.data : (ordersRes.data as { list: CertOrder[] }).list || [])
      }
      if (providersRes.code === 0 && providersRes.data) {
        setProviders(providersRes.data.deploy || {})
      }
    } catch (error) {
      console.error('Failed to load data:', error)
      toast.error('加载数据失败')
    } finally {
      setLoading(false)
    }
  }

  // 根据选中的账户获取 deploy_config
  const currentDeployConfig = useMemo(() => {
    if (!formData.account_id) return []
    const account = accounts.find(a => a.id.toString() === formData.account_id)
    if (!account) return []
    const provider = providers[account.type]
    return provider?.deploy_config || []
  }, [formData.account_id, accounts, providers])

  const currentDeployProvider = useMemo(() => {
    if (!formData.account_id) return undefined
    const account = accounts.find(a => a.id.toString() === formData.account_id)
    if (!account) return undefined
    return providers[account.type]
  }, [formData.account_id, accounts, providers])

  const issuedOrders = useMemo(
    () => [...orders].filter((o) => o.status === 3).sort(compareIssuedCertOrders),
    [orders],
  )

  const orderSelectTriggerClass =
    'w-full min-h-[4.25rem] h-auto items-start py-2.5 whitespace-normal text-left [&_[data-slot=select-value]]:line-clamp-none [&_[data-slot=select-value]]:w-full [&_[data-slot=select-value]]:items-start [&_[data-slot=select-value]]:text-left'

  const applyAccountDeployDefaults = (accountId: string, base: Record<string, string> = {}) => {
    const account = accounts.find(a => a.id.toString() === accountId)
    const fields = account ? providers[account.type]?.deploy_config : undefined
    return mergeProviderFieldDefaults(fields, base)
  }

  // 渲染配置字段
  const renderConfigField = (field: ProviderConfigField) => {
    const raw = formData.config[field.key]
    const value = raw ?? field.value ?? ''

    if (field.type === 'radio' && field.options) {
      const v = value || field.value || field.options[0]?.value || ''
      return (
        <RadioGroup
          value={v}
          onValueChange={(nv) =>
            setFormData((prev) => ({ ...prev, config: { ...prev.config, [field.key]: nv } }))
          }
          className="flex flex-wrap gap-4"
        >
          {field.options.map((opt) => (
            <div key={opt.value} className="flex items-center space-x-2">
              <RadioGroupItem value={opt.value} id={`deploy-${field.key}-${opt.value}`} />
              <Label htmlFor={`deploy-${field.key}-${opt.value}`} className="font-normal cursor-pointer">
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
            setFormData((prev) => ({ ...prev, config: { ...prev.config, [field.key]: v } }))
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
            setFormData((prev) => ({ ...prev, config: { ...prev.config, [field.key]: e.target.value } }))
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
          setFormData((prev) => ({ ...prev, config: { ...prev.config, [field.key]: e.target.value } }))
        }
        placeholder={field.placeholder}
      />
    )
  }

  const handleAdd = () => {
    setFormData({
      account_id: '',
      order_id: '',
      config: {},
      remark: '',
    })
    setShowAddDialog(true)
  }

  const handleEdit = (deploy: CertDeploy) => {
    setSelectedDeploy(deploy)
    let config = {}
    try {
      config = deploy.config ? JSON.parse(deploy.config) : {}
    } catch {
      // ignore
    }
    const acc = accounts.find((a) => a.id === deploy.aid)
    const fields = acc ? providers[acc.type]?.deploy_config : undefined
    const merged = mergeProviderFieldDefaults(fields, config as Record<string, string>)
    setFormData({
      account_id: deploy.aid.toString(),
      order_id: deploy.oid.toString(),
      config: merged,
      remark: deploy.remark || '',
    })
    setShowEditDialog(true)
  }

  const handleSubmit = async (isEdit: boolean) => {
    if (!formData.account_id || !formData.order_id) {
      toast.error('请选择部署账户和证书订单')
      return
    }

    for (const field of currentDeployConfig) {
      if (!isDeployFieldVisible(field, formData.config)) continue
      const v = formData.config[field.key]
      if (field.required && (v === undefined || String(v).trim() === '')) {
        toast.error(`请填写${field.name}`)
        return
      }
    }

    setSubmitting(true)
    try {
      const data = {
        account_id: parseInt(formData.account_id),
        order_id: parseInt(formData.order_id),
        config: formData.config,
        remark: formData.remark,
      }

      let res
      if (isEdit && selectedDeploy) {
        res = await certApi.updateDeploy(selectedDeploy.id, data)
      } else {
        res = await certApi.createDeploy(data)
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

  const handleProcess = async (deploy: CertDeploy) => {
    try {
      const res = await certApi.processDeploy(deploy.id)
      if (res.code === 0) {
        toast.success(res.msg || '部署成功')
        loadData()
      } else {
        toast.error(res.msg || '部署失败')
      }
    } catch {
      toast.error('部署失败')
    }
  }

  const handleDelete = (deploy: CertDeploy) => {
    setSelectedDeploy(deploy)
    setShowDeleteDialog(true)
  }

  const confirmDelete = async () => {
    if (!selectedDeploy) return
    try {
      const res = await certApi.deleteDeploy(selectedDeploy.id)
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
      setSelectedDeploy(null)
    }
  }

  const filteredDeploys = deploys.filter(deploy => {
    const matchKeyword = !keyword || 
      (deploy.domains && deploy.domains.some(d => d.includes(keyword))) ||
      deploy.remark?.includes(keyword)
    const matchStatus = statusFilter === 'all' || 
      (statusFilter === 'error' && deploy.status < 0) ||
      deploy.status.toString() === statusFilter
    return matchKeyword && matchStatus
  })

  // Stats
  const deployedCount = deploys.filter(d => d.status === 2).length
  const pendingCount = deploys.filter(d => d.status === 0).length
  const runningCount = deploys.filter(d => d.status === 1).length
  const errorCount = deploys.filter(d => d.status < 0).length

  const getStatusBadge = (status: number) => {
    const statusInfo = DEPLOY_STATUS_MAP[status] || { label: '未知', color: 'bg-gray-500', icon: null }
    return (
      <Badge className={`${statusInfo.color} text-white flex items-center gap-1`}>
        {statusInfo.icon}
        {statusInfo.label}
      </Badge>
    )
  }

  const renderOrderCell = (orderId: number) => {
    const order = orders.find((o) => o.id === orderId)
    if (!order) {
      return <span className="text-muted-foreground tabular-nums">订单 #{orderId}</span>
    }
    const exp = formatCertOrderExpiryLine(order)
    return (
      <div className="max-w-[280px] space-y-1">
        <p className="text-sm font-medium leading-snug break-all">{certOrderDomainsLine(order, 3)}</p>
        <div className="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-xs text-muted-foreground">
          <span className="tabular-nums">#{order.id}</span>
          <span>{certOrderKindShort(order)}</span>
          <span
            className={
              exp.text === '已过期'
                ? 'text-destructive font-medium'
                : exp.urgent
                  ? 'text-amber-700 dark:text-amber-400'
                  : ''
            }
          >
            {exp.text}
          </span>
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-violet-500 to-purple-600 flex items-center justify-center shadow-lg shadow-violet-500/20">
              <Upload className="h-5 w-5 text-white" />
            </div>
            部署管理
          </h1>
          <p className="text-muted-foreground mt-1">管理SSL证书的自动部署任务</p>
        </div>
        <div className="flex gap-2">
          <Link href="/dashboard/accounts">
            <Button variant="outline">
              <Server className="h-4 w-4 mr-2" />
              账户管理
            </Button>
          </Link>
          <Button onClick={handleAdd} className="bg-gradient-to-r from-violet-600 to-purple-600 hover:from-violet-500 hover:to-purple-500">
            <Plus className="h-4 w-4 mr-2" />
            添加部署任务
          </Button>
        </div>
      </div>

      {/* Stats Cards */}
      <div className="grid gap-4 md:grid-cols-4">
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">已部署</CardTitle>
            <CheckCircle className="h-4 w-4 text-green-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-green-600">{deployedCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">待处理</CardTitle>
            <Clock className="h-4 w-4 text-gray-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-gray-600">{pendingCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">部署中</CardTitle>
            <RefreshCw className="h-4 w-4 text-blue-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-blue-600">{runningCount}</div>
          </CardContent>
        </Card>
        <Card>
          <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
            <CardTitle className="text-sm font-medium">部署失败</CardTitle>
            <XCircle className="h-4 w-4 text-red-500" />
          </CardHeader>
          <CardContent>
            <div className="text-2xl font-bold text-red-600">{errorCount}</div>
          </CardContent>
        </Card>
      </div>

      {/* Deploy List */}
      <Card>
        <CardHeader>
          <CardTitle>部署任务列表</CardTitle>
          <CardDescription>查看和管理所有SSL证书部署任务</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex flex-col sm:flex-row gap-4 mb-6">
            <div className="relative flex-1">
              <Search className="absolute left-3 top-1/2 transform -translate-y-1/2 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="搜索域名或备注..."
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
                <SelectItem value="0">待处理</SelectItem>
                <SelectItem value="2">已部署</SelectItem>
                <SelectItem value="error">部署失败</SelectItem>
              </SelectContent>
            </Select>
            <Button variant="outline" onClick={loadData}>
              <RefreshCw className="h-4 w-4 mr-2" />
              刷新
            </Button>
          </div>

          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">
                    <Checkbox 
                      checked={selectedDeploys.length === filteredDeploys.length && filteredDeploys.length > 0}
                      onCheckedChange={(checked) => {
                        setSelectedDeploys(checked ? filteredDeploys.map(d => d.id) : [])
                      }}
                    />
                  </TableHead>
                  <TableHead>证书订单</TableHead>
                  <TableHead>部署账户</TableHead>
                  <TableHead>证书类型</TableHead>
                  <TableHead>状态</TableHead>
                  <TableHead>启用</TableHead>
                  <TableHead>备注</TableHead>
                  <TableHead className="text-right">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {loading ? (
                  <TableRow>
                    <TableCell colSpan={8} className="text-center py-8">
                      <RefreshCw className="h-6 w-6 animate-spin mx-auto mb-2 text-muted-foreground" />
                      <span className="text-muted-foreground">加载中...</span>
                    </TableCell>
                  </TableRow>
                ) : filteredDeploys.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={8} className="text-center py-8">
                      <Upload className="h-12 w-12 mx-auto mb-2 text-muted-foreground/50" />
                      <p className="text-muted-foreground">暂无部署任务</p>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredDeploys.map((deploy) => (
                    <TableRow key={deploy.id}>
                      <TableCell>
                        <Checkbox 
                          checked={selectedDeploys.includes(deploy.id)}
                          onCheckedChange={(checked) => {
                            setSelectedDeploys(checked 
                              ? [...selectedDeploys, deploy.id]
                              : selectedDeploys.filter(id => id !== deploy.id)
                            )
                          }}
                        />
                      </TableCell>
                      <TableCell>{renderOrderCell(deploy.oid)}</TableCell>
                      <TableCell>
                        <span className="text-sm">{deploy.type_name || accounts.find(a => a.id === deploy.aid)?.name || '-'}</span>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm">{deploy.cert_type_name || '-'}</span>
                      </TableCell>
                      <TableCell>{getStatusBadge(deploy.status)}</TableCell>
                      <TableCell>
                        <Switch
                          checked={deploy.active}
                          onCheckedChange={async () => {
                            try {
                              await certApi.updateDeploy(deploy.id, { ...deploy, active: !deploy.active })
                              loadData()
                            } catch {
                              toast.error('操作失败')
                            }
                          }}
                        />
                      </TableCell>
                      <TableCell>
                        <span className="text-sm text-muted-foreground">{deploy.remark || '-'}</span>
                      </TableCell>
                      <TableCell className="text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button size="sm" variant="ghost" onClick={() => handleProcess(deploy)} title="执行部署">
                            <Play className="h-4 w-4" />
                          </Button>
                          <Button size="sm" variant="ghost" onClick={() => handleEdit(deploy)} title="编辑">
                            <Settings className="h-4 w-4" />
                          </Button>
                          <Button size="sm" variant="ghost" className="text-red-500 hover:text-red-600" onClick={() => handleDelete(deploy)} title="删除">
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* Add Dialog */}
      <Dialog open={showAddDialog} onOpenChange={setShowAddDialog}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>添加部署任务</DialogTitle>
            <DialogDescription>创建新的SSL证书部署任务</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>部署账户 *</Label>
              <Select
                value={formData.account_id}
                onValueChange={(v) =>
                  setFormData((prev) => ({
                    ...prev,
                    account_id: v,
                    config: applyAccountDeployDefaults(v, {}),
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择部署账户" />
                </SelectTrigger>
                <SelectContent>
                  {accounts.map((acc) => (
                    <SelectItem key={acc.id} value={acc.id.toString()}>
                      {acc.name} ({acc.type_name || acc.type})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {accounts.length === 0 && (
                <p className="text-xs text-muted-foreground">暂无部署账户，请先添加部署账户</p>
              )}
            </div>
            <div className="space-y-2">
              <Label>证书订单 *</Label>
              <p className="text-xs text-muted-foreground">
                仅显示已签发订单；按剩余有效期排序，快过期在前。下拉内可看到域名、CA、密钥、验证方式与到期。
              </p>
              <Select
                value={formData.order_id}
                onValueChange={(v) => setFormData({ ...formData, order_id: v })}
              >
                <SelectTrigger className={orderSelectTriggerClass}>
                  <SelectValue placeholder="选择已签发的证书订单" />
                </SelectTrigger>
                <SelectContent position="popper" className="max-h-[min(70vh,380px)] w-[min(100vw-2rem,520px)] max-w-[520px]">
                  {issuedOrders.map((order) => (
                    <CertOrderSelectItem key={order.id} order={order} />
                  ))}
                </SelectContent>
              </Select>
              {issuedOrders.length === 0 && (
                <p className="text-xs text-muted-foreground">暂无已签发的证书订单</p>
              )}
            </div>

            {currentDeployProvider?.note && (
              <p className="text-sm text-muted-foreground border-l-2 border-primary/40 pl-3 py-1">
                {currentDeployProvider.note}
              </p>
            )}
            {currentDeployProvider?.deploy_note && (
              <div className="flex gap-2 rounded-md border border-amber-200/80 bg-amber-50/80 dark:border-amber-900/50 dark:bg-amber-950/40 px-3 py-2 text-sm text-amber-900 dark:text-amber-100">
                <Info className="h-4 w-4 shrink-0 mt-0.5" />
                <span>{currentDeployProvider.deploy_note}</span>
              </div>
            )}

            {/* 部署配置字段 */}
            {currentDeployConfig
              .filter((field) => evaluateDeployFieldShow(field.show, formData.config))
              .map((field) => (
                <div key={field.key} className="space-y-2">
                  <Label>
                    {field.name}
                    {field.required && <span className="text-destructive ml-1">*</span>}
                  </Label>
                  {renderConfigField(field)}
                  {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
                </div>
              ))}

            <div className="space-y-2">
              <Label>备注</Label>
              <Textarea
                placeholder="输入备注信息"
                value={formData.remark}
                onChange={(e) => setFormData({ ...formData, remark: e.target.value })}
                rows={2}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowAddDialog(false)}>取消</Button>
            <Button onClick={() => handleSubmit(false)} disabled={submitting}>
              {submitting ? '提交中...' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Edit Dialog */}
      <Dialog open={showEditDialog} onOpenChange={setShowEditDialog}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>编辑部署任务</DialogTitle>
            <DialogDescription>修改部署任务配置</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>部署账户 *</Label>
              <Select
                value={formData.account_id}
                onValueChange={(v) =>
                  setFormData((prev) => ({
                    ...prev,
                    account_id: v,
                    config: applyAccountDeployDefaults(v, {}),
                  }))
                }
              >
                <SelectTrigger>
                  <SelectValue placeholder="选择部署账户" />
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
              <Label>证书订单 *</Label>
              <Select
                value={formData.order_id}
                onValueChange={(v) => setFormData({ ...formData, order_id: v })}
              >
                <SelectTrigger className={orderSelectTriggerClass}>
                  <SelectValue placeholder="选择已签发的证书订单" />
                </SelectTrigger>
                <SelectContent position="popper" className="max-h-[min(70vh,380px)] w-[min(100vw-2rem,520px)] max-w-[520px]">
                  {issuedOrders.map((order) => (
                    <CertOrderSelectItem key={order.id} order={order} />
                  ))}
                </SelectContent>
              </Select>
            </div>

            {currentDeployProvider?.note && (
              <p className="text-sm text-muted-foreground border-l-2 border-primary/40 pl-3 py-1">
                {currentDeployProvider.note}
              </p>
            )}
            {currentDeployProvider?.deploy_note && (
              <div className="flex gap-2 rounded-md border border-amber-200/80 bg-amber-50/80 dark:border-amber-900/50 dark:bg-amber-950/40 px-3 py-2 text-sm text-amber-900 dark:text-amber-100">
                <Info className="h-4 w-4 shrink-0 mt-0.5" />
                <span>{currentDeployProvider.deploy_note}</span>
              </div>
            )}

            {/* 部署配置字段 */}
            {currentDeployConfig
              .filter((field) => evaluateDeployFieldShow(field.show, formData.config))
              .map((field) => (
                <div key={field.key} className="space-y-2">
                  <Label>
                    {field.name}
                    {field.required && <span className="text-destructive ml-1">*</span>}
                  </Label>
                  {renderConfigField(field)}
                  {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
                </div>
              ))}

            <div className="space-y-2">
              <Label>备注</Label>
              <Textarea
                placeholder="输入备注信息"
                value={formData.remark}
                onChange={(e) => setFormData({ ...formData, remark: e.target.value })}
                rows={2}
              />
            </div>
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
              确定要删除该部署任务吗？此操作不可撤销。
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
    </div>
  )
}
