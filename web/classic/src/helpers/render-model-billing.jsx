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

export function renderTaskBillingProcess(other, content) {
  if (other?.task_id != null) {
    return renderBillingArticle([content].filter(Boolean), {
      showReferenceNote: false,
    });
  }
  return renderBillingArticle([
    buildBillingText('任务预扣费（将在任务完成后按实际token重算）'),
  ]);
}

export function renderModelPrice(opts) {
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
    image = false,
    image_ratio: imageRatio = 1.0,
    image_output: imageOutputTokens = 0,
    web_search: webSearch = false,
    web_search_call_count: webSearchCallCount = 0,
    web_search_price: webSearchPrice = 0,
    file_search: fileSearch = false,
    file_search_call_count: fileSearchCallCount = 0,
    file_search_price: fileSearchPrice = 0,
    audio_input_seperate_price: audioInputSeperatePrice = false,
    audio_input_token_count: audioInputTokens = 0,
    audio_input_price: audioInputPrice = 0,
    image_generation_call: imageGenerationCall = false,
    image_generation_call_price: imageGenerationCallPrice = 0,
    displayMode = 'price',
  } = opts;
  const { ratio: effectiveGroupRatio, label: ratioLabel } = getEffectiveRatio(
    _groupRatio,
    user_group_ratio,
  );
  let groupRatio = effectiveGroupRatio;
  const completionRatio = _completionRatio ?? 0;

  const { symbol, rate } = getCurrencyConfig();

  if (!shouldUseRatioBillingProcess(modelPrice)) {
    if (modelPrice !== -1) {
      return renderBillingArticle([
        buildBillingPriceText('按次：{{symbol}}{{price}}', {
          symbol,
          usdAmount: modelPrice,
          rate,
        }),
        buildBillingPriceText(
          '按次 {{symbol}}{{price}} * {{ratioType}} {{ratio}} = {{symbol}}{{total}}',
          {
            symbol,
            usdAmount: modelPrice,
            rate,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amountKey: 'price',
            total: formatBillingDisplayPrice(modelPrice * groupRatio, rate),
          },
        ),
      ]);
    }

    const inputRatioPrice = modelRatio * 2.0;
    const completionRatioPrice = modelRatio * 2.0 * completionRatio;
    const cacheRatioPrice = modelRatio * 2.0 * cacheRatio;
    const imageRatioPrice = modelRatio * 2.0 * imageRatio;
    let effectiveInputTokens =
      inputTokens - cacheTokens + cacheTokens * cacheRatio;
    if (image && imageOutputTokens > 0) {
      effectiveInputTokens =
        inputTokens - imageOutputTokens + imageOutputTokens * imageRatio;
    }
    if (audioInputTokens > 0) {
      effectiveInputTokens -= audioInputTokens;
    }
    const price =
      (effectiveInputTokens / 1000000) * inputRatioPrice * groupRatio +
      (audioInputTokens / 1000000) * audioInputPrice * groupRatio +
      (completionTokens / 1000000) * completionRatioPrice * groupRatio +
      (webSearchCallCount / 1000) * webSearchPrice * groupRatio +
      (fileSearchCallCount / 1000) * fileSearchPrice * groupRatio +
      imageGenerationCallPrice * groupRatio;

    let inputDesc = '';
    if (image && imageOutputTokens > 0) {
      inputDesc = buildBillingPriceText(
        '(输入 {{nonImageInput}} tokens + 图片输入 {{imageInput}} tokens / 1M tokens * {{symbol}}{{price}}',
        {
          nonImageInput: inputTokens - imageOutputTokens,
          imageInput: imageOutputTokens,
          symbol,
          usdAmount: inputRatioPrice,
          rate,
        },
      );
    } else if (cacheTokens > 0) {
      inputDesc = buildBillingText(
        '(输入 {{nonCacheInput}} tokens / 1M tokens * {{symbol}}{{price}} + 缓存 {{cacheInput}} tokens / 1M tokens * {{symbol}}{{cachePrice}}',
        {
          nonCacheInput: inputTokens - cacheTokens,
          cacheInput: cacheTokens,
          symbol,
          price: formatBillingDisplayPrice(inputRatioPrice, rate),
          cachePrice: formatBillingDisplayPrice(cacheRatioPrice, rate),
        },
      );
    } else if (audioInputSeperatePrice && audioInputTokens > 0) {
      inputDesc = buildBillingText(
        '(输入 {{nonAudioInput}} tokens / 1M tokens * {{symbol}}{{price}} + 音频输入 {{audioInput}} tokens / 1M tokens * {{symbol}}{{audioPrice}}',
        {
          nonAudioInput: inputTokens - audioInputTokens,
          audioInput: audioInputTokens,
          symbol,
          price: formatBillingDisplayPrice(inputRatioPrice, rate),
          audioPrice: formatBillingDisplayPrice(audioInputPrice, rate),
        },
      );
    } else {
      inputDesc = buildBillingPriceText(
        '(输入 {{input}} tokens / 1M tokens * {{symbol}}{{price}}',
        {
          input: inputTokens,
          symbol,
          usdAmount: inputRatioPrice,
          rate,
        },
      );
    }

    const outputDesc = buildBillingText(
      '输出 {{completion}} tokens / 1M tokens * {{symbol}}{{compPrice}}) * {{ratioType}} {{ratio}}',
      {
        completion: completionTokens,
        symbol,
        compPrice: formatBillingDisplayPrice(completionRatioPrice, rate),
        ratio: groupRatio,
        ratioType: ratioLabel,
      },
    );

    const extraServices = [
      webSearch && webSearchCallCount > 0
        ? buildBillingPriceText(
            ' + Web搜索 {{count}}次 / 1K 次 * {{symbol}}{{price}} * {{ratioType}} {{ratio}}',
            {
              count: webSearchCallCount,
              symbol,
              usdAmount: webSearchPrice,
              rate,
              ratio: groupRatio,
              ratioType: ratioLabel,
            },
          )
        : '',
      fileSearch && fileSearchCallCount > 0
        ? buildBillingPriceText(
            ' + 文件搜索 {{count}}次 / 1K 次 * {{symbol}}{{price}} * {{ratioType}} {{ratio}}',
            {
              count: fileSearchCallCount,
              symbol,
              usdAmount: fileSearchPrice,
              rate,
              ratio: groupRatio,
              ratioType: ratioLabel,
            },
          )
        : '',
      imageGenerationCall && imageGenerationCallPrice > 0
        ? buildBillingPriceText(
            ' + 图片生成调用 {{symbol}}{{price}} / 1次 * {{ratioType}} {{ratio}}',
            {
              symbol,
              usdAmount: imageGenerationCallPrice,
              rate,
              ratio: groupRatio,
              ratioType: ratioLabel,
            },
          )
        : '',
    ].join('');

    const billingLines = [
      buildBillingPriceText(
        '输入价格：{{symbol}}{{price}} / 1M tokens{{audioPrice}}',
        {
          symbol,
          usdAmount: inputRatioPrice,
          rate,
          audioPrice: audioInputSeperatePrice
            ? `，${i18next.t('音频输入价格')} ${symbol}${formatBillingDisplayPrice(audioInputPrice, rate)} / 1M tokens`
            : '',
        },
      ),
      buildBillingPriceText('输出价格：{{symbol}}{{total}} / 1M tokens', {
        symbol,
        usdAmount: completionRatioPrice,
        rate,
        amountKey: 'total',
      }),
      cacheTokens > 0
        ? buildBillingPriceText(
            '缓存读取价格：{{symbol}}{{total}} / 1M tokens',
            {
              symbol,
              usdAmount: inputRatioPrice * cacheRatio,
              rate,
              amountKey: 'total',
            },
          )
        : null,
      image && imageOutputTokens > 0
        ? buildBillingPriceText(
            '图片输入价格：{{symbol}}{{total}} / 1M tokens',
            {
              symbol,
              usdAmount: imageRatioPrice,
              rate,
              amountKey: 'total',
            },
          )
        : null,
      webSearch && webSearchCallCount > 0
        ? buildBillingPriceText('Web搜索价格：{{symbol}}{{price}} / 1K 次', {
            symbol,
            usdAmount: webSearchPrice,
            rate,
          })
        : null,
      fileSearch && fileSearchCallCount > 0
        ? buildBillingPriceText('文件搜索价格：{{symbol}}{{price}} / 1K 次', {
            symbol,
            usdAmount: fileSearchPrice,
            rate,
          })
        : null,
      imageGenerationCall && imageGenerationCallPrice > 0
        ? buildBillingPriceText('图片生成调用：{{symbol}}{{price}} / 1次', {
            symbol,
            usdAmount: imageGenerationCallPrice,
            rate,
          })
        : null,
      buildBillingText(
        '{{inputDesc}} + {{outputDesc}}{{extraServices}} = {{symbol}}{{total}}',
        {
          inputDesc,
          outputDesc,
          extraServices,
          symbol,
          total: formatBillingDisplayPrice(price, rate),
        },
      ),
    ];

    return renderBillingArticle(billingLines);
  }

  if (modelPrice !== -1) {
    const displayPrice = (modelPrice * rate).toFixed(6);
    const displayTotal = (modelPrice * groupRatio * rate).toFixed(6);
    return i18next.t(
      '按次：{{symbol}}{{price}} * {{ratioType}}：{{ratio}} = {{symbol}}{{total}}',
      {
        symbol: symbol,
        price: displayPrice,
        ratio: groupRatio,
        total: displayTotal,
        ratioType: ratioLabel,
      },
    );
  }

  const modelRatioValue = formatRatioValue(modelRatio);
  const completionRatioValue = formatRatioValue(completionRatio);
  const cacheRatioValue = formatRatioValue(cacheRatio);
  const imageRatioValue = formatRatioValue(imageRatio);
  const inputRatioPrice = modelRatio * 2.0;
  const completionRatioPrice = modelRatio * 2.0 * completionRatioValue;
  const audioRatioValue =
    audioInputSeperatePrice && audioInputPrice > 0
      ? formatRatioValue(audioInputPrice / inputRatioPrice)
      : null;

  const textInputTokens = Math.max(
    inputTokens - cacheTokens - audioInputTokens,
    0,
  );
  const imageInputTokens =
    image && imageOutputTokens > 0 ? imageOutputTokens : 0;
  const cacheInputTokens = cacheTokens;

  const textInputAmount =
    (textInputTokens / 1000000) * inputRatioPrice * groupRatio;
  const cacheInputAmount =
    (cacheInputTokens / 1000000) *
    inputRatioPrice *
    cacheRatioValue *
    groupRatio;
  const imageInputAmount =
    (imageInputTokens / 1000000) *
    inputRatioPrice *
    imageRatioValue *
    groupRatio;
  const audioInputAmount =
    (audioInputTokens / 1000000) * audioInputPrice * groupRatio;
  const completionAmount =
    (completionTokens / 1000000) * completionRatioPrice * groupRatio;
  const webSearchAmount =
    (webSearchCallCount / 1000) * webSearchPrice * groupRatio;
  const fileSearchAmount =
    (fileSearchCallCount / 1000) * fileSearchPrice * groupRatio;
  const imageGenerationAmount = imageGenerationCallPrice * groupRatio;

  const totalAmount =
    textInputAmount +
    cacheInputAmount +
    imageInputAmount +
    audioInputAmount +
    completionAmount +
    webSearchAmount +
    fileSearchAmount +
    imageGenerationAmount;

  return renderBillingArticle([
    [
      buildBillingText('模型倍率 {{modelRatio}}', {
        modelRatio: modelRatioValue,
      }),
      buildBillingText('补全倍率 {{completionRatio}}', {
        completionRatio: completionRatioValue,
      }),
      cacheInputTokens > 0
        ? buildBillingText('缓存倍率 {{cacheRatio}}', {
            cacheRatio: cacheRatioValue,
          })
        : null,
      imageInputTokens > 0
        ? buildBillingText('图片倍率 {{imageRatio}}', {
            imageRatio: imageRatioValue,
          })
        : null,
      audioRatioValue !== null
        ? buildBillingText('音频倍率 {{audioRatio}}', {
            audioRatio: audioRatioValue,
          })
        : null,
      buildBillingText('{{ratioType}} {{ratio}}', {
        ratioType: ratioLabel,
        ratio: groupRatio,
      }),
    ]
      .filter(Boolean)
      .join('，'),
    textInputTokens > 0
      ? buildBillingText(
          '普通输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: textInputTokens,
            modelRatio: modelRatioValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(textInputAmount),
          },
        )
      : null,
    cacheInputTokens > 0
      ? buildBillingText(
          '缓存输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 缓存倍率 {{cacheRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: cacheInputTokens,
            modelRatio: modelRatioValue,
            cacheRatio: cacheRatioValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(cacheInputAmount),
          },
        )
      : null,
    imageInputTokens > 0
      ? buildBillingText(
          '图片输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 图片倍率 {{imageRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: imageInputTokens,
            modelRatio: modelRatioValue,
            imageRatio: imageRatioValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(imageInputAmount),
          },
        )
      : null,
    audioInputTokens > 0 && audioRatioValue !== null
      ? buildBillingText(
          '音频输入：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 音频倍率 {{audioRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            tokens: audioInputTokens,
            modelRatio: modelRatioValue,
            audioRatio: audioRatioValue,
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(audioInputAmount),
          },
        )
      : null,
    buildBillingText(
      '输出：{{tokens}} / 1M * 模型倍率 {{modelRatio}} * 补全倍率 {{completionRatio}} * {{ratioType}} {{ratio}} = {{amount}}',
      {
        tokens: completionTokens,
        modelRatio: modelRatioValue,
        completionRatio: completionRatioValue,
        ratioType: ratioLabel,
        ratio: groupRatio,
        amount: renderDisplayAmountFromUsd(completionAmount),
      },
    ),
    webSearch && webSearchCallCount > 0
      ? buildBillingText(
          'Web 搜索：{{count}} / 1K * 单价 {{price}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            count: webSearchCallCount,
            price: renderDisplayAmountFromUsd(webSearchPrice),
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(webSearchAmount),
          },
        )
      : null,
    fileSearch && fileSearchCallCount > 0
      ? buildBillingText(
          '文件搜索：{{count}} / 1K * 单价 {{price}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            count: fileSearchCallCount,
            price: renderDisplayAmountFromUsd(fileSearchPrice),
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(fileSearchAmount),
          },
        )
      : null,
    imageGenerationCall && imageGenerationCallPrice > 0
      ? buildBillingText(
          '图片生成：1 次 * 单价 {{price}} * {{ratioType}} {{ratio}} = {{amount}}',
          {
            price: renderDisplayAmountFromUsd(imageGenerationCallPrice),
            ratioType: ratioLabel,
            ratio: groupRatio,
            amount: renderDisplayAmountFromUsd(imageGenerationAmount),
          },
        )
      : null,
    buildBillingText('合计：{{total}}', {
      total: renderDisplayAmountFromUsd(totalAmount),
    }),
  ]);
}

export function renderLogContent(opts) {
  const {
    model_ratio: modelRatio,
    completion_ratio: completionRatio,
    model_price: modelPrice = -1,
    group_ratio: groupRatio,
    user_group_ratio,
    cache_ratio: cacheRatio = 1.0,
    image = false,
    image_ratio: imageRatio = 1.0,
    web_search: webSearch = false,
    web_search_call_count: webSearchCallCount = 0,
    file_search: fileSearch = false,
    file_search_call_count: fileSearchCallCount = 0,
    displayMode = 'price',
  } = opts;
  const {
    ratio,
    label: ratioLabel,
    useUserGroupRatio: useUserGroupRatio,
  } = getEffectiveRatio(groupRatio, user_group_ratio);

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
    ];
    appendPricePart(
      parts,
      cacheRatio !== 1.0,
      '缓存读取价格 {{symbol}}{{price}} / 1M tokens',
      {
        symbol,
        price: (modelRatio * 2.0 * cacheRatio * rate).toFixed(6),
      },
    );
    appendPricePart(
      parts,
      image,
      '图片输入价格 {{symbol}}{{price}} / 1M tokens',
      {
        symbol,
        price: (modelRatio * 2.0 * imageRatio * rate).toFixed(6),
      },
    );
    appendPricePart(
      parts,
      webSearch,
      'Web 搜索调用 {{webSearchCallCount}} 次',
      {
        webSearchCallCount,
      },
    );
    appendPricePart(
      parts,
      fileSearch,
      '文件搜索调用 {{fileSearchCallCount}} 次',
      {
        fileSearchCallCount,
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
      ratio,
    });
  } else {
    if (image) {
      return i18next.t(
        '模型倍率 {{modelRatio}}，缓存倍率 {{cacheRatio}}，输出倍率 {{completionRatio}}，图片输入倍率 {{imageRatio}}，{{ratioType}} {{ratio}}',
        {
          modelRatio: modelRatio,
          cacheRatio: cacheRatio,
          completionRatio: completionRatio,
          imageRatio: imageRatio,
          ratioType: ratioLabel,
          ratio,
        },
      );
    } else if (webSearch) {
      return i18next.t(
        '模型倍率 {{modelRatio}}，缓存倍率 {{cacheRatio}}，输出倍率 {{completionRatio}}，{{ratioType}} {{ratio}}，Web 搜索调用 {{webSearchCallCount}} 次',
        {
          modelRatio: modelRatio,
          cacheRatio: cacheRatio,
          completionRatio: completionRatio,
          ratioType: ratioLabel,
          ratio,
          webSearchCallCount,
        },
      );
    } else {
      return i18next.t(
        '模型倍率 {{modelRatio}}，缓存倍率 {{cacheRatio}}，输出倍率 {{completionRatio}}，{{ratioType}} {{ratio}}',
        {
          modelRatio: modelRatio,
          cacheRatio: cacheRatio,
          completionRatio: completionRatio,
          ratioType: ratioLabel,
          ratio,
        },
      );
    }
  }
}
