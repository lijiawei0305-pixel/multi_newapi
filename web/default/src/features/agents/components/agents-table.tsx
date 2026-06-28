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
import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { getAdminAgents } from '../api'
import { useAgentsColumns } from './agents-columns'
import { useAgents } from './agents-provider'

export function AgentsTable() {
  const { t } = useTranslation()
  const columns = useAgentsColumns()
  const { refreshTrigger } = useAgents()

  const { data, isLoading } = useQuery({
    queryKey: ['admin-agents', refreshTrigger],
    queryFn: async () => {
      const result = await getAdminAgents()
      return result.data || []
    },
    placeholderData: (prev) => prev,
  })

  const agents = useMemo(() => data || [], [data])

  const { table } = useDataTable({
    data: agents,
    columns,
    withFilteredRowModel: false,
    withFacetedRowModel: false,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      emptyTitle={t('No agents yet')}
      emptyDescription={t('Click "Create Agent" to add your first sub-agent')}
      skeletonKeyPrefix='agents-skeleton'
      applyHeaderSize
    />
  )
}
