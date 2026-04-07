'use client'

import { useState, useEffect } from 'react'
import { Plus, Search, MoreHorizontal, Pencil, Trash2, RefreshCw, Loader2, ShieldCheck, FileText } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { DropdownMenu, DropdownMenuContent, DropdownMenuItem, DropdownMenuTrigger } from '@/components/ui/dropdown-menu'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { toast } from 'sonner'
import { certApi, api, CertAccount, CertProvider, CertProviderConfig, ProviderConfigField } from '@/lib/api'
import { formatDate } from '@/lib/utils'
import { ProviderBadge } from '@/components/provider-icon'
import Link from 'next/link'

export default function CertAccountsPage() {
  const [accounts, setAccounts] = useState<CertAccount[]>([])
  const [providers, setProviders] = useState<CertProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedAccount, setSelectedAccount] = useState<CertAccount | null>(null)
  const [submitting, setSubmitting] = useState(false)

  const [formData, setFormData] = useState({
    type: '',
    name: '',
    config: {} as Record<string, string>,
    remark: '',
  })

  useEffect(() => {
    fetchProviders()
    fetchAccounts()
  }, [])

  const fetchProviders = async () => {
    try {
      const res = await certApi.getProviders()
      if (res.code === 0 && res.data) {
        // 后端返回 {cert: {...}, deploy: {...}} 格式
        const certProviders = res.data.cert || {}
        // 转换为数组格式
        const providerList: CertProvider[] = Object.entries(certProviders).map(([type, cfg]: [string, CertProviderConfig]) => ({
          type,
          name: cfg.name,
          icon: cfg.icon,
          config: cfg.config || [],
          cname: cfg.cname,
          is_deploy: false,
        }))
        setProviders(providerList)
      }
    } catch {
      toast.error('获取证书服务商列表失败')
    }
  }

  const fetchAccounts = async () => {
    setLoading(true)
    try {
      const res = await certApi.getAccounts({ is_deploy: false })
      if (res.code === 0 && res.data) {
        const data = Array.isArray(res.data) ? res.data : (res.data as { list: CertAccount[] }).list || []
        setAccounts(data.filter(a => 
          !keyword || a.name.toLowerCase().includes(keyword.toLowerCase())
        ))
      }
    } catch {
      toast.error('获取账户列表失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    fetchAccounts()
  }

  const openEditDialog = (account: CertAccount) => {
    setSelectedAccount(account)
    let config = {}
    try {
      config = account.config ? JSON.parse(account.config) : {}
    } catch {
      // ignore
    }
    setFormData({
      type: account.type,
      name: account.name,
      config: config as Record<string, string>,
      remark: account.remark || '',
    })
    setDialogOpen(true)
  }

  const openDeleteDialog = (account: CertAccount) => {
    setSelectedAccount(account)
    setDeleteDialogOpen(true)
  }

  const handleSubmit = async () => {
    if (!formData.type || !formData.name) {
      toast.error('请填写必填项')
      return
    }

    setSubmitting(true)
    try {
      // 后端期望 config 是对象，不是字符串
      const res = await api.post(`/cert/accounts/${selectedAccount!.id}`, {
        type: formData.type,
        name: formData.name,
        config: formData.config,
        remark: formData.remark,
        is_deploy: false,
      })

      if (res.code === 0) {
        toast.success('修改成功')
        setDialogOpen(false)
        fetchAccounts()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async () => {
    if (!selectedAccount) return

    try {
      const res = await certApi.deleteAccount(selectedAccount.id)
      if (res.code === 0) {
        toast.success('删除成功')
        setDeleteDialogOpen(false)
        fetchAccounts()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  const currentProvider = providers.find((p) => p.type === formData.type)

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

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-emerald-500 to-teal-600 flex items-center justify-center shadow-lg shadow-emerald-500/20">
              <ShieldCheck className="h-5 w-5 text-white" />
            </div>
            SSL证书账户
          </h1>
          <p className="text-muted-foreground mt-1">管理SSL证书申请渠道账户</p>
        </div>
        <div className="flex gap-2">
          <Link href="/dashboard/cert">
            <Button variant="outline">
              <FileText className="h-4 w-4 mr-2" />
              证书订单
            </Button>
          </Link>
          <Link href="/dashboard/cert-accounts/add">
            <Button>
              <Plus className="h-4 w-4 mr-2" />
              添加账户
            </Button>
          </Link>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>账户列表</CardTitle>
          <CardDescription>查看和管理所有SSL证书申请账户</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center gap-4 mb-4">
            <form onSubmit={handleSearch} className="flex-1 flex gap-2">
              <div className="relative flex-1 max-w-sm">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="搜索账户名称..."
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  className="pl-9"
                />
              </div>
              <Button type="submit" variant="secondary">搜索</Button>
            </form>
            <Button variant="outline" onClick={fetchAccounts}>
              <RefreshCw className="h-4 w-4 mr-2" />
              刷新
            </Button>
          </div>

          {loading ? (
            <div className="flex items-center justify-center py-8">
              <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
            </div>
          ) : accounts.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              <ShieldCheck className="h-12 w-12 mx-auto mb-2 opacity-50" />
              <p>暂无数据，请添加证书账户</p>
            </div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>ID</TableHead>
                  <TableHead>账户名称</TableHead>
                  <TableHead>所属平台</TableHead>
                  <TableHead>备注</TableHead>
                  <TableHead>添加时间</TableHead>
                  <TableHead className="w-[100px]">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {accounts.map((account) => (
                  <TableRow key={account.id}>
                    <TableCell>{account.id}</TableCell>
                    <TableCell className="font-medium">{account.name}</TableCell>
                    <TableCell>
                      <ProviderBadge type={account.type} name={account.type_name} />
                    </TableCell>
                    <TableCell className="text-muted-foreground">{account.remark || '-'}</TableCell>
                    <TableCell>{formatDate(account.created_at)}</TableCell>
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => openEditDialog(account)}>
                            <Pencil className="h-4 w-4 mr-2" />
                            编辑
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => openDeleteDialog(account)} className="text-destructive">
                            <Trash2 className="h-4 w-4 mr-2" />
                            删除
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>

      {/* 编辑弹窗 */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>编辑账户</DialogTitle>
            <DialogDescription>修改SSL证书账户配置</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <Label>证书渠道</Label>
              <div className="flex items-center gap-2 p-2 bg-muted rounded-md">
                <ProviderBadge type={formData.type} name={currentProvider?.name} />
              </div>
            </div>

            <div className="space-y-2">
              <Label>账户名称 <span className="text-destructive">*</span></Label>
              <Input
                value={formData.name}
                onChange={(e) => setFormData({ ...formData, name: e.target.value })}
                placeholder="请输入账户名称"
              />
            </div>

            {currentProvider?.config?.map((field) => (
              <div key={field.key} className="space-y-2">
                <Label>
                  {field.name}
                  {field.required && <span className="text-destructive">*</span>}
                </Label>
                {renderConfigField(field)}
                {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
              </div>
            ))}

            <div className="space-y-2">
              <Label>备注</Label>
              <Textarea
                value={formData.remark}
                onChange={(e) => setFormData({ ...formData, remark: e.target.value })}
                placeholder="请输入备注信息"
                rows={2}
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={handleSubmit} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              确定
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 删除确认弹窗 */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除账户 &ldquo;{selectedAccount?.name}&rdquo; 吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive text-destructive-foreground hover:bg-destructive/90">
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
