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
  Agent,
  AgentPayload,
  AgentUpdatePayload,
  ApiResponse,
} from './types'

// ============================================================================
// Admin agent CRUD — GET/POST/PATCH /api/admin/agents[/:id].
// ============================================================================

export async function getAdminAgents(): Promise<ApiResponse<Agent[]>> {
  const res = await api.get('/api/admin/agents')
  return res.data
}

export async function createAgent(
  data: AgentPayload
): Promise<ApiResponse<Agent>> {
  const res = await api.post('/api/admin/agents', data)
  return res.data
}

export async function updateAgent(
  id: number,
  data: AgentUpdatePayload
): Promise<ApiResponse<Agent>> {
  const res = await api.patch(`/api/admin/agents/${id}`, data)
  return res.data
}
