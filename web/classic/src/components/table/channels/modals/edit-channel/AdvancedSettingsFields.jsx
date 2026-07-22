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
  Col,
  Dropdown,
  Form,
  Row,
  Space,
  Tag,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import { IconChevronDown, IconCode, IconCopy } from '@douyinfe/semi-icons';
import { MODEL_FETCHABLE_CHANNEL_TYPES } from '../../../../../constants';
import { STATUS_CODE_MAPPING_EXAMPLE } from './constants';
import JSONEditor from '../../../../common/ui/JSONEditor';
import { useEditChannelEditor } from './EditorContext';

const { Text } = Typography;

export default function AdvancedSettingsFields() {
  const {
    applyParamOverrideTemplate,
    channelId,
    clearParamOverride,
    copyParamOverrideJson,
    formApiRef,
    formatJsonField,
    formatUnixTime,
    handleChannelOtherSettingsChange,
    handleChannelSettingsChange,
    handleInputChange,
    inputs,
    isEdit,
    paramOverrideMeta,
    setParamOverrideEditorVisible,
    t,
    upstreamDetectedModels,
    upstreamDetectedModelsOmittedCount,
    upstreamDetectedModelsPreview,
  } = useEditChannelEditor();

  return (
    <div className='space-y-4'>
      {/* Upstream Model Management Section */}
      {MODEL_FETCHABLE_CHANNEL_TYPES.has(inputs.type) && (
        <div className='pb-3 border-b border-gray-100'>
          <Text className='text-sm font-medium text-gray-500 mb-3 block'>
            {t('上游模型管理')}
          </Text>

          <Form.Switch
            field='upstream_model_update_check_enabled'
            label={t('是否检测上游模型更新')}
            checkedText={t('开')}
            uncheckedText={t('关')}
            onChange={(value) =>
              handleChannelOtherSettingsChange(
                'upstream_model_update_check_enabled',
                value,
              )
            }
            extraText={t('开启后由后端定时任务检测该渠道上游模型变化')}
          />
          <Form.Switch
            field='upstream_model_update_auto_sync_enabled'
            label={t('是否自动同步上游模型更新')}
            checkedText={t('开')}
            uncheckedText={t('关')}
            disabled={!inputs.upstream_model_update_check_enabled}
            onChange={(value) =>
              handleChannelOtherSettingsChange(
                'upstream_model_update_auto_sync_enabled',
                value,
              )
            }
            extraText={t('开启后检测到新增模型会自动加入当前渠道模型列表')}
          />
          <Form.Input
            field='upstream_model_update_ignored_models'
            label={t('已忽略模型')}
            placeholder={t(
              '例如：gpt-4.1-nano,regex:^claude-.*$,regex:^sora-.*$',
            )}
            extraText={t('支持精确匹配；使用 regex: 开头可按正则匹配。')}
            onChange={(value) =>
              handleInputChange('upstream_model_update_ignored_models', value)
            }
            showClear
          />
          <div className='text-xs text-gray-500 mb-2'>
            {t('上次检测时间')}:&nbsp;
            {formatUnixTime(inputs.upstream_model_update_last_check_time)}
          </div>
          <div className='text-xs text-gray-500 mb-3'>
            {t('上次检测到可加入模型')}:&nbsp;
            {upstreamDetectedModels.length === 0 ? (
              t('暂无')
            ) : (
              <>
                <Tooltip
                  position='topLeft'
                  content={
                    <div className='max-w-[640px] break-all text-xs leading-5'>
                      {upstreamDetectedModels.join(', ')}
                    </div>
                  }
                >
                  <span className='cursor-help break-all'>
                    {upstreamDetectedModelsPreview.join(', ')}
                  </span>
                </Tooltip>
                <span className='ml-1 text-gray-400'>
                  {upstreamDetectedModelsOmittedCount > 0
                    ? t('（共 {{total}} 个，省略 {{omit}} 个）', {
                        total: upstreamDetectedModels.length,
                        omit: upstreamDetectedModelsOmittedCount,
                      })
                    : t('（共 {{total}} 个）', {
                        total: upstreamDetectedModels.length,
                      })}
                </span>
              </>
            )}
          </div>
        </div>
      )}

      {/* Request Config Section */}
      <div className='py-3 border-b border-gray-100'>
        <Text className='text-sm font-medium text-gray-500 mb-3 block'>
          {t('请求配置')}
        </Text>

        <div className='mb-4'>
          <div className='flex items-center justify-between gap-2 mb-1'>
            <Text className='text-sm font-medium'>{t('参数覆盖')}</Text>
            <Space>
              <Button
                size='small'
                type='primary'
                icon={<IconCode size={14} />}
                onClick={() => setParamOverrideEditorVisible(true)}
              >
                {t('可视化编辑')}
              </Button>
              <Dropdown
                trigger='click'
                position='bottomRight'
                menu={[
                  {
                    node: 'item',
                    name: t('填充新模板'),
                    onClick: () =>
                      applyParamOverrideTemplate('operations', 'fill'),
                  },
                  {
                    node: 'item',
                    name: t('填充旧模板'),
                    onClick: () => applyParamOverrideTemplate('legacy', 'fill'),
                  },
                  {
                    node: 'item',
                    name: t('清空'),
                    onClick: clearParamOverride,
                  },
                ]}
              >
                <Button size='small' type='tertiary'>
                  {t('更多')} <IconChevronDown size={12} />
                </Button>
              </Dropdown>
            </Space>
          </div>
          <Text type='tertiary' size='small'>
            {t('此项可选，用于覆盖请求参数。不支持覆盖 stream 参数')}
          </Text>
          <div
            className='mt-2 rounded-xl p-3'
            style={{
              backgroundColor: 'var(--semi-color-fill-0)',
              border: '1px solid var(--semi-color-fill-2)',
            }}
          >
            <div className='flex items-center justify-between mb-2'>
              <Tag color={paramOverrideMeta.tagColor}>
                {paramOverrideMeta.tagLabel}
              </Tag>
              <Button
                size='small'
                icon={<IconCopy />}
                type='tertiary'
                onClick={copyParamOverrideJson}
              >
                {t('复制')}
              </Button>
            </div>
            <pre className='mb-0 text-xs leading-5 whitespace-pre-wrap break-all max-h-56 overflow-auto'>
              {paramOverrideMeta.preview}
            </pre>
          </div>
        </div>

        <Form.TextArea
          field='header_override'
          label={t('请求头覆盖')}
          placeholder={
            t('此项可选，用于覆盖请求头参数') +
            '\n' +
            t('格式示例：') +
            '\n{\n  "User-Agent": "Mozilla/5.0 ...",\n  "Authorization": "Bearer {api_key}"\n}'
          }
          autosize
          onChange={(value) => handleInputChange('header_override', value)}
          extraText={
            <div className='flex flex-col gap-1'>
              <div className='flex gap-2 flex-wrap items-center'>
                <Text
                  className='!text-semi-color-primary cursor-pointer'
                  onClick={() =>
                    handleInputChange(
                      'header_override',
                      JSON.stringify(
                        {
                          '*': true,
                          're:^X-Trace-.*$': true,
                          'X-Foo': '{client_header:X-Foo}',
                          Authorization: 'Bearer {api_key}',
                          'User-Agent':
                            'Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36 Edg/139.0.0.0',
                        },
                        null,
                        2,
                      ),
                    )
                  }
                >
                  {t('填入模板')}
                </Text>
                <Text
                  className='!text-semi-color-primary cursor-pointer'
                  onClick={() =>
                    handleInputChange(
                      'header_override',
                      JSON.stringify({ '*': true }, null, 2),
                    )
                  }
                >
                  {t('填入透传模版')}
                </Text>
                <Text
                  className='!text-semi-color-primary cursor-pointer'
                  onClick={() => formatJsonField('header_override')}
                >
                  {t('格式化')}
                </Text>
              </div>
              <div>
                <Text type='tertiary' size='small'>
                  {t('支持变量：')}
                </Text>
                <div className='text-xs text-tertiary ml-2'>
                  <div>
                    {t('渠道密钥')}: {'{api_key}'}
                  </div>
                </div>
              </div>
            </div>
          }
          showClear
        />
        <JSONEditor
          key={`status_code_mapping-${isEdit ? channelId : 'new'}`}
          field='status_code_mapping'
          label={t('状态码复写')}
          placeholder={
            t(
              '此项可选，用于复写返回的状态码，仅影响本地判断，不修改返回到上游的状态码，比如将claude渠道的400错误复写为500（用于重试），请勿滥用该功能，例如：',
            ) +
            '\n' +
            JSON.stringify(STATUS_CODE_MAPPING_EXAMPLE, null, 2)
          }
          value={inputs.status_code_mapping || ''}
          onChange={(value) => handleInputChange('status_code_mapping', value)}
          template={STATUS_CODE_MAPPING_EXAMPLE}
          templateLabel={t('填入模板')}
          editorType='keyValue'
          formApi={formApiRef.current}
          extraText={t('键为原状态码，值为要复写的状态码，仅影响本地判断')}
        />
      </div>

      {/* Channel Behavior Section */}
      <div className='py-3 border-b border-gray-100'>
        <Text className='text-sm font-medium text-gray-500 mb-3 block'>
          {t('渠道行为')}
        </Text>

        <Form.Input
          field='tag'
          label={t('渠道标签')}
          placeholder={t('渠道标签')}
          showClear
          onChange={(value) => handleInputChange('tag', value)}
        />
        <Form.TextArea
          field='remark'
          label={t('备注')}
          placeholder={t('请输入备注（仅管理员可见）')}
          maxLength={255}
          showClear
          onChange={(value) => handleInputChange('remark', value)}
        />

        <Row gutter={12}>
          <Col span={12}>
            <Form.InputNumber
              field='priority'
              label={t('渠道优先级')}
              placeholder={t('渠道优先级')}
              min={0}
              onNumberChange={(value) => handleInputChange('priority', value)}
              style={{ width: '100%' }}
            />
          </Col>
          <Col span={12}>
            <Form.InputNumber
              field='weight'
              label={t('渠道权重')}
              placeholder={t('渠道权重')}
              min={0}
              onNumberChange={(value) => handleInputChange('weight', value)}
              style={{ width: '100%' }}
            />
          </Col>
        </Row>

        {inputs.type === 1 && (
          <>
            <div className='mt-4 mb-2 text-sm font-medium text-gray-700'>
              {t('字段透传控制')}
            </div>
            <Form.Switch
              field='allow_service_tier'
              label={t('允许 service_tier 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange('allow_service_tier', value)
              }
              extraText={t(
                'service_tier 字段用于指定服务层级，允许透传可能导致实际计费高于预期。默认关闭以避免额外费用',
              )}
            />
            <Form.Switch
              field='disable_store'
              label={t('禁用 store 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange('disable_store', value)
              }
              extraText={t(
                'store 字段用于授权 OpenAI 存储请求数据以评估和优化产品。默认关闭，开启后可能导致 Codex 无法正常使用',
              )}
            />
            <Form.Switch
              field='allow_safety_identifier'
              label={t('允许 safety_identifier 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange(
                  'allow_safety_identifier',
                  value,
                )
              }
              extraText={t(
                'safety_identifier 字段用于帮助 OpenAI 识别可能违反使用政策的应用程序用户。默认关闭以保护用户隐私',
              )}
            />
            <Form.Switch
              field='allow_include_obfuscation'
              label={t('允许 stream_options.include_obfuscation 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange(
                  'allow_include_obfuscation',
                  value,
                )
              }
              extraText={t(
                'include_obfuscation 用于控制 Responses 流混淆字段。默认关闭以避免客户端关闭该安全保护',
              )}
            />
          </>
        )}

        {inputs.type === 14 && (
          <>
            <div className='mt-4 mb-2 text-sm font-medium text-gray-700'>
              {t('字段透传控制')}
            </div>
            <Form.Switch
              field='allow_service_tier'
              label={t('允许 service_tier 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange('allow_service_tier', value)
              }
              extraText={t(
                'service_tier 字段用于指定服务层级，允许透传可能导致实际计费高于预期。默认关闭以避免额外费用',
              )}
            />
            <Form.Switch
              field='allow_inference_geo'
              label={t('允许 inference_geo 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange('allow_inference_geo', value)
              }
              extraText={t(
                'inference_geo 字段用于控制 Claude 数据驻留推理区域。默认关闭以避免未经授权透传地域信息',
              )}
            />
            <Form.Switch
              field='allow_speed'
              label={t('允许 speed 透传')}
              checkedText={t('开')}
              uncheckedText={t('关')}
              onChange={(value) =>
                handleChannelOtherSettingsChange('allow_speed', value)
              }
              extraText={t(
                'speed 字段用于控制 Claude 推理速度模式。默认关闭以避免意外切换到 fast 模式',
              )}
            />
          </>
        )}
      </div>

      {/* Extra Settings Section */}
      <div className='pt-3'>
        <Text className='text-sm font-medium text-gray-500 mb-3 block'>
          {t('额外设置')}
        </Text>

        {inputs.type === 14 && (
          <Form.Switch
            field='claude_beta_query'
            label={t('Claude 强制 beta=true')}
            checkedText={t('开')}
            uncheckedText={t('关')}
            onChange={(value) =>
              handleChannelOtherSettingsChange('claude_beta_query', value)
            }
            extraText={t(
              '开启后，该渠道请求 Claude 时将强制追加 ?beta=true（无需客户端手动传参）',
            )}
          />
        )}

        {inputs.type === 1 && (
          <Form.Switch
            field='force_format'
            label={t('强制格式化')}
            checkedText={t('开')}
            uncheckedText={t('关')}
            onChange={(value) =>
              handleChannelSettingsChange('force_format', value)
            }
            extraText={t(
              '强制将响应格式化为 OpenAI 标准格式（只适用于OpenAI渠道类型）',
            )}
          />
        )}

        <Form.Switch
          field='thinking_to_content'
          label={t('思考内容转换')}
          checkedText={t('开')}
          uncheckedText={t('关')}
          onChange={(value) =>
            handleChannelSettingsChange('thinking_to_content', value)
          }
          extraText={t('将 reasoning_content 转换为 <think> 标签拼接到内容中')}
        />
        <Form.Switch
          field='pass_through_body_enabled'
          label={t('透传请求体')}
          checkedText={t('开')}
          uncheckedText={t('关')}
          onChange={(value) =>
            handleChannelSettingsChange('pass_through_body_enabled', value)
          }
          extraText={t('启用请求体透传功能')}
        />

        <Form.Input
          field='proxy'
          label={t('代理地址')}
          placeholder={t('例如: socks5://user:pass@host:port')}
          onChange={(value) => handleChannelSettingsChange('proxy', value)}
          showClear
          extraText={t('用于配置网络代理，支持 socks5 协议')}
        />

        <Form.TextArea
          field='system_prompt'
          label={t('系统提示词')}
          placeholder={t('输入系统提示词，用户的系统提示词将优先于此设置')}
          onChange={(value) =>
            handleChannelSettingsChange('system_prompt', value)
          }
          autosize
          showClear
          extraText={t(
            '用户优先：如果用户在请求中指定了系统提示词，将优先使用用户的设置',
          )}
        />
        <Form.Switch
          field='system_prompt_override'
          label={t('系统提示词拼接')}
          checkedText={t('开')}
          uncheckedText={t('关')}
          onChange={(value) =>
            handleChannelSettingsChange('system_prompt_override', value)
          }
          extraText={t(
            '如果用户请求中包含系统提示词，则使用此设置拼接到用户的系统提示词前面',
          )}
        />
      </div>
    </div>
  );
}
