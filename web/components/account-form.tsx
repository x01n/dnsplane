'use client'

import { useState, useEffect } from 'react'
import { useRouter } from 'next/navigation'
import { ArrowLeft, Loader2, ChevronRight } from 'lucide-react'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { toast } from 'sonner'
import { ProviderConfigField } from '@/lib/api'
import { cn } from '@/lib/utils'
import {
  evaluateDeployFieldShow,
  isDeployFieldVisible,
  resolveSelectFieldValue,
} from '@/lib/deploy-config-form'
import { RadioGroup, RadioGroupItem } from '@/components/ui/radio-group'

// 提供商分类配置
const DNS_CATEGORIES: Record<string, string[]> = {
  '国内DNS服务商': ['aliyun', 'dnspod', 'huawei', 'baidu', 'huoshan', 'jdcloud', 'west', 'dnsla', 'bt'],
  '国际DNS服务商': ['cloudflare', 'namesilo', 'spaceship'],
  '专业DNS解决方案': ['powerdns', 'aliyunesa', 'tencenteo'],
}

const CERT_CATEGORIES: Record<string, string[]> = {
  '免费证书': ['letsencrypt', 'zerossl', 'google', 'litessl'],
  '云厂商证书': ['tencent', 'aliyun_cert', 'huoshan', 'ucloud'],
  '自定义': ['customacme'],
}

const DEPLOY_CATEGORIES: Record<string, string[]> = {
  '远程与本地': ['ssh', 'ftp', 'local'],
  '面板 / 编排': [
    'btpanel',
    '1panel',
    'synology',
    'mwpanel',
    'ratpanel',
    'chopsticks',
    'xpanel',
    'kangle',
    'kangle_admin',
    'goedge',
    'cdnfly',
    'lecdn',
    'lucky',
    'fnos',
    'proxmox',
    'k8s',
    'btwaf',
    'baolei',
    'opanel',
  ],
  'CDN / 云': [
    'aliyun_cdn',
    'tencent_cdn',
    'huawei_cdn',
    'qiniu',
    'upyun',
    'aws_cloudfront',
    'ucloud',
    'ucloud_cdn',
    'safeline',
    'aws',
  ],
}

// 提供商图标和颜色配置
const providerStyles: Record<string, { color: string; bgColor: string; icon: string; image?: string }> = {
  cloudflare: { color: '#F38020', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'CF', image: '/icons/cloudflare.ico' },
  aliyun: { color: '#FF6A00', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: '阿', image: '/icons/aliyun.png' },
  dnspod: { color: '#00A4FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '腾', image: '/icons/dnspod.ico' },
  huawei: { color: '#C7000B', bgColor: 'bg-red-50 dark:bg-red-950', icon: '华', image: '/icons/huawei.ico' },
  baidu: { color: '#2932E1', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '百', image: '/icons/baidu.ico' },
  huoshan: { color: '#3370FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '火', image: '/icons/huoshan.ico' },
  jdcloud: { color: '#C91623', bgColor: 'bg-red-50 dark:bg-red-950', icon: '京', image: '/icons/jdcloud.ico' },
  west: { color: '#2E7D32', bgColor: 'bg-green-50 dark:bg-green-950', icon: '西', image: '/icons/west.ico' },
  dnsla: { color: '#1565C0', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'LA', image: '/icons/dnsla.ico' },
  namesilo: { color: '#4CAF50', bgColor: 'bg-green-50 dark:bg-green-950', icon: 'NS', image: '/icons/namesilo.ico' },
  powerdns: { color: '#263238', bgColor: 'bg-gray-50 dark:bg-gray-900', icon: 'PD', image: '/icons/powerdns.ico' },
  bt: { color: '#20A53A', bgColor: 'bg-green-50 dark:bg-green-950', icon: '宝', image: '/icons/bt.png' },
  aliyunesa: { color: '#FF6A00', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'EA', image: '/icons/aliyun.png' },
  tencenteo: { color: '#00A4FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'EO', image: '/icons/tencent.png' },
  spaceship: { color: '#6366F1', bgColor: 'bg-indigo-50 dark:bg-indigo-950', icon: 'SP', image: '/icons/spaceship.ico' },
  letsencrypt: { color: '#003A70', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'LE', image: '/icons/letsencrypt.ico' },
  zerossl: { color: '#E91E63', bgColor: 'bg-pink-50 dark:bg-pink-950', icon: 'ZS', image: '/icons/zerossl.ico' },
  google: { color: '#4285F4', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'G', image: '/icons/google.ico' },
  litessl: { color: '#00BCD4', bgColor: 'bg-cyan-50 dark:bg-cyan-950', icon: 'LS', image: '/icons/litessl.ico' },
  tencent: { color: '#00A4FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '腾', image: '/icons/tencent.png' },
  aliyun_cert: { color: '#FF6A00', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: '阿', image: '/icons/aliyun.png' },
  customacme: { color: '#9E9E9E', bgColor: 'bg-gray-50 dark:bg-gray-900', icon: 'AC', image: '/icons/ssl.ico' },
  ssh: { color: '#424242', bgColor: 'bg-gray-50 dark:bg-gray-900', icon: 'SS', image: '/icons/server.png' },
  local: { color: '#607D8B', bgColor: 'bg-gray-50 dark:bg-gray-900', icon: 'LC', image: '/icons/host.png' },
  aliyun_cdn: { color: '#FF6A00', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'AC', image: '/icons/aliyun.png' },
  tencent_cdn: { color: '#00A4FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'TC', image: '/icons/tencent.png' },
  huawei_cdn: { color: '#C7000B', bgColor: 'bg-red-50 dark:bg-red-950', icon: 'HC', image: '/icons/huawei.ico' },
  qiniu: { color: '#07BEFF', bgColor: 'bg-cyan-50 dark:bg-cyan-950', icon: '七', image: '/icons/qiniu.ico' },
  upyun: { color: '#2196F3', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '又', image: '/icons/upyun.ico' },
  btpanel: { color: '#20A53A', bgColor: 'bg-green-50 dark:bg-green-950', icon: '宝', image: '/icons/bt.png' },
  '1panel': { color: '#1976D2', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '1P', image: '/icons/opanel.png' },
  synology: { color: '#FF9800', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: '群', image: '/icons/synology.png' },
  pve: { color: '#E57000', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'PV', image: '/icons/proxmox.ico' },
  proxmox: { color: '#E57000', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'PV', image: '/icons/proxmox.ico' },
  k8s: { color: '#326CE5', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'K8', image: '/icons/cloud.png' },
  aws: { color: '#FF9900', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'AW', image: '/icons/aws.png' },
  ftp: { color: '#795548', bgColor: 'bg-amber-50 dark:bg-amber-950', icon: 'FT', image: '/icons/server2.png' },
  safeline: { color: '#3F51B5', bgColor: 'bg-indigo-50 dark:bg-indigo-950', icon: '雷', image: '/icons/safeline.png' },
  ucloud: { color: '#4A90E2', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'UC', image: '/icons/ucloud.ico' },
  mwpanel: { color: '#673AB7', bgColor: 'bg-purple-50 dark:bg-purple-950', icon: 'MW', image: '/icons/mwpanel.ico' },
  ratpanel: { color: '#009688', bgColor: 'bg-teal-50 dark:bg-teal-950', icon: 'RP', image: '/icons/ratpanel.ico' },
  gcore: { color: '#FF6B35', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'GC', image: '/icons/gcore.ico' },
  wangsu: { color: '#1890FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '网', image: '/icons/wangsu.ico' },
  ctyun: { color: '#0066FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '天', image: '/icons/ctyun.ico' },
  ksyun: { color: '#0066FF', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '金', image: '/icons/ksyun.ico' },
  lucky: { color: '#FF5722', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'LK', image: '/icons/lucky.png' },
  fnos: { color: '#2196F3', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'FN', image: '/icons/fnos.png' },
  opanel: { color: '#1976D2', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: '1P', image: '/icons/opanel.png' },
  aws_cloudfront: { color: '#FF9900', bgColor: 'bg-orange-50 dark:bg-orange-950', icon: 'CF', image: '/icons/aws.png' },
  chopsticks: { color: '#8D6E63', bgColor: 'bg-amber-50 dark:bg-amber-950', icon: '筷', image: '/icons/bt.png' },
  xpanel: { color: '#00ACC1', bgColor: 'bg-cyan-50 dark:bg-cyan-950', icon: 'XP', image: '/icons/host.png' },
  goedge: { color: '#5C6BC0', bgColor: 'bg-indigo-50 dark:bg-indigo-950', icon: 'GE', image: '/icons/waf.png' },
  cdnfly: { color: '#7E57C2', bgColor: 'bg-purple-50 dark:bg-purple-950', icon: 'CF', image: '/icons/waf.png' },
  lecdn: { color: '#26A69A', bgColor: 'bg-teal-50 dark:bg-teal-950', icon: 'LE', image: '/icons/waf.png' },
  btwaf: { color: '#20A53A', bgColor: 'bg-green-50 dark:bg-green-950', icon: 'BW', image: '/icons/bt.png' },
  baolei: { color: '#D32F2F', bgColor: 'bg-red-50 dark:bg-red-950', icon: '堡', image: '/icons/waf.png' },
  ucloud_cdn: { color: '#4A90E2', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'UC', image: '/icons/ucloud.ico' },
  kangle: { color: '#0288D1', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'KA', image: '/icons/host.png' },
  kangle_admin: { color: '#0277BD', bgColor: 'bg-blue-50 dark:bg-blue-950', icon: 'K+', image: '/icons/host.png' },
}

export interface Provider {
  type: string
  name: string
  icon?: string
  note?: string
  deploy_note?: string
  config: ProviderConfigField[]
}

interface AccountFormProps {
  type: 'dns' | 'cert' | 'deploy'
  providers: Provider[]
  onSubmit: (data: { type: string; name: string; config: Record<string, string>; remark: string }) => Promise<{ code: number; msg?: string }>
  backUrl: string
  title: string
  description: string
}

// 独立的配置字段组件，避免闭包问题
function ConfigField({
  field,
  value,
  onChange,
  allConfig,
}: {
  field: ProviderConfigField
  value: string
  onChange: (key: string, value: string) => void
  allConfig: Record<string, string>
}) {
  if (!evaluateDeployFieldShow(field.show, allConfig)) return null

  if (field.type === 'radio' && field.options) {
    const v = value || field.value || field.options[0]?.value || ''
    return (
      <div className="space-y-2">
        <Label>
          {field.name}
          {field.required && <span className="text-destructive ml-1">*</span>}
        </Label>
        <RadioGroup
          value={v}
          onValueChange={(nv) => onChange(field.key, nv)}
          className="flex flex-wrap gap-4"
        >
          {field.options.map((opt) => (
            <div key={opt.value} className="flex items-center space-x-2">
              <RadioGroupItem value={opt.value} id={`acc-${field.key}-${opt.value}`} />
              <Label htmlFor={`acc-${field.key}-${opt.value}`} className="font-normal cursor-pointer">
                {opt.label}
              </Label>
            </div>
          ))}
        </RadioGroup>
        {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
      </div>
    )
  }

  if (field.type === 'select' && field.options) {
    const resolved = resolveSelectFieldValue(field, value)
    return (
      <div className="space-y-2">
        <Label htmlFor={`config-${field.key}`}>
          {field.name}
          {field.required && <span className="text-destructive ml-1">*</span>}
        </Label>
        <Select value={resolved} onValueChange={(v) => onChange(field.key, v)}>
          <SelectTrigger id={`config-${field.key}`}>
            <SelectValue placeholder={field.placeholder || `请选择${field.name}`} />
          </SelectTrigger>
          <SelectContent>
            {field.options.map((opt) => (
              <SelectItem key={opt.value} value={opt.value}>
                {opt.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
        {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
      </div>
    )
  }

  if (field.type === 'textarea') {
    return (
      <div className="space-y-2">
        <Label htmlFor={`config-${field.key}`}>
          {field.name}
          {field.required && <span className="text-destructive ml-1">*</span>}
        </Label>
        <Textarea
          id={`config-${field.key}`}
          name={`config-${field.key}`}
          value={value}
          onChange={(e) => onChange(field.key, e.target.value)}
          placeholder={field.placeholder}
          rows={3}
        />
        {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
      </div>
    )
  }

  return (
    <div className="space-y-2">
      <Label htmlFor={`config-${field.key}`}>
        {field.name}
        {field.required && <span className="text-destructive ml-1">*</span>}
      </Label>
      <Input
        id={`config-${field.key}`}
        name={`config-${field.key}`}
        type={field.type === 'password' ? 'password' : 'text'}
        value={value}
        onChange={(e) => onChange(field.key, e.target.value)}
        placeholder={field.placeholder}
        autoComplete="off"
      />
      {field.note && <p className="text-xs text-muted-foreground">{field.note}</p>}
    </div>
  )
}

export function AccountForm({ type, providers, onSubmit, backUrl, title, description }: AccountFormProps) {
  const router = useRouter()
  const [selectedType, setSelectedType] = useState<string | null>(null)
  const [formData, setFormData] = useState({
    name: '',
    config: {} as Record<string, string>,
    remark: '',
  })
  const [submitting, setSubmitting] = useState(false)

  // 根据账户类型选择分类配置
  const categories = type === 'dns' ? DNS_CATEGORIES : type === 'cert' ? CERT_CATEGORIES : DEPLOY_CATEGORIES

  // 获取当前选中的提供商
  const currentProvider = providers.find((p) => p.type === selectedType)

  // 选择类型后初始化配置
  useEffect(() => {
    if (currentProvider) {
      const initialConfig: Record<string, string> = {}
      currentProvider.config?.forEach((field) => {
        initialConfig[field.key] = field.value || ''
      })
      setFormData((prev) => ({ ...prev, config: initialConfig, name: '' }))
    }
  }, [currentProvider])

  // 按分类分组提供商
  const groupedProviders = Object.entries(categories).map(([category, typeList]) => ({
    category,
    providers: providers.filter((p) => typeList.includes(p.type)),
  })).filter((g) => g.providers.length > 0)

  // 未分类的提供商
  const categorizedTypes = Object.values(categories).flat()
  const uncategorizedProviders = providers.filter((p) => !categorizedTypes.includes(p.type))
  if (uncategorizedProviders.length > 0) {
    groupedProviders.push({ category: '其他', providers: uncategorizedProviders })
  }

  const getProviderStyle = (providerType: string) => {
    return providerStyles[providerType] || {
      color: '#9E9E9E',
      bgColor: 'bg-gray-100 dark:bg-gray-800',
      icon: providerType.charAt(0).toUpperCase(),
      image: undefined,
    }
  }

  // 渲染提供商图标
  const renderProviderIcon = (providerType: string, size: 'sm' | 'md' = 'md') => {
    const style = getProviderStyle(providerType)
    const sizeClass = size === 'sm' ? 'w-8 h-8' : 'w-10 h-10'
    const textSize = size === 'sm' ? 'text-xs' : 'text-sm'
    
    if (style.image) {
      return (
        <div className={cn(sizeClass, 'rounded-lg overflow-hidden flex-shrink-0 bg-white dark:bg-gray-800 p-1')}>
          {/* eslint-disable-next-line @next/next/no-img-element -- 提供商静态图标 URL */}
          <img
            src={style.image}
            alt={providerType}
            className="w-full h-full object-contain"
            onError={(e) => {
              // 图片加载失败时降级为文字图标（安全审计 M-2：改用 textContent 构建 DOM，杜绝 innerHTML 注入面）
              const target = e.target as HTMLImageElement
              const parent = target.parentElement
              if (!parent) return
              target.style.display = 'none'
              parent.replaceChildren()
              const span = document.createElement('span')
              span.className = `${textSize} font-bold`
              span.style.color = style.color
              span.textContent = style.icon
              parent.appendChild(span)
              parent.classList.add('flex', 'items-center', 'justify-center', style.bgColor)
            }}
          />
        </div>
      )
    }
    
    return (
      <div
        className={cn(
          sizeClass,
          'rounded-lg flex items-center justify-center font-bold flex-shrink-0',
          textSize,
          style.bgColor
        )}
        style={{ color: style.color }}
      >
        {style.icon}
      </div>
    )
  }

  const handleSelectType = (providerType: string) => {
    setSelectedType(providerType)
  }

  const handleBack = () => {
    if (selectedType) {
      setSelectedType(null)
    } else {
      router.push(backUrl)
    }
  }

  // 配置字段变更处理函数
  const handleConfigChange = (key: string, value: string) => {
    setFormData((prev) => ({
      ...prev,
      config: { ...prev.config, [key]: value },
    }))
  }

  const handleSubmit = async () => {
    if (!selectedType) {
      toast.error('请选择类型')
      return
    }

    // 验证必填字段
    const requiredFields = currentProvider?.config?.filter((f) => f.required) || []
    for (const field of requiredFields) {
      if (!isDeployFieldVisible(field, formData.config)) continue
      const v = formData.config[field.key]
      if (v === undefined || String(v).trim() === '') {
        toast.error(`请填写${field.name}`)
        return
      }
    }

    // 使用第一个配置字段的值作为账户名称（如果name为空）
    let accountName = formData.name
    if (!accountName && currentProvider?.config?.length) {
      accountName = formData.config[currentProvider.config[0].key] || ''
    }
    if (!accountName) {
      toast.error('请填写账户名称')
      return
    }

    setSubmitting(true)
    try {
      const res = await onSubmit({
        type: selectedType,
        name: accountName,
        config: formData.config,
        remark: formData.remark,
      })

      if (res.code === 0) {
        toast.success('添加成功')
        router.push(backUrl)
      } else {
        toast.error(res.msg || '添加失败')
      }
    } catch {
      toast.error('添加失败')
    } finally {
      setSubmitting(false)
    }
  }

  return (
    <div className="space-y-6">
      <div className="flex items-center gap-4">
        <Button variant="ghost" size="icon" onClick={handleBack}>
          <ArrowLeft className="h-5 w-5" />
        </Button>
        <div>
          <h1 className="text-2xl font-bold tracking-tight">{title}</h1>
          <p className="text-muted-foreground">{description}</p>
        </div>
      </div>

      {!selectedType ? (
        // 类型选择视图
        <div className="space-y-6">
          {groupedProviders.map(({ category, providers: categoryProviders }) => (
            <div key={category}>
              <h3 className="text-lg font-semibold mb-4 text-muted-foreground border-b pb-2">
                {category}
              </h3>
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-3 xl:grid-cols-4 gap-4">
                {categoryProviders.map((provider) => {
                  return (
                    <Card
                      key={provider.type}
                      className="cursor-pointer hover:border-primary hover:shadow-md transition-all"
                      onClick={() => handleSelectType(provider.type)}
                    >
                      <CardContent className="p-4">
                        <div className="flex items-start gap-3">
                          {renderProviderIcon(provider.type, 'md')}
                          <div className="flex-1 min-w-0">
                            <div className="flex items-center justify-between">
                              <h4 className="font-medium truncate">{provider.name}</h4>
                              <ChevronRight className="h-4 w-4 text-muted-foreground flex-shrink-0" />
                            </div>
                            {provider.note && (
                              <p className="text-xs text-muted-foreground mt-1 line-clamp-2">
                                {provider.note}
                              </p>
                            )}
                          </div>
                        </div>
                      </CardContent>
                    </Card>
                  )
                })}
              </div>
            </div>
          ))}
        </div>
      ) : (
        // 配置表单视图
        <Card>
          <CardHeader>
            <div className="flex items-center justify-between">
              <div className="flex items-center gap-3">
                {renderProviderIcon(selectedType, 'md')}
                <div>
                  <CardTitle>{currentProvider?.name}</CardTitle>
                  <CardDescription>配置账户信息</CardDescription>
                </div>
              </div>
              <Button variant="outline" size="sm" onClick={() => setSelectedType(null)}>
                重新选择
              </Button>
            </div>
          </CardHeader>
          <CardContent className="space-y-4">
            {currentProvider?.config?.map((field) => (
              <ConfigField
                key={field.key}
                field={field}
                value={formData.config[field.key] || ''}
                onChange={handleConfigChange}
                allConfig={formData.config}
              />
            ))}

            <div className="space-y-2">
              <Label htmlFor="account-remark">备注</Label>
              <Textarea
                id="account-remark"
                value={formData.remark}
                onChange={(e) => setFormData((prev) => ({ ...prev, remark: e.target.value }))}
                placeholder="请输入备注信息（可选）"
                rows={2}
              />
            </div>

            {currentProvider?.deploy_note && type === 'deploy' && (
              <div className="p-3 rounded-lg border border-amber-200/80 bg-amber-50/80 dark:border-amber-900/50 dark:bg-amber-950/40 text-sm text-amber-900 dark:text-amber-100">
                <strong className="mr-1">任务说明：</strong>
                {currentProvider.deploy_note}
              </div>
            )}
            {currentProvider?.note && (
              <div className="p-4 bg-blue-50 dark:bg-blue-950 rounded-lg">
                <p className="text-sm text-blue-700 dark:text-blue-300">
                  <strong>提示：</strong>
                  {currentProvider.note}
                </p>
              </div>
            )}

            <div className="flex gap-3 pt-4">
              <Button variant="outline" onClick={() => setSelectedType(null)}>
                返回
              </Button>
              <Button onClick={handleSubmit} disabled={submitting}>
                {submitting && <Loader2 className="h-4 w-4 mr-2 animate-spin" />}
                提交
              </Button>
            </div>
          </CardContent>
        </Card>
      )}
    </div>
  )
}
