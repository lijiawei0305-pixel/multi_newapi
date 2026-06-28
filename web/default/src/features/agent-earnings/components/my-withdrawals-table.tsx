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
import { cny, formatDateTime, withdrawalStatusMeta } from '../lib'
import type { MyWithdrawal } from '../types'

interface Props {
  items: MyWithdrawal[]
  loading?: boolean
}

export function MyWithdrawalsTable({ items, loading }: Props) {
  const { t } = useTranslation()

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('ID')}</TableHead>
          <TableHead>{t('Amount (¥)')}</TableHead>
          <TableHead>{t('Status')}</TableHead>
          <TableHead>{t('Created At')}</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {loading ? (
          <TableRow>
            <TableCell colSpan={4} className='text-muted-foreground text-center'>
              {t('Loading...')}
            </TableCell>
          </TableRow>
        ) : items.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4} className='text-muted-foreground text-center'>
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
                <TableCell>
                  <StatusBadge
                    label={meta.label}
                    variant={meta.variant}
                    pulse={meta.pulse}
                    copyable={false}
                  />
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
