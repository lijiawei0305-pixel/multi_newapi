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
  DetailItem,
  DetailParams,
  DetailResponse,
  ExportFormat,
  FinanceSummary,
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

export async function getAdminFinanceDetail(
  params: DetailParams
): Promise<ApiResponse<DetailResponse<DetailItem>>> {
  const res = await api.get('/api/admin/finance/detail', { params })
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

export async function getTenantFinanceDetail(
  params: DetailParams
): Promise<ApiResponse<DetailResponse<DetailItem>>> {
  const res = await api.get('/api/tenant/finance/detail', { params })
  return res.data
}

// ---------------------------------------------------------------------------
// Export download. The `detail` endpoints stream a file when `format` is set
// (the ONE documented exception to the JSON envelope, contract §1). We bypass
// the business-error interceptor (`skipBusinessError`) since the body is a
// Blob, then trigger a browser download from the response.
// ---------------------------------------------------------------------------

const DETAIL_URL: Record<'admin' | 'tenant', string> = {
  admin: '/api/admin/finance/detail',
  tenant: '/api/tenant/finance/detail',
}

export async function downloadFinanceDetail(
  scope: 'admin' | 'tenant',
  params: DetailParams,
  format: ExportFormat
): Promise<void> {
  const res = await api.get(DETAIL_URL[scope], {
    params: { ...params, format },
    responseType: 'blob',
    skipBusinessError: true,
    skipErrorHandler: true,
  })

  const blob = res.data as Blob
  const filename =
    filenameFromContentDisposition(
      (res.headers?.['content-disposition'] ??
        res.headers?.['Content-Disposition']) as string | undefined
    ) ??
    `finance-${params.lens}-${params.start_timestamp}-${params.end_timestamp}.${format}`

  triggerBlobDownload(blob, filename)
}

/** Parse a filename out of a `Content-Disposition` header (RFC 5987 aware). */
function filenameFromContentDisposition(
  header: string | undefined
): string | undefined {
  if (!header) return undefined
  // Prefer the RFC 5987 `filename*=UTF-8''...` extended form.
  const star = /filename\*=(?:UTF-8'')?([^;]+)/i.exec(header)
  if (star?.[1]) {
    const raw = star[1].trim().replace(/^"|"$/g, '')
    try {
      return decodeURIComponent(raw)
    } catch {
      return raw
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(header)
  return plain?.[1]?.trim()
}

/** Create a temporary object URL + anchor to save the blob, then clean up. */
function triggerBlobDownload(blob: Blob, filename: string): void {
  const url = window.URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  window.URL.revokeObjectURL(url)
}
