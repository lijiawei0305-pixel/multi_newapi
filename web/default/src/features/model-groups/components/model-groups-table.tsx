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
import { getModelGroups } from '../api'
import { useModelGroupsColumns } from './model-groups-columns'
import { useModelGroups } from './model-groups-provider'

export function ModelGroupsTable() {
  const { t } = useTranslation()
  const columns = useModelGroupsColumns()
  const { refreshTrigger } = useModelGroups()

  const { data, isLoading } = useQuery({
    queryKey: ['admin-model-groups', refreshTrigger],
    queryFn: async () => {
      const result = await getModelGroups()
      return result.data || []
    },
    placeholderData: (prev) => prev,
  })

  const groups = useMemo(() => data || [], [data])

  const { table } = useDataTable({
    data: groups,
    columns,
    withFilteredRowModel: false,
    withFacetedRowModel: false,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      emptyTitle={t('No model groups yet')}
      emptyDescription={t(
        'Click "Create Model Group" to add your first model group'
      )}
      skeletonKeyPrefix='model-groups-skeleton'
      applyHeaderSize
    />
  )
}
