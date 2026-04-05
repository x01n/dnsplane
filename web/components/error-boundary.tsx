'use client'

import React from 'react'
import { AlertTriangle, RefreshCw, Home, Copy, ChevronDown, ChevronUp } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter, CardHeader, CardTitle } from '@/components/ui/card'

interface ErrorBoundaryState {
  hasError: boolean
  error: Error | null
  errorInfo: React.ErrorInfo | null
  showDetails: boolean
}

interface ErrorBoundaryProps {
  children: React.ReactNode
  fallback?: React.ReactNode
}

export class ErrorBoundary extends React.Component<ErrorBoundaryProps, ErrorBoundaryState> {
  constructor(props: ErrorBoundaryProps) {
    super(props)
    this.state = { hasError: false, error: null, errorInfo: null, showDetails: false }
  }

  static getDerivedStateFromError(error: Error): Partial<ErrorBoundaryState> {
    return { hasError: true, error }
  }

  componentDidCatch(error: Error, errorInfo: React.ErrorInfo) {
    this.setState({ error, errorInfo })
    // 可以在这里添加错误日志上报
    console.error('ErrorBoundary caught an error:', error, errorInfo)
  }

  handleReload = () => {
    window.location.reload()
  }

  handleGoHome = () => {
    window.location.href = '/dashboard/'
  }

  handleCopyError = () => {
    const { error, errorInfo } = this.state
    const errorText = `Error: ${error?.message}

Stack: ${error?.stack}

Component Stack: ${errorInfo?.componentStack}`
    navigator.clipboard.writeText(errorText)
  }

  toggleDetails = () => {
    this.setState(prev => ({ showDetails: !prev.showDetails }))
  }

  render() {
    if (this.state.hasError) {
      if (this.props.fallback) {
        return this.props.fallback
      }

      const { error, showDetails, errorInfo } = this.state

      return (
        <div className="min-h-screen bg-gradient-to-br from-gray-50 to-gray-100 dark:from-gray-900 dark:to-gray-800 flex items-center justify-center p-4">
          <Card className="w-full max-w-lg shadow-xl border-0">
            <CardHeader className="text-center pb-2">
              <div className="mx-auto mb-4 h-16 w-16 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
                <AlertTriangle className="h-8 w-8 text-red-600 dark:text-red-400" />
              </div>
              <CardTitle className="text-xl font-semibold text-gray-900 dark:text-gray-100">
                页面出现错误
              </CardTitle>
              <p className="text-sm text-muted-foreground mt-2">
                抱歉，页面加载时遇到了问题。您可以尝试刷新页面或返回首页。
              </p>
            </CardHeader>
            
            <CardContent className="space-y-4">
              <div className="rounded-lg bg-red-50 dark:bg-red-900/20 p-4 border border-red-200 dark:border-red-800">
                <p className="text-sm font-medium text-red-800 dark:text-red-300">
                  {error?.message || '发生未知错误'}
                </p>
              </div>

              <button
                onClick={this.toggleDetails}
                className="flex items-center justify-between w-full text-sm text-muted-foreground hover:text-foreground transition-colors"
              >
                <span>查看详细信息</span>
                {showDetails ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
              </button>

              {showDetails && (
                <div className="space-y-3">
                  <div className="rounded-lg bg-gray-50 dark:bg-gray-800 p-3 max-h-48 overflow-auto">
                    <p className="text-xs font-mono text-gray-600 dark:text-gray-400 whitespace-pre-wrap break-all">
                      {error?.stack || '无堆栈信息'}
                    </p>
                  </div>
                  {errorInfo?.componentStack && (
                    <div className="rounded-lg bg-gray-50 dark:bg-gray-800 p-3 max-h-32 overflow-auto">
                      <p className="text-xs font-mono text-gray-600 dark:text-gray-400 whitespace-pre-wrap">
                        {errorInfo.componentStack}
                      </p>
                    </div>
                  )}
                </div>
              )}
            </CardContent>

            <CardFooter className="flex flex-col sm:flex-row gap-3">
              <Button onClick={this.handleReload} className="w-full sm:w-auto">
                <RefreshCw className="h-4 w-4 mr-2" />
                刷新页面
              </Button>
              <Button variant="outline" onClick={this.handleGoHome} className="w-full sm:w-auto">
                <Home className="h-4 w-4 mr-2" />
                返回首页
              </Button>
              <Button variant="ghost" onClick={this.handleCopyError} className="w-full sm:w-auto">
                <Copy className="h-4 w-4 mr-2" />
                复制错误
              </Button>
            </CardFooter>
          </Card>
        </div>
      )
    }

    return this.props.children
  }
}

// 用于函数组件的错误提示组件
export function ErrorFallback({ 
  error, 
  resetError 
}: { 
  error: Error
  resetError?: () => void 
}) {
  return (
    <div className="min-h-[400px] flex items-center justify-center p-4">
      <Card className="w-full max-w-md">
        <CardHeader className="text-center pb-2">
          <div className="mx-auto mb-3 h-12 w-12 rounded-full bg-red-100 dark:bg-red-900/30 flex items-center justify-center">
            <AlertTriangle className="h-6 w-6 text-red-600 dark:text-red-400" />
          </div>
          <CardTitle className="text-lg">加载出错</CardTitle>
        </CardHeader>
        <CardContent>
          <p className="text-sm text-muted-foreground text-center mb-4">
            {error.message || '发生未知错误'}
          </p>
        </CardContent>
        <CardFooter className="justify-center gap-2">
          {resetError && (
            <Button onClick={resetError} size="sm">
              <RefreshCw className="h-4 w-4 mr-2" />
              重试
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
            刷新页面
          </Button>
        </CardFooter>
      </Card>
    </div>
  )
}
