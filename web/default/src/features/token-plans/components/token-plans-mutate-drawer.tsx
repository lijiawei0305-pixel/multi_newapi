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
import { zodResolver } from '@hookform/resolvers/zod'
import { CalendarClock, CreditCard, Settings2, Sparkles } from 'lucide-react'
import { useEffect, useState } from 'react'
import { useForm, type Resolver } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  SideDrawerSection,
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
  sideDrawerSwitchItemClassName,
} from '@/components/drawer-layout'
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
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import { Switch } from '@/components/ui/switch'

import { createTokenPlan, updateTokenPlan } from '../api'
import {
  getTokenPlanFormSchema,
  TOKEN_PLAN_FORM_DEFAULTS,
  planToFormValues,
  formValuesToPayload,
  type TokenPlanFormValues,
} from '../lib'
import type { AdminTokenPlan } from '../types'
import { useTokenPlans } from './token-plans-provider'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: AdminTokenPlan
}

/** Number `<Input>` that writes a numeric value back into the form. */
function numberChange(onChange: (v: number) => void, integer = false) {
  return (e: React.ChangeEvent<HTMLInputElement>) => {
    const parsed = integer
      ? Number.parseInt(e.target.value, 10)
      : Number.parseFloat(e.target.value)
    onChange(Number.isNaN(parsed) ? 0 : parsed)
  }
}

export function TokenPlansMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: Props) {
  const { t } = useTranslation()
  const isEdit = !!currentRow?.id
  const { triggerRefresh } = useTokenPlans()
  const [isSubmitting, setIsSubmitting] = useState(false)

  const schema = getTokenPlanFormSchema(t)
  const form = useForm<TokenPlanFormValues>({
    resolver: zodResolver(schema) as unknown as Resolver<TokenPlanFormValues>,
    defaultValues: TOKEN_PLAN_FORM_DEFAULTS,
  })

  useEffect(() => {
    if (open) {
      form.reset(
        currentRow ? planToFormValues(currentRow) : TOKEN_PLAN_FORM_DEFAULTS
      )
    }
  }, [open, currentRow, form])

  const onSubmit = async (values: TokenPlanFormValues) => {
    setIsSubmitting(true)
    try {
      const payload = formValuesToPayload(values)
      const res =
        isEdit && currentRow?.id
          ? await updateTokenPlan(currentRow.id, payload)
          : await createTokenPlan(payload)
      if (res.success) {
        toast.success(isEdit ? t('Update succeeded') : t('Create succeeded'))
        onOpenChange(false)
        triggerRefresh()
      }
      // Business failures (success === false) are surfaced by the shared
      // axios interceptor reading `message`/`code` (api-contract §1.1).
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
            {isEdit ? t('Update token plan') : t('Create new token plan')}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? t('Modify the token plan configuration')
              : t('Fill in the fields to create a new token plan')}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='token-plan-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            {/* Basic Info */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <Settings2 className='h-4 w-4' />
                {t('Basic Info')}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='code'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Plan Code')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder='mini'
                          disabled={isEdit}
                        />
                      </FormControl>
                      <FormDescription>
                        {t('Unique identifier, e.g. mini / basic / pro')}
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
                      <FormLabel>{t('Plan Name')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder={t('e.g. Mini')} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SideDrawerSection>

            {/* Pricing (CNY ¥) */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <CreditCard className='h-4 w-4' />
                {t('Pricing')}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='base_price_cny'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Selling Price (¥)')}</FormLabel>
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
                  name='anchor_price_cny'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Anchor Price (¥)')}</FormLabel>
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
                        {t('Strikethrough marketing price only; not billed.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

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
                  name='min_price_cny'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Protection Price (¥)')}</FormLabel>
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
                        {t('Lowest retail price agents may set.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <FormField
                control={form.control}
                name='discount_label'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Discount Label')}</FormLabel>
                    <FormControl>
                      <Input {...field} placeholder='-91%' />
                    </FormControl>
                    <FormMessage />
                  </FormItem>
                )}
              />
            </SideDrawerSection>

            {/* Quota & Validity */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <CalendarClock className='h-4 w-4' />
                {t('Quota & Validity')}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='month_limit_usd'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Monthly Limit (USD)')}</FormLabel>
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
                        {t('Monthly usage cap, measured in USD.')}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='valid_days'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Valid Days')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          min={1}
                          onChange={numberChange(field.onChange, true)}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='multiplier'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Multiplier')}</FormLabel>
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
              </div>
            </SideDrawerSection>

            {/* Display */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <Sparkles className='h-4 w-4' />
                {t('Display')}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='badge'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Badge')}</FormLabel>
                      <FormControl>
                        <Input {...field} placeholder={t('e.g. Hot')} />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='sort'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Sort')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          onChange={numberChange(field.onChange, true)}
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>

              <div className='flex flex-col gap-3'>
                <FormField
                  control={form.control}
                  name='is_recommended'
                  render={({ field }) => (
                    <FormItem className={sideDrawerSwitchItemClassName()}>
                      <FormLabel className='!mt-0'>
                        {t('Recommended')}
                      </FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value}
                          onCheckedChange={field.onChange}
                        />
                      </FormControl>
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='status'
                  render={({ field }) => (
                    <FormItem className={sideDrawerSwitchItemClassName()}>
                      <FormLabel className='!mt-0'>{t('Status')}</FormLabel>
                      <FormControl>
                        <Switch
                          checked={field.value === 'enabled'}
                          onCheckedChange={(c) =>
                            field.onChange(c ? 'enabled' : 'disabled')
                          }
                        />
                      </FormControl>
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
            form='token-plan-form'
            type='submit'
            disabled={isSubmitting}
            data-testid='tp-form-save'
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
