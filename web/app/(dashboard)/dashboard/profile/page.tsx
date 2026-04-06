'use client'

import { useState, useEffect, useRef, useMemo, useCallback } from 'react'
import { useRouter } from 'next/navigation'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Separator } from '@/components/ui/separator'
import { Badge } from '@/components/ui/badge'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { AlertDialog, AlertDialogAction, AlertDialogCancel, AlertDialogContent, AlertDialogDescription, AlertDialogFooter, AlertDialogHeader, AlertDialogTitle } from '@/components/ui/alert-dialog'
import { toast } from 'sonner'
import { authApi, totpApi, oauthApi, userApi, User, OAuthBinding, OAuthProvider } from '@/lib/api'
import { Shield, Key, Mail, RefreshCw, ShieldCheck, ShieldOff, QrCode, Copy, CheckCircle, AlertTriangle, LinkIcon, Unlink, Loader2, Github, Globe, Lock, MessageCircle, Building2, RefreshCcw, Ticket } from 'lucide-react'
import { isValidUserEmail } from '@/lib/email'
import { OAUTH_CALLBACK_ERRORS } from '@/lib/oauth-callback'
import QRCode from 'qrcode'

const PROVIDER_META: Record<string, { icon: typeof Globe; label: string; tint: string }> = {
  github: { icon: Github, label: 'GitHub', tint: 'bg-zinc-100 dark:bg-zinc-800 text-zinc-700 dark:text-zinc-200' },
  google: { icon: Globe, label: 'Google', tint: 'bg-blue-50 dark:bg-blue-950 text-blue-600' },
  wechat: { icon: MessageCircle, label: '微信', tint: 'bg-emerald-50 dark:bg-emerald-950 text-emerald-600' },
  dingtalk: { icon: Building2, label: '钉钉', tint: 'bg-sky-50 dark:bg-sky-950 text-sky-600' },
  custom: { icon: Key, label: '自定义 OAuth2', tint: 'bg-violet-50 dark:bg-violet-950 text-violet-600' },
}

/** 账户绑定页始终展示顺序（与产品说明一致），未在后台启用的项显示为「未开放」 */
const CANONICAL_OAUTH = ['github', 'wechat', 'dingtalk', 'custom'] as const

const PROFILE_TAB_STORAGE_KEY = 'dnsplane.profile.activeTab'

export default function ProfilePage() {
  const router = useRouter()
  const [user, setUser] = useState<User | null>(null)
  const [loading, setLoading] = useState(true)

  // OAuth
  const [oauthBindings, setOauthBindings] = useState<OAuthBinding[]>([])
  const [oauthProviders, setOauthProviders] = useState<OAuthProvider[]>([])
  const [oauthLoading, setOauthLoading] = useState(false)

  // Password
  const [showPasswordDialog, setShowPasswordDialog] = useState(false)
  const [passwordForm, setPasswordForm] = useState({ old_password: '', new_password: '', confirm_password: '' })
  const [changingPassword, setChangingPassword] = useState(false)

  // TOTP
  const [showTotpDialog, setShowTotpDialog] = useState(false)
  const [showDisableTotpDialog, setShowDisableTotpDialog] = useState(false)
  const [showRecoveryCodesDialog, setShowRecoveryCodesDialog] = useState(false)
  const [recoveryCodesKind, setRecoveryCodesKind] = useState<'first' | 'regen'>('first')
  const [recoveryCodes, setRecoveryCodes] = useState<string[]>([])
  const [showRegenRecoveryDialog, setShowRegenRecoveryDialog] = useState(false)
  const [regenForm, setRegenForm] = useState({ password: '', code: '' })
  const [totpData, setTotpData] = useState<{ secret: string; uri: string } | null>(null)
  const [totpQrCode, setTotpQrCode] = useState('')
  const [totpCode, setTotpCode] = useState('')
  const [disableForm, setDisableForm] = useState({ password: '', code: '' })
  const [submitting, setSubmitting] = useState(false)

  // Email binding
  const [showEmailDialog, setShowEmailDialog] = useState(false)
  const [emailForm, setEmailForm] = useState({ email: '', code: '' })
  const [emailCountdown, setEmailCountdown] = useState(0)
  const emailTimerRef = useRef<NodeJS.Timeout | null>(null)

  const [showResetApiKeyDialog, setShowResetApiKeyDialog] = useState(false)
  const [apiKeyResetting, setApiKeyResetting] = useState(false)

  const [profileTab, setProfileTab] = useState<'bind' | 'security'>('bind')

  useEffect(() => {
    loadUser()
    loadOAuth()
  }, [])

  useEffect(() => {
    try {
      const raw = localStorage.getItem(PROFILE_TAB_STORAGE_KEY)
      if (raw === 'bind' || raw === 'security') setProfileTab(raw)
    } catch { /* ignore */ }
  }, [])

  const onProfileTabChange = useCallback((v: string) => {
    if (v !== 'bind' && v !== 'security') return
    setProfileTab(v)
    try {
      localStorage.setItem(PROFILE_TAB_STORAGE_KEY, v)
    } catch { /* ignore */ }
  }, [])

  useEffect(() => {
    return () => {
      if (emailTimerRef.current) clearInterval(emailTimerRef.current)
    }
  }, [])

  const oauthDisplayRows = useMemo(() => {
    type Row = { name: string; displayName: string; enabled: boolean; binding?: OAuthBinding }
    const rows: Row[] = []
    for (const name of CANONICAL_OAUTH) {
      const prov = oauthProviders.find((p) => p.name === name)
      const binding = oauthBindings.find((b) => b.provider === name)
      const meta = PROVIDER_META[name]
      rows.push({
        name,
        displayName: prov?.display_name ?? meta.label,
        enabled: !!prov,
        binding,
      })
    }
    for (const prov of oauthProviders) {
      if ((CANONICAL_OAUTH as readonly string[]).includes(prov.name)) continue
      rows.push({
        name: prov.name,
        displayName: prov.display_name,
        enabled: true,
        binding: oauthBindings.find((b) => b.provider === prov.name),
      })
    }
    const listed = new Set(rows.map((r) => r.name))
    for (const b of oauthBindings) {
      if (listed.has(b.provider)) continue
      rows.push({
        name: b.provider,
        displayName: b.provider,
        enabled: false,
        binding: b,
      })
    }
    return rows
  }, [oauthProviders, oauthBindings])

  const openRegenRecoveryDialog = useCallback(() => {
    setRegenForm({ password: '', code: '' })
    setShowRegenRecoveryDialog(true)
  }, [])

  const loadUser = async () => {
    setLoading(true)
    try {
      const res = await authApi.getUserInfo()
      if (res.code === 0 && res.data) setUser(res.data)
    } catch { toast.error('加载用户信息失败') }
    finally { setLoading(false) }
  }

  const loadOAuth = async () => {
    setOauthLoading(true)
    try {
      const [bindRes, provRes] = await Promise.all([
        oauthApi.getBindings(),
        oauthApi.getProviders(),
      ])
      if (bindRes.code === 0 && bindRes.data) setOauthBindings(Array.isArray(bindRes.data) ? bindRes.data : [])
      if (provRes.code === 0 && provRes.data) setOauthProviders(Array.isArray(provRes.data) ? provRes.data : [])
    } catch { /* ignore */ }
    finally { setOauthLoading(false) }
  }

  // OAuth 绑定完成回调：/dashboard/profile?bind=success 或 ?error=...
  useEffect(() => {
    if (typeof window === 'undefined') return
    const params = new URLSearchParams(window.location.search)
    const bind = params.get('bind')
    const err = params.get('error')
    if (bind !== 'success' && !err) return

    if (bind === 'success') {
      toast.success('第三方账号绑定成功')
      void (async () => {
        try {
          const bindRes = await oauthApi.getBindings()
          if (bindRes.code === 0 && bindRes.data) {
            setOauthBindings(Array.isArray(bindRes.data) ? bindRes.data : [])
          }
        } catch { /* ignore */ }
      })()
    }
    if (err) {
      toast.error(OAUTH_CALLBACK_ERRORS[err] || '操作失败: ' + err)
    }
    router.replace('/dashboard/profile', { scroll: false })
  }, [router])

  // ===== OAuth Bind/Unbind =====
  const handleBind = async (provider: string) => {
    try {
      const res = await oauthApi.getBindURL(provider)
      if (res.code === 0 && res.data?.url) window.location.href = res.data.url
      else toast.error(res.msg || '获取绑定链接失败')
    } catch { toast.error('获取绑定链接失败') }
  }

  const handleUnbind = async (provider: string) => {
    try {
      const res = await oauthApi.unbind(provider)
      if (res.code === 0) { toast.success('解绑成功'); loadOAuth() }
      else toast.error(res.msg || '解绑失败')
    } catch { toast.error('解绑失败') }
  }

  // ===== Password =====
  const handleChangePassword = async () => {
    if (!passwordForm.old_password || !passwordForm.new_password) { toast.error('请填写完整'); return }
    if (passwordForm.new_password !== passwordForm.confirm_password) { toast.error('两次密码不一致'); return }
    if (passwordForm.new_password.length < 6) { toast.error('新密码至少6位'); return }
    setChangingPassword(true)
    try {
      const res = await authApi.changePassword({ old_password: passwordForm.old_password, new_password: passwordForm.new_password })
      if (res.code === 0) { toast.success('密码修改成功'); setShowPasswordDialog(false); setPasswordForm({ old_password: '', new_password: '', confirm_password: '' }) }
      else toast.error(res.msg || '修改失败')
    } catch { toast.error('修改失败') }
    finally { setChangingPassword(false) }
  }

  // ===== TOTP =====
  const handleEnableTOTP = async () => {
    setSubmitting(true)
    try {
      const res = await totpApi.enable()
      if (res.code === 0 && res.data) {
        const uri = res.data.uri || `otpauth://totp/DNSPlane:${user?.username}?secret=${res.data.secret}&issuer=DNSPlane`
        setTotpData({ secret: res.data.secret, uri })
        setTotpQrCode(await QRCode.toDataURL(uri))
        setShowTotpDialog(true)
      } else toast.error(res.msg || '获取密钥失败')
    } catch { toast.error('获取密钥失败') }
    finally { setSubmitting(false) }
  }

  const handleVerifyTOTP = async () => {
    if (!totpCode || totpCode.length !== 6) { toast.error('请输入6位验证码'); return }
    setSubmitting(true)
    try {
      const res = await totpApi.verify(totpCode) as { code: number; msg?: string; data?: { recovery_codes?: string[] } }
      if (res.code === 0) {
        toast.success('二步验证已启用'); setShowTotpDialog(false); setTotpCode(''); setTotpData(null)
        if (res.data?.recovery_codes?.length) {
          setRecoveryCodesKind('first')
          setRecoveryCodes(res.data.recovery_codes)
          setShowRecoveryCodesDialog(true)
        }
        loadUser()
      } else toast.error(res.msg || '验证码错误')
    } catch { toast.error('验证失败') }
    finally { setSubmitting(false) }
  }

  const handleRegenerateRecovery = async () => {
    if (!regenForm.password || regenForm.code.length !== 6) { toast.error('请输入密码和 6 位动态码'); return }
    setSubmitting(true)
    try {
      const res = await totpApi.regenerateRecovery(regenForm.password, regenForm.code) as { code: number; msg?: string; data?: { recovery_codes?: string[] } }
      if (res.code === 0 && res.data?.recovery_codes?.length) {
        toast.success('恢复码已重新生成')
        setShowRegenRecoveryDialog(false)
        setRegenForm({ password: '', code: '' })
        setRecoveryCodesKind('regen')
        setRecoveryCodes(res.data.recovery_codes)
        setShowRecoveryCodesDialog(true)
      } else toast.error(res.msg || '操作失败')
    } catch { toast.error('操作失败') }
    finally { setSubmitting(false) }
  }

  const handleDisableTOTP = async () => {
    if (!disableForm.password || !disableForm.code) { toast.error('请填写密码和验证码'); return }
    setSubmitting(true)
    try {
      const res = await totpApi.disable(disableForm.password, disableForm.code)
      if (res.code === 0) { toast.success('二步验证已关闭'); setShowDisableTotpDialog(false); setDisableForm({ password: '', code: '' }); loadUser() }
      else toast.error(res.msg || '关闭失败')
    } catch { toast.error('关闭失败') }
    finally { setSubmitting(false) }
  }

  // ===== Email Binding =====
  const clearEmailBindTimer = () => {
    if (emailTimerRef.current) {
      clearInterval(emailTimerRef.current)
      emailTimerRef.current = null
    }
  }

  const handleSendEmailCode = () => {
    if (!emailForm.email) { toast.error('请输入邮箱'); return }
    if (!isValidUserEmail(emailForm.email)) { toast.error('请输入有效的邮箱地址'); return }
    clearEmailBindTimer()
    setEmailCountdown(60)
    emailTimerRef.current = setInterval(() => {
      setEmailCountdown((prev) => {
        if (prev <= 1) {
          clearEmailBindTimer()
          return 0
        }
        return prev - 1
      })
    }, 1000)
    toast.info('已提交发送请求')
    void authApi
      .sendCode(emailForm.email, 'bindmail')
      .then((res) => {
        if (res.code !== 0) {
          clearEmailBindTimer()
          setEmailCountdown(0)
          toast.error(res.msg || '发送失败')
        }
      })
      .catch((e) => {
        if (e instanceof Error && e.name === 'AbortError') return
        clearEmailBindTimer()
        setEmailCountdown(0)
        toast.error('发送失败')
      })
  }

  const handleBindEmail = async () => {
    if (!emailForm.email || !emailForm.code) { toast.error('请输入邮箱和验证码'); return }
    if (!isValidUserEmail(emailForm.email)) { toast.error('请输入有效的邮箱地址'); return }
    setSubmitting(true)
    try {
      const res = await authApi.bindEmail(emailForm.email, emailForm.code)
      if (res.code === 0) {
        toast.success('邮箱绑定成功')
        setShowEmailDialog(false)
        setEmailForm({ email: '', code: '' })
        loadUser()
      } else toast.error(res.msg || '绑定失败')
    } catch { toast.error('绑定失败') }
    finally { setSubmitting(false) }
  }

  const copyToClipboard = (text: string) => { navigator.clipboard.writeText(text); toast.success('已复制') }

  const handleResetApiKey = async () => {
    if (!user?.id) return
    setApiKeyResetting(true)
    try {
      const res = await userApi.resetAPIKey(user.id)
      if (res.code === 0 && res.data && typeof res.data === 'object' && 'api_key' in res.data) {
        const key = (res.data as { api_key: string }).api_key
        setUser({ ...user, api_key: key })
        toast.success('访问令牌已重新生成')
        setShowResetApiKeyDialog(false)
      } else toast.error(res.msg || '重置失败')
    } catch { toast.error('重置失败') }
    finally { setApiKeyResetting(false) }
  }

  if (loading) return <div className="flex items-center justify-center h-[400px]"><RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" /></div>

  return (
    <div className="space-y-6 max-w-3xl mx-auto">
      <Card className="border-border/80 shadow-sm">
        <CardContent className="flex items-center gap-4 pt-6">
          <div className="h-16 w-16 rounded-full bg-gradient-to-br from-violet-500 to-purple-600 flex items-center justify-center text-white text-2xl font-bold shadow-md shrink-0">
            {user?.username?.charAt(0).toUpperCase()}
          </div>
          <div className="min-w-0">
            <h1 className="text-2xl font-bold tracking-tight truncate">{user?.username}</h1>
            <div className="flex flex-wrap items-center gap-2 mt-2">
              <Badge className={user?.level === 2 ? 'bg-blue-500' : 'bg-gray-500'}>{user?.level === 2 ? '管理员' : '普通用户'}</Badge>
              {user?.is_api && (
                <Badge variant="outline" className="text-purple-600 border-purple-500/60">
                  <Key className="h-3 w-3 mr-1" />
                  API 访问
                </Badge>
              )}
            </div>
          </div>
        </CardContent>
      </Card>

      <Tabs value={profileTab} onValueChange={onProfileTabChange} className="w-full">
        <TabsList className="grid w-full grid-cols-2 h-10">
          <TabsTrigger value="bind">账户绑定</TabsTrigger>
          <TabsTrigger value="security">安全设置</TabsTrigger>
        </TabsList>

        <TabsContent value="bind" className="mt-4 focus-visible:outline-none">
          <Card className="border-border/80 shadow-sm overflow-hidden">
            <CardHeader className="border-b bg-muted/30">
              <CardTitle className="text-base flex items-center gap-2">
                <LinkIcon className="h-4 w-4 text-primary" />
                账户绑定
              </CardTitle>
              <CardDescription>邮箱与各登录方式的绑定状态</CardDescription>
            </CardHeader>
            <CardContent className="p-0">
              {oauthLoading ? (
                <div className="flex justify-center py-12"><Loader2 className="h-6 w-6 animate-spin text-muted-foreground" /></div>
              ) : (
                <div className="divide-y">
                  <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-4">
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="h-10 w-10 rounded-xl bg-blue-50 dark:bg-blue-950 flex items-center justify-center shrink-0">
                        <Mail className="h-5 w-5 text-blue-600" />
                      </div>
                      <div className="min-w-0">
                        <p className="font-medium text-sm">邮箱</p>
                        <p className="text-xs text-muted-foreground truncate">{user?.email || '未绑定邮箱'}</p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2 shrink-0">
                      <Badge variant={user?.email ? 'default' : 'secondary'} className={user?.email ? 'bg-emerald-600 hover:bg-emerald-600' : ''}>
                        {user?.email ? '已绑定' : '未绑定'}
                      </Badge>
                      <Button variant="outline" size="sm" className="h-8" onClick={() => { setEmailForm({ email: user?.email || '', code: '' }); setShowEmailDialog(true) }}>
                        {user?.email ? '修改' : '绑定'}
                      </Button>
                    </div>
                  </div>

                  {oauthDisplayRows.map((row) => {
                    const meta = PROVIDER_META[row.name] || {
                      icon: Globe,
                      label: row.displayName,
                      tint: 'bg-muted text-muted-foreground',
                    }
                    const Icon = meta.icon
                    const binding = row.binding
                    const sub = binding
                      ? !row.enabled
                        ? `${binding.provider_name || binding.provider_user_id}（登录方式已关闭，可解绑）`
                        : binding.provider_name || binding.provider_user_id
                      : row.enabled
                        ? '未绑定第三方账号'
                        : '管理员未启用此登录方式'
                    return (
                      <div key={row.name} className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-4">
                        <div className="flex items-center gap-3 min-w-0">
                          <div className={`h-10 w-10 rounded-xl flex items-center justify-center shrink-0 ${meta.tint}`}>
                            <Icon className="h-5 w-5" />
                          </div>
                          <div className="min-w-0">
                            <p className="font-medium text-sm">{row.displayName}</p>
                            <p className="text-xs text-muted-foreground truncate" title={sub}>
                              {sub}
                            </p>
                          </div>
                        </div>
                        <div className="flex items-center gap-2 shrink-0">
                          {binding ? (
                            <>
                              <Badge className="bg-emerald-600 hover:bg-emerald-600">已绑定</Badge>
                              <Button variant="ghost" size="sm" className="h-8 text-destructive hover:text-destructive" onClick={() => handleUnbind(row.name)}>
                                <Unlink className="h-3.5 w-3.5 mr-1" />
                                解绑
                              </Button>
                            </>
                          ) : row.enabled ? (
                            <Button variant="outline" size="sm" className="h-8" onClick={() => handleBind(row.name)}>
                              <LinkIcon className="h-3.5 w-3.5 mr-1" />
                              绑定
                            </Button>
                          ) : (
                            <>
                              <Badge variant="secondary">未开放</Badge>
                              <Button variant="outline" size="sm" className="h-8" disabled>
                                绑定
                              </Button>
                            </>
                          )}
                        </div>
                      </div>
                    )
                  })}
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>

        <TabsContent value="security" className="mt-4 focus-visible:outline-none">
          <Card className="border-border/80 shadow-sm overflow-hidden">
            <CardHeader className="border-b bg-muted/30">
              <CardTitle className="text-base flex items-center gap-2">
                <Shield className="h-4 w-4 text-primary" />
                安全设置
              </CardTitle>
              <CardDescription>密码、二步验证（TOTP）与 API 访问令牌</CardDescription>
            </CardHeader>
            <CardContent className="p-0">
              <div className="divide-y">
                <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-4">
                  <div className="flex items-center gap-3">
                    <div className="h-10 w-10 rounded-xl bg-orange-50 dark:bg-orange-950 flex items-center justify-center shrink-0">
                      <Lock className="h-5 w-5 text-orange-600" />
                    </div>
                    <div>
                      <p className="font-medium text-sm">密码管理</p>
                      <p className="text-xs text-muted-foreground">定期修改密码可降低被盗风险</p>
                    </div>
                  </div>
                  <Button variant="outline" size="sm" className="h-8 shrink-0" onClick={() => setShowPasswordDialog(true)}>修改密码</Button>
                </div>

                <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-4">
                  <div className="flex items-center gap-3 min-w-0">
                    <div className={`h-10 w-10 rounded-xl flex items-center justify-center shrink-0 ${user?.totp_open ? 'bg-green-50 dark:bg-green-950' : 'bg-muted'}`}>
                      {user?.totp_open ? <ShieldCheck className="h-5 w-5 text-green-600" /> : <ShieldOff className="h-5 w-5 text-muted-foreground" />}
                    </div>
                    <div className="min-w-0">
                      <p className="font-medium text-sm flex flex-wrap items-center gap-2">
                        二步验证（TOTP）
                        <Badge variant={user?.totp_open ? 'default' : 'secondary'} className={user?.totp_open ? 'bg-green-600' : ''}>
                          {user?.totp_open ? '已启用' : '未启用'}
                        </Badge>
                      </p>
                      <p className="text-xs text-muted-foreground mt-0.5">
                        {user?.totp_open ? '登录时需输入验证器中的 6 位动态码；可使用恢复码应急' : '启用后需使用验证器应用扫描二维码完成绑定'}
                      </p>
                    </div>
                  </div>
                  <div className="flex shrink-0">
                    {user?.totp_open ? (
                      <Button variant="outline" size="sm" className="h-8 text-destructive border-destructive/30 hover:bg-destructive/10" onClick={() => setShowDisableTotpDialog(true)}>
                        关闭验证
                      </Button>
                    ) : (
                      <Button size="sm" className="h-8" onClick={handleEnableTOTP} disabled={submitting}>
                        {submitting ? <Loader2 className="h-3.5 w-3.5 mr-1 animate-spin" /> : <QrCode className="h-3.5 w-3.5 mr-1" />}
                        启用验证
                      </Button>
                    )}
                  </div>
                </div>

                {user?.totp_open && (
                  <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-3 px-4 py-4">
                    <div className="flex items-center gap-3 min-w-0">
                      <div className="h-10 w-10 rounded-xl bg-amber-50 dark:bg-amber-950 flex items-center justify-center shrink-0">
                        <Ticket className="h-5 w-5 text-amber-700 dark:text-amber-400" />
                      </div>
                      <div className="min-w-0">
                        <p className="font-medium text-sm">恢复码</p>
                        <p className="text-xs text-muted-foreground">手机丢失时可用恢复码登录或关闭验证；每条码仅一次有效。可重新生成使旧码全部失效。</p>
                      </div>
                    </div>
                    <Button variant="outline" size="sm" className="h-8 shrink-0" onClick={openRegenRecoveryDialog}>
                      重新生成
                    </Button>
                  </div>
                )}

                {user?.is_api && (
                  <div className="flex flex-col gap-3 px-4 py-4">
                    <div className="flex flex-col sm:flex-row sm:items-start sm:justify-between gap-3">
                      <div className="flex items-center gap-3">
                        <div className="h-10 w-10 rounded-xl bg-purple-50 dark:bg-purple-950 flex items-center justify-center shrink-0">
                          <Key className="h-5 w-5 text-purple-600" />
                        </div>
                        <div>
                          <p className="font-medium text-sm">API 访问令牌</p>
                          <p className="text-xs text-muted-foreground">用于 HMAC 签名的 API 密钥，请保密</p>
                        </div>
                      </div>
                      <div className="flex flex-wrap items-center gap-2 sm:justify-end">
                        {user.api_key ? (
                          <>
                            <code className="text-xs bg-muted px-2 py-1.5 rounded-md max-w-[200px] truncate font-mono border">{user.api_key}</code>
                            <Button variant="outline" size="sm" className="h-8" onClick={() => copyToClipboard(user.api_key!)}>
                              <Copy className="h-3.5 w-3.5 mr-1" />
                              复制
                            </Button>
                            <Button variant="outline" size="sm" className="h-8" onClick={() => setShowResetApiKeyDialog(true)}>
                              <RefreshCcw className="h-3.5 w-3.5 mr-1" />
                              重置
                            </Button>
                          </>
                        ) : (
                          <span className="text-xs text-muted-foreground">请联系管理员开启 API 后由系统生成密钥</span>
                        )}
                      </div>
                    </div>
                  </div>
                )}
              </div>
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      <AlertDialog open={showResetApiKeyDialog} onOpenChange={setShowResetApiKeyDialog}>
        <AlertDialogContent>
          <AlertDialogHeader>
            <AlertDialogTitle>重置 API 访问令牌？</AlertDialogTitle>
            <AlertDialogDescription>
              重置后旧令牌立即失效，使用旧令牌的集成将认证失败。请尽快更新各环境中的密钥配置。
            </AlertDialogDescription>
          </AlertDialogHeader>
          <AlertDialogFooter>
            <AlertDialogCancel disabled={apiKeyResetting}>取消</AlertDialogCancel>
            <AlertDialogAction onClick={(e) => { e.preventDefault(); void handleResetApiKey() }} disabled={apiKeyResetting}>
              {apiKeyResetting ? '处理中…' : '确认重置'}
            </AlertDialogAction>
          </AlertDialogFooter>
        </AlertDialogContent>
      </AlertDialog>

      {/* ========== Dialogs ========== */}

      {/* 修改密码 */}
      <Dialog open={showPasswordDialog} onOpenChange={setShowPasswordDialog}>
        <DialogContent className="max-w-sm">
          <DialogHeader><DialogTitle>修改密码</DialogTitle><DialogDescription>请输入当前密码和新密码</DialogDescription></DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1"><Label>当前密码</Label><Input type="password" value={passwordForm.old_password} onChange={(e) => setPasswordForm({ ...passwordForm, old_password: e.target.value })} /></div>
            <div className="space-y-1"><Label>新密码</Label><Input type="password" placeholder="至少6位" value={passwordForm.new_password} onChange={(e) => setPasswordForm({ ...passwordForm, new_password: e.target.value })} /></div>
            <div className="space-y-1"><Label>确认新密码</Label><Input type="password" value={passwordForm.confirm_password} onChange={(e) => setPasswordForm({ ...passwordForm, confirm_password: e.target.value })} /></div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowPasswordDialog(false)}>取消</Button>
            <Button onClick={handleChangePassword} disabled={changingPassword}>{changingPassword ? '修改中...' : '确认修改'}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 启用 TOTP */}
      <Dialog open={showTotpDialog} onOpenChange={setShowTotpDialog}>
        <DialogContent className="max-w-md">
          <DialogHeader><DialogTitle>启用二步验证</DialogTitle><DialogDescription>使用身份验证器App扫描二维码</DialogDescription></DialogHeader>
          <div className="space-y-4">
            {totpQrCode && <div className="flex justify-center"><div className="p-3 bg-white rounded-lg"><img src={totpQrCode} alt="QR" className="w-44 h-44" /></div></div>}
            {totpData?.secret && (
              <div className="space-y-1">
                <Label className="text-xs">手动输入密钥</Label>
                <div className="flex gap-2"><code className="flex-1 text-xs bg-muted px-2 py-1.5 rounded font-mono break-all">{totpData.secret}</code><Button size="sm" variant="outline" onClick={() => copyToClipboard(totpData.secret)}><Copy className="h-3 w-3" /></Button></div>
              </div>
            )}
            <Separator />
            <div className="space-y-1">
              <Label>输入验证码</Label>
              <Input placeholder="6位数字" value={totpCode} onChange={(e) => setTotpCode(e.target.value.replace(/\D/g, '').slice(0, 6))} maxLength={6} className="text-center text-xl tracking-widest font-mono" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => { setShowTotpDialog(false); setTotpCode('') }}>取消</Button>
            <Button onClick={handleVerifyTOTP} disabled={submitting || totpCode.length !== 6}>{submitting ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : <CheckCircle className="h-4 w-4 mr-1" />}验证并启用</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 重新生成恢复码 */}
      <Dialog open={showRegenRecoveryDialog} onOpenChange={setShowRegenRecoveryDialog}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>重新生成恢复码</DialogTitle>
            <DialogDescription>验证身份后生成新恢复码，旧的恢复码将立即全部失效。</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label>登录密码</Label>
              <Input type="password" value={regenForm.password} onChange={(e) => setRegenForm({ ...regenForm, password: e.target.value })} autoComplete="current-password" />
            </div>
            <div className="space-y-1">
              <Label>验证器 6 位动态码</Label>
              <Input
                placeholder="000000"
                value={regenForm.code}
                onChange={(e) => setRegenForm({ ...regenForm, code: e.target.value.replace(/\D/g, '').slice(0, 6) })}
                maxLength={6}
                className="text-center text-xl tracking-widest font-mono"
              />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowRegenRecoveryDialog(false)}>取消</Button>
            <Button onClick={handleRegenerateRecovery} disabled={submitting || regenForm.code.length !== 6}>
              {submitting ? <Loader2 className="h-4 w-4 animate-spin mr-1" /> : null}
              确认生成
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 关闭 TOTP */}
      <Dialog open={showDisableTotpDialog} onOpenChange={setShowDisableTotpDialog}>
        <DialogContent className="max-w-sm">
          <DialogHeader><DialogTitle>关闭二步验证</DialogTitle><DialogDescription>关闭后登录将不再需要验证码</DialogDescription></DialogHeader>
          <div className="p-3 bg-red-50 dark:bg-red-900/20 border border-red-200 dark:border-red-800 rounded-lg text-sm text-red-700 dark:text-red-300">关闭二步验证将降低账户安全性。</div>
          <div className="space-y-3">
            <div className="space-y-1"><Label>当前密码</Label><Input type="password" value={disableForm.password} onChange={(e) => setDisableForm({ ...disableForm, password: e.target.value })} /></div>
            <div className="space-y-1">
              <Label>验证码或恢复码</Label>
              <Input placeholder="6位验证码或恢复码" value={disableForm.code} onChange={(e) => setDisableForm({ ...disableForm, code: e.target.value.slice(0, 12) })} maxLength={12} className="font-mono" />
              <p className="text-xs text-muted-foreground">输入验证器动态码，或恢复码（格式: XXXXX-XXXXX）</p>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowDisableTotpDialog(false)}>取消</Button>
            <Button variant="destructive" onClick={handleDisableTOTP} disabled={submitting}>{submitting ? '处理中...' : '确认关闭'}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 恢复码 */}
      <Dialog open={showRecoveryCodesDialog} onOpenChange={setShowRecoveryCodesDialog}>
        <DialogContent className="max-w-md">
          <DialogHeader>
            <DialogTitle className="flex items-center gap-2">
              <ShieldCheck className="h-5 w-5 text-green-500" />
              {recoveryCodesKind === 'regen' ? '新的恢复码' : '二步验证已启用'}
            </DialogTitle>
            <DialogDescription>
              {recoveryCodesKind === 'regen'
                ? '请立即保存；旧恢复码已全部失效。'
                : '请保存以下恢复码，当您无法使用验证器时可以使用恢复码登录'}
            </DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="p-3 bg-amber-50 dark:bg-amber-900/20 border border-amber-200 dark:border-amber-800 rounded-lg text-sm text-amber-700 dark:text-amber-300 flex gap-2">
              <AlertTriangle className="h-4 w-4 mt-0.5 flex-shrink-0" />
              <span>每个恢复码只能使用一次，请妥善保管。</span>
            </div>
            <div className="grid grid-cols-2 gap-2 p-3 bg-muted rounded-lg font-mono text-sm">
              {recoveryCodes.map((code, i) => <div key={i} className="py-1 px-2 bg-background rounded border text-center">{code}</div>)}
            </div>
            <Button variant="outline" className="w-full" onClick={() => { navigator.clipboard.writeText(recoveryCodes.join('\n')); toast.success('已复制') }}>
              <Copy className="h-4 w-4 mr-2" />复制全部恢复码
            </Button>
          </div>
          <DialogFooter><Button onClick={() => setShowRecoveryCodesDialog(false)}>我已保存，关闭</Button></DialogFooter>
        </DialogContent>
      </Dialog>

      {/* 邮箱绑定 */}
      <Dialog open={showEmailDialog} onOpenChange={setShowEmailDialog}>
        <DialogContent className="max-w-sm">
          <DialogHeader>
            <DialogTitle>{user?.email ? '修改邮箱' : '绑定邮箱'}</DialogTitle>
            <DialogDescription>输入新邮箱并验证</DialogDescription>
          </DialogHeader>
          <div className="space-y-3">
            <div className="space-y-1">
              <Label>邮箱</Label>
              <div className="flex gap-2">
                <Input type="email" placeholder="输入邮箱地址" value={emailForm.email} onChange={(e) => setEmailForm({ ...emailForm, email: e.target.value })} className="flex-1" />
                <Button type="button" variant="outline" size="sm" className="shrink-0 h-9 px-3 text-xs"
                  onClick={handleSendEmailCode} disabled={emailCountdown > 0}>
                  {emailCountdown > 0 ? `${emailCountdown}s` : '发送验证码'}
                </Button>
              </div>
            </div>
            <div className="space-y-1">
              <Label>验证码</Label>
              <Input placeholder="8位验证码" value={emailForm.code} onChange={(e) => setEmailForm({ ...emailForm, code: e.target.value.toUpperCase() })} maxLength={8} className="font-mono tracking-widest" />
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setShowEmailDialog(false)}>取消</Button>
            <Button onClick={handleBindEmail} disabled={submitting}>{submitting ? '绑定中...' : '确认绑定'}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
