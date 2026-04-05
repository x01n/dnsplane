'use client'

import { useEffect, Suspense } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { Loader2, XCircle, Command } from 'lucide-react'
import { Button } from '@/components/ui/button'

/**
 * 邮件等场景下发的「魔法链接」token 与后端快速登录一致，交给 /api/quicklogin 设置 HttpOnly 会话并跳转。
 */
export default function MagicLoginWrapper() {
  return (
    <Suspense fallback={<div className="flex min-h-svh items-center justify-center"><Loader2 className="h-8 w-8 animate-spin" /></div>}>
      <MagicLoginPage />
    </Suspense>
  )
}

function MagicLoginPage() {
  const router = useRouter()
  const searchParams = useSearchParams()

  useEffect(() => {
    const token = searchParams.get('token')
    if (token) {
      window.location.assign(`/api/quicklogin?token=${encodeURIComponent(token)}`)
    }
  }, [searchParams])

  const token = searchParams.get('token')
  if (!token) {
    return (
      <div className="flex min-h-svh items-center justify-center bg-background">
        <div className="w-full max-w-sm space-y-6 text-center px-6">
          <XCircle className="h-12 w-12 mx-auto text-destructive" />
          <p className="text-lg font-medium">登录失败</p>
          <p className="text-sm text-muted-foreground">缺少登录凭证</p>
          <Button onClick={() => router.push('/login')} variant="outline">返回登录页</Button>
        </div>
      </div>
    )
  }

  return (
    <div className="flex min-h-svh items-center justify-center bg-background">
      <div className="w-full max-w-sm space-y-6 text-center px-6">
        <div className="flex items-center justify-center gap-2 mb-8">
          <div className="flex h-8 w-8 items-center justify-center rounded-md bg-primary text-primary-foreground">
            <Command className="h-4 w-4" />
          </div>
          <span className="text-lg font-semibold">DNSPlane</span>
        </div>
        <div className="space-y-4">
          <Loader2 className="h-12 w-12 animate-spin mx-auto text-primary" />
          <p className="text-lg font-medium">正在验证登录链接...</p>
          <p className="text-sm text-muted-foreground">请稍候</p>
        </div>
      </div>
    </div>
  )
}
