import type { UserSubscriptionRecord } from '../types'

export type UsageLevel = 'none' | 'warn' | 'exhausted'

export interface UsageAlert {
  level: UsageLevel
  ratio: number
  subscriptionId: number
}

const WARN_THRESHOLD = 0.8
const RANK: Record<UsageLevel, number> = { none: 0, warn: 1, exhausted: 2 }

/**
 * 从用户活跃订阅算出满额告警档位。取最严重的一条（exhausted>warn>none）。
 * 规则：status=expired（时间到期，非满额）与 amount_total<=0（无限额）跳过；
 * ratio>=1 或 status=exhausted → exhausted；ratio>=0.8 → warn；否则 none。
 */
export function computeUsageAlert(
  records: UserSubscriptionRecord[] | undefined
): UsageAlert {
  let best: UsageAlert = { level: 'none', ratio: 0, subscriptionId: 0 }
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
      best = { level, ratio, subscriptionId: s.id }
    }
  }
  return best
}
