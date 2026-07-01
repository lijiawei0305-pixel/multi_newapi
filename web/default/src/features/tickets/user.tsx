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
import { userTicketAdapter } from './audience'
import { TicketDetailView } from './components/ticket-detail-view'
import { TicketListView } from './components/ticket-list-view'

// 🅤 User surface: a logged-in user's own tickets (session user_id, scoped
// server-side). No route guard beyond `_authenticated`.

export function UserTickets() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const open = (id: number) =>
    navigate({ to: '/tickets/$ticketId', params: { ticketId: String(id) } })
  return (
    <TicketListView
      adapter={userTicketAdapter}
      title={t('Support Tickets')}
      onOpen={open}
      onCreated={open}
    />
  )
}

export function UserTicketDetail(props: { id: number }) {
  const navigate = useNavigate()
  return (
    <TicketDetailView
      adapter={userTicketAdapter}
      id={props.id}
      onBack={() => navigate({ to: '/tickets' })}
    />
  )
}
