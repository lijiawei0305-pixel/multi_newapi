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

// ============================================================================
// Content moderation — banned words (6e · §2.14).
//   admin : /api/admin/moderation/words   (global base library, tenant_id=0)
//   agent : /api/tenant/moderation/words  (own tenant's words)
// Both share the same CRUD shape; only the base path differs (see api.ts).
// ============================================================================

export type MatchType = 'contains' | 'exact' | 'regex'
export type ModerationAction = 'remind' | 'block'

export interface BannedWord {
  id: number
  tenant_id: number
  word: string
  match_type: MatchType
  action: ModerationAction
  enabled: boolean
  created_at?: number | string
}

/** Create (id=0) / update (id>0) payload. */
export interface WordInput {
  id: number
  word: string
  match_type: MatchType
  action: ModerationAction
  enabled: boolean
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}

/** The 3 CRUD calls a WordsManager needs; admin/agent inject their own paths. */
export interface WordsApi {
  listWords: () => Promise<ApiResponse<BannedWord[]>>
  upsertWord: (w: WordInput) => Promise<ApiResponse<BannedWord>>
  deleteWord: (id: number) => Promise<ApiResponse<unknown>>
}
