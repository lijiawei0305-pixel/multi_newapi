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
import { useEffect, useState } from 'react'
import { getTenantRechargeMethods, type TenantRechargeMethod } from '../api'

/**
 * useRechargeMethods loads the payment channels a buyer may use
 * (enabled && configured), gating which provider buttons render.
 *
 * `methods` starts as null (loading) so the card can avoid a flash of all
 * buttons before the real availability arrives. On API failure the underlying
 * fetch falls back to both channels to keep existing behavior.
 */
export function useRechargeMethods() {
  const [methods, setMethods] = useState<TenantRechargeMethod[] | null>(null)
  const [loading, setLoading] = useState(true)

  useEffect(() => {
    let cancelled = false
    setLoading(true)
    getTenantRechargeMethods()
      .then((res) => {
        if (!cancelled) setMethods(res.methods)
      })
      .finally(() => {
        if (!cancelled) setLoading(false)
      })
    return () => {
      cancelled = true
    }
  }, [])

  return { methods, loading }
}
