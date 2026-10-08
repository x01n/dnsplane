'use client'

import { useEffect, useRef, useState } from 'react'
import { useRouter } from 'next/navigation'
import Link from 'next/link'
import {
  CheckCircle2,
  KeyRound,
  RefreshCw,
  ShieldAlert,
  ShieldCheck,
  TriangleAlert,
} from 'lucide-react'
import { oauthApi } from '@/lib/api'
import { toast } from 'sonner'
import { OAUTH_CALLBACK_ERRORS } from '@/lib/oauth-callback'
import { AuthAnimatedLayout } from '@/components/auth/auth-animated-layout'
import cb from '@/components/auth/oauth-callback.module.css'
import shell from '@/components/auth/animated-login-shell.module.css'

/** 成功态停留时长，让用户看清「登录成功」再跳转 */
const REDIRECT_DELAY_MS = 800

/** 回调处理阶段：验证中 → 成功 / 失败，每阶段渲染不同的图标与文案 */
type Phase = 'verifying' | 'success' | 'error'

/** 失败原因：unknown 表示 URL 未带任何可用参数，通常是链接被重放或直接访问 */
type FailureReason = 'unknown' | { code: string; message: string }

/** 成功态去向：登录进控制台，绑定回个人资料页 */
type SuccessTarget = { kind: 'login' | 'bind'; href: string }

const idleCharacters = {
  username: '',
  password: '',
  usernameFocused: false,
  passwordFocused: false,
  showPassword: false,
  errorNonce: 0,
}

/** 把回调错误码或原始消息转成可展示的中文原因 */
function describeFailure(reason: FailureReason): string {
  if (reason === 'unknown') {
    return '未收到登录凭据。该链接可能已被使用或已过期，请重新发起登录。'
  }
  return reason.message
}

/** 后端返回的失败原因是错误码而非文案，未收录的码回落到通用提示 */
function toFailure(code: string): FailureReason {
  return {
    code,
    message: OAUTH_CALLBACK_ERRORS[code] ?? `登录失败：${code}`,
  }
}

function OAuthCallbackInner() {
  const router = useRouter()
  const [phase, setPhase] = useState<Phase>('verifying')
  const [failure, setFailure] = useState<FailureReason>('unknown')
  const [successTarget, setSuccessTarget] = useState<SuccessTarget | null>(null)
  // 阶段推进只做一次；React 严格模式下 effect 会执行两次，用 ref 守住动画与跳转
  const startedRef = useRef(false)

  useEffect(() => {
    let cancelled = false

    // 用一个宏任务把阶段推进推到首帧绘制之后：预渲染出的 HTML 就是「验证中」，
    // 这样用户先看到明确的处理中状态再切结果；同时避免在 effect 体内同步 setState
    // 触发级联渲染（React 会把这些更新合并进同一次渲染，阶段反馈反而消失）
    const timer = setTimeout(async () => {
      if (cancelled || startedRef.current) return
      startedRef.current = true

      const params = new URLSearchParams(window.location.search)
      const error = params.get('error')
      if (error) {
        setFailure(toFailure(error))
        setPhase('error')
        return
      }

      const code = params.get('code') ?? ''
      const state = params.get('state') ?? ''
      if (!code || !state) {
        setFailure(toFailure('invalid_request'))
        setPhase('error')
        return
      }

      // code 与 state 都是一次性凭据，发请求前先清掉查询串，避免被复制进书签或留在浏览器历史
      window.history.replaceState(null, '', window.location.pathname)

      let res: Awaited<ReturnType<typeof oauthApi.exchange>>
      try {
        res = await oauthApi.exchange({ code, state })
      } catch {
        if (cancelled) return
        setFailure('unknown')
        setPhase('error')
        return
      }
      if (cancelled) return

      if (res.code !== 0 || !res.data) {
        setFailure(toFailure(res.msg || 'login_failed'))
        setPhase('error')
        return
      }

      if (res.data.mode === 'bind') {
        toast.success('第三方账号已绑定')
        setSuccessTarget({
          kind: 'bind',
          href: res.data.redirect || '/dashboard/profile?bind=success',
        })
        setPhase('success')
        return
      }

      toast.success('登录成功')
      setSuccessTarget({ kind: 'login', href: res.data.redirect || '/dashboard/' })
      setPhase('success')
    }, 0)

    return () => {
      cancelled = true
      clearTimeout(timer)
    }
  }, [])

  useEffect(() => {
    if (phase !== 'success' || !successTarget) return
    const timer = setTimeout(() => router.replace(successTarget.href), REDIRECT_DELAY_MS)
    return () => clearTimeout(timer)
  }, [phase, successTarget, router])

  // 回调页没有可输入的表单，角色动画一律按静止态传入
  const layoutProps = {
    variant: 'classic' as const,
    ...idleCharacters,
  }

  if (phase === 'success' && successTarget) {
    const isBind = successTarget.kind === 'bind'
    return (
      <AuthAnimatedLayout
        title={isBind ? '绑定成功' : '登录成功'}
        description={isBind ? '正在返回个人资料' : '正在进入控制台'}
        {...layoutProps}
      >
        <div className={cb.stageRoot} role="status" aria-live="polite">
          <div className={cb.stageIcon}>
            <span className={cb.successHalo} aria-hidden />
            <span className={cb.successBadge}>
              <CheckCircle2 className="h-10 w-10" aria-hidden />
            </span>
          </div>
          <div className="space-y-1.5">
            <p className={cb.stageTitle}>
              {isBind ? '第三方账号已绑定' : '身份验证已完成'}
            </p>
            <p className={cb.stageHint}>
              {isBind
                ? '该第三方账号已关联到你的账户，正在返回个人资料页。'
                : '登录凭据已安全保存，正在为你打开管理控制台。'}
            </p>
          </div>
          <div className={cb.progressTrack} aria-hidden>
            <div className={cb.progressFill} />
          </div>
          <Link href={successTarget.href} className={`${shell.btnPrimaryClassic} ${cb.actionRow}`}>
            {isBind ? '返回个人资料' : '立即进入控制台'}
          </Link>
        </div>
      </AuthAnimatedLayout>
    )
  }

  if (phase === 'error') {
    const message = describeFailure(failure)
    return (
      <AuthAnimatedLayout
        title="登录未完成"
        description="第三方登录未能完成"
        {...layoutProps}
      >
        <div className={cb.stageRoot} role="alert">
          <span className={cb.errorBadge}>
            <ShieldAlert className="h-10 w-10" aria-hidden />
          </span>
          <div className="space-y-1.5">
            <p className={cb.stageTitle}>无法完成第三方登录</p>
            <p className={cb.errorReason}>{message}</p>
          </div>

          {failure !== 'unknown' && (
            <p className={`${cb.stageHint} flex items-center gap-1.5`}>
              <TriangleAlert className="h-3.5 w-3.5 shrink-0" aria-hidden />
              错误代码：{failure.code}
            </p>
          )}

          <div className={cb.actionRow}>
            <Link
              href={
                failure === 'unknown'
                  ? '/login'
                  : `/login?error=${encodeURIComponent(failure.code)}`
              }
              className={shell.btnPrimaryClassic}
            >
              <KeyRound className="h-4 w-4" aria-hidden />
              重新登录
            </Link>
            <Link href="/magic-link/" className={shell.linkMutedClassic}>
              改用邮箱验证码登录
            </Link>
          </div>

          <p className={cb.stageHint}>
            若反复失败，请确认第三方账号未被其他账户绑定，或联系管理员检查第三方登录配置。
          </p>
        </div>
      </AuthAnimatedLayout>
    )
  }

  return (
    <AuthAnimatedLayout
      title="第三方登录"
      description="正在验证登录凭据"
      {...layoutProps}
    >
      <div className={cb.stageRoot} role="status" aria-live="polite">
        <div className={cb.stageIcon}>
          <span className={cb.pendingPulse} aria-hidden />
          <span className={cb.pendingRing} aria-hidden />
          <span className={cb.pendingCore}>
            <ShieldCheck className="h-7 w-7" aria-hidden />
          </span>
        </div>
        <div className="space-y-1.5">
          <p className={cb.stageTitle}>正在验证登录凭据</p>
          <p className={cb.stageHint}>
            正在与第三方平台核对身份信息，请勿关闭此页面。
          </p>
        </div>
        <p className={`${cb.stageHint} flex items-center gap-1.5 text-xs`}>
          <RefreshCw className="h-3.5 w-3.5 shrink-0" aria-hidden />
          通常只需一两秒
        </p>
      </div>
    </AuthAnimatedLayout>
  )
}

/**
 * OAuthCallbackView - 第三方登录回调页
 *
 * 第三方统一回跳到本页并带上 code 与 state，页面调用后端交换接口完成登录，
 * 令牌只经 HttpOnly cookie 下发，不经过任何 URL。
 *
 * 不依赖 useSearchParams：静态导出下该 hook 会把整棵子树推给客户端渲染，
 * 页面只剩「加载中…」占位；这里改为在 effect 中直接读 window.location，
 * 让预渲染 HTML 就是真实的「正在验证登录凭据」阶段，首屏即有内容
 */
export function OAuthCallbackView() {
  return <OAuthCallbackInner />
}