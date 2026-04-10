'use client'

import { useCallback, useEffect, useRef, useState } from 'react'
import { RefreshCw, Loader2, AlertCircle } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { authApi } from '@/lib/api'
import { cn } from '@/lib/utils'

export type AuthCaptchaValue = {
  captchaId: string
  answer: string
}

type Props = {
  enabled: boolean
  captchaType: string
  siteKey?: string
  refreshSignal?: number
  onChange: (v: AuthCaptchaValue) => void
  disabled?: boolean
}

function imageDataUrl(b64s: string) {
  if (!b64s) return ''
  if (b64s.startsWith('data:')) return b64s
  return `data:image/png;base64,${b64s}`
}

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

export function AuthCaptchaFields({
  enabled,
  captchaType,
  siteKey,
  refreshSignal = 0,
  onChange,
  disabled,
}: Props) {
  const type = !enabled ? 'none' : captchaType || 'image'
  const onChangeRef = useRef(onChange)
  onChangeRef.current = onChange

  const notify = useCallback((captchaId: string, answer: string) => {
    onChangeRef.current({ captchaId, answer })
  }, [])

  const [imgId, setImgId] = useState('')
  const [imgB64, setImgB64] = useState('')
  const [imgText, setImgText] = useState('')
  const [imgLoading, setImgLoading] = useState(false)
  const [imgError, setImgError] = useState(false)
  const widgetHostRef = useRef<HTMLDivElement>(null)
  const widgetHandleRef = useRef<string | number | null>(null)

  // 图形验证码
  useEffect(() => {
    if (type !== 'image') return
    let cancelled = false
    setImgLoading(true)
    setImgError(false)
    ;(async () => {
      try {
        const res = await authApi.getCaptcha()
        if (cancelled) return
        if (res.code === 0 && res.data) {
          setImgId(res.data.captcha_id)
          setImgB64(res.data.captcha_image)
          setImgText('')
          notify(res.data.captcha_id, '')
          setImgError(false)
        } else {
          setImgId('')
          setImgB64('')
          setImgError(true)
          notify('', '')
        }
      } catch {
        if (!cancelled) {
          setImgId('')
          setImgB64('')
          setImgError(true)
          notify('', '')
        }
      } finally {
        if (!cancelled) setImgLoading(false)
      }
    })()
    return () => {
      cancelled = true
    }
  }, [type, refreshSignal, notify])

  useEffect(() => {
    if (type === 'image') notify(imgId, imgText)
  }, [type, imgId, imgText, notify])

  // Cloudflare Turnstile
  useEffect(() => {
    if (type !== 'turnstile' || !siteKey) return
    const host = widgetHostRef.current
    if (!host) return
    host.innerHTML = ''
    widgetHandleRef.current = null
    let cancelled = false

    const run = async () => {
      try {
        await loadScriptOnce('https://challenges.cloudflare.com/turnstile/v0/api.js', 'data-cf-turnstile-api')
      } catch {
        if (!cancelled) notify('', '')
        return
      }
      if (cancelled || !host) return
      const turnstile = (window as unknown as { turnstile?: { render: (el: HTMLElement, opts: Record<string, unknown>) => string; remove?: (id: string) => void } }).turnstile
      if (!turnstile?.render) {
        notify('', '')
        return
      }
      notify('', '')
      const wid = turnstile.render(host, {
        sitekey: siteKey,
        callback: (token: string) => notify('', token),
        'expired-callback': () => notify('', ''),
        'error-callback': () => notify('', ''),
      })
      widgetHandleRef.current = wid
    }

    void run()
    return () => {
      cancelled = true
      const turnstile = (window as unknown as { turnstile?: { remove?: (id: string) => void } }).turnstile
      const h = widgetHandleRef.current
      if (h != null && turnstile?.remove) {
        try {
          turnstile.remove(String(h))
        } catch {
          /* ignore */
        }
      }
      widgetHandleRef.current = null
      host.innerHTML = ''
    }
  }, [type, siteKey, refreshSignal, notify])

  // Google reCAPTCHA v2
  useEffect(() => {
    if (type !== 'recaptcha' || !siteKey) return
    const host = widgetHostRef.current
    if (!host) return
    host.innerHTML = ''
    widgetHandleRef.current = null
    let cancelled = false

    const run = async () => {
      try {
        await loadScriptOnce('https://www.google.com/recaptcha/api.js?render=explicit', 'data-recaptcha-api')
      } catch {
        if (!cancelled) notify('', '')
        return
      }
      if (cancelled || !host) return
      const g = (window as unknown as { grecaptcha?: { render: (el: HTMLElement, p: Record<string, unknown>) => number; reset: (id: number) => void } }).grecaptcha
      if (!g?.render) {
        notify('', '')
        return
      }
      notify('', '')
      const wid = g.render(host, {
        sitekey: siteKey,
        callback: (token: string) => notify('', token),
        'expired-callback': () => notify('', ''),
      })
      widgetHandleRef.current = wid
    }

    void run()
    return () => {
      cancelled = true
      host.innerHTML = ''
      widgetHandleRef.current = null
    }
  }, [type, siteKey, refreshSignal, notify])

  // hCaptcha
  useEffect(() => {
    if (type !== 'hcaptcha' || !siteKey) return
    const host = widgetHostRef.current
    if (!host) return
    host.innerHTML = ''
    widgetHandleRef.current = null
    let cancelled = false

    const run = async () => {
      try {
        await loadScriptOnce('https://js.hcaptcha.com/1/api.js', 'data-hcaptcha-api')
      } catch {
        if (!cancelled) notify('', '')
        return
      }
      if (cancelled || !host) return
      const h = (window as unknown as { hcaptcha?: { render: (el: HTMLElement, p: Record<string, unknown>) => string; reset: (id: string) => void } }).hcaptcha
      if (!h?.render) {
        notify('', '')
        return
      }
      notify('', '')
      const wid = h.render(host, {
        sitekey: siteKey,
        callback: (token: string) => notify('', token),
        'error-callback': () => notify('', ''),
        'expired-callback': () => notify('', ''),
      })
      widgetHandleRef.current = wid
    }

    void run()
    return () => {
      cancelled = true
      host.innerHTML = ''
      widgetHandleRef.current = null
    }
  }, [type, siteKey, refreshSignal, notify])

  if (type === 'none') return null

  if (type === 'image') {
    const reloadImage = () => {
      setImgText('')
      setImgLoading(true)
      setImgError(false)
      void (async () => {
        try {
          const res = await authApi.getCaptcha()
          if (res.code === 0 && res.data) {
            setImgId(res.data.captcha_id)
            setImgB64(res.data.captcha_image)
            notify(res.data.captcha_id, '')
            setImgError(false)
          } else {
            setImgId('')
            setImgB64('')
            setImgError(true)
            notify('', '')
          }
        } catch {
          setImgId('')
          setImgB64('')
          setImgError(true)
          notify('', '')
        } finally {
          setImgLoading(false)
        }
      })()
    }

    return (
      <div className="space-y-3 rounded-lg border border-border/80 bg-muted/30 p-3">
        <div className="flex items-center justify-between gap-2">
          <Label className="text-foreground">人机验证</Label>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            className="h-8 px-2 text-xs"
            disabled={disabled || imgLoading}
            onClick={reloadImage}
          >
            <RefreshCw className={cn('h-3.5 w-3.5 mr-1', imgLoading && 'animate-spin')} />
            换一张
          </Button>
        </div>
        {imgLoading ? (
          <div className="h-[52px] rounded-md border bg-background flex items-center justify-center gap-2 text-muted-foreground text-xs">
            <Loader2 className="h-4 w-4 animate-spin shrink-0" />
            正在加载验证码…
          </div>
        ) : imgError ? (
          <button
            type="button"
            onClick={reloadImage}
            disabled={disabled}
            className="w-full h-[52px] rounded-md border border-destructive/35 bg-destructive/5 flex items-center justify-center gap-2 text-destructive text-xs px-2 hover:bg-destructive/10 transition-colors disabled:opacity-50"
          >
            <AlertCircle className="h-4 w-4 shrink-0" />
            加载失败，点击重试
          </button>
        ) : imgB64 ? (
          <>
            {/* eslint-disable-next-line @next/next/no-img-element -- 后端下发的 Base64 验证码图 */}
            <img
              src={imageDataUrl(imgB64)}
              alt="验证码"
              className="rounded-md border bg-background max-h-[52px] w-auto object-contain object-left"
              onError={() => {
                setImgError(true)
                setImgB64('')
                notify('', '')
              }}
            />
          </>
        ) : (
          <div className="h-[52px] rounded-md border bg-muted/50 animate-pulse" />
        )}
        <Input
          placeholder="请输入图中字符（不区分大小写以服务端为准）"
          value={imgText}
          onChange={(e) => setImgText(e.target.value)}
          disabled={disabled || imgLoading || imgError}
          autoComplete="off"
          className="font-mono tracking-wider bg-background"
        />
        <p className="text-[11px] text-muted-foreground leading-snug">
          密码或验证码错误时验证码会自动更换，请重新输入。
        </p>
      </div>
    )
  }

  if (!siteKey) {
    return (
      <p className="text-sm text-destructive">
        管理员已开启验证码但未配置站点密钥，请联系管理员在系统设置中填写 captcha_site_key。
      </p>
    )
  }

  return (
    <div className="space-y-2 rounded-lg border border-border/80 bg-muted/30 p-3">
      <Label className="text-foreground">人机验证</Label>
      <div ref={widgetHostRef} className="min-h-[65px] w-full" />
      <p className="text-[11px] text-muted-foreground leading-snug">第三方验证通过前请勿重复点击登录；加载失败时请刷新页面。</p>
    </div>
  )
}
