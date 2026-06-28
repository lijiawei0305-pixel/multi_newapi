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
import type { ApiResponse, PromotionChannel } from './types'

// Auth is carried by new-api's shared axios instance (session cookie +
// New-Api-User header); the backend scopes every response to the caller.

export async function getPromotionChannels(): Promise<
  ApiResponse<PromotionChannel[]>
> {
  const res = await api.get('/api/tenant/promotion/channels')
  return res.data
}

export async function createPromotionChannel(
  name: string
): Promise<ApiResponse<PromotionChannel>> {
  const res = await api.post('/api/tenant/promotion/channels', { name })
  return res.data
}
