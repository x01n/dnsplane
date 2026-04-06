'use client'

import { useEffect, useState } from 'react'
import Link from 'next/link'
import { authApi } from '@/lib/api'

const linkClass =
  'text-sm text-neutral-500 transition-colors hover:text-neutral-900 dark:hover:text-neutral-100'

export type AuthFooterNavCurrent =
  | 'login'
  | 'register'
  | 'forgot'
  | 'forgot_totp'
  | 'reset'
  | 'magic'

/**
 * 登录 / 注册 / 找回密码 / 无密码登录 之间的统一导航（注册仍依 /auth/config；邮箱登录入口始终展示，是否可用由服务端校验）。
 */
export function AuthFooterNav({ current }: { current: AuthFooterNavCurrent }) {
  const [registerEnabled, setRegisterEnabled] = useState(false)

  useEffect(() => {
    void authApi.getConfig().then((res) => {
      if (res.code === 0 && res.data) {
        setRegisterEnabled(!!res.data.register_enabled)
      }
    })
  }, [])

  return (
    <nav
      className="mt-6 flex flex-wrap items-center justify-center gap-x-4 gap-y-2 border-t border-neutral-200/90 pt-5 dark:border-neutral-700/80"
      aria-label="认证页面导航"
    >
      {current !== 'login' && (
        <Link href="/login/" className={linkClass}>
          登录
        </Link>
      )}
      {registerEnabled && current !== 'register' && (
        <Link href="/register/" className={linkClass}>
          注册
        </Link>
      )}
      {current !== 'magic' && (
        <Link href="/magic-link/" className={linkClass}>
          邮箱登录
        </Link>
      )}
      {current !== 'forgot' && current !== 'forgot_totp' && (
        <Link href="/forgot-password/" className={linkClass}>
          忘记密码
        </Link>
      )}
    </nav>
  )
}
