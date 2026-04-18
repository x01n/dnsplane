'use client'

import { useState, useEffect, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Label } from '@/components/ui/label'
import { Switch } from '@/components/ui/switch'
import { Separator } from '@/components/ui/separator'
import { Textarea } from '@/components/ui/textarea'
import { toast } from 'sonner'
import { authControlApi, oauthApi, OAuthProvider } from '@/lib/api'
import { RefreshCw, ShieldCheck, KeyRound, Mail, Users } from 'lucide-react'

/*
 * AuthControlSettings 登录注册控制组件
 * 功能：管理员独立控制各 OAuth 来源的登录/注册开关、密码登录、邮箱白名单等
 */
export default function AuthControlSettings() {
  const [config, setConfig] = useState<Record<string, string>>({})
  const [providers, setProviders] = useState<OAuthProvider[]>([])
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)

  const loadData = useCallback(async () => {
    setLoading(true)
    try {
      const [configRes, providerRes] = await Promise.all([
        authControlApi.get(),
        oauthApi.getProviders(),
      ])
      if (configRes.code === 0 && configRes.data) {
        // 后端返回 Record<string, unknown>；本地 state 是 Record<string, string>，统一转字符串
        const stringified: Record<string, string> = {}
        Object.entries(configRes.data).forEach(([k, v]) => {
          stringified[k] = v == null ? '' : String(v)
        })
        setConfig(stringified)
      }
      /* providers 可能直接返回数组或者在 data 字段 */
      if (Array.isArray(providerRes)) {
        setProviders(providerRes)
      } else if (providerRes && Array.isArray((providerRes as { data?: OAuthProvider[] }).data)) {
        setProviders((providerRes as { data: OAuthProvider[] }).data)
      }
    } catch {
      toast.error('加载配置失败')
    } finally {
      setLoading(false)
    }
  }, [])

  useEffect(() => { loadData() }, [loadData])

  const setVal = (key: string, value: string) => {
    setConfig((prev) => ({ ...prev, [key]: value }))
  }

  const getBool = (key: string, defaultVal = true): boolean => {
    if (config[key] === undefined) return defaultVal
    return config[key] === 'true'
  }

  const handleSave = async () => {
    setSaving(true)
    try {
      const res = await authControlApi.update(config)
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

  if (loading) {
    return (
      <div className="flex items-center justify-center h-[200px]">
        <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 密码登录与注册控制 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <KeyRound className="h-5 w-5" />
            密码登录与注册
          </CardTitle>
          <CardDescription>控制用户通过账号密码方式的登录和注册权限</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>允许密码登录</Label>
              <p className="text-sm text-muted-foreground">禁用后用户只能通过 OAuth 方式登录</p>
            </div>
            <Switch
              checked={getBool('auth_password_login')}
              onCheckedChange={(c) => setVal('auth_password_login', c ? 'true' : 'false')}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>允许密码注册</Label>
              <p className="text-sm text-muted-foreground">禁用后用户不能通过账号密码方式注册新账户</p>
            </div>
            <Switch
              checked={getBool('auth_password_register')}
              onCheckedChange={(c) => setVal('auth_password_register', c ? 'true' : 'false')}
            />
          </div>
          <Separator />
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>注册邮箱验证</Label>
              <p className="text-sm text-muted-foreground">启用后注册时需要验证邮箱地址</p>
            </div>
            <Switch
              checked={getBool('auth_email_verify', false)}
              onCheckedChange={(c) => setVal('auth_email_verify', c ? 'true' : 'false')}
            />
          </div>
        </CardContent>
      </Card>

      {/* OAuth 来源独立控制 */}
      {providers.length > 0 && (
        <Card>
          <CardHeader>
            <CardTitle className="flex items-center gap-2">
              <Users className="h-5 w-5" />
              OAuth 来源控制
            </CardTitle>
            <CardDescription>单独控制每个 OAuth 提供商的登录和注册权限</CardDescription>
          </CardHeader>
          <CardContent className="space-y-4">
            {providers.map((provider, index) => (
              <div key={provider.name}>
                {index > 0 && <Separator className="my-4" />}
                <div className="space-y-3">
                  <h4 className="font-medium flex items-center gap-2">
                    <ShieldCheck className="h-4 w-4" />
                    {provider.display_name || provider.name}
                  </h4>
                  <div className="grid grid-cols-1 md:grid-cols-2 gap-4 pl-6">
                    <div className="flex items-center justify-between">
                      <div className="space-y-0.5">
                        <Label className="text-sm">允许登录</Label>
                        <p className="text-xs text-muted-foreground">通过 {provider.display_name || provider.name} 登录</p>
                      </div>
                      <Switch
                        checked={getBool(`auth_oauth_${provider.name}_login`)}
                        onCheckedChange={(c) => setVal(`auth_oauth_${provider.name}_login`, c ? 'true' : 'false')}
                      />
                    </div>
                    <div className="flex items-center justify-between">
                      <div className="space-y-0.5">
                        <Label className="text-sm">允许注册</Label>
                        <p className="text-xs text-muted-foreground">通过 {provider.display_name || provider.name} 注册新账户</p>
                      </div>
                      <Switch
                        checked={getBool(`auth_oauth_${provider.name}_register`)}
                        onCheckedChange={(c) => setVal(`auth_oauth_${provider.name}_register`, c ? 'true' : 'false')}
                      />
                    </div>
                  </div>
                </div>
              </div>
            ))}
          </CardContent>
        </Card>
      )}

      {/* 邮箱白名单 */}
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Mail className="h-5 w-5" />
            邮箱白名单
          </CardTitle>
          <CardDescription>限制只允许特定邮箱域名的用户注册</CardDescription>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex items-center justify-between">
            <div className="space-y-0.5">
              <Label>启用邮箱白名单</Label>
              <p className="text-sm text-muted-foreground">启用后，只有白名单中的邮箱域名才能注册</p>
            </div>
            <Switch
              checked={getBool('auth_email_whitelist_enabled', false)}
              onCheckedChange={(c) => setVal('auth_email_whitelist_enabled', c ? 'true' : 'false')}
            />
          </div>
          {getBool('auth_email_whitelist_enabled', false) && (
            <div className="space-y-2">
              <Label>邮箱域名白名单</Label>
              <Textarea
                value={config['auth_email_whitelist'] || ''}
                onChange={(e) => setVal('auth_email_whitelist', e.target.value)}
                placeholder={"每行一个邮箱域名，例如：\nexample.com\n@gmail.com\ncompany.cn"}
                rows={5}
              />
              <p className="text-xs text-muted-foreground">
                支持域名后缀匹配，如 <code>example.com</code> 或 <code>@example.com</code>，每行一条
              </p>
            </div>
          )}
        </CardContent>
      </Card>

      {/* 保存按钮 */}
      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <><RefreshCw className="h-4 w-4 mr-1 animate-spin" />保存中...</> : '保存设置'}
        </Button>
      </div>
    </div>
  )
}
