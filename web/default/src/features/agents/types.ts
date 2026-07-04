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
// Sub-agent management — Admin types.
//
// Mirrors the field contract aligned with the backend Worker (snake_case):
//   id / owner_user_id / owner_username / slug / name / level /
//   commission_ratio / discount_ratio / status /
//   withdrawable_cny / frozen_cny / total_earned_cny
// Backed by GET/POST/PATCH /api/admin/agents[/:id]. Auth is carried by
// new-api's shared axios instance (session cookie + New-Api-User header),
// identical to every other admin page.
// ============================================================================

/** One row of the admin agent table. `status` is a tolerant string; `level`
 *  drives capability gating (0=普通/basic, 1=独立/independent). */
export interface Agent {
  id: number
  owner_user_id: number
  owner_username: string
  slug: string
  name: string
  level: number
  /** 分润比例 — share of consumption revenue (e.g. 0.1). */
  commission_ratio: number
  /** 折扣系数 — 全线批发折扣 = 主站价 × 系数（消耗按分组基准、套餐按主站价）；0/空 = 不打折。 */
  discount_ratio: number
  status: string
  /** 可提现余额 (¥). */
  withdrawable_cny: number
  /** 冻结中余额 (¥). */
  frozen_cny?: number
  /** 累计收益 (¥). */
  total_earned_cny?: number
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}

/** Create body for POST /api/admin/agents. */
export interface AgentPayload {
  owner_user_id: number
  slug: string
  name: string
  commission_ratio: number
  discount_ratio: number
  level: number
}

/** PATCH /api/admin/agents/:id — owner & slug are immutable after creation. */
export type AgentUpdatePayload = Partial<
  Omit<AgentPayload, 'owner_user_id' | 'slug'>
>

export type AgentsDialogType = 'create' | 'update'

/** Read-only promotion-decision metrics for one agent (GET /api/admin/agents/:id/metrics). */
export interface AgentMetrics {
  recharge_total_cny: number
  commission_earned_cny: number
  downstream_user_count: number
}
