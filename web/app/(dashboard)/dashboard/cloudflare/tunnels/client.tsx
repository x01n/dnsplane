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
import { toast } from 'sonner'
import { Loader2, Plus, RefreshCw, Trash2, Copy, Server, Network, Globe } from 'lucide-react'
import { TableSkeleton } from '@/components/table-skeleton'
import { EmptyState } from '@/components/empty-state'
import { accountApi, cloudflareApi, CloudflareTunnel, CidrRoute, HostnameRoute, TunnelPublicHostname, Account } from '@/lib/api'

export default function CloudflareTunnelsPage() {
  const [accounts, setAccounts] = useState<Account[]>([])
  const [selectedAccountId, setSelectedAccountId] = useState<string | null>(null)
  const [tunnels, setTunnels] = useState<CloudflareTunnel[]>([])
  const [loading, setLoading] = useState(true)
  const [accountsLoading, setAccountsLoading] = useState(true)
  const [addDialogOpen, setAddDialogOpen] = useState(false)
  const [deleteDialogOpen, setDeleteDialogOpen] = useState(false)
  const [selectedTunnel, setSelectedTunnel] = useState<CloudflareTunnel | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [newTunnelName, setNewTunnelName] = useState('')
  const [tokenMap, setTokenMap] = useState<Record<string, string>>({})

  // CIDR Routes
  const [selectedTunnelForCidr, setSelectedTunnelForCidr] = useState<string | null>(null)
  const [cidrRoutes, setCidrRoutes] = useState<CidrRoute[]>([])
  const [cidrLoading, setCidrLoading] = useState(false)
  const [newCidr, setNewCidr] = useState('')
  const [newCidrComment, setNewCidrComment] = useState('')

  // Hostname Routes
  const [selectedTunnelForHostname, setSelectedTunnelForHostname] = useState<string | null>(null)
  const [hostnameRoutes, setHostnameRoutes] = useState<HostnameRoute[]>([])
  const [hostnameLoading, setHostnameLoading] = useState(false)
  const [newHostname, setNewHostname] = useState('')
  const [newHostnameComment, setNewHostnameComment] = useState('')

  // Public Hostnames
  const [selectedTunnelForPublic, setSelectedTunnelForPublic] = useState<string | null>(null)
  const [publicHostnames, setPublicHostnames] = useState<TunnelPublicHostname[]>([])
  const [publicLoading, setPublicLoading] = useState(false)
  const [publicHostname, setPublicHostname] = useState('')
  const [publicPath, setPublicPath] = useState('')
  const [publicService, setPublicService] = useState('')

  const fetchAccounts = useCallback(async () => {
    setAccountsLoading(true)
    try {
      const res = await accountApi.list({ page_size: 100 })
      if (res.code === 0 && res.data) {
        // 过滤出 Cloudflare 类型的账户
        const cfAccounts = (res.data.list || []).filter((acc: Account) => acc.type === 'cloudflare')
        setAccounts(cfAccounts)
        // 如果有且仅有一个账户，自动选中
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

  const fetchTunnels = useCallback(async () => {
    if (!selectedAccountId) return
    setLoading(true)
    try {
      const res = await cloudflareApi.getTunnels(selectedAccountId)
      if (res.code === 0 && res.data) {
        setTunnels(res.data)
      } else {
        toast.error(res.msg || '获取 Tunnel 列表失败')
        setTunnels([])
      }
    } catch {
      toast.error('获取 Tunnel 列表失败')
      setTunnels([])
    } finally {
      setLoading(false)
    }
  }, [selectedAccountId])

  useEffect(() => {
    fetchAccounts()
  }, [fetchAccounts])

  useEffect(() => {
    if (selectedAccountId) {
      fetchTunnels()
    }
  }, [selectedAccountId, fetchTunnels])

  const handleAddTunnel = async () => {
    if (!selectedAccountId || !newTunnelName) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.addTunnel(selectedAccountId, newTunnelName)
      if (res.code === 0) {
        toast.success('创建 Tunnel 成功')
        setAddDialogOpen(false)
        setNewTunnelName('')
        fetchTunnels()
      } else {
        toast.error(res.msg || '创建失败')
      }
    } catch {
      toast.error('创建 Tunnel 失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDeleteTunnel = async () => {
    if (!selectedAccountId || !selectedTunnel) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.deleteTunnel(selectedAccountId, selectedTunnel.id)
      if (res.code === 0) {
        toast.success('删除 Tunnel 成功')
        setDeleteDialogOpen(false)
        fetchTunnels()
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除 Tunnel 失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleGetToken = async (tunnel: CloudflareTunnel) => {
    if (!selectedAccountId) return
    try {
      const res = await cloudflareApi.getTunnelToken(selectedAccountId, tunnel.id)
      if (res.code === 0 && res.data) {
        setTokenMap((prev) => ({ ...prev, [tunnel.id]: res.data!.token }))
        toast.success('获取 Token 成功')
      } else {
        toast.error(res.msg || '获取 Token 失败')
      }
    } catch {
      toast.error('获取 Token 失败')
    }
  }

  const fetchCidrRoutes = async (tunnelId: string) => {
    if (!selectedAccountId) return
    setCidrLoading(true)
    try {
      const res = await cloudflareApi.getCidrRoutes(selectedAccountId, tunnelId)
      if (res.code === 0 && res.data) {
        setCidrRoutes(res.data)
      } else {
        toast.error(res.msg || '获取 CIDR 路由列表失败')
        setCidrRoutes([])
      }
    } catch {
      toast.error('获取 CIDR 路由列表失败')
      setCidrRoutes([])
    } finally {
      setCidrLoading(false)
    }
  }

  const handleAddCidrRoute = async () => {
    if (!selectedAccountId || !selectedTunnelForCidr || !newCidr) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.addCidrRoute(selectedAccountId, {
        tunnel_id: selectedTunnelForCidr,
        network: newCidr,
        comment: newCidrComment,
      })
      if (res.code === 0) {
        toast.success('添加 CIDR 路由成功')
        setNewCidr('')
        setNewCidrComment('')
        fetchCidrRoutes(selectedTunnelForCidr)
      } else {
        toast.error(res.msg || '添加失败')
      }
    } catch {
      toast.error('添加 CIDR 路由失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDeleteCidrRoute = async (routeId: string) => {
    if (!selectedAccountId || !selectedTunnelForCidr) return
    try {
      const res = await cloudflareApi.deleteCidrRoute(selectedAccountId, routeId, selectedTunnelForCidr)
      if (res.code === 0) {
        toast.success('删除 CIDR 路由成功')
        fetchCidrRoutes(selectedTunnelForCidr)
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除 CIDR 路由失败')
    }
  }

  const fetchHostnameRoutes = async (tunnelId: string) => {
    if (!selectedAccountId) return
    setHostnameLoading(true)
    try {
      const res = await cloudflareApi.getHostnameRoutes(selectedAccountId, tunnelId)
      if (res.code === 0 && res.data) {
        setHostnameRoutes(res.data)
      } else {
        toast.error(res.msg || '获取主机名路由列表失败')
        setHostnameRoutes([])
      }
    } catch {
      toast.error('获取主机名路由列表失败')
      setHostnameRoutes([])
    } finally {
      setHostnameLoading(false)
    }
  }

  const handleAddHostnameRoute = async () => {
    if (!selectedAccountId || !selectedTunnelForHostname || !newHostname) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.addHostnameRoute(selectedAccountId, {
        tunnel_id: selectedTunnelForHostname,
        hostname: newHostname,
        comment: newHostnameComment,
      })
      if (res.code === 0) {
        toast.success('添加主机名路由成功')
        setNewHostname('')
        setNewHostnameComment('')
        fetchHostnameRoutes(selectedTunnelForHostname)
      } else {
        toast.error(res.msg || '添加失败')
      }
    } catch {
      toast.error('添加主机名路由失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDeleteHostnameRoute = async (routeId: string) => {
    if (!selectedAccountId || !selectedTunnelForHostname) return
    try {
      const res = await cloudflareApi.deleteHostnameRoute(selectedAccountId, routeId, selectedTunnelForHostname)
      if (res.code === 0) {
        toast.success('删除主机名路由成功')
        fetchHostnameRoutes(selectedTunnelForHostname)
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除主机名路由失败')
    }
  }

  const fetchPublicHostnames = async (tunnelId: string) => {
    if (!selectedAccountId) return
    setPublicLoading(true)
    try {
      const res = await cloudflareApi.getTunnelPublicHostnames(selectedAccountId, tunnelId)
      if (res.code === 0 && res.data) {
        setPublicHostnames(res.data)
      } else {
        toast.error(res.msg || '获取 Public Hostnames 失败')
        setPublicHostnames([])
      }
    } catch {
      toast.error('获取 Public Hostnames 失败')
      setPublicHostnames([])
    } finally {
      setPublicLoading(false)
    }
  }

  const handleSavePublicHostname = async () => {
    if (!selectedAccountId || !selectedTunnelForPublic || !publicHostname || !publicService) return
    setSubmitting(true)
    try {
      const res = await cloudflareApi.saveTunnelPublicHostname(selectedAccountId, {
        tunnel_id: selectedTunnelForPublic,
        hostname: publicHostname,
        path: publicPath,
        service: publicService,
      })
      if (res.code === 0) {
        toast.success('配置 Public Hostname 成功')
        setPublicHostname('')
        setPublicPath('')
        setPublicService('')
        fetchPublicHostnames(selectedTunnelForPublic)
      } else {
        toast.error(res.msg || '配置失败')
      }
    } catch {
      toast.error('配置 Public Hostname 失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleDeletePublicHostname = async (row: TunnelPublicHostname) => {
    if (!selectedAccountId || !selectedTunnelForPublic) return
    try {
      const res = await cloudflareApi.deleteTunnelPublicHostname(selectedAccountId, {
        tunnel_id: selectedTunnelForPublic,
        hostname: row.hostname,
        path: row.path || '',
      })
      if (res.code === 0) {
        toast.success('删除 Public Hostname 成功')
        fetchPublicHostnames(selectedTunnelForPublic)
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除 Public Hostname 失败')
    }
  }

  const selectedAccount = accounts.find(acc => String(acc.id) === selectedAccountId)

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-bold">Cloudflare Tunnels</h1>
          <p className="text-muted-foreground mt-1">管理 Cloudflare Tunnel、CIDR 路由与主机名路由</p>
        </div>
      </div>

      {/* Account Selector */}
      <Card>
        <CardHeader>
          <CardTitle>选择账户</CardTitle>
        </CardHeader>
        <CardContent>
          {accountsLoading ? (
            <div className="flex items-center gap-2 text-muted-foreground">
              <Loader2 className="h-4 w-4 animate-spin" />
              <span>加载账户中...</span>
            </div>
          ) : accounts.length === 0 ? (
            <EmptyState icon={Server} title="暂无 Cloudflare 账户" description="请先添加 Cloudflare 类型的 DNS 账户" />
          ) : (
            <Select value={selectedAccountId || ''} onValueChange={setSelectedAccountId}>
              <SelectTrigger className="w-full max-w-md">
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
          )}
        </CardContent>
      </Card>

      {/* Tunnels List */}
      {selectedAccountId && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle>Tunnel 列表</CardTitle>
                {selectedAccount && (
                  <p className="text-sm text-muted-foreground mt-1">账户: {selectedAccount.name}</p>
                )}
              </div>
              <div className="flex gap-2">
                <Button size="sm" onClick={() => setAddDialogOpen(true)}>
                  <Plus className="h-4 w-4 mr-1" /> 创建 Tunnel
                </Button>
                <Button size="sm" variant="outline" onClick={fetchTunnels}>
                  <RefreshCw className="h-4 w-4 mr-1" /> 刷新
                </Button>
              </div>
            </div>
          </CardHeader>
          <CardContent>
            {loading ? (
              <TableSkeleton rows={3} columns={5} />
            ) : tunnels.length === 0 ? (
              <EmptyState icon={Server} title="暂无 Tunnel" description="点击上方创建 Tunnel 按钮创建新的 Tunnel" />
            ) : (
              <div className="overflow-x-auto">
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>名称</TableHead>
                      <TableHead>状态</TableHead>
                      <TableHead>连接</TableHead>
                      <TableHead>创建时间</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {tunnels.map((tunnel) => {
                      const isActive = tunnel.status === 'active' || (tunnel.connections && tunnel.connections.length > 0)
                      return (
                        <TableRow key={tunnel.id}>
                          <TableCell className="font-medium">{tunnel.name}</TableCell>
                          <TableCell>
                            <Badge variant={isActive ? 'default' : 'secondary'}>
                              {isActive ? '运行中' : '未连接'}
                            </Badge>
                          </TableCell>
                          <TableCell>
                            {tunnel.connections?.length || 0} 个连接
                            {tunnel.connections && tunnel.connections.length > 0 && (
                              <span className="text-muted-foreground ml-1">
                                ({tunnel.connections[0].colo_name})
                              </span>
                            )}
                          </TableCell>
                          <TableCell className="text-muted-foreground">
                            {new Date(tunnel.created_at).toLocaleString('zh-CN')}
                          </TableCell>
                          <TableCell className="text-right">
                            <div className="flex justify-end gap-1">
                              <Button size="sm" variant="outline" onClick={() => handleGetToken(tunnel)}>
                                <Copy className="h-3 w-3 mr-1" /> Token
                              </Button>
                              <Button size="sm" variant="outline" onClick={() => {
                                setSelectedTunnelForCidr(tunnel.id)
                                setSelectedTunnelForHostname(null)
                                setSelectedTunnelForPublic(null)
                                fetchCidrRoutes(tunnel.id)
                              }}>
                                <Network className="h-3 w-3 mr-1" /> CIDR
                              </Button>
                              <Button size="sm" variant="outline" onClick={() => {
                                setSelectedTunnelForHostname(tunnel.id)
                                setSelectedTunnelForCidr(null)
                                setSelectedTunnelForPublic(null)
                                fetchHostnameRoutes(tunnel.id)
                              }}>
                                <Globe className="h-3 w-3 mr-1" /> 路由
                              </Button>
                              <Button size="sm" variant="outline" onClick={() => {
                                setSelectedTunnelForPublic(tunnel.id)
                                setSelectedTunnelForCidr(null)
                                setSelectedTunnelForHostname(null)
                                fetchPublicHostnames(tunnel.id)
                              }}>
                                <Globe className="h-3 w-3 mr-1" /> 穿透
                              </Button>
                              <Button size="icon" variant="ghost" onClick={() => { setSelectedTunnel(tunnel); setDeleteDialogOpen(true) }}>
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
      )}

      {/* Token Display */}
      {Object.keys(tokenMap).length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle>Tunnel Token</CardTitle>
          </CardHeader>
          <CardContent>
            <div className="space-y-2">
              {Object.entries(tokenMap).map(([tunnelId, token]) => (
                <div key={tunnelId} className="flex items-center gap-2">
                  <code className="flex-1 bg-muted px-3 py-2 rounded text-sm break-all">
                    {token}
                  </code>
                  <Button
                    size="sm"
                    variant="outline"
                    onClick={() => {
                      navigator.clipboard.writeText(token)
                      toast.success('已复制 Token')
                    }}
                  >
                    <Copy className="h-3 w-3" />
                  </Button>
                </div>
              ))}
            </div>
          </CardContent>
        </Card>
      )}

      {/* CIDR Routes */}
      {selectedTunnelForCidr && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>CIDR 路由</CardTitle>
              <Button size="sm" variant="outline" onClick={() => setSelectedTunnelForCidr(null)}>
                关闭
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {/* Add form */}
              <div className="flex gap-2">
                <Input
                  placeholder="CIDR (例如 10.0.0.0/8)"
                  value={newCidr}
                  onChange={(e) => setNewCidr(e.target.value)}
                />
                <Input
                  placeholder="备注"
                  value={newCidrComment}
                  onChange={(e) => setNewCidrComment(e.target.value)}
                />
                <Button onClick={handleAddCidrRoute} disabled={submitting || !newCidr}>
                  {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  添加
                </Button>
              </div>

              {/* Table */}
              {cidrLoading ? (
                <TableSkeleton rows={3} columns={4} />
              ) : cidrRoutes.length === 0 ? (
                <EmptyState icon={Network} title="暂无 CIDR 路由" />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>网络</TableHead>
                      <TableHead>备注</TableHead>
                      <TableHead>创建时间</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {cidrRoutes.map((route) => (
                      <TableRow key={route.id}>
                        <TableCell className="font-mono">{route.network}</TableCell>
                        <TableCell>{route.comment || '-'}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {new Date(route.created_at).toLocaleString('zh-CN')}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button size="sm" variant="destructive" onClick={() => handleDeleteCidrRoute(route.id)}>
                            删除
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Hostname Routes */}
      {selectedTunnelForHostname && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <CardTitle>主机名路由</CardTitle>
              <Button size="sm" variant="outline" onClick={() => setSelectedTunnelForHostname(null)}>
                关闭
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              {/* Add form */}
              <div className="flex gap-2">
                <Input
                  placeholder="主机名 (例如 app.example.com)"
                  value={newHostname}
                  onChange={(e) => setNewHostname(e.target.value)}
                />
                <Input
                  placeholder="备注"
                  value={newHostnameComment}
                  onChange={(e) => setNewHostnameComment(e.target.value)}
                />
                <Button onClick={handleAddHostnameRoute} disabled={submitting || !newHostname}>
                  {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  添加
                </Button>
              </div>

              {/* Table */}
              {hostnameLoading ? (
                <TableSkeleton rows={3} columns={4} />
              ) : hostnameRoutes.length === 0 ? (
                <EmptyState icon={Globe} title="暂无主机名路由" />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>主机名</TableHead>
                      <TableHead>备注</TableHead>
                      <TableHead>创建时间</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {hostnameRoutes.map((route) => (
                      <TableRow key={route.id}>
                        <TableCell className="font-mono">{route.hostname}</TableCell>
                        <TableCell>{route.comment || '-'}</TableCell>
                        <TableCell className="text-muted-foreground">
                          {new Date(route.created_at).toLocaleString('zh-CN')}
                        </TableCell>
                        <TableCell className="text-right">
                          <Button size="sm" variant="destructive" onClick={() => handleDeleteHostnameRoute(route.id)}>
                            删除
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Public Hostnames */}
      {selectedTunnelForPublic && (
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div>
                <CardTitle>穿透配置</CardTitle>
                <p className="text-sm text-muted-foreground mt-1">配置 Tunnel ingress 规则，并自动同步同名 CNAME 记录</p>
              </div>
              <Button size="sm" variant="outline" onClick={() => setSelectedTunnelForPublic(null)}>
                关闭
              </Button>
            </div>
          </CardHeader>
          <CardContent>
            <div className="space-y-4">
              <div className="grid gap-2 md:grid-cols-[1fr_1fr_1.5fr_auto]">
                <Input
                  placeholder="主机名，例如 app.example.com"
                  value={publicHostname}
                  onChange={(e) => setPublicHostname(e.target.value)}
                />
                <Input
                  placeholder="路径，可选，例如 /api/*"
                  value={publicPath}
                  onChange={(e) => setPublicPath(e.target.value)}
                />
                <Input
                  placeholder="服务地址，例如 http://localhost:8080"
                  value={publicService}
                  onChange={(e) => setPublicService(e.target.value)}
                />
                <Button onClick={handleSavePublicHostname} disabled={submitting || !publicHostname || !publicService}>
                  {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                  保存
                </Button>
              </div>

              {publicLoading ? (
                <TableSkeleton rows={3} columns={5} />
              ) : publicHostnames.length === 0 ? (
                <EmptyState icon={Globe} title="暂无穿透配置" />
              ) : (
                <Table>
                  <TableHeader>
                    <TableRow>
                      <TableHead>主机名</TableHead>
                      <TableHead>路径</TableHead>
                      <TableHead>服务</TableHead>
                      <TableHead>匹配域名</TableHead>
                      <TableHead className="text-right">操作</TableHead>
                    </TableRow>
                  </TableHeader>
                  <TableBody>
                    {publicHostnames.map((row) => (
                      <TableRow key={`${row.hostname}-${row.path || ''}`}>
                        <TableCell className="font-mono">{row.hostname}</TableCell>
                        <TableCell className="font-mono">{row.path || '-'}</TableCell>
                        <TableCell className="font-mono">{row.service}</TableCell>
                        <TableCell>{row.zone_name || '-'}</TableCell>
                        <TableCell className="text-right">
                          <Button size="sm" variant="destructive" onClick={() => handleDeletePublicHostname(row)}>
                            删除
                          </Button>
                        </TableCell>
                      </TableRow>
                    ))}
                  </TableBody>
                </Table>
              )}
            </div>
          </CardContent>
        </Card>
      )}

      {/* Add Tunnel Dialog */}
      <Dialog open={addDialogOpen} onOpenChange={setAddDialogOpen}>
        <DialogContent className="max-w-lg">
          <DialogHeader>
            <DialogTitle>创建 Tunnel</DialogTitle>
            <DialogDescription>输入 Tunnel 名称</DialogDescription>
          </DialogHeader>
          <Input
            value={newTunnelName}
            onChange={(e) => setNewTunnelName(e.target.value)}
            placeholder="例如 my-tunnel"
          />
          <DialogFooter>
            <Button variant="outline" onClick={() => setAddDialogOpen(false)}>取消</Button>
            <Button onClick={handleAddTunnel} disabled={submitting || !newTunnelName}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              创建
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* Delete Tunnel Dialog */}
      <AlertDialog open={deleteDialogOpen} onOpenChange={setDeleteDialogOpen}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>确认删除</AlertDialogTitle>
            <AlertDialogDescription>
              确定要删除 Tunnel &ldquo;{selectedTunnel?.name}&rdquo; 吗？此操作不可撤销。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel>取消</AlertDialogCancel>
            <AlertDialogAction onClick={handleDeleteTunnel} className="bg-destructive text-destructive-foreground hover:bg-destructive/90" disabled={submitting}>
              {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
              删除
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>
    </div>
  )
}
