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
import { Banknote, Check, X } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { Button } from '@/components/ui/button'
import type { Withdrawal } from '../types'
import {
  cny,
  formatDateTime,
  isApproved,
  isPending,
  payoutMethodLabel,
  withdrawalStatusMeta,
} from '../lib'
import { useWithdrawals } from './withdrawals-provider'

function RowActions({ row }: { row: Withdrawal }) {
  const { t } = useTranslation()
  const { openAction } = useWithdrawals()

  if (isPending(row.status)) {
    return (
      <div className='flex items-center gap-2'>
        <Button
          size='sm'
          variant='outline'
          className='h-7 text-success hover:text-success'
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

export function useWithdrawalsColumns(): ColumnDef<Withdrawal>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<Withdrawal>[] => [
      {
        accessorFn: (row) => row.id,
        id: 'id',
        header: t('ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span data-testid={`wd-row-${row.original.id}`}>
            <TableId value={row.original.id} />
          </span>
        ),
        size: 60,
      },
      {
        accessorFn: (row) => row.agent_name,
        id: 'agent_name',
        header: t('Agent'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <span className='font-medium'>{row.original.agent_name}</span>
        ),
        size: 160,
      },
      {
        accessorFn: (row) => row.amount_cny,
        id: 'amount_cny',
        header: t('Amount (¥)'),
        cell: ({ row }) => (
          <span className='font-semibold text-emerald-600 tabular-nums'>
            {cny(row.original.amount_cny)}
          </span>
        ),
        size: 120,
      },
      {
        accessorFn: (row) => row.payout_account,
        id: 'payout_account',
        header: t('Payout Account', { defaultValue: '收款账户' }),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const w = row.original
          if (!w.payout_account) {
            return <span className='text-muted-foreground'>-</span>
          }
          return (
            <div className='flex flex-col text-xs'>
              <span className='font-medium'>
                {payoutMethodLabel(w.payout_method, t)} · {w.payout_account}
              </span>
              {(w.payout_name || w.payout_bank) && (
                <span className='text-muted-foreground'>
                  {w.payout_name}
                  {w.payout_bank ? ` · ${w.payout_bank}` : ''}
                </span>
              )}
            </div>
          )
        },
        size: 180,
      },
      {
        accessorFn: (row) => row.status,
        id: 'status',
        header: t('Status'),
        meta: { mobileBadge: true },
        cell: ({ row }) => {
          const meta = withdrawalStatusMeta(row.original.status, t)
          return (
            <StatusBadge
              label={meta.label}
              variant={meta.variant}
              pulse={meta.pulse}
              copyable={false}
              className='-ml-1.5'
            />
          )
        },
        size: 110,
      },
      {
        accessorFn: (row) => row.remark,
        id: 'remark',
        header: t('Remark / Rejection Reason', {
          defaultValue: '备注/驳回原因',
        }),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span
            className='text-muted-foreground block max-w-48 truncate text-xs'
            title={row.original.remark || undefined}
          >
            {row.original.remark || '-'}
          </span>
        ),
        size: 160,
      },
      {
        accessorFn: (row) => row.payout_ref,
        id: 'payout_ref',
        header: t('Payout Reference', { defaultValue: '打款凭证' }),
        meta: { mobileHidden: true },
        cell: ({ row }) => {
          const w = row.original
          if (!w.payout_ref && !w.paid_at) {
            return <span className='text-muted-foreground'>-</span>
          }
          return (
            <div className='flex flex-col text-xs'>
              {w.payout_ref && <span>{w.payout_ref}</span>}
              {w.paid_at && (
                <span className='text-muted-foreground'>
                  {formatDateTime(w.paid_at)}
                </span>
              )}
            </div>
          )
        },
        size: 160,
      },
      {
        accessorFn: (row) => row.created_at,
        id: 'created_at',
        header: t('Created At'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground text-sm'>
            {formatDateTime(row.original.created_at)}
          </span>
        ),
        size: 150,
      },
      {
        id: 'actions',
        header: () => t('Actions'),
        cell: ({ row }) => <RowActions row={row.original} />,
        meta: { pinned: 'right' as const },
        size: 180,
      },
    ],
    [t]
  )
}
