'use client'

import { useState, useEffect } from 'react'
import { ProviderConfigField } from '@/lib/api'

interface DynamicFormProps {
  fields: ProviderConfigField[]
  values: Record<string, string>
  onChange: (values: Record<string, string>) => void
  disabled?: boolean
}

export function DynamicForm({ fields, values, onChange, disabled }: DynamicFormProps) {
  const [localValues, setLocalValues] = useState<Record<string, string>>(values)

  useEffect(() => {
    setLocalValues(values)
  }, [values])

  const handleChange = (key: string, value: string) => {
    const newValues = { ...localValues, [key]: value }
    setLocalValues(newValues)
    onChange(newValues)
  }

  const evaluateCondition = (show: string | undefined): boolean => {
    if (!show) return true
    
    try {
      // 支持简单的条件表达式: key==value, key!=value, key==value||key==value2
      const orParts = show.split('||')
      return orParts.some(orPart => {
        const andParts = orPart.split('&&')
        return andParts.every(condition => {
          condition = condition.trim()
          if (condition.includes('!=')) {
            const [key, val] = condition.split('!=').map(s => s.trim().replace(/['"]/g, ''))
            return localValues[key] !== val
          }
          if (condition.includes('==')) {
            const [key, val] = condition.split('==').map(s => s.trim().replace(/['"]/g, ''))
            return localValues[key] === val
          }
          return true
        })
      })
    } catch {
      return true
    }
  }

  const renderField = (field: ProviderConfigField) => {
    if (!evaluateCondition(field.show)) return null

    const value = localValues[field.key] ?? field.value ?? ''

    return (
      <div key={field.key} className="space-y-1">
        <label className="block text-sm font-medium">
          {field.name}
          {field.required && <span className="text-red-500 ml-1">*</span>}
        </label>
        
        {field.type === 'radio' && field.options ? (
          <div className="flex flex-wrap gap-4">
            {field.options.map((opt) => (
              <label key={opt.value} className="flex items-center gap-2 cursor-pointer">
                <input
                  type="radio"
                  name={field.key}
                  value={opt.value}
                  checked={value === opt.value}
                  onChange={(e) => handleChange(field.key, e.target.value)}
                  disabled={disabled}
                  className="w-4 h-4"
                />
                <span className="text-sm">{opt.label}</span>
              </label>
            ))}
          </div>
        ) : field.type === 'select' && field.options ? (
          <select
            value={value}
            onChange={(e) => handleChange(field.key, e.target.value)}
            disabled={disabled}
            className="bs-form-control"
          >
            <option value="">{field.placeholder || `请选择${field.name}`}</option>
            {field.options.map((opt) => (
              <option key={opt.value} value={opt.value}>{opt.label}</option>
            ))}
          </select>
        ) : field.type === 'textarea' ? (
          <textarea
            value={value}
            onChange={(e) => handleChange(field.key, e.target.value)}
            placeholder={field.placeholder}
            disabled={disabled}
            rows={4}
            className="bs-form-control"
          />
        ) : (
          <input
            type={field.type === 'password' ? 'password' : 'text'}
            value={value}
            onChange={(e) => handleChange(field.key, e.target.value)}
            placeholder={field.placeholder}
            disabled={disabled}
            className="bs-form-control"
          />
        )}
        
        {field.note && (
          <p className="text-xs text-green-600 dark:text-green-400 mt-1">{field.note}</p>
        )}
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {fields.map(renderField)}
    </div>
  )
}

export default DynamicForm
