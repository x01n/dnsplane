"use client"

import { useSyncExternalStore } from "react"
import { useTheme } from "next-themes"
import { Monitor, Moon, Sun } from "lucide-react"
import { cn } from "@/lib/utils"

/**
 * 挂载判据：服务端返回 false，客户端返回 true
 *
 * 用它替代 useState + useEffect 置位：后者会在 effect 里同步 setState，
 * 触发一次多余的级联渲染，且被 react-hooks/set-state-in-effect 规则拦下。
 * useSyncExternalStore 的语义正是「读取一个外部快照」，首帧即给出确定值
 */
const emptySubscribe = () => () => {}

/**
 * 主题切换三态按钮：浅色 / 深色 / 跟随系统
 *
 * 挂载前按浅色高亮：服务端预渲染时读不到用户偏好，next-themes 的 theme 是 undefined，
 * 直接采信会让首帧闪一下错误的选中态
 *
 * @param className 追加到按钮组外层的类名，供不同容器微调间距
 */
export function ThemeToggle({ className }: { className?: string }) {
  const { theme, setTheme } = useTheme()
  const mounted = useSyncExternalStore(emptySubscribe, () => true, () => false)

  const options = [
    { value: "light", label: "浅色", Icon: Sun },
    { value: "dark", label: "深色", Icon: Moon },
    { value: "system", label: "跟随系统", Icon: Monitor },
  ] as const

  const active = mounted ? theme : "light"

  return (
    <div
      className={cn(
        "inline-flex items-center gap-0.5 rounded-lg border border-border bg-muted/40 p-0.5",
        className
      )}
      role="radiogroup"
      aria-label="界面主题"
    >
      {options.map(({ value, label, Icon }) => {
        const selected = active === value
        return (
          <button
            key={value}
            type="button"
            role="radio"
            aria-checked={selected}
            aria-label={label}
            title={label}
            onClick={() => setTheme(value)}
            className={cn(
              "rounded-md p-1.5 transition-colors",
              selected
                ? "bg-background text-foreground shadow-sm"
                : "text-muted-foreground hover:text-foreground"
            )}
          >
            <Icon className="h-4 w-4" aria-hidden />
          </button>
        )
      })}
    </div>
  )
}