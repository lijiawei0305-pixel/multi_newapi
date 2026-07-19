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
// Model-group management — Admin types.
//
// A model group (e.g. claude-kiro=0.3, openai-plus=0.5) is the second
// dimension of the 2D billing ratio: final ratio = user-tier ratio ×
// model-group ratio. It is defined by name + ratio, and users may pick one
// when creating an API key.
//
// Channel ↔ model-group is many-to-one: a group can be served by multiple
// channels. The binding lives on the channel side (a channel's "model group"
// field, channels.group). `serving_channels` is derived (read-only) from that
// and shown here; it is never edited on this page.
//
// Backed by GET/POST/PUT/DELETE /api/admin/model-groups[/:name]. Auth is the
// shared AdminAuth carried by new-api's axios instance (session cookie +
// New-Api-User header), identical to every other admin page.
// ============================================================================

/** One upstream channel that serves this model group (read-only, server-derived). */
export interface ServingChannel {
  id: number
  name: string
}

/** One row of the admin model-group table. */
export interface ModelGroup {
  id: number
  name: string
  /** 模型分组倍率 — multiplied with the user-tier ratio (e.g. 0.3). */
  ratio: number
  /**
   * Channels serving this group — derived from each channel's group field
   * (all channels whose `group` contains this group name). Read-only.
   */
  serving_channels: ServingChannel[]
  description: string
  enabled: boolean
  sort: number
  created_at?: number | string
  updated_at?: number | string
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}

/** Create body for POST /api/admin/model-groups. No channel binding here — it is set on the channel side. */
export interface ModelGroupPayload {
  name: string
  ratio: number
  description?: string
  enabled?: boolean
  sort?: number
}

/** PUT /api/admin/model-groups/:name — name is immutable after creation. */
export type ModelGroupUpdatePayload = Partial<Omit<ModelGroupPayload, 'name'>>

export type ModelGroupsDialogType = 'create' | 'update' | 'delete'
