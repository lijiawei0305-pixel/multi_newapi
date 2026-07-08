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
import { describe, it, expect } from 'vitest'
import type { TFunction } from 'i18next'
import {
  alertLevelMeta,
  clampPct,
  count,
  formatSnapshotTs,
  unusedBarColor,
  unusedPct,
  usd,
} from './index'

// 假的 TFunction：直接返回 defaultValue（无 i18n 目录时也能测中文兜底）。
const fakeT = ((_key: string, opts?: { defaultValue?: string }) =>
  opts?.defaultValue ?? _key) as unknown as TFunction

describe('usd', () => {
  it('正数带 $ 与两位小数', () => {
    expect(usd(1234.5)).toBe('$1,234.50')
  })
  it('负数带前导负号', () => {
    expect(usd(-49.5)).toBe('-$49.50')
  })
  it('字符串数字可解析', () => {
    expect(usd('789.01')).toBe('$789.01')
  })
  it('非法/缺省 → $0.00', () => {
    expect(usd(undefined)).toBe('$0.00')
    expect(usd(null)).toBe('$0.00')
    expect(usd('abc')).toBe('$0.00')
    expect(usd(NaN)).toBe('$0.00')
  })
})

describe('count', () => {
  it('整数千分位、无小数', () => {
    expect(count(12345)).toBe('12,345')
  })
  it('缺省 → 0', () => {
    expect(count(undefined)).toBe('0')
  })
})

describe('clampPct', () => {
  it('区间内原样返回', () => {
    expect(clampPct(42)).toBe(42)
  })
  it('超过 100 夹到 100', () => {
    expect(clampPct(150)).toBe(100)
  })
  it('小于 0 夹到 0', () => {
    expect(clampPct(-5)).toBe(0)
  })
  it('NaN/缺省 → 0', () => {
    expect(clampPct(NaN)).toBe(0)
    expect(clampPct(undefined)).toBe(0)
  })
})

describe('unusedPct', () => {
  it('limit=50 used=0.5 → 未使用 99%', () => {
    expect(unusedPct(50, 0.5)).toBeCloseTo(99)
  })
  it('全用完 → 0%', () => {
    expect(unusedPct(50, 50)).toBe(0)
  })
  it('used>limit（脏数据）夹到 0，不出现负数', () => {
    expect(unusedPct(50, 60)).toBe(0)
  })
  it('limit<=0（无限额/脏数据）→ 0，不除零', () => {
    expect(unusedPct(0, 10)).toBe(0)
    expect(unusedPct(-1, 10)).toBe(0)
  })
})

describe('formatSnapshotTs', () => {
  it('epoch 秒 → 日期串', () => {
    // 2026-06-08 00:00:00 UTC = 1749340800
    expect(formatSnapshotTs(1749340800)).toMatch(/^\d{4}-\d{2}-\d{2}$/)
  })
  it('<=0 / 缺省 → -', () => {
    expect(formatSnapshotTs(0)).toBe('-')
    expect(formatSnapshotTs(undefined)).toBe('-')
  })
})

describe('alertLevelMeta', () => {
  it('warn → warning/预警', () => {
    const m = alertLevelMeta('warn', fakeT)
    expect(m.variant).toBe('warning')
    expect(m.label).toBe('预警')
  })
  it('critical → danger/严重', () => {
    const m = alertLevelMeta('critical', fakeT)
    expect(m.variant).toBe('danger')
    expect(m.label).toBe('严重')
  })
  it('exhausted → danger/已耗尽', () => {
    const m = alertLevelMeta('exhausted', fakeT)
    expect(m.variant).toBe('danger')
    expect(m.label).toBe('已耗尽')
  })
  it('空字符串 → success/正常（健康）', () => {
    const m = alertLevelMeta('', fakeT)
    expect(m.variant).toBe('success')
    expect(m.label).toBe('正常')
  })
  it('null/undefined → success/正常', () => {
    expect(alertLevelMeta(null, fakeT).variant).toBe('success')
    expect(alertLevelMeta(undefined, fakeT).variant).toBe('success')
  })
})

describe('unusedBarColor', () => {
  it('>=90% → destructive', () => {
    expect(unusedBarColor(95)).toBe('bg-destructive')
    expect(unusedBarColor(90)).toBe('bg-destructive')
  })
  it('[75,90) → warning', () => {
    expect(unusedBarColor(80)).toBe('bg-warning')
    expect(unusedBarColor(75)).toBe('bg-warning')
  })
  it('<75 → primary', () => {
    expect(unusedBarColor(10)).toBe('bg-primary')
  })
})
