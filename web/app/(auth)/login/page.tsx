'use client'

import { useState, useEffect, lazy, Suspense } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Eye, EyeOff, RefreshCw, Loader2 } from 'lucide-react'
import { cn } from '@/lib/utils'
import { authApi, api } from '@/lib/api'
import { OAUTH_CALLBACK_ERRORS } from '@/lib/oauth-callback'
import {
  AuthAnimatedLayout,
  AuthAnimatedLoading,
} from '@/components/auth/auth-animated-layout'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import shell from '@/components/auth/animated-login-shell.module.css'

const GoCaptchaDialog = lazy(() => import('@/components/go-captcha-dialog'))

export default function LoginPage() {
  const router = useRouter()
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [captchaEnabled, setCaptchaEnabled] = useState(false)
  const [captchaType, setCaptchaType] = useState('image')
  const [captchaId, setCaptchaId] = useState('')
  const [captchaImage, setCaptchaImage] = useState('')
  const [captchaLoading, setCaptchaLoading] = useState(false)
  const [verifyToken, setVerifyToken] = useState('')
  const [showGoCaptcha, setShowGoCaptcha] = useState(false)
  const [needTotp, setNeedTotp] = useState(false)
  const [installed, setInstalled] = useState(true)
  const [checkingInstall, setCheckingInstall] = useState(true)

  const [usernameFocused, setUsernameFocused] = useState(false)
  const [passwordFocused, setPasswordFocused] = useState(false)
  const [characterErrorNonce, setCharacterErrorNonce] = useState(0)

  const [formData, setFormData] = useState({
    username: '',
    password: '',
    captcha: '',
    totp_code: '',
  })

  const bumpCharacterError = () => {
    setCharacterErrorNonce((n) => n + 1)
  }

  useEffect(() => {
    checkInstallStatus()
    // eslint-disable-next-line react-hooks/exhaustive-deps -- 仅挂载时检查安装状态
  }, [])

  useEffect(() => {
    if (typeof window === 'undefined') return
    const params = new URLSearchParams(window.location.search)
    const err = params.get('error')
    if (!err) return
    toast.error(OAUTH_CALLBACK_ERRORS[err] || `操作失败：${err}`)
    params.delete('error')
    const q = params.toString()
    window.history.replaceState(null, '', window.location.pathname + (q ? `?${q}` : ''))
  }, [])

  const checkInstallStatus = async () => {
    try {
      const res = await authApi.getInstallStatus()
      if (res.code === 0 && res.data) {
        setInstalled(res.data.installed)
        if (res.data.installed) {
          checkAuthConfig()
        }
      }
    } catch {
      // ignore
    } finally {
      setCheckingInstall(false)
    }
  }

  const checkAuthConfig = async () => {
    try {
      const res = await authApi.getConfig()
      if (res.code === 0 && res.data) {
        const needCaptcha = !!(res.data.login_captcha ?? res.data.captcha_enabled)
        setCaptchaEnabled(needCaptcha)
        const type = (res.data as Record<string, unknown>).captcha_type as string || 'image'
        setCaptchaType(type)
        if (needCaptcha && (type === 'image' || !type)) {
          loadCaptcha()
        }
      }
    } catch {
      // ignore
    }
  }

  const loadCaptcha = async () => {
    setCaptchaLoading(true)
    try {
      const res = await authApi.getCaptcha()
      if (res.code === 0 && res.data) {
        setCaptchaId(res.data.captcha_id)
        setCaptchaImage(res.data.captcha_image)
      }
    } catch {
      toast.error('获取验证码失败')
    } finally {
      setCaptchaLoading(false)
    }
  }

  const handleInstall = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!formData.username || !formData.password) {
      toast.error('请输入管理员用户名和密码')
      bumpCharacterError()
      return
    }
    setLoading(true)
    try {
      const res = await authApi.install({
        username: formData.username,
        password: formData.password,
      })
      if (res.code === 0) {
        toast.success('安装成功')
        setInstalled(true)
        checkAuthConfig()
      } else {
        toast.error(res.msg || '安装失败')
        bumpCharacterError()
      }
    } catch {
      toast.error('安装失败')
      bumpCharacterError()
    } finally {
      setLoading(false)
    }
  }

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!formData.username || !formData.password) {
      toast.error('请输入用户名和密码')
      bumpCharacterError()
      return
    }
    if (captchaEnabled && captchaType === 'behavioral' && !verifyToken) {
      setShowGoCaptcha(true)
      return
    }
    if (captchaEnabled && captchaType !== 'behavioral' && !formData.captcha) {
      toast.error('请输入验证码')
      bumpCharacterError()
      return
    }
    if (needTotp && !formData.totp_code) {
      toast.error('请输入动态口令')
      bumpCharacterError()
      return
    }

    setLoading(true)
    try {
      const res = await authApi.login({
        username: formData.username,
        password: formData.password,
        captcha_id: captchaId,
        captcha: formData.captcha,
        verify_token: verifyToken,
        totp_code: formData.totp_code,
      })

      if (res.code === 0 && res.data) {
        api.setTokens({
          token: res.data.token,
          refresh_token: res.data.refresh_token,
        })
        toast.success('登录成功')
        router.push('/dashboard/')
      } else if (res.code === 2) {
        setNeedTotp(true)
        toast.info('请输入动态口令')
      } else {
        toast.error(res.msg || '登录失败')
        bumpCharacterError()
        if (captchaEnabled) {
          if (captchaType === 'behavioral') {
            setVerifyToken('')
          } else {
            loadCaptcha()
            setFormData((prev) => ({ ...prev, captcha: '' }))
          }
        }
      }
    } catch {
      toast.error('登录失败')
      bumpCharacterError()
      if (captchaEnabled) {
        if (captchaType === 'behavioral') {
          setVerifyToken('')
        } else {
          loadCaptcha()
        }
      }
    } finally {
      setLoading(false)
    }
  }

  if (checkingInstall) {
    return <AuthAnimatedLoading variant="classic" />
  }

  return (
    <AuthAnimatedLayout
      variant="classic"
      title={installed ? '欢迎回来' : '初始化系统'}
      description={installed ? '登录到您的账户' : '请设置管理员账号'}
      usernameFocused={usernameFocused}
      passwordFocused={passwordFocused}
      showPassword={showPassword}
      username={formData.username}
      password={formData.password}
      errorNonce={characterErrorNonce}
    >
      <form
        onSubmit={installed ? handleLogin : handleInstall}
        className="space-y-0"
        noValidate
      >
        <div className={shell.formGroupClassic}>
          <label className={shell.labelClassic} htmlFor="username">
            {installed ? '用户名' : '管理员用户名'}
          </label>
          <div className={shell.inputWrap}>
            <input
              id="username"
              name="username"
              autoComplete="username"
              placeholder={installed ? '请输入用户名' : '请设置管理员用户名'}
              value={formData.username}
              disabled={loading}
              onFocus={() => setUsernameFocused(true)}
              onBlur={() => setUsernameFocused(false)}
              onChange={(e) =>
                setFormData({ ...formData, username: e.target.value })
              }
              className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
            />
          </div>
        </div>

        <div className={shell.formGroupClassic}>
          <label className={shell.labelClassic} htmlFor="password">
            {installed ? '密码' : '管理员密码'}
          </label>
          <div className={shell.inputWrap}>
            <input
              id="password"
              name="password"
              type={showPassword ? 'text' : 'password'}
              autoComplete={installed ? 'current-password' : 'new-password'}
              placeholder={installed ? '请输入密码' : '请设置管理员密码'}
              value={formData.password}
              disabled={loading}
              onFocus={() => setPasswordFocused(true)}
              onBlur={() => setPasswordFocused(false)}
              onChange={(e) =>
                setFormData({ ...formData, password: e.target.value })
              }
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

        {installed && captchaEnabled && captchaType !== 'behavioral' && (
          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="captcha">
              验证码
            </label>
            <div className={shell.captchaRow}>
              <div className={cn(shell.inputWrap, 'min-w-0 flex-1')}>
                <input
                  id="captcha"
                  name="captcha"
                  placeholder="请输入验证码"
                  value={formData.captcha}
                  disabled={loading}
                  onChange={(e) =>
                    setFormData({ ...formData, captcha: e.target.value })
                  }
                  className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
                />
              </div>
              <button
                type="button"
                onClick={loadCaptcha}
                disabled={captchaLoading}
                className={shell.captchaBtnClassic}
              >
                {captchaLoading ? (
                  <RefreshCw className="h-4 w-4 animate-spin text-neutral-500" />
                ) : captchaImage ? (
                  // eslint-disable-next-line @next/next/no-img-element -- 服务端返回的 base64 验证码
                  <img
                    src={captchaImage}
                    alt="验证码"
                    className="h-full w-full object-contain"
                  />
                ) : (
                  <span className="text-xs text-neutral-500">获取</span>
                )}
              </button>
            </div>
          </div>
        )}

        {installed && captchaEnabled && captchaType === 'behavioral' && (
          <div className={shell.formGroupClassic}>
            <div className="flex items-center gap-2">
              {verifyToken ? (
                <span className="text-sm text-green-600">验证已通过</span>
              ) : (
                <button
                  type="button"
                  onClick={() => setShowGoCaptcha(true)}
                  className="text-sm text-primary underline-offset-2 hover:underline"
                >
                  点击完成安全验证
                </button>
              )}
            </div>
          </div>
        )}

        {installed && needTotp && (
          <div className={shell.formGroupClassic}>
            <label className={shell.labelClassic} htmlFor="totp_code">
              动态口令
            </label>
            <div className={shell.inputWrap}>
              <input
                id="totp_code"
                name="totp_code"
                inputMode="numeric"
                autoComplete="one-time-code"
                placeholder="请输入6位动态口令"
                value={formData.totp_code}
                disabled={loading}
                maxLength={6}
                onChange={(e) =>
                  setFormData({ ...formData, totp_code: e.target.value })
                }
                className={cn(shell.inputClassic, shell.inputClassicNoToggle)}
              />
            </div>
          </div>
        )}

        <button
          type="submit"
          className={shell.btnPrimaryClassic}
          disabled={loading}
        >
          {loading ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" />
              {installed ? '登录中...' : '安装中...'}
            </>
          ) : installed ? (
            '登录'
          ) : (
            '开始安装'
          )}
        </button>

        {installed && (
          <div className={cn(shell.linkRowClassic, 'justify-center')}>
            <Link href="/magic-link/" className={shell.linkMutedClassic}>
              邮箱登录
            </Link>
          </div>
        )}

        {installed && needTotp && (
          <div className={cn(shell.linkRowClassic, 'justify-center')}>
            <Link href="/forgot-totp/" className={shell.linkMutedClassic}>
              无法使用动态口令？
            </Link>
          </div>
        )}
        {installed && <AuthFooterNav current="login" />}
      </form>
      {captchaType === 'behavioral' && (
        <Suspense fallback={null}>
          <GoCaptchaDialog
            open={showGoCaptcha}
            onClose={() => setShowGoCaptcha(false)}
            onSuccess={(token) => {
              setVerifyToken(token)
              setShowGoCaptcha(false)
            }}
          />
        </Suspense>
      )}
    </AuthAnimatedLayout>
  )
}
