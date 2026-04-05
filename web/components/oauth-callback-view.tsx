'use client'

import { useEffect, Suspense } from 'react'
import { useSearchParams, useRouter } from 'next/navigation'
import { Loader2, Globe } from 'lucide-react'
import { api } from '@/lib/api'
import { toast } from 'sonner'
import { consumeOAuthTokensFromUrl, OAUTH_CALLBACK_ERRORS } from '@/lib/oauth-callback'

function OAuthCallbackInner() {
  const searchParams = useSearchParams()
  const router = useRouter()

  useEffect(() => {
    const error = searchParams.get('error')
    if (error) {
      toast.error(OAUTH_CALLBACK_ERRORS[error] || '第三方登录失败：' + error)
      router.push('/login?error=' + encodeURIComponent(error))
      return
    }

    const { access_token, refresh_token } = consumeOAuthTokensFromUrl(searchParams)
    if (access_token && refresh_token) {
      api.setTokens({ token: access_token, refresh_token })
      toast.success('登录成功')
      router.push('/dashboard')
      return
    }

    router.push('/login?error=login_failed')
  }, [searchParams, router])

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-900 via-purple-900 to-slate-900 relative overflow-hidden">
      <div className="absolute inset-0 overflow-hidden">
        <div className="absolute -top-40 -right-40 w-80 h-80 bg-purple-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob" />
        <div className="absolute -bottom-40 -left-40 w-80 h-80 bg-cyan-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob animation-delay-2000" />
      </div>
      <div className="relative flex flex-col items-center gap-4">
        <div className="flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500 to-purple-600 shadow-lg shadow-purple-500/30">
          <Globe className="h-8 w-8 text-white" />
        </div>
        <Loader2 className="h-8 w-8 animate-spin text-white/70" />
        <p className="text-white/80 text-lg">正在处理登录...</p>
      </div>
    </div>
  )
}

export function OAuthCallbackView() {
  return (
    <Suspense
      fallback={
        <div className="min-h-screen flex items-center justify-center">
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
        </div>
      }
    >
      <OAuthCallbackInner />
    </Suspense>
  )
}
