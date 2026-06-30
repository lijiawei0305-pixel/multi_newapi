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
import { useQuery } from '@tanstack/react-query'
import { useTranslation } from 'react-i18next'
import { SectionPageLayout } from '@/components/layout'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'
import { fmtDateTime } from '@/lib/agent-format'
import type { ApiResponse, ViolationEvent, ViolationQuery } from './types'

/** Shared read-only violation-log table; admin & agent inject their own `listFn`. */
export function ViolationsView({
  listFn,
  title,
  queryKey,
  showTenant,
}: {
  listFn: (params: ViolationQuery) => Promise<ApiResponse<ViolationEvent[]>>
  title: string
  queryKey: string
  showTenant?: boolean
}) {
  const { t } = useTranslation()
  const [userId, setUserId] = useState('')
  const [applied, setApplied] = useState<{ user_id?: number }>({})

  const { data, isLoading } = useQuery({
    queryKey: [queryKey, applied],
    queryFn: async () => (await listFn({ ...applied, limit: 200 })).data || [],
    placeholderData: (prev) => prev,
  })
  const rows = data || []

  return (
    <SectionPageLayout>
      <SectionPageLayout.Title>{title}</SectionPageLayout.Title>
      <SectionPageLayout.Actions>
        <div className='flex items-center gap-2'>
          <Input
            placeholder={t('User ID')}
            value={userId}
            onChange={(e) => setUserId(e.target.value)}
            className='w-32'
            data-testid='violation-userid'
          />
          <Button
            variant='outline'
            onClick={() =>
              setApplied({ user_id: userId ? Number(userId) : undefined })
            }
            data-testid='violation-filter'
          >
            {t('Filter')}
          </Button>
        </div>
      </SectionPageLayout.Actions>
      <SectionPageLayout.Content>
        <div className='overflow-hidden rounded-lg border' data-testid='violations-table'>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>{t('ID')}</TableHead>
                {showTenant && <TableHead>{t('Tenant')}</TableHead>}
                <TableHead>{t('User')}</TableHead>
                <TableHead>{t('Model')}</TableHead>
                <TableHead>{t('Matched Words')}</TableHead>
                <TableHead>{t('Excerpt')}</TableHead>
                <TableHead>{t('Action')}</TableHead>
                <TableHead>{t('Created At')}</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={showTenant ? 8 : 7} className='text-muted-foreground text-center'>
                    {t('Loading...')}
                  </TableCell>
                </TableRow>
              ) : rows.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={showTenant ? 8 : 7} className='text-muted-foreground text-center'>
                    {t('No violations yet')}
                  </TableCell>
                </TableRow>
              ) : (
                rows.map((row: ViolationEvent) => (
                  <TableRow key={row.id} data-testid={`violation-row-${row.id}`}>
                    <TableCell className='tabular-nums'>{row.id}</TableCell>
                    {showTenant && (
                      <TableCell className='tabular-nums'>{row.tenant_id ?? 0}</TableCell>
                    )}
                    <TableCell>
                      {row.username
                        ? `${row.username} (${row.user_id})`
                        : row.user_id}
                    </TableCell>
                    <TableCell>{row.model || '-'}</TableCell>
                    <TableCell>
                      <div className='flex flex-wrap gap-1'>
                        {(row.matched_words || []).map((w, i) => (
                          <Badge key={i} variant='secondary'>
                            {w}
                          </Badge>
                        ))}
                      </div>
                    </TableCell>
                    <TableCell className='text-muted-foreground max-w-xs truncate text-sm'>
                      {row.excerpt}
                    </TableCell>
                    <TableCell>
                      <Badge variant={row.action_taken === 'block' ? 'destructive' : 'outline'}>
                        {row.action_taken === 'block' ? t('Block') : t('Remind')}
                      </Badge>
                    </TableCell>
                    <TableCell className='text-muted-foreground text-sm'>
                      {fmtDateTime(row.created_at)}
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </div>
      </SectionPageLayout.Content>
    </SectionPageLayout>
  )
}
