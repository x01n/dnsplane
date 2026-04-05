'use client'

import { useEffect } from 'react'
import { AlertTriangle, RefreshCw, Home } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'

/*
 * Auth 层级错误页面
 * 功能：捕获 (auth) 路由组内所有页面（登录/注册/重置密码等）的渲染错误
 *       提供简洁的错误提示和重试/返回操作
 */
export default function AuthError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  useEffect(() => {
    console.error('[Auth Error]', error)
  }, [error])

  return (
    <div className="min-h-svh flex items-center justify-center bg-background p-4">
      <Card className="w-full max-w-sm shadow-lg border-0">
        <CardHeader className="text-center pb-2">
          <div className="mx-auto mb-3 h-12 w-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <AlertTriangle className="h-6 w-6 text-red-600 dark:text-red-400" />
          </div>
          <CardTitle className="text-lg">页面出现错误</CardTitle>
          <p className="text-sm text-muted-foreground mt-1">
            {error.message || '加载时遇到问题，请重试'}
          </p>
        </CardHeader>
        <CardContent />
        <CardFooter className="flex gap-2 justify-center">
          <Button onClick={reset} size="sm">
            <RefreshCw className="h-4 w-4 mr-2" />
            重试
          </Button>
          <Button variant="outline" size="sm" onClick={() => window.location.href = '/login'}>
            <Home className="h-4 w-4 mr-2" />
            返回登录
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}
