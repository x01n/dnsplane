'use client'

import { useState, useEffect, useCallback } from 'react'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog'
import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
} from '@/components/ui/alert-dialog'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { toast } from 'sonner'
import { Loader2, Plus, RefreshCw, Search, Trash2, Pencil, Copy, ExternalLink } from 'lucide-react'
import { TableSkeleton } from '@/components/table-skeleton'
import { EmptyState } from '@/components/empty-state'
import { accountApi, cloudflareApi, domainApi, CustomHostname, Account, Domain } from '@/lib/api'

const SSL_STATUS_MAP: Record<string, { label: string; variant: 'default' | 'destructive' | 'secondary' | 'outline' }> = {
  'pending_validation': { label: '待验证', variant: 'secondary' },
  'active': { label: '已激活', variant: 'default' },
  'pending_deletion': { label: '待删除', variant: 'destructive' },
  'expired': { label: '已过期', variant: 'destructive' },
  'initializing': { label: '初始化中', variant: 'secondary' },
  'ssl_validating': { label: '验证中', variant: 'secondary' },
}

const SSL_METHOD_MAP: Record<string, string> = {
  'txt': 'TXT 验证',
  'http': 'HTTP 验证',
  'email': 'Email 验证',
}

export default function CloudflareHostnamesPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [selectedAccountId, setSelectedAccountId] = useState<string | null>(null)
  const [domains, setDomains] = useState<Domain[]>([])
  const [selectedDomainId, setSelectedDomainId] = useState<string | null>(null)
  const [domainsLoading, setDomainsLoading] = useState(false)

  const [hostnames, setHostnames] = useState<CustomHostname[]>([])
  const [loading, setLoading] = useState(true)
  const [accountsLoading, setAccountsLoading] = useState(true)
  const [searchKeyword, setSearchKeyword] = useState('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedItem, setSelectedItem] = useState<CustomHostname | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [refreshingId, setRefreshingId] = useState<string | null>(null)
  const [selectedHostnameIds, setSelectedHostnameIds] = useState<string[]>([])
  const [batchDialogOpen, setBatchDialogOpen] = useState(false)
  const [batchMode, setBatchMode] = useState<'add' | 'update'>('add')
  const [batchHostnames, setBatchHostnames] = useState('')

  // Fallback Origin
  const [fallbackOrigin, setFallbackOrigin] = useState('')
  const [fallbackSubmitting, setFallbackSubmitting] = useState(false)

  // DCV Delegation
  const [dcvUuid, setDcvUuid] = useState('')
  const [dcvLoading, setDcvLoading] = useState(false)

  // Form state
  const [formHostname, setFormHostname] = useState('')
  const [formOrigin, setFormOrigin] = useState('')
  const [formSslMethod, setFormSslMethod] = useState<'txt' | 'http'>('txt')
  const [formMinTls, setFormMinTls] = useState('1.0')
  const [isEdit, setIsEdit] = useState(false)

  const fetchAccounts = useCallback(async () => {
    setAccountsLoading(true)
    try {
      const res = await accountApi.list({ page_size: 100 })
      if (res.code === 0 && res.data) {
        const cfAccounts = (res.data.list || []).filter((acc: Account) => acc.type === 'cloudflare')
        setAccounts(cfAccounts)
        if (cfAccounts.length === 1) {
          setSelectedAccountId(String(cfAccounts[0].id))
        }
      }
    } catch {
      toast.error('获取账户列表失败')
    } finally {
      setAccountsLoading(false)
    }
  }, [])

  const fetchDomains = useCallback(async () => {
    if (!selectedAccountId) return
    setDomainsLoading(true)
    try {
      const res = await domainApi.list({ aid: Number(selectedAccountId), page_size: 100 })
      if (res.code === 0 && res.data) {
        const domainList = res.data.list || []
        setDomains(domainList)
        if (domainList.length === 1) {
          setSelectedDomainId(String(domainList[0].id))
        }
      }
    } catch {
      toast.error('获取域名列表失败')
    } finally {
      setDomainsLoading(false)
    }
  }, [selectedAccountId])

  const fetchHostnames = useCallback(async () => {
    if (!selectedDomainId) return
    setLoading(true)
    try {
      const res = await cloudflareApi.getHostnames(selectedDomainId)
      if (res.code === 0 && res.data) {
        setHostnames(res.data)
        setSelectedHostnameIds([])
      } else {
        toast.error(res.msg || '获取自定义主机名列表失败')
        setHostnames([])
      }
    } catch {
      toast.error('获取自定义主机名列表失败')
      setHostnames([])
    } finally {
      setLoading(false)
    }
  }, [selectedDomainId])

  const loadFallbackOrigin = useCallback(async () => {
    if (!selectedDomainId) return
    try {
      const res = await cloudflareApi.getFallbackOrigin(selectedDomainId)
      if (res.code === 0 && res.data) {
        setFallbackOrigin(res.data.origin || '')
      }
    } catch {
      // ignore
    }
  }, [selectedDomainId])

  const loadDcvUuid = useCallback(async () => {
    if (!selectedDomainId) return
    setDcvLoading(true)
    try {
      const res = await cloudflareApi.getDcvDelegationUuid(selectedDomainId)
      if (res.code === 0 && res.data) {
        setDcvUuid(res.data.uuid || '')
      }
    } catch {
      // ignore
    } finally {
      setDcvLoading(false)
    }
  }, [selectedDomainId])

  useEffect(() => {
    fetchAccounts()
  }, [fetchAccounts])

  useEffect(() => {
    if (selectedAccountId) {
      fetchDomains()
    } else {
      setDomains([])
      setSelectedDomainId(null)
    }
  }, [selectedAccountId, fetchDomains])

  useEffect(() => {
    if (selectedDomainId) {
      fetchHostnames()
      loadFallbackOrigin()
      loadDcvUuid()
    }
  }, [selectedDomainId, fetchHostnames, loadFallbackOrigin, loadDcvUuid])

  const handleSaveFallback = async () => {
    if (!selectedDomainId) return
    setFallbackSubmitting(true)
    try {
      const res = await cloudflareApi.setFallbackOrigin(selectedDomainId, fallbackOrigin)
      if (res.code === 0) {
        toast.success('Fallback Origin 保存成功')
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存 Fallback Origin 失败')
    } finally {
      setFallbackSubmitting(false)
    }
  }

  const handleDeleteFallback = async () => {
    if (!selectedDomainId) return
    try {
      const res = await cloudflareApi.deleteFallbackOrigin(selectedDomainId)
      if (res.code === 0) {
        setFallbackOrigin('')
        toast.success('Fallback Origin 已删除')
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除 Fallback Origin 失败')
    }
  }

  const openAddDialog = () => {
    setIsEdit(false)
    setFormHostname('')
    setFormOrigin('')
    setFormSslMethod('txt')
    setFormMinTls('1.0')
    setSelectedItem(null)
    setDialogOpen(true)
  }

  const openEditDialog = (item: CustomHostname) => {
    setIsEdit(true)
    setSelectedItem(item)
    setFormHostname(item.hostname)
    setFormOrigin(item.custom_origin_server || '')
    setFormSslMethod(item.ssl?.method === 'http' ? 'http' : 'txt')
    setFormMinTls(item.ssl?.min_tls_version || item.ssl?.settings?.min_tls_version || '1.0')
    setDialogOpen(true)
  }

  const handleSubmit = async () => {
    if (!selectedDomainId || !formHostname) return
    setSubmitting(true)
    try {
      const basePayload = {
        custom_origin_server: formOrigin || undefined,
        ssl_method: formSslMethod,
        min_tls_version: formMinTls,
      }
      const res = isEdit && selectedItem
        ? await cloudflareApi.updateHostname(selectedDomainId, { ...basePayload, hostname_id: selectedItem.id })
        : await cloudflareApi.addHostname(selectedDomainId, { ...basePayload, hostname: formHostname })
      if (res.code === 0) {
        toast.success(isEdit ? '更新成功' : '添加成功')
        setDialogOpen(false)
        fetchHostnames()
      } else {
        toast.error(res.msg || (isEdit ? '更新失败' : '添加失败'))
      }
    } catch {
      toast.error(isEdit ? '更新失败' : '添加失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDelete = async () => {
    if (!selectedDomainId || !selectedItem) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.deleteHostname(selectedDomainId, selectedItem.id)
      if (res.code === 0) {
        toast.success('删除成功')
        setDeleteDialogOpen(false)
        fetchHostnames()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleRefresh = async (item: CustomHostname) => {
    if (!selectedDomainId) return
    setRefreshingId(item.id)
    try {
      const res = await cloudflareApi.refreshHostname(selectedDomainId, item.id)
      if (res.code === 0) {
        toast.success('刷新成功')
        fetchHostnames()
      } else {
        toast.error(res.msg || '刷新失败')
      }
    } catch {
      toast.error('刷新失败')
    } finally {
      setRefreshingId(null)
    }
  }

  const filteredHostnames = hostnames.filter((h) =>
    !searchKeyword || h.hostname.toLowerCase().includes(searchKeyword.toLowerCase())
  )
  const selectedHostnameIdSet = new Set(selectedHostnameIds)
  const allFilteredSelected = filteredHostnames.length > 0 && filteredHostnames.every((item) => selectedHostnameIdSet.has(item.id))

  const toggleHostnameSelection = (id: string, checked: boolean | 'indeterminate') => {
    setSelectedHostnameIds((prev) => {
      if (checked === true) return prev.includes(id) ? prev : [...prev, id]
      return prev.filter((item) => item !== id)
    })
  }

  const toggleAllFilteredHostnames = (checked: boolean | 'indeterminate') => {
    const ids = filteredHostnames.map((item) => item.id)
    setSelectedHostnameIds((prev) => {
      if (checked === true) return Array.from(new Set([...prev, ...ids]))
      return prev.filter((item) => !ids.includes(item))
    })
  }

  const openBatchDialog = (mode: 'add' | 'update') => {
    setBatchMode(mode)
    setBatchHostnames('')
    setFormOrigin('')
    setFormSslMethod('txt')
    setFormMinTls('1.0')
    setBatchDialogOpen(true)
  }

  const handleBatchSubmit = async () => {
    if (!selectedDomainId) return
    if (batchMode === 'add' && !batchHostnames.trim()) return
    if (batchMode === 'update' && selectedHostnameIds.length === 0) return
    setSubmitting(true)
    try {
      const res = batchMode === 'add'
        ? await cloudflareApi.batchAddHostnames(selectedDomainId, {
          hostnames: batchHostnames,
          custom_origin_server: formOrigin || undefined,
          ssl_method: formSslMethod,
          min_tls_version: formMinTls,
        })
        : await cloudflareApi.batchUpdateHostnames(selectedDomainId, {
          hostname_ids: selectedHostnameIds,
          custom_origin_server: formOrigin,
          ssl_method: formSslMethod,
          min_tls_version: formMinTls,
        })
      if (res.code === 0) {
        const failedCount = res.data?.failed.length || 0
        toast.success(`${batchMode === 'add' ? '批量添加' : '批量修改'}完成，失败 ${failedCount} 个`)
        setBatchDialogOpen(false)
        fetchHostnames()
      } else {
        toast.error(res.msg || '批量操作失败')
      }
    } catch {
      toast.error('批量操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleBatchDelete = async () => {
    if (!selectedDomainId || selectedHostnameIds.length === 0) return
    if (!window.confirm(`确定要删除选中的 ${selectedHostnameIds.length} 个自定义主机名吗？`)) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.batchDeleteHostnames(selectedDomainId, selectedHostnameIds)
      if (res.code === 0) {
        const failedCount = res.data?.failed.length || 0
        toast.success(`批量删除完成，失败 ${failedCount} 个`)
        fetchHostnames()
      } else {
        toast.error(res.msg || '批量删除失败')
      }
    } catch {
      toast.error('批量删除失败')
    } finally {
      setSubmitting(false)
    }
  }

  const selectedDomain = domains.find(d => String(d.id) === selectedDomainId)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Cloudflare 自定义主机名</h1>
          <p className="text-muted-foreground mt-1">管理 Cloudflare 自定义主机名、证书状态与验证</p>
        </div>
      </div>

      {/* Account & Domain Selector */}
      <Card>
        <CardHeader>
          <CardTitle>选择账户与域名</CardTitle>
        </CardHeader>
        <CardContent>
          {accountsLoading ? (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span>加载账户中...</span>
            </div>
          ) : accounts.length === 0 ? (
            <EmptyState icon={ExternalLink} title="暂无 Cloudflare 账户" description="请先添加 Cloudflare 类型的 DNS 账户" />
          ) : (
            <div className="flex flex-col sm:flex-row gap-4">
              <div className="flex-1">
                <label className="text-sm font-medium mb-2 block">账户</label>
                <Select value={selectedAccountId || ''} onValueChange={(val) => {
                  setSelectedAccountId(val)
                  setSelectedDomainId(null)
                }}>
                  <SelectTrigger>
                    <SelectValue placeholder="选择 Cloudflare 账户" />
                  </SelectTrigger>
                  <SelectContent>
                    {accounts.map((acc) => (
                      <SelectItem key={acc.id} value={String(acc.id)}>
                        {acc.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {selectedAccountId && (
                <div className="flex-1">
                  <label className="text-sm font-medium mb-2 block">域名</label>
                  {domainsLoading ? (
                    <div className="flex items-center gap-2 text-muted-foreground h-10">
                      <Loader2 className="h-4 w-4 animate-spin" />
                      <span>加载域名中...</span>
                    </div>
                  ) : domains.length === 0 ? (
                    <p className="text-sm text-muted-foreground h-10 flex items-center">该账户下暂无域名</p>
                  ) : (
                    <Select value={selectedDomainId || ''} onValueChange={setSelectedDomainId}>
                      <SelectTrigger>
                        <SelectValue placeholder="选择域名" />
                      </SelectTrigger>
                      <SelectContent>
                        {domains.map((domain) => (
                          <SelectItem key={domain.id} value={String(domain.id)}>
                            {domain.name}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  )}
                </div>
              )}
            </div>
          )}
        </CardContent>
      </Card>

      {/* Main Content */}
      {selectedDomainId && (
        <>
          {/* Fallback Origin */}
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>Fallback Origin</CardTitle>
                {selectedDomain && (
                  <span className="text-sm text-muted-foreground">域名: {selectedDomain.name}</span>
                )}
              </div>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                <Input
                  value={fallbackOrigin}
                  onChange={(e) => setFallbackOrigin(e.target.value)}
                  placeholder="例如 origin.example.com"
                />
                <div className="flex gap-2">
                  <Button size="sm" onClick={handleSaveFallback} disabled={fallbackSubmitting}>
                    {fallbackSubmitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                    保存
                  </Button>
                  <Button size="sm" variant="outline" onClick={loadFallbackOrigin}>
                    刷新
                  </Button>
                  <Button size="sm" variant="destructive" onClick={handleDeleteFallback}>
                    清空
                  </Button>
                </div>
              </div>
            </CardContent>
          </Card>

          {/* DCV Delegation */}
          <Card>
            <CardHeader>
              <CardTitle>DCV 委派</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="space-y-3">
                <Input value={dcvUuid || ''} readOnly placeholder={dcvLoading ? '获取中...' : '未获取到 UUID'} />
                <div className="flex gap-2">
                  <Button size="sm" variant="outline" onClick={loadDcvUuid} disabled={dcvLoading}>
                    {dcvLoading && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                    刷新
                  </Button>
                  {dcvUuid && (
                    <Button size="sm" variant="outline" onClick={() => {
                      navigator.clipboard.writeText(`_dcv.${dcvUuid}.cf.dcval.cloudflare.net`)
                      toast.success('已复制 CNAME 目标')
                    }}>
                      <Copy className="h-4 w-4 mr-1" /> 复制 CNAME 目标
                    </Button>
                  )}
                </div>
              </div>
            </CardContent>
          </Card>

          {/* Hostnames List */}
          <Card>
            <CardHeader>
              <div className="flex items-center justify-between">
                <CardTitle>自定义主机名列表</CardTitle>
                <div className="flex gap-2">
                  <Button size="sm" onClick={openAddDialog}>
                    <Plus className="h-4 w-4 mr-1" /> 添加
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => openBatchDialog('add')}>
                    批量添加
                  </Button>
                  <Button size="sm" variant="outline" onClick={() => openBatchDialog('update')} disabled={selectedHostnameIds.length === 0}>
                    批量修改
                  </Button>
                  <Button size="sm" variant="destructive" onClick={handleBatchDelete} disabled={selectedHostnameIds.length === 0 || submitting}>
                    批量删除
                  </Button>
                  <Button size="sm" variant="outline" onClick={fetchHostnames}>
                    <RefreshCw className="h-4 w-4 mr-1" /> 刷新
                  </Button>
                </div>
              </div>
            </CardHeader>
            <CardContent>
              {/* Search */}
              <div className="mb-4">
                <div className="relative max-w-sm">
                  <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
                  <Input
                    placeholder="搜索主机名..."
                    value={searchKeyword}
                    onChange={(e) => setSearchKeyword(e.target.value)}
                    className="pl-8"
                  />
                </div>
              </div>

              {/* Table */}
              {loading ? (
                <TableSkeleton rows={5} columns={6} />
              ) : filteredHostnames.length === 0 ? (
                <EmptyState
                  icon={ExternalLink}
                  title="暂无自定义主机名"
                  description="点击上方添加按钮添加自定义主机名"
                />
              ) : (
                <div className="overflow-x-auto">
                  <Table>
                    <TableHeader>
                      <TableRow>
                        <TableHead className="w-10">
                          <Checkbox
                            checked={allFilteredSelected}
                            onCheckedChange={toggleAllFilteredHostnames}
                            aria-label="选择当前列表全部主机名"
                          />
                        </TableHead>
                        <TableHead>主机名</TableHead>
                        <TableHead>源站</TableHead>
                        <TableHead>证书状态</TableHead>
                        <TableHead>验证方法</TableHead>
                        <TableHead>最低 TLS</TableHead>
                        <TableHead className="text-right">操作</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {filteredHostnames.map((item) => {
                        const sslStatus = SSL_STATUS_MAP[item.ssl?.status] || { label: item.ssl?.status || '未知', variant: 'secondary' }
                        return (
                          <TableRow key={item.id}>
                            <TableCell>
                              <Checkbox
                                checked={selectedHostnameIdSet.has(item.id)}
                                onCheckedChange={(checked) => toggleHostnameSelection(item.id, checked)}
                                aria-label={`选择 ${item.hostname}`}
                              />
                            </TableCell>
                            <TableCell className="font-mono max-w-[250px] truncate" title={item.hostname}>
                              {item.hostname}
                            </TableCell>
                            <TableCell className="font-mono max-w-[200px] truncate" title={item.custom_origin_server || '-'}>
                              {item.custom_origin_server || <span className="text-muted-foreground">Fallback</span>}
                            </TableCell>
                            <TableCell>
                              <Badge variant={sslStatus.variant}>{sslStatus.label}</Badge>
                            </TableCell>
                            <TableCell>{SSL_METHOD_MAP[item.ssl?.method] || item.ssl?.method || '-'}</TableCell>
                            <TableCell>{item.ssl?.min_tls_version || item.ssl?.settings?.min_tls_version || '-'}</TableCell>
                            <TableCell className="text-right">
                              <div className="flex justify-end gap-1">
                                <Button size="icon" variant="ghost" onClick={() => handleRefresh(item)} disabled={refreshingId === item.id}>
                                  {refreshingId === item.id ? <Loader2 className="h-4 w-4 animate-spin" /> : <RefreshCw className="h-4 w-4" />}
                                </Button>
                                <Button size="icon" variant="ghost" onClick={() => openEditDialog(item)}>
                                  <Pencil className="h-4 w-4" />
                                </Button>
                                <Button size="icon" variant="ghost" onClick={() => { setSelectedItem(item); setDeleteDialogOpen(true) }}>
                                  <Trash2 className="h-4 w-4 text-destructive" />
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
            </CardContent>
          </Card>
        </>
      )}

      {/* Add/Edit Dialog */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{isEdit ? '编辑自定义主机名' : '添加自定义主机名'}</DialogTitle>
            <DialogDescription>
              {isEdit ? '修改自定义主机名配置' : '创建后主机名不能直接改名，如需改名请删除后重建。'}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div>
              <label className="text-sm font-medium">主机名</label>
              <Input
                value={formHostname}
                onChange={(e) => setFormHostname(e.target.value)}
                placeholder="例如 app.example.com 或 *.example.com"
                disabled={isEdit}
              />
            </div>
            <div>
              <label className="text-sm font-medium">自定义源站</label>
              <Input
                value={formOrigin}
                onChange={(e) => setFormOrigin(e.target.value)}
                placeholder="可留空，回退到 Fallback Origin"
              />
              <p className="text-xs text-muted-foreground mt-1">留空表示回退到 Fallback Origin 或默认源站逻辑</p>
            </div>
            <div>
              <label className="text-sm font-medium">证书验证方法</label>
              <select
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={formSslMethod}
                onChange={(e) => setFormSslMethod(e.target.value as 'txt' | 'http')}
              >
                <option value="txt">TXT 验证</option>
                <option value="http">HTTP 验证</option>
              </select>
            </div>
            <div>
              <label className="text-sm font-medium">最低 TLS 版本</label>
              <select
                className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                value={formMinTls}
                onChange={(e) => setFormMinTls(e.target.value)}
              >
                <option value="1.0">TLS 1.0</option>
                <option value="1.1">TLS 1.1</option>
                <option value="1.2">TLS 1.2</option>
                <option value="1.3">TLS 1.3</option>
              </select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={handleSubmit} disabled={submitting || !formHostname}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              确定
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Batch Dialog */}
      <Dialog open={batchDialogOpen} onOpenChange={setBatchDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>{batchMode === 'add' ? '批量添加自定义主机名' : '批量修改自定义主机名'}</DialogTitle>
            <DialogDescription>
              {batchMode === 'add' ? '每行或用逗号分隔一个主机名。' : `将修改选中的 ${selectedHostnameIds.length} 个自定义主机名。`}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            {batchMode === 'add' && (
              <div>
                <label className="text-sm font-medium">主机名列表</label>
                <textarea
                  className="min-h-28 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
                  value={batchHostnames}
                  onChange={(e) => setBatchHostnames(e.target.value)}
                  placeholder="app.example.com\napi.example.com"
                />
              </div>
            )}
            <div>
              <label className="text-sm font-medium">自定义源站</label>
              <Input value={formOrigin} onChange={(e) => setFormOrigin(e.target.value)} placeholder="留空可清空或回退到 Fallback Origin" />
            </div>
            <div>
              <label className="text-sm font-medium">证书验证方法</label>
              <select className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm" value={formSslMethod} onChange={(e) => setFormSslMethod(e.target.value as 'txt' | 'http')}>
                <option value="txt">TXT 验证</option>
                <option value="http">HTTP 验证</option>
              </select>
            </div>
            <div>
              <label className="text-sm font-medium">最低 TLS 版本</label>
              <select className="w-full rounded-md border border-input bg-background px-3 py-2 text-sm" value={formMinTls} onChange={(e) => setFormMinTls(e.target.value)}>
                <option value="1.0">TLS 1.0</option>
                <option value="1.1">TLS 1.1</option>
                <option value="1.2">TLS 1.2</option>
                <option value="1.3">TLS 1.3</option>
              </select>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBatchDialogOpen(false)}>取消</Button>
            <Button onClick={handleBatchSubmit} disabled={submitting || (batchMode === 'add' && !batchHostnames.trim()) || (batchMode === 'update' && selectedHostnameIds.length === 0)}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              确定
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Dialog */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除自定义主机名 &ldquo;{selectedItem?.hostname}&rdquo; 吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDelete} className="bg-destructive text-destructive-foreground hover:bg-destructive/90" disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
