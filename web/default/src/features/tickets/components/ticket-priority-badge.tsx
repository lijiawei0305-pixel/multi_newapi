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
import { priorityMeta } from '../lib'
import type { TicketPriority } from '../types'

/** Colored pill for a ticket priority (low / normal / high / urgent). */
export function TicketPriorityBadge(props: {
  priority: TicketPriority | string
}) {
  const { t } = useTranslation()
  const meta = priorityMeta(props.priority, t)
  return (
    <StatusBadge
      label={meta.label}
      variant={meta.variant}
      copyable={false}
    />
  )
}
