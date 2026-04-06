/**
 * 认证相关页面共用布局：路由切换时轻量入场动画。
 */
export default function AuthGroupLayout({
  children,
}: {
  children: React.ReactNode
}) {
  return <div className="animate-auth-route min-h-screen">{children}</div>
}
