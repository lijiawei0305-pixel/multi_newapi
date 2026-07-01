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
import { type ReactNode } from 'react'
import type { TFunction } from 'i18next'
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
import { cn } from '@/lib/utils'
import {
  cny,
  count,
  formatDateTime,
  sourceTypeLabel,
  usd,
  withdrawalStatusMeta,
} from '../lib'
import type {
  ConsumptionDetailItem,
  DetailItem,
  EarningsDetailItem,
  Lens,
  RechargeDetailItem,
  WithdrawalsDetailItem,
} from '../types'
import { ReportPagination } from './report-pagination'

// ============================================================================
// Paginated per-lens detail table. The `items` union is discriminated by the
// active `lens` (contract §1.4 / §1.7) to pick the column set. `showTenant`
// prepends the cross-tenant agent column for ADMIN scope and is omitted for the
// single-tenant agent page, keeping this component PROP-DRIVEN + scope-agnostic.
// ============================================================================

export interface DetailTableProps {
  lens: Lens
  items: DetailItem[]
  loading?: boolean
  /** Admin scope renders the tenant/agent column; agent scope hides it. */
  showTenant?: boolean
  page: number
  pageSize: number
  total: number
  onPageChange: (page: number) => void
}

interface ColumnDef {
  header: string
  className?: string
  cell: (row: DetailItem) => ReactNode
}

function tenantCell(row: DetailItem): ReactNode {
  const r = row as { tenant_id?: number; agent_name?: string }
  if (r.agent_name) return r.agent_name
  return r.tenant_id != null ? `#${r.tenant_id}` : '-'
}

function columnsForLens(
  lens: Lens,
  showTenant: boolean,
  t: TFunction
): ColumnDef[] {
  const tenantCol: ColumnDef = {
    header: t('Agent'),
    className: 'font-medium',
    cell: tenantCell,
  }

  let cols: ColumnDef[]
  switch (lens) {
    case 'withdrawals':
      cols = [
        {
          header: t('ID'),
          className: 'text-muted-foreground tabular-nums',
          cell: (row) => (row as WithdrawalsDetailItem).id,
        },
        {
          header: t('Amount (¥)'),
          className: 'font-semibold tabular-nums',
          cell: (row) => cny((row as WithdrawalsDetailItem).amount_cny),
        },
        {
          header: t('Status'),
          cell: (row) => {
            const meta = withdrawalStatusMeta(
              (row as WithdrawalsDetailItem).status,
              t
            )
            return (
              <StatusBadge
                label={meta.label}
                variant={meta.variant}
                pulse={meta.pulse}
                copyable={false}
              />
            )
          },
        },
        {
          header: t('Created At'),
          className: 'text-muted-foreground text-sm',
          cell: (row) =>
            formatDateTime((row as WithdrawalsDetailItem).created_at),
        },
        {
          header: t('Reviewed At'),
          className: 'text-muted-foreground text-sm',
          cell: (row) =>
            formatDateTime((row as WithdrawalsDetailItem).reviewed_at),
        },
      ]
      break
    case 'recharge':
      cols = [
        {
          header: t('Order No'),
          className: 'font-mono text-xs',
          cell: (row) => (row as RechargeDetailItem).order_no,
        },
        {
          header: t('Type'),
          cell: (row) =>
            (row as RechargeDetailItem).kind === 'subscription'
              ? t('Subscription')
              : t('Recharge'),
        },
        {
          header: t('Provider'),
          cell: (row) => (row as RechargeDetailItem).provider || '-',
        },
        {
          header: t('Amount ($)'),
          className: 'tabular-nums',
          cell: (row) => usd((row as RechargeDetailItem).amount_usd),
        },
        {
          header: t('Paid (¥)'),
          className: 'font-semibold tabular-nums',
          cell: (row) => cny((row as RechargeDetailItem).actual_paid_cny),
        },
        {
          header: t('Cost (¥)'),
          className: 'tabular-nums',
          cell: (row) => cny((row as RechargeDetailItem).agent_cost_price_cny),
        },
        {
          header: t('Status'),
          className: 'text-muted-foreground text-sm',
          cell: (row) => (row as RechargeDetailItem).status || '-',
        },
        {
          header: t('Created At'),
          className: 'text-muted-foreground text-sm',
          cell: (row) => formatDateTime((row as RechargeDetailItem).created_at),
        },
      ]
      break
    case 'consumption':
      cols = [
        {
          header: t('Model'),
          className: 'font-mono text-xs',
          cell: (row) => (row as ConsumptionDetailItem).model_name || '-',
        },
        {
          header: t('Calls'),
          className: 'tabular-nums',
          cell: (row) => count((row as ConsumptionDetailItem).calls),
        },
        {
          header: t('Tokens'),
          className: 'tabular-nums',
          cell: (row) => count((row as ConsumptionDetailItem).tokens),
        },
        {
          header: t('Quota'),
          className: 'tabular-nums',
          cell: (row) => count((row as ConsumptionDetailItem).used_quota),
        },
        {
          header: t('Cost (¥)'),
          className: 'font-semibold tabular-nums',
          cell: (row) => cny((row as ConsumptionDetailItem).used_cost_cny),
        },
      ]
      break
    case 'earnings':
    default:
      cols = [
        {
          header: t('Source'),
          cell: (row) =>
            sourceTypeLabel((row as EarningsDetailItem).source_type, t),
        },
        {
          header: t('Amount (¥)'),
          className: 'font-semibold tabular-nums',
          cell: (row) => cny((row as EarningsDetailItem).amount_cny),
        },
        {
          header: t('Reference'),
          className: 'text-muted-foreground font-mono text-sm',
          cell: (row) => (row as EarningsDetailItem).reference || '-',
        },
        {
          header: t('Created At'),
          className: 'text-muted-foreground text-sm',
          cell: (row) => formatDateTime((row as EarningsDetailItem).created_at),
        },
      ]
      break
  }

  return showTenant ? [tenantCol, ...cols] : cols
}

export function DetailTable({
  lens,
  items,
  loading,
  showTenant = false,
  page,
  pageSize,
  total,
  onPageChange,
}: DetailTableProps) {
  const { t } = useTranslation()
  const columns = columnsForLens(lens, showTenant, t)

  return (
    <div className='overflow-hidden rounded-lg border' data-testid='detail-table'>
      <Table>
        <TableHeader>
          <TableRow>
            {columns.map((col) => (
              <TableHead key={col.header}>{col.header}</TableHead>
            ))}
          </TableRow>
        </TableHeader>
        <TableBody>
          {loading ? (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className='text-muted-foreground text-center'
              >
                {t('Loading...')}
              </TableCell>
            </TableRow>
          ) : items.length === 0 ? (
            <TableRow>
              <TableCell
                colSpan={columns.length}
                className='text-muted-foreground text-center'
              >
                {t('No data available')}
              </TableCell>
            </TableRow>
          ) : (
            items.map((row, i) => (
              <TableRow key={i} data-testid={`detail-row-${i}`}>
                {columns.map((col) => (
                  <TableCell key={col.header} className={cn(col.className)}>
                    {col.cell(row)}
                  </TableCell>
                ))}
              </TableRow>
            ))
          )}
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
