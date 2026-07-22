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
import { renderToStaticMarkup } from 'react-dom/server'
import { describe, expect, it, vi } from 'vitest'

import { SubscriptionsTable } from './subscriptions-table'

const queryState = vi.hoisted(() => ({
  refetch: vi.fn(),
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({
    t: (key: string) => key,
    i18n: { resolvedLanguage: 'en' },
  }),
}))

vi.mock('@tanstack/react-query', () => ({
  useQuery: () => ({
    data: undefined,
    isLoading: false,
    isError: true,
    isFetching: false,
    error: new Error('Plan ownership is temporarily unavailable.'),
    refetch: queryState.refetch,
  }),
}))

vi.mock('@/components/data-table', () => ({
  DataTablePage: () => <div>table</div>,
  useDataTable: () => ({ table: {} }),
}))

vi.mock('./subscriptions-columns', () => ({
  useSubscriptionsColumns: () => [],
}))

vi.mock('./subscriptions-provider', () => ({
  useSubscriptions: () => ({ refreshTrigger: 0 }),
}))

describe('subscription plan list errors', () => {
  it('renders the ownership-list failure instead of an empty table', () => {
    const markup = renderToStaticMarkup(<SubscriptionsTable />)

    expect(markup).toContain('role="alert"')
    expect(markup).toContain('Failed to load')
    expect(markup).toContain('Plan ownership is temporarily unavailable.')
    expect(markup).toContain('Retry')
    expect(markup).not.toContain('>table<')
  })
})
