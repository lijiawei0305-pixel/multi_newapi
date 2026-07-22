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

import i18next from 'i18next';
import {
  BILLING_PRICING_VARS,
  BILLING_VAR_KEY_TO_FIELD,
  BILLING_VAR_REGEX,
} from '../constants';
import { getCurrencyConfig, renderQuota } from './render-basics';
import {
  buildBillingPriceText,
  buildBillingText,
  formatBillingDisplayPrice,
  formatCompactDisplayPrice,
  formatRatioValue,
  getEffectiveRatio,
  getGroupRatioText,
  isPriceDisplayMode,
  renderBillingArticle,
  renderDisplayAmountFromUsd,
  renderPriceSimpleCore,
  shouldUseRatioBillingProcess,
} from './render-billing-core';

export function stripExprVersion(exprStr) {
  if (!exprStr) return { version: 1, body: '' };
  const m = exprStr.match(/^v(\d+):([\s\S]*)$/);
  if (m) return { version: Number(m[1]), body: m[2] };
  return { version: 1, body: exprStr };
}

function parseTierBody(bodyStr) {
  const coeffs = {};
  const re = new RegExp(BILLING_VAR_REGEX.source, 'g');
  let m;
  while ((m = re.exec(bodyStr)) !== null) {
    if (!(m[1] in coeffs)) coeffs[m[1]] = Number(m[2]);
  }
  const tier = {};
  for (const [varName, field] of Object.entries(BILLING_VAR_KEY_TO_FIELD)) {
    tier[field] = coeffs[varName] || 0;
  }
  return tier;
}

export function parseTiersFromExpr(exprStr) {
  if (!exprStr) return [];
  try {
    const { body } = stripExprVersion(exprStr);
    const condGroup = `((?:(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*[\\d.eE+]+)(?:\\s*&&\\s*(?:p|c|len)\\s*(?:<|<=|>|>=)\\s*[\\d.eE+]+)*)`;
    const tierRe = new RegExp(
      `(?:${condGroup}\\s*\\?\\s*)?tier\\("([^"]*)",\\s*([^)]+)\\)`,
      'g',
    );
    const tiers = [];
    let m;
    while ((m = tierRe.exec(body)) !== null) {
      const condStr = m[1] || '';
      const conditions = [];
      if (condStr) {
        for (const cp of condStr.split(/\s*&&\s*/)) {
          const cm = cp.trim().match(/^(p|c|len)\s*(<|<=|>|>=)\s*([\d.eE+]+)$/);
          if (cm)
            conditions.push({ var: cm[1], op: cm[2], value: Number(cm[3]) });
        }
      }
      const tier = parseTierBody(m[3]);
      tier.label = m[2];
      tier.conditions = conditions;
      tiers.push(tier);
    }
    return tiers;
  } catch {
    return [];
  }
}

export const decodeFromBase64 = (base64) => {
  if (!base64) return '';

  const binaryString =
    typeof window !== 'undefined'
      ? window.atob(base64)
      : Buffer.from(base64, 'base64').toString('binary');
  const bytes = new Uint8Array(binaryString.length);

  for (let i = 0; i < binaryString.length; i++) {
    bytes[i] = binaryString.charCodeAt(i);
  }

  if (typeof TextDecoder !== 'undefined') {
    return new TextDecoder().decode(bytes);
  }

  return decodeURIComponent(
    Array.prototype.map
      .call(bytes, (byte) => '%' + byte.toString(16).padStart(2, '0'))
      .join(''),
  );
};

export const normalizeLabel = (label) => {
  if (!label) return '';
  return label
    .replace(/<[=＝]?|≤|＜[=＝]?/g, '<')
    .replace(/>[=＝]?|≥|＞[=＝]?/g, '>')
    .replace(/\s+/g, '')
    .toLowerCase();
};

export function renderTieredModelPrice(opts) {
  const {
    prompt_tokens: inputTokens = 0,
    completion_tokens: completionTokens = 0,
    expr_b64: exprB64,
    matched_tier: matchedTier,
    group_ratio: groupRatio,
    cache_tokens: cacheTokens = 0,
    cache_creation_tokens: cacheCreationTokens = 0,
    cache_creation_tokens_5m: cacheCreationTokens5m = 0,
    cache_creation_tokens_1h: cacheCreationTokens1h = 0,
  } = opts;
  let exprStr = '';
  try {
    exprStr = decodeFromBase64(exprB64);
  } catch {
    /* ignore */
  }
  const tiers = parseTiersFromExpr(exprStr);
  if (tiers.length === 0) {
    return i18next.t('阶梯计费（表达式解析失败）');
  }

  const tier = tiers.find((t) => {
    const l1 = normalizeLabel(t.label);
    const l2 = normalizeLabel(matchedTier);
    return l1 === l2 && l1 !== '';
  });

  if (!tier) {
    return i18next.t('阶梯计费（未匹配到对应阶梯）');
  }
  const { symbol, rate } = getCurrencyConfig();
  const gr = groupRatio || 1;

  const hasAnyCacheTokens =
    cacheTokens > 0 ||
    cacheCreationTokens > 0 ||
    cacheCreationTokens5m > 0 ||
    cacheCreationTokens1h > 0;

  const priceLines = BILLING_PRICING_VARS.filter(
    (v) => v.group !== 'cache' || hasAnyCacheTokens,
  ).map((v) => [v.field, v.label]);

  const lines = [
    buildBillingText('命中档位：{{tier}}', { tier: matchedTier || tier.label }),
    ...priceLines
      .filter(([field]) => tier[field] > 0)
      .map(([field, label]) =>
        buildBillingPriceText(`${label}：{{symbol}}{{price}} / 1M tokens`, {
          symbol,
          usdAmount: tier[field],
          rate,
        }),
      ),
  ];

  return renderBillingArticle(lines);
}

export function renderTieredModelPriceSimple(opts) {
  const {
    expr_b64: exprB64,
    matched_tier: matchedTier,
    group_ratio: groupRatio,
    user_group_ratio,
    cache_tokens: cacheTokens = 0,
    cache_creation_tokens_5m: cacheCreationTokens5m = 0,
    cache_creation_tokens_1h: cacheCreationTokens1h = 0,
    cache_creation_tokens: cacheCreationTokens = 0,
    displayMode = 'price',
    outputMode = 'segments',
  } = opts;
  let exprStr = '';
  try {
    exprStr = decodeFromBase64(exprB64);
  } catch {
    /* ignore */
  }
  const tiers = parseTiersFromExpr(exprStr);
  const tier = tiers.find((t) => {
    const l1 = normalizeLabel(t.label);
    const l2 = normalizeLabel(matchedTier);
    return l1 === l2 && l1 !== '';
  });

  if (outputMode === 'segments') {
    const segments = [
      {
        tone: 'primary',
        text: getGroupRatioText(groupRatio, user_group_ratio),
      },
    ];

    if (!tier) {
      segments.push({
        tone: 'secondary',
        text:
          tiers.length === 0
            ? i18next.t('阶梯计费（表达式解析失败）')
            : i18next.t('阶梯计费（未匹配到对应阶梯）'),
      });
    } else if (isPriceDisplayMode(displayMode)) {
      const hasAnyCacheTokens =
        cacheTokens > 0 ||
        cacheCreationTokens > 0 ||
        cacheCreationTokens5m > 0 ||
        cacheCreationTokens1h > 0;
      const priceSegments = BILLING_PRICING_VARS.filter(
        (v) => v.group !== 'cache' || hasAnyCacheTokens,
      ).map((v) => [v.field, v.shortLabel]);
      for (const [field, label] of priceSegments) {
        if (tier[field] > 0) {
          segments.push({
            tone: 'secondary',
            text: i18next.t('{{label}} {{price}} / 1M tokens', {
              label: i18next.t(label),
              price: formatCompactDisplayPrice(tier[field]),
            }),
          });
        }
      }
    }

    return segments;
  }

  return [];
}

export function renderModelPriceSimple(opts) {
  const {
    model_ratio: modelRatio,
    model_price: modelPrice = -1,
    group_ratio: groupRatio,
    user_group_ratio,
    cache_tokens: cacheTokens = 0,
    cache_ratio: cacheRatio = 1.0,
    cache_creation_tokens: cacheCreationTokens = 0,
    cache_creation_ratio: cacheCreationRatio = 1.0,
    cache_creation_tokens_5m: cacheCreationTokens5m = 0,
    cache_creation_ratio_5m: cacheCreationRatio5m = 1.0,
    cache_creation_tokens_1h: cacheCreationTokens1h = 0,
    cache_creation_ratio_1h: cacheCreationRatio1h = 1.0,
    image = false,
    image_ratio: imageRatio = 1.0,
    is_system_prompt_overwritten: isSystemPromptOverride = false,
    provider = 'openai',
    displayMode = 'price',
    outputMode = 'text',
  } = opts;
  return renderPriceSimpleCore({
    modelRatio,
    modelPrice,
    groupRatio,
    user_group_ratio,
    cacheTokens,
    cacheRatio,
    cacheCreationTokens,
    cacheCreationRatio,
    cacheCreationTokens5m,
    cacheCreationRatio5m,
    cacheCreationTokens1h,
    cacheCreationRatio1h,
    image,
    imageRatio,
    isSystemPromptOverride,
    displayMode,
    outputMode,
  });
}

export function renderAudioModelPrice(opts) {
  const {
    prompt_tokens: inputTokens = 0,
    completion_tokens: completionTokens = 0,
    model_ratio: modelRatio = 0,
    model_price: modelPrice = -1,
    completion_ratio: _completionRatio,
    audio_input: audioInputTokens = 0,
    audio_output: audioCompletionTokens = 0,
    audio_ratio: _audioRatio,
    audio_completion_ratio: _audioCompletionRatio,
    group_ratio: _groupRatio,
    user_group_ratio,
    cache_tokens: cacheTokens = 0,
    cache_ratio: cacheRatio = 1.0,
    displayMode = 'price',
  } = opts;
  const { ratio: effectiveGroupRatio, label: ratioLabel } = getEffectiveRatio(
    _groupRatio,
    user_group_ratio,
  );
  let groupRatio = effectiveGroupRatio;
  const completionRatio = _completionRatio ?? 0;
  const audioRatio = parseFloat(_audioRatio ?? 0).toFixed(6);
  const audioCompletionRatio = _audioCompletionRatio ?? 0;

  // 获取货币配置
  const { symbol, rate } = getCurrencyConfig();

  if (!shouldUseRatioBillingProcess(modelPrice)) {
    if (modelPrice !== -1) {
      return renderBillingArticle([
        buildBillingPriceText('模型价格：{{symbol}}{{price}} / 次', {
          symbol,
          usdAmount: modelPrice,
          rate,
        }),
        buildBillingPriceText(
          '模型价格 {{symbol}}{{price}} / 次 * {{ratioType}} {{ratio}} = {{symbol}}{{total}}',
          {
            symbol,
            usdAmount: modelPrice,
            rate,
            ratioType: ratioLabel,
            ratio: groupRatio,
            total: formatBillingDisplayPrice(modelPrice * groupRatio, rate),
          },
        ),
      ]);
    }

    const inputRatioPrice = modelRatio * 2.0;
    const completionRatioPrice = modelRatio * 2.0 * completionRatio;
    const textPrice =
      ((inputTokens - cacheTokens + cacheTokens * cacheRatio) / 1000000) *
        inputRatioPrice *
        groupRatio +
      (completionTokens / 1000000) * completionRatioPrice * groupRatio;
    const audioPrice =
      (audioInputTokens / 1000000) * inputRatioPrice * audioRatio * groupRatio +
      (audioCompletionTokens / 1000000) *
        inputRatioPrice *
        audioRatio *
        audioCompletionRatio *
        groupRatio;
    const totalPrice = textPrice + audioPrice;

    return renderBillingArticle([
      buildBillingPriceText('输入价格：{{symbol}}{{price}} / 1M tokens', {
        symbol,
        usdAmount: inputRatioPrice,
        rate,
      }),
      buildBillingPriceText('输出价格：{{symbol}}{{price}} / 1M tokens', {
        symbol,
        usdAmount: completionRatioPrice,
        rate,
      }),
      cacheTokens > 0
        ? buildBillingPriceText(
            '缓存读取价格：{{symbol}}{{price}} / 1M tokens',
            {
              symbol,
              usdAmount: inputRatioPrice * cacheRatio,
              rate,
            },
          )
        : null,
      buildBillingPriceText('音频输入价格：{{symbol}}{{price}} / 1M tokens', {
        symbol,
        usdAmount: inputRatioPrice * audioRatio,
        rate,
      }),
      buildBillingPriceText('音频补全价格：{{symbol}}{{price}} / 1M tokens', {
        symbol,
        usdAmount: inputRatioPrice * audioRatio * audioCompletionRatio,
        rate,
      }),
      buildBillingText(
        '文字提示 {{input}} tokens / 1M tokens * {{symbol}}{{textInputPrice}} + 文字补全 {{completion}} tokens / 1M tokens * {{symbol}}{{textCompPrice}} + 音频提示 {{audioInput}} tokens / 1M tokens * {{symbol}}{{audioInputPrice}} + 音频补全 {{audioCompletion}} tokens / 1M tokens * {{symbol}}{{audioCompPrice}} * {{ratioType}} {{ratio}} = {{symbol}}{{total}}',
        {
          input: inputTokens,
          completion: completionTokens,
          audioInput: audioInputTokens,
          audioCompletion: audioCompletionTokens,
          textInputPrice: formatBillingDisplayPrice(inputRatioPrice, rate),
          textCompPrice: formatBillingDisplayPrice(completionRatioPrice, rate),
          audioInputPrice: formatBillingDisplayPrice(
            audioRatio * inputRatioPrice,
            rate,
          ),
          audioCompPrice: formatBillingDisplayPrice(
            audioRatio * audioCompletionRatio * inputRatioPrice,
            rate,
          ),
          ratioType: ratioLabel,
          ratio: groupRatio,
          symbol,
          total: formatBillingDisplayPrice(totalPrice, rate),
        },
      ),
    ]);
  }

  // 1 ratio = $0.002 / 1K tokens
  if (modelPrice !== -1) {
    return i18next.t(
      '模型价格：{{symbol}}{{price}} * {{ratioType}}：{{ratio}} = {{symbol}}{{total}}',
      {
        symbol: symbol,
        price: (modelPrice * rate).toFixed(6),
        ratio: groupRatio,
        total: (modelPrice * groupRatio * rate).toFixed(6),
        ratioType: ratioLabel,
      },
    );
  }

  const modelRatioValue = formatRatioValue(modelRatio);
  const completionRatioValue = formatRatioValue(completionRatio);
  const cacheRatioValue = formatRatioValue(cacheRatio);
  const audioRatioValue = formatRatioValue(audioRatio);
  const audioCompletionRatioValue = formatRatioValue(audioCompletionRatio);

  const inputRatioPrice = modelRatio * 2.0;
  const completionRatioPrice = modelRatio * 2.0 * completionRatioValue;

  const effectiveInputTokens =
    inputTokens - cacheTokens + cacheTokens * cacheRatioValue;

  const textPrice =
    (effectiveInputTokens / 1000000) * inputRatioPrice * groupRatio +
    (completionTokens / 1000000) * completionRatioPrice * groupRatio;
  const audioPrice =
    (audioInputTokens / 1000000) *
      inputRatioPrice *
      audioRatioValue *
      groupRatio +
    (audioCompletionTokens / 1000000) *
      inputRatioPrice *
      audioRatioValue *
      audioCompletionRatioValue *
      groupRatio;
  const totalPrice = textPrice + audioPrice;

  return renderBillingArticle([
    buildBillingText(
      '模型倍率 {{modelRatio}}，补全倍率 {{completionRatio}}，音频倍率 {{audioRatio}}，音频补全倍率 {{audioCompletionRatio}}，{{cachePart}}{{ratioType}} {{ratio}}',
      {
        modelRatio: modelRatioValue,
        completionRatio: completionRatioValue,
        audioRatio: audioRatioValue,
        audioCompletionRatio: audioCompletionRatioValue,
        cachePart:
          cacheTokens > 0
            ? `${i18next.t('缓存倍率')} ${cacheRatioValue}，`
            : '',
        ratioType: ratioLabel,
        ratio: groupRatio,
      },
    ),
    buildBillingText(
      '普通输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
      {
        tokens: Math.max(inputTokens - cacheTokens, 0),
        modelRatio: modelRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
        amount: renderDisplayAmountFromUsd(
          (Math.max(inputTokens - cacheTokens, 0) / 1000000) *
            inputRatioPrice *
            groupRatio,
        ),
      },
    ),
    cacheTokens > 0
      ? buildBillingText(
          '缓存输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 缓存倍率 {{cacheRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: cacheTokens,
            modelRatio: modelRatioValue,
            cacheRatio: cacheRatioValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(
              (cacheTokens / 1000000) *
                inputRatioPrice *
                cacheRatioValue *
                groupRatio,
            ),
          },
        )
      : null,
    buildBillingText(
      '文字输出：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 补全倍率 {{completionRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
      {
        tokens: completionTokens,
        modelRatio: modelRatioValue,
        completionRatio: completionRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
        amount: renderDisplayAmountFromUsd(
          (completionTokens / 1000000) *
            inputRatioPrice *
            completionRatioValue *
            groupRatio,
        ),
      },
    ),
    buildBillingText(
      '音频输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 音频倍率 {{audioRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
      {
        tokens: audioInputTokens,
        modelRatio: modelRatioValue,
        audioRatio: audioRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
        amount: renderDisplayAmountFromUsd(
          (audioInputTokens / 1000000) *
            inputRatioPrice *
            audioRatioValue *
            groupRatio,
        ),
      },
    ),
    buildBillingText(
      '音频输出：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 音频倍率 {{audioRatio}} * 音频补全倍率 {{audioCompletionRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
      {
        tokens: audioCompletionTokens,
        modelRatio: modelRatioValue,
        audioRatio: audioRatioValue,
        audioCompletionRatio: audioCompletionRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
        amount: renderDisplayAmountFromUsd(
          (audioCompletionTokens / 1000000) *
            inputRatioPrice *
            audioRatioValue *
            audioCompletionRatioValue *
            groupRatio,
        ),
      },
    ),
    buildBillingText(
      '合计：文字部分 {{textTotal}} + 音频部分 {{audioTotal}} = {{total}}',
      {
        textTotal: renderDisplayAmountFromUsd(textPrice),
        audioTotal: renderDisplayAmountFromUsd(audioPrice),
        total: renderDisplayAmountFromUsd(totalPrice),
      },
    ),
  ]);
}

export function renderQuotaWithPrompt(quota, digits) {
  const quotaDisplayType = localStorage.getItem('quota_display_type') || 'USD';
  if (quotaDisplayType !== 'TOKENS') {
    return i18next.t('等价金额：') + renderQuota(quota, digits);
  }
  return '';
}
