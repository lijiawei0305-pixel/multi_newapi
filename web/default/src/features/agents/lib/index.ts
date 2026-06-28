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
import type { StatusVariant } from '@/components/status-badge'
import type { AgentType } from '../types'

export {
  getAgentFormSchema,
  AGENT_FORM_DEFAULTS,
  agentToFormValues,
  formValuesToPayload,
  type AgentFormValues,
} from './agent-form'

export const cny = (v: number | undefined) => `¥${Number(v || 0).toFixed(2)}`

/** Plain numeric formatter for ratios/discounts (trims trailing zeros). */
export const num = (v: number | undefined) => {
  const n = Number(v || 0)
  return Number.isInteger(n) ? String(n) : n.toFixed(2)
}

/** i18n label for an agent type. */
export function agentTypeLabel(type: AgentType | string, t: TFunction): string {
  switch (type) {
    case 'oem':
      return t('OEM')
    case 'api':
      return t('API')
    case 'normal':
      return t('Normal')
    default:
      return String(type || '-')
  }
}

/** Badge styling + i18n label for an agent status (tolerant of backend values). */
export function agentStatusMeta(
  status: string | undefined,
  t: TFunction
): { variant: StatusVariant; label: string } {
  switch (status) {
    case 'disabled':
    case 'suspended':
    case 'banned':
      return { variant: 'danger', label: t('Disabled') }
    case 'pending':
      return { variant: 'warning', label: t('Pending') }
    case '':
    case undefined:
    case null as unknown as string:
      return { variant: 'success', label: t('Enabled') }
    default:
      return { variant: 'success', label: t('Enabled') }
  }
}
