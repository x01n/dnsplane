'use client'

import { useState, useEffect, lazy, Suspense } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Eye, EyeOff, RefreshCw, Loader2, AlertCircle } from 'lucide-react'
import { cn } from '@/lib/utils'
import { authApi, api, oauthApi, OAuthProvider } from '@/lib/api'
import { OAUTH_CALLBACK_ERRORS } from '@/lib/oauth-callback'
import { isValidSiteKey } from '@/lib/site-key'
import {
  AuthAnimatedLayout,
} from '@/components/auth/auth-animated-layout'
import { AuthFormSkeleton } from '@/components/auth/auth-form-skeleton'
import { AuthFooterNav } from '@/components/auth/auth-footer-nav'
import { AuthCaptchaFields } from '@/components/auth-captcha-fields'
import shell from '@/components/auth/animated-login-shell.module.css'

const GoCaptchaDialog = lazy(() => import('@/components/go-captcha-dialog'))

/**
 * 表单元素的逐项上浮延迟：50ms 起、每项 45ms 递增，
 * 最后一项约 0.36s 落位，与卡片入场动画（0.46s）在时间轴上重叠而不排队
 */
const revealDelay = (index: number) => ({ '--reveal-delay': `${50 + index * 45}ms` }) as React.CSSProperties

/**
 * 内置行为验证码的类型值
 *
 * 后端 handler.Login 的分派同时接受 behavioral 与 go-captcha 两个拼法，
 * 前端若只认前者，历史数据的 go-captcha 会被当成图形验证码渲染，
 * 输入框永远取不到图、提交也拿不到 verify_token
 *
 * @param captchaType captcha_type 配置值
 * @returns 是否走内置行为验证码
 */
function isBuiltinBehavioralCaptcha(captchaType: string): boolean {
  return captchaType === 'behavioral' || captchaType === 'go-captcha'
}

/**
 * 需要第三方组件产出 token 的验证码类型
 * 与后端 notify.IsThirdPartyCaptchaType 支持集合一致（turnstile 走独立密钥与校验入口）
 *
 * @param captchaType captcha_type 配置值
 * @returns 是否为 turnstile / recaptcha / hcaptcha
 */
function isThirdPartyCaptchaType(captchaType: string): boolean {
  return captchaType === 'turnstile' || captchaType === 'recaptcha' || captchaType === 'hcaptcha'
}

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
  /**
   * 可用的第三方登录来源
   *
   * 只取后端「已配置凭据」的提供商列表。管理员是否允许该来源登录不在此处判定：
   * 那个开关（auth_oauth_<provider>_login）只有管理员才读得到，
   * 登录页在未登录上下文里取不到它，取不到就隐藏会让入口对所有人生效为零
   */
  const [oauthProviders, setOauthProviders] = useState<OAuthProvider[]>([])

  /**
   * 第三方验证码（turnstile / recaptcha / hcaptcha）的站点密钥
   *
   * 后端按 captcha_type 收起：turnstile 读 turnstile_site_key，
   * recaptcha / hcaptcha 读 captcha_site_key，两把并行不可混用
   */
  const [captchaSiteKey, setCaptchaSiteKey] = useState('')
  /** 第三方验证码组件产出的 token，提交时按类型分走 turnstile_token 或 captcha_token */
  const [captchaToken, setCaptchaToken] = useState('')
  const [captchaRefreshSignal, setCaptchaRefreshSignal] = useState(0)

  const [usernameFocused, setUsernameFocused] = useState(false)
  const [passwordFocused, setPasswordFocused] = useState(false)
  const [characterErrorNonce, setCharacterErrorNonce] = useState(0)
  /** 最近一次失败原因，展示在表单顶部；toast 会消失，这条不会 */
  const [formError, setFormError] = useState('')

  const [formData, setFormData] = useState({
    username: '',
    password: '',
    captcha: '',
    totp_code: '',
  })

  const bumpCharacterError = () => {
    setCharacterErrorNonce((n) => n + 1)
  }

  /** 同时推送 toast 与表单内联错误，两处口径始终一致 */
  const failWith = (message: string) => {
    setFormError(message)
    toast.error(message)
    bumpCharacterError()
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
    const message = OAUTH_CALLBACK_ERRORS[err] || `操作失败：${err}`
    setFormError(message)
    toast.error(message)
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
          loadOAuthProviders()
        }
      }
    } catch {
      // ignore
    } finally {
      setCheckingInstall(false)
    }
  }

  /**
   * 读取可用的第三方登录来源
   *
   * 两个请求都失败时保持空列表，入口整块不渲染——登录页不应为了
   * 一个可选的第三方入口向用户报错，凭据未配置本就是默认状态
   */
  const loadOAuthProviders = async () => {
    try {
      const res = await oauthApi.getProviders()
      if (res.code !== 0) return
      setOauthProviders(Array.isArray(res.data) ? res.data : [])
    } catch {
      // ignore
    }
  }

  const checkAuthConfig = async () => {
    try {
      const res = await authApi.getConfig()
      if (res.code === 0 && res.data) {
        const needCaptcha = !!(res.data.login_captcha ?? res.data.captcha_enabled)
        setCaptchaEnabled(needCaptcha)
        const type = res.data.captcha_type || 'image'
        setCaptchaType(type)
        // 站点密钥按类型取：turnstile 与另两家是并行的两把键，
        // 拿错 key 去初始化组件必然失败，故此处与后端 GetAuthConfig 的收起规则逐条对齐
        setCaptchaSiteKey(
          type === 'turnstile'
            ? res.data.turnstile_site_key || ''
            : type === 'recaptcha' || type === 'hcaptcha'
              ? res.data.captcha_site_key || ''
              : ''
        )
        if (needCaptcha && !isThirdPartyCaptchaType(type) && !isBuiltinBehavioralCaptcha(type)) {
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
      failWith('请输入管理员用户名和密码')
      return
    }
    setLoading(true)
    setFormError('')
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
        failWith(res.msg || '安装失败')
      }
    } catch {
      failWith('安装失败')
    } finally {
      setLoading(false)
    }
  }

  /** 当前类型是否为需要第三方组件产 token 的三类之一 */
  const isThirdPartyCaptcha = isThirdPartyCaptchaType(captchaType)
  /** 当前类型走内置行为验证码（behavioral / go-captcha） */
  const isBehavioralCaptcha = isBuiltinBehavioralCaptcha(captchaType)
  /** 当前类型走内置图形验证码 */
  const isImageCaptcha = !isThirdPartyCaptcha && !isBehavioralCaptcha
  /**
   * 第三方验证码的站点密钥是否可用于渲染组件
   *
   * 站点密钥非空但格式非法时要按「配置损坏」处理而非「未配置」，
   * 两者都要显式报错，不能静默放行 —— 后端在私钥已配置时会拒绝无 token 的提交
   */
  const thirdPartySiteKeyValid = isValidSiteKey(captchaSiteKey)

  /**
   * 实际可用的第三方登录来源
   *
   * 管理员关闭某来源时由后端 OAuthLogin 拒绝并回跳本页，前端不重复判一遍开关：
   * 那个开关只有管理员读得到，前端读不到就只能全部隐藏，反而让功能失效
   */
  const availableOAuthProviders = installed ? oauthProviders.filter((p) => p.enabled) : []

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!formData.username || !formData.password) {
      failWith('请输入用户名和密码')
      return
    }
    if (captchaEnabled && isBehavioralCaptcha && !verifyToken) {
      setShowGoCaptcha(true)
      return
    }
    // 第三方类型：组件能否渲染取决于站点密钥，密钥不可用时提交必然被服务端拒绝，
    // 在此拦截并给出可操作提示，而不是放行后让用户看到无法理解的错误
    if (captchaEnabled && isThirdPartyCaptcha) {
      if (!thirdPartySiteKeyValid) {
        failWith('管理员已开启第三方验证码但站点密钥不可用，请联系管理员检查系统设置')
        return
      }
      if (!captchaToken) {
        failWith('请先完成人机验证')
        return
      }
    }
    if (captchaEnabled && isImageCaptcha && !formData.captcha) {
      failWith('请输入验证码')
      return
    }
    if (needTotp && !formData.totp_code) {
      failWith('请输入动态口令')
      return
    }

    setLoading(true)
    setFormError('')
    try {
      const res = await authApi.login({
        username: formData.username,
        password: formData.password,
        captcha_id: captchaId,
        captcha: formData.captcha,
        verify_token: verifyToken,
        totp_code: formData.totp_code,
        // 两家第三方类型的 token 字段名不同：turnstile 走 turnstile_token，
        // recaptcha / hcaptcha 走 captcha_token，对应后端 LoginRequest 内嵌的两个结构
        ...(captchaEnabled && captchaType === 'turnstile' ? { turnstile_token: captchaToken } : {}),
        ...(captchaEnabled && isThirdPartyCaptcha && captchaType !== 'turnstile'
          ? { captcha_token: captchaToken }
          : {}),
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
        failWith(res.msg || '登录失败')
        if (captchaEnabled) {
          if (isBehavioralCaptcha) {
            setVerifyToken('')
          } else if (isThirdPartyCaptcha) {
            // 与服务端判定口径一致：第三方 token 一次性，失败后必须重取
            setCaptchaToken('')
            setCaptchaRefreshSignal((n) => n + 1)
          } else {
            loadCaptcha()
            setFormData((prev) => ({ ...prev, captcha: '' }))
          }
        }
      }
    } catch {
      failWith('登录失败')
      if (captchaEnabled) {
        if (isBehavioralCaptcha) {
          setVerifyToken('')
        } else if (isThirdPartyCaptcha) {
          setCaptchaToken('')
          setCaptchaRefreshSignal((n) => n + 1)
        } else {
          loadCaptcha()
        }
      }
    } finally {
      setLoading(false)
    }
  }

  if (checkingInstall) {
    return <AuthFormSkeleton title="正在加载" description="正在检查系统状态" />
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
        {formError && (
          <div className={cn(shell.errorMsg, shell.errorMsgVisible)} role="alert">
            <AlertCircle className={shell.errorMsgIcon} aria-hidden />
            <span className={shell.errorMsgText}>{formError}</span>
          </div>
        )}

        <div className={cn(shell.formGroupClassic, shell.revealItem)} style={revealDelay(0)}>
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

        <div className={cn(shell.formGroupClassic, shell.revealItem)} style={revealDelay(1)}>
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

        {installed && captchaEnabled && isImageCaptcha && (
          <div className={cn(shell.formGroupClassic, shell.revealItem)} style={revealDelay(2)}>
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

        {installed && captchaEnabled && isBehavioralCaptcha && (
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

        {installed && captchaEnabled && isThirdPartyCaptcha && (
          <div className={cn(shell.formGroupClassic, shell.revealItem)} style={revealDelay(2)}>
            {thirdPartySiteKeyValid ? (
              <AuthCaptchaFields
                enabled={captchaEnabled}
                captchaType={captchaType}
                siteKey={captchaSiteKey}
                refreshSignal={captchaRefreshSignal}
                onChange={(v) => setCaptchaToken(v.answer)}
                disabled={loading}
              />
            ) : (
              <div
                role="alert"
                className="flex gap-3 rounded-lg border border-destructive/35 bg-destructive/5 p-3"
              >
                <AlertCircle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" aria-hidden />
                <div className="space-y-1 text-sm">
                  <p className="font-medium text-destructive">人机验证配置不完整</p>
                  <p className="text-destructive/90 leading-snug">
                    管理员已开启登录验证码但{captchaSiteKey ? '站点密钥格式不合法' : '未配置站点密钥'}，
                    验证组件无法加载。请联系管理员在系统设置的「登录安全设置」中检查
                    {captchaType === 'turnstile' ? ' Turnstile 站点密钥' : ' 主验证码站点密钥'}。
                  </p>
                  <button
                    type="button"
                    onClick={() => checkAuthConfig()}
                    className="inline-flex items-center gap-1.5 rounded-md border border-destructive/40 px-2.5 py-1 text-xs font-medium text-destructive transition-colors hover:bg-destructive/10"
                  >
                    <RefreshCw className="h-3 w-3" aria-hidden />
                    重新加载配置
                  </button>
                </div>
              </div>
            )}
          </div>
        )}

        {installed && needTotp && (
          <div className={cn(shell.formGroupClassic, shell.revealItem)} style={revealDelay(3)}>
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
          className={cn(shell.btnPrimaryClassic, shell.revealItem)}
          style={revealDelay(4)}
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

        {installed && availableOAuthProviders.length > 0 && (
          <div className={shell.formGroupClassic}>
            <div className="flex items-center justify-center gap-4" aria-hidden>
              <span className="h-px w-full bg-neutral-200" />
              <span className="shrink-0 text-xs text-neutral-500">第三方账号登录</span>
              <span className="h-px w-full bg-neutral-200" />
            </div>
            <div className={cn(shell.linkRowClassic, 'justify-center')}>
              {availableOAuthProviders.map((provider) => (
                /* 必须是整页跳转：该地址由后端 302 到第三方授权页，走客户端路由到不了 */
                <a
                  key={provider.name}
                  href={`/api/auth/oauth/${provider.name}/login`}
                  className={shell.linkMutedClassic}
                >
                  {provider.display_name || provider.name}
                </a>
              ))}
            </div>
          </div>
        )}

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
      {isBehavioralCaptcha && (
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
