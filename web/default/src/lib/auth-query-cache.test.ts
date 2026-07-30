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
import { QueryClient, QueryObserver } from '@tanstack/react-query'
import axios from 'axios'
import { afterEach, describe, expect, it } from 'vitest'

import { useAuthStore } from '@/stores/auth-store'

import { api } from './api'
import { bindAuthQueryCacheIsolation } from './auth-query-cache'

function setUser(id: number, username = `user-${id}`) {
  useAuthStore.getState().auth.setUser({ id, username, role: 1 })
}

function seedSensitiveQueries(queryClient: QueryClient) {
  queryClient.setQueryData(['agent-context'], { is_agent_owner: true })
  queryClient.setQueryData(['tenant-earnings'], { withdrawable_cny: 88 })
  queryClient.setQueryData(['payout-account'], { payout_account: 'secret' })
  queryClient.setQueryData(['tenant-withdrawals'], [{ id: 7 }])
}

afterEach(() => {
  useAuthStore.getState().auth.reset()
})

describe('auth-scoped React Query cache', () => {
  it('clears every sensitive query on session expiry and again on re-login', () => {
    setUser(1, 'old-user')
    const queryClient = new QueryClient()
    const unbind = bindAuthQueryCacheIsolation(queryClient)

    seedSensitiveQueries(queryClient)
    useAuthStore.getState().auth.reset()

    expect(queryClient.getQueryCache().getAll()).toHaveLength(0)

    seedSensitiveQueries(queryClient)
    setUser(2, 'new-user')

    expect(queryClient.getQueryCache().getAll()).toHaveLength(0)
    unbind()
  })

  it('clears cached tenant data on a direct authenticated account switch', () => {
    setUser(10)
    const queryClient = new QueryClient()
    const unbind = bindAuthQueryCacheIsolation(queryClient)
    seedSensitiveQueries(queryClient)

    setUser(11)

    expect(queryClient.getQueryCache().getAll()).toHaveLength(0)
    unbind()
  })

  it('keeps cache when refreshing profile data for the same user', () => {
    setUser(20, 'before-refresh')
    const queryClient = new QueryClient()
    const unbind = bindAuthQueryCacheIsolation(queryClient)
    seedSensitiveQueries(queryClient)

    setUser(20, 'after-refresh')

    expect(queryClient.getQueryData(['agent-context'])).toEqual({
      is_agent_owner: true,
    })
    expect(queryClient.getQueryCache().getAll()).toHaveLength(4)
    unbind()
  })

  it('drops mounted sensitive data when the observer moves to the next user scope', () => {
    setUser(30)
    const queryClient = new QueryClient()
    const unbind = bindAuthQueryCacheIsolation(queryClient)
    queryClient.setQueryData(['tenant-payout-account', 30], {
      payout_account: 'old-secret',
    })
    const observer = new QueryObserver(queryClient, {
      queryKey: ['tenant-payout-account', 30],
      queryFn: async () => ({ payout_account: 'old-secret' }),
    })
    const unsubscribe = observer.subscribe(() => {})
    expect(observer.getCurrentResult().data).toEqual({
      payout_account: 'old-secret',
    })

    setUser(31)
    observer.setOptions({
      queryKey: ['tenant-payout-account', 31],
      queryFn: () => new Promise(() => {}),
    })

    expect(observer.getCurrentResult().data).toBeUndefined()
    unsubscribe()
    unbind()
    queryClient.clear()
  })

  it('does not deduplicate or apply a response from an expired auth session', async () => {
    setUser(40)
    const queryClient = new QueryClient()
    const unbind = bindAuthQueryCacheIsolation(queryClient)
    let firstStarted!: () => void
    let releaseFirst!: () => void
    let secondStarted!: () => void
    let releaseSecond!: () => void
    const firstStartedPromise = new Promise<void>((resolve) => {
      firstStarted = resolve
    })
    const firstGate = new Promise<void>((resolve) => {
      releaseFirst = resolve
    })
    const secondStartedPromise = new Promise<void>((resolve) => {
      secondStarted = resolve
    })
    const secondGate = new Promise<void>((resolve) => {
      releaseSecond = resolve
    })

    try {
      const first = api.get('/api/test/auth-scoped-dedupe', {
        adapter: async (config) => {
          firstStarted()
          await firstGate
          return {
            data: { owner: 40 },
            status: 200,
            statusText: 'OK',
            headers: {},
            config,
          }
        },
      })
      await firstStartedPromise

      setUser(41)
      const second = api.get('/api/test/auth-scoped-dedupe', {
        adapter: async (config) => {
          secondStarted()
          await secondGate
          return {
            data: { owner: 41 },
            status: 200,
            statusText: 'OK',
            headers: {},
            config,
          }
        },
      })
      await secondStartedPromise
      releaseSecond()
      expect((await second).data).toEqual({ owner: 41 })

      releaseFirst()
      try {
        await first
        expect.fail('the old-session response must be canceled')
      } catch (error) {
        expect(axios.isCancel(error)).toBe(true)
      }
    } finally {
      releaseFirst()
      releaseSecond()
      unbind()
      queryClient.clear()
    }
  })

  it('does not let an old 401 log out the newly authenticated user', async () => {
    setUser(50)
    const queryClient = new QueryClient()
    const unbind = bindAuthQueryCacheIsolation(queryClient)
    let releaseOldRequest!: () => void
    const oldRequestGate = new Promise<void>((resolve) => {
      releaseOldRequest = resolve
    })

    try {
      const oldRequest = api.get('/api/test/stale-unauthorized', {
        adapter: async (config) => {
          await oldRequestGate
          throw new axios.AxiosError(
            'Unauthorized',
            axios.AxiosError.ERR_BAD_REQUEST,
            config,
            undefined,
            {
              data: { message: 'expired' },
              status: 401,
              statusText: 'Unauthorized',
              headers: {},
              config,
            }
          )
        },
      })

      setUser(51)
      releaseOldRequest()

      try {
        await oldRequest
        expect.fail('the expired-session 401 must be canceled')
      } catch (error) {
        expect(axios.isCancel(error)).toBe(true)
      }
      expect(useAuthStore.getState().auth.user?.id).toBe(51)
    } finally {
      releaseOldRequest()
      unbind()
      queryClient.clear()
    }
  })
})
