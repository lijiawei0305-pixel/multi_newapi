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
import { Server } from 'lucide-react'

import { getLobeIcon } from '@/lib/lobe-icon'
import { cn } from '@/lib/utils'

import { CHANNEL_TYPE_OPTIONS } from '../../constants'
import { getChannelTypeIcon } from '../../lib'

export function ChannelTypeLogo(props: {
  type: number
  size?: number
  className?: string
}) {
  const isKnownType = CHANNEL_TYPE_OPTIONS.some(
    (option) => option.value === props.type
  )

  if (!isKnownType) {
    return (
      <Server
        className={cn('text-muted-foreground shrink-0', props.className)}
        style={{
          width: props.size ?? 16,
          height: props.size ?? 16,
        }}
        aria-hidden='true'
      />
    )
  }

  return (
    <span className={cn('inline-flex shrink-0', props.className)}>
      {getLobeIcon(`${getChannelTypeIcon(props.type)}.Color`, props.size ?? 16)}
    </span>
  )
}
