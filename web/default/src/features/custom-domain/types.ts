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
// Agent self-service: custom domain binding (OEM).
//   POST   /api/tenant/custom-domain         bind (returns A + TXT guidance)
//   GET    /api/tenant/custom-domain         current binding + status
//   POST   /api/tenant/custom-domain/verify  trigger DNS TXT ownership check
//   DELETE /api/tenant/custom-domain         unbind
// State machine: pending_dns → verifying → dns_verified → active; failure → failed.
// Only `active` domains resolve to the tenant (backend safety invariant).
// ============================================================================

export type CustomDomainStatus =
  | 'pending_dns'
  | 'verifying'
  | 'dns_verified'
  | 'active'
  | 'failed'

export interface DnsRecord {
  type: string
  name: string
  value: string
}

export interface CustomDomain {
  bound: boolean
  domain?: string
  status?: CustomDomainStatus
  verify_token?: string
  cert_status?: string
  cert_expires_at?: string
  last_error?: string
  dns?: {
    a_record: DnsRecord
    txt_record: DnsRecord
  }
}

/** Unified new-api control-plane envelope: `{ success, message, data }`. */
export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
