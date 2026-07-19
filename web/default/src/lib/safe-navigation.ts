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

function hasControlCharacters(value: string): boolean {
  for (const character of value) {
    const code = character.charCodeAt(0)
    if (code <= 0x1f || code === 0x7f) return true
  }
  return false
}

export function normalizeHttpNavigationUrl(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed || hasControlCharacters(trimmed)) return null
  try {
    const url = new URL(trimmed)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    if (url.username || url.password) return null
    return url.toString()
  } catch {
    return null
  }
}

export function normalizeHttpResourceUrl(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed || hasControlCharacters(trimmed) || trimmed.includes('\\')) {
    return null
  }
  try {
    const base =
      typeof window !== 'undefined' ? window.location.origin : undefined
    const url = new URL(trimmed, base)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    if (url.username || url.password) return null
    return url.toString()
  } catch {
    return null
  }
}

export function normalizeSameOriginNavigationPath(
  value: unknown
): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed || hasControlCharacters(trimmed) || trimmed.includes('\\')) {
    return null
  }

  const browserOrigin =
    typeof window !== 'undefined' ? window.location.origin : null
  const baseOrigin = browserOrigin ?? 'https://new-api.invalid'

  try {
    const url = new URL(trimmed, `${baseOrigin}/`)
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null
    if (url.username || url.password || url.origin !== baseOrigin) return null
    if (!browserOrigin && /^[a-z][a-z\d+.-]*:/i.test(trimmed)) return null
    return `${url.pathname}${url.search}${url.hash}`
  } catch {
    return null
  }
}

export function normalizeRasterImageDataUrl(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  return /^data:image\/(?:avif|bmp|gif|jpe?g|png|webp);base64,[a-z\d+/=\r\n]+$/i.test(
    trimmed
  )
    ? trimmed
    : null
}

export function normalizeImageResourceUrl(value: unknown): string | null {
  return normalizeRasterImageDataUrl(value) ?? normalizeHttpResourceUrl(value)
}

export function normalizeBase64VideoDataUrl(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  return /^data:video\/(?:mp4|ogg|quicktime|webm|x-m4v);base64,[a-z\d+/=\r\n]+$/i.test(
    trimmed
  )
    ? trimmed
    : null
}

export function normalizePaymentQrNavigationUrl(value: unknown): string | null {
  if (typeof value !== 'string') return null
  const trimmed = value.trim()
  if (!trimmed) return null
  try {
    const url = new URL(trimmed)
    if (!['http:', 'https:', 'weixin:'].includes(url.protocol)) return null
    if (
      (url.protocol === 'http:' || url.protocol === 'https:') &&
      (url.username || url.password)
    ) {
      return null
    }
    return url.toString()
  } catch {
    return null
  }
}

export function openHttpUrlInNewTab(value: unknown): boolean {
  const url = normalizeHttpNavigationUrl(value)
  if (!url || typeof window === 'undefined') return false
  const opened = window.open(url, '_blank', 'noopener,noreferrer')
  if (opened) opened.opener = null
  return true
}

export function openHttpResourceUrlInNewTab(value: unknown): boolean {
  const url = normalizeHttpResourceUrl(value)
  if (!url || typeof window === 'undefined') return false
  const opened = window.open(url, '_blank', 'noopener,noreferrer')
  if (opened) opened.opener = null
  return true
}
