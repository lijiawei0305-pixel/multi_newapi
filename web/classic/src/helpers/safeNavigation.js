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

function hasControlCharacters(value) {
  for (const character of value) {
    const code = character.charCodeAt(0);
    if (code <= 0x1f || code === 0x7f) return true;
  }
  return false;
}

export function normalizeHttpNavigationUrl(value) {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (!trimmed || hasControlCharacters(trimmed)) return null;
  try {
    const url = new URL(trimmed);
    if (url.protocol !== 'http:' && url.protocol !== 'https:') return null;
    if (url.username || url.password) return null;
    return url.toString();
  } catch {
    return null;
  }
}

const CHAT_INTEGRATION_PROTOCOLS = new Set([
  'aionui:',
  'ama:',
  'ccswitch:',
  'cherrystudio:',
  'deepchat:',
  'opencat:',
]);

export function normalizeChatNavigationUrl(value, { webOnly = false } = {}) {
  if (typeof value !== 'string') return null;
  const trimmed = value.trim();
  if (!trimmed || hasControlCharacters(trimmed)) return null;
  try {
    const url = new URL(trimmed);
    if (url.protocol === 'http:' || url.protocol === 'https:') {
      if (url.username || url.password) return null;
      return url.toString();
    }
    if (!webOnly && CHAT_INTEGRATION_PROTOCOLS.has(url.protocol)) {
      return url.toString();
    }
    return null;
  } catch {
    return null;
  }
}

export function resolveChatTemplateUrl(
  template,
  serverAddress,
  tokenKey,
  { webOnly = false } = {},
) {
  if (
    typeof template !== 'string' ||
    typeof serverAddress !== 'string' ||
    typeof tokenKey !== 'string' ||
    !serverAddress ||
    !tokenKey
  ) {
    return null;
  }

  const resolved = template
    .replaceAll('{address}', encodeURIComponent(serverAddress))
    .replaceAll('{key}', `sk-${tokenKey}`);
  return normalizeChatNavigationUrl(resolved, { webOnly });
}

export function findFirstSafeWebChatTemplate(chats) {
  if (!Array.isArray(chats)) return null;
  for (const chat of chats) {
    if (!chat || typeof chat !== 'object' || Array.isArray(chat)) continue;
    for (const template of Object.values(chat)) {
      if (normalizeChatNavigationUrl(template, { webOnly: true })) {
        return template;
      }
    }
  }
  return null;
}

export function openHttpUrlInNewTab(value) {
  const url = normalizeHttpNavigationUrl(value);
  if (!url || typeof window === 'undefined') return false;
  const opened = window.open(url, '_blank', 'noopener,noreferrer');
  if (opened) opened.opener = null;
  return true;
}

export function openChatUrlInNewTab(value) {
  const url = normalizeChatNavigationUrl(value);
  if (!url || typeof window === 'undefined') return false;
  const opened = window.open(url, '_blank', 'noopener,noreferrer');
  if (opened) opened.opener = null;
  return true;
}

export function openSameOriginPathInNewTab(value) {
  if (
    typeof value !== 'string' ||
    !value.trim() ||
    typeof window === 'undefined'
  ) {
    return false;
  }
  const trimmed = value.trim();
  if (hasControlCharacters(trimmed) || trimmed.includes('\\')) return false;
  try {
    const url = new URL(trimmed, window.location.origin);
    if (url.origin !== window.location.origin || url.username || url.password) {
      return false;
    }
    const opened = window.open(url.href, '_blank', 'noopener,noreferrer');
    if (opened) opened.opener = null;
    return true;
  } catch {
    return false;
  }
}

export function assignHttpNavigationUrl(value) {
  const url = normalizeHttpNavigationUrl(value);
  if (!url || typeof window === 'undefined') return false;
  window.location.assign(url);
  return true;
}

export function submitHttpPaymentForm(value, params, openInNewTab) {
  const url = normalizeHttpNavigationUrl(value);
  if (!url || typeof document === 'undefined') return false;
  const form = document.createElement('form');
  form.action = url;
  form.method = 'POST';
  if (openInNewTab) {
    form.target = '_blank';
    form.rel = 'noopener noreferrer';
  }
  for (const [key, fieldValue] of Object.entries(params || {})) {
    const input = document.createElement('input');
    input.type = 'hidden';
    input.name = key;
    input.value = String(fieldValue);
    form.appendChild(input);
  }
  document.body.appendChild(form);
  form.submit();
  document.body.removeChild(form);
  return true;
}
