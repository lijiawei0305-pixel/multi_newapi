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

import { beforeAll, expect, mock, test } from 'bun:test';
import { JSDOM } from 'jsdom';
import React, { act } from 'react';

import { evaluateSafeBillingExpression } from './safeBillingEvaluator';

const NullComponent = () => null;
mock.module('@douyinfe/semi-ui', () => ({
  Banner: NullComponent,
  Button: NullComponent,
  Card: NullComponent,
  Collapsible: NullComponent,
  Input: NullComponent,
  InputNumber: NullComponent,
  Radio: NullComponent,
  RadioGroup: NullComponent,
  Select: NullComponent,
  Tag: NullComponent,
  TextArea: NullComponent,
  Typography: { Text: NullComponent },
}));
mock.module('@douyinfe/semi-icons', () => ({
  IconCopy: NullComponent,
  IconDelete: NullComponent,
  IconPlus: NullComponent,
}));
mock.module('./render', () => ({ renderQuota: () => '' }));
mock.module('.', () => ({ copy: () => {}, showSuccess: () => {} }));

let convertRawBillingCostToQuota;
let evalExprLocally;
let generateExprFromVisualConfig;
let tryParseVisualConfig;
let TieredPricingEditor;

beforeAll(async () => {
  const editorModule =
    await import('../pages/Setting/Ratio/components/TieredPricingEditor');
  convertRawBillingCostToQuota = editorModule.convertRawBillingCostToQuota;
  evalExprLocally = editorModule.evalExprLocally;
  generateExprFromVisualConfig = editorModule.generateExprFromVisualConfig;
  tryParseVisualConfig = editorModule.tryParseVisualConfig;
  TieredPricingEditor = editorModule.default;
});

const noExtras = {
  cacheReadTokens: 0,
  cacheCreateTokens: 0,
  cacheCreate1hTokens: 0,
  imageTokens: 0,
  imageOutputTokens: 0,
  audioInputTokens: 0,
  audioOutputTokens: 0,
};

test('safe billing evaluator calculates tiers and rejects executable code', () => {
  expect(
    evaluateSafeBillingExpression(
      'v1:len <= 20 ? tier("short", max(p * 2, c * 3)) : tier("long", p)',
      { p: 10, c: 2, len: 12 },
    ),
  ).toEqual({ value: 20, matchedTier: 'short' });

  delete globalThis.billingEvaluatorOwned;
  for (const payload of [
    'globalThis.billingEvaluatorOwned = true',
    'tier.constructor("globalThis.billingEvaluatorOwned=true")()',
    'p; globalThis.billingEvaluatorOwned = true',
    '({}).constructor.constructor("globalThis.billingEvaluatorOwned=true")()',
  ]) {
    expect(() => evaluateSafeBillingExpression(payload, { p: 1 })).toThrow();
    expect(globalThis.billingEvaluatorOwned).toBeUndefined();
  }
});

test('safe billing evaluator rejects backend-incompatible syntax in every branch', () => {
  for (const expression of [
    'tier("unsafe", 9007199254740993 - 9007199254740992)',
    'tier("unsafe", p % 2)',
    ' v1:tier("unsafe", p)',
    'true ? tier("ok", 1) : unknown()',
    'true ? tier("ok", 1) : tier("bad")',
    'true ? tier("ok", 1) : "wrong type"',
  ]) {
    expect(() =>
      evaluateSafeBillingExpression(expression, { p: 1, c: 1, len: 1 }),
    ).toThrow();
  }
});

test('classic estimator preserves normalized variables and quota conversion', () => {
  expect(
    evalExprLocally(
      'tier("normalized", p + c * 10 + len * 100 + cr * 1000)',
      2,
      3,
      4,
      { ...noExtras, cacheReadTokens: 5 },
    ),
  ).toEqual({ cost: 5432, matchedTier: 'normalized', error: null });
  expect(convertRawBillingCostToQuota(2500000, 500000)).toBe(1250000);
  expect(convertRawBillingCostToQuota(2500000, 0)).toBe(0);
});

test('classic visual parser only regenerates equivalent finite expressions', () => {
  const canonical = 'v1:tier("base\\\"tier", p * 2.5 + c * 15 + cr * 0.25)';
  const parsed = tryParseVisualConfig(canonical);
  expect(parsed).not.toBeNull();
  expect(generateExprFromVisualConfig(parsed)).toBe(canonical);

  for (const expression of [
    'tier("base", p * 1+2 + c * 3)',
    'tier("base", p * 1-2 + c * 3)',
    'tier("base", p * 1e999 + c * 3)',
    'v2:tier("base", p * 1 + c * 3)',
  ]) {
    expect(tryParseVisualConfig(expression)).toBeNull();
  }

  expect(() =>
    generateExprFromVisualConfig({
      tiers: [
        {
          label: 'missing_condition',
          conditions: [],
          input_unit_cost: 1,
          output_unit_cost: 1,
        },
        {
          label: 'fallback',
          conditions: [],
          input_unit_cost: 2,
          output_unit_cost: 2,
        },
      ],
    }),
  ).toThrow('Invalid visual tier configuration');
});

test('classic editor does not rewrite a non-equivalent raw expression on mount', async () => {
  const dom = new JSDOM(
    '<!doctype html><html><body><div id="root"></div></body></html>',
    {
      url: 'http://localhost',
    },
  );
  const previousWindow = globalThis.window;
  const previousDocument = globalThis.document;
  const previousLocalStorage = globalThis.localStorage;
  globalThis.window = dom.window;
  globalThis.document = dom.window.document;
  globalThis.localStorage = dom.window.localStorage;
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;
  const { createRoot } = await import('react-dom/client');
  const changes = [];
  const root = createRoot(dom.window.document.querySelector('#root'));

  await act(async () => {
    root.render(
      React.createElement(TieredPricingEditor, {
        model: {
          name: 'malicious-coefficient',
          billingExpr: 'tier("base", p * 1+2 + c * 3)',
        },
        onExprChange: (expression) => changes.push(expression),
        requestRuleExpr: '',
        onRequestRuleExprChange: () => {},
        t: (key) => key,
      }),
    );
  });

  expect(changes).toEqual([]);
  await act(async () => root.unmount());
  globalThis.window = previousWindow;
  globalThis.document = previousDocument;
  globalThis.localStorage = previousLocalStorage;
  delete globalThis.IS_REACT_ACT_ENVIRONMENT;
});
