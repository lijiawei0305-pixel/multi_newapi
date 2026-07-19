/*
Copyright (C) 2025 QuantumNous

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

import createDOMPurify from 'dompurify';
import { marked } from 'marked';

const purifier = createDOMPurify(window);

purifier.addHook('afterSanitizeAttributes', (node) => {
  if (node.tagName !== 'A') return;
  const href = node.getAttribute('href') || '';
  if (/^https?:\/\//i.test(href)) {
    node.setAttribute('target', '_blank');
  }
  if (node.getAttribute('target') === '_blank') {
    node.setAttribute('rel', 'noopener noreferrer');
  }
});

const sanitizeConfig = {
  ALLOW_DATA_ATTR: false,
  FORBID_TAGS: [
    'script',
    'style',
    'svg',
    'math',
    'iframe',
    'object',
    'embed',
    'link',
    'meta',
    'form',
    'input',
    'button',
  ],
  FORBID_ATTR: ['style', 'srcdoc'],
};

export function sanitizeHtmlContent(value) {
  return purifier.sanitize(String(value || ''), sanitizeConfig);
}

export function renderSafeMarkdown(value) {
  return sanitizeHtmlContent(marked.parse(String(value || '')));
}
