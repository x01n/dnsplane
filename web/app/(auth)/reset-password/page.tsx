'use client'

import { useState, useEffect, Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Eye, EyeOff, Loader2, CheckCircle, XCircle, Check, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { encryptedPost } from '@/lib/api'
import { AuthAnimatedLayout } from '@/components/auth/auth-animated-layout'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import shell from '@/components/auth/animated-login-shell.module.css'
import { cn } from '@/lib/utils'
import type { AnimatedLoginCharactersProps } from '@/components/auth/animated-login-characters'

type AnimState = Pick<
  AnimatedLoginCharactersProps,
  | 'username'
  | 'usernameFocused'
  | 'password'
  | 'passwordFocused'
  | 'showPassword'
  | 'errorNonce'
>

const defaultAnim = (): AnimState => ({
  username: '',
  password: '',
  usernameFocused: false,
  passwordFocused: false,
  showPassword: false,
  errorNonce: 0,
})

function ResetPasswordContent() {
  const searchParams = useSearchParams()
  const token = searchParams.get('token')

  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)
  const [pwdFocused, setPwdFocused] = useState(false)
  const [confirmFocused, setConfirmFocused] = useState(false)
  const [formData, setFormData] = useState({
    password: '',
    confirmPassword: '',
  })
  const [anim, setAnim] = useState<AnimState>(defaultAnim)

  const bumpError = () =>
    setAnim((a) => ({ ...a, errorNonce: a.errorNonce + 1 }))

  useEffect(() => {
    setAnim((a) => ({
      ...a,
      username: '',
      usernameFocused: false,
      password: formData.password,
      passwordFocused: pwdFocused || confirmFocused,
      showPassword: showPassword || showConfirmPassword,
    }))
  }, [
    formData.password,
    pwdFocused,
    confirmFocused,
    showPassword,
    showConfirmPassword,
  ])

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!token) {
      toast.error('无效的重置链接')
      bumpError()
      return
    }

    if (!formData.password) {
      toast.error('请输入新密码')
      bumpError()
      return
    }

    if (formData.password.length < 8) {
      toast.error('密码长度至少8位')
      bumpError()
      return
    }
    if (!/[A-Z]/.test(formData.password)) {
      toast.error('密码需包含至少一个大写字母')
      bumpError()
      return
    }
    if (!/[a-z]/.test(formData.password)) {
      toast.error('密码需包含至少一个小写字母')
      bumpError()
      return
    }
    if (!/[0-9]/.test(formData.password)) {
      toast.error('密码需包含至少一个数字')
      bumpError()
      return
    }

    if (formData.password !== formData.confirmPassword) {
      toast.error('两次输入的密码不一致')
      bumpError()
      return
    }

    setLoading(true)
    try {
      const res = await encryptedPost<{ code: number; msg?: string }>(
        '/auth/reset-password',
        {
          token,
          password: formData.password,
        },
      )
      if (res.code === 0) {
        setSuccess(true)
        toast.success('密码重置成功')
      } else {
        toast.error(res.msg || '重置失败')
        bumpError()
      }
    } catch {
      toast.error('重置失败，请稍后重试')
      bumpError()
    } finally {
      setLoading(false)
    }
  }

  if (!token) {
    return (
      <AuthAnimatedLayout
        variant="classic"
        title="链接无效"
        description="无法重置密码"
        username=""
        password=""
        usernameFocused={false}
        passwordFocused={false}
        showPassword={false}
        errorNonce={anim.errorNonce}
      >
        <div className="space-y-4 text-center">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-red-100">
            <XCircle className="h-8 w-8 text-red-600" />
          </div>
          <p className="text-sm text-neutral-600">
            此链接无效或已过期，请重新申请密码重置。
          </p>
          <Link href="/forgot-password/">
            <Button className="w-full">重新申请</Button>
          </Link>
          <AuthFooterNav current="reset" />
        </div>
      </AuthAnimatedLayout>
    )
  }

  if (success) {
    return (
      <AuthAnimatedLayout
        variant="classic"
        title="重置成功"
        description="请使用新密码登录"
        username=""
        password=""
        usernameFocused={false}
        passwordFocused={false}
        showPassword={false}
        errorNonce={anim.errorNonce}
      >
        <div className="space-y-4 text-center">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100">
            <CheckCircle className="h-8 w-8 text-green-600" />
          </div>
          <p className="text-sm text-neutral-600">
            您的密码已成功重置，请使用新密码登录。
          </p>
          <Link href="/login/">
            <Button className="w-full">去登录</Button>
          </Link>
          <AuthFooterNav current="reset" />
        </div>
      </AuthAnimatedLayout>
    )
  }

  return (
    <AuthAnimatedLayout
      variant="classic"
      title="重置密码"
      description="请设置您的新密码"
      username={anim.username}
      usernameFocused={anim.usernameFocused}
      password={anim.password}
      passwordFocused={anim.passwordFocused}
      showPassword={anim.showPassword}
      errorNonce={anim.errorNonce}
    >
      <form onSubmit={handleSubmit} className="space-y-0" noValidate>
        <div className={shell.formGroup}>
          <label className={shell.label} htmlFor="password">
            新密码
          </label>
          <div className={shell.inputWrap}>
            <input
              id="password"
              name="password"
              type={showPassword ? 'text' : 'password'}
              autoComplete="new-password"
              placeholder="8位以上，含大小写和数字"
              value={formData.password}
              disabled={loading}
              onFocus={() => setPwdFocused(true)}
              onBlur={() => setPwdFocused(false)}
              onChange={(e) =>
                setFormData({ ...formData, password: e.target.value })
              }
              className={shell.inputUnderline}
            />
            <button
              type="button"
              className={shell.togglePassword}
              onClick={() => setShowPassword(!showPassword)}
              aria-label={showPassword ? '隐藏密码' : '显示密码'}
            >
              {showPassword ? (
                <EyeOff className="h-5 w-5" />
              ) : (
                <Eye className="h-5 w-5" />
              )}
            </button>
          </div>
        </div>
        {formData.password ? (
          <div className="mb-4 space-y-1 text-xs">
            {[
              { ok: formData.password.length >= 8, text: '至少8个字符' },
              { ok: /[A-Z]/.test(formData.password), text: '包含大写字母' },
              { ok: /[a-z]/.test(formData.password), text: '包含小写字母' },
              { ok: /[0-9]/.test(formData.password), text: '包含数字' },
            ].map((rule) => (
              <div
                key={rule.text}
                className={cn(
                  'flex items-center gap-1.5',
                  rule.ok ? 'text-green-600' : 'text-neutral-400',
                )}
              >
                {rule.ok ? (
                  <Check className="h-3 w-3" />
                ) : (
                  <X className="h-3 w-3" />
                )}
                <span>{rule.text}</span>
              </div>
            ))}
          </div>
        ) : null}

        <div className={shell.formGroup}>
          <label className={shell.label} htmlFor="confirmPassword">
            确认密码
          </label>
          <div className={shell.inputWrap}>
            <input
              id="confirmPassword"
              name="confirmPassword"
              type={showConfirmPassword ? 'text' : 'password'}
              autoComplete="new-password"
              placeholder="请再次输入新密码"
              value={formData.confirmPassword}
              disabled={loading}
              onFocus={() => setConfirmFocused(true)}
              onBlur={() => setConfirmFocused(false)}
              onChange={(e) =>
                setFormData({ ...formData, confirmPassword: e.target.value })
              }
              className={shell.inputUnderline}
            />
            <button
              type="button"
              className={shell.togglePassword}
              onClick={() => setShowConfirmPassword(!showConfirmPassword)}
              aria-label={showConfirmPassword ? '隐藏密码' : '显示密码'}
            >
              {showConfirmPassword ? (
                <EyeOff className="h-5 w-5" />
              ) : (
                <Eye className="h-5 w-5" />
              )}
            </button>
          </div>
        </div>

        <button type="submit" className={shell.btnLogin} disabled={loading}>
          <span className={shell.btnText}>
            {loading ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                重置中...
              </>
            ) : (
              '重置密码'
            )}
          </span>
          <div className={shell.btnHoverContent} aria-hidden>
            <span>重置密码</span>
            <svg
              width="16"
              height="16"
              viewBox="0 0 24 24"
              fill="none"
              stroke="currentColor"
              strokeWidth="2.5"
              strokeLinecap="round"
              strokeLinejoin="round"
            >
              <line x1="5" y1="12" x2="19" y2="12" />
              <polyline points="12 5 19 12 12 19" />
            </svg>
          </div>
        </button>

        <AuthFooterNav current="reset" />
      </form>
    </AuthAnimatedLayout>
  )
}

export default function ResetPasswordPage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-screen items-center justify-center bg-gradient-to-br from-slate-50 to-slate-200">
          <Loader2 className="h-8 w-8 animate-spin text-neutral-500" />
        </div>
      }
    >
      <ResetPasswordContent />
    </Suspense>
  )
}
