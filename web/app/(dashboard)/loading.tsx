import { Skeleton } from '@/components/ui/skeleton'

/*
 * Dashboard 路由组加载骨架屏
 * 功能：在 (dashboard) 路由组内页面切换时显示，
 *       模拟侧边栏已加载、内容区正在加载的状态
 */
export default function DashboardLoading() {
  return (
    <div className="space-y-6">
      {/* 标题区域骨架 */}
      <div className="flex items-center justify-between">
        <div className="space-y-2">
          <Skeleton className="h-7 w-32" />
          <Skeleton className="h-4 w-48" />
        </div>
        <div className="flex gap-2">
          <Skeleton className="h-9 w-24" />
          <Skeleton className="h-9 w-20" />
        </div>
      </div>

      {/* 统计卡片骨架 */}
      <div className="grid gap-4 md:grid-cols-2 lg:grid-cols-4">
        {[1, 2, 3, 4].map((i) => (
          <div key={i} className="rounded-xl border bg-card p-6 shadow-sm">
            <div className="flex items-center justify-between">
              <div className="space-y-2">
                <Skeleton className="h-4 w-16" />
                <Skeleton className="h-8 w-12" />
              </div>
              <Skeleton className="h-10 w-10 rounded-full" />
            </div>
          </div>
        ))}
      </div>

      {/* 内容区骨架 */}
      <div className="rounded-xl border bg-card shadow-sm">
        <div className="p-6 space-y-4">
          <div className="flex items-center gap-4">
            <Skeleton className="h-9 w-64" />
            <Skeleton className="h-9 w-32" />
          </div>
          <div className="space-y-3">
            {[1, 2, 3, 4, 5].map((i) => (
              <div key={i} className="flex items-center gap-4 py-3">
                <Skeleton className="h-4 w-4" />
                <Skeleton className="h-4 flex-1 max-w-[200px]" />
                <Skeleton className="h-4 w-24" />
                <Skeleton className="h-4 w-16" />
                <Skeleton className="h-4 w-20" />
                <Skeleton className="h-6 w-16 rounded-full" />
              </div>
            ))}
          </div>
        </div>
      </div>
    </div>
  )
}
