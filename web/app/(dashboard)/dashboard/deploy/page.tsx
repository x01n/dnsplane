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
import { Upload, Plus, Search, RefreshCw, Trash2, Play, CheckCircle, XCircle, Clock, Server, Settings } from 'lucide-react'
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

  // 根据选中的账户获取deploy_config
  const currentDeployConfig = useMemo(() => {
    if (!formData.account_id) return []
    const account = accounts.find(a => a.id.toString() === formData.account_id)
    if (!account) return []
    const provider = providers[account.type]
    return provider?.deploy_config || []
  }, [formData.account_id, accounts, providers])

  // 评估条件显示
  const evaluateShow = (show: string | undefined, config: Record<string, string>) => {
    if (!show) return true
    try {
      const showCondition = show.replace(/(\w+)/g, (match) => {
        if (config.hasOwnProperty(match)) {
          return `"${config[match]}"`
        }
        return match
      })
      return eval(showCondition)
    } catch {
      return true
    }
  }

  // 渲染配置字段
  const renderConfigField = (field: ProviderConfigField) => {
    const value = formData.config[field.key] || field.value || ''

    if (field.type === 'radio' && field.options) {
      return (
        <Select
          value={value}
          onValueChange={(v) =>
            setFormData((prev) => ({ ...prev, config: { ...prev.config, [field.key]: v } }))
          }
        >
          <SelectTrigger>
            <SelectValue placeholder={`请选择${field.name}`} />
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

    if (field.type === 'select' && field.options) {
      return (
        <Select
          value={value}
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
      return (
        <Textarea
          value={value}
          onChange={(e) =>
            setFormData((prev) => ({ ...prev, config: { ...prev.config, [field.key]: e.target.value } }))
          }
          placeholder={field.placeholder}
          rows={3}
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
    setFormData({
      account_id: deploy.aid.toString(),
      order_id: deploy.oid.toString(),
      config: config as Record<string, string>,
      remark: deploy.remark || '',
    })
    setShowEditDialog(true)
  }

  const handleSubmit = async (isEdit: boolean) => {
    if (!formData.account_id || !formData.order_id) {
      toast.error('请选择部署账户和证书订单')
      return
    }

    // 验证必填字段
    for (const field of currentDeployConfig) {
      if (field.required && !formData.config[field.key]) {
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
    } catch (error) {
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
    } catch (error) {
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
    } catch (error) {
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

  const getOrderDisplay = (orderId: number) => {
    const order = orders.find(o => o.id === orderId)
    if (!order) return `订单 #${orderId}`
    const domainStr = order.domains?.slice(0, 2).join(', ')
    if (order.domains && order.domains.length > 2) {
      return `${domainStr} 等${order.domains.length}个域名`
    }
    return domainStr || `订单 #${orderId}`
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
                      <TableCell>
                        <div className="max-w-[200px]">
                          {deploy.domains?.slice(0, 2).map((d, i) => (
                            <div key={i} className="truncate text-sm">{d}</div>
                          ))}
                          {deploy.domains && deploy.domains.length > 2 && (
                            <div className="text-xs text-muted-foreground">等 {deploy.domains.length} 个域名</div>
                          )}
                          {(!deploy.domains || deploy.domains.length === 0) && (
                            <span className="text-muted-foreground">{getOrderDisplay(deploy.oid)}</span>
                          )}
                        </div>
                      </TableCell>
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
                            } catch (e) {
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
        <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>添加部署任务</DialogTitle>
            <DialogDescription>创建新的SSL证书部署任务</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>部署账户 *</Label>
              <Select value={formData.account_id} onValueChange={(v) => setFormData({ ...formData, account_id: v, config: {} })}>
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
              <Select value={formData.order_id} onValueChange={(v) => setFormData({ ...formData, order_id: v })}>
                <SelectTrigger>
                  <SelectValue placeholder="选择证书订单" />
                </SelectTrigger>
                <SelectContent>
                  {orders.filter(o => o.status === 3).map((order) => (
                    <SelectItem key={order.id} value={order.id.toString()}>
                      {order.domains?.slice(0, 2).join(', ')} {order.domains && order.domains.length > 2 ? `等${order.domains.length}个域名` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              {orders.filter(o => o.status === 3).length === 0 && (
                <p className="text-xs text-muted-foreground">暂无已签发的证书订单</p>
              )}
            </div>
            
            {/* 部署配置字段 */}
            {currentDeployConfig.filter(field => evaluateShow(field.show, formData.config)).map((field) => (
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
        <DialogContent className="max-w-lg max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>编辑部署任务</DialogTitle>
            <DialogDescription>修改部署任务配置</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>部署账户 *</Label>
              <Select value={formData.account_id} onValueChange={(v) => setFormData({ ...formData, account_id: v, config: {} })}>
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
              <Select value={formData.order_id} onValueChange={(v) => setFormData({ ...formData, order_id: v })}>
                <SelectTrigger>
                  <SelectValue placeholder="选择证书订单" />
                </SelectTrigger>
                <SelectContent>
                  {orders.filter(o => o.status === 3).map((order) => (
                    <SelectItem key={order.id} value={order.id.toString()}>
                      {order.domains?.slice(0, 2).join(', ')} {order.domains && order.domains.length > 2 ? `等${order.domains.length}个域名` : ''}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            </div>

            {/* 部署配置字段 */}
            {currentDeployConfig.filter(field => evaluateShow(field.show, formData.config)).map((field) => (
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
