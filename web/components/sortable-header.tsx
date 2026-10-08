'use client'

import { TableHead } from '@/components/ui/table'
import { ArrowUp, ArrowDown, ArrowUpDown } from 'lucide-react'

export type SortState = { field: string; order: 'asc' | 'desc' } | null

interface SortableHeaderProps {
  field: string
  label: string
  currentSort: SortState
  onSort: (field: string) => void
  className?: string
}

export function SortableHeader({ field, label, currentSort, onSort, className }: SortableHeaderProps) {
  const isActive = currentSort?.field === field
  const ariaSort: 'none' | 'ascending' | 'descending' = isActive
    ? (currentSort?.order === 'asc' ? 'ascending' : 'descending')
    : 'none'
  return (
    <TableHead aria-sort={ariaSort} className={className}>
      <button
        type="button"
        onClick={() => onSort(field)}
        className="flex cursor-pointer select-none items-center gap-1 rounded-sm outline-none transition-colors hover:text-foreground focus-visible:ring-[3px] focus-visible:ring-ring/50"
      >
        {label}
        {isActive ? (
          currentSort.order === 'asc' ? <ArrowUp className="h-3 w-3" /> : <ArrowDown className="h-3 w-3" />
        ) : (
          <ArrowUpDown className="h-3 w-3 text-muted-foreground" />
        )}
      </button>
    </TableHead>
  )
}

export function handleSortToggle(field: string, currentSort: SortState): SortState {
  if (currentSort?.field === field) {
    if (currentSort.order === 'asc') return { field, order: 'desc' }
    return null
  }
  return { field, order: 'asc' }
}
