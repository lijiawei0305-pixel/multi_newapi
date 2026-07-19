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
import { api } from '@/lib/api'

import type {
  ApiResponse,
  BannedWord,
  WordsApi,
  WordInput,
} from '../moderation-words/types'

// Agent self-service: own-tenant banned words. AgentOwnerAuth scopes every
// response to the calling agent's tenant; upsert uses skipErrorHandler to
// surface MODERATION_WORD_INVALID as a localized toast.

export async function listTenantWords(): Promise<ApiResponse<BannedWord[]>> {
  const res = await api.get('/api/tenant/moderation/words')
  return res.data
}

export async function upsertTenantWord(
  w: WordInput
): Promise<ApiResponse<BannedWord>> {
  const res = await api.post('/api/tenant/moderation/words', w, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function deleteTenantWord(
  id: number
): Promise<ApiResponse<unknown>> {
  const res = await api.delete(`/api/tenant/moderation/words/${id}`)
  return res.data
}

/** GET /api/tenant/moderation/base-words — 只读查看全站基础库（继承的全局词）。 */
export async function listBaseWords(): Promise<ApiResponse<BannedWord[]>> {
  const res = await api.get('/api/tenant/moderation/base-words')
  return res.data
}

/** Agent (own tenant) words API. */
export const tenantWordsApi: WordsApi = {
  listWords: listTenantWords,
  upsertWord: upsertTenantWord,
  deleteWord: deleteTenantWord,
}
