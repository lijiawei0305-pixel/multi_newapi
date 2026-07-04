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
import { StatusBadge } from '@/components/status-badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import {
  cny,
  formatDateTime,
  payoutMethodLabel,
  withdrawalStatusMeta,
} from '../lib'
import type { MyWithdrawal } from '../types'

interface Props {
  items: MyWithdrawal[]
  loading?: boolean
}

const COLUMN_COUNT = 7

export function MyWithdrawalsTable({ items, loading }: Props) {
  const { t } = useTranslation()

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('ID')}</TableHead>
          <TableHead>{t('Amount (¥)')}</TableHead>
          <TableHead>
            {t('Payout Account', { defaultValue: '收款账户' })}
          </TableHead>
          <TableHead>{t('Status')}</TableHead>
          <TableHead>
            {t('Remark / Rejection Reason', { defaultValue: '备注/驳回原因' })}
          </TableHead>
          <TableHead>
            {t('Payout Reference', { defaultValue: '打款凭证' })}
          </TableHead>
          <TableHead>{t('Created At')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableRow>
            <TableCell
              colSpan={COLUMN_COUNT}
              className='text-muted-foreground text-center'
            >
              {t('Loading...')}
            </TableCell>
          </TableRow>
        ) : items.length === 0 ? (
          <TableRow>
            <TableCell
              colSpan={COLUMN_COUNT}
              className='text-muted-foreground text-center'
            >
              {t('No withdrawals')}
            </TableCell>
          </TableRow>
        ) : (
          items.map((row) => {
            const meta = withdrawalStatusMeta(row.status, t)
            return (
              <TableRow key={row.id} data-testid={`my-wd-row-${row.id}`}>
                <TableCell className='text-muted-foreground tabular-nums'>
                  {row.id}
                </TableCell>
                <TableCell className='font-semibold tabular-nums'>
                  {cny(row.amount_cny)}
                </TableCell>
                <TableCell className='text-sm'>
                  {row.payout_account ? (
                    <span>
                      {payoutMethodLabel(row.payout_method, t)}{' '}
                      {row.payout_account}
                    </span>
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </TableCell>
                <TableCell>
                  <StatusBadge
                    label={meta.label}
                    variant={meta.variant}
                    pulse={meta.pulse}
                    copyable={false}
                  />
                </TableCell>
                <TableCell
                  className='text-muted-foreground max-w-48 truncate text-sm'
                  title={row.remark || undefined}
                >
                  {row.remark || '-'}
                </TableCell>
                <TableCell className='text-sm'>
                  {row.status === 'paid' && (row.payout_ref || row.paid_at) ? (
                    <div className='flex flex-col'>
                      {row.payout_ref && <span>{row.payout_ref}</span>}
                      {row.paid_at && (
                        <span className='text-muted-foreground text-xs'>
                          {formatDateTime(row.paid_at)}
                        </span>
                      )}
                    </div>
                  ) : (
                    <span className='text-muted-foreground'>-</span>
                  )}
                </TableCell>
                <TableCell className='text-muted-foreground text-sm'>
                  {formatDateTime(row.created_at)}
                </TableCell>
              </TableRow>
            )
          })
        )}
      </TableBody>
    </Table>
  )
}
