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
import { Card, Radio, Space, Spin, Tag, Typography } from '@douyinfe/semi-ui';

const { Text, Title } = Typography;
const RadioGroup = Radio.Group;

export default function DeploymentPriceEstimate({ controller }) {
  const {
    currencyLabel,
    handleCurrencyChange,
    loadingPrice,
    priceCurrency,
    priceEstimation,
    priceSectionRef,
    priceSummaryItems,
    priceUnavailableContent,
    t,
  } = controller;

  return (
    <div ref={priceSectionRef}>
      <Card className='mb-4'>
        <div className='flex flex-wrap items-center justify-between gap-3'>
          <Title heading={6} style={{ margin: 0 }}>
            {t('价格预估')}
          </Title>
          <Space align='center' spacing={12} className='flex flex-wrap'>
            <Text type='secondary' size='small'>
              {t('计价币种')}
            </Text>
            <RadioGroup
              type='button'
              value={priceCurrency}
              onChange={handleCurrencyChange}
            >
              <Radio value='usdc'>USDC</Radio>
              <Radio value='iocoin'>IOCOIN</Radio>
            </RadioGroup>
            <Tag size='small' color='blue'>
              {currencyLabel}
            </Tag>
          </Space>
        </div>

        {priceEstimation ? (
          <div className='mt-4 flex w-full flex-col gap-4'>
            <div className='grid w-full gap-4 md:grid-cols-2 lg:grid-cols-3'>
              <div
                className='flex flex-col gap-1 rounded-md px-4 py-3'
                style={{
                  border: '1px solid var(--semi-color-border)',
                  backgroundColor: 'var(--semi-color-fill-0)',
                }}
              >
                <Text size='small' type='tertiary'>
                  {t('预估总费用')}
                </Text>
                <div
                  style={{
                    fontSize: 24,
                    fontWeight: 600,
                    color: 'var(--semi-color-text-0)',
                  }}
                >
                  {typeof priceEstimation.estimated_cost === 'number'
                    ? `${priceEstimation.estimated_cost.toFixed(4)} ${currencyLabel}`
                    : '--'}
                </div>
              </div>
              <div
                className='flex flex-col gap-1 rounded-md px-4 py-3'
                style={{
                  border: '1px solid var(--semi-color-border)',
                  backgroundColor: 'var(--semi-color-fill-0)',
                }}
              >
                <Text size='small' type='tertiary'>
                  {t('小时费率')}
                </Text>
                <Text strong>
                  {typeof priceEstimation.price_breakdown?.hourly_rate ===
                  'number'
                    ? `${priceEstimation.price_breakdown.hourly_rate.toFixed(4)} ${currencyLabel}/h`
                    : '--'}
                </Text>
              </div>
              <div
                className='flex flex-col gap-1 rounded-md px-4 py-3'
                style={{
                  border: '1px solid var(--semi-color-border)',
                  backgroundColor: 'var(--semi-color-fill-0)',
                }}
              >
                <Text size='small' type='tertiary'>
                  {t('计算成本')}
                </Text>
                <Text strong>
                  {typeof priceEstimation.price_breakdown?.compute_cost ===
                  'number'
                    ? `${priceEstimation.price_breakdown.compute_cost.toFixed(4)} ${currencyLabel}`
                    : '--'}
                </Text>
              </div>
            </div>

            <div className='grid gap-3 sm:grid-cols-2 lg:grid-cols-3'>
              {priceSummaryItems.map((item) => (
                <div
                  key={item.key}
                  className='flex items-center justify-between gap-3 rounded-md px-3 py-2'
                  style={{
                    border: '1px solid var(--semi-color-border)',
                    backgroundColor: 'var(--semi-color-fill-0)',
                  }}
                >
                  <Text size='small' type='tertiary'>
                    {item.label}
                  </Text>
                  <Text strong>{item.value}</Text>
                </div>
              ))}
            </div>
          </div>
        ) : (
          priceUnavailableContent
        )}

        {priceEstimation && loadingPrice && (
          <Space align='center' spacing={8} style={{ marginTop: 12 }}>
            <Spin size='small' />
            <Text size='small' type='tertiary'>
              {t('价格重新计算中...')}
            </Text>
          </Space>
        )}
      </Card>
    </div>
  );
}
