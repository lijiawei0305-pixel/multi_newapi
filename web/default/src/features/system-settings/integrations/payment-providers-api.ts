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

// ============================================================================
// Admin payment providers (WeChat / Alipay) status + enable toggle
// ----------------------------------------------------------------------------
// configured: real credentials are present in the independent auth-service
//   (surfaced via /auth/healthz providers). Read-only here.
// enabled: admin switch persisted in the main site (mtwire). Missing record
//   defaults to true so existing "both channels visible" behavior is kept.
// Buyer availability = enabled && configured.
// ============================================================================

export type PaymentProvider = 'wxpay' | 'alipay'

export interface PaymentProviderState {
  configured: boolean
  enabled: boolean
}

export interface PaymentProvidersData {
  wxpay: PaymentProviderState
  alipay: PaymentProviderState
}

interface ApiEnvelope<T> {
  success?: boolean
  message?: string
  data?: T
}

/**
 * Fetch the configured/enabled status of both payment providers.
 * GET /api/admin/payment/providers
 */
export async function getPaymentProviders(): Promise<
  ApiEnvelope<PaymentProvidersData>
> {
  const res = await api.get<ApiEnvelope<PaymentProvidersData>>(
    '/api/admin/payment/providers',
    {
      // The status section renders its own error state; suppress the global
      // interceptor toast to avoid a duplicate error message on load failure.
      skipBusinessError: true,
      skipErrorHandler: true,
    } as Record<string, unknown>
  )
  return res.data
}

/**
 * Toggle a provider's enabled flag (admin only).
 * PUT /api/admin/payment/providers/:provider  body { enabled }
 */
export async function updatePaymentProvider(
  provider: PaymentProvider,
  enabled: boolean
): Promise<ApiEnvelope<{ provider: PaymentProvider; enabled: boolean }>> {
  const res = await api.put<
    ApiEnvelope<{ provider: PaymentProvider; enabled: boolean }>
  >(`/api/admin/payment/providers/${provider}`, { enabled }, {
    // The status section owns success/rollback toasts; suppress the global
    // interceptor so failures surface once with the localized message.
    skipBusinessError: true,
    skipErrorHandler: true,
  } as Record<string, unknown>)
  return res.data
}
