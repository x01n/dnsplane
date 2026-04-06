'use client'

import { useState, useRef } from 'react'
import { toast } from 'sonner'
import { CheckCircle, Mail } from 'lucide-react'
import { authApi } from '@/lib/api'
import { isValidUserEmail } from '@/lib/email'
import { AuthAnimatedLayout } from '@/components/auth/auth-animated-layout'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import shell from '@/components/auth/animated-login-shell.module.css'
import { cn } from '@/lib/utils'

/**
 * 无密码登录：POST /api/auth/magic-link，邮件链接 → /api/quicklogin。
 * 不依赖 GET /auth/config 才显示入口；未开启时由接口返回提示。
 */
export default function MagicLinkPage() {
  const [step, setStep] = useState(1)
  const submitLockRef = useRef(false)
  const [email, setEmail] = useState('')
  const [emailFocus, setEmailFocus] = useState(false)
  const [errorNonce, setErrorNonce] = useState(0)

  const bumpError = () => setErrorNonce((n) => n + 1)

  const title = step === 2 ? '请查收邮件' : '邮箱登录'
  const desc =
    step === 2
      ? '若邮箱已绑定账号，您将收到登录链接'
      : '向您的注册邮箱发送一次性登录链接'

  return (
    <AuthAnimatedLayout
      variant="classic"
      title={title}
      description={desc}
      username={email}
      usernameFocused={step === 1 && emailFocus}
      passwordFocused={false}
      showPassword={false}
      password=""
      errorNonce={errorNonce}
    >
      {step === 2 ? (
        <div className="space-y-4 text-center">
          <CheckCircle className="mx-auto h-12 w-12 text-emerald-500" aria-hidden />
          <p className="text-sm text-neutral-600">
            若 <strong>{email}</strong> 已绑定账号且系统已配置邮件，您将收到登录链接。
          </p>
          <p className="text-xs text-neutral-500">
            链接一次性有效；请检查垃圾箱。已开启二步验证的账号在打开链接后需再输入动态口令。
          </p>
          <button
            type="button"
            className={cn(shell.btnPrimaryClassic, 'mt-2')}
            onClick={() => setStep(1)}
          >
            重新发送
          </button>
          <AuthFooterNav current="magic" />
        </div>
      ) : (
        <form
          className="space-y-0"
          noValidate
          onSubmit={(e) => {
            e.preventDefault()
            const trimmed = email.trim()
            if (!trimmed) {
              toast.error('请输入邮箱')
              bumpError()
              return
            }
            if (!isValidUserEmail(trimmed)) {
              toast.error('请输入有效的邮箱地址')
              bumpError()
              return
            }
            if (submitLockRef.current) return
            submitLockRef.current = true
            setStep(2)
            toast.success('请求已提交，请留意邮箱与垃圾箱')
            void authApi
              .requestMagicLink(trimmed)
              .then((res) => {
                if (res.code !== 0) {
                  toast.error(res.msg || '请求失败')
                  setStep(1)
                  bumpError()
                }
              })
              .catch((err) => {
                if (err instanceof Error && err.name === 'AbortError') return
                toast.error('请求失败')
                setStep(1)
                bumpError()
              })
              .finally(() => {
                submitLockRef.current = false
              })
          }}
        >
          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="magic-email">
              邮箱
            </label>
            <div className={shell.inputWrap}>
              <input
                id="magic-email"
                name="email"
                type="email"
                autoComplete="email"
                placeholder="与账号绑定的邮箱"
                value={email}
                onFocus={() => setEmailFocus(true)}
                onBlur={() => setEmailFocus(false)}
                onChange={(e) => setEmail(e.target.value)}
                className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
              />
            </div>
          </div>

          <button type="submit" className={shell.btnPrimaryClassic}>
            <>
              <Mail className="h-4 w-4" aria-hidden />
              发送登录链接
            </>
          </button>
          <AuthFooterNav current="magic" />
        </form>
      )}
    </AuthAnimatedLayout>
  )
}
