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
import { afterAll, beforeAll, expect, test } from 'bun:test';
import { JSDOM } from 'jsdom';

let originalWindow;
let originalDocument;
let sanitizeHtmlContent;
let renderSafeMarkdown;

beforeAll(async () => {
  originalWindow = globalThis.window;
  originalDocument = globalThis.document;
  const { window } = new JSDOM('<html><body></body></html>');
  globalThis.window = window;
  globalThis.document = window.document;
  ({ sanitizeHtmlContent, renderSafeMarkdown } = await import('./sanitize.js'));
});

afterAll(() => {
  globalThis.window = originalWindow;
  globalThis.document = originalDocument;
});

test('sanitizeHtmlContent removes executable HTML and unsafe URLs', () => {
  const sanitized = sanitizeHtmlContent(`
    <img src=x onerror="globalThis.pwned=1">
    <a href="javascript:globalThis.pwned=2">bad</a>
    <svg><a href="javascript:globalThis.pwned=3">svg</a></svg>
    <style>body{display:none}</style>
    <p style="background:url(javascript:globalThis.pwned=4)">safe text</p>
  `);

  expect(sanitized).not.toContain('onerror');
  expect(sanitized).not.toContain('javascript:');
  expect(sanitized).not.toContain('<svg');
  expect(sanitized).not.toContain('<style');
  expect(sanitized).not.toContain('style=');
  expect(sanitized).toContain('safe text');
});

test('sanitizeHtmlContent hardens external links', () => {
  const sanitized = sanitizeHtmlContent(
    '<a href="https://example.com">docs</a>',
  );
  expect(sanitized).toContain('target="_blank"');
  expect(sanitized).toContain('rel="noopener noreferrer"');
});

test('renderSafeMarkdown sanitizes raw HTML and Markdown URLs', () => {
  const sanitized = renderSafeMarkdown(`
**safe text**

[bad link](javascript:globalThis.pwned=1)
<img src=x onerror="globalThis.pwned=2">
<script>globalThis.pwned=3</script>
  `);

  expect(sanitized).toContain('<strong>safe text</strong>');
  expect(sanitized).not.toContain('javascript:');
  expect(sanitized).not.toContain('onerror');
  expect(sanitized).not.toContain('<script');
});
