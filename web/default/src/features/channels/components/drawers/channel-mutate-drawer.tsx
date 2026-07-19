import { zodResolver } from '@hookform/resolvers/zod'
import { useQuery, useQueryClient } from '@tanstack/react-query'
import {
  AlertCircle,
  Boxes,
  CheckCircle2,
  Circle,
  KeyRound,
  Loader2,
  Server,
  Settings,
} from 'lucide-react'
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
import {
  type ReactNode,
  useEffect,
  useState,
  useMemo,
  useCallback,
  useRef,
} from 'react'
import { type SubmitErrorHandler, useForm } from 'react-hook-form'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import {
  sideDrawerContentClassName,
  sideDrawerFooterClassName,
  sideDrawerFormClassName,
  sideDrawerHeaderClassName,
} from '@/components/drawer-layout'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { Form } from '@/components/ui/form'
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet'
import {
  SecureVerificationDialog,
  useSecureVerification,
} from '@/features/auth/secure-verification'
import { useCopyToClipboard } from '@/hooks/use-copy-to-clipboard'
import { useHiddenClickUnlock } from '@/hooks/use-hidden-click-unlock'
import {
  ADMIN_PERMISSION_ACTIONS,
  ADMIN_PERMISSION_RESOURCES,
  hasPermission,
} from '@/lib/admin-permissions'
import { ROLE } from '@/lib/roles'
import { cn } from '@/lib/utils'
import { useAuthStore } from '@/stores/auth-store'

import {
  fetchModels,
  getAllModels,
  getChannel,
  getChannelKey,
  getModelGroups,
  getPrefillGroups,
  refreshCodexCredential,
} from '../../api'
import {
  ADD_MODE_OPTIONS,
  CHANNEL_STATUS_LABELS,
  CHANNEL_TYPE_OPTIONS,
  ERROR_MESSAGES,
  MODEL_FETCHABLE_TYPES,
} from '../../constants'
import { useChannelMutateForm } from '../../hooks/use-channel-mutate-form'
import {
  CHANNEL_FORM_DEFAULT_VALUES,
  channelFormSchema,
  channelsQueryKeys,
  getAdvancedCustomStats,
  transformChannelToFormDefaults,
  type ChannelFormValues,
  deduplicateKeys,
  parseModelsString,
  formatModelsArray,
  extractRedirectModels,
  extractMappingSourceModels,
  hasModelConfigChanged,
  findMissingModelsInMapping,
  validateModelMappingJson,
  hasAdvancedSettingsErrors,
} from '../../lib'
import {
  collectInvalidStatusCodeEntries,
  collectNewDisallowedStatusCodeRedirects,
} from '../../lib/status-code-risk-guard'
import type { Channel } from '../../types'
import { useChannels } from '../channels-provider'
import { AdvancedCustomEditorDialog } from '../dialogs/advanced-custom-editor-dialog'
import { FetchModelsDialog } from '../dialogs/fetch-models-dialog'
import {
  MissingModelsConfirmationDialog,
  type MissingModelsAction,
} from '../dialogs/missing-models-confirmation-dialog'
import { ParamOverrideEditorDialog } from '../dialogs/param-override-editor-dialog'
import { StatusCodeRiskDialog } from '../dialogs/status-code-risk-dialog'
import { ChannelAdvancedFields } from './channel-advanced-fields'
import { ChannelCredentialsFields } from './channel-credentials-fields'
import { ChannelIdentityFields } from './channel-identity-fields'
import { ChannelModelsFields } from './channel-models-fields'
import {
  ADVANCED_SETTINGS_EXPANDED_KEY,
  MODEL_MAPPING_PREVIEW_FALLBACK,
  SENSITIVE_FORM_FIELDS,
  UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT,
  createEmptyModelMappingGuardrail,
  hasAdvancedSettingsValues,
  parseSettingsRecord,
  readAdvancedSettingsPreference,
  type ModelMappingGuardrail,
} from './channel-mutate-model'
import { ChannelTypeLogo } from './channel-type-logo'
import { ChannelEditorLoadingState } from './sections'

type ChannelMutateDrawerProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  currentRow?: Channel | null
}

type ChannelEditorSectionStatus = 'complete' | 'configured' | 'error' | 'idle'

type ChannelEditorNavItem = {
  id: string
  title: string
  description?: string
  statusLabel: string
  status: ChannelEditorSectionStatus
  icon: ReactNode
}

function getSectionStatusIcon(status: ChannelEditorSectionStatus): ReactNode {
  if (status === 'error') {
    return <AlertCircle className='h-3.5 w-3.5' aria-hidden='true' />
  }
  if (status === 'complete' || status === 'configured') {
    return <CheckCircle2 className='h-3.5 w-3.5' aria-hidden='true' />
  }
  return <Circle className='h-3.5 w-3.5' aria-hidden='true' />
}

function getCompletionStatus(
  hasErrors: boolean,
  isComplete: boolean
): ChannelEditorSectionStatus {
  if (hasErrors) return 'error'
  if (isComplete) return 'complete'
  return 'idle'
}

function getSectionStatusLabel(
  status: ChannelEditorSectionStatus,
  t: (key: string) => string
): string {
  if (status === 'error') return t('Error')
  if (status === 'complete' || status === 'configured') return t('Ready')
  return t('Incomplete')
}

function ChannelEditorNav(props: {
  providerLogo: ReactNode
  providerLabel: string
  statusLabel: string
  progressLabel: string
  navigationLabel: string
  items: ChannelEditorNavItem[]
}) {
  return (
    <aside className='hidden self-start lg:sticky lg:top-4 lg:z-20 lg:block'>
      <div className='flex max-h-[calc(100dvh-12rem)] flex-col gap-3 overflow-y-auto overscroll-contain pr-1'>
        <div className='border-border/60 bg-muted/20 rounded-lg border p-3'>
          <div className='flex min-w-0 items-center gap-2'>
            <span className='bg-background flex size-8 shrink-0 items-center justify-center rounded-md border'>
              {props.providerLogo}
            </span>
            <div className='min-w-0'>
              <p className='truncate text-sm font-medium'>
                {props.providerLabel}
              </p>
              <p className='text-muted-foreground truncate text-xs'>
                {props.statusLabel} · {props.progressLabel}
              </p>
            </div>
          </div>
        </div>

        <nav
          className='border-border/60 bg-background rounded-lg border p-1'
          aria-label={props.navigationLabel}
        >
          {props.items.map((item) => {
            const isError = item.status === 'error'
            const isDone =
              item.status === 'complete' || item.status === 'configured'
            return (
              <button
                key={item.id}
                type='button'
                className={cn(
                  'hover:bg-muted/60 flex w-full items-start gap-2 rounded-md px-2 py-2 text-left transition-colors',
                  isError && 'text-destructive hover:bg-destructive/10'
                )}
                onClick={() => {
                  document
                    .querySelector<HTMLElement>(`#${item.id}`)
                    ?.scrollIntoView({ behavior: 'smooth', block: 'start' })
                }}
              >
                <span
                  className={cn(
                    'bg-muted text-muted-foreground mt-0.5 flex size-7 shrink-0 items-center justify-center rounded-md',
                    isError && 'bg-destructive/10 text-destructive',
                    isDone && !isError && 'text-primary'
                  )}
                >
                  {item.icon}
                </span>
                <span className='min-w-0 flex-1'>
                  <span className='block truncate text-sm font-medium'>
                    {item.title}
                  </span>
                  {item.description && (
                    <span className='text-muted-foreground block truncate text-xs'>
                      {item.description}
                    </span>
                  )}
                </span>
                <span
                  className={cn(
                    'text-muted-foreground mt-1 shrink-0',
                    isError && 'text-destructive',
                    isDone && !isError && 'text-primary'
                  )}
                  aria-label={item.statusLabel}
                >
                  {getSectionStatusIcon(item.status)}
                </span>
              </button>
            )
          })}
        </nav>
      </div>
    </aside>
  )
}

export function ChannelMutateDrawer({
  open,
  onOpenChange,
  currentRow,
}: ChannelMutateDrawerProps) {
  const { t } = useTranslation()
  const queryClient = useQueryClient()
  const { setOpen } = useChannels()
  const currentUser = useAuthStore((s) => s.auth.user)
  const canEditSensitive = hasPermission(
    currentUser,
    ADMIN_PERMISSION_RESOURCES.CHANNEL,
    ADMIN_PERMISSION_ACTIONS.SENSITIVE_WRITE
  )
  const canRevealChannelKey = currentUser?.role === ROLE.SUPER_ADMIN
  const [fetchModelsDialogOpen, setFetchModelsDialogOpen] = useState(false)
  const [channelKey, setChannelKey] = useState<string | null>(null)
  const [isChannelKeyLoading, setIsChannelKeyLoading] = useState(false)
  const [isCodexCredentialRefreshing, setIsCodexCredentialRefreshing] =
    useState(false)
  const initialModelsRef = useRef<string[]>([])
  const initialModelMappingRef = useRef<string>('')
  const initialStatusCodeMappingRef = useRef<string>('')
  const [statusCodeRiskOpen, setStatusCodeRiskOpen] = useState(false)
  const [statusCodeRiskDetailItems, setStatusCodeRiskDetailItems] = useState<
    string[]
  >([])
  const statusCodeRiskResolveRef = useRef<
    ((confirmed: boolean) => void) | null
  >(null)
  const [missingModelsDialogOpen, setMissingModelsDialogOpen] = useState(false)
  const [missingModelsList, setMissingModelsList] = useState<string[]>([])
  const missingModelsResolveRef = useRef<
    ((action: MissingModelsAction) => void) | null
  >(null)
  const [advancedSettingsOpen, setAdvancedSettingsOpen] = useState(false)
  const [paramOverrideEditorOpen, setParamOverrideEditorOpen] = useState(false)
  const [advancedCustomEditorOpen, setAdvancedCustomEditorOpen] =
    useState(false)

  const isEditing = Boolean(currentRow)
  const channelId = currentRow?.id ?? null
  const sensitiveLocked = isEditing && !canEditSensitive

  // Fetch channel details if editing
  const { data: channelData, isLoading: isChannelLoading } = useQuery({
    queryKey: channelsQueryKeys.detail(channelId || 0),
    queryFn: () => getChannel(channelId || 0),
    enabled: isEditing && Boolean(channelId),
  })

  // Fetch registered model groups. The channel "Model Group" field is scoped
  // to these (the model set this channel serves / routes to); user tiers like
  // default/vip/svip are intentionally excluded so they can't be selected here.
  const { data: modelGroupsData, isLoading: isLoadingGroups } = useQuery({
    queryKey: ['admin-model-groups'],
    queryFn: getModelGroups,
  })

  // Fetch all available models
  const { data: allModelsData } = useQuery({
    queryKey: ['channel_models'],
    queryFn: getAllModels,
  })

  // Fetch prefill model groups
  const { data: prefillGroupsData } = useQuery({
    queryKey: ['prefill_groups', 'model'],
    queryFn: () => getPrefillGroups('model'),
  })

  const { copyToClipboard } = useCopyToClipboard()

  const {
    open: verificationOpen,
    methods: verificationMethods,
    state: verificationState,
    executeVerification,
    withVerification,
    cancel: cancelVerification,
    setCode: setVerificationCode,
    switchMethod: switchVerificationMethod,
  } = useSecureVerification()

  useEffect(() => {
    if (!open) {
      setChannelKey(null)
      setIsChannelKeyLoading(false)
    } else if (channelId) {
      setChannelKey(null)
    }
  }, [open, channelId])

  // Check if this is a multi-key channel
  const isMultiKeyChannel =
    isEditing && channelData?.data?.channel_info?.is_multi_key === true

  // Form setup
  const form = useForm<ChannelFormValues>({
    resolver: zodResolver(channelFormSchema),
    defaultValues: CHANNEL_FORM_DEFAULT_VALUES,
  })

  // Watch form values for conditional rendering
  const multiKeyMode = form.watch('multi_key_mode')
  const multiKeyType = form.watch('multi_key_type')
  const keyMode = form.watch('key_mode')
  const currentGroups = form.watch('group')
  const currentType = form.watch('type')
  const currentStatus = form.watch('status')
  const currentBaseUrl = form.watch('base_url')
  const currentKey = form.watch('key')
  const currentOther = form.watch('other')
  const currentModels = form.watch('models')
  const currentName = form.watch('name')
  const currentModelMapping = form.watch('model_mapping')
  const awsKeyType = form.watch('aws_key_type')
  const vertexKeyType = form.watch('vertex_key_type')
  const upstreamModelUpdateCheckEnabled = form.watch(
    'upstream_model_update_check_enabled'
  )
  const currentSettings = form.watch('settings')
  const currentAdvancedCustom = form.watch('advanced_custom')
  const {
    unlocked: doubaoApiEditUnlocked,
    handleClick: handleApiConfigSecretClick,
    reset: resetDoubaoApiUnlock,
  } = useHiddenClickUnlock({
    requiredClicks: 10,
    disabled: currentType !== 45 || sensitiveLocked,
    onUnlock: () => {
      toast.info(t('Doubao custom API address editing unlocked'))
    },
  })

  useEffect(() => {
    if (!open) {
      resetDoubaoApiUnlock()
    }
  }, [open, resetDoubaoApiUnlock])

  // Helper computed values
  const isBatchMode =
    multiKeyMode === 'batch' || multiKeyMode === 'multi_to_single'
  const isChannelDetailLoading = isEditing && isChannelLoading
  const supportsMultiKeyAddMode =
    currentType !== 57 && !(currentType === 41 && vertexKeyType === 'api_key')
  const addModeOptions = useMemo(
    () =>
      supportsMultiKeyAddMode
        ? ADD_MODE_OPTIONS
        : ADD_MODE_OPTIONS.filter((option) => option.value === 'single'),
    [supportsMultiKeyAddMode]
  )

  const advancedCustomStats = useMemo(
    () => getAdvancedCustomStats(currentAdvancedCustom),
    [currentAdvancedCustom]
  )

  // Get all models list
  const allModelsList = useMemo(
    () => allModelsData?.data?.map((model) => model.id).filter(Boolean) || [],
    [allModelsData]
  )

  // Get basic models for the current channel type
  const basicModels = useMemo(() => {
    if (!allModelsList.length) return []
    // Filter models based on common patterns for specific types
    if (currentType === 1) {
      return allModelsList.filter(
        (model) => model.startsWith('gpt-') || model.startsWith('text-')
      )
    }
    return allModelsList
  }, [allModelsList, currentType])

  // Get prefill groups
  const prefillGroups = useMemo(
    () => prefillGroupsData?.data || [],
    [prefillGroupsData]
  )

  // Transform enabled model groups to multi-select options. Only registered,
  // enabled model groups are offered; already-selected values (e.g. legacy
  // tier groups) still render as removable chips via MultiSelect's fallback.
  const groupOptions = useMemo(() => {
    const names = (modelGroupsData?.data ?? [])
      .filter((group) => group.enabled)
      .map((group) => group.name)
    return [...new Set(names)].map((group) => ({
      value: group,
      label: group,
    }))
  }, [modelGroupsData])

  // Parse current models as array
  const currentModelsArray = useMemo(
    () => parseModelsString(currentModels),
    [currentModels]
  )

  const currentTypeLabel = useMemo(
    () =>
      CHANNEL_TYPE_OPTIONS.find((option) => option.value === currentType)
        ?.label || `#${currentType}`,
    [currentType]
  )

  const channelTypeOptions = useMemo(() => {
    const options = CHANNEL_TYPE_OPTIONS.map((option) => ({
      value: String(option.value),
      label: t(option.label),
      icon: <ChannelTypeLogo type={option.value} size={16} />,
    }))
    if (!options.some((option) => Number(option.value) === currentType)) {
      options.push({
        value: String(currentType),
        label: `#${currentType}`,
        icon: <ChannelTypeLogo type={currentType} size={16} />,
      })
    }
    return options
  }, [currentType, t])

  const formErrors = form.formState.errors
  const identityHasErrors = Boolean(
    formErrors.name ||
    formErrors.type ||
    formErrors.status ||
    formErrors.openai_organization
  )
  const credentialsHaveErrors = Boolean(
    formErrors.key ||
    formErrors.base_url ||
    formErrors.other ||
    formErrors.multi_key_mode ||
    formErrors.multi_key_type ||
    formErrors.key_mode ||
    formErrors.vertex_key_type ||
    formErrors.aws_key_type ||
    formErrors.azure_responses_version
  )
  const modelsHaveErrors = Boolean(
    formErrors.models || formErrors.group || formErrors.model_mapping
  )
  const advancedHaveErrors =
    hasAdvancedSettingsErrors(formErrors) || Boolean(formErrors.advanced_custom)
  const providerRequiresBaseUrl = [3, 8, 36, 45].includes(currentType)
  const providerRequiresOther = [3, 18, 21, 39, 41, 49].includes(currentType)
  const identityComplete = Boolean(currentName?.trim() && currentType > 0)
  const credentialsComplete = Boolean(
    (isEditing || currentKey?.trim()) &&
    (!providerRequiresBaseUrl || currentBaseUrl?.trim()) &&
    (!providerRequiresOther || currentOther?.trim())
  )
  const modelsComplete = Boolean(
    currentModelsArray.length > 0 && currentGroups?.length
  )
  const requiredCompletedCount = [
    identityComplete,
    credentialsComplete,
    modelsComplete,
  ].filter(Boolean).length
  const currentStatusLabel =
    CHANNEL_STATUS_LABELS[
      currentStatus as keyof typeof CHANNEL_STATUS_LABELS
    ] || 'Unknown'
  const progressLabel = `${requiredCompletedCount}/3`
  const identityStatus = getCompletionStatus(
    identityHasErrors,
    identityComplete
  )
  const credentialsStatus = getCompletionStatus(
    credentialsHaveErrors,
    credentialsComplete
  )
  const modelsStatus = getCompletionStatus(modelsHaveErrors, modelsComplete)
  const advancedStatus: ChannelEditorSectionStatus = advancedHaveErrors
    ? 'error'
    : 'idle'
  const advancedSummary = advancedHaveErrors ? t('Error') : undefined
  const editorNavItems: ChannelEditorNavItem[] = [
    {
      id: 'channel-section-identity',
      title: t('Basic Information'),
      description: getSectionStatusLabel(identityStatus, t),
      statusLabel: getSectionStatusLabel(identityStatus, t),
      status: identityStatus,
      icon: <Server className='h-4 w-4' aria-hidden='true' />,
    },
    {
      id: 'channel-section-credentials',
      title: t('Credentials'),
      description: getSectionStatusLabel(credentialsStatus, t),
      statusLabel: getSectionStatusLabel(credentialsStatus, t),
      status: credentialsStatus,
      icon: <KeyRound className='h-4 w-4' aria-hidden='true' />,
    },
    {
      id: 'channel-section-models',
      title: t('Models & Groups'),
      description: getSectionStatusLabel(modelsStatus, t),
      statusLabel: getSectionStatusLabel(modelsStatus, t),
      status: modelsStatus,
      icon: <Boxes className='h-4 w-4' aria-hidden='true' />,
    },
    {
      id: 'channel-section-advanced',
      title: t('Advanced Settings'),
      description: advancedSummary,
      statusLabel: advancedSummary ?? t('Advanced Settings'),
      status: advancedStatus,
      icon: <Settings className='h-4 w-4' aria-hidden='true' />,
    },
  ]

  // Extract redirect models from model_mapping (target values)
  const redirectModelList = useMemo(
    () => extractRedirectModels(currentModelMapping || ''),
    [currentModelMapping]
  )

  // Extract source keys from model_mapping (models being remapped FROM)
  const redirectModelKeyList = useMemo(
    () => extractMappingSourceModels(currentModelMapping || ''),
    [currentModelMapping]
  )

  // Transform models to multi-select options
  const modelOptions = useMemo(() => {
    const allModels = new Set([...allModelsList, ...currentModelsArray])
    return [...allModels].map((model) => ({
      value: model,
      label: model,
    }))
  }, [allModelsList, currentModelsArray])

  const modelMappingGuardrail = useMemo<ModelMappingGuardrail>(() => {
    if (!currentModelMapping?.trim()) {
      return createEmptyModelMappingGuardrail()
    }

    try {
      const parsed = JSON.parse(currentModelMapping)
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
      }

      const entries = Object.entries(parsed).reduce<
        Array<{ source: string; target: string }>
      >((acc, [rawSource, rawTarget]) => {
        const source = String(rawSource).trim()
        const target = String(rawTarget ?? '').trim()

        if (!source || !target) {
          return acc
        }

        acc.push({ source, target })
        return acc
      }, [])

      const missingSourceModels = [
        ...new Set(
          entries
            .filter(
              (entry) =>
                Boolean(entry.source) &&
                !currentModelsArray.includes(entry.source)
            )
            .map((entry) => entry.source)
        ),
      ]

      const exposedTargetModels = [
        ...new Set(
          entries
            .filter(
              (entry) =>
                Boolean(entry.target) &&
                currentModelsArray.includes(entry.target)
            )
            .map((entry) => entry.target)
        ),
      ]

      return {
        invalidJson: false,
        entries,
        missingSourceModels,
        exposedTargetModels,
      }
    } catch {
      return { ...createEmptyModelMappingGuardrail(), invalidJson: true }
    }
  }, [currentModelMapping, currentModelsArray])

  const mappingPreviewPairs =
    modelMappingGuardrail.entries.length > 0
      ? modelMappingGuardrail.entries.slice(0, 3)
      : MODEL_MAPPING_PREVIEW_FALLBACK
  const remainingMappingCount =
    modelMappingGuardrail.entries.length > 3
      ? modelMappingGuardrail.entries.length - 3
      : 0

  const upstreamUpdateMeta = useMemo(() => {
    const settings = parseSettingsRecord(currentSettings)
    const detectedModels = Array.isArray(
      settings.upstream_model_update_last_detected_models
    )
      ? settings.upstream_model_update_last_detected_models
          .map((model) => String(model || '').trim())
          .filter(Boolean)
      : []

    return {
      lastCheckTime: settings.upstream_model_update_last_check_time,
      detectedModels: [...new Set(detectedModels)],
    }
  }, [currentSettings])

  const upstreamDetectedModelsPreview = upstreamUpdateMeta.detectedModels.slice(
    0,
    UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT
  )
  const upstreamDetectedModelsOmittedCount =
    upstreamUpdateMeta.detectedModels.length -
    upstreamDetectedModelsPreview.length

  // Load channel data into form when editing
  useEffect(() => {
    if (isEditing && channelData?.data) {
      const defaults = transformChannelToFormDefaults(channelData.data)
      form.reset(defaults)
      setAdvancedSettingsOpen(
        readAdvancedSettingsPreference() || hasAdvancedSettingsValues(defaults)
      )
      // Store initial values for comparison
      initialModelsRef.current = parseModelsString(
        channelData.data.models || ''
      )
      initialModelMappingRef.current = channelData.data.model_mapping || ''
      initialStatusCodeMappingRef.current =
        channelData.data.status_code_mapping || ''
    } else if (!isEditing) {
      form.reset(CHANNEL_FORM_DEFAULT_VALUES)
      setAdvancedSettingsOpen(false)
      initialModelsRef.current = []
      initialModelMappingRef.current = ''
      initialStatusCodeMappingRef.current = ''
    }
  }, [isEditing, channelData, form])

  // Handle type change - set default values for specific types
  useEffect(() => {
    if (isEditing) return // Don't auto-set defaults when editing

    // Type 45 (VolcEngine) - set default base_url
    if (currentType === 45) {
      const currentBaseUrlValue = form.getValues('base_url')
      if (!currentBaseUrlValue || currentBaseUrlValue === '') {
        form.setValue('base_url', 'https://ark.cn-beijing.volces.com')
      }
    }

    // Type 18 (Xunfei) - set default other (version)
    if (currentType === 18) {
      const currentOther = form.getValues('other')
      if (!currentOther || currentOther === '') {
        form.setValue('other', 'v2.1')
      }
    }
  }, [currentType, isEditing, form])

  useEffect(() => {
    if (currentType !== 45 || currentBaseUrl !== 'doubao-coding-plan') return

    form.setValue('base_url', 'https://ark.cn-beijing.volces.com', {
      shouldDirty: false,
      shouldValidate: true,
    })
  }, [currentBaseUrl, currentType, form])

  useEffect(() => {
    if (isEditing || supportsMultiKeyAddMode) return
    if (multiKeyMode && multiKeyMode !== 'single') {
      form.setValue('multi_key_mode', 'single', {
        shouldDirty: true,
        shouldValidate: true,
      })
    }
  }, [form, isEditing, multiKeyMode, supportsMultiKeyAddMode])

  // Validate base_url - warn if it ends with /v1
  useEffect(() => {
    if (!currentBaseUrl || !currentBaseUrl.endsWith('/v1')) return

    // Show warning toast
    const timer = setTimeout(() => {
      toast.warning(
        t(
          'Warning: Base URL should not end with /v1. New API will handle it automatically. This may cause request failures.'
        ),
        { duration: 5000 }
      )
    }, 500)

    return () => clearTimeout(timer)
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [currentBaseUrl])

  // Handle key deduplication
  const handleDeduplicateKeys = () => {
    const currentKey = form.getValues('key')
    if (!currentKey || currentKey.trim() === '') {
      toast.info(t('Please enter keys first'))
      return
    }

    const result = deduplicateKeys(currentKey)

    if (result.removedCount === 0) {
      toast.info(t('No duplicate keys found'))
    } else {
      form.setValue('key', result.deduplicatedText)
      toast.success(
        t(
          'Removed {{removed}} duplicate key(s). Before: {{before}}, After: {{after}}',
          {
            removed: result.removedCount,
            before: result.beforeCount,
            after: result.afterCount,
          }
        )
      )
    }
  }

  const fetchChannelKey = useCallback(async () => {
    if (!channelId) {
      throw new Error('Channel is not selected')
    }

    setIsChannelKeyLoading(true)
    try {
      const res = await getChannelKey(channelId)
      if (!res.success) {
        throw new Error(res.message || t('Failed to fetch channel key'))
      }

      const keyValue = res.data?.key ?? ''
      setChannelKey(keyValue)
      toast.success(t('Channel key unlocked'))
      return res
    } finally {
      setIsChannelKeyLoading(false)
    }
  }, [channelId, t])

  const handleRevealKey = useCallback(async () => {
    if (!channelId) return

    try {
      await withVerification(fetchChannelKey, {
        preferredMethod: 'passkey',
        title: 'Verify to view channel key',
        description:
          'Use Passkey or 2FA to confirm your identity before revealing this channel key.',
      })
    } catch (error) {
      if (error instanceof Error) {
        toast.error(error.message)
      }
    }
  }, [channelId, withVerification, fetchChannelKey])

  const handleRefreshCodexCredential = useCallback(async () => {
    if (!channelId) return
    setIsCodexCredentialRefreshing(true)
    try {
      const res = await refreshCodexCredential(channelId)
      if (!res.success) {
        throw new Error(res.message || t('Failed to refresh credential'))
      }
      toast.success(t('Credential refreshed'))
      queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    } catch (error) {
      toast.error(error instanceof Error ? error.message : t('Refresh failed'))
    } finally {
      setIsCodexCredentialRefreshing(false)
    }
  }, [channelId, queryClient, t])

  // Unified function to update models
  const updateModels = useCallback(
    (newModels: string[], merge: boolean = false) => {
      const finalModels = merge
        ? formatModelsArray([...currentModelsArray, ...newModels])
        : formatModelsArray(newModels)
      form.setValue('models', finalModels)
      return newModels.length
    },
    [currentModelsArray, form]
  )

  // Handle fetching models from upstream
  const handleFetchModels = useCallback(async () => {
    const type = form.getValues('type')

    if (!MODEL_FETCHABLE_TYPES.has(type)) {
      toast.error(t('This channel type does not support fetching models'))
      return
    }

    if (!isEditing && !canEditSensitive) {
      toast.error(t("You don't have necessary permission"))
      return
    }

    // For creation mode, validate key before opening dialog
    if (!isEditing) {
      const key = form.getValues('key')
      if (!key?.trim()) {
        toast.error(t('Please enter API key first'))
        return
      }
    }

    setFetchModelsDialogOpen(true)
  }, [isEditing, canEditSensitive, form, t])

  const createModeFetcher = useCallback(async (): Promise<string[]> => {
    if (!canEditSensitive) {
      throw new Error(t("You don't have necessary permission"))
    }
    const response = await fetchModels({
      type: form.getValues('type'),
      key: form.getValues('key'),
      base_url: form.getValues('base_url') || '',
    })
    if (response.success && response.data) {
      return response.data
    }
    throw new Error(response.message || 'No models fetched from upstream')
  }, [canEditSensitive, form, t])

  // Handle model operations
  const handleFillRelatedModels = useCallback(() => {
    if (!basicModels.length) {
      toast.info(t('No related models available for this channel type'))
      return
    }
    updateModels(basicModels)
    toast.success(
      t('Filled {{count}} related model(s)', { count: basicModels.length })
    )
  }, [basicModels, updateModels, t])

  const handleFillAllModels = useCallback(() => {
    if (!allModelsList.length) {
      toast.info(t('No models available'))
      return
    }
    updateModels(allModelsList)
    toast.success(
      t('Filled {{count}} model(s)', { count: allModelsList.length })
    )
  }, [allModelsList, updateModels, t])

  const handleClearModels = useCallback(() => {
    form.setValue('models', '')
    toast.success(t('Cleared all models'))
  }, [form, t])

  const handleCopyModels = useCallback(async () => {
    const models = form.getValues('models')
    if (!models?.trim()) {
      toast.info(t('No models to copy'))
      return
    }
    await copyToClipboard(models)
  }, [form, copyToClipboard, t])

  // Handle adding prefill group models
  const handleAddPrefillGroup = useCallback(
    (group: { id: number; name: string; items: string | string[] }) => {
      try {
        const items = Array.isArray(group.items)
          ? group.items
          : JSON.parse(group.items)

        if (!Array.isArray(items)) {
          throw new Error('Invalid items format')
        }

        const count = updateModels(items, true)
        toast.success(
          t('Added {{count}} models from "{{name}}"', {
            count,
            name: group.name,
          })
        )
      } catch {
        toast.error(t('Failed to parse group items'))
      }
    },
    [updateModels, t]
  )

  // Handle model selection change from MultiSelect
  const handleModelsChange = useCallback(
    (selected: string[]) => {
      form.setValue('models', selected.join(','))
    },
    [form]
  )

  // Handle successful submission
  const handleSuccess = useCallback(() => {
    queryClient.invalidateQueries({ queryKey: channelsQueryKeys.lists() })
    if (channelId) {
      queryClient.invalidateQueries({
        queryKey: channelsQueryKeys.detail(channelId),
      })
    }
    onOpenChange(false)
    setOpen(null)
  }, [channelId, queryClient, onOpenChange, setOpen])

  // Show missing models confirmation dialog
  const confirmMissingModelMappings = useCallback(
    (missingModels: string[]): Promise<MissingModelsAction> => {
      return new Promise((resolve) => {
        setMissingModelsList(missingModels)
        setMissingModelsDialogOpen(true)
        missingModelsResolveRef.current = resolve
      })
    },
    []
  )

  // Handle missing models dialog action
  const handleMissingModelsAction = useCallback(
    (action: MissingModelsAction) => {
      setMissingModelsDialogOpen(false)
      if (missingModelsResolveRef.current) {
        missingModelsResolveRef.current(action)
        missingModelsResolveRef.current = null
      }
    },
    []
  )

  const confirmStatusCodeRisk = useCallback(
    (detailItems: string[]): Promise<boolean> =>
      new Promise((resolve) => {
        statusCodeRiskResolveRef.current = resolve
        setStatusCodeRiskDetailItems(detailItems)
        setStatusCodeRiskOpen(true)
      }),
    []
  )

  const handleStatusCodeRiskAction = useCallback((confirmed: boolean) => {
    setStatusCodeRiskOpen(false)
    setStatusCodeRiskDetailItems([])
    if (statusCodeRiskResolveRef.current) {
      statusCodeRiskResolveRef.current(confirmed)
      statusCodeRiskResolveRef.current = null
    }
  }, [])

  useEffect(() => {
    return () => {
      if (statusCodeRiskResolveRef.current) {
        statusCodeRiskResolveRef.current(false)
        statusCodeRiskResolveRef.current = null
      }
    }
  }, [])

  const channelMutation = useChannelMutateForm({
    currentRow,
    isEditing,
    isMultiKeyChannel,
    onSuccess: handleSuccess,
  })

  const isSubmitting = channelMutation.isPending

  // Submit handler
  const onSubmit = useCallback(
    async (data: ChannelFormValues) => {
      // Validate key is required when creating
      if (!isEditing && !data.key?.trim()) {
        form.setError('key', {
          type: 'manual',
          message: ERROR_MESSAGES.REQUIRED_KEY,
        })
        return
      }

      if (sensitiveLocked) {
        const dirtyFields = form.formState.dirtyFields as Partial<
          Record<keyof ChannelFormValues, unknown>
        >
        const hasSensitiveChanges = SENSITIVE_FORM_FIELDS.some((field) =>
          Boolean(dirtyFields[field])
        )
        if (hasSensitiveChanges) {
          toast.error(
            t('You do not have permission to edit sensitive channel settings.')
          )
          return
        }
      }

      // Validate status_code_mapping entries
      if (data.status_code_mapping?.trim()) {
        const invalidEntries = collectInvalidStatusCodeEntries(
          data.status_code_mapping
        )
        if (invalidEntries.length > 0) {
          toast.error(
            t('Invalid status code mapping entries: {{entries}}', {
              entries: invalidEntries.join(', '),
            })
          )
          return
        }

        const riskyRedirects = collectNewDisallowedStatusCodeRedirects(
          initialStatusCodeMappingRef.current,
          data.status_code_mapping
        )
        if (riskyRedirects.length > 0) {
          const confirmed = await confirmStatusCodeRisk(riskyRedirects)
          if (!confirmed) return
        }
      }

      // Validate model_mapping JSON format
      const hasModelMapping =
        typeof data.model_mapping === 'string' &&
        data.model_mapping.trim() !== ''
      const modelMappingValue = data.model_mapping || ''

      if (hasModelMapping) {
        const validation = validateModelMappingJson(modelMappingValue)
        if (!validation.valid) {
          toast.error(t(validation.error || 'Invalid model mapping'))
          return
        }
      }

      // Normalize models array
      const normalizedModels = parseModelsString(data.models || '')

      // Check for missing models in model_mapping
      if (hasModelMapping) {
        const missingModels = findMissingModelsInMapping(
          modelMappingValue,
          normalizedModels
        )

        const shouldPromptMissing =
          missingModels.length > 0 &&
          hasModelConfigChanged(
            normalizedModels,
            data.model_mapping || '',
            initialModelsRef.current,
            initialModelMappingRef.current
          )

        if (shouldPromptMissing) {
          const confirmAction = await confirmMissingModelMappings(missingModels)
          if (confirmAction === 'cancel') {
            return
          }
          if (confirmAction === 'add') {
            const updatedModels = [
              ...new Set([...normalizedModels, ...missingModels]),
            ]
            data.models = formatModelsArray(updatedModels)
            form.setValue('models', data.models)
          }
        }
      }

      await channelMutation.mutateAsync(data)
    },
    [
      isEditing,
      sensitiveLocked,
      form,
      confirmMissingModelMappings,
      confirmStatusCodeRisk,
      channelMutation,
      t,
    ]
  )

  const handleAdvancedSettingsOpenChange = useCallback((nextOpen: boolean) => {
    setAdvancedSettingsOpen(nextOpen)
    if (typeof window !== 'undefined') {
      window.localStorage.setItem(
        ADVANCED_SETTINGS_EXPANDED_KEY,
        String(nextOpen)
      )
    }
  }, [])

  const onInvalid: SubmitErrorHandler<ChannelFormValues> = useCallback(
    (errors) => {
      if (hasAdvancedSettingsErrors(errors)) {
        handleAdvancedSettingsOpenChange(true)
      }
      toast.error(t('Please fix the highlighted fields before saving'))
    },
    [handleAdvancedSettingsOpenChange, t]
  )

  // Handle drawer close
  const handleOpenChange = useCallback(
    (v: boolean) => {
      onOpenChange(v)
      if (!v) {
        form.reset(CHANNEL_FORM_DEFAULT_VALUES)
        setAdvancedSettingsOpen(false)
      }
    },
    [onOpenChange, form]
  )

  return (
    <>
      <Sheet open={open} onOpenChange={handleOpenChange}>
        <SheetContent className={sideDrawerContentClassName('sm:max-w-5xl')}>
          <SheetHeader className={sideDrawerHeaderClassName()}>
            <SheetTitle className='flex items-center gap-3'>
              <span className='bg-muted flex size-9 shrink-0 items-center justify-center rounded-md'>
                <ChannelTypeLogo type={currentType} size={22} />
              </span>
              <span>
                {isEditing ? t('Edit Channel') : t('Create Channel')}
                <span className='text-muted-foreground ml-2 text-sm font-normal'>
                  {t(currentTypeLabel)}
                </span>
              </span>
            </SheetTitle>
            <SheetDescription>
              {isEditing
                ? t(
                    "Update channel configuration and click save when you're done."
                  )
                : t(
                    'Add a new channel by providing the necessary information.'
                  )}
            </SheetDescription>
          </SheetHeader>

          {sensitiveLocked && (
            <Alert className='border-amber-200 bg-amber-50 text-amber-900 dark:border-amber-500/40 dark:bg-amber-500/10 dark:text-amber-50'>
              <AlertDescription>
                {t(
                  'Sensitive channel settings are read-only for your account.'
                )}{' '}
                {t(
                  'You can still edit non-sensitive operations fields such as models, groups, priority, and weight.'
                )}
              </AlertDescription>
            </Alert>
          )}

          <Form {...form}>
            <form
              id='channel-form'
              onSubmit={form.handleSubmit(onSubmit, onInvalid)}
              className={sideDrawerFormClassName('gap-5')}
            >
              {isChannelDetailLoading ? (
                <ChannelEditorLoadingState />
              ) : (
                <div className='grid gap-5 lg:grid-cols-[13rem_minmax(0,1fr)] lg:items-start'>
                  <ChannelEditorNav
                    providerLogo={
                      <ChannelTypeLogo type={currentType} size={18} />
                    }
                    providerLabel={t(currentTypeLabel)}
                    statusLabel={t(currentStatusLabel)}
                    progressLabel={progressLabel}
                    navigationLabel={t('Channels')}
                    items={editorNavItems}
                  />
                  <div className='flex min-w-0 flex-col gap-5'>
                    <ChannelIdentityFields
                      form={form}
                      channelTypeOptions={channelTypeOptions}
                      currentType={currentType}
                      isEditing={isEditing}
                      sensitiveLocked={sensitiveLocked}
                    />
                    <ChannelCredentialsFields
                      form={form}
                      currentType={currentType}
                      sensitiveLocked={sensitiveLocked}
                      isEditing={isEditing}
                      channelId={channelId}
                      isMultiKeyChannel={isMultiKeyChannel}
                      isBatchMode={isBatchMode}
                      multiKeyMode={multiKeyMode}
                      multiKeyType={multiKeyType}
                      keyMode={keyMode}
                      awsKeyType={awsKeyType}
                      vertexKeyType={vertexKeyType}
                      addModeOptions={addModeOptions}
                      advancedCustomStats={advancedCustomStats}
                      doubaoApiEditUnlocked={doubaoApiEditUnlocked}
                      canRevealChannelKey={canRevealChannelKey}
                      channelKey={channelKey}
                      isChannelKeyLoading={isChannelKeyLoading}
                      isCodexCredentialRefreshing={isCodexCredentialRefreshing}
                      verificationState={verificationState}
                      copyToClipboard={copyToClipboard}
                      handleApiConfigSecretClick={handleApiConfigSecretClick}
                      handleDeduplicateKeys={handleDeduplicateKeys}
                      handleRefreshCodexCredential={
                        handleRefreshCodexCredential
                      }
                      handleRevealKey={handleRevealKey}
                      setAdvancedCustomEditorOpen={setAdvancedCustomEditorOpen}
                    />
                    <ChannelModelsFields
                      form={form}
                      currentType={currentType}
                      isEditing={isEditing}
                      canEditSensitive={canEditSensitive}
                      isLoadingGroups={isLoadingGroups}
                      isSubmitting={isSubmitting}
                      allModelsList={allModelsList}
                      basicModels={basicModels}
                      currentModelsArray={currentModelsArray}
                      modelOptions={modelOptions}
                      groupOptions={groupOptions}
                      prefillGroups={prefillGroups}
                      modelMappingGuardrail={modelMappingGuardrail}
                      mappingPreviewPairs={mappingPreviewPairs}
                      remainingMappingCount={remainingMappingCount}
                      updateModels={updateModels}
                      handleModelsChange={handleModelsChange}
                      handleFillRelatedModels={handleFillRelatedModels}
                      handleFillAllModels={handleFillAllModels}
                      handleFetchModels={handleFetchModels}
                      handleCopyModels={handleCopyModels}
                      handleClearModels={handleClearModels}
                      handleAddPrefillGroup={handleAddPrefillGroup}
                    />
                    <ChannelAdvancedFields
                      form={form}
                      currentType={currentType}
                      advancedSettingsOpen={advancedSettingsOpen}
                      advancedSummary={advancedSummary}
                      isSubmitting={isSubmitting}
                      sensitiveLocked={sensitiveLocked}
                      upstreamModelUpdateCheckEnabled={
                        upstreamModelUpdateCheckEnabled
                      }
                      upstreamUpdateMeta={upstreamUpdateMeta}
                      upstreamDetectedModelsPreview={
                        upstreamDetectedModelsPreview
                      }
                      upstreamDetectedModelsOmittedCount={
                        upstreamDetectedModelsOmittedCount
                      }
                      handleAdvancedSettingsOpenChange={
                        handleAdvancedSettingsOpenChange
                      }
                      setParamOverrideEditorOpen={setParamOverrideEditorOpen}
                    />
                  </div>
                </div>
              )}
            </form>
          </Form>

          <SheetFooter className={sideDrawerFooterClassName()}>
            <SheetClose
              render={<Button variant='outline' disabled={isSubmitting} />}
            >
              {t('Cancel')}
            </SheetClose>
            <Button form='channel-form' type='submit' disabled={isSubmitting}>
              {isSubmitting && (
                <Loader2 className='mr-2 h-4 w-4 animate-spin' />
              )}
              {isEditing ? t('Update Channel') : t('Save changes')}
            </Button>
          </SheetFooter>
        </SheetContent>
      </Sheet>

      {paramOverrideEditorOpen && !sensitiveLocked && (
        <ParamOverrideEditorDialog
          open={paramOverrideEditorOpen}
          value={form.watch('param_override') || ''}
          onOpenChange={setParamOverrideEditorOpen}
          onSave={(nextValue) => {
            form.setValue('param_override', nextValue, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }}
        />
      )}

      {advancedCustomEditorOpen && !sensitiveLocked && (
        <AdvancedCustomEditorDialog
          open={advancedCustomEditorOpen}
          value={form.watch('advanced_custom') || ''}
          onOpenChange={setAdvancedCustomEditorOpen}
          onSave={(nextValue) => {
            form.setValue('advanced_custom', nextValue, {
              shouldDirty: true,
              shouldValidate: true,
            })
          }}
        />
      )}

      {/* Fetch Models Dialog */}
      <FetchModelsDialog
        open={fetchModelsDialogOpen}
        onOpenChange={setFetchModelsDialogOpen}
        onModelsSelected={(models) => {
          form.setValue('models', formatModelsArray(models))
        }}
        redirectModels={redirectModelList}
        redirectSourceModels={redirectModelKeyList}
        customFetcher={!isEditing ? createModeFetcher : undefined}
        channelName={!isEditing ? currentName?.trim() : undefined}
        existingModelsOverride={
          !isEditing
            ? parseModelsString(form.getValues('models') || '')
            : undefined
        }
      />

      <SecureVerificationDialog
        open={verificationOpen}
        onOpenChange={(open) => {
          if (!open) {
            cancelVerification()
          }
        }}
        methods={verificationMethods}
        state={verificationState}
        onVerify={async (method, code) => {
          await executeVerification(method, code)
        }}
        onCancel={cancelVerification}
        onCodeChange={setVerificationCode}
        onMethodChange={switchVerificationMethod}
      />

      {/* Missing Models Confirmation Dialog */}
      <MissingModelsConfirmationDialog
        open={missingModelsDialogOpen}
        missingModels={missingModelsList}
        onConfirm={handleMissingModelsAction}
        onOpenChange={setMissingModelsDialogOpen}
      />

      <StatusCodeRiskDialog
        open={statusCodeRiskOpen}
        onOpenChange={(v) => {
          if (!v) handleStatusCodeRiskAction(false)
        }}
        detailItems={statusCodeRiskDetailItems}
        onConfirm={() => handleStatusCodeRiskAction(true)}
      />
    </>
  )
}
