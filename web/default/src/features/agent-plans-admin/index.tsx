/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import { useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardHeader, CardTitle } from '@/components/ui/card'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import { Skeleton } from '@/components/ui/skeleton'
import { Switch } from '@/components/ui/switch'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import {
  type AdminAgentPlan,
  type AdminAgentPlanInput,
  createAgentPlan,
  getAdminAgentPlans,
  updateAgentPlan,
} from './api'

/** 表单态（数值字段用 string 便于输入，提交时转数字）。 */
interface FormState {
  id: number | null
  code: string
  name: string
  description: string
  price_cny: string
  anchor_price_cny: string
  discount_label: string
  grant_level: string
  grant_can_api: boolean
  grant_discount_ratio: string
  valid_days: string
  is_recommended: boolean
  badge: string
  sort: string
  enabled: boolean
}

const emptyForm: FormState = {
  id: null,
  code: '',
  name: '',
  description: '',
  price_cny: '0',
  anchor_price_cny: '0',
  discount_label: '',
  grant_level: '0',
  grant_can_api: false,
  grant_discount_ratio: '0',
  valid_days: '365',
  is_recommended: false,
  badge: '',
  sort: '0',
  enabled: true,
}

function toForm(p: AdminAgentPlan): FormState {
  return {
    id: p.id,
    code: p.code,
    name: p.name,
    description: p.description,
    price_cny: String(p.price_cny),
    anchor_price_cny: String(p.anchor_price_cny),
    discount_label: p.discount_label,
    grant_level: String(p.grant_level),
    grant_can_api: p.grant_can_api,
    grant_discount_ratio: String(p.grant_discount_ratio),
    valid_days: String(p.valid_days),
    is_recommended: p.is_recommended,
    badge: p.badge,
    sort: String(p.sort),
    enabled: p.status === 'enabled',
  }
}

function toInput(f: FormState): AdminAgentPlanInput {
  const num = (s: string) => Number.parseFloat(s) || 0
  const int = (s: string) => Math.trunc(Number.parseFloat(s) || 0)
  return {
    code: f.code.trim(),
    name: f.name.trim(),
    description: f.description.trim(),
    price_cny: num(f.price_cny),
    anchor_price_cny: num(f.anchor_price_cny),
    discount_label: f.discount_label.trim(),
    grant_level: int(f.grant_level),
    grant_can_api: f.grant_can_api,
    grant_discount_ratio: num(f.grant_discount_ratio),
    valid_days: int(f.valid_days),
    is_recommended: f.is_recommended,
    badge: f.badge.trim(),
    sort: int(f.sort),
    status: f.enabled ? 'enabled' : 'disabled',
  }
}

export function AgentPlansAdmin() {
  const { t } = useTranslation()
  const qc = useQueryClient()
  const [form, setForm] = useState<FormState | null>(null)

  const { data: plans, isLoading } = useQuery({
    queryKey: ['admin', 'agent-plans'],
    queryFn: getAdminAgentPlans,
  })

  const set = <K extends keyof FormState>(k: K, v: FormState[K]) =>
    setForm((f) => (f ? { ...f, [k]: v } : f))

  const save = useMutation({
    mutationFn: (f: FormState) =>
      f.id == null ? createAgentPlan(toInput(f)) : updateAgentPlan(f.id, toInput(f)),
    onSuccess: (res) => {
      if (!res.success) return
      toast.success(t('Saved', { defaultValue: '已保存' }))
      qc.invalidateQueries({ queryKey: ['admin', 'agent-plans'] })
      qc.invalidateQueries({ queryKey: ['become-agent', 'plans'] })
      qc.invalidateQueries({ queryKey: ['agent-join', 'public-agent-plans'] })
      setForm(null)
    },
  })

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>
        {t('Agent Plans Admin', { defaultValue: '代理套餐管理' })}
      </SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4 pb-4'>
          <div className='flex items-center justify-between'>
            <p className='text-muted-foreground text-sm'>
              {t('Agent Plans Admin Intro', {
                defaultValue:
                  '配置「购买代理套餐」的档位:价格、授予能力(代理等级/开放 API/批发折扣系数)、有效期与上下架。',
              })}
            </p>
            <Button size='sm' onClick={() => setForm({ ...emptyForm })}>
              {t('New', { defaultValue: '新建' })}
            </Button>
          </div>

          {isLoading ? (
            <Skeleton className='h-64 w-full' />
          ) : (
            <div className='rounded-lg border'>
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>{t('Name', { defaultValue: '名称' })}</TableHead>
                    <TableHead>Code</TableHead>
                    <TableHead>{t('Price', { defaultValue: '价格(¥)' })}</TableHead>
                    <TableHead>{t('Grant', { defaultValue: '授予' })}</TableHead>
                    <TableHead>{t('Validity', { defaultValue: '有效期' })}</TableHead>
                    <TableHead>{t('Status', { defaultValue: '状态' })}</TableHead>
                    <TableHead className='text-right'>
                      {t('Actions', { defaultValue: '操作' })}
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(plans ?? []).map((p) => (
                    <TableRow key={p.id}>
                      <TableCell className='font-medium'>
                        {p.name}
                        {p.is_recommended ? (
                          <span className='text-primary ml-1 text-[11px]'>★</span>
                        ) : null}
                      </TableCell>
                      <TableCell className='text-muted-foreground'>{p.code}</TableCell>
                      <TableCell>¥{p.price_cny.toLocaleString('zh-CN')}</TableCell>
                      <TableCell className='text-muted-foreground text-xs'>
                        L{p.grant_level}
                        {p.grant_can_api ? ' · API' : ''}
                        {p.grant_discount_ratio > 0
                          ? ` · ×${p.grant_discount_ratio}`
                          : ''}
                      </TableCell>
                      <TableCell>{p.valid_days}d</TableCell>
                      <TableCell>
                        <span
                          className={
                            p.status === 'enabled'
                              ? 'text-emerald-600 dark:text-emerald-400'
                              : 'text-muted-foreground'
                          }
                        >
                          {p.status === 'enabled'
                            ? t('Enabled', { defaultValue: '已上架' })
                            : t('Disabled', { defaultValue: '已下架' })}
                        </span>
                      </TableCell>
                      <TableCell className='text-right'>
                        <Button
                          variant='outline'
                          size='sm'
                          onClick={() => setForm(toForm(p))}
                        >
                          {t('Edit', { defaultValue: '编辑' })}
                        </Button>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {/* 内联创建/编辑表单 */}
          {form ? (
            <Card>
              <CardHeader>
                <CardTitle className='text-base'>
                  {form.id == null
                    ? t('New Agent Plan', { defaultValue: '新建代理套餐' })
                    : t('Edit Agent Plan', { defaultValue: '编辑代理套餐' })}
                </CardTitle>
              </CardHeader>
              <CardContent className='space-y-4'>
                <div className='grid gap-3 sm:grid-cols-2'>
                  <Field label='Code'>
                    <Input
                      value={form.code}
                      onChange={(e) => set('code', e.target.value)}
                      placeholder='basic / oem / api'
                    />
                  </Field>
                  <Field label={t('Name', { defaultValue: '名称' })}>
                    <Input value={form.name} onChange={(e) => set('name', e.target.value)} />
                  </Field>
                  <Field label={t('Description', { defaultValue: '描述' })} full>
                    <Input
                      value={form.description}
                      onChange={(e) => set('description', e.target.value)}
                    />
                  </Field>
                  <Field label={t('Price CNY', { defaultValue: '现价(¥)' })}>
                    <Input
                      type='number'
                      value={form.price_cny}
                      onChange={(e) => set('price_cny', e.target.value)}
                    />
                  </Field>
                  <Field label={t('Anchor Price CNY', { defaultValue: '原价划线(¥)' })}>
                    <Input
                      type='number'
                      value={form.anchor_price_cny}
                      onChange={(e) => set('anchor_price_cny', e.target.value)}
                    />
                  </Field>
                  <Field label={t('Discount Label', { defaultValue: '折扣角标' })}>
                    <Input
                      value={form.discount_label}
                      onChange={(e) => set('discount_label', e.target.value)}
                      placeholder='5折'
                    />
                  </Field>
                  <Field label={t('Valid Days', { defaultValue: '有效期(天)' })}>
                    <Input
                      type='number'
                      value={form.valid_days}
                      onChange={(e) => set('valid_days', e.target.value)}
                    />
                  </Field>
                  <Field label={t('Grant Level', { defaultValue: '代理等级(0普通/1独立)' })}>
                    <Input
                      type='number'
                      value={form.grant_level}
                      onChange={(e) => set('grant_level', e.target.value)}
                    />
                  </Field>
                  <Field
                    label={t('Grant Discount Ratio', { defaultValue: '批发折扣系数(0=不设)' })}
                  >
                    <Input
                      type='number'
                      value={form.grant_discount_ratio}
                      onChange={(e) => set('grant_discount_ratio', e.target.value)}
                      placeholder='0.9'
                    />
                  </Field>
                  <Field label={t('Sort', { defaultValue: '排序' })}>
                    <Input
                      type='number'
                      value={form.sort}
                      onChange={(e) => set('sort', e.target.value)}
                    />
                  </Field>
                </div>

                <div className='flex flex-wrap items-center gap-6'>
                  <Toggle
                    label={t('Grant CanAPI', { defaultValue: '授予开放 API' })}
                    checked={form.grant_can_api}
                    onChange={(v) => set('grant_can_api', v)}
                  />
                  <Toggle
                    label={t('Recommended', { defaultValue: '推荐' })}
                    checked={form.is_recommended}
                    onChange={(v) => set('is_recommended', v)}
                  />
                  <Toggle
                    label={t('On Sale', { defaultValue: '上架' })}
                    checked={form.enabled}
                    onChange={(v) => set('enabled', v)}
                  />
                </div>

                <div className='flex justify-end gap-2'>
                  <Button variant='outline' onClick={() => setForm(null)}>
                    {t('Cancel', { defaultValue: '取消' })}
                  </Button>
                  <Button disabled={save.isPending} onClick={() => save.mutate(form)}>
                    {t('Save', { defaultValue: '保存' })}
                  </Button>
                </div>
              </CardContent>
            </Card>
          ) : null}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}

function Field(props: {
  label: string
  full?: boolean
  children: React.ReactNode
}) {
  return (
    <div className={`space-y-1.5 ${props.full ? 'sm:col-span-2' : ''}`}>
      <Label className='text-xs'>{props.label}</Label>
      {props.children}
    </div>
  )
}

function Toggle(props: {
  label: string
  checked: boolean
  onChange: (v: boolean) => void
}) {
  return (
    <label className='flex cursor-pointer items-center gap-2 text-sm'>
      <Switch checked={props.checked} onCheckedChange={props.onChange} />
      <span>{props.label}</span>
    </label>
  )
}
