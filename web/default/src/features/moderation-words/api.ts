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
import type { ApiResponse, BannedWord, WordsApi, WordInput } from './types'

// Auth is carried by new-api's shared axios instance (session cookie +
// New-Api-User header). `upsertWord` uses skipErrorHandler so the page can
// surface the stable MODERATION_WORD_INVALID code as a localized toast.

export async function listAdminWords(): Promise<ApiResponse<BannedWord[]>> {
  const res = await api.get('/api/admin/moderation/words')
  return res.data
}

export async function upsertAdminWord(
  w: WordInput
): Promise<ApiResponse<BannedWord>> {
  const res = await api.post('/api/admin/moderation/words', w, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function deleteAdminWord(
  id: number
): Promise<ApiResponse<unknown>> {
  const res = await api.delete(`/api/admin/moderation/words/${id}`)
  return res.data
}

/** Admin (global base library, tenant_id=0) words API. */
export const adminWordsApi: WordsApi = {
  listWords: listAdminWords,
  upsertWord: upsertAdminWord,
  deleteWord: deleteAdminWord,
}
