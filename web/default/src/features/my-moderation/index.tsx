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

import { Badge } from '@/components/ui/badge'
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table'

import type { BannedWord } from '../moderation-words/types'
import { ModerationWordsManager } from '../moderation-words/words-manager'
import { listBaseWords, tenantWordsApi } from './api'

/** Read-only view of the global base library (tenant_id=0) the agent inherits. */
function BaseLibrarySection() {
  const { t } = useTranslation()
  const { data, isLoading } = useQuery({
    queryKey: ['tenant-base-words'],
    queryFn: async () => (await listBaseWords()).data || [],
    placeholderData: (prev) => prev,
  })
  const rows = data || []
  return (
    <div className='mt-8'>
      <h3 className='text-muted-foreground mb-2 text-sm font-medium'>
        {t('Global Base Library (read-only, inherited)')}
      </h3>
      <div className='overflow-hidden rounded-lg border'>
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>{t('Word')}</TableHead>
              <TableHead>{t('Match Type')}</TableHead>
              <TableHead>{t('Action')}</TableHead>
              <TableHead>{t('Enabled')}</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading && (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className='text-muted-foreground text-center'
                >
                  {t('Loading...')}
                </TableCell>
              </TableRow>
            )}
            {!isLoading && rows.length === 0 && (
              <TableRow>
                <TableCell
                  colSpan={4}
                  className='text-muted-foreground text-center'
                >
                  {t('No banned words yet')}
                </TableCell>
              </TableRow>
            )}
            {!isLoading &&
              rows.length > 0 &&
              rows.map((row: BannedWord) => (
                <TableRow key={row.id}>
                  <TableCell className='font-medium'>{row.word}</TableCell>
                  <TableCell>
                    <Badge variant='secondary'>{row.match_type}</Badge>
                  </TableCell>
                  <TableCell>
                    <Badge
                      variant={
                        row.action === 'block' ? 'destructive' : 'outline'
                      }
                    >
                      {row.action === 'block' ? t('Block') : t('Remind')}
                    </Badge>
                  </TableCell>
                  <TableCell>
                    {row.enabled ? t('Enabled') : t('Disabled')}
                  </TableCell>
                </TableRow>
              ))}
          </TableBody>
        </Table>
      </div>
    </div>
  )
}

/** Agent self-service: own-tenant banned words (editable) + inherited base library (read-only). */
export function MyModeration() {
  const { t } = useTranslation()
  return (
    <ModerationWordsManager
      api={tenantWordsApi}
      title={t('My Banned Words')}
      queryKey='tenant-moderation-words'
      footer={<BaseLibrarySection />}
    />
  )
}
