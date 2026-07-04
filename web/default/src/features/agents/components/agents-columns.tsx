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
import { useMemo } from 'react'
import { type ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import type { Agent } from '../types'
import { agentStatusMeta, agentLevelLabel, cny, num } from '../lib'
import { DataTableRowActions } from './data-table-row-actions'

export function useAgentsColumns(): ColumnDef<Agent>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<Agent>[] => [
      {
        accessorFn: (row) => row.id,
        id: 'id',
        header: t('ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span data-testid={`agent-row-${row.original.id}`}>
            <TableId value={row.original.id} />
          </span>
        ),
        size: 60,
      },
      {
        accessorFn: (row) => row.owner_username,
        id: 'owner_username',
        header: t('Owner'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <span className='font-medium'>{row.original.owner_username}</span>
        ),
        size: 140,
      },
      {
        accessorFn: (row) => row.name,
        id: 'name',
        header: t('Agent Name'),
        cell: ({ row }) => (
          <span className='truncate'>{row.original.name}</span>
        ),
        size: 160,
      },
      {
        accessorFn: (row) => row.level,
        id: 'level',
        header: t('Level'),
        meta: { mobileBadge: true },
        cell: ({ row }) => (
          <StatusBadge
            label={agentLevelLabel(row.original.level, t)}
            variant={row.original.level >= 1 ? 'info' : 'neutral'}
            copyable={false}
          />
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.cost_price_cny,
        id: 'cost_price_cny',
        header: t('Cost Price (¥)'),
        cell: ({ row }) => (
          <span className='tabular-nums'>{cny(row.original.cost_price_cny)}</span>
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.package_discount,
        id: 'package_discount',
        header: t('Package Discount'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground tabular-nums'>
            {num(row.original.package_discount)}
          </span>
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.commission_ratio,
        id: 'commission_ratio',
        header: t('Commission Ratio'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground tabular-nums'>
            {num(row.original.commission_ratio)}
          </span>
        ),
        size: 110,
      },
      {
        accessorFn: (row) => row.discount_ratio,
        id: 'discount_ratio',
        header: t('Agent Discount Ratio', { defaultValue: '折扣系数' }),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='tabular-nums'>
            {row.original.discount_ratio
              ? num(row.original.discount_ratio)
              : '—'}
          </span>
        ),
        size: 100,
      },
      {
        accessorFn: (row) => row.status,
        id: 'status',
        header: t('Status'),
        meta: { mobileBadge: true },
        cell: ({ row }) => {
          const meta = agentStatusMeta(row.original.status, t)
          return (
            <StatusBadge
              label={meta.label}
              variant={meta.variant}
              copyable={false}
              className='-ml-1.5'
            />
          )
        },
        size: 90,
      },
      {
        accessorFn: (row) => row.withdrawable_cny,
        id: 'withdrawable_cny',
        header: t('Withdrawable (¥)'),
        cell: ({ row }) => (
          <span className='font-semibold text-emerald-600 tabular-nums'>
            {cny(row.original.withdrawable_cny)}
          </span>
        ),
        size: 120,
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
