'use client'

import { useState } from 'react'

/**
 * usePageSize - 分页大小持久化 hook
 * @param key 存储键名标识
 * @param defaultSize 默认分页大小
 */
export function usePageSize(key: string, defaultSize = 20): [number, (size: number) => void] {
  const storageKey = `page_size_${key}`
  const [pageSize, setPageSize] = useState(() => {
    if (typeof window === 'undefined') return defaultSize
    const saved = localStorage.getItem(storageKey)
    return saved ? parseInt(saved, 10) : defaultSize
  })

  const updatePageSize = (size: number) => {
    setPageSize(size)
    localStorage.setItem(storageKey, String(size))
  }

  return [pageSize, updatePageSize]
}
