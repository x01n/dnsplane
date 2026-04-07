'use client'

import { useState, useEffect, useMemo, useRef } from 'react'
import { useRouter } from 'next/navigation'
import {
  Plus,
  Search,
  MoreHorizontal,
  Pencil,
  Trash2,
  Pause,
  Play,
  ArrowLeft,
  Loader2,
  Copy,
  FileText,
  RefreshCw,
  Shield,
  Grid3X3,
  List,
  Filter,
  ChevronDown,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { TableSkeleton } from '@/components/table-skeleton'
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
import { Textarea } from '@/components/ui/textarea'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { toast } from 'sonner'
import { domainApi, monitorApi, DNSRecord, RecordLine, authApi, User } from '@/lib/api'
import { DNS_RECORD_TYPES, copyToClipboard, cn, hasModuleAccess } from '@/lib/utils'
import { ProviderBadge } from '@/components/provider-icon'

const LIST_PAGE_SIZE_OPTIONS = [10, 20, 50, 100] as const
const LS_RECORDS_PAGE_SIZE = 'dnsplane-records-page-size'

function readStoredRecordsPageSize(): number {
  if (typeof window === 'undefined') return 20
  const n = parseInt(localStorage.getItem(LS_RECORDS_PAGE_SIZE) || '', 10)
  return LIST_PAGE_SIZE_OPTIONS.includes(n as (typeof LIST_PAGE_SIZE_OPTIONS)[number]) ? n : 20
}

const RECORD_TYPE_COLORS: Record<string, string> = {
  A: 'bg-blue-100 text-blue-700 border-blue-200 dark:bg-blue-900/30 dark:text-blue-400',
  AAAA: 'bg-purple-100 text-purple-700 border-purple-200 dark:bg-purple-900/30 dark:text-purple-400',
  CNAME: 'bg-green-100 text-green-700 border-green-200 dark:bg-green-900/30 dark:text-green-400',
  MX: 'bg-orange-100 text-orange-700 border-orange-200 dark:bg-orange-900/30 dark:text-orange-400',
  TXT: 'bg-gray-100 text-gray-700 border-gray-200 dark:bg-gray-800 dark:text-gray-400',
  NS: 'bg-cyan-100 text-cyan-700 border-cyan-200 dark:bg-cyan-900/30 dark:text-cyan-400',
  SRV: 'bg-pink-100 text-pink-700 border-pink-200 dark:bg-pink-900/30 dark:text-pink-400',
  CAA: 'bg-yellow-100 text-yellow-700 border-yellow-200 dark:bg-yellow-900/30 dark:text-yellow-400',
  PTR: 'bg-indigo-100 text-indigo-700 border-indigo-200 dark:bg-indigo-900/30 dark:text-indigo-400',
}

export default function DomainRecordsClient() {
  const router = useRouter()
  
  const [domainId, setDomainId] = useState<string>('')
  const [domainInfo, setDomainInfo] = useState<{ name: string; type_name: string; account_type: string; record_count: number } | null>(null)

  const [records, setRecords] = useState<DNSRecord[]>([])
  const [lines, setLines] = useState<RecordLine[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [debouncedKeyword, setDebouncedKeyword] = useState('')
  const [filterType, setFilterType] = useState('')
  const [filterLine, setFilterLine] = useState('')
  const [filterStatus, setFilterStatus] = useState('')
  const [filterSubdomain, setFilterSubdomain] = useState('')
  const [filterValue, setFilterValue] = useState('')
  const [advancedFiltersOpen, setAdvancedFiltersOpen] = useState(false)
  /** 主搜索框同时按记录值模糊查询（与服务商 keyword 并行） */
  const [wideFuzzy, setWideFuzzy] = useState(true)
  const [recordPage, setRecordPage] = useState(1)
  const [recordTotal, setRecordTotal] = useState(0)
  const [recordPageSize, setRecordPageSize] = useState(readStoredRecordsPageSize)
  const [viewMode, setViewMode] = useState<'table' | 'card'>('table')
  const [dialogOpen, setDialogOpen] = useState(false)
  const [batchDialogOpen, setBatchDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [monitorDialogOpen, setMonitorDialogOpen] = useState(false)
  const [selectedRecord, setSelectedRecord] = useState<DNSRecord | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [selectedRecordIds, setSelectedRecordIds] = useState<string[]>([])

  const [formData, setFormData] = useState({
    Name: '',
    Type: 'A',
    Value: '',
    Line: '',
    TTL: 600,
    MX: 10,
    Remark: '',
  })

  const [batchData, setBatchData] = useState({
    records: '',
    type: '',
    line: '',
    ttl: 600,
  })

  const [monitorData, setMonitorData] = useState({
    type: 0,
    check_type: 0,
    backup_value: '',
    frequency: 60,
    cycle: 3,
    timeout: 5,
  })
  const [currentUser, setCurrentUser] = useState<User | null>(null)
  const canUseMonitor = currentUser != null && hasModuleAccess(currentUser, 'monitor')

  // 记录统计
  const recordStats = useMemo(() => {
    const stats: Record<string, number> = {}
    records.forEach(r => {
      stats[r.Type] = (stats[r.Type] || 0) + 1
    })
    return stats
  }, [records])

  useEffect(() => {
    if (typeof window !== 'undefined') {
      const path = window.location.pathname
      const match = path.match(/\/dashboard\/domains\/([^/]+)/)
      if (match) {
        setDomainId(match[1])
      }
      if (window.matchMedia('(max-width: 639px)').matches) {
        setViewMode('card')
      }
    }
  }, [])

  useEffect(() => {
    authApi.getUserInfo().then((res) => {
      if (res.code === 0 && res.data) setCurrentUser(res.data)
    })
  }, [])

  useEffect(() => {
    const t = setTimeout(() => setDebouncedKeyword(keyword.trim()), 400)
    return () => clearTimeout(t)
  }, [keyword])

  useEffect(() => {
    if (domainId) {
      fetchDomainInfo()
      fetchLines()
    }
  }, [domainId])

  const filterKey = useMemo(
    () =>
      [
        domainId,
        debouncedKeyword,
        filterType,
        filterLine,
        filterStatus,
        filterSubdomain,
        filterValue,
        wideFuzzy ? '1' : '0',
        recordPageSize,
      ].join('\x1e'),
    [domainId, debouncedKeyword, filterType, filterLine, filterStatus, filterSubdomain, filterValue, wideFuzzy, recordPageSize]
  )
  const filterKeyRef = useRef(filterKey)
  const lastFetchSigRef = useRef('')

  useEffect(() => {
    if (!domainId) return
    const fkChanged = filterKeyRef.current !== filterKey
    filterKeyRef.current = filterKey
    const pageToFetch = fkChanged ? 1 : recordPage
    if (fkChanged) setRecordPage(1)
    const sig = `${domainId}\x1e${filterKey}\x1e${pageToFetch}`
    if (lastFetchSigRef.current === sig) return
    lastFetchSigRef.current = sig
    fetchRecords(false, pageToFetch)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [domainId, recordPage, filterKey])

  const fetchDomainInfo = async () => {
    try {
      const res = await domainApi.detail(domainId)
      if (res.code === 0 && res.data) {
        const domain = res.data
        setDomainInfo({
          name: domain.name,
          type_name: domain.type_name || domain.account_type || '',
          account_type: domain.account_type || '',
          record_count: domain.record_count || 0,
        })
      }
    } catch {
      // ignore
    }
  }

  const fetchLines = async () => {
    try {
      const res = await domainApi.getLines(domainId)
      if (res.code === 0 && res.data) {
        setLines(res.data)
      }
    } catch {
      // ignore
    }
  }

  const fetchRecords = async (showRefreshing = false, overridePage?: number) => {
    if (showRefreshing) {
      setRefreshing(true)
    } else {
      setLoading(true)
    }
    try {
      const currentPage = overridePage ?? recordPage
      const params: Record<string, string | number> = { page: currentPage, page_size: recordPageSize }
      if (debouncedKeyword) params.keyword = debouncedKeyword
      if (filterType && filterType !== 'all') params.type = filterType
      if (filterLine && filterLine !== 'all') params.line = filterLine
      if (filterStatus === '1' || filterStatus === '0') params.status = filterStatus
      const sub = filterSubdomain.trim()
      if (sub) params.subdomain = sub
      const fv = filterValue.trim()
      if (fv) params.value = fv
      else if (wideFuzzy && debouncedKeyword) params.value = debouncedKeyword
      const res = await domainApi.getRecords(domainId, params)
      if (res.code === 0 && res.data) {
        setRecords(res.data.list || [])
        setRecordTotal(res.data.total || 0)
      } else if (res.code !== 0) {
        toast.error(res.msg || '获取记录列表失败')
      }
    } catch {
      toast.error('获取记录列表失败')
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    setDebouncedKeyword(keyword.trim())
    setRecordPage(1)
  }

  const handleRefresh = () => {
    fetchRecords(true)
  }

  const handleRecordPageSizeChange = (value: string) => {
    const next = parseInt(value, 10)
    if (!LIST_PAGE_SIZE_OPTIONS.includes(next as (typeof LIST_PAGE_SIZE_OPTIONS)[number])) return
    setRecordPageSize(next)
    try {
      localStorage.setItem(LS_RECORDS_PAGE_SIZE, String(next))
    } catch {
      // ignore
    }
  }

  const openCreateDialog = () => {
    setSelectedRecord(null)
    setFormData({
      Name: '',
      Type: 'A',
      Value: '',
      Line: lines[0]?.id || '',
      TTL: 600,
      MX: 10,
      Remark: '',
    })
    setDialogOpen(true)
  }

  const openEditDialog = (record: DNSRecord) => {
    setSelectedRecord(record)
    setFormData({
      Name: record.Name,
      Type: record.Type,
      Value: Array.isArray(record.Value) ? record.Value.join('\n') : record.Value,
      Line: record.Line,
      TTL: record.TTL,
      MX: record.MX || 10,
      Remark: record.Remark || '',
    })
    setDialogOpen(true)
  }

  const openDeleteDialog = (record: DNSRecord) => {
    setSelectedRecord(record)
    setDeleteDialogOpen(true)
  }

  const openMonitorDialog = (record: DNSRecord) => {
    setSelectedRecord(record)
    setMonitorData({
      type: 0,
      check_type: 0,
      backup_value: '',
      frequency: 60,
      cycle: 3,
      timeout: 5,
    })
    setMonitorDialogOpen(true)
  }

  const handleSubmit = async () => {
    if (!formData.Name || !formData.Type || !formData.Value) {
      toast.error('请填写必填项')
      return
    }

    setSubmitting(true)
    try {
      const data = {
        name: formData.Name,
        type: formData.Type,
        value: formData.Value,
        line: formData.Line,
        ttl: formData.TTL,
        mx: formData.MX,
        remark: formData.Remark,
      }

      const res = selectedRecord
        ? await domainApi.updateRecord(domainId, selectedRecord.RecordId, data)
        : await domainApi.createRecord(domainId, data)

      if (res.code === 0) {
        toast.success(selectedRecord ? '修改成功' : '添加成功')
        setDialogOpen(false)
        fetchRecords()
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
    if (!selectedRecord) return
    try {
      const res = await domainApi.deleteRecord(domainId, selectedRecord.RecordId)
      if (res.code === 0) {
        toast.success('删除成功')
        setDeleteDialogOpen(false)
        fetchRecords()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  const handleToggleStatus = async (record: DNSRecord) => {
    const newEnable = record.Status !== '1'
    try {
      const res = await domainApi.setRecordStatus(domainId, record.RecordId, newEnable)
      if (res.code === 0) {
        toast.success(newEnable ? '已启用' : '已暂停')
        fetchRecords()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    }
  }

  const handleCreateMonitor = async () => {
    if (!selectedRecord) return
    setSubmitting(true)
    try {
      const res = await monitorApi.create({
        domain_id: domainId,
        rr: selectedRecord.Name,
        record_id: selectedRecord.RecordId,
        main_value: Array.isArray(selectedRecord.Value) ? selectedRecord.Value[0] : selectedRecord.Value,
        type: monitorData.type,
        check_type: monitorData.check_type,
        backup_value: monitorData.backup_value,
        frequency: monitorData.frequency,
        cycle: monitorData.cycle,
        timeout: monitorData.timeout,
      })
      if (res.code === 0) {
        toast.success('监控任务创建成功')
        setMonitorDialogOpen(false)
      } else {
        toast.error(res.msg || '创建失败')
      }
    } catch {
      toast.error('创建失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleBatchAdd = async () => {
    if (!batchData.records.trim()) {
      toast.error('请输入记录内容')
      return
    }
    setSubmitting(true)
    try {
      const res = await domainApi.batchAddRecords(domainId, {
        records: batchData.records,
        type: batchData.type,
        line: batchData.line,
        ttl: batchData.ttl,
      })
      if (res.code === 0) {
        toast.success(res.msg || '批量添加成功')
        setBatchDialogOpen(false)
        fetchRecords()
      } else {
        toast.error(res.msg || '批量添加失败')
      }
    } catch {
      toast.error('批量添加失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleBatchAction = async (action: string) => {
    if (selectedRecordIds.length === 0) {
      toast.error('请选择记录')
      return
    }
    try {
      const res = await domainApi.batchActionRecords(domainId, {
        record_ids: selectedRecordIds,
        action,
      })
      if (res.code === 0) {
        toast.success(res.msg || '操作成功')
        setSelectedRecordIds([])
        fetchRecords()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    }
  }

  const handleCopyValue = async (value: string) => {
    const success = await copyToClipboard(value)
    if (success) {
      toast.success('已复制')
    }
  }

  const toggleSelectAll = () => {
    if (selectedRecordIds.length === filteredRecords.length) {
      setSelectedRecordIds([])
    } else {
      setSelectedRecordIds(filteredRecords.map((r) => r.RecordId))
    }
  }

  const toggleSelect = (id: string) => {
    if (selectedRecordIds.includes(id)) {
      setSelectedRecordIds(selectedRecordIds.filter((i) => i !== id))
    } else {
      setSelectedRecordIds([...selectedRecordIds, id])
    }
  }

  const getLineName = (lineId: string, lineName?: string) => {
    if (lineName) return lineName
    const line = lines.find((l) => l.id === lineId)
    return line?.name || lineId || '默认'
  }

  const filteredRecords = records

  // 快速类型过滤
  const handleQuickFilter = (type: string) => {
    if (filterType === type) {
      setFilterType('')
    } else {
      setFilterType(type)
    }
  }

  return (
    <div className="space-y-6">
      {/* 页面头部 */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-start sm:justify-between">
        <div className="flex items-start gap-3 min-w-0">
          <Button variant="ghost" size="icon" className="shrink-0 mt-0.5" onClick={() => router.push('/dashboard/domains')}>
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2 sm:gap-3">
              <h1 className="text-xl sm:text-2xl font-bold">解析管理</h1>
              {domainInfo && (
                <Badge variant="outline" className="text-xs sm:text-sm font-normal shrink-0">
                  {recordTotal > 0 ? recordTotal : records.length} 条记录
                </Badge>
              )}
            </div>
            <div className="flex flex-wrap items-center gap-2 mt-1 text-muted-foreground text-sm">
              {domainInfo ? (
                <>
                  {(domainInfo.account_type || domainInfo.type_name) && (
                    <ProviderBadge 
                      type={domainInfo.account_type} 
                      name={domainInfo.type_name}
                    />
                  )}
                  <span className="font-medium text-foreground truncate">{domainInfo.name}</span>
                </>
              ) : domainId ? (
                <span>加载中...</span>
              ) : (
                <span>无效的域名</span>
              )}
            </div>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2 w-full sm:w-auto sm:justify-end">
          <Button variant="outline" size="sm" className="min-h-10 flex-1 sm:flex-initial" onClick={handleRefresh} disabled={refreshing}>
            <RefreshCw className={cn("h-4 w-4 sm:mr-2", refreshing && "animate-spin")} />
            <span className="hidden sm:inline">刷新</span>
          </Button>
          <Button variant="outline" size="sm" className="min-h-10 flex-1 sm:flex-initial" onClick={() => setBatchDialogOpen(true)}>
            <FileText className="h-4 w-4 sm:mr-2" />
            <span className="hidden sm:inline">批量添加</span>
            <span className="sm:hidden">批量</span>
          </Button>
          <Button size="sm" className="min-h-10 flex-1 sm:flex-initial" onClick={openCreateDialog}>
            <Plus className="h-4 w-4 sm:mr-2" />
            添加记录
          </Button>
        </div>
      </div>

      {/* 记录类型统计卡片 */}
      {Object.keys(recordStats).length > 0 && (
        <div className="flex flex-wrap gap-2">
          {Object.entries(recordStats).map(([type, count]) => (
            <button
              key={type}
              onClick={() => handleQuickFilter(type)}
              className={cn(
                "inline-flex items-center gap-2 px-3 py-1.5 rounded-full text-sm font-medium border transition-all",
                RECORD_TYPE_COLORS[type] || 'bg-gray-100 text-gray-700 border-gray-200',
                filterType === type && "ring-2 ring-offset-2 ring-primary"
              )}
            >
              <span>{type}</span>
              <span className="opacity-70">{count}</span>
            </button>
          ))}
        </div>
      )}

      {/* 主要内容卡片 */}
      <Card>
        <CardHeader className="pb-4">
          <div className="flex flex-col xl:flex-row items-stretch xl:items-center gap-4">
            {/* 搜索和筛选 */}
            <form onSubmit={handleSearch} className="flex-1 flex flex-col gap-3">
              <div className="flex flex-wrap gap-2 items-center">
                <div className="relative flex-1 min-w-[min(100%,12rem)]">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="关键词（主机名/记录值模糊）..."
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  className="pl-9 min-h-10"
                />
                </div>
                <Select value={filterType || 'all'} onValueChange={(v) => setFilterType(v === 'all' ? '' : v)}>
                  <SelectTrigger className="w-full sm:w-[120px] min-h-10">
                    <SelectValue placeholder="记录类型" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部类型</SelectItem>
                    {DNS_RECORD_TYPES.map((type) => (
                      <SelectItem key={type} value={type}>
                        {type}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
                <Select value={filterStatus || 'all'} onValueChange={(v) => setFilterStatus(v === 'all' ? '' : v)}>
                  <SelectTrigger className="w-full sm:w-[110px] min-h-10">
                    <SelectValue placeholder="状态" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="all">全部状态</SelectItem>
                    <SelectItem value="1">已启用</SelectItem>
                    <SelectItem value="0">已暂停</SelectItem>
                  </SelectContent>
                </Select>
                {lines.length > 1 && (
                  <Select value={filterLine || 'all'} onValueChange={(v) => setFilterLine(v === 'all' ? '' : v)}>
                    <SelectTrigger className="w-full sm:w-[120px] min-h-10">
                      <SelectValue placeholder="解析线路" />
                    </SelectTrigger>
                    <SelectContent>
                      <SelectItem value="all">全部线路</SelectItem>
                      {lines.map((line) => (
                        <SelectItem key={line.id} value={line.id}>
                          {line.name}
                        </SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                )}
                <Button type="submit" variant="secondary" size="icon" className="h-10 w-10 shrink-0" title="搜索">
                  <Search className="h-4 w-4" />
                </Button>
              </div>
              <label className="flex items-center gap-2 text-sm text-muted-foreground cursor-pointer select-none">
                <Checkbox checked={wideFuzzy} onCheckedChange={(c) => setWideFuzzy(c === true)} />
                宽域模糊：主搜索同时匹配记录值（与高级里「记录值」二选一优先生效）
              </label>
              <div>
                <button
                  type="button"
                  onClick={() => setAdvancedFiltersOpen((o) => !o)}
                  className={cn(
                    'inline-flex items-center gap-1.5 text-sm text-muted-foreground hover:text-foreground',
                    advancedFiltersOpen && 'text-foreground'
                  )}
                >
                  <Filter className="h-4 w-4" />
                  高级筛选
                  <ChevronDown className={cn('h-4 w-4 transition-transform', advancedFiltersOpen && 'rotate-180')} />
                </button>
                {advancedFiltersOpen && (
                  <div className="mt-3 grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground">主机记录（精确）</Label>
                      <Input
                        placeholder="如 www 或 @"
                        value={filterSubdomain}
                        onChange={(e) => setFilterSubdomain(e.target.value)}
                        className="min-h-10"
                      />
                    </div>
                    <div className="space-y-1.5">
                      <Label className="text-xs text-muted-foreground">记录值（包含）</Label>
                      <Input
                        placeholder="模糊匹配记录值"
                        value={filterValue}
                        onChange={(e) => setFilterValue(e.target.value)}
                        className="min-h-10"
                      />
                    </div>
                  </div>
                )}
              </div>
            </form>

            {/* 视图切换 */}
            <div className="flex items-center justify-end gap-2 shrink-0">
              <Tabs value={viewMode} onValueChange={(v) => setViewMode(v as 'table' | 'card')}>
                <TabsList className="h-10">
                  <TabsTrigger value="table" className="px-3">
                    <List className="h-4 w-4" />
                  </TabsTrigger>
                  <TabsTrigger value="card" className="px-3">
                    <Grid3X3 className="h-4 w-4" />
                  </TabsTrigger>
                </TabsList>
              </Tabs>
            </div>
          </div>

          {/* 批量操作栏 */}
          {selectedRecordIds.length > 0 && (
            <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:flex-wrap mt-4 p-3 bg-primary/5 border border-primary/20 rounded-lg">
              <div className="flex items-center gap-2">
                <Checkbox
                  checked={selectedRecordIds.length === filteredRecords.length && filteredRecords.length > 0}
                  onCheckedChange={toggleSelectAll}
                />
                <span className="text-sm font-medium">已选 {selectedRecordIds.length} 项</span>
              </div>
              <div className="flex flex-wrap gap-2 sm:ml-auto">
                <Button variant="outline" size="sm" className="min-h-10" onClick={() => handleBatchAction('open')}>
                  <Play className="h-4 w-4 mr-1" />
                  启用
                </Button>
                <Button variant="outline" size="sm" className="min-h-10" onClick={() => handleBatchAction('pause')}>
                  <Pause className="h-4 w-4 mr-1" />
                  暂停
                </Button>
                <Button variant="destructive" size="sm" className="min-h-10" onClick={() => handleBatchAction('delete')}>
                  <Trash2 className="h-4 w-4 mr-1" />
                  删除
                </Button>
              </div>
            </div>
          )}
        </CardHeader>

        <CardContent>
          {loading ? (
            <TableSkeleton rows={6} columns={8} />
          ) : filteredRecords.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-4">
                <FileText className="h-8 w-8 text-muted-foreground" />
              </div>
              <h3 className="text-lg font-medium mb-2">暂无解析记录</h3>
              <p className="text-muted-foreground mb-4">点击&ldquo;添加记录&rdquo;按钮创建第一条DNS记录</p>
              <Button onClick={openCreateDialog}>
                <Plus className="h-4 w-4 mr-2" />
                添加记录
              </Button>
            </div>
          ) : viewMode === 'table' ? (
            /* 表格视图 */
            <div className="border rounded-lg overflow-hidden overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="bg-muted/50">
                    <TableHead className="w-12">
                      <Checkbox
                        checked={selectedRecordIds.length === filteredRecords.length && filteredRecords.length > 0}
                        onCheckedChange={toggleSelectAll}
                      />
                    </TableHead>
                    <TableHead className="font-semibold">主机记录</TableHead>
                    <TableHead className="font-semibold">类型</TableHead>
                    <TableHead className="font-semibold">线路</TableHead>
                    <TableHead className="font-semibold">记录值</TableHead>
                    <TableHead className="font-semibold w-20">TTL</TableHead>
                    <TableHead className="font-semibold w-24">状态</TableHead>
                    <TableHead className="w-[80px]"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filteredRecords.map((record) => (
                    <TableRow key={record.RecordId} className="group hover:bg-muted/30">
                      <TableCell>
                        <Checkbox
                          checked={selectedRecordIds.includes(record.RecordId)}
                          onCheckedChange={() => toggleSelect(record.RecordId)}
                        />
                      </TableCell>
                      <TableCell>
                        <div className="flex flex-col">
                          <span className="font-medium">{record.Name}</span>
                          {record.Remark && (
                            <span className="text-xs text-muted-foreground truncate max-w-[150px]" title={record.Remark}>
                              {record.Remark}
                            </span>
                          )}
                        </div>
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className={cn("font-mono text-xs", RECORD_TYPE_COLORS[record.Type])}>
                          {record.Type}
                        </Badge>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm">{getLineName(record.Line, record.LineName)}</span>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2 max-w-[280px]">
                          <code className="text-sm bg-muted px-2 py-0.5 rounded truncate flex-1" title={Array.isArray(record.Value) ? record.Value.join(', ') : record.Value}>
                            {Array.isArray(record.Value) ? record.Value.join(', ') : record.Value}
                          </code>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-7 w-7 opacity-0 group-hover:opacity-100 transition-opacity"
                            onClick={() => handleCopyValue(Array.isArray(record.Value) ? record.Value.join(', ') : record.Value)}
                          >
                            <Copy className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                      <TableCell>
                        <span className="text-sm text-muted-foreground">{record.TTL}s</span>
                      </TableCell>
                      <TableCell>
                        <div className="flex items-center gap-2">
                          <Switch
                            checked={record.Status === '1'}
                            onCheckedChange={() => handleToggleStatus(record)}
                            className="data-[state=checked]:bg-green-500"
                          />
                          <span className={cn("text-xs", record.Status === '1' ? "text-green-600 dark:text-green-400" : "text-muted-foreground")}>
                            {record.Status === '1' ? '启用' : '暂停'}
                          </span>
                        </div>
                      </TableCell>
                      <TableCell>
                        <DropdownMenu>
                          <DropdownMenuTrigger asChild>
                            <Button variant="ghost" size="icon" className="h-8 w-8">
                              <MoreHorizontal className="h-4 w-4" />
                            </Button>
                          </DropdownMenuTrigger>
                          <DropdownMenuContent align="end" className="w-48">
                            <DropdownMenuItem onClick={() => openEditDialog(record)}>
                              <Pencil className="h-4 w-4 mr-2" />
                              编辑记录
                            </DropdownMenuItem>
                            <DropdownMenuItem onClick={() => handleCopyValue(Array.isArray(record.Value) ? record.Value.join(', ') : record.Value)}>
                              <Copy className="h-4 w-4 mr-2" />
                              复制记录值
                            </DropdownMenuItem>
                            {canUseMonitor && (record.Type === 'A' || record.Type === 'AAAA') && (
                              <DropdownMenuItem onClick={() => openMonitorDialog(record)}>
                                <Shield className="h-4 w-4 mr-2" />
                                添加容灾监控
                              </DropdownMenuItem>
                            )}
                            <DropdownMenuSeparator />
                            <DropdownMenuItem
                              onClick={() => openDeleteDialog(record)}
                              className="text-destructive focus:text-destructive"
                            >
                              <Trash2 className="h-4 w-4 mr-2" />
                              删除记录
                            </DropdownMenuItem>
                          </DropdownMenuContent>
                        </DropdownMenu>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          ) : (
            /* 卡片视图 */
            <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
              {filteredRecords.map((record) => (
                <div
                  key={record.RecordId}
                  className={cn(
                    "relative p-4 rounded-lg border bg-card hover:shadow-md transition-all group",
                    selectedRecordIds.includes(record.RecordId) && "ring-2 ring-primary"
                  )}
                >
                  {/* 选择框 */}
                  <div className="absolute top-3 left-3">
                    <Checkbox
                      checked={selectedRecordIds.includes(record.RecordId)}
                      onCheckedChange={() => toggleSelect(record.RecordId)}
                    />
                  </div>
                  
                  {/* 操作按钮 */}
                  <div className="absolute top-3 right-3">
                    <DropdownMenu>
                      <DropdownMenuTrigger asChild>
                        <Button variant="ghost" size="icon" className="h-7 w-7">
                          <MoreHorizontal className="h-4 w-4" />
                        </Button>
                      </DropdownMenuTrigger>
                      <DropdownMenuContent align="end">
                        <DropdownMenuItem onClick={() => openEditDialog(record)}>
                          <Pencil className="h-4 w-4 mr-2" />
                          编辑
                        </DropdownMenuItem>
                        {canUseMonitor && (record.Type === 'A' || record.Type === 'AAAA') && (
                          <DropdownMenuItem onClick={() => openMonitorDialog(record)}>
                            <Shield className="h-4 w-4 mr-2" />
                            添加监控
                          </DropdownMenuItem>
                        )}
                        <DropdownMenuSeparator />
                        <DropdownMenuItem onClick={() => openDeleteDialog(record)} className="text-destructive">
                          <Trash2 className="h-4 w-4 mr-2" />
                          删除
                        </DropdownMenuItem>
                      </DropdownMenuContent>
                    </DropdownMenu>
                  </div>

                  <div className="pt-6 space-y-3">
                    {/* 主机记录和类型 */}
                    <div className="flex items-center gap-2">
                      <Badge variant="outline" className={cn("text-xs", RECORD_TYPE_COLORS[record.Type])}>
                        {record.Type}
                      </Badge>
                      <span className="font-semibold text-lg">{record.Name}</span>
                    </div>

                    {/* 记录值 */}
                    <div className="flex items-center gap-2">
                      <code className="flex-1 text-sm bg-muted px-2 py-1 rounded truncate">
                        {Array.isArray(record.Value) ? record.Value.join(', ') : record.Value}
                      </code>
                      <Button
                        variant="ghost"
                        size="icon"
                        className="h-7 w-7 shrink-0"
                        onClick={() => handleCopyValue(Array.isArray(record.Value) ? record.Value.join(', ') : record.Value)}
                      >
                        <Copy className="h-3.5 w-3.5" />
                      </Button>
                    </div>

                    {/* 底部信息 */}
                    <div className="flex items-center justify-between pt-2 border-t">
                      <div className="flex items-center gap-3 text-xs text-muted-foreground">
                        <span>{getLineName(record.Line, record.LineName)}</span>
                        <span>TTL: {record.TTL}s</span>
                      </div>
                      <Switch
                        checked={record.Status === '1'}
                        onCheckedChange={() => handleToggleStatus(record)}
                        className="data-[state=checked]:bg-green-500"
                      />
                    </div>

                    {/* 备注 */}
                    {record.Remark && (
                      <p className="text-xs text-muted-foreground truncate" title={record.Remark}>
                        {record.Remark}
                      </p>
                    )}
                  </div>
                </div>
              ))}
            </div>
          )}

          {/* 分页 */}
          {recordTotal > 0 && (
            <div className="flex flex-wrap items-center justify-between gap-3 mt-4 pt-4 border-t">
              <div className="flex flex-wrap items-center gap-3 text-sm text-muted-foreground">
                <span>
                  共 {recordTotal} 条，第 {recordPage}/{Math.max(1, Math.ceil(recordTotal / recordPageSize))} 页
                </span>
                <div className="flex items-center gap-2">
                  <span className="whitespace-nowrap">每页</span>
                  <Select value={String(recordPageSize)} onValueChange={handleRecordPageSizeChange}>
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
              <div className="flex items-center gap-2 w-full sm:w-auto">
                <Button
                  variant="outline"
                  size="sm"
                  className="min-h-10 flex-1 sm:flex-initial"
                  disabled={recordPage <= 1}
                  onClick={() => {
                    const newPage = recordPage - 1
                    setRecordPage(newPage)
                  }}
                >
                  上一页
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  className="min-h-10 flex-1 sm:flex-initial"
                  disabled={recordPage >= Math.max(1, Math.ceil(recordTotal / recordPageSize))}
                  onClick={() => {
                    const newPage = recordPage + 1
                    setRecordPage(newPage)
                  }}
                >
                  下一页
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 添加/编辑记录弹窗 */}
      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>{selectedRecord ? '编辑记录' : '添加记录'}</DialogTitle>
            <DialogDescription>
              {selectedRecord ? '修改DNS解析记录' : '添加新的DNS解析记录'}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>主机记录 <span className="text-destructive">*</span></Label>
                <Input
                  value={formData.Name}
                  onChange={(e) => setFormData({ ...formData, Name: e.target.value })}
                  placeholder="如 www 或 @"
                />
              </div>
              <div className="space-y-2">
                <Label>记录类型 <span className="text-destructive">*</span></Label>
                <Select
                  value={formData.Type}
                  onValueChange={(v) => setFormData({ ...formData, Type: v })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {DNS_RECORD_TYPES.map((type) => (
                      <SelectItem key={type} value={type}>
                        {type}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
            </div>

            <div className="space-y-2">
              <Label>记录值 <span className="text-destructive">*</span></Label>
              <Textarea
                value={formData.Value}
                onChange={(e) => setFormData({ ...formData, Value: e.target.value })}
                placeholder="请输入记录值"
                rows={2}
              />
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>解析线路</Label>
                <Select
                  value={formData.Line}
                  onValueChange={(v) => setFormData({ ...formData, Line: v })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="默认" />
                  </SelectTrigger>
                  <SelectContent>
                    {lines.map((line) => (
                      <SelectItem key={line.id} value={line.id}>
                        {line.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>TTL</Label>
                <Input
                  type="number"
                  value={formData.TTL}
                  onChange={(e) => setFormData({ ...formData, TTL: parseInt(e.target.value) || 600 })}
                  min={1}
                />
              </div>
            </div>

            {formData.Type === 'MX' && (
              <div className="space-y-2">
                <Label>MX优先级</Label>
                <Input
                  type="number"
                  value={formData.MX}
                  onChange={(e) => setFormData({ ...formData, MX: parseInt(e.target.value) || 10 })}
                  min={1}
                  max={100}
                />
              </div>
            )}

            <div className="space-y-2">
              <Label>备注</Label>
              <Input
                value={formData.Remark}
                onChange={(e) => setFormData({ ...formData, Remark: e.target.value })}
                placeholder="可选"
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

      {/* 批量添加弹窗 */}
      <Dialog open={batchDialogOpen} onOpenChange={setBatchDialogOpen}>
        <DialogContent className="max-w-lg sm:max-w-xl">
          <DialogHeader>
            <DialogTitle>批量添加记录</DialogTitle>
            <DialogDescription>每行一条：<code className="text-xs bg-muted px-1 rounded">主机名 记录值</code>（空格分隔；类型可选自动识别）</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="space-y-2">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <Label>记录内容 <span className="text-destructive">*</span></Label>
                <div className="flex flex-wrap gap-2">
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() =>
                      setBatchData((b) => ({
                        ...b,
                        records: ['@ 127.0.0.1', 'www 127.0.0.1', '* 127.0.0.1'].join('\n'),
                      }))
                    }
                  >
                    快捷：常见 A
                  </Button>
                  <Button
                    type="button"
                    variant="outline"
                    size="sm"
                    className="h-8 text-xs"
                    onClick={() =>
                      setBatchData((b) => ({
                        ...b,
                        records: ['www cname.example.com', 'api cname.example.com'].join('\n'),
                      }))
                    }
                  >
                    快捷：CNAME
                  </Button>
                  <Button type="button" variant="ghost" size="sm" className="h-8 text-xs" onClick={() => setBatchData((b) => ({ ...b, records: '' }))}>
                    清空
                  </Button>
                </div>
              </div>
              <Textarea
                value={batchData.records}
                onChange={(e) => setBatchData({ ...batchData, records: e.target.value })}
                placeholder="www 1.2.3.4&#10;@ 1.2.3.4&#10;mail 1.2.3.4"
                rows={6}
                className="font-mono text-sm min-h-[140px]"
              />
            </div>
            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label>记录类型</Label>
                <Select
                  value={batchData.type}
                  onValueChange={(v) => setBatchData({ ...batchData, type: v })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="自动识别" />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="auto">自动识别</SelectItem>
                    {DNS_RECORD_TYPES.map((type) => (
                      <SelectItem key={type} value={type}>
                        {type}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>解析线路</Label>
                <Select
                  value={batchData.line}
                  onValueChange={(v) => setBatchData({ ...batchData, line: v })}
                >
                  <SelectTrigger>
                    <SelectValue placeholder="默认" />
                  </SelectTrigger>
                  <SelectContent>
                    {lines.map((line) => (
                      <SelectItem key={line.id} value={line.id}>
                        {line.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>TTL</Label>
                <Input
                  type="number"
                  value={batchData.ttl}
                  onChange={(e) => setBatchData({ ...batchData, ttl: parseInt(e.target.value) || 600 })}
                  min={1}
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setBatchDialogOpen(false)}>取消</Button>
            <Button onClick={handleBatchAdd} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              添加
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
              确定要删除记录 &ldquo;{selectedRecord?.Name}&rdquo; 吗？此操作不可撤销。
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

      {/* 添加容灾监控弹窗 */}
      <Dialog open={monitorDialogOpen} onOpenChange={setMonitorDialogOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>添加容灾监控</DialogTitle>
            <DialogDescription>
              为记录 &ldquo;{selectedRecord?.Name}&rdquo; 配置容灾切换任务
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="p-3 bg-muted rounded-lg space-y-1">
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">主机记录:</span>
                <span className="font-medium">{selectedRecord?.Name}</span>
              </div>
              <div className="flex items-center gap-2 text-sm">
                <span className="text-muted-foreground">记录值:</span>
                <code className="bg-background px-1.5 py-0.5 rounded text-xs">
                  {selectedRecord && (Array.isArray(selectedRecord.Value) ? selectedRecord.Value[0] : selectedRecord.Value)}
                </code>
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>切换方式</Label>
                <Select
                  value={monitorData.type.toString()}
                  onValueChange={(v) => setMonitorData({ ...monitorData, type: parseInt(v) })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">暂停/启用记录</SelectItem>
                    <SelectItem value="1">删除记录</SelectItem>
                    <SelectItem value="2">切换备用记录</SelectItem>
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>检测类型</Label>
                <Select
                  value={monitorData.check_type.toString()}
                  onValueChange={(v) => setMonitorData({ ...monitorData, check_type: parseInt(v) })}
                >
                  <SelectTrigger>
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    <SelectItem value="0">Ping</SelectItem>
                    <SelectItem value="1">TCP</SelectItem>
                    <SelectItem value="2">HTTP</SelectItem>
                    <SelectItem value="3">HTTPS</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {monitorData.type === 2 && (
              <div className="space-y-2">
                <Label>备用IP/值</Label>
                <Input
                  value={monitorData.backup_value}
                  onChange={(e) => setMonitorData({ ...monitorData, backup_value: e.target.value })}
                  placeholder="故障时切换到的备用IP或域名"
                />
              </div>
            )}

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label>检测间隔(秒)</Label>
                <Input
                  type="number"
                  value={monitorData.frequency}
                  onChange={(e) => setMonitorData({ ...monitorData, frequency: parseInt(e.target.value) || 60 })}
                  min={10}
                />
              </div>
              <div className="space-y-2">
                <Label>失败次数</Label>
                <Input
                  type="number"
                  value={monitorData.cycle}
                  onChange={(e) => setMonitorData({ ...monitorData, cycle: parseInt(e.target.value) || 3 })}
                  min={1}
                />
              </div>
              <div className="space-y-2">
                <Label>超时时间(秒)</Label>
                <Input
                  type="number"
                  value={monitorData.timeout}
                  onChange={(e) => setMonitorData({ ...monitorData, timeout: parseInt(e.target.value) || 5 })}
                  min={1}
                />
              </div>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setMonitorDialogOpen(false)}>取消</Button>
            <Button onClick={handleCreateMonitor} disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              创建监控
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
