'use client'

import { useState, Suspense } from 'react'
import { useSearchParams } from 'next/navigation'
import Link from 'next/link'
import { toast } from 'sonner'
import { ShieldOff, ArrowLeft, Loader2, CheckCircle, XCircle, AlertTriangle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { encryptedPost } from '@/lib/api'
import { AuthAnimatedLayout } from '@/components/auth/auth-animated-layout'

function ResetTOTPContent() {
  const searchParams = useSearchParams()
  const token = searchParams.get('token')

  const [loading, setLoading] = useState(false)
  const [success, setSuccess] = useState(false)
  const [characterErrorNonce, setCharacterErrorNonce] = useState(0)

  const bumpError = () => setCharacterErrorNonce((n) => n + 1)

  const idleCharacters = {
    username: '',
    password: '',
    usernameFocused: false,
    passwordFocused: false,
    showPassword: false,
    errorNonce: characterErrorNonce,
  }

  const handleSubmit = async () => {
    if (!token) {
      toast.error('无效的重置链接')
      bumpError()
      return
    }

    setLoading(true)
    try {
      const res = await encryptedPost<{ code: number; msg?: string }>(
        '/auth/reset-totp',
        { token },
      )
      if (res.code === 0) {
        setSuccess(true)
        toast.success('二步验证已关闭')
      } else {
        toast.error(res.msg || '重置失败')
        bumpError()
      }
    } catch {
      toast.error('重置失败，请稍后重试')
      bumpError()
    } finally {
      setLoading(false)
    }
  }

  if (!token) {
    return (
      <AuthAnimatedLayout
        title="链接无效"
        description="无法完成二步验证重置"
        leftFooter={
          <span className="flex items-center gap-1.5">
            <ShieldOff className="h-3.5 w-3.5 opacity-70" aria-hidden />
            安全 DNS 管理
          </span>
        }
        {...idleCharacters}
      >
        <div className="space-y-4 text-center">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-red-100">
            <XCircle className="h-8 w-8 text-red-600" />
          </div>
          <p className="text-sm text-neutral-600">
            此链接无效或已过期，请重新申请二步验证重置。
          </p>
          <Link href="/login">
            <Button className="w-full">返回登录</Button>
          </Link>
        </div>
      </AuthAnimatedLayout>
    )
  }

  if (success) {
    return (
      <AuthAnimatedLayout
        title="二步验证已关闭"
        description="现在可以使用密码登录"
        leftFooter={
          <span className="flex items-center gap-1.5">
            <ShieldOff className="h-3.5 w-3.5 opacity-70" aria-hidden />
            安全 DNS 管理
          </span>
        }
        {...idleCharacters}
      >
        <div className="space-y-4 text-center">
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-green-100">
            <CheckCircle className="h-8 w-8 text-green-600" />
          </div>
          <p className="text-sm text-neutral-600">
            您账户的二步验证已成功关闭，现在可以使用密码登录。
          </p>
          <p className="text-xs text-neutral-500">
            建议您登录后重新设置二步验证以保护账户安全。
          </p>
          <Link href="/login">
            <Button className="w-full">去登录</Button>
          </Link>
        </div>
      </AuthAnimatedLayout>
    )
  }

  return (
    <AuthAnimatedLayout
      title="重置二步验证"
      description="关闭账户的二步验证功能"
      leftFooter={
        <span className="flex items-center gap-1.5">
          <ShieldOff className="h-3.5 w-3.5 opacity-70" aria-hidden />
          安全 DNS 管理
        </span>
      }
      {...idleCharacters}
    >
      <div className="space-y-4">
        <div className="rounded-lg border border-amber-200 bg-amber-50 p-4">
          <div className="flex gap-3">
            <AlertTriangle className="mt-0.5 h-5 w-5 flex-shrink-0 text-amber-600" />
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

        <button
          type="button"
          onClick={handleSubmit}
          disabled={loading}
          className="flex h-[50px] w-full items-center justify-center gap-2 rounded-[25px] bg-amber-600 text-[15px] font-semibold text-white transition-colors hover:bg-amber-700 disabled:opacity-70"
        >
          {loading ? (
            <>
              <Loader2 className="h-4 w-4 animate-spin" />
              处理中...
            </>
          ) : (
            <>
              <ShieldOff className="h-4 w-4" />
              确认关闭二步验证
            </>
          )}
        </button>

        <div className="text-center">
          <Link
            href="/login"
            className="inline-flex items-center text-sm text-neutral-500 hover:text-[#5b21b6]"
          >
            <ArrowLeft className="mr-1 h-3 w-3" />
            返回登录
          </Link>
        </div>
      </div>
    </AuthAnimatedLayout>
  )
}

export default function ResetTOTPPage() {
  return (
    <Suspense
      fallback={
        <div className="flex min-h-screen items-center justify-center bg-[#1a1a2e]">
          <Loader2 className="h-8 w-8 animate-spin text-white/70" />
        </div>
      }
    >
      <ResetTOTPContent />
    </Suspense>
  )
}
