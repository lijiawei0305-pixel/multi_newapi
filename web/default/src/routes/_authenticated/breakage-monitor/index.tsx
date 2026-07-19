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
import { createFileRoute, redirect } from '@tanstack/react-router'

import { BreakageMonitor } from '@/features/breakage-monitor'
import { ROLE } from '@/lib/roles'
import { useAuthStore } from '@/stores/auth-store'

// breakage 监控（额度沉淀）。管理员可见：主站看全平台，命中代理租户则隔离本租户
// （作用域由后端 tenantFrom(c) 决定；前端仅按 admin 角色门禁）。
export const Route = createFileRoute('/_authenticated/breakage-monitor/')({
  beforeLoad: () => {
    const { auth } = useAuthStore.getState()
    if (!auth.user || auth.user.role < ROLE.ADMIN) {
      throw redirect({ to: '/403' })
    }
  },
  component: BreakageMonitor,
})
