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

export type UsageLevel = 'none' | 'warn' | 'expiring' | 'expired' | 'exhausted'

export interface UsageAlert {
  level: UsageLevel
  ratio: number
  subscriptionId: number
  resetMarker: number
  /** 原生 subscription_plans.id——一键续费深链反查我们的套餐 id 用（P3-RNW 降级版）。 */
  planId: number
  /** level='expiring' 时的剩余天数（1..7）；其余档位为 0。 */
  daysLeft: number
}

const WARN_THRESHOLD = 0.8
/** 时间维告警窗口：到期前 ≤7 天提醒续费；到期后 ≤7 天宽限内仍提醒，更久不再纠缠。 */
const EXPIRY_WINDOW_SEC = 7 * 86400
const RANK: Record<UsageLevel, number> = {
  none: 0,
  warn: 1,
  expiring: 2,
  expired: 3,
  exhausted: 4,
}

const NONE: UsageAlert = {
  level: 'none',
  ratio: 0,
  subscriptionId: 0,
  resetMarker: 0,
  planId: 0,
  daysLeft: 0,
}

/**
 * 从用户订阅算出告警档位，取最严重的一条（exhausted>expired>expiring>warn>none）。
 *
 * 用量维（amount_total>0 且未时间到期才有意义）：ratio>=1 或 status=exhausted →
 * exhausted；ratio>=0.8 → warn。
 * 时间维（end_time>0 才有时间语义；无限额套餐同样适用）：距 end_time ≤7 天 →
 * expiring（daysLeft=剩余天数，向上取整、最小 1）；已过 end_time（或 status=expired）
 * 且在 7 天宽限窗口内 → expired；过期超 7 天不再提醒（P3-RNW 到期提醒，2026-07-08）。
 *
 * now 为 epoch 秒，由调用方传入（保持纯函数、测试确定性）。
 * resetMarker 取选中记录的 next_reset_time（缺省 0）：后端月度重置是原地清零
 * amount_used（同一条记录），关闭态必须靠它识别"新周期"。planId 为该记录的原生
 * plan_id，供一键续费深链反查。
 */
export function computeUsageAlert(
  records: UserSubscriptionRecord[] | undefined,
  now: number
): UsageAlert {
  let best: UsageAlert = NONE
  for (const r of records ?? []) {
    const s = r?.subscription
    if (!s) continue

    // 用量维。
    let ratio = 0
    let usage: UsageLevel = 'none'
    const timedOut = s.status === 'expired'
    if (s.amount_total > 0 && !timedOut) {
      ratio = s.amount_used / s.amount_total
      usage =
        ratio >= 1 || s.status === 'exhausted'
          ? 'exhausted'
          : ratio >= WARN_THRESHOLD
            ? 'warn'
            : 'none'
    }

    // 时间维。
    let time: UsageLevel = 'none'
    let days = 0
    const end = s.end_time ?? 0
    if (end > 0) {
      if (end <= now || timedOut) {
        if (end >= now - EXPIRY_WINDOW_SEC) time = 'expired'
      } else if (end - now <= EXPIRY_WINDOW_SEC) {
        time = 'expiring'
        days = Math.max(1, Math.ceil((end - now) / 86400))
      }
    }

    const level = RANK[time] > RANK[usage] ? time : usage
    if (level === 'none') continue
    if (
      RANK[level] > RANK[best.level] ||
      (RANK[level] === RANK[best.level] && ratio > best.ratio)
    ) {
      best = {
        level,
        ratio,
        subscriptionId: s.id,
        resetMarker: s.next_reset_time ?? 0,
        planId: s.plan_id ?? 0,
        daysLeft: level === 'expiring' ? days : 0,
      }
    }
  }
  return best
}
