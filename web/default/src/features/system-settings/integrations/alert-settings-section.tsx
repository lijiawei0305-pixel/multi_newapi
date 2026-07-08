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
import { useEffect, useMemo, useRef } from 'react'
import * as z from 'zod'
import { useForm } from 'react-hook-form'
import { zodResolver } from '@hookform/resolvers/zod'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'
import {
  Form,
  FormControl,
  FormDescription,
  FormField,
  FormItem,
  FormLabel,
  FormMessage,
} from '@/components/ui/form'
import { Input } from '@/components/ui/input'
import { Switch } from '@/components/ui/switch'
import { Textarea } from '@/components/ui/textarea'
import {
  SettingsForm,
  SettingsSwitchContent,
  SettingsSwitchItem,
} from '../components/settings-form-layout'
import { SettingsPageFormActions } from '../components/settings-page-context'
import { SettingsSection } from '../components/settings-section'
import { useResetForm } from '../hooks/use-reset-form'
import { useUpdateOption } from '../hooks/use-update-option'

/**
 * breakage 告警配置区块（系统设置 → 计费/告警，与「支付」并列）。
 *
 * 四个 option 键与后端 internal/alert/alert.go 定义的常量严格一致
 * （单一真相源），前端表单字段名 = Core-C LoadConfig 读取键：
 *   - breakage_alert_enabled       "true"/"false"  → Config.Enabled
 *   - breakage_alert_email         逗号/换行分隔    → Config.Email []string
 *   - breakage_alert_webhook_url   URL             → Config.WebhookURL
 *   - breakage_alert_threshold_pct 数字字符串 e.g. "95" → Config.ThresholdPct
 *
 * 阈值留空时后端回落到 DefaultThresholdPct = 95.0，因此这里允许空值。
 */

// 后端常量镜像（internal/alert/alert.go）——冻结的 option 键
const ALERT_OPTION_KEYS = {
  enabled: 'breakage_alert_enabled',
  email: 'breakage_alert_email',
  webhookUrl: 'breakage_alert_webhook_url',
  thresholdPct: 'breakage_alert_threshold_pct',
} as const

const DEFAULT_THRESHOLD_PCT = '95'

const thresholdString = z.string().refine((value) => {
  const trimmed = value.trim()
  if (!trimmed) return true
  const parsed = Number(trimmed)
  return !Number.isNaN(parsed) && parsed > 0 && parsed <= 100
}, '请输入 0 到 100 之间的百分比，或留空以使用默认值')

const alertSchema = z.object({
  breakage_alert_enabled: z.boolean(),
  breakage_alert_email: z.string(),
  breakage_alert_webhook_url: z
    .string()
    .refine((value) => {
      const trimmed = value.trim()
      if (!trimmed) return true
      return /^https?:\/\//.test(trimmed)
    }, '请填写以 http:// 或 https:// 开头的有效地址'),
  breakage_alert_threshold_pct: thresholdString,
})

type AlertFormValues = z.infer<typeof alertSchema>

type AlertSettingsDefaults = {
  breakage_alert_enabled: boolean
  breakage_alert_email: string
  breakage_alert_webhook_url: string
  breakage_alert_threshold_pct: string
}

type AlertSettingsSectionProps = {
  defaultValues: AlertSettingsDefaults
}

const normalizeDefaults = (
  defaults: AlertSettingsDefaults
): AlertSettingsDefaults => ({
  breakage_alert_enabled: defaults.breakage_alert_enabled,
  breakage_alert_email: (defaults.breakage_alert_email ?? '').trim(),
  breakage_alert_webhook_url: (defaults.breakage_alert_webhook_url ?? '').trim(),
  breakage_alert_threshold_pct: (
    defaults.breakage_alert_threshold_pct ?? ''
  ).trim(),
})

const normalizeFormValues = (
  values: AlertFormValues
): AlertSettingsDefaults => ({
  breakage_alert_enabled: values.breakage_alert_enabled,
  breakage_alert_email: values.breakage_alert_email.trim(),
  breakage_alert_webhook_url: values.breakage_alert_webhook_url.trim(),
  breakage_alert_threshold_pct: values.breakage_alert_threshold_pct.trim(),
})

export function AlertSettingsSection({
  defaultValues,
}: AlertSettingsSectionProps) {
  const { t } = useTranslation()
  const updateOption = useUpdateOption()

  const formDefaults = useMemo<AlertFormValues>(
    () => ({
      breakage_alert_enabled: defaultValues.breakage_alert_enabled,
      breakage_alert_email: defaultValues.breakage_alert_email ?? '',
      breakage_alert_webhook_url: defaultValues.breakage_alert_webhook_url ?? '',
      breakage_alert_threshold_pct:
        defaultValues.breakage_alert_threshold_pct ?? '',
    }),
    [defaultValues]
  )

  const baselineRef = useRef<AlertSettingsDefaults>(
    normalizeDefaults(defaultValues)
  )
  const baselineSerializedRef = useRef<string>(
    JSON.stringify(normalizeDefaults(defaultValues))
  )

  const form = useForm<AlertFormValues>({
    resolver: zodResolver(alertSchema),
    mode: 'onChange',
    defaultValues: formDefaults,
  })

  useResetForm(form, formDefaults)

  useEffect(() => {
    const normalized = normalizeDefaults(defaultValues)
    const serialized = JSON.stringify(normalized)
    if (serialized === baselineSerializedRef.current) return
    baselineRef.current = normalized
    baselineSerializedRef.current = serialized
  }, [defaultValues])

  const alertEnabled = form.watch('breakage_alert_enabled')

  const onSubmit = async (values: AlertFormValues) => {
    const normalized = normalizeFormValues(values)
    const updates = (
      Object.keys(normalized) as Array<keyof AlertSettingsDefaults>
    ).filter((key) => normalized[key] !== baselineRef.current[key])

    if (updates.length === 0) {
      toast.info(t('No changes to save', { defaultValue: '没有需要保存的更改' }))
      return
    }

    for (const key of updates) {
      await updateOption.mutateAsync({
        key,
        value: normalized[key],
      })
    }

    baselineRef.current = normalized
    baselineSerializedRef.current = JSON.stringify(normalized)
  }

  return (
    <SettingsSection
      title={t('Breakage Alerts', { defaultValue: '额度沉淀告警' })}
    >
      <Form {...form}>
        <SettingsForm onSubmit={form.handleSubmit(onSubmit)}>
          <SettingsPageFormActions
            onSave={form.handleSubmit(onSubmit)}
            isSaving={updateOption.isPending}
          />

          <p className='text-muted-foreground text-sm'>
            {t('Breakage Alerts Description', {
              defaultValue:
                '当订阅额度沉淀（用户用不满）触及阈值或检测到异常时，向下列渠道推送告警。未配置视为关闭，不发送任何告警。',
            })}
          </p>

          <FormField
            control={form.control}
            name={ALERT_OPTION_KEYS.enabled}
            render={({ field }) => (
              <SettingsSwitchItem>
                <SettingsSwitchContent>
                  <FormLabel>
                    {t('Enable Breakage Alerts', { defaultValue: '启用告警' })}
                  </FormLabel>
                  <FormDescription>
                    {t('Enable Breakage Alerts Desc', {
                      defaultValue:
                        '开启后才会通过邮件 / Webhook 推送额度沉淀告警',
                    })}
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
            name={ALERT_OPTION_KEYS.email}
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Alert Email Recipients', {
                    defaultValue: '收件邮箱（可多个）',
                  })}
                </FormLabel>
                <FormControl>
                  <Textarea
                    rows={3}
                    placeholder='ops@example.com, risk@example.com'
                    disabled={!alertEnabled}
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Alert Email Recipients Desc', {
                    defaultValue:
                      '多个邮箱用英文逗号或换行分隔；留空则不发送邮件告警',
                  })}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name={ALERT_OPTION_KEYS.webhookUrl}
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Alert Webhook URL', { defaultValue: 'Webhook 地址' })}
                </FormLabel>
                <FormControl>
                  <Input
                    type='url'
                    placeholder='https://hooks.example.com/breakage'
                    autoComplete='off'
                    disabled={!alertEnabled}
                    {...field}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Alert Webhook URL Desc', {
                    defaultValue:
                      '告警将以 POST JSON 推送到该地址；留空则不发送 Webhook 告警',
                  })}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />

          <FormField
            control={form.control}
            name={ALERT_OPTION_KEYS.thresholdPct}
            render={({ field }) => (
              <FormItem>
                <FormLabel>
                  {t('Alert Threshold Percent', {
                    defaultValue: '告警阈值（%）',
                  })}
                </FormLabel>
                <FormControl>
                  <Input
                    type='number'
                    min={0}
                    max={100}
                    step={1}
                    placeholder={DEFAULT_THRESHOLD_PCT}
                    disabled={!alertEnabled}
                    value={field.value}
                    onChange={(event) => field.onChange(event.target.value)}
                  />
                </FormControl>
                <FormDescription>
                  {t('Alert Threshold Percent Desc', {
                    defaultValue:
                      '订阅用量占比达到该百分比即视为满额并触发告警；留空默认 95%',
                  })}
                </FormDescription>
                <FormMessage />
              </FormItem>
            )}
          />
        </SettingsForm>
      </Form>
    </SettingsSection>
  )
}
