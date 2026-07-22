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
import { Code2, Eye, ShieldAlert } from 'lucide-react'
import type { Dispatch, SetStateAction } from 'react'
import type { UseFormReturn } from 'react-hook-form'
import { useTranslation } from 'react-i18next'

import { IconAlipay, IconWeChat } from '@/assets/brand-icons'
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import {
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Tabs, TabsContent, TabsList, TabsTrigger } from '@/components/ui/tabs'
import { Textarea } from '@/components/ui/textarea'

import {
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { safeNumberFieldProps } from '../utils/numeric-field'
import { AdvancedSubscriptionPlansLink } from './advanced-subscription-plans-link'
import { AmountDiscountVisualEditor } from './amount-discount-visual-editor'
import { AmountOptionsVisualEditor } from './amount-options-visual-editor'
import { CreemProductsVisualEditor } from './creem-products-visual-editor'
import { PaymentMethodsVisualEditor } from './payment-methods-visual-editor'
import type { PaymentFormValues } from './payment-settings-section'
import {
  WaffoPancakeSettingsSection,
  type WaffoPancakeBinding,
  type WaffoPancakeSettingsValues,
} from './waffo-pancake-settings-section'
import {
  type PayMethod,
  WaffoSettingsSection,
  type WaffoSettingsValues,
} from './waffo-settings-section'

const paymentTabContentClassName = 'mt-6 min-w-0'

type WaffoFormFieldValues = Omit<WaffoSettingsValues, 'WaffoPayMethods'>

type PaymentGatewayTabsProps = {
  form: UseFormReturn<PaymentFormValues>
  payMethodsVisualMode: boolean
  setPayMethodsVisualMode: Dispatch<SetStateAction<boolean>>
  amountOptionsVisualMode: boolean
  setAmountOptionsVisualMode: Dispatch<SetStateAction<boolean>>
  amountDiscountVisualMode: boolean
  setAmountDiscountVisualMode: Dispatch<SetStateAction<boolean>>
  creemProductsVisualMode: boolean
  setCreemProductsVisualMode: Dispatch<SetStateAction<boolean>>
  waffoPancakeDefaultValues: WaffoPancakeSettingsValues
  waffoPancakeValues: WaffoPancakeSettingsValues
  setWaffoPancakeValue: <K extends keyof WaffoPancakeSettingsValues>(
    key: K,
    value: WaffoPancakeSettingsValues[K]
  ) => void
  waffoPancakeSelection: WaffoPancakeBinding
  waffoPancakeSavedBinding: WaffoPancakeBinding
  setWaffoPancakeSelection: Dispatch<SetStateAction<WaffoPancakeBinding>>
  waffoValues: WaffoSettingsValues
  setWaffoValue: <K extends keyof WaffoFormFieldValues>(
    key: K,
    value: WaffoFormFieldValues[K]
  ) => void
  waffoPayMethods: PayMethod[]
  setWaffoPayMethods: Dispatch<SetStateAction<PayMethod[]>>
}

export function PaymentGatewayTabs(props: PaymentGatewayTabsProps) {
  const { t } = useTranslation()
  const form = props.form
  const payMethodsVisualMode = props.payMethodsVisualMode
  const setPayMethodsVisualMode = props.setPayMethodsVisualMode
  const amountOptionsVisualMode = props.amountOptionsVisualMode
  const setAmountOptionsVisualMode = props.setAmountOptionsVisualMode
  const amountDiscountVisualMode = props.amountDiscountVisualMode
  const setAmountDiscountVisualMode = props.setAmountDiscountVisualMode
  const creemProductsVisualMode = props.creemProductsVisualMode
  const setCreemProductsVisualMode = props.setCreemProductsVisualMode
  const waffoPancakeDefaultValues = props.waffoPancakeDefaultValues
  const waffoPancakeValues = props.waffoPancakeValues
  const setWaffoPancakeValue = props.setWaffoPancakeValue
  const waffoPancakeSelection = props.waffoPancakeSelection
  const waffoPancakeSavedBinding = props.waffoPancakeSavedBinding
  const setWaffoPancakeSelection = props.setWaffoPancakeSelection
  const waffoValues = props.waffoValues
  const setWaffoValue = props.setWaffoValue
  const waffoPayMethods = props.waffoPayMethods
  const setWaffoPayMethods = props.setWaffoPayMethods

  return (
    <Tabs defaultValue='general' className='min-w-0'>
      <div className='overflow-x-auto pb-1'>
        <TabsList className='grid min-w-[58rem] grid-cols-8'>
          <TabsTrigger value='general'>{t('General')}</TabsTrigger>
          <TabsTrigger value='wxpay'>{t('WeChat Pay')}</TabsTrigger>
          <TabsTrigger value='alipay'>{t('Alipay')}</TabsTrigger>
          <TabsTrigger value='epay'>Epay</TabsTrigger>
          <TabsTrigger value='stripe'>{t('Stripe')}</TabsTrigger>
          <TabsTrigger value='creem'>Creem</TabsTrigger>
          <TabsTrigger value='waffo-pancake'>Waffo Pancake</TabsTrigger>
          <TabsTrigger value='waffo'>Waffo</TabsTrigger>
        </TabsList>
      </div>

      <TabsContent value='general' className={paymentTabContentClassName}>
        <div className='space-y-4'>
          <div>
            <h3 className='text-lg font-medium'>{t('General Settings')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t('Shared configuration for all payment gateways')}
            </p>
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='Price'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Price (local currency / USD)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.01'
                      min={0}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'How much to charge for each US dollar of balance (Epay)'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='MinTopUp'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum top-up (USD)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.01'
                      min={0}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Smallest USD amount users can recharge (Epay)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='PayMethods'
            render={({ field }) => (
              <FormItem>
                <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                  <FormLabel>{t('Payment methods')}</FormLabel>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      setPayMethodsVisualMode(!payMethodsVisualMode)
                    }
                    className='w-full sm:w-auto'
                  >
                    {payMethodsVisualMode ? (
                      <>
                        <Code2 className='mr-2 h-3 w-3' />
                        {t('JSON Editor')}
                      </>
                    ) : (
                      <>
                        <Eye className='mr-2 h-3 w-3' />
                        {t('Visual Editor')}
                      </>
                    )}
                  </Button>
                </div>
                <FormControl>
                  {payMethodsVisualMode ? (
                    <PaymentMethodsVisualEditor
                      value={field.value}
                      onChange={field.onChange}
                    />
                  ) : (
                    <Textarea
                      rows={4}
                      placeholder={t('Payment methods JSON example', {
                        defaultValue:
                          '[{"name":"支付宝","type":"alipay","icon":"SiAlipay"}]',
                      })}
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  )}
                </FormControl>
                <FormDescription>
                  {t(
                    'Configured as PayMethods JSON. The type value decides which payment flow is used: stripe for Stripe, waffo_pancake for Waffo Pancake, and other values are sent to Epay as the type parameter.'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='grid gap-6 md:grid-cols-2 md:items-start'>
            <FormField
              control={form.control}
              name='AmountOptions'
              render={({ field }) => (
                <FormItem>
                  <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <FormLabel>{t('Top-up amount options')}</FormLabel>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        setAmountOptionsVisualMode(!amountOptionsVisualMode)
                      }
                      className='w-full sm:w-auto'
                    >
                      {amountOptionsVisualMode ? (
                        <>
                          <Code2 className='mr-2 h-3 w-3' />
                          {t('JSON Editor')}
                        </>
                      ) : (
                        <>
                          <Eye className='mr-2 h-3 w-3' />
                          {t('Visual Editor')}
                        </>
                      )}
                    </Button>
                  </div>
                  <FormControl>
                    {amountOptionsVisualMode ? (
                      <AmountOptionsVisualEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    ) : (
                      <Textarea
                        rows={4}
                        placeholder='[10, 20, 50, 100]'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    )}
                  </FormControl>
                  <FormDescription>
                    {t('Preset recharge amounts (JSON array)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AmountDiscount'
              render={({ field }) => (
                <FormItem>
                  <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                    <FormLabel>{t('Amount discount')}</FormLabel>
                    <Button
                      type='button'
                      variant='outline'
                      size='sm'
                      onClick={() =>
                        setAmountDiscountVisualMode(!amountDiscountVisualMode)
                      }
                      className='w-full sm:w-auto'
                    >
                      {amountDiscountVisualMode ? (
                        <>
                          <Code2 className='mr-2 h-3 w-3' />
                          {t('JSON Editor')}
                        </>
                      ) : (
                        <>
                          <Eye className='mr-2 h-3 w-3' />
                          {t('Visual Editor')}
                        </>
                      )}
                    </Button>
                  </div>
                  <FormControl>
                    {amountDiscountVisualMode ? (
                      <AmountDiscountVisualEditor
                        value={field.value}
                        onChange={field.onChange}
                      />
                    ) : (
                      <Textarea
                        rows={4}
                        placeholder='{"100":0.95,"200":0.9}'
                        {...field}
                        onChange={(event) => field.onChange(event.target.value)}
                      />
                    )}
                  </FormControl>
                  <FormDescription>
                    {t('Discount map by recharge amount (JSON object)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </div>
      </TabsContent>

      <TabsContent value='wxpay' className={paymentTabContentClassName}>
        <div className='space-y-4'>
          <div>
            <h3 className='flex items-center gap-2 text-lg font-medium'>
              <IconWeChat className='h-5 w-5' style={{ color: '#07C160' }} />
              {t('WeChat Pay Gateway')}
            </h3>
            <p className='text-muted-foreground text-sm'>
              {t('Configuration for WeChat Pay (Native) integration')}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Once credentials are filled and the channel is enabled here, it appears automatically for buyers on the recharge and plan-purchase pages — no need to add it under Add payment method.'
              )}
            </p>
          </div>

          <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
            <p className='mb-2 font-medium'>{t('Callback Configuration:')}</p>
            <ul className='list-inside list-disc space-y-1'>
              <li>
                {t('Callback URL:')}{' '}
                <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                  {'<ServerAddress>/api/pay/wechat/notify'}
                </code>
              </li>
              <li>
                {t(
                  'Register this notify address in the merchant / gateway dashboard.'
                )}
              </li>
            </ul>
          </div>

          <FormField
            control={form.control}
            name='WechatPayEnabled'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Enable WeChat Pay')}</FormLabel>
                  <FormDescription>
                    {t('Enable WeChat Pay as a buyer recharge method')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='WechatPayAppID'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('App ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='wx8888888888888888'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('WeChat Pay App ID (official account / mini program)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='WechatPayMchID'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Merchant ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='1230000109'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('WeChat Pay merchant number (mch_id)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='WechatPayAPIv3Key'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('APIv3 Key')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('Enter new key to update')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'APIv3 key for callback decryption (leave blank unless updating)'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='WechatPayCertSerial'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Certificate serial number')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='1DDE55AD98...'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Merchant API certificate serial number')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='WechatPayPrivateKey'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Merchant private key (PEM)')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={6}
                    className='font-mono text-xs'
                    placeholder={
                      '-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----'
                    }
                    autoComplete='off'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Paste PEM content; leave blank to keep the current key')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <div className='rounded-md bg-amber-50 p-4 text-sm text-amber-900 dark:bg-amber-950 dark:text-amber-100'>
            <p>
              {t(
                'WeChat Pay requires merchant accounts created since 2024 to use "WeChat Pay Public Key" mode instead of platform certificates. If order creation fails with RESOURCE_NOT_EXISTS / "no available platform certificate", fill in the two fields below (Merchant Platform → Account Center → API Security → WeChat Pay Public Key).'
              )}
            </p>
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='WechatPayPublicKeyID'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('WeChat Pay Public Key ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='PUB_KEY_ID_0123456789...'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'The publicKeyID shown next to the WeChat Pay public key in the merchant platform'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='WechatPayPublicKey'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('WeChat Pay public key (PEM)')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={6}
                    className='font-mono text-xs'
                    placeholder={
                      '-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----'
                    }
                    autoComplete='off'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Paste PEM content; leave blank to keep the current key')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      </TabsContent>

      <TabsContent value='alipay' className={paymentTabContentClassName}>
        <div className='space-y-4'>
          <div>
            <h3 className='flex items-center gap-2 text-lg font-medium'>
              <IconAlipay className='h-5 w-5' style={{ color: '#1677FF' }} />
              {t('Alipay Gateway')}
            </h3>
            <p className='text-muted-foreground text-sm'>
              {t('Configuration for Alipay payment integration')}
            </p>
            <p className='text-muted-foreground mt-1 text-xs'>
              {t(
                'Once credentials are filled and the channel is enabled here, it appears automatically for buyers on the recharge and plan-purchase pages — no need to add it under Add payment method.'
              )}
            </p>
          </div>

          <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
            <p className='mb-2 font-medium'>{t('Callback Configuration:')}</p>
            <ul className='list-inside list-disc space-y-1'>
              <li>
                {t('Callback URL:')}{' '}
                <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                  {'<ServerAddress>/api/pay/alipay/notify'}
                </code>
              </li>
              <li>
                {t(
                  'Register this notify address in the merchant / gateway dashboard.'
                )}
              </li>
            </ul>
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='AlipayEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Enable Alipay')}</FormLabel>
                    <FormDescription>
                      {t('Enable Alipay as a buyer recharge method')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />

            <FormField
              control={form.control}
              name='AlipaySandbox'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Sandbox mode')}</FormLabel>
                    <FormDescription>
                      {t('Use the Alipay sandbox environment')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='AlipayAppID'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('App ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='2021000000000000'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>{t('Alipay App ID')}</FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='AlipaySellerID'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Seller ID (optional)')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='2088000000000000'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Optional seller account ID; leave blank to use the app account'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='AlipayReturnURL'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Return URL')}</FormLabel>
                <FormControl>
                  <Input
                    placeholder={t('https://your-site.com/pay/return')}
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('URL buyers return to after completing the payment')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='AlipayPrivateKey'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('App private key (PEM)')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={6}
                    className='font-mono text-xs'
                    placeholder={
                      '-----BEGIN PRIVATE KEY-----\n...\n-----END PRIVATE KEY-----'
                    }
                    autoComplete='off'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t(
                    'Application private key (PEM, leave blank unless updating)'
                  )}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name='AlipayPublicKey'
            render={({ field }) => (
              <FormItem>
                <FormLabel>{t('Alipay public key (PEM)')}</FormLabel>
                <FormControl>
                  <Textarea
                    rows={6}
                    className='font-mono text-xs'
                    placeholder={
                      '-----BEGIN PUBLIC KEY-----\n...\n-----END PUBLIC KEY-----'
                    }
                    autoComplete='off'
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Alipay public key (PEM, leave blank unless updating)')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      </TabsContent>

      <TabsContent value='epay' className={paymentTabContentClassName}>
        <div className='space-y-4'>
          <div>
            <h3 className='text-lg font-medium'>{t('Epay Gateway')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t('Configuration for Epay payment integration')}
            </p>
          </div>

          <Alert>
            <ShieldAlert className='h-4 w-4' />
            <AlertTitle>{t('Epay safety reminder')}</AlertTitle>
            <AlertDescription>
              {t(
                'Epay is a payment protocol, not a specific official website. Verify the provider yourself and do not trust random third-party Epay deployments.'
              )}
            </AlertDescription>
          </Alert>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='PayAddress'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Epay endpoint')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('https://pay.example.com')}
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Base address provided by your Epay service')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='CustomCallbackAddress'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Callback address')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('https://gateway.example.com')}
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t(
                      'Only enter the site origin, for example https://api.example.com. Do not include any path such as /api/user/epay/notify. Leave blank to use the server address.'
                    )}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='EpayId'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Epay merchant ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder='10001'
                      autoComplete='off'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='EpayKey'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Epay secret key')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('Enter new key to update')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Leave blank unless rotating the secret')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>
        </div>
      </TabsContent>

      <TabsContent value='stripe' className={paymentTabContentClassName}>
        <div className='space-y-4'>
          <div>
            <h3 className='text-lg font-medium'>{t('Stripe Gateway')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t('Configuration for Stripe payment integration')}
            </p>
          </div>

          <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
            <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
            <ul className='list-inside list-disc space-y-1'>
              <li>
                {t('Webhook URL:')}{' '}
                <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                  {'<ServerAddress>/api/stripe/webhook'}
                </code>
              </li>
              <li>
                {t('Required events:')}{' '}
                <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                  {t('checkout.session.completed')}
                </code>{' '}
                {t('and')}{' '}
                <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                  {t('checkout.session.expired')}
                </code>
              </li>
              <li>
                {t('Configure at:')}{' '}
                <a
                  href='https://dashboard.stripe.com/developers'
                  target='_blank'
                  rel='noreferrer'
                  className='underline hover:no-underline'
                >
                  {t('Stripe Dashboard')}
                </a>
              </li>
            </ul>
          </div>

          <Alert>
            <AlertDescription className='flex flex-col gap-3'>
              {t(
                'Stripe/Creem requires creating products on the third-party platform and entering the ID'
              )}
              <AdvancedSubscriptionPlansLink />
            </AlertDescription>
          </Alert>

          <div className='grid gap-6 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='StripeApiSecret'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('API secret')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('sk_xxx or rk_xxx')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Stripe API key (leave blank unless updating)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='StripeWebhookSecret'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Webhook secret')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('whsec_xxx')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Webhook signing secret (leave blank unless updating)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='StripePriceId'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Price ID')}</FormLabel>
                  <FormControl>
                    <Input
                      placeholder={t('price_xxx')}
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Stripe product price ID')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <div className='grid gap-6 md:grid-cols-3'>
            <FormField
              control={form.control}
              name='StripeUnitPrice'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>
                    {t('Unit price (local currency / USD)')}
                  </FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.01'
                      min={0}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('e.g., 8 means 8 local currency per USD')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='StripeMinTopUp'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Minimum top-up (USD)')}</FormLabel>
                  <FormControl>
                    <Input
                      type='number'
                      step='0.01'
                      min={0}
                      {...safeNumberFieldProps(field)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Minimum recharge amount in USD')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='StripePromotionCodesEnabled'
              render={({ field }) => (
                <SettingsSwitchItem>
                  <SettingsSwitchContent>
                    <FormLabel>{t('Promotion codes')}</FormLabel>
                    <FormDescription>
                      {t('Allow users to enter promo codes')}
                    </FormDescription>
                  </SettingsSwitchContent>
                  <FormControl>
                    <Switch
                      checked={field.value}
                      onCheckedChange={field.onChange}
                    />
                  </FormControl>
                </SettingsSwitchItem>
              )}
            />
          </div>
        </div>
      </TabsContent>

      <TabsContent value='creem' className={paymentTabContentClassName}>
        <div className='space-y-4'>
          <div>
            <h3 className='text-lg font-medium'>{t('Creem Gateway')}</h3>
            <p className='text-muted-foreground text-sm'>
              {t('Configuration for Creem payment integration')}
            </p>
          </div>

          <div className='rounded-md bg-blue-50 p-4 text-sm text-blue-900 dark:bg-blue-950 dark:text-blue-100'>
            <p className='mb-2 font-medium'>{t('Webhook Configuration:')}</p>
            <ul className='list-inside list-disc space-y-1'>
              <li>
                {t('Webhook URL:')}{' '}
                <code className='rounded bg-blue-100 px-1 py-0.5 text-xs dark:bg-blue-900'>
                  {'<ServerAddress>/api/creem/webhook'}
                </code>
              </li>
              <li>{t('Configure in your Creem dashboard')}</li>
            </ul>
          </div>

          <Alert>
            <AlertDescription className='flex flex-col gap-3'>
              {t(
                'Stripe/Creem requires creating products on the third-party platform and entering the ID'
              )}
              <AdvancedSubscriptionPlansLink />
            </AlertDescription>
          </Alert>

          <div className='grid gap-6 md:grid-cols-2'>
            <FormField
              control={form.control}
              name='CreemApiKey'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('API Key')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('Enter Creem API key')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Creem API key (leave blank unless updating)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />

            <FormField
              control={form.control}
              name='CreemWebhookSecret'
              render={({ field }) => (
                <FormItem>
                  <FormLabel>{t('Webhook Secret')}</FormLabel>
                  <FormControl>
                    <Input
                      type='password'
                      placeholder={t('Enter webhook secret')}
                      autoComplete='new-password'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  </FormControl>
                  <FormDescription>
                    {t('Webhook signing secret (leave blank unless updating)')}
                  </FormDescription>
                  <FormMessage />
                </FormItem>
              )}
            />
          </div>

          <FormField
            control={form.control}
            name='CreemTestMode'
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>{t('Test Mode')}</FormLabel>
                  <FormDescription>
                    {t('Enable test mode for Creem payments')}
                  </FormDescription>
                </SettingsSwitchContent>
                <FormControl>
                  <Switch
                    checked={field.value}
                    onCheckedChange={field.onChange}
                  />
                </FormControl>
              </SettingsSwitchItem>
            )}
          />

          <FormField
            control={form.control}
            name='CreemProducts'
            render={({ field }) => (
              <FormItem>
                <div className='mb-2 flex flex-col gap-2 sm:flex-row sm:items-center sm:justify-between'>
                  <FormLabel>{t('Products')}</FormLabel>
                  <Button
                    type='button'
                    variant='outline'
                    size='sm'
                    onClick={() =>
                      setCreemProductsVisualMode(!creemProductsVisualMode)
                    }
                    className='w-full sm:w-auto'
                  >
                    {creemProductsVisualMode ? (
                      <>
                        <Code2 className='mr-2 h-3 w-3' />
                        {t('JSON Editor')}
                      </>
                    ) : (
                      <>
                        <Eye className='mr-2 h-3 w-3' />
                        {t('Visual Editor')}
                      </>
                    )}
                  </Button>
                </div>
                <FormControl>
                  {creemProductsVisualMode ? (
                    <CreemProductsVisualEditor
                      value={field.value}
                      onChange={field.onChange}
                    />
                  ) : (
                    <Textarea
                      rows={4}
                      placeholder='[{"name":"Basic","productId":"prod_xxx","price":10,"quota":500000,"currency":"USD"}]'
                      {...field}
                      onChange={(event) => field.onChange(event.target.value)}
                    />
                  )}
                </FormControl>
                <FormDescription>
                  {t('Configure Creem products. Provide a JSON array.')}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </div>
      </TabsContent>

      <TabsContent value='waffo-pancake' className={paymentTabContentClassName}>
        <WaffoPancakeSettingsSection
          defaultValues={waffoPancakeDefaultValues}
          values={waffoPancakeValues}
          onValueChange={setWaffoPancakeValue}
          selectedBinding={waffoPancakeSelection}
          savedBinding={waffoPancakeSavedBinding}
          onSelectedBindingChange={setWaffoPancakeSelection}
        />
      </TabsContent>

      <TabsContent value='waffo' className={paymentTabContentClassName}>
        <WaffoSettingsSection
          values={waffoValues}
          onValueChange={setWaffoValue}
          payMethods={waffoPayMethods}
          onPayMethodsChange={setWaffoPayMethods}
        />
      </TabsContent>
    </Tabs>
  )
}
