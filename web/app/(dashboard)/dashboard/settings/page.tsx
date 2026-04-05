'use client'

import { useState, useEffect } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Textarea } from '@/components/ui/textarea'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Separator } from '@/components/ui/separator'
import { toast } from 'sonner'
import { systemApi, SystemConfig, CronConfig, TaskStatus } from '@/lib/api'
import { Settings, Mail, MessageSquare, Webhook, Shield, Globe, RefreshCw, CheckCircle, Lock, Send, Bell, Server, Home, UserPlus, Github, Clock, Timer } from 'lucide-react'
import { Badge } from '@/components/ui/badge'

export default function SettingsPage() {
  const [config, setConfig] = useState<SystemConfig>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [testing, setTesting] = useState<string | null>(null)
  const [expandedNotify, setExpandedNotify] = useState<string | null>(null)
  const [cronConfig, setCronConfig] = useState<CronConfig>({})
  const [taskStatus, setTaskStatus] = useState<TaskStatus | null>(null)
  const [savingCron, setSavingCron] = useState(false)

  const toggleNotifySection = (section: string) => {
    setExpandedNotify(expandedNotify === section ? null : section)
  }

  useEffect(() => {
    loadConfig()
    loadCronConfig()
    loadTaskStatus()
  }, [])

  const loadConfig = async () => {
    setLoading(true)
    try {
      const res = await systemApi.getConfig()
      if (res.code === 0 && res.data) {
        setConfig(res.data)
      }
    } catch (error) {
      console.error('Failed to load config:', error)
      toast.error('加载配置失败')
    } finally {
      setLoading(false)
    }
  }

  const loadCronConfig = async () => {
    try {
      const res = await systemApi.getCronConfig()
      if (res.code === 0 && res.data) {
        setCronConfig(res.data)
      }
    } catch {
      // ignore
    }
  }

  const loadTaskStatus = async () => {
    try {
      const res = await systemApi.getTaskStatus()
      if (res.code === 0 && res.data) {
        setTaskStatus(res.data)
      }
    } catch {
      // ignore
    }
  }

  const handleSaveCron = async () => {
    setSavingCron(true)
    try {
      const res = await systemApi.updateCronConfig(cronConfig)
      if (res.code === 0) {
        toast.success('定时任务配置已保存')
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存失败')
    } finally {
      setSavingCron(false)
    }
  }

  const handleSave = async () => {
    const mp = config.mail_port
    if (mp !== undefined && mp !== 0 && (mp < 1 || mp > 65535)) {
      toast.error('SMTP 端口须为 1–65535，或留空')
      return
    }
    if (config.proxy_enabled && (config.proxy_server || '').trim()) {
      const pp = config.proxy_port
      if (pp === undefined || pp < 1 || pp > 65535) {
        toast.error('已填写代理地址时，请填写有效代理端口（1–65535）')
        return
      }
    } else {
      const pp = config.proxy_port
      if (pp !== undefined && pp !== 0 && (pp < 1 || pp > 65535)) {
        toast.error('代理端口须为 1–65535，或留空')
        return
      }
    }

    setSaving(true)
    try {
      const noticeDays =
        typeof config.cert_expire_notice_days === 'number' && config.cert_expire_notice_days > 0
          ? config.cert_expire_notice_days
          : null
      const noticeInterval =
        typeof config.cert_expire_notice_interval_days === 'number' &&
        config.cert_expire_notice_interval_days > 0
          ? config.cert_expire_notice_interval_days
          : null
      const res = await systemApi.updateConfig({
        ...config,
        cert_expire_notice_days: noticeDays,
        cert_expire_notice_interval_days: noticeInterval,
      })
      if (res.code === 0) {
        toast.success('保存成功')
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleTestMail = async () => {
    setTesting('mail')
    try {
      const res = await systemApi.testMail()
      if (res.code === 0) {
        toast.success('邮件发送成功')
      } else {
        toast.error(res.msg || '邮件发送失败')
      }
    } catch {
      toast.error('邮件发送失败')
    } finally {
      setTesting(null)
    }
  }

  const handleTestTelegram = async () => {
    setTesting('telegram')
    try {
      const res = await systemApi.testTelegram()
      if (res.code === 0) {
        toast.success('Telegram 消息发送成功')
      } else {
        toast.error(res.msg || 'Telegram 消息发送失败')
      }
    } catch {
      toast.error('Telegram 消息发送失败')
    } finally {
      setTesting(null)
    }
  }

  const handleTestWebhook = async () => {
    setTesting('webhook')
    try {
      const res = await systemApi.testWebhook()
      if (res.code === 0) {
        toast.success('Webhook 请求成功')
      } else {
        toast.error(res.msg || 'Webhook 请求失败')
      }
    } catch {
      toast.error('Webhook 请求失败')
    } finally {
      setTesting(null)
    }
  }

  const handleTestDiscord = async () => {
    setTesting('discord')
    try {
      const res = await systemApi.testDiscord()
      if (res.code === 0) {
        toast.success('Discord 消息发送成功')
      } else {
        toast.error(res.msg || 'Discord 消息发送失败')
      }
    } catch {
      toast.error('Discord 消息发送失败')
    } finally {
      setTesting(null)
    }
  }

  const handleTestBark = async () => {
    setTesting('bark')
    try {
      const res = await systemApi.testBark()
      if (res.code === 0) {
        toast.success('Bark 推送发送成功')
      } else {
        toast.error(res.msg || 'Bark 推送发送失败')
      }
    } catch {
      toast.error('Bark 推送发送失败')
    } finally {
      setTesting(null)
    }
  }

  const handleTestWechat = async () => {
    setTesting('wechat')
    try {
      const res = await systemApi.testWechat()
      if (res.code === 0) {
        toast.success('企业微信消息发送成功')
      } else {
        toast.error(res.msg || '企业微信消息发送失败')
      }
    } catch {
      toast.error('企业微信消息发送失败')
    } finally {
      setTesting(null)
    }
  }

  const handleTestProxy = async () => {
    if (!config.proxy_server || !config.proxy_port) {
      toast.error('请先配置代理服务器')
      return
    }
    if (config.proxy_port < 1 || config.proxy_port > 65535) {
      toast.error('代理端口须为 1–65535')
      return
    }
    setTesting('proxy')
    try {
      const res = await systemApi.testProxy({
        host: config.proxy_server || '',
        port: config.proxy_port || 0,
        type: config.proxy_type || 'http',
        user: config.proxy_user,
        pass: config.proxy_password,
      })
      if (res.code === 0) {
        toast.success('代理连接成功')
      } else {
        toast.error(res.msg || '代理连接失败')
      }
    } catch {
      toast.error('代理连接失败')
    } finally {
      setTesting(null)
    }
  }

  const handleClearCache = async () => {
    try {
      const res = await systemApi.clearCache()
      if (res.code === 0) {
        toast.success('缓存清除成功')
      } else {
        toast.error(res.msg || '缓存清除失败')
      }
    } catch {
      toast.error('缓存清除失败')
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-[400px]">
        <RefreshCw className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-3">
            <div className="h-10 w-10 rounded-xl bg-gradient-to-br from-slate-500 to-gray-600 flex items-center justify-center shadow-lg shadow-slate-500/20">
              <Settings className="h-5 w-5 text-white" />
            </div>
            系统设置
          </h1>
          <p className="text-muted-foreground mt-1">配置系统参数和通知设置</p>
        </div>
        <Button onClick={handleSave} disabled={saving}>
          {saving ? (
            <>
              <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
              保存中...
            </>
          ) : (
            <>
              <CheckCircle className="h-4 w-4 mr-2" />
              保存设置
            </>
          )}
        </Button>
      </div>

      <Tabs defaultValue="site" className="space-y-6">
        <TabsList className="flex flex-wrap gap-1 h-auto p-1">
          <TabsTrigger value="site" className="flex items-center gap-2">
            <Home className="h-4 w-4" />
            <span className="hidden sm:inline">站点设置</span>
          </TabsTrigger>
          <TabsTrigger value="login" className="flex items-center gap-2">
            <Lock className="h-4 w-4" />
            <span className="hidden sm:inline">登录设置</span>
          </TabsTrigger>
          <TabsTrigger value="notify" className="flex items-center gap-2">
            <Bell className="h-4 w-4" />
            <span className="hidden sm:inline">通知设置</span>
          </TabsTrigger>
          <TabsTrigger value="cron" className="flex items-center gap-2">
            <Timer className="h-4 w-4" />
            <span className="hidden sm:inline">定时任务</span>
          </TabsTrigger>
          <TabsTrigger value="proxy" className="flex items-center gap-2">
            <Globe className="h-4 w-4" />
            <span className="hidden sm:inline">代理</span>
          </TabsTrigger>
        </TabsList>

        {/* Site Settings */}
        <TabsContent value="site">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Home className="h-5 w-5" />
                站点基本设置
              </CardTitle>
              <CardDescription>配置站点名称、URL等基本信息</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="space-y-4">
                <div className="space-y-2">
                  <Label>站点名称</Label>
                  <Input
                    placeholder="DNSPlane"
                    value={config.site_name || ''}
                    onChange={(e) => setConfig({ ...config, site_name: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">显示在邮件通知等位置的站点名称</p>
                </div>
                <div className="space-y-2">
                  <Label>站点 URL</Label>
                  <Input
                    placeholder="https://dns.example.com"
                    value={config.site_url || ''}
                    onChange={(e) => setConfig({ ...config, site_url: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">
                    用于生成密码重置链接、OAuth 回调与 CORS 校验。前后端不同域时必须填写前端访问的完整 Origin（含协议与端口），否则浏览器会拦截跨域带 Cookie 的请求。
                  </p>
                </div>
                <Separator />
                <p className="text-sm font-medium text-foreground">证书（SSL）</p>
                <div className="space-y-2">
                  <Label>自动续期提前天数</Label>
                  <Input
                    type="number"
                    placeholder="30"
                    min={1}
                    value={config.cert_expire_days || ''}
                    onChange={(e) => setConfig({ ...config, cert_expire_days: parseInt(e.target.value, 10) || 0 })}
                  />
                  <p className="text-xs text-muted-foreground">
                    证书仍有效时，到期前多少天由后台任务触发自动续期（需订单开启自动续期）。
                  </p>
                </div>
                <div className="flex items-center justify-between rounded-lg border p-3">
                  <div className="space-y-0.5 pr-4">
                    <Label>证书到期推送</Label>
                    <p className="text-xs text-muted-foreground">关闭后不再发送证书即将/已过期通知（续期任务仍可按上项执行）</p>
                  </div>
                  <Switch
                    checked={config.cert_expire_notice_enabled !== false}
                    onCheckedChange={(checked) => setConfig({ ...config, cert_expire_notice_enabled: checked })}
                  />
                </div>
                <div className="space-y-2">
                  <Label>证书到期推送 · 提前天数</Label>
                  <Input
                    type="number"
                    placeholder="与「自动续期提前天数」相同"
                    min={1}
                    value={config.cert_expire_notice_days != null && config.cert_expire_notice_days > 0 ? String(config.cert_expire_notice_days) : ''}
                    onChange={(e) => {
                      const raw = e.target.value.trim()
                      if (raw === '') {
                        setConfig({ ...config, cert_expire_notice_days: undefined })
                        return
                      }
                      const n = parseInt(raw, 10)
                      setConfig({ ...config, cert_expire_notice_days: Number.isFinite(n) && n > 0 ? n : undefined })
                    }}
                  />
                  <p className="text-xs text-muted-foreground">
                    距离到期剩余天数 ≤ 该值时纳入提醒窗口。留空则与「自动续期提前天数」一致。
                  </p>
                </div>
                <div className="space-y-2">
                  <Label>证书到期推送 · 重复间隔（天）</Label>
                  <Input
                    type="number"
                    placeholder="默认 1（与域名推送「每天最多一次」类似）"
                    min={1}
                    value={
                      config.cert_expire_notice_interval_days != null &&
                      config.cert_expire_notice_interval_days > 0
                        ? String(config.cert_expire_notice_interval_days)
                        : ''
                    }
                    onChange={(e) => {
                      const raw = e.target.value.trim()
                      if (raw === '') {
                        setConfig({ ...config, cert_expire_notice_interval_days: undefined })
                        return
                      }
                      const n = parseInt(raw, 10)
                      setConfig({
                        ...config,
                        cert_expire_notice_interval_days: Number.isFinite(n) && n > 0 ? n : undefined,
                      })
                    }}
                  />
                  <p className="text-xs text-muted-foreground">
                    在提醒窗口内，同一订单至少间隔多少天再推送一次；新签发或自动续期开始后会重置计时。留空为 1 天。
                  </p>
                </div>
                <Separator />
                <p className="text-sm font-medium text-foreground">域名（WHOIS）</p>
                <div className="flex items-center justify-between rounded-lg border p-3">
                  <div className="space-y-0.5 pr-4">
                    <Label>域名到期推送</Label>
                    <p className="text-xs text-muted-foreground">关闭后仍可按计划刷新 WHOIS，但不发通知</p>
                  </div>
                  <Switch
                    checked={config.domain_expire_notice_enabled !== false}
                    onCheckedChange={(checked) => setConfig({ ...config, domain_expire_notice_enabled: checked })}
                  />
                </div>
                <div className="space-y-2">
                  <Label>域名到期推送 · 提前天数</Label>
                  <Input
                    type="number"
                    placeholder="30"
                    min={1}
                    value={config.domain_expire_days || ''}
                    onChange={(e) => setConfig({ ...config, domain_expire_days: parseInt(e.target.value, 10) || 0 })}
                  />
                  <p className="text-xs text-muted-foreground">距离到期剩余天数 ≤ 该值时发送提醒（同一域名每天最多一次）</p>
                </div>
                <Separator />
                <div className="flex items-center justify-between rounded-lg border p-3">
                  <div className="space-y-0.5 pr-4">
                    <Label>证书部署失败告警</Label>
                    <p className="text-xs text-muted-foreground">部署任务达到最大重试仍失败时，是否通过已配置渠道推送告警</p>
                  </div>
                  <Switch
                    checked={config.cert_deploy_notice_enabled !== false}
                    onCheckedChange={(checked) => setConfig({ ...config, cert_deploy_notice_enabled: checked })}
                  />
                </div>
                <div className="flex items-center justify-between rounded-lg border p-3">
                  <div className="space-y-0.5 pr-4">
                    <Label>证书部署成功通知</Label>
                    <p className="text-xs text-muted-foreground">每次自动部署成功时是否推送（默认关，避免频繁打扰）</p>
                  </div>
                  <Switch
                    checked={config.cert_deploy_success_notice_enabled === true}
                    onCheckedChange={(checked) =>
                      setConfig({ ...config, cert_deploy_success_notice_enabled: checked })
                    }
                  />
                </div>
                <div className="flex items-center justify-between rounded-lg border p-3">
                  <div className="space-y-0.5 pr-4">
                    <Label>证书自动续期失败通知</Label>
                    <p className="text-xs text-muted-foreground">
                      自动续期连续失败、ACME 最终失败或重试次数用尽时，通过已配置渠道推送摘要（同一订单 24 小时内最多一次）
                    </p>
                  </div>
                  <Switch
                    checked={config.cert_renew_fail_notice_enabled !== false}
                    onCheckedChange={(checked) =>
                      setConfig({ ...config, cert_renew_fail_notice_enabled: checked })
                    }
                  />
                </div>
              </div>
            </CardContent>
          </Card>
        </TabsContent>

        {/* Login Settings */}
        <TabsContent value="login">
          <div className="space-y-6">
            {/* Captcha Settings */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Lock className="h-5 w-5" />
                  登录安全设置
                </CardTitle>
                <CardDescription>配置登录验证和安全选项</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>启用验证码</Label>
                    <p className="text-sm text-muted-foreground">登录时需要输入验证码</p>
                  </div>
                  <Switch
                    checked={config.captcha_enabled || false}
                    onCheckedChange={(checked) => setConfig({ ...config, captcha_enabled: checked })}
                  />
                </div>
                <Separator />
                {config.captcha_enabled && (
                  <div className="space-y-4">
                    <div className="space-y-2">
                      <Label>验证码类型</Label>
                      <Select
                        value={config.captcha_type || 'image'}
                        onValueChange={(v) => setConfig({ ...config, captcha_type: v })}
                      >
                        <SelectTrigger>
                          <SelectValue />
                        </SelectTrigger>
                        <SelectContent>
                          <SelectItem value="image">图形验证码（内置）</SelectItem>
                          <SelectItem value="turnstile">Cloudflare Turnstile</SelectItem>
                          <SelectItem value="recaptcha">Google reCAPTCHA v2</SelectItem>
                          <SelectItem value="hcaptcha">hCaptcha</SelectItem>
                        </SelectContent>
                      </Select>
                      <p className="text-xs text-muted-foreground">
                        {config.captcha_type === 'turnstile' && '免费、隐私友好的验证码服务，推荐使用'}
                        {config.captcha_type === 'recaptcha' && '需要科学上网才能正常使用'}
                        {config.captcha_type === 'hcaptcha' && '隐私友好的验证码服务'}
                        {(!config.captcha_type || config.captcha_type === 'image') && '服务端生成图形验证码，无需额外配置'}
                      </p>
                    </div>
                    {config.captcha_type && config.captcha_type !== 'image' && (
                      <>
                        <div className="space-y-2">
                          <Label>Site Key（站点密钥）</Label>
                          <Input
                            placeholder="在验证码服务商后台获取"
                            value={config.captcha_site_key || ''}
                            onChange={(e) => setConfig({ ...config, captcha_site_key: e.target.value })}
                          />
                          <p className="text-xs text-muted-foreground">公开密钥，用于前端加载验证码组件</p>
                        </div>
                        <div className="space-y-2">
                          <Label>Secret Key（私有密钥）</Label>
                          <Input
                            type="password"
                            placeholder="在验证码服务商后台获取"
                            value={config.captcha_secret_key || ''}
                            onChange={(e) => setConfig({ ...config, captcha_secret_key: e.target.value })}
                          />
                          <p className="text-xs text-muted-foreground">私有密钥，用于服务端验证，请勿泄露</p>
                        </div>
                      </>
                    )}
                  </div>
                )}
              </CardContent>
            </Card>

            {/* 独立 Turnstile：忘记密码 / 重置二步验证（verifyTurnstile） */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Shield className="h-5 w-5" />
                  敏感操作验证（Turnstile）
                </CardTitle>
                <CardDescription>
                  与上方「登录验证码」分开配置。保存非空的私有密钥后，找回密码与重置二步验证页将要求用户完成 Turnstile；请同时填写站点密钥与密钥（可在 Cloudflare 控制台创建与登录验证码相同或不同的 Widget）。
                </CardDescription>
              </CardHeader>
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Turnstile 站点密钥（Site Key）</Label>
                  <Input
                    placeholder="0x4AAAA..."
                    value={config.turnstile_site_key || ''}
                    onChange={(e) => setConfig({ ...config, turnstile_site_key: e.target.value })}
                    autoComplete="off"
                  />
                  <p className="text-xs text-muted-foreground">公开，用于忘记密码等页面的验证组件</p>
                </div>
                <div className="space-y-2">
                  <Label>Turnstile 私有密钥（Secret Key）</Label>
                  <Input
                    type="password"
                    placeholder="留空表示不强制敏感操作 Turnstile"
                    value={config.turnstile_secret_key || ''}
                    onChange={(e) => setConfig({ ...config, turnstile_secret_key: e.target.value })}
                    autoComplete="new-password"
                  />
                  <p className="text-xs text-muted-foreground">仅服务端校验使用；清空并保存可关闭该要求</p>
                </div>
              </CardContent>
            </Card>

            {/* Register Settings */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <UserPlus className="h-5 w-5" />
                  注册设置
                </CardTitle>
                <CardDescription>配置用户注册功能</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="flex items-center justify-between">
                  <div className="space-y-0.5">
                    <Label>开放注册</Label>
                    <p className="text-sm text-muted-foreground">允许新用户自行注册账户</p>
                  </div>
                  <Switch
                    checked={config.register_enabled || false}
                    onCheckedChange={(checked) => setConfig({ ...config, register_enabled: checked })}
                  />
                </div>
              </CardContent>
            </Card>

            {/* GitHub OAuth Settings */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Github className="h-5 w-5" />
                  GitHub 登录
                </CardTitle>
                <CardDescription>配置 GitHub OAuth / GitHub App 第三方登录</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="space-y-4">
                  <div className="space-y-2">
                    <Label>登录模式</Label>
                    <Select
                      value={config.github_mode || 'oauth'}
                      onValueChange={(v) => setConfig({ ...config, github_mode: v })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="oauth">OAuth App</SelectItem>
                        <SelectItem value="app">GitHub App</SelectItem>
                      </SelectContent>
                    </Select>
                    <p className="text-xs text-muted-foreground">
                      {(!config.github_mode || config.github_mode === 'oauth') ? '使用 OAuth App 进行用户认证' : '使用 GitHub App 进行用户认证'}
                    </p>
                  </div>

                  {(!config.github_mode || config.github_mode === 'oauth') ? (
                    <>
                      <div className="space-y-2">
                        <Label>Client ID</Label>
                        <Input
                          placeholder="GitHub OAuth App Client ID"
                          value={config.github_client_id || ''}
                          onChange={(e) => setConfig({ ...config, github_client_id: e.target.value })}
                        />
                        <p className="text-xs text-muted-foreground">
                          在 GitHub Settings &gt; Developer settings &gt; OAuth Apps 中创建应用获取
                        </p>
                      </div>
                      <div className="space-y-2">
                        <Label>Client Secret</Label>
                        <Input
                          type="password"
                          placeholder="GitHub OAuth App Client Secret"
                          value={config.github_client_secret || ''}
                          onChange={(e) => setConfig({ ...config, github_client_secret: e.target.value })}
                        />
                        <p className="text-xs text-muted-foreground">私有密钥，请勿泄露</p>
                      </div>
                      <div className="bg-muted/50 rounded-md p-3">
                        <p className="text-sm font-medium mb-2">配置说明：</p>
                        <div className="text-xs text-muted-foreground space-y-1">
                          <p>1. 前往 GitHub Settings &gt; Developer settings &gt; OAuth Apps 创建新应用</p>
                          <p>2. Homepage URL 填写您的站点地址，如 <code className="bg-background px-1 rounded">{config.site_url || 'https://dns.example.com'}</code></p>
                          <p>3. Authorization callback URL 填写 <code className="bg-background px-1 rounded">{(config.site_url || 'https://dns.example.com') + '/api/auth/github/callback'}</code></p>
                          <p>4. 填写 Client ID 和 Client Secret 后保存即可启用 GitHub 登录</p>
                        </div>
                      </div>
                    </>
                  ) : (
                    <>
                      <div className="space-y-2">
                        <Label>App ID</Label>
                        <Input
                          placeholder="GitHub App ID"
                          value={config.github_app_id || ''}
                          onChange={(e) => setConfig({ ...config, github_app_id: e.target.value })}
                        />
                        <p className="text-xs text-muted-foreground">
                          在 GitHub Settings &gt; Developer settings &gt; GitHub Apps 中获取 App ID
                        </p>
                      </div>
                      <div className="space-y-2">
                        <Label>Private Key</Label>
                        <Textarea
                          placeholder="-----BEGIN RSA PRIVATE KEY-----&#10;...&#10;-----END RSA PRIVATE KEY-----"
                          value={config.github_app_private_key || ''}
                          onChange={(e) => setConfig({ ...config, github_app_private_key: e.target.value })}
                          rows={6}
                        />
                        <p className="text-xs text-muted-foreground">GitHub App 的私钥（PEM 格式），请勿泄露</p>
                      </div>
                      <div className="bg-muted/50 rounded-md p-3">
                        <p className="text-sm font-medium mb-2">配置说明：</p>
                        <div className="text-xs text-muted-foreground space-y-1">
                          <p>1. 前往 GitHub Settings &gt; Developer settings &gt; GitHub Apps 创建新应用</p>
                          <p>2. Homepage URL 填写您的站点地址，如 <code className="bg-background px-1 rounded">{config.site_url || 'https://dns.example.com'}</code></p>
                          <p>3. Callback URL 填写 <code className="bg-background px-1 rounded">{(config.site_url || 'https://dns.example.com') + '/api/auth/github-app/callback'}</code></p>
                          <p>4. 在 App 设置页面生成并下载 Private Key</p>
                          <p>5. 填写 App ID 和 Private Key 后保存即可启用 GitHub App 登录</p>
                        </div>
                      </div>
                    </>
                  )}
                </div>
              </CardContent>
            </Card>

            {/* OAuth2 Multi-Provider Settings */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Globe className="h-5 w-5" />
                  其他 OAuth2 登录
                </CardTitle>
                <CardDescription>配置 Google、微信、钉钉或自定义 OAuth2 第三方登录</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                {/* Google */}
                <div className="space-y-3 p-4 border rounded-lg">
                  <h4 className="font-medium">Google</h4>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1">
                      <Label className="text-xs">Client ID</Label>
                      <Input placeholder="Google OAuth Client ID" value={config.oauth_google_client_id || ''} onChange={(e) => setConfig({ ...config, oauth_google_client_id: e.target.value })} />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Client Secret</Label>
                      <Input type="password" placeholder="Client Secret" value={config.oauth_google_client_secret || ''} onChange={(e) => setConfig({ ...config, oauth_google_client_secret: e.target.value })} />
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">回调地址: <code className="bg-muted px-1 rounded">{(config.site_url || 'https://dns.example.com') + '/api/auth/oauth/google/callback'}</code></p>
                </div>

                {/* WeChat */}
                <div className="space-y-3 p-4 border rounded-lg">
                  <h4 className="font-medium">微信开放平台</h4>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1">
                      <Label className="text-xs">AppID</Label>
                      <Input placeholder="微信 AppID" value={config.oauth_wechat_app_id || ''} onChange={(e) => setConfig({ ...config, oauth_wechat_app_id: e.target.value })} />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">AppSecret</Label>
                      <Input type="password" placeholder="AppSecret" value={config.oauth_wechat_app_secret || ''} onChange={(e) => setConfig({ ...config, oauth_wechat_app_secret: e.target.value })} />
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">回调地址: <code className="bg-muted px-1 rounded">{(config.site_url || 'https://dns.example.com') + '/api/auth/oauth/wechat/callback'}</code></p>
                </div>

                {/* DingTalk */}
                <div className="space-y-3 p-4 border rounded-lg">
                  <h4 className="font-medium">钉钉</h4>
                  <div className="grid grid-cols-2 gap-3">
                    <div className="space-y-1">
                      <Label className="text-xs">AppKey</Label>
                      <Input placeholder="钉钉 AppKey" value={config.oauth_dingtalk_app_key || ''} onChange={(e) => setConfig({ ...config, oauth_dingtalk_app_key: e.target.value })} />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">AppSecret</Label>
                      <Input type="password" placeholder="AppSecret" value={config.oauth_dingtalk_app_secret || ''} onChange={(e) => setConfig({ ...config, oauth_dingtalk_app_secret: e.target.value })} />
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">回调地址: <code className="bg-muted px-1 rounded">{(config.site_url || 'https://dns.example.com') + '/api/auth/oauth/dingtalk/callback'}</code></p>
                </div>

                {/* Custom OAuth2 */}
                <div className="space-y-3 p-4 border rounded-lg">
                  <h4 className="font-medium">自定义 OAuth2</h4>
                  <div className="space-y-3">
                    <div className="space-y-1">
                      <Label className="text-xs">显示名称</Label>
                      <Input placeholder="如: Gitea, Keycloak" value={config.oauth_custom_name || ''} onChange={(e) => setConfig({ ...config, oauth_custom_name: e.target.value })} />
                    </div>
                    <div className="grid grid-cols-2 gap-3">
                      <div className="space-y-1">
                        <Label className="text-xs">Client ID</Label>
                        <Input placeholder="Client ID" value={config.oauth_custom_client_id || ''} onChange={(e) => setConfig({ ...config, oauth_custom_client_id: e.target.value })} />
                      </div>
                      <div className="space-y-1">
                        <Label className="text-xs">Client Secret</Label>
                        <Input type="password" placeholder="Client Secret" value={config.oauth_custom_client_secret || ''} onChange={(e) => setConfig({ ...config, oauth_custom_client_secret: e.target.value })} />
                      </div>
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Authorize URL</Label>
                      <Input placeholder="https://auth.example.com/oauth/authorize" value={config.oauth_custom_authorize_url || ''} onChange={(e) => setConfig({ ...config, oauth_custom_authorize_url: e.target.value })} />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Token URL</Label>
                      <Input placeholder="https://auth.example.com/oauth/token" value={config.oauth_custom_token_url || ''} onChange={(e) => setConfig({ ...config, oauth_custom_token_url: e.target.value })} />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">UserInfo URL</Label>
                      <Input placeholder="https://auth.example.com/api/user" value={config.oauth_custom_userinfo_url || ''} onChange={(e) => setConfig({ ...config, oauth_custom_userinfo_url: e.target.value })} />
                    </div>
                    <div className="space-y-1">
                      <Label className="text-xs">Scopes</Label>
                      <Input placeholder="openid profile email (可选)" value={config.oauth_custom_scopes || ''} onChange={(e) => setConfig({ ...config, oauth_custom_scopes: e.target.value })} />
                    </div>
                  </div>
                  <p className="text-xs text-muted-foreground">回调地址: <code className="bg-muted px-1 rounded">{(config.site_url || 'https://dns.example.com') + '/api/auth/oauth/custom/callback'}</code></p>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        {/* Notification Settings - Merged */}
        <TabsContent value="notify" className="space-y-4">
          {/* 邮件通知 */}
          <Card>
            <CardHeader className="cursor-pointer" onClick={() => toggleNotifySection('mail')}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Mail className="h-5 w-5" />
                  <CardTitle className="text-base">邮件通知</CardTitle>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={config.mail_enabled || false}
                    onCheckedChange={(checked) => { setConfig({ ...config, mail_enabled: checked }) }}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); handleTestMail(); }} disabled={testing === 'mail'}>
                    {testing === 'mail' ? <RefreshCw className="h-3 w-3 animate-spin" /> : '测试'}
                  </Button>
                </div>
              </div>
            </CardHeader>
            {expandedNotify === 'mail' && (
              <CardContent className="space-y-4">
                <div className="grid grid-cols-3 gap-4">
                  <div className="space-y-2 col-span-2">
                    <Label>SMTP 服务器</Label>
                    <Input
                      placeholder="smtp.example.com"
                      value={config.mail_host || ''}
                      onChange={(e) => setConfig({ ...config, mail_host: e.target.value })}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>SMTP 端口</Label>
                    <Input
                      type="number"
                      placeholder="465"
                      value={config.mail_port || ''}
                      onChange={(e) => setConfig({ ...config, mail_port: parseInt(e.target.value) || 0 })}
                    />
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>加密方式</Label>
                    <Select
                      value={config.mail_secure || 'tls'}
                      onValueChange={(v) => setConfig({ ...config, mail_secure: v })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="none">无加密</SelectItem>
                        <SelectItem value="ssl">SSL (端口465)</SelectItem>
                        <SelectItem value="tls">STARTTLS (端口587)</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="space-y-2">
                    <Label>验证方式</Label>
                    <Select
                      value={config.mail_auth || 'plain'}
                      onValueChange={(v) => setConfig({ ...config, mail_auth: v })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="plain">PLAIN</SelectItem>
                        <SelectItem value="login">LOGIN</SelectItem>
                        <SelectItem value="crammd5">CRAM-MD5</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                </div>
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>SMTP 用户名</Label>
                    <Input
                      placeholder="user@example.com"
                      value={config.mail_user || ''}
                      onChange={(e) => setConfig({ ...config, mail_user: e.target.value })}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>SMTP 密码</Label>
                    <Input
                      type="password"
                      placeholder="••••••••"
                      value={config.mail_password || ''}
                      onChange={(e) => setConfig({ ...config, mail_password: e.target.value })}
                    />
                  </div>
                </div>
                <Separator />
                <div className="grid grid-cols-2 gap-4">
                  <div className="space-y-2">
                    <Label>发件人名称</Label>
                    <Input
                      placeholder="DNSPlane"
                      value={config.mail_from_name || ''}
                      onChange={(e) => setConfig({ ...config, mail_from_name: e.target.value })}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>发件人地址</Label>
                    <Input
                      placeholder="noreply@example.com"
                      value={config.mail_from || ''}
                      onChange={(e) => setConfig({ ...config, mail_from: e.target.value })}
                    />
                  </div>
                </div>
                <div className="space-y-2">
                  <Label>通知收件人</Label>
                  <Input
                    placeholder="admin@example.com，多个邮箱用逗号分隔"
                    value={config.mail_recv || ''}
                    onChange={(e) => setConfig({ ...config, mail_recv: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">系统通知邮件的收件人，支持多个邮箱，用逗号分隔</p>
                </div>
                <Separator />
                <div className="space-y-4">
                  <div className="flex items-center justify-between">
                    <div>
                      <Label className="text-base">自定义邮件模板</Label>
                      <p className="text-sm text-muted-foreground">自定义通知邮件的标题和内容格式</p>
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label>邮件标题模板</Label>
                    <Input
                      placeholder="[{site_name}] {title}"
                      value={config.mail_subject_template || ''}
                      onChange={(e) => setConfig({ ...config, mail_subject_template: e.target.value })}
                    />
                  </div>
                  <div className="space-y-2">
                    <Label>邮件内容模板</Label>
                    <Textarea
                      placeholder="尊敬的用户：&#10;&#10;{message}&#10;&#10;时间：{time}&#10;来自：{site_name}"
                      value={config.mail_body_template || ''}
                      onChange={(e) => setConfig({ ...config, mail_body_template: e.target.value })}
                      rows={6}
                    />
                  </div>
                  <div className="bg-muted/50 rounded-md p-3">
                    <p className="text-sm font-medium mb-2">可用占位符：</p>
                    <div className="grid grid-cols-2 gap-2 text-xs text-muted-foreground">
                      <div><code className="bg-background px-1 rounded">{'{site_name}'}</code> - 站点名称</div>
                      <div><code className="bg-background px-1 rounded">{'{title}'}</code> - 通知标题</div>
                      <div><code className="bg-background px-1 rounded">{'{message}'}</code> - 通知内容</div>
                      <div><code className="bg-background px-1 rounded">{'{time}'}</code> - 发送时间</div>
                      <div><code className="bg-background px-1 rounded">{'{domain}'}</code> - 相关域名</div>
                      <div><code className="bg-background px-1 rounded">{'{username}'}</code> - 用户名</div>
                    </div>
                  </div>
                </div>
              </CardContent>
            )}
          </Card>

          {/* Telegram 通知 */}
          <Card>
            <CardHeader className="cursor-pointer" onClick={() => toggleNotifySection('telegram')}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <MessageSquare className="h-5 w-5" />
                  <CardTitle className="text-base">Telegram</CardTitle>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={config.tgbot_enabled || false}
                    onCheckedChange={(checked) => { setConfig({ ...config, tgbot_enabled: checked }) }}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); handleTestTelegram(); }} disabled={testing === 'telegram'}>
                    {testing === 'telegram' ? <RefreshCw className="h-3 w-3 animate-spin" /> : '测试'}
                  </Button>
                </div>
              </div>
            </CardHeader>
            {expandedNotify === 'telegram' && (
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Bot Token</Label>
                  <Input
                    placeholder="123456789:ABCdefGHIjklMNOpqrsTUVwxyz"
                    value={config.tgbot_token || ''}
                    onChange={(e) => setConfig({ ...config, tgbot_token: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">从 @BotFather 获取的 Bot Token</p>
                </div>
                <div className="space-y-2">
                  <Label>Chat ID</Label>
                  <Input
                    placeholder="123456789"
                    value={config.tgbot_chatid || ''}
                    onChange={(e) => setConfig({ ...config, tgbot_chatid: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">接收通知的聊天 ID，可通过 @userinfobot 获取</p>
                </div>
              </CardContent>
            )}
          </Card>

          {/* Discord 通知 */}
          <Card>
            <CardHeader className="cursor-pointer" onClick={() => toggleNotifySection('discord')}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Bell className="h-5 w-5" />
                  <CardTitle className="text-base">Discord</CardTitle>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={config.discord_enabled || false}
                    onCheckedChange={(checked) => { setConfig({ ...config, discord_enabled: checked }) }}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); handleTestDiscord(); }} disabled={testing === 'discord'}>
                    {testing === 'discord' ? <RefreshCw className="h-3 w-3 animate-spin" /> : '测试'}
                  </Button>
                </div>
              </div>
            </CardHeader>
            {expandedNotify === 'discord' && (
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Webhook URL</Label>
                  <Input
                    placeholder="https://discord.com/api/webhooks/..."
                    value={config.discord_webhook || ''}
                    onChange={(e) => setConfig({ ...config, discord_webhook: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">在Discord频道设置中创建Webhook获取URL</p>
                </div>
              </CardContent>
            )}
          </Card>

          {/* Bark 推送 */}
          <Card>
            <CardHeader className="cursor-pointer" onClick={() => toggleNotifySection('bark')}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Send className="h-5 w-5" />
                  <CardTitle className="text-base">Bark</CardTitle>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={config.bark_enabled || false}
                    onCheckedChange={(checked) => { setConfig({ ...config, bark_enabled: checked }) }}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); handleTestBark(); }} disabled={testing === 'bark'}>
                    {testing === 'bark' ? <RefreshCw className="h-3 w-3 animate-spin" /> : '测试'}
                  </Button>
                </div>
              </div>
            </CardHeader>
            {expandedNotify === 'bark' && (
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>服务器地址</Label>
                  <Input
                    placeholder="https://api.day.app（默认）"
                    value={config.bark_server || ''}
                    onChange={(e) => setConfig({ ...config, bark_server: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">留空使用官方服务器，也可自建Bark服务器</p>
                </div>
                <div className="space-y-2">
                  <Label>Device Key</Label>
                  <Input
                    placeholder="在Bark App中获取"
                    value={config.bark_key || ''}
                    onChange={(e) => setConfig({ ...config, bark_key: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">打开Bark App复制设备Key</p>
                </div>
              </CardContent>
            )}
          </Card>

          {/* 企业微信通知 */}
          <Card>
            <CardHeader className="cursor-pointer" onClick={() => toggleNotifySection('wechat')}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <MessageSquare className="h-5 w-5" />
                  <CardTitle className="text-base">企业微信</CardTitle>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={config.wechat_enabled || false}
                    onCheckedChange={(checked) => { setConfig({ ...config, wechat_enabled: checked }) }}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); handleTestWechat(); }} disabled={testing === 'wechat'}>
                    {testing === 'wechat' ? <RefreshCw className="h-3 w-3 animate-spin" /> : '测试'}
                  </Button>
                </div>
              </div>
            </CardHeader>
            {expandedNotify === 'wechat' && (
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Webhook URL</Label>
                  <Input
                    placeholder="https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=..."
                    value={config.wechat_webhook || ''}
                    onChange={(e) => setConfig({ ...config, wechat_webhook: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">在企业微信群设置中添加群机器人获取Webhook地址</p>
                </div>
              </CardContent>
            )}
          </Card>

          {/* Webhook 通知 */}
          <Card>
            <CardHeader className="cursor-pointer" onClick={() => toggleNotifySection('webhook')}>
              <div className="flex items-center justify-between">
                <div className="flex items-center gap-2">
                  <Webhook className="h-5 w-5" />
                  <CardTitle className="text-base">Webhook</CardTitle>
                </div>
                <div className="flex items-center gap-2">
                  <Switch
                    checked={config.webhook_enabled || false}
                    onCheckedChange={(checked) => { setConfig({ ...config, webhook_enabled: checked }) }}
                    onClick={(e) => e.stopPropagation()}
                  />
                  <Button size="sm" variant="outline" onClick={(e) => { e.stopPropagation(); handleTestWebhook(); }} disabled={testing === 'webhook'}>
                    {testing === 'webhook' ? <RefreshCw className="h-3 w-3 animate-spin" /> : '测试'}
                  </Button>
                </div>
              </div>
            </CardHeader>
            {expandedNotify === 'webhook' && (
              <CardContent className="space-y-4">
                <div className="space-y-2">
                  <Label>Webhook URL</Label>
                  <Input
                    placeholder="https://example.com/webhook"
                    value={config.webhook_url || ''}
                    onChange={(e) => setConfig({ ...config, webhook_url: e.target.value })}
                  />
                  <p className="text-xs text-muted-foreground">系统将向此URL发送POST请求</p>
                </div>
              </CardContent>
            )}
          </Card>
        </TabsContent>

        {/* Cron Settings */}
        <TabsContent value="cron">
          <div className="space-y-6">
            {/* 任务状态概览 */}
            {taskStatus && (
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2">
                    <Clock className="h-5 w-5" />
                    任务状态概览
                  </CardTitle>
                  <CardDescription>后台任务运行状态</CardDescription>
                </CardHeader>
                <CardContent>
                  <div className="grid grid-cols-2 md:grid-cols-4 gap-4">
                    <div className="space-y-1.5 p-3 rounded-lg border">
                      <div className="text-sm text-muted-foreground">定时任务</div>
                      <div className="text-xl font-bold">{taskStatus.schedule.active} <span className="text-sm font-normal text-muted-foreground">/ {taskStatus.schedule.total}</span></div>
                      {taskStatus.schedule.last_time && (
                        <div className="text-xs text-muted-foreground">上次执行: {taskStatus.schedule.last_time}</div>
                      )}
                    </div>
                    <div className="space-y-1.5 p-3 rounded-lg border">
                      <div className="text-sm text-muted-foreground">优选IP</div>
                      <div className="text-xl font-bold">{taskStatus.optimize.active} <span className="text-sm font-normal text-muted-foreground">/ {taskStatus.optimize.total}</span></div>
                      {taskStatus.optimize.last_time && (
                        <div className="text-xs text-muted-foreground">上次执行: {taskStatus.optimize.last_time}</div>
                      )}
                    </div>
                    <div className="space-y-1.5 p-3 rounded-lg border">
                      <div className="text-sm text-muted-foreground">自动续期证书</div>
                      <div className="text-xl font-bold">{taskStatus.cert_auto}</div>
                    </div>
                    <div className="space-y-1.5 p-3 rounded-lg border">
                      <div className="text-sm text-muted-foreground">到期通知域名</div>
                      <div className="text-xl font-bold">{taskStatus.domain_notice}</div>
                    </div>
                  </div>
                </CardContent>
              </Card>
            )}

            {/* Cron 配置 */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2">
                  <Timer className="h-5 w-5" />
                  Cron 执行周期
                </CardTitle>
                <CardDescription>配置各定时任务的执行频率（Cron表达式）</CardDescription>
              </CardHeader>
              <CardContent className="space-y-6">
                <div className="space-y-4">
                  <div className="space-y-2">
                    <Label>定时任务执行周期</Label>
                    <Input
                      placeholder="*/1 * * * *"
                      value={cronConfig.cron_schedule || ''}
                      onChange={(e) => setCronConfig({ ...cronConfig, cron_schedule: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">用户自定义定时任务的检查周期，默认每分钟 <Badge variant="outline" className="text-xs ml-1">*/1 * * * *</Badge></p>
                  </div>
                  <div className="space-y-2">
                    <Label>优选IP执行周期</Label>
                    <Input
                      placeholder="*/30 * * * *"
                      value={cronConfig.cron_optimize || ''}
                      onChange={(e) => setCronConfig({ ...cronConfig, cron_optimize: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">优选IP任务的执行周期，默认每30分钟 <Badge variant="outline" className="text-xs ml-1">*/30 * * * *</Badge></p>
                  </div>
                  <div className="space-y-2">
                    <Label>证书自动续期检查周期</Label>
                    <Input
                      placeholder="0 * * * *"
                      value={cronConfig.cron_cert || ''}
                      onChange={(e) => setCronConfig({ ...cronConfig, cron_cert: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">自动续期证书的检查周期，默认每小时 <Badge variant="outline" className="text-xs ml-1">0 * * * *</Badge></p>
                  </div>
                  <div className="space-y-2">
                    <Label>到期通知检查周期</Label>
                    <Input
                      placeholder="0 8 * * *"
                      value={cronConfig.cron_expire || ''}
                      onChange={(e) => setCronConfig({ ...cronConfig, cron_expire: e.target.value })}
                    />
                    <p className="text-xs text-muted-foreground">域名和证书到期通知的检查周期，默认每天8点 <Badge variant="outline" className="text-xs ml-1">0 8 * * *</Badge></p>
                  </div>
                </div>
                <Separator />
                <div className="flex items-center justify-between">
                  <div className="bg-muted/50 rounded-md p-3 flex-1 mr-4">
                    <p className="text-sm font-medium mb-1">Cron 表达式说明</p>
                    <div className="text-xs text-muted-foreground space-y-0.5">
                      <p>格式：<code className="bg-background px-1 rounded">分 时 日 月 周</code></p>
                      <p><code className="bg-background px-1 rounded">*/5 * * * *</code> = 每5分钟 | <code className="bg-background px-1 rounded">0 */2 * * *</code> = 每2小时</p>
                      <p><code className="bg-background px-1 rounded">0 8 * * *</code> = 每天8点 | <code className="bg-background px-1 rounded">0 0 * * 1</code> = 每周一</p>
                    </div>
                  </div>
                  <Button onClick={handleSaveCron} disabled={savingCron}>
                    {savingCron ? (
                      <>
                        <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                        保存中...
                      </>
                    ) : (
                      <>
                        <CheckCircle className="h-4 w-4 mr-2" />
                        保存
                      </>
                    )}
                  </Button>
                </div>
              </CardContent>
            </Card>
          </div>
        </TabsContent>

        {/* Proxy Settings */}
        <TabsContent value="proxy">
          <Card>
            <CardHeader>
              <CardTitle className="flex items-center gap-2">
                <Globe className="h-5 w-5" />
                代理服务器设置
              </CardTitle>
              <CardDescription>配置HTTP/SOCKS代理用于访问外部服务</CardDescription>
            </CardHeader>
            <CardContent className="space-y-6">
              <div className="flex items-center justify-between">
                <div className="space-y-0.5">
                  <Label>启用代理</Label>
                  <p className="text-sm text-muted-foreground">通过代理服务器访问外部API</p>
                </div>
                <Switch
                  checked={config.proxy_enabled || false}
                  onCheckedChange={(checked) => setConfig({ ...config, proxy_enabled: checked })}
                />
              </div>
              <Separator />
              {config.proxy_enabled && (
                <div className="space-y-4">
                  <div className="grid grid-cols-3 gap-4">
                    <div className="space-y-2 col-span-2">
                      <Label>代理服务器</Label>
                      <Input
                        placeholder="127.0.0.1"
                        value={config.proxy_server || ''}
                        onChange={(e) => setConfig({ ...config, proxy_server: e.target.value })}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>端口</Label>
                      <Input
                        type="number"
                        placeholder="1080"
                        value={config.proxy_port || ''}
                        onChange={(e) => setConfig({ ...config, proxy_port: parseInt(e.target.value) || 0 })}
                      />
                    </div>
                  </div>
                  <div className="space-y-2">
                    <Label>代理类型</Label>
                    <Select
                      value={config.proxy_type || 'http'}
                      onValueChange={(v) => setConfig({ ...config, proxy_type: v })}
                    >
                      <SelectTrigger>
                        <SelectValue />
                      </SelectTrigger>
                      <SelectContent>
                        <SelectItem value="http">HTTP</SelectItem>
                        <SelectItem value="socks5">SOCKS5</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>
                  <div className="grid grid-cols-2 gap-4">
                    <div className="space-y-2">
                      <Label>用户名（可选）</Label>
                      <Input
                        placeholder="username"
                        value={config.proxy_user || ''}
                        onChange={(e) => setConfig({ ...config, proxy_user: e.target.value })}
                      />
                    </div>
                    <div className="space-y-2">
                      <Label>密码（可选）</Label>
                      <Input
                        type="password"
                        placeholder="••••••••"
                        value={config.proxy_password || ''}
                        onChange={(e) => setConfig({ ...config, proxy_password: e.target.value })}
                      />
                    </div>
                  </div>
                  <Button variant="outline" onClick={handleTestProxy} disabled={testing === 'proxy'}>
                    {testing === 'proxy' ? (
                      <RefreshCw className="h-4 w-4 mr-2 animate-spin" />
                    ) : (
                      <Server className="h-4 w-4 mr-2" />
                    )}
                    测试代理连接
                  </Button>
                </div>
              )}
            </CardContent>
          </Card>
        </TabsContent>
      </Tabs>

      {/* Additional Actions */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Shield className="h-5 w-5" />
            系统维护
          </CardTitle>
          <CardDescription>系统维护和缓存管理</CardDescription>
        </CardHeader>
        <CardContent>
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>清除系统缓存</Label>
              <p className="text-sm text-muted-foreground">清除DNS记录缓存和配置缓存</p>
            </div>
            <Button variant="outline" onClick={handleClearCache}>
              <RefreshCw className="h-4 w-4 mr-2" />
              清除缓存
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
