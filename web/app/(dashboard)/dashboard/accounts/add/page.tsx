'use client'

import { useState, useEffect } from 'react'
import { AccountForm, Provider } from '@/components/account-form'
import { systemApi, api, DNSProvider } from '@/lib/api'
import { toast } from 'sonner'
import { Loader2 } from 'lucide-react'

export default function AddDNSAccountPage() {
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchProviders()
  }, [])

  const fetchProviders = async () => {
    try {
      const res = await systemApi.getDNSProviders()
      if (res.code === 0 && res.data) {
        // 转换为 Provider 格式
        const providerList: Provider[] = (res.data as DNSProvider[]).map((p) => ({
          type: p.type,
          name: p.name,
          icon: p.icon,
          config: p.config || [],
        }))
        setProviders(providerList)
      }
    } catch {
      toast.error('获取DNS服务商列表失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSubmit = async (data: { type: string; name: string; config: Record<string, string>; remark: string }) => {
    // 后端期望 config 是对象，不是字符串
    return api.post('/accounts', {
      type: data.type,
      name: data.name,
      config: data.config,
      remark: data.remark,
    })
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center py-12">
        <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <AccountForm
      type="dns"
      providers={providers}
      onSubmit={handleSubmit}
      backUrl="/dashboard/accounts"
      title="添加DNS账户"
      description="选择DNS服务商并配置账户信息"
    />
  )
}
