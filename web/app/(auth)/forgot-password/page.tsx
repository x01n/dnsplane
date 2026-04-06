'use client'

import { useState, useRef } from 'react'
import { toast } from 'sonner'
import { CheckCircle } from 'lucide-react'
import { authApi } from '@/lib/api'
import { isValidUserEmail } from '@/lib/email'
import {
  AuthAnimatedLayout,
} from '@/components/auth/auth-animated-layout'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import shell from '@/components/auth/animated-login-shell.module.css'
import { cn } from '@/lib/utils'

/**
 * POST /api/auth/forgot-password，仅 { email }。未配置邮件时服务端仍返回成功提示。
 */
export default function ForgotPasswordPage() {
  const [step, setStep] = useState(1)
  const submitLockRef = useRef(false)
  const [email, setEmail] = useState('')
  const [emailFocus, setEmailFocus] = useState(false)
  const [characterErrorNonce, setCharacterErrorNonce] = useState(0)

  const bumpError = () => setCharacterErrorNonce((n) => n + 1)

  const layoutTitle = step === 2 ? '请查收邮件' : '找回密码'
  const layoutDesc =
    step === 2
      ? '若邮箱已注册，您将收到重置链接'
      : '输入注册时使用的邮箱，我们将发送重置链接'

  return (
    <AuthAnimatedLayout
      variant="classic"
      title={layoutTitle}
      description={layoutDesc}
      username={email}
      usernameFocused={step === 1 && emailFocus}
      passwordFocused={false}
      showPassword={false}
      password=""
      errorNonce={characterErrorNonce}
    >
      {step === 2 ? (
        <div className="space-y-4 text-center">
          <CheckCircle className="mx-auto h-12 w-12 text-emerald-500" aria-hidden />
          <p className="text-sm text-neutral-600">
            若 <strong>{email}</strong> 已注册，您将收到重置密码邮件。
          </p>
          <p className="text-xs text-neutral-500">
            链接有效期以邮件说明为准；如未收到请检查垃圾箱。
          </p>
          <button
            type="button"
            className={cn(shell.btnPrimaryClassic)}
            onClick={() => setStep(1)}
          >
            重新发送
          </button>
          <AuthFooterNav current="forgot" />
        </div>
      ) : (
        <form
          onSubmit={async (e) => {
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
              .forgotPassword({ email: trimmed })
              .then((res) => {
                if (res.code !== 0) {
                  toast.error(res.msg || '发送失败')
                  setStep(1)
                  bumpError()
                }
              })
              .catch((e) => {
                if (e instanceof Error && e.name === 'AbortError') return
                toast.error('发送失败')
                setStep(1)
                bumpError()
              })
              .finally(() => {
                submitLockRef.current = false
              })
          }}
          className="space-y-0"
          noValidate
        >
          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="email">
              邮箱
            </label>
            <div className={shell.inputWrap}>
              <input
                id="email"
                name="email"
                type="email"
                autoComplete="email"
                placeholder="注册时使用的邮箱"
                value={email}
                onFocus={() => setEmailFocus(true)}
                onBlur={() => setEmailFocus(false)}
                onChange={(e) => setEmail(e.target.value)}
                className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
              />
            </div>
          </div>

          <button type="submit" className={shell.btnPrimaryClassic}>
            发送重置链接
          </button>
          <AuthFooterNav current="forgot" />
        </form>
      )}
    </AuthAnimatedLayout>
  )
}
