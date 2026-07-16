import { describe, it, expect } from 'vitest'
import {
  RATIO_USD_FACTOR,
  ratioToDisplayPrice,
  displayPriceToRatio,
  usdPriceToDisplay,
  displayToUsdPrice,
} from './model-pricing-currency'

describe('model-pricing-currency', () => {
  it('ratio 1 ⟷ $2/1M at rate 1 (USD, backward compatible)', () => {
    expect(RATIO_USD_FACTOR).toBe(2)
    expect(ratioToDisplayPrice(1, 1)).toBe(2)
    expect(displayPriceToRatio(2, 1)).toBe(1)
  })

  it('ratio 1 ⟷ ¥14.6/1M at rate 7.3 (CNY)', () => {
    expect(ratioToDisplayPrice(1, 7.3)).toBeCloseTo(14.6, 10)
    expect(displayPriceToRatio(14.6, 7.3)).toBeCloseTo(1, 10)
  })

  it('ratio round-trips through display at rate 7.3 (no 7.3× drift)', () => {
    for (const ratio of [0.25, 1, 2.5, 40]) {
      const shown = ratioToDisplayPrice(ratio, 7.3)
      expect(displayPriceToRatio(shown, 7.3)).toBeCloseTo(ratio, 10)
    }
  })

  it('per-request price round-trips (¥ display ⟷ USD store)', () => {
    for (const usd of [0.01, 0.5, 3]) {
      const shown = usdPriceToDisplay(usd, 7.3)
      expect(displayToUsdPrice(shown, 7.3)).toBeCloseTo(usd, 10)
    }
    expect(usdPriceToDisplay(0.01, 7.3)).toBeCloseTo(0.073, 10)
  })
})
