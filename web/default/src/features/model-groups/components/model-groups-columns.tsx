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
import type { ColumnDef } from '@tanstack/react-table'
import { useTranslation } from 'react-i18next'
import { BadgeListCell } from '@/components/data-table'
import { StatusBadge } from '@/components/status-badge'
import { TableId } from '@/components/table-id'
import { ratioText } from '../lib'
import type { ModelGroup } from '../types'
import { DataTableRowActions } from './data-table-row-actions'
import { EnabledSwitchCell } from './enabled-switch-cell'

export function useModelGroupsColumns(): ColumnDef<ModelGroup>[] {
  const { t } = useTranslation()

  return useMemo(
    (): ColumnDef<ModelGroup>[] => [
      {
        accessorFn: (row) => row.id,
        id: 'id',
        header: t('ID'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span data-testid={`model-group-row-${row.original.id}`}>
            <TableId value={row.original.id} />
          </span>
        ),
        size: 60,
      },
      {
        accessorFn: (row) => row.name,
        id: 'name',
        header: t('Group Name'),
        meta: { mobileTitle: true },
        cell: ({ row }) => (
          <span className='font-medium'>{row.original.name}</span>
        ),
        size: 160,
      },
      {
        accessorFn: (row) => row.ratio,
        id: 'ratio',
        header: t('Ratio'),
        cell: ({ row }) => (
          <span className='tabular-nums'>×{ratioText(row.original.ratio)}</span>
        ),
        size: 90,
      },
      {
        // Read-only «Serving Channels»: all channels whose group field contains
        // this group name (channel ↔ group is many-to-one). Binding is set on the
        // channel side; this column is derived and never editable here.
        accessorFn: (row) => row.serving_channels?.length ?? 0,
        id: 'serving_channels',
        header: t('Serving Channels'),
        enableSorting: false,
        cell: ({ row }) => (
          <BadgeListCell
            items={(row.original.serving_channels ?? []).map((c) => (
              <StatusBadge
                key={c.id}
                label={c.name}
                autoColor={c.name}
                size='sm'
              />
            ))}
          />
        ),
        size: 200,
      },
      {
        accessorFn: (row) => row.description,
        id: 'description',
        header: t('Description'),
        meta: { mobileHidden: true },
        cell: ({ row }) => (
          <span className='text-muted-foreground truncate'>
            {row.original.description || '-'}
          </span>
        ),
        size: 220,
      },
      {
        accessorFn: (row) => row.enabled,
        id: 'enabled',
        header: t('Enabled'),
        meta: { mobileBadge: true },
        cell: ({ row }) => <EnabledSwitchCell row={row} />,
        size: 90,
      },
      {
        id: 'actions',
        header: () => t('Actions'),
        cell: ({ row }) => <DataTableRowActions row={row} />,
        meta: { pinned: 'right' as const },
      },
    ],
    [t]
  )
}
