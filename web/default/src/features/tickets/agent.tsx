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
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'

import { agentTicketAdapter } from './audience'
import { TicketDetailView } from './components/ticket-detail-view'
import { TicketListView } from './components/ticket-list-view'

// 🅖 Agent surface: tickets belonging to the current Host's tenant. Access is
// gated in the route `beforeLoad` (agentContextQueryOptions → /403); the
// backend re-scopes every response to the authoritative agentTenantID.

export function AgentTickets() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const open = (id: number) =>
    navigate({
      to: '/agent-tickets/$ticketId',
      params: { ticketId: String(id) },
    })
  return (
    <TicketListView
      adapter={agentTicketAdapter}
      title={t('Support Tickets')}
      onOpen={open}
    />
  )
}

export function AgentTicketDetail(props: { id: number }) {
  const navigate = useNavigate()
  return (
    <TicketDetailView
      adapter={agentTicketAdapter}
      id={props.id}
      onBack={() => navigate({ to: '/agent-tickets' })}
    />
  )
}
