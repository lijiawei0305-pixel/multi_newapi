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
import { useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { DataTablePage, useDataTable } from '@/components/data-table'
import { Label } from '@/components/ui/label'
import {
  NativeSelect,
  NativeSelectOption,
} from '@/components/ui/native-select'
import { getAdminSubscriptions } from '../api'
import type { AlertLevel } from '../types'
import { useSubscriptionMonitorColumns } from './subscription-monitor-columns'

type AlertFilter = 'all' | AlertLevel

export function SubscriptionMonitorTable() {
  const { t } = useTranslation()
  const columns = useSubscriptionMonitorColumns()
  const [alertFilter, setAlertFilter] = useState<AlertFilter>('all')

  const { data, isLoading } = useQuery({
    queryKey: ['admin-subscriptions'],
    queryFn: async () => {
      const res = await getAdminSubscriptions()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = useMemo(() => {
    const list = data || []
    if (alertFilter === 'all') return list
    return list.filter((r) => r.alert_level === alertFilter)
  }, [data, alertFilter])

  const { table } = useDataTable({
    data: rows,
    columns,
    withFilteredRowModel: false,
    withFacetedRowModel: false,
  })

  const toolbar = (
    <div className='flex items-center gap-2'>
      <Label htmlFor='alert-filter' className='text-muted-foreground text-sm'>
        {t('Alert')}
      </Label>
      <NativeSelect
        id='alert-filter'
        data-testid='alert-filter'
        value={alertFilter}
        onChange={(e) => setAlertFilter(e.target.value as AlertFilter)}
      >
        <NativeSelectOption value='all'>{t('All')}</NativeSelectOption>
        <NativeSelectOption value='warn'>{t('Warning')}</NativeSelectOption>
        <NativeSelectOption value='critical'>
          {t('Critical')}
        </NativeSelectOption>
        <NativeSelectOption value='exhausted'>
          {t('Exhausted')}
        </NativeSelectOption>
      </NativeSelect>
    </div>
  )

  return (
    <DataTablePage
      table={table}
      columns={columns}
      isLoading={isLoading}
      toolbar={toolbar}
      emptyTitle={t('No subscriptions')}
      emptyDescription={t('No subscriptions match the current filter')}
      skeletonKeyPrefix='subscription-monitor-skeleton'
      applyHeaderSize
    />
  )
}
