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
import type { TicketAudience, TicketListQuery } from './types'

// ============================================================================
// React Query key factory for support tickets. Keys are namespaced by audience
// so a mutation on one surface only invalidates that surface's caches.
// ============================================================================

export const ticketKeys = {
  all: ['tickets'] as const,
  audience: (audience: TicketAudience) => ['tickets', audience] as const,
  lists: (audience: TicketAudience) => ['tickets', audience, 'list'] as const,
  list: (audience: TicketAudience, params: TicketListQuery) =>
    ['tickets', audience, 'list', params] as const,
  details: (audience: TicketAudience) =>
    ['tickets', audience, 'detail'] as const,
  detail: (audience: TicketAudience, id: number) =>
    ['tickets', audience, 'detail', id] as const,
}
