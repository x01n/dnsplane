'use client'

import Link from 'next/link'
import { FileQuestion, LayoutDashboard, ArrowLeft } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent } from '@/components/ui/card'

export default function DashboardNotFound() {
  return (
    <div className="flex items-center justify-center min-h-[60vh]">
      <Card className="w-full max-w-md shadow-lg border">
        <CardContent className="pt-10 pb-10 px-8 text-center space-y-6">
          {/* 图标 */}
          <div className="mx-auto flex h-16 w-16 items-center justify-center rounded-full bg-muted">
            <FileQuestion className="h-8 w-8 text-muted-foreground" />
          </div>

          {/* 404 数字 */}
          <div className="space-y-2">
            <h1 className="text-5xl font-extrabold text-gradient">404</h1>
            <h2 className="text-xl font-semibold text-foreground">
              页面不存在
            </h2>
            <p className="text-sm text-muted-foreground max-w-sm mx-auto">
              您访问的页面不存在或已被移除，请返回仪表盘继续操作。
            </p>
          </div>

          {/* 操作按钮 */}
          <div className="flex items-center justify-center gap-3 pt-2">
            <Button variant="outline" asChild>
              <Link href="/dashboard">
                <ArrowLeft className="mr-2 h-4 w-4" />
                返回上一页
              </Link>
            </Button>
            <Button asChild>
              <Link href="/dashboard">
                <LayoutDashboard className="mr-2 h-4 w-4" />
                返回仪表盘
              </Link>
            </Button>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
