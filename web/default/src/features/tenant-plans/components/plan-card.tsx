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
import { CalendarClock, Loader2, Sparkles, Wallet2 } from 'lucide-react'
import { useTranslation } from 'react-i18next'
import { StatusBadge } from '@/components/status-badge'
import { Button } from '@/components/ui/button'
import { Card, CardContent, CardFooter } from '@/components/ui/card'
import { formatBillingCurrencyFromUSD } from '@/lib/currency'
import { cn } from '@/lib/utils'
import { cny } from '../lib/format'
import type { TenantPlan } from '../types'

interface PlanCardProps {
  plan: TenantPlan
  onBuy: (plan: TenantPlan) => void
  purchasing: boolean
  /** Disabled when no official payment channel is configured (or still loading). */
  disabled?: boolean
}

export function PlanCard({
  plan,
  onBuy,
  purchasing,
  disabled,
}: PlanCardProps) {
  const { t } = useTranslation()
  const recommended = !!plan.is_recommended

  return (
    <Card
      data-testid={`plan-card-${plan.code}`}
      className={cn(
        'relative flex flex-col',
        recommended && 'ring-2 ring-primary'
      )}
    >
      {/* Discount corner badge */}
      {plan.discount_label ? (
        <span className='bg-destructive text-destructive-foreground absolute top-0 right-0 rounded-bl-lg px-2 py-0.5 text-xs font-semibold'>
          {plan.discount_label}
        </span>
      ) : null}

      <CardContent className='flex flex-1 flex-col gap-3 pt-1'>
        <div className='flex flex-wrap items-center gap-2'>
          <h3 className='text-base font-semibold'>{plan.name}</h3>
          {recommended ? (
            <StatusBadge
              label={t('Recommended')}
              variant='warning'
              icon={Sparkles}
              copyable={false}
            />
          ) : null}
          {plan.badge ? (
            <StatusBadge label={plan.badge} variant='info' copyable={false} />
          ) : null}
        </div>

        <div className='flex items-end gap-2'>
          <span className='text-primary text-3xl font-bold'>
            {cny(plan.retail_price_cny)}
          </span>
          {plan.anchor_price_cny ? (
            <span className='text-muted-foreground pb-1 text-sm line-through'>
              {cny(plan.anchor_price_cny)}
            </span>
          ) : null}
        </div>

        <div className='text-muted-foreground mt-1 flex flex-col gap-1.5 text-sm'>
          <span className='flex items-center gap-2'>
            <Wallet2 className='size-4 shrink-0' />
            {t('Monthly limit')}: {formatBillingCurrencyFromUSD(plan.month_limit_usd)}
          </span>
          <span className='flex items-center gap-2'>
            <CalendarClock className='size-4 shrink-0' />
            {t('Valid for')} {plan.valid_days} {t('days')}
          </span>
        </div>
      </CardContent>

      <CardFooter>
        <Button
          className='w-full'
          variant={recommended ? 'default' : 'outline'}
          disabled={purchasing || disabled}
          onClick={() => onBuy(plan)}
          data-testid={`buy-${plan.code}`}
        >
          {purchasing ? <Loader2 className='size-4 animate-spin' /> : null}
          {t('Buy Now')}
        </Button>
      </CardFooter>
    </Card>
  )
}
