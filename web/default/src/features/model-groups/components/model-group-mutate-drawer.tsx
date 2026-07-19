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
import { Link2, Sliders } from 'lucide-react'
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
import { Textarea } from '@/components/ui/textarea'

import { createModelGroup, updateModelGroup } from '../api'
import {
  MODEL_GROUP_FORM_DEFAULTS,
  formValuesToCreatePayload,
  formValuesToUpdatePayload,
  getModelGroupFormSchema,
  modelGroupToFormValues,
  type ModelGroupFormValues,
} from '../lib'
import type { ModelGroup } from '../types'
import { useModelGroups } from './model-groups-provider'

interface Props {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: ModelGroup
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

export function ModelGroupMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: Props) {
  const { t } = useTranslation()
  const isEdit = !!currentRow?.id
  const { triggerRefresh } = useModelGroups()
  const [isSubmitting, setIsSubmitting] = useState(false)

  const schema = getModelGroupFormSchema(t)
  const form = useForm<ModelGroupFormValues>({
    resolver: zodResolver(schema) as unknown as Resolver<ModelGroupFormValues>,
    defaultValues: MODEL_GROUP_FORM_DEFAULTS,
  })

  useEffect(() => {
    if (open) {
      form.reset(
        currentRow
          ? modelGroupToFormValues(currentRow)
          : MODEL_GROUP_FORM_DEFAULTS
      )
    }
  }, [open, currentRow, form])

  const onSubmit = async (values: ModelGroupFormValues) => {
    setIsSubmitting(true)
    try {
      const res =
        isEdit && currentRow?.name
          ? await updateModelGroup(
              currentRow.name,
              formValuesToUpdatePayload(values)
            )
          : await createModelGroup(formValuesToCreatePayload(values))
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
            {isEdit ? t('Update model group') : t('Create new model group')}
          </SheetTitle>
          <SheetDescription>
            {isEdit
              ? t('Modify the model group configuration')
              : t('Fill in the fields to create a new model group')}
          </SheetDescription>
        </SheetHeader>
        <Form {...form}>
          <form
            id='model-group-form'
            onSubmit={form.handleSubmit(onSubmit)}
            className={sideDrawerFormClassName()}
          >
            {/* Basics */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <Sliders className='h-4 w-4' />
                {t('Basics')}
              </h3>

              <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
                <FormField
                  control={form.control}
                  name='name'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Group Name')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          placeholder='claude-kiro'
                          disabled={isEdit}
                          data-testid='model-group-name-input'
                        />
                      </FormControl>
                      <FormMessage />
                    </FormItem>
                  )}
                />

                <FormField
                  control={form.control}
                  name='ratio'
                  render={({ field }) => (
                    <FormItem>
                      <FormLabel>{t('Ratio')}</FormLabel>
                      <FormControl>
                        <Input
                          {...field}
                          type='number'
                          step='0.01'
                          min={0}
                          onChange={numberChange(field.onChange)}
                          data-testid='model-group-ratio-input'
                        />
                      </FormControl>
                      <FormDescription>
                        {t(
                          'Model group coefficient, multiplied with the user tier ratio.'
                        )}
                      </FormDescription>
                      <FormMessage />
                    </FormItem>
                  )}
                />
              </div>
            </SideDrawerSection>

            {/* Binding & Display */}
            <SideDrawerSection>
              <h3 className='flex items-center gap-2 text-sm font-medium'>
                <Link2 className='h-4 w-4' />
                {t('Binding & Display')}
              </h3>

              <p className='text-muted-foreground text-xs'>
                {t(
                  'Binding is configured in Channel Management: put this group name into the channel Model Group field.'
                )}
              </p>

              <FormField
                control={form.control}
                name='description'
                render={({ field }) => (
                  <FormItem>
                    <FormLabel>{t('Description')}</FormLabel>
                    <FormControl>
                      <Textarea {...field} rows={3} />
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
                        step='1'
                        onChange={numberChange(field.onChange, true)}
                      />
                    </FormControl>
                    <FormDescription>
                      {t('Lower values appear first.')}
                    </FormDescription>
                    <FormMessage />
                  </FormItem>
                )}
              />

              <FormField
                control={form.control}
                name='enabled'
                render={({ field }) => (
                  <FormItem className={sideDrawerSwitchItemClassName()}>
                    <FormLabel className='!mt-0'>{t('Enabled')}</FormLabel>
                    <FormControl>
                      <Switch
                        checked={field.value}
                        onCheckedChange={field.onChange}
                      />
                    </FormControl>
                  </FormItem>
                )}
              />
            </SideDrawerSection>
          </form>
        </Form>
        <SheetFooter className={sideDrawerFooterClassName()}>
          <SheetClose render={<Button variant='outline' />}>
            {t('Close')}
          </SheetClose>
          <Button
            form='model-group-form'
            type='submit'
            disabled={isSubmitting}
            data-testid='model-group-form-save'
          >
            {isSubmitting ? t('Saving...') : t('Save changes')}
          </Button>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  )
}
