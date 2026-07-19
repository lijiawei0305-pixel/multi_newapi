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

import { expect, test } from 'bun:test';
import { JSDOM } from 'jsdom';

import {
  findFirstSafeWebChatTemplate,
  normalizeChatNavigationUrl,
  normalizeHttpNavigationUrl,
  openChatUrlInNewTab,
  openHttpUrlInNewTab,
  openSameOriginPathInNewTab,
  resolveChatTemplateUrl,
  submitHttpPaymentForm,
} from './safeNavigation';

test('navigation accepts only absolute HTTP(S) URLs', () => {
  expect(normalizeHttpNavigationUrl(' https://pay.example/a ')).toBe(
    'https://pay.example/a',
  );
  for (const value of [
    'javascript:globalThis.pwned=true',
    'data:text/html,<script>alert(1)</script>',
    'blob:https://example.com/id',
    'https://user:password@pay.example/path',
    '//evil.example/path',
    '/relative',
    '',
    null,
  ]) {
    expect(normalizeHttpNavigationUrl(value)).toBeNull();
  }
});

test('chat navigation accepts safe web and allowlisted app protocols only', () => {
  expect(normalizeChatNavigationUrl('https://chat.example/path')).toBe(
    'https://chat.example/path',
  );
  expect(normalizeChatNavigationUrl('cherrystudio://providers/import')).toBe(
    'cherrystudio://providers/import',
  );
  expect(
    normalizeChatNavigationUrl('cherrystudio://providers/import', {
      webOnly: true,
    }),
  ).toBeNull();
  for (const value of [
    'javascript:alert(1)',
    'data:text/html,<script>alert(1)</script>',
    'blob:https://example.com/id',
    'file:///etc/passwd',
    'intent://scan/#Intent;scheme=zxing;end',
    'mailto:attacker@example.com',
    'https://user:password@chat.example/path',
    '/relative',
  ]) {
    expect(normalizeChatNavigationUrl(value)).toBeNull();
  }
});

test('chat templates resolve placeholders only after selecting a safe scheme', () => {
  expect(
    resolveChatTemplateUrl(
      'https://chat.example/?base={address}&key={key}',
      'https://api.example',
      'secret',
      { webOnly: true },
    ),
  ).toBe('https://chat.example/?base=https%3A%2F%2Fapi.example&key=sk-secret');
  expect(
    resolveChatTemplateUrl(
      'javascript:{key}',
      'https://api.example',
      'alert(1)',
    ),
  ).toBeNull();
  expect(
    findFirstSafeWebChatTemplate([
      { Local: 'deepchat://provider/install' },
      { Evil: 'data:text/html,pwned' },
      { Web: 'https://chat.example/{key}' },
    ]),
  ).toBe('https://chat.example/{key}');
});

test('new tabs have no opener and payment forms reject script schemes', () => {
  const originalWindow = globalThis.window;
  const originalDocument = globalThis.document;
  const opened = { opener: {} };
  const calls = [];
  globalThis.window = {
    open: (...args) => {
      calls.push(args);
      return opened;
    },
  };
  try {
    expect(openHttpUrlInNewTab('https://pay.example/checkout')).toBe(true);
    expect(calls).toEqual([
      ['https://pay.example/checkout', '_blank', 'noopener,noreferrer'],
    ]);
    expect(opened.opener).toBeNull();
    expect(openHttpUrlInNewTab('javascript:alert(1)')).toBe(false);
    expect(calls).toHaveLength(1);
    expect(openChatUrlInNewTab('opencat://team/join')).toBe(true);
    expect(calls[1]).toEqual([
      'opencat://team/join',
      '_blank',
      'noopener,noreferrer',
    ]);
    expect(openChatUrlInNewTab('file:///etc/passwd')).toBe(false);
    expect(calls).toHaveLength(2);

    globalThis.window.location = { origin: 'https://console.example' };
    expect(openSameOriginPathInNewTab('/console/setting?tab=ratio')).toBe(true);
    expect(calls[2]).toEqual([
      'https://console.example/console/setting?tab=ratio',
      '_blank',
      'noopener,noreferrer',
    ]);
    expect(openSameOriginPathInNewTab('//evil.example/path')).toBe(false);
    expect(openSameOriginPathInNewTab('/\\evil.example/path')).toBe(false);
    expect(calls).toHaveLength(3);

    const dom = new JSDOM('<!doctype html><body></body>');
    globalThis.document = dom.window.document;
    let submittedForm;
    dom.window.HTMLFormElement.prototype.submit = function submit() {
      submittedForm = this;
    };
    expect(submitHttpPaymentForm('javascript:alert(1)', {}, true)).toBe(false);
    expect(submittedForm).toBeUndefined();
    expect(
      submitHttpPaymentForm('https://pay.example/form', { order: 7 }, true),
    ).toBe(true);
    expect(submittedForm.action).toBe('https://pay.example/form');
    expect(submittedForm.target).toBe('_blank');
    expect(submittedForm.rel).toBe('noopener noreferrer');
  } finally {
    if (originalWindow === undefined) delete globalThis.window;
    else globalThis.window = originalWindow;
    if (originalDocument === undefined) delete globalThis.document;
    else globalThis.document = originalDocument;
  }
});
