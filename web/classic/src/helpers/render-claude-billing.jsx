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
import { getCurrencyConfig } from './render-basics';
import {
  appendPricePart,
  buildBillingPriceText,
  buildBillingText,
  formatBillingDisplayPrice,
  formatRatioValue,
  getEffectiveRatio,
  getGroupRatioText,
  isPriceDisplayMode,
  joinBillingSummary,
  renderBillingArticle,
  renderDisplayAmountFromUsd,
  shouldUseRatioBillingProcess,
} from './render-billing-core';

export function renderClaudeModelPrice(opts) {
  const {
    prompt_tokens: inputTokens = 0,
    completion_tokens: completionTokens = 0,
    model_ratio: modelRatio = 0,
    model_price: modelPrice = -1,
    completion_ratio: _completionRatio,
    group_ratio: _groupRatio,
    user_group_ratio,
    cache_tokens: cacheTokens = 0,
    cache_ratio: cacheRatio = 1.0,
    cache_creation_tokens: cacheCreationTokens = 0,
    cache_creation_ratio: cacheCreationRatio = 1.0,
    cache_creation_tokens_5m: cacheCreationTokens5m = 0,
    cache_creation_ratio_5m: cacheCreationRatio5m = 1.0,
    cache_creation_tokens_1h: cacheCreationTokens1h = 0,
    cache_creation_ratio_1h: cacheCreationRatio1h = 1.0,
    displayMode = 'price',
  } = opts;
  const { ratio: effectiveGroupRatio, label: ratioLabel } = getEffectiveRatio(
    _groupRatio,
    user_group_ratio,
  );
  let groupRatio = effectiveGroupRatio;
  const completionRatio = _completionRatio ?? 0;

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
    const cacheRatioPrice = modelRatio * 2.0 * cacheRatio;
    const cacheCreationRatioPrice = modelRatio * 2.0 * cacheCreationRatio;
    const cacheCreationRatioPrice5m = modelRatio * 2.0 * cacheCreationRatio5m;
    const cacheCreationRatioPrice1h = modelRatio * 2.0 * cacheCreationRatio1h;
    const hasSplitCacheCreation =
      cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0;
    const legacyCacheCreationTokens = hasSplitCacheCreation
      ? 0
      : cacheCreationTokens;
    const effectiveInputTokens =
      inputTokens +
      cacheTokens * cacheRatio +
      legacyCacheCreationTokens * cacheCreationRatio +
      cacheCreationTokens5m * cacheCreationRatio5m +
      cacheCreationTokens1h * cacheCreationRatio1h;
    const price =
      (effectiveInputTokens / 1000000) * inputRatioPrice * groupRatio +
      (completionTokens / 1000000) * completionRatioPrice * groupRatio;
    const inputUnitPrice = inputRatioPrice * rate;
    const completionUnitPrice = completionRatioPrice * rate;
    const cacheUnitPrice = cacheRatioPrice * rate;
    const cacheCreationUnitPrice = cacheCreationRatioPrice * rate;
    const cacheCreationUnitPrice5m = cacheCreationRatioPrice5m * rate;
    const cacheCreationUnitPrice1h = cacheCreationRatioPrice1h * rate;
    const cacheCreationUnitPriceTotal =
      cacheCreationUnitPrice5m + cacheCreationUnitPrice1h;
    const shouldShowCache = cacheTokens > 0;
    const shouldShowLegacyCacheCreation =
      !hasSplitCacheCreation && cacheCreationTokens > 0;
    const shouldShowCacheCreation5m =
      hasSplitCacheCreation && cacheCreationTokens5m > 0;
    const shouldShowCacheCreation1h =
      hasSplitCacheCreation && cacheCreationTokens1h > 0;

    const breakdownSegments = [
      i18next.t('提示 {{input}} tokens / 1M tokens * {{symbol}}{{price}}', {
        input: inputTokens,
        symbol,
        price: inputUnitPrice.toFixed(6),
      }),
    ];

    if (shouldShowCache) {
      breakdownSegments.push(
        i18next.t('缓存 {{tokens}} tokens / 1M tokens * {{symbol}}{{price}}', {
          tokens: cacheTokens,
          symbol,
          price: cacheUnitPrice.toFixed(6),
        }),
      );
    }

    if (shouldShowLegacyCacheCreation) {
      breakdownSegments.push(
        i18next.t(
          '缓存创建 {{tokens}} tokens / 1M tokens * {{symbol}}{{price}}',
          {
            tokens: cacheCreationTokens,
            symbol,
            price: cacheCreationUnitPrice.toFixed(6),
          },
        ),
      );
    }

    if (shouldShowCacheCreation5m) {
      breakdownSegments.push(
        i18next.t(
          '5m缓存创建 {{tokens}} tokens / 1M tokens * {{symbol}}{{price}}',
          {
            tokens: cacheCreationTokens5m,
            symbol,
            price: cacheCreationUnitPrice5m.toFixed(6),
          },
        ),
      );
    }

    if (shouldShowCacheCreation1h) {
      breakdownSegments.push(
        i18next.t(
          '1h缓存创建 {{tokens}} tokens / 1M tokens * {{symbol}}{{price}}',
          {
            tokens: cacheCreationTokens1h,
            symbol,
            price: cacheCreationUnitPrice1h.toFixed(6),
          },
        ),
      );
    }

    breakdownSegments.push(
      i18next.t(
        '补全 {{completion}} tokens / 1M tokens * {{symbol}}{{price}}',
        {
          completion: completionTokens,
          symbol,
          price: completionUnitPrice.toFixed(6),
        },
      ),
    );

    const breakdownText = breakdownSegments.join(' + ');

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
              usdAmount: cacheRatioPrice,
              rate,
            },
          )
        : null,
      !hasSplitCacheCreation && cacheCreationTokens > 0
        ? buildBillingPriceText(
            '缓存创建价格：{{symbol}}{{price}} / 1M tokens',
            {
              symbol,
              usdAmount: cacheCreationRatioPrice,
              rate,
            },
          )
        : null,
      hasSplitCacheCreation && cacheCreationTokens5m > 0
        ? buildBillingPriceText(
            '5m缓存创建价格：{{symbol}}{{price}} / 1M tokens',
            {
              symbol,
              usdAmount: cacheCreationRatioPrice5m,
              rate,
            },
          )
        : null,
      hasSplitCacheCreation && cacheCreationTokens1h > 0
        ? buildBillingPriceText(
            '1h缓存创建价格：{{symbol}}{{price}} / 1M tokens',
            {
              symbol,
              usdAmount: cacheCreationRatioPrice1h,
              rate,
            },
          )
        : null,
      buildBillingText(
        '{{breakdown}} * {{ratioType}} {{ratio}} = {{symbol}}{{total}}',
        {
          breakdown: breakdownText,
          ratioType: ratioLabel,
          ratio: groupRatio,
          symbol,
          total: formatBillingDisplayPrice(price, rate),
        },
      ),
    ]);
  }

  if (modelPrice !== -1) {
    return i18next.t(
      '模型价格：{{symbol}}{{price}} * {{ratioType}}：{{ratio}} = {{symbol}}{{total}}',
      {
        symbol: symbol,
        price: (modelPrice * rate).toFixed(6),
        ratioType: ratioLabel,
        ratio: groupRatio,
        total: (modelPrice * groupRatio * rate).toFixed(6),
      },
    );
  }

  const modelRatioValue = formatRatioValue(modelRatio);
  const completionRatioValue = formatRatioValue(completionRatio);
  const cacheRatioValue = formatRatioValue(cacheRatio);
  const cacheCreationRatioValue = formatRatioValue(cacheCreationRatio);
  const cacheCreationRatio5mValue = formatRatioValue(cacheCreationRatio5m);
  const cacheCreationRatio1hValue = formatRatioValue(cacheCreationRatio1h);

  const inputRatioPrice = modelRatio * 2.0;
  const completionRatioPrice = modelRatio * 2.0 * completionRatioValue;

  const hasSplitCacheCreation =
    cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0;
  const shouldShowCache = cacheTokens > 0;
  const shouldShowLegacyCacheCreation =
    !hasSplitCacheCreation && cacheCreationTokens > 0;
  const shouldShowCacheCreation5m =
    hasSplitCacheCreation && cacheCreationTokens5m > 0;
  const shouldShowCacheCreation1h =
    hasSplitCacheCreation && cacheCreationTokens1h > 0;

  const legacyCacheCreationTokens = hasSplitCacheCreation
    ? 0
    : cacheCreationTokens;
  const effectiveInputTokens =
    inputTokens +
    cacheTokens * cacheRatioValue +
    legacyCacheCreationTokens * cacheCreationRatioValue +
    cacheCreationTokens5m * cacheCreationRatio5mValue +
    cacheCreationTokens1h * cacheCreationRatio1hValue;

  const totalAmount =
    (effectiveInputTokens / 1000000) * inputRatioPrice * groupRatio +
    (completionTokens / 1000000) * completionRatioPrice * groupRatio;

  return renderBillingArticle([
    buildBillingText(
      '模型倍率 {{modelRatio}}，输出倍率 {{completionRatio}}，缓存倍率 {{cacheRatio}}，{{ratioType}} {{ratio}}',
      {
        modelRatio: modelRatioValue,
        completionRatio: completionRatioValue,
        cacheRatio: cacheRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
      },
    ),
    hasSplitCacheCreation
      ? buildBillingText(
          '缓存创建倍率 5m {{cacheCreationRatio5m}} / 1h {{cacheCreationRatio1h}}',
          {
            cacheCreationRatio5m: cacheCreationRatio5mValue,
            cacheCreationRatio1h: cacheCreationRatio1hValue,
          },
        )
      : buildBillingText('缓存创建倍率 {{cacheCreationRatio}}', {
          cacheCreationRatio: cacheCreationRatioValue,
        }),
    buildBillingText(
      '普通输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
      {
        tokens: inputTokens,
        modelRatio: modelRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
        amount: renderDisplayAmountFromUsd(
          (inputTokens / 1000000) * inputRatioPrice * groupRatio,
        ),
      },
    ),
    shouldShowCache
      ? buildBillingText(
          '缓存读取：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 缓存倍率 {{cacheRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
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
    shouldShowLegacyCacheCreation
      ? buildBillingText(
          '缓存创建：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 缓存创建倍率 {{cacheCreationRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: cacheCreationTokens,
            modelRatio: modelRatioValue,
            cacheCreationRatio: cacheCreationRatioValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(
              (cacheCreationTokens / 1000000) *
                inputRatioPrice *
                cacheCreationRatioValue *
                groupRatio,
            ),
          },
        )
      : null,
    shouldShowCacheCreation5m
      ? buildBillingText(
          '5m缓存创建：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 5m缓存创建倍率 {{cacheCreationRatio5m}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: cacheCreationTokens5m,
            modelRatio: modelRatioValue,
            cacheCreationRatio5m: cacheCreationRatio5mValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(
              (cacheCreationTokens5m / 1000000) *
                inputRatioPrice *
                cacheCreationRatio5mValue *
                groupRatio,
            ),
          },
        )
      : null,
    shouldShowCacheCreation1h
      ? buildBillingText(
          '1h缓存创建：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 1h缓存创建倍率 {{cacheCreationRatio1h}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: cacheCreationTokens1h,
            modelRatio: modelRatioValue,
            cacheCreationRatio1h: cacheCreationRatio1hValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(
              (cacheCreationTokens1h / 1000000) *
                inputRatioPrice *
                cacheCreationRatio1hValue *
                groupRatio,
            ),
          },
        )
      : null,
    buildBillingText(
      '补全 {{completion}} tokens * 输出倍率 {{completionRatio}}',
      {
        completion: completionTokens,
        completionRatio: completionRatioValue,
      },
    ),
    buildBillingText(
      '输出：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 输出倍率 {{completionRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
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
    buildBillingText('合计：{{total}}', {
      total: renderDisplayAmountFromUsd(totalAmount),
    }),
  ]);
}

export function renderClaudeLogContent(opts) {
  const {
    model_ratio: modelRatio,
    completion_ratio: completionRatio,
    model_price: modelPrice = -1,
    group_ratio: _groupRatio,
    user_group_ratio,
    cache_ratio: cacheRatio = 1.0,
    cache_creation_ratio: cacheCreationRatio = 1.0,
    cache_creation_tokens_5m: cacheCreationTokens5m = 0,
    cache_creation_ratio_5m: cacheCreationRatio5m = 1.0,
    cache_creation_tokens_1h: cacheCreationTokens1h = 0,
    cache_creation_ratio_1h: cacheCreationRatio1h = 1.0,
    displayMode = 'price',
  } = opts;
  const { ratio: effectiveGroupRatio, label: ratioLabel } = getEffectiveRatio(
    _groupRatio,
    user_group_ratio,
  );
  let groupRatio = effectiveGroupRatio;

  // 获取货币配置
  const { symbol, rate } = getCurrencyConfig();

  if (isPriceDisplayMode(displayMode, modelPrice)) {
    if (modelPrice !== -1) {
      return joinBillingSummary([
        i18next.t('模型价格 {{symbol}}{{price}} / 次', {
          symbol,
          price: (modelPrice * rate).toFixed(6),
        }),
        getGroupRatioText(groupRatio, user_group_ratio),
      ]);
    }

    const parts = [
      i18next.t('输入价格 {{symbol}}{{price}} / 1M tokens', {
        symbol,
        price: (modelRatio * 2.0 * rate).toFixed(6),
      }),
      i18next.t('输出价格 {{symbol}}{{price}} / 1M tokens', {
        symbol,
        price: (modelRatio * 2.0 * completionRatio * rate).toFixed(6),
      }),
      i18next.t('缓存读取价格 {{symbol}}{{price}} / 1M tokens', {
        symbol,
        price: (modelRatio * 2.0 * cacheRatio * rate).toFixed(6),
      }),
    ];
    const hasSplitCacheCreation =
      cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0;
    appendPricePart(
      parts,
      hasSplitCacheCreation && cacheCreationTokens5m > 0,
      '5m缓存创建价格 {{symbol}}{{price}} / 1M tokens',
      {
        symbol,
        price: (modelRatio * 2.0 * cacheCreationRatio5m * rate).toFixed(6),
      },
    );
    appendPricePart(
      parts,
      hasSplitCacheCreation && cacheCreationTokens1h > 0,
      '1h缓存创建价格 {{symbol}}{{price}} / 1M tokens',
      {
        symbol,
        price: (modelRatio * 2.0 * cacheCreationRatio1h * rate).toFixed(6),
      },
    );
    appendPricePart(
      parts,
      !hasSplitCacheCreation,
      '缓存创建价格 {{symbol}}{{price}} / 1M tokens',
      {
        symbol,
        price: (modelRatio * 2.0 * cacheCreationRatio * rate).toFixed(6),
      },
    );
    parts.push(getGroupRatioText(groupRatio, user_group_ratio));
    return joinBillingSummary(parts);
  }

  if (modelPrice !== -1) {
    return i18next.t('模型价格 {{symbol}}{{price}}，{{ratioType}} {{ratio}}', {
      symbol: symbol,
      price: (modelPrice * rate).toFixed(6),
      ratioType: ratioLabel,
      ratio: groupRatio,
    });
  } else {
    const hasSplitCacheCreation =
      cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0;
    const shouldShowCacheCreation5m =
      hasSplitCacheCreation && cacheCreationTokens5m > 0;
    const shouldShowCacheCreation1h =
      hasSplitCacheCreation && cacheCreationTokens1h > 0;

    let cacheCreationPart = null;
    if (hasSplitCacheCreation) {
      if (shouldShowCacheCreation5m && shouldShowCacheCreation1h) {
        cacheCreationPart = i18next.t(
          '缓存创建倍率 5m {{cacheCreationRatio5m}} / 1h {{cacheCreationRatio1h}}',
          {
            cacheCreationRatio5m,
            cacheCreationRatio1h,
          },
        );
      } else if (shouldShowCacheCreation5m) {
        cacheCreationPart = i18next.t(
          '缓存创建倍率 5m {{cacheCreationRatio5m}}',
          {
            cacheCreationRatio5m,
          },
        );
      } else if (shouldShowCacheCreation1h) {
        cacheCreationPart = i18next.t(
          '缓存创建倍率 1h {{cacheCreationRatio1h}}',
          {
            cacheCreationRatio1h,
          },
        );
      }
    }

    if (!cacheCreationPart) {
      cacheCreationPart = i18next.t('缓存创建倍率 {{cacheCreationRatio}}', {
        cacheCreationRatio,
      });
    }

    const parts = [
      i18next.t('模型倍率 {{modelRatio}}', { modelRatio }),
      i18next.t('输出倍率 {{completionRatio}}', { completionRatio }),
      i18next.t('缓存倍率 {{cacheRatio}}', { cacheRatio }),
      cacheCreationPart,
      i18next.t('{{ratioType}} {{ratio}}', {
        ratioType: ratioLabel,
        ratio: groupRatio,
      }),
    ];

    return parts.join('，');
  }
}
