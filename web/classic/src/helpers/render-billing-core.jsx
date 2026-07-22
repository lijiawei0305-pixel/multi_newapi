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
import { getCurrencyConfig, renderQuotaWithAmount } from './render-basics';

export function isValidGroupRatio(ratio) {
  return Number.isFinite(ratio) && ratio !== -1;
}

/**
 * Helper function to get effective ratio and label
 * @param {number} groupRatio - The default group ratio
 * @param {number} user_group_ratio - The user-specific group ratio
 * @returns {Object} - Object containing { ratio, label, useUserGroupRatio }
 */
export function getEffectiveRatio(groupRatio, user_group_ratio) {
  const useUserGroupRatio = isValidGroupRatio(user_group_ratio);
  const ratioLabel = useUserGroupRatio
    ? i18next.t('专属倍率')
    : i18next.t('分组倍率');
  const effectiveRatio = useUserGroupRatio ? user_group_ratio : groupRatio;

  return {
    ratio: effectiveRatio,
    label: ratioLabel,
    useUserGroupRatio: useUserGroupRatio,
  };
}

export function getQuotaDisplayType() {
  return localStorage.getItem('quota_display_type') || 'USD';
}

export function resolveBillingDisplayMode(displayMode, modelPrice = -1) {
  if (modelPrice !== -1) {
    return 'price';
  }
  if (getQuotaDisplayType() === 'TOKENS') {
    return 'ratio';
  }
  return displayMode === 'ratio' ? 'ratio' : 'price';
}

export function isPriceDisplayMode(displayMode, modelPrice = -1) {
  return resolveBillingDisplayMode(displayMode, modelPrice) === 'price';
}

export function shouldUseRatioBillingProcess(modelPrice = -1) {
  return modelPrice === -1 && getQuotaDisplayType() === 'TOKENS';
}

export function formatCompactDisplayPrice(usdAmount, digits = 6) {
  const { symbol, rate } = getCurrencyConfig();
  const amount = Number((usdAmount * rate).toFixed(digits));
  return `${symbol}${amount}`;
}

export function appendPricePart(parts, condition, key, vars) {
  if (!condition) {
    return;
  }
  parts.push(i18next.t(key, vars));
}

export function joinBillingSummary(parts) {
  return parts.filter(Boolean).join('，');
}

export function getGroupRatioText(groupRatio, user_group_ratio) {
  const { ratio, label } = getEffectiveRatio(groupRatio, user_group_ratio);
  return i18next.t('{{ratioType}} {{ratio}}x', {
    ratioType: label,
    ratio,
  });
}

export function formatRatioValue(value, digits = 6) {
  const num = Number(value);
  if (!Number.isFinite(num)) {
    return 0;
  }
  return Number(num.toFixed(digits));
}

export function renderDisplayAmountFromUsd(usdAmount, digits = 6) {
  return renderQuotaWithAmount(Number(Number(usdAmount || 0).toFixed(digits)));
}

export function formatBillingDisplayPrice(usdAmount, rate, digits = 6) {
  return (usdAmount * rate).toFixed(digits);
}

export function buildBillingText(key, vars) {
  return i18next.t(key, vars);
}

export function buildBillingPriceText(
  key,
  { symbol, usdAmount, rate, amountKey = 'price', digits = 6, ...vars },
) {
  return buildBillingText(key, {
    symbol,
    [amountKey]: formatBillingDisplayPrice(usdAmount, rate, digits),
    ...vars,
  });
}

export function renderBillingArticle(lines, { showReferenceNote = true } = {}) {
  const articleLines = lines.filter(Boolean);

  if (showReferenceNote) {
    articleLines.push(buildBillingText('仅供参考，以实际扣费为准'));
  }

  return (
    <article>
      {articleLines.map((line, index) => (
        <p key={index}>{line}</p>
      ))}
    </article>
  );
}

// Shared core for simple price rendering (used by OpenAI-like and Claude-like variants)
export function renderPriceSimpleCore({
  modelRatio,
  modelPrice = -1,
  groupRatio,
  user_group_ratio,
  cacheTokens = 0,
  cacheRatio = 1.0,
  cacheCreationTokens = 0,
  cacheCreationRatio = 1.0,
  cacheCreationTokens5m = 0,
  cacheCreationRatio5m = 1.0,
  cacheCreationTokens1h = 0,
  cacheCreationRatio1h = 1.0,
  image = false,
  imageRatio = 1.0,
  isSystemPromptOverride = false,
  displayMode = 'price',
  outputMode = 'text',
}) {
  const { ratio: effectiveGroupRatio, label: ratioLabel } = getEffectiveRatio(
    groupRatio,
    user_group_ratio,
  );
  const finalGroupRatio = effectiveGroupRatio;

  const { symbol, rate } = getCurrencyConfig();
  const hasSplitCacheCreation =
    cacheCreationTokens5m > 0 || cacheCreationTokens1h > 0;

  const shouldShowLegacyCacheCreation =
    !hasSplitCacheCreation && cacheCreationTokens !== 0;

  const shouldShowCache = cacheTokens !== 0;
  const shouldShowCacheCreation5m =
    hasSplitCacheCreation && cacheCreationTokens5m > 0;
  const shouldShowCacheCreation1h =
    hasSplitCacheCreation && cacheCreationTokens1h > 0;

  if (outputMode === 'segments') {
    const segments = [
      {
        tone: 'primary',
        text: getGroupRatioText(groupRatio, user_group_ratio),
      },
    ];

    if (modelPrice !== -1) {
      segments.push({
        tone: 'secondary',
        text: isPriceDisplayMode(displayMode, modelPrice)
          ? i18next.t('模型价格 {{price}}', {
              price: formatCompactDisplayPrice(modelPrice),
            })
          : i18next.t('按次'),
      });
    } else if (isPriceDisplayMode(displayMode, modelPrice)) {
      segments.push({
        tone: 'secondary',
        text: i18next.t('输入 {{price}} / 1M tokens', {
          price: formatCompactDisplayPrice(modelRatio * 2.0),
        }),
      });

      if (shouldShowCache) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('缓存读 {{price}} / 1M tokens', {
            price: formatCompactDisplayPrice(modelRatio * 2.0 * cacheRatio),
          }),
        });
      }

      if (hasSplitCacheCreation && shouldShowCacheCreation5m) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('5m缓存创建 {{price}} / 1M tokens', {
            price: formatCompactDisplayPrice(
              modelRatio * 2.0 * cacheCreationRatio5m,
            ),
          }),
        });
      }
      if (hasSplitCacheCreation && shouldShowCacheCreation1h) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('1h缓存创建 {{price}} / 1M tokens', {
            price: formatCompactDisplayPrice(
              modelRatio * 2.0 * cacheCreationRatio1h,
            ),
          }),
        });
      }
      if (!hasSplitCacheCreation && shouldShowLegacyCacheCreation) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('缓存创建 {{price}} / 1M tokens', {
            price: formatCompactDisplayPrice(
              modelRatio * 2.0 * cacheCreationRatio,
            ),
          }),
        });
      }

      if (image) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('图片输入 {{price}} / 1M tokens', {
            price: formatCompactDisplayPrice(modelRatio * 2.0 * imageRatio),
          }),
        });
      }
    } else {
      segments.push({
        tone: 'secondary',
        text: i18next.t('模型: {{ratio}}', {
          ratio: modelRatio,
        }),
      });

      if (shouldShowCache) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('缓存: {{cacheRatio}}', {
            cacheRatio: cacheRatio,
          }),
        });
      }

      if (hasSplitCacheCreation) {
        if (shouldShowCacheCreation5m && shouldShowCacheCreation1h) {
          segments.push({
            tone: 'secondary',
            text: i18next.t(
              '缓存创建: 5m {{cacheCreationRatio5m}} / 1h {{cacheCreationRatio1h}}',
              {
                cacheCreationRatio5m: cacheCreationRatio5m,
                cacheCreationRatio1h: cacheCreationRatio1h,
              },
            ),
          });
        } else if (shouldShowCacheCreation5m) {
          segments.push({
            tone: 'secondary',
            text: i18next.t('缓存创建: 5m {{cacheCreationRatio5m}}', {
              cacheCreationRatio5m: cacheCreationRatio5m,
            }),
          });
        } else if (shouldShowCacheCreation1h) {
          segments.push({
            tone: 'secondary',
            text: i18next.t('缓存创建: 1h {{cacheCreationRatio1h}}', {
              cacheCreationRatio1h: cacheCreationRatio1h,
            }),
          });
        }
      } else if (shouldShowLegacyCacheCreation) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('缓存创建: {{cacheCreationRatio}}', {
            cacheCreationRatio: cacheCreationRatio,
          }),
        });
      }

      if (image) {
        segments.push({
          tone: 'secondary',
          text: i18next.t('图片输入: {{imageRatio}}', {
            imageRatio: imageRatio,
          }),
        });
      }
    }

    if (isSystemPromptOverride) {
      segments.push({
        tone: 'primary',
        text: i18next.t('系统提示覆盖'),
      });
    }

    return segments;
  }

  if (modelPrice !== -1) {
    if (isPriceDisplayMode(displayMode, modelPrice)) {
      return joinBillingSummary([
        i18next.t('模型价格：{{symbol}}{{price}}', {
          symbol: symbol,
          price: (modelPrice * rate).toFixed(6),
        }),
        getGroupRatioText(groupRatio, user_group_ratio),
      ]);
    }
    const displayPrice = (modelPrice * rate).toFixed(6);
    return i18next.t('价格：{{symbol}}{{price}} * {{ratioType}}：{{ratio}}', {
      symbol: symbol,
      price: displayPrice,
      ratioType: ratioLabel,
      ratio: finalGroupRatio,
    });
  }

  if (isPriceDisplayMode(displayMode, modelPrice)) {
    const parts = [];
    if (modelPrice !== -1) {
      parts.push(
        i18next.t('模型价格 {{price}}', {
          price: formatCompactDisplayPrice(modelPrice),
        }),
      );
      parts.push(getGroupRatioText(groupRatio, user_group_ratio));
      return joinBillingSummary(parts);
    }

    parts.push(
      i18next.t('输入 {{price}} / 1M tokens', {
        price: formatCompactDisplayPrice(modelRatio * 2.0),
      }),
    );

    if (shouldShowCache) {
      parts.push(
        i18next.t('缓存读 {{price}} / 1M tokens', {
          price: formatCompactDisplayPrice(modelRatio * 2.0 * cacheRatio),
        }),
      );
    }

    if (hasSplitCacheCreation && shouldShowCacheCreation5m) {
      parts.push(
        i18next.t('5m缓存创建 {{price}} / 1M tokens', {
          price: formatCompactDisplayPrice(
            modelRatio * 2.0 * cacheCreationRatio5m,
          ),
        }),
      );
    }
    if (hasSplitCacheCreation && shouldShowCacheCreation1h) {
      parts.push(
        i18next.t('1h缓存创建 {{price}} / 1M tokens', {
          price: formatCompactDisplayPrice(
            modelRatio * 2.0 * cacheCreationRatio1h,
          ),
        }),
      );
    }
    if (!hasSplitCacheCreation && shouldShowLegacyCacheCreation) {
      parts.push(
        i18next.t('缓存创建 {{price}} / 1M tokens', {
          price: formatCompactDisplayPrice(
            modelRatio * 2.0 * cacheCreationRatio,
          ),
        }),
      );
    }

    if (image) {
      parts.push(
        i18next.t('图片输入 {{price}} / 1M tokens', {
          price: formatCompactDisplayPrice(modelRatio * 2.0 * imageRatio),
        }),
      );
    }

    parts.push(getGroupRatioText(groupRatio, user_group_ratio));

    let result = joinBillingSummary(parts);
    if (isSystemPromptOverride) {
      result += '\n\r' + i18next.t('系统提示覆盖');
    }
    return result;
  }

  const parts = [];
  // base: model ratio
  parts.push(i18next.t('模型: {{ratio}}'));

  // cache part (label differs when with image)
  if (shouldShowCache) {
    parts.push(i18next.t('缓存: {{cacheRatio}}'));
  }

  if (hasSplitCacheCreation) {
    if (shouldShowCacheCreation5m && shouldShowCacheCreation1h) {
      parts.push(
        i18next.t(
          '缓存创建: 5m {{cacheCreationRatio5m}} / 1h {{cacheCreationRatio1h}}',
        ),
      );
    } else if (shouldShowCacheCreation5m) {
      parts.push(i18next.t('缓存创建: 5m {{cacheCreationRatio5m}}'));
    } else if (shouldShowCacheCreation1h) {
      parts.push(i18next.t('缓存创建: 1h {{cacheCreationRatio1h}}'));
    }
  } else if (shouldShowLegacyCacheCreation) {
    parts.push(i18next.t('缓存创建: {{cacheCreationRatio}}'));
  }

  // image part
  if (image) {
    parts.push(i18next.t('图片输入: {{imageRatio}}'));
  }

  parts.push(`{{ratioType}}: {{groupRatio}}`);

  let result = i18next.t(parts.join(' * '), {
    ratio: modelRatio,
    ratioType: ratioLabel,
    groupRatio: finalGroupRatio,
    cacheRatio: cacheRatio,
    cacheCreationRatio: cacheCreationRatio,
    cacheCreationRatio5m: cacheCreationRatio5m,
    cacheCreationRatio1h: cacheCreationRatio1h,
    imageRatio: imageRatio,
  });

  if (isSystemPromptOverride) {
    result += '\n\r' + i18next.t('系统提示覆盖');
  }

  return result;
}
