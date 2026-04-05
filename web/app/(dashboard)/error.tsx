'use client'

import { useEffect } from 'react'
import { AlertTriangle, RefreshCw, Home, Copy, ChevronDown, ChevronUp } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'
import { useState } from 'react'
import { toast } from 'sonner'

/*
 * Dashboard 层级错误页面
 * 功能：捕获 (dashboard) 路由组内所有页面的渲染错误
 *       提供刷新/返回首页/复制错误信息等操作
 *       不影响侧边栏和顶部导航的正常显示（因为 layout 不受 error boundary 影响）
 */
export default function DashboardError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  const [showDetails, setShowDetails] = useState(false)

  useEffect(() => {
    console.error('[Dashboard Error]', error)
  }, [error])

  const handleCopy = () => {
    const text = `Error: ${error.message}\nDigest: ${error.digest || 'N/A'}\nStack: ${error.stack || 'N/A'}`
    navigator.clipboard.writeText(text).then(() => toast.success('已复制错误信息'))
  }

  return (
    <div className="flex items-center justify-center min-h-[60vh] p-4">
      <Card className="w-full max-w-lg shadow-lg border-0">
        <CardHeader className="text-center pb-2">
          <div className="mx-auto mb-4 h-14 w-14 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <AlertTriangle className="h-7 w-7 text-red-600 dark:text-red-400" />
          </div>
          <CardTitle className="text-xl">页面加载出错</CardTitle>
          <p className="text-sm text-muted-foreground mt-2">
            当前页面遇到了问题，其他功能不受影响。
          </p>
        </CardHeader>

        <CardContent className="space-y-3">
          <div className="rounded-lg bg-red-50 dark:bg-red-900/20 p-3 border border-red-200 dark:border-red-800">
            <p className="text-sm font-medium text-red-800 dark:text-red-300 break-all">
              {error.message || '发生未知错误'}
            </p>
            {error.digest && (
              <p className="text-xs text-red-600/60 dark:text-red-400/60 mt-1">ID: {error.digest}</p>
            )}
          </div>

          <button
            onClick={() => setShowDetails(v => !v)}
            className="flex items-center justify-between w-full text-sm text-muted-foreground hover:text-foreground transition-colors py-1"
          >
            <span>错误详情</span>
            {showDetails ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
          </button>

          {showDetails && error.stack && (
            <div className="rounded-lg bg-muted/50 p-3 max-h-40 overflow-auto">
              <pre className="text-xs font-mono text-muted-foreground whitespace-pre-wrap break-all">
                {error.stack}
              </pre>
            </div>
          )}
        </CardContent>

        <CardFooter className="flex flex-col sm:flex-row gap-2">
          <Button onClick={reset} className="w-full sm:w-auto">
            <RefreshCw className="h-4 w-4 mr-2" />
            重试
          </Button>
          <Button variant="outline" onClick={() => window.location.href = '/dashboard'} className="w-full sm:w-auto">
            <Home className="h-4 w-4 mr-2" />
            返回仪表盘
          </Button>
          <Button variant="ghost" onClick={handleCopy} className="w-full sm:w-auto">
            <Copy className="h-4 w-4 mr-2" />
            复制
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}
