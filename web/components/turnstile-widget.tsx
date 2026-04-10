'use client'

import { useEffect, useRef } from 'react'
import { Label } from '@/components/ui/label'

function loadScriptOnce(src: string, attr: string): Promise<void> {
  return new Promise((resolve, reject) => {
    const existing = document.querySelector(`script[${attr}]`)
    if (existing) {
      if ((existing as HTMLScriptElement).dataset.loaded === '1') {
        resolve()
        return
      }
      existing.addEventListener('load', () => resolve(), { once: true })
      existing.addEventListener('error', () => reject(new Error('script load failed')), { once: true })
      return
    }
    const s = document.createElement('script')
    s.src = src
    s.async = true
    s.setAttribute(attr, '1')
    s.onload = () => {
      s.dataset.loaded = '1'
      resolve()
    }
    s.onerror = () => reject(new Error('script load failed'))
    document.body.appendChild(s)
  })
}

type Props = {
  siteKey: string
  onToken: (token: string) => void
  refreshSignal?: number
  disabled?: boolean
  label?: string
}

/** 独立 Cloudflare Turnstile（用于忘记密码等与主验证码配置分离的场景） */
export function TurnstileWidget({ siteKey, onToken, refreshSignal = 0, disabled, label = '人机验证' }: Props) {
  const hostRef = useRef<HTMLDivElement>(null)
  const widgetIdRef = useRef<string | null>(null)
  const onTokenRef = useRef(onToken)

  useEffect(() => {
    onTokenRef.current = onToken
  }, [onToken])

  useEffect(() => {
    if (!siteKey || disabled) {
      onTokenRef.current('')
      return
    }
    const host = hostRef.current
    if (!host) return
    host.innerHTML = ''
    widgetIdRef.current = null
    let cancelled = false

    const run = async () => {
      try {
        await loadScriptOnce('https://challenges.cloudflare.com/turnstile/v0/api.js', 'data-cf-turnstile-api')
      } catch {
        if (!cancelled) onTokenRef.current('')
        return
      }
      if (cancelled || !host) return
      const turnstile = (window as unknown as {
        turnstile?: { render: (el: HTMLElement, opts: Record<string, unknown>) => string; remove?: (id: string) => void }
      }).turnstile
      if (!turnstile?.render) {
        onTokenRef.current('')
        return
      }
      onTokenRef.current('')
      const wid = turnstile.render(host, {
        sitekey: siteKey,
        callback: (token: string) => onTokenRef.current(token),
        'expired-callback': () => onTokenRef.current(''),
        'error-callback': () => onTokenRef.current(''),
      })
      widgetIdRef.current = wid
    }

    void run()
    return () => {
      cancelled = true
      const turnstile = (window as unknown as { turnstile?: { remove?: (id: string) => void } }).turnstile
      const h = widgetIdRef.current
      if (h != null && turnstile?.remove) {
        try {
          turnstile.remove(String(h))
        } catch {
          /* ignore */
        }
      }
      widgetIdRef.current = null
      host.innerHTML = ''
    }
  }, [siteKey, refreshSignal, disabled])

  if (!siteKey) {
    return (
      <p className="text-sm text-destructive">
        已开启 Turnstile 校验但未配置站点密钥，请联系管理员设置 turnstile_site_key。
      </p>
    )
  }

  return (
    <div className="space-y-2">
      <Label>{label}</Label>
      <div ref={hostRef} className="min-h-[65px]" />
    </div>
  )
}
