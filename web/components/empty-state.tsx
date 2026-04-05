'use client'

import { type LucideIcon, Inbox } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { cn } from '@/lib/utils'

/*
 * EmptyState 统一空状态组件
 * 功能：各列表页数据为空时的友好提示与引导操作
 * 参数：
 *   icon - 自定义图标（默认 Inbox）
 *   title - 标题文字
 *   description - 描述文字
 *   action - 操作按钮文字
 *   onAction - 操作按钮回调
 *   actionIcon - 操作按钮图标
 *   className - 自定义样式
 */
interface EmptyStateProps {
  icon?: LucideIcon
  title?: string
  description?: string
  action?: string
  onAction?: () => void
  actionIcon?: LucideIcon
  className?: string
  children?: React.ReactNode
}

export function EmptyState({
  icon: Icon = Inbox,
  title = '暂无数据',
  description = '当前没有可显示的内容',
  action,
  onAction,
  actionIcon: ActionIcon,
  className,
  children,
}: EmptyStateProps) {
  return (
    <div className={cn('flex flex-col items-center justify-center py-12 px-4 text-center', className)}>
      <div className="mx-auto mb-4 h-14 w-14 rounded-full bg-muted/50 flex items-center justify-center">
        <Icon className="h-7 w-7 text-muted-foreground/60" />
      </div>
      <h3 className="text-base font-medium text-foreground mb-1">{title}</h3>
      <p className="text-sm text-muted-foreground max-w-sm mb-4">{description}</p>
      {action && onAction && (
        <Button onClick={onAction} size="sm" variant="outline">
          {ActionIcon && <ActionIcon className="h-4 w-4 mr-2" />}
          {action}
        </Button>
      )}
      {children}
    </div>
  )
}
