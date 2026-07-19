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
import { api } from '@/lib/api'

import type {
  ApiResponse,
  CreateTicketPayload,
  PagedTickets,
  Ticket,
  TicketDetail,
  TicketListQuery,
  TicketMessage,
  TicketStatus,
  TicketStatusResult,
} from './types'

// ============================================================================
// Support-ticket API client. Every call rides the shared `api` axios instance
// (session cookie + injected `New-Api-User` header); the backend scopes every
// response to the caller, so NO scope key (user_id / tenant_id) is ever sent
// for the user / agent surfaces. Mutations use `skipErrorHandler` so callers
// can map the stable TICKET_* `code` to a localized toast instead of the raw
// backend message shown by the global interceptor.
// ============================================================================

// ---------------------------------------------------------------------------
// 🅤 User — /api/tenant/tickets (UserAuth, user_id = session)
// ---------------------------------------------------------------------------

export async function listUserTickets(
  params: TicketListQuery
): Promise<ApiResponse<PagedTickets>> {
  const res = await api.get('/api/tenant/tickets', { params })
  return res.data
}

export async function getUserTicket(
  id: number
): Promise<ApiResponse<TicketDetail>> {
  const res = await api.get(`/api/tenant/tickets/${id}`, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function createUserTicket(
  payload: CreateTicketPayload
): Promise<ApiResponse<Ticket>> {
  const res = await api.post('/api/tenant/tickets', payload, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function replyUserTicket(
  id: number,
  content: string
): Promise<ApiResponse<TicketMessage>> {
  const res = await api.post(
    `/api/tenant/tickets/${id}/replies`,
    { content },
    { skipErrorHandler: true }
  )
  return res.data
}

export async function closeUserTicket(
  id: number
): Promise<ApiResponse<TicketStatusResult>> {
  const res = await api.post(`/api/tenant/tickets/${id}/close`, undefined, {
    skipErrorHandler: true,
  })
  return res.data
}

// ---------------------------------------------------------------------------
// 🅖 Agent — /api/tenant/agent/tickets (UserAuth + AgentOwnerAuth)
// Distinct sub-prefix avoids gin collision with the user `/tickets/:id` route.
// ---------------------------------------------------------------------------

export async function listAgentTickets(
  params: TicketListQuery
): Promise<ApiResponse<PagedTickets>> {
  const res = await api.get('/api/tenant/agent/tickets', { params })
  return res.data
}

export async function getAgentTicket(
  id: number
): Promise<ApiResponse<TicketDetail>> {
  const res = await api.get(`/api/tenant/agent/tickets/${id}`, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function replyAgentTicket(
  id: number,
  content: string
): Promise<ApiResponse<TicketMessage>> {
  const res = await api.post(
    `/api/tenant/agent/tickets/${id}/replies`,
    { content },
    { skipErrorHandler: true }
  )
  return res.data
}

export async function setAgentTicketStatus(
  id: number,
  status: TicketStatus
): Promise<ApiResponse<TicketStatusResult>> {
  const res = await api.post(
    `/api/tenant/agent/tickets/${id}/status`,
    { status },
    { skipErrorHandler: true }
  )
  return res.data
}

// ---------------------------------------------------------------------------
// 🅐 Admin — /api/admin/tickets (AdminAuth, cross-tenant, no TenantMiddleware)
// ---------------------------------------------------------------------------

export async function listAdminTickets(
  params: TicketListQuery
): Promise<ApiResponse<PagedTickets>> {
  const res = await api.get('/api/admin/tickets', { params })
  return res.data
}

export async function getAdminTicket(
  id: number
): Promise<ApiResponse<TicketDetail>> {
  const res = await api.get(`/api/admin/tickets/${id}`, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function replyAdminTicket(
  id: number,
  content: string
): Promise<ApiResponse<TicketMessage>> {
  const res = await api.post(
    `/api/admin/tickets/${id}/replies`,
    { content },
    { skipErrorHandler: true }
  )
  return res.data
}

export async function setAdminTicketStatus(
  id: number,
  status: TicketStatus
): Promise<ApiResponse<TicketStatusResult>> {
  const res = await api.post(
    `/api/admin/tickets/${id}/status`,
    { status },
    { skipErrorHandler: true }
  )
  return res.data
}
