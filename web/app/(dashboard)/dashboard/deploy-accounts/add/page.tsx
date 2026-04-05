'use client'

import { useState, useEffect } from 'react'
import { AccountForm, Provider } from '@/components/account-form'
import { certApi, api, CertProviderConfig } from '@/lib/api'
import { toast } from 'sonner'
import { Loader2 } from 'lucide-react'

export default function AddDeployAccountPage() {
  const [providers, setProviders] = useState<Provider[]>([])
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    fetchProviders()
  }, [])

  const fetchProviders = async () => {
    try {
      const res = await certApi.getProviders()
      if (res.code === 0 && res.data) {
        // 后端返回 {cert: {...}, deploy: {...}} 格式
        const deployProviders = res.data.deploy || {}
        // 转换为 Provider 格式
        const providerList: Provider[] = Object.entries(deployProviders).map(
          ([type, cfg]: [string, CertProviderConfig]) => ({
            type,
            name: cfg.name,
            icon: cfg.icon,
            note: cfg.note,
            config: cfg.config || [],
          })
        )
        setProviders(providerList)
      }
    } catch {
      toast.error('获取部署方式列表失败')
    } finally {
      setLoading(false)
    }
  }

  const handleSubmit = async (data: { type: string; name: string; config: Record<string, string>; remark: string }) => {
    // 后端期望 config 是对象，不是字符串
    return api.post('/cert/accounts', {
      type: data.type,
      name: data.name,
      config: data.config,
      remark: data.remark,
      is_deploy: true,
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
      type="deploy"
      providers={providers}
      onSubmit={handleSubmit}
      backUrl="/dashboard/deploy-accounts"
      title="添加部署账户"
      description="选择部署方式并配置账户信息"
    />
  )
}
