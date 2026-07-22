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
import { renderToStaticMarkup } from 'react-dom/server';

import SubscriptionPlanOwnershipBadge from './SubscriptionPlanOwnershipBadge';

const translate = (key) => key;

test('mapped subscription rows show their read-only owner', () => {
  const markup = renderToStaticMarkup(
    <SubscriptionPlanOwnershipBadge
      record={{ managed_by: 'token_plan', read_only: true }}
      t={translate}
    />,
  );

  expect(markup).toContain('只读');
  expect(markup).toContain('title="由默认主题的套餐管理维护"');
  expect(markup).toContain('aria-label="由默认主题的套餐管理维护"');
});

test('native subscription rows do not show an ownership badge', () => {
  const markup = renderToStaticMarkup(
    <SubscriptionPlanOwnershipBadge
      record={{ managed_by: 'native_subscription', read_only: false }}
      t={translate}
    />,
  );

  expect(markup).toBe('');
});
