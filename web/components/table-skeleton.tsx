/**
 * @component TableSkeleton
 * @description 通用表格骨架屏组件，用于列表页数据加载时的占位展示
 * @param rows - 骨架行数，默认 5
 * @param columns - 骨架列数，默认 5
 */
import { Skeleton } from '@/components/ui/skeleton'

interface TableSkeletonProps {
  rows?: number
  columns?: number
}

export function TableSkeleton({ rows = 5, columns = 5 }: TableSkeletonProps) {
  return (
    <div className="space-y-3 py-4">
      {/* 表头骨架 */}
      <div className="flex gap-4 px-2">
        {Array.from({ length: columns }).map((_, i) => (
          <Skeleton key={`h-${i}`} className="h-4 flex-1" />
        ))}
      </div>
      {/* 数据行骨架 */}
      {Array.from({ length: rows }).map((_, row) => (
        <div key={row} className="flex gap-4 px-2 py-2">
          {Array.from({ length: columns }).map((_, col) => (
            <Skeleton
              key={`${row}-${col}`}
              className="h-4 flex-1"
              style={{ opacity: 1 - row * 0.12 }}
            />
          ))}
        </div>
      ))}
    </div>
  )
}
