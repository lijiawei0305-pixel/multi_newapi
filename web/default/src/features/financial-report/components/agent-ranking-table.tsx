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
import { ChevronDown, ChevronUp, ChevronsUpDown } from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { cn } from '@/lib/utils'

import { cny, count } from '../lib'
import type { AgentRankRow, AgentSortBy, SortOrder } from '../types'
import { ReportPagination } from './report-pagination'

// ============================================================================
// ADMIN-ONLY sortable per-agent / per-tenant ranking (contract §1.3). Clicking
// a numeric column header re-queries server-side via `onSortChange(sort_by,
// order)` — sorting is NOT done client-side (the table only shows the current
// page). Column `key`s are exactly the `AgentSortBy` union the backend accepts.
// ============================================================================

export interface AgentRankingTableProps {
  items: AgentRankRow[]
  loading?: boolean
  sortBy: AgentSortBy
  order: SortOrder
  onSortChange: (sortBy: AgentSortBy, order: SortOrder) => void
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}

interface RankColumn {
  /** Non-null = server-sortable; null = the static agent identity column. */
  key: AgentSortBy | null
  label: string
  cell: (row: AgentRankRow) => ReactNode
}

export function AgentRankingTable({
  items,
  loading,
  sortBy,
  order,
  onSortChange,
  page,
  pageSize,
  total,
  onPageChange,
}: AgentRankingTableProps) {
  const { t } = useTranslation()

  const columns: RankColumn[] = [
    {
      key: null,
      label: t('Agent'),
      cell: (row) => (
        <div className='flex flex-col'>
          <span className='font-medium'>
            {row.agent_name || `#${row.tenant_id}`}
          </span>
          <span className='text-muted-foreground text-xs'>
            {row.owner_username}
          </span>
        </div>
      ),
    },
    {
      key: 'total_earned_cny',
      label: t('Total Earned'),
      cell: (row) => cny(row.total_earned_cny),
    },
    {
      key: 'recharge_paid_cny',
      label: t('Recharge Paid'),
      cell: (row) => cny(row.recharge_paid_cny),
    },
    {
      key: 'subscription_paid_cny',
      label: t('Subscription Paid'),
      cell: (row) => cny(row.subscription_paid_cny),
    },
    {
      key: 'consumption_cost_cny',
      label: t('Consumption Cost'),
      cell: (row) => cny(row.consumption_cost_cny),
    },
    {
      key: 'consumption_used_quota',
      label: t('Quota'),
      cell: (row) => count(row.consumption_used_quota),
    },
    {
      key: 'withdrawn_cny',
      label: t('Withdrawn'),
      cell: (row) => cny(row.withdrawn_cny),
    },
    {
      key: 'pending_withdraw_cny',
      label: t('Pending Withdrawals'),
      cell: (row) => cny(row.pending_withdraw_cny),
    },
    {
      key: 'withdrawable_cny',
      label: t('Withdrawable'),
      cell: (row) => cny(row.withdrawable_cny),
    },
  ]

  const handleSort = (key: AgentSortBy) => {
    if (key === sortBy) {
      onSortChange(key, order === 'asc' ? 'desc' : 'asc')
    } else {
      onSortChange(key, 'desc')
    }
  }

  return (
    <div
      className='overflow-hidden rounded-lg border'
      data-testid='agent-ranking-table'
    >
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((col) => {
              const numeric = col.key !== null
              const active = col.key === sortBy
              let SortIcon = ChevronsUpDown
              if (active && order === 'asc') SortIcon = ChevronUp
              else if (active) SortIcon = ChevronDown
              return (
                <TableHead
                  key={col.label}
                  className={numeric ? 'text-right' : 'text-left'}
                >
                  {numeric ? (
                    <button
                      type='button'
                      onClick={() => handleSort(col.key as AgentSortBy)}
                      data-testid={`rank-sort-${col.key}`}
                      className='hover:text-foreground ml-auto inline-flex items-center gap-1'
                    >
                      {col.label}
                      <SortIcon
                        className={cn(
                          'size-3.5',
                          !active && 'text-muted-foreground/50'
                        )}
                      />
                    </button>
                  ) : (
                    col.label
                  )}
                </TableHead>
              )
            })}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading && (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className='text-muted-foreground text-center'
              >
                {t('Loading...')}
              </TableCell>
            </TableRow>
          )}
          {!loading && items.length === 0 && (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className='text-muted-foreground text-center'
              >
                {t('No data available')}
              </TableCell>
            </TableRow>
          )}
          {!loading &&
            items.length > 0 &&
            items.map((row) => (
              <TableRow
                key={row.tenant_id}
                data-testid={`rank-row-${row.tenant_id}`}
              >
                {columns.map((col) => {
                  const numeric = col.key !== null
                  return (
                    <TableCell
                      key={col.label}
                      className={cn(
                        numeric && 'text-right tabular-nums',
                        col.key === 'total_earned_cny' &&
                          'font-semibold text-emerald-600'
                      )}
                    >
                      {col.cell(row)}
                    </TableCell>
                  )
                })}
              </TableRow>
            ))}
        </TableBody>
      </Table>
      <ReportPagination
        page={page}
        pageSize={pageSize}
        total={total}
        onPageChange={onPageChange}
      />
    </div>
  )
}
