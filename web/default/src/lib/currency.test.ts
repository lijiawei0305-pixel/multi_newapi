import { describe, it, expect, beforeEach } from 'vitest'
import { useSystemConfigStore } from '../stores/system-config-store'
import { getEffectiveBillingRate } from './currency'

function setCurrency(partial: Record<string, unknown>) {
  useSystemConfigStore.setState({
    config: {
      currency: {
        quotaDisplayType: 'USD',
        usdExchangeRate: 7.3,
        quotaPerUnit: 500000,
        customCurrencyExchangeRate: 1,
        customCurrencySymbol: '¤',
        ...partial,
      },
    },
  } as never)
}

describe('getEffectiveBillingRate', () => {
  beforeEach(() => setCurrency({}))

  it('returns 1 in USD mode', () => {
    setCurrency({ quotaDisplayType: 'USD' })
    expect(getEffectiveBillingRate()).toBe(1)
  })

  it('returns usdExchangeRate in CNY mode', () => {
    setCurrency({ quotaDisplayType: 'CNY', usdExchangeRate: 7.3 })
    expect(getEffectiveBillingRate()).toBe(7.3)
  })

  it('returns customCurrencyExchangeRate in CUSTOM mode', () => {
    setCurrency({ quotaDisplayType: 'CUSTOM', customCurrencyExchangeRate: 0.9 })
    expect(getEffectiveBillingRate()).toBe(0.9)
  })

  it('returns 1 in TOKENS mode (billing never tokenized)', () => {
    setCurrency({ quotaDisplayType: 'TOKENS' })
    expect(getEffectiveBillingRate()).toBe(1)
  })
})
