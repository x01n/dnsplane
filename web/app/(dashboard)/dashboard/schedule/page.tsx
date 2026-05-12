'use client'

import { useEffect, useState } from 'react'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from '@/components/ui/table'
import { Input } from '@/components/ui/input'
import { Badge } from '@/components/ui/badge'
import { Checkbox } from '@/components/ui/checkbox'
import { Label } from '@/components/ui/label'
import { Dialog, DialogContent, DialogFooter, DialogHeader, DialogTitle } from '@/components/ui/dialog'
import { Select, SelectContent, SelectItem, SelectTrigger, SelectValue } from '@/components/ui/select'
import { RefreshCw, Power, Trash2, Clock3, Plus, Pencil } from 'lucide-react'
import { toast } from 'sonner'
import { domainApi, scheduleApi, type Domain } from '@/lib/api'

type ScheduleTask = {
  id: number
  did: number
  rr: string
  record_id: string
  type: number
  cycle: number
  switch_type: number
  switch_date: string
  switch_time: string
  value: string
  line: string
  remark: string
  record_info?: string
  add_time: number
  update_time: number
  next_time: number
  active: boolean
  domain?: string
}

type ScheduleForm = {
  did: string
  rr: string
  recordid: string
  type: string
  cycle: string
  switchtype: string
  switchdate: string
  switchtime: string
  value: string
  line: string
  remark: string
  recordinfo: string
  active: boolean
}

const emptyForm: ScheduleForm = {
  did: '', rr: '', recordid: '', type: '0', cycle: '0', switchtype: '0', switchdate: '', switchtime: '', value: '', line: '', remark: '', recordinfo: '', active: true,
}

export default function SchedulePage() {
  const [loading, setLoading] = useState(true)
  const [refreshing, setRefreshing] = useState(false)
  const [keyword, setKeyword] = useState('')
  const [tasks, setTasks] = useState<ScheduleTask[]>([])
  const [total, setTotal] = useState(0)
  const [domains, setDomains] = useState<Domain[]>([])
  const [selectedIds, setSelectedIds] = useState<number[]>([])
  const [dialogOpen, setDialogOpen] = useState(false)
  const [editingTask, setEditingTask] = useState<ScheduleTask | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [batchSubmitting, setBatchSubmitting] = useState(false)
  const [bulkAction, setBulkAction] = useState<'open' | 'close' | 'delete'>('open')
  const [form, setForm] = useState<ScheduleForm>(emptyForm)

  const loadDomains = async () => {
    try {
      const res = await domainApi.list({ page: 1, page_size: 500 })
      if (res.code === 0 && res.data) setDomains(res.data.list || [])
    } catch {
      // ignore
    }
  }

  const load = async (kw?: string) => {
    setLoading(true)
    try {
      const res = await scheduleApi.list(kw ? { keyword: kw, page: 1, page_size: 100 } : { page: 1, page_size: 100 })
      if (res.code === 0 && res.data) {
        setTasks((res.data.list || []) as ScheduleTask[])
        setTotal(res.data.total || 0)
      } else {
        toast.error(res.msg || '加载失败')
      }
    } catch {
      toast.error('加载失败')
    } finally {
      setLoading(false)
    }
  }

  useEffect(() => { load(); loadDomains() }, [])

  const refresh = async () => {
    setRefreshing(true)
    await load(keyword)
    setRefreshing(false)
  }

  const toggle = async (id: number, active: boolean) => {
    try {
      const res = await scheduleApi.toggle(id, !active)
      if (res.code === 0) {
        toast.success('状态已更新')
        load(keyword)
      } else {
        toast.error(res.msg || '更新失败')
      }
    } catch {
      toast.error('更新失败')
    }
  }

  const remove = async (id: number) => {
    try {
      const res = await scheduleApi.delete(id)
      if (res.code === 0) {
        toast.success('删除成功')
        load(keyword)
      } else {
        toast.error(res.msg || '删除失败')
      }
    } catch {
      toast.error('删除失败')
    }
  }

  const openCreate = () => {
    setEditingTask(null)
    setForm(emptyForm)
    setDialogOpen(true)
  }

  const openEdit = (task: ScheduleTask) => {
    setEditingTask(task)
    setForm({
      did: String(task.did),
      rr: task.rr || '',
      recordid: task.record_id || '',
      type: String(task.type ?? 0),
      cycle: String(task.cycle ?? 0),
      switchtype: String(task.switch_type ?? 0),
      switchdate: task.switch_date || '',
      switchtime: task.switch_time || '',
      value: task.value || '',
      line: task.line || '',
      remark: task.remark || '',
      recordinfo: task.record_info || '',
      active: !!task.active,
    })
    setDialogOpen(true)
  }

  const submitForm = async () => {
    if (!form.did || !form.rr.trim() || !form.recordid.trim()) {
      toast.error('请填写必填项')
      return
    }
    if (form.switchtype === '0' && !form.value.trim()) {
      toast.error('修改模式下请填写记录值')
      return
    }
    setSubmitting(true)
    const payload = {
      did: Number(form.did),
      rr: form.rr.trim(),
      recordid: form.recordid.trim(),
      type: Number(form.type),
      cycle: Number(form.cycle),
      switchtype: Number(form.switchtype),
      switchdate: form.switchdate.trim(),
      switchtime: form.switchtime.trim(),
      value: form.value.trim(),
      line: form.line.trim(),
      remark: form.remark.trim(),
      recordinfo: form.recordinfo.trim(),
      active: form.active,
    }
    try {
      const res = editingTask ? await scheduleApi.update(editingTask.id, payload) : await scheduleApi.create(payload)
      if (res.code === 0) {
        toast.success(editingTask ? '修改成功' : '创建成功')
        setDialogOpen(false)
        setForm(emptyForm)
        load(keyword)
      } else {
        toast.error(res.msg || '操作失败')
      }
    } catch {
      toast.error('操作失败')
    } finally {
      setSubmitting(false)
    }
  }

  const handleBatch = async () => {
    if (selectedIds.length === 0) {
      toast.error('请先选择任务')
      return
    }
    setBatchSubmitting(true)
    try {
      const res = await scheduleApi.batch(selectedIds, bulkAction)
      if (res.code === 0) {
        toast.success('批量操作成功')
        setSelectedIds([])
        load(keyword)
      } else {
        toast.error(res.msg || '批量操作失败')
      }
    } catch {
      toast.error('批量操作失败')
    } finally {
      setBatchSubmitting(false)
    }
  }

  const toggleSelect = (id: number) => setSelectedIds((prev) => prev.includes(id) ? prev.filter((x) => x !== id) : [...prev, id])
  const toggleSelectAll = () => setSelectedIds((prev) => prev.length === tasks.length ? [] : tasks.map((t) => t.id))

  const typeLabel = (v: number) => ({ 0: '单次', 1: '循环' }[v] || String(v))
  const switchTypeLabel = (v: number) => ({ 0: '修改', 1: '启用', 2: '暂停', 3: '删除' }[v] || String(v))
  const cycleLabel = (v: number) => ({ 0: '每天', 1: '每周', 2: '每月' }[v] || String(v))
  const formatTs = (ts: number) => ts > 0 ? new Date(ts * 1000).toLocaleString() : '-'

  const selectedDomain = domains.find((d) => String(d.id) === form.did)
  const sameDomainTasks = selectedDomain ? tasks.filter((t) => t.did === selectedDomain.id) : []

  return (
    <div className="space-y-6">
      <div className="flex items-center justify-between gap-4">
        <div>
          <h1 className="text-2xl font-bold tracking-tight flex items-center gap-2"><Clock3 className="h-6 w-6" />定时切换</h1>
          <p className="text-muted-foreground">查看并管理后端定时切换任务，当前共 {total} 项。</p>
        </div>
        <div className="flex gap-2">
          <Button variant="outline" onClick={refresh} disabled={refreshing}>{refreshing ? <RefreshCw className="h-4 w-4 animate-spin" /> : '刷新'}</Button>
          <Button onClick={openCreate}><Plus className="h-4 w-4 mr-2" />新建任务</Button>
        </div>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>任务列表</CardTitle>
        </CardHeader>
        <CardContent className="space-y-4">
          <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <div className="flex gap-2">
              <Input placeholder="搜索域名 / RR / 备注 / 值" value={keyword} onChange={(e) => setKeyword(e.target.value)} onKeyDown={(e) => { if (e.key === 'Enter') load(keyword) }} />
              <Button onClick={() => load(keyword)}>搜索</Button>
            </div>
            <div className="flex items-center gap-2">
              <Checkbox checked={selectedIds.length === tasks.length && tasks.length > 0} onCheckedChange={toggleSelectAll} />
              <span className="text-sm text-muted-foreground">已选 {selectedIds.length} 项</span>
              <Select value={bulkAction} onValueChange={(v: 'open' | 'close' | 'delete') => setBulkAction(v)}>
                <SelectTrigger className="w-[120px]"><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="open">启用</SelectItem>
                  <SelectItem value="close">停用</SelectItem>
                  <SelectItem value="delete">删除</SelectItem>
                </SelectContent>
              </Select>
              <Button variant="outline" onClick={handleBatch} disabled={batchSubmitting || selectedIds.length === 0}>{batchSubmitting ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : null}批量执行</Button>
            </div>
          </div>

          <Table>
            <TableHeader>
              <TableRow>
                <TableHead className="w-10"></TableHead>
                <TableHead>ID</TableHead>
                <TableHead>域名</TableHead>
                <TableHead>RR</TableHead>
                <TableHead>动作</TableHead>
                <TableHead>模式</TableHead>
                <TableHead>周期</TableHead>
                <TableHead>执行时间</TableHead>
                <TableHead>下次执行</TableHead>
                <TableHead>状态</TableHead>
                <TableHead className="text-right">操作</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {loading ? (
                <TableRow><TableCell colSpan={11}>加载中...</TableCell></TableRow>
              ) : tasks.length === 0 ? (
                <TableRow><TableCell colSpan={11}>暂无任务</TableCell></TableRow>
              ) : tasks.map((task) => (
                <TableRow key={task.id}>
                  <TableCell><Checkbox checked={selectedIds.includes(task.id)} onCheckedChange={() => toggleSelect(task.id)} /></TableCell>
                  <TableCell>{task.id}</TableCell>
                  <TableCell>{task.domain || '-'}</TableCell>
                  <TableCell>{task.rr}</TableCell>
                  <TableCell>{switchTypeLabel(task.switch_type)}</TableCell>
                  <TableCell>{typeLabel(task.type)}</TableCell>
                  <TableCell>{task.type === 1 ? cycleLabel(task.cycle) : '-'}</TableCell>
                  <TableCell>{task.type === 1 ? `${task.switch_date || '-'} ${task.switch_time || '-'}` : task.switch_time || '-'}</TableCell>
                  <TableCell>{formatTs(task.next_time)}</TableCell>
                  <TableCell>{task.active ? <Badge>启用</Badge> : <Badge variant="secondary">停用</Badge>}</TableCell>
                  <TableCell className="text-right space-x-2">
                    <Button variant="outline" size="sm" onClick={() => openEdit(task)}><Pencil className="h-4 w-4 mr-1" />编辑</Button>
                    <Button variant="outline" size="sm" onClick={() => toggle(task.id, task.active)}><Power className="h-4 w-4 mr-1" />{task.active ? '停用' : '启用'}</Button>
                    <Button variant="destructive" size="sm" onClick={() => remove(task.id)}><Trash2 className="h-4 w-4 mr-1" />删除</Button>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      <Dialog open={dialogOpen} onOpenChange={setDialogOpen}>
        <DialogContent className="max-w-2xl">
          <DialogHeader>
            <DialogTitle>{editingTask ? '编辑任务' : '新建任务'}</DialogTitle>
          </DialogHeader>
          <div className="grid grid-cols-1 md:grid-cols-2 gap-4">
            <div className="space-y-2">
              <Label>域名</Label>
              <Select value={form.did} onValueChange={(v) => setForm((prev) => ({ ...prev, did: v }))}>
                <SelectTrigger><SelectValue placeholder="选择域名" /></SelectTrigger>
                <SelectContent>
                  {domains.map((domain) => <SelectItem key={domain.id} value={String(domain.id)}>{domain.name}</SelectItem>)}
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>主机记录 RR</Label>
              <Input value={form.rr} onChange={(e) => setForm((prev) => ({ ...prev, rr: e.target.value }))} placeholder="如 @ / www" />
            </div>
            <div className="space-y-2">
              <Label>Record ID</Label>
              {sameDomainTasks.length > 0 ? (
                <Select value={form.recordid} onValueChange={(v) => setForm((prev) => ({ ...prev, recordid: v, rr: prev.rr || (sameDomainTasks.find((t) => t.record_id === v)?.rr || prev.rr) }))}>
                  <SelectTrigger><SelectValue placeholder="选择已有 record_id 或手动输入" /></SelectTrigger>
                  <SelectContent>
                    {sameDomainTasks.map((task) => <SelectItem key={`${task.id}-${task.record_id}`} value={task.record_id}>{task.rr} ({task.record_id})</SelectItem>)}
                  </SelectContent>
                </Select>
              ) : (
                <Input value={form.recordid} onChange={(e) => setForm((prev) => ({ ...prev, recordid: e.target.value }))} placeholder="服务商记录 ID" />
              )}
            </div>
            <div className="space-y-2">
              <Label>任务模式</Label>
              <Select value={form.type} onValueChange={(v) => setForm((prev) => ({ ...prev, type: v }))}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">单次</SelectItem>
                  <SelectItem value="1">循环</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>动作类型</Label>
              <Select value={form.switchtype} onValueChange={(v) => setForm((prev) => ({ ...prev, switchtype: v }))}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">修改</SelectItem>
                  <SelectItem value="1">启用</SelectItem>
                  <SelectItem value="2">暂停</SelectItem>
                  <SelectItem value="3">删除</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>循环周期</Label>
              <Select value={form.cycle} onValueChange={(v) => setForm((prev) => ({ ...prev, cycle: v }))}>
                <SelectTrigger><SelectValue /></SelectTrigger>
                <SelectContent>
                  <SelectItem value="0">每天</SelectItem>
                  <SelectItem value="1">每周</SelectItem>
                  <SelectItem value="2">每月</SelectItem>
                </SelectContent>
              </Select>
            </div>
            <div className="space-y-2">
              <Label>切换日期</Label>
              <Input value={form.switchdate} onChange={(e) => setForm((prev) => ({ ...prev, switchdate: e.target.value }))} placeholder={form.cycle === '1' ? '0-6' : form.cycle === '2' ? '1-31' : '可留空'} />
            </div>
            <div className="space-y-2">
              <Label>执行时间</Label>
              <Input value={form.switchtime} onChange={(e) => setForm((prev) => ({ ...prev, switchtime: e.target.value }))} placeholder={form.type === '1' ? '08:30' : '2026-05-01 08:30'} />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label>记录值</Label>
              <Input value={form.value} onChange={(e) => setForm((prev) => ({ ...prev, value: e.target.value }))} placeholder="修改模式必填，例如 1.2.3.4" disabled={form.switchtype !== '0'} />
            </div>
            <div className="space-y-2">
              <Label>线路</Label>
              <Input value={form.line} onChange={(e) => setForm((prev) => ({ ...prev, line: e.target.value }))} placeholder="如 default" disabled={form.switchtype !== '0'} />
            </div>
            <div className="space-y-2">
              <Label>备注</Label>
              <Input value={form.remark} onChange={(e) => setForm((prev) => ({ ...prev, remark: e.target.value }))} placeholder="可选备注" />
            </div>
            <div className="space-y-2 md:col-span-2">
              <Label>RecordInfo JSON</Label>
              <Input value={form.recordinfo} onChange={(e) => setForm((prev) => ({ ...prev, recordinfo: e.target.value }))} placeholder='{"Line":"default","TTL":600}' disabled={form.switchtype !== '0'} />
            </div>
            <div className="flex items-center gap-2 md:col-span-2">
              <Checkbox checked={form.active} onCheckedChange={(checked) => setForm((prev) => ({ ...prev, active: checked === true }))} />
              <span className="text-sm">启用任务</span>
            </div>
          </div>
          <DialogFooter>
            <Button variant="outline" onClick={() => setDialogOpen(false)}>取消</Button>
            <Button onClick={submitForm} disabled={submitting}>{submitting ? <RefreshCw className="h-4 w-4 mr-2 animate-spin" /> : null}{editingTask ? '保存修改' : '创建任务'}</Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </div>
  )
}
