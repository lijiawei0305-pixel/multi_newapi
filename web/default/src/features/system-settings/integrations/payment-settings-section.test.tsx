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
// @vitest-environment jsdom

import {
  act,
  type Dispatch,
  type PropsWithChildren,
  type SetStateAction,
} from 'react'
import { createRoot, type Root } from 'react-dom/client'
import type { UseFormReturn } from 'react-hook-form'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

import {
  PaymentSettingsSection,
  type PaymentFormValues,
} from './payment-settings-section'
import type { WaffoPancakeBinding } from './waffo-pancake-settings-section'

const testState = vi.hoisted(() => ({
  guardWhen: false,
}))

vi.mock('react-i18next', () => ({
  useTranslation: () => ({ t: (key: string) => key }),
}))

vi.mock('@tanstack/react-query', () => ({
  useMutation: () => ({ isPending: false, mutate: vi.fn() }),
  useQueryClient: () => ({ invalidateQueries: vi.fn() }),
}))

vi.mock('@/components/risk-acknowledgement-dialog', () => ({
  RiskAcknowledgementDialog: () => null,
}))

vi.mock('../api', () => ({
  confirmPaymentCompliance: vi.fn(),
}))

vi.mock('../components/form-navigation-guard', () => ({
  FormNavigationGuard: (props: { when: boolean }) => {
    testState.guardWhen = props.when
    return <div data-navigation-guard={String(props.when)} />
  },
}))

vi.mock('../components/settings-form-layout', () => ({
  SettingsForm: (props: PropsWithChildren) => <form>{props.children}</form>,
}))

vi.mock('../components/settings-page-context', () => ({
  SettingsPageFormActions: () => null,
}))

vi.mock('../components/settings-section', () => ({
  SettingsSection: (props: PropsWithChildren) => (
    <section>{props.children}</section>
  ),
}))

vi.mock('../hooks/use-update-option', () => ({
  useUpdateOption: () => ({ isPending: false, mutateAsync: vi.fn() }),
}))

type MockGatewayProps = {
  form: Pick<UseFormReturn<PaymentFormValues>, 'setValue'>
  setWaffoPancakeSelection: Dispatch<SetStateAction<WaffoPancakeBinding>>
}

vi.mock('./payment-gateway-tabs', () => ({
  PaymentGatewayTabs: (props: MockGatewayProps) => (
    <div>
      <button
        type='button'
        data-testid='dirty-form'
        onClick={() =>
          props.form.setValue('StripeApiSecret', 'changed', {
            shouldDirty: true,
          })
        }
      >
        Dirty form
      </button>
      <button
        type='button'
        data-testid='change-pancake-binding'
        onClick={() =>
          props.setWaffoPancakeSelection({
            storeID: 'store-next',
            productID: 'product-next',
          })
        }
      >
        Change binding
      </button>
    </div>
  ),
}))

vi.mock('./waffo-pancake-api', () => ({
  saveWaffoPancakeConfig: vi.fn(),
}))

const paymentDefaults = {
  PayAddress: '',
  EpayId: '',
  EpayKey: '',
  Price: 7.3,
  MinTopUp: 1,
  CustomCallbackAddress: '',
  PayMethods: '',
  AmountOptions: '',
  AmountDiscount: '',
  StripeApiSecret: '',
  StripeWebhookSecret: '',
  StripePriceId: '',
  StripeUnitPrice: 8,
  StripeMinTopUp: 1,
  StripePromotionCodesEnabled: false,
  CreemApiKey: '',
  CreemWebhookSecret: '',
  CreemTestMode: false,
  CreemProducts: '[]',
  WechatPayEnabled: false,
  WechatPayAppID: '',
  WechatPayMchID: '',
  WechatPayAPIv3Key: '',
  WechatPayCertSerial: '',
  WechatPayPrivateKey: '',
  WechatPayPublicKeyID: '',
  WechatPayPublicKey: '',
  AlipayEnabled: false,
  AlipayAppID: '',
  AlipayPrivateKey: '',
  AlipayPublicKey: '',
  AlipaySellerID: '',
  AlipayReturnURL: '',
  AlipaySandbox: false,
}

const waffoDefaults = {
  WaffoEnabled: false,
  WaffoApiKey: '',
  WaffoPrivateKey: '',
  WaffoPublicCert: '',
  WaffoSandboxPublicCert: '',
  WaffoSandboxApiKey: '',
  WaffoSandboxPrivateKey: '',
  WaffoSandbox: false,
  WaffoMerchantId: '',
  WaffoCurrency: 'USD',
  WaffoUnitPrice: 1,
  WaffoMinTopUp: 1,
  WaffoNotifyUrl: '',
  WaffoReturnUrl: '',
  WaffoPayMethods: '[]',
}

let container: HTMLDivElement
let root: Root

async function renderPaymentSettings() {
  await act(async () => {
    root.render(
      <PaymentSettingsSection
        defaultValues={paymentDefaults}
        waffoDefaultValues={waffoDefaults}
        waffoPancakeDefaultValues={{
          WaffoPancakeMerchantID: '',
          WaffoPancakePrivateKey: '',
          WaffoPancakeReturnURL: '',
        }}
        waffoPancakeProvisionedStoreID=''
        waffoPancakeProvisionedProductID=''
        complianceDefaults={{
          confirmed: true,
          termsVersion: 'v1',
          confirmedAt: 0,
          confirmedBy: 0,
        }}
      />
    )
  })
}

beforeEach(() => {
  testState.guardWhen = false
  container = document.createElement('div')
  document.body.append(container)
  root = createRoot(container)
})

afterEach(async () => {
  await act(async () => root.unmount())
  container.remove()
})

describe('payment settings navigation protection', () => {
  it('blocks navigation after a form field becomes dirty', async () => {
    await renderPaymentSettings()
    expect(testState.guardWhen).toBe(false)

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>('[data-testid="dirty-form"]')
        ?.click()
    })

    expect(testState.guardWhen).toBe(true)
  })

  it('blocks navigation when the unsaved Pancake catalog binding changes', async () => {
    await renderPaymentSettings()
    expect(testState.guardWhen).toBe(false)

    await act(async () => {
      container
        .querySelector<HTMLButtonElement>(
          '[data-testid="change-pancake-binding"]'
        )
        ?.click()
    })

    expect(testState.guardWhen).toBe(true)
  })
})
