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
  ModelGroup,
  ModelGroupPayload,
  ModelGroupUpdatePayload,
} from './types'

// ============================================================================
// Admin model-group CRUD — GET/POST/PUT/DELETE /api/admin/model-groups[/:name].
// ============================================================================

export async function getModelGroups(): Promise<ApiResponse<ModelGroup[]>> {
  const res = await api.get('/api/admin/model-groups')
  return res.data
}

export async function createModelGroup(
  data: ModelGroupPayload
): Promise<ApiResponse<ModelGroup>> {
  const res = await api.post('/api/admin/model-groups', data)
  return res.data
}

export async function updateModelGroup(
  name: string,
  data: ModelGroupUpdatePayload
): Promise<ApiResponse<{ name: string }>> {
  const res = await api.put(
    `/api/admin/model-groups/${encodeURIComponent(name)}`,
    data
  )
  return res.data
}

export async function deleteModelGroup(
  name: string
): Promise<ApiResponse<{ name: string }>> {
  const res = await api.delete(
    `/api/admin/model-groups/${encodeURIComponent(name)}`
  )
  return res.data
}

// ============================================================================
// Channel dropdown — reuse the existing admin channel listing
// (GET /api/channel/?p=0&page_size=100) and project to id + name.
// ============================================================================

/** Minimal channel shape consumed by the bound-channel dropdown. */
export interface ChannelOption {
  id: number
  name: string
}

interface ChannelListResponse {
  success: boolean
  message?: string
  data?: { items?: Array<{ id: number; name: string }> }
}

export async function getChannelOptions(): Promise<ChannelOption[]> {
  const res = await api.get('/api/channel/', {
    params: { p: 0, page_size: 100 },
  })
  const body = res.data as ChannelListResponse
  const items = body?.data?.items ?? []
  return items.map((c) => ({ id: c.id, name: c.name }))
}
