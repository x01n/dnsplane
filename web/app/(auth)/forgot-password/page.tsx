'use client'

import { useState, useEffect, useRef, useCallback } from 'react'
import Link from 'next/link'
import { toast } from 'sonner'
import { Globe, Loader2, ArrowLeft, CheckCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { authApi } from '@/lib/api'
import { isValidUserEmail } from '@/lib/email'
import { TurnstileWidget } from '@/components/turnstile-widget'

export default function ForgotPasswordPage() {
  const [step, setStep] = useState(1) // 1=输入邮箱+验证码, 2=已发送
  const [loading, setLoading] = useState(false)
  const [email, setEmail] = useState('')
  const [code, setCode] = useState('')

  const [needTurnstile, setNeedTurnstile] = useState(false)
  const [turnstileSiteKey, setTurnstileSiteKey] = useState('')
  const [turnstileToken, setTurnstileToken] = useState('')
  const [turnstileRefresh, setTurnstileRefresh] = useState(0)

  const [codeSending, setCodeSending] = useState(false)
  const [countdown, setCountdown] = useState(0)
  const timerRef = useRef<NodeJS.Timeout | null>(null)

  useEffect(() => { return () => { if (timerRef.current) clearInterval(timerRef.current) } }, [])

  useEffect(() => {
    void (async () => {
      try {
        const res = await authApi.getConfig()
        if (res.code !== 0 || !res.data) return
        const d = res.data
        if (d.turnstile_standalone_required) {
          setNeedTurnstile(true)
          setTurnstileSiteKey(d.turnstile_site_key?.trim() || '')
        }
      } catch { /* ignore */ }
    })()
  }, [])

  const onTurnstileToken = useCallback((t: string) => {
    setTurnstileToken(t)
  }, [])

  const handleSendCode = async () => {
    if (!email) { toast.error('请输入邮箱'); return }
    if (!isValidUserEmail(email)) { toast.error('请输入有效的邮箱地址'); return }
    setCodeSending(true)
    try {
      const res = await authApi.sendCode(email, 'forgot_password')
      if (res.code === 0) {
        toast.success('验证码已发送')
        setCountdown(60)
        timerRef.current = setInterval(() => {
          setCountdown(prev => { if (prev <= 1) { clearInterval(timerRef.current!); return 0 }; return prev - 1 })
        }, 1000)
      } else toast.error(res.msg || '发送失败')
    } catch { toast.error('发送失败') }
    finally { setCodeSending(false) }
  }

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()
    if (!email || !code) { toast.error('请输入邮箱和验证码'); return }
    if (needTurnstile && !turnstileToken.trim()) {
      toast.error('请完成人机验证')
      return
    }
    setLoading(true)
    try {
      const res = await authApi.forgotPassword({
        email,
        code,
        ...(needTurnstile && turnstileToken ? { turnstile_token: turnstileToken } : {}),
      })
      if (res.code === 0) { setStep(2); toast.success('重置邮件已发送') }
      else {
        toast.error(res.msg || '发送失败')
        if (needTurnstile) setTurnstileRefresh((k) => k + 1)
      }
    } catch {
      toast.error('发送失败')
      if (needTurnstile) setTurnstileRefresh((k) => k + 1)
    }
    finally { setLoading(false) }
  }

  return (
    <div className="flex min-h-svh w-full items-center justify-center p-6 md:p-10">
      <div className="w-full max-w-sm">
        <div className="flex flex-col items-center gap-2 mb-6">
          <div className="flex h-10 w-10 items-center justify-center rounded-lg bg-primary text-primary-foreground"><Globe className="h-5 w-5" /></div>
          <h1 className="text-xl font-bold">DNSPlane</h1>
        </div>
        <Card>
          <CardHeader className="text-center">
            <CardTitle className="text-xl">找回密码</CardTitle>
            <CardDescription>输入邮箱和验证码来重置密码</CardDescription>
          </CardHeader>
          <CardContent>
            {step === 2 ? (
              <div className="text-center space-y-4">
                <CheckCircle className="h-12 w-12 text-green-500 mx-auto" />
                <p className="text-sm">重置链接已发送到 <strong>{email}</strong>，请查收邮件。</p>
                <p className="text-xs text-muted-foreground">链接有效期30分钟，如未收到请检查垃圾箱。</p>
                <Button variant="outline" className="w-full" onClick={() => { setStep(1); setCode('') }}>重新发送</Button>
                <Link href="/login"><Button variant="ghost" className="w-full"><ArrowLeft className="mr-2 h-4 w-4" />返回登录</Button></Link>
              </div>
            ) : (
              <form onSubmit={handleSubmit} className="space-y-4">
                <div className="space-y-2">
                  <Label>邮箱</Label>
                  <div className="flex gap-2">
                    <Input type="email" placeholder="注册时使用的邮箱" value={email} onChange={(e) => setEmail(e.target.value)} disabled={loading} className="flex-1" />
                    <Button type="button" variant="outline" size="sm" className="shrink-0 h-9 px-3 text-xs"
                      onClick={handleSendCode} disabled={codeSending || countdown > 0}>
                      {codeSending ? <Loader2 className="h-3 w-3 animate-spin" /> : countdown > 0 ? `${countdown}s` : '发送验证码'}
                    </Button>
                  </div>
                </div>
                <div className="space-y-2">
                  <Label>验证码</Label>
                  <Input placeholder="8位验证码" value={code} onChange={(e) => setCode(e.target.value.toUpperCase())} maxLength={8} className="font-mono tracking-widest" />
                </div>
                {needTurnstile && (
                  <TurnstileWidget
                    siteKey={turnstileSiteKey}
                    onToken={onTurnstileToken}
                    refreshSignal={turnstileRefresh}
                    disabled={loading}
                    label="安全验证（Turnstile）"
                  />
                )}
                <Button type="submit" className="w-full" disabled={loading}>
                  {loading && <Loader2 className="mr-2 h-4 w-4 animate-spin" />}发送重置链接
                </Button>
                <div className="text-center"><Link href="/login" className="text-sm text-muted-foreground hover:underline"><ArrowLeft className="inline mr-1 h-3 w-3" />返回登录</Link></div>
              </form>
            )}
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
