"use client"

import { ThemeProvider as NextThemesProvider } from "next-themes"

/**
 * 主题提供者
 *
 * 项目在 globals.css 里定义了 `.dark` 变量块与 370 余处 `dark:` 变体，
 * 但此前没有任何代码给根节点挂 `.dark`，这些样式在运行时全部失效。
 * 这里把 next-themes 接到根布局上；`attribute="class"` 正是写入 html 的 class，
 * 与 `@custom-variant dark (&:is(.dark *))` 的选择器约定一致。
 *
 * 默认跟随系统而非固定浅色：暗色样式早已写好，跟随系统能让它在用户的
 * 深色偏好下直接生效，不需要用户先找到切换入口
 */
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  return (
    <NextThemesProvider
      attribute="class"
      defaultTheme="system"
      enableSystem
      disableTransitionOnChange
    >
      {children}
    </NextThemesProvider>
  )
}