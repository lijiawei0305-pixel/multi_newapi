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
import { Copy, Plus, Trash2 } from 'lucide-react'
import { memo, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useTranslation } from 'react-i18next'
import { toast } from 'sonner'

import { Alert, AlertDescription } from '@/components/ui/alert'
import { Badge } from '@/components/ui/badge'
import { Button } from '@/components/ui/button'
import {
  Collapsible,
  CollapsibleContent,
  CollapsibleTrigger,
} from '@/components/ui/collapsible'
import { Field, FieldLabel } from '@/components/ui/field'
import { Input } from '@/components/ui/input'
import { Label } from '@/components/ui/label'
import {
  Select,
  SelectContent,
  SelectGroup,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select'
import { Textarea } from '@/components/ui/textarea'
import {
  COMMON_TIMEZONES,
  MATCH_CONTAINS,
  MATCH_EQ,
  MATCH_EXISTS,
  MATCH_GT,
  MATCH_GTE,
  MATCH_LT,
  MATCH_LTE,
  MATCH_RANGE,
  SOURCE_HEADER,
  SOURCE_PARAM,
  SOURCE_TIME,
  TIME_FUNCS,
  buildRequestRuleExpr,
  combineBillingExpr,
  createEmptyCondition,
  createEmptyRuleGroup,
  createEmptyTimeCondition,
  getRequestRuleMatchOptions,
  splitBillingExprAndRequestRules,
  tryParseRequestRuleExpr,
  type ParamHeaderCondition,
  type RequestCondition,
  type RequestRuleGroup,
  type TimeCondition,
  type TimeFunc,
} from '@/features/pricing/lib/billing-expr'
import {
  type VisualConfig,
  createInitialVisualEditorState,
  createDefaultVisualConfig,
  generateExprFromVisualConfig,
  tryParseVisualConfig,
} from '@/features/pricing/lib/tier-expr'

import { BillingCostEstimator } from './billing-cost-estimator'
import { DraftNumberInput } from './draft-number-input'
import { VisualEditor } from './tiered-pricing-visual-editor'

type Preset = {
  key: string
  label: string
  expr: string
  requestRules?: RequestRuleGroup[]
}

type PresetGroup = {
  group: string
  presets: Preset[]
}

const PRESET_GROUPS: PresetGroup[] = [
  {
    group: 'Fixed price',
    presets: [
      { key: 'flat', label: 'Flat', expr: 'tier("base", p * 2 + c * 4)' },
      {
        key: 'claude-opus',
        label: 'Claude Opus 4.6',
        expr: 'tier("base", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)',
      },
      {
        key: 'gpt-5.4',
        label: 'GPT-5.4',
        expr: 'len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)',
      },
    ],
  },
  {
    group: 'Tiered',
    presets: [
      {
        key: 'claude-sonnet',
        label: 'Claude Sonnet 4.5',
        expr: 'len <= 200000 ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6) : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)',
      },
      {
        key: 'qwen3-max',
        label: 'Qwen3 Max',
        expr: 'len <= 32000 ? tier("short", p * 1.2 + c * 6 + cr * 0.24 + cc * 1.5) : len <= 128000 ? tier("mid", p * 2.4 + c * 12 + cr * 0.48 + cc * 3) : tier("long", p * 3 + c * 15 + cr * 0.6 + cc * 3.75)',
      },
      {
        key: 'glm-4.5-air',
        label: 'GLM-4.5 Air',
        expr: 'len < 32000 && c < 200 ? tier("short_output", p * 0.8 + c * 2 + cr * 0.16) : len < 32000 && c >= 200 ? tier("long_output", p * 0.8 + c * 6 + cr * 0.16) : tier("mid_context", p * 1.2 + c * 8 + cr * 0.24)',
      },
      {
        key: 'doubao-seed-1.8',
        label: 'Doubao Seed 1.8',
        expr: 'len <= 32000 && c <= 200 ? tier("discount", p * 0.8 + c * 2 + cr * 0.16 + cc * 0.17) : len <= 32000 ? tier("short", p * 0.8 + c * 8 + cr * 0.16 + cc * 0.17) : len <= 128000 ? tier("mid", p * 1.2 + c * 16 + cr * 0.16 + cc * 0.17) : tier("long", p * 2.4 + c * 24 + cr * 0.16 + cc * 0.17)',
      },
    ],
  },
  {
    group: 'Multimodal',
    presets: [
      {
        key: 'gpt-image-1-mini',
        label: 'GPT Image 1 Mini',
        expr: 'tier("base", p * 2 + c * 8 + img * 2.5)',
      },
      {
        key: 'gemini-2.5-flash',
        label: 'Gemini 2.5 Flash',
        expr: 'tier("base", p * 0.3 + c * 2.5 + cr * 0.03 + ai * 1.0)',
      },
      {
        key: 'gemini-3-pro-image',
        label: 'Gemini 3 Pro Image',
        expr: 'tier("base", p * 2 + c * 12 + img_o * 120)',
      },
      {
        key: 'qwen3-omni-flash',
        label: 'Qwen3 Omni Flash',
        expr: 'tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)',
      },
    ],
  },
  {
    group: 'Request rule',
    presets: [
      {
        key: 'claude-opus-fast',
        label: 'Claude Opus 4.6 Fast',
        expr: 'tier("base", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_HEADER as 'header',
                path: 'anthropic-beta',
                mode: MATCH_CONTAINS,
                value: 'fast-mode-2026-02-01',
              },
            ],
            multiplier: '6',
          },
        ],
      },
      {
        key: 'gpt-5.4-tiers',
        label: 'GPT-5.4 Priority/Flex',
        expr: 'len <= 272000 ? tier("standard", p * 2.5 + c * 15 + cr * 0.25) : tier("long_context", p * 5 + c * 22.5 + cr * 0.5)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_PARAM as 'param',
                path: 'service_tier',
                mode: MATCH_EQ,
                value: 'priority',
              },
            ],
            multiplier: '2',
          },
          {
            conditions: [
              {
                source: SOURCE_PARAM as 'param',
                path: 'service_tier',
                mode: MATCH_EQ,
                value: 'flex',
              },
            ],
            multiplier: '0.5',
          },
        ],
      },
    ],
  },
  {
    group: 'Time-based',
    presets: [
      {
        key: 'night-discount',
        label: 'Night discount (50%)',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME as 'time',
                timeFunc: 'hour',
                timezone: 'Asia/Shanghai',
                mode: MATCH_RANGE,
                value: '',
                rangeStart: '21',
                rangeEnd: '6',
              },
            ],
            multiplier: '0.5',
          },
        ],
      },
      {
        key: 'weekend-discount',
        label: 'Weekend discount (80%)',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME as 'time',
                timeFunc: 'weekday',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '0',
                rangeStart: '',
                rangeEnd: '',
              },
            ],
            multiplier: '0.8',
          },
          {
            conditions: [
              {
                source: SOURCE_TIME as 'time',
                timeFunc: 'weekday',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '6',
                rangeStart: '',
                rangeEnd: '',
              },
            ],
            multiplier: '0.8',
          },
        ],
      },
    ],
  },
]

// ---------------------------------------------------------------------------
// Raw expression editor
// ---------------------------------------------------------------------------

type RawExprEditorProps = {
  exprString: string
  onChange: (value: string) => void
}

function RawExprEditor({ exprString, onChange }: RawExprEditorProps) {
  const { t } = useTranslation()
  return (
    <div className='space-y-3'>
      <Alert>
        <AlertDescription className='space-y-1 text-xs'>
          <div>
            {t('Variables')}: <code>len</code>, <code>p</code>, <code>c</code>,{' '}
            <code>cr</code>, <code>cc</code>, <code>cc1h</code>,{' '}
            <code>img</code>, <code>img_o</code>, <code>ai</code>,{' '}
            <code>ao</code>
          </div>
          <div>
            {t('Functions')}: <code>tier(name, value)</code>, <code>max</code>,{' '}
            <code>min</code>, <code>ceil</code>, <code>floor</code>,{' '}
            <code>abs</code>, <code>header(name)</code>,{' '}
            <code>param(path)</code>, <code>has(source, text)</code>
          </div>
        </AlertDescription>
      </Alert>
      <Textarea
        value={exprString}
        onChange={(event) => onChange(event.target.value)}
        placeholder='tier("base", p * 3 + c * 15)'
        rows={6}
        className='font-mono text-xs'
        spellCheck={false}
      />
    </div>
  )
}

// ---------------------------------------------------------------------------
// Request rule condition row
// ---------------------------------------------------------------------------

type RuleConditionRowProps = {
  condition: RequestCondition
  onChange: (next: RequestCondition) => void
  onRemove: () => void
}

function RuleConditionRow({
  condition,
  onChange,
  onRemove,
}: RuleConditionRowProps) {
  const { t } = useTranslation()
  const matchOptions = getRequestRuleMatchOptions(condition.source)
  const getMatchLabel = (mode: string) => {
    switch (mode) {
      case MATCH_EQ:
        return t('Equals')
      case MATCH_CONTAINS:
        return t('Contains')
      case MATCH_EXISTS:
        return t('Exists')
      case MATCH_GT:
        return t('Greater than')
      case MATCH_GTE:
        return t('Greater than or equal')
      case MATCH_LT:
        return t('Less than')
      case MATCH_LTE:
        return t('Less than or equal')
      case MATCH_RANGE:
        return t('Overnight range')
      default:
        return mode
    }
  }
  const getTimeFuncLabel = (timeFunc: TimeFunc) => {
    switch (timeFunc) {
      case 'hour':
        return t('Hour of day')
      case 'minute':
        return t('Minute')
      case 'weekday':
        return t('Weekday')
      case 'month':
        return t('Month number')
      case 'day':
        return t('Day of month')
      default:
        return timeFunc
    }
  }
  let sourceLabel = t('Time')
  if (condition.source === SOURCE_PARAM) sourceLabel = t('Body param')
  else if (condition.source === SOURCE_HEADER) sourceLabel = t('Header')

  const handleSourceChange = (source: string) => {
    if (source === SOURCE_TIME) {
      onChange(createEmptyTimeCondition())
    } else if (source === SOURCE_HEADER || source === SOURCE_PARAM) {
      onChange({
        ...createEmptyCondition(),
        source: source as 'param' | 'header',
      })
    }
  }

  const handleModeChange = (mode: string) => {
    onChange({ ...condition, mode } as RequestCondition)
  }

  const renderTimeCondition = (timeCond: TimeCondition) => (
    <>
      <Select
        items={TIME_FUNCS.map((fn) => ({
          value: fn,
          label: getTimeFuncLabel(fn),
        }))}
        value={timeCond.timeFunc}
        onValueChange={(value) =>
          onChange({ ...timeCond, timeFunc: value as TimeFunc })
        }
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getTimeFuncLabel(timeCond.timeFunc)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {TIME_FUNCS.map((fn) => (
              <SelectItem key={fn} value={fn}>
                {getTimeFuncLabel(fn)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={COMMON_TIMEZONES.map((tz) => ({
          value: tz.value,
          label: tz.label,
        }))}
        value={timeCond.timezone}
        onValueChange={(value) =>
          value !== null && onChange({ ...timeCond, timezone: value })
        }
      >
        <SelectTrigger className='w-56' size='sm'>
          <SelectValue>
            {COMMON_TIMEZONES.find((tz) => tz.value === timeCond.timezone)
              ?.label ?? timeCond.timezone}
          </SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {COMMON_TIMEZONES.map((tz) => (
              <SelectItem key={tz.value} value={tz.value}>
                {tz.label}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      <Select
        items={matchOptions.map((option) => ({
          value: option.value,
          label: getMatchLabel(option.value),
        }))}
        value={timeCond.mode}
        onValueChange={(v) => v !== null && handleModeChange(v)}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getMatchLabel(timeCond.mode)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {matchOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {getMatchLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {timeCond.mode === MATCH_RANGE ? (
        <>
          <DraftNumberInput
            value={timeCond.rangeStart}
            onValueChange={(value) =>
              onChange({ ...timeCond, rangeStart: String(value) })
            }
            placeholder={t('Start')}
            className='w-20'
          />
          <span className='text-muted-foreground text-xs'>~</span>
          <DraftNumberInput
            value={timeCond.rangeEnd}
            onValueChange={(value) =>
              onChange({ ...timeCond, rangeEnd: String(value) })
            }
            placeholder={t('End')}
            className='w-20'
          />
        </>
      ) : (
        <DraftNumberInput
          value={timeCond.value}
          onValueChange={(value) =>
            onChange({ ...timeCond, value: String(value) })
          }
          placeholder={t('Value')}
          className='w-24'
        />
      )}
    </>
  )

  const renderParamHeaderCondition = (phCond: ParamHeaderCondition) => (
    <>
      <Input
        value={phCond.path}
        onChange={(event) => onChange({ ...phCond, path: event.target.value })}
        placeholder={
          phCond.source === SOURCE_HEADER ? 'X-Header-Name' : 'service_tier'
        }
        className='w-44'
      />
      <Select
        items={matchOptions.map((option) => ({
          value: option.value,
          label: getMatchLabel(option.value),
        }))}
        value={phCond.mode}
        onValueChange={(v) => v !== null && handleModeChange(v)}
      >
        <SelectTrigger className='w-32' size='sm'>
          <SelectValue>{getMatchLabel(phCond.mode)}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            {matchOptions.map((option) => (
              <SelectItem key={option.value} value={option.value}>
                {getMatchLabel(option.value)}
              </SelectItem>
            ))}
          </SelectGroup>
        </SelectContent>
      </Select>
      {phCond.mode !== MATCH_EXISTS && (
        <Input
          value={phCond.value}
          onChange={(event) =>
            onChange({ ...phCond, value: event.target.value })
          }
          placeholder={t('Value')}
          className='w-44'
        />
      )}
    </>
  )

  return (
    <div className='flex flex-wrap items-center gap-2'>
      <Select
        items={[
          { value: SOURCE_PARAM, label: t('Body param') },
          { value: SOURCE_HEADER, label: t('Header') },
          { value: SOURCE_TIME, label: t('Time') },
        ]}
        value={condition.source}
        onValueChange={(v) => v !== null && handleSourceChange(v)}
      >
        <SelectTrigger className='w-28' size='sm'>
          <SelectValue>{sourceLabel}</SelectValue>
        </SelectTrigger>
        <SelectContent alignItemWithTrigger={false}>
          <SelectGroup>
            <SelectItem value={SOURCE_PARAM}>{t('Body param')}</SelectItem>
            <SelectItem value={SOURCE_HEADER}>{t('Header')}</SelectItem>
            <SelectItem value={SOURCE_TIME}>{t('Time')}</SelectItem>
          </SelectGroup>
        </SelectContent>
      </Select>
      {condition.source === SOURCE_TIME
        ? renderTimeCondition(condition as TimeCondition)
        : renderParamHeaderCondition(condition as ParamHeaderCondition)}
      <Button
        variant='ghost'
        size='icon'
        onClick={onRemove}
        aria-label={t('Remove condition')}
        className='ml-auto'
      >
        <Trash2 className='text-destructive h-4 w-4' />
      </Button>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Request rule group card
// ---------------------------------------------------------------------------

type RuleGroupCardProps = {
  group: RequestRuleGroup
  index: number
  onChange: (next: RequestRuleGroup) => void
  onRemove: () => void
}

function RuleGroupCard({
  group,
  index,
  onChange,
  onRemove,
}: RuleGroupCardProps) {
  const { t } = useTranslation()
  const conditionKeysRef = useRef<string[]>([])
  const nextConditionKeyRef = useRef(0)
  while (conditionKeysRef.current.length < group.conditions.length) {
    nextConditionKeyRef.current += 1
    conditionKeysRef.current.push(
      `rule-condition-${nextConditionKeyRef.current}`
    )
  }
  conditionKeysRef.current.length = group.conditions.length

  const handleConditionChange = (
    conditionIndex: number,
    next: RequestCondition
  ) => {
    const conditions = [...group.conditions]
    conditions[conditionIndex] = next
    onChange({ ...group, conditions })
  }

  const handleConditionRemove = (conditionIndex: number) => {
    conditionKeysRef.current.splice(conditionIndex, 1)
    onChange({
      ...group,
      conditions: group.conditions.filter((_, i) => i !== conditionIndex),
    })
  }

  const handleAddCondition = (timeMode: boolean) => {
    onChange({
      ...group,
      conditions: [
        ...group.conditions,
        timeMode ? createEmptyTimeCondition() : createEmptyCondition(),
      ],
    })
  }

  return (
    <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
      <div className='flex items-center justify-between gap-2'>
        <Badge variant='outline'>
          {t('Rule group')} #{index + 1}
        </Badge>
        <Button
          variant='ghost'
          size='icon'
          onClick={onRemove}
          aria-label={t('Remove rule group')}
        >
          <Trash2 className='text-destructive h-4 w-4' />
        </Button>
      </div>

      <div className='space-y-2'>
        {group.conditions.map((condition, conditionIndex) => (
          <RuleConditionRow
            key={conditionKeysRef.current[conditionIndex]}
            condition={condition}
            onChange={(next) => handleConditionChange(conditionIndex, next)}
            onRemove={() => handleConditionRemove(conditionIndex)}
          />
        ))}
        <div className='flex flex-wrap gap-2'>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => handleAddCondition(false)}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add param/header')}
          </Button>
          <Button
            variant='ghost'
            size='sm'
            onClick={() => handleAddCondition(true)}
          >
            <Plus className='mr-1 h-3 w-3' />
            {t('Add time condition')}
          </Button>
        </div>
      </div>

      <div className='flex items-center gap-2'>
        <Label className='text-xs'>{t('Multiplier')}</Label>
        <DraftNumberInput
          min={0}
          step={0.000001}
          value={group.multiplier}
          onValueChange={(value) =>
            onChange({ ...group, multiplier: String(value) })
          }
          className='w-32'
          placeholder='1.0'
        />
        <span className='text-muted-foreground text-xs'>
          {t('Final cost = base × multiplier when conditions match')}
        </span>
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// Preset section
// ---------------------------------------------------------------------------

type PresetSectionProps = {
  applyPreset: (preset: Preset) => void
}

function PresetSection({ applyPreset }: PresetSectionProps) {
  const { t } = useTranslation()
  const [expanded, setExpanded] = useState(false)
  const visible = expanded ? PRESET_GROUPS : PRESET_GROUPS.slice(0, 2)
  const hasMore = PRESET_GROUPS.length > 2

  return (
    <div className='space-y-2'>
      <div className='flex items-center gap-2'>
        <span className='text-sm font-medium'>{t('Preset templates')}</span>
        {hasMore && (
          <Button
            variant='ghost'
            size='sm'
            className='h-6 px-2 text-xs'
            onClick={() => setExpanded((prev) => !prev)}
          >
            {expanded ? t('Collapse') : t('More templates...')}
          </Button>
        )}
      </div>
      <div className='space-y-1'>
        {visible.map((presetGroup) => (
          <div
            key={presetGroup.group}
            className='flex flex-wrap items-center gap-2'
          >
            <Badge variant='secondary' className='min-w-[60px] justify-center'>
              {t(presetGroup.group)}
            </Badge>
            {presetGroup.presets.map((preset) => (
              <Button
                key={preset.key}
                variant='outline'
                size='sm'
                className='h-7 text-xs'
                onClick={() => applyPreset(preset)}
              >
                {preset.label}
              </Button>
            ))}
          </div>
        ))}
      </div>
    </div>
  )
}

// ---------------------------------------------------------------------------
// LLM prompt helper
// ---------------------------------------------------------------------------

const LLM_PROMPT_TEMPLATE = `You are an AI API billing expression design assistant. The user needs help designing a billing expression for an AI API gateway.

## Expression Language

Expressions are based on standard arithmetic with ternary operators.

### Token Variables

Input side:
- p — input token count (for pricing). Automatically excludes sub-categories priced separately (e.g., if cr is used, cache tokens are deducted from p)
- len — total input context length (for condition checks). Not affected by auto-exclusion; always reflects the full input length. Use in tier conditions
- cr — cache-hit (read) token count
- cc — cache-create token count (5-min TTL)
- cc1h — cache-create token count (1-hour TTL, Claude-specific)
- img — image input token count
- ai — audio input token count

Output side:
- c — output token count. Also auto-excludes sub-categories priced separately
- img_o — image output token count
- ao — audio output token count

### p/c Auto-exclusion

p and c are fallback variables representing all tokens not separately priced in the expression. If the expression uses a sub-category variable (e.g., cr), those tokens are deducted from p to avoid double-billing. Unused sub-category tokens remain in p/c at base price.

Important: len is NOT affected by auto-exclusion. Tier conditions should use len instead of p to prevent cache hits from lowering p and misidentifying the tier.

### Built-in Functions

- tier(name, value) — labels the billing tier; must wrap the cost expression
- max(a, b), min(a, b) — maximum/minimum
- ceil(x), floor(x), abs(x) — ceiling, floor, absolute value
- header(name) — reads a request header
- param(path) — reads a request body JSON path (gjson syntax)
- has(source, substr) — substring check
- hour(tz), minute(tz), weekday(tz), month(tz), day(tz) — time functions, tz is a timezone like "Asia/Shanghai"

### Price Coefficients

Numbers in the expression are $/1M tokens prices. For example, p * 2.5 means input $2.50/1M tokens.

## Expression Examples

Simple pricing:
tier("base", p * 2.5 + c * 15)

With cache:
tier("base", p * 2.5 + c * 15 + cr * 0.25)

Multi-tier (use len for conditions):
len <= 200000
  ? tier("standard", p * 3 + c * 15 + cr * 0.3 + cc * 3.75 + cc1h * 6)
  : tier("long_context", p * 6 + c * 22.5 + cr * 0.6 + cc * 7.5 + cc1h * 12)

Image model:
tier("base", p * 2 + c * 8 + img * 2.5)

Multimodal with audio:
tier("base", p * 0.43 + c * 3.06 + img * 0.78 + ai * 3.81 + ao * 15.11)

Three-tier example:
len <= 128000
  ? tier("standard", p * 1.1 + c * 4.4)
  : (len <= 1000000
    ? tier("medium", p * 2.2 + c * 8.8)
    : tier("long", p * 4.4 + c * 17.6))

## Rules

1. Every leaf branch must be wrapped in tier("name", cost_expr)
2. Use English tier names, e.g. "base", "standard", "long_context"
3. Use len for tier conditions (not p), supports <, <=, >, >=
4. Multi-tier uses nested ternary: cond1 ? tier(...) : (cond2 ? tier(...) : tier(...))
5. Price coefficients are the provider's official $/1M tokens prices
6. If cache/image/audio don't need separate pricing, omit those variables; their tokens are included in p/c automatically

Please generate a billing expression based on the model information and pricing requirements provided.`

type LlmPromptHelperProps = {
  modelName?: string
}

function LlmPromptHelper({ modelName }: LlmPromptHelperProps) {
  const { t } = useTranslation()
  const [open, setOpen] = useState(false)

  const prompt = useMemo(() => {
    if (modelName) {
      return `${LLM_PROMPT_TEMPLATE}\n\nCurrent model: ${modelName}`
    }
    return LLM_PROMPT_TEMPLATE
  }, [modelName])

  const handleCopy = useCallback(async () => {
    try {
      await navigator.clipboard.writeText(prompt)
      toast.success(t('Copied to clipboard'))
    } catch {
      toast.error(t('Failed to copy'))
    }
  }, [prompt, t])

  return (
    <Collapsible open={open} onOpenChange={setOpen}>
      <CollapsibleTrigger
        render={
          <Button variant='ghost' size='sm' className='h-7 px-2 text-xs' />
        }
      >
        <Copy className='mr-1.5 h-3 w-3' />
        {t('LLM prompt helper')}
      </CollapsibleTrigger>
      <CollapsibleContent className='mt-2'>
        <div className='bg-muted/30 rounded-md border p-3'>
          <div className='mb-2 flex items-center justify-between'>
            <p className='text-muted-foreground text-xs'>
              {t(
                'Copy this prompt and send it to an LLM (e.g. ChatGPT / Claude) to help design your billing expression.'
              )}
            </p>
            <Button
              variant='outline'
              size='sm'
              className='ml-3 shrink-0'
              onClick={handleCopy}
            >
              <Copy className='mr-1.5 h-3 w-3' />
              {t('Copy prompt')}
            </Button>
          </div>
          <Textarea
            value={prompt}
            readOnly
            rows={8}
            className='font-mono text-xs'
            spellCheck={false}
          />
        </div>
      </CollapsibleContent>
    </Collapsible>
  )
}

// ---------------------------------------------------------------------------
// Main editor
// ---------------------------------------------------------------------------

export type TieredPricingEditorProps = {
  modelName?: string
  billingExpr: string
  requestRuleExpr: string
  onBillingExprChange: (next: string) => void
  onRequestRuleExprChange: (next: string) => void
}

type EditorMode = 'visual' | 'raw'

export const TieredPricingEditor = memo(function TieredPricingEditor({
  modelName,
  billingExpr: currentExpr,
  requestRuleExpr: currentRequestRuleExpr,
  onBillingExprChange,
  onRequestRuleExprChange,
}: TieredPricingEditorProps) {
  const { t } = useTranslation()
  const [initialEditorState] = useState(() =>
    createInitialVisualEditorState(currentExpr)
  )
  const [editorMode, setEditorMode] = useState<EditorMode>(
    initialEditorState.mode
  )
  const [visualConfig, setVisualConfig] = useState<VisualConfig | null>(
    initialEditorState.visualConfig
  )
  const [rawExpr, setRawExpr] = useState(() =>
    combineBillingExpr(currentExpr || '', currentRequestRuleExpr || '')
  )
  const [requestRuleGroups, setRequestRuleGroups] = useState<
    RequestRuleGroup[]
  >(() => tryParseRequestRuleExpr(currentRequestRuleExpr) || [])
  const requestRuleGroupKeysRef = useRef<string[]>([])
  const nextRequestRuleGroupKeyRef = useRef(0)
  while (requestRuleGroupKeysRef.current.length < requestRuleGroups.length) {
    nextRequestRuleGroupKeyRef.current += 1
    requestRuleGroupKeysRef.current.push(
      `request-rule-group-${nextRequestRuleGroupKeyRef.current}`
    )
  }
  requestRuleGroupKeysRef.current.length = requestRuleGroups.length
  const initRef = useRef(false)

  useEffect(() => {
    if (initRef.current) return
    initRef.current = true
    const parsedConfig = tryParseVisualConfig(currentExpr)
    if (parsedConfig) {
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setVisualConfig(parsedConfig)
      setEditorMode('visual')
    } else if (currentExpr) {
      setVisualConfig(null)
      setEditorMode('raw')
    } else {
      setVisualConfig(createDefaultVisualConfig())
    }
    setRawExpr(
      combineBillingExpr(currentExpr || '', currentRequestRuleExpr || '')
    )
    setRequestRuleGroups(tryParseRequestRuleExpr(currentRequestRuleExpr) || [])
  }, [currentExpr, currentRequestRuleExpr])

  useEffect(() => {
    initRef.current = false
  }, [modelName])

  const canUseVisualRules = useMemo(() => {
    if (!currentRequestRuleExpr) return true
    return tryParseRequestRuleExpr(currentRequestRuleExpr) !== null
  }, [currentRequestRuleExpr])

  const visualExpression = useMemo(() => {
    if (editorMode !== 'visual') {
      return { expression: '', invalid: false }
    }
    try {
      return {
        expression: generateExprFromVisualConfig(visualConfig),
        invalid: false,
      }
    } catch {
      return { expression: '', invalid: true }
    }
  }, [editorMode, visualConfig])

  const effectiveExpr =
    editorMode === 'visual'
      ? visualExpression.expression
      : splitBillingExprAndRequestRules(rawExpr).billingExpr

  useEffect(() => {
    if (visualExpression.invalid) return
    if (effectiveExpr !== currentExpr) {
      onBillingExprChange(effectiveExpr)
    }
  }, [
    effectiveExpr,
    currentExpr,
    onBillingExprChange,
    visualExpression.invalid,
  ])

  useEffect(() => {
    if (editorMode !== 'visual') return
    const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
    if (ruleExpr !== currentRequestRuleExpr) {
      onRequestRuleExprChange(ruleExpr)
    }
  }, [
    editorMode,
    requestRuleGroups,
    currentRequestRuleExpr,
    onRequestRuleExprChange,
  ])

  const handleVisualChange = useCallback((next: VisualConfig) => {
    setVisualConfig(next)
  }, [])

  const handleRawChange = useCallback(
    (value: string) => {
      setRawExpr(value)
      const { requestRuleExpr: ruleStr } =
        splitBillingExprAndRequestRules(value)
      onRequestRuleExprChange(ruleStr)
    },
    [onRequestRuleExprChange]
  )

  const handleModeChange = useCallback(
    (next: EditorMode) => {
      if (next === 'visual') {
        const { billingExpr, requestRuleExpr: ruleStr } =
          splitBillingExprAndRequestRules(rawExpr)
        const parsed = tryParseVisualConfig(billingExpr)
        if (!parsed) return
        setVisualConfig(parsed)
        const parsedGroups = tryParseRequestRuleExpr(ruleStr)
        setRequestRuleGroups(parsedGroups || [])
        onRequestRuleExprChange(ruleStr)
      } else {
        let expr: string
        try {
          expr = generateExprFromVisualConfig(visualConfig)
        } catch {
          return
        }
        const ruleExpr = buildRequestRuleExpr(requestRuleGroups)
        setRawExpr(combineBillingExpr(expr, ruleExpr) || expr)
      }
      setEditorMode(next)
    },
    [rawExpr, visualConfig, requestRuleGroups, onRequestRuleExprChange]
  )

  const applyPreset = useCallback(
    (preset: Preset) => {
      const presetGroups = preset.requestRules || []
      const ruleExpr = buildRequestRuleExpr(presetGroups)
      const combined = combineBillingExpr(preset.expr, ruleExpr) || preset.expr
      setRawExpr(combined)
      const parsed = tryParseVisualConfig(preset.expr)
      if (parsed) {
        setVisualConfig(parsed)
        setEditorMode('visual')
      } else {
        setEditorMode('raw')
        setVisualConfig(null)
      }
      setRequestRuleGroups(presetGroups)
      onRequestRuleExprChange(ruleExpr)
    },
    [onRequestRuleExprChange]
  )

  const handleRuleGroupsChange = useCallback((next: RequestRuleGroup[]) => {
    setRequestRuleGroups(next)
  }, [])

  return (
    <div className='space-y-5'>
      <div className='grid gap-3 sm:grid-cols-[minmax(0,1fr)_auto] sm:items-end'>
        <Field className='gap-2'>
          <FieldLabel>{t('Editor mode')}</FieldLabel>
          <Select
            items={[
              { value: 'visual', label: t('Visual editor') },
              { value: 'raw', label: t('Expression editor') },
            ]}
            value={editorMode}
            onValueChange={(value) => handleModeChange(value as EditorMode)}
          >
            <SelectTrigger className='w-full sm:w-56' size='sm'>
              <SelectValue />
            </SelectTrigger>
            <SelectContent alignItemWithTrigger={false}>
              <SelectGroup>
                <SelectItem value='visual'>{t('Visual editor')}</SelectItem>
                <SelectItem value='raw'>{t('Expression editor')}</SelectItem>
              </SelectGroup>
            </SelectContent>
          </Select>
        </Field>
        {editorMode === 'raw' && (
          <div className='sm:pb-0.5'>
            <LlmPromptHelper modelName={modelName} />
          </div>
        )}
      </div>

      <PresetSection applyPreset={applyPreset} />

      <div className='bg-muted/30 space-y-3 rounded-md border p-3'>
        {editorMode === 'visual' ? (
          <VisualEditor
            visualConfig={visualConfig}
            onChange={handleVisualChange}
          />
        ) : (
          <RawExprEditor exprString={rawExpr} onChange={handleRawChange} />
        )}

        {editorMode === 'visual' && (
          <div className='space-y-3 border-t pt-3'>
            <div className='space-y-1'>
              <h4 className='text-sm font-medium'>
                {t('Request rule pricing')}
              </h4>
              <p className='text-muted-foreground text-xs'>
                {t(
                  'When conditions match, the final price is multiplied by X. Multiple matches multiply together; values < 1 act as discounts.'
                )}
              </p>
            </div>

            {currentRequestRuleExpr && !canUseVisualRules ? (
              <Alert>
                <AlertDescription className='text-xs'>
                  {t(
                    'This expression is too complex for the visual editor. Please switch to expression mode to edit.'
                  )}
                </AlertDescription>
              </Alert>
            ) : (
              <>
                {requestRuleGroups.map((group, groupIndex) => (
                  <RuleGroupCard
                    key={requestRuleGroupKeysRef.current[groupIndex]}
                    group={group}
                    index={groupIndex}
                    onChange={(next) => {
                      const updated = [...requestRuleGroups]
                      updated[groupIndex] = next
                      handleRuleGroupsChange(updated)
                    }}
                    onRemove={() => {
                      requestRuleGroupKeysRef.current.splice(groupIndex, 1)
                      handleRuleGroupsChange(
                        requestRuleGroups.filter((_, i) => i !== groupIndex)
                      )
                    }}
                  />
                ))}
                <Button
                  variant='outline'
                  size='sm'
                  className='h-9 w-36 justify-center'
                  onClick={() =>
                    handleRuleGroupsChange([
                      ...requestRuleGroups,
                      createEmptyRuleGroup(),
                    ])
                  }
                >
                  <Plus className='mr-2 h-4 w-4' />
                  {t('Add rule group')}
                </Button>
              </>
            )}
          </div>
        )}
      </div>

      <BillingCostEstimator
        effectiveExpr={effectiveExpr}
        visualConfigInvalid={visualExpression.invalid}
      />
    </div>
  )
})
