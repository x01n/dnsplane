'use client'

import { useState, useEffect, useMemo, useRef } from 'react'
import Link from 'next/link'
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
  ShieldCheck,
  Activity,
  Grid3X3,
  List,
  Filter,
  ChevronDown,
  ChevronsUpDown,
  Check,
  Link2,
  Weight,
  Sparkles,
  History as HistoryIcon,
  Globe,
  Zap,
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
import { domainApi, DNSRecord, RecordLine, authApi, User, Domain } from '@/lib/api'
import { DNS_RECORD_TYPES, copyToClipboard, cn, hasModuleAccess } from '@/lib/utils'
import { ProviderBadge } from '@/components/provider-icon'
import { Popover, PopoverContent, PopoverTrigger } from '@/components/ui/popover'
import { Command, CommandInput, CommandList, CommandEmpty, CommandGroup, CommandItem } from '@/components/ui/command'
import { Pagination } from '@/components/pagination'

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
  const [domainInfo, setDomainInfo] = useState<{ name: string; type_name: string; account_type: string; record_count: number; aid: number } | null>(null)

  const [records, setRecords] = useState<DNSRecord[]>([])
  const [lines, setLines] = useState<RecordLine[]>([])
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)

  // 域名快速切换
  const [switcherOpen, setSwitcherOpen] = useState(false)
  const [siblingDomains, setSiblingDomains] = useState<Domain[]>([])
  const [siblingLoading, setSiblingLoading] = useState(false)
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
  const [selectedRecord, setSelectedRecord] = useState<DNSRecord | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [selectedRecordIds, setSelectedRecordIds] = useState<string[]>([])
  const [aliases, setAliases] = useState<Array<{ id: number; did: number; name: string }>>([])
  const [newAlias, setNewAlias] = useState('')
  const [smartParseValue, setSmartParseValue] = useState('')
  const [smartParseResult, setSmartParseResult] = useState<{ type: string; value: string } | null>(null)
  const [quickInfo, setQuickInfo] = useState<{ lines: RecordLine[]; min_ttl: number; supports_weight: boolean; supports_remark: number; supports_log: boolean; supports_status: boolean } | null>(null)
  const [recordLogs, setRecordLogs] = useState<unknown[]>([])
  const [recordLogsLoading, setRecordLogsLoading] = useState(false)

  const [dnsCheckOpen, setDnsCheckOpen] = useState(false)
  const [dnsCheckDomain, setDnsCheckDomain] = useState('')
  const [dnsCheckType, setDnsCheckType] = useState('A')
  const [dnsCheckLoading, setDnsCheckLoading] = useState(false)
  const [dnsCheckResults, setDnsCheckResults] = useState<Array<{ server: string; ip: string; results: string[]; ttl: string; cost: number; error?: string }>>([])

  const [formData, setFormData] = useState({
    Weight: 0,
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

  const [currentUser, setCurrentUser] = useState<User | null>(null)
  const [accelDialogOpen, setAccelDialogOpen] = useState(false)
  const [accelRecord, setAccelRecord] = useState<DNSRecord | null>(null)
  const [accelStrategy, setAccelStrategy] = useState('')
  const [accelLoading, setAccelLoading] = useState(false)
  const canUseMonitor = currentUser != null && hasModuleAccess(currentUser, 'monitor')
  const canUseCert = currentUser != null && hasModuleAccess(currentUser, 'cert')

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

  // domainId 变化时拉取；fetch 函数内含本地状态，不宜放入依赖避免循环
  useEffect(() => {
    if (domainId) {
      fetchDomainInfo()
      fetchLines()
      fetchAliases()
      fetchQuickInfo()
      fetchRecordLogs()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [domainId])

  const fetchAliases = async () => {
    try {
      const res = await domainApi.getAliases(domainId)
      if (res.code === 0 && res.data) setAliases(res.data.list || [])
    } catch {
      // ignore
    }
  }

  const fetchQuickInfo = async () => {
    try {
      const res = await domainApi.getRecordQuickInfo(domainId)
      if (res.code === 0 && res.data) setQuickInfo(res.data)
    } catch {
      // ignore
    }
  }

  const fetchRecordLogs = async () => {
    setRecordLogsLoading(true)
    try {
      const res = await domainApi.getRecordChangeLogs(domainId, { page: 1, page_size: 20 })
      if (res.code === 0 && res.data) setRecordLogs(res.data.list || [])
    } catch {
      // ignore
    } finally {
      setRecordLogsLoading(false)
    }
  }

  const handleAddAlias = async () => {
    const name = newAlias.trim()
    if (!name) {
      toast.error('请输入别名')
      return
    }
    try {
      const res = await domainApi.addAlias(domainId, name)
      if (res.code === 0) {
        toast.success('别名添加成功')
        setNewAlias('')
        fetchAliases()
      } else {
        toast.error(res.msg || '添加失败')
      }
    } catch {
      toast.error('添加失败')
    }
  }

  const handleDeleteAlias = async (aliasId: number) => {
    try {
      const res = await domainApi.deleteAlias(aliasId)
      if (res.code === 0) {
        toast.success('别名删除成功')
        fetchAliases()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  const handleSmartParse = async () => {
    const value = smartParseValue.trim()
    if (!value) {
      toast.error('请输入要识别的值')
      return
    }
    try {
      const res = await domainApi.smartParse(value)
      if (res.code === 0 && res.data) {
        setSmartParseResult(res.data)
        setFormData((prev) => ({ ...prev, Type: res.data?.type || prev.Type, Value: res.data?.value || prev.Value }))
        toast.success('识别成功，已填入表单')
      } else {
        toast.error(res.msg || '识别失败')
      }
    } catch {
      toast.error('识别失败')
    }
  }

  const handleUpdateWeight = async (record: DNSRecord, weight: number) => {
    try {
      const res = await domainApi.updateRecordWeight(domainId, record.RecordId, weight)
      if (res.code === 0) {
        toast.success('权重更新成功')
        fetchRecords(true)
      } else {
        toast.error(res.msg || '权重更新失败')
      }
    } catch {
      toast.error('权重更新失败')
    }
  }

  const openWeightPrompt = (record: DNSRecord) => {
    const current = typeof record.Weight === 'number' ? record.Weight : 0
    const raw = window.prompt(`请输入 ${record.Name} 的新权重`, String(current))
    if (raw == null) return
    const next = parseInt(raw, 10)
    if (!Number.isFinite(next) || next < 0) {
      toast.error('请输入有效权重')
      return
    }
    handleUpdateWeight(record, next)
  }

  const canEditWeight = quickInfo?.supports_weight === true
  const canViewRecordLogs = quickInfo?.supports_log === true
  const minTTLHint = quickInfo?.min_ttl || 600
  const lineOptions = quickInfo?.lines || lines
  const fmtLog = (item: unknown) => typeof item === 'string' ? item : JSON.stringify(item)

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
          aid: domain.aid,
        })
      }
    } catch {
      // ignore
    }
  }

  const fetchSiblingDomains = async (aid: number) => {
    setSiblingLoading(true)
    try {
      const res = await domainApi.list({ aid, page_size: 200 })
      if (res.code === 0 && res.data) {
        setSiblingDomains(res.data.list || [])
      }
    } catch {
      // ignore
    } finally {
      setSiblingLoading(false)
    }
  }

  useEffect(() => {
    if (domainInfo?.aid) {
      fetchSiblingDomains(domainInfo.aid)
    }
  }, [domainInfo?.aid])

  const handleSwitchDomain = (targetId: number) => {
    setSwitcherOpen(false)
    router.push(`/dashboard/domains/${targetId}`)
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
      Type: smartParseResult?.type || 'A',
      Value: smartParseResult?.value || '',
      Line: lineOptions[0]?.id || '',
      TTL: minTTLHint,
      MX: 10,
      Weight: 0,
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
      Weight: typeof record.Weight === 'number' ? record.Weight : 0,
      Remark: record.Remark || '',
    })
    setDialogOpen(true)
  }

  const openDeleteDialog = (record: DNSRecord) => {
    setSelectedRecord(record)
    setDeleteDialogOpen(true)
  }

  /** 跳转监控页智能创建向导，并预填域名与子域（主机记录） */
  const openMonitorWizard = (record: DNSRecord) => {
    if (!domainId) return
    const rr = encodeURIComponent(record.Name)
    router.push(
      `/dashboard/monitor?open_smart=1&prefill_did=${encodeURIComponent(domainId)}&prefill_rr=${rr}`,
    )
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

  const [accelPlatforms, setAccelPlatforms] = useState<Array<{ platform: string; enabled: boolean; available: boolean; prefer_domain?: string; domain_name?: string; zone_id?: string; site_id?: string }>>([])
  const [accelDefaultStrategy, setAccelDefaultStrategy] = useState('')
  const [accelResults, setAccelResults] = useState<Array<{ platform: string; line_name: string; cname: string; status: string; msg?: string }>>([])

  const handleAccelerate = (record: DNSRecord) => {
    setAccelRecord(record)
    setAccelStrategy('')
    setAccelResults([])
    setAccelDialogOpen(true)
    if (accelPlatforms.length === 0) {
      domainApi.getAccelPlatforms().then(res => {
        if (res.code === 0 && res.data) {
          setAccelPlatforms(res.data.platforms)
          setAccelDefaultStrategy(res.data.strategy)
        }
      })
    }
  }

  const handleAccelConfirm = async () => {
    if (!accelRecord) return
    setAccelLoading(true)
    setAccelResults([])
    try {
      const res = await domainApi.accelerate(domainId, {
        record_name: accelRecord.Name,
        record_type: accelRecord.Type,
        record_value: Array.isArray(accelRecord.Value) ? accelRecord.Value[0] : accelRecord.Value,
        strategy: accelStrategy || undefined,
      })
      if (res.code === 0) {
        const data = res.data as Record<string, unknown>
        const accelRes = (data?.accel_results || []) as Array<{ platform: string; line_name: string; cname: string; status: string; msg?: string }>
        const dnsRes = (data?.dns_results || []) as Array<{ platform: string; line_name: string; cname: string; status: string; msg?: string }>
        setAccelResults([...accelRes, ...dnsRes])
        toast.success(res.msg || '加速配置完成')
        fetchRecords()
      } else {
        toast.error(res.msg || '加速失败')
      }
    } catch {
      toast.error('加速请求失败')
    } finally {
      setAccelLoading(false)
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

  const handleDnsCheck = async () => {
    if (!dnsCheckDomain.trim()) {
      toast.error('请输入域名')
      return
    }
    setDnsCheckLoading(true)
    setDnsCheckResults([])
    try {
      const res = await domainApi.dnsCheck({ domain: dnsCheckDomain.trim(), type: dnsCheckType })
      if (res.code === 0 && res.data) {
        setDnsCheckResults(res.data)
      } else {
        toast.error(res.msg || '检测失败')
      }
    } catch {
      toast.error('检测请求失败')
    } finally {
      setDnsCheckLoading(false)
    }
  }

  const openDnsCheck = () => {
    if (domainInfo?.name && !dnsCheckDomain) {
      setDnsCheckDomain(domainInfo.name)
    }
    setDnsCheckOpen(true)
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
                  <Popover open={switcherOpen} onOpenChange={setSwitcherOpen}>
                    <PopoverTrigger asChild>
                      <button className="inline-flex items-center gap-1.5 font-medium text-foreground hover:text-primary transition-colors rounded px-1.5 py-0.5 hover:bg-accent">
                        <span className="truncate max-w-[200px]">{domainInfo.name}</span>
                        <ChevronsUpDown className="h-3.5 w-3.5 shrink-0 opacity-60" />
                      </button>
                    </PopoverTrigger>
                    <PopoverContent className="w-[280px] p-0" align="start">
                      <Command>
                        <CommandInput placeholder="搜索域名..." />
                        <CommandList>
                          <CommandEmpty>
                            {siblingLoading ? (
                              <span className="inline-flex items-center gap-2">
                                <Loader2 className="h-4 w-4 animate-spin" />
                                加载中...
                              </span>
                            ) : '未找到域名'}
                          </CommandEmpty>
                          <CommandGroup>
                            {siblingDomains.map((d) => (
                              <CommandItem
                                key={d.id}
                                value={d.name}
                                disabled={String(d.id) === domainId}
                                onSelect={() => handleSwitchDomain(d.id)}
                                className="flex items-center justify-between"
                              >
                                <span className="truncate">{d.name}</span>
                                {String(d.id) === domainId && (
                                  <Check className="h-4 w-4 shrink-0 text-primary" />
                                )}
                              </CommandItem>
                            ))}
                          </CommandGroup>
                        </CommandList>
                      </Command>
                    </PopoverContent>
                  </Popover>
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
          {domainInfo && canUseCert && (
            <Button variant="outline" size="sm" className="min-h-10 flex-1 sm:flex-initial" asChild>
              <Link href={`/dashboard/cert?domain=${encodeURIComponent(domainInfo.name)}`}>
                <ShieldCheck className="h-4 w-4 sm:mr-2" />
                <span className="hidden sm:inline">申请证书</span>
                <span className="sm:hidden">证书</span>
              </Link>
            </Button>
          )}
          {domainId && canUseMonitor && (
            <Button variant="outline" size="sm" className="min-h-10 flex-1 sm:flex-initial" asChild>
              <Link
                href={`/dashboard/monitor?open_smart=1&prefill_did=${encodeURIComponent(domainId)}`}
              >
                <Activity className="h-4 w-4 sm:mr-2" />
                <span className="hidden sm:inline">智能监控</span>
                <span className="sm:hidden">监控</span>
              </Link>
            </Button>
          )}
          <Button variant="outline" size="sm" className="min-h-10 flex-1 sm:flex-initial" onClick={openDnsCheck}>
            <Globe className="h-4 w-4 sm:mr-2" />
            <span className="hidden sm:inline">DNS检测</span>
            <span className="sm:hidden">检测</span>
          </Button>
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

      {domainInfo && (
        <div className="grid gap-4 lg:grid-cols-3">
          <Card>
            <CardHeader><div className="flex items-center gap-2 font-semibold"><Link2 className="h-4 w-4" />域名别名</div></CardHeader>
            <CardContent className="space-y-3">
              <div className="flex gap-2">
                <Input placeholder="添加别名" value={newAlias} onChange={(e) => setNewAlias(e.target.value)} />
                <Button onClick={handleAddAlias}>添加</Button>
              </div>
              <div className="flex flex-wrap gap-2">
                {aliases.length === 0 ? <span className="text-sm text-muted-foreground">暂无别名</span> : aliases.map((alias) => (
                  <Badge key={alias.id} variant="secondary" className="gap-2">
                    {alias.name}
                    <button type="button" onClick={() => handleDeleteAlias(alias.id)} className="text-xs opacity-80 hover:opacity-100">×</button>
                  </Badge>
                ))}
              </div>
            </CardContent>
          </Card>

          <Card>
            <CardHeader><div className="flex items-center gap-2 font-semibold"><Sparkles className="h-4 w-4" />智能识别</div></CardHeader>
            <CardContent className="space-y-3">
              <div className="flex gap-2">
                <Input placeholder="输入 IP / 域名 / 值" value={smartParseValue} onChange={(e) => setSmartParseValue(e.target.value)} />
                <Button variant="outline" onClick={handleSmartParse}>识别</Button>
              </div>
              {smartParseResult && <div className="text-sm text-muted-foreground">识别结果：<Badge variant="outline">{smartParseResult.type}</Badge><span className="ml-2 break-all">{smartParseResult.value}</span></div>}
              {quickInfo && <div className="text-xs text-muted-foreground">最小 TTL：{quickInfo.min_ttl}，权重：{quickInfo.supports_weight ? '支持' : '不支持'}，备注：{quickInfo.supports_remark ? '支持' : '不支持'}</div>}
            </CardContent>
          </Card>

          <Card>
            <CardHeader><div className="flex items-center gap-2 font-semibold"><HistoryIcon className="h-4 w-4" />记录日志</div></CardHeader>
            <CardContent className="space-y-2">
              {!canViewRecordLogs ? (
                <div className="text-sm text-muted-foreground">当前服务商不支持记录日志</div>
              ) : recordLogsLoading ? (
                <div className="text-sm text-muted-foreground">加载中...</div>
              ) : recordLogs.length === 0 ? (
                <div className="text-sm text-muted-foreground">暂无日志</div>
              ) : (
                <div className="space-y-2 max-h-40 overflow-auto text-xs">
                  {recordLogs.map((item, idx) => (
                    <div key={idx} className="rounded border p-2 break-all bg-muted/30">{fmtLog(item)}</div>
                  ))}
                </div>
              )}
            </CardContent>
          </Card>
        </div>
      )}

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
                    <TableHead className="font-semibold w-20">权重</TableHead>
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
                        {canEditWeight && (record.Type === 'A' || record.Type === 'AAAA') ? (
                          <Button variant="ghost" size="sm" onClick={() => openWeightPrompt(record)} className="h-8 px-2">
                            <Weight className="h-3.5 w-3.5 mr-1" />{typeof record.Weight === 'number' ? record.Weight : 0}
                          </Button>
                        ) : (
                          <span className="text-sm text-muted-foreground">{typeof record.Weight === 'number' ? record.Weight : '-'}</span>
                        )}
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
                              <DropdownMenuItem onClick={() => openMonitorWizard(record)}>
                                <Shield className="h-4 w-4 mr-2" />
                                智能监控向导
                              </DropdownMenuItem>
                            )}
                            {(record.Type === 'A' || record.Type === 'AAAA' || record.Type === 'CNAME') && (
                              <DropdownMenuItem onClick={() => handleAccelerate(record)}>
                                <Zap className="h-4 w-4 mr-2" />
                                一键加速
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
                          <DropdownMenuItem onClick={() => openMonitorWizard(record)}>
                            <Shield className="h-4 w-4 mr-2" />
                            智能监控向导
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
                        <span>权重: {typeof record.Weight === 'number' ? record.Weight : '-'}</span>
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
            <Pagination
              page={recordPage}
              pageSize={recordPageSize}
              total={recordTotal}
              onPageChange={setRecordPage}
              onPageSizeChange={(size) => handleRecordPageSizeChange(String(size))}
              pageSizeOptions={LIST_PAGE_SIZE_OPTIONS}
            />
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

            {canEditWeight && (formData.Type === 'A' || formData.Type === 'AAAA') && (
              <div className="space-y-2">
                <Label>权重</Label>
                <Input
                  type="number"
                  value={formData.Weight}
                  onChange={(e) => setFormData({ ...formData, Weight: Math.max(0, parseInt(e.target.value) || 0) })}
                  min={0}
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

      {/* DNS检测弹窗 */}
      <Dialog open={dnsCheckOpen} onOpenChange={setDnsCheckOpen}>
        <DialogContent className="max-w-2xl max-h-[80vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>DNS检测工具</DialogTitle>
            <DialogDescription>查询域名在各个公共DNS服务器上的解析结果</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-[1fr_auto_auto] gap-3 items-end">
              <div className="space-y-2">
                <Label>域名</Label>
                <Input
                  value={dnsCheckDomain}
                  onChange={(e) => setDnsCheckDomain(e.target.value)}
                  placeholder="example.com"
                />
              </div>
              <div className="space-y-2">
                <Label>类型</Label>
                <Select value={dnsCheckType} onValueChange={setDnsCheckType}>
                  <SelectTrigger className="w-[100px]">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent>
                    {['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS'].map((t) => (
                      <SelectItem key={t} value={t}>{t}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <Button onClick={handleDnsCheck} disabled={dnsCheckLoading}>
                {dnsCheckLoading && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                检测
              </Button>
            </div>
            {dnsCheckResults.length > 0 && (
              <div className="rounded-md border">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>DNS服务器</TableHead>
                      <TableHead>IP</TableHead>
                      <TableHead>解析结果</TableHead>
                      <TableHead className="text-right">耗时</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {dnsCheckResults.map((r, idx) => (
                      <TableRow key={idx}>
                        <TableCell className="font-medium">{r.server}</TableCell>
                        <TableCell className="text-muted-foreground text-xs">{r.ip}</TableCell>
                        <TableCell>
                          {r.error ? (
                            <span className="text-destructive text-sm">{r.error}</span>
                          ) : r.results.length > 0 ? (
                            <div className="space-y-0.5">
                              {r.results.map((v, vi) => (
                                <div key={vi} className="text-sm font-mono">{v}</div>
                              ))}
                            </div>
                          ) : (
                            <span className="text-muted-foreground text-sm">无结果</span>
                          )}
                        </TableCell>
                        <TableCell className="text-right text-sm">{r.cost}ms</TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              </div>
            )}
          </div>
        </DialogContent>
      </Dialog>

      {/* 加速策略选择对话框 */}
      <Dialog open={accelDialogOpen} onOpenChange={setAccelDialogOpen}>
        <DialogContent className="sm:max-w-md">
          <DialogHeader>
            <DialogTitle>一键加速</DialogTitle>
            <DialogDescription>
              {accelRecord && `为 ${accelRecord.Name} 配置加速`}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-4 py-4">
            {accelResults.length > 0 ? (
              <div className="space-y-2">
                <Label className="text-xs text-muted-foreground">加速结果</Label>
                <div className="space-y-1">
                  {accelResults.map((r, i) => (
                    <div key={i} className={`rounded border p-2 text-xs ${r.status === 'error' || r.status === 'failed' ? 'border-red-500/50 bg-red-50 dark:bg-red-950/20' : r.status === 'skipped' ? 'border-yellow-500/50 bg-yellow-50 dark:bg-yellow-950/20' : 'border-green-500/50 bg-green-50 dark:bg-green-950/20'}`}>
                      <div className="flex items-center justify-between">
                        <span className="font-medium">{r.platform === 'cloudflare' ? 'CF' : r.platform === 'tencenteo' ? 'EO' : 'ESA'}{r.line_name ? ` (${r.line_name})` : ''}</span>
                        <span className={r.status === 'error' || r.status === 'failed' ? 'text-red-600' : r.status === 'skipped' ? 'text-yellow-600' : 'text-green-600'}>
                          {{success: '成功', added: '已添加', exists: '已存在', skipped: '跳过', error: '失败', failed: '失败'}[r.status] || r.status}
                        </span>
                      </div>
                      {r.cname && <div className="text-muted-foreground mt-1 truncate" title={r.cname}>CNAME → {r.cname}</div>}
                      {r.msg && <div className="text-red-600 mt-1">{r.msg}</div>}
                    </div>
                  ))}
                </div>
              </div>
            ) : (
              <>
                {accelPlatforms.length > 0 && (
                  <div className="space-y-2">
                    <Label className="text-xs text-muted-foreground">平台状态</Label>
                    <div className="grid grid-cols-3 gap-2 text-xs">
                      {accelPlatforms.map(p => (
                        <div key={p.platform} className={`rounded border p-2 ${p.available ? 'border-green-500/50 bg-green-50 dark:bg-green-950/20' : 'border-muted opacity-50'}`}>
                          <div className="font-medium">
                            {p.platform === 'cloudflare' ? 'Cloudflare' : p.platform === 'tencenteo' ? '腾讯云 EO' : '阿里云 ESA'}
                          </div>
                          <div className={p.available ? 'text-green-600' : 'text-muted-foreground'}>
                            {p.available ? '已配置' : '未配置'}
                          </div>
                          {p.available && p.platform === 'cloudflare' && p.prefer_domain && (
                            <div className="text-muted-foreground truncate" title={p.prefer_domain}>{p.prefer_domain}</div>
                          )}
                        </div>
                      ))}
                    </div>
                  </div>
                )}
                {accelPlatforms.length > 0 && !accelPlatforms.some(p => p.available) ? (
                  <div className="rounded-md bg-yellow-50 dark:bg-yellow-950/20 border border-yellow-200 dark:border-yellow-800 p-3 text-sm text-yellow-800 dark:text-yellow-200">
                    尚未配置任何加速平台，请前往 <a href="/dashboard/settings" className="underline font-medium">系统设置 → 一键加速</a> 完成配置
                  </div>
                ) : (
                  <>
                    <div className="space-y-2">
                      <Label>加速策略</Label>
                      <Select value={accelStrategy} onValueChange={setAccelStrategy}>
                        <SelectTrigger>
                          <SelectValue placeholder={`使用系统默认（${
                            {cf_only: '仅 CF', eo_only: '仅 EO', esa_only: '仅 ESA', mixed_cf_eo: 'CF+EO', mixed_cf_esa: 'CF+ESA'}[accelDefaultStrategy] || accelDefaultStrategy
                          }）`} />
                        </SelectTrigger>
                        <SelectContent>
                          {accelPlatforms.find(p => p.platform === 'cloudflare')?.available && (
                            <SelectItem value="cf_only">仅 Cloudflare（海外优选）</SelectItem>
                          )}
                          {accelPlatforms.find(p => p.platform === 'tencenteo')?.available && (
                            <SelectItem value="eo_only">仅腾讯云 EO</SelectItem>
                          )}
                          {accelPlatforms.find(p => p.platform === 'aliyunesa')?.available && (
                            <SelectItem value="esa_only">仅阿里云 ESA</SelectItem>
                          )}
                          {accelPlatforms.find(p => p.platform === 'cloudflare')?.available && accelPlatforms.find(p => p.platform === 'tencenteo')?.available && (
                            <SelectItem value="mixed_cf_eo">混合：国内 EO + 海外 CF</SelectItem>
                          )}
                          {accelPlatforms.find(p => p.platform === 'cloudflare')?.available && accelPlatforms.find(p => p.platform === 'aliyunesa')?.available && (
                            <SelectItem value="mixed_cf_esa">混合：国内 ESA + 海外 CF</SelectItem>
                          )}
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">留空则使用系统默认策略，仅显示已配置账号的可用选项</p>
                    </div>
                    {accelRecord && (
                      <div className="text-sm text-muted-foreground space-y-1">
                        <p>记录类型：{accelRecord.Type}</p>
                        <p>源站地址：{Array.isArray(accelRecord.Value) ? accelRecord.Value[0] : accelRecord.Value}</p>
                      </div>
                    )}
                  </>
                )}
              </>
            )}
          </div>
          <DialogFooter>
            {accelResults.length > 0 ? (
              <Button onClick={() => setAccelDialogOpen(false)}>完成</Button>
            ) : (
              <>
                <Button variant="outline" onClick={() => setAccelDialogOpen(false)}>取消</Button>
                <Button onClick={handleAccelConfirm} disabled={accelLoading || (accelPlatforms.length > 0 && !accelPlatforms.some(p => p.available))}>
                  {accelLoading ? '加速中...' : '确认加速'}
                </Button>
              </>
            )}
          </DialogFooter>
        </DialogContent>
      </Dialog>

    </div>
  )
}
