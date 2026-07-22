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
import {
  Button,
  Card,
  Input,
  Modal,
  Select,
  Space,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import {
  FIELD_GUIDE_TARGET_OPTIONS,
  HEADER_VALUE_JSONC_EXAMPLE,
} from './editorDomain';
import { useParamOverrideEditor } from './EditorContext';

const { Text } = Typography;

export default function ParamOverrideAuxiliaryModals() {
  const {
    applyBuiltinField,
    copyBuiltinField,
    fieldGuideActionLabel,
    fieldGuideFieldCount,
    fieldGuideKeyword,
    fieldGuideTarget,
    fieldGuideVisible,
    filteredFieldGuideSections,
    headerValueExampleVisible,
    setFieldGuideKeyword,
    setFieldGuideTarget,
    setFieldGuideVisible,
    setHeaderValueExampleVisible,
    t,
  } = useParamOverrideEditor();

  return (
    <>
      <Modal
        title={t('anthropic-beta JSON 示例')}
        visible={headerValueExampleVisible}
        width={760}
        footer={null}
        onCancel={() => setHeaderValueExampleVisible(false)}
        bodyStyle={{ padding: 16, paddingBottom: 24 }}
      >
        <Space vertical align='start' spacing={12} style={{ width: '100%' }}>
          <Text type='tertiary' size='small'>
            {t('下面是带注释的示例，仅用于参考；实际保存时请删除注释。')}
          </Text>
          <TextArea
            value={HEADER_VALUE_JSONC_EXAMPLE}
            readOnly
            autosize={{ minRows: 16, maxRows: 20 }}
            style={{ marginBottom: 8 }}
          />
        </Space>
      </Modal>

      <Modal
        title={null}
        visible={fieldGuideVisible}
        width={860}
        footer={null}
        onCancel={() => setFieldGuideVisible(false)}
        bodyStyle={{
          maxHeight: '72vh',
          overflowY: 'auto',
          padding: 16,
          background: 'var(--semi-color-bg-0)',
        }}
      >
        <Space vertical spacing={12} style={{ width: '100%' }}>
          <div className='flex items-start justify-between gap-3'>
            <div>
              <Text strong style={{ fontSize: 22, lineHeight: '30px' }}>
                {t('字段速查')}
              </Text>
              <Text
                type='tertiary'
                size='small'
                className='block mt-1'
                style={{ maxWidth: 560 }}
              >
                {t(
                  '先搜索，再一键复制字段名或填入当前规则。字段名为系统内部路径，可直接用于路径 / 来源 / 目标。',
                )}
              </Text>
            </div>
            <Tag color='blue'>{`${fieldGuideFieldCount} ${t('个字段')}`}</Tag>
          </div>

          <Card
            className='!rounded-xl !border-0'
            bodyStyle={{
              padding: 12,
              background: 'var(--semi-color-fill-0)',
            }}
          >
            <div className='flex items-center gap-2'>
              <Input
                value={fieldGuideKeyword}
                onChange={(nextValue) => setFieldGuideKeyword(nextValue || '')}
                placeholder={t('搜索字段名 / 中文说明')}
                showClear
                style={{ flex: 1 }}
              />
              <Select
                value={fieldGuideTarget}
                optionList={FIELD_GUIDE_TARGET_OPTIONS}
                onChange={(nextValue) =>
                  setFieldGuideTarget(nextValue || 'path')
                }
                style={{ width: 170 }}
              />
            </div>
          </Card>

          {filteredFieldGuideSections.length === 0 ? (
            <Card
              className='!rounded-xl !border-0'
              bodyStyle={{
                padding: 20,
                background: 'var(--semi-color-fill-0)',
              }}
            >
              <Text type='tertiary'>{t('没有匹配的字段')}</Text>
            </Card>
          ) : (
            <div className='flex flex-col gap-2'>
              {filteredFieldGuideSections.map((section) => (
                <Card
                  key={section.title}
                  className='!rounded-xl !border-0'
                  bodyStyle={{
                    padding: 14,
                    background: 'var(--semi-color-fill-0)',
                  }}
                >
                  <div className='flex items-center justify-between mb-1'>
                    <Text strong style={{ fontSize: 18 }}>
                      {section.title}
                    </Text>
                    <Tag color='grey'>{`${section.fields.length} ${t('项')}`}</Tag>
                  </div>
                  <div
                    style={{
                      display: 'flex',
                      flexDirection: 'column',
                      marginTop: 6,
                    }}
                  >
                    {section.fields.map((field, index) => (
                      <div
                        key={field.key}
                        className='flex items-start justify-between gap-3'
                        style={{
                          paddingTop: 10,
                          paddingBottom: 10,
                          borderTop:
                            index === 0
                              ? 'none'
                              : '1px solid var(--semi-color-border)',
                        }}
                      >
                        <div style={{ flex: 1, minWidth: 0 }}>
                          <Text strong>{field.label}</Text>
                          <Text
                            type='secondary'
                            size='small'
                            className='block mt-1 font-mono'
                            style={{
                              background: 'var(--semi-color-bg-1)',
                              border: '1px solid var(--semi-color-border)',
                              borderRadius: 8,
                              padding: '4px 8px',
                              width: 'fit-content',
                            }}
                          >
                            {field.key}
                          </Text>
                          <Text
                            type='tertiary'
                            size='small'
                            className='block mt-1'
                            style={{ lineHeight: '18px' }}
                          >
                            {field.tip}
                          </Text>
                        </div>
                        <Space spacing={6} align='center'>
                          <Button
                            size='small'
                            type='tertiary'
                            onClick={() => copyBuiltinField(field.key)}
                          >
                            {t('复制')}
                          </Button>
                          <Button
                            size='small'
                            onClick={() =>
                              applyBuiltinField(field.key, fieldGuideTarget)
                            }
                          >
                            {fieldGuideActionLabel}
                          </Button>
                        </Space>
                      </div>
                    ))}
                  </div>
                </Card>
              ))}
            </div>
          )}
        </Space>
      </Modal>
    </>
  );
}
