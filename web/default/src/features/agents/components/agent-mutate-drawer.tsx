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
import { useEffect, useState } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery } from '@tanstack/react-query'
import { CreditCard, TrendingUp, UserCog } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import { getUsers } from '@/features/users/api'
import { Button } from '@/components/ui/button'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { createAgent, getAgentMetrics, setAgentDomain, updateAgent } from '../api'
import {
  AGENT_FORM_DEFAULTS,
  agentToFormValues,
  cny,
  formValuesToPayload,
  getAgentFormSchema,
  type AgentFormValues,
} from '../lib'
import type { Agent } from '../types'
import { useAgents } from './agents-provider'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Agent
}

/** Number `<Input>` that writes a numeric value back into the form. */
function numberChange(onChange: (v: number) => void, integer = false) {
  return (e: React.ChangeEvent<HTMLInputElement>) => {
    const parsed = integer
      ? parseInt(e.target.value, 10)
      : parseFloat(e.target.value)
    onChange(Number.isNaN(parsed) ? 0 : parsed)
  }
}

export function AgentMutateDrawer({ open, onOpenChange, currentRow }: Props) {
  const { t } = useTranslation()
  const isEdit = !!currentRow?.id
  const { triggerRefresh } = useAgents()
  const [isSubmitting, setIsSubmitting] = useState(false)
  const [domainLabel, setDomainLabel] = useState('')
  const [settingDomain, setSettingDomain] = useState(false)

  // Owner candidates — only needed when creating a brand-new agent.
  const { data: users } = useQuery({
    queryKey: ['admin-users-for-agent'],
    queryFn: async () => {
      const res = await getUsers({ p: 1, page_size: 100 })
      return res.data?.items || []
    },
    enabled: open && !isEdit,
  })

  // Read-only promotion metrics — only when editing an existing agent.
  const { data: metricsRes } = useQuery({
    queryKey: ['admin-agent-metrics', currentRow?.id],
    queryFn: () => (currentRow?.id ? getAgentMetrics(currentRow.id) : null),
    enabled: open && isEdit && !!currentRow?.id,
  })
  const metrics = metricsRes?.data

  const schema = getAgentFormSchema(t)
  const form = useForm<AgentFormValues>({
    resolver: zodResolver(schema) as unknown as Resolver<AgentFormValues>,
    defaultValues: AGENT_FORM_DEFAULTS,
  })

  useEffect(() => {
    if (open) {
      form.reset(
        currentRow ? agentToFormValues(currentRow) : AGENT_FORM_DEFAULTS
      )
    }
  }, [open, currentRow, form])

  const onSubmit = async (values: AgentFormValues) => {
    setIsSubmitting(true)
    try {
      const payload = formValuesToPayload(values)
      const res =
        isEdit && currentRow?.id
          ? await updateAgent(currentRow.id, {
              name: payload.name,
              commission_ratio: payload.commission_ratio,
              discount_ratio: payload.discount_ratio,
              level: payload.level,
            })
          : await createAgent(payload)
      if (res.success) {
        toast.success(
          isEdit
            ? t('Update succeeded', { defaultValue: '更新成功' })
            : t('Create succeeded', { defaultValue: '创建成功' })
        )
        onOpenChange(false)
        triggerRefresh()
      }
      // Business failures (success === false) are surfaced by the shared
      // axios interceptor reading `message`/`code`.
    } catch {
      toast.error(t('Request failed', { defaultValue: '请求失败' }))
    } finally {
      setIsSubmitting(false)
    }
  }

  // 开通/更新子域名是独立于表单提交的动作（PUT /api/admin/agents/:id/domain）。
  const handleSetDomain = async () => {
    const label = domainLabel.trim().toLowerCase()
    if (!currentRow?.id || !label) return
    setSettingDomain(true)
    try {
      const res = await setAgentDomain(currentRow.id, label)
      if (res.success) {
        toast.success(t('Subdomain opened', { defaultValue: '子域名已开通' }))
        setDomainLabel('')
        triggerRefresh()
      }
    } catch {
      toast.error(t('Request failed', { defaultValue: '请求失败' }))
    } finally {
      setSettingDomain(false)
    }
  }

  return (
    <Sheet
      open={open}
      onOpenChange={(v) => {
        onOpenChange(v)
        if (!v) form.reset()
      }}
    >
      <SheetContent className={sideDrawerContentClassName('sm:max-w-[600px]')}>
        <SheetHeader className={sideDrawerHeaderClassName()}>
          <SheetTitle>
            {isEdit
              ? t('Update agent', { defaultValue: '编辑代理' })
              : t('Create new agent', { defaultValue: '新建代理' })}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? t('Modify the agent configuration', {
                  defaultValue: '修改代理配置',
                })
              : t('Fill in the fields to create a new agent', {
                  defaultValue: '填写以下字段以创建新代理',
                })}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='agent-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            {/* Identity */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <UserCog className='h-4 w-4' />
                {t('Identity', { defaultValue: '身份' })}
              </h3>

              <FormField
                control={form.control}
                name='owner_user_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>
                      {t('Owner User', { defaultValue: '所属用户' })}
                    </FormLabel>
                    <FormControl>
                      {isEdit ? (
                        <Input
                          value={currentRow?.owner_username || ''}
                          disabled
                          readOnly
                        />
                      ) : (
                        <NativeSelect
                          value={field.value ? String(field.value) : ''}
                          onChange={(e) =>
                            field.onChange(Number(e.target.value) || 0)
                          }
                        >
                          <NativeSelectOption value=''>
                            {t('Select a user', { defaultValue: '选择用户' })}
                          </NativeSelectOption>
                          {(users || []).map((u) => (
                            <NativeSelectOption key={u.id} value={String(u.id)}>
                              {u.username}
                              {u.display_name ? ` (${u.display_name})` : ''}
                            </NativeSelectOption>
                          ))}
                        </NativeSelect>
                      )}
                    </FormControl>
                    <FormDescription>
                      {t('The user account this agent belongs to.', {
                        defaultValue: '该代理归属的用户账号。',
                      })}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='slug'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Slug', { defaultValue: '标识 Slug' })}
                      </FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder='acme'
                          disabled={isEdit}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Unique tenant identifier; used in links.', {
                          defaultValue: '租户唯一标识，用于生成链接域名。',
                        })}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Agent Name', { defaultValue: '代理名称' })}
                      </FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder={t('e.g. Acme', {
                            defaultValue: '如：某某代理',
                          })}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='level'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Agent Level', { defaultValue: '代理等级' })}
                      </FormLabel>
                      <FormControl>
                        <NativeSelect
                          value={String(field.value ?? 0)}
                          onChange={(e) =>
                            field.onChange(Number(e.target.value) || 0)
                          }
                        >
                          <NativeSelectOption value='0'>
                            {t('Basic Agent', { defaultValue: '基础代理' })}
                          </NativeSelectOption>
                          <NativeSelectOption value='1'>
                            {t('Independent Agent', { defaultValue: '独立代理' })}
                          </NativeSelectOption>
                        </NativeSelect>
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Independent unlocks subdomain, custom domain and site branding. Promote manually when the agent performs well.',
                          {
                            defaultValue: '独立代理解锁子域名、自定义域名与站点品牌装修；代理表现良好时再手动升级。',
                          }
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              {isEdit && (
                <div className='flex flex-col gap-1.5'>
                  <label className='text-sm font-medium'>
                    {t('Subdomain', { defaultValue: '子域名' })}
                  </label>
                  {currentRow?.subdomain ? (
                    <div className='text-muted-foreground text-sm'>
                      {t('Current', { defaultValue: '当前' })}:{' '}
                      <a
                        href={`https://${currentRow.subdomain}`}
                        target='_blank'
                        rel='noreferrer'
                        className='text-primary underline'
                      >
                        {currentRow.subdomain}
                      </a>
                    </div>
                  ) : null}
                  <div className='flex items-center gap-2'>
                    <Input
                      value={domainLabel}
                      onChange={(e) => setDomainLabel(e.target.value)}
                      placeholder='acme'
                    />
                    <span className='text-muted-foreground text-sm whitespace-nowrap'>
                      .wedreamhub.com
                    </span>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={handleSetDomain}
                      disabled={settingDomain || !domainLabel.trim()}
                    >
                      {settingDomain
                        ? t('Saving...', { defaultValue: '保存中…' })
                        : t('Open subdomain', { defaultValue: '开通' })}
                    </Button>
                  </div>
                  <p className='text-muted-foreground text-xs'>
                    {t('Subdomain hint', {
                      defaultValue: '输入 label 开通 <label>.wedreamhub.com 代理站；更新会替换旧子域名。',
                    })}
                  </p>
                </div>
              )}
            </SideDrawerSection>

            {isEdit && (
              <SideDrawerSection>
                <h3 className='flex items-center gap-2 text-sm font-medium'>
                  <TrendingUp className='h-4 w-4' />
                  {t('Promotion metrics', { defaultValue: '升级参考指标' })}
                </h3>
                <div className='grid grid-cols-3 gap-3'>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Total recharge (¥)', { defaultValue: '累计充值（¥）' })}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? cny(metrics.recharge_total_cny) : '—'}
                    </div>
                  </div>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Commission earned (¥)', {
                        defaultValue: '累计分润（¥）',
                      })}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? cny(metrics.commission_earned_cny) : '—'}
                    </div>
                  </div>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Downstream users', { defaultValue: '下级用户数' })}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? String(metrics.downstream_user_count) : '—'}
                    </div>
                  </div>
                </div>
                <FormDescription>
                  {t(
                    'Lifetime totals to help you decide whether to promote this agent to independent (level 1).',
                    {
                      defaultValue: '累计数据，帮助你判断是否将该代理升级为独立档（等级 1）。',
                    }
                  )}
                </FormDescription>
              </SideDrawerSection>
            )}

            {/* Commercials */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <CreditCard className='h-4 w-4' />
                {t('Commercials', { defaultValue: '商务配置' })}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='discount_ratio'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Agent Discount Ratio', { defaultValue: '折扣系数' })}
                      </FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          step='0.01'
                          min={0}
                          onChange={numberChange(field.onChange)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Agent Discount Ratio Hint', {
                          defaultValue: '全线批发折扣 = 主站价 × 系数（如 0.8 即八折）。消耗按分组基准倍率、套餐按主站价缩放；留空或 0 = 不打折。',
                        })}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='commission_ratio'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>
                        {t('Commission Ratio', { defaultValue: '分润比例' })}
                      </FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          step='0.01'
                          min={0}
                          onChange={numberChange(field.onChange)}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Share of consumption revenue, e.g. 0.1.', {
                          defaultValue: '消耗分润比例（L0 基础档提成），如 0.1。',
                        })}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close', { defaultValue: '关闭' })}
          </SheetClose>
          <Button
            form='agent-form'
            type='submit'
            disabled={isSubmitting}
            data-testid='agent-form-save'
          >
            {isSubmitting
              ? t('Saving...', { defaultValue: '保存中…' })
              : t('Save changes', { defaultValue: '保存' })}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
