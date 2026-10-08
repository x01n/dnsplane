'use client'

import { useState, useEffect, useCallback } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { toast } from 'sonner'
import { systemApi } from '@/lib/api'
import { RefreshCw, Gauge, KeyRound, Route, Timer } from 'lucide-react'

/*
 * OptimizeIPSettings 优选IP 配置组件
 * 功能：读写 /system/optimizeip/config 白名单内的四个键 ——
 *      optimize_ip_api 数据接口、optimize_ip_key 接口密钥、
 *      optimize_ip_proxy 代理前缀、optimize_ip_min 自动更新间隔。
 *      后端只落库白名单内的键，提交额外键会被静默跳过
 */

/**
 * 数据接口选项
 * 取值与 service.OptimizeAPIWetest / OptimizeAPIHostmonit / OptimizeAPIXingpingcn 三个常量对齐
 */
const API_OPTIONS = [
  {
    value: '0',
    label: 'wetest.vip',
    hint: '按 CDN 线路返回优选结果，需要接口密钥',
  },
  {
    value: '1',
    label: 'hostmonit.com',
    hint: '不区分 CDN 线路，需要接口密钥',
  },
  {
    value: '2',
    label: 'xingpingcn.top',
    hint: '免费且无需密钥，数据取自 GitHub 仓库；仅电信/联通/移动三网且不支持 IPv6',
  },
] as const

/** 自动更新间隔下限（分钟），与 handler.UpdateOptimizeIPConfig 的校验保持一致 */
const MIN_INTERVAL_MINUTES = 10

/**
 * OptimizeIPSettings 优选IP 配置卡片
 * @returns 优选IP 数据接口、密钥、代理与更新间隔的编辑界面
 */
export default function OptimizeIPSettings() {
  const [config, setConfig] = useState<Record<string, string>>({})
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [querying, setQuerying] = useState(false)

  /**
   * 拉取优选IP 配置
   * @param silent 为 true 时保持现有界面不进入加载态，用于保存后的静默刷新
   */
  const loadConfig = useCallback(async (silent = false) => {
    if (!silent) setLoading(true)
    try {
      const res = await systemApi.getOptimizeIPConfig()
      if (res.code === 0 && res.data) {
        setConfig({
          optimize_ip_api: res.data.optimize_ip_api || '0',
          optimize_ip_key: res.data.optimize_ip_key || '',
          optimize_ip_proxy: res.data.optimize_ip_proxy || '',
          optimize_ip_min: res.data.optimize_ip_min || '30',
        })
      } else if (!silent) {
        toast.error(res.msg || '加载配置失败')
      }
    } catch {
      if (!silent) toast.error('加载配置失败')
    } finally {
      if (!silent) setLoading(false)
    }
  }, [])

  useEffect(() => { loadConfig() }, [loadConfig])

  const setVal = (key: string, value: string) => {
    setConfig((prev) => ({ ...prev, [key]: value }))
  }

  const api = config.optimize_ip_api || '0'
  const apiHint = API_OPTIONS.find((o) => o.value === api)?.hint

  const handleSave = async () => {
    const minutes = Number(config.optimize_ip_min)
    if (!Number.isFinite(minutes) || minutes < MIN_INTERVAL_MINUTES) {
      toast.error(`自动更新间隔不能小于 ${MIN_INTERVAL_MINUTES} 分钟`)
      return
    }
    setSaving(true)
    try {
      const res = await systemApi.updateOptimizeIPConfig({
        optimize_ip_api: api,
        optimize_ip_key: config.optimize_ip_key || '',
        optimize_ip_proxy: config.optimize_ip_proxy || '',
        optimize_ip_min: String(minutes),
      })
      if (res.code === 0) {
        toast.success('保存成功')
        // 留空的密钥与间隔会被后端在读取时回填默认值，静默重载以显示实际生效值
        void loadConfig(true)
      } else {
        toast.error(res.msg || '保存失败')
      }
    } catch {
      toast.error('保存失败')
    } finally {
      setSaving(false)
    }
  }

  const handleQueryLicense = async () => {
    setQuerying(true)
    try {
      const res = await systemApi.queryOptimizeIPLicense({
        optimize_ip_api: Number(api),
        optimize_ip_key: config.optimize_ip_key || '',
      })
      if (res.code === 0) {
        toast.success(res.msg || '查询成功')
      } else {
        toast.error(res.msg || '查询失败')
      }
    } catch {
      toast.error('查询失败')
    } finally {
      setQuerying(false)
    }
  }

  if (loading) {
    return (
      <div className="flex items-center justify-center h-[200px]">
        <RefreshCw className="h-6 w-6 animate-spin text-muted-foreground" />
      </div>
    )
  }

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle className="flex items-center gap-2">
            <Gauge className="h-5 w-5" />
            优选IP 配置
          </CardTitle>
          <CardDescription>
            配置优选IP 任务使用的数据接口与自动更新间隔，后台调度器每分钟检查一次，按此处设置的间隔刷新解析记录
          </CardDescription>
        </CardHeader>
        <CardContent className="space-y-6">
          <div className="space-y-2">
            <Label>数据接口</Label>
            <Select value={api} onValueChange={(v) => setVal('optimize_ip_api', v)}>
              <SelectTrigger>
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {API_OPTIONS.map((option) => (
                  <SelectItem key={option.value} value={option.value}>
                    {option.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            {apiHint && <p className="text-xs text-muted-foreground">{apiHint}</p>}
          </div>

          {api !== '2' && (
            <div className="space-y-2">
              <Label className="flex items-center gap-1.5">
                <KeyRound className="h-3.5 w-3.5" />
                接口密钥
              </Label>
              <div className="flex gap-2">
                <Input
                  value={config.optimize_ip_key || ''}
                  onChange={(e) => setVal('optimize_ip_key', e.target.value)}
                  placeholder="留空则使用默认密钥"
                  autoComplete="off"
                  className="flex-1"
                />
                <Button
                  type="button"
                  variant="outline"
                  onClick={handleQueryLicense}
                  disabled={querying}
                >
                  {querying ? <RefreshCw className="h-4 w-4 animate-spin" /> : '查询余额'}
                </Button>
              </div>
              <p className="text-xs text-muted-foreground">
                默认密钥的请求次数有限，可在此查询剩余可用次数；留空保存后读取时会显示为默认密钥
              </p>
            </div>
          )}

          {api === '2' && (
            <div className="space-y-2">
              <Label className="flex items-center gap-1.5">
                <Route className="h-3.5 w-3.5" />
                代理前缀
              </Label>
              <Input
                value={config.optimize_ip_proxy || ''}
                onChange={(e) => setVal('optimize_ip_proxy', e.target.value)}
                placeholder="https://your-proxy.example.com"
                autoComplete="off"
              />
              <p className="text-xs text-muted-foreground">
                该接口的数据文件托管在 GitHub，直连失败时可填写代理前缀替代默认的 raw.githubusercontent.com；
                地址由服务端做安全校验，不合法会让拉取直接失败
              </p>
            </div>
          )}

          <div className="space-y-2">
            <Label className="flex items-center gap-1.5">
              <Timer className="h-3.5 w-3.5" />
              自动更新间隔（分钟）
            </Label>
            <Input
              type="number"
              min={MIN_INTERVAL_MINUTES}
              value={config.optimize_ip_min || ''}
              onChange={(e) => setVal('optimize_ip_min', e.target.value)}
            />
            <p className="text-xs text-muted-foreground">
              不能小于 {MIN_INTERVAL_MINUTES} 分钟；留空按 30 分钟计算
            </p>
          </div>
        </CardContent>
      </Card>

      <div className="flex justify-end">
        <Button onClick={handleSave} disabled={saving}>
          {saving ? <><RefreshCw className="h-4 w-4 mr-1 animate-spin" />保存中...</> : '保存设置'}
        </Button>
      </div>
    </div>
  )
}