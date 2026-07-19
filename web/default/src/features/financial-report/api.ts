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
  AdminFinanceOverview,
  AgentFinanceOverview,
  AgentRankParams,
  AgentRankResponse,
  ApiResponse,
  FinanceSummary,
  Granularity,
  NetIncomeTrendResponse,
  RangeParams,
  TrendParams,
  TrendResponse,
} from './types'

// ============================================================================
// Financial Reporting API client. Every call rides the shared `api` axios
// instance (session cookie + injected `New-Api-User` header). Admin endpoints
// are cross-tenant (AdminAuth); tenant endpoints are scoped server-side from
// `agentTenantID(c)` — NEVER pass a tenant id from the client. All params are
// snake_case epoch-seconds / enum strings per the FROZEN contract.
// ============================================================================

// ---------------------------------------------------------------------------
// Admin (cross-tenant) — /api/admin/finance/**
// ---------------------------------------------------------------------------

export async function getAdminFinanceSummary(
  params: RangeParams
): Promise<ApiResponse<FinanceSummary<AdminFinanceOverview>>> {
  const res = await api.get('/api/admin/finance/summary', { params })
  return res.data
}

export async function getAdminFinanceTrend(
  params: TrendParams
): Promise<ApiResponse<TrendResponse>> {
  const res = await api.get('/api/admin/finance/trend', { params })
  return res.data
}

export async function getAdminFinanceAgents(
  params: AgentRankParams
): Promise<ApiResponse<AgentRankResponse>> {
  const res = await api.get('/api/admin/finance/agents', { params })
  return res.data
}

// Admin net-income trend (3-line chart source): 套餐净 / api净 series; the total
// line is summed client-side. Same range/granularity params as `/finance/trend`.
export async function getAdminFinanceNetTrend(
  params: RangeParams & { granularity: Granularity }
): Promise<ApiResponse<NetIncomeTrendResponse>> {
  const res = await api.get('/api/admin/finance/net-trend', { params })
  return res.data
}

// ---------------------------------------------------------------------------
// Agent self-service (single tenant) — /api/tenant/finance/**
// ---------------------------------------------------------------------------

export async function getTenantFinanceSummary(
  params: RangeParams
): Promise<ApiResponse<FinanceSummary<AgentFinanceOverview>>> {
  const res = await api.get('/api/tenant/finance/summary', { params })
  return res.data
}

export async function getTenantFinanceTrend(
  params: TrendParams
): Promise<ApiResponse<TrendResponse>> {
  const res = await api.get('/api/tenant/finance/trend', { params })
  return res.data
}
