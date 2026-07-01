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

// ============================================================================
// Support tickets (doc/api-contract.md §2.12). One feature, three audiences:
//   🅤 user  — /api/tenant/tickets          (own tickets, session user_id)
//   🅖 agent — /api/tenant/agent/tickets     (own tenant, AgentOwnerAuth)
//   🅐 admin — /api/admin/tickets            (cross-tenant, AdminAuth)
// Isolation is enforced SERVER-SIDE; the client never sends a scope key.
// Times are ISO-8601 UTC strings (empty string when zero).
// ============================================================================

/** Ticket lifecycle: open(待客服) → pending(待用户) → resolved → closed. */
export type TicketStatus = 'open' | 'pending' | 'resolved' | 'closed'

/** Ticket urgency. Defaults to `normal` server-side. */
export type TicketPriority = 'low' | 'normal' | 'high' | 'urgent'

/** Who wrote a message / last touched a ticket. */
export type AuthorRole = 'user' | 'agent' | 'admin'

/** The three front-end audiences, each mapped to a distinct API surface. */
export type TicketAudience = 'user' | 'agent' | 'admin'

export interface Ticket {
  id: number
  tenant_id: number
  user_id: number
  username: string
  title: string
  status: TicketStatus
  priority: TicketPriority
  message_count: number
  last_reply_at: string
  /** '' when there has been no reply yet. */
  last_reply_role: AuthorRole | ''
  created_at: string
  updated_at: string
}

export interface TicketMessage {
  id: number
  ticket_id: number
  user_id: number
  username: string
  author_role: AuthorRole
  content: string
  created_at: string
}

/** Detail response shape: `{ ticket, messages[] }` (messages ascending). */
export interface TicketDetail {
  ticket: Ticket
  messages: TicketMessage[]
}

/** Nested pagination envelope carried inside `data`. */
export interface PagedTickets {
  items: Ticket[]
  total: number
  page: number
  page_size: number
}

/**
 * Common list query. `user_id` is only honoured for agent/admin, `tenant_id`
 * only for admin; the backend ignores any scope key it does not own.
 */
export interface TicketListQuery {
  page?: number
  page_size?: number
  status?: TicketStatus
  priority?: TicketPriority
  keyword?: string
  user_id?: number
  tenant_id?: number
}

/** POST /api/tenant/tickets body. */
export interface CreateTicketPayload {
  title: string
  content: string
  priority?: TicketPriority
}

/** Result of a status mutation (`{ id, status }`). */
export interface TicketStatusResult {
  id: number
  status: TicketStatus
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
