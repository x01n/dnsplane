import type { ProviderConfigField } from '@/lib/api'

/**
 * 解析后端 deploy/config 中的 show 表达式（与 dnsmgr、DynamicForm 一致）
 * 支持：key==value、key!=value，用 &&、|| 组合；比较值为单引号或双引号包裹或无引号
 */
export function evaluateDeployFieldShow(
  show: string | undefined,
  values: Record<string, string>
): boolean {
  if (!show || !show.trim()) return true

  const stripQuotes = (s: string) => s.trim().replace(/^['"]|['"]$/g, '')

  const evalOne = (condition: string): boolean => {
    const c = condition.trim()
    if (!c) return true
    if (c.includes('!=')) {
      const [key, val] = c.split('!=').map(stripQuotes)
      return (values[key] ?? '') !== val
    }
    if (c.includes('==')) {
      const [key, val] = c.split('==').map(stripQuotes)
      return (values[key] ?? '') === val
    }
    return true
  }

  try {
    const orParts = show.split('||').map((s) => s.trim())
    return orParts.some((orPart) => {
      const andParts = orPart.split('&&').map((s) => s.trim())
      return andParts.every((part) => evalOne(part))
    })
  } catch {
    return true
  }
}

/** 用字段上的 value 补全空缺的表单值（保证 product 等默认值参与 show 条件） */
export function mergeProviderFieldDefaults(
  fields: ProviderConfigField[] | undefined,
  existing: Record<string, string>
): Record<string, string> {
  const out = { ...existing }
  if (!fields) return out
  for (const f of fields) {
    const cur = out[f.key]
    if ((cur === undefined || cur === '') && f.value != null && String(f.value) !== '') {
      out[f.key] = String(f.value)
    }
  }
  return out
}

export function isDeployFieldVisible(
  field: ProviderConfigField,
  values: Record<string, string>
): boolean {
  return evaluateDeployFieldShow(field.show, values)
}

/**
 * Radix Select 要求 value 必须是某一选项；将无效空值回落到字段默认值或首项
 */
export function resolveSelectFieldValue(
  field: ProviderConfigField,
  raw: string | undefined
): string {
  const opts = field.options?.map((o) => o.value) ?? []
  if (opts.length === 0) return raw ?? ''
  const v = raw ?? ''
  if (v !== '' && opts.includes(v)) return v
  if (field.value && opts.includes(field.value)) return field.value
  return opts[0] ?? ''
}
