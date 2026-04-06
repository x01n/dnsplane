'use client'

import { useState, useEffect, useRef } from 'react'
import { useRouter } from 'next/navigation'
import { toast } from 'sonner'
import { Eye, EyeOff, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { authApi } from '@/lib/api'
import { AuthAnimatedLayout } from '@/components/auth/auth-animated-layout'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import shell from '@/components/auth/animated-login-shell.module.css'

/** 密码注册：发送验证码 POST /api/auth/send-code，提交 POST /api/auth/register */
export default function RegisterPage() {
  const router = useRouter()
  const [usernameFocused, setUsernameFocused] = useState(false)
  const [passwordFocused, setPasswordFocused] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [characterErrorNonce, setCharacterErrorNonce] = useState(0)

  const [submitting, setSubmitting] = useState(false)
  const [codeCountdown, setCodeCountdown] = useState(0)
  const codeTimerRef = useRef<ReturnType<typeof setInterval> | null>(null)

  const [form, setForm] = useState({
    username: '',
    email: '',
    password: '',
    code: '',
  })

  const bumpCharacterError = () => setCharacterErrorNonce((n) => n + 1)

  useEffect(() => {
    return () => {
      if (codeTimerRef.current) clearInterval(codeTimerRef.current)
    }
  }, [])

  const clearRegisterCodeTimer = () => {
    if (codeTimerRef.current) {
      clearInterval(codeTimerRef.current)
      codeTimerRef.current = null
    }
  }

  const sendRegisterCode = () => {
    if (!form.email.trim()) {
      toast.error('请先填写邮箱')
      return
    }
    clearRegisterCodeTimer()
    setCodeCountdown(60)
    codeTimerRef.current = setInterval(() => {
      setCodeCountdown((prev) => {
        if (prev <= 1) {
          clearRegisterCodeTimer()
          return 0
        }
        return prev - 1
      })
    }, 1000)
    toast.info('已提交发送请求，请留意邮箱')
    void authApi
      .sendCode(form.email.trim(), 'register')
      .then((res) => {
        if (res.code !== 0) {
          clearRegisterCodeTimer()
          setCodeCountdown(0)
          toast.error(res.msg || '发送失败')
          bumpCharacterError()
        }
      })
      .catch((e) => {
        if (e instanceof Error && e.name === 'AbortError') return
        clearRegisterCodeTimer()
        setCodeCountdown(0)
        toast.error('发送失败')
        bumpCharacterError()
      })
  }

  const handleRegister = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!form.username.trim() || !form.password || !form.email.trim() || !form.code.trim()) {
      toast.error('请填写完整信息')
      bumpCharacterError()
      return
    }
    if (form.password.length < 6) {
      toast.error('密码至少 6 位')
      bumpCharacterError()
      return
    }
    setSubmitting(true)
    try {
      const res = await authApi.register({
        username: form.username.trim(),
        password: form.password,
        email: form.email.trim(),
        code: form.code.trim(),
      })
      if (res.code === 0) {
        toast.success(res.msg || '注册成功')
        router.push('/login')
      } else {
        toast.error(res.msg || '注册失败')
        bumpCharacterError()
      }
    } catch {
      toast.error('注册失败')
      bumpCharacterError()
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <AuthAnimatedLayout
      variant="classic"
      title="创建账户"
      description="使用邮箱验证码完成注册"
      usernameFocused={usernameFocused}
      passwordFocused={passwordFocused}
      showPassword={showPassword}
      username={form.username}
      password={form.password}
      errorNonce={characterErrorNonce}
    >
      <div className="space-y-5 text-sm text-neutral-600">
        <form className="space-y-0" onSubmit={handleRegister} noValidate>
          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="reg-username">
              用户名
            </label>
            <div className={shell.inputWrap}>
              <input
                id="reg-username"
                name="username"
                autoComplete="username"
                placeholder="2～64 个字符"
                value={form.username}
                disabled={submitting}
                onFocus={() => setUsernameFocused(true)}
                onBlur={() => setUsernameFocused(false)}
                onChange={(e) => setForm({ ...form, username: e.target.value })}
                className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
              />
            </div>
          </div>

          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="reg-email">
              邮箱
            </label>
            <div className={shell.inputWrap}>
              <input
                id="reg-email"
                name="email"
                type="email"
                autoComplete="email"
                placeholder="用于登录与找回密码"
                value={form.email}
                disabled={submitting}
                onChange={(e) => setForm({ ...form, email: e.target.value })}
                className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
              />
            </div>
          </div>

          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="reg-password">
              密码
            </label>
            <div className={shell.inputWrap}>
              <input
                id="reg-password"
                name="password"
                type={showPassword ? 'text' : 'password'}
                autoComplete="new-password"
                placeholder="至少 6 位"
                value={form.password}
                disabled={submitting}
                onFocus={() => setPasswordFocused(true)}
                onBlur={() => setPasswordFocused(false)}
                onChange={(e) => setForm({ ...form, password: e.target.value })}
                className={shell.inputClassic}
              />
              <button
                type="button"
                className={shell.togglePasswordClassic}
                onClick={() => setShowPassword(!showPassword)}
                aria-label={showPassword ? '隐藏密码' : '显示密码'}
              >
                {showPassword ? (
                  <EyeOff className="h-4 w-4" />
                ) : (
                  <Eye className="h-4 w-4" />
                )}
              </button>
            </div>
          </div>

          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="reg-code">
              邮箱验证码
            </label>
            <div className={shell.captchaRow}>
              <div className={cn(shell.inputWrap, 'min-w-0 flex-1')}>
                <input
                  id="reg-code"
                  name="code"
                  inputMode="text"
                  autoComplete="one-time-code"
                  placeholder="验证码"
                  value={form.code}
                  disabled={submitting}
                  onChange={(e) => setForm({ ...form, code: e.target.value })}
                  className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
                />
              </div>
              <button
                type="button"
                onClick={() => sendRegisterCode()}
                disabled={codeCountdown > 0 || submitting}
                className={shell.captchaBtnClassic}
              >
                {codeCountdown > 0 ? (
                  <span className="text-xs text-neutral-500">{codeCountdown}s</span>
                ) : (
                  <span className="text-xs font-medium text-neutral-700">发送</span>
                )}
              </button>
            </div>
          </div>

          <button
            type="submit"
            className={shell.btnPrimaryClassic}
            disabled={submitting}
          >
            {submitting ? (
              <>
                <Loader2 className="h-4 w-4 animate-spin" />
                注册中…
              </>
            ) : (
              '注册'
            )}
          </button>
        </form>

        <AuthFooterNav current="register" />
      </div>
    </AuthAnimatedLayout>
  )
}
