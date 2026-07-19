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
import { useMemo, useState } from 'react'
import { useTranslation } from 'react-i18next'

import { Badge } from '@/components/ui/badge'
import { Field, FieldLabel } from '@/components/ui/field'
import { BILLING_EXTRA_VARS } from '@/features/pricing/lib/billing-expr'
import {
  type ExtraTokenValues,
  convertRawBillingCostToQuota,
  evalExprLocally,
  exprUsesExtraVars,
} from '@/features/pricing/lib/tier-expr'
import { useSystemConfig } from '@/hooks/use-system-config'
import { cn } from '@/lib/utils'

import { DraftNumberInput } from './draft-number-input'

type BillingCostEstimatorProps = {
  effectiveExpr: string
  visualConfigInvalid: boolean
}

export function BillingCostEstimator(props: BillingCostEstimatorProps) {
  const { t } = useTranslation()
  const { currency } = useSystemConfig()
  const quotaPerUnit = currency.quotaPerUnit
  const [billableInputTokens, setBillableInputTokens] = useState(0)
  const [billableOutputTokens, setBillableOutputTokens] = useState(0)
  const [fullInputLength, setFullInputLength] = useState(0)
  const [extras, setExtras] = useState<ExtraTokenValues>({
    cacheReadTokens: 0,
    cacheCreateTokens: 0,
    cacheCreate1hTokens: 0,
    imageTokens: 0,
    imageOutputTokens: 0,
    audioInputTokens: 0,
    audioOutputTokens: 0,
  })

  const usesExtras = useMemo(
    () => exprUsesExtraVars(props.effectiveExpr),
    [props.effectiveExpr]
  )

  const result = useMemo(() => {
    const evaluated = evalExprLocally(
      props.effectiveExpr,
      billableInputTokens,
      billableOutputTokens,
      fullInputLength,
      extras
    )
    if (evaluated.error) return evaluated
    return {
      ...evaluated,
      cost: convertRawBillingCostToQuota(evaluated.cost, quotaPerUnit),
    }
  }, [
    props.effectiveExpr,
    billableInputTokens,
    billableOutputTokens,
    fullInputLength,
    extras,
    quotaPerUnit,
  ])
  const hasError = props.visualConfigInvalid || Boolean(result.error)

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='space-y-1'>
        <h4 className='text-sm font-medium'>{t('Token estimator')}</h4>
        <p className='text-muted-foreground text-xs'>
          {t(
            'Enter token counts to preview the estimated cost (excluding group multipliers).'
          )}
        </p>
      </div>
      <div className='grid grid-cols-1 gap-3 sm:grid-cols-3'>
        <Field className='gap-1'>
          <FieldLabel htmlFor='billing-estimator-p' className='text-xs'>
            {t('Billable input tokens')} (p)
          </FieldLabel>
          <DraftNumberInput
            id='billing-estimator-p'
            min={0}
            value={billableInputTokens}
            onValueChange={setBillableInputTokens}
          />
        </Field>
        <Field className='gap-1'>
          <FieldLabel htmlFor='billing-estimator-c' className='text-xs'>
            {t('Billable output tokens')} (c)
          </FieldLabel>
          <DraftNumberInput
            id='billing-estimator-c'
            min={0}
            value={billableOutputTokens}
            onValueChange={setBillableOutputTokens}
          />
        </Field>
        <Field className='gap-1'>
          <FieldLabel htmlFor='billing-estimator-len' className='text-xs'>
            {t('Full input length')} (len)
          </FieldLabel>
          <DraftNumberInput
            id='billing-estimator-len'
            min={0}
            value={fullInputLength}
            onValueChange={setFullInputLength}
          />
        </Field>
      </div>
      {usesExtras && (
        <div className='grid grid-cols-2 gap-3'>
          {BILLING_EXTRA_VARS.map((variable) => {
            if (!variable.field) return null
            const stateKey = variable.field.replace(
              'Price',
              'Tokens'
            ) as keyof ExtraTokenValues
            const inputId = `billing-estimator-${variable.key}`
            return (
              <Field key={variable.key} className='gap-1'>
                <FieldLabel htmlFor={inputId} className='text-xs'>
                  {t(variable.shortLabel)}
                </FieldLabel>
                <DraftNumberInput
                  id={inputId}
                  min={0}
                  value={extras[stateKey]}
                  onValueChange={(value) =>
                    setExtras((previous) => ({
                      ...previous,
                      [stateKey]: value,
                    }))
                  }
                />
              </Field>
            )
          })}
        </div>
      )}
      <div
        className={cn(
          'rounded-md border p-3 text-sm',
          hasError
            ? 'border-destructive/50 bg-destructive/10 text-destructive'
            : 'border-primary/50 bg-primary/10'
        )}
      >
        {hasError ? (
          <span>
            {t('Expression error')}
            {!props.visualConfigInvalid && result.error
              ? `: ${result.error}`
              : ''}
          </span>
        ) : (
          <div className='flex items-center gap-2'>
            <span className='font-medium'>
              {t('Estimated quota cost')}: {result.cost.toLocaleString()}
            </span>
            {result.matchedTier && (
              <Badge variant='outline' className='text-xs'>
                {t('Hit tier')}: {result.matchedTier}
              </Badge>
            )}
          </div>
        )}
      </div>
    </div>
  )
}
