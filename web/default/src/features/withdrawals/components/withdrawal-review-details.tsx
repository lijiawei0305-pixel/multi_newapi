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
import { useTranslation } from 'react-i18next'

import { cny, formatDateTime, payoutMethodLabel } from '../lib'
import type { Withdrawal } from '../types'

type WithdrawalReviewDetailsProps = {
  withdrawal: Withdrawal
}

export function WithdrawalReviewDetails(props: WithdrawalReviewDetailsProps) {
  const { t } = useTranslation()
  const withdrawal = props.withdrawal

  return (
    <dl
      aria-label={t('Details')}
      className='bg-muted/50 ring-foreground/10 grid grid-cols-[minmax(0,6.5rem)_minmax(0,1fr)] gap-x-3 gap-y-2 rounded-lg p-3 text-sm ring-1'
      data-testid='withdrawal-review-details'
    >
      <dt className='text-muted-foreground'>{t('ID')}</dt>
      <dd
        className='text-end font-mono font-medium tabular-nums'
        data-testid='withdrawal-review-id'
      >
        #{withdrawal.id}
      </dd>

      <dt className='text-muted-foreground'>{t('Created At')}</dt>
      <dd
        className='text-end tabular-nums'
        data-testid='withdrawal-review-created-at'
      >
        {formatDateTime(withdrawal.created_at)}
      </dd>

      <dt className='text-muted-foreground'>{t('Reviewed At')}</dt>
      <dd
        className='text-end tabular-nums'
        data-testid='withdrawal-review-reviewed-at'
      >
        {withdrawal.reviewed_at ? formatDateTime(withdrawal.reviewed_at) : '-'}
      </dd>

      <dt className='text-muted-foreground'>{t('Amount (¥)')}</dt>
      <dd
        className='text-end font-semibold tabular-nums'
        data-testid='withdrawal-review-amount'
      >
        {cny(withdrawal.amount_cny)}
      </dd>

      <dt className='text-muted-foreground'>{t('Payout Method')}</dt>
      <dd className='text-end' data-testid='withdrawal-review-method'>
        {payoutMethodLabel(withdrawal.payout_method, t)}
      </dd>

      <dt className='text-muted-foreground'>{t('Payout Account')}</dt>
      <dd
        className='min-w-0 text-end font-mono text-xs font-medium break-all select-all'
        data-testid='withdrawal-review-account'
      >
        {withdrawal.payout_account || '-'}
      </dd>

      <dt className='text-muted-foreground'>{t('Real Name')}</dt>
      <dd
        className='min-w-0 text-end break-words'
        data-testid='withdrawal-review-name'
      >
        {withdrawal.payout_name || '-'}
      </dd>

      <dt className='text-muted-foreground'>{t('Bank Name')}</dt>
      <dd
        className='min-w-0 text-end break-words'
        data-testid='withdrawal-review-bank'
      >
        {withdrawal.payout_bank || '-'}
      </dd>
    </dl>
  )
}
