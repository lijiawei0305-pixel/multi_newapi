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
import type {
  ModelGroup,
  ModelGroupPayload,
  ModelGroupUpdatePayload,
} from '../types'

export function getModelGroupFormSchema(t: TFunction) {
  return z.object({
    name: z.string().min(1, t('Please enter a group name')),
    ratio: z.coerce.number().min(0, t('Please enter a valid ratio')),
    channel_id: z.coerce.number().int().min(0),
    description: z.string(),
    enabled: z.boolean(),
    sort: z.coerce.number().int(),
  })
}

export type ModelGroupFormValues = z.infer<
  ReturnType<typeof getModelGroupFormSchema>
>

export const MODEL_GROUP_FORM_DEFAULTS: ModelGroupFormValues = {
  name: '',
  ratio: 1,
  channel_id: 0,
  description: '',
  enabled: true,
  sort: 0,
}

export function modelGroupToFormValues(g: ModelGroup): ModelGroupFormValues {
  return {
    name: g.name || '',
    ratio: Number(g.ratio ?? 1),
    channel_id: Number(g.channel_id || 0),
    description: g.description || '',
    enabled: g.enabled ?? true,
    sort: Number(g.sort || 0),
  }
}

/** Create body — omit channel_id when none is bound (0). */
export function formValuesToCreatePayload(
  values: ModelGroupFormValues
): ModelGroupPayload {
  const channelId = Number(values.channel_id || 0)
  return {
    name: values.name.trim(),
    ratio: Number(values.ratio || 0),
    ...(channelId > 0 ? { channel_id: channelId } : {}),
    description: values.description.trim(),
    enabled: values.enabled,
    sort: Number(values.sort || 0),
  }
}

/** Update body — name is immutable; channel_id is always sent (0 = unbind). */
export function formValuesToUpdatePayload(
  values: ModelGroupFormValues
): ModelGroupUpdatePayload {
  return {
    ratio: Number(values.ratio || 0),
    channel_id: Number(values.channel_id || 0),
    description: values.description.trim(),
    enabled: values.enabled,
    sort: Number(values.sort || 0),
  }
}
