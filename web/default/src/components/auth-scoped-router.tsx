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
import { RouterProvider, type AnyRouter } from '@tanstack/react-router'

import { useAuthStore } from '@/stores/auth-store'

interface AuthScopedRouterProps<TRouter extends AnyRouter> {
  router: TRouter
}

export function AuthScopedRouter<TRouter extends AnyRouter>({
  router,
}: AuthScopedRouterProps<TRouter>) {
  const userId = useAuthStore((state) => state.auth.user?.id ?? null)

  // Clearing the QueryClient removes reusable cache entries; remounting the
  // routed tree also discards mounted observers and open dialogs that could
  // otherwise retain the previous identity's last rendered row in memory.
  return <RouterProvider key={userId ?? 'anonymous'} router={router} />
}
