'use client'

import { useState, useEffect, useMemo, useCallback, useRef } from 'react'
import { useSearchParams, useRouter, usePathname } from 'next/navigation'
import {
  Plus,
  Search,
  MoreHorizontal,
  Pencil,
  Trash2,
  RefreshCw,
  Loader2,
  CheckCircle,
  XCircle,
  Activity,
  AlertTriangle,
  Shield,
  Eye,
  ArrowRightLeft,
  Globe,
  Zap,
  ChevronRight,
  ChevronLeft,
  Settings2,
  Bell,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
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
import { Badge } from '@/components/ui/badge'
import { Switch } from '@/components/ui/switch'
import { Checkbox } from '@/components/ui/checkbox'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Separator } from '@/components/ui/separator'
import { Tooltip, TooltipContent, TooltipProvider, TooltipTrigger } from '@/components/ui/tooltip'
import { toast } from 'sonner'
import { monitorApi, domainApi, MonitorTask, MonitorOverview, Domain, MonitorLog, authApi, User } from '@/lib/api'
import { formatDate, MONITOR_CHECK_TYPES, MONITOR_SWITCH_TYPES, cn, hasModuleAccess } from '@/lib/utils'

/** 智能添加 / lookup 返回的记录行（兼容 PascalCase 与小写） */
type LookupRecordRow = {
  id?: string
  RecordId?: string
  record_id?: string
  type?: string
  Type?: string
  value?: string
  Value?: string
  line?: string
  Line?: string
  LineName?: string
  line_name?: string
  name?: string
}

type ResolveStatusRow = {
  address?: string
  value?: string
  role?: string
  status?: string
  success?: boolean
  latency?: number
  duration?: number
  error?: string
  last_check?: string
}

type HistoryPoint = { success: boolean; duration: number; created_at: string }

// ============== Mini Uptime Bar ==============
function MiniUptimeBar({ history }: { history: HistoryPoint[] }) {
  const last20 = history.slice(-20)
  if (last20.length === 0) {
    return (
      <div className="flex gap-[2px] items-center h-4">
        {Array.from({ length: 20 }).map((_, i) => (
          <div key={i} className="w-[6px] h-3 rounded-[1px] bg-muted" />
        ))}
      </div>
    )
  }
  const padded = [...Array.from({ length: Math.max(0, 20 - last20.length) }).map(() => null), ...last20]
  return (
    <TooltipProvider>
      <div className="flex gap-[2px] items-center h-4">
        {padded.map((point, i) => (
          <Tooltip key={i}>
            <TooltipTrigger asChild>
              <div
                className={cn(
                  "w-[6px] h-3 rounded-[1px] transition-all hover:scale-y-125",
                  point === null ? 'bg-muted' : point.success ? 'bg-green-500' : 'bg-red-500'
                )}
              />
            </TooltipTrigger>
            {point && (
              <TooltipContent side="top" className="text-xs">
                <p>{point.success ? '正常' : '异常'} - {point.duration}ms</p>
                <p className="text-muted-foreground">{new Date(point.created_at).toLocaleString()}</p>
              </TooltipContent>
            )}
          </Tooltip>
        ))}
      </div>
    </TooltipProvider>
  )
}

export default function MonitorPage() {
  const searchParams = useSearchParams()
  const router = useRouter()
  const pathname = usePathname()
  const urlSmartPrefillDone = useRef(false)

  const [me, setMe] = useState<User | null>(null)
  const canManageMonitor = me != null && hasModuleAccess(me, 'monitor')

  const [tasks, setTasks] = useState<MonitorTask[]>([])
  const [domains, setDomains] = useState<Domain[]>([])
  const [overview, setOverview] = useState<MonitorOverview | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [statusFilter, setStatusFilter] = useState('all')
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedTask, setSelectedTask] = useState<MonitorTask | null>(null)
  const [submitting, setSubmitting] = useState(false)

  // Detail dialog
  const [detailTask, setDetailTask] = useState<MonitorTask | null>(null)
  const [showDetail, setShowDetail] = useState(false)
  const [detailLoading, setDetailLoading] = useState(false)
  const [historyData, setHistoryData] = useState<{ id: number; task_id: number; success: boolean; duration: number; created_at: string }[]>([])
  const [uptimeData, setUptimeData] = useState<Record<string, { total: number; success: number; uptime: number; avg_duration: number }> | null>(null)
  const [taskLogs, setTaskLogs] = useState<MonitorLog[]>([])
  const [resolveStatus, setResolveStatus] = useState<ResolveStatusRow[]>([])

  // Task history cache (for mini uptime bars)
  const [taskHistoryCache, setTaskHistoryCache] = useState<Record<number, HistoryPoint[]>>({})

  // Edit dialog
  const [editDialogOpen, setEditDialogOpen] = useState(false)
  const [editTask, setEditTask] = useState<MonitorTask | null>(null)
  const [editFormData, setEditFormData] = useState({
    did: '',
    rr: '',
    record_id: '',
    type: 0,
    main_value: '',
    backup_value: '',
    check_type: 0,
    check_url: '',
    tcp_port: 80,
    frequency: 60,
    cycle: 3,
    timeout: 5,
    use_proxy: false,
    cdn: false,
    remark: '',
    expect_status: '',
    expect_keyword: '',
    max_redirects: 0,
    proxy_type: '',
    proxy_host: '',
    proxy_port: 0,
    proxy_username: '',
    proxy_password: '',
    auto_restore: true,
    notify_enabled: false,
  })

  // Smart Add dialog
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [addStep, setAddStep] = useState(1)
  const [addDomainId, setAddDomainId] = useState('')
  const [addSubDomain, setAddSubDomain] = useState('')
  const [lookupLoading, setLookupLoading] = useState(false)
  const [lookupResult, setLookupResult] = useState<{
    domain: string
    account_type: string
    records: LookupRecordRow[]
  } | null>(null)
  const [selectedRecords, setSelectedRecords] = useState<LookupRecordRow[]>([])
  const [addConfig, setAddConfig] = useState({
    backup_value: '',
    type: 0,
    check_type: 0,
    check_url: '',
    tcp_port: 80,
    frequency: 60,
    cycle: 3,
    timeout: 5,
    auto_restore: true,
    notify_enabled: false,
    notify_channels: [] as string[],
    expect_status: '',
    expect_keyword: '',
    max_redirects: 0,
    use_proxy: false,
    proxy_type: '',
    proxy_host: '',
    proxy_port: 0,
    proxy_username: '',
    proxy_password: '',
  })

  // Task statistics (use overview data if available, fall back to local count)
  const taskStats = useMemo(() => {
    return {
      total: overview?.task_count ?? tasks.length,
      active: overview?.active_count ?? tasks.filter(t => t.active).length,
      healthy: overview?.healthy_count ?? tasks.filter(t => t.status === 0 && t.active).length,
      faulty: overview?.faulty_count ?? tasks.filter(t => t.status === 1).length,
      switchCount: overview?.switch_count ?? 0,
      avgUptime: overview?.avg_uptime ?? 0,
    }
  }, [tasks, overview])

  useEffect(() => {
    authApi.getUserInfo().then((res) => {
      if (res.code === 0 && res.data) setMe(res.data)
    })
  }, [])

  const fetchAllTaskHistories = useCallback(async (taskList: MonitorTask[]) => {
    const promises = taskList.map(async (task) => {
      try {
        const res = await monitorApi.getHistory(task.id, '1h')
        if (res.code === 0 && res.data) {
          return { id: task.id, data: res.data as HistoryPoint[] }
        }
      } catch {
        // ignore
      }
      return { id: task.id, data: [] as HistoryPoint[] }
    })
    const results = await Promise.all(promises)
    const cache: Record<number, HistoryPoint[]> = {}
    results.forEach((r) => {
      cache[r.id] = r.data
    })
    setTaskHistoryCache(cache)
  }, [])

  const fetchDomains = useCallback(async () => {
    try {
      const res = await domainApi.list({ page_size: 500 })
      if (res.code === 0 && res.data) {
        setDomains(res.data.list || [])
      }
    } catch {
      // ignore
    }
  }, [])

  const fetchTasks = useCallback(async () => {
    try {
      const params: Record<string, string | number> = { page_size: 200 }
      const res = await monitorApi.list(params)
      if (res.code === 0 && res.data) {
        const taskList = res.data.list || []
        setTasks(taskList)
        void fetchAllTaskHistories(taskList)
      } else if (res.code !== 0) {
        toast.error(res.msg || '获取任务列表失败')
      }
    } catch {
      toast.error('获取任务列表失败')
    } finally {
      setLoading(false)
    }
  }, [fetchAllTaskHistories])

  const fetchOverview = useCallback(async () => {
    try {
      const res = await monitorApi.getOverview()
      if (res.code === 0 && res.data) {
        setOverview(res.data)
      }
    } catch {
      // ignore
    }
  }, [])

  useEffect(() => {
    void fetchDomains()
    void fetchTasks()
    void fetchOverview()
    const interval = setInterval(() => {
      void fetchOverview()
      void fetchTasks()
    }, 15000)
    return () => clearInterval(interval)
  }, [fetchDomains, fetchTasks, fetchOverview])

  const handleSearch = (e: React.FormEvent) => {
    e.preventDefault()
    fetchTasks()
  }

  const handleRefresh = () => {
    setRefreshing(true)
    Promise.all([fetchTasks(), fetchOverview()]).finally(() => setRefreshing(false))
  }

  // ============== Edit Dialog ==============
  const openEditDialog = (task: MonitorTask) => {
    setEditTask(task)
    setEditFormData({
      did: task.did.toString(),
      rr: task.rr,
      record_id: task.record_id,
      type: task.type,
      main_value: task.main_value,
      backup_value: task.backup_value || task.backup_values || '',
      check_type: task.check_type,
      check_url: task.check_url || '',
      tcp_port: task.tcp_port || 80,
      frequency: task.frequency,
      cycle: task.cycle,
      timeout: task.timeout,
      use_proxy: task.use_proxy,
      cdn: task.cdn,
      remark: task.remark || '',
      expect_status: task.expect_status || '',
      expect_keyword: task.expect_keyword || '',
      max_redirects: task.max_redirects ?? 0,
      proxy_type: task.proxy_type || '',
      proxy_host: task.proxy_host || '',
      proxy_port: task.proxy_port || 0,
      proxy_username: task.proxy_username || '',
      proxy_password: task.proxy_password || '',
      auto_restore: task.auto_restore,
      notify_enabled: task.notify_enabled,
    })
    setEditDialogOpen(true)
  }

  const handleEditSubmit = async () => {
    if (!editTask) return
    setSubmitting(true)
    try {
      const data = {
        domain_id: parseInt(editFormData.did),
        rr: editFormData.rr,
        record_id: editFormData.record_id,
        type: editFormData.type,
        main_value: editFormData.main_value,
        backup_value: editFormData.backup_value,
        check_type: editFormData.check_type,
        check_url: editFormData.check_url,
        tcp_port: editFormData.tcp_port,
        frequency: editFormData.frequency,
        cycle: editFormData.cycle,
        timeout: editFormData.timeout,
        use_proxy: editFormData.use_proxy,
        cdn: editFormData.cdn,
        remark: editFormData.remark,
        expect_status: editFormData.expect_status,
        expect_keyword: editFormData.expect_keyword,
        max_redirects: editFormData.max_redirects,
        proxy_type: editFormData.proxy_type,
        proxy_host: editFormData.proxy_host,
        proxy_port: editFormData.proxy_port,
        proxy_username: editFormData.proxy_username,
        proxy_password: editFormData.proxy_password,
        auto_restore: editFormData.auto_restore,
        notify_enabled: editFormData.notify_enabled,
      }
      const res = await monitorApi.update(editTask.id, data)
      if (res.code === 0) {
        toast.success('修改成功')
        setEditDialogOpen(false)
        fetchTasks()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  // ============== Delete ==============
  const openDeleteDialog = (task: MonitorTask) => {
    setSelectedTask(task)
    setDeleteDialogOpen(true)
  }

  const handleDelete = async () => {
    if (!selectedTask) return
    try {
      const res = await monitorApi.delete(selectedTask.id)
      if (res.code === 0) {
        toast.success('删除成功')
        setDeleteDialogOpen(false)
        fetchTasks()
        fetchOverview()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  // ============== Toggle & Switch ==============
  const handleToggle = async (task: MonitorTask) => {
    try {
      const res = await monitorApi.toggle(task.id, !task.active)
      if (res.code === 0) {
        toast.success(task.active ? '已禁用' : '已启用')
        fetchTasks()
        fetchOverview()
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    }
  }

  const handleSwitch = async (task: MonitorTask) => {
    try {
      const res = await monitorApi.switch(task.id)
      if (res.code === 0) {
        toast.success('手动切换成功')
        fetchTasks()
      } else {
        toast.error(res.msg || '切换失败')
      }
    } catch {
      toast.error('切换失败')
    }
  }

  // ============== Detail Dialog ==============
  const handleShowDetail = async (task: MonitorTask) => {
    setDetailTask(task)
    setShowDetail(true)
    setDetailLoading(true)
    setHistoryData([])
    setUptimeData(null)
    setTaskLogs([])
    setResolveStatus([])
    try {
      const [histRes, upRes, logRes] = await Promise.all([
        monitorApi.getHistory(task.id, '24h'),
        monitorApi.getUptime(task.id),
        monitorApi.getLogs(task.id, { page_size: 50 }),
      ])
      if (histRes.code === 0) setHistoryData(histRes.data || [])
      if (upRes.code === 0) setUptimeData(upRes.data || null)
      if (logRes.code === 0 && logRes.data) setTaskLogs(logRes.data.list || [])

      // Try to fetch resolve status
      try {
        const rsRes = await monitorApi.getResolveStatus(task.id)
        if (rsRes.code === 0 && Array.isArray(rsRes.data)) {
          setResolveStatus(rsRes.data as ResolveStatusRow[])
        }
      } catch {
        // ignore - API may not exist yet
      }
    } catch { /* ignore */ }
    finally { setDetailLoading(false) }
  }

  // ============== Smart Add Dialog ==============
  const openSmartAdd = useCallback((prefill?: { domainId?: string; subDomain?: string }) => {
    setAddStep(1)
    setAddDomainId(prefill?.domainId ?? '')
    setAddSubDomain(prefill?.subDomain ?? '')
    setLookupResult(null)
    setSelectedRecords([])
    setAddConfig({
      backup_value: '',
      type: 0,
      check_type: 0,
      check_url: '',
      tcp_port: 80,
      frequency: 60,
      cycle: 3,
      timeout: 5,
      auto_restore: true,
      notify_enabled: false,
      notify_channels: [],
      expect_status: '',
      expect_keyword: '',
      max_redirects: 0,
      use_proxy: false,
      proxy_type: 'http',
      proxy_host: '',
      proxy_port: 0,
      proxy_username: '',
      proxy_password: '',
    })
    setAddDialogOpen(true)
  }, [])

  /* 从域名管理跳转：/dashboard/monitor?open_smart=1&prefill_did=123&prefill_rr=www */
  useEffect(() => {
    if (urlSmartPrefillDone.current) return
    if (searchParams.get('open_smart') !== '1') return
    const did = searchParams.get('prefill_did')
    if (!did) return
    urlSmartPrefillDone.current = true
    const rrRaw = searchParams.get('prefill_rr')
    const sub =
      rrRaw === null || rrRaw === ''
        ? ''
        : decodeURIComponent(rrRaw.replace(/\+/g, ' '))
    openSmartAdd({ domainId: did, subDomain: sub })
    router.replace(pathname, { scroll: false })
  }, [searchParams, router, pathname, openSmartAdd])

  const handleLookup = async () => {
    if (!addDomainId || !addSubDomain) {
      toast.error('请选择域名并输入子域名')
      return
    }
    setLookupLoading(true)
    try {
      const res = await monitorApi.lookup(parseInt(addDomainId), addSubDomain)
      if (res.code === 0 && res.data) {
        setLookupResult({
          domain: res.data.domain,
          account_type: res.data.account_type,
          records: Array.isArray(res.data.records) ? (res.data.records as LookupRecordRow[]) : [],
        })
        setSelectedRecords([])
        setAddStep(2)
      } else {
        toast.error(res.msg || '查询失败')
      }
    } catch {
      toast.error('查询失败')
    } finally {
      setLookupLoading(false)
    }
  }

  const handleSmartCreate = async () => {
    if (selectedRecords.length === 0) {
      toast.error('请选择至少一条记录')
      return
    }
    setSubmitting(true)
    try {
      // 将多行备用值转换为逗号分隔
      const backupValues = addConfig.backup_value
        .split('\n')
        .map(v => v.trim())
        .filter(v => v)
        .join(',')

      const data = {
        domain_id: parseInt(addDomainId),
        sub_domain: addSubDomain,
        records: selectedRecords,
        backup_value: backupValues,
        type: addConfig.type,
        check_type: addConfig.check_type,
        check_url: addConfig.check_url,
        tcp_port: addConfig.tcp_port,
        frequency: addConfig.frequency,
        cycle: addConfig.cycle,
        timeout: addConfig.timeout,
        auto_restore: addConfig.auto_restore,
        notify_enabled: addConfig.notify_enabled,
        notify_channels: addConfig.notify_channels,
        expect_status: addConfig.expect_status,
        expect_keyword: addConfig.expect_keyword,
        max_redirects: addConfig.max_redirects,
        use_proxy: addConfig.use_proxy,
        proxy_type: addConfig.proxy_type,
        proxy_host: addConfig.proxy_host,
        proxy_port: addConfig.proxy_port,
        proxy_username: addConfig.proxy_username,
        proxy_password: addConfig.proxy_password,
      }
      const res = await monitorApi.autoCreate(data)
      if (res.code === 0) {
        const created = res.data?.created ?? 0
        toast.success(`成功创建 ${created} 个监控任务`)
        setAddDialogOpen(false)
        fetchTasks()
        fetchOverview()
      } else {
        toast.error(res.msg || '创建失败')
      }
    } catch {
      toast.error('创建失败')
    } finally {
      setSubmitting(false)
    }
  }

  // ============== Record Field Helpers ==============
  // dns.Record 返回 {id, type, value, line, name}, 需兼容 PascalCase/snake_case
  const getRecordId = (r: LookupRecordRow): string => r.id || r.RecordId || r.record_id || ''
  const getRecordType = (r: LookupRecordRow): string => r.type || r.Type || ''
  const getRecordValue = (r: LookupRecordRow): string => r.value || r.Value || ''
  const getRecordLine = (r: LookupRecordRow): string =>
    r.LineName || r.line_name || r.line || r.Line || '-'

  // ============== Helpers ==============
  const parseBackupHealth = (backupHealthStr?: string): Record<string, boolean> => {
    if (!backupHealthStr) return {}
    try {
      return JSON.parse(backupHealthStr)
    } catch {
      return {}
    }
  }

  const isAnyBackupHealthy = (backupHealthStr?: string): boolean | null => {
    const map = parseBackupHealth(backupHealthStr)
    const entries = Object.entries(map)
    if (entries.length === 0) return null // no backup
    return entries.some(([, v]) => v)
  }

  const getCheckTypeName = (type: number) => {
    return MONITOR_CHECK_TYPES.find((t) => t.value === type)?.label || '未知'
  }

  const getSwitchTypeName = (type: number) => {
    return MONITOR_SWITCH_TYPES.find((t) => t.value === type)?.label || '未知'
  }

  const getActionText = (action: number) => {
    switch (action) {
      case 1: return '故障切换'
      case 2: return '恢复正常'
      default: return '未知操作'
    }
  }

  const getStatusBadge = (task: MonitorTask) => {
    if (!task.active) {
      return <Badge variant="secondary">已停用</Badge>
    }
    if (task.status === 0) {
      return (
        <Badge variant="outline" className="bg-green-50 text-green-700 border-green-200 dark:bg-green-950 dark:text-green-400 dark:border-green-800">
          <span className="mr-1">🟢</span>正常
        </Badge>
      )
    }
    if (task.status === 1) {
      return (
        <Badge variant="destructive">
          <span className="mr-1">🔴</span>已切换
        </Badge>
      )
    }
    return (
      <Badge variant="outline" className="bg-yellow-50 text-yellow-700 border-yellow-200 dark:bg-yellow-950 dark:text-yellow-400 dark:border-yellow-800">
        <span className="mr-1">🟡</span>异常
      </Badge>
    )
  }

  // Filtered tasks
  const filteredTasks = useMemo(() => {
    return tasks.filter(task => {
      if (statusFilter === '0' && task.status !== 0) return false
      if (statusFilter === '1' && task.status !== 1) return false
      if (statusFilter === 'active' && !task.active) return false
      if (statusFilter === 'inactive' && task.active) return false
      if (keyword) {
        const search = keyword.toLowerCase()
        if (!task.domain?.toLowerCase().includes(search) && !task.rr.toLowerCase().includes(search) && !task.main_value?.toLowerCase().includes(search)) {
          return false
        }
      }
      return true
    })
  }, [tasks, statusFilter, keyword])

  return (
    <div className="space-y-6">
      {me && !hasModuleAccess(me, 'monitor') && (
        <div className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-100">
          当前账号未开通「容灾监控」模块权限，仅可浏览页面；写操作将被服务器拒绝。
        </div>
      )}
      {/* Page Header */}
      <div className="flex items-center justify-between">
        <div className="flex items-center gap-3">
          <div className="p-2 rounded-lg bg-primary/10">
            <Shield className="h-6 w-6 text-primary" />
          </div>
          <div>
            <h1 className="text-2xl font-bold">容灾监控</h1>
            <p className="text-sm text-muted-foreground">DNS容灾检测与自动切换管理</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={handleRefresh} disabled={refreshing}>
            <RefreshCw className={cn("h-4 w-4 mr-2", refreshing && "animate-spin")} />
            刷新
          </Button>
          {canManageMonitor && (
            <Button size="sm" onClick={openSmartAdd}>
              <Plus className="h-4 w-4 mr-2" />
              添加任务
            </Button>
          )}
        </div>
      </div>

      {/* Stat Cards */}
      <div className="grid gap-4 md:grid-cols-3 lg:grid-cols-6">
        <Card className="hover:shadow-md transition-shadow">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">监控任务</p>
                <p className="text-2xl font-bold">{taskStats.total}</p>
              </div>
              <div className="p-2 rounded-full bg-blue-50 dark:bg-blue-950">
                <Activity className="h-5 w-5 text-blue-600 dark:text-blue-400" />
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="hover:shadow-md transition-shadow">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">活跃任务</p>
                <p className="text-2xl font-bold">{taskStats.active}</p>
              </div>
              <div className="p-2 rounded-full bg-purple-50 dark:bg-purple-950">
                <Zap className="h-5 w-5 text-purple-600 dark:text-purple-400" />
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="hover:shadow-md transition-shadow">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">健康任务</p>
                <p className="text-2xl font-bold text-green-600">{taskStats.healthy}</p>
              </div>
              <div className="p-2 rounded-full bg-green-50 dark:bg-green-950">
                <CheckCircle className="h-5 w-5 text-green-600 dark:text-green-400" />
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="hover:shadow-md transition-shadow">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">故障任务</p>
                <p className="text-2xl font-bold text-red-600">{taskStats.faulty}</p>
              </div>
              <div className="p-2 rounded-full bg-red-50 dark:bg-red-950">
                <XCircle className="h-5 w-5 text-red-600 dark:text-red-400" />
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="hover:shadow-md transition-shadow">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">24h切换</p>
                <p className="text-2xl font-bold text-orange-600">{taskStats.switchCount}</p>
              </div>
              <div className="p-2 rounded-full bg-orange-50 dark:bg-orange-950">
                <ArrowRightLeft className="h-5 w-5 text-orange-600 dark:text-orange-400" />
              </div>
            </div>
          </CardContent>
        </Card>
        <Card className="hover:shadow-md transition-shadow">
          <CardContent className="p-4">
            <div className="flex items-center justify-between">
              <div>
                <p className="text-sm text-muted-foreground">平均可用率</p>
                <p className={cn(
                  "text-2xl font-bold",
                  taskStats.avgUptime >= 99 ? 'text-green-600' : taskStats.avgUptime >= 95 ? 'text-yellow-600' : 'text-red-600'
                )}>
                  {taskStats.avgUptime > 0 ? `${taskStats.avgUptime}%` : '-'}
                </p>
              </div>
              <div className="p-2 rounded-full bg-cyan-50 dark:bg-cyan-950">
                <Shield className="h-5 w-5 text-cyan-600 dark:text-cyan-400" />
              </div>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* Task Table */}
      <Card>
        <CardHeader className="pb-3">
          <div className="flex items-center gap-4">
            <form onSubmit={handleSearch} className="flex-1 flex gap-2">
              <div className="relative flex-1 max-w-sm">
                <Search className="absolute left-3 top-1/2 -translate-y-1/2 h-4 w-4 text-muted-foreground" />
                <Input
                  placeholder="搜索域名或主机记录..."
                  value={keyword}
                  onChange={(e) => setKeyword(e.target.value)}
                  className="pl-9"
                />
              </div>
              <Select value={statusFilter} onValueChange={setStatusFilter}>
                <SelectTrigger className="w-32">
                  <SelectValue placeholder="全部状态" />
                </SelectTrigger>
                <SelectContent>
                  <SelectItem value="all">全部状态</SelectItem>
                  <SelectItem value="0">正常</SelectItem>
                  <SelectItem value="1">已切换</SelectItem>
                  <SelectItem value="active">已启用</SelectItem>
                  <SelectItem value="inactive">已停用</SelectItem>
                </SelectContent>
              </Select>
              <Button type="submit" variant="secondary">搜索</Button>
            </form>
          </div>
        </CardHeader>
        <CardContent>
          {loading ? (
            <div className="flex flex-col items-center justify-center py-16">
              <Loader2 className="h-10 w-10 animate-spin text-primary mb-4" />
              <p className="text-muted-foreground">加载中...</p>
            </div>
          ) : filteredTasks.length === 0 ? (
            <div className="flex flex-col items-center justify-center py-16 text-center">
              <div className="w-16 h-16 rounded-full bg-muted flex items-center justify-center mb-4">
                <Shield className="h-8 w-8 text-muted-foreground" />
              </div>
              <h3 className="text-lg font-medium mb-2">暂无监控任务</h3>
              <p className="text-muted-foreground mb-4">
                {canManageMonitor ? '点击「添加任务」创建第一个容灾监控任务' : '当前账号无监控写权限，如需创建请联系管理员'}
              </p>
              {canManageMonitor && (
                <Button onClick={openSmartAdd}>
                  <Plus className="h-4 w-4 mr-2" />
                  添加任务
                </Button>
              )}
            </div>
          ) : (
            <div className="border rounded-lg overflow-hidden">
              <Table>
                <TableHeader>
                  <TableRow className="bg-muted/50">
                    <TableHead className="font-semibold">域名</TableHead>
                    <TableHead className="font-semibold">类型</TableHead>
                    <TableHead className="font-semibold">主值</TableHead>
                    <TableHead className="font-semibold w-20 text-center">主源</TableHead>
                    <TableHead className="font-semibold w-20 text-center">备用</TableHead>
                    <TableHead className="font-semibold w-24">状态</TableHead>
                    <TableHead className="font-semibold">可用率</TableHead>
                    <TableHead className="font-semibold w-20">启用</TableHead>
                    <TableHead className="w-[80px]"></TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {filteredTasks.map((task) => {
                    const hist = taskHistoryCache[task.id] || []
                    const backupHealthy = isAnyBackupHealthy(task.backup_health)
                    return (
                      <TableRow key={task.id} className="group hover:bg-muted/30 cursor-pointer" onClick={() => handleShowDetail(task)}>
                        <TableCell>
                          <div className="font-medium">
                            <span className="text-primary">{task.rr}</span>
                            <span className="text-muted-foreground">.{task.domain || '?'}</span>
                          </div>
                          {task.last_error && task.status === 1 && (
                            <p className="text-xs text-red-500 truncate max-w-[200px] mt-0.5">{task.last_error}</p>
                          )}
                        </TableCell>
                        <TableCell>
                          <Badge variant="outline" className={cn(
                            "text-xs",
                            task.check_type === 0 && "bg-blue-50 text-blue-700 border-blue-200 dark:bg-blue-950 dark:text-blue-400",
                            task.check_type === 1 && "bg-purple-50 text-purple-700 border-purple-200 dark:bg-purple-950 dark:text-purple-400",
                            task.check_type === 2 && "bg-green-50 text-green-700 border-green-200 dark:bg-green-950 dark:text-green-400",
                            task.check_type === 3 && "bg-orange-50 text-orange-700 border-orange-200 dark:bg-orange-950 dark:text-orange-400",
                          )}>
                            {getCheckTypeName(task.check_type)}
                          </Badge>
                        </TableCell>
                        <TableCell>
                          <code className="text-sm bg-muted px-1.5 py-0.5 rounded max-w-[180px] truncate block">
                            {task.main_value}
                          </code>
                        </TableCell>
                        {/* Main health indicator */}
                        <TableCell className="text-center">
                          {!task.active ? (
                            <span className="inline-block w-3 h-3 rounded-full bg-gray-300 dark:bg-gray-600" title="未启用" />
                          ) : task.main_health ? (
                            <span className="inline-block w-3 h-3 rounded-full bg-green-500 shadow-[0_0_6px_rgba(34,197,94,0.5)]" title="主源健康" />
                          ) : (
                            <span className="inline-block w-3 h-3 rounded-full bg-red-500 shadow-[0_0_6px_rgba(239,68,68,0.5)] animate-pulse" title="主源异常" />
                          )}
                        </TableCell>
                        {/* Backup health indicator */}
                        <TableCell className="text-center">
                          {backupHealthy === null ? (
                            <span className="inline-block w-3 h-3 rounded-full bg-gray-200 dark:bg-gray-700" title="无备用" />
                          ) : backupHealthy ? (
                            <span className="inline-block w-3 h-3 rounded-full bg-green-500 shadow-[0_0_6px_rgba(34,197,94,0.5)]" title="备用健康" />
                          ) : (
                            <span className="inline-block w-3 h-3 rounded-full bg-red-500 shadow-[0_0_6px_rgba(239,68,68,0.5)]" title="备用异常" />
                          )}
                        </TableCell>
                        <TableCell>{getStatusBadge(task)}</TableCell>
                        <TableCell>
                          <MiniUptimeBar history={hist} />
                        </TableCell>
                        <TableCell onClick={(e) => e.stopPropagation()}>
                          <Switch
                            checked={task.active}
                            disabled={!canManageMonitor}
                            onCheckedChange={() => handleToggle(task)}
                            className="data-[state=checked]:bg-green-500"
                          />
                        </TableCell>
                        <TableCell onClick={(e) => e.stopPropagation()}>
                          <DropdownMenu>
                            <DropdownMenuTrigger asChild>
                              <Button variant="ghost" size="icon" className="h-8 w-8">
                                <MoreHorizontal className="h-4 w-4" />
                              </Button>
                            </DropdownMenuTrigger>
                            <DropdownMenuContent align="end" className="w-48">
                              <DropdownMenuItem onClick={() => handleShowDetail(task)}>
                                <Eye className="h-4 w-4 mr-2" />
                                详情
                              </DropdownMenuItem>
                              {canManageMonitor && (
                                <>
                                  <DropdownMenuItem onClick={() => openEditDialog(task)}>
                                    <Pencil className="h-4 w-4 mr-2" />
                                    编辑任务
                                  </DropdownMenuItem>
                                  <DropdownMenuItem onClick={() => handleSwitch(task)}>
                                    <ArrowRightLeft className="h-4 w-4 mr-2" />
                                    手动切换
                                  </DropdownMenuItem>
                                  <DropdownMenuSeparator />
                                </>
                              )}
                              {canManageMonitor && (
                                <DropdownMenuItem
                                  onClick={() => openDeleteDialog(task)}
                                  className="text-destructive focus:text-destructive"
                                >
                                  <Trash2 className="h-4 w-4 mr-2" />
                                  删除任务
                                </DropdownMenuItem>
                              )}
                            </DropdownMenuContent>
                          </DropdownMenu>
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

      {/* ============== Detail Dialog ============== */}
      <Dialog open={showDetail} onOpenChange={setShowDetail}>
        <DialogContent className="max-w-3xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Shield className="h-5 w-5" />
              监控详情 - {detailTask?.rr}.{detailTask?.domain}
            </DialogTitle>
            <DialogDescription>
              {detailTask?.remark || '查看任务详细信息和监控数据'}
            </DialogDescription>
          </DialogHeader>

          {detailLoading ? (
            <div className="flex items-center justify-center py-12">
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
            </div>
          ) : (
            <Tabs defaultValue="overview" className="w-full">
              <TabsList className="grid w-full grid-cols-3">
                <TabsTrigger value="overview">概览</TabsTrigger>
                <TabsTrigger value="resolve">解析状态</TabsTrigger>
                <TabsTrigger value="events">事件日志</TabsTrigger>
              </TabsList>

              {/* Overview Tab */}
              <TabsContent value="overview" className="space-y-4 mt-4">
                {/* Health Status Cards */}
                {detailTask && (
                  <div className="grid grid-cols-2 gap-4">
                    <Card className={cn(
                      "border-2",
                      !detailTask.active ? "border-gray-200 dark:border-gray-700" :
                      detailTask.main_health ? "border-green-200 dark:border-green-800" : "border-red-200 dark:border-red-800"
                    )}>
                      <CardContent className="p-4">
                        <div className="flex items-center gap-3">
                          <div className={cn(
                            "w-4 h-4 rounded-full",
                            !detailTask.active ? "bg-gray-300" :
                            detailTask.main_health ? "bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.6)]" : "bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.6)] animate-pulse"
                          )} />
                          <div>
                            <p className="text-sm font-medium">主源状态</p>
                            <p className="text-xs text-muted-foreground">
                              {!detailTask.active ? '未启用' : detailTask.main_health ? '健康' : '异常'}
                            </p>
                          </div>
                        </div>
                        <code className="text-xs bg-muted px-1.5 py-0.5 rounded mt-2 block truncate">{detailTask.main_value}</code>
                      </CardContent>
                    </Card>
                    <Card className={cn(
                      "border-2",
                      (() => {
                        const bh = isAnyBackupHealthy(detailTask.backup_health)
                        if (bh === null) return "border-gray-200 dark:border-gray-700"
                        return bh ? "border-green-200 dark:border-green-800" : "border-red-200 dark:border-red-800"
                      })()
                    )}>
                      <CardContent className="p-4">
                        <div className="flex items-center gap-3">
                          {(() => {
                            const bh = isAnyBackupHealthy(detailTask.backup_health)
                            return (
                              <>
                                <div className={cn(
                                  "w-4 h-4 rounded-full",
                                  bh === null ? "bg-gray-300" :
                                  bh ? "bg-green-500 shadow-[0_0_8px_rgba(34,197,94,0.6)]" : "bg-red-500 shadow-[0_0_8px_rgba(239,68,68,0.6)]"
                                )} />
                                <div>
                                  <p className="text-sm font-medium">备源状态</p>
                                  <p className="text-xs text-muted-foreground">
                                    {bh === null ? '未配置' : bh ? '健康' : '异常'}
                                  </p>
                                </div>
                              </>
                            )
                          })()}
                        </div>
                        <code className="text-xs bg-muted px-1.5 py-0.5 rounded mt-2 block truncate">
                          {detailTask.backup_value || detailTask.backup_values || '未配置'}
                        </code>
                      </CardContent>
                    </Card>
                  </div>
                )}

                {/* Basic info */}
                <Card>
                  <CardContent className="p-4">
                    <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                      <div>
                        <p className="text-xs text-muted-foreground">域名</p>
                        <p className="font-medium text-sm">{detailTask?.rr}.{detailTask?.domain}</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">检测类型</p>
                        <p className="font-medium text-sm">{getCheckTypeName(detailTask?.check_type || 0)}</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">切换方式</p>
                        <p className="font-medium text-sm">{getSwitchTypeName(detailTask?.type || 0)}</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">当前状态</p>
                        {detailTask && getStatusBadge(detailTask)}
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">检测间隔</p>
                        <p className="font-medium text-sm">{detailTask?.frequency || 60}秒</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">失败阈值</p>
                        <p className="font-medium text-sm">{detailTask?.cycle || 3}次 / 连续失败 {detailTask?.err_count || 0}次</p>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">自动恢复</p>
                        <Badge variant={detailTask?.auto_restore ? "default" : "secondary"} className="text-xs">
                          {detailTask?.auto_restore ? '已开启' : '已关闭'}
                        </Badge>
                      </div>
                      <div>
                        <p className="text-xs text-muted-foreground">通知</p>
                        <Badge variant={detailTask?.notify_enabled ? "default" : "secondary"} className="text-xs">
                          {detailTask?.notify_enabled ? '已开启' : '已关闭'}
                        </Badge>
                      </div>
                    </div>

                    {/* Fault/Recovery info */}
                    {(detailTask?.fault_time || detailTask?.recover_time || detailTask?.last_error) && (
                      <>
                        <Separator className="my-3" />
                        <div className="grid grid-cols-2 md:grid-cols-3 gap-4">
                          {detailTask?.fault_time && (
                            <div>
                              <p className="text-xs text-muted-foreground">故障时间</p>
                              <p className="font-medium text-sm text-red-600">{formatDate(detailTask.fault_time)}</p>
                            </div>
                          )}
                          {detailTask?.recover_time && (
                            <div>
                              <p className="text-xs text-muted-foreground">恢复时间</p>
                              <p className="font-medium text-sm text-green-600">{formatDate(detailTask.recover_time)}</p>
                            </div>
                          )}
                          {detailTask?.last_error && (
                            <div className="col-span-2 md:col-span-1">
                              <p className="text-xs text-muted-foreground">最后错误</p>
                              <p className="font-medium text-sm text-red-500 break-all">{detailTask.last_error}</p>
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </CardContent>
                </Card>

                {/* Uptime Percentages */}
                {uptimeData && (
                  <div className="grid grid-cols-3 gap-4">
                    {(['24h', '7d', '30d'] as const).map(period => {
                      const uptime = uptimeData[period]?.uptime || 0
                      return (
                        <Card key={period}>
                          <CardContent className="p-4 text-center">
                            <p className="text-sm text-muted-foreground mb-1">{period} 可用率</p>
                            <p className={cn(
                              "text-2xl font-bold",
                              uptime >= 99 ? 'text-green-600' : uptime >= 95 ? 'text-yellow-600' : 'text-red-600'
                            )}>
                              {uptime.toFixed(2)}%
                            </p>
                            <p className="text-xs text-muted-foreground mt-1">
                              平均 {uptimeData[period]?.avg_duration?.toFixed(0) || 0}ms
                            </p>
                          </CardContent>
                        </Card>
                      )
                    })}
                  </div>
                )}

                {/* Uptime Bar */}
                {historyData.length > 0 && (
                  <Card>
                    <CardContent className="p-4">
                      <p className="text-sm font-medium mb-3">24小时可用性记录</p>
                      <div className="flex gap-px h-8 bg-muted rounded overflow-hidden">
                        {historyData.slice(-90).map((point, i) => (
                          <TooltipProvider key={i}>
                            <Tooltip>
                              <TooltipTrigger asChild>
                                <div
                                  className={cn(
                                    "flex-1 min-w-[3px] transition-all hover:opacity-80",
                                    point.success ? 'bg-green-500' : 'bg-red-500'
                                  )}
                                />
                              </TooltipTrigger>
                              <TooltipContent side="top" className="text-xs">
                                <p>{point.success ? '正常' : '异常'} - {point.duration}ms</p>
                                <p>{new Date(point.created_at).toLocaleString()}</p>
                              </TooltipContent>
                            </Tooltip>
                          </TooltipProvider>
                        ))}
                      </div>
                      <div className="flex justify-between mt-1 text-xs text-muted-foreground">
                        <span>24小时前</span>
                        <span>现在</span>
                      </div>
                    </CardContent>
                  </Card>
                )}

                {!uptimeData && historyData.length === 0 && (
                  <div className="text-center py-8 text-muted-foreground">暂无监控数据</div>
                )}
              </TabsContent>

              {/* Resolve Status Tab */}
              <TabsContent value="resolve" className="mt-4">
                {resolveStatus.length > 0 ? (
                  <div className="border rounded-lg overflow-hidden">
                    <Table>
                      <TableHeader>
                        <TableRow className="bg-muted/50">
                          <TableHead>地址</TableHead>
                          <TableHead>角色</TableHead>
                          <TableHead>状态</TableHead>
                          <TableHead>延迟</TableHead>
                          <TableHead>错误</TableHead>
                          <TableHead>最后检查</TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {resolveStatus.map((rs, i) => (
                          <TableRow key={i}>
                            <TableCell>
                              <code className="text-sm bg-muted px-1.5 py-0.5 rounded">{rs.address || rs.value || '-'}</code>
                            </TableCell>
                            <TableCell>
                              <Badge variant={rs.role === 'main' ? 'default' : 'secondary'}>
                                {rs.role === 'main' ? '主' : '备'}
                              </Badge>
                            </TableCell>
                            <TableCell>
                              {rs.status === 'ok' || rs.success ? (
                                <Badge variant="outline" className="bg-green-50 text-green-700 border-green-200">正常</Badge>
                              ) : (
                                <Badge variant="destructive">异常</Badge>
                              )}
                            </TableCell>
                            <TableCell className="text-sm tabular-nums">{rs.latency || rs.duration || 0}ms</TableCell>
                            <TableCell className="text-sm text-red-600">{rs.error || '-'}</TableCell>
                            <TableCell className="text-sm text-muted-foreground">
                              {rs.last_check ? formatDate(rs.last_check) : '-'}
                            </TableCell>
                          </TableRow>
                        ))}
                      </TableBody>
                    </Table>
                  </div>
                ) : detailTask ? (
                  <div className="space-y-4">
                    {/* Show derived status from task data */}
                    <div className="border rounded-lg overflow-hidden">
                      <Table>
                        <TableHeader>
                          <TableRow className="bg-muted/50">
                            <TableHead>地址</TableHead>
                            <TableHead>角色</TableHead>
                            <TableHead>状态</TableHead>
                          </TableRow>
                        </TableHeader>
                        <TableBody>
                          {/* Main value */}
                          <TableRow>
                            <TableCell>
                              <code className="text-sm bg-muted px-1.5 py-0.5 rounded">{detailTask.main_value}</code>
                            </TableCell>
                            <TableCell><Badge>主</Badge></TableCell>
                            <TableCell>
                              {detailTask.main_health ? (
                                <Badge variant="outline" className="bg-green-50 text-green-700 border-green-200 dark:bg-green-950 dark:text-green-400">
                                  <span className="mr-1 inline-block w-2 h-2 rounded-full bg-green-500" />正常
                                </Badge>
                              ) : (
                                <Badge variant="destructive">
                                  <span className="mr-1 inline-block w-2 h-2 rounded-full bg-red-300" />异常
                                </Badge>
                              )}
                            </TableCell>
                          </TableRow>
                          {/* Backup values */}
                          {(() => {
                            const bMap = parseBackupHealth(detailTask.backup_health)
                            return Object.entries(bMap).map(([addr, healthy]) => (
                              <TableRow key={addr}>
                                <TableCell>
                                  <code className="text-sm bg-muted px-1.5 py-0.5 rounded">{addr}</code>
                                </TableCell>
                                <TableCell><Badge variant="secondary">备</Badge></TableCell>
                                <TableCell>
                                  {healthy ? (
                                    <Badge variant="outline" className="bg-green-50 text-green-700 border-green-200 dark:bg-green-950 dark:text-green-400">
                                      <span className="mr-1 inline-block w-2 h-2 rounded-full bg-green-500" />正常
                                    </Badge>
                                  ) : (
                                    <Badge variant="destructive">
                                      <span className="mr-1 inline-block w-2 h-2 rounded-full bg-red-300" />异常
                                    </Badge>
                                  )}
                                </TableCell>
                              </TableRow>
                            ))
                          })()}
                        </TableBody>
                      </Table>
                    </div>
                    {!detailTask.backup_health && !detailTask.backup_value && (
                      <div className="text-center py-4 text-muted-foreground text-sm">未配置备用源</div>
                    )}
                  </div>
                ) : (
                  <div className="text-center py-8 text-muted-foreground">暂无解析状态数据</div>
                )}
              </TabsContent>

              {/* Events Tab */}
              <TabsContent value="events" className="mt-4">
                {taskLogs.length > 0 ? (
                  <div className="space-y-3 max-h-[400px] overflow-y-auto">
                    {taskLogs.map((log) => (
                      <div
                        key={log.id}
                        className={cn(
                          "p-3 rounded-lg border flex items-start gap-3",
                          log.action === 1 ? "bg-red-50 border-red-200 dark:bg-red-950 dark:border-red-800" : "bg-green-50 border-green-200 dark:bg-green-950 dark:border-green-800"
                        )}
                      >
                        <div className={cn(
                          "p-1.5 rounded-full mt-0.5",
                          log.action === 1 ? "bg-red-100 dark:bg-red-900" : "bg-green-100 dark:bg-green-900"
                        )}>
                          {log.action === 1 ? (
                            <AlertTriangle className="h-3.5 w-3.5 text-red-600 dark:text-red-400" />
                          ) : (
                            <CheckCircle className="h-3.5 w-3.5 text-green-600 dark:text-green-400" />
                          )}
                        </div>
                        <div className="flex-1 min-w-0">
                          <div className="flex items-center justify-between">
                            <Badge variant={log.action === 1 ? "destructive" : "outline"} className={cn("text-xs", log.action === 2 && "bg-green-100 text-green-700 border-green-300 dark:bg-green-900 dark:text-green-400")}>
                              {getActionText(log.action)}
                            </Badge>
                            <span className="text-xs text-muted-foreground">
                              {formatDate(log.created_at)}
                            </span>
                          </div>
                          {log.err_msg && (
                            <p className="mt-1.5 text-sm text-muted-foreground">{log.err_msg}</p>
                          )}
                        </div>
                      </div>
                    ))}
                  </div>
                ) : (
                  <div className="text-center py-8 text-muted-foreground">暂无事件日志</div>
                )}
              </TabsContent>
            </Tabs>
          )}
        </DialogContent>
      </Dialog>

      {/* ============== Edit Dialog ============== */}
      <Dialog open={editDialogOpen} onOpenChange={setEditDialogOpen}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>编辑监控任务</DialogTitle>
            <DialogDescription>修改DNS容灾监控任务配置</DialogDescription>
          </DialogHeader>
          <div className="space-y-4">
            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>域名</Label>
                <Select value={editFormData.did} onValueChange={(v) => setEditFormData({ ...editFormData, did: v })}>
                  <SelectTrigger>
                    <SelectValue placeholder="请选择域名" />
                  </SelectTrigger>
                  <SelectContent>
                    {domains.map((domain) => (
                      <SelectItem key={domain.id} value={domain.id.toString()}>
                        {domain.name}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>主机记录</Label>
                <Input value={editFormData.rr} onChange={(e) => setEditFormData({ ...editFormData, rr: e.target.value })} placeholder="如 www 或 @" />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>记录ID</Label>
                <Input value={editFormData.record_id} onChange={(e) => setEditFormData({ ...editFormData, record_id: e.target.value })} placeholder="DNS记录ID" />
              </div>
              <div className="space-y-2">
                <Label>主IP/值</Label>
                <Input value={editFormData.main_value} onChange={(e) => setEditFormData({ ...editFormData, main_value: e.target.value })} placeholder="主要记录值" />
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>切换方式</Label>
                <Select value={editFormData.type.toString()} onValueChange={(v) => setEditFormData({ ...editFormData, type: parseInt(v) })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {MONITOR_SWITCH_TYPES.map((t) => (
                      <SelectItem key={t.value} value={t.value.toString()}>{t.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {editFormData.type === 2 && (
                <div className="space-y-2">
                  <Label>备用IP/值</Label>
                  <Input value={editFormData.backup_value} onChange={(e) => setEditFormData({ ...editFormData, backup_value: e.target.value })} placeholder="备用记录值" />
                </div>
              )}
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
              <div className="space-y-2">
                <Label>检测类型</Label>
                <Select value={editFormData.check_type.toString()} onValueChange={(v) => setEditFormData({ ...editFormData, check_type: parseInt(v) })}>
                  <SelectTrigger><SelectValue /></SelectTrigger>
                  <SelectContent>
                    {MONITOR_CHECK_TYPES.map((t) => (
                      <SelectItem key={t.value} value={t.value.toString()}>{t.label}</SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              {editFormData.check_type === 1 && (
                <div className="space-y-2">
                  <Label>TCP端口</Label>
                  <Input type="number" value={editFormData.tcp_port} onChange={(e) => setEditFormData({ ...editFormData, tcp_port: parseInt(e.target.value) || 80 })} />
                </div>
              )}
              {(editFormData.check_type === 2 || editFormData.check_type === 3) && (
                <div className="space-y-2">
                  <Label>检测URL</Label>
                  <Input value={editFormData.check_url} onChange={(e) => setEditFormData({ ...editFormData, check_url: e.target.value })} placeholder="http(s)://example.com/health" />
                </div>
              )}
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
              <div className="space-y-2">
                <Label>检测间隔(秒)</Label>
                <Input type="number" value={editFormData.frequency} onChange={(e) => setEditFormData({ ...editFormData, frequency: parseInt(e.target.value) || 60 })} min={10} />
              </div>
              <div className="space-y-2">
                <Label>失败次数</Label>
                <Input type="number" value={editFormData.cycle} onChange={(e) => setEditFormData({ ...editFormData, cycle: parseInt(e.target.value) || 3 })} min={1} />
              </div>
              <div className="space-y-2">
                <Label>超时时间(秒)</Label>
                <Input type="number" value={editFormData.timeout} onChange={(e) => setEditFormData({ ...editFormData, timeout: parseInt(e.target.value) || 5 })} min={1} />
              </div>
            </div>

            {(editFormData.check_type === 2 || editFormData.check_type === 3) && (
              <div className="space-y-4 rounded-lg border border-dashed p-3 sm:p-4 bg-muted/20">
                <p className="text-xs font-medium text-muted-foreground">HTTP(S) 检测高级项</p>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                  <div className="space-y-2">
                    <Label>期望状态码</Label>
                    <Input value={editFormData.expect_status} onChange={(e) => setEditFormData({ ...editFormData, expect_status: e.target.value })} placeholder="留空=接受 2xx–3xx；填 200,301" />
                  </div>
                  <div className="space-y-2 sm:col-span-2">
                    <Label>期望关键字</Label>
                    <Input value={editFormData.expect_keyword} onChange={(e) => setEditFormData({ ...editFormData, expect_keyword: e.target.value })} placeholder="响应正文前 8KB 内包含即通过（可选）" />
                  </div>
                  <div className="space-y-2">
                    <Label>最大重定向</Label>
                    <Input type="number" value={editFormData.max_redirects} onChange={(e) => setEditFormData({ ...editFormData, max_redirects: parseInt(e.target.value, 10) || 0 })} placeholder="0=默认 3；-1=不跳转" />
                  </div>
                </div>
                <div className="rounded-md border bg-card p-3 space-y-3">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <div>
                      <p className="text-sm font-medium">出站代理</p>
                      <p className="text-xs text-muted-foreground">填写下方主机与端口时走 HTTP / SOCKS5；仅开关闭合且未填主机则用环境变量 HTTP_PROXY</p>
                    </div>
                    <div className="flex items-center gap-2">
                      <span className="text-xs text-muted-foreground">环境代理</span>
                      <Switch checked={editFormData.use_proxy} onCheckedChange={(v) => setEditFormData({ ...editFormData, use_proxy: v })} />
                    </div>
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                    <div className="space-y-2">
                      <Label>类型</Label>
                      <Select value={editFormData.proxy_type || 'http'} onValueChange={(v) => setEditFormData({ ...editFormData, proxy_type: v })}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="http">HTTP</SelectItem>
                          <SelectItem value="socks5">SOCKS5</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    <div className="space-y-2">
                      <Label>端口</Label>
                      <Input type="number" placeholder="1080" value={editFormData.proxy_port || ''} onChange={(e) => setEditFormData({ ...editFormData, proxy_port: parseInt(e.target.value, 10) || 0 })} />
                    </div>
                    <div className="space-y-2 sm:col-span-2">
                      <Label>主机</Label>
                      <Input value={editFormData.proxy_host} onChange={(e) => setEditFormData({ ...editFormData, proxy_host: e.target.value })} placeholder="127.0.0.1" />
                    </div>
                    <div className="space-y-2">
                      <Label>用户名</Label>
                      <Input value={editFormData.proxy_username} onChange={(e) => setEditFormData({ ...editFormData, proxy_username: e.target.value })} autoComplete="off" />
                    </div>
                    <div className="space-y-2">
                      <Label>密码</Label>
                      <Input type="password" value={editFormData.proxy_password} onChange={(e) => setEditFormData({ ...editFormData, proxy_password: e.target.value })} autoComplete="new-password" />
                    </div>
                  </div>
                </div>
              </div>
            )}

            <Separator />
            <div className="flex items-center justify-between p-3 rounded-lg border">
              <div>
                <p className="text-sm font-medium">自动恢复</p>
                <p className="text-xs text-muted-foreground">故障恢复后自动切换回主值</p>
              </div>
              <Switch
                checked={editFormData.auto_restore}
                onCheckedChange={(v) => setEditFormData({ ...editFormData, auto_restore: v })}
              />
            </div>
            <div className="flex items-center justify-between p-3 rounded-lg border">
              <div>
                <p className="text-sm font-medium">通知</p>
                <p className="text-xs text-muted-foreground">故障/恢复时发送通知</p>
              </div>
              <Switch
                checked={editFormData.notify_enabled}
                onCheckedChange={(v) => setEditFormData({ ...editFormData, notify_enabled: v })}
              />
            </div>

            <div className="space-y-2">
              <Label>备注</Label>
              <Input value={editFormData.remark} onChange={(e) => setEditFormData({ ...editFormData, remark: e.target.value })} placeholder="可选" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setEditDialogOpen(false)}>取消</Button>
            <Button onClick={handleEditSubmit} disabled={!canManageMonitor || submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              确定
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ============== Smart Add Dialog ============== */}
      <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
        <DialogContent className="max-w-2xl max-h-[90vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <Plus className="h-5 w-5" />
              添加监控任务
            </DialogTitle>
            <DialogDescription>
              {addStep === 1 && '第 1 步：选择域名和输入子域名'}
              {addStep === 2 && '第 2 步：选择要监控的DNS记录'}
              {addStep === 3 && '第 3 步：配置监控参数'}
            </DialogDescription>
          </DialogHeader>

          {/* Step indicator */}
          <div className="flex items-center gap-2 px-1">
            {[1, 2, 3].map((s) => (
              <div key={s} className="flex items-center gap-2 flex-1">
                <div className={cn(
                  "w-7 h-7 rounded-full flex items-center justify-center text-xs font-medium transition-colors",
                  addStep >= s ? 'bg-primary text-primary-foreground' : 'bg-muted text-muted-foreground'
                )}>
                  {s}
                </div>
                <span className={cn("text-sm hidden sm:inline", addStep >= s ? 'text-foreground' : 'text-muted-foreground')}>
                  {s === 1 && '查询'}
                  {s === 2 && '选择'}
                  {s === 3 && '配置'}
                </span>
                {s < 3 && <div className={cn("flex-1 h-px", addStep > s ? 'bg-primary' : 'bg-muted')} />}
              </div>
            ))}
          </div>

          <Separator />

          {/* Step 1: Select domain + subdomain */}
          {addStep === 1 && (
            <div className="space-y-4">
              <div className="space-y-2">
                <Label>选择域名 <span className="text-destructive">*</span></Label>
                <Select value={addDomainId} onValueChange={setAddDomainId}>
                  <SelectTrigger>
                    <SelectValue placeholder="请选择域名" />
                  </SelectTrigger>
                  <SelectContent>
                    {domains.map((domain) => (
                      <SelectItem key={domain.id} value={domain.id.toString()}>
                        {domain.name}
                        {domain.account_name && <span className="text-muted-foreground ml-2">({domain.account_name})</span>}
                      </SelectItem>
                    ))}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-2">
                <Label>子域名/主机记录 <span className="text-destructive">*</span></Label>
                <Input
                  value={addSubDomain}
                  onChange={(e) => setAddSubDomain(e.target.value)}
                  placeholder="如 www 或 @ 或 mail"
                />
                <p className="text-xs text-muted-foreground">输入要监控的子域名，然后点击查询获取对应的DNS记录</p>
              </div>
              <Button onClick={handleLookup} disabled={lookupLoading || !addDomainId || !addSubDomain} className="w-full">
                {lookupLoading ? <Loader2 className="h-4 w-4 mr-2 animate-spin" /> : <Search className="h-4 w-4 mr-2" />}
                查询DNS记录
              </Button>
            </div>
          )}

          {/* Step 2: Select records */}
          {addStep === 2 && lookupResult && (
            <div className="space-y-4">
              <div className="flex items-center gap-2 p-3 rounded-lg bg-muted">
                <Globe className="h-4 w-4 text-muted-foreground" />
                <span className="text-sm font-medium">{addSubDomain}.{lookupResult.domain}</span>
                <Badge variant="outline" className="ml-auto">{lookupResult.account_type}</Badge>
              </div>

              {lookupResult.records.length === 0 ? (
                <div className="text-center py-8 text-muted-foreground">
                  未找到匹配的DNS记录
                </div>
              ) : (
                <div className="border rounded-lg overflow-hidden">
                  <Table>
                    <TableHeader>
                      <TableRow className="bg-muted/50">
                        <TableHead className="w-12">
                          <Checkbox
                            checked={selectedRecords.length === lookupResult.records.length}
                            onCheckedChange={(checked) => {
                              if (checked) {
                                setSelectedRecords([...lookupResult.records])
                              } else {
                                setSelectedRecords([])
                              }
                            }}
                          />
                        </TableHead>
                        <TableHead>类型</TableHead>
                        <TableHead>记录ID</TableHead>
                        <TableHead>值</TableHead>
                        <TableHead>线路</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {lookupResult.records.map((record, idx) => (
                        <TableRow key={idx}>
                          <TableCell>
                            <Checkbox
                              checked={selectedRecords.some((r) => getRecordId(r) === getRecordId(record))}
                              onCheckedChange={(checked) => {
                                if (checked) {
                                  setSelectedRecords([...selectedRecords, record])
                                } else {
                                  setSelectedRecords(
                                    selectedRecords.filter((r) => getRecordId(r) !== getRecordId(record)),
                                  )
                                }
                              }}
                            />
                          </TableCell>
                          <TableCell>
                            <Badge variant="outline">{getRecordType(record)}</Badge>
                          </TableCell>
                          <TableCell>
                            <code className="text-xs bg-muted px-1 py-0.5 rounded">{getRecordId(record)}</code>
                          </TableCell>
                          <TableCell>
                            <code className="text-sm bg-muted px-1.5 py-0.5 rounded max-w-[200px] truncate block">
                              {getRecordValue(record)}
                            </code>
                          </TableCell>
                          <TableCell className="text-sm text-muted-foreground">
                            {getRecordLine(record)}
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>
                </div>
              )}
            </div>
          )}

          {/* Step 3: Configuration */}
          {addStep === 3 && (
            <div className="space-y-4">
              <div className="grid grid-cols-1 sm:grid-cols-2 gap-4">
                <div className="space-y-2">
                  <Label>切换方式</Label>
                  <Select value={addConfig.type.toString()} onValueChange={(v) => setAddConfig({ ...addConfig, type: parseInt(v) })}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {MONITOR_SWITCH_TYPES.map((t) => (
                        <SelectItem key={t.value} value={t.value.toString()}>{t.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
                <div className="space-y-2">
                  <Label>检测类型</Label>
                  <Select value={addConfig.check_type.toString()} onValueChange={(v) => setAddConfig({ ...addConfig, check_type: parseInt(v) })}>
                    <SelectTrigger><SelectValue /></SelectTrigger>
                    <SelectContent>
                      {MONITOR_CHECK_TYPES.map((t) => (
                        <SelectItem key={t.value} value={t.value.toString()}>{t.label}</SelectItem>
                      ))}
                    </SelectContent>
                  </Select>
                </div>
              </div>

              {addConfig.type === 2 && (
                <div className="space-y-2">
                  <Label>备用值 (每行一个)</Label>
                  <Textarea
                    value={addConfig.backup_value}
                    onChange={(e) => setAddConfig({ ...addConfig, backup_value: e.target.value })}
                    placeholder="备用IP或CNAME值，每行一个"
                    rows={3}
                  />
                </div>
              )}

              {(addConfig.check_type === 2 || addConfig.check_type === 3) && (
                <div className="space-y-4 rounded-lg border p-3 bg-muted/15">
                  <div className="space-y-2">
                    <Label>自定义检测URL</Label>
                    <Input
                      value={addConfig.check_url}
                      onChange={(e) => setAddConfig({ ...addConfig, check_url: e.target.value })}
                      placeholder="http(s)://example.com/health (留空使用默认)"
                    />
                  </div>
                  <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                    <div className="space-y-2">
                      <Label>期望状态码</Label>
                      <Input
                        value={addConfig.expect_status}
                        onChange={(e) => setAddConfig({ ...addConfig, expect_status: e.target.value })}
                        placeholder="200,301 或留空"
                      />
                    </div>
                    <div className="space-y-2 sm:col-span-2">
                      <Label>期望关键字</Label>
                      <Input
                        value={addConfig.expect_keyword}
                        onChange={(e) => setAddConfig({ ...addConfig, expect_keyword: e.target.value })}
                        placeholder="正文前 8KB 内包含（可选）"
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>最大重定向</Label>
                      <Input
                        type="number"
                        value={addConfig.max_redirects}
                        onChange={(e) => setAddConfig({ ...addConfig, max_redirects: parseInt(e.target.value, 10) || 0 })}
                        placeholder="0=默认3；-1=不跳转"
                      />
                    </div>
                  </div>
                  <div className="rounded-md border bg-card p-3 space-y-3">
                    <div className="flex flex-wrap items-center justify-between gap-2">
                      <p className="text-sm font-medium">代理（HTTP / SOCKS5 含认证）</p>
                      <div className="flex items-center gap-2 text-xs text-muted-foreground">
                        环境 HTTP_PROXY
                        <Switch checked={addConfig.use_proxy} onCheckedChange={(v) => setAddConfig({ ...addConfig, use_proxy: v })} />
                      </div>
                    </div>
                    <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
                      <Select value={addConfig.proxy_type || 'http'} onValueChange={(v) => setAddConfig({ ...addConfig, proxy_type: v })}>
                        <SelectTrigger><SelectValue /></SelectTrigger>
                        <SelectContent>
                          <SelectItem value="http">HTTP</SelectItem>
                          <SelectItem value="socks5">SOCKS5</SelectItem>
                        </SelectContent>
                      </Select>
                      <Input type="number" placeholder="端口" value={addConfig.proxy_port || ''} onChange={(e) => setAddConfig({ ...addConfig, proxy_port: parseInt(e.target.value, 10) || 0 })} />
                      <Input className="sm:col-span-2" placeholder="代理主机" value={addConfig.proxy_host} onChange={(e) => setAddConfig({ ...addConfig, proxy_host: e.target.value })} />
                      <Input placeholder="用户名" value={addConfig.proxy_username} onChange={(e) => setAddConfig({ ...addConfig, proxy_username: e.target.value })} />
                      <Input type="password" placeholder="密码" value={addConfig.proxy_password} onChange={(e) => setAddConfig({ ...addConfig, proxy_password: e.target.value })} />
                    </div>
                  </div>
                </div>
              )}

              {addConfig.check_type === 1 && (
                <div className="space-y-2">
                  <Label>TCP端口</Label>
                  <Input
                    type="number"
                    value={addConfig.tcp_port}
                    onChange={(e) => setAddConfig({ ...addConfig, tcp_port: parseInt(e.target.value) || 80 })}
                    placeholder="80"
                  />
                </div>
              )}

              {/* Advanced settings */}
              <Separator />
              <div className="space-y-3">
                <p className="text-sm font-medium flex items-center gap-2">
                  <Settings2 className="h-4 w-4" />
                  高级设置
                </p>
                <div className="grid grid-cols-1 sm:grid-cols-3 gap-4">
                  <div className="space-y-2">
                    <Label>检测间隔(秒)</Label>
                    <Input type="number" value={addConfig.frequency} onChange={(e) => setAddConfig({ ...addConfig, frequency: parseInt(e.target.value) || 60 })} min={10} />
                  </div>
                  <div className="space-y-2">
                    <Label>失败次数</Label>
                    <Input type="number" value={addConfig.cycle} onChange={(e) => setAddConfig({ ...addConfig, cycle: parseInt(e.target.value) || 3 })} min={1} />
                  </div>
                  <div className="space-y-2">
                    <Label>超时(秒)</Label>
                    <Input type="number" value={addConfig.timeout} onChange={(e) => setAddConfig({ ...addConfig, timeout: parseInt(e.target.value) || 5 })} min={1} />
                  </div>
                </div>
                <div className="flex items-center justify-between p-3 rounded-lg border">
                  <div>
                    <p className="text-sm font-medium">自动恢复</p>
                    <p className="text-xs text-muted-foreground">故障恢复后自动切换回主值</p>
                  </div>
                  <Switch
                    checked={addConfig.auto_restore}
                    onCheckedChange={(v) => setAddConfig({ ...addConfig, auto_restore: v })}
                  />
                </div>
              </div>

              {/* Notification */}
              <Separator />
              <div className="space-y-3">
                <div className="flex items-center justify-between">
                  <p className="text-sm font-medium flex items-center gap-2">
                    <Bell className="h-4 w-4" />
                    通知设置
                  </p>
                  <Switch
                    checked={addConfig.notify_enabled}
                    onCheckedChange={(v) => setAddConfig({ ...addConfig, notify_enabled: v })}
                  />
                </div>
                {addConfig.notify_enabled && (
                  <div className="flex flex-wrap gap-3 p-3 rounded-lg border">
                    {['email', 'telegram', 'webhook', 'discord', 'bark', 'wechat'].map((ch) => (
                      <label key={ch} className="flex items-center gap-2 text-sm">
                        <Checkbox
                          checked={addConfig.notify_channels.includes(ch)}
                          onCheckedChange={(checked) => {
                            if (checked) {
                              setAddConfig({ ...addConfig, notify_channels: [...addConfig.notify_channels, ch] })
                            } else {
                              setAddConfig({ ...addConfig, notify_channels: addConfig.notify_channels.filter(c => c !== ch) })
                            }
                          }}
                        />
                        {ch === 'email' && '邮件'}
                        {ch === 'telegram' && 'Telegram'}
                        {ch === 'webhook' && 'Webhook'}
                        {ch === 'discord' && 'Discord'}
                        {ch === 'bark' && 'Bark'}
                        {ch === 'wechat' && '企业微信'}
                      </label>
                    ))}
                  </div>
                )}
              </div>
            </div>
          )}

          <DialogFooter className="flex justify-between sm:justify-between">
            <div>
              {addStep > 1 && (
                <Button variant="outline" onClick={() => setAddStep(addStep - 1)}>
                  <ChevronLeft className="h-4 w-4 mr-1" />
                  上一步
                </Button>
              )}
            </div>
            <div className="flex gap-2">
              <Button variant="outline" onClick={() => setAddDialogOpen(false)}>取消</Button>
              {addStep === 2 && (
                <Button
                  onClick={() => setAddStep(3)}
                  disabled={selectedRecords.length === 0}
                >
                  下一步
                  <ChevronRight className="h-4 w-4 ml-1" />
                </Button>
              )}
              {addStep === 3 && (
                <Button onClick={handleSmartCreate} disabled={submitting}>
                  {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  创建任务
                </Button>
              )}
            </div>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* ============== Delete Dialog ============== */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除监控任务 &ldquo;{selectedTask?.rr}.{selectedTask?.domain}&rdquo; 吗？此操作不可撤销。
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
