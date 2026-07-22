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

export const MODEL_MAPPING_EXAMPLE = {
  'gpt-3.5-turbo': 'gpt-3.5-turbo-0125',
};

export const STATUS_CODE_MAPPING_EXAMPLE = {
  400: '500',
};

export const REGION_EXAMPLE = {
  default: 'global',
  'gemini-1.5-pro-002': 'europe-west2',
  'gemini-1.5-flash-002': 'europe-west2',
  'claude-3-5-sonnet-20240620': 'europe-west1',
};
export const UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT = 8;
export const ADVANCED_SETTINGS_EXPANDED_KEY =
  'channel-advanced-settings-expanded';

export const PARAM_OVERRIDE_LEGACY_TEMPLATE = {
  temperature: 0,
};

export const PARAM_OVERRIDE_OPERATIONS_TEMPLATE = {
  operations: [
    {
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

export const DEPRECATED_DOUBAO_CODING_PLAN_BASE_URL = 'doubao-coding-plan';

// 支持并且已适配通过接口获取模型列表的渠道类型
export const MODEL_FETCHABLE_TYPES = new Set([
  1, 4, 14, 34, 17, 26, 27, 24, 47, 25, 20, 23, 31, 40, 42, 48, 43,
]);

export function type2secretPrompt(type) {
  // inputs.type === 15 ? '按照如下格式输入：APIKey|SecretKey' : (inputs.type === 18 ? '按照如下格式输入：APPID|APISecret|APIKey' : '请输入渠道对应的鉴权密钥')
  switch (type) {
    case 15:
      return '按照如下格式输入：APIKey|SecretKey';
    case 18:
      return '按照如下格式输入：APPID|APISecret|APIKey';
    case 22:
      return '按照如下格式输入：APIKey-AppId，例如：fastgpt-0sp2gtvfdgyi4k30jwlgwf1i-64f335d84283f05518e9e041';
    case 23:
      return '按照如下格式输入：AppId|SecretId|SecretKey';
    case 33:
      return '按照如下格式输入：Ak|Sk|Region';
    case 45:
      return '请输入渠道对应的鉴权密钥, 豆包语音输入：AppId|AccessToken';
    case 50:
      return '按照如下格式输入: AccessKey|SecretKey, 如果上游是New API，则直接输ApiKey';
    case 51:
      return '按照如下格式输入: AccessKey|SecretAccessKey';
    case 57:
      return '请输入 JSON 格式的 OAuth 凭据（必须包含 access_token 和 account_id）';
    default:
      return '请输入渠道对应的鉴权密钥';
  }
}
