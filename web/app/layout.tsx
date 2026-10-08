import type { Metadata } from "next"
import { Suspense } from "react"
import { Toaster } from "@/components/ui/sonner"
import { ClientErrorBoundary } from "@/components/client-error-boundary"
import { GlobalErrorHandler } from "@/components/global-error-handler"
import { RouteProgress } from "@/components/route-progress"
import { ThemeProvider } from "@/components/theme-provider"
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
    <html lang="zh-CN" suppressHydrationWarning>
      <body className="antialiased">
        <ThemeProvider>
          <Suspense>
            <RouteProgress />
          </Suspense>
          <ClientErrorBoundary>
            {children}
          </ClientErrorBoundary>
          <Toaster position="top-center" richColors />
          <GlobalErrorHandler />
        </ThemeProvider>
      </body>
    </html>
  )
}
