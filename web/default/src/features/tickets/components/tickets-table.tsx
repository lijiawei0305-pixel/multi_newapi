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
import { Button } from '@/components/ui/button'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime } from '@/lib/agent-format'
import type { Ticket } from '../types'
import { TicketPriorityBadge } from './ticket-priority-badge'
import { TicketStatusBadge } from './ticket-status-badge'

// ============================================================================
// Shared, prop-driven ticket list table. Column set flexes by audience:
//   `showTenant` — admin cross-tenant tenant column (0 = platform ticket)
//   `showUser`   — agent/admin submitter column (users only see their own)
// Rows open the detail view via `onOpen`. Loading / empty states are rendered
// inline to match the other list features (moderation, my-users, finance).
// ============================================================================

export function TicketsTable(props: {
  rows: Ticket[]
  loading?: boolean
  showTenant?: boolean
  showUser?: boolean
  onOpen: (id: number) => void
}) {
  const { t } = useTranslation()

  const leadingCols = (props.showTenant ? 1 : 0) + (props.showUser ? 1 : 0)
  const colSpan = 7 + leadingCols

  return (
    <div className='w-full overflow-x-auto rounded-lg border' data-testid='tickets-table'>
      <Table>
        <TableHeader>
          <TableRow>
            <TableHead>{t('ID')}</TableHead>
            {props.showTenant && <TableHead>{t('Tenant')}</TableHead>}
            <TableHead className='min-w-48'>{t('Title')}</TableHead>
            {props.showUser && <TableHead>{t('User')}</TableHead>}
            <TableHead>{t('Status')}</TableHead>
            <TableHead>{t('Priority')}</TableHead>
            <TableHead className='text-right'>{t('Messages')}</TableHead>
            <TableHead>{t('Last Reply')}</TableHead>
            <TableHead>{t('Created At')}</TableHead>
          </TableRow>
        </TableHeader>
        <TableBody>
          {props.loading && (
            <TableRow>
              <TableCell colSpan={colSpan} className='text-muted-foreground text-center'>
                {t('Loading...')}
              </TableCell>
            </TableRow>
          )}
          {!props.loading && props.rows.length === 0 && (
            <TableRow>
              <TableCell colSpan={colSpan} className='text-muted-foreground text-center'>
                {t('No tickets yet')}
              </TableCell>
            </TableRow>
          )}
          {!props.loading &&
            props.rows.length > 0 &&
            props.rows.map((row) => (
              <TableRow
                key={row.id}
                className='cursor-pointer'
                onClick={() => props.onOpen(row.id)}
                data-testid={`ticket-row-${row.id}`}
              >
                <TableCell className='tabular-nums'>{row.id}</TableCell>
                {props.showTenant && (
                  <TableCell className='tabular-nums'>
                    {row.tenant_id === 0 ? t('Platform') : row.tenant_id}
                  </TableCell>
                )}
                <TableCell className='max-w-xs'>
                  <Button
                    variant='link'
                    className='h-auto max-w-full justify-start truncate p-0 font-medium'
                    onClick={(e) => {
                      e.stopPropagation()
                      props.onOpen(row.id)
                    }}
                  >
                    <span className='truncate'>{row.title || t('(untitled)')}</span>
                  </Button>
                </TableCell>
                {props.showUser && (
                  <TableCell>
                    {row.username
                      ? `${row.username} (${row.user_id})`
                      : row.user_id}
                  </TableCell>
                )}
                <TableCell>
                  <TicketStatusBadge status={row.status} />
                </TableCell>
                <TableCell>
                  <TicketPriorityBadge priority={row.priority} />
                </TableCell>
                <TableCell className='text-right tabular-nums'>
                  {row.message_count}
                </TableCell>
                <TableCell className='text-muted-foreground text-sm'>
                  {row.last_reply_at ? fmtDateTime(row.last_reply_at) : '-'}
                </TableCell>
                <TableCell className='text-muted-foreground text-sm'>
                  {fmtDateTime(row.created_at)}
                </TableCell>
              </TableRow>
            ))}
        </TableBody>
      </Table>
    </div>
  )
}
