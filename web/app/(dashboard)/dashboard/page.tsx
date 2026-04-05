'use client'

import { useState, useEffect, useCallback, useRef } from 'react'
import Link from 'next/link'
import {
  Globe,
  Activity,
  FileText,
  Upload,
  CheckCircle,
  AlertCircle,
  Clock,
  XCircle,
  TrendingUp,
  Server,
  Cpu,
  HardDrive,
  MemoryStick,
  Info,
  Zap,
  RefreshCw,
} from 'lucide-react'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Badge } from '@/components/ui/badge'
import { Progress } from '@/components/ui/progress'
import { Button } from '@/components/ui/button'
import { dashboardApi, systemApi, DashboardStats, SystemInfo } from '@/lib/api'
import { Skeleton } from '@/components/ui/skeleton'

/* 自动刷新间隔（毫秒） */
const AUTO_REFRESH_INTERVAL = 30_000

export default function DashboardPage() {
  const [stats, setStats] = useState<DashboardStats | null>(null)
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [sysInfo, setSysInfo] = useState<SystemInfo | null>(null)
  const [lastUpdate, setLastUpdate] = useState<Date | null>(null)
  const [autoRefresh, setAutoRefresh] = useState(true)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const fetchAll = useCallback(async (isManual = false) => {
    if (isManual) setRefreshing(true)
    try {
      const [statsRes, sysRes] = await Promise.all([
        dashboardApi.getStats(),
        systemApi.getSystemInfo(),
      ])
      if (statsRes.code === 0 && statsRes.data) setStats(statsRes.data)
      if (sysRes.code === 0 && sysRes.data) setSysInfo(sysRes.data)
      setLastUpdate(new Date())
    } catch {
      /* ignore */
    } finally {
      setLoading(false)
      setRefreshing(false)
    }
  }, [])

  /* 首次加载 + 定时自动刷新 */
  useEffect(() => {
    fetchAll()
  }, [fetchAll])

  useEffect(() => {
    const startTimer = () => {
      if (timerRef.current) clearInterval(timerRef.current)
      if (autoRefresh) {
        timerRef.current = setInterval(() => fetchAll(), AUTO_REFRESH_INTERVAL)
      }
    }
    const stopTimer = () => {
      if (timerRef.current) { clearInterval(timerRef.current); timerRef.current = null }
    }

    /* 页面不可见时暂停自动刷新，可见时恢复 */
    const handleVisibility = () => {
      if (document.hidden) { stopTimer() } else { startTimer() }
    }

    startTimer()
    document.addEventListener('visibilitychange', handleVisibility)
    return () => { stopTimer(); document.removeEventListener('visibilitychange', handleVisibility) }
  }, [autoRefresh, fetchAll])

  /* 格式化最后更新时间 */
  const formatLastUpdate = () => {
    if (!lastUpdate) return ''
    return lastUpdate.toLocaleTimeString('zh-CN', { hour: '2-digit', minute: '2-digit', second: '2-digit' })
  }

  const formatBytes = (bytes: number) => {
    if (!bytes || bytes === 0) return '0 B'
    const k = 1024
    const sizes = ['B', 'KB', 'MB', 'GB', 'TB']
    const i = Math.floor(Math.log(bytes) / Math.log(k))
    return parseFloat((bytes / Math.pow(k, i)).toFixed(2)) + ' ' + sizes[i]
  }

  const statCards = [
    {
      title: '域名总数',
      value: stats?.domains ?? 0,
      icon: Globe,
      href: '/dashboard/domains',
      color: 'text-blue-600 dark:text-blue-400',
      bgColor: 'bg-blue-50 dark:bg-blue-950',
    },
    {
      title: '监控任务',
      value: stats?.tasks ?? 0,
      icon: Activity,
      href: '/dashboard/monitor',
      color: 'text-green-600 dark:text-green-400',
      bgColor: 'bg-green-50 dark:bg-green-950',
    },
    {
      title: '证书订单',
      value: stats?.certs ?? 0,
      icon: FileText,
      href: '/dashboard/cert',
      color: 'text-purple-600 dark:text-purple-400',
      bgColor: 'bg-purple-50 dark:bg-purple-950',
    },
    {
      title: '部署任务',
      value: stats?.deploys ?? 0,
      icon: Upload,
      href: '/dashboard/deploy',
      color: 'text-orange-600 dark:text-orange-400',
      bgColor: 'bg-orange-50 dark:bg-orange-950',
    },
  ]

  if (loading) {
    return (
      <div className="space-y-6">
        <div>
          <h1 className="text-2xl font-bold">仪表盘</h1>
          <p className="text-muted-foreground">系统概览和统计信息</p>
        </div>
        <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
          {[1, 2, 3, 4].map((i) => (
            <Card key={i}>
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <Skeleton className="h-4 w-20" />
                <Skeleton className="h-8 w-8 rounded-full" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-8 w-16" />
              </CardContent>
            </Card>
          ))}
        </div>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">仪表盘</h1>
          <p className="text-muted-foreground">系统概览和统计信息</p>
        </div>
        <div className="flex items-center gap-3">
          {lastUpdate && (
            <span className="text-xs text-muted-foreground hidden sm:inline">
              更新于 {formatLastUpdate()}
            </span>
          )}
          <Button
            variant="outline"
            size="sm"
            onClick={() => setAutoRefresh(prev => !prev)}
            className={autoRefresh ? 'border-green-300 dark:border-green-700 text-green-700 dark:text-green-400' : ''}
          >
            <Activity className="h-3.5 w-3.5 mr-1.5" />
            {autoRefresh ? '自动刷新' : '已暂停'}
          </Button>
          <Button
            variant="outline"
            size="sm"
            onClick={() => fetchAll(true)}
            disabled={refreshing}
          >
            <RefreshCw className={`h-3.5 w-3.5 mr-1.5 ${refreshing ? 'animate-spin' : ''}`} />
            刷新
          </Button>
        </div>
      </div>

      {/* 主要统计卡片 */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {statCards.map((card) => (
          <Link key={card.title} href={card.href}>
            <Card className="hover:shadow-lg transition-all duration-300 cursor-pointer hover:scale-[1.02] group">
              <CardHeader className="flex flex-row items-center justify-between space-y-0 pb-2">
                <CardTitle className="text-sm font-medium text-muted-foreground group-hover:text-foreground transition-colors">
                  {card.title}
                </CardTitle>
                <div className={`p-2 rounded-full ${card.bgColor} group-hover:scale-110 transition-transform`}>
                  <card.icon className={`h-4 w-4 ${card.color}`} />
                </div>
              </CardHeader>
              <CardContent>
                <div className="text-3xl font-bold tabular-nums">{card.value}</div>
              </CardContent>
            </Card>
          </Link>
        ))}
      </div>

      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {/* 容灾监控状态 */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Activity className="h-5 w-5" />
              容灾监控
            </CardTitle>
            <CardDescription>监控任务运行情况</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">运行状态</span>
              {stats?.dmonitor_state === 1 ? (
                <Badge variant="outline" className="bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300 border-green-200 dark:border-green-800">
                  <CheckCircle className="h-3 w-3 mr-1" />
                  正常运行
                </Badge>
              ) : (
                <Badge variant="destructive">
                  <XCircle className="h-3 w-3 mr-1" />
                  未运行
                </Badge>
              )}
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">已启用任务</span>
              <span className="font-medium">{stats?.dmonitor_active ?? 0}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">正常任务</span>
              <Badge variant="outline" className="bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300 border-green-200 dark:border-green-800">
                {stats?.dmonitor_status_0 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">已切换任务</span>
              <Badge variant="destructive">{stats?.dmonitor_status_1 ?? 0}</Badge>
            </div>
          </CardContent>
        </Card>

        {/* 优选IP状态 */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Zap className="h-5 w-5" />
              优选IP
            </CardTitle>
            <CardDescription>优选IP任务情况</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">已启用任务</span>
              <span className="font-medium">{stats?.optimizeip_active ?? 0}</span>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">正在运行</span>
              <Badge variant="outline" className="bg-blue-50 dark:bg-blue-950 text-blue-700 dark:text-blue-300 border-blue-200 dark:border-blue-800">
                <Activity className="h-3 w-3 mr-1" />
                {stats?.optimizeip_status_1 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">执行失败</span>
              <Badge variant="destructive">
                <XCircle className="h-3 w-3 mr-1" />
                {stats?.optimizeip_status_2 ?? 0}
              </Badge>
            </div>
          </CardContent>
        </Card>

        {/* 证书状态 */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <FileText className="h-5 w-5" />
              证书状态
            </CardTitle>
            <CardDescription>SSL证书申请情况</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">已签发</span>
              <Badge variant="outline" className="bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300 border-green-200 dark:border-green-800">
                <CheckCircle className="h-3 w-3 mr-1" />
                {stats?.certorder_status_3 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">申请错误</span>
              <Badge variant="destructive">
                <AlertCircle className="h-3 w-3 mr-1" />
                {stats?.certorder_status_5 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">即将过期</span>
              <Badge variant="outline" className="bg-yellow-50 dark:bg-yellow-950 text-yellow-700 dark:text-yellow-300 border-yellow-200 dark:border-yellow-800">
                <Clock className="h-3 w-3 mr-1" />
                {stats?.certorder_status_6 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">已过期</span>
              <Badge variant="destructive">
                <XCircle className="h-3 w-3 mr-1" />
                {stats?.certorder_status_7 ?? 0}
              </Badge>
            </div>
          </CardContent>
        </Card>

        {/* 部署状态 */}
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Upload className="h-5 w-5" />
              部署状态
            </CardTitle>
            <CardDescription>证书部署任务情况</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">待部署</span>
              <Badge variant="secondary">
                <Clock className="h-3 w-3 mr-1" />
                {stats?.certdeploy_status_0 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">已部署</span>
              <Badge variant="outline" className="bg-green-50 dark:bg-green-950 text-green-700 dark:text-green-300 border-green-200 dark:border-green-800">
                <CheckCircle className="h-3 w-3 mr-1" />
                {stats?.certdeploy_status_1 ?? 0}
              </Badge>
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-muted-foreground">部署失败</span>
              <Badge variant="destructive">
                <XCircle className="h-3 w-3 mr-1" />
                {stats?.certdeploy_status_2 ?? 0}
              </Badge>
            </div>
          </CardContent>
        </Card>
      </div>

      {/* 快捷操作 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <TrendingUp className="h-5 w-5" />
            快捷操作
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-4 md:grid-cols-4">
            <Link
              href="/dashboard/accounts"
              className="flex items-center gap-3 p-4 rounded-lg border hover:bg-accent hover:border-blue-200 dark:hover:border-blue-800 transition-all duration-200 group"
            >
              <Server className="h-8 w-8 text-blue-600 dark:text-blue-400 group-hover:scale-110 transition-transform" />
              <div>
                <div className="font-medium group-hover:text-blue-600 dark:group-hover:text-blue-400 transition-colors">添加账户</div>
                <div className="text-sm text-muted-foreground">配置DNS服务商</div>
              </div>
            </Link>
            <Link
              href="/dashboard/domains"
              className="flex items-center gap-3 p-4 rounded-lg border hover:bg-accent hover:border-green-200 dark:hover:border-green-800 transition-all duration-200 group"
            >
              <Globe className="h-8 w-8 text-green-600 dark:text-green-400 group-hover:scale-110 transition-transform" />
              <div>
                <div className="font-medium group-hover:text-green-600 dark:group-hover:text-green-400 transition-colors">管理域名</div>
                <div className="text-sm text-muted-foreground">添加和管理域名</div>
              </div>
            </Link>
            <Link
              href="/dashboard/cert"
              className="flex items-center gap-3 p-4 rounded-lg border hover:bg-accent hover:border-purple-200 dark:hover:border-purple-800 transition-all duration-200 group"
            >
              <FileText className="h-8 w-8 text-purple-600 dark:text-purple-400 group-hover:scale-110 transition-transform" />
              <div>
                <div className="font-medium group-hover:text-purple-600 dark:group-hover:text-purple-400 transition-colors">申请证书</div>
                <div className="text-sm text-muted-foreground">申请SSL证书</div>
              </div>
            </Link>
            <Link
              href="/dashboard/monitor"
              className="flex items-center gap-3 p-4 rounded-lg border hover:bg-accent hover:border-orange-200 dark:hover:border-orange-800 transition-all duration-200 group"
            >
              <Activity className="h-8 w-8 text-orange-600 dark:text-orange-400 group-hover:scale-110 transition-transform" />
              <div>
                <div className="font-medium group-hover:text-orange-600 dark:group-hover:text-orange-400 transition-colors">容灾监控</div>
                <div className="text-sm text-muted-foreground">配置故障切换</div>
              </div>
            </Link>
          </div>
        </CardContent>
      </Card>

      {/* 系统信息 */}
      {sysInfo && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Info className="h-5 w-5" />
              系统信息
            </CardTitle>
            <CardDescription>运行环境与资源使用情况</CardDescription>
          </CardHeader>
          <CardContent>
            <div className="grid gap-6 md:grid-cols-2 lg:grid-cols-4">
              {/* Runtime */}
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <div className="p-1.5 rounded-md bg-blue-50 dark:bg-blue-950">
                    <Cpu className="h-4 w-4 text-blue-600 dark:text-blue-400" />
                  </div>
                  <span className="text-sm font-medium">运行环境</span>
                </div>
                <div className="space-y-2 text-sm">
                  {sysInfo.version && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">版本</span>
                      <Badge variant="secondary" className="text-xs h-5">{sysInfo.version}</Badge>
                    </div>
                  )}
                  {sysInfo.go_version && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Go 版本</span>
                      <span className="font-mono text-xs">{sysInfo.go_version}</span>
                    </div>
                  )}
                  {sysInfo.os && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">操作系统</span>
                      <span className="font-mono text-xs">{sysInfo.os}/{sysInfo.arch}</span>
                    </div>
                  )}
                  {sysInfo.num_cpu !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">CPU 核心</span>
                      <span className="font-mono text-xs">{sysInfo.num_cpu}</span>
                    </div>
                  )}
                  {sysInfo.goroutines !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">Goroutines</span>
                      <Badge variant="outline" className="text-xs h-5">{sysInfo.goroutines}</Badge>
                    </div>
                  )}
                </div>
              </div>

              {/* Memory */}
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <div className="p-1.5 rounded-md bg-green-50 dark:bg-green-950">
                    <MemoryStick className="h-4 w-4 text-green-600 dark:text-green-400" />
                  </div>
                  <span className="text-sm font-medium">内存使用</span>
                </div>
                <div className="space-y-2 text-sm">
                  {sysInfo.memory_alloc !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">已分配</span>
                      <span className="font-mono text-xs">{formatBytes(sysInfo.memory_alloc)}</span>
                    </div>
                  )}
                  {sysInfo.memory_sys !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">系统内存</span>
                      <span className="font-mono text-xs">{formatBytes(sysInfo.memory_sys)}</span>
                    </div>
                  )}
                  {sysInfo.memory_alloc && sysInfo.memory_sys && (
                    <Progress
                      value={Math.min((sysInfo.memory_alloc / sysInfo.memory_sys) * 100, 100)}
                      className="h-1.5 mt-1"
                    />
                  )}
                </div>
              </div>

              {/* Database */}
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <div className="p-1.5 rounded-md bg-purple-50 dark:bg-purple-950">
                    <HardDrive className="h-4 w-4 text-purple-600 dark:text-purple-400" />
                  </div>
                  <span className="text-sm font-medium">数据库</span>
                </div>
                <div className="space-y-2 text-sm">
                  <div className="flex justify-between">
                    <span className="text-muted-foreground">驱动</span>
                    <Badge variant="outline" className="text-xs h-5">SQLite</Badge>
                  </div>
                  {sysInfo.data_db_size !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">数据库</span>
                      <span className="font-mono text-xs">{formatBytes(sysInfo.data_db_size)}</span>
                    </div>
                  )}
                  {sysInfo.logs_db_size !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">日志库</span>
                      <span className="font-mono text-xs">{formatBytes(sysInfo.logs_db_size)}</span>
                    </div>
                  )}
                  {sysInfo.request_db_size !== undefined && (
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">请求日志</span>
                      <span className="font-mono text-xs">{formatBytes(sysInfo.request_db_size)}</span>
                    </div>
                  )}
                </div>
              </div>

              {/* DB Maintenance */}
              <div className="space-y-3">
                <div className="flex items-center gap-2">
                  <div className="p-1.5 rounded-md bg-orange-50 dark:bg-orange-950">
                    <Clock className="h-4 w-4 text-orange-600 dark:text-orange-400" />
                  </div>
                  <span className="text-sm font-medium">数据维护</span>
                </div>
                <div className="space-y-2 text-sm">
                  {sysInfo.db_maintenance && (
                    <>
                      {sysInfo.db_maintenance.last_vacuum && (
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">上次优化</span>
                          <span className="font-mono text-xs">{sysInfo.db_maintenance.last_vacuum}</span>
                        </div>
                      )}
                      {sysInfo.db_maintenance.next_vacuum && (
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">下次优化</span>
                          <span className="font-mono text-xs">{sysInfo.db_maintenance.next_vacuum}</span>
                        </div>
                      )}
                      {sysInfo.db_maintenance.main_db?.size_text && (
                        <div className="flex justify-between">
                          <span className="text-muted-foreground">主库连接</span>
                          <Badge variant="outline" className="text-xs h-5">{sysInfo.db_maintenance.main_db.size_text}</Badge>
                        </div>
                      )}
                    </>
                  )}
                  {!sysInfo.db_maintenance && (
                    <div className="text-muted-foreground text-xs">暂无维护记录</div>
                  )}
                </div>
              </div>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
