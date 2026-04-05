import type { Metadata } from "next"
import { Suspense } from "react"
import { Toaster } from "@/components/ui/sonner"
import { ClientErrorBoundary } from "@/components/client-error-boundary"
import { GlobalErrorHandler } from "@/components/global-error-handler"
import { RouteProgress } from "@/components/route-progress"
import "./globals.css"

export const metadata: Metadata = {
  title: "DNSPlane - DNS管理系统",
  description: "现代化DNS管理系统，支持多平台DNS管理、SSL证书申请、容灾切换",
}

export default function RootLayout({
  children,
}: Readonly<{
  children: React.ReactNode
}>) {
  return (
    <html lang="zh-CN">
      <body className="antialiased">
        <Suspense>
          <RouteProgress />
        </Suspense>
        <ClientErrorBoundary>
          {children}
        </ClientErrorBoundary>
        <Toaster position="top-center" richColors />
        <GlobalErrorHandler />
      </body>
    </html>
  )
}
