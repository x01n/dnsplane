import type { CertOrder } from '@/lib/api'
import { formatDate } from '@/lib/utils'

/** 与证书列表 orderKindLabel 语义一致，用于短标签 */
export function certOrderKindShort(order: CertOrder): string {
  const ch = (order.challenge_type || '').toLowerCase()
  switch (order.order_kind) {
    case 'ip':
      return 'IP'
    case 'mixed':
      return ch === 'http-01' ? '混合·HTTP' : '混合'
    case 'dns':
    default:
      return ch === 'http-01' ? 'HTTP-01' : 'DNS-01'
  }
}

export function getCertOrderDaysLeft(order: CertOrder): number | null {
  if (order.end_day !== undefined && order.end_day !== null) return order.end_day
  if (!order.expire_time) return null
  const t = new Date(order.expire_time).getTime()
  if (Number.isNaN(t)) return null
  return Math.ceil((t - Date.now()) / 86400000)
}

export function formatCertOrderExpiryLine(order: CertOrder): { text: string; urgent: boolean } {
  const days = getCertOrderDaysLeft(order)
  if (days === null) {
    return { text: '到期未记录', urgent: false }
  }
  if (days <= 0) {
    return { text: '已过期', urgent: true }
  }
  if (order.expire_time) {
    const d = formatDate(order.expire_time, 'date')
    return { text: `至 ${d} · 余 ${days} 天`, urgent: days <= 30 }
  }
  return { text: `余 ${days} 天`, urgent: days <= 30 }
}

/** 域名展示：前 maxShow 个 + 总数 */
export function certOrderDomainsLine(order: CertOrder, maxShow = 3): string {
  const d = order.domains?.filter(Boolean) ?? []
  if (d.length === 0) return '（无域名列表，订单 #' + order.id + '）'
  if (d.length <= maxShow) return d.join('、')
  return `${d.slice(0, maxShow).join('、')} 等 ${d.length} 个`
}

/** 已签发订单：先按剩余天数升序（快过期的在前），无到期信息的靠后，同组内按 id 降序 */
export function compareIssuedCertOrders(a: CertOrder, b: CertOrder): number {
  const da = getCertOrderDaysLeft(a)
  const db = getCertOrderDaysLeft(b)
  const score = (x: number | null) => (x === null ? 1_000_000 : Math.max(x, -99999))
  const sa = score(da)
  const sb = score(db)
  if (sa !== sb) return sa - sb
  return b.id - a.id
}
