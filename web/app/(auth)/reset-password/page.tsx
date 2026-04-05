'use client'

import { useState, Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Globe, Eye, EyeOff, ArrowLeft, Loader2, CheckCircle, XCircle, Check, X } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { encryptedPost } from '@/lib/api'

function ResetPasswordForm() {
  const searchParams = useSearchParams()
  const token = searchParams.get('token')

  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)
  const [showPassword, setShowPassword] = useState(false)
  const [showConfirmPassword, setShowConfirmPassword] = useState(false)
  const [formData, setFormData] = useState({
    password: '',
    confirmPassword: '',
  })

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!token) {
      toast.error('无效的重置链接')
      return
    }

    if (!formData.password) {
      toast.error('请输入新密码')
      return
    }

    if (formData.password.length < 8) {
      toast.error('密码长度至少8位')
      return
    }
    if (!/[A-Z]/.test(formData.password)) {
      toast.error('密码需包含至少一个大写字母')
      return
    }
    if (!/[a-z]/.test(formData.password)) {
      toast.error('密码需包含至少一个小写字母')
      return
    }
    if (!/[0-9]/.test(formData.password)) {
      toast.error('密码需包含至少一个数字')
      return
    }

    if (formData.password !== formData.confirmPassword) {
      toast.error('两次输入的密码不一致')
      return
    }

    setLoading(true)
    try {
      const res = await encryptedPost<{ code: number; msg?: string }>('/auth/reset-password', {
        token,
        password: formData.password,
      })
      if (res.code === 0) {
        setSuccess(true)
        toast.success('密码重置成功')
      } else {
        toast.error(res.msg || '重置失败')
      }
    } catch {
      toast.error('重置失败，请稍后重试')
    } finally {
      setLoading(false)
    }
  }

  if (!token) {
    return (
      <div className="text-center space-y-4">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-red-100 dark:bg-red-900/30">
          <XCircle className="h-8 w-8 text-red-600 dark:text-red-400" />
        </div>
        <div className="space-y-2">
          <h3 className="font-medium">无效的重置链接</h3>
          <p className="text-sm text-muted-foreground">
            此链接无效或已过期，请重新申请密码重置。
          </p>
        </div>
        <Link href="/forgot-password">
          <Button className="w-full">重新申请</Button>
        </Link>
      </div>
    )
  }

  if (success) {
    return (
      <div className="text-center space-y-4">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100 dark:bg-green-900/30">
          <CheckCircle className="h-8 w-8 text-green-600 dark:text-green-400" />
        </div>
        <div className="space-y-2">
          <h3 className="font-medium">密码重置成功</h3>
          <p className="text-sm text-muted-foreground">
            您的密码已成功重置，请使用新密码登录。
          </p>
        </div>
        <Link href="/login">
          <Button className="w-full">去登录</Button>
        </Link>
      </div>
    )
  }

  return (
    <form onSubmit={handleSubmit} className="space-y-4">
      <div className="space-y-2">
        <Label htmlFor="password">新密码</Label>
        <div className="relative">
          <Input
            id="password"
            type={showPassword ? 'text' : 'password'}
            placeholder="8位以上，含大小写和数字"
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
      {formData.password && (
        <div className="space-y-1 text-xs">
          {[
            { ok: formData.password.length >= 8, text: '至少8个字符' },
            { ok: /[A-Z]/.test(formData.password), text: '包含大写字母' },
            { ok: /[a-z]/.test(formData.password), text: '包含小写字母' },
            { ok: /[0-9]/.test(formData.password), text: '包含数字' },
          ].map((rule) => (
            <div key={rule.text} className={`flex items-center gap-1.5 ${rule.ok ? 'text-green-600 dark:text-green-400' : 'text-muted-foreground'}`}>
              {rule.ok ? <Check className="h-3 w-3" /> : <X className="h-3 w-3" />}
              <span>{rule.text}</span>
            </div>
          ))}
        </div>
      )}

      <div className="space-y-2">
        <Label htmlFor="confirmPassword">确认密码</Label>
        <div className="relative">
          <Input
            id="confirmPassword"
            type={showConfirmPassword ? 'text' : 'password'}
            placeholder="请再次输入新密码"
            value={formData.confirmPassword}
            onChange={(e) => setFormData({ ...formData, confirmPassword: e.target.value })}
            disabled={loading}
          />
          <Button
            type="button"
            variant="ghost"
            size="icon"
            className="absolute right-0 top-0 h-full px-3 hover:bg-transparent"
            onClick={() => setShowConfirmPassword(!showConfirmPassword)}
          >
            {showConfirmPassword ? (
              <EyeOff className="h-4 w-4 text-muted-foreground" />
            ) : (
              <Eye className="h-4 w-4 text-muted-foreground" />
            )}
          </Button>
        </div>
      </div>

      <Button type="submit" className="w-full" disabled={loading}>
        {loading ? (
          <>
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            重置中...
          </>
        ) : (
          '重置密码'
        )}
      </Button>

      <div className="text-center">
        <Link href="/login" className="text-sm text-muted-foreground hover:text-primary">
          <ArrowLeft className="inline mr-1 h-3 w-3" />
          返回登录
        </Link>
      </div>
    </form>
  )
}

export default function ResetPasswordPage() {
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
            <CardTitle className="text-xl">重置密码</CardTitle>
            <CardDescription>请设置您的新密码</CardDescription>
          </CardHeader>
          <CardContent>
            <Suspense fallback={<div className="flex justify-center"><Loader2 className="h-6 w-6 animate-spin" /></div>}>
              <ResetPasswordForm />
            </Suspense>
          </CardContent>
        </Card>
      </div>
    </div>
  )
}
