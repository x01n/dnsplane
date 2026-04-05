'use client'

import { useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { Globe, RotateCcw, Home, AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

export default function Error({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  const router = useRouter()

  useEffect(() => {
    console.error('Application error:', error)
  }, [error])

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-900 via-purple-900 to-slate-900 p-4 relative overflow-hidden">
      {/* 背景装饰 */}
      <div className="absolute inset-0 overflow-hidden">
        <div className="absolute -top-40 -right-40 w-80 h-80 bg-purple-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob"></div>
        <div className="absolute -bottom-40 -left-40 w-80 h-80 bg-cyan-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob animation-delay-2000"></div>
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-80 h-80 bg-pink-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob animation-delay-4000"></div>
      </div>

      <div className="relative w-full max-w-lg space-y-8">
        {/* Logo */}
        <div className="text-center">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500 to-purple-600 shadow-lg shadow-purple-500/30">
            <Globe className="h-8 w-8 text-white" />
          </div>
        </div>

        {/* 错误卡片 */}
        <Card className="backdrop-blur-sm bg-white/95 dark:bg-slate-900/95 shadow-2xl border-0">
          <CardContent className="pt-8 pb-8 px-8 text-center space-y-6">
            {/* 错误图标 */}
            <div className="mx-auto flex h-14 w-14 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
              <AlertTriangle className="h-7 w-7 text-red-600 dark:text-red-400" />
            </div>

            {/* 错误信息 */}
            <div className="space-y-2">
              <h1 className="text-2xl font-bold text-foreground">出现错误</h1>
              <p className="text-muted-foreground">
                应用程序遇到了意外错误，请尝试重试操作。
              </p>
            </div>

            {/* 错误详情 */}
            {error?.message && (
              <div className="rounded-lg bg-muted/50 p-4 text-left">
                <p className="text-xs font-medium text-muted-foreground mb-1">错误详情</p>
                <p className="text-sm text-foreground/80 font-mono break-all">
                  {error.message}
                </p>
                {error.digest && (
                  <p className="text-xs text-muted-foreground mt-2">
                    错误ID: {error.digest}
                  </p>
                )}
              </div>
            )}

            {/* 操作按钮 */}
            <div className="flex items-center justify-center gap-4 pt-2">
              <Button
                variant="outline"
                onClick={() => router.push('/login')}
              >
                <Home className="mr-2 h-4 w-4" />
                返回首页
              </Button>
              <Button
                className="bg-gradient-to-r from-violet-600 to-purple-600 hover:from-violet-500 hover:to-purple-500 shadow-lg"
                onClick={reset}
              >
                <RotateCcw className="mr-2 h-4 w-4" />
                重试
              </Button>
            </div>
          </CardContent>
        </Card>

        {/* 品牌 */}
        <p className="text-center text-sm text-white/30 font-medium">
          DNSPlane — DNS管理系统
        </p>
      </div>
    </div>
  )
}
