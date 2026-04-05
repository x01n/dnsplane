'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Globe, Loader2, Eye, EyeOff } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { authApi, api } from '@/lib/api'
import { isValidUserEmail } from '@/lib/email'
import { AuthCaptchaFields, type AuthCaptchaValue } from '@/components/auth-captcha-fields'

export default function RegisterPage() {
  const router = useRouter()
  const [loading, setLoading] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [username, setUsername] = useState('')
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')
  const [password, setPassword] = useState('')

  const [registerEnabled, setRegisterEnabled] = useState(true)
  const [captchaEnabled, setCaptchaEnabled] = useState(false)
  const [captchaType, setCaptchaType] = useState('image')
  const [captchaSiteKey, setCaptchaSiteKey] = useState<string | undefined>(undefined)
  const [captchaValue, setCaptchaValue] = useState<AuthCaptchaValue>({ captchaId: '', answer: '' })
  const [captchaRefresh, setCaptchaRefresh] = useState(0)

  const onCaptchaChange = useCallback((v: AuthCaptchaValue) => {
    setCaptchaValue(v)
  }, [])

  // 验证码倒计时
  const [codeSending, setCodeSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const timerRef = useRef<NodeJS.Timeout | null>(null)

  useEffect(() => {
    return () => { if (timerRef.current) clearInterval(timerRef.current) }
  }, [])

  useEffect(() => {
    void (async () => {
      try {
        const res = await authApi.getConfig()
        if (res.code !== 0 || !res.data) return
        const d = res.data
        setRegisterEnabled(!!d.register_enabled)
        setCaptchaEnabled(!!d.captcha_enabled)
        const ct = d.captcha_type || 'image'
        setCaptchaType(ct)
        const site =
          ct === 'turnstile'
            ? (d.captcha_site_key || d.turnstile_site_key || '')
            : (d.captcha_site_key || '')
        setCaptchaSiteKey(site || undefined)
      } catch { /* ignore */ }
    })()
  }, [])

  const handleSendCode = async () => {
    if (!email) { toast.error('请输入邮箱'); return }
    if (!isValidUserEmail(email)) { toast.error('请输入有效的邮箱地址'); return }
    setCodeSending(true)
    try {
      const res = await authApi.sendCode(email, 'register')
      if (res.code === 0) {
        toast.success('验证码已发送，请查收邮件')
        setCountdown(60)
        timerRef.current = setInterval(() => {
          setCountdown(prev => {
            if (prev <= 1) { clearInterval(timerRef.current!); return 0 }
            return prev - 1
          })
        }, 1000)
      } else {
        toast.error(res.msg || '发送失败')
      }
    } catch { toast.error('发送失败') }
    finally { setCodeSending(false) }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!registerEnabled) { toast.error('管理员已关闭注册'); return }
    if (!username || !email || !code || !password) { toast.error('请填写完整信息'); return }
    if (password.length < 6) { toast.error('密码至少6位'); return }

    if (captchaEnabled) {
      const t = captchaType || 'image'
      if (t === 'image') {
        if (!captchaValue.captchaId || !captchaValue.answer.trim()) {
          toast.error('请完成人机验证')
          return
        }
      } else if (!captchaValue.answer.trim()) {
        toast.error('请完成人机验证')
        return
      }
    }

    setLoading(true)
    try {
      const payload: Parameters<typeof authApi.register>[0] = { username, password, email, code }
      if (captchaEnabled) {
        const t = captchaType || 'image'
        if (t === 'image') {
          payload.captcha_id = captchaValue.captchaId
          payload.captcha = captchaValue.answer.trim()
        } else {
          payload.captcha = captchaValue.answer.trim()
        }
      }
      const res = await authApi.register(payload)
      if (res.code === 0 && res.data) {
        api.setTokens({ token: res.data.token, refresh_token: res.data.refresh_token })
        toast.success('注册成功')
        router.push('/dashboard')
      } else {
        toast.error(res.msg || '注册失败')
        setCaptchaRefresh((k) => k + 1)
      }
    } catch {
      toast.error('注册失败')
      setCaptchaRefresh((k) => k + 1)
    }
    finally { setLoading(false) }
  }

  return (
    <div className="flex min-h-svh w-full items-center justify-center p-6 md:p-10">
      <div className="w-full max-w-sm">
        <div className="flex flex-col items-center gap-2 mb-6">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary text-primary-foreground">
            <Globe className="h-5 w-5" />
          </div>
          <h1 className="text-xl font-bold">DNSPlane</h1>
        </div>

        <Card>
          <CardHeader className="text-center">
            <CardTitle className="text-xl">创建账户</CardTitle>
            <CardDescription>注册一个新的 DNSPlane 账户</CardDescription>
          </CardHeader>
          <CardContent>
            {!registerEnabled && (
              <p className="text-sm text-destructive mb-4">当前未开放新用户注册，如有需要请联系管理员。</p>
            )}
            <form onSubmit={handleSubmit} className="space-y-4">
              <div className="space-y-2">
                <Label htmlFor="username">用户名</Label>
                <Input id="username" placeholder="请输入用户名" value={username} onChange={(e) => setUsername(e.target.value)} disabled={loading || !registerEnabled} />
              </div>

              <div className="space-y-2">
                <Label htmlFor="email">邮箱</Label>
                <div className="flex gap-2">
                  <Input id="email" type="email" placeholder="请输入邮箱" value={email} onChange={(e) => setEmail(e.target.value)} disabled={loading || !registerEnabled} className="flex-1" />
                  <Button type="button" variant="outline" size="sm" className="shrink-0 h-9 px-3 text-xs"
                    onClick={handleSendCode} disabled={codeSending || countdown > 0 || loading || !registerEnabled}>
                    {codeSending ? <Loader2 className="h-3 w-3 animate-spin" /> : countdown > 0 ? `${countdown}s` : '发送验证码'}
                  </Button>
                </div>
              </div>

              <div className="space-y-2">
                <Label htmlFor="code">验证码</Label>
                <Input id="code" placeholder="输入邮箱收到的8位验证码" value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} maxLength={8} disabled={loading || !registerEnabled} className="font-mono tracking-widest" />
              </div>

              {registerEnabled && captchaEnabled && (
                <AuthCaptchaFields
                  enabled={captchaEnabled}
                  captchaType={captchaType}
                  siteKey={captchaSiteKey}
                  refreshSignal={captchaRefresh}
                  onChange={onCaptchaChange}
                  disabled={loading}
                />
              )}

              <div className="space-y-2">
                <Label htmlFor="password">密码</Label>
                <div className="relative">
                  <Input id="password" type={showPassword ? 'text' : 'password'} placeholder="至少6位" value={password} onChange={(e) => setPassword(e.target.value)} disabled={loading || !registerEnabled} className="pr-10" />
                  <Button type="button" variant="ghost" size="icon" className="absolute right-0 top-0 h-full w-10 hover:bg-transparent" onClick={() => setShowPassword(!showPassword)} tabIndex={-1} disabled={loading || !registerEnabled}>
                    {showPassword ? <EyeOff className="h-4 w-4 text-muted-foreground" /> : <Eye className="h-4 w-4 text-muted-foreground" />}
                  </Button>
                </div>
              </div>

              <Button type="submit" className="w-full" disabled={loading || !registerEnabled}>
                {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}
                注册
              </Button>
            </form>

            <div className="mt-4 text-center text-sm">
              已有账号?{' '}
              <Link href="/login" className="underline underline-offset-4 hover:text-primary">去登录</Link>
            </div>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
