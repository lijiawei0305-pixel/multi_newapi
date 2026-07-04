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
import { createAgent, getAgentMetrics, updateAgent } from '../api'
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
              cost_price_cny: payload.cost_price_cny,
              package_discount: payload.package_discount,
              commission_ratio: payload.commission_ratio,
              discount_ratio: payload.discount_ratio,
              level: payload.level,
            })
          : await createAgent(payload)
      if (res.success) {
        toast.success(isEdit ? t('Update succeeded') : t('Create succeeded'))
        onOpenChange(false)
        triggerRefresh()
      }
      // Business failures (success === false) are surfaced by the shared
      // axios interceptor reading `message`/`code`.
    } catch {
      toast.error(t('Request failed'))
    } finally {
      setIsSubmitting(false)
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
            {isEdit ? t('Update agent') : t('Create new agent')}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? t('Modify the agent configuration')
              : t('Fill in the fields to create a new agent')}
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
                {t('Identity')}
              </h3>

              <FormField
                control={form.control}
                name='owner_user_id'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Owner User')}</FormLabel>
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
                            {t('Select a user')}
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
                      {t('The user account this agent belongs to.')}
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
                      <FormLabel>{t('Slug')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder='acme'
                          disabled={isEdit}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Unique tenant identifier; used in links.')}
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
                      <FormLabel>{t('Agent Name')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder={t('e.g. Acme')} />
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
                      <FormLabel>{t('Agent Level')}</FormLabel>
                      <FormControl>
                        <NativeSelect
                          value={String(field.value ?? 0)}
                          onChange={(e) =>
                            field.onChange(Number(e.target.value) || 0)
                          }
                        >
                          <NativeSelectOption value='0'>
                            {t('Basic Agent')}
                          </NativeSelectOption>
                          <NativeSelectOption value='1'>
                            {t('Independent Agent')}
                          </NativeSelectOption>
                        </NativeSelect>
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Independent unlocks subdomain, custom domain and site branding. Promote manually when the agent performs well.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SideDrawerSection>

            {isEdit && (
              <SideDrawerSection>
                <h3 className='flex items-center gap-2 text-sm font-medium'>
                  <TrendingUp className='h-4 w-4' />
                  {t('Promotion metrics')}
                </h3>
                <div className='grid grid-cols-3 gap-3'>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Total recharge (¥)')}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? cny(metrics.recharge_total_cny) : '—'}
                    </div>
                  </div>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Commission earned (¥)')}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? cny(metrics.commission_earned_cny) : '—'}
                    </div>
                  </div>
                  <div className='rounded-md border p-3'>
                    <div className='text-xs text-muted-foreground'>
                      {t('Downstream users')}
                    </div>
                    <div className='text-lg font-semibold'>
                      {metrics ? String(metrics.downstream_user_count) : '—'}
                    </div>
                  </div>
                </div>
                <FormDescription>
                  {t(
                    'Lifetime totals to help you decide whether to promote this agent to independent (level 1).'
                  )}
                </FormDescription>
              </SideDrawerSection>
            )}

            {/* Commercials */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <CreditCard className='h-4 w-4' />
                {t('Commercials')}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='cost_price_cny'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Cost Price (¥)')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          step='0.01'
                          min={0}
                          onChange={numberChange(field.onChange)}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='package_discount'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Package Discount')}</FormLabel>
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
                        {t('Multiplier on plan retail price, e.g. 0.85.')}
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
                      <FormLabel>{t('Commission Ratio')}</FormLabel>
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
                        {t('Share of consumption revenue, e.g. 0.1.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

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
                          defaultValue:
                            '全线批发折扣 = 主站价 × 系数（如 0.8 即八折）。消耗按分组基准倍率、套餐按主站价缩放；留空或 0 = 不打折。',
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
            {t('Close')}
          </SheetClose>
          <Button
            form='agent-form'
            type='submit'
            disabled={isSubmitting}
            data-testid='agent-form-save'
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
