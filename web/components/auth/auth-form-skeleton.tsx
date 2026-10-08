'use client'

import { AuthAnimatedLayout } from '@/components/auth/auth-animated-layout'
import { cn } from '@/lib/utils'
import shell from '@/components/auth/animated-login-shell.module.css'

type Props = {
  title: string
  description?: string
}

/**
 * AuthFormSkeleton - 认证卡片表单区的加载骨架
 *
 * 认证页在拉取安装状态与登录策略期间没有任何可渲染内容，
 * 全屏转圈会让用户面对一片空白；骨架先撑出卡片与字段占位，
 * 数据到达后原地替换为真实表单，视觉尺寸不跳变
 *
 * @param title 卡片标题，加载期间即显示真实文案，避免骨架撤下时文字闪动
 * @param description 卡片副标题
 */
export function AuthFormSkeleton({ title, description }: Props) {
  return (
    <AuthAnimatedLayout
      variant="classic"
      title={title}
      description={description}
      username=""
      password=""
      usernameFocused={false}
      passwordFocused={false}
      showPassword={false}
      errorNonce={0}
    >
      <div
        className={shell.skeletonStack}
        role="status"
        aria-busy="true"
        aria-live="polite"
      >
        <span className="sr-only">正在加载登录表单</span>
        {[0, 1].map((i) => (
          <div key={i} aria-hidden>
            <div className={cn(shell.skeletonLabel, shell.skeletonShimmer)} />
            <div className={cn(shell.skeletonInput, shell.skeletonShimmer)} />
          </div>
        ))}
        <div className={cn(shell.skeletonButton, shell.skeletonShimmer)} aria-hidden />
        <div className={cn(shell.skeletonNav, shell.skeletonShimmer)} aria-hidden />
      </div>
    </AuthAnimatedLayout>
  )
}