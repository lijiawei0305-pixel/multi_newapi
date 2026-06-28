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
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cny, formatDateTime, sourceTypeLabel } from '../lib'
import type { Earning } from '../types'

interface Props {
  items: Earning[]
  loading?: boolean
}

export function EarningsTable({ items, loading }: Props) {
  const { t } = useTranslation()

  return (
    <Table>
      <TableHeader>
        <TableRow>
          <TableHead>{t('Source')}</TableHead>
          <TableHead>{t('Amount (¥)')}</TableHead>
          <TableHead>{t('Reference')}</TableHead>
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
              {t('No earnings yet')}
            </TableCell>
          </TableRow>
        ) : (
          items.map((row, i) => (
            <TableRow key={`${row.source_type}-${row.created_at}-${i}`}>
              <TableCell>{sourceTypeLabel(row.source_type, t)}</TableCell>
              <TableCell className='font-semibold text-emerald-600 tabular-nums'>
                {cny(row.amount_cny)}
              </TableCell>
              <TableCell className='text-muted-foreground font-mono text-sm'>
                {row.reference || '-'}
              </TableCell>
              <TableCell className='text-muted-foreground text-sm'>
                {formatDateTime(row.created_at)}
              </TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}
