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
// Agent self-service: site branding (OEM minimal).
//   GET  /api/tenant/site-config
//   PUT  /api/tenant/site-config            { site_name, brand_hidden, theme_color, ... }
//   POST /api/tenant/site-config/logo       multipart "file" → data: URL logo_url
// ============================================================================

export interface SiteConfig {
  site_name: string
  logo_url: string
  favicon_url: string
  hero_title: string
  hero_subtitle: string
  announcement: string
  customer_service: string
  footer: string
  brand_hidden: boolean
  theme_color: string
}

export interface SiteConfigPatch {
  site_name?: string
  logo_url?: string
  brand_hidden?: boolean
  theme_color?: string
}

export interface ApiResponse<T = unknown> {
  success: boolean
  message?: string
  code?: string
  data?: T
}
