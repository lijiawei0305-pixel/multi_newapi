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
// Main-site admin: cross-tenant custom-domain overview.
//   GET    /api/admin/custom-domains        list all tenants' custom domains
//   DELETE /api/admin/custom-domains/:id     force-unbind (moderation)
// ============================================================================

export interface AdminCustomDomain {
  id: number
  tenant_id: number
  tenant_slug: string
  tenant_name: string
  domain: string
  status: string
  cert_status: string
  cert_expires_at: string
  last_error: string
  created_at: string
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
