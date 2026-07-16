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
import { StatusBadge } from '@/components/status-badge'
import { Card, CardContent } from '@/components/ui/card'
import { Skeleton } from '@/components/ui/skeleton'
import { cn } from '@/lib/utils'
import { clampPct, formatDate, usd } from '../lib/format'
import type { TenantSubscription } from '../types'

function statusVariant(status: string) {
  const s = (status || '').toLowerCase()
  if (s === 'active' || s === 'enabled' || s === 'valid') return 'success'
  if (s === 'expired' || s === 'exhausted') return 'neutral'
  if (s === 'pending') return 'warning'
  return 'neutral'
}

/** Threshold colouring for the usage bar (mirrors the monitor alert levels). */
function barColor(pct: number) {
  if (pct >= 100) return 'bg-destructive'
  if (pct >= 90) return 'bg-destructive'
  if (pct >= 75) return 'bg-warning'
  return 'bg-primary'
}

function SubscriptionRow({ sub }: { sub: TenantSubscription }) {
  const { t } = useTranslation()
  const pct = clampPct(sub.usage_pct)

  return (
    <Card size='sm'>
      <CardContent className='flex flex-col gap-2'>
        <div className='flex flex-wrap items-center justify-between gap-2'>
          <div className='flex items-center gap-2'>
            <span className='font-medium'>{sub.plan_name}</span>
            <span className='text-muted-foreground font-mono text-xs'>
              {sub.plan_code}
            </span>
          </div>
          <StatusBadge
            label={sub.status}
            variant={statusVariant(sub.status)}
            copyable={false}
          />
        </div>

        <div className='flex items-center justify-between text-sm'>
          <span className='text-muted-foreground'>
            {usd(sub.used_usd)} / {usd(sub.limit_usd)}
          </span>
          <span className='tabular-nums'>{pct.toFixed(0)}%</span>
        </div>

        <div className='bg-muted h-1.5 w-full overflow-hidden rounded-full'>
          <div
            className={cn('h-full rounded-full transition-all', barColor(pct))}
            style={{ width: `${pct}%` }}
          />
        </div>

        <div className='text-muted-foreground text-xs'>
          {t('Expires on')}: {formatDate(sub.period_end)}
        </div>
      </CardContent>
    </Card>
  )
}

interface MySubscriptionsProps {
  subscriptions: TenantSubscription[]
  isLoading: boolean
}

export function MySubscriptions({
  subscriptions,
  isLoading,
}: MySubscriptionsProps) {
  const { t } = useTranslation()

  return (
    <section data-testid='my-subs' className='flex flex-col gap-3'>
      <h3 className='text-sm font-semibold'>{t('My Subscriptions')}</h3>
      {isLoading && (
        <div className='flex flex-col gap-3'>
          <Skeleton className='h-24 w-full' />
          <Skeleton className='h-24 w-full' />
        </div>
      )}
      {!isLoading && subscriptions.length === 0 && (
        <p className='text-muted-foreground text-sm'>
          {t('You have no subscriptions yet')}
        </p>
      )}
      {!isLoading && subscriptions.length > 0 && (
        <div className='grid grid-cols-1 gap-3 sm:grid-cols-2'>
          {subscriptions.map((sub) => (
            <SubscriptionRow
              key={`${sub.plan_code}-${sub.period_start ?? ''}-${sub.period_end ?? ''}`}
              sub={sub}
            />
          ))}
        </div>
      )}
    </section>
  )
}
