'use client'

import { useCallback, useEffect, useState } from 'react'
import Link from 'next/link'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Textarea } from '@/components/ui/textarea'
import { EmptyState } from '@/components/empty-state'
import { TableSkeleton } from '@/components/table-skeleton'
import { quotaApi, Plan, QuotaMap } from '@/lib/api'
import { cn } from '@/lib/utils'
import { toast } from 'sonner'
import { AlertCircle, CheckCircle2, Gauge, Pencil, Plus, RefreshCw, Star, Trash2 } from 'lucide-react'

/**
 * 配额维度：key 与后端 applyQuotaJSON 读取的 JSON 键一一对应
 * 与 handler.quotaDimensionKeys 的顺序保持一致，便于两处对照
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

/** 功能开关：key 与 handler.featureLabels 对应 */
const FEATURE_KEYS = [
  { key: 'ssl', label: 'SSL 证书' },
  { key: 'monitor', label: '容灾监控' },
  { key: 'schedule', label: '定时切换' },
  { key: 'optimize_ip', label: '优选 IP' },
  { key: 'api_access', label: 'API 访问' },
  { key: 'batch_ops', label: '批量操作' },
  { key: 'export_import', label: '导入导出' },
] as const

/** 上限展示：-1 显示「不限」而非负数，避免误读成「倒欠」 */
function formatQuotaLimit(limit: number | undefined): string {
  if (limit === undefined) return '-'
  return limit < 0 ? '不限' : String(limit)
}

/** 空表单：所有维度默认不限、所有功能默认关闭 */
function emptyQuotaForm(): QuotaMap {
  const form: QuotaMap = {}
  QUOTA_DIMENSIONS.forEach((d) => {
    form[d.key] = -1
  })
  return form
}

export default function PlansPage() {
  const [plans, setPlans] = useState<Plan[]>([])
  const [loading, setLoading] = useState(true)
  const [loadError, setLoadError] = useState('')
  const [submitting, setSubmitting] = useState(false)

  const [dialogOpen, setDialogOpen] = useState(false)
  /** 编辑中的套餐 id；空表示新建 */
  const [editingId, setEditingId] = useState('')
  const [formName, setFormName] = useState('')
  const [formDescription, setFormDescription] = useState('')
  const [formStatus, setFormStatus] = useState(1)
  const [formSort, setFormSort] = useState(0)
  const [formQuotas, setFormQuotas] = useState<QuotaMap>(emptyQuotaForm)
  const [formFeatures, setFormFeatures] = useState<Record<string, boolean>>({})
  /* 套餐绑定的域名，逗号分隔；授予套餐时一并下发访问权限 */
  const [formDomains, setFormDomains] = useState('')

  const [deleteTarget, setDeleteTarget] = useState<Plan | null>(null)

  const loadPlans = useCallback(async () => {
    setLoading(true)
    try {
      const res = await quotaApi.listPlans()
      if (res.code === 0 && res.data) {
        setPlans(res.data.list || [])
        setLoadError('')
      } else {
        setLoadError(res.msg || '加载套餐失败')
      }
    } catch {
      setLoadError('加载套餐失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => {
    loadPlans()
  }, [loadPlans])

  const openCreate = () => {
    setEditingId('')
    setFormName('')
    setFormDescription('')
    setFormStatus(1)
    setFormSort(0)
    setFormQuotas(emptyQuotaForm())
    setFormFeatures({})
    setFormDomains('')
    setDialogOpen(true)
  }

  const openEdit = (plan: Plan) => {
    setEditingId(plan.id)
    setFormName(plan.name)
    setFormDescription(plan.description || '')
    setFormStatus(plan.status)
    setFormSort(plan.sort || 0)
    // 后端把 quotas 存成 JSON 字符串，这里解成维度映射；解析失败时退回全不限
    let parsed: QuotaMap = {}
    try {
      parsed = plan.quotas ? (JSON.parse(plan.quotas) as QuotaMap) : {}
    } catch {
      parsed = {}
    }
    const form = emptyQuotaForm()
    QUOTA_DIMENSIONS.forEach((d) => {
      if (parsed[d.key] !== undefined) form[d.key] = parsed[d.key]
    })
    setFormQuotas(form)

    let features: Record<string, boolean> = {}
    try {
      features = plan.features ? (JSON.parse(plan.features) as Record<string, boolean>) : {}
    } catch {
      features = {}
    }
    setFormFeatures(features)
    setDialogOpen(true)
  }

  const handleSubmit = async () => {
    const name = formName.trim()
    if (!name) {
      toast.error('套餐名称不能为空')
      return
    }
    setSubmitting(true)
    try {
      const payload = {
        name,
        description: formDescription,
        status: formStatus,
        sort: formSort,
        quotas: formQuotas,
        features: formFeatures,
        included_domains: formDomains,
      }
      const res = editingId
        ? await quotaApi.updatePlan(editingId, payload)
        : await quotaApi.createPlan(payload)
      if (res.code === 0) {
        toast.success(editingId ? '套餐已保存' : '套餐已创建')
        setDialogOpen(false)
        loadPlans()
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async () => {
    if (!deleteTarget) return
    setSubmitting(true)
    try {
      const res = await quotaApi.deletePlan(deleteTarget.id)
      if (res.code === 0) {
        toast.success('套餐已删除')
        setDeleteTarget(null)
        loadPlans()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleSetDefault = async (plan: Plan) => {
    try {
      const res = await quotaApi.setDefaultPlan(plan.id)
      if (res.code === 0) {
        toast.success(`已将「${plan.name}」设为默认套餐`)
        loadPlans()
      } else {
        toast.error(res.msg || '设置失败')
      }
    } catch {
      toast.error('设置失败')
    }
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-indigo-500 to-blue-600 flex items-center justify-center shadow-lg shadow-indigo-500/20">
              <Gauge className="h-5 w-5 text-white" />
            </div>
            套餐管理
          </h1>
          <p className="text-muted-foreground mt-1">
            配额模板：为不同用户群设定资源上限，由「用户管理」授予订阅
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={loadPlans} disabled={loading}>
            <RefreshCw className={cn('h-4 w-4 mr-2', loading && 'animate-spin')} />
            刷新
          </Button>
          <Button onClick={openCreate}>
            <Plus className="h-4 w-4 mr-2" />
            新建套餐
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>套餐列表</CardTitle>
          <CardDescription>
            上限取值：-1 不限、0 禁止创建、正数为上限。默认套餐会在新用户注册时自动参考。
            「子域名」按占用的子域名前缀计，同一前缀下的多条记录只占一条额度
          </CardDescription>
        </CardHeader>
        <CardContent>
          {loadError ? (
            <EmptyState
              icon={AlertCircle}
              title="加载失败"
              description={loadError}
              action="重试"
              onAction={loadPlans}
            />
          ) : loading ? (
            <TableSkeleton rows={4} columns={5} />
          ) : plans.length === 0 ? (
            <EmptyState
              icon={Gauge}
              title="暂无套餐"
              description="创建第一个套餐后即可在用户管理中授予订阅"
              action="新建套餐"
              onAction={openCreate}
            />
          ) : (
            <div className="rounded-md border">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>名称</TableHead>
                    <TableHead className="w-24">状态</TableHead>
                    <TableHead className="w-28">订阅数</TableHead>
                    <TableHead>配额</TableHead>
                    <TableHead className="text-right">操作</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {plans.map((plan) => {
                    let quotas: QuotaMap = {}
                    try {
                      quotas = plan.quotas ? (JSON.parse(plan.quotas) as QuotaMap) : {}
                    } catch {
                      quotas = {}
                    }
                    return (
                      <TableRow key={plan.id}>
                        <TableCell>
                          <div className="font-medium flex items-center gap-2">
                            {plan.name}
                            {plan.is_default && (
                              <Badge variant="outline" className="text-xs">
                                <Star className="h-3 w-3 mr-1" />
                                默认
                              </Badge>
                            )}
                          </div>
                          {plan.description && (
                            <p className="text-xs text-muted-foreground mt-0.5 max-w-md truncate" title={plan.description}>
                              {plan.description}
                            </p>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant={plan.status === 1 ? 'default' : 'secondary'}>
                            {plan.status === 1 ? '启用' : '停用'}
                          </Badge>
                        </TableCell>
                        <TableCell className="text-muted-foreground">{plan.subscriber_count}</TableCell>
                        <TableCell className="text-xs">
                          {QUOTA_DIMENSIONS.map((d) => (
                            <span key={d.key} className="inline-block mr-3 mb-1 text-muted-foreground">
                              {d.label} {formatQuotaLimit(quotas[d.key])}
                            </span>
                          ))}
                        </TableCell>
                        <TableCell className="text-right">
                          <div className="flex justify-end gap-1">
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8"
                              aria-label={`将 ${plan.name} 设为默认套餐`}
                              disabled={plan.is_default || plan.status !== 1}
                              onClick={() => handleSetDefault(plan)}
                            >
                              <CheckCircle2 className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8"
                              aria-label={`编辑套餐 ${plan.name}`}
                              onClick={() => openEdit(plan)}
                            >
                              <Pencil className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon"
                              className="h-8 w-8 text-destructive"
                              aria-label={`删除套餐 ${plan.name}`}
                              onClick={() => setDeleteTarget(plan)}
                            >
                              <Trash2 className="h-4 w-4" />
                            </Button>
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })}
                </TableBody>
              </Table>
            </div>
          )}
          <div className="mt-4">
            <Link href="/dashboard/users" className="text-sm text-primary hover:underline">
              ← 回到用户管理授予订阅
            </Link>
          </div>
        </CardContent>
      </Card>

      {/* 新建 / 编辑 Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{editingId ? '编辑套餐' : '新建套餐'}</DialogTitle>
            <DialogDescription>
              留 -1 表示不限、0 表示禁止创建；未填写的维度按不限处理
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-5">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>套餐名称 *</Label>
                <Input
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="例如：基础版"
                />
              </div>
              <div className="space-y-2">
                <Label>排序</Label>
                <Input
                  type="number"
                  value={formSort}
                  onChange={(e) => setFormSort(e.target.value === '' ? 0 : Number(e.target.value))}
                />
              </div>
            </div>
            <div className="space-y-2">
              <Label>描述</Label>
              <Textarea
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
                placeholder="可选，说明该套餐适用的用户群"
                rows={2}
              />
            </div>

            <div className="flex items-center justify-between p-3 rounded-lg border">
              <div>
                <p className="text-sm font-medium">启用</p>
                <p className="text-xs text-muted-foreground">停用的套餐无法授予给用户</p>
              </div>
              <Switch
                checked={formStatus === 1}
                onCheckedChange={(v) => setFormStatus(v ? 1 : 0)}
                aria-label="启用该套餐"
              />
            </div>

            <div className="space-y-2">
              <Label className="text-sm">绑定域名</Label>
              <Input
                value={formDomains}
                onChange={(e) => setFormDomains(e.target.value)}
                placeholder="a.example.com, b.example.com"
              />
              <p className="text-xs text-muted-foreground">
                授予该套餐时，这些域名的访问权限会一并下发给用户；撤销套餐时只回收这一部分，
                管理员单独授予的权限不受影响。与「域名」配额是两回事——那个限的是用户自己接入多少。
              </p>
            </div>

            <div className="space-y-3">
              <p className="text-sm font-medium">配额上限</p>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {QUOTA_DIMENSIONS.map((d) => (
                  <div key={d.key} className="flex items-center gap-2">
                    <Label className="w-24 shrink-0 text-sm">{d.label}</Label>
                    <Input
                      type="number"
                      min={-1}
                      value={formQuotas[d.key] ?? -1}
                      onChange={(e) => {
                        const raw = e.target.value
                        setFormQuotas({ ...formQuotas, [d.key]: raw === '' ? -1 : Number(raw) })
                      }}
                      className="w-24"
                    />
                    {(formQuotas[d.key] ?? -1) < 0 && (
                      <span className="text-xs text-muted-foreground">不限</span>
                    )}
                  </div>
                ))}
              </div>
            </div>

            <div className="space-y-3">
              <p className="text-sm font-medium">功能开关</p>
              <p className="text-xs text-muted-foreground">
                未出现的功能一律放行，只有显式关闭才会拦截
              </p>
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                {FEATURE_KEYS.map((f) => (
                  <div key={f.key} className="flex items-center justify-between p-2 rounded-lg border">
                    <Label className="text-sm">{f.label}</Label>
                    <Switch
                      checked={formFeatures[f.key] !== false}
                      onCheckedChange={(v) => setFormFeatures({ ...formFeatures, [f.key]: v })}
                      aria-label={`${f.label} 功能开关`}
                    />
                  </div>
                ))}
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)} disabled={submitting}>
              取消
            </Button>
            <Button onClick={handleSubmit} disabled={submitting}>
              {submitting && <RefreshCw className="h-4 w-4 mr-2 animate-spin" />}
              {editingId ? '保存' : '创建'}
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认 Dialog */}
      <Dialog open={deleteTarget !== null} onOpenChange={(open) => !open && setDeleteTarget(null)}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle>删除套餐</DialogTitle>
            <DialogDescription>
              确定要删除套餐「{deleteTarget?.name}」吗？
              {deleteTarget && deleteTarget.subscriber_count > 0 && (
                <span className="text-destructive">
                  {' '}
                  该套餐仍有 {deleteTarget.subscriber_count} 个订阅在用，需先撤销订阅。
                </span>
              )}
            </DialogDescription>
          </DialogHeader>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDeleteTarget(null)} disabled={submitting}>
              取消
            </Button>
            <Button
              variant="destructive"
              onClick={handleDelete}
              disabled={submitting || (deleteTarget?.subscriber_count ?? 0) > 0}
            >
              {submitting && <RefreshCw className="h-4 w-4 mr-2 animate-spin" />}
              删除
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}