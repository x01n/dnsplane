'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import {
  Plus,
  Search,
  MoreHorizontal,
  Pencil,
  Trash2,
  ExternalLink,
  RefreshCw,
  Loader2,
  Download,
  Globe,
  Layers,
} from 'lucide-react'
import { EmptyState } from '@/components/empty-state'
import { TableSkeleton } from '@/components/table-skeleton'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Card, CardContent, CardHeader } from '@/components/ui/card'
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
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
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
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Label } from '@/components/ui/label'
import { Checkbox } from '@/components/ui/checkbox'
import { Badge } from '@/components/ui/badge'
import { Separator } from '@/components/ui/separator'
import { toast } from 'sonner'
import { domainApi, accountApi, Domain, Account, DomainItem, WhoisInfo } from '@/lib/api'
import { formatDate, getDaysRemaining } from '@/lib/utils'
import { ProviderBadge } from '@/components/provider-icon'

const LIST_PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const
const LS_DOMAINS_PAGE_SIZE = 'dnsplane-domains-page-size'

function readStoredDomainsPageSize(): number {
  if (typeof window === 'undefined') return 20
  const n = parseInt(localStorage.getItem(LS_DOMAINS_PAGE_SIZE) || '', 10)
  return LIST_PAGE_SIZE_OPTIONS.includes(n as (typeof LIST_PAGE_SIZE_OPTIONS)[number]) ? n : 20
}

export default function DomainsPage() {
  const router = useRouter()
  const [domains, setDomains] = useState<Domain[]>([])
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize, setPageSize] = useState(readStoredDomainsPageSize)
  const [accounts, setAccounts] = useState<Account[]>([])
  const [loading, setLoading] = useState(true)
  const [keyword, setKeyword] = useState('')
  const [selectedAid, setSelectedAid] = useState<string>('')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedDomain, setSelectedDomain] = useState<Domain | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [selectedIds, setSelectedIds] = useState<number[]>([])

  // Edit form
  const [editFormData, setEditFormData] = useState({
    remark: '',
    is_notice: false,
    expire_time: '',
  })
  const [whoisLoading, setWhoisLoading] = useState(false)
  const [whoisError, setWhoisError] = useState<string | null>(null)
  const [whoisInfo, setWhoisInfo] = useState<WhoisInfo | null>(null)

  // Add domain (sync-style) dialog
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [addAccountId, setAddAccountId] = useState<string>('')
  const [addDomains, setAddDomains] = useState<DomainItem[]>([])
  const [addLoading, setAddLoading] = useState(false)
  const [addSelectedDomains, setAddSelectedDomains] = useState<DomainItem[]>([])

  // Sync dialog (kept for quick bulk import)
  const [syncDialogOpen, setSyncDialogOpen] = useState(false)
  const [syncAccountId, setSyncAccountId] = useState<string>('')
  const [syncDomains, setSyncDomains] = useState<DomainItem[]>([])
  const [syncLoading, setSyncLoading] = useState(false)
  const [syncSelectedDomains, setSyncSelectedDomains] = useState<DomainItem[]>([])

  // Cross-account import dialog
  const [crossImportOpen, setCrossImportOpen] = useState(false)
  const [crossLoading, setCrossLoading] = useState(false)
  const [crossAccountDomains, setCrossAccountDomains] = useState<{ account: Account; domains: DomainItem[]; loading: boolean }[]>([])
  const [crossSelectedDomains, setCrossSelectedDomains] = useState<{ accountId: string; domains: DomainItem[] }[]>([])

  useEffect(() => {
    fetchAccounts()
    fetchDomains()
  }, [])

  const fetchAccounts = async () => {
    try {
      const res = await accountApi.list({ page_size: 100 })
      if (res.code === 0 && res.data) {
        setAccounts(res.data.list || [])
      }
    } catch {
      // ignore
    }
  }

  const fetchDomains = async (p?: number, sizeOverride?: number) => {
    setLoading(true)
    try {
      const currentPage = p ?? page
      const ps = sizeOverride ?? pageSize
      const params: Record<string, string | number> = { page: currentPage, page_size: ps }
      if (keyword) params.keyword = keyword
      if (selectedAid) params.aid = selectedAid
      const res = await domainApi.list(params)
      if (res.code === 0 && res.data) {
        setDomains(res.data.list || [])
        setTotal(res.data.total || 0)
      }
    } catch {
      toast.error('获取域名列表失败')
    } finally {
      setLoading(false)
    }
  }

  /* 搜索防抖：输入停止 500ms 后自动触发搜索 */
  const debounceRef = useRef<ReturnType<typeof setTimeout> | null>(null)
  const handleKeywordChange = useCallback((value: string) => {
    setKeyword(value)
    if (debounceRef.current) clearTimeout(debounceRef.current)
    debounceRef.current = setTimeout(() => {
      setPage(1)
      fetchDomains(1)
    }, 500)
  }, []) // eslint-disable-line react-hooks/exhaustive-deps

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    if (debounceRef.current) clearTimeout(debounceRef.current)
    setPage(1)
    fetchDomains(1)
  }

  const handlePageSizeChange = (value: string) => {
    const next = parseInt(value, 10)
    if (!LIST_PAGE_SIZE_OPTIONS.includes(next as (typeof LIST_PAGE_SIZE_OPTIONS)[number])) return
    setPageSize(next)
    try {
      localStorage.setItem(LS_DOMAINS_PAGE_SIZE, String(next))
    } catch {
      // ignore
    }
    setPage(1)
    fetchDomains(1, next)
  }

  /* 根据到期天数返回表格行高亮 className */
  const getRowClassName = (domain: Domain) => {
    const days = getDaysRemaining(domain.expire_time)
    if (days === null) return ''
    if (days <= 0) return 'bg-red-50/50 dark:bg-red-950/20'
    if (days <= 30) return 'bg-yellow-50/50 dark:bg-yellow-950/20'
    return ''
  }

  // ============== Edit Dialog ==============
  const openEditDialog = (domain: Domain) => {
    setSelectedDomain(domain)
    setWhoisError(null)
    setWhoisInfo(null)
    setEditFormData({
      remark: domain.remark || '',
      is_notice: domain.is_notice || false,
      expire_time: domain.expire_time ? domain.expire_time.split('T')[0] : '',
    })
    setDialogOpen(true)
    handleQueryWhois(domain.id, false)
  }

  const handleQueryWhois = async (domainId: number, showToast = true) => {
    setWhoisLoading(true)
    setWhoisError(null)
    try {
      const res = await domainApi.queryWhois(domainId)
      if (res.code === 0 && res.data) {
        setWhoisInfo(res.data)
        if (res.data.expiry_date) {
          const expireDate = new Date(res.data.expiry_date).toISOString().split('T')[0]
          setEditFormData(prev => ({ ...prev, expire_time: expireDate }))
          setWhoisError(null)
        } else {
          setWhoisError('WHOIS未返回过期时间，请手动输入')
        }
        if (showToast) toast.success('WHOIS查询成功')
        fetchDomains()
      } else {
        setWhoisInfo(null)
        setWhoisError(res.msg || 'WHOIS查询失败，请手动输入过期时间')
        if (showToast) toast.error(res.msg || 'WHOIS查询失败')
      }
    } catch {
      setWhoisInfo(null)
      setWhoisError('WHOIS查询失败，请手动输入过期时间')
      if (showToast) toast.error('WHOIS查询失败')
    } finally {
      setWhoisLoading(false)
    }
  }

  const handleEditSubmit = async () => {
    if (!selectedDomain) return
    setSubmitting(true)
    try {
      const updateData: Record<string, unknown> = {
        remark: editFormData.remark,
        is_notice: editFormData.is_notice,
      }
      if (editFormData.expire_time) {
        updateData.expire_time = new Date(editFormData.expire_time).toISOString()
      }
      const res = await domainApi.update(selectedDomain.id, updateData)
      if (res.code === 0) {
        toast.success('修改成功')
        setDialogOpen(false)
        fetchDomains()
      } else {
        toast.error(res.msg || '修改失败')
      }
    } catch {
      toast.error('操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  // ============== Add Domain (sync-style) ==============
  const openAddDialog = () => {
    setAddAccountId('')
    setAddDomains([])
    setAddSelectedDomains([])
    setAddDialogOpen(true)
  }

  const handleLoadAddDomains = async () => {
    if (!addAccountId) {
      toast.error('请选择账户')
      return
    }
    setAddLoading(true)
    try {
      const res = await accountApi.getDomainList(Number(addAccountId), { page_size: 100 })
      if (res.code === 0 && res.data) {
        setAddDomains(res.data.list || [])
        setAddSelectedDomains([])
      } else {
        toast.error(res.msg || '获取域名列表失败')
      }
    } catch {
      toast.error('获取域名列表失败')
    } finally {
      setAddLoading(false)
    }
  }

  const handleAddImport = async () => {
    if (addSelectedDomains.length === 0) {
      toast.error('请选择要导入的域名')
      return
    }
    setSubmitting(true)
    try {
      const res = await domainApi.sync({
        aid: Number(addAccountId),
        domains: addSelectedDomains.map((d) => ({
          name: d.Domain,
          id: d.DomainId,
          record_count: d.RecordCount,
        })),
      })
      if (res.code === 0) {
        toast.success(`成功导入 ${addSelectedDomains.length} 个域名`)
        setAddDialogOpen(false)
        fetchDomains()
      } else {
        toast.error(res.msg || '导入失败')
      }
    } catch {
      toast.error('导入失败')
    } finally {
      setSubmitting(false)
    }
  }

  // ============== Sync Dialog (bulk) ==============
  const openSyncDialog = () => {
    setSyncAccountId('')
    setSyncDomains([])
    setSyncSelectedDomains([])
    setSyncDialogOpen(true)
  }

  const handleLoadSyncDomains = async () => {
    if (!syncAccountId) {
      toast.error('请选择账户')
      return
    }
    setSyncLoading(true)
    try {
      const res = await accountApi.getDomainList(Number(syncAccountId), { page_size: 100 })
      if (res.code === 0 && res.data) {
        setSyncDomains(res.data.list || [])
        setSyncSelectedDomains([])
      } else {
        toast.error(res.msg || '获取域名列表失败')
      }
    } catch {
      toast.error('获取域名列表失败')
    } finally {
      setSyncLoading(false)
    }
  }

  const handleSync = async () => {
    if (syncSelectedDomains.length === 0) {
      toast.error('请选择要同步的域名')
      return
    }
    setSubmitting(true)
    try {
      const res = await domainApi.sync({
        aid: Number(syncAccountId),
        domains: syncSelectedDomains.map((d) => ({
          name: d.Domain,
          id: d.DomainId,
          record_count: d.RecordCount,
        })),
      })
      if (res.code === 0) {
        toast.success('同步成功')
        setSyncDialogOpen(false)
        fetchDomains()
      } else {
        toast.error(res.msg || '同步失败')
      }
    } catch {
      toast.error('同步失败')
    } finally {
      setSubmitting(false)
    }
  }

  // ============== Cross-Account Import ==============
  const openCrossImport = async () => {
    setCrossImportOpen(true)
    setCrossLoading(true)
    setCrossAccountDomains([])
    setCrossSelectedDomains([])

    // Load all accounts' domains
    const results: { account: Account; domains: DomainItem[]; loading: boolean }[] = accounts.map(a => ({
      account: a,
      domains: [],
      loading: true,
    }))
    setCrossAccountDomains(results)

    const promises = accounts.map(async (account, index) => {
      try {
        const res = await accountApi.getDomainList(account.id, { page_size: 200 })
        if (res.code === 0 && res.data) {
          return { index, domains: res.data.list || [] }
        }
      } catch {
        // ignore
      }
      return { index, domains: [] }
    })

    const resolvedResults = await Promise.all(promises)
    const updated = [...results]
    resolvedResults.forEach(r => {
      updated[r.index] = { ...updated[r.index], domains: r.domains, loading: false }
    })
    setCrossAccountDomains(updated)
    setCrossLoading(false)
  }

  const toggleCrossSelect = (accountId: string, domain: DomainItem) => {
    setCrossSelectedDomains(prev => {
      const existing = prev.find(p => p.accountId === accountId)
      if (existing) {
        const hasDomain = existing.domains.some(d => d.DomainId === domain.DomainId)
        if (hasDomain) {
          const newDomains = existing.domains.filter(d => d.DomainId !== domain.DomainId)
          if (newDomains.length === 0) {
            return prev.filter(p => p.accountId !== accountId)
          }
          return prev.map(p => p.accountId === accountId ? { ...p, domains: newDomains } : p)
        } else {
          return prev.map(p => p.accountId === accountId ? { ...p, domains: [...p.domains, domain] } : p)
        }
      } else {
        return [...prev, { accountId, domains: [domain] }]
      }
    })
  }

  const isCrossDomainSelected = (accountId: string, domainId: string) => {
    return crossSelectedDomains.some(p => p.accountId === accountId && p.domains.some(d => d.DomainId === domainId))
  }

  const totalCrossSelected = crossSelectedDomains.reduce((s, p) => s + p.domains.length, 0)

  const handleCrossImport = async () => {
    if (totalCrossSelected === 0) {
      toast.error('请选择要导入的域名')
      return
    }
    setSubmitting(true)
    try {
      let successCount = 0
      let errorCount = 0
      for (const group of crossSelectedDomains) {
        try {
          const res = await domainApi.sync({
            aid: Number(group.accountId),
            domains: group.domains.map(d => ({
              name: d.Domain,
              id: d.DomainId,
              record_count: d.RecordCount,
            })),
          })
          if (res.code === 0) {
            successCount += group.domains.length
          } else {
            errorCount += group.domains.length
          }
        } catch {
          errorCount += group.domains.length
        }
      }
      if (successCount > 0) {
        toast.success(`成功导入 ${successCount} 个域名${errorCount > 0 ? `，${errorCount} 个失败` : ''}`)
      } else {
        toast.error('导入失败')
      }
      setCrossImportOpen(false)
      fetchDomains()
    } catch {
      toast.error('导入失败')
    } finally {
      setSubmitting(false)
    }
  }

  // ============== Delete ==============
  const openDeleteDialog = (domain: Domain) => {
    setSelectedDomain(domain)
    setDeleteDialogOpen(true)
  }

  const handleDelete = async () => {
    if (!selectedDomain) return
    try {
      const res = await domainApi.delete(selectedDomain.id)
      if (res.code === 0) {
        toast.success('删除成功')
        setDeleteDialogOpen(false)
        fetchDomains()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  const handleBatchDelete = async () => {
    if (selectedIds.length === 0) {
      toast.error('请选择要删除的域名')
      return
    }
    try {
      const res = await domainApi.batchAction({ ids: selectedIds, action: 'delete' })
      if (res.code === 0) {
        toast.success('批量删除成功')
        setSelectedIds([])
        fetchDomains()
      } else {
        toast.error(res.msg || '批量删除失败')
      }
    } catch {
      toast.error('批量删除失败')
    }
  }

  const toggleSelectAll = () => {
    if (selectedIds.length === domains.length) {
      setSelectedIds([])
    } else {
      setSelectedIds(domains.map((d) => d.id))
    }
  }

  const toggleSelect = (id: number) => {
    if (selectedIds.includes(id)) {
      setSelectedIds(selectedIds.filter((i) => i !== id))
    } else {
      setSelectedIds([...selectedIds, id])
    }
  }

  const getExpireStatus = (domain: Domain) => {
    const days = getDaysRemaining(domain.expire_time)
    if (days === null) return null
    if (days <= 0) {
      return <Badge variant="destructive">已过期</Badge>
    }
    if (days <= 30) {
      return <Badge variant="outline" className="bg-yellow-50 dark:bg-yellow-950 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-800">即将过期 ({days}天)</Badge>
    }
    return <Badge variant="outline" className="bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300 border-green-200 dark:border-green-800">{days}天</Badge>
  }

  // Shared domain list component used in add/sync dialogs
  const DomainListTable = ({
    domainList,
    selectedList,
    onToggle,
    onToggleAll,
  }: {
    domainList: DomainItem[]
    selectedList: DomainItem[]
    onToggle: (d: DomainItem, checked: boolean) => void
    onToggleAll: (checked: boolean) => void
  }) => (
    <div className="border rounded-lg max-h-80 overflow-auto">
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead className="w-12">
              <Checkbox
                checked={selectedList.length === domainList.filter(d => !d.disabled).length && domainList.filter(d => !d.disabled).length > 0}
                onCheckedChange={(checked) => onToggleAll(!!checked)}
              />
            </TableHead>
            <TableHead>域名</TableHead>
            <TableHead>记录数</TableHead>
            <TableHead>状态</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {domainList.map((domain) => (
            <TableRow key={domain.DomainId}>
              <TableCell>
                <Checkbox
                  checked={selectedList.some(d => d.DomainId === domain.DomainId)}
                  disabled={domain.disabled}
                  onCheckedChange={(checked) => onToggle(domain, !!checked)}
                />
              </TableCell>
              <TableCell className="font-medium">{domain.Domain}</TableCell>
              <TableCell>{domain.RecordCount}</TableCell>
              <TableCell>
                {domain.disabled ? (
                  <Badge variant="secondary">已存在</Badge>
                ) : (
                  <Badge variant="outline">可导入</Badge>
                )}
              </TableCell>
            </TableRow>
          ))}
        </TableBody>
      </Table>
    </div>
  )

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">域名管理</h1>
          <p className="text-muted-foreground">管理您的域名和DNS解析</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={openCrossImport}>
            <Layers className="h-4 w-4 mr-2" />
            跨账户导入
          </Button>
          <Button variant="outline" onClick={openSyncDialog}>
            <Download className="h-4 w-4 mr-2" />
            同步域名
          </Button>
          <Button onClick={openAddDialog}>
            <Plus className="h-4 w-4 mr-2" />
            添加域名
          </Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <div className="flex flex-col sm:flex-row items-start sm:items-center gap-4">
            <form onSubmit={handleSearch} className="flex-1 flex gap-2">
              <div className="relative flex-1 max-w-sm">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="搜索域名..."
                  value={keyword}
                  onChange={(e) => handleKeywordChange(e.target.value)}
                  className="pl-9"
                />
              </div>
              <Select value={selectedAid || 'all'} onValueChange={(v) => setSelectedAid(v === 'all' ? '' : v)}>
                <SelectTrigger className="w-40">
                  <SelectValue placeholder="全部账户" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部账户</SelectItem>
                  {accounts.map((account) => (
                    <SelectItem key={account.id} value={account.id.toString()}>
                      {account.name}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button type="submit" variant="secondary">搜索</Button>
            </form>
            {selectedIds.length > 0 && (
              <div className="flex items-center gap-2">
                <span className="text-sm text-muted-foreground">已选 {selectedIds.length} 项</span>
                <Button variant="destructive" size="sm" onClick={handleBatchDelete}>
                  批量删除
                </Button>
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {loading ? (
            <TableSkeleton rows={5} columns={7} />
          ) : domains.length === 0 ? (
            <EmptyState
              icon={Globe}
              title="暂无域名"
              description="还没有添加任何域名，请点击上方按钮添加"
            />
          ) : (
            <div className="overflow-x-auto -mx-6 px-6">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-12">
                    <Checkbox
                      checked={selectedIds.length === domains.length && domains.length > 0}
                      onCheckedChange={toggleSelectAll}
                    />
                  </TableHead>
                  <TableHead>域名</TableHead>
                  <TableHead>账户</TableHead>
                  <TableHead>记录数</TableHead>
                  <TableHead>过期时间</TableHead>
                  <TableHead>备注</TableHead>
                  <TableHead className="w-[100px]">操作</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {domains.map((domain) => (
                  <TableRow key={domain.id} className={getRowClassName(domain)}>
                    <TableCell>
                      <Checkbox
                        checked={selectedIds.includes(domain.id)}
                        onCheckedChange={() => toggleSelect(domain.id)}
                      />
                    </TableCell>
                    <TableCell>
                      <button
                        className="font-medium text-primary hover:underline"
                        onClick={() => router.push(`/dashboard/domains/${domain.id}`)}
                      >
                        {domain.name}
                      </button>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-2">
                        <span className="font-medium">{domain.account_name || '-'}</span>
                        {(domain.account_type || domain.type_name) && (
                          <ProviderBadge
                            type={domain.account_type || ''}
                            name={domain.type_name}
                          />
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      <div className="flex items-center gap-1">
                        <span>{domain.record_count}</span>
                        {domain.perm_sub && domain.perm_sub !== '*' && (
                          <Badge variant="outline" className="text-xs font-normal">{domain.perm_sub}</Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell>
                      {domain.expire_time ? (
                        <div className="flex items-center gap-2">
                          <span className="text-sm">{formatDate(domain.expire_time, 'date')}</span>
                          {getExpireStatus(domain)}
                        </div>
                      ) : (
                        '-'
                      )}
                    </TableCell>
                    <TableCell className="text-muted-foreground">{domain.remark || '-'}</TableCell>
                    <TableCell>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button variant="ghost" size="icon">
                            <MoreHorizontal className="h-4 w-4" />
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => router.push(`/dashboard/domains/${domain.id}`)}>
                            <ExternalLink className="h-4 w-4 mr-2" />
                            管理解析
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => openEditDialog(domain)}>
                            <Pencil className="h-4 w-4 mr-2" />
                            编辑
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => handleQueryWhois(domain.id)}>
                            <Globe className="h-4 w-4 mr-2" />
                            更新WHOIS
                          </DropdownMenuItem>
                          <DropdownMenuSeparator />
                          <DropdownMenuItem
                            onClick={() => openDeleteDialog(domain)}
                            className="text-destructive"
                          >
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
            </div>
          )}

          {/* Pagination */}
          {total > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-3 mt-4 pt-4 border-t">
              <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
                <span>
                  共 {total} 条，第 {page}/{Math.max(1, Math.ceil(total / pageSize))} 页
                </span>
                <div className="flex items-center gap-2">
                  <span className="text-muted-foreground whitespace-nowrap">每页</span>
                  <Select value={String(pageSize)} onValueChange={handlePageSizeChange}>
                    <SelectTrigger className="h-8 w-[92px]" aria-label="每页条数">
                      <SelectValue />
                    </SelectTrigger>
                    <SelectContent>
                      {LIST_PAGE_SIZE_OPTIONS.map((n) => (
                        <SelectItem key={n} value={String(n)}>
                          {n} 条
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>
              <div className="flex items-center gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page <= 1}
                  onClick={() => {
                    setPage(page - 1)
                    fetchDomains(page - 1)
                  }}
                >
                  上一页
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page >= Math.max(1, Math.ceil(total / pageSize))}
                  onClick={() => {
                    setPage(page + 1)
                    fetchDomains(page + 1)
                  }}
                >
                  下一页
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* ============== Edit Domain Dialog ============== */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>编辑域名</DialogTitle>
            <DialogDescription>修改域名配置信息</DialogDescription>
          </DialogHeader>
          {selectedDomain && (
            <div className="space-y-4">
              <div className="grid grid-cols-2 gap-4 p-3 bg-muted rounded-lg">
                <div>
                  <span className="text-sm text-muted-foreground">域名</span>
                  <p className="font-medium">{selectedDomain.name}</p>
                </div>
                <div>
                  <span className="text-sm text-muted-foreground">账户</span>
                  <div className="flex items-center gap-2 mt-1">
                    <span className="font-medium">{selectedDomain.account_name}</span>
                    {(selectedDomain.account_type || selectedDomain.type_name) && (
                      <ProviderBadge
                        type={selectedDomain.account_type || ''}
                        name={selectedDomain.type_name}
                      />
                    )}
                  </div>
                </div>
              </div>

              {/* WHOIS 信息展示 */}
              <div className="space-y-2">
                <div className="flex items-center justify-between">
                  <Label className="font-medium">WHOIS 信息</Label>
                  <Button
                    type="button"
                    variant="ghost"
                    size="sm"
                    onClick={() => selectedDomain && handleQueryWhois(selectedDomain.id)}
                    disabled={whoisLoading}
                  >
                    {whoisLoading ? (
                      <Loader2 className="h-4 w-4 animate-spin" />
                    ) : (
                      <Globe className="h-4 w-4" />
                    )}
                    <span className="ml-1">查询WHOIS</span>
                  </Button>
                </div>
                {whoisError && (
                  <p className="text-sm text-amber-600 dark:text-amber-400">{whoisError}</p>
                )}
                {whoisInfo && (
                  <div className="text-sm border rounded-lg p-3 space-y-1.5 bg-muted/50">
                    {whoisInfo.registrar && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">注册商</span>
                        <span className="font-medium truncate ml-4 max-w-[60%] text-right">{whoisInfo.registrar}</span>
                      </div>
                    )}
                    {whoisInfo.name_servers && whoisInfo.name_servers.length > 0 && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">NS服务器</span>
                        <span className="font-medium truncate ml-4 max-w-[60%] text-right">{whoisInfo.name_servers.join(', ')}</span>
                      </div>
                    )}
                    {whoisInfo.created_date && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">注册时间</span>
                        <span className="font-medium">{new Date(whoisInfo.created_date).toLocaleDateString()}</span>
                      </div>
                    )}
                    {whoisInfo.expiry_date && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">到期时间</span>
                        <span className="font-medium">{new Date(whoisInfo.expiry_date).toLocaleDateString()}</span>
                      </div>
                    )}
                    {whoisInfo.updated_date && (
                      <div className="flex justify-between">
                        <span className="text-muted-foreground">更新时间</span>
                        <span className="font-medium">{new Date(whoisInfo.updated_date).toLocaleDateString()}</span>
                      </div>
                    )}
                    {whoisInfo.status && whoisInfo.status.length > 0 && (
                      <div className="flex justify-between items-start">
                        <span className="text-muted-foreground shrink-0">状态</span>
                        <div className="flex flex-wrap gap-1 justify-end ml-4">
                          {whoisInfo.status.map((s, i) => (
                            <Badge key={i} variant="secondary" className="text-xs">{s}</Badge>
                          ))}
                        </div>
                      </div>
                    )}
                  </div>
                )}
                {whoisLoading && !whoisInfo && (
                  <div className="flex items-center justify-center py-3 text-sm text-muted-foreground">
                    <Loader2 className="h-4 w-4 animate-spin mr-2" />
                    正在查询WHOIS信息...
                  </div>
                )}
              </div>

              <Separator />

              <div className="space-y-2">
                <Label>过期时间</Label>
                <Input
                  type="date"
                  value={editFormData.expire_time}
                  onChange={(e) => {
                    setEditFormData({ ...editFormData, expire_time: e.target.value })
                    setWhoisError(null)
                  }}
                  placeholder="手动输入或自动获取"
                />
              </div>

              <div className="space-y-2">
                <Label>备注</Label>
                <Input
                  value={editFormData.remark}
                  onChange={(e) => setEditFormData({ ...editFormData, remark: e.target.value })}
                  placeholder="输入备注信息"
                />
              </div>

              <div className="flex items-center space-x-2">
                <Checkbox
                  id="is_notice"
                  checked={editFormData.is_notice}
                  onCheckedChange={(checked) => setEditFormData({ ...editFormData, is_notice: !!checked })}
                />
                <Label htmlFor="is_notice" className="cursor-pointer">启用临期通知</Label>
              </div>
            </div>
          )}
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={handleEditSubmit} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              确定
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ============== Add Domain Dialog (sync-style) ============== */}
      <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>添加域名</DialogTitle>
            <DialogDescription>从DNS服务商账户获取域名并导入</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex gap-2">
              <Select value={addAccountId || '_empty'} onValueChange={(v) => setAddAccountId(v === '_empty' ? '' : v)}>
                <SelectTrigger className="flex-1">
                  <SelectValue placeholder="请选择账户" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_empty" disabled>请选择账户</SelectItem>
                  {accounts.map((account) => (
                    <SelectItem key={account.id} value={account.id.toString()}>
                      {account.name} ({account.type_name || account.type})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button onClick={handleLoadAddDomains} disabled={addLoading || !addAccountId}>
                {addLoading ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <RefreshCw className="h-4 w-4" />
                )}
                <span className="ml-2">获取域名</span>
              </Button>
            </div>

            {addDomains.length > 0 && (
              <DomainListTable
                domainList={addDomains}
                selectedList={addSelectedDomains}
                onToggle={(domain, checked) => {
                  if (checked) {
                    setAddSelectedDomains([...addSelectedDomains, domain])
                  } else {
                    setAddSelectedDomains(addSelectedDomains.filter(d => d.DomainId !== domain.DomainId))
                  }
                }}
                onToggleAll={(checked) => {
                  if (checked) {
                    setAddSelectedDomains(addDomains.filter(d => !d.disabled))
                  } else {
                    setAddSelectedDomains([])
                  }
                }}
              />
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddDialogOpen(false)}>取消</Button>
            <Button onClick={handleAddImport} disabled={submitting || addSelectedDomains.length === 0}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              导入选中 ({addSelectedDomains.length})
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ============== Sync Dialog ============== */}
      <Dialog open={syncDialogOpen} onOpenChange={setSyncDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>同步域名</DialogTitle>
            <DialogDescription>从DNS服务商同步域名列表</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="flex gap-2">
              <Select value={syncAccountId || '_empty'} onValueChange={(v) => setSyncAccountId(v === '_empty' ? '' : v)}>
                <SelectTrigger className="flex-1">
                  <SelectValue placeholder="请选择账户" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="_empty" disabled>请选择账户</SelectItem>
                  {accounts.map((account) => (
                    <SelectItem key={account.id} value={account.id.toString()}>
                      {account.name} ({account.type_name || account.type})
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
              <Button onClick={handleLoadSyncDomains} disabled={syncLoading}>
                {syncLoading ? (
                  <Loader2 className="h-4 w-4 animate-spin" />
                ) : (
                  <RefreshCw className="h-4 w-4" />
                )}
                <span className="ml-2">获取列表</span>
              </Button>
            </div>

            {syncDomains.length > 0 && (
              <DomainListTable
                domainList={syncDomains}
                selectedList={syncSelectedDomains}
                onToggle={(domain, checked) => {
                  if (checked) {
                    setSyncSelectedDomains([...syncSelectedDomains, domain])
                  } else {
                    setSyncSelectedDomains(syncSelectedDomains.filter(d => d.DomainId !== domain.DomainId))
                  }
                }}
                onToggleAll={(checked) => {
                  if (checked) {
                    setSyncSelectedDomains(syncDomains.filter(d => !d.disabled))
                  } else {
                    setSyncSelectedDomains([])
                  }
                }}
              />
            )}
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setSyncDialogOpen(false)}>取消</Button>
            <Button onClick={handleSync} disabled={submitting || syncSelectedDomains.length === 0}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              同步 ({syncSelectedDomains.length})
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ============== Cross-Account Import Dialog ============== */}
      <Dialog open={crossImportOpen} onOpenChange={setCrossImportOpen}>
        <DialogContent className="max-w-3xl max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Layers className="h-5 w-5" />
              跨账户导入
            </DialogTitle>
            <DialogDescription>从所有DNS账户批量获取并导入域名</DialogDescription>
          </DialogHeader>

          {crossLoading && crossAccountDomains.every(a => a.loading) ? (
            <div className="flex flex-col items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin text-primary mb-4" />
              <p className="text-muted-foreground">正在获取所有账户的域名列表...</p>
            </div>
          ) : crossAccountDomains.length === 0 ? (
            <div className="text-center py-8 text-muted-foreground">
              暂无DNS账户，请先添加账户
            </div>
          ) : (
            <div className="space-y-4">
              {crossAccountDomains.map((item) => (
                <div key={item.account.id} className="border rounded-lg overflow-hidden">
                  <div className="flex items-center justify-between p-3 bg-muted/50">
                    <div className="flex items-center gap-2">
                      <span className="font-medium">{item.account.name}</span>
                      <Badge variant="outline" className="text-xs">
                        {item.account.type_name || item.account.type}
                      </Badge>
                      {!item.loading && (
                        <span className="text-xs text-muted-foreground">
                          {item.domains.length} 个域名
                        </span>
                      )}
                    </div>
                    {item.loading && <Loader2 className="h-4 w-4 animate-spin text-muted-foreground" />}
                  </div>
                  {!item.loading && item.domains.length > 0 && (
                    <div className="max-h-48 overflow-y-auto">
                      <Table>
                        <TableBody>
                          {item.domains.map((domain) => (
                            <TableRow key={domain.DomainId} className="hover:bg-muted/30">
                              <TableCell className="w-12">
                                <Checkbox
                                  checked={isCrossDomainSelected(String(item.account.id), domain.DomainId)}
                                  disabled={domain.disabled}
                                  onCheckedChange={() => toggleCrossSelect(String(item.account.id), domain)}
                                />
                              </TableCell>
                              <TableCell className="font-medium">{domain.Domain}</TableCell>
                              <TableCell className="text-sm text-muted-foreground">{domain.RecordCount} 条记录</TableCell>
                              <TableCell>
                                {domain.disabled ? (
                                  <Badge variant="secondary" className="text-xs">已存在</Badge>
                                ) : (
                                  <Badge variant="outline" className="text-xs">可导入</Badge>
                                )}
                              </TableCell>
                            </TableRow>
                          ))}
                        </TableBody>
                      </Table>
                    </div>
                  )}
                  {!item.loading && item.domains.length === 0 && (
                    <div className="p-3 text-center text-sm text-muted-foreground">此账户下暂无域名</div>
                  )}
                </div>
              ))}
            </div>
          )}

          <DialogFooter>
            <Button variant="outline" onClick={() => setCrossImportOpen(false)}>取消</Button>
            <Button onClick={handleCrossImport} disabled={submitting || totalCrossSelected === 0}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              导入选中 ({totalCrossSelected})
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ============== Delete Dialog ============== */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除域名 &ldquo;{selectedDomain?.name}&rdquo; 吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction
              onClick={handleDelete}
              className="bg-destructive text-destructive-foreground hover:bg-destructive/90"
            >
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
