import { GripVertical, Plus, Search } from 'lucide-react'
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
  type DragEvent,
  type KeyboardEvent,
  useCallback,
  useEffect,
  useMemo,
  useState,
} from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Dialog } from '@/components/dialog'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import { Input } from '@/components/ui/input'
import { ScrollArea } from '@/components/ui/scroll-area'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import { cn } from '@/lib/utils'

import {
  LEGACY_TEMPLATE,
  OPERATION_MODE_LABEL_MAP,
  OPERATION_TEMPLATE,
  TEMPLATE_PRESET_CONFIG,
  buildOperationsJson,
  buildPruneObjectsValueText,
  buildReturnErrorValueText,
  createDefaultCondition,
  createDefaultOperation,
  getModeTagTailwind,
  getOperationSummary,
  isOperationBlank,
  normalizeOperation,
  normalizePruneRule,
  parseInitialState,
  parseLooseValue,
  parsePruneObjectsDraft,
  parseReturnErrorDraft,
  reorderOperations,
  verifyJSON,
  type ParamOverrideCondition,
  type ParamOverrideEditorDialogProps,
  type ParamOverrideOperation,
  type PruneObjectsDraft,
  type PruneRule,
  type ReturnErrorDraft,
} from './param-override-editor-model'
import { RuleEditor } from './param-override-rule-editor'

export type { ParamOverrideEditorDialogProps } from './param-override-editor-model'

export function ParamOverrideEditorDialog(
  props: ParamOverrideEditorDialogProps
) {
  const { t } = useTranslation()

  const [editMode, setEditMode] = useState<'visual' | 'json'>('visual')
  const [visualMode, setVisualMode] = useState<'operations' | 'legacy'>(
    'operations'
  )
  const [legacyValue, setLegacyValue] = useState('')
  const [operations, setOperations] = useState<ParamOverrideOperation[]>([
    createDefaultOperation(),
  ])
  const [jsonText, setJsonText] = useState('')
  const [jsonError, setJsonError] = useState('')
  const [operationSearch, setOperationSearch] = useState('')
  const [selectedOperationId, setSelectedOperationId] = useState('')
  const [expandedConditions, setExpandedConditions] = useState<
    Record<string, boolean>
  >({})
  const [draggedOperationId, setDraggedOperationId] = useState('')
  const [dragOverOperationId, setDragOverOperationId] = useState('')
  const [dragOverPosition, setDragOverPosition] = useState<'before' | 'after'>(
    'before'
  )
  const [templatePresetKey, setTemplatePresetKey] =
    useState('operations_default')

  // Initialize state when dialog opens
  useEffect(() => {
    if (!props.open) return
    const state = parseInitialState(props.value)
    setEditMode(state.editMode)
    setVisualMode(state.visualMode)
    setLegacyValue(state.legacyValue)
    setOperations(state.operations)
    setJsonText(state.jsonText)
    setJsonError(state.jsonError)
    setOperationSearch('')
    setSelectedOperationId(state.operations[0]?.id || '')
    setExpandedConditions({})
    setDraggedOperationId('')
    setDragOverOperationId('')
    setDragOverPosition('before')
    if (state.visualMode === 'legacy') {
      setTemplatePresetKey('legacy_default')
    } else {
      setTemplatePresetKey('operations_default')
    }
  }, [props.open, props.value])

  // Keep selectedOperationId valid
  useEffect(() => {
    if (operations.length === 0) {
      setSelectedOperationId('')
      return
    }
    if (!operations.some((o) => o.id === selectedOperationId)) {
      setSelectedOperationId(operations[0].id)
    }
  }, [operations, selectedOperationId])

  // Template preset options filtered by group
  const templatePresetOptions = useMemo(
    () =>
      Object.entries(TEMPLATE_PRESET_CONFIG).map(([value, config]) => ({
        value,
        label: config.label,
      })),
    []
  )

  const operationCount = useMemo(
    () => operations.filter((o) => !isOperationBlank(o)).length,
    [operations]
  )

  const filteredOperations = useMemo(() => {
    const keyword = operationSearch.trim().toLowerCase()
    if (!keyword) return operations
    return operations.filter((op) => {
      const searchableText = [
        op.description,
        op.mode,
        op.path,
        op.from,
        op.to,
        op.value_text,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase()
      return searchableText.includes(keyword)
    })
  }, [operationSearch, operations])

  const selectedOperation = useMemo(
    () => operations.find((o) => o.id === selectedOperationId),
    [operations, selectedOperationId]
  )

  const selectedOperationIndex = useMemo(
    () => operations.findIndex((o) => o.id === selectedOperationId),
    [operations, selectedOperationId]
  )

  const returnErrorDraft = useMemo(() => {
    if (!selectedOperation || selectedOperation.mode !== 'return_error') {
      return null
    }
    return parseReturnErrorDraft(selectedOperation.value_text)
  }, [selectedOperation])

  const pruneObjectsDraft = useMemo(() => {
    if (!selectedOperation || selectedOperation.mode !== 'prune_objects') {
      return null
    }
    return parsePruneObjectsDraft(selectedOperation.value_text)
  }, [selectedOperation])

  const topOperationModes = useMemo(() => {
    const counts: Record<string, number> = {}
    for (const op of operations) {
      const mode = op.mode || 'set'
      counts[mode] = (counts[mode] || 0) + 1
    }
    return Object.entries(counts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 4)
  }, [operations])

  // ---------------------------------------------------------------------------
  // Operations
  // ---------------------------------------------------------------------------

  const updateOperation = useCallback(
    (operationId: string, patch: Partial<ParamOverrideOperation>) => {
      setOperations((prev) =>
        prev.map((o) => (o.id === operationId ? { ...o, ...patch } : o))
      )
    },
    []
  )

  const addOperation = useCallback(() => {
    const created = createDefaultOperation()
    setOperations((prev) => [...prev, created])
    setSelectedOperationId(created.id)
  }, [])

  const duplicateOperation = useCallback((operationId: string) => {
    let insertedId = ''
    setOperations((prev) => {
      const idx = prev.findIndex((o) => o.id === operationId)
      if (idx < 0) return prev
      const source = prev[idx]
      const cloned = normalizeOperation({
        description: source.description,
        path: source.path,
        mode: source.mode,
        value: parseLooseValue(source.value_text),
        keep_origin: source.keep_origin,
        from: source.from,
        to: source.to,
        logic: source.logic,
        conditions: source.conditions.map((c) => ({
          path: c.path,
          mode: c.mode,
          value: parseLooseValue(c.value_text),
          invert: c.invert,
          pass_missing_key: c.pass_missing_key,
        })),
      })
      insertedId = cloned.id
      const next = [...prev]
      next.splice(idx + 1, 0, cloned)
      return next
    })
    if (insertedId) setSelectedOperationId(insertedId)
  }, [])

  const removeOperation = useCallback((operationId: string) => {
    setOperations((prev) => {
      if (prev.length <= 1) return [createDefaultOperation()]
      return prev.filter((o) => o.id !== operationId)
    })
  }, [])

  // Conditions
  const addCondition = useCallback((operationId: string) => {
    const created = createDefaultCondition()
    setOperations((prev) =>
      prev.map((op) =>
        op.id === operationId
          ? { ...op, conditions: [...op.conditions, created] }
          : op
      )
    )
    setExpandedConditions((prev) => ({ ...prev, [created.id]: true }))
  }, [])

  const updateCondition = useCallback(
    (
      operationId: string,
      conditionId: string,
      patch: Partial<ParamOverrideCondition>
    ) => {
      setOperations((prev) =>
        prev.map((op) =>
          op.id === operationId
            ? {
                ...op,
                conditions: op.conditions.map((c) =>
                  c.id === conditionId ? { ...c, ...patch } : c
                ),
              }
            : op
        )
      )
    },
    []
  )

  const removeCondition = useCallback(
    (operationId: string, conditionId: string) => {
      setOperations((prev) =>
        prev.map((op) =>
          op.id === operationId
            ? {
                ...op,
                conditions: op.conditions.filter((c) => c.id !== conditionId),
              }
            : op
        )
      )
    },
    []
  )

  // return_error draft
  const updateReturnErrorDraft = useCallback(
    (operationId: string, draftPatch: Partial<ReturnErrorDraft>) => {
      setOperations((prev) =>
        prev.map((op) => {
          if (op.id !== operationId) return op
          const draft = parseReturnErrorDraft(op.value_text)
          const nextDraft = { ...draft, ...draftPatch }
          return {
            ...op,
            value_text: buildReturnErrorValueText(nextDraft),
          }
        })
      )
    },
    []
  )

  // prune_objects draft
  const updatePruneObjectsDraft = useCallback(
    (
      operationId: string,
      updater:
        | Partial<PruneObjectsDraft>
        | ((draft: PruneObjectsDraft) => PruneObjectsDraft)
    ) => {
      setOperations((prev) =>
        prev.map((op) => {
          if (op.id !== operationId) return op
          const draft = parsePruneObjectsDraft(op.value_text)
          const nextDraft =
            typeof updater === 'function'
              ? updater(draft)
              : { ...draft, ...updater }
          return {
            ...op,
            value_text: buildPruneObjectsValueText(nextDraft),
          }
        })
      )
    },
    []
  )

  const addPruneRule = useCallback(
    (operationId: string) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        simpleMode: false,
        rules: [...draft.rules, normalizePruneRule({})],
      }))
    },
    [updatePruneObjectsDraft]
  )

  const updatePruneRule = useCallback(
    (operationId: string, ruleId: string, patch: Partial<PruneRule>) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        rules: draft.rules.map((r) =>
          r.id === ruleId ? { ...r, ...patch } : r
        ),
      }))
    },
    [updatePruneObjectsDraft]
  )

  const removePruneRule = useCallback(
    (operationId: string, ruleId: string) => {
      updatePruneObjectsDraft(operationId, (draft) => ({
        ...draft,
        rules: draft.rules.filter((r) => r.id !== ruleId),
      }))
    },
    [updatePruneObjectsDraft]
  )

  // Drag and drop
  const resetDragState = useCallback(() => {
    setDraggedOperationId('')
    setDragOverOperationId('')
    setDragOverPosition('before')
  }, [])

  const handleDragStart = useCallback(
    (event: DragEvent, operationId: string) => {
      setDraggedOperationId(operationId)
      setSelectedOperationId(operationId)
      event.dataTransfer.effectAllowed = 'move'
      event.dataTransfer.setData('text/plain', operationId)
    },
    []
  )

  const handleDragOver = useCallback(
    (event: DragEvent, operationId: string) => {
      event.preventDefault()
      if (!draggedOperationId || draggedOperationId === operationId) return
      const rect = event.currentTarget.getBoundingClientRect()
      const position: 'before' | 'after' =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before'
      setDragOverOperationId(operationId)
      setDragOverPosition(position)
      event.dataTransfer.dropEffect = 'move'
    },
    [draggedOperationId]
  )

  const handleDrop = useCallback(
    (event: DragEvent, operationId: string) => {
      event.preventDefault()
      const sourceId =
        draggedOperationId || event.dataTransfer.getData('text/plain')
      const position =
        dragOverOperationId === operationId ? dragOverPosition : 'before'
      if (sourceId && operationId && sourceId !== operationId) {
        setOperations((prev) =>
          reorderOperations(prev, sourceId, operationId, position)
        )
        setSelectedOperationId(sourceId)
      }
      resetDragState()
    },
    [draggedOperationId, dragOverOperationId, dragOverPosition, resetDragState]
  )

  // ---------------------------------------------------------------------------
  // Mode switching & templates
  // ---------------------------------------------------------------------------

  const buildVisualJson = useCallback((): string => {
    if (visualMode === 'legacy') {
      const trimmed = legacyValue.trim()
      if (!trimmed) return ''
      if (!verifyJSON(trimmed)) {
        throw new Error(t('Parameter override must be valid JSON format'))
      }
      const parsed = JSON.parse(trimmed) as unknown
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(t('Legacy format must be a JSON object'))
      }
      return JSON.stringify(parsed, null, 2)
    }
    return buildOperationsJson(operations, { validate: true }, t)
  }, [legacyValue, operations, t, visualMode])

  const switchToJsonMode = useCallback(() => {
    if (editMode === 'json') return
    try {
      setJsonText(buildVisualJson())
      setJsonError('')
    } catch (error) {
      toast.error((error as Error).message)
      if (visualMode === 'legacy') {
        setJsonText(legacyValue)
      } else {
        setJsonText(buildOperationsJson(operations, { validate: false }, t))
      }
      setJsonError(
        (error as Error).message || t('Parameter configuration error')
      )
    }
    setEditMode('json')
  }, [buildVisualJson, editMode, legacyValue, operations, t, visualMode])

  const switchToVisualMode = useCallback(() => {
    if (editMode === 'visual') return
    const trimmed = jsonText.trim()
    if (!trimmed) {
      const fallback = createDefaultOperation()
      setVisualMode('operations')
      setOperations([fallback])
      setSelectedOperationId(fallback.id)
      setLegacyValue('')
      setJsonError('')
      setEditMode('visual')
      return
    }
    if (!verifyJSON(trimmed)) {
      toast.error(t('Parameter override must be valid JSON format'))
      return
    }
    const parsed = JSON.parse(trimmed) as Record<string, unknown>
    if (
      parsed &&
      typeof parsed === 'object' &&
      !Array.isArray(parsed) &&
      Array.isArray(parsed.operations)
    ) {
      const nextOps =
        (parsed.operations as Record<string, unknown>[]).length > 0
          ? (parsed.operations as Record<string, unknown>[]).map(
              normalizeOperation
            )
          : [createDefaultOperation()]
      setVisualMode('operations')
      setOperations(nextOps)
      setSelectedOperationId(nextOps[0]?.id || '')
      setLegacyValue('')
      setJsonError('')
      setEditMode('visual')
      setTemplatePresetKey('operations_default')
      return
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const fallback = createDefaultOperation()
      setVisualMode('legacy')
      setLegacyValue(JSON.stringify(parsed, null, 2))
      setOperations([fallback])
      setSelectedOperationId(fallback.id)
      setJsonError('')
      setEditMode('visual')
      setTemplatePresetKey('legacy_default')
      return
    }
    toast.error(t('Parameter override must be a valid JSON object'))
  }, [editMode, jsonText, t])

  const fillTemplate = useCallback(
    (mode: 'fill' | 'append') => {
      const preset =
        TEMPLATE_PRESET_CONFIG[templatePresetKey] ||
        TEMPLATE_PRESET_CONFIG.operations_default
      const payload = preset.payload as Record<string, unknown>

      if (preset.kind === 'legacy') {
        if (mode === 'append' && visualMode === 'legacy') {
          const trimmed = legacyValue.trim()
          let parsedCurrent: Record<string, unknown> = {}
          if (trimmed) {
            if (!verifyJSON(trimmed)) {
              toast.error(t('Current legacy JSON is invalid, cannot append'))
              return
            }
            parsedCurrent = JSON.parse(trimmed) as Record<string, unknown>
          }
          const merged = { ...payload, ...parsedCurrent }
          const text = JSON.stringify(merged, null, 2)
          setVisualMode('legacy')
          setLegacyValue(text)
          setOperations([createDefaultOperation()])
          setJsonText(text)
          setJsonError('')
          setEditMode('visual')
        } else {
          const text = JSON.stringify(payload, null, 2)
          setVisualMode('legacy')
          setLegacyValue(text)
          setOperations([createDefaultOperation()])
          setJsonText(text)
          setJsonError('')
          setEditMode('visual')
        }
        return
      }

      const operationsPayload = ((payload as Record<string, unknown>)
        .operations || []) as Record<string, unknown>[]

      if (mode === 'append') {
        const appended = operationsPayload.map(normalizeOperation)
        const existing =
          visualMode === 'operations'
            ? operations.filter((o) => !isOperationBlank(o))
            : []
        const nextOps = [...existing, ...appended]
        setVisualMode('operations')
        setOperations(nextOps.length > 0 ? nextOps : appended)
        setSelectedOperationId(nextOps[0]?.id || appended[0]?.id || '')
        setLegacyValue('')
        setJsonError('')
        setEditMode('visual')
        setJsonText('')
      } else {
        const nextOps = operationsPayload.map(normalizeOperation)
        const finalOps =
          nextOps.length > 0 ? nextOps : [createDefaultOperation()]
        setVisualMode('operations')
        setOperations(finalOps)
        setSelectedOperationId(finalOps[0]?.id || '')
        setJsonText(JSON.stringify({ operations: operationsPayload }, null, 2))
        setJsonError('')
        setEditMode('visual')
      }
    },
    [legacyValue, operations, templatePresetKey, visualMode, t]
  )

  const resetEditorState = useCallback(() => {
    const fallback = createDefaultOperation()
    setVisualMode('operations')
    setLegacyValue('')
    setOperations([fallback])
    setSelectedOperationId(fallback.id)
    setJsonText('')
    setJsonError('')
    setTemplatePresetKey('operations_default')
    setEditMode('visual')
  }, [])

  // JSON mode
  const handleJsonChange = useCallback(
    (nextValue: string) => {
      setJsonText(nextValue)
      const trimmed = nextValue.trim()
      if (!trimmed) {
        setJsonError('')
        return
      }
      setJsonError(verifyJSON(trimmed) ? '' : t('JSON format error'))
    },
    [t]
  )

  const formatJson = useCallback(() => {
    const trimmed = jsonText.trim()
    if (!trimmed) return
    if (!verifyJSON(trimmed)) {
      toast.error(t('Parameter override must be valid JSON format'))
      return
    }
    setJsonText(JSON.stringify(JSON.parse(trimmed), null, 2))
    setJsonError('')
  }, [jsonText, t])

  const visualValidationError = useMemo(() => {
    if (editMode !== 'visual') return ''
    try {
      buildVisualJson()
      return ''
    } catch (error) {
      return (error as Error)?.message || t('Parameter configuration error')
    }
  }, [buildVisualJson, editMode, t])

  // Save
  const handleSave = useCallback(() => {
    try {
      let result = ''
      if (editMode === 'json') {
        const trimmed = jsonText.trim()
        if (trimmed) {
          if (!verifyJSON(trimmed)) {
            throw new Error(t('Parameter override must be valid JSON format'))
          }
          result = JSON.stringify(JSON.parse(trimmed), null, 2)
        }
      } else {
        result = buildVisualJson()
      }
      props.onSave(result)
      props.onOpenChange(false)
    } catch (error) {
      toast.error((error as Error).message)
    }
  }, [buildVisualJson, editMode, jsonText, props, t])

  // Expand/collapse all conditions
  const expandAllConditions = useCallback(() => {
    if (!selectedOperation) return
    const map: Record<string, boolean> = {}
    for (const c of selectedOperation.conditions) map[c.id] = true
    setExpandedConditions((prev) => ({ ...prev, ...map }))
  }, [selectedOperation])

  const collapseAllConditions = useCallback(() => {
    if (!selectedOperation) return
    const map: Record<string, boolean> = {}
    for (const c of selectedOperation.conditions) map[c.id] = false
    setExpandedConditions((prev) => ({ ...prev, ...map }))
  }, [selectedOperation])

  // ---------------------------------------------------------------------------
  // Render
  // ---------------------------------------------------------------------------

  return (
    <Dialog
      open={props.open}
      onOpenChange={props.onOpenChange}
      title={t('Parameter Override')}
      description={t(
        'Create request parameter override rules with a visual editor or raw JSON.'
      )}
      contentClassName='flex max-h-[90vh] flex-col gap-0 p-0 sm:max-w-5xl'
      headerClassName='border-b px-6 py-4'
      footerClassName='border-t px-6 py-4'
      contentHeight='min(72vh, 720px)'
      bodyClassName='space-y-4'
      footer={
        <>
          <Button
            type='button'
            variant='outline'
            onClick={() => props.onOpenChange(false)}
          >
            {t('Cancel')}
          </Button>
          <Button type='button' onClick={handleSave}>
            {t('Save')}
          </Button>
        </>
      }
    >
      {/* Toolbar */}
      <div className='bg-muted/30 border-b px-4 py-3'>
        <div className='flex flex-wrap items-center gap-2'>
          <span className='text-muted-foreground text-xs font-medium'>
            {t('Mode')}
          </span>
          <Button
            type='button'
            variant={editMode === 'visual' ? 'default' : 'outline'}
            size='sm'
            onClick={switchToVisualMode}
          >
            {t('Visual')}
          </Button>
          <Button
            type='button'
            variant={editMode === 'json' ? 'default' : 'outline'}
            size='sm'
            onClick={switchToJsonMode}
          >
            {t('JSON Text')}
          </Button>

          <div className='bg-border mx-1 h-5 w-px' />

          <span className='text-muted-foreground text-xs font-medium'>
            {t('Template')}
          </span>
          <Select
            items={templatePresetOptions.map((o) => ({
              value: o.value,
              label: t(o.label),
            }))}
            value={templatePresetKey}
            onValueChange={(v) =>
              setTemplatePresetKey(v || 'operations_default')
            }
          >
            <SelectTrigger className='h-8 w-[220px]'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                {templatePresetOptions.map((o) => (
                  <SelectItem key={o.value} value={o.value}>
                    {t(o.label)}
                  </SelectItem>
                ))}
              </SelectGroup>
            </SelectContent>
          </Select>
          <Button
            type='button'
            variant='outline'
            size='sm'
            onClick={() => fillTemplate('fill')}
          >
            {t('Fill Template')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={() => fillTemplate('append')}
          >
            {t('Append Template')}
          </Button>
          <Button
            type='button'
            variant='ghost'
            size='sm'
            onClick={resetEditorState}
          >
            {t('Reset')}
          </Button>
        </div>
      </div>
      {/* Content */}
      <div className='min-h-0 flex-1 overflow-hidden'>
        {editMode === 'visual' && visualMode === 'legacy' && (
          <div className='p-4'>
            <p className='text-muted-foreground mb-2 text-sm'>
              {t('Legacy Format (JSON Object)')}
            </p>
            <Textarea
              value={legacyValue}
              onChange={(e) => setLegacyValue(e.target.value)}
              placeholder={JSON.stringify(LEGACY_TEMPLATE, null, 2)}
              rows={14}
              className='font-mono text-xs'
            />
            <p className='text-muted-foreground mt-2 text-xs'>
              {t(
                'Edit JSON object directly. Suitable for simple parameter overrides.'
              )}
            </p>
          </div>
        )}
        {editMode === 'visual' && visualMode !== 'legacy' && (
          <div className='flex h-full'>
            {/* Left sidebar */}
            <div className='flex w-[280px] flex-shrink-0 flex-col border-r'>
              <div className='flex items-center justify-between border-b px-3 py-2'>
                <div className='flex items-center gap-2'>
                  <span className='text-sm font-medium'>{t('Rules')}</span>
                  <Badge variant='secondary'>
                    {operationCount}/{operations.length}
                  </Badge>
                </div>
                <Button
                  type='button'
                  variant='ghost'
                  size='sm'
                  onClick={addOperation}
                >
                  <Plus className='h-4 w-4' />
                </Button>
              </div>

              {topOperationModes.length > 0 && (
                <div className='flex flex-wrap gap-1 border-b px-3 py-2'>
                  {topOperationModes.map(([mode, count]) => (
                    <span
                      key={`mode_stat_${mode}`}
                      className={cn(
                        'inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium',
                        getModeTagTailwind(mode)
                      )}
                    >
                      {t(OPERATION_MODE_LABEL_MAP[mode] || mode)} · {count}
                    </span>
                  ))}
                </div>
              )}

              <div className='px-3 py-2'>
                <div className='relative'>
                  <Search className='text-muted-foreground absolute top-2.5 left-2.5 h-3.5 w-3.5' />
                  <Input
                    value={operationSearch}
                    onChange={(e) => setOperationSearch(e.target.value)}
                    placeholder={t('Search rules...')}
                    className='h-8 pl-8 text-xs'
                  />
                </div>
              </div>

              <ScrollArea className='flex-1'>
                <div className='flex flex-col gap-1 px-3 pb-3'>
                  {filteredOperations.length === 0 ? (
                    <p className='text-muted-foreground py-4 text-center text-xs'>
                      {t('No matching rules')}
                    </p>
                  ) : (
                    filteredOperations.map((operation) => {
                      const index = operations.findIndex(
                        (o) => o.id === operation.id
                      )
                      const isActive = operation.id === selectedOperationId
                      const isDragging = operation.id === draggedOperationId
                      const isDropTarget =
                        operation.id === dragOverOperationId &&
                        draggedOperationId !== '' &&
                        draggedOperationId !== operation.id
                      return (
                        <div
                          key={operation.id}
                          role='button'
                          tabIndex={0}
                          draggable={operations.length > 1}
                          onClick={() => setSelectedOperationId(operation.id)}
                          onDragStart={(e) => handleDragStart(e, operation.id)}
                          onDragOver={(e) => handleDragOver(e, operation.id)}
                          onDrop={(e) => handleDrop(e, operation.id)}
                          onDragEnd={resetDragState}
                          onKeyDown={(e: KeyboardEvent) => {
                            if (e.key === 'Enter' || e.key === ' ') {
                              e.preventDefault()
                              setSelectedOperationId(operation.id)
                            }
                          }}
                          className={cn(
                            'cursor-pointer rounded-lg border p-2.5 transition-colors',
                            isActive
                              ? 'border-primary bg-primary/5'
                              : 'hover:bg-muted/50',
                            isDragging && 'opacity-50',
                            isDropTarget &&
                              dragOverPosition === 'before' &&
                              'border-t-primary border-t-2',
                            isDropTarget &&
                              dragOverPosition === 'after' &&
                              'border-b-primary border-b-2'
                          )}
                        >
                          <div className='flex items-start gap-2'>
                            <GripVertical
                              className={cn(
                                'text-muted-foreground mt-0.5 h-3.5 w-3.5 flex-shrink-0',
                                operations.length > 1
                                  ? 'cursor-grab'
                                  : 'cursor-default'
                              )}
                            />
                            <div className='min-w-0 flex-1'>
                              <div className='flex items-center justify-between gap-1'>
                                <span className='text-xs font-semibold'>
                                  #{index + 1}
                                </span>
                                <Badge
                                  variant='outline'
                                  className='text-[10px]'
                                >
                                  {operation.conditions.length}
                                </Badge>
                              </div>
                              <p className='text-muted-foreground mt-0.5 line-clamp-1 text-[11px]'>
                                {getOperationSummary(operation, index)}
                              </p>
                              {operation.description.trim() && (
                                <p className='text-muted-foreground mt-0.5 line-clamp-2 text-[10px]'>
                                  {operation.description}
                                </p>
                              )}
                              <span
                                className={cn(
                                  'mt-1 inline-flex items-center rounded-md border px-1.5 py-0.5 text-[10px] font-medium',
                                  getModeTagTailwind(operation.mode || 'set')
                                )}
                              >
                                {t(
                                  OPERATION_MODE_LABEL_MAP[
                                    operation.mode || 'set'
                                  ] ||
                                    operation.mode ||
                                    'set'
                                )}
                              </span>
                            </div>
                          </div>
                        </div>
                      )
                    })
                  )}
                </div>
              </ScrollArea>
            </div>

            {/* Right panel - Rule editor */}
            <div className='flex min-w-0 flex-1 flex-col overflow-y-auto'>
              {selectedOperation ? (
                <RuleEditor
                  operation={selectedOperation}
                  operationIndex={selectedOperationIndex}
                  operations={operations}
                  returnErrorDraft={returnErrorDraft}
                  pruneObjectsDraft={pruneObjectsDraft}
                  expandedConditions={expandedConditions}
                  setExpandedConditions={setExpandedConditions}
                  updateOperation={updateOperation}
                  duplicateOperation={duplicateOperation}
                  removeOperation={removeOperation}
                  addCondition={addCondition}
                  updateCondition={updateCondition}
                  removeCondition={removeCondition}
                  updateReturnErrorDraft={updateReturnErrorDraft}
                  updatePruneObjectsDraft={updatePruneObjectsDraft}
                  addPruneRule={addPruneRule}
                  updatePruneRule={updatePruneRule}
                  removePruneRule={removePruneRule}
                  expandAllConditions={expandAllConditions}
                  collapseAllConditions={collapseAllConditions}
                />
              ) : (
                <div className='flex flex-1 items-center justify-center'>
                  <p className='text-muted-foreground text-sm'>
                    {t('Select a rule to edit.')}
                  </p>
                </div>
              )}

              {visualValidationError && (
                <div className='border-t px-4 py-2'>
                  <p className='text-destructive text-xs'>
                    {visualValidationError}
                  </p>
                </div>
              )}
            </div>
          </div>
        )}
        {editMode === 'json' && (
          /* JSON mode */
          <div className='p-4'>
            <div className='mb-2 flex items-center gap-2'>
              <Button
                type='button'
                variant='outline'
                size='sm'
                onClick={formatJson}
              >
                {t('Format')}
              </Button>
              <span className='text-muted-foreground text-xs'>
                {t('Advanced text editing')}
              </span>
            </div>
            <Textarea
              value={jsonText}
              onChange={(e) => handleJsonChange(e.target.value)}
              placeholder={JSON.stringify(OPERATION_TEMPLATE, null, 2)}
              rows={20}
              className='font-mono text-xs'
            />
            <p className='text-muted-foreground mt-2 text-xs'>
              {t('Edit JSON text directly. Format will be validated on save.')}
            </p>
            {jsonError && (
              <p className='text-destructive mt-1 text-xs'>{jsonError}</p>
            )}
          </div>
        )}
      </div>
      {/* Footer */}
    </Dialog>
  )
}
