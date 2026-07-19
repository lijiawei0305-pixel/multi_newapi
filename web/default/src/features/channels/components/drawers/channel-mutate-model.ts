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
import type { ChannelFormValues } from '../../lib'

export type ModelMappingGuardrail = {
  invalidJson: boolean
  entries: Array<{ source: string; target: string }>
  missingSourceModels: string[]
  exposedTargetModels: string[]
}
// Helper functions
export const createEmptyModelMappingGuardrail = (): ModelMappingGuardrail => ({
  invalidJson: false,
  entries: [],
  missingSourceModels: [],
  exposedTargetModels: [],
})

export const formatModelNames = (models: string[]): string =>
  models.map((model) => `"${model}"`).join(', ')

export const MODEL_MAPPING_PREVIEW_FALLBACK: Array<{
  source: string
  target: string
}> = [{ source: 'client-model', target: 'upstream-model' }]

export const ADVANCED_SETTINGS_EXPANDED_KEY =
  'channel-advanced-settings-expanded'
export const UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT = 8
export const SENSITIVE_FORM_FIELDS = [
  'type',
  'base_url',
  'key',
  'openai_organization',
  'other',
  'key_mode',
  'param_override',
  'header_override',
  'settings',
  'setting',
  'advanced_custom',
  'is_enterprise_account',
  'vertex_key_type',
  'aws_key_type',
  'azure_responses_version',
  'force_format',
  'thinking_to_content',
  'proxy',
  'pass_through_body_enabled',
  'system_prompt',
  'system_prompt_override',
  'allow_service_tier',
  'disable_store',
  'allow_safety_identifier',
  'allow_include_obfuscation',
  'allow_inference_geo',
  'allow_speed',
  'claude_beta_query',
  'disable_task_polling_sleep',
  'upstream_model_update_check_enabled',
  'upstream_model_update_auto_sync_enabled',
  'upstream_model_update_ignored_models',
] satisfies (keyof ChannelFormValues)[]

export function readAdvancedSettingsPreference(): boolean {
  if (typeof window === 'undefined') return false
  return window.localStorage.getItem(ADVANCED_SETTINGS_EXPANDED_KEY) === 'true'
}

export function hasAdvancedSettingsValues(values: ChannelFormValues): boolean {
  return Boolean(
    values.param_override?.trim() ||
    values.header_override?.trim() ||
    values.advanced_custom?.trim() ||
    values.status_code_mapping?.trim() ||
    values.tag?.trim() ||
    values.remark?.trim() ||
    values.priority ||
    values.weight ||
    values.proxy?.trim() ||
    values.system_prompt?.trim() ||
    values.force_format ||
    values.thinking_to_content ||
    values.pass_through_body_enabled ||
    values.system_prompt_override ||
    values.claude_beta_query ||
    values.upstream_model_update_check_enabled ||
    values.upstream_model_update_auto_sync_enabled ||
    values.upstream_model_update_ignored_models?.trim()
  )
}

export function parseSettingsRecord(
  settings: string | undefined
): Record<string, unknown> {
  if (!settings?.trim()) return {}
  try {
    const parsed = JSON.parse(settings)
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      return parsed as Record<string, unknown>
    }
  } catch {
    return {}
  }
  return {}
}

export function formatUnixTime(timestamp: unknown): string {
  const seconds = Number(timestamp)
  if (!Number.isFinite(seconds) || seconds <= 0) return '-'
  return new Date(seconds * 1000).toLocaleString(undefined, {
    timeZone: 'Asia/Shanghai',
  })
}
