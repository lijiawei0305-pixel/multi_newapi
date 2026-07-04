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

export interface StuckOrder {
  kind: 'RCG' | 'SUB'
  order_no: string
  tenant_id: number
  user_id: number
  amount: number
  status: string
  stuck_secs: number
}

export interface ReconcileHeartbeat {
  last_run_at: number // unix seconds; 0 = never
  last_trigger: string // 'cron' | 'manual' | ''
  today_runs: number
  last_stuck_count: number
  last_failed_count: number
}

export interface StuckList {
  stuck: StuckOrder[]
  threshold_secs: number
  heartbeat?: ReconcileHeartbeat
}

export interface ReconcileRunResult {
  rcg?: { scanned: number; credited: string[] | null; failed: Record<string, string> }
  rcg_created?: {
    scanned: number
    credited: string[] | null
    expired?: string[] | null
    failed: Record<string, string>
  }
  sub?: {
    scanned: number
    activated: string[] | null
    unpaid: string[] | null
    failed: Record<string, string>
  }
}

/** 列当前卡单（RCG paid + SUB pending，早于对账阈值）。只读。 */
export async function listStuckOrders(): Promise<StuckList> {
  const res = await api.get('/api/admin/reconcile/stuck')
  return res.data.data
}

/** 手动立即对账（RCG+SUB），返回本次结果。与 5min 定时扫同逻辑、同样幂等。 */
export async function runReconcile(): Promise<ReconcileRunResult> {
  const res = await api.post('/api/admin/reconcile/run')
  return res.data.data
}

export interface HistoryRun {
  id: number
  ran_at: number // unix seconds
  trigger: string // 'cron' | 'manual'
  summary: string
  detail: unknown // parsed JSON: { paid, created, sub }
}

/** 列最近对账记录（倒序）。手动全记 + 定时有实事才记。 */
export async function listHistory(limit = 50): Promise<HistoryRun[]> {
  const res = await api.get('/api/admin/reconcile/history', { params: { limit } })
  return res.data.data.runs
}

// ---- 支付概览（4 态汇总 + 分页订单，doc/payment-overview.md）----

export type PaymentStatus = 'created' | 'paid' | 'credited' | 'failed'

export interface PaymentSummaryItem {
  status: PaymentStatus
  count: number
  amount_cny: number
}

export interface PaymentOrderItem {
  order_no: string
  type: 'recharge' | 'subscription'
  provider: 'wxpay' | 'alipay'
  amount_cny: number
  status: string
  user_id: number
  tenant_id: number
  created_at_ts: number
}

export interface PaymentOverview {
  summary: PaymentSummaryItem[]
  orders: {
    items: PaymentOrderItem[]
    total: number
    page: number
    page_size: number
  }
}

export interface PaymentOverviewParams {
  start_timestamp: number
  end_timestamp: number
  provider?: string
  type?: string
  status?: string
  page?: number
  page_size?: number
}

/** 支付概览：按 时间/方式/类型 汇总 4 态（笔数+金额）+ 分页订单列表（可再按 status 筛）。 */
export async function getPaymentOverview(
  params: PaymentOverviewParams
): Promise<PaymentOverview> {
  const res = await api.get('/api/admin/reconcile/overview', { params })
  return res.data.data
}
