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

import type {
  ModelGroup,
  ModelGroupPayload,
  ModelGroupUpdatePayload,
} from '../types'

export function getModelGroupFormSchema(t: TFunction) {
  return z.object({
    name: z.string().min(1, t('Please enter a group name')),
    ratio: z.coerce.number().min(0, t('Please enter a valid ratio')),
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
  description: '',
  enabled: true,
  sort: 0,
}

export function modelGroupToFormValues(g: ModelGroup): ModelGroupFormValues {
  return {
    name: g.name || '',
    ratio: Number(g.ratio ?? 1),
    description: g.description || '',
    enabled: g.enabled ?? true,
    sort: Number(g.sort || 0),
  }
}

/** Create body — channel binding is set on the channel side, not here. */
export function formValuesToCreatePayload(
  values: ModelGroupFormValues
): ModelGroupPayload {
  return {
    name: values.name.trim(),
    ratio: Number(values.ratio || 0),
    description: values.description.trim(),
    enabled: values.enabled,
    sort: Number(values.sort || 0),
  }
}

/** Update body — name is immutable; channel binding is set on the channel side. */
export function formValuesToUpdatePayload(
  values: ModelGroupFormValues
): ModelGroupUpdatePayload {
  return {
    ratio: Number(values.ratio || 0),
    description: values.description.trim(),
    enabled: values.enabled,
    sort: Number(values.sort || 0),
  }
}
