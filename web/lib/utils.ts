import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

// 格式化日期时间
export function formatDate(date: string | Date | undefined | null, format: 'date' | 'datetime' | 'relative' = 'datetime'): string {
  if (!date) return '-'
  const d = typeof date === 'string' ? new Date(date) : date
  if (isNaN(d.getTime())) return '-'
  
  if (format === 'relative') {
    const now = new Date()
    const diff = now.getTime() - d.getTime()
    const seconds = Math.floor(diff / 1000)
    const minutes = Math.floor(seconds / 60)
    const hours = Math.floor(minutes / 60)
    const days = Math.floor(hours / 24)
    
    if (seconds < 60) return '刚刚'
    if (minutes < 60) return `${minutes}分钟前`
    if (hours < 24) return `${hours}小时前`
    if (days < 30) return `${days}天前`
    return formatDate(d, 'date')
  }
  
  const year = d.getFullYear()
  const month = String(d.getMonth() + 1).padStart(2, '0')
  const day = String(d.getDate()).padStart(2, '0')
  
  if (format === 'date') {
    return `${year}-${month}-${day}`
  }
  
  const hours = String(d.getHours()).padStart(2, '0')
  const minutes = String(d.getMinutes()).padStart(2, '0')
  const seconds = String(d.getSeconds()).padStart(2, '0')
  return `${year}-${month}-${day} ${hours}:${minutes}:${seconds}`
}

// 格式化时间戳
export function formatTimestamp(timestamp: number | undefined | null, format: 'date' | 'datetime' = 'datetime'): string {
  if (!timestamp) return '-'
  return formatDate(new Date(timestamp * 1000), format)
}

// 计算剩余天数
export function getDaysRemaining(expireTime: string | undefined | null): number | null {
  if (!expireTime) return null
  const expire = new Date(expireTime)
  const now = new Date()
  const diff = expire.getTime() - now.getTime()
  return Math.ceil(diff / (1000 * 60 * 60 * 24))
}

// DNS记录类型
export const DNS_RECORD_TYPES = ['A', 'AAAA', 'CNAME', 'MX', 'TXT', 'NS', 'SRV', 'CAA', 'PTR']

// 证书状态映射
export const CERT_STATUS_MAP: Record<number, { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' }> = {
  0: { label: '待申请', variant: 'secondary' },
  1: { label: '验证中', variant: 'default' },
  2: { label: '已验证', variant: 'default' },
  3: { label: '已签发', variant: 'outline' },
  4: { label: '已吊销', variant: 'destructive' },
  '-1': { label: '错误', variant: 'destructive' },
  '-2': { label: '错误', variant: 'destructive' },
  '-3': { label: '错误', variant: 'destructive' },
}

// 监控状态映射
export const MONITOR_STATUS_MAP: Record<number, { label: string; variant: 'default' | 'secondary' | 'destructive' | 'outline' }> = {
  0: { label: '正常', variant: 'outline' },
  1: { label: '已切换', variant: 'destructive' },
}

// 监控类型映射
export const MONITOR_CHECK_TYPES = [
  { value: 0, label: 'Ping' },
  { value: 1, label: 'TCP' },
  { value: 2, label: 'HTTP' },
  { value: 3, label: 'HTTPS' },
]

// 切换类型映射
export const MONITOR_SWITCH_TYPES = [
  { value: 0, label: '暂停/启用记录' },
  { value: 1, label: '删除记录' },
  { value: 2, label: '切换备用记录' },
]

// 复制到剪贴板
export async function copyToClipboard(text: string): Promise<boolean> {
  try {
    await navigator.clipboard.writeText(text)
    return true
  } catch {
    return false
  }
}

// 下载文件
export function downloadFile(content: string, filename: string, type = 'text/plain') {
  const blob = new Blob([content], { type })
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  document.body.removeChild(a)
  URL.revokeObjectURL(url)
}

/** 功能模块权限：与侧栏、用户管理 PERMISSIONS key 一致；permissions 未返回视为全开 */
export function hasModuleAccess(
  user: { level: number; permissions?: string[] | null } | null | undefined,
  module?: string
): boolean {
  if (!module) return true
  if (!user) return true
  if (user.level >= 2) return true
  const p = user.permissions
  if (p === undefined || p === null) return true
  return p.includes(module)
}
