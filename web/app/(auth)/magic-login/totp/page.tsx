'use client'

import { Suspense, useState } from 'react'
import { useRouter, useSearchParams } from 'next/navigation'
import { toast } from 'sonner'
import { Loader2, Shield } from 'lucide-react'
import {
  AuthAnimatedLayout,
  AuthAnimatedLoading,
} from '@/components/auth/auth-animated-layout'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import shell from '@/components/auth/animated-login-shell.module.css'
import { cn } from '@/lib/utils'
import { api, authApi } from '@/lib/api'

/**
 * 无密码登录且账号已启用 TOTP：由 /api/quicklogin 重定向至此，提交动态口令后 POST /api/auth/magic-link/totp。
 */
export default function MagicLoginTotpWrapper() {
  return (
    <Suspense fallback={<AuthAnimatedLoading variant="classic" label="加载中…" />}>
      <MagicLoginTotpPage />
    </Suspense>
  )
}

function MagicLoginTotpPage() {
  const router = useRouter()
  const searchParams = useSearchParams()
  const preauth = searchParams.get('preauth')?.trim() ?? ''

  const [code, setCode] = useState('')
  const [focused, setFocused] = useState(false)
  const [submitting, setSubmitting] = useState(false)
  const [errorNonce, setErrorNonce] = useState(0)

  const bumpError = () => setErrorNonce((n) => n + 1)

  if (!preauth) {
    return (
      <AuthAnimatedLayout
        variant="classic"
        title="链接无效"
        description="缺少验证参数"
        username=""
        password=""
        usernameFocused={false}
        passwordFocused={false}
        showPassword={false}
        errorNonce={0}
      >
        <p className="text-center text-sm text-neutral-600">
          请重新点击邮件中的登录链接，或返回邮箱登录页重新发送。
        </p>
        <AuthFooterNav current="magic" />
      </AuthAnimatedLayout>
    )
  }

  return (
    <AuthAnimatedLayout
      variant="classic"
      title="二步验证"
      description="请输入身份验证器中的 6 位动态口令"
      username={code}
      usernameFocused={focused}
      passwordFocused={false}
      showPassword={false}
      password=""
      errorNonce={errorNonce}
    >
      <form
        className="space-y-0"
        noValidate
        onSubmit={async (e) => {
          e.preventDefault()
          const c = code.trim()
          if (c.length !== 6) {
            toast.error('请输入 6 位动态口令')
            bumpError()
            return
          }
          setSubmitting(true)
          try {
            const res = await authApi.verifyMagicLinkTotp(preauth, c)
            if (res.code === 0 && res.data) {
              api.setTokens({
                token: res.data.token,
                refresh_token: res.data.refresh_token,
              })
              toast.success(res.msg || '登录成功')
              const target = res.data.redirect || '/dashboard/'
              router.push(target.endsWith('/') ? target : `${target}/`)
            } else {
              toast.error(res.msg || '验证失败')
              bumpError()
            }
          } catch {
            toast.error('验证失败')
            bumpError()
          } finally {
            setSubmitting(false)
          }
        }}
      >
        <div className="mb-2 flex justify-center">
          <div className="flex h-12 w-12 items-center justify-center rounded-full bg-violet-100 dark:bg-violet-950">
            <Shield className="h-6 w-6 text-violet-600" aria-hidden />
          </div>
        </div>
        <div className={shell.formGroupClassic}>
          <label className={shell.labelClassic} htmlFor="magic-totp">
            动态口令
          </label>
          <div className={shell.inputWrap}>
            <input
              id="magic-totp"
              name="totp_code"
              inputMode="numeric"
              autoComplete="one-time-code"
              placeholder="000000"
              maxLength={6}
              value={code}
              disabled={submitting}
              onFocus={() => setFocused(true)}
              onBlur={() => setFocused(false)}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, '').slice(0, 6))}
              className={cn(shell.inputClassic, shell.inputClassicNoToggle, 'text-center tracking-[0.35em]')}
            />
          </div>
        </div>
        <button type="submit" className={shell.btnPrimaryClassic} disabled={submitting}>
          {submitting ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" />
              验证中…
            </>
          ) : (
            '完成登录'
          )}
        </button>
        <AuthFooterNav current="magic" />
      </form>
    </AuthAnimatedLayout>
  )
}
