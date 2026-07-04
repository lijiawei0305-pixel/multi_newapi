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
import { Landmark, Pencil, Wallet } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Skeleton } from '@/components/ui/skeleton'
import { payoutMethodLabel } from '../lib'
import type { PayoutAccount } from '../types'

interface Props {
  account?: PayoutAccount
  loading?: boolean
  /** Open the edit dialog (owned by the parent page — also reused when a
   * withdrawal request bounces with PAYOUT_ACCOUNT_REQUIRED). */
  onEdit: () => void
}

/**
 * Status strip for the agent's payout (收款) destination, shown near the
 * balance on the earnings page. Read-only display only — editing happens in
 * the sibling `PayoutAccountDialog`, opened via `onEdit`.
 */
export function PayoutAccountCard({ account, loading, onEdit }: Props) {
  const { t } = useTranslation()
  const configured = !!account?.configured

  return (
    <section className='flex flex-col gap-2'>
      <h3 className='text-sm font-semibold'>
        {t('Payout Account', { defaultValue: '收款账户' })}
      </h3>
      <div className='flex flex-col gap-3 rounded-lg border p-3 sm:flex-row sm:items-center sm:justify-between sm:p-4'>
        {loading ? (
          <Skeleton className='h-5 w-48' />
        ) : configured ? (
          <div className='flex min-w-0 flex-wrap items-center gap-2 text-sm'>
            <StatusBadge
              label={t('Configured', { defaultValue: '已设置' })}
              variant='success'
              copyable={false}
            />
            {account?.payout_method === 'bank' ? (
              <Landmark className='text-muted-foreground size-4 shrink-0' />
            ) : (
              <Wallet className='text-muted-foreground size-4 shrink-0' />
            )}
            <span className='font-medium'>
              {payoutMethodLabel(account?.payout_method, t)}
            </span>
            <span className='text-muted-foreground truncate'>
              {account?.payout_account}
            </span>
            <span className='text-muted-foreground truncate'>
              {account?.payout_name}
            </span>
            {account?.payout_bank && (
              <span className='text-muted-foreground truncate'>
                {account.payout_bank}
              </span>
            )}
          </div>
        ) : (
          <div className='flex items-center gap-2 text-sm'>
            <StatusBadge
              label={t('Not set', { defaultValue: '未设置' })}
              variant='warning'
              copyable={false}
            />
            <span className='text-muted-foreground'>
              {t('Set your payout account before requesting a withdrawal', {
                defaultValue: '请先设置收款账户，才能申请提现',
              })}
            </span>
          </div>
        )}
        <Button
          size='sm'
          variant='outline'
          onClick={onEdit}
          data-testid='payout-account-edit-btn'
          className='shrink-0'
        >
          <Pencil className='h-3.5 w-3.5' />
          {configured
            ? t('Edit', { defaultValue: '修改' })
            : t('Set Payout Account', { defaultValue: '设置收款账户' })}
        </Button>
      </div>
    </section>
  )
}
