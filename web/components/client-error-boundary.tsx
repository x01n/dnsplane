'use client'

import { ErrorBoundary } from '@/components/error-boundary'

export function ClientErrorBoundary({ children }: { children: React.ReactNode }) {
  return <ErrorBoundary>{children}</ErrorBoundary>
}
