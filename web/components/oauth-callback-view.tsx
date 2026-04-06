'use client'

import { useEffect, Suspense } from 'react'
import { useSearchParams, useRouter } from 'next/navigation'
import { Loader2 } from 'lucide-react'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import { consumeOAuthTokensFromUrl } from '@/lib/oauth-callback'
import { AuthAnimatedLayout, AuthAnimatedLoading } from '@/components/auth/auth-animated-layout'

const idleCharacters = {
  username: '',
  password: '',
  usernameFocused: false,
  passwordFocused: false,
  showPassword: false,
  errorNonce: 0,
}

function OAuthCallbackInner() {
  const searchParams = useSearchParams()
  const router = useRouter()

  useEffect(() => {
    const error = searchParams.get('error')
    if (error) {
      router.push('/login?error=' + encodeURIComponent(error))
      return
    }

    const { access_token, refresh_token } = consumeOAuthTokensFromUrl(searchParams)
    if (access_token && refresh_token) {
      api.setTokens({ token: access_token, refresh_token })
      toast.success('登录成功')
      router.push('/dashboard/')
      return
    }

    router.push('/login?error=login_failed')
  }, [searchParams, router])

  return (
    <AuthAnimatedLayout
      title="第三方登录"
      description="正在处理登录..."
      {...idleCharacters}
    >
      <div className="flex flex-col items-center gap-4 py-4 text-center">
        <Loader2
          className="h-12 w-12 animate-spin text-[#5b21b6]"
          aria-hidden
        />
        <p className="text-sm text-neutral-600">
          正在验证 OAuth 回调，请稍候。
        </p>
      </div>
    </AuthAnimatedLayout>
  )
}

export function OAuthCallbackView() {
  return (
    <Suspense fallback={<AuthAnimatedLoading label="加载中..." />}>
      <OAuthCallbackInner />
    </Suspense>
  )
}
