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
import type { ApiResponse, SiteConfig, SiteConfigPatch } from './types'

export async function getSiteConfig(): Promise<ApiResponse<SiteConfig>> {
  const res = await api.get('/api/tenant/site-config', {
    disableDuplicate: true,
  })
  return res.data
}

// skipErrorHandler so the page can localize THEME_NOT_IN_PALETTE etc.
export async function updateSiteConfig(
  patch: SiteConfigPatch
): Promise<ApiResponse<SiteConfig>> {
  const res = await api.put('/api/tenant/site-config', patch, {
    skipErrorHandler: true,
  })
  return res.data
}

export async function uploadLogo(
  file: File
): Promise<ApiResponse<{ logo_url: string }>> {
  const fd = new FormData()
  fd.append('file', file)
  // axios sets the multipart boundary automatically for FormData bodies.
  const res = await api.post('/api/tenant/site-config/logo', fd, {
    skipErrorHandler: true,
  })
  return res.data
}
