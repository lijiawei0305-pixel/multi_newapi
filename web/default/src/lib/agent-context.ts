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
import { queryOptions } from '@tanstack/react-query'

import { api } from '@/lib/api'

/**
 * Agent self-service identity + capability gate.
 *
 * `GET /api/tenant/agent-context` is UserAuth-only (any logged-in user may
 * call it) and returns two orthogonal signals:
 *
 * - `is_agent_owner` / `level` / `can_api` — **identity** (owner-based,
 *   Host-independent): the caller owns a tenant (`tenant.owner_user_id ===
 *   session user id`), plus that tenant's `level` (0=普通/basic, 1=独立/
 *   independent) and `can_api`. The main-site wallet L0 referral card gates
 *   on this (`is_agent_owner && level===0`) and relies on it staying true on
 *   the main site.
 * - `on_own_site` — **location**: the current Host resolves to the very
 *   tenant the caller owns. The Agent Self-Service sidebar group and agent
 *   route guards gate on `is_agent_owner && on_own_site`, so the agent
 *   console appears only on the agent's own site (subdomain / custom
 *   domain) — never on the main site or another agent's site. L0 has no
 *   site of its own → console never shows for L0 (wallet card is their UI).
 *
 * It always responds 200; no tenant / not the owner / not logged in all
 * yield the fail-closed defaults (`is_agent_owner: false, level: 0,
 * can_api: false, on_own_site: false`).
 *
 * Shared by the sidebar (React `useQuery`) and the route guards (`beforeLoad`
 * via `queryClient.fetchQuery`) so both honour one source of truth.
 */
export type AgentContext = {
  is_agent_owner: boolean
  level: number
  can_api: boolean
  on_own_site: boolean
}

type AgentContextEnvelope = {
  success?: boolean
  data?: Partial<AgentContext> | null
}

const CLOSED: AgentContext = {
  is_agent_owner: false,
  level: 0,
  can_api: false,
  on_own_site: false,
}

/**
 * Fetch the agent context for the current Host + session. Any failure
 * (network, 401, non-owner) collapses to the fail-closed defaults — the
 * agent menus stay hidden and the guarded routes redirect away.
 */
async function fetchAgentContext(): Promise<AgentContext> {
  try {
    const res = await api.get<AgentContextEnvelope>(
      '/api/tenant/agent-context',
      { skipBusinessError: true, skipErrorHandler: true }
    )
    const d = res.data?.data
    return {
      is_agent_owner: Boolean(d?.is_agent_owner),
      level: Number(d?.level ?? 0),
      can_api: Boolean(d?.can_api),
      on_own_site: Boolean(d?.on_own_site),
    }
  } catch {
    return CLOSED
  }
}

/**
 * TanStack Query options for the agent context. Usable directly with
 * `useQuery(agentContextQueryOptions)` and
 * `queryClient.fetchQuery(agentContextQueryOptions)`.
 */
export const agentContextQueryOptions = queryOptions({
  queryKey: ['agent-context'],
  queryFn: fetchAgentContext,
  // Owner identity is stable within a session; cache generously to avoid
  // re-hitting the endpoint on every sidebar render / route navigation.
  staleTime: 5 * 60 * 1000,
  retry: false,
})
