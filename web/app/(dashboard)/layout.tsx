'use client'

import { useState, useEffect } from 'react'
import { useRouter, usePathname } from 'next/navigation'
import Link from 'next/link'
import {
  Globe,
  LayoutDashboard,
  Server,
  FileText,
  Shield,
  Activity,
  Upload,
  Users,
  Settings,
  LogOut,
  Menu,
  X,
  ChevronDown,
  ShieldCheck,
  Rocket,
  ChevronRight,
  User as UserIcon,
} from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Avatar, AvatarFallback } from '@/components/ui/avatar'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn, hasModuleAccess } from '@/lib/utils'
import { api, authApi, User } from '@/lib/api'
import { toast } from 'sonner'
import { DashboardUserProvider } from '@/contexts/dashboard-user-context'

type NavItem = {
  name: string
  href?: string
  icon: React.ComponentType<{ className?: string }>
  adminOnly?: boolean
  /** 与「用户管理」中的功能权限 key 一致；未配置权限的老用户不携带该字段即放行 */
  module?: string
  children?: NavItem[]
}

const navigation: NavItem[] = [
  { name: '仪表盘', href: '/dashboard', icon: LayoutDashboard },
  { name: 'DNS账户', href: '/dashboard/accounts', icon: Server, module: 'domain' },
  { name: '域名管理', href: '/dashboard/domains', icon: Globe, module: 'domain' },
  { name: '容灾监控', href: '/dashboard/monitor', icon: Activity, module: 'monitor' },
  {
    name: 'SSL证书',
    icon: ShieldCheck,
    module: 'cert',
    children: [
      { name: '证书账户', href: '/dashboard/cert-accounts', icon: ShieldCheck, module: 'cert' },
      { name: '证书订单', href: '/dashboard/cert', icon: FileText, module: 'cert' },
    ],
  },
  {
    name: '自动部署',
    icon: Rocket,
    module: 'deploy',
    children: [
      { name: '部署账户', href: '/dashboard/deploy-accounts', icon: Upload, module: 'deploy' },
      { name: '部署任务', href: '/dashboard/deploy', icon: Rocket, module: 'deploy' },
    ],
  },
  { name: '用户管理', href: '/dashboard/users', icon: Users, adminOnly: true },
  { name: '操作日志', href: '/dashboard/logs', icon: Shield, adminOnly: true },
  { name: '请求日志', href: '/dashboard/request-logs', icon: FileText, adminOnly: true },
  { name: '系统设置', href: '/dashboard/settings', icon: Settings, adminOnly: true },
]

export default function DashboardLayout({
  children,
}: {
  children: React.ReactNode
}) {
  const router = useRouter()
  const pathname = usePathname()
  const [sidebarOpen, setSidebarOpen] = useState(false)
  const [user, setUser] = useState<User | null>(null)
  const [expandedGroups, setExpandedGroups] = useState<string[]>([])

  // Dynamic expand based on current route
  useEffect(() => {
    const activeGroups: string[] = []
    navigation.forEach(item => {
      if (item.children) {
        const isActive = item.children.some(child => child.href && pathname.startsWith(child.href))
        if (isActive) {
          activeGroups.push(item.name)
        }
      }
    })
    setExpandedGroups(activeGroups)
  }, [pathname])

  useEffect(() => {
    /* 兼容旧版 OAuth 重定向：?token=&refresh_token= 写入本地后立即从地址栏移除，避免泄露到 Referer */
    if (typeof window !== 'undefined') {
      const u = new URL(window.location.href)
      const t = u.searchParams.get('token')
      const r = u.searchParams.get('refresh_token')
      if (t && r) {
        api.setTokens({ token: t, refresh_token: r })
        u.searchParams.delete('token')
        u.searchParams.delete('refresh_token')
        const q = u.searchParams.toString()
        window.history.replaceState(null, '', u.pathname + (q ? '?' + q : '') + u.hash)
      }
    }

    let retryCount = 0
    const maxRetries = 2

    const fetchUserInfo = async () => {
      try {
        const res = await authApi.getUserInfo()
        if (res.code === 0 && res.data) {
          setUser(res.data)
        } else if (res.code === -1 && res.msg?.includes('未登录')) {
          // 明确的未授权响应
          api.setToken(null)
          router.replace('/login')
        } else {
          // 其他错误（如签名验证暂时失败），重试
          if (retryCount < maxRetries) {
            retryCount++
            setTimeout(fetchUserInfo, 500 * retryCount)
          } else {
            // 重试用尽，跳转登录
            router.replace('/login')
          }
        }
      } catch (err) {
        // 区分「未登录」异常和其他异常
        if (err instanceof Error && err.message.includes('未登录')) {
          api.setToken(null)
          router.replace('/login')
        } else if (retryCount < maxRetries) {
          // 网络/解密等临时错误，重试
          retryCount++
          setTimeout(fetchUserInfo, 500 * retryCount)
        } else {
          router.replace('/login')
        }
      }
    }

    const token = api.getToken()
    if (!token) {
      router.replace('/login')
      return
    }
    fetchUserInfo()
  }, [router])

  const handleLogout = async () => {
    try {
      await authApi.logout()
    } catch {
      // ignore
    }
    api.setToken(null)
    toast.success('已退出登录')
    router.replace('/login')
  }

  const isActive = (href: string) => {
    // 精确匹配，避免 /dashboard/cert 匹配 /dashboard/cert-accounts
    if (href === '/dashboard') {
      return pathname === '/dashboard'
    }
    // 对于子路由，需要精确匹配，避免前缀匹配
    if (pathname === href) {
      return true
    }
    // 检查是否是子路径（如 /dashboard/cert/123 匹配 /dashboard/cert）
    return pathname.startsWith(href + '/')
  }

  const toggleGroup = (name: string) => {
    setExpandedGroups((prev) =>
      prev.includes(name) ? prev.filter((g) => g !== name) : [...prev, name]
    )
  }

  const isGroupActive = (item: NavItem) => {
    return item.children?.some((child) => child.href && isActive(child.href))
  }

  const filteredNavigation = navigation
    .filter((item) => {
      if (item.adminOnly && (!user || user.level < 2)) return false
      if (!hasModuleAccess(user, item.module)) return false
      return true
    })
    .map((item) => {
      if (item.children) {
        const children = item.children.filter((child) => {
          if (child.adminOnly && (!user || user.level < 2)) return false
          if (!hasModuleAccess(user, child.module ?? item.module)) return false
          return true
        })
        if (children.length === 0) return null
        return { ...item, children }
      }
      return item
    })
    .filter((item): item is NavItem => item !== null)

  if (!user) {
    return (
      <div className="min-h-screen flex items-center justify-center bg-background">
        <div className="animate-spin rounded-full h-8 w-8 border-2 border-primary border-t-transparent" />
      </div>
    )
  }

  return (
    <DashboardUserProvider user={user}>
    <div className="relative min-h-screen bg-background">
      {/* 背景装饰 */}
      <div
        className="pointer-events-none fixed inset-0 -z-10 overflow-hidden"
        aria-hidden
      >
        <div className="absolute -left-32 -top-32 h-96 w-96 rounded-full bg-primary/[0.06] blur-3xl" />
        <div className="absolute -bottom-48 -right-32 h-[28rem] w-[28rem] rounded-full bg-primary/[0.05] blur-3xl" />
      </div>

      {/* Mobile sidebar backdrop */}
      {sidebarOpen && (
        <div
          className="fixed inset-0 z-40 bg-black/40 backdrop-blur-sm lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      {/* Sidebar */}
      <aside
        className={cn(
          'fixed inset-y-0 left-0 z-50 flex w-64 flex-col border-r border-border bg-card/85 shadow-sm backdrop-blur-xl transition-transform duration-200 ease-in-out lg:translate-x-0',
          sidebarOpen ? 'translate-x-0' : '-translate-x-full'
        )}
      >
        <div className="flex h-16 shrink-0 items-center gap-2 border-b border-border px-6">
          <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary text-primary-foreground shadow-sm">
            <Globe className="h-5 w-5" />
          </div>
          <span className="text-lg font-semibold tracking-tight">DNSPlane</span>
          <Button
            variant="ghost"
            size="icon"
            className="ml-auto lg:hidden"
            onClick={() => setSidebarOpen(false)}
          >
            <X className="h-5 w-5" />
          </Button>
        </div>
        <nav className="flex-1 space-y-1 overflow-y-auto p-4">
          {filteredNavigation.map((item) => {
            if (item.children) {
              const expanded = expandedGroups.includes(item.name)
              const groupActive = isGroupActive(item)
              return (
                <div key={item.name}>
                  <button
                    onClick={() => toggleGroup(item.name)}
                    className={cn(
                      'flex w-full items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                      groupActive
                        ? 'text-primary'
                        : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                    )}
                  >
                    <item.icon className="h-5 w-5" />
                    {item.name}
                    <ChevronRight
                      className={cn(
                        'ml-auto h-4 w-4 transition-transform',
                        expanded && 'rotate-90'
                      )}
                    />
                  </button>
                  {expanded && (
                    <div className="ml-4 mt-1 space-y-1">
                      {item.children.map((child) => {
                        const active = child.href ? isActive(child.href) : false
                        return (
                          <Link
                            key={child.name}
                            href={child.href || '#'}
                            onClick={() => setSidebarOpen(false)}
                            className={cn(
                              'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                              active
                                ? 'bg-primary text-primary-foreground shadow-sm'
                                : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                            )}
                          >
                            <child.icon className="h-4 w-4" />
                            {child.name}
                          </Link>
                        )
                      })}
                    </div>
                  )}
                </div>
              )
            }

            const active = item.href ? isActive(item.href) : false
            return (
              <Link
                key={item.name}
                href={item.href || '#'}
                onClick={() => setSidebarOpen(false)}
                className={cn(
                  'flex items-center gap-3 rounded-lg px-3 py-2 text-sm font-medium transition-colors',
                  active
                    ? 'bg-primary text-primary-foreground shadow-sm'
                    : 'text-muted-foreground hover:bg-accent hover:text-accent-foreground'
                )}
              >
                <item.icon className="h-5 w-5" />
                {item.name}
              </Link>
            )
          })}
        </nav>
      </aside>

      {/* Main content */}
      <div className="lg:pl-64">
        {/* Top header */}
        <header className="sticky top-0 z-30 flex h-16 items-center gap-4 border-b border-border bg-background/80 px-4 backdrop-blur-md supports-[backdrop-filter]:bg-background/70 sm:px-6">
          <Button
            variant="ghost"
            size="icon"
            className="lg:hidden"
            onClick={() => setSidebarOpen(true)}
          >
            <Menu className="h-5 w-5" />
          </Button>

          <div className="flex-1" />

          <DropdownMenu>
            <DropdownMenuTrigger asChild>
              <Button variant="ghost" className="gap-2">
                <Avatar className="h-8 w-8">
                  <AvatarFallback className="bg-primary text-primary-foreground text-sm">
                    {user.username.slice(0, 2).toUpperCase()}
                  </AvatarFallback>
                </Avatar>
                <span className="hidden sm:inline-block">{user.username}</span>
                <ChevronDown className="h-4 w-4" />
              </Button>
            </DropdownMenuTrigger>
            <DropdownMenuContent align="end" className="w-48">
              <DropdownMenuItem disabled>
                <span className="text-muted-foreground">
                  {user.level === 2 ? '管理员' : '普通用户'}
                </span>
              </DropdownMenuItem>
              <DropdownMenuSeparator />
              <DropdownMenuItem asChild>
                <Link href="/dashboard/profile">
                  <UserIcon className="mr-2 h-4 w-4" />
                  个人中心
                </Link>
              </DropdownMenuItem>
              <DropdownMenuItem onClick={handleLogout}>
                <LogOut className="mr-2 h-4 w-4" />
                退出登录
              </DropdownMenuItem>
            </DropdownMenuContent>
          </DropdownMenu>
        </header>

        {/* Page content */}
        <main className="p-4 sm:p-6">{children}</main>
      </div>
    </div>
    </DashboardUserProvider>
  )
}
