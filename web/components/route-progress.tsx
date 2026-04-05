'use client'

import { useEffect, useRef, useCallback } from 'react'
import { usePathname, useSearchParams } from 'next/navigation'

/*
 * RouteProgress 路由切换顶部进度条
 * 功能：监听 Next.js 路由变化，在页面切换时显示顶部蓝色进度条动画
 *       无需第三方依赖（替代 NProgress），纯 DOM 操作避免 setState 级联渲染
 * 原理：pathname 或 searchParams 变化时，通过 ref 直接操作 DOM 元素的 style
 */
export function RouteProgress() {
  const pathname = usePathname()
  const searchParams = useSearchParams()
  const barRef = useRef<HTMLDivElement>(null)
  const containerRef = useRef<HTMLDivElement>(null)
  const prevPath = useRef(pathname)
  const timerRef = useRef<ReturnType<typeof setInterval> | null>(null)
  const progressRef = useRef(0)

  const startProgress = useCallback(() => {
    if (!barRef.current || !containerRef.current) return
    if (timerRef.current) clearInterval(timerRef.current)
    progressRef.current = 20
    containerRef.current.style.opacity = '1'
    barRef.current.style.width = '20%'
    timerRef.current = setInterval(() => {
      if (progressRef.current >= 90) {
        if (timerRef.current) clearInterval(timerRef.current)
        return
      }
      progressRef.current += Math.random() * 10
      if (barRef.current) barRef.current.style.width = `${progressRef.current}%`
    }, 200)
  }, [])

  const completeProgress = useCallback(() => {
    if (timerRef.current) clearInterval(timerRef.current)
    progressRef.current = 100
    if (barRef.current) barRef.current.style.width = '100%'
    setTimeout(() => {
      if (containerRef.current) containerRef.current.style.opacity = '0'
      setTimeout(() => {
        progressRef.current = 0
        if (barRef.current) barRef.current.style.width = '0%'
      }, 300)
    }, 200)
  }, [])

  useEffect(() => {
    const currentPath = pathname + (searchParams?.toString() || '')
    if (currentPath !== prevPath.current) {
      startProgress()
      const t = setTimeout(completeProgress, 150)
      prevPath.current = pathname
      return () => clearTimeout(t)
    }
  }, [pathname, searchParams, startProgress, completeProgress])

  return (
    <div
      ref={containerRef}
      className="fixed top-0 left-0 right-0 z-[9999] h-[2px] pointer-events-none transition-opacity duration-300"
      style={{ opacity: 0 }}
    >
      <div
        ref={barRef}
        className="h-full bg-primary shadow-[0_0_8px_rgba(59,130,246,0.5)] transition-[width] duration-300 ease-out"
        style={{ width: '0%' }}
      />
    </div>
  )
}
