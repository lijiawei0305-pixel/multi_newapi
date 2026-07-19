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
import type { ColumnDef } from '@tanstack/react-table'
import { useMemo } from 'react'
import { useTranslation } from 'react-i18next'

import { BadgeCell } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'

import type { AdminTokenPlan } from '../types'
import { DataTableRowActions } from './data-table-row-actions'

const cny = (v: number | undefined) => `¥${Number(v || 0).toFixed(2)}`

export function useTokenPlansColumns(): ColumnDef<AdminTokenPlan>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<AdminTokenPlan>[] => [
      {
        accessorFn: (row) => row.id,
        id: 'id',
        header: t('ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => <TableId value={row.original.id} />,
        size: 60,
      },
      {
        accessorFn: (row) => row.code,
        id: 'code',
        header: t('Plan Code'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <span
            className='font-mono text-sm'
            data-testid={`tp-row-${row.original.code}`}
          >
            {row.original.code}
          </span>
        ),
        size: 120,
      },
      {
        accessorFn: (row) => row.name,
        id: 'name',
        header: t('Plan Name'),
        cell: ({ row }) => {
          const plan = row.original
          return (
            <div className='flex min-w-0 items-center gap-2'>
              <span className='truncate font-medium'>{plan.name}</span>
              {plan.is_recommended && (
                <StatusBadge
                  label={t('Recommended')}
                  variant='warning'
                  copyable={false}
                />
              )}
            </div>
          )
        },
        size: 200,
      },
      {
        accessorFn: (row) => row.base_price_cny,
        id: 'base_price_cny',
        header: t('Selling Price (¥)'),
        cell: ({ row }) => (
          <span className='font-semibold text-emerald-600'>
            {cny(row.original.base_price_cny)}
          </span>
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.anchor_price_cny,
        id: 'anchor_price_cny',
        header: t('Anchor Price (¥)'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground line-through'>
            {cny(row.original.anchor_price_cny)}
          </span>
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.month_limit_usd,
        id: 'month_limit_usd',
        header: t('Monthly Limit (USD)'),
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {cny(row.original.month_limit_usd)}
          </span>
        ),
        size: 130,
      },
      {
        accessorFn: (row) => row.sort,
        id: 'sort',
        header: t('Priority'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground'>{row.original.sort}</span>
        ),
        size: 90,
      },
      {
        accessorFn: (row) => row.status,
        id: 'status',
        header: t('Status'),
        meta: { mobileBadge: true },
        cell: ({ row }) =>
          row.original.status !== 'disabled' ? (
            <StatusBadge
              label={t('Enable')}
              variant='success'
              copyable={false}
              className='-ml-1.5'
            />
          ) : (
            <StatusBadge
              label={t('Disable')}
              variant='neutral'
              copyable={false}
              className='-ml-1.5'
            />
          ),
        size: 90,
      },
      {
        id: 'discount',
        header: t('Discount Label'),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const label = row.original.discount_label
          if (!label) return <span className='text-muted-foreground'>—</span>
          return (
            <BadgeCell>
              <StatusBadge label={label} variant='neutral' copyable={false} />
            </BadgeCell>
          )
        },
        size: 110,
      },
      {
        id: 'actions',
        header: () => t('Actions'),
        cell: ({ row }) => <DataTableRowActions row={row} />,
        meta: { pinned: 'right' as const },
      },
    ],
    [t]
  )
}
