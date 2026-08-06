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
  RedemptionRequest,
  PaymentRequest,
  AmountRequest,
  AffiliateTransferRequest,
  ApiResponse,
  TopupInfoResponse,
  RedemptionResponse,
  AmountResponse,
  PaymentResponse,
  StripePaymentResponse,
  AffiliateCodeResponse,
  AffiliateTransferResponse,
  BillingHistoryResponse,
  CompleteOrderRequest,
  CreemPaymentRequest,
  CreemPaymentResponse,
  WaffoPaymentRequest,
  WaffoPaymentResponse,
  WaffoPancakePaymentRequest,
  WaffoPancakePaymentResponse,
} from './types'

// ============================================================================
// Wallet API Functions
// ============================================================================

/**
 * Check if API response is successful
 */
export function isApiSuccess(response: ApiResponse): boolean {
  return response.success === true || response.message === 'success'
}

/**
 * Get topup configuration info
 */
export async function getTopupInfo(): Promise<TopupInfoResponse> {
  const res = await api.get('/api/user/topup/info')
  return res.data
}

/**
 * Redeem a topup code
 */
export async function redeemTopupCode(
  request: RedemptionRequest
): Promise<RedemptionResponse> {
  const res = await api.post('/api/user/topup', request)
  return res.data
}

/**
 * Calculate payment amount for regular payment
 */
export async function calculateAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Stripe payment
 */
export async function calculateStripeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/stripe/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request regular payment
 */
export async function requestPayment(
  request: PaymentRequest
): Promise<PaymentResponse> {
  const res = await api.post('/api/user/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return {
    ...res.data,
    url: res.data.url || (res as unknown as { url?: string }).url,
  }
}

/**
 * Request Stripe payment
 */
export async function requestStripePayment(
  request: PaymentRequest
): Promise<StripePaymentResponse> {
  const res = await api.post('/api/user/stripe/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Creem payment
 */
export async function requestCreemPayment(
  request: CreemPaymentRequest
): Promise<CreemPaymentResponse> {
  const res = await api.post('/api/user/creem/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Waffo payment
 */
export async function requestWaffoPayment(
  request: WaffoPaymentRequest
): Promise<WaffoPaymentResponse> {
  const res = await api.post('/api/user/waffo/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Calculate payment amount for Waffo Pancake payment
 */
export async function calculateWaffoPancakeAmount(
  request: AmountRequest
): Promise<AmountResponse> {
  const res = await api.post('/api/user/waffo-pancake/amount', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Request Waffo Pancake payment
 */
export async function requestWaffoPancakePayment(
  request: WaffoPancakePaymentRequest
): Promise<WaffoPancakePaymentResponse> {
  const res = await api.post('/api/user/waffo-pancake/pay', request, {
    skipBusinessError: true,
  } as Record<string, unknown>)
  return res.data
}

/**
 * Get affiliate code
 */
export async function getAffiliateCode(): Promise<AffiliateCodeResponse> {
  const res = await api.get('/api/user/aff')
  return res.data
}

/**
 * Transfer affiliate quota to balance
 */
export async function transferAffiliateQuota(
  request: AffiliateTransferRequest
): Promise<AffiliateTransferResponse> {
  const res = await api.post('/api/user/aff_transfer', request)
  return res.data
}

/**
 * Get billing history for current user
 */
export async function getUserBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<ApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/user/topup/self?${params.toString()}`)
  return res.data
}

/**
 * Get billing history for all users (admin only)
 */
export async function getAllBillingHistory(
  page: number,
  pageSize: number,
  keyword?: string
): Promise<ApiResponse<BillingHistoryResponse>> {
  const params = new URLSearchParams({
    p: page.toString(),
    page_size: pageSize.toString(),
  })
  if (keyword) {
    params.append('keyword', keyword)
  }
  const res = await api.get(`/api/user/topup?${params.toString()}`)
  return res.data
}

/**
 * Complete a pending order (admin only)
 */
export async function completeOrder(
  request: CompleteOrderRequest
): Promise<ApiResponse> {
  const res = await api.post('/api/user/topup/complete', request)
  return res.data
}

// ============================================================================
// Tenant wallet recharge (multi-tenant; official WeChat / Alipay, in-process SDK)
// ============================================================================

export interface TenantRechargeRequest {
  /** 美元充值口径（旧）。人民币充值时传 amount_cny，此字段可省。 */
  amount_usd?: number
  /** 人民币充值口径（所见即所付）。>0 时后端优先按此下单，实付精确到分。 */
  amount_cny?: number
  provider: 'wxpay' | 'alipay'
  /** 客户端支付意图幂等键；网络重试保持同一 key（PAY-IDEM-01）。 */
  idempotency_key?: string
}

export type TenantRechargeResponse = ApiResponse<{
  order_no: string
  amount_usd: number
  amount_cny: number
  provider: string
  status?: string
  idempotency_key?: string
  expires_at?: string
  poll_path?: string
  pay: {
    wxpay_qr?: string
    alipay_url?: string
  }
}>

/**
 * Create a tenant wallet recharge order.
 *
 * Credits native quota ($1 = 500k) on payment. WeChat returns a QR payload,
 * Alipay returns a redirect URL. Settlement is handled in-process by the real
 * WeChat/Alipay SDK via the async notify callback (verify) + active query.
 *
 * PAY_CREATE_UNKNOWN (HTTP 202) still returns order_no in data — frontend must
 * keep the same idempotency_key and poll status until QR appears or terminal.
 */
export async function createTenantRecharge(
  request: TenantRechargeRequest
): Promise<TenantRechargeResponse> {
  const res = await api.post('/api/tenant/wallet/recharge', request, {
    skipBusinessError: true,
    // 后端同步 best-effort≈2.5s 后 202 queued；8s 远大于 2.5s+RTT+CF+DB，确保 202 先于 axios 超时。
    timeout: 8000,
  } as Record<string, unknown>)
  return res.data
}

export type TenantRechargeMethod = 'wxpay' | 'alipay'

/**
 * Get the payment channels a buyer may use (enabled && configured).
 *
 * GET /api/tenant/wallet/recharge/methods → { data: { methods: [...] } }.
 * Single-gate: on failure or an unexpected shape we fall back to an EMPTY set
 * rather than assuming both channels are open — the buyer card stays hidden
 * until the real enabled && configured set is known (the recharge/purchase
 * backends also reject unconfigured providers as a second line of defense).
 */
export async function getTenantRechargeMethods(): Promise<{
  methods: TenantRechargeMethod[]
}> {
  try {
    const res = await api.get('/api/tenant/wallet/recharge/methods', {
      skipBusinessError: true,
      skipErrorHandler: true,
    } as Record<string, unknown>)
    const body = res.data as ApiResponse<{ methods?: TenantRechargeMethod[] }>
    if (isApiSuccess(body) && Array.isArray(body.data?.methods)) {
      return {
        methods: body.data.methods.filter(
          (m): m is TenantRechargeMethod => m === 'wxpay' || m === 'alipay'
        ),
      }
    }
    return { methods: [] }
  } catch {
    return { methods: [] }
  }
}
