'use client'

import { useState, useEffect, useCallback } from 'react'
import { usePathname, useRouter } from 'next/navigation'
import { X } from 'lucide-react'
import { cn } from '@/lib/utils'

type Tab = {
  path: string
  title: string
}

const STORAGE_KEY = 'dnsplane_page_tabs'
const MAX_TABS = 15

const PATH_TITLES: Record<string, string> = {
  '/dashboard': '仪表盘',
  '/dashboard/accounts': 'DNS账户',
  '/dashboard/domains': '域名管理',
  '/dashboard/cloudflare/hostnames': '自定义主机名',
  '/dashboard/cloudflare/tunnels': 'Tunnels',
  '/dashboard/monitor': '容灾监控',
  '/dashboard/schedule': '定时切换',
  '/dashboard/cert-accounts': '证书账户',
  '/dashboard/cert': '证书订单',
  '/dashboard/deploy-accounts': '部署账户',
  '/dashboard/deploy': '部署任务',
  '/dashboard/users': '用户管理',
  '/dashboard/logs': '操作日志',
  '/dashboard/request-logs': '请求日志',
  '/dashboard/settings': '系统设置',
  '/dashboard/profile': '个人中心',
}

function getTitle(path: string): string {
  if (PATH_TITLES[path]) return PATH_TITLES[path]
  for (const [prefix, title] of Object.entries(PATH_TITLES)) {
    if (path.startsWith(prefix + '/')) return title
  }
  return path.split('/').pop() || '页面'
}

function loadTabs(): Tab[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = localStorage.getItem(STORAGE_KEY)
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

function saveTabs(tabs: Tab[]) {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(tabs))
  } catch {
    // ignore
  }
}

export function PageTabs() {
  const pathname = usePathname()
  const router = useRouter()
  const [tabs, setTabs] = useState<Tab[]>([])

  useEffect(() => {
    setTabs(loadTabs())
  }, [])

  useEffect(() => {
    if (!pathname || !pathname.startsWith('/dashboard')) return
    setTabs(prev => {
      const existing = prev.find(t => t.path === pathname)
      if (existing) return prev
      const newTab: Tab = { path: pathname, title: getTitle(pathname) }
      const updated = [...prev, newTab].slice(-MAX_TABS)
      saveTabs(updated)
      return updated
    })
  }, [pathname])

  const closeTab = useCallback((e: React.MouseEvent, path: string) => {
    e.stopPropagation()
    setTabs(prev => {
      const updated = prev.filter(t => t.path !== path)
      saveTabs(updated)
      if (path === pathname && updated.length > 0) {
        router.push(updated[updated.length - 1].path)
      }
      return updated
    })
  }, [pathname, router])

  const navigateTo = useCallback((path: string) => {
    if (path !== pathname) {
      router.push(path)
    }
  }, [pathname, router])

  if (tabs.length === 0) return null

  return (
    <div className="flex items-center gap-0.5 overflow-x-auto border-b border-border bg-muted/30 px-2 py-1 scrollbar-none">
      {tabs.map(tab => (
        <button
          key={tab.path}
          onClick={() => navigateTo(tab.path)}
          className={cn(
            'group flex items-center gap-1.5 rounded-md px-3 py-1.5 text-xs font-medium whitespace-nowrap transition-colors',
            tab.path === pathname
              ? 'bg-background text-foreground shadow-sm'
              : 'text-muted-foreground hover:bg-background/60 hover:text-foreground'
          )}
        >
          <span>{tab.title}</span>
          <span
            onClick={(e) => closeTab(e, tab.path)}
            className={cn(
              'flex h-4 w-4 items-center justify-center rounded-sm transition-colors',
              'opacity-0 group-hover:opacity-100',
              tab.path === pathname && 'opacity-60',
              'hover:bg-destructive/20 hover:text-destructive'
            )}
          >
            <X className="h-3 w-3" />
          </span>
        </button>
      ))}
    </div>
  )
}
