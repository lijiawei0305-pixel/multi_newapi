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
import { useTranslation } from 'react-i18next'
import { Badge } from '@/components/ui/badge'
import { fmtDateTime } from '@/lib/agent-format'
import { cn } from '@/lib/utils'
import { authorRoleLabel } from '../lib'
import type { TicketMessage } from '../types'

// ============================================================================
// Message thread. Customer messages (`author_role === 'user'`) align left;
// staff messages (agent/admin) align right, so every audience reads the same
// conversation orientation. Content is plain text rendered with preserved
// whitespace (no HTML) — React's default escaping guards against injection.
// ============================================================================

export function TicketThread(props: { messages: TicketMessage[] }) {
  const { t } = useTranslation()

  if (props.messages.length === 0) {
    return (
      <p className='text-muted-foreground text-center text-sm'>
        {t('No messages yet')}
      </p>
    )
  }

  return (
    <div className='flex flex-col gap-3' data-testid='ticket-thread'>
      {props.messages.map((m) => {
        const isStaff = m.author_role === 'agent' || m.author_role === 'admin'
        return (
          <div
            key={m.id}
            className={cn('flex', isStaff ? 'justify-end' : 'justify-start')}
            data-testid={`ticket-message-${m.id}`}
          >
            <div
              className={cn(
                'max-w-[85%] rounded-lg border px-3 py-2 sm:max-w-[75%]',
                isStaff ? 'bg-muted' : 'bg-card'
              )}
            >
              <div className='mb-1 flex flex-wrap items-center gap-2'>
                <span className='text-sm font-medium'>
                  {m.username || `#${m.user_id}`}
                </span>
                <Badge variant={isStaff ? 'default' : 'secondary'}>
                  {authorRoleLabel(m.author_role, t)}
                </Badge>
                <span className='text-muted-foreground text-xs'>
                  {fmtDateTime(m.created_at)}
                </span>
              </div>
              <p className='text-sm whitespace-pre-wrap break-words'>
                {m.content}
              </p>
            </div>
          </div>
        )
      })}
    </div>
  )
}
