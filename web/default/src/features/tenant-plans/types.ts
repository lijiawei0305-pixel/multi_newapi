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
// tokenplan (套餐) — Buyer-facing tenant types
//
// Mirrors doc/uiux.md §4.1 (purchase card) and the multi-tenant endpoints
// delivered by the parallel backend worker:
//   GET  /api/tenant/token-plans            → TenantPlan[]
//   POST /api/tenant/token-plans/:id/purchase → PurchaseResult (PayURL)
//   GET  /api/tenant/subscriptions          → TenantSubscription[]
//
// Currency convention (api-contract §1): `*_cny` are CNY ¥ (price), `*_usd`
// are USD quota/metering. Never mix the two.
// ============================================================================

/** A purchasable plan as shown on the buyer card grid (uiux §4.1). */
export interface TenantPlan {
  /** Numeric primary key (used for the purchase endpoint when present). */
  id?: number
  /** Stable plan code — the identifier surfaced in testids/UI. */
  code: string
  name: string
  /** 零售价 — current retail selling price (¥). */
  retail_price_cny: number
  /** 原价 — strikethrough marketing anchor (¥); not billed. */
  anchor_price_cny?: number
  /** 月限额 — monthly usage cap (USD). */
  month_limit_usd: number
  /** Subscription validity window in days (e.g. 30). */
  valid_days: number
  /** 折扣文案 — e.g. "-91%". */
  discount_label?: string
  /** Marketing badge text (e.g. "热销"). */
  badge?: string
  /** 推荐 — highlight + emphasis on the purchase page. */
  is_recommended?: boolean
  /** 排序 — ascending display order. */
  sort?: number
}

/** A buyer's own active/expired subscription record (the "我的订阅" block). */
export interface TenantSubscription {
  plan_code: string
  plan_name: string
  status: string
  /** Quota already consumed this period (USD). */
  used_usd: number
  /** Quota ceiling for this period (USD). */
  limit_usd: number
  /** Server-computed usage percentage [0..100]. */
  usage_pct: number
  /** Period bounds — unix seconds, unix ms, or ISO string (formatter is tolerant). */
  period_start?: number | string
  period_end?: number | string
  created_at?: number | string
}

/**
 * Purchase response payload (snake_case, mirrors the wallet recharge envelope).
 * The backend creates a SUB order then asks the auth-service for a mock pay page:
 * `pay_url` and `pay.*` carry the same hosted URL. WeChat surfaces it as a QR
 * payload (`pay.wxpay_qr`), Alipay as a redirect URL (`pay.alipay_url`).
 */
export interface PurchaseResult {
  /** Hosted mock pay page (auth-service); same value as pay.wxpay_qr / pay.alipay_url. */
  pay_url?: string
  /** SUB order number (matches the recharge `order_no` shape). */
  order_no?: string
  /** Retail price actually charged (¥). */
  amount_cny?: number
  /** Purchased plan id. */
  plan_id?: number
  /** Provider-specific pay credential. */
  pay?: {
    /** WeChat: QR payload (mock confirm page URL). */
    wxpay_qr?: string
    /** Alipay: redirect target. */
    alipay_url?: string
  }
  // Legacy / alternative field names kept for tolerance.
  pay_link?: string
  payment_url?: string
  url?: string
  order_id?: string
  [key: string]: unknown
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
