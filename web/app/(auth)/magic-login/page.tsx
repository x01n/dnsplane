'use client'

import { useEffect, Suspense } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { Loader2, XCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import {
  AuthAnimatedLayout,
  AuthAnimatedLoading,
} from '@/components/auth/auth-animated-layout'

const idleCharacters = {
  username: '',
  password: '',
  usernameFocused: false,
  passwordFocused: false,
  showPassword: false,
  errorNonce: 0,
}

/**
 * 邮件魔法链接：跳转 /api/quicklogin?token=；无 TOTP 则写会话并进入控制台，有 TOTP 则跳转 /magic-login/totp/ 继续验证。
 */
export default function MagicLoginWrapper() {
  return (
    <Suspense fallback={<AuthAnimatedLoading variant="classic" label="加载中…" />}>
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
      <AuthAnimatedLayout
        variant="classic"
        title="登录失败"
        description="缺少登录凭证"
        {...idleCharacters}
      >
        <div className="space-y-4 text-center">
          <XCircle className="mx-auto h-12 w-12 text-red-500" aria-hidden />
          <p className="text-sm text-neutral-600">无法完成魔法链接登录。</p>
          <Button
            onClick={() => router.push('/login/')}
            variant="outline"
            className="w-full"
          >
            返回登录页
          </Button>
        </div>
      </AuthAnimatedLayout>
    )
  }

  return (
    <AuthAnimatedLayout
      variant="classic"
      title="正在登录"
      description="请稍候"
      {...idleCharacters}
    >
      <div className="space-y-4 text-center">
        <Loader2
          className="mx-auto h-12 w-12 animate-spin text-violet-600"
          aria-hidden
        />
        <p className="text-sm text-neutral-600">正在验证登录链接…</p>
      </div>
    </AuthAnimatedLayout>
  )
}
