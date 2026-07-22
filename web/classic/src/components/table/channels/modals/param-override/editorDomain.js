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

import { verifyJSON } from '../../../../../helpers';
import {
  CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
} from '../../../../../constants/channel-affinity-template.constants';

export const OPERATION_MODE_OPTIONS = [
  { label: '设置字段', value: 'set' },
  { label: '删除字段', value: 'delete' },
  { label: '追加到末尾', value: 'append' },
  { label: '追加到开头', value: 'prepend' },
  { label: '复制字段', value: 'copy' },
  { label: '移动字段', value: 'move' },
  { label: '字符串替换', value: 'replace' },
  { label: '正则替换', value: 'regex_replace' },
  { label: '裁剪前缀', value: 'trim_prefix' },
  { label: '裁剪后缀', value: 'trim_suffix' },
  { label: '确保前缀', value: 'ensure_prefix' },
  { label: '确保后缀', value: 'ensure_suffix' },
  { label: '去掉空白', value: 'trim_space' },
  { label: '转小写', value: 'to_lower' },
  { label: '转大写', value: 'to_upper' },
  { label: '返回自定义错误', value: 'return_error' },
  { label: '清理对象项', value: 'prune_objects' },
  { label: '请求头透传', value: 'pass_headers' },
  { label: '字段同步', value: 'sync_fields' },
  { label: '设置请求头', value: 'set_header' },
  { label: '删除请求头', value: 'delete_header' },
  { label: '复制请求头', value: 'copy_header' },
  { label: '移动请求头', value: 'move_header' },
];

export const OPERATION_MODE_VALUES = new Set(
  OPERATION_MODE_OPTIONS.map((item) => item.value),
);

export const CONDITION_MODE_OPTIONS = [
  { label: '完全匹配', value: 'full' },
  { label: '前缀匹配', value: 'prefix' },
  { label: '后缀匹配', value: 'suffix' },
  { label: '包含', value: 'contains' },
  { label: '大于', value: 'gt' },
  { label: '大于等于', value: 'gte' },
  { label: '小于', value: 'lt' },
  { label: '小于等于', value: 'lte' },
];

export const CONDITION_MODE_VALUES = new Set(
  CONDITION_MODE_OPTIONS.map((item) => item.value),
);

export const MODE_META = {
  delete: { path: true },
  set: { path: true, value: true, keepOrigin: true },
  append: { path: true, value: true, keepOrigin: true },
  prepend: { path: true, value: true, keepOrigin: true },
  copy: { from: true, to: true },
  move: { from: true, to: true },
  replace: { path: true, from: true, to: false },
  regex_replace: { path: true, from: true, to: false },
  trim_prefix: { path: true, value: true },
  trim_suffix: { path: true, value: true },
  ensure_prefix: { path: true, value: true },
  ensure_suffix: { path: true, value: true },
  trim_space: { path: true },
  to_lower: { path: true },
  to_upper: { path: true },
  return_error: { value: true },
  prune_objects: { pathOptional: true, value: true },
  pass_headers: { value: true, keepOrigin: true },
  sync_fields: { from: true, to: true },
  set_header: { path: true, value: true, keepOrigin: true },
  delete_header: { path: true },
  copy_header: { from: true, to: true, keepOrigin: true, pathAlias: true },
  move_header: { from: true, to: true, keepOrigin: true, pathAlias: true },
};

export const VALUE_REQUIRED_MODES = new Set([
  'trim_prefix',
  'trim_suffix',
  'ensure_prefix',
  'ensure_suffix',
  'set_header',
  'return_error',
  'prune_objects',
  'pass_headers',
]);

export const FROM_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'replace',
  'regex_replace',
  'copy_header',
  'move_header',
  'sync_fields',
]);

export const TO_REQUIRED_MODES = new Set([
  'copy',
  'move',
  'copy_header',
  'move_header',
  'sync_fields',
]);

export const MODE_DESCRIPTIONS = {
  set: '把值写入目标字段',
  delete: '删除目标字段',
  append: '把值追加到数组 / 字符串 / 对象末尾',
  prepend: '把值追加到数组 / 字符串 / 对象开头',
  copy: '把来源字段复制到目标字段',
  move: '把来源字段移动到目标字段',
  replace: '在目标字段里做字符串替换',
  regex_replace: '在目标字段里做正则替换',
  trim_prefix: '去掉字符串前缀',
  trim_suffix: '去掉字符串后缀',
  ensure_prefix: '确保字符串有指定前缀',
  ensure_suffix: '确保字符串有指定后缀',
  trim_space: '去掉字符串头尾空白',
  to_lower: '把字符串转成小写',
  to_upper: '把字符串转成大写',
  return_error: '立即返回自定义错误',
  prune_objects: '按条件清理对象中的子项',
  pass_headers: '把指定请求头透传到上游请求',
  sync_fields: '在一个字段有值、另一个缺失时自动补齐',
  set_header:
    '设置运行期请求头：可直接覆盖整条值，也可对逗号分隔的 token 做删除、替换、追加或白名单保留',
  delete_header: '删除运行期请求头',
  copy_header: '复制请求头',
  move_header: '移动请求头',
};

export const getModePathLabel = (mode) => {
  if (mode === 'set_header' || mode === 'delete_header') {
    return '请求头名称';
  }
  if (mode === 'prune_objects') {
    return '目标路径（可选）';
  }
  return '目标字段路径';
};

export const getModePathPlaceholder = (mode) => {
  if (mode === 'set_header') return 'Authorization';
  if (mode === 'delete_header') return 'X-Debug-Mode';
  if (mode === 'prune_objects') return 'messages';
  return 'temperature';
};

export const getModeFromLabel = (mode) => {
  if (mode === 'replace') return '匹配文本';
  if (mode === 'regex_replace') return '正则表达式';
  if (mode === 'copy_header' || mode === 'move_header') return '来源请求头';
  return '来源字段';
};

export const getModeFromPlaceholder = (mode) => {
  if (mode === 'replace') return 'openai/';
  if (mode === 'regex_replace') return '^gpt-';
  if (mode === 'copy_header' || mode === 'move_header') return 'Authorization';
  return 'model';
};

export const getModeToLabel = (mode) => {
  if (mode === 'replace' || mode === 'regex_replace') return '替换为';
  if (mode === 'copy_header' || mode === 'move_header') return '目标请求头';
  return '目标字段';
};

export const getModeToPlaceholder = (mode) => {
  if (mode === 'replace') return '（可留空）';
  if (mode === 'regex_replace') return 'openai/gpt-';
  if (mode === 'copy_header' || mode === 'move_header')
    return 'X-Upstream-Auth';
  return 'original_model';
};

export const getModeValueLabel = (mode) => {
  if (mode === 'set_header') return '请求头值（支持字符串或 JSON 映射）';
  if (mode === 'pass_headers') return '透传请求头（支持逗号分隔或 JSON 数组）';
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  ) {
    return '前后缀文本';
  }
  if (mode === 'prune_objects') {
    return '清理规则（字符串或 JSON 对象）';
  }
  return '值（支持 JSON 或普通文本）';
};

export const HEADER_VALUE_JSONC_EXAMPLE = `{
  // 置空：删除 Bedrock 不支持的 beta特性
  "files-api-2025-04-14": null,

  // 替换：把旧特性改成兼容特性
  "advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",

  // 追加：在末尾补一个需要的特性
  "$append": ["context-1m-2025-08-07"]
}`;

export const getModeValuePlaceholder = (mode) => {
  if (mode === 'set_header') {
    return [
      '纯字符串（整条覆盖）：',
      'Bearer sk-xxx',
      '',
      '或使用 JSON 规则：',
      '{',
      '  "files-api-2025-04-14": null,',
      '  "advanced-tool-use-2025-11-20": "tool-search-tool-2025-10-19",',
      '  "$append": ["context-1m-2025-08-07"]',
      '}',
    ].join('\n');
  }
  if (mode === 'pass_headers') return 'Authorization, X-Request-Id';
  if (
    mode === 'trim_prefix' ||
    mode === 'trim_suffix' ||
    mode === 'ensure_prefix' ||
    mode === 'ensure_suffix'
  ) {
    return 'openai/';
  }
  if (mode === 'prune_objects') {
    return '{"type":"redacted_thinking"}';
  }
  return '0.7';
};

export const SYNC_TARGET_TYPE_OPTIONS = [
  { label: '请求体字段', value: 'json' },
  { label: '请求头字段', value: 'header' },
];

export const LEGACY_TEMPLATE = {
  temperature: 0,
  max_tokens: 1000,
};

export const OPERATION_TEMPLATE = {
  operations: [
    {
      description: 'Set default temperature for openai/* models.',
      path: 'temperature',
      mode: 'set',
      value: 0.7,
      conditions: [
        {
          path: 'model',
          mode: 'prefix',
          value: 'openai/',
        },
      ],
      logic: 'AND',
    },
  ],
};

export const HEADER_PASSTHROUGH_TEMPLATE = {
  operations: [
    {
      description: 'Pass through X-Request-Id header to upstream.',
      mode: 'pass_headers',
      value: ['X-Request-Id'],
      keep_origin: true,
    },
  ],
};

export const GEMINI_IMAGE_4K_TEMPLATE = {
  operations: [
    {
      description:
        'Set imageSize to 4K when model contains gemini/image and ends with 4k.',
      mode: 'set',
      path: 'generationConfig.imageConfig.imageSize',
      value: '4K',
      conditions: [
        {
          path: 'original_model',
          mode: 'contains',
          value: 'gemini',
        },
        {
          path: 'original_model',
          mode: 'contains',
          value: 'image',
        },
        {
          path: 'original_model',
          mode: 'suffix',
          value: '4k',
        },
      ],
      logic: 'AND',
    },
  ],
};

export const AWS_BEDROCK_ANTHROPIC_COMPAT_TEMPLATE = {
  operations: [
    {
      description:
        'Normalize anthropic-beta header tokens for Bedrock compatibility.',
      mode: 'set_header',
      path: 'anthropic-beta',
      // https://github.com/BerriAI/litellm/blob/main/litellm/anthropic_beta_headers_config.json
      value: {
        'advanced-tool-use-2025-11-20': 'tool-search-tool-2025-10-19',
        bash_20241022: null,
        bash_20250124: null,
        'code-execution-2025-08-25': null,
        'compact-2026-01-12': 'compact-2026-01-12',
        'computer-use-2025-01-24': 'computer-use-2025-01-24',
        'computer-use-2025-11-24': 'computer-use-2025-11-24',
        'context-1m-2025-08-07': 'context-1m-2025-08-07',
        'context-management-2025-06-27': 'context-management-2025-06-27',
        'effort-2025-11-24': null,
        'fast-mode-2026-02-01': null,
        'files-api-2025-04-14': null,
        'fine-grained-tool-streaming-2025-05-14': null,
        'interleaved-thinking-2025-05-14': 'interleaved-thinking-2025-05-14',
        'mcp-client-2025-11-20': null,
        'mcp-client-2025-04-04': null,
        'mcp-servers-2025-12-04': null,
        'output-128k-2025-02-19': null,
        'structured-output-2024-03-01': null,
        'prompt-caching-scope-2026-01-05': null,
        'skills-2025-10-02': null,
        'structured-outputs-2025-11-13': null,
        text_editor_20241022: null,
        text_editor_20250124: null,
        'token-efficient-tools-2025-02-19': null,
        'tool-search-tool-2025-10-19': 'tool-search-tool-2025-10-19',
        'web-fetch-2025-09-10': null,
        'web-search-2025-03-05': null,
        'oauth-2025-04-20': null,
      },
    },
    {
      description:
        'Remove all tools[*].custom.input_examples before upstream relay.',
      mode: 'delete',
      path: 'tools.*.custom.input_examples',
    },
  ],
};

export const TEMPLATE_GROUP_OPTIONS = [
  { label: '基础模板', value: 'basic' },
  { label: '场景模板', value: 'scenario' },
];

export const TEMPLATE_PRESET_CONFIG = {
  operations_default: {
    group: 'basic',
    label: '新格式模板（规则集）',
    kind: 'operations',
    payload: OPERATION_TEMPLATE,
  },
  legacy_default: {
    group: 'basic',
    label: '旧格式模板（JSON 对象）',
    kind: 'legacy',
    payload: LEGACY_TEMPLATE,
  },
  pass_headers_auth: {
    group: 'scenario',
    label: '请求头透传（X-Request-Id）',
    kind: 'operations',
    payload: HEADER_PASSTHROUGH_TEMPLATE,
  },
  gemini_image_4k: {
    group: 'scenario',
    label: 'Gemini 图片 4K',
    kind: 'operations',
    payload: GEMINI_IMAGE_4K_TEMPLATE,
  },
  claude_cli_headers_passthrough: {
    group: 'scenario',
    label: 'Claude CLI 请求头透传',
    kind: 'operations',
    payload: CLAUDE_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  codex_cli_headers_passthrough: {
    group: 'scenario',
    label: 'Codex CLI 请求头透传',
    kind: 'operations',
    payload: CODEX_CLI_HEADER_PASSTHROUGH_TEMPLATE,
  },
  aws_bedrock_anthropic_beta_override: {
    group: 'scenario',
    label: 'AWS Bedrock Claude 兼容模板',
    kind: 'operations',
    payload: AWS_BEDROCK_ANTHROPIC_COMPAT_TEMPLATE,
  },
};

export const FIELD_GUIDE_TARGET_OPTIONS = [
  { label: '填入目标路径', value: 'path' },
  { label: '填入来源字段', value: 'from' },
  { label: '填入目标字段', value: 'to' },
];

export const BUILTIN_FIELD_SECTIONS = [
  {
    title: '常用请求字段',
    fields: [
      {
        key: 'model',
        label: '模型名称',
        tip: '支持多级模型名，例如 openai/gpt-4o-mini',
      },
      { key: 'temperature', label: '采样温度', tip: '控制输出随机性' },
      { key: 'max_tokens', label: '最大输出 Token', tip: '控制输出长度上限' },
      {
        key: 'messages.-1.content',
        label: '最后一条消息内容',
        tip: '常用于重写用户输入',
      },
    ],
  },
  {
    title: '上下文字段',
    fields: [
      { key: 'retry.is_retry', label: '是否重试', tip: 'true 表示重试请求' },
      { key: 'last_error.code', label: '上次错误码', tip: '配合重试策略使用' },
      {
        key: 'metadata.conversation_id',
        label: '会话 ID',
        tip: '可用于路由或缓存命中',
      },
    ],
  },
  {
    title: '请求头映射字段',
    fields: [
      {
        key: 'header_override_normalized.authorization',
        label: '标准化 Authorization',
        tip: '统一小写后可稳定匹配',
      },
      {
        key: 'header_override_normalized.x_debug_mode',
        label: '标准化 X-Debug-Mode',
        tip: '适合灰度 / 调试开关判断',
      },
    ],
  },
];

export const OPERATION_MODE_LABEL_MAP = OPERATION_MODE_OPTIONS.reduce(
  (acc, item) => {
    acc[item.value] = item.label;
    return acc;
  },
  {},
);

let localIdSeed = 0;
export const nextLocalId = () =>
  `param_override_${Date.now()}_${localIdSeed++}`;

export const toValueText = (value) => {
  if (value === undefined) return '';
  if (typeof value === 'string') return value;
  try {
    return JSON.stringify(value);
  } catch (error) {
    return String(value);
  }
};

export const parseLooseValue = (valueText) => {
  const raw = String(valueText ?? '');
  if (raw.trim() === '') return '';
  try {
    return JSON.parse(raw);
  } catch (error) {
    return raw;
  }
};

export const parsePassHeaderNames = (rawValue) => {
  if (Array.isArray(rawValue)) {
    return rawValue.map((item) => String(item ?? '').trim()).filter(Boolean);
  }
  if (rawValue && typeof rawValue === 'object') {
    if (Array.isArray(rawValue.headers)) {
      return rawValue.headers
        .map((item) => String(item ?? '').trim())
        .filter(Boolean);
    }
    if (rawValue.header !== undefined) {
      const single = String(rawValue.header ?? '').trim();
      return single ? [single] : [];
    }
    return [];
  }
  if (typeof rawValue === 'string') {
    return rawValue
      .split(',')
      .map((item) => item.trim())
      .filter(Boolean);
  }
  return [];
};

export const parseReturnErrorDraft = (valueText) => {
  const defaults = {
    message: '',
    statusCode: 400,
    code: '',
    type: '',
    skipRetry: true,
    simpleMode: true,
  };

  const raw = String(valueText ?? '').trim();
  if (!raw) {
    return defaults;
  }

  try {
    const parsed = JSON.parse(raw);
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const statusRaw =
        parsed.status_code !== undefined ? parsed.status_code : parsed.status;
      const statusValue = Number(statusRaw);
      return {
        ...defaults,
        message: String(parsed.message || parsed.msg || '').trim(),
        statusCode:
          Number.isInteger(statusValue) &&
          statusValue >= 100 &&
          statusValue <= 599
            ? statusValue
            : 400,
        code: String(parsed.code || '').trim(),
        type: String(parsed.type || '').trim(),
        skipRetry: parsed.skip_retry !== false,
        simpleMode: false,
      };
    }
  } catch (error) {
    // treat as plain text message
  }

  return {
    ...defaults,
    message: raw,
    simpleMode: true,
  };
};

export const buildReturnErrorValueText = (draft = {}) => {
  const message = String(draft.message || '').trim();
  if (draft.simpleMode) {
    return message;
  }

  const statusCode = Number(draft.statusCode);
  const payload = {
    message,
    status_code:
      Number.isInteger(statusCode) && statusCode >= 100 && statusCode <= 599
        ? statusCode
        : 400,
  };
  const code = String(draft.code || '').trim();
  const type = String(draft.type || '').trim();
  if (code) payload.code = code;
  if (type) payload.type = type;
  if (draft.skipRetry === false) {
    payload.skip_retry = false;
  }
  return JSON.stringify(payload);
};

export const normalizePruneRule = (rule = {}) => ({
  id: nextLocalId(),
  path: typeof rule.path === 'string' ? rule.path : '',
  mode: CONDITION_MODE_VALUES.has(rule.mode) ? rule.mode : 'full',
  value_text: toValueText(rule.value),
  invert: rule.invert === true,
  pass_missing_key: rule.pass_missing_key === true,
});

export const parsePruneObjectsDraft = (valueText) => {
  const defaults = {
    simpleMode: true,
    typeText: '',
    logic: 'AND',
    recursive: true,
    rules: [],
  };

  const raw = String(valueText ?? '').trim();
  if (!raw) {
    return defaults;
  }

  try {
    const parsed = JSON.parse(raw);
    if (typeof parsed === 'string') {
      return {
        ...defaults,
        simpleMode: true,
        typeText: parsed.trim(),
      };
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const rules = [];
      if (
        parsed.where &&
        typeof parsed.where === 'object' &&
        !Array.isArray(parsed.where)
      ) {
        Object.entries(parsed.where).forEach(([path, value]) => {
          rules.push(
            normalizePruneRule({
              path,
              mode: 'full',
              value,
            }),
          );
        });
      }
      if (Array.isArray(parsed.conditions)) {
        parsed.conditions.forEach((item) => {
          if (item && typeof item === 'object') {
            rules.push(normalizePruneRule(item));
          }
        });
      } else if (
        parsed.conditions &&
        typeof parsed.conditions === 'object' &&
        !Array.isArray(parsed.conditions)
      ) {
        Object.entries(parsed.conditions).forEach(([path, value]) => {
          rules.push(
            normalizePruneRule({
              path,
              mode: 'full',
              value,
            }),
          );
        });
      }

      const typeText =
        parsed.type === undefined ? '' : String(parsed.type).trim();
      const logic =
        String(parsed.logic || 'AND').toUpperCase() === 'OR' ? 'OR' : 'AND';
      const recursive = parsed.recursive !== false;
      const hasAdvancedFields =
        parsed.logic !== undefined ||
        parsed.recursive !== undefined ||
        parsed.where !== undefined ||
        parsed.conditions !== undefined;

      return {
        ...defaults,
        simpleMode: !hasAdvancedFields,
        typeText,
        logic,
        recursive,
        rules,
      };
    }
    return {
      ...defaults,
      simpleMode: true,
      typeText: String(parsed ?? '').trim(),
    };
  } catch (error) {
    return {
      ...defaults,
      simpleMode: true,
      typeText: raw,
    };
  }
};

export const buildPruneObjectsValueText = (draft = {}) => {
  const typeText = String(draft.typeText || '').trim();
  if (draft.simpleMode) {
    return typeText;
  }

  const payload = {};
  if (typeText) {
    payload.type = typeText;
  }
  if (String(draft.logic || 'AND').toUpperCase() === 'OR') {
    payload.logic = 'OR';
  }
  if (draft.recursive === false) {
    payload.recursive = false;
  }

  const conditions = (draft.rules || [])
    .filter((rule) => String(rule.path || '').trim())
    .map((rule) => {
      const conditionPayload = {
        path: String(rule.path || '').trim(),
        mode: CONDITION_MODE_VALUES.has(rule.mode) ? rule.mode : 'full',
      };
      const valueRaw = String(rule.value_text || '').trim();
      if (valueRaw !== '') {
        conditionPayload.value = parseLooseValue(valueRaw);
      }
      if (rule.invert) {
        conditionPayload.invert = true;
      }
      if (rule.pass_missing_key) {
        conditionPayload.pass_missing_key = true;
      }
      return conditionPayload;
    });

  if (conditions.length > 0) {
    payload.conditions = conditions;
  }

  if (!payload.type && !payload.conditions) {
    return JSON.stringify({ logic: 'AND' });
  }
  return JSON.stringify(payload);
};

export const parseSyncTargetSpec = (spec) => {
  const raw = String(spec ?? '').trim();
  if (!raw) return { type: 'json', key: '' };
  const idx = raw.indexOf(':');
  if (idx < 0) return { type: 'json', key: raw };
  const prefix = raw.slice(0, idx).trim().toLowerCase();
  const key = raw.slice(idx + 1).trim();
  if (prefix === 'header') {
    return { type: 'header', key };
  }
  return { type: 'json', key };
};

export const buildSyncTargetSpec = (type, key) => {
  const normalizedType = type === 'header' ? 'header' : 'json';
  const normalizedKey = String(key ?? '').trim();
  if (!normalizedKey) return '';
  return `${normalizedType}:${normalizedKey}`;
};

export const normalizeCondition = (condition = {}) => ({
  id: nextLocalId(),
  path: typeof condition.path === 'string' ? condition.path : '',
  mode: CONDITION_MODE_VALUES.has(condition.mode) ? condition.mode : 'full',
  value_text: toValueText(condition.value),
  invert: condition.invert === true,
  pass_missing_key: condition.pass_missing_key === true,
});

export const createDefaultCondition = () => normalizeCondition({});

export const normalizeOperation = (operation = {}) => ({
  id: nextLocalId(),
  description:
    typeof operation.description === 'string' ? operation.description : '',
  path: typeof operation.path === 'string' ? operation.path : '',
  mode: OPERATION_MODE_VALUES.has(operation.mode) ? operation.mode : 'set',
  value_text: toValueText(operation.value),
  keep_origin: operation.keep_origin === true,
  from: typeof operation.from === 'string' ? operation.from : '',
  to: typeof operation.to === 'string' ? operation.to : '',
  logic: String(operation.logic || 'OR').toUpperCase() === 'AND' ? 'AND' : 'OR',
  conditions: Array.isArray(operation.conditions)
    ? operation.conditions.map(normalizeCondition)
    : [],
});

export const createDefaultOperation = () => normalizeOperation({ mode: 'set' });

export const reorderOperations = (
  sourceOperations = [],
  sourceId,
  targetId,
  position = 'before',
) => {
  if (!sourceId || !targetId || sourceId === targetId) {
    return sourceOperations;
  }

  const sourceIndex = sourceOperations.findIndex(
    (item) => item.id === sourceId,
  );

  if (sourceIndex < 0) {
    return sourceOperations;
  }

  const nextOperations = [...sourceOperations];
  const [moved] = nextOperations.splice(sourceIndex, 1);
  let insertIndex = nextOperations.findIndex((item) => item.id === targetId);

  if (insertIndex < 0) {
    return sourceOperations;
  }

  if (position === 'after') {
    insertIndex += 1;
  }

  nextOperations.splice(insertIndex, 0, moved);
  return nextOperations;
};

export const getOperationSummary = (operation = {}, index = 0) => {
  const mode = operation.mode || 'set';
  const modeLabel = OPERATION_MODE_LABEL_MAP[mode] || mode;
  if (mode === 'sync_fields') {
    const from = String(operation.from || '').trim();
    const to = String(operation.to || '').trim();
    return `${index + 1}. ${modeLabel} · ${from || to || '-'}`;
  }
  const path = String(operation.path || '').trim();
  const from = String(operation.from || '').trim();
  const to = String(operation.to || '').trim();
  return `${index + 1}. ${modeLabel} · ${path || from || to || '-'}`;
};

export const getOperationModeTagColor = (mode = 'set') => {
  if (mode.includes('header')) return 'cyan';
  if (mode.includes('replace') || mode.includes('trim')) return 'violet';
  if (mode.includes('copy') || mode.includes('move')) return 'blue';
  if (mode.includes('error') || mode.includes('prune')) return 'red';
  if (mode.includes('sync')) return 'green';
  return 'grey';
};

export const parseInitialState = (rawValue) => {
  const text = typeof rawValue === 'string' ? rawValue : '';
  const trimmed = text.trim();
  if (!trimmed) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyValue: '',
      operations: [createDefaultOperation()],
      jsonText: '',
      jsonError: '',
    };
  }

  if (!verifyJSON(trimmed)) {
    return {
      editMode: 'json',
      visualMode: 'operations',
      legacyValue: '',
      operations: [createDefaultOperation()],
      jsonText: text,
      jsonError: 'JSON 格式不正确',
    };
  }

  const parsed = JSON.parse(trimmed);
  const pretty = JSON.stringify(parsed, null, 2);

  if (
    parsed &&
    typeof parsed === 'object' &&
    !Array.isArray(parsed) &&
    Array.isArray(parsed.operations)
  ) {
    return {
      editMode: 'visual',
      visualMode: 'operations',
      legacyValue: '',
      operations:
        parsed.operations.length > 0
          ? parsed.operations.map(normalizeOperation)
          : [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    };
  }

  if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
    return {
      editMode: 'visual',
      visualMode: 'legacy',
      legacyValue: pretty,
      operations: [createDefaultOperation()],
      jsonText: pretty,
      jsonError: '',
    };
  }

  return {
    editMode: 'json',
    visualMode: 'operations',
    legacyValue: '',
    operations: [createDefaultOperation()],
    jsonText: pretty,
    jsonError: '',
  };
};

export const isOperationBlank = (operation) => {
  const hasCondition = (operation.conditions || []).some(
    (condition) =>
      condition.path.trim() ||
      String(condition.value_text ?? '').trim() ||
      condition.mode !== 'full' ||
      condition.invert ||
      condition.pass_missing_key,
  );
  return (
    operation.mode === 'set' &&
    !operation.path.trim() &&
    !operation.from.trim() &&
    !operation.to.trim() &&
    String(operation.value_text ?? '').trim() === '' &&
    !operation.keep_origin &&
    !hasCondition
  );
};

export const buildConditionPayload = (condition) => {
  const path = condition.path.trim();
  if (!path) return null;
  const payload = {
    path,
    mode: condition.mode || 'full',
    value: parseLooseValue(condition.value_text),
  };
  if (condition.invert) payload.invert = true;
  if (condition.pass_missing_key) payload.pass_missing_key = true;
  return payload;
};

export const validateOperations = (operations, t) => {
  for (let i = 0; i < operations.length; i++) {
    const op = operations[i];
    const mode = op.mode || 'set';
    const meta = MODE_META[mode] || MODE_META.set;
    const line = i + 1;
    const pathValue = op.path.trim();
    const fromValue = op.from.trim();
    const toValue = op.to.trim();

    if (meta.path && !pathValue) {
      return t('第 {{line}} 条操作缺少目标路径', { line });
    }
    if (FROM_REQUIRED_MODES.has(mode) && !fromValue) {
      if (!(meta.pathAlias && pathValue)) {
        return t('第 {{line}} 条操作缺少来源字段', { line });
      }
    }
    if (TO_REQUIRED_MODES.has(mode) && !toValue) {
      if (!(meta.pathAlias && pathValue)) {
        return t('第 {{line}} 条操作缺少目标字段', { line });
      }
    }
    if (meta.from && !fromValue) {
      return t('第 {{line}} 条操作缺少来源字段', { line });
    }
    if (meta.to && !toValue) {
      return t('第 {{line}} 条操作缺少目标字段', { line });
    }
    if (
      VALUE_REQUIRED_MODES.has(mode) &&
      String(op.value_text ?? '').trim() === ''
    ) {
      return t('第 {{line}} 条操作缺少值', { line });
    }
    if (mode === 'return_error') {
      const raw = String(op.value_text ?? '').trim();
      if (!raw) {
        return t('第 {{line}} 条操作缺少值', { line });
      }
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          if (!String(parsed.message || '').trim()) {
            return t('第 {{line}} 条 return_error 需要 message 字段', { line });
          }
        }
      } catch (error) {
        // plain string value is allowed
      }
    }

    if (mode === 'prune_objects') {
      const raw = String(op.value_text ?? '').trim();
      if (!raw) {
        return t('第 {{line}} 条 prune_objects 缺少条件', { line });
      }
      try {
        const parsed = JSON.parse(raw);
        if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
          const hasType =
            parsed.type !== undefined && String(parsed.type).trim() !== '';
          const hasWhere =
            parsed.where &&
            typeof parsed.where === 'object' &&
            !Array.isArray(parsed.where) &&
            Object.keys(parsed.where).length > 0;
          const hasConditionsArray =
            Array.isArray(parsed.conditions) && parsed.conditions.length > 0;
          const hasConditionsObject =
            parsed.conditions &&
            typeof parsed.conditions === 'object' &&
            !Array.isArray(parsed.conditions) &&
            Object.keys(parsed.conditions).length > 0;
          if (
            !hasType &&
            !hasWhere &&
            !hasConditionsArray &&
            !hasConditionsObject
          ) {
            return t('第 {{line}} 条 prune_objects 需要至少一个匹配条件', {
              line,
            });
          }
        }
      } catch (error) {
        // non-JSON string is treated as type string
      }
    }

    if (mode === 'pass_headers') {
      const raw = String(op.value_text ?? '').trim();
      if (!raw) {
        return t('第 {{line}} 条请求头透传缺少请求头名称', { line });
      }
      const parsed = parseLooseValue(raw);
      const headers = parsePassHeaderNames(parsed);
      if (headers.length === 0) {
        return t('第 {{line}} 条请求头透传格式无效', { line });
      }
    }
  }
  return '';
};
