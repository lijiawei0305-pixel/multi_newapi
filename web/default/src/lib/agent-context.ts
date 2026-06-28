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
 * Agent self-service identity gate.
 *
 * `GET /api/tenant/agent-context` is UserAuth-only (any logged-in user may
 * call it) and returns whether the *current* user owns the *current Host's*
 * tenant — the authoritative `tenant.owner_user_id === session user id` check,
 * read straight from the DB on the backend. It always responds 200; no
 * tenant / not the owner / not logged in all yield `is_agent_owner === false`.
 *
 * Shared by the sidebar (React `useQuery`) and the route guards (`beforeLoad`
 * via `queryClient.fetchQuery`) so both honour one source of truth.
 */
export type AgentContext = {
  is_agent_owner: boolean
}

type AgentContextEnvelope = {
  success?: boolean
  data?: Partial<AgentContext> | null
}

/**
 * Fetch the agent-owner flag for the current Host + session. Any failure
 * (network, 401, non-owner) collapses to `false` so the gate fails closed —
 * the agent menus stay hidden and the guarded routes redirect away.
 */
async function fetchAgentContext(): Promise<boolean> {
  try {
    const res = await api.get<AgentContextEnvelope>(
      '/api/tenant/agent-context',
      { skipBusinessError: true, skipErrorHandler: true }
    )
    return Boolean(res.data?.data?.is_agent_owner)
  } catch {
    return false
  }
}

/**
 * TanStack Query options for the agent-owner flag. Usable directly with
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
