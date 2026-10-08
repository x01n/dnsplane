'use client'

import { useCallback, useEffect, useState } from 'react'
import { authApi } from '@/lib/api'
import { isValidSiteKey } from '@/lib/site-key'

/**
 * usePageSize - 分页大小持久化 hook
 * @param key 存储键名标识
 * @param defaultSize 默认分页大小
 */
export function usePageSize(key: string, defaultSize = 20): [number, (size: number) => void] {
  const storageKey = `page_size_${key}`
  const [pageSize, setPageSize] = useState(() => {
    if (typeof window === 'undefined') return defaultSize
    const saved = localStorage.getItem(storageKey)
    return saved ? parseInt(saved, 10) : defaultSize
  })

  const updatePageSize = (size: number) => {
    setPageSize(size)
    localStorage.setItem(storageKey, String(size))
  }

  return [pageSize, updatePageSize]
}

/**
 * Turnstile 独立校验的三态判定结果
 *
 * 后端 GET /api/auth/config 的 turnstile_standalone_required 是无条件下发的布尔值，
 * 但前端不能只按它渲染：判据与站点密钥来自两次独立的配置读取，
 * 私钥存在而站点密钥缺失或格式非法时，后端会拒绝请求而前端渲染不出组件，
 * 用户将陷入无法通过的提交死锁
 */
export type TurnstileGateStatus =
  /** 配置尚未拉取完成 */
  | 'loading'
  /** 后端明确表示不校验独立 Turnstile，前端不渲染也不拦截提交 */
  | 'off'
  /** 需要校验且站点密钥可用，应渲染 Turnstile 组件 */
  | 'ready'
  /** 需要校验但站点密钥缺失或格式非法，必须显式报错而非静默放行 */
  | 'misconfigured'
  /** 配置接口不可用，无法判断后端是否校验，按需要校验处理并提示重试 */
  | 'unavailable'

export type TurnstileGate = {
  status: TurnstileGateStatus
  /** 可直接交给 Turnstile SDK 的站点密钥，仅 status 为 ready 时非空 */
  siteKey: string
  /** 是否必须渲染人机验证组件并等待 token */
  enabled: boolean
  /** 重新拉取认证配置，用于 unavailable 状态下的重试 */
  retry: () => void
}

/**
 * useTurnstileGate - 判定独立 Turnstile 的三态并给出站点密钥
 *
 * 判定规则以「后端是否会拒绝」为准，而不是「前端能否渲染」：
 * 1. turnstile_standalone_required 为 false 时后端不校验，直接关闭
 * 2. 为 true 或字段缺失（连到尚未下发该信号的后端）时，后端都可能校验，
 *    此时有合法站点密钥就渲染组件，没有则报错，绝不静默放行
 * 第 2 条同时覆盖了后端升级前后的两种部署，避免旧后端下退回死锁
 *
 * @returns status 三态结果、siteKey 站点密钥、enabled 是否需要提交 token、retry 重试函数
 */
export function useTurnstileGate(): TurnstileGate {
  const [gate, setGate] = useState<Omit<TurnstileGate, 'retry'>>({
    status: 'loading',
    siteKey: '',
    enabled: false,
  })
  const [reloadNonce, setReloadNonce] = useState(0)

  useEffect(() => {
    let cancelled = false
    void authApi
      .getConfig()
      .then((res) => {
        if (cancelled) return
        if (res.code !== 0 || !res.data) {
          setGate({ status: 'unavailable', siteKey: '', enabled: false })
          return
        }
        if (res.data.turnstile_standalone_required === false) {
          setGate({ status: 'off', siteKey: '', enabled: false })
          return
        }
        const raw = res.data.turnstile_site_key
        if (isValidSiteKey(raw)) {
          setGate({ status: 'ready', siteKey: raw, enabled: true })
          return
        }
        setGate({ status: 'misconfigured', siteKey: '', enabled: true })
      })
      .catch(() => {
        if (!cancelled) setGate({ status: 'unavailable', siteKey: '', enabled: false })
      })
    return () => {
      cancelled = true
    }
  }, [reloadNonce])

  const retry = useCallback(() => setReloadNonce((n) => n + 1), [])

  return { ...gate, retry }
}
