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

import React, { useState } from 'react';
import { Banner, Button, Tag, TextArea, Typography } from '@douyinfe/semi-ui';
import {
  MATCH_CONTAINS,
  MATCH_EQ,
  MATCH_RANGE,
  SOURCE_HEADER,
  SOURCE_PARAM,
  SOURCE_TIME,
} from '../requestRuleExpr';

const { Text } = Typography;

const PRESET_GROUPS = [
  {
    group: '固定价格',
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
    group: '阶梯计费',
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
    group: '多模态',
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
    group: '请求条件',
    presets: [
      {
        key: 'claude-opus-fast',
        label: 'Claude Opus 4.6 Fast',
        expr: 'tier("base", p * 5 + c * 25 + cr * 0.5 + cc * 6.25 + cc1h * 10)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_HEADER,
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
                source: SOURCE_PARAM,
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
                source: SOURCE_PARAM,
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
    group: '时间促销',
    presets: [
      {
        key: 'night-discount',
        label: '夜间半价',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME,
                timeFunc: 'hour',
                timezone: 'Asia/Shanghai',
                mode: MATCH_RANGE,
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
        label: '周末8折',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME,
                timeFunc: 'weekday',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '0',
              },
            ],
            multiplier: '0.8',
          },
          {
            conditions: [
              {
                source: SOURCE_TIME,
                timeFunc: 'weekday',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '6',
              },
            ],
            multiplier: '0.8',
          },
        ],
      },
      {
        key: 'new-year-promo',
        label: '新年促销',
        expr: 'tier("base", p * 3 + c * 15)',
        requestRules: [
          {
            conditions: [
              {
                source: SOURCE_TIME,
                timeFunc: 'month',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '1',
              },
              {
                source: SOURCE_TIME,
                timeFunc: 'day',
                timezone: 'Asia/Shanghai',
                mode: MATCH_EQ,
                value: '1',
              },
            ],
            multiplier: '0.5',
          },
        ],
      },
    ],
  },
];

const PRESET_DEFAULT_VISIBLE = 2;

export function PresetSection({ applyPreset, t }) {
  const [expanded, setExpanded] = useState(false);
  const visibleGroups = expanded
    ? PRESET_GROUPS
    : PRESET_GROUPS.slice(0, PRESET_DEFAULT_VISIBLE);
  const hasMore = PRESET_GROUPS.length > PRESET_DEFAULT_VISIBLE;

  return (
    <div style={{ marginBottom: 12 }}>
      <div
        style={{
          display: 'flex',
          alignItems: 'center',
          gap: 8,
          marginBottom: 6,
        }}
      >
        <Text size='small' style={{ color: 'var(--semi-color-text-2)' }}>
          {t('预设模板')}
        </Text>
        {hasMore && (
          <Button
            theme='borderless'
            size='small'
            onClick={() => setExpanded(!expanded)}
            style={{
              padding: '0 4px',
              fontSize: 12,
              color: 'var(--semi-color-primary)',
            }}
          >
            {expanded ? t('收起') : t('更多模板...')}
          </Button>
        )}
      </div>
      <div style={{ display: 'flex', flexDirection: 'column', gap: 4 }}>
        {visibleGroups.map((g) => (
          <div
            key={g.group}
            style={{
              display: 'flex',
              alignItems: 'center',
              gap: 6,
              flexWrap: 'wrap',
            }}
          >
            <Tag
              size='small'
              color='grey'
              style={{ minWidth: 60, textAlign: 'center' }}
            >
              {t(g.group)}
            </Tag>
            {g.presets.map((p) => (
              <Button
                key={p.key}
                size='small'
                theme='light'
                onClick={() => applyPreset(p)}
              >
                {p.label}
              </Button>
            ))}
          </div>
        ))}
      </div>
    </div>
  );
}

export function RawExprEditor({ exprString, onChange, t }) {
  return (
    <div>
      <Banner
        type='info'
        description={
          <div>
            <div>
              {t('变量')}: <code>p</code> ({t('输入 Token')}), <code>c</code> (
              {t('输出 Token')}), <code>len</code> ({t('输入长度')}),{' '}
              <code>cr</code> ({t('缓存读取')}), <code>cc</code> (
              {t('缓存创建')}), <code>cc1h</code> ({t('缓存创建-1小时')})
            </div>
            <div>
              {t('函数')}: <code>tier(name, value)</code>,{' '}
              <code>max(a, b)</code>, <code>min(a, b)</code>,{' '}
              <code>ceil(x)</code>, <code>floor(x)</code>, <code>abs(x)</code>,{' '}
              <code>header(name)</code>, <code>param(path)</code>,{' '}
              <code>has(source, text)</code>
            </div>
          </div>
        }
        style={{ marginBottom: 12 }}
      />

      <TextArea
        value={exprString}
        onChange={onChange}
        autosize={{ minRows: 3, maxRows: 12 }}
        style={{ fontFamily: 'monospace', fontSize: 13 }}
        placeholder={t('输入计费表达式...')}
      />
    </div>
  );
}
