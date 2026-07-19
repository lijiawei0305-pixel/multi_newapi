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
import type { TFunction } from 'i18next'

import type { StatusVariant } from '@/components/status-badge'
import dayjs from '@/lib/dayjs'

import type { AlertLevel } from '../types'

// ============================================================================
// Breakage 监控 — 纯展示/计算辅助（无 React、无 IO，可单测）。
// 金额全为 USD（后端已 round2），这里只负责格式化；计数保持整数展示。
// 时间戳容忍 ISO-8601 字符串（overview/detail）或 epoch 秒（snapshots）。
// 所有面向用户的文案通过 `t('English key', { defaultValue: '中文' })` 兜底（W5）。
// ============================================================================

function toFinite(v: number | string | null | undefined): number {
  const n = typeof v === 'string' ? Number(v) : (v ?? 0)
  return Number.isFinite(n) ? (n as number) : 0
}

/** 格式化 USD 金额：`$` + 千分位 + 2 位小数。容忍负数与非法输入。 */
export const usd = (v: number | string | null | undefined): string => {
  const n = toFinite(v)
  const sign = n < 0 ? '-' : ''
  return `${sign}$${Math.abs(n).toLocaleString(undefined, {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  })}`
}

/** 格式化整数计数（千分位、不保留小数）。 */
export const count = (v: number | string | null | undefined): string =>
  toFinite(v).toLocaleString(undefined, { maximumFractionDigits: 0 })

/**
 * 把使用率夹到 [0, 100]。后端已给 usage_pct，这里仅防脏数据越界。
 * NaN / 缺省 → 0。
 */
export function clampPct(v: number | string | null | undefined): number {
  const n = toFinite(v)
  return Math.min(100, Math.max(0, n))
}

/**
 * 由额度上限与已用额算未使用占比（百分比 [0..100]）。
 * limit ≤ 0（无限额/脏数据）→ 0，避免除零。前端一般直接用后端 usage_pct，
 * 此函数供 lib 单测与本地兜底计算。unused = limit − used（clamp≥0）。
 */
export function unusedPct(
  limitUsd: number | string | null | undefined,
  usedUsd: number | string | null | undefined
): number {
  const limit = toFinite(limitUsd)
  if (limit <= 0) return 0
  const used = toFinite(usedUsd)
  const unused = Math.max(0, limit - used)
  return clampPct((unused / limit) * 100)
}

/**
 * 容忍时间格式化：接受 ISO-8601 字符串、epoch 秒、epoch 毫秒。
 * overview/detail 的 period_end 是 ISO；snapshots 的 period_end 是 epoch 秒。
 */
export function formatDate(value: number | string | null | undefined): string {
  if (value === undefined || value === null || value === '') return '-'
  if (typeof value === 'number') {
    const ms = value < 1e12 ? value * 1000 : value
    return dayjs(ms).tz().format('YYYY-MM-DD')
  }
  const d = dayjs(value)
  return d.isValid() ? d.tz().format('YYYY-MM-DD') : String(value)
}

/** 把 epoch 秒的快照锚点渲染成日期标签（用于趋势图 X 轴 / tooltip）。 */
export function formatSnapshotTs(bucketTs: number | null | undefined): string {
  const n = toFinite(bucketTs)
  if (n <= 0) return '-'
  return dayjs(n * 1000)
    .tz()
    .format('YYYY-MM-DD')
}

// ---------------------------------------------------------------------------
// 告警级 → 徽章配色 + 中文标签
// ---------------------------------------------------------------------------

/**
 * 告警级 → StatusBadge variant + i18n 标签。
 * 空/缺省 = 健康（绿色 success）；warn=warning；critical/exhausted=danger。
 * 与 subscription-monitor 的 alertLevelMeta 口径一致。
 */
export function alertLevelMeta(
  level: AlertLevel | '' | null | undefined,
  t: TFunction
): { variant: StatusVariant; label: string } {
  switch (level) {
    case 'warn':
      return {
        variant: 'warning',
        label: t('Warning', { defaultValue: '预警' }),
      }
    case 'critical':
      return {
        variant: 'danger',
        label: t('Critical', { defaultValue: '严重' }),
      }
    case 'exhausted':
      return {
        variant: 'danger',
        label: t('Exhausted', { defaultValue: '已耗尽' }),
      }
    default:
      return {
        variant: 'success',
        label: t('Normal', { defaultValue: '正常' }),
      }
  }
}

/** 明细筛选下拉里可选的告警级（含「全部」由页面单独渲染空值）。 */
export const ALERT_LEVEL_FILTER_OPTIONS: AlertLevel[] = [
  'warn',
  'critical',
  'exhausted',
]

/** 未使用占比越高（沉淀越多）颜色越告警：用于进度条底色。 */
export function unusedBarColor(pct: number): string {
  if (pct >= 90) return 'bg-destructive'
  if (pct >= 75) return 'bg-warning'
  return 'bg-primary'
}
