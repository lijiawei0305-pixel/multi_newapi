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
import { MERMAID_CONFIG } from './mermaid-config';

test('untrusted Mermaid diagrams cannot invoke window callbacks', async () => {
  const dom = new JSDOM('<!doctype html><html><body></body></html>', {
    pretendToBeVisual: true,
    url: 'https://example.test/',
  });
  const globalNames = [
    'window',
    'document',
    'navigator',
    'HTMLElement',
    'SVGElement',
    'Element',
    'Node',
    'XMLSerializer',
    'DOMParser',
    'getComputedStyle',
    'Event',
    'MouseEvent',
    'CSSStyleSheet',
  ];
  const originalGlobals = new Map(
    globalNames.map((name) => [name, globalThis[name]]),
  );

  for (const name of globalNames.slice(0, -1)) {
    globalThis[name] = dom.window[name];
  }
  globalThis.CSSStyleSheet = class CSSStyleSheet {
    constructor() {
      this.cssRules = [];
    }

    insertRule(rule, index = this.cssRules.length) {
      this.cssRules.splice(index, 0, { cssText: rule });
    }

    replaceSync(cssText) {
      this.cssRules = [{ cssText }];
    }
  };
  dom.window.SVGElement.prototype.getBBox = () => ({
    x: 0,
    y: 0,
    width: 100,
    height: 20,
  });
  dom.window.SVGElement.prototype.getComputedTextLength = () => 100;

  try {
    let callbackCount = 0;
    dom.window.__mermaidPwn = () => {
      callbackCount += 1;
    };

    const { default: mermaid } = await import('mermaid');
    mermaid.initialize(MERMAID_CONFIG);
    const container = document.createElement('div');
    container.textContent = [
      'flowchart TD',
      'A[Click me]',
      'click A call __mermaidPwn()',
    ].join('\n');
    document.body.append(container);

    await mermaid.run({ nodes: [container] });
    const node = container.querySelector('.node');
    expect(node).not.toBeNull();
    node.dispatchEvent(new dom.window.MouseEvent('click', { bubbles: true }));

    expect(MERMAID_CONFIG.securityLevel).toBe('strict');
    expect(callbackCount).toBe(0);
  } finally {
    dom.window.close();
    for (const [name, value] of originalGlobals) {
      if (value === undefined) delete globalThis[name];
      else globalThis[name] = value;
    }
  }
});
