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
import {
  closeUserTicket,
  createUserTicket,
  getAdminTicket,
  getAgentTicket,
  getUserTicket,
  listAdminTickets,
  listAgentTickets,
  listUserTickets,
  replyAdminTicket,
  replyAgentTicket,
  replyUserTicket,
  setAdminTicketStatus,
  setAgentTicketStatus,
} from './api'
import type {
  ApiResponse,
  CreateTicketPayload,
  PagedTickets,
  Ticket,
  TicketAudience,
  TicketDetail,
  TicketListQuery,
  TicketMessage,
  TicketStatus,
  TicketStatusResult,
} from './types'

// ============================================================================
// Audience adapter — collapses the three per-surface API modules into one
// capability-flagged object so the shared list / detail / thread components
// stay route- and scope-agnostic. Optional members (`create`, `close`,
// `setStatus`) express what each audience is allowed to do:
//   🅤 user  → create + close (no status select; reply reopens server-side)
//   🅖 agent → setStatus + user filter
//   🅐 admin → setStatus + user filter + tenant column/filter
// ============================================================================

export interface TicketApiAdapter {
  audience: TicketAudience
  list: (params: TicketListQuery) => Promise<ApiResponse<PagedTickets>>
  get: (id: number) => Promise<ApiResponse<TicketDetail>>
  reply: (id: number, content: string) => Promise<ApiResponse<TicketMessage>>
  /** User only — open a new ticket. */
  create?: (payload: CreateTicketPayload) => Promise<ApiResponse<Ticket>>
  /** User only — close one's own ticket. */
  close?: (id: number) => Promise<ApiResponse<TicketStatusResult>>
  /** Agent / admin — drive the status machine (incl. reopening closed). */
  setStatus?: (
    id: number,
    status: TicketStatus
  ) => Promise<ApiResponse<TicketStatusResult>>
  /** Agent / admin see a user-id filter; users only ever see their own. */
  showUserFilter: boolean
  /** Admin sees the cross-tenant tenant column + filter. */
  showTenantColumn: boolean
}

export const userTicketAdapter: TicketApiAdapter = {
  audience: 'user',
  list: listUserTickets,
  get: getUserTicket,
  reply: replyUserTicket,
  create: createUserTicket,
  close: closeUserTicket,
  showUserFilter: false,
  showTenantColumn: false,
}

export const agentTicketAdapter: TicketApiAdapter = {
  audience: 'agent',
  list: listAgentTickets,
  get: getAgentTicket,
  reply: replyAgentTicket,
  setStatus: setAgentTicketStatus,
  showUserFilter: true,
  showTenantColumn: false,
}

export const adminTicketAdapter: TicketApiAdapter = {
  audience: 'admin',
  list: listAdminTickets,
  get: getAdminTicket,
  reply: replyAdminTicket,
  setStatus: setAdminTicketStatus,
  showUserFilter: true,
  showTenantColumn: true,
}
