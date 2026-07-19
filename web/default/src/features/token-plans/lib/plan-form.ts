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
import { z } from 'zod'

import type { AdminTokenPlan, TokenPlanPayload } from '../types'

export function getTokenPlanFormSchema(t: TFunction) {
  return z.object({
    code: z.string().min(1, t('Please enter plan code')),
    name: z.string().min(1, t('Please enter plan name')),
    base_price_cny: z.coerce.number().min(0, t('Please enter amount')),
    anchor_price_cny: z.coerce.number().min(0),
    month_limit_usd: z.coerce.number().min(0),
    cost_price_cny: z.coerce.number().min(0),
    min_price_cny: z.coerce.number().min(0),
    multiplier: z.coerce.number().min(0),
    valid_days: z.coerce.number().min(1),
    discount_label: z.string().optional(),
    badge: z.string().optional(),
    is_recommended: z.boolean(),
    sort: z.coerce.number(),
    status: z.enum(['enabled', 'disabled']),
  })
}

export type TokenPlanFormValues = z.infer<
  ReturnType<typeof getTokenPlanFormSchema>
>

export const TOKEN_PLAN_FORM_DEFAULTS: TokenPlanFormValues = {
  code: '',
  name: '',
  base_price_cny: 0,
  anchor_price_cny: 0,
  month_limit_usd: 0,
  cost_price_cny: 0,
  min_price_cny: 0,
  multiplier: 1,
  valid_days: 30,
  discount_label: '',
  badge: '',
  is_recommended: false,
  sort: 0,
  status: 'enabled',
}

export function planToFormValues(plan: AdminTokenPlan): TokenPlanFormValues {
  return {
    code: plan.code || '',
    name: plan.name || '',
    base_price_cny: Number(plan.base_price_cny || 0),
    anchor_price_cny: Number(plan.anchor_price_cny || 0),
    month_limit_usd: Number(plan.month_limit_usd || 0),
    cost_price_cny: Number(plan.cost_price_cny || 0),
    min_price_cny: Number(plan.min_price_cny || 0),
    multiplier: Number(plan.multiplier || 1),
    valid_days: Number(plan.valid_days || 30),
    discount_label: plan.discount_label || '',
    badge: plan.badge || '',
    is_recommended: !!plan.is_recommended,
    sort: Number(plan.sort || 0),
    status: plan.status === 'disabled' ? 'disabled' : 'enabled',
  }
}

export function formValuesToPayload(
  values: TokenPlanFormValues
): TokenPlanPayload {
  return {
    code: values.code.trim(),
    name: values.name.trim(),
    base_price_cny: Number(values.base_price_cny || 0),
    anchor_price_cny: Number(values.anchor_price_cny || 0),
    month_limit_usd: Number(values.month_limit_usd || 0),
    cost_price_cny: Number(values.cost_price_cny || 0),
    min_price_cny: Number(values.min_price_cny || 0),
    multiplier: Number(values.multiplier || 0),
    valid_days: Number(values.valid_days || 0),
    discount_label: values.discount_label?.trim() || '',
    badge: values.badge?.trim() || '',
    is_recommended: !!values.is_recommended,
    sort: Number(values.sort || 0),
    status: values.status,
  }
}
