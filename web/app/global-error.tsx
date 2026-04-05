'use client'

/*
 * GlobalError 根级别最终兜底错误页面
 * 功能：当 root layout 自身崩溃时，Next.js 会渲染此组件
 *       注意：此组件必须自带 <html> 和 <body> 标签，因为 root layout 已失效
 *       这是整个应用的最后一道错误防线
 */
export default function GlobalError({
  error,
  reset,
}: {
  error: Error & { digest?: string }
  reset: () => void
}) {
  return (
    <html lang="zh-CN">
      <body className="antialiased">
        <div style={{
          minHeight: '100vh',
          display: 'flex',
          alignItems: 'center',
          justifyContent: 'center',
          background: 'linear-gradient(135deg, #1e1b4b 0%, #312e81 50%, #1e1b4b 100%)',
          padding: '1rem',
          fontFamily: '-apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif',
        }}>
          <div style={{
            maxWidth: '28rem',
            width: '100%',
            background: 'rgba(255,255,255,0.95)',
            borderRadius: '1rem',
            padding: '2.5rem 2rem',
            textAlign: 'center',
            boxShadow: '0 25px 50px -12px rgba(0,0,0,0.25)',
          }}>
            {/* 错误图标 */}
            <div style={{
              width: '4rem', height: '4rem', margin: '0 auto 1.5rem',
              borderRadius: '50%', background: '#fef2f2',
              display: 'flex', alignItems: 'center', justifyContent: 'center',
            }}>
              <svg width="28" height="28" viewBox="0 0 24 24" fill="none" stroke="#dc2626" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round">
                <path d="m21.73 18-8-14a2 2 0 0 0-3.48 0l-8 14A2 2 0 0 0 4 21h16a2 2 0 0 0 1.73-3Z" />
                <path d="M12 9v4" />
                <path d="M12 17h.01" />
              </svg>
            </div>

            <h1 style={{ fontSize: '1.5rem', fontWeight: 700, color: '#111', margin: '0 0 0.5rem' }}>
              应用发生严重错误
            </h1>
            <p style={{ fontSize: '0.875rem', color: '#6b7280', margin: '0 0 1rem' }}>
              应用程序遇到了无法恢复的错误，请尝试刷新页面。
            </p>

            {error?.message && (
              <div style={{
                background: '#f9fafb', borderRadius: '0.5rem', padding: '0.75rem',
                margin: '0 0 1.5rem', textAlign: 'left',
              }}>
                <p style={{ fontSize: '0.75rem', color: '#9ca3af', margin: '0 0 0.25rem' }}>错误信息</p>
                <p style={{ fontSize: '0.8125rem', color: '#374151', fontFamily: 'monospace', wordBreak: 'break-all', margin: 0 }}>
                  {error.message}
                </p>
              </div>
            )}

            <div style={{ display: 'flex', gap: '0.75rem', justifyContent: 'center' }}>
              <button
                onClick={() => window.location.href = '/login'}
                style={{
                  padding: '0.5rem 1.25rem', borderRadius: '0.5rem',
                  border: '1px solid #d1d5db', background: 'white', color: '#374151',
                  fontSize: '0.875rem', cursor: 'pointer', fontWeight: 500,
                }}
              >
                返回首页
              </button>
              <button
                onClick={reset}
                style={{
                  padding: '0.5rem 1.25rem', borderRadius: '0.5rem',
                  border: 'none', background: 'linear-gradient(135deg, #7c3aed, #6d28d9)',
                  color: 'white', fontSize: '0.875rem', cursor: 'pointer', fontWeight: 500,
                  boxShadow: '0 4px 6px -1px rgba(124,58,237,0.3)',
                }}
              >
                重试
              </button>
            </div>

            <p style={{ fontSize: '0.75rem', color: 'rgba(107,114,128,0.5)', marginTop: '2rem' }}>
              DNSPlane — DNS管理系统
            </p>
          </div>
        </div>
      </body>
    </html>
  )
}
