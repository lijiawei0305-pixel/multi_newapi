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
import { z } from 'zod'
import type { TFunction } from 'i18next'
import type { Agent, AgentPayload } from '../types'

export function getAgentFormSchema(t: TFunction) {
  return z.object({
    owner_user_id: z.coerce
      .number()
      .int()
      .min(1, t('Please select an owner user')),
    slug: z.string().min(1, t('Please enter a slug')),
    name: z.string().min(1, t('Please enter agent name')),
    commission_ratio: z.coerce.number().min(0),
    discount_ratio: z.coerce.number().min(0),
    level: z.coerce.number().int().min(0),
  })
}

export type AgentFormValues = z.infer<ReturnType<typeof getAgentFormSchema>>

export const AGENT_FORM_DEFAULTS: AgentFormValues = {
  owner_user_id: 0,
  slug: '',
  name: '',
  commission_ratio: 0,
  discount_ratio: 0,
  level: 0,
}

export function agentToFormValues(agent: Agent): AgentFormValues {
  return {
    owner_user_id: Number(agent.owner_user_id || 0),
    slug: agent.slug || '',
    name: agent.name || '',
    commission_ratio: Number(agent.commission_ratio || 0),
    discount_ratio: Number(agent.discount_ratio || 0),
    level: Number(agent.level || 0),
  }
}

export function formValuesToPayload(values: AgentFormValues): AgentPayload {
  return {
    owner_user_id: Number(values.owner_user_id || 0),
    slug: values.slug.trim(),
    name: values.name.trim(),
    commission_ratio: Number(values.commission_ratio || 0),
    discount_ratio: Number(values.discount_ratio || 0),
    level: Number(values.level || 0),
  }
}
