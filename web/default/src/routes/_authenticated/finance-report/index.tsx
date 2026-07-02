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
import { agentContextQueryOptions } from '@/lib/agent-context'
import { AgentFinancialReport } from '@/features/financial-report/agent'

// Agent self-service financial report: accessible only to the agent owner of
// the current Host's tenant. Non-owners (normal users, or agents on the main
// site / another agent's site) are redirected before the tenant finance
// endpoints reject with AGENT_FORBIDDEN. Mirrors the agent-earnings gate.
export const Route = createFileRoute('/_authenticated/finance-report/')({
  beforeLoad: async ({ context }) => {
    const ctx = await context.queryClient.fetchQuery(agentContextQueryOptions)
    if (!ctx.is_agent_owner) {
      throw redirect({ to: '/403' })
    }
  },
  component: AgentFinancialReport,
})
