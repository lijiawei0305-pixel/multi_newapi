/*
Copyright (C) 2025 QuantumNous

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
import React, { useCallback, useEffect, useMemo, useState } from 'react';
import {
  Banner,
  Button,
  Card,
  Collapsible,
  Input,
  InputNumber,
  Radio,
  RadioGroup,
  Select,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconCopy, IconDelete, IconPlus } from '@douyinfe/semi-icons';
import { renderQuota } from '../../../../helpers/render';
import { copy, showSuccess } from '../../../../helpers';
import {
  BILLING_EXTRA_VARS,
  BILLING_CACHE_VAR_MAP,
  BILLING_CONDITION_VARS,
} from '../../../../constants';
import {
  createEmptyCondition,
  createEmptyTimeCondition,
  createEmptyRuleGroup,
  createEmptyTimeRuleGroup,
  getRequestRuleMatchOptions,
  normalizeCondition,
  tryParseRequestRuleExpr,
  buildRequestRuleExpr,
  combineBillingExpr,
  splitBillingExprAndRequestRules,
  MATCH_EQ,
  MATCH_EXISTS,
  MATCH_CONTAINS,
  MATCH_RANGE,
  MATCH_GTE,
  SOURCE_HEADER,
  SOURCE_PARAM,
  SOURCE_TIME,
  TIME_FUNCS,
  COMMON_TIMEZONES,
} from './requestRuleExpr';
import { evaluateSafeBillingExpression } from '../../../../helpers/safeBillingEvaluator';
import { PresetSection, RawExprEditor } from './tiered-pricing/PresetEditors';
import { RuleGroupCard } from './tiered-pricing/RequestRuleCards';
import LlmPromptHelper from './tiered-pricing/LlmPromptHelper';

const { Text } = Typography;

const PRICE_SUFFIX = '$/1M tokens';

function unitCostToPrice(uc) {
  return Number(uc) || 0;
}
function priceToUnitCost(price) {
  return Number(price) || 0;
}

const OPS = ['<', '<=', '>', '>='];
const VAR_OPTIONS = [
  { value: 'len', label: 'len (长度)' },
  { value: 'p', label: 'p (输入)' },
  { value: 'c', label: 'c (输出)' },
];

const CACHE_MODE_TIMED = 'timed';
const CACHE_MODE_GENERIC = 'generic';

function formatTokenHint(n) {
  if (n == null || n === '' || Number.isNaN(Number(n))) return '';
  const v = Number(n);
  if (v === 0) return '= 0';
  if (v >= 1000000) return `= ${(v / 1000000).toLocaleString()}M tokens`;
  if (v >= 1000) return `= ${(v / 1000).toLocaleString()}K tokens`;
  return `= ${v.toLocaleString()} tokens`;
}

// ---------------------------------------------------------------------------
// Expr generation from visual config (multi-condition)
// ---------------------------------------------------------------------------

function buildConditionStr(conditions) {
  if (!conditions || conditions.length === 0) return '';
  return conditions
    .map((condition) => {
      const value = Number(condition.value);
      if (
        !['p', 'c', 'len'].includes(condition.var) ||
        !['<', '<=', '>', '>='].includes(condition.op) ||
        condition.value === '' ||
        !Number.isFinite(value)
      ) {
        throw new Error('Invalid visual tier configuration');
      }
      return `${condition.var} ${condition.op} ${value}`;
    })
    .join(' && ');
}

const CACHE_VAR_MAP = BILLING_CACHE_VAR_MAP;

function getTierCacheMode(tier) {
  if (tier?.cache_mode === CACHE_MODE_TIMED) {
    return CACHE_MODE_TIMED;
  }
  if (tier?.cache_mode === CACHE_MODE_GENERIC) {
    return CACHE_MODE_GENERIC;
  }
  return Number(tier?.cache_create_1h_unit_cost) > 0
    ? CACHE_MODE_TIMED
    : CACHE_MODE_GENERIC;
}

function normalizeVisualTier(tier = {}) {
  return {
    ...tier,
    conditions: Array.isArray(tier.conditions) ? tier.conditions : [],
    cache_mode: getTierCacheMode(tier),
  };
}

function createDefaultVisualConfig() {
  return {
    tiers: [
      normalizeVisualTier({
        conditions: [],
        input_unit_cost: 0,
        output_unit_cost: 0,
        label: 'base',
        cache_mode: CACHE_MODE_GENERIC,
      }),
    ],
  };
}

function normalizeVisualConfig(config) {
  if (!config || !Array.isArray(config.tiers) || config.tiers.length === 0) {
    return createDefaultVisualConfig();
  }
  return {
    ...config,
    tiers: config.tiers.map((tier) => normalizeVisualTier(tier)),
  };
}

function buildTierBodyExpr(tier) {
  const parts = [];
  const ic = Number(tier.input_unit_cost);
  const oc = Number(tier.output_unit_cost);
  if (!Number.isFinite(ic) || !Number.isFinite(oc)) {
    throw new Error('Invalid visual tier configuration');
  }
  parts.push(`p * ${ic}`);
  parts.push(`c * ${oc}`);
  for (const cv of CACHE_VAR_MAP) {
    const rawValue = tier[cv.field];
    const v = rawValue == null || rawValue === '' ? 0 : Number(rawValue);
    if (!Number.isFinite(v)) {
      throw new Error('Invalid visual tier configuration');
    }
    if (v !== 0) parts.push(`${cv.exprVar} * ${v}`);
  }
  return parts.join(' + ');
}

export function generateExprFromVisualConfig(config) {
  if (!config || !config.tiers || config.tiers.length === 0)
    return 'p * 0 + c * 0';
  const tiers = config.tiers;

  if (tiers.length > 1) {
    for (let index = 0; index < tiers.length - 1; index += 1) {
      if (!buildConditionStr(tiers[index].conditions)) {
        throw new Error('Invalid visual tier configuration');
      }
    }
    if (buildConditionStr(tiers[tiers.length - 1].conditions)) {
      throw new Error('Invalid visual tier configuration');
    }
  }

  if (tiers.length === 1) {
    const t = tiers[0];
    const label = t.label || 'default';
    const body = `tier(${JSON.stringify(label)}, ${buildTierBodyExpr(t)})`;
    const cond = buildConditionStr(t.conditions);
    if (cond) {
      return applyVisualExpressionVersion(
        config,
        `${cond} ? ${body} : p * 0 + c * 0`,
      );
    }
    return applyVisualExpressionVersion(config, body);
  }

  const parts = [];
  for (let i = 0; i < tiers.length; i++) {
    const t = tiers[i];
    const label = t.label || `第${i + 1}档`;
    const body = `tier(${JSON.stringify(label)}, ${buildTierBodyExpr(t)})`;
    const cond = buildConditionStr(t.conditions);

    if (i < tiers.length - 1 && cond) {
      parts.push(`${cond} ? ${body}`);
    } else {
      parts.push(body);
    }
  }
  return applyVisualExpressionVersion(config, parts.join(' : '));
}

function applyVisualExpressionVersion(config, expression) {
  return config.version === 1 ? `v1:${expression}` : expression;
}

// ---------------------------------------------------------------------------
// Reverse-parse an Expr string back into visual config
// ---------------------------------------------------------------------------

export function tryParseVisualConfig(exprStr) {
  if (!exprStr) return null;
  try {
    const originalExpression = exprStr;
    let version;
    const versionMatch = exprStr.match(/^v(\d+):([\s\S]*)$/);
    if (versionMatch) {
      if (versionMatch[1] !== '1') return null;
      version = 1;
      exprStr = versionMatch[2];
    }
    const cacheVarNames = CACHE_VAR_MAP.map((cv) => cv.exprVar);
    const numberPattern = '[+-]?(?:\\d+(?:\\.\\d*)?|\\.\\d+)(?:[eE][+-]?\\d+)?';
    const optCacheStr = cacheVarNames
      .map((v) => `(?:\\s*\\+\\s*${v}\\s*\\*\\s*(${numberPattern}))?`)
      .join('');

    // Body pattern: p * X + c * Y [+ cr * A] [+ cc * B] [+ cc1h * C]
    const bodyPat = `p\\s*\\*\\s*(${numberPattern})\\s*\\+\\s*c\\s*\\*\\s*(${numberPattern})${optCacheStr}`;
    const labelPattern = '"((?:\\\\.|[^"\\\\])*)"';

    // Single-tier: tier("label", body)
    const singleRe = new RegExp(`^tier\\(${labelPattern},\\s*${bodyPat}\\)$`);
    const simple = exprStr.match(singleRe);
    if (simple) {
      const tier = {
        conditions: [],
        input_unit_cost: parseFiniteVisualNumber(simple[2]),
        output_unit_cost: parseFiniteVisualNumber(simple[3]),
        label: decodeTierLabel(simple[1]),
      };
      CACHE_VAR_MAP.forEach((cv, i) => {
        const val = simple[4 + i];
        if (val != null) tier[cv.field] = parseFiniteVisualNumber(val);
      });
      const config = normalizeVisualConfig({
        tiers: [normalizeVisualTier(tier)],
        version,
      });
      if (!visualExpressionMatches(originalExpression, config)) return null;
      return config;
    }

    // Multi-tier: cond1 ? tier(body) : cond2 ? tier(body) : tier(body)
    const condGroup = `((?:(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*${numberPattern})(?:\\s*&&\\s*(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*${numberPattern})*)`;
    const tierRe = new RegExp(
      `(?:${condGroup}\\s*\\?\\s*)?tier\\(${labelPattern},\\s*${bodyPat}\\)`,
      'g',
    );
    const tiers = [];
    let match;
    while ((match = tierRe.exec(exprStr)) !== null) {
      const condStr = match[1] || '';
      const conditions = [];
      if (condStr) {
        const condParts = condStr.split(/\s*&&\s*/);
        for (const cp of condParts) {
          const cm = cp
            .trim()
            .match(
              new RegExp(`^(p|c|len)\\s*(<|<=|>|>=)\\s*(${numberPattern})$`),
            );
          if (cm) {
            conditions.push({
              var: cm[1],
              op: cm[2],
              value: parseFiniteVisualNumber(cm[3]),
            });
          }
        }
      }
      const tier = {
        conditions,
        input_unit_cost: parseFiniteVisualNumber(match[3]),
        output_unit_cost: parseFiniteVisualNumber(match[4]),
        label: decodeTierLabel(match[2]),
      };
      CACHE_VAR_MAP.forEach((cv, i) => {
        const val = match[5 + i];
        if (val != null) tier[cv.field] = parseFiniteVisualNumber(val);
      });
      tiers.push(normalizeVisualTier(tier));
    }
    if (tiers.length === 0) return null;

    const cfg = normalizeVisualConfig({ tiers, version });
    if (!visualExpressionMatches(originalExpression, cfg)) return null;
    return cfg;
  } catch {
    return null;
  }
}

function decodeTierLabel(encodedBody) {
  const value = JSON.parse(`"${encodedBody}"`);
  if (typeof value !== 'string') throw new Error('Tier label is not a string');
  return value;
}

function parseFiniteVisualNumber(value) {
  const parsed = Number(value);
  if (!Number.isFinite(parsed)) {
    throw new Error('Invalid visual tier configuration');
  }
  return parsed;
}

function visualExpressionMatches(expression, config) {
  const regenerated = generateExprFromVisualConfig(config);
  return regenerated.replace(/\s+/g, '') === expression.replace(/\s+/g, '');
}

// ---------------------------------------------------------------------------
// Condition editor row
// ---------------------------------------------------------------------------

function ConditionRow({ cond, onChange, onRemove, t }) {
  const hint = formatTokenHint(cond.value);
  return (
    <div
      style={{
        marginBottom: 6,
        display: 'grid',
        gridTemplateColumns: '1fr auto 1fr auto',
        gap: '4px 6px',
        alignItems: 'center',
      }}
    >
      <Select
        size='small'
        value={cond.var || 'len'}
        onChange={(val) => onChange({ ...cond, var: val })}
      >
        {VAR_OPTIONS.map((v) => (
          <Select.Option key={v.value} value={v.value}>
            {v.label}
          </Select.Option>
        ))}
      </Select>
      <Select
        size='small'
        value={cond.op || '<'}
        onChange={(val) => onChange({ ...cond, op: val })}
        style={{ width: 70 }}
      >
        {OPS.map((op) => (
          <Select.Option key={op} value={op}>
            {op}
          </Select.Option>
        ))}
      </Select>
      <InputNumber
        size='small'
        min={0}
        value={cond.value ?? ''}
        onChange={(val) => onChange({ ...cond, value: val })}
      />
      <Button
        icon={<IconDelete />}
        type='danger'
        theme='borderless'
        size='small'
        onClick={onRemove}
      />
      {hint ? (
        <Text
          size='small'
          style={{
            color: 'var(--semi-color-text-3)',
            gridColumn: '3 / 4',
          }}
        >
          = {hint}
        </Text>
      ) : null}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Price input that preserves intermediate text like "7." or "0.5"
// ---------------------------------------------------------------------------

function PriceInput({ unitCost, field, index, onUpdate, placeholder }) {
  const priceFromModel = unitCostToPrice(unitCost);
  const [text, setText] = useState(
    priceFromModel === 0 ? '' : String(priceFromModel),
  );

  useEffect(() => {
    const current = Number(text);
    if (text === '' && priceFromModel === 0) return;
    if (!Number.isNaN(current) && current === priceFromModel) return;
    setText(priceFromModel === 0 ? '' : String(priceFromModel));
  }, [priceFromModel]);

  const handleChange = (val) => {
    setText(val);
    if (val === '') {
      onUpdate(index, field, 0);
      return;
    }
    const num = Number(val);
    if (!Number.isNaN(num)) {
      onUpdate(index, field, priceToUnitCost(num));
    }
  };

  return (
    <Input
      value={text}
      placeholder={placeholder || '0'}
      suffix={PRICE_SUFFIX}
      onChange={handleChange}
      style={{ width: '100%', marginTop: 2 }}
    />
  );
}

// ---------------------------------------------------------------------------
// Extended price block (cache fields) — collapsible per tier, with mode switch
// ---------------------------------------------------------------------------

const CACHE_FIELDS_TIMED = [
  { field: 'cache_read_unit_cost', labelKey: '缓存读取价格' },
  { field: 'cache_create_unit_cost', labelKey: '缓存创建价格（5分钟）' },
  { field: 'cache_create_1h_unit_cost', labelKey: '缓存创建价格（1小时）' },
];

const CACHE_FIELDS_GENERIC = [
  { field: 'cache_read_unit_cost', labelKey: '缓存读取价格' },
  { field: 'cache_create_unit_cost', labelKey: '缓存创建价格' },
];

function ExtendedPriceBlock({ tier, index, onUpdate, t }) {
  const mediaFields = BILLING_EXTRA_VARS.filter((v) => v.group === 'media');
  const hasAny = [
    ...CACHE_FIELDS_TIMED,
    ...mediaFields.map((v) => v.tierField),
  ].some((f) => Number(tier[typeof f === 'string' ? f : f.field]) > 0);
  const [expanded, setExpanded] = useState(hasAny);
  const cacheMode = getTierCacheMode(tier);

  const handleCacheModeChange = (e) => {
    const mode = e.target.value;
    const patch = { cache_mode: mode };
    if (mode === CACHE_MODE_GENERIC) {
      patch.cache_create_1h_unit_cost = 0;
    }
    onUpdate(index, patch);
  };

  const activeFields =
    cacheMode === CACHE_MODE_TIMED ? CACHE_FIELDS_TIMED : CACHE_FIELDS_GENERIC;

  return (
    <div style={{ marginTop: 8 }}>
      <Button
        theme='borderless'
        size='small'
        onClick={() => setExpanded(!expanded)}
        style={{
          padding: '2px 0',
          color: 'var(--semi-color-text-2)',
          fontSize: 12,
        }}
      >
        {expanded ? '▾' : '▸'} {t('扩展价格')}
      </Button>
      <Collapsible isOpen={expanded}>
        <div
          style={{
            marginTop: 4,
            padding: '8px 0',
          }}
        >
          <div className='text-xs text-gray-500 mb-2'>
            {t('这些价格都是可选项，不填也可以。')}
          </div>
          <div style={{ marginBottom: 8 }}>
            <RadioGroup
              type='button'
              size='small'
              value={cacheMode}
              onChange={handleCacheModeChange}
            >
              <Radio value={CACHE_MODE_GENERIC}>{t('通用缓存')}</Radio>
              <Radio value={CACHE_MODE_TIMED}>{t('分时缓存 (Claude)')}</Radio>
            </RadioGroup>
          </div>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: 8,
            }}
          >
            {activeFields.map((cf) => (
              <div key={cf.field}>
                <Text
                  size='small'
                  style={{ color: 'var(--semi-color-text-2)' }}
                >
                  {t(cf.labelKey)}
                </Text>
                <PriceInput
                  unitCost={tier[cf.field]}
                  field={cf.field}
                  index={index}
                  onUpdate={onUpdate}
                />
              </div>
            ))}
          </div>
          <div className='text-xs text-gray-500 mb-2 mt-3'>
            {t('图片/音频价格（可选）')}
          </div>
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: '1fr 1fr',
              gap: 8,
            }}
          >
            {mediaFields
              .map((v) => ({ field: v.tierField, labelKey: v.label }))
              .map((cf) => (
                <div key={cf.field}>
                  <Text
                    size='small'
                    style={{ color: 'var(--semi-color-text-2)' }}
                  >
                    {t(cf.labelKey)}
                  </Text>
                  <PriceInput
                    unitCost={tier[cf.field]}
                    field={cf.field}
                    index={index}
                    onUpdate={onUpdate}
                  />
                </div>
              ))}
          </div>
        </div>
      </Collapsible>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Visual Tier Card (multi-condition)
// ---------------------------------------------------------------------------

function VisualTierCard({
  tier,
  index,
  isLast,
  isOnly,
  onUpdate,
  onRemove,
  t,
}) {
  const conditions = tier.conditions || [];

  const varLabel = { len: t('长度'), p: t('输入'), c: t('输出') };
  const condSummary = useMemo(() => {
    if (conditions.length === 0) return t('无条件（兜底档）');
    return conditions
      .filter((c) => c.var && c.op && c.value != null)
      .map(
        (c) =>
          `${varLabel[c.var] || c.var} ${c.op} ${formatTokenHint(c.value)}`,
      )
      .join(' && ');
  }, [conditions, t]);

  const updateCondition = (ci, newCond) => {
    const next = conditions.map((c, i) => (i === ci ? newCond : c));
    onUpdate(index, 'conditions', next);
  };

  const removeCondition = (ci) => {
    onUpdate(
      index,
      'conditions',
      conditions.filter((_, i) => i !== ci),
    );
  };

  const addCondition = () => {
    if (conditions.length >= 2) return;
    const usedVars = conditions.map((c) => c.var);
    const nextVar = usedVars.includes('len') ? 'c' : 'len';
    onUpdate(index, 'conditions', [
      ...conditions,
      { var: nextVar, op: '<', value: 200000 },
    ]);
  };

  return (
    <div
      style={{
        padding: '12px 16px',
        borderRadius: 8,
        border: '1px solid var(--semi-color-border)',
        background: 'var(--semi-color-bg-2)',
        marginBottom: 8,
      }}
    >
      <div
        style={{
          display: 'flex',
          justifyContent: 'space-between',
          alignItems: 'center',
          marginBottom: 10,
        }}
      >
        <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
          <Tag color='blue' size='small'>
            {t('第 {{n}} 档', { n: index + 1 })}
          </Tag>
          {isLast && !isOnly ? (
            <Tag color='grey' size='small'>
              {t('兜底档')}
            </Tag>
          ) : null}
        </div>
        {!isOnly ? (
          <Button
            icon={<IconDelete />}
            type='danger'
            theme='borderless'
            size='small'
            onClick={() => onRemove(index)}
          />
        ) : null}
      </div>

      {/* Tier label */}
      <div style={{ marginBottom: 8 }}>
        <Text size='small' style={{ color: 'var(--semi-color-text-2)' }}>
          {t('档位名称')}
        </Text>
        <Input
          size='small'
          value={tier.label || ''}
          placeholder={t('第 {{n}} 档', { n: index + 1 })}
          onChange={(val) => onUpdate(index, 'label', val)}
          style={{ width: '100%', marginTop: 2 }}
        />
      </div>

      {/* Conditions */}
      {!isLast || isOnly ? (
        <div style={{ marginBottom: 10 }}>
          <Text
            size='small'
            style={{
              color: 'var(--semi-color-text-2)',
              display: 'block',
              marginBottom: 4,
            }}
          >
            {t('条件')}
          </Text>
          {conditions.map((cond, ci) => (
            <ConditionRow
              key={ci}
              cond={cond}
              onChange={(nc) => updateCondition(ci, nc)}
              onRemove={() => removeCondition(ci)}
              t={t}
            />
          ))}
          {conditions.length < 2 && (
            <Button
              icon={<IconPlus />}
              size='small'
              theme='borderless'
              onClick={addCondition}
              style={{ marginTop: 2 }}
            >
              {t('添加条件')}
            </Button>
          )}
        </div>
      ) : (
        <div
          style={{
            marginBottom: 10,
            padding: '4px 8px',
            borderRadius: 4,
            background: 'var(--semi-color-fill-1)',
          }}
        >
          <Text size='small' style={{ color: 'var(--semi-color-text-3)' }}>
            {condSummary}
          </Text>
        </div>
      )}

      {/* Prices */}
      <div style={{ display: 'grid', gridTemplateColumns: '1fr 1fr', gap: 8 }}>
        <div>
          <Text size='small' style={{ color: 'var(--semi-color-text-2)' }}>
            {t('输入价格')}
          </Text>
          <PriceInput
            unitCost={tier.input_unit_cost}
            field='input_unit_cost'
            index={index}
            onUpdate={onUpdate}
          />
        </div>
        <div>
          <Text size='small' style={{ color: 'var(--semi-color-text-2)' }}>
            {t('输出价格')}
          </Text>
          <PriceInput
            unitCost={tier.output_unit_cost}
            field='output_unit_cost'
            index={index}
            onUpdate={onUpdate}
          />
        </div>
      </div>

      {/* Extended prices (cache) — collapsible */}
      <ExtendedPriceBlock tier={tier} index={index} onUpdate={onUpdate} t={t} />
    </div>
  );
}

// ---------------------------------------------------------------------------
// Visual editor
// ---------------------------------------------------------------------------

function VisualEditor({ visualConfig, onChange, t }) {
  const config = normalizeVisualConfig(visualConfig);
  const tiers = config.tiers || [];

  const updateTier = (index, field, value) => {
    const patch = typeof field === 'string' ? { [field]: value } : { ...field };
    const next = tiers.map((tier, i) =>
      i === index ? normalizeVisualTier({ ...tier, ...patch }) : tier,
    );
    onChange({ ...config, tiers: next });
  };

  const addTier = () => {
    const newTiers = [...tiers];
    if (
      newTiers.length > 0 &&
      (!newTiers[newTiers.length - 1].conditions ||
        newTiers[newTiers.length - 1].conditions.length === 0)
    ) {
      newTiers[newTiers.length - 1] = {
        ...newTiers[newTiers.length - 1],
        conditions: [{ var: 'len', op: '<', value: 200000 }],
      };
    }
    newTiers.push({
      conditions: [],
      input_unit_cost: 0,
      output_unit_cost: 0,
      label: `第${newTiers.length + 1}档`,
      cache_mode: CACHE_MODE_GENERIC,
    });
    onChange({ ...config, tiers: newTiers });
  };

  const removeTier = (index) => {
    if (tiers.length <= 1) return;
    const next = tiers.filter((_, i) => i !== index);
    if (next.length > 0) {
      next[next.length - 1] = {
        ...next[next.length - 1],
        conditions: [],
      };
    }
    onChange({ ...config, tiers: next });
  };

  return (
    <div>
      <Banner
        type='info'
        description={t(
          '每个档位可设置 0~2 个条件（对 len、p 和 c），最后一档为兜底档无需条件。len 为输入上下文总长度（含缓存），推荐用于阶梯条件。',
        )}
        style={{ marginBottom: 12 }}
      />

      {tiers.map((tier, index) => (
        <VisualTierCard
          key={index}
          tier={tier}
          index={index}
          isLast={index === tiers.length - 1}
          isOnly={tiers.length === 1}
          onUpdate={updateTier}
          onRemove={removeTier}
          t={t}
        />
      ))}
      <Button
        icon={<IconPlus />}
        size='small'
        theme='light'
        onClick={addTier}
        style={{ marginTop: 4 }}
      >
        {t('添加更多档位')}
      </Button>
    </div>
  );
}

// ---------------------------------------------------------------------------
// Raw Expr editor with preset templates
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Cache token inputs for estimator — auto-shown when expression uses cache vars
// ---------------------------------------------------------------------------

const EXTRA_ESTIMATOR_FIELDS = BILLING_EXTRA_VARS.map((v) => ({
  var: v.key,
  stateKey: v.field.replace('Price', 'Tokens'),
  labelKey: `${v.shortLabel} Token (${v.key})`,
}));

function CacheTokenEstimatorInputs({
  effectiveExpr,
  extraTokenValues,
  extraTokenSetters,
  t,
}) {
  const usesExtra = useMemo(() => {
    if (!effectiveExpr) return false;
    const varNames = EXTRA_ESTIMATOR_FIELDS.map((f) =>
      f.var.replace('_', '_'),
    ).join('|');
    return new RegExp(`\\b(${varNames})\\b`).test(effectiveExpr);
  }, [effectiveExpr]);

  if (!usesExtra) return null;

  return (
    <div
      style={{
        display: 'grid',
        gridTemplateColumns: '1fr 1fr',
        gap: 12,
        marginBottom: 12,
      }}
    >
      {EXTRA_ESTIMATOR_FIELDS.map((cf) => (
        <div key={cf.var}>
          <Text size='small' className='mb-1' style={{ display: 'block' }}>
            {t(cf.labelKey)}
          </Text>
          <InputNumber
            value={extraTokenValues[cf.stateKey]}
            min={0}
            onChange={(val) => extraTokenSetters[cf.stateKey](val ?? 0)}
            style={{ width: '100%' }}
          />
        </div>
      ))}
    </div>
  );
}

// ---------------------------------------------------------------------------
// Cost estimator (works with any Expr string)
// ---------------------------------------------------------------------------

export function evalExprLocally(exprStr, p, c, len, extraTokenValues) {
  try {
    const env = {
      p,
      c,
      len,
    };
    for (const field of EXTRA_ESTIMATOR_FIELDS) {
      env[field.var] = extraTokenValues[field.stateKey] || 0;
    }
    const result = evaluateSafeBillingExpression(exprStr, env);
    return { cost: result.value, matchedTier: result.matchedTier, error: null };
  } catch (e) {
    return { cost: 0, matchedTier: '', error: e.message };
  }
}

export function convertRawBillingCostToQuota(rawCost, quotaPerUnit) {
  if (!Number.isFinite(rawCost) || !Number.isFinite(quotaPerUnit)) {
    throw new Error('Billing cost conversion requires finite values');
  }
  return (rawCost / 1000000) * quotaPerUnit;
}

// ---------------------------------------------------------------------------
// Request condition rule row (moved from RequestMultiplierEditor)
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// LLM prompt helper — copyable prompt for LLM-assisted expression design
// ---------------------------------------------------------------------------

// ---------------------------------------------------------------------------
// Main component
// ---------------------------------------------------------------------------

export default function TieredPricingEditor({
  model,
  onExprChange,
  requestRuleExpr,
  onRequestRuleExprChange,
  t,
}) {
  const currentExpr = model?.billingExpr || '';

  const [editorMode, setEditorMode] = useState(() =>
    currentExpr && !tryParseVisualConfig(currentExpr) ? 'raw' : 'visual',
  );
  const [visualConfig, setVisualConfig] = useState(() =>
    currentExpr
      ? tryParseVisualConfig(currentExpr)
      : createDefaultVisualConfig(),
  );
  const [rawExpr, setRawExpr] = useState(currentExpr);
  const [billableInputTokens, setBillableInputTokens] = useState(200000);
  const [billableOutputTokens, setBillableOutputTokens] = useState(10000);
  const [fullInputLength, setFullInputLength] = useState(200000);
  const [cacheReadTokens, setCacheReadTokens] = useState(0);
  const [cacheCreateTokens, setCacheCreateTokens] = useState(0);
  const [cacheCreate1hTokens, setCacheCreate1hTokens] = useState(0);
  const [imageTokens, setImageTokens] = useState(0);
  const [imageOutputTokens, setImageOutputTokens] = useState(0);
  const [audioInputTokens, setAudioInputTokens] = useState(0);
  const [audioOutputTokens, setAudioOutputTokens] = useState(0);

  const currentRequestRuleExpr = requestRuleExpr || '';
  const parsedRequestRuleGroups = useMemo(
    () => tryParseRequestRuleExpr(currentRequestRuleExpr),
    [currentRequestRuleExpr],
  );
  const canUseVisualRules = parsedRequestRuleGroups !== null;
  const [requestRuleGroups, setRequestRuleGroups] = useState(
    parsedRequestRuleGroups || [],
  );

  useEffect(() => {
    if (parsedRequestRuleGroups) {
      setRequestRuleGroups(parsedRequestRuleGroups);
    } else {
      setRequestRuleGroups([]);
    }
  }, [currentRequestRuleExpr, parsedRequestRuleGroups]);

  const handleRequestRuleGroupsChange = useCallback(
    (nextGroups) => {
      setRequestRuleGroups(nextGroups);
      onRequestRuleExprChange(buildRequestRuleExpr(nextGroups));
    },
    [onRequestRuleExprChange],
  );

  useEffect(() => {
    const parsed = tryParseVisualConfig(currentExpr);
    if (parsed) {
      setEditorMode('visual');
      setVisualConfig(parsed);
      setRawExpr(currentExpr);
    } else if (currentExpr) {
      setEditorMode('raw');
      setRawExpr(currentExpr);
      setVisualConfig(null);
    } else {
      setEditorMode('visual');
      setVisualConfig(createDefaultVisualConfig());
      setRawExpr('');
    }
  }, [model?.name]);

  const visualExpression = useMemo(() => {
    if (editorMode !== 'visual') {
      return { expression: '', invalid: false };
    }
    try {
      return {
        expression: generateExprFromVisualConfig(visualConfig),
        invalid: false,
      };
    } catch {
      return { expression: '', invalid: true };
    }
  }, [editorMode, visualConfig]);

  const effectiveExpr =
    editorMode === 'visual'
      ? visualExpression.expression
      : splitBillingExprAndRequestRules(rawExpr).billingExpr;

  useEffect(() => {
    if (visualExpression.invalid) return;
    if (effectiveExpr !== currentExpr) {
      onExprChange(effectiveExpr);
    }
  }, [currentExpr, effectiveExpr, onExprChange, visualExpression.invalid]);

  const handleVisualChange = useCallback((newConfig) => {
    setVisualConfig(newConfig);
  }, []);

  const handleRawChange = useCallback(
    (val) => {
      setRawExpr(val);
      const { requestRuleExpr: ruleStr } = splitBillingExprAndRequestRules(val);
      onRequestRuleExprChange(ruleStr);
    },
    [onRequestRuleExprChange],
  );

  const handleModeSwitch = useCallback(
    (e) => {
      const newMode = e.target.value;
      if (newMode === 'visual') {
        const { billingExpr, requestRuleExpr: ruleStr } =
          splitBillingExprAndRequestRules(rawExpr);
        const parsed = tryParseVisualConfig(billingExpr);
        if (!parsed) return;
        setVisualConfig(parsed);
        const parsedGroups = tryParseRequestRuleExpr(ruleStr);
        setRequestRuleGroups(parsedGroups || []);
        onRequestRuleExprChange(ruleStr);
      } else {
        let expr;
        try {
          expr = generateExprFromVisualConfig(visualConfig);
        } catch {
          return;
        }
        const ruleExpr = buildRequestRuleExpr(requestRuleGroups);
        setRawExpr(combineBillingExpr(expr, ruleExpr) || expr);
      }
      setEditorMode(newMode);
    },
    [rawExpr, visualConfig, requestRuleGroups, onRequestRuleExprChange],
  );

  const applyPreset = useCallback(
    (preset) => {
      const presetGroups = preset.requestRules || [];
      const ruleExpr = buildRequestRuleExpr(presetGroups);
      const combined = combineBillingExpr(preset.expr, ruleExpr) || preset.expr;
      setRawExpr(combined);
      const parsed = tryParseVisualConfig(preset.expr);
      if (parsed) {
        setVisualConfig(parsed);
      } else {
        setEditorMode('raw');
        setVisualConfig(null);
      }
      setRequestRuleGroups(presetGroups);
      onRequestRuleExprChange(ruleExpr);
    },
    [onRequestRuleExprChange],
  );

  const extraTokenValues = {
    cacheReadTokens,
    cacheCreateTokens,
    cacheCreate1hTokens,
    imageTokens,
    imageOutputTokens,
    audioInputTokens,
    audioOutputTokens,
  };
  const extraTokenSetters = {
    cacheReadTokens: setCacheReadTokens,
    cacheCreateTokens: setCacheCreateTokens,
    cacheCreate1hTokens: setCacheCreate1hTokens,
    imageTokens: setImageTokens,
    imageOutputTokens: setImageOutputTokens,
    audioInputTokens: setAudioInputTokens,
    audioOutputTokens: setAudioOutputTokens,
  };

  const evalResult = useMemo(() => {
    const result = evalExprLocally(
      effectiveExpr,
      billableInputTokens,
      billableOutputTokens,
      fullInputLength,
      extraTokenValues,
    );
    if (!result.error) {
      const storedQuotaPerUnit = localStorage.getItem('quota_per_unit');
      const parsedQuotaPerUnit = Number(storedQuotaPerUnit);
      const quotaPerUnit =
        storedQuotaPerUnit !== null && Number.isFinite(parsedQuotaPerUnit)
          ? parsedQuotaPerUnit
          : 500000;
      result.cost = convertRawBillingCostToQuota(result.cost, quotaPerUnit);
    }
    return result;
  }, [
    effectiveExpr,
    billableInputTokens,
    billableOutputTokens,
    fullInputLength,
    cacheReadTokens,
    cacheCreateTokens,
    cacheCreate1hTokens,
    imageTokens,
    imageOutputTokens,
    audioInputTokens,
    audioOutputTokens,
  ]);
  const hasEstimatorError =
    visualExpression.invalid || Boolean(evalResult.error);

  return (
    <div>
      <div style={{ marginBottom: 12 }}>
        <RadioGroup
          type='button'
          size='small'
          value={editorMode}
          onChange={handleModeSwitch}
        >
          <Radio value='visual'>{t('可视化编辑')}</Radio>
          <Radio value='raw'>{t('表达式编辑')}</Radio>
        </RadioGroup>
      </div>

      <PresetSection applyPreset={applyPreset} t={t} />

      <Card
        bodyStyle={{ padding: 16 }}
        style={{ marginBottom: 12, background: 'var(--semi-color-fill-0)' }}
      >
        {editorMode === 'visual' ? (
          <VisualEditor
            visualConfig={visualConfig}
            onChange={handleVisualChange}
            t={t}
          />
        ) : (
          <RawExprEditor
            exprString={rawExpr}
            onChange={handleRawChange}
            t={t}
          />
        )}

        {editorMode === 'visual' && (
          <>
            <div
              style={{
                borderTop: '1px solid var(--semi-color-border)',
                margin: '16px 0',
              }}
            />

            <div className='font-medium mb-2'>{t('请求条件调价')}</div>
            <div style={{ marginBottom: 12 }}>
              <Text type='secondary' size='small'>
                {t(
                  '满足条件时，整单价格乘以 X；如果有多条同时命中，会继续相乘。',
                )}
              </Text>
              <div style={{ marginTop: 2 }}>
                <Text type='secondary' size='small'>
                  {t(
                    'X 也可以小于 1，当折扣用。想做"只给输出加价"或"额外加固定费用"，请直接写完整计费公式。',
                  )}
                </Text>
              </div>
            </div>

            {currentRequestRuleExpr && !canUseVisualRules ? (
              <Banner
                type='warning'
                bordered
                fullMode={false}
                closeIcon={null}
                style={{ marginBottom: 12 }}
                title={t(
                  '这个公式比较复杂，下面的简化表单没法完整还原，请在表达式编辑模式下修改。',
                )}
              />
            ) : (
              <>
                {requestRuleGroups.map((group, gi) => (
                  <RuleGroupCard
                    key={`rule-group-${gi}`}
                    group={group}
                    index={gi}
                    t={t}
                    onChange={(nextGroup) => {
                      const next = [...requestRuleGroups];
                      next[gi] = nextGroup;
                      handleRequestRuleGroupsChange(next);
                    }}
                    onRemove={() => {
                      handleRequestRuleGroupsChange(
                        requestRuleGroups.filter((_, i) => i !== gi),
                      );
                    }}
                  />
                ))}
                <Button
                  icon={<IconPlus />}
                  size='small'
                  theme='light'
                  onClick={() =>
                    handleRequestRuleGroupsChange([
                      ...requestRuleGroups,
                      createEmptyRuleGroup(),
                    ])
                  }
                  style={{ marginTop: 4 }}
                >
                  {t('添加条件组')}
                </Button>
              </>
            )}
          </>
        )}
      </Card>

      <Card
        bodyStyle={{ padding: 16 }}
        style={{ marginBottom: 12, background: 'var(--semi-color-fill-0)' }}
      >
        <div className='font-medium mb-2'>{t('Token 估算器')}</div>
        <div className='text-xs text-gray-500 mb-3'>
          {t('输入 Token 数量，查看按当前配置的预计费用（不含分组倍率）。')}
        </div>
        <div
          style={{
            display: 'grid',
            gridTemplateColumns: 'repeat(3, minmax(0, 1fr))',
            gap: 12,
            marginBottom: 12,
          }}
        >
          <div>
            <Text size='small' className='mb-1' style={{ display: 'block' }}>
              {t('计费输入 Token')} (p)
            </Text>
            <InputNumber
              value={billableInputTokens}
              min={0}
              onChange={(val) => setBillableInputTokens(val ?? 0)}
              style={{ width: '100%' }}
            />
          </div>
          <div>
            <Text size='small' className='mb-1' style={{ display: 'block' }}>
              {t('计费输出 Token')} (c)
            </Text>
            <InputNumber
              value={billableOutputTokens}
              min={0}
              onChange={(val) => setBillableOutputTokens(val ?? 0)}
              style={{ width: '100%' }}
            />
          </div>
          <div>
            <Text size='small' className='mb-1' style={{ display: 'block' }}>
              {t('完整输入长度')} (len)
            </Text>
            <InputNumber
              value={fullInputLength}
              min={0}
              onChange={(val) => setFullInputLength(val ?? 0)}
              style={{ width: '100%' }}
            />
          </div>
        </div>
        {/* Cache token inputs — shown when expression uses cache variables */}
        <CacheTokenEstimatorInputs
          effectiveExpr={effectiveExpr}
          extraTokenValues={extraTokenValues}
          extraTokenSetters={extraTokenSetters}
          t={t}
        />
        <div
          style={{
            padding: '10px 14px',
            borderRadius: 8,
            background: hasEstimatorError
              ? 'var(--semi-color-danger-light-default)'
              : 'var(--semi-color-primary-light-default)',
            border: `1px solid ${hasEstimatorError ? 'var(--semi-color-danger)' : 'var(--semi-color-primary)'}`,
          }}
        >
          {hasEstimatorError ? (
            <Text type='danger'>
              {t('表达式错误')}
              {!visualExpression.invalid && evalResult.error
                ? `: ${evalResult.error}`
                : ''}
            </Text>
          ) : (
            <div>
              <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
                <Text strong style={{ fontSize: 15 }}>
                  {t('预计费用')}：{renderQuota(evalResult.cost, 4)}
                </Text>
                {evalResult.matchedTier && (
                  <Tag size='small' color='blue' type='light'>
                    {t('命中档位')}：{evalResult.matchedTier}
                  </Tag>
                )}
              </div>
              <Text
                size='small'
                style={{
                  display: 'block',
                  marginTop: 2,
                  color: 'var(--semi-color-text-3)',
                }}
              >
                {t('原始额度')}：{evalResult.cost.toLocaleString()}
              </Text>
            </div>
          )}
        </div>
      </Card>

      <LlmPromptHelper t={t} model={model} />
    </div>
  );
}
