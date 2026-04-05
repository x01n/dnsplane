'use client'

import { useRouter } from 'next/navigation'
import { Globe, ArrowLeft, Home } from 'lucide-react'
import { Button } from '@/components/ui/button'

export default function NotFound() {
  const router = useRouter()

  return (
    <div className="min-h-screen flex items-center justify-center bg-gradient-to-br from-slate-900 via-purple-900 to-slate-900 p-4 relative overflow-hidden">
      {/* 背景装饰 */}
      <div className="absolute inset-0 overflow-hidden">
        <div className="absolute -top-40 -right-40 w-80 h-80 bg-purple-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob"></div>
        <div className="absolute -bottom-40 -left-40 w-80 h-80 bg-cyan-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob animation-delay-2000"></div>
        <div className="absolute top-1/2 left-1/2 -translate-x-1/2 -translate-y-1/2 w-80 h-80 bg-pink-500 rounded-full mix-blend-multiply filter blur-3xl opacity-20 animate-blob animation-delay-4000"></div>
      </div>

      <div className="relative text-center space-y-8">
        {/* Logo */}
        <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-2xl bg-gradient-to-br from-violet-500 to-purple-600 shadow-lg shadow-purple-500/30">
          <Globe className="h-8 w-8 text-white" />
        </div>

        {/* 404 数字 */}
        <div className="space-y-3">
          <h1 className="text-8xl font-extrabold bg-gradient-to-r from-violet-400 via-purple-400 to-pink-400 bg-clip-text text-transparent drop-shadow-sm">
            404
          </h1>
          <h2 className="text-2xl font-bold text-white/90">
            页面未找到
          </h2>
          <p className="text-base text-white/60 max-w-md mx-auto">
            您访问的页面不存在或已被移除，请检查链接是否正确。
          </p>
        </div>

        {/* 操作按钮 */}
        <div className="flex items-center justify-center gap-4">
          <Button
            variant="outline"
            className="bg-white/10 border-white/20 text-white hover:bg-white/20 hover:text-white"
            onClick={() => router.back()}
          >
            <ArrowLeft className="mr-2 h-4 w-4" />
            返回上一页
          </Button>
          <Button
            className="bg-gradient-to-r from-violet-600 to-purple-600 hover:from-violet-500 hover:to-purple-500 shadow-lg shadow-purple-500/25"
            onClick={() => router.push('/login')}
          >
            <Home className="mr-2 h-4 w-4" />
            返回首页
          </Button>
        </div>

        {/* 品牌 */}
        <p className="text-sm text-white/30 font-medium">
          DNSPlane — DNS管理系统
        </p>
      </div>
    </div>
  )
}
