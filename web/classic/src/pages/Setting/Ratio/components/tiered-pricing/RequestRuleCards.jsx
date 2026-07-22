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

import React from 'react';
import { Button, Input, Select, Tag, Typography } from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import {
  COMMON_TIMEZONES,
  MATCH_CONTAINS,
  MATCH_EQ,
  MATCH_EXISTS,
  MATCH_GTE,
  MATCH_RANGE,
  SOURCE_HEADER,
  SOURCE_PARAM,
  SOURCE_TIME,
  TIME_FUNCS,
  createEmptyCondition,
  createEmptyTimeCondition,
  getRequestRuleMatchOptions,
  normalizeCondition,
} from '../requestRuleExpr';

const { Text } = Typography;

const TIME_FUNC_LABELS = {
  hour: '小时',
  minute: '分钟',
  weekday: '星期',
  month: '月份',
  day: '日期',
};

const TIME_FUNC_HINTS = {
  hour: '0~23',
  minute: '0~59',
  weekday: '0=周日 1=周一 2=周二 3=周三 4=周四 5=周五 6=周六',
  month: '1=一月 ... 12=十二月',
  day: '1~31',
};

const TIME_FUNC_PLACEHOLDERS = {
  hour: '0-23',
  minute: '0-59',
  weekday: '0-6',
  month: '1-12',
  day: '1-31',
};

function RuleConditionRow({ cond, onChange, onRemove, t }) {
  const normalized = normalizeCondition(cond);
  const isTime = normalized.source === SOURCE_TIME;
  const matchOptions = getRequestRuleMatchOptions(normalized.source, t);

  const sourceSelect = (
    <Select
      size='small'
      value={normalized.source}
      onChange={(value) => {
        if (value === SOURCE_TIME) {
          onChange(
            normalizeCondition({
              source: SOURCE_TIME,
              timeFunc: 'hour',
              timezone: 'Asia/Shanghai',
              mode: MATCH_GTE,
            }),
          );
        } else {
          onChange(
            normalizeCondition({ source: value, path: '', mode: MATCH_EQ }),
          );
        }
      }}
      style={{ width: 110 }}
    >
      <Select.Option value={SOURCE_PARAM}>{t('请求参数')}</Select.Option>
      <Select.Option value={SOURCE_HEADER}>{t('请求头')}</Select.Option>
      <Select.Option value={SOURCE_TIME}>{t('时间条件')}</Select.Option>
    </Select>
  );

  const removeBtn = (
    <Button
      icon={<IconDelete />}
      type='danger'
      theme='borderless'
      size='small'
      onClick={onRemove}
    />
  );

  if (isTime) {
    const isRange = normalized.mode === MATCH_RANGE;
    const ph = TIME_FUNC_PLACEHOLDERS[normalized.timeFunc] || '';
    const hint = TIME_FUNC_HINTS[normalized.timeFunc] || '';
    return (
      <div
        style={{
          marginBottom: 8,
          padding: '8px 10px',
          borderRadius: 6,
          background: 'var(--semi-color-fill-0)',
          display: 'flex',
          flexDirection: 'column',
          gap: 6,
        }}
      >
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          {sourceSelect}
          <Select
            size='small'
            value={normalized.timeFunc}
            onChange={(value) => onChange({ ...normalized, timeFunc: value })}
            style={{ flex: 1 }}
          >
            {TIME_FUNCS.map((fn) => (
              <Select.Option key={fn} value={fn}>
                {t(TIME_FUNC_LABELS[fn] || fn)}
              </Select.Option>
            ))}
          </Select>
          {removeBtn}
        </div>
        <Select
          size='small'
          value={normalized.timezone}
          onChange={(value) => onChange({ ...normalized, timezone: value })}
          filter
          allowCreate
          placeholder={t('时区')}
        >
          {COMMON_TIMEZONES.map((tz) => (
            <Select.Option key={tz.value} value={tz.value}>
              {tz.label}
            </Select.Option>
          ))}
        </Select>
        <div style={{ display: 'flex', gap: 6, alignItems: 'center' }}>
          <Select
            size='small'
            value={normalized.mode}
            onChange={(value) =>
              onChange(normalizeCondition({ ...normalized, mode: value }))
            }
            style={{ flex: 1 }}
          >
            {matchOptions.map((item) => (
              <Select.Option key={item.value} value={item.value}>
                {item.label}
              </Select.Option>
            ))}
          </Select>
          {isRange ? (
            <div
              style={{ display: 'flex', gap: 4, alignItems: 'center', flex: 1 }}
            >
              <Input
                size='small'
                value={normalized.rangeStart}
                placeholder={ph}
                style={{ flex: 1 }}
                onChange={(value) =>
                  onChange({ ...normalized, rangeStart: value })
                }
              />
              <span>~</span>
              <Input
                size='small'
                value={normalized.rangeEnd}
                placeholder={ph}
                style={{ flex: 1 }}
                onChange={(value) =>
                  onChange({ ...normalized, rangeEnd: value })
                }
              />
            </div>
          ) : (
            <Input
              size='small'
              value={normalized.value}
              placeholder={ph}
              style={{ flex: 1 }}
              onChange={(value) => onChange({ ...normalized, value })}
            />
          )}
        </div>
        {hint && (
          <Text size='small' style={{ color: 'var(--semi-color-text-3)' }}>
            {t(hint)}
          </Text>
        )}
      </div>
    );
  }

  const showValue = normalized.mode !== MATCH_EXISTS;
  return (
    <div
      style={{
        marginBottom: 8,
        padding: '8px 10px',
        borderRadius: 6,
        background: 'var(--semi-color-fill-0)',
        display: 'grid',
        gridTemplateColumns: '1fr 1fr auto',
        gap: '6px 8px',
      }}
    >
      {sourceSelect}
      <Input
        size='small'
        value={normalized.path}
        placeholder={
          normalized.source === SOURCE_HEADER
            ? t('例如 anthropic-beta')
            : t('例如 service_tier')
        }
        onChange={(value) => onChange({ ...normalized, path: value })}
      />
      {removeBtn}
      <Select
        size='small'
        value={normalized.mode}
        onChange={(value) =>
          onChange(
            normalizeCondition({
              ...normalized,
              mode: value,
              value: value === MATCH_EXISTS ? '' : normalized.value,
            }),
          )
        }
      >
        {matchOptions.map((item) => (
          <Select.Option key={item.value} value={item.value}>
            {item.label}
          </Select.Option>
        ))}
      </Select>
      <Input
        size='small'
        value={normalized.value}
        placeholder={
          normalized.mode === MATCH_CONTAINS
            ? t('匹配内容')
            : normalized.mode === MATCH_EXISTS
              ? ''
              : t('匹配值')
        }
        disabled={!showValue}
        onChange={(value) => onChange({ ...normalized, value })}
      />
      <div />
    </div>
  );
}

export function RuleGroupCard({ group, index, onChange, onRemove, t }) {
  const conditions = group.conditions || [];

  const updateCondition = (ci, newCond) => {
    const next = conditions.map((c, i) => (i === ci ? newCond : c));
    onChange({ ...group, conditions: next });
  };
  const removeCondition = (ci) => {
    const next = conditions.filter((_, i) => i !== ci);
    onChange({
      ...group,
      conditions: next.length > 0 ? next : [createEmptyCondition()],
    });
  };
  const addCondition = (cond) => {
    onChange({ ...group, conditions: [...conditions, cond] });
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
        <Tag color='blue' size='small'>
          {t('第 {{n}} 组', { n: index + 1 })}
        </Tag>
        <Button
          icon={<IconDelete />}
          type='danger'
          theme='borderless'
          size='small'
          onClick={onRemove}
        />
      </div>

      <div style={{ marginBottom: 8 }}>
        <Text
          size='small'
          style={{
            color: 'var(--semi-color-text-2)',
            display: 'block',
            marginBottom: 4,
          }}
        >
          {t('条件')}
          {conditions.length > 1 ? ` (${t('同时满足')})` : ''}
        </Text>
        {conditions.map((cond, ci) => (
          <RuleConditionRow
            key={ci}
            cond={cond}
            onChange={(nc) => updateCondition(ci, nc)}
            onRemove={() => removeCondition(ci)}
            t={t}
          />
        ))}
        <div style={{ display: 'flex', gap: 6 }}>
          <Button
            icon={<IconPlus />}
            size='small'
            theme='borderless'
            onClick={() => addCondition(createEmptyCondition())}
          >
            {t('添加条件')}
          </Button>
          <Button
            icon={<IconPlus />}
            size='small'
            theme='borderless'
            onClick={() => addCondition(createEmptyTimeCondition())}
          >
            {t('添加时间条件')}
          </Button>
        </div>
      </div>

      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <Text
          size='small'
          style={{ color: 'var(--semi-color-text-2)', whiteSpace: 'nowrap' }}
        >
          {t('倍率')}
        </Text>
        <Input
          size='small'
          value={group.multiplier || ''}
          placeholder={t('例如 0.5 或 2')}
          suffix='x'
          onChange={(value) => onChange({ ...group, multiplier: value })}
          style={{ width: 160 }}
        />
      </div>
    </div>
  );
}
