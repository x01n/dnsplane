'use client'

import { Globe, Loader2 } from 'lucide-react'
import type { ReactNode } from 'react'
import {
  AnimatedLoginCharacters,
  type AnimatedLoginCharactersProps,
} from './animated-login-characters'
import shell from './animated-login-shell.module.css'

export type AuthAnimatedLayoutProps = {
  title: string
  description?: string
  /** 默认：深色底 + 下划线输入 + 星形图标；classic：浅色渐变 + 白卡片 + Globe（与旧版登录一致） */
  variant?: 'default' | 'classic'
  leftFooter?: ReactNode
  children: ReactNode
} & AnimatedLoginCharactersProps

/**
 * 认证页通用布局：左侧渐变 + 互动角色，右侧表单区。
 */
export function AuthAnimatedLayout({
  title,
  description,
  variant = 'default',
  leftFooter,
  children,
  ...characterProps
}: AuthAnimatedLayoutProps) {
  const isClassic = variant === 'classic'

  const pageClass = isClassic ? shell.loginPageClassic : shell.loginPage
  const rightClass = isClassic ? shell.rightPanelClassic : shell.rightPanel

  const headerDefault = (
    <>
      <div className={shell.sparkleIcon}>
        <svg
          viewBox="0 0 24 24"
          fill="none"
          xmlns="http://www.w3.org/2000/svg"
          aria-hidden
        >
          <path d="M12 2L13.5 9H10.5L12 2Z" fill="#1a1a2e" />
          <path d="M12 22L10.5 15H13.5L12 22Z" fill="#1a1a2e" />
          <path d="M2 12L9 10.5V13.5L2 12Z" fill="#1a1a2e" />
          <path d="M22 12L15 13.5V10.5L22 12Z" fill="#1a1a2e" />
        </svg>
      </div>
      <div className={shell.formHeader}>
        <h1>{title}</h1>
        {description ? <p>{description}</p> : null}
      </div>
    </>
  )

  const headerClassic = (
    <>
      <div className={shell.brandMark}>
        <div className={shell.brandIcon} aria-hidden>
          <Globe className="h-6 w-6" strokeWidth={2} />
        </div>
      </div>
      <div className={shell.formHeaderClassic}>
        <h1>{title}</h1>
        {description ? <p>{description}</p> : null}
      </div>
    </>
  )

  return (
    <div className={pageClass}>
      <div className={shell.leftPanel}>
        <div className={shell.logo}>
          <svg
            className={shell.logoSvg}
            viewBox="0 0 24 24"
            fill="none"
            stroke="white"
            strokeWidth="2"
            aria-hidden
          >
            <path d="M12 2L15 9H9L12 2Z" />
            <path d="M12 22L9 15H15L12 22Z" />
            <path d="M2 12L9 9V15L2 12Z" />
            <path d="M22 12L15 15V9L22 12Z" />
          </svg>
          <span>DNSPlane</span>
        </div>
        <AnimatedLoginCharacters {...characterProps} />
        <div className={shell.footerLinks}>
          {leftFooter ?? <span>安全 DNS 管理</span>}
        </div>
      </div>

      <div className={rightClass}>
        {isClassic ? (
          <div className={shell.formCard}>
            {headerClassic}
            {children}
          </div>
        ) : (
          <div className={shell.formContainer}>
            {headerDefault}
            {children}
          </div>
        )}
      </div>
    </div>
  )
}

export function AuthAnimatedLoading({
  label,
  variant = 'default',
}: {
  label?: string
  variant?: 'default' | 'classic'
}) {
  const root =
    variant === 'classic' ? shell.loadingRootClassic : shell.loadingRoot
  const textClass =
    variant === 'classic'
      ? 'text-neutral-500'
      : 'text-white/80'

  return (
    <div className={root}>
      <div className={`flex flex-col items-center gap-3 ${textClass}`}>
        <Loader2 className="h-8 w-8 animate-spin" aria-hidden />
        {label ? <span className="text-sm">{label}</span> : null}
      </div>
    </div>
  )
}
