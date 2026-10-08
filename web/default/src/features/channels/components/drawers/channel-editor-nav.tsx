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
import type { TFunction } from 'i18next'
import {
  AlertCircle,
  Boxes,
  CheckCircle2,
  Circle,
  KeyRound,
  Server,
  Settings,
} from 'lucide-react'
import type { ReactNode } from 'react'
import { useTranslation } from 'react-i18next'

import { cn } from '@/lib/utils'

import type { ChannelEditorSectionStatus } from './channel-editor-status'

export type ChannelEditorNavItem = {
  id: string
  title: string
  description?: string
  statusLabel: string
  status: ChannelEditorSectionStatus
  icon: ReactNode
}

function getSectionStatusIcon(status: ChannelEditorSectionStatus): ReactNode {
  if (status === 'error') {
    return <AlertCircle className='h-3.5 w-3.5' aria-hidden='true' />
  }
  if (status === 'complete' || status === 'configured') {
    return <CheckCircle2 className='h-3.5 w-3.5' aria-hidden='true' />
  }
  return <Circle className='h-3.5 w-3.5' aria-hidden='true' />
}

function getSectionStatusLabel(
  status: ChannelEditorSectionStatus,
  t: TFunction
): string {
  if (status === 'error') return t('Error')
  if (status === 'complete' || status === 'configured') return t('Ready')
  return t('Incomplete')
}

function buildChannelEditorNavItems(props: {
  t: TFunction
  identityStatus: ChannelEditorSectionStatus
  credentialsStatus: ChannelEditorSectionStatus
  modelsStatus: ChannelEditorSectionStatus
  advancedStatus: ChannelEditorSectionStatus
}): ChannelEditorNavItem[] {
  const advancedSummary =
    props.advancedStatus === 'error' ? props.t('Error') : undefined

  return [
    {
      id: 'channel-section-identity',
      title: props.t('Basic Information'),
      description: getSectionStatusLabel(props.identityStatus, props.t),
      statusLabel: getSectionStatusLabel(props.identityStatus, props.t),
      status: props.identityStatus,
      icon: <Server className='h-4 w-4' aria-hidden='true' />,
    },
    {
      id: 'channel-section-credentials',
      title: props.t('Credentials'),
      description: getSectionStatusLabel(props.credentialsStatus, props.t),
      statusLabel: getSectionStatusLabel(props.credentialsStatus, props.t),
      status: props.credentialsStatus,
      icon: <KeyRound className='h-4 w-4' aria-hidden='true' />,
    },
    {
      id: 'channel-section-models',
      title: props.t('Models & Groups'),
      description: getSectionStatusLabel(props.modelsStatus, props.t),
      statusLabel: getSectionStatusLabel(props.modelsStatus, props.t),
      status: props.modelsStatus,
      icon: <Boxes className='h-4 w-4' aria-hidden='true' />,
    },
    {
      id: 'channel-section-advanced',
      title: props.t('Advanced Settings'),
      description: advancedSummary,
      statusLabel: advancedSummary ?? props.t('Advanced Settings'),
      status: props.advancedStatus,
      icon: <Settings className='h-4 w-4' aria-hidden='true' />,
    },
  ]
}

export function ChannelEditorNav(props: {
  providerLogo: ReactNode
  providerLabel: string
  statusLabel: string
  progressLabel: string
  navigationLabel: string
  identityStatus: ChannelEditorSectionStatus
  credentialsStatus: ChannelEditorSectionStatus
  modelsStatus: ChannelEditorSectionStatus
  advancedStatus: ChannelEditorSectionStatus
}) {
  const { t } = useTranslation()
  const items = buildChannelEditorNavItems({
    t,
    identityStatus: props.identityStatus,
    credentialsStatus: props.credentialsStatus,
    modelsStatus: props.modelsStatus,
    advancedStatus: props.advancedStatus,
  })

  return (
    <aside className='hidden self-start lg:sticky lg:top-4 lg:z-20 lg:block'>
      <div className='flex max-h-[calc(100dvh-12rem)] flex-col gap-3 overflow-y-auto overscroll-contain pr-1'>
        <div className='border-border/60 bg-muted/20 rounded-lg border p-3'>
          <div className='flex min-w-0 items-center gap-2'>
            <span className='bg-background flex size-8 shrink-0 items-center justify-center rounded-md border'>
              {props.providerLogo}
            </span>
            <div className='min-w-0'>
              <p className='truncate text-sm font-medium'>
                {props.providerLabel}
              </p>
              <p className='text-muted-foreground truncate text-xs'>
                {props.statusLabel} · {props.progressLabel}
              </p>
            </div>
          </div>
        </div>

        <nav
          className='border-border/60 bg-background rounded-lg border p-1'
          aria-label={props.navigationLabel}
        >
          {items.map((item) => {
            const isError = item.status === 'error'
            const isDone =
              item.status === 'complete' || item.status === 'configured'
            return (
              <button
                key={item.id}
                type='button'
                className={cn(
                  'hover:bg-muted/60 flex w-full items-start gap-2 rounded-md px-2 py-2 text-left transition-colors',
                  isError && 'text-destructive hover:bg-destructive/10'
                )}
                onClick={() => {
                  document
                    .querySelector<HTMLElement>(`#${item.id}`)
                    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
                }}
              >
                <span
                  className={cn(
                    'bg-muted text-muted-foreground mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md',
                    isError && 'bg-destructive/10 text-destructive',
                    isDone && !isError && 'text-primary'
                  )}
                >
                  {item.icon}
                </span>
                <span className='min-w-0 flex-1'>
                  <span className='block truncate text-sm font-medium'>
                    {item.title}
                  </span>
                  {item.description && (
                    <span className='text-muted-foreground block truncate text-xs'>
                      {item.description}
                    </span>
                  )}
                </span>
                <span
                  className={cn(
                    'text-muted-foreground mt-1 shrink-0',
                    isError && 'text-destructive',
                    isDone && !isError && 'text-primary'
                  )}
                  aria-label={item.statusLabel}
                >
                  {getSectionStatusIcon(item.status)}
                </span>
              </button>
            )
          })}
        </nav>
      </div>
    </aside>
  )
}
