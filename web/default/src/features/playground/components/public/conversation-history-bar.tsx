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
import { History, Plus, Trash2 } from 'lucide-react'
import { useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Button } from '@/components/ui/button'
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuTrigger,
} from '@/components/ui/dropdown-menu'
import { cn } from '@/lib/utils'

import type { ConversationSummary } from '../../hooks/use-conversation-history'

export interface ConversationHistoryBarProps {
  conversations: ConversationSummary[]
  activeId: string
  onNew: () => void
  onSwitch: (id: string) => void
  onDelete: (id: string) => void
}

export function ConversationHistoryBar({
  conversations,
  activeId,
  onNew,
  onSwitch,
  onDelete,
}: ConversationHistoryBarProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  return (
    <div className='flex shrink-0 items-center gap-2 px-1 pb-2'>
      <Button
        size='sm'
        variant='outline'
        className='h-8 gap-1.5 rounded-full'
        onClick={onNew}
      >
        <Plus className='size-3.5' />
        {t('New chat')}
      </Button>

      <DropdownMenu open={open} onOpenChange={setOpen}>
        <DropdownMenuTrigger
          render={
            <Button
              size='sm'
              variant='outline'
              className='h-8 gap-1.5 rounded-full'
            />
          }
        >
          <History className='size-3.5' />
          {t('History')}
          {conversations.length > 0 && (
            <span className='text-muted-foreground tabular-nums'>
              {conversations.length}
            </span>
          )}
        </DropdownMenuTrigger>
        <DropdownMenuContent align='start' className='w-72 p-1'>
          {conversations.length === 0 ? (
            <div className='text-muted-foreground px-2 py-6 text-center text-sm'>
              {t('No conversation history yet')}
            </div>
          ) : (
            <div className='max-h-80 overflow-y-auto'>
              {conversations.map((c) => (
                <div
                  key={c.id}
                  className={cn(
                    'group flex items-center gap-1 rounded-md px-2 py-1.5 transition-colors',
                    'hover:bg-muted',
                    c.id === activeId && 'bg-muted/60'
                  )}
                >
                  <button
                    type='button'
                    className='min-w-0 flex-1 truncate text-left text-sm'
                    title={c.title}
                    onClick={() => {
                      onSwitch(c.id)
                      setOpen(false)
                    }}
                  >
                    {c.title}
                  </button>
                  <span className='text-muted-foreground shrink-0 text-xs tabular-nums'>
                    {c.messageCount}
                  </span>
                  <button
                    type='button'
                    aria-label={t('Delete conversation')}
                    title={t('Delete conversation')}
                    className={cn(
                      'shrink-0 rounded p-1 text-muted-foreground transition-colors',
                      'hover:bg-destructive/10 hover:text-destructive',
                      'opacity-0 group-hover:opacity-100 focus-visible:opacity-100'
                    )}
                    onClick={() => onDelete(c.id)}
                  >
                    <Trash2 className='size-3.5' />
                  </button>
                </div>
              ))}
            </div>
          )}
        </DropdownMenuContent>
      </DropdownMenu>
    </div>
  )
}
