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
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { ArrowLeft } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { SectionPageLayout } from '@/components/layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { fmtDateTime } from '@/lib/agent-format'

import type { TicketApiAdapter } from '../audience'
import { STATUS_VALUES, statusMeta, ticketErrorMessage } from '../lib'
import { ticketKeys } from '../query-keys'
import type { TicketStatus } from '../types'
import { TicketPriorityBadge } from './ticket-priority-badge'
import { TicketReplyBox } from './ticket-reply-box'
import { TicketStatusBadge } from './ticket-status-badge'
import { TicketThread } from './ticket-thread'

// ============================================================================
// Shared detail page for all three audiences (prop-driven by `adapter`):
//   - user  → Close action; reply reopens server-side; no status select.
//   - staff → status select (incl. reopening closed); reply sets pending.
// Isolation is enforced server-side; a cross-scope id collapses to
// TICKET_NOT_FOUND, which renders the friendly not-found panel below.
// ============================================================================

/** Staff-only status machine control (agent/admin). */
function StatusControl(props: {
  value: TicketStatus
  onChange: (status: TicketStatus) => void
}) {
  const { t } = useTranslation()
  return (
    <Select
      value={props.value}
      onValueChange={(v) => props.onChange(v as TicketStatus)}
    >
      <SelectTrigger className='w-40' data-testid='ticket-status-control'>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {STATUS_VALUES.map((s) => (
          <SelectItem key={s} value={s}>
            {statusMeta(s, t).label}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  )
}

export function TicketDetailView(props: {
  adapter: TicketApiAdapter
  id: number
  onBack: () => void
}) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()

  const { data, isLoading, isError } = useQuery({
    queryKey: ticketKeys.detail(props.adapter.audience, props.id),
    queryFn: async () => (await props.adapter.get(props.id)).data,
    retry: false,
  })

  const invalidate = () => {
    queryClient.invalidateQueries({
      queryKey: ticketKeys.detail(props.adapter.audience, props.id),
    })
    queryClient.invalidateQueries({
      queryKey: ticketKeys.lists(props.adapter.audience),
    })
  }

  const handleReply = async (content: string): Promise<boolean> => {
    try {
      const res = await props.adapter.reply(props.id, content)
      if (res.success) {
        toast.success(t('Reply sent'))
        invalidate()
        return true
      }
      return false
    } catch (err) {
      toast.error(ticketErrorMessage(err, t))
      return false
    }
  }

  const handleStatus = async (status: TicketStatus) => {
    if (!props.adapter.setStatus) return
    try {
      const res = await props.adapter.setStatus(props.id, status)
      if (res.success) {
        toast.success(t('Status updated'))
        invalidate()
      }
    } catch (err) {
      toast.error(ticketErrorMessage(err, t))
    }
  }

  const handleClose = async () => {
    if (!props.adapter.close) return
    try {
      const res = await props.adapter.close(props.id)
      if (res.success) {
        toast.success(t('Ticket closed'))
        invalidate()
      }
    } catch (err) {
      toast.error(ticketErrorMessage(err, t))
    }
  }

  const backButton = (
    <Button
      variant='ghost'
      size='sm'
      className='mb-3 -ml-2'
      onClick={props.onBack}
      data-testid='ticket-detail-back'
    >
      <ArrowLeft className='mr-1 size-4' />
      {t('Back to tickets')}
    </Button>
  )

  if (isLoading) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Ticket')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          {backButton}
          <p className='text-muted-foreground text-center text-sm'>
            {t('Loading...')}
          </p>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  if (isError || !data) {
    return (
      <SectionPageLayout>
        <SectionPageLayout.Title>{t('Ticket')}</SectionPageLayout.Title>
        <SectionPageLayout.Content>
          {backButton}
          <Alert variant='destructive' data-testid='ticket-not-found'>
            <AlertDescription>{t('Ticket not found')}</AlertDescription>
          </Alert>
        </SectionPageLayout.Content>
      </SectionPageLayout>
    )
  }

  const ticket = data.ticket
  const messages = data.messages ?? []
  const isClosed = ticket.status === 'closed'

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{ticket.title}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        {props.adapter.setStatus && (
          <StatusControl value={ticket.status} onChange={handleStatus} />
        )}
        {props.adapter.close && !isClosed && (
          <Button
            variant='outline'
            onClick={handleClose}
            data-testid='ticket-close'
          >
            {t('Close Ticket')}
          </Button>
        )}
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='ticket-detail-page'>
          {backButton}

          <div className='flex flex-wrap items-center gap-x-4 gap-y-2 text-sm'>
            <span className='text-muted-foreground tabular-nums'>
              #{ticket.id}
            </span>
            <TicketStatusBadge status={ticket.status} />
            <TicketPriorityBadge priority={ticket.priority} />
            {props.adapter.showTenantColumn && (
              <span className='text-muted-foreground'>
                {t('Tenant')}:{' '}
                {ticket.tenant_id === 0
                  ? t('Platform')
                  : `#${ticket.tenant_id}`}
              </span>
            )}
            {props.adapter.showUserFilter && (
              <span className='text-muted-foreground'>
                {t('User')}: {ticket.username || `#${ticket.user_id}`}
              </span>
            )}
            <span className='text-muted-foreground'>
              {t('Created At')}: {fmtDateTime(ticket.created_at)}
            </span>
          </div>

          <div className='rounded-lg border p-3 sm:p-4'>
            <TicketThread messages={messages} />
          </div>

          {isClosed ? (
            <Alert data-testid='ticket-closed-note'>
              <AlertDescription>
                {t('This ticket is closed.')}
                {props.adapter.setStatus
                  ? ` ${t('Set the status to Open to reopen it.')}`
                  : ''}
              </AlertDescription>
            </Alert>
          ) : (
            <TicketReplyBox onSubmit={handleReply} />
          )}
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
