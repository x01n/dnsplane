'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Globe, Eye, EyeOff, RefreshCw, Loader2 } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { authApi, api } from '@/lib/api'

export default function LoginPage() {
  const router = useRouter()
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [captchaEnabled, setCaptchaEnabled] = useState(false)
  const [captchaId, setCaptchaId] = useState('')
  const [captchaImage, setCaptchaImage] = useState('')
  const [captchaLoading, setCaptchaLoading] = useState(false)
  const [needTotp, setNeedTotp] = useState(false)
  const [installed, setInstalled] = useState(true)
  const [checkingInstall, setCheckingInstall] = useState(true)

  const [formData, setFormData] = useState({
    username: '',
    password: '',
    captcha: '',
    totp_code: '',
  })

  // 检查安装状态
  useEffect(() => {
    checkInstallStatus()
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
        setCaptchaEnabled(res.data.captcha_enabled)
        if (res.data.captcha_enabled) {
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
      }
    } catch {
      toast.error('安装失败')
    } finally {
      setLoading(false)
    }
  }

  const handleLogin = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!formData.username || !formData.password) {
      toast.error('请输入用户名和密码')
      return
    }
    if (captchaEnabled && !formData.captcha) {
      toast.error('请输入验证码')
      return
    }
    if (needTotp && !formData.totp_code) {
      toast.error('请输入动态口令')
      return
    }

    setLoading(true)
    try {
      const res = await authApi.login({
        username: formData.username,
        password: formData.password,
        captcha_id: captchaId,
        captcha: formData.captcha,
        totp_code: formData.totp_code,
      })

      if (res.code === 0 && res.data) {
        api.setToken(res.data.token)
        toast.success('登录成功')
        router.push('/dashboard')
      } else if (res.code === 1001) {
        // 需要TOTP验证
        setNeedTotp(true)
        toast.info('请输入动态口令')
      } else {
        toast.error(res.msg || '登录失败')
        if (captchaEnabled) {
          loadCaptcha()
          setFormData(prev => ({ ...prev, captcha: '' }))
        }
      }
    } catch {
      toast.error('登录失败')
      if (captchaEnabled) {
        loadCaptcha()
      }
    } finally {
      setLoading(false)
    }
  }

  if (checkingInstall) {
    return (
      <div className="min-h-screen flex items-center justify-center">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-50 to-slate-100 p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-primary">
            <Globe className="h-6 w-6 text-primary-foreground" />
          </div>
          <CardTitle className="text-2xl">DNSPlane</CardTitle>
          <CardDescription>
            {installed ? '登录到您的账户' : '初始化系统'}
          </CardDescription>
        </CardHeader>
        <CardContent>
          <form onSubmit={installed ? handleLogin : handleInstall} className="space-y-4">
            <div className="space-y-2">
              <Label htmlFor="username">{installed ? '用户名' : '管理员用户名'}</Label>
              <Input
                id="username"
                placeholder={installed ? '请输入用户名' : '请设置管理员用户名'}
                value={formData.username}
                onChange={(e) => setFormData({ ...formData, username: e.target.value })}
                disabled={loading}
              />
            </div>

            <div className="space-y-2">
              <Label htmlFor="password">{installed ? '密码' : '管理员密码'}</Label>
              <div className="relative">
                <Input
                  id="password"
                  type={showPassword ? 'text' : 'password'}
                  placeholder={installed ? '请输入密码' : '请设置管理员密码'}
                  value={formData.password}
                  onChange={(e) => setFormData({ ...formData, password: e.target.value })}
                  disabled={loading}
                />
                <Button
                  type="button"
                  variant="ghost"
                  size="icon"
                  className="absolute right-0 top-0 h-full px-3 hover:bg-transparent"
                  onClick={() => setShowPassword(!showPassword)}
                >
                  {showPassword ? (
                    <EyeOff className="h-4 w-4 text-muted-foreground" />
                  ) : (
                    <Eye className="h-4 w-4 text-muted-foreground" />
                  )}
                </Button>
              </div>
            </div>

            {installed && captchaEnabled && (
              <div className="space-y-2">
                <Label htmlFor="captcha">验证码</Label>
                <div className="flex gap-2">
                  <Input
                    id="captcha"
                    placeholder="请输入验证码"
                    value={formData.captcha}
                    onChange={(e) => setFormData({ ...formData, captcha: e.target.value })}
                    disabled={loading}
                    className="flex-1"
                  />
                  <button
                    type="button"
                    onClick={loadCaptcha}
                    disabled={captchaLoading}
                    className="h-9 w-24 flex items-center justify-center border rounded-md overflow-hidden bg-white hover:opacity-80"
                  >
                    {captchaLoading ? (
                      <RefreshCw className="h-4 w-4 animate-spin" />
                    ) : captchaImage ? (
                      <img src={captchaImage} alt="验证码" className="h-full w-full object-contain" />
                    ) : (
                      <span className="text-xs text-muted-foreground">点击获取</span>
                    )}
                  </button>
                </div>
              </div>
            )}

            {installed && needTotp && (
              <div className="space-y-2">
                <Label htmlFor="totp_code">动态口令</Label>
                <Input
                  id="totp_code"
                  placeholder="请输入6位动态口令"
                  value={formData.totp_code}
                  onChange={(e) => setFormData({ ...formData, totp_code: e.target.value })}
                  disabled={loading}
                  maxLength={6}
                />
              </div>
            )}

            <Button type="submit" className="w-full" disabled={loading}>
              {loading ? (
                <>
                  <Loader2 className="mr-2 h-4 w-4 animate-spin" />
                  {installed ? '登录中...' : '安装中...'}
                </>
              ) : (
                installed ? '登录' : '开始安装'
              )}
            </Button>

            {installed && (
              <div className="flex justify-between text-sm">
                <Link href="/forgot-password" className="text-muted-foreground hover:text-primary">
                  忘记密码?
                </Link>
                {needTotp && (
                  <Link href="/forgot-password" className="text-muted-foreground hover:text-primary">
                    无法使用动态口令?
                  </Link>
                )}
              </div>
            )}
          </form>
        </CardContent>
      </Card>
    </div>
  )
}
