'use client'

import { Braces, Info } from 'lucide-react'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Textarea } from '@/components/ui/textarea'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import type { SystemConfig } from '@/lib/api'

type Props = {
  config: SystemConfig
  /** 以补丁方式回写配置，避免子组件持有整份 config 的更新细节 */
  onChange: (patch: Partial<SystemConfig>) => void
}

/** 后端 sendCustomWebhook 会 ToUpper+Trim 后交给 http.NewRequest，标准方法均可用 */
const METHODS = ['POST', 'PUT', 'PATCH', 'GET'] as const

/** 模板留空时后端使用该通用报文，占位符按字面替换 */
const DEFAULT_BODY = '{"title":"{title}","content":"{content}"}'

/**
 * CustomWebhookFields - 自定义 Webhook 渠道的配置项
 *
 * 字段与后端 notify.CustomWebhookConfig 一一对应：
 * 地址、方法、请求头、正文模板、内容类型、内容格式全部由用户定义，
 * 且后端不做钉钉/飞书/企业微信的地址识别，与「Webhook 通用」渠道互不干扰
 *
 * @param config 当前系统配置，读取 custom_webhook_* 六个键
 * @param onChange 字段变更回调，参数为需要合并进配置的补丁
 */
export function CustomWebhookFields({ config, onChange }: Props) {
  return (
    <div className="space-y-4">
      <div className="space-y-2">
        <Label>Webhook URL</Label>
        <Input
          placeholder="https://example.com/custom-hook"
          value={config.custom_webhook_url || ''}
          onChange={(e) => onChange({ custom_webhook_url: e.target.value })}
          autoComplete="off"
        />
        <p className="text-xs text-muted-foreground">
          内网地址与回环地址会被服务端拦截，请填写可从服务器访问的公网地址
        </p>
      </div>

      <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
        <div className="space-y-2">
          <Label>请求方法</Label>
          <Select
            value={config.custom_webhook_method || 'POST'}
            onValueChange={(v) => onChange({ custom_webhook_method: v })}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              {METHODS.map((m) => (
                <SelectItem key={m} value={m}>
                  {m}
                </SelectItem>
              ))}
            </SelectContent>
          </Select>
          <p className="text-xs text-muted-foreground">
            选 GET 时正文会转为查询参数附加到地址后
          </p>
        </div>
        <div className="space-y-2">
          <Label>Content-Type</Label>
          <Select
            value={config.custom_webhook_content_type || 'application/json'}
            onValueChange={(v) => onChange({ custom_webhook_content_type: v })}
          >
            <SelectTrigger>
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="application/json">application/json</SelectItem>
              <SelectItem value="application/x-www-form-urlencoded">
                application/x-www-form-urlencoded
              </SelectItem>
              <SelectItem value="text/plain">text/plain</SelectItem>
            </SelectContent>
          </Select>
        </div>
      </div>

      <div className="space-y-2">
        <Label>内容格式</Label>
        <Select
          value={config.custom_webhook_content_format || 'text'}
          onValueChange={(v) => onChange({ custom_webhook_content_format: v })}
        >
          <SelectTrigger>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            <SelectItem value="text">纯文本（去 HTML 标签）</SelectItem>
            <SelectItem value="markdown">Markdown（去标签并转换粗体）</SelectItem>
            <SelectItem value="html">原样（保留 HTML 标签）</SelectItem>
          </SelectContent>
        </Select>
        <p className="text-xs text-muted-foreground">
          告警正文携带换行与强调标签，可按接收端支持的格式转换
        </p>
      </div>

      <div className="space-y-2">
        <Label>自定义请求头</Label>
        <Textarea
          placeholder={'Authorization: Bearer xxx\nX-Custom: value'}
          rows={3}
          value={config.custom_webhook_headers || ''}
          onChange={(e) => onChange({ custom_webhook_headers: e.target.value })}
        />
        <p className="text-xs text-muted-foreground">每行一个，格式: Key: Value</p>
      </div>

      <div className="space-y-2">
        <Label className="flex items-center gap-1.5">
          <Braces className="h-3.5 w-3.5" aria-hidden />
          请求体模板
        </Label>
        <Textarea
          placeholder={DEFAULT_BODY}
          rows={4}
          value={config.custom_webhook_body || ''}
          onChange={(e) => onChange({ custom_webhook_body: e.target.value })}
        />
        <div className="flex gap-1.5 rounded-md border border-border/70 bg-muted/40 p-2.5">
          <Info className="mt-0.5 h-3.5 w-3.5 shrink-0 text-muted-foreground" aria-hidden />
          <p className="text-xs leading-snug text-muted-foreground">
            支持占位符 {'{title}'} 与 {'{content}'}，留空时使用通用报文 {DEFAULT_BODY}。
            Content-Type 为 application/json 且模板拼出的正文不是合法 JSON（例如正文含换行或引号）时，
            服务端会回退为通用报文，以免接收端解析失败导致通知丢失；需要正文保持原样时请改用 text/plain。
          </p>
        </div>
      </div>
    </div>
  )
}