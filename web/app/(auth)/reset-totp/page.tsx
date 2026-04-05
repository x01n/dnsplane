'use client'

import { useState, Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { Globe, ShieldOff, ArrowLeft, Loader2, CheckCircle, XCircle, AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { encryptedPost } from '@/lib/api'

function ResetTOTPForm() {
  const searchParams = useSearchParams()
  const token = searchParams.get('token')

  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)

  const handleSubmit = async () => {
    if (!token) {
      toast.error('无效的重置链接')
      return
    }

    setLoading(true)
    try {
      const res = await encryptedPost<{ code: number; msg?: string }>('/auth/reset-totp', { token })
      if (res.code === 0) {
        setSuccess(true)
        toast.success('二步验证已关闭')
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
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-red-100">
          <XCircle className="h-8 w-8 text-red-600" />
        </div>
        <div className="space-y-2">
          <h3 className="font-medium">无效的重置链接</h3>
          <p className="text-sm text-muted-foreground">
            此链接无效或已过期，请重新申请二步验证重置。
          </p>
        </div>
        <Link href="/login">
          <Button className="w-full">返回登录</Button>
        </Link>
      </div>
    )
  }

  if (success) {
    return (
      <div className="text-center space-y-4">
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100">
          <CheckCircle className="h-8 w-8 text-green-600" />
        </div>
        <div className="space-y-2">
          <h3 className="font-medium">二步验证已关闭</h3>
          <p className="text-sm text-muted-foreground">
            您账户的二步验证已成功关闭，现在可以使用密码登录。
          </p>
          <p className="text-sm text-muted-foreground">
            建议您登录后重新设置二步验证以保护账户安全。
          </p>
        </div>
        <Link href="/login">
          <Button className="w-full">去登录</Button>
        </Link>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      <div className="rounded-lg border border-amber-200 bg-amber-50 p-4">
        <div className="flex gap-3">
          <AlertTriangle className="h-5 w-5 text-amber-600 flex-shrink-0 mt-0.5" />
          <div className="space-y-1">
            <h4 className="font-medium text-amber-800">安全警告</h4>
            <p className="text-sm text-amber-700">
              此操作将关闭您账户的二步验证(TOTP)功能。关闭后，您只需输入密码即可登录。
            </p>
            <p className="text-sm text-amber-700">
              如果您没有发起此请求，请立即修改密码并联系管理员。
            </p>
          </div>
        </div>
      </div>

      <Button
        onClick={handleSubmit}
        className="w-full bg-amber-600 hover:bg-amber-700"
        disabled={loading}
      >
        {loading ? (
          <>
            <Loader2 className="mr-2 h-4 w-4 animate-spin" />
            处理中...
          </>
        ) : (
          <>
            <ShieldOff className="mr-2 h-4 w-4" />
            确认关闭二步验证
          </>
        )}
      </Button>

      <div className="text-center">
        <Link href="/login" className="text-sm text-muted-foreground hover:text-primary">
          <ArrowLeft className="inline mr-1 h-3 w-3" />
          返回登录
        </Link>
      </div>
    </div>
  )
}

export default function ResetTOTPPage() {
  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-900 via-purple-900 to-slate-900 p-4 relative overflow-hidden">
      {/* 背景装饰 */}
      <div className="absolute inset-0 overflow-hidden">
        <div className="absolute -top-40 -right-40 w-80 h-80 bg-purple-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob"></div>
        <div className="absolute -bottom-40 -left-40 w-80 h-80 bg-cyan-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob animation-delay-2000"></div>
      </div>

      <Card className="w-full max-w-md relative backdrop-blur-sm bg-white/95 dark:bg-slate-900/95 shadow-2xl border-0">
        <CardHeader className="text-center">
          <div className="mx-auto mb-4 flex h-12 w-12 items-center justify-center rounded-full bg-amber-500 shadow-lg shadow-amber-500/30">
            <Globe className="h-6 w-6 text-white" />
          </div>
          <CardTitle className="text-2xl">重置二步验证</CardTitle>
          <CardDescription>
            关闭账户的二步验证功能
          </CardDescription>
        </CardHeader>
        <CardContent>
          <Suspense fallback={<div className="flex justify-center"><Loader2 className="h-6 w-6 animate-spin" /></div>}>
            <ResetTOTPForm />
          </Suspense>
        </CardContent>
      </Card>
    </div>
  )
}
