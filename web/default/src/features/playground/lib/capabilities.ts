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
import { ENDPOINT_TYPES } from '@/features/pricing/constants'
import type { PricingModel } from '@/features/pricing/types'

import type { CatalogFilter, PlaygroundCapability } from '../types'

// ---------------------------------------------------------------------------
// Internal endpoint-type sets (derived from ENDPOINT_TYPES constants)
// ---------------------------------------------------------------------------

/** Endpoint types that map to the "chat" capability (OR logic). */
const CHAT_ENDPOINT_TYPES = new Set<string>([
  ENDPOINT_TYPES.OPENAI,
  ENDPOINT_TYPES.OPENAI_RESPONSE,
  ENDPOINT_TYPES.ANTHROPIC,
  ENDPOINT_TYPES.GEMINI,
])

/** Endpoint type that maps to the "image" capability. */
const IMAGE_ENDPOINT_TYPE = ENDPOINT_TYPES.IMAGE_GENERATION

/** Endpoint type that maps to the "video" capability. */
const VIDEO_ENDPOINT_TYPE = ENDPOINT_TYPES.OPENAI_VIDEO

// ---------------------------------------------------------------------------
// Public API
// ---------------------------------------------------------------------------

/**
 * Returns all capabilities supported by a model based on its
 * `supported_endpoint_types` field.
 *
 * Handles `undefined` / empty arrays safely.
 */
export function getModelCapabilities(m: PricingModel): PlaygroundCapability[] {
  const types = m.supported_endpoint_types ?? []
  const caps: PlaygroundCapability[] = []

  const hasChat = types.some((t) => CHAT_ENDPOINT_TYPES.has(t))
  if (hasChat) caps.push('chat')

  if (types.includes(IMAGE_ENDPOINT_TYPE)) caps.push('image')
  if (types.includes(VIDEO_ENDPOINT_TYPE)) caps.push('video')

  return caps
}

/**
 * Returns the single "primary" capability for display / workspace routing.
 * Priority order: video > image > chat.
 * Falls back to 'chat' when no recognised endpoint types are present.
 */
export function getPrimaryCapability(m: PricingModel): PlaygroundCapability {
  const types = m.supported_endpoint_types ?? []

  if (types.includes(VIDEO_ENDPOINT_TYPE)) return 'video'
  if (types.includes(IMAGE_ENDPOINT_TYPE)) return 'image'
  return 'chat'
}

/**
 * Filters a list of models by the given `CatalogFilter`.
 *
 * - `'all'`   → no filtering, return as-is
 * - `'chat'`  → model has at least one of: openai / openai-response / anthropic / gemini
 * - `'image'` → model has `image-generation`
 * - `'video'` → model has `openai-video`
 */
export function filterByCapability(
  models: PricingModel[],
  f: CatalogFilter
): PricingModel[] {
  if (f === 'all') return models

  return models.filter((m) => {
    const types = m.supported_endpoint_types ?? []
    switch (f) {
      case 'chat':
        return types.some((t) => CHAT_ENDPOINT_TYPES.has(t))
      case 'image':
        return types.includes(IMAGE_ENDPOINT_TYPE)
      case 'video':
        return types.includes(VIDEO_ENDPOINT_TYPE)
      default:
        return true
    }
  })
}

/**
 * Filter chip definitions for the model catalog.
 * Order: 全部 / 聊天 / 图片 / 视频
 */
export const CATALOG_FILTERS: { value: CatalogFilter; label: string }[] = [
  { value: 'all', label: '全部' },
  { value: 'chat', label: '聊天' },
  { value: 'image', label: '图片' },
  { value: 'video', label: '视频' },
]
