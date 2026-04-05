import { Loader2 } from 'lucide-react'

/*
 * Auth 路由组加载页面
 * 功能：在 (auth) 路由组内页面切换时显示居中加载动画
 */
export default function AuthLoading() {
  return (
    <div className="flex min-h-svh items-center justify-center">
      <div className="flex flex-col items-center gap-3">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <p className="text-sm text-muted-foreground">加载中...</p>
      </div>
    </div>
  )
}
