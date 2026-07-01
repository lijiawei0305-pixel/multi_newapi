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
import { useState } from 'react'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import { SectionPageLayout } from '@/components/layout'
import type { TicketApiAdapter } from '../audience'
import { ticketKeys } from '../query-keys'
import type { CreateTicketPayload, TicketListQuery } from '../types'
import { CreateTicketDialog } from './create-ticket-dialog'
import { TicketFilterBar, type TicketFilterValues } from './ticket-filter-bar'
import { TicketsPagination } from './tickets-pagination'
import { TicketsTable } from './tickets-table'

const PAGE_SIZE = 20

// ============================================================================
// Shared list page for all three audiences. The `adapter` decides capabilities:
//   - `create` present → renders the user "New Ticket" dialog + action.
//   - `showUserFilter` / `showTenantColumn` toggle the extra filter/column.
// Filters commit into the react-query key on apply; page resets to 1 on change.
// ============================================================================

export function TicketListView(props: {
  adapter: TicketApiAdapter
  title: string
  onOpen: (id: number) => void
  onCreated?: (id: number) => void
}) {
  const queryClient = useQueryClient()
  const [filters, setFilters] = useState<TicketFilterValues>({})
  const [page, setPage] = useState(1)

  const params: TicketListQuery = {
    ...filters,
    page,
    page_size: PAGE_SIZE,
  }

  const { data, isLoading } = useQuery({
    queryKey: ticketKeys.list(props.adapter.audience, params),
    queryFn: async () => (await props.adapter.list(params)).data,
    placeholderData: (prev) => prev,
  })

  const rows = data?.items ?? []
  const total = data?.total ?? 0

  const handleApply = (next: TicketFilterValues) => {
    setFilters(next)
    setPage(1)
  }

  const handleCreate = async (
    payload: CreateTicketPayload
  ): Promise<number | null> => {
    if (!props.adapter.create) return null
    const res = await props.adapter.create(payload)
    if (res.success && res.data) {
      queryClient.invalidateQueries({
        queryKey: ticketKeys.lists(props.adapter.audience),
      })
      props.onCreated?.(res.data.id)
      return res.data.id
    }
    return null
  }

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{props.title}</SectionPageLayout.Title>
      {props.adapter.create && (
        <SectionPageLayout.Actions>
          <CreateTicketDialog onCreate={handleCreate} />
        </SectionPageLayout.Actions>
      )}
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='tickets-list-page'>
          <TicketFilterBar
            showUserFilter={props.adapter.showUserFilter}
            showTenantFilter={props.adapter.showTenantColumn}
            onApply={handleApply}
          />

          <TicketsTable
            rows={rows}
            loading={isLoading}
            showTenant={props.adapter.showTenantColumn}
            showUser={props.adapter.showUserFilter}
            onOpen={props.onOpen}
          />

          <TicketsPagination
            page={page}
            pageSize={data?.page_size ?? PAGE_SIZE}
            total={total}
            onPageChange={setPage}
          />
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
