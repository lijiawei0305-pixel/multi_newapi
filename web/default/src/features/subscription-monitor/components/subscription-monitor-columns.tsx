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

import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { cn } from '@/lib/utils'

import {
  alertLevelMeta,
  clampPct,
  formatDate,
  usageBarColor,
  usd,
} from '../lib'
import type { MonitorSubscription } from '../types'

export function useSubscriptionMonitorColumns(): ColumnDef<MonitorSubscription>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<MonitorSubscription>[] => [
      {
        accessorFn: (row) => row.tenant_name || row.tenant_id,
        id: 'tenant',
        header: t('Tenant'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground'>
            {row.original.tenant_name || `#${row.original.tenant_id}`}
          </span>
        ),
        size: 120,
      },
      {
        accessorFn: (row) => row.user_id,
        id: 'user_id',
        header: t('User ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span data-testid={`sub-row-${row.original.user_id}`}>
            <TableId value={row.original.user_id} />
          </span>
        ),
        size: 80,
      },
      {
        accessorFn: (row) => row.username,
        id: 'username',
        header: t('Username'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <span className='font-medium'>{row.original.username}</span>
        ),
        size: 140,
      },
      {
        accessorFn: (row) => row.plan_code,
        id: 'plan_code',
        header: t('Plan Code'),
        cell: ({ row }) => (
          <span className='font-mono text-sm'>{row.original.plan_code}</span>
        ),
        size: 120,
      },
      {
        accessorFn: (row) => row.status,
        id: 'status',
        header: t('Status'),
        meta: { mobileBadge: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground'>{row.original.status}</span>
        ),
        size: 100,
      },
      {
        accessorFn: (row) => row.usage_pct,
        id: 'usage',
        header: t('Usage'),
        cell: ({ row }) => {
          const sub = row.original
          const pct = clampPct(sub.usage_pct)
          return (
            <div className='flex min-w-[140px] flex-col gap-1'>
              <div className='flex items-center justify-between text-xs'>
                <span className='text-muted-foreground'>
                  {usd(sub.used_usd)} / {usd(sub.limit_usd)}
                </span>
                <span className='tabular-nums'>{pct.toFixed(0)}%</span>
              </div>
              <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
                <div
                  className={cn('h-full rounded-full', usageBarColor(pct))}
                  style={{ width: `${pct}%` }}
                />
              </div>
            </div>
          )
        },
        size: 180,
      },
      {
        accessorFn: (row) => row.alert_level,
        id: 'alert_level',
        header: t('Alert'),
        meta: { mobileBadge: true },
        cell: ({ row }) => {
          const meta = alertLevelMeta(row.original.alert_level, t)
          return (
            <StatusBadge
              label={meta.label}
              variant={meta.variant}
              copyable={false}
            />
          )
        },
        size: 100,
      },
      {
        accessorFn: (row) => row.period_end,
        id: 'period_end',
        header: t('Period End'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground text-sm'>
            {formatDate(row.original.period_end)}
          </span>
        ),
        size: 120,
      },
    ],
    [t]
  )
}
