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
import { Loader2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import {
  sideDrawerFooterClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  SheetClose,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'

import { ChannelTypeLogo } from './channel-type-logo'

export function ChannelMutateSheetHeader(props: {
  currentType: number
  isEditing: boolean
  typeLabel: string
}) {
  const { t } = useTranslation()

  return (
    <SheetHeader className={sideDrawerHeaderClassName()}>
      <SheetTitle className='flex items-center gap-3'>
        <span className='bg-muted flex size-9 shrink-0 items-center justify-center rounded-md'>
          <ChannelTypeLogo type={props.currentType} size={22} />
        </span>
        <span>
          {props.isEditing ? t('Edit Channel') : t('Create Channel')}
          <span className='text-muted-foreground ml-2 text-sm font-normal'>
            {props.typeLabel}
          </span>
        </span>
      </SheetTitle>
      <SheetDescription>
        {props.isEditing
          ? t("Update channel configuration and click save when you're done.")
          : t('Add a new channel by providing the necessary information.')}
      </SheetDescription>
    </SheetHeader>
  )
}

export function ChannelSensitiveSettingsAlert() {
  const { t } = useTranslation()

  return (
    <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
      <AlertDescription>
        {t('Sensitive channel settings are read-only for your account.')}{' '}
        {t(
          'You can still edit non-sensitive operations fields such as models, groups, priority, and weight.'
        )}
      </AlertDescription>
    </Alert>
  )
}

export function ChannelMutateSheetFooter(props: {
  isEditing: boolean
  isSubmitting: boolean
}) {
  const { t } = useTranslation()

  return (
    <SheetFooter className={sideDrawerFooterClassName()}>
      <SheetClose
        render={<Button variant='outline' disabled={props.isSubmitting} />}
      >
        {t('Cancel')}
      </SheetClose>
      <Button form='channel-form' type='submit' disabled={props.isSubmitting}>
        {props.isSubmitting && (
          <Loader2 className='mr-2 h-4 w-4 animate-spin' />
        )}
        {props.isEditing ? t('Update Channel') : t('Save changes')}
      </Button>
    </SheetFooter>
  )
}
