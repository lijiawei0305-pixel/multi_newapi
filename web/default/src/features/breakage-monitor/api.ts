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
  BreakageDetailParams,
  BreakageDetailResponse,
  BreakageOverview,
  BreakageSnapshotResponse,
  RangeParams,
} from './types'

// ============================================================================
// Breakage 监控 API 客户端。每次调用都走共享 `api` axios 实例（会话 cookie +
// 注入的 `New-Api-User` 头），与其它 admin 页一致。
//
// 全部端点挂在 /api/admin/breakage（AdminAuth + TenantMiddleware）：后端从鉴权
// 中间件解析租户作用域 —— 主站看全平台，代理隔离本租户。前端「绝不」传 tenant_id。
// 参数一律 snake_case / epoch 秒 / 枚举串，对齐冻结契约。
// ============================================================================

/** 1) 概览 4 指标（无时间区间，now = 服务器时间）。 */
export async function getBreakageOverview(): Promise<
  ApiResponse<BreakageOverview>
> {
  const res = await api.get('/api/admin/breakage/overview')
  return res.data
}

/**
 * 2) 明细表（JSON）。分页 + 档位/时间/告警级筛选。
 * CSV 导出走独立的 `exportBreakageDetailCsv`（blob 下载），不经此函数。
 */
export async function getBreakageDetail(
  params: BreakageDetailParams
): Promise<ApiResponse<BreakageDetailResponse>> {
  const res = await api.get('/api/admin/breakage/detail', { params })
  return res.data
}

/** 3) 历史快照趋势（按期升序）。start/end 必填。 */
export async function getBreakageSnapshots(
  params: RangeParams
): Promise<ApiResponse<BreakageSnapshotResponse>> {
  const res = await api.get('/api/admin/breakage/snapshots', { params })
  return res.data
}

// ---------------------------------------------------------------------------
// CSV 导出
// ---------------------------------------------------------------------------

/** 从 Content-Disposition 解出文件名（`filename*=UTF-8''..` 或 `filename="..."`）。 */
function filenameFromDisposition(
  disposition: string | undefined,
  fallback: string
): string {
  if (!disposition) return fallback
  // RFC 5987 `filename*=UTF-8''<pct-encoded>` 优先。
  const star = /filename\*=(?:UTF-8'')?([^;]+)/i.exec(disposition)
  if (star?.[1]) {
    try {
      return decodeURIComponent(star[1].trim().replaceAll(/^["']|["']$/g, ''))
    } catch {
      /* 退回普通 filename */
    }
  }
  const plain = /filename="?([^";]+)"?/i.exec(disposition)
  if (plain?.[1]) return plain[1].trim()
  return fallback
}

/** 触发浏览器保存一个 Blob 为文件（用完即撤销 URL）。 */
function saveBlob(blob: Blob, filename: string): void {
  const url = URL.createObjectURL(blob)
  const a = document.createElement('a')
  a.href = url
  a.download = filename
  document.body.appendChild(a)
  a.click()
  a.remove()
  URL.revokeObjectURL(url)
}

/**
 * 明细 CSV 导出：请求 `?format=csv`，以 blob 接收后触发下载。
 * 表头由后端按作用域决定（跨租户含 tenant_id/tenant_name，代理作用域去掉）；
 * 文件名优先取 Content-Disposition，缺省回退 `breakage-detail-<start>-<end>.csv`。
 * 复用同一批筛选参数（档位/时间/告警级），故导出与当前视图口径一致。
 */
export async function exportBreakageDetailCsv(
  params: Omit<BreakageDetailParams, 'format' | 'page' | 'page_size'>
): Promise<void> {
  const res = await api.get('/api/admin/breakage/detail', {
    params: { ...params, format: 'csv' },
    responseType: 'blob',
    // blob 响应不是统一 JSON 信封，跳过全局业务错误解析。
    skipBusinessError: true,
  })
  const start = params.start_timestamp ?? 0
  const end = params.end_timestamp ?? 0
  const fallback = `breakage-detail-${start}-${end}.csv`
  const disposition =
    (res.headers?.['content-disposition'] as string | undefined) ??
    (res.headers?.['Content-Disposition'] as string | undefined)
  const filename = filenameFromDisposition(disposition, fallback)
  const blob =
    res.data instanceof Blob
      ? res.data
      : new Blob([res.data as BlobPart], { type: 'text/csv;charset=utf-8' })
  saveBlob(blob, filename)
}
