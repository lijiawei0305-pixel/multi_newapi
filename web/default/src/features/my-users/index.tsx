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
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { StatusBadge, type StatusVariant } from '@/components/status-badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime, quotaToUsd } from '@/lib/agent-format'
import { getTenantUsers } from './api'
import type { TenantUser } from './types'

/** Tolerant user-status → badge styling (new-api: 1 enabled, 2 disabled). */
function statusMeta(
  status: number | string | undefined,
  t: (k: string) => string
): { variant: StatusVariant; label: string } {
  const s = typeof status === 'number' ? String(status) : status
  switch (s) {
    case '1':
    case 'enabled':
    case 'active':
      return { variant: 'success', label: t('Enabled') }
    case '2':
    case '3':
    case 'disabled':
    case 'banned':
      return { variant: 'danger', label: t('Disabled') }
    default:
      return { variant: 'neutral', label: s || '-' }
  }
}

export function MyUsers() {
  const { t } = useTranslation()

  const { data, isLoading } = useQuery({
    queryKey: ['tenant-users'],
    queryFn: async () => {
      const res = await getTenantUsers()
      return res.data || []
    },
    placeholderData: (prev) => prev,
  })

  const rows = data || []

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{t('My Users')}</SectionPageLayout.Title>
      <SectionPageLayout.Content>
        <div className='flex flex-col gap-4' data-testid='my-users-page'>
          <div className='overflow-hidden rounded-lg border'>
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>{t('ID')}</TableHead>
                  <TableHead>{t('Username')}</TableHead>
                  <TableHead>{t('Display Name')}</TableHead>
                  <TableHead>{t('Balance ($)')}</TableHead>
                  <TableHead>{t('Used ($)')}</TableHead>
                  <TableHead>{t('Status')}</TableHead>
                  <TableHead>{t('Created At')}</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  <TableRow>
                    <TableCell
                      colSpan={7}
                      className='text-muted-foreground text-center'
                    >
                      {t('Loading...')}
                    </TableCell>
                  </TableRow>
                ) : rows.length === 0 ? (
                  <TableRow>
                    <TableCell
                      colSpan={7}
                      className='text-muted-foreground text-center'
                    >
                      {t('No users yet')}
                    </TableCell>
                  </TableRow>
                ) : (
                  rows.map((row: TenantUser) => {
                    const meta = statusMeta(row.status, t)
                    return (
                      <TableRow key={row.id} data-testid={`user-row-${row.id}`}>
                        <TableCell className='tabular-nums'>{row.id}</TableCell>
                        <TableCell className='font-medium'>
                          {row.username}
                        </TableCell>
                        <TableCell>{row.display_name || '-'}</TableCell>
                        <TableCell className='tabular-nums'>
                          {quotaToUsd(row.quota)}
                        </TableCell>
                        <TableCell className='tabular-nums'>
                          {quotaToUsd(row.used_quota)}
                        </TableCell>
                        <TableCell>
                          <StatusBadge
                            variant={meta.variant}
                            label={meta.label}
                            copyable={false}
                          />
                        </TableCell>
                        <TableCell className='text-muted-foreground text-sm'>
                          {fmtDateTime(row.created_at)}
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </div>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
