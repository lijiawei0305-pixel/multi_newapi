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

import path from 'node:path';

import { afterAll, beforeAll, beforeEach, expect, mock, test } from 'bun:test';
import { JSDOM } from 'jsdom';
import { act } from 'react';

const apiGet = mock(async () => ({ data: { success: true, data: [] } }));
const apiPatch = mock(async () => ({ data: { success: true } }));
const showError = mock();
const showSuccess = mock();

mock.module(path.resolve(import.meta.dir, '../../../helpers/api.js'), () => ({
  API: { get: apiGet, patch: apiPatch },
}));

mock.module(
  path.resolve(import.meta.dir, '../../../helpers/utils.jsx'),
  () => ({
    showError,
    showSuccess,
  }),
);

mock.module(
  path.resolve(import.meta.dir, '../../../hooks/common/useTableCompactMode.js'),
  () => ({ useTableCompactMode: () => [false, mock()] }),
);

mock.module('react-i18next', () => ({
  useTranslation: () => ({ t: (key) => key }),
}));

let dom;
let createRoot;
let useSubscriptionsData;
let originalWindowDescriptor;
let originalDocumentDescriptor;
let originalNavigatorDescriptor;

beforeAll(async () => {
  dom = new JSDOM('<!doctype html><html><body></body></html>', {
    url: 'http://localhost',
  });
  originalWindowDescriptor = Object.getOwnPropertyDescriptor(
    globalThis,
    'window',
  );
  originalDocumentDescriptor = Object.getOwnPropertyDescriptor(
    globalThis,
    'document',
  );
  originalNavigatorDescriptor = Object.getOwnPropertyDescriptor(
    globalThis,
    'navigator',
  );
  Object.defineProperty(globalThis, 'window', {
    configurable: true,
    value: dom.window,
  });
  Object.defineProperty(globalThis, 'document', {
    configurable: true,
    value: dom.window.document,
  });
  Object.defineProperty(globalThis, 'navigator', {
    configurable: true,
    value: dom.window.navigator,
  });
  globalThis.IS_REACT_ACT_ENVIRONMENT = true;

  ({ createRoot } = await import('react-dom/client'));
  ({ useSubscriptionsData } =
    await import('../../../hooks/subscriptions/useSubscriptionsData'));
});

afterAll(() => {
  dom.window.close();
  if (originalWindowDescriptor) {
    Object.defineProperty(globalThis, 'window', originalWindowDescriptor);
  } else {
    delete globalThis.window;
  }
  if (originalDocumentDescriptor) {
    Object.defineProperty(globalThis, 'document', originalDocumentDescriptor);
  } else {
    delete globalThis.document;
  }
  if (originalNavigatorDescriptor) {
    Object.defineProperty(globalThis, 'navigator', originalNavigatorDescriptor);
  } else {
    delete globalThis.navigator;
  }
  delete globalThis.IS_REACT_ACT_ENVIRONMENT;
});

beforeEach(() => {
  apiGet.mockClear();
  apiPatch.mockClear();
  showError.mockClear();
  showSuccess.mockClear();
});

const mappedPlan = {
  managed_by: 'token_plan',
  read_only: true,
  plan: { id: 7, title: 'Mapped', enabled: true },
};

const nativePlan = {
  managed_by: 'native_subscription',
  read_only: false,
  plan: { id: 8, title: 'Native', enabled: true },
};

test('hook guards block mapped edit and status mutations before side effects', async () => {
  let latest;
  const container = document.createElement('div');
  document.body.append(container);
  const root = createRoot(container);

  function Harness() {
    latest = useSubscriptionsData();
    return (
      <output
        data-show-edit={String(latest.showEdit)}
        data-editing-id={latest.editingPlan?.plan?.id ?? ''}
      />
    );
  }

  await act(async () => {
    root.render(<Harness />);
    await Promise.resolve();
  });

  await act(async () => latest.openEdit(mappedPlan));
  expect(container.querySelector('output')?.dataset.showEdit).toBe('false');
  expect(container.querySelector('output')?.dataset.editingId).toBe('');

  await act(async () => latest.setPlanEnabled(mappedPlan, false));
  expect(apiPatch).not.toHaveBeenCalled();

  await act(async () => latest.openEdit(nativePlan));
  expect(container.querySelector('output')?.dataset.showEdit).toBe('true');
  expect(container.querySelector('output')?.dataset.editingId).toBe('8');

  await act(async () => latest.setPlanEnabled(nativePlan, false));
  expect(apiPatch).toHaveBeenCalledWith('/api/subscription/admin/plans/8', {
    enabled: false,
  });

  await act(async () => root.unmount());
  container.remove();
});
