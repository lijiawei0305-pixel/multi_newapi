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
import type { TFunction } from 'i18next'
import type { StatusVariant } from '@/components/status-badge'
import { getApiErrorCode } from '@/lib/api'
import type { AuthorRole, TicketPriority, TicketStatus } from './types'

// ============================================================================
// Presentation helpers for support tickets: status / priority / author-role
// labels + badge variants, select option lists, and TICKET_* error-code → i18n
// message mapping. All user-facing text goes through `t()`.
// ============================================================================

/** Status → { i18n label, badge variant }. Unknown values fall back to neutral. */
export function statusMeta(
  status: TicketStatus | string,
  t: TFunction
): { label: string; variant: StatusVariant } {
  switch (status) {
    case 'open':
      return { label: t('Open'), variant: 'info' }
    case 'pending':
      return { label: t('Pending'), variant: 'warning' }
    case 'resolved':
      return { label: t('Resolved'), variant: 'success' }
    case 'closed':
      return { label: t('Closed'), variant: 'neutral' }
    default:
      return { label: String(status || '-'), variant: 'neutral' }
  }
}

/** Priority → { i18n label, badge variant }. */
export function priorityMeta(
  priority: TicketPriority | string,
  t: TFunction
): { label: string; variant: StatusVariant } {
  switch (priority) {
    case 'low':
      return { label: t('Low'), variant: 'neutral' }
    case 'normal':
      return { label: t('Normal'), variant: 'info' }
    case 'high':
      return { label: t('High'), variant: 'warning' }
    case 'urgent':
      return { label: t('Urgent'), variant: 'danger' }
    default:
      return { label: String(priority || '-'), variant: 'neutral' }
  }
}

/** Author role → localized label for the message thread. */
export function authorRoleLabel(role: AuthorRole | string, t: TFunction): string {
  switch (role) {
    case 'user':
      return t('User')
    case 'agent':
      return t('Agent')
    case 'admin':
      return t('Admin')
    default:
      return String(role || '-')
  }
}

/** Status transition targets offered on the staff (agent/admin) status select. */
export const STATUS_VALUES: TicketStatus[] = [
  'open',
  'pending',
  'resolved',
  'closed',
]

/** Priority options offered on the create form + filters. */
export const PRIORITY_VALUES: TicketPriority[] = [
  'low',
  'normal',
  'high',
  'urgent',
]

/**
 * Map a rejected ticket request to a localized toast message via its stable
 * `code`. Falls back to a generic message when the code is unknown (callers
 * pass `skipErrorHandler` so the global interceptor does not also toast).
 */
export function ticketErrorMessage(err: unknown, t: TFunction): string {
  const code = getApiErrorCode(err)
  switch (code) {
    case 'TICKET_NOT_FOUND':
      return t('Ticket not found')
    case 'TICKET_INPUT_INVALID':
      return t('Please provide a title and message')
    case 'TICKET_PRIORITY_INVALID':
      return t('Invalid priority')
    case 'TICKET_STATUS_INVALID':
      return t('Invalid status change')
    case 'TICKET_CLOSED':
      return t('This ticket is closed and cannot be replied to')
    case 'TICKET_REPLY_EMPTY':
      return t('Reply cannot be empty')
    case 'AGENT_FORBIDDEN':
      return t('You do not have access to this ticket')
    default:
      return t('Request failed')
  }
}
