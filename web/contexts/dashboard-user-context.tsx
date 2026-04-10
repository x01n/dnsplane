'use client'

import { createContext, useContext, type ReactNode } from 'react'
import type { User } from '@/lib/api'

const DashboardUserContext = createContext<User | null>(null)

export function DashboardUserProvider({
  user,
  children,
}: {
  user: User
  children: ReactNode
}) {
  return (
    <DashboardUserContext.Provider value={user}>
      {children}
    </DashboardUserContext.Provider>
  )
}

export function useDashboardUser() {
  return useContext(DashboardUserContext)
}
