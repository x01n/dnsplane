'use client'

import { useEffect } from 'react'
import { toast } from 'sonner'

/*
 * GlobalErrorHandler 全局未捕获异常处理器
 * 功能：监听 window.onerror 和 unhandledrejection 事件
 *       将未被 ErrorBoundary 捕获的运行时错误和未处理的 Promise 拒绝
 *       以 toast 形式通知用户，同时记录到 console
 * 注意：React 渲染错误由 ErrorBoundary 和 error.tsx 捕获，
 *       本组件主要处理异步操作、事件回调等非渲染阶段的异常
 */
export function GlobalErrorHandler() {
  useEffect(() => {
    /* 全局 JS 运行时错误 */
    const handleError = (event: ErrorEvent) => {
      /* 忽略跨域脚本错误（无法获取详情） */
      if (event.message === 'Script error.') return

      /* 忽略 ResizeObserver 循环限制错误（浏览器误报，不影响功能） */
      if (event.message?.includes('ResizeObserver')) return

      console.error('[GlobalError]', event.error || event.message)
      toast.error('运行时错误', {
        description: event.message || '发生了未知错误',
        duration: 5000,
      })
    }

    /* 未处理的 Promise 拒绝 */
    const handleRejection = (event: PromiseRejectionEvent) => {
      /* 忽略 AbortError（fetch 被取消，属于正常行为） */
      if (event.reason?.name === 'AbortError') return

      /* 忽略网络错误的重复提示（api 层已有 toast） */
      if (event.reason?.message?.includes('Failed to fetch')) return
      if (event.reason?.message?.includes('NetworkError')) return

      console.error('[UnhandledRejection]', event.reason)
      const msg = event.reason?.message || String(event.reason) || '异步操作失败'
      toast.error('操作异常', {
        description: msg.length > 200 ? msg.slice(0, 200) + '...' : msg,
        duration: 5000,
      })
    }

    window.addEventListener('error', handleError)
    window.addEventListener('unhandledrejection', handleRejection)

    return () => {
      window.removeEventListener('error', handleError)
      window.removeEventListener('unhandledrejection', handleRejection)
    }
  }, [])

  return null
}
