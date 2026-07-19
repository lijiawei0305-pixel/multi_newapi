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
import { Banknote, Check, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'

import { isApproved, isPending } from '../lib'
import type { Withdrawal } from '../types'
import { useWithdrawals } from './withdrawals-provider'

export function WithdrawalRowActions({ row }: { row: Withdrawal }) {
  const { t } = useTranslation()
  const { openAction } = useWithdrawals()

  if (isPending(row.status)) {
    return (
      <div className='flex items-center gap-2'>
        <Button
          size='sm'
          variant='outline'
          className='text-success hover:text-success h-7'
          onClick={() => openAction(row, 'approve')}
          data-testid={`wd-approve-${row.id}`}
        >
          <Check className='h-3.5 w-3.5' />
          {t('Approve')}
        </Button>
        <Button
          size='sm'
          variant='outline'
          className='text-destructive hover:text-destructive h-7'
          onClick={() => openAction(row, 'reject')}
          data-testid={`wd-reject-${row.id}`}
        >
          <X className='h-3.5 w-3.5' />
          {t('Reject')}
        </Button>
      </div>
    )
  }

  if (isApproved(row.status)) {
    return (
      <Button
        size='sm'
        variant='outline'
        className='h-7'
        onClick={() => openAction(row, 'mark-paid')}
        data-testid={`wd-mark-paid-${row.id}`}
      >
        <Banknote className='h-3.5 w-3.5' />
        {t('Mark as paid', { defaultValue: '标记已打款' })}
      </Button>
    )
  }

  return <span className='text-muted-foreground'>—</span>
}
