'use client'

import { useState, useRef, useCallback, useEffect } from 'react'
import { Dialog, DialogContent, DialogTitle, DialogDescription } from '@/components/ui/dialog'
import { VisuallyHidden } from '@radix-ui/react-visually-hidden'
import { toast } from 'sonner'
import { authApi } from '@/lib/api'
import { Loader2, RefreshCw } from 'lucide-react'
import { Button } from '@/components/ui/button'
import GoCaptcha from 'go-captcha-react'
import type { ClickDot } from 'go-captcha-react/dist/components/Click/meta/data'
import type { ClickRef } from 'go-captcha-react/dist/components/Click/Index'
import type { SlidePoint } from 'go-captcha-react/dist/components/Slide/meta/data'
import type { SlideRef } from 'go-captcha-react/dist/components/Slide/Index'
import type { RotateRef } from 'go-captcha-react/dist/components/Rotate/Index'

/*
 * GoCaptchaDialog 行为验证码弹窗组件
 * 功能：支持点选/滑动/旋转三种验证码，通过弹窗展示，验证通过后返回 verify_token
 */

interface GoCaptchaDialogProps {
  open: boolean
  onClose: () => void
  onSuccess: (verifyToken: string) => void
}

interface CaptchaData {
  captcha_id: string
  captcha_type: string
  master_image: string
  thumb_image: string
  thumb_x?: number
  thumb_y?: number
  thumb_width?: number
  thumb_height?: number
  thumb_size?: number
}

export default function GoCaptchaDialog({ open, onClose, onSuccess }: GoCaptchaDialogProps) {
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState(false)
  const [captchaData, setCaptchaData] = useState<CaptchaData | null>(null)
  const clickRef = useRef<ClickRef>(null)
  const slideRef = useRef<SlideRef>(null)
  const rotateRef = useRef<RotateRef>(null)

  const loadCaptcha = useCallback(async () => {
    setLoading(true)
    setError(false)
    try {
      const res = await authApi.getGoCaptcha()
      const d = res as { code: number; data?: CaptchaData; msg?: string }
      if (d.code === 0 && d.data) {
        setCaptchaData(d.data)
      } else {
        setError(true)
        toast.error(d.msg || '获取验证码失败')
      }
    } catch (e) {
      setError(true)
      console.error('[GoCaptcha] 加载失败:', e)
      toast.error('获取验证码失败')
    } finally {
      setLoading(false)
    }
  }, [])

  /* open prop 变化时加载验证码（onOpenChange 不在程序化设置 open 时触发） */
  useEffect(() => {
    if (open) {
      loadCaptcha()
    } else {
      setCaptchaData(null)
      setError(false)
    }
  }, [open, loadCaptcha])

  const handleOpenChange = (isOpen: boolean) => {
    if (!isOpen) {
      onClose()
    }
  }

  const handleRefresh = () => {
    setCaptchaData(null)
    loadCaptcha()
  }

  /* 提交验证 */
  const submitVerify = async (captchaType: string, answer: unknown) => {
    if (!captchaData) return false
    try {
      const res = await authApi.verifyGoCaptcha({
        captcha_id: captchaData.captcha_id,
        captcha_type: captchaType,
        answer,
      })
      const d = res as { code: number; msg?: string; data?: { verify_token: string } }
      if (d.code === 0 && d.data?.verify_token) {
        toast.success('验证成功')
        onSuccess(d.data.verify_token)
        setCaptchaData(null)
        return true
      } else {
        toast.error(d.msg || '验证失败，请重试')
        loadCaptcha()
        return false
      }
    } catch {
      toast.error('验证失败')
      loadCaptcha()
      return false
    }
  }

  /* 点选确认回调 */
  const handleClickConfirm = (dots: Array<ClickDot>, reset: () => void) => {
    const answer = dots.map(d => ({ x: d.x, y: d.y }))
    submitVerify('click', answer).then(ok => {
      if (!ok) reset()
    })
  }

  /* 滑动确认回调 */
  const handleSlideConfirm = (point: SlidePoint, reset: () => void) => {
    submitVerify('slide', { x: point.x, y: point.y }).then(ok => {
      if (!ok) reset()
    })
  }

  /* 旋转确认回调 */
  const handleRotateConfirm = (angle: number, reset: () => void) => {
    submitVerify('rotate', { angle: Math.round(angle) }).then(ok => {
      if (!ok) reset()
    })
  }

  /* 是否显示加载/错误状态（需要有背景的卡片） */
  const showStatusCard = (loading && !captchaData) || error

  return (
    <Dialog open={open} onOpenChange={handleOpenChange}>
      <DialogContent
        className={
          showStatusCard
            ? 'sm:max-w-sm p-0 overflow-hidden'
            : 'sm:max-w-md p-0 overflow-hidden bg-transparent border-none shadow-none [&>button]:hidden'
        }
      >
        <VisuallyHidden>
          <DialogTitle>验证码</DialogTitle>
          <DialogDescription>请完成安全验证</DialogDescription>
        </VisuallyHidden>

        <div className="flex items-center justify-center min-h-[180px]">
          {/* 加载中 */}
          {loading && !captchaData && (
            <div className="flex flex-col items-center gap-3 p-8">
              <Loader2 className="h-8 w-8 animate-spin text-primary" />
              <p className="text-sm text-muted-foreground">加载验证码...</p>
            </div>
          )}

          {/* 加载失败 */}
          {error && !loading && (
            <div className="flex flex-col items-center gap-3 p-8">
              <p className="text-sm text-destructive">验证码加载失败</p>
              <Button variant="outline" size="sm" onClick={handleRefresh}>
                <RefreshCw className="h-4 w-4 mr-1" />
                重试
              </Button>
            </div>
          )}

          {/* 点选验证码 */}
          {captchaData?.captcha_type === 'click' && (
            <GoCaptcha.Click
              ref={clickRef}
              data={{
                image: captchaData.master_image,
                thumb: captchaData.thumb_image,
              }}
              events={{
                refresh: handleRefresh,
                close: onClose,
                confirm: handleClickConfirm,
              }}
            />
          )}

          {/* 滑动验证码 */}
          {captchaData?.captcha_type === 'slide' && (
            <GoCaptcha.Slide
              ref={slideRef}
              data={{
                image: captchaData.master_image,
                thumb: captchaData.thumb_image,
                thumbX: captchaData.thumb_x || 0,
                thumbY: captchaData.thumb_y || 0,
                thumbWidth: captchaData.thumb_width || 0,
                thumbHeight: captchaData.thumb_height || 0,
              }}
              events={{
                refresh: handleRefresh,
                close: onClose,
                confirm: handleSlideConfirm,
              }}
            />
          )}

          {/* 旋转验证码 */}
          {captchaData?.captcha_type === 'rotate' && (
            <GoCaptcha.Rotate
              ref={rotateRef}
              data={{
                image: captchaData.master_image,
                thumb: captchaData.thumb_image,
                angle: 0,
                thumbSize: captchaData.thumb_size || 0,
              }}
              events={{
                refresh: handleRefresh,
                close: onClose,
                confirm: handleRotateConfirm,
              }}
            />
          )}
        </div>
      </DialogContent>
    </Dialog>
  )
}
