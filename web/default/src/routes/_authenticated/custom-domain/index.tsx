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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { CustomDomain } from '@/features/custom-domain'
import { agentContextQueryOptions } from '@/lib/agent-context'

// Agent self-service (custom domain): an independent-tier capability, so this
// is accessible only to the agent owner of the current Host's tenant AND only
// once that tenant is level>=1 (独立/independent). Non-owners and level-0
// (普通/basic) owners are redirected before the backend rejects with
// AGENT_FORBIDDEN / AGENT_LEVEL_LOCKED.
export const Route = createFileRoute('/_authenticated/custom-domain/')({
  beforeLoad: async ({ context }) => {
    const ctx = await context.queryClient.fetchQuery(agentContextQueryOptions)
    if (!ctx.is_agent_owner || !ctx.on_own_site || ctx.level < 1) {
      throw redirect({ to: '/403' })
    }
  },
  component: CustomDomain,
})
