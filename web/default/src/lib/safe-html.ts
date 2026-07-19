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

import DOMPurify from 'dompurify'

import {
  normalizeHttpResourceUrl,
  normalizeImageResourceUrl,
} from '@/lib/safe-navigation'

export const FORBIDDEN_RICH_CONTENT_TAGS = [
  'base',
  'embed',
  'form',
  'iframe',
  'object',
]

export const FORBIDDEN_RICH_CONTENT_ATTRIBUTES = ['style']

export function sanitizeUntrustedRichHtml(html: string): string {
  return hardenSanitizedHtml(
    DOMPurify.sanitize(html, {
      FORBID_ATTR: FORBIDDEN_RICH_CONTENT_ATTRIBUTES,
      FORBID_TAGS: FORBIDDEN_RICH_CONTENT_TAGS,
    })
  )
}

export function hardenSanitizedHtml(html: string): string {
  if (typeof document === 'undefined') return html

  const template = document.createElement('template')
  template.innerHTML = html

  template.content
    .querySelectorAll<HTMLAnchorElement>('a[href], area[href]')
    .forEach((link) => {
      const rawHref = link.getAttribute('href') ?? ''
      const safeHref = rawHref.startsWith('#')
        ? rawHref
        : normalizeHttpResourceUrl(rawHref)

      if (!safeHref) {
        link.removeAttribute('href')
        link.removeAttribute('target')
        link.removeAttribute('rel')
        return
      }

      link.setAttribute('href', safeHref)
      const target = link.getAttribute('target')
      if (target === '_blank') {
        link.setAttribute('rel', 'noopener noreferrer')
      } else if (target && target !== '_self') {
        link.removeAttribute('target')
      }
    })

  template.content
    .querySelectorAll<HTMLImageElement>('img[src]')
    .forEach((image) => {
      const safeSrc = normalizeImageResourceUrl(image.getAttribute('src'))
      if (safeSrc) {
        image.setAttribute('src', safeSrc)
      } else {
        image.removeAttribute('src')
      }
      image.removeAttribute('srcset')
    })

  template.content
    .querySelectorAll<HTMLMediaElement>(
      'audio[src], video[src], source[src], track[src]'
    )
    .forEach((media) => {
      const safeSrc = normalizeHttpResourceUrl(media.getAttribute('src'))
      if (safeSrc) {
        media.setAttribute('src', safeSrc)
      } else {
        media.removeAttribute('src')
      }
      media.removeAttribute('srcset')
    })

  template.content
    .querySelectorAll<HTMLVideoElement>('video[poster]')
    .forEach((video) => {
      const safePoster = normalizeImageResourceUrl(video.getAttribute('poster'))
      if (safePoster) {
        video.setAttribute('poster', safePoster)
      } else {
        video.removeAttribute('poster')
      }
    })

  return template.innerHTML
}
