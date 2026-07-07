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
import type { UserSubscriptionRecord } from '../types'

export type UsageLevel = 'none' | 'warn' | 'exhausted'

export interface UsageAlert {
  level: UsageLevel
  ratio: number
  subscriptionId: number
  resetMarker: number
}

const WARN_THRESHOLD = 0.8
const RANK: Record<UsageLevel, number> = { none: 0, warn: 1, exhausted: 2 }

/**
 * 从用户活跃订阅算出满额告警档位。取最严重的一条（exhausted>warn>none）。
 * 规则：status=expired（时间到期，非满额）与 amount_total<=0（无限额）跳过；
 * ratio>=1 或 status=exhausted → exhausted；ratio>=0.8 → warn；否则 none。
 * resetMarker 取自选中记录的 subscription.next_reset_time（缺省 0），用于
 * 让关闭态随计费周期失效——后端月度重置是原地清零 amount_used（同一条记录），
 * 不会产生新 subscriptionId，必须靠 next_reset_time 的变化来识别"新周期"。
 */
export function computeUsageAlert(
  records: UserSubscriptionRecord[] | undefined
): UsageAlert {
  let best: UsageAlert = { level: 'none', ratio: 0, subscriptionId: 0, resetMarker: 0 }
  for (const r of records ?? []) {
    const s = r?.subscription
    if (!s) continue
    if (s.status === 'expired') continue
    if (!(s.amount_total > 0)) continue
    const ratio = s.amount_used / s.amount_total
    const level: UsageLevel =
      ratio >= 1 || s.status === 'exhausted'
        ? 'exhausted'
        : ratio >= WARN_THRESHOLD
          ? 'warn'
          : 'none'
    if (
      RANK[level] > RANK[best.level] ||
      (RANK[level] === RANK[best.level] && ratio > best.ratio)
    ) {
      best = { level, ratio, subscriptionId: s.id, resetMarker: s.next_reset_time ?? 0 }
    }
  }
  return best
}
