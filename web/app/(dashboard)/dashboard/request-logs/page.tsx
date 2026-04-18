"use client"

import { useState, useEffect, useCallback } from "react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Badge } from "@/components/ui/badge"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Dialog, DialogContent, DialogDescription, DialogHeader, DialogTitle } from "@/components/ui/dialog"
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from "@/components/ui/alert-dialog"
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from "@/components/ui/select"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { requestLogApi, RequestLog, RequestLogStats } from "@/lib/api"
import { Search, RefreshCw, Trash2, AlertCircle, CheckCircle, Clock, Database, FileText, Code } from "lucide-react"
import { TableSkeleton } from '@/components/table-skeleton'
import { EmptyState } from '@/components/empty-state'
import { toast } from "sonner"

export default function RequestLogsPage() {
  const [logs, setLogs] = useState<RequestLog[]>([])
  const [stats, setStats] = useState<RequestLogStats | null>(null)
  const [loading, setLoading] = useState(false)
  const [cleanDialogOpen, setCleanDialogOpen] = useState(false)
  const [total, setTotal] = useState(0)
  const [page, setPage] = useState(1)
  const [pageSize] = useState(20)
  const [keyword, setKeyword] = useState("")
  const [isError, setIsError] = useState<string>("")
  const [method, setMethod] = useState<string>("")
  const [selectedLog, setSelectedLog] = useState<RequestLog | null>(null)
  const [detailOpen, setDetailOpen] = useState(false)
  const [searchId, setSearchId] = useState("")

  const fetchLogs = useCallback(async () => {
    setLoading(true)
    try {
      const res = await requestLogApi.getLogs({
        page,
        page_size: pageSize,
        keyword,
        is_error: isError,
        method,
      })
      if (res.code === 0 && res.data) {
        setLogs(res.data.list || [])
        setTotal(res.data.total)
      }
    } catch {
      toast.error("获取日志失败")
    } finally {
      setLoading(false)
    }
  }, [page, pageSize, keyword, isError, method])

  const fetchStats = useCallback(async () => {
    try {
      const res = await requestLogApi.getStats()
      if (res.code === 0 && res.data) {
        setStats(res.data)
      }
    } catch {
      /* 忽略统计加载失败 */
    }
  }, [])

  useEffect(() => {
    fetchLogs()
    fetchStats()
  }, [fetchLogs, fetchStats])

  const handleSearch = () => {
    setPage(1)
    fetchLogs()
  }

  const handleSearchById = async () => {
    if (!searchId.trim()) {
      toast.error("请输入请求ID或错误ID")
      return
    }

    setLoading(true)
    try {
      let res
      if (searchId.startsWith("req_")) {
        res = await requestLogApi.getByRequestId(searchId)
      } else if (searchId.startsWith("err_")) {
        res = await requestLogApi.getByErrorId(searchId)
      } else {
        // 尝试两种方式
        res = await requestLogApi.getByRequestId(searchId)
        if (res.code !== 0) {
          res = await requestLogApi.getByErrorId(searchId)
        }
      }

      if (res.code === 0 && res.data) {
        setSelectedLog(res.data)
        setDetailOpen(true)
      } else {
        toast.error("未找到对应的请求记录")
      }
    } catch {
      toast.error("查询失败")
    } finally {
      setLoading(false)
    }
  }

  const handleCleanLogs = async () => {
    setCleanDialogOpen(false)
    try {
      const res = await requestLogApi.cleanLogs(30)
      if (res.code === 0) {
        toast.success(`已清理 ${res.data?.deleted || 0} 条日志`)
        fetchLogs()
        fetchStats()
      } else {
        toast.error(res.msg || "清理失败")
      }
    } catch {
      toast.error("清理失败")
    }
  }

  const formatDate = (dateStr: string) => {
    return new Date(dateStr).toLocaleString("zh-CN")
  }

  const getMethodBadge = (method: string) => {
    const colors: Record<string, string> = {
      GET: "bg-green-500",
      POST: "bg-blue-500",
      PUT: "bg-yellow-500",
      DELETE: "bg-red-500",
    }
    return <Badge className={colors[method] || "bg-gray-500"}>{method}</Badge>
  }

  const renderDetailDialog = () => {
    if (!selectedLog) return null

    let dbQueries: Array<{ sql: string; duration_ms: number; rows: number; error?: string }> = []
    if (selectedLog.db_queries) {
      try {
        dbQueries = JSON.parse(selectedLog.db_queries)
      } catch {
        /* 忽略解析失败 */
      }
    }

    let headers: Record<string, string> = {}
    if (selectedLog.headers) {
      try {
        headers = JSON.parse(selectedLog.headers)
      } catch {
        /* 忽略解析失败 */
      }
    }

    let extra: Record<string, unknown> = {}
    if (selectedLog.extra) {
      try {
        extra = JSON.parse(selectedLog.extra)
      } catch {
        /* 忽略解析失败 */
      }
    }

    return (
      <Dialog open={detailOpen} onOpenChange={setDetailOpen}>
        <DialogContent className="max-w-4xl max-h-[90vh] overflow-hidden flex flex-col">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              {selectedLog.is_error ? (
                <AlertCircle className="h-5 w-5 text-red-500" />
              ) : (
                <CheckCircle className="h-5 w-5 text-green-500" />
              )}
              请求详情
            </DialogTitle>
            <DialogDescription className="font-mono text-xs">
              Request ID: {selectedLog.request_id}
              {selectedLog.error_id && ` | Error ID: ${selectedLog.error_id}`}
            </DialogDescription>
          </DialogHeader>

          <Tabs defaultValue="basic" className="flex-1 overflow-hidden flex flex-col">
            <TabsList>
              <TabsTrigger value="basic">基本信息</TabsTrigger>
              <TabsTrigger value="request">请求数据</TabsTrigger>
              <TabsTrigger value="response">响应数据</TabsTrigger>
              <TabsTrigger value="db">数据库查询 ({dbQueries.length})</TabsTrigger>
              {selectedLog.error_stack && <TabsTrigger value="stack">错误堆栈</TabsTrigger>}
            </TabsList>

            <div className="flex-1 mt-4 overflow-auto max-h-[60vh]">
              <TabsContent value="basic" className="space-y-4 m-0">
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">请求方法</div>
                    <div>{getMethodBadge(selectedLog.method)}</div>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">状态码</div>
                    <Badge variant={selectedLog.status_code >= 400 ? "destructive" : "default"}>
                      {selectedLog.status_code}
                    </Badge>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">请求路径</div>
                    <code className="text-sm bg-muted px-2 py-1 rounded">{selectedLog.path}</code>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">查询参数</div>
                    <code className="text-sm bg-muted px-2 py-1 rounded">{selectedLog.query || "-"}</code>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">用户</div>
                    <div>{selectedLog.username || "-"} (ID: {selectedLog.user_id})</div>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">客户端IP</div>
                    <div>{selectedLog.ip}</div>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">请求耗时</div>
                    <div className="flex items-center gap-1">
                      <Clock className="h-4 w-4" />
                      {selectedLog.duration}ms
                    </div>
                  </div>
                  <div className="space-y-2">
                    <div className="text-sm text-muted-foreground">数据库耗时</div>
                    <div className="flex items-center gap-1">
                      <Database className="h-4 w-4" />
                      {selectedLog.db_query_time || 0}ms
                    </div>
                  </div>
                  <div className="space-y-2 col-span-2">
                    <div className="text-sm text-muted-foreground">请求时间</div>
                    <div>{formatDate(selectedLog.created_at)}</div>
                  </div>
                  <div className="space-y-2 col-span-2">
                    <div className="text-sm text-muted-foreground">User-Agent</div>
                    <div className="text-xs break-all">{selectedLog.user_agent}</div>
                  </div>
                  {selectedLog.error_msg && (
                    <div className="space-y-2 col-span-2">
                      <div className="text-sm text-muted-foreground">错误信息</div>
                      <div className="text-red-500">{selectedLog.error_msg}</div>
                    </div>
                  )}
                </div>
              </TabsContent>

              <TabsContent value="request" className="space-y-4 m-0">
                <div className="space-y-4">
                  <div>
                    <div className="text-sm text-muted-foreground mb-2">完整路径（含查询串）</div>
                    <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-24 whitespace-pre-wrap break-all">
                      {selectedLog.path}
                      {selectedLog.query ? `?${selectedLog.query}` : ''}
                    </pre>
                  </div>
                  <div>
                    <div className="text-sm text-muted-foreground mb-2">查询参数 (RawQuery)</div>
                    <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-32 whitespace-pre-wrap break-all">
                      {selectedLog.query && selectedLog.query.trim() !== ''
                        ? selectedLog.query
                        : '（无）'}
                    </pre>
                  </div>
                  <div>
                    <div className="text-sm text-muted-foreground mb-2">请求头</div>
                    <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-48">
                      {JSON.stringify(headers, null, 2)}
                    </pre>
                  </div>
                  <div>
                    <div className="text-sm text-muted-foreground mb-2">请求体（原始 Body；GET 通常为空；部分接口会发字面量 {}）</div>
                    <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-64">
                      {selectedLog.body === "[FILTERED]" ? (
                        <span className="text-muted-foreground">[敏感内容已过滤]</span>
                      ) : (
                        (() => {
                          try {
                            return JSON.stringify(JSON.parse(selectedLog.body || '""'), null, 2)
                          } catch {
                            return selectedLog.body || "-"
                          }
                        })()
                      )}
                    </pre>
                  </div>
                </div>
              </TabsContent>

              <TabsContent value="response" className="space-y-4 m-0">
                <div>
                  <div className="text-sm text-muted-foreground mb-2">响应内容</div>
                  <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-96">
                    {(() => {
                      try {
                        return JSON.stringify(JSON.parse(selectedLog.response || '""'), null, 2)
                      } catch {
                        return selectedLog.response || "-"
                      }
                    })()}
                  </pre>
                </div>
                {Object.keys(extra).length > 0 && (
                  <div>
                    <div className="text-sm text-muted-foreground mb-2">额外信息</div>
                    <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-48">
                      {JSON.stringify(extra, null, 2)}
                    </pre>
                  </div>
                )}
              </TabsContent>

              <TabsContent value="db" className="space-y-4 m-0">
                {dbQueries.length === 0 ? (
                  <div className="text-center text-muted-foreground py-8">无数据库查询记录</div>
                ) : (
                  <div className="space-y-3">
                    {dbQueries.map((query, index) => (
                      <div key={index} className="border rounded p-3 space-y-2">
                        <div className="flex items-center justify-between">
                          <Badge variant="outline">查询 #{index + 1}</Badge>
                          <div className="text-xs text-muted-foreground">
                            {query.duration_ms}ms | {query.rows} 行
                          </div>
                        </div>
                        <pre className="text-xs bg-muted p-2 rounded overflow-auto">{query.sql}</pre>
                        {query.error && (
                          <div className="text-xs text-red-500">错误: {query.error}</div>
                        )}
                      </div>
                    ))}
                  </div>
                )}
              </TabsContent>

              {selectedLog.error_stack && (
                <TabsContent value="stack" className="m-0">
                  <pre className="text-xs bg-muted p-4 rounded overflow-auto max-h-96 whitespace-pre-wrap">
                    {selectedLog.error_stack}
                  </pre>
                </TabsContent>
              )}
            </div>
          </Tabs>
        </DialogContent>
      </Dialog>
    )
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">请求日志</h1>
          <p className="text-muted-foreground">查看和分析API请求记录</p>
        </div>
        <Button variant="outline" onClick={() => setCleanDialogOpen(true)}>
          <Trash2 className="h-4 w-4 mr-2" />
          清理旧日志
        </Button>
      </div>

      {stats && (
        <div className="grid grid-cols-4 gap-4">
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">总请求数</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.total_count}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">错误请求数</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-red-500 dark:text-red-400">{stats.error_count}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">今日请求</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold">{stats.today_count}</div>
            </CardContent>
          </Card>
          <Card>
            <CardHeader className="pb-2">
              <CardTitle className="text-sm font-medium">今日错误</CardTitle>
            </CardHeader>
            <CardContent>
              <div className="text-2xl font-bold text-red-500 dark:text-red-400">{stats.today_error_count}</div>
            </CardContent>
          </Card>
        </div>
      )}

      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <FileText className="h-5 w-5" />
            日志查询
          </CardTitle>
          <CardDescription>通过请求ID或错误ID快速定位问题</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex gap-4 mb-4">
            <div className="flex-1 flex gap-2">
              <Input
                placeholder="输入请求ID (req_xxx) 或错误ID (err_xxx)"
                value={searchId}
                onChange={(e) => setSearchId(e.target.value)}
                onKeyDown={(e) => e.key === "Enter" && handleSearchById()}
              />
              <Button onClick={handleSearchById}>
                <Code className="h-4 w-4 mr-2" />
                ID查询
              </Button>
            </div>
          </div>

          <div className="flex gap-4 mb-4">
            <Input
              placeholder="搜索路径、用户名、IP..."
              value={keyword}
              onChange={(e) => setKeyword(e.target.value)}
              className="max-w-sm"
            />
            <Select value={method || "all"} onValueChange={(v) => setMethod(v === "all" ? "" : v)}>
              <SelectTrigger className="w-28">
                <SelectValue placeholder="全部方法" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部方法</SelectItem>
                <SelectItem value="GET">GET</SelectItem>
                <SelectItem value="POST">POST</SelectItem>
                <SelectItem value="PUT">PUT</SelectItem>
                <SelectItem value="DELETE">DELETE</SelectItem>
              </SelectContent>
            </Select>
            <Select value={isError || "all"} onValueChange={(v) => setIsError(v === "all" ? "" : v)}>
              <SelectTrigger className="w-28">
                <SelectValue placeholder="全部状态" />
              </SelectTrigger>
              <SelectContent>
                <SelectItem value="all">全部状态</SelectItem>
                <SelectItem value="1">仅错误</SelectItem>
                <SelectItem value="0">仅成功</SelectItem>
              </SelectContent>
            </Select>
            <Button onClick={handleSearch}>
              <Search className="h-4 w-4 mr-2" />
              搜索
            </Button>
            <Button variant="outline" onClick={fetchLogs}>
              <RefreshCw className="h-4 w-4 mr-2" />
              刷新
            </Button>
          </div>

          {loading ? (
            <TableSkeleton rows={5} columns={8} />
          ) : logs.length === 0 ? (
            <EmptyState
              icon={FileText}
              title="暂无请求日志"
              description="还没有任何请求日志记录"
            />
          ) : (
          <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-24">方法</TableHead>
                <TableHead>路径</TableHead>
                <TableHead className="w-24">状态</TableHead>
                <TableHead className="w-24">耗时</TableHead>
                <TableHead className="w-32">用户</TableHead>
                <TableHead className="w-32">IP</TableHead>
                <TableHead className="w-40">时间</TableHead>
                <TableHead className="w-20">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {logs.map((log) => (
                  <TableRow
                    key={log.id}
                    className={log.is_error ? "bg-red-50 dark:bg-red-950/20" : ""}
                  >
                    <TableCell>{getMethodBadge(log.method)}</TableCell>
                    <TableCell className="font-mono text-sm max-w-xs truncate" title={log.path}>
                      {log.path}
                    </TableCell>
                    <TableCell>
                      <Badge variant={log.status_code >= 400 ? "destructive" : "secondary"}>
                        {log.status_code}
                      </Badge>
                    </TableCell>
                    <TableCell>{log.duration}ms</TableCell>
                    <TableCell>{log.username || "-"}</TableCell>
                    <TableCell className="font-mono text-xs">{log.ip}</TableCell>
                    <TableCell className="text-xs">{formatDate(log.created_at)}</TableCell>
                    <TableCell>
                      <Button
                        variant="ghost"
                        size="sm"
                        onClick={async () => {
                          /* 列表接口不返回 headers/body/db_queries，必须按 request_id 拉全量 */
                          try {
                            const res = await requestLogApi.getByRequestId(log.request_id)
                            if (res.code === 0 && res.data) {
                              setSelectedLog(res.data)
                              setDetailOpen(true)
                            } else {
                              toast.error(res.msg || "加载详情失败")
                            }
                          } catch {
                            toast.error("加载详情失败")
                          }
                        }}
                      >
                        详情
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
            </TableBody>
          </Table>
          </div>
          )}

          {total > pageSize && (
            <div className="flex flex-wrap items-center justify-between gap-2 mt-4">
              <div className="text-sm text-muted-foreground">
                共 {total} 条记录
              </div>
              <div className="flex gap-2">
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page === 1}
                  onClick={() => setPage(page - 1)}
                >
                  上一页
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={page * pageSize >= total}
                  onClick={() => setPage(page + 1)}
                >
                  下一页
                </Button>
              </div>
            </div>
          )}
        </CardContent>
      </Card>

      {renderDetailDialog()}

      <AlertDialog open={cleanDialogOpen} onOpenChange={setCleanDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认清理日志</AlertDialogTitle>
            <AlertDialogDescription>确定要清理30天前的日志吗？此操作不可撤销。</AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleCleanLogs} className="bg-red-600 hover:bg-red-700">确定清理</AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
