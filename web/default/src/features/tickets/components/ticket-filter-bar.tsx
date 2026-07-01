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
import { useTranslation } from 'react-i18next'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { PRIORITY_VALUES, STATUS_VALUES, priorityMeta, statusMeta } from '../lib'
import type { TicketListQuery, TicketPriority, TicketStatus } from '../types'

// ============================================================================
// Filter bar shared by all three audiences. `status` / `priority` / `keyword`
// are universal; `user_id` is exposed for agent+admin, `tenant_id` for admin
// only. A local draft is committed to the parent on "Filter" (or Enter) so the
// list query key only changes on an explicit apply.
// ============================================================================

const ALL = 'all'

export type TicketFilterValues = Pick<
  TicketListQuery,
  'status' | 'priority' | 'keyword' | 'user_id' | 'tenant_id'
>

export function TicketFilterBar(props: {
  showUserFilter: boolean
  showTenantFilter: boolean
  onApply: (values: TicketFilterValues) => void
}) {
  const { t } = useTranslation()
  const [status, setStatus] = useState<string>(ALL)
  const [priority, setPriority] = useState<string>(ALL)
  const [keyword, setKeyword] = useState('')
  const [userId, setUserId] = useState('')
  const [tenantId, setTenantId] = useState('')

  const apply = () => {
    props.onApply({
      status: status === ALL ? undefined : (status as TicketStatus),
      priority: priority === ALL ? undefined : (priority as TicketPriority),
      keyword: keyword.trim() || undefined,
      user_id:
        props.showUserFilter && userId.trim() ? Number(userId) : undefined,
      tenant_id:
        props.showTenantFilter && tenantId.trim() ? Number(tenantId) : undefined,
    })
  }

  return (
    <div
      className='flex flex-wrap items-center gap-2'
      data-testid='ticket-filter-bar'
    >
      <Select value={status} onValueChange={setStatus}>
        <SelectTrigger className='w-36' data-testid='ticket-filter-status'>
          <SelectValue placeholder={t('Status')} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{t('All Statuses')}</SelectItem>
          {STATUS_VALUES.map((s) => (
            <SelectItem key={s} value={s}>
              {statusMeta(s, t).label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      <Select value={priority} onValueChange={setPriority}>
        <SelectTrigger className='w-36' data-testid='ticket-filter-priority'>
          <SelectValue placeholder={t('Priority')} />
        </SelectTrigger>
        <SelectContent>
          <SelectItem value={ALL}>{t('All Priorities')}</SelectItem>
          {PRIORITY_VALUES.map((p) => (
            <SelectItem key={p} value={p}>
              {priorityMeta(p, t).label}
            </SelectItem>
          ))}
        </SelectContent>
      </Select>

      {props.showUserFilter && (
        <Input
          placeholder={t('User ID')}
          value={userId}
          inputMode='numeric'
          onChange={(e) => setUserId(e.target.value.replace(/\D/g, ''))}
          onKeyDown={(e) => e.key === 'Enter' && apply()}
          className='w-28'
          data-testid='ticket-filter-userid'
        />
      )}

      {props.showTenantFilter && (
        <Input
          placeholder={t('Tenant ID')}
          value={tenantId}
          inputMode='numeric'
          onChange={(e) => setTenantId(e.target.value.replace(/\D/g, ''))}
          onKeyDown={(e) => e.key === 'Enter' && apply()}
          className='w-28'
          data-testid='ticket-filter-tenantid'
        />
      )}

      <Input
        placeholder={t('Search title')}
        value={keyword}
        onChange={(e) => setKeyword(e.target.value)}
        onKeyDown={(e) => e.key === 'Enter' && apply()}
        className='w-full sm:w-56'
        data-testid='ticket-filter-keyword'
      />

      <Button variant='outline' onClick={apply} data-testid='ticket-filter-apply'>
        {t('Filter')}
      </Button>
    </div>
  )
}
