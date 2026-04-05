'use client'

import { cn } from '@/lib/utils'

// DNS 和证书服务商图标配置
const providerConfig: Record<string, { name: string; color: string; bgColor: string; icon: string }> = {
  // DNS 服务商
  cloudflare: {
    name: 'Cloudflare',
    color: '#F38020',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: 'CF',
  },
  aliyun: {
    name: '阿里云',
    color: '#FF6A00',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: '阿',
  },
  dnspod: {
    name: '腾讯云',
    color: '#00A4FF',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: '腾',
  },
  huawei: {
    name: '华为云',
    color: '#C7000B',
    bgColor: 'bg-red-50 dark:bg-red-950',
    icon: '华',
  },
  baidu: {
    name: '百度云',
    color: '#2932E1',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: '百',
  },
  huoshan: {
    name: '火山引擎',
    color: '#3370FF',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: '火',
  },
  jdcloud: {
    name: '京东云',
    color: '#C91623',
    bgColor: 'bg-red-50 dark:bg-red-950',
    icon: '京',
  },
  west: {
    name: '西部数码',
    color: '#2E7D32',
    bgColor: 'bg-green-50 dark:bg-green-950',
    icon: '西',
  },
  dnsla: {
    name: 'DNSLA',
    color: '#1565C0',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: 'LA',
  },
  namesilo: {
    name: 'NameSilo',
    color: '#4CAF50',
    bgColor: 'bg-green-50 dark:bg-green-950',
    icon: 'NS',
  },
  powerdns: {
    name: 'PowerDNS',
    color: '#263238',
    bgColor: 'bg-gray-50 dark:bg-gray-900',
    icon: 'PD',
  },
  bt: {
    name: '宝塔域名',
    color: '#20A53A',
    bgColor: 'bg-green-50 dark:bg-green-950',
    icon: '宝',
  },
  aliyunesa: {
    name: '阿里云ESA',
    color: '#FF6A00',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: 'EA',
  },
  tencenteo: {
    name: '腾讯云EO',
    color: '#00A4FF',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: 'EO',
  },
  spaceship: {
    name: 'Spaceship',
    color: '#6366F1',
    bgColor: 'bg-indigo-50 dark:bg-indigo-950',
    icon: 'SP',
  },
  // 证书服务商
  letsencrypt: {
    name: "Let's Encrypt",
    color: '#003A70',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: 'LE',
  },
  zerossl: {
    name: 'ZeroSSL',
    color: '#E91E63',
    bgColor: 'bg-pink-50 dark:bg-pink-950',
    icon: 'ZS',
  },
  google: {
    name: 'Google SSL',
    color: '#4285F4',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: 'G',
  },
  litessl: {
    name: 'LiteSSL',
    color: '#00BCD4',
    bgColor: 'bg-cyan-50 dark:bg-cyan-950',
    icon: 'LS',
  },
  tencent: {
    name: '腾讯云SSL',
    color: '#00A4FF',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: '腾',
  },
  aliyun_cert: {
    name: '阿里云SSL',
    color: '#FF6A00',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: '阿',
  },
  customacme: {
    name: '自定义ACME',
    color: '#9E9E9E',
    bgColor: 'bg-gray-50 dark:bg-gray-900',
    icon: 'AC',
  },
  // 部署服务商
  ssh: {
    name: 'SSH',
    color: '#424242',
    bgColor: 'bg-gray-50 dark:bg-gray-900',
    icon: 'SS',
  },
  local: {
    name: '本地',
    color: '#607D8B',
    bgColor: 'bg-gray-50 dark:bg-gray-900',
    icon: 'LC',
  },
  aliyun_cdn: {
    name: '阿里云CDN',
    color: '#FF6A00',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: 'AC',
  },
  tencent_cdn: {
    name: '腾讯云CDN',
    color: '#00A4FF',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: 'TC',
  },
  huawei_cdn: {
    name: '华为云CDN',
    color: '#C7000B',
    bgColor: 'bg-red-50 dark:bg-red-950',
    icon: 'HC',
  },
  qiniu: {
    name: '七牛云',
    color: '#07BEFF',
    bgColor: 'bg-cyan-50 dark:bg-cyan-950',
    icon: '七',
  },
  upyun: {
    name: '又拍云',
    color: '#2196F3',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: '又',
  },
  btpanel: {
    name: '宝塔面板',
    color: '#20A53A',
    bgColor: 'bg-green-50 dark:bg-green-950',
    icon: '宝',
  },
  '1panel': {
    name: '1Panel',
    color: '#1976D2',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: '1P',
  },
  synology: {
    name: '群晖',
    color: '#FF9800',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: '群',
  },
  pve: {
    name: 'Proxmox',
    color: '#E57000',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: 'PV',
  },
  k8s: {
    name: 'K8S',
    color: '#326CE5',
    bgColor: 'bg-blue-50 dark:bg-blue-950',
    icon: 'K8',
  },
  aws: {
    name: 'AWS',
    color: '#FF9900',
    bgColor: 'bg-orange-50 dark:bg-orange-950',
    icon: 'AW',
  },
  ftp: {
    name: 'FTP',
    color: '#795548',
    bgColor: 'bg-amber-50 dark:bg-amber-950',
    icon: 'FT',
  },
  safeline: {
    name: '雷池WAF',
    color: '#3F51B5',
    bgColor: 'bg-indigo-50 dark:bg-indigo-950',
    icon: '雷',
  },
}

interface ProviderIconProps {
  type: string
  size?: 'sm' | 'md' | 'lg'
  showName?: boolean
  className?: string
}

export function ProviderIcon({ type, size = 'md', showName = false, className }: ProviderIconProps) {
  const config = providerConfig[type?.toLowerCase()] || {
    name: type || '未知',
    color: '#9E9E9E',
    bgColor: 'bg-gray-100 dark:bg-gray-800',
    icon: type?.charAt(0)?.toUpperCase() || '?',
  }

  const sizeClasses = {
    sm: 'w-5 h-5 text-[10px]',
    md: 'w-7 h-7 text-xs',
    lg: 'w-9 h-9 text-sm',
  }

  return (
    <div className={cn('flex items-center gap-2', className)}>
      <div
        className={cn(
          'rounded-md flex items-center justify-center font-bold flex-shrink-0',
          config.bgColor,
          sizeClasses[size]
        )}
        style={{ color: config.color }}
        title={config.name}
      >
        {config.icon}
      </div>
      {showName && <span className="text-sm font-medium">{config.name}</span>}
    </div>
  )
}

// 获取服务商名称
export function getProviderName(type: string): string {
  const config = providerConfig[type?.toLowerCase()]
  return config?.name || type || '未知'
}

// 获取服务商配置
export function getProviderConfig(type: string) {
  return providerConfig[type?.toLowerCase()] || null
}

// Badge 样式的服务商标签
interface ProviderBadgeProps {
  type: string
  name?: string
  className?: string
}

export function ProviderBadge({ type, name, className }: ProviderBadgeProps) {
  const config = providerConfig[type?.toLowerCase()] || {
    name: name || type || '未知',
    color: '#9E9E9E',
    bgColor: 'bg-gray-100 dark:bg-gray-800',
    icon: type?.charAt(0)?.toUpperCase() || '?',
  }

  return (
    <span
      className={cn(
        'inline-flex items-center gap-1.5 px-2 py-0.5 rounded-md text-xs font-medium',
        config.bgColor,
        className
      )}
      style={{ color: config.color }}
    >
      <span className="font-bold">{config.icon}</span>
      <span>{name || config.name}</span>
    </span>
  )
}
