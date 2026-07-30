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
import type { QueryClient } from '@tanstack/react-query'

import { rotateAuthRequestScope } from '@/lib/api'
import { useAuthStore } from '@/stores/auth-store'

/**
 * Treat every authenticated user ID as a separate React Query cache scope.
 * Password, Passkey, 2FA, WeChat, and OAuth login flows all converge on the
 * auth store, so binding once at app startup covers logout, session expiry,
 * re-login, and direct account switches without duplicating flow-specific
 * cleanup. Same-user profile refreshes keep their cache.
 */
export function bindAuthQueryCacheIsolation(
  queryClient: QueryClient
): () => void {
  return useAuthStore.subscribe((state, previousState) => {
    const userId = state.auth.user?.id ?? null
    const previousUserId = previousState.auth.user?.id ?? null

    if (userId === previousUserId) return

    rotateAuthRequestScope()
    queryClient.clear()
  })
}
