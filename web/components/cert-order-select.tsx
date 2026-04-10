'use client'

import type { CertOrder } from '@/lib/api'
import {
  certOrderDomainsLine,
  certOrderKindShort,
  formatCertOrderExpiryLine,
} from '@/lib/cert-order-display'
import { cn } from '@/lib/utils'
import { Badge } from '@/components/ui/badge'
import { SelectItem } from '@/components/ui/select'

/** 部署/证书流程中：下拉项内展示域名、订单号、CA、密钥、验证方式、到期 */
export function CertOrderSelectItem({ order }: { order: CertOrder }) {
  const exp = formatCertOrderExpiryLine(order)
  const textValue = [
    order.id,
    ...(order.domains ?? []),
    order.type_name,
    order.key_type,
    order.key_size,
    certOrderKindShort(order),
    exp.text,
  ]
    .filter(Boolean)
    .join(' ')

  const keyLine = [order.key_type, order.key_size].filter(Boolean).join(' ') || '默认密钥'

  return (
    <SelectItem
      value={String(order.id)}
      textValue={textValue}
      className="cursor-pointer items-start py-2.5 pl-2 pr-8 [&>span:last-child]:w-full [&>span:last-child]:items-start"
    >
      <div className="flex min-w-0 w-full flex-col gap-1 text-left">
        <p className="font-medium text-foreground leading-snug break-all">
          {certOrderDomainsLine(order, 4)}
        </p>
        <div className="flex flex-wrap items-center gap-x-2 gap-y-1 text-xs text-muted-foreground">
          <span className="tabular-nums shrink-0">订单 #{order.id}</span>
          {order.type_name ? (
            <Badge variant="secondary" className="h-5 max-w-[140px] truncate px-1.5 text-[10px] font-normal">
              {order.type_name}
            </Badge>
          ) : null}
          <span className="shrink-0">{keyLine}</span>
          <Badge variant="outline" className="h-5 px-1.5 text-[10px] font-normal">
            {certOrderKindShort(order)}
          </Badge>
          <span
            className={cn(
              'min-w-0 break-all',
              exp.urgent && 'font-medium text-amber-700 dark:text-amber-400',
              exp.text === '已过期' && 'text-destructive',
            )}
          >
            {exp.text}
          </span>
        </div>
      </div>
    </SelectItem>
  )
}
