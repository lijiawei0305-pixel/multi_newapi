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
import { useQuery } from '@tanstack/react-query'
import type { PaginationState } from '@tanstack/react-table'
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { DataTablePage, useDataTable } from '@/components/data-table'
import { useAuthStore } from '@/stores/auth-store'

import { getAdminWithdrawals } from '../api'
import { useWithdrawalsColumns } from './withdrawals-columns'

export function WithdrawalsTable() {
  const { t } = useTranslation()
  const userId = useAuthStore((state) => state.auth.user?.id ?? null)
  const columns = useWithdrawalsColumns()
  const [pagination, setPagination] = useState<PaginationState>({
    pageIndex: 0,
    pageSize: 20,
  })

  const { data, isLoading } = useQuery({
    queryKey: [
      'admin-withdrawals',
      userId,
      pagination.pageIndex,
      pagination.pageSize,
    ],
    queryFn: async () => {
      const result = await getAdminWithdrawals(
        pagination.pageIndex + 1,
        pagination.pageSize
      )
      return result.data
    },
    enabled: userId !== null,
  })

  const rows = useMemo(() => data?.items || [], [data])

  const { table } = useDataTable({
    data: rows,
    columns,
    totalCount: data?.total || 0,
    pagination,
    onPaginationChange: setPagination,
    manualPagination: true,
    withFilteredRowModel: false,
    withFacetedRowModel: false,
  })

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      emptyTitle={t('No withdrawals')}
      emptyDescription={t('No withdrawal requests to review')}
      skeletonKeyPrefix='withdrawals-skeleton'
      applyHeaderSize
    />
  )
}
