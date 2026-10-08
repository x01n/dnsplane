'use client'

import { AlertTriangle, RefreshCw } from 'lucide-react'
import type { TurnstileGateStatus } from '@/lib/hooks'

type Props = {
  status: TurnstileGateStatus
  onRetry: () => void
  disabled?: boolean
}

/**
 * TurnstileGateNotice - 独立 Turnstile 无法渲染时的显式提示
 *
 * 后端已配置私有密钥（会拒绝缺少 token 的请求）但前端拿不到可用站点密钥时，
 * 若静默不渲染组件，用户会看到「提交被拒绝却没有任何可操作的验证入口」的死锁。
 * 这里把配置故障暴露出来，并给出重试入口，让用户能区分「需要验证」与「配置损坏」。
 *
 * @param status 三态判定结果，仅 misconfigured 与 unavailable 会渲染内容
 * @param onRetry 重新拉取认证配置的回调
 * @param disabled 提交进行中时禁用重试按钮
 */
export function TurnstileGateNotice({ status, onRetry, disabled }: Props) {
  if (status === 'misconfigured') {
    return (
      <div
        role="alert"
        className="mb-4 flex gap-3 rounded-lg border border-destructive/35 bg-destructive/5 p-3"
      >
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-destructive" aria-hidden />
        <div className="space-y-1 text-sm">
          <p className="font-medium text-destructive">人机验证配置不完整</p>
          <p className="text-destructive/90 leading-snug">
            管理员已启用 Turnstile 校验，但站点密钥缺失或格式不合法，验证组件无法加载。
            请联系管理员在系统设置的「敏感操作验证」中重新填写 Turnstile 站点密钥。
          </p>
        </div>
      </div>
    )
  }

  if (status === 'unavailable') {
    return (
      <div
        role="alert"
        className="mb-4 flex gap-3 rounded-lg border border-amber-300 bg-amber-50 p-3"
      >
        <AlertTriangle className="mt-0.5 h-4 w-4 shrink-0 text-amber-600" aria-hidden />
        <div className="space-y-2 text-sm">
          <p className="font-medium text-amber-800">无法确认人机验证要求</p>
          <p className="text-amber-700 leading-snug">
            读取认证配置失败，暂时无法判断本次操作是否需要人机验证。
            为避免提交被服务端拒绝，请先重试加载配置。
          </p>
          <button
            type="button"
            onClick={onRetry}
            disabled={disabled}
            className="inline-flex items-center gap-1.5 rounded-md border border-amber-400 bg-white px-2.5 py-1 text-xs font-medium text-amber-800 transition-colors hover:bg-amber-100 disabled:cursor-not-allowed disabled:opacity-60"
          >
            <RefreshCw className="h-3 w-3" aria-hidden />
            重新加载配置
          </button>
        </div>
      </div>
    )
  }

  return null
}