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
import { createFileRoute } from '@tanstack/react-router'

import { TenantPlans } from '@/features/tenant-plans'

// Buyer-facing purchase page. Login is already enforced by the
// `/_authenticated` parent guard, so no extra role gate is needed.
export const Route = createFileRoute('/_authenticated/plans/')({
  // ?renew=<套餐id>：满额/到期横幅「立即续费」深链——页面挂载后自动对该套餐发起购买（P3-RNW 降级版）。
  validateSearch: (search: Record<string, unknown>) => {
    const n = Number(search.renew)
    return Number.isFinite(n) && n > 0 ? { renew: n } : {}
  },
  component: TenantPlans,
})
