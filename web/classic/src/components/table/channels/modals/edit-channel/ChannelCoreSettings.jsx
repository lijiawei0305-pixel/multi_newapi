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
  Avatar,
  Banner,
  Button,
  Card,
  Dropdown,
  Form,
  Space,
  Tooltip,
  Typography,
} from '@douyinfe/semi-ui';
import {
  IconBolt,
  IconChevronDown,
  IconGlobe,
  IconSearch,
  IconServer,
} from '@douyinfe/semi-icons';
import {
  copy,
  selectFilter,
  showError,
  showInfo,
  showSuccess,
} from '../../../../../helpers';
import { MODEL_FETCHABLE_CHANNEL_TYPES } from '../../../../../constants';
import { openHttpUrlInNewTab } from '../../../../../helpers/safeNavigation';
import {
  DEPRECATED_DOUBAO_CODING_PLAN_BASE_URL,
  MODEL_MAPPING_EXAMPLE,
  REGION_EXAMPLE,
  type2secretPrompt,
} from './constants';
import JSONEditor from '../../../../common/ui/JSONEditor';
import { useEditChannelEditor } from './EditorContext';

const { Text } = Typography;

export default function ChannelCoreSettings() {
  const {
    addCustomModels,
    autoBan,
    basicModels,
    batch,
    batchExtra,
    canKeepDeprecatedDoubaoCodingPlan,
    channelId,
    channelOptionList,
    codexCredentialRefreshing,
    customModel,
    doubaoApiEditUnlocked,
    doubaoCodingPlanOptionLabel,
    fetchUpstreamModelList,
    formApiRef,
    formatJsonField,
    fullModels,
    groupOptions,
    handleApiConfigSecretClick,
    handleChannelOtherSettingsChange,
    handleInputChange,
    handleOpenIonetDeployment,
    handleRefreshCodexCredential,
    handleShow2FAModal,
    handleVertexUploadChange,
    inputs,
    ionetMetadata,
    isEdit,
    isIonetChannel,
    isIonetLocked,
    isMultiKeyChannel,
    keyMode,
    modelGroups,
    modelOptions,
    modelSearchHintText,
    multiToSingle,
    openModelMappingValueModal,
    renderChannelOption,
    setAutoBan,
    setBatch,
    setChannelSearchValue,
    setCustomModel,
    setInputs,
    setIsEnterpriseAccount,
    setKeyMode,
    setModelSearchValue,
    setMultiKeyMode,
    setOllamaModalVisible,
    setUseManualInput,
    setVertexFileList,
    setVertexKeys,
    showApiConfigCard,
    t,
    useManualInput,
    vertexFileList,
  } = useEditChannelEditor();

  return (
    <Card className='!rounded-2xl shadow-sm border-0'>
      {/* Header */}
      <div className='flex items-center mb-4'>
        <Avatar size='small' color='blue' className='mr-2 shadow-md'>
          <IconServer size={16} />
        </Avatar>
        <div>
          <Text className='text-lg font-medium'>{t('核心配置')}</Text>
          <div className='text-xs text-gray-600'>
            {t('创建渠道所需的基本信息')}
          </div>
        </div>
      </div>

      {isIonetChannel && (
        <Banner
          type='info'
          closeIcon={null}
          className='mb-4 rounded-xl'
          description={t(
            '此渠道由 IO.NET 自动同步，类型、密钥和 API 地址已锁定。',
          )}
        >
          <Space>
            {ionetMetadata?.deployment_id && (
              <Button
                size='small'
                theme='light'
                type='primary'
                icon={<IconGlobe />}
                onClick={handleOpenIonetDeployment}
              >
                {t('查看关联部署')}
              </Button>
            )}
          </Space>
        </Banner>
      )}

      <Form.Select
        field='type'
        label={t('类型')}
        placeholder={t('请选择渠道类型')}
        rules={[{ required: true, message: t('请选择渠道类型') }]}
        optionList={channelOptionList}
        style={{ width: '100%' }}
        filter={selectFilter}
        autoClearSearchValue={false}
        searchPosition='dropdown'
        onSearch={(value) => setChannelSearchValue(value)}
        renderOptionItem={renderChannelOption}
        onChange={(value) => handleInputChange('type', value)}
        disabled={isIonetLocked}
      />

      {inputs.type === 57 && (
        <Banner
          type='warning'
          closeIcon={null}
          className='mb-4 rounded-xl'
          description={t(
            '免责声明：仅限个人使用，请勿分发或共享任何凭证。该渠道存在前置条件与使用门槛，请在充分了解流程与风险后使用，并遵守 OpenAI 的相关条款与政策。相关凭证与配置仅限接入 Codex CLI 使用，不适用于其他客户端、平台或渠道。',
          )}
        />
      )}

      {inputs.type === 20 && (
        <Form.Switch
          field='is_enterprise_account'
          label={t('是否为企业账户')}
          checkedText={t('是')}
          uncheckedText={t('否')}
          onChange={(value) => {
            setIsEnterpriseAccount(value);
            handleInputChange('is_enterprise_account', value);
          }}
          extraText={t(
            '企业账户为特殊返回格式，需要特殊处理，如果非企业账户，请勿勾选',
          )}
          initValue={inputs.is_enterprise_account}
        />
      )}

      <Form.Input
        field='name'
        label={t('名称')}
        placeholder={t('请为渠道命名')}
        rules={[{ required: true, message: t('请为渠道命名') }]}
        showClear
        onChange={(value) => handleInputChange('name', value)}
        autoComplete='new-password'
      />

      {inputs.type === 33 && (
        <>
          <Form.Select
            field='aws_key_type'
            label={t('密钥格式')}
            placeholder={t('请选择密钥格式')}
            optionList={[
              {
                label: 'AccessKey / SecretAccessKey',
                value: 'ak_sk',
              },
              { label: 'API Key', value: 'api_key' },
            ]}
            style={{ width: '100%' }}
            value={inputs.aws_key_type || 'ak_sk'}
            onChange={(value) => {
              handleChannelOtherSettingsChange('aws_key_type', value);
            }}
            extraText={t(
              'AK/SK 模式：使用 AccessKey 和 SecretAccessKey；API Key 模式：使用 API Key',
            )}
          />
        </>
      )}

      {inputs.type === 41 && (
        <Form.Select
          field='vertex_key_type'
          label={t('密钥格式')}
          placeholder={t('请选择密钥格式')}
          optionList={[
            { label: 'JSON', value: 'json' },
            { label: 'API Key', value: 'api_key' },
          ]}
          style={{ width: '100%' }}
          value={inputs.vertex_key_type || 'json'}
          onChange={(value) => {
            // 更新设置中的 vertex_key_type
            handleChannelOtherSettingsChange('vertex_key_type', value);
            // 切换为 api_key 时，关闭批量与手动/文件切换，并清理已选文件
            if (value === 'api_key') {
              setBatch(false);
              setUseManualInput(false);
              setVertexKeys([]);
              setVertexFileList([]);
              if (formApiRef.current) {
                formApiRef.current.setValue('vertex_files', []);
              }
            }
          }}
          extraText={
            inputs.vertex_key_type === 'api_key'
              ? t('API Key 模式下不支持批量创建')
              : t('JSON 模式支持手动输入或上传服务账号 JSON')
          }
        />
      )}
      {batch ? (
        inputs.type === 41 && (inputs.vertex_key_type || 'json') === 'json' ? (
          <Form.Upload
            field='vertex_files'
            label={t('密钥文件 (.json)')}
            accept='.json'
            multiple
            draggable
            dragIcon={<IconBolt />}
            dragMainText={t('点击上传文件或拖拽文件到这里')}
            dragSubText={t('仅支持 JSON 文件，支持多文件')}
            style={{ marginTop: 10 }}
            uploadTrigger='custom'
            beforeUpload={() => false}
            onChange={handleVertexUploadChange}
            fileList={vertexFileList}
            rules={
              isEdit
                ? []
                : [
                    {
                      required: true,
                      message: t('请上传密钥文件'),
                    },
                  ]
            }
            extraText={batchExtra}
          />
        ) : (
          <Form.TextArea
            field='key'
            label={t('密钥')}
            placeholder={
              inputs.type === 33
                ? inputs.aws_key_type === 'api_key'
                  ? t('请输入 API Key，一行一个，格式：APIKey|Region')
                  : t(
                      '请输入密钥，一行一个，格式：AccessKey|SecretAccessKey|Region',
                    )
                : t('请输入密钥，一行一个')
            }
            rules={isEdit ? [] : [{ required: true, message: t('请输入密钥') }]}
            autosize
            autoComplete='new-password'
            onChange={(value) => handleInputChange('key', value)}
            disabled={isIonetLocked}
            extraText={
              <div className='flex items-center gap-2 flex-wrap'>
                {isEdit && isMultiKeyChannel && keyMode === 'append' && (
                  <Text type='warning' size='small'>
                    {t('追加模式：新密钥将添加到现有密钥列表的末尾')}
                  </Text>
                )}
                {isEdit && (
                  <Button
                    size='small'
                    type='primary'
                    theme='outline'
                    onClick={handleShow2FAModal}
                  >
                    {t('查看密钥')}
                  </Button>
                )}
                {batchExtra}
              </div>
            }
            showClear
          />
        )
      ) : (
        <>
          {inputs.type === 57 ? (
            <>
              <Form.TextArea
                field='key'
                label={
                  isEdit
                    ? t('密钥（编辑模式下，保存的密钥不会显示）')
                    : t('密钥')
                }
                placeholder={t(
                  '请输入 JSON 格式的 OAuth 凭据，例如：\n{\n  "access_token": "...",\n  "account_id": "..." \n}',
                )}
                rules={
                  isEdit
                    ? []
                    : [
                        {
                          required: true,
                          message: t('请输入密钥'),
                        },
                      ]
                }
                autoComplete='new-password'
                onChange={(value) => handleInputChange('key', value)}
                disabled={isIonetLocked}
                extraText={
                  <div className='flex flex-col gap-2'>
                    <Text type='tertiary' size='small'>
                      {t(
                        '仅支持 JSON 对象，必须包含 access_token 与 account_id',
                      )}
                    </Text>

                    <Space wrap spacing='tight'>
                      {isEdit && (
                        <Button
                          size='small'
                          type='primary'
                          theme='outline'
                          onClick={handleRefreshCodexCredential}
                          loading={codexCredentialRefreshing}
                          disabled={isIonetLocked}
                        >
                          {t('刷新凭证')}
                        </Button>
                      )}
                      <Button
                        size='small'
                        type='primary'
                        theme='outline'
                        onClick={() => formatJsonField('key')}
                        disabled={isIonetLocked}
                      >
                        {t('格式化')}
                      </Button>
                      {isEdit && (
                        <Button
                          size='small'
                          type='primary'
                          theme='outline'
                          onClick={handleShow2FAModal}
                          disabled={isIonetLocked}
                        >
                          {t('查看密钥')}
                        </Button>
                      )}
                      {batchExtra}
                    </Space>
                  </div>
                }
                autosize
                showClear
              />
            </>
          ) : inputs.type === 41 &&
            (inputs.vertex_key_type || 'json') === 'json' ? (
            <>
              {!batch && (
                <div className='flex items-center justify-between mb-3'>
                  <Text className='text-sm font-medium'>
                    {t('密钥输入方式')}
                  </Text>
                  <Space>
                    <Button
                      size='small'
                      type={!useManualInput ? 'primary' : 'tertiary'}
                      onClick={() => {
                        setUseManualInput(false);
                        // 切换到文件上传模式时清空手动输入的密钥
                        if (formApiRef.current) {
                          formApiRef.current.setValue('key', '');
                        }
                        handleInputChange('key', '');
                      }}
                    >
                      {t('文件上传')}
                    </Button>
                    <Button
                      size='small'
                      type={useManualInput ? 'primary' : 'tertiary'}
                      onClick={() => {
                        setUseManualInput(true);
                        // 切换到手动输入模式时清空文件上传相关状态
                        setVertexKeys([]);
                        setVertexFileList([]);
                        if (formApiRef.current) {
                          formApiRef.current.setValue('vertex_files', []);
                        }
                        setInputs((prev) => ({
                          ...prev,
                          vertex_files: [],
                        }));
                      }}
                    >
                      {t('手动输入')}
                    </Button>
                  </Space>
                </div>
              )}

              {batch && (
                <Banner
                  type='info'
                  description={t(
                    '批量创建模式下仅支持文件上传，不支持手动输入',
                  )}
                  className='!rounded-lg mb-3'
                />
              )}

              {useManualInput && !batch ? (
                <Form.TextArea
                  field='key'
                  label={
                    isEdit
                      ? t('密钥（编辑模式下，保存的密钥不会显示）')
                      : t('密钥')
                  }
                  placeholder={t(
                    '请输入 JSON 格式的密钥内容，例如：\n{\n  "type": "service_account",\n  "project_id": "your-project-id",\n  "private_key_id": "...",\n  "private_key": "...",\n  "client_email": "...",\n  "client_id": "...",\n  "auth_uri": "...",\n  "token_uri": "...",\n  "auth_provider_x509_cert_url": "...",\n  "client_x509_cert_url": "..."\n}',
                  )}
                  rules={
                    isEdit
                      ? []
                      : [
                          {
                            required: true,
                            message: t('请输入密钥'),
                          },
                        ]
                  }
                  autoComplete='new-password'
                  onChange={(value) => handleInputChange('key', value)}
                  extraText={
                    <div className='flex items-center gap-2'>
                      <Text type='tertiary' size='small'>
                        {t('请输入完整的 JSON 格式密钥内容')}
                      </Text>
                      {isEdit && isMultiKeyChannel && keyMode === 'append' && (
                        <Text type='warning' size='small'>
                          {t('追加模式：新密钥将添加到现有密钥列表的末尾')}
                        </Text>
                      )}
                      {isEdit && (
                        <Button
                          size='small'
                          type='primary'
                          theme='outline'
                          onClick={handleShow2FAModal}
                        >
                          {t('查看密钥')}
                        </Button>
                      )}
                      {batchExtra}
                    </div>
                  }
                  autosize
                  showClear
                />
              ) : (
                <Form.Upload
                  field='vertex_files'
                  label={t('密钥文件 (.json)')}
                  accept='.json'
                  draggable
                  dragIcon={<IconBolt />}
                  dragMainText={t('点击上传文件或拖拽文件到这里')}
                  dragSubText={t('仅支持 JSON 文件')}
                  style={{ marginTop: 10 }}
                  uploadTrigger='custom'
                  beforeUpload={() => false}
                  onChange={handleVertexUploadChange}
                  fileList={vertexFileList}
                  rules={
                    isEdit
                      ? []
                      : [
                          {
                            required: true,
                            message: t('请上传密钥文件'),
                          },
                        ]
                  }
                  extraText={batchExtra}
                />
              )}
            </>
          ) : (
            <Form.Input
              field='key'
              label={
                isEdit ? t('密钥（编辑模式下，保存的密钥不会显示）') : t('密钥')
              }
              placeholder={
                inputs.type === 33
                  ? inputs.aws_key_type === 'api_key'
                    ? t('请输入 API Key，格式：APIKey|Region')
                    : t('按照如下格式输入：AccessKey|SecretAccessKey|Region')
                  : t(type2secretPrompt(inputs.type))
              }
              rules={
                isEdit
                  ? []
                  : [
                      {
                        required: true,
                        message: t('请输入密钥'),
                      },
                    ]
              }
              autoComplete='new-password'
              onChange={(value) => handleInputChange('key', value)}
              extraText={
                <div className='flex items-center gap-2'>
                  {isEdit && isMultiKeyChannel && keyMode === 'append' && (
                    <Text type='warning' size='small'>
                      {t('追加模式：新密钥将添加到现有密钥列表的末尾')}
                    </Text>
                  )}
                  {isEdit && (
                    <Button
                      size='small'
                      type='primary'
                      theme='outline'
                      onClick={handleShow2FAModal}
                    >
                      {t('查看密钥')}
                    </Button>
                  )}
                  {batchExtra}
                </div>
              }
              showClear
            />
          )}
        </>
      )}

      {isEdit && isMultiKeyChannel && (
        <Form.Select
          field='key_mode'
          label={t('密钥更新模式')}
          placeholder={t('请选择密钥更新模式')}
          optionList={[
            { label: t('追加到现有密钥'), value: 'append' },
            { label: t('覆盖现有密钥'), value: 'replace' },
          ]}
          style={{ width: '100%' }}
          value={keyMode}
          onChange={(value) => setKeyMode(value)}
          extraText={
            <Text type='tertiary' size='small'>
              {keyMode === 'replace'
                ? t('覆盖模式：将完全替换现有的所有密钥')
                : t('追加模式：将新密钥添加到现有密钥列表末尾')}
            </Text>
          }
        />
      )}
      {batch && multiToSingle && (
        <>
          <Form.Select
            field='multi_key_mode'
            label={t('密钥聚合模式')}
            placeholder={t('请选择多密钥使用策略')}
            optionList={[
              { label: t('随机'), value: 'random' },
              { label: t('轮询'), value: 'polling' },
            ]}
            style={{ width: '100%' }}
            value={inputs.multi_key_mode || 'random'}
            onChange={(value) => {
              setMultiKeyMode(value);
              handleInputChange('multi_key_mode', value);
            }}
          />
          {inputs.multi_key_mode === 'polling' && (
            <Banner
              type='warning'
              description={t(
                '轮询模式必须搭配Redis和内存缓存功能使用，否则性能将大幅降低，并且无法实现轮询功能',
              )}
              className='!rounded-lg mt-2'
            />
          )}
        </>
      )}

      {inputs.type === 18 && (
        <Form.Input
          field='other'
          label={t('模型版本')}
          placeholder={
            '请输入星火大模型版本，注意是接口地址中的版本号，例如：v2.1'
          }
          onChange={(value) => handleInputChange('other', value)}
          showClear
        />
      )}

      {inputs.type === 41 && (
        <JSONEditor
          key={`region-${isEdit ? channelId : 'new'}`}
          field='other'
          label={t('部署地区')}
          placeholder={t(
            '请输入部署地区，例如：us-central1\n支持使用模型映射格式\n{\n    "default": "us-central1",\n    "claude-3-5-sonnet-20240620": "europe-west1"\n}',
          )}
          value={inputs.other || ''}
          onChange={(value) => handleInputChange('other', value)}
          rules={[{ required: true, message: t('请填写部署地区') }]}
          template={REGION_EXAMPLE}
          templateLabel={t('填入模板')}
          editorType='region'
          formApi={formApiRef.current}
          extraText={t('设置默认地区和特定模型的专用地区')}
        />
      )}

      {inputs.type === 21 && (
        <Form.Input
          field='other'
          label={t('知识库 ID')}
          placeholder={'请输入知识库 ID，例如：123456'}
          onChange={(value) => handleInputChange('other', value)}
          showClear
        />
      )}

      {inputs.type === 39 && (
        <Form.Input
          field='other'
          label='Account ID'
          placeholder={'请输入Account ID，例如：d6b5da8hk1awo8nap34ube6gh'}
          onChange={(value) => handleInputChange('other', value)}
          showClear
        />
      )}

      {inputs.type === 49 && (
        <Form.Input
          field='other'
          label={t('智能体ID')}
          placeholder={'请输入智能体ID，例如：7342866812345'}
          onChange={(value) => handleInputChange('other', value)}
          showClear
        />
      )}

      {inputs.type === 1 && (
        <Form.Input
          field='openai_organization'
          label={t('组织')}
          placeholder={t('请输入组织org-xxx')}
          showClear
          helpText={t('组织，不填则为默认组织')}
          onChange={(value) => handleInputChange('openai_organization', value)}
        />
      )}

      {/* API Configuration Section */}
      {showApiConfigCard && (
        <div onClick={handleApiConfigSecretClick}>
          {inputs.type === 40 && (
            <Banner
              type='info'
              description={
                <div>
                  <Text strong>{t('邀请链接')}:</Text>
                  <Text
                    link
                    underline
                    className='ml-2 cursor-pointer'
                    onClick={() =>
                      openHttpUrlInNewTab(
                        'https://cloud.siliconflow.cn/i/hij0YNTZ',
                      )
                    }
                  >
                    https://cloud.siliconflow.cn/i/hij0YNTZ
                  </Text>
                </div>
              }
              className='!rounded-lg'
            />
          )}

          {inputs.type === 3 && (
            <>
              <Banner
                type='warning'
                description={t(
                  '2025年5月10日后添加的渠道，不需要再在部署的时候移除模型名称中的"."',
                )}
                className='!rounded-lg'
              />
              <div>
                <Form.Input
                  field='base_url'
                  label='AZURE_OPENAI_ENDPOINT'
                  placeholder={t(
                    '请输入 AZURE_OPENAI_ENDPOINT，例如：https://docs-test-001.openai.azure.com',
                  )}
                  onChange={(value) => handleInputChange('base_url', value)}
                  showClear
                  disabled={isIonetLocked}
                />
              </div>
              <div>
                <Form.Input
                  field='other'
                  label={t('默认 API 版本')}
                  placeholder={t(
                    '请输入默认 API 版本，例如：2025-04-01-preview',
                  )}
                  onChange={(value) => handleInputChange('other', value)}
                  showClear
                />
              </div>
              <div>
                <Form.Input
                  field='azure_responses_version'
                  label={t('默认 Responses API 版本，为空则使用上方版本')}
                  placeholder={t('例如：preview')}
                  onChange={(value) =>
                    handleChannelOtherSettingsChange(
                      'azure_responses_version',
                      value,
                    )
                  }
                  showClear
                />
              </div>
            </>
          )}

          {inputs.type === 8 && (
            <>
              <Banner
                type='warning'
                description={t(
                  '如果你对接的是上游One API或者New API等转发项目，请使用OpenAI类型，不要使用此类型，除非你知道你在做什么。',
                )}
                className='!rounded-lg'
              />
              <div>
                <Form.Input
                  field='base_url'
                  label={t('完整的 Base URL，支持变量{model}')}
                  placeholder={t(
                    '请输入完整的URL，例如：https://api.openai.com/v1/chat/completions',
                  )}
                  onChange={(value) => handleInputChange('base_url', value)}
                  showClear
                  disabled={isIonetLocked}
                />
              </div>
            </>
          )}

          {inputs.type === 37 && (
            <Banner
              type='warning'
              description={t(
                'Dify渠道只适配chatflow和agent，并且agent不支持图片！',
              )}
              className='!rounded-lg'
            />
          )}

          {inputs.type !== 3 &&
            inputs.type !== 8 &&
            inputs.type !== 22 &&
            inputs.type !== 36 &&
            (inputs.type !== 45 || doubaoApiEditUnlocked) && (
              <div>
                <Form.Input
                  field='base_url'
                  label={t('API地址')}
                  placeholder={t(
                    '此项可选，用于通过自定义API地址来进行 API 调用，末尾不要带/v1和/',
                  )}
                  onChange={(value) => handleInputChange('base_url', value)}
                  showClear
                  disabled={isIonetLocked}
                  extraText={t(
                    '对于官方渠道，new-api已经内置地址，除非是第三方代理站点或者Azure的特殊接入地址，否则不需要填写',
                  )}
                />
              </div>
            )}

          {inputs.type === 22 && (
            <div>
              <Form.Input
                field='base_url'
                label={t('私有部署地址')}
                placeholder={t(
                  '请输入私有部署地址，格式为：https://fastgpt.run/api/openapi',
                )}
                onChange={(value) => handleInputChange('base_url', value)}
                showClear
                disabled={isIonetLocked}
              />
            </div>
          )}

          {inputs.type === 36 && (
            <div>
              <Form.Input
                field='base_url'
                label={t(
                  '注意非Chat API，请务必填写正确的API地址，否则可能导致无法使用',
                )}
                placeholder={t(
                  '请输入到 /suno 前的路径，通常就是域名，例如：https://api.example.com',
                )}
                onChange={(value) => handleInputChange('base_url', value)}
                showClear
                disabled={isIonetLocked}
              />
            </div>
          )}

          {inputs.type === 45 && !doubaoApiEditUnlocked && (
            <div>
              <Form.Select
                field='base_url'
                label={t('API地址')}
                placeholder={t('请选择API地址')}
                onChange={(value) => handleInputChange('base_url', value)}
                optionList={[
                  {
                    value: 'https://ark.cn-beijing.volces.com',
                    label: 'https://ark.cn-beijing.volces.com',
                  },
                  {
                    value: 'https://ark.ap-southeast.bytepluses.com',
                    label: 'https://ark.ap-southeast.bytepluses.com',
                  },
                  {
                    value: DEPRECATED_DOUBAO_CODING_PLAN_BASE_URL,
                    label: doubaoCodingPlanOptionLabel,
                    disabled: !canKeepDeprecatedDoubaoCodingPlan,
                  },
                ]}
                defaultValue='https://ark.cn-beijing.volces.com'
                disabled={isIonetLocked}
              />
            </div>
          )}
        </div>
      )}

      {/* Model Selection - Part of Core Config */}
      <Form.Select
        field='models'
        label={t('模型')}
        placeholder={t('请选择该渠道所支持的模型')}
        rules={[{ required: true, message: t('请选择模型') }]}
        multiple
        filter={selectFilter}
        allowCreate
        autoClearSearchValue={false}
        searchPosition='dropdown'
        optionList={modelOptions}
        onSearch={(value) => setModelSearchValue(value)}
        innerBottomSlot={
          modelSearchHintText ? (
            <Text className='px-3 py-2 block text-xs !text-semi-color-text-2'>
              {modelSearchHintText}
            </Text>
          ) : null
        }
        style={{ width: '100%' }}
        onChange={(value) => handleInputChange('models', value)}
        renderSelectedItem={(optionNode) => {
          const modelName = String(optionNode?.value ?? '');
          return {
            isRenderInTag: true,
            content: (
              <span
                className='cursor-pointer select-none'
                role='button'
                tabIndex={0}
                title={t('点击复制模型名称')}
                onClick={async (e) => {
                  e.stopPropagation();
                  const ok = await copy(modelName);
                  if (ok) {
                    showSuccess(
                      t('已复制：{{name}}', {
                        name: modelName,
                      }),
                    );
                  } else {
                    showError(t('复制失败'));
                  }
                }}
              >
                {optionNode.label || modelName}
              </span>
            ),
          };
        }}
        extraText={
          <Space>
            <Button
              size='small'
              type='primary'
              onClick={() => handleInputChange('models', basicModels)}
            >
              {t('填入相关模型')}
            </Button>
            {MODEL_FETCHABLE_CHANNEL_TYPES.has(inputs.type) && (
              <Button
                size='small'
                type='tertiary'
                onClick={() => fetchUpstreamModelList('models')}
              >
                {t('获取模型列表')}
              </Button>
            )}
            <Dropdown
              trigger='click'
              position='bottomRight'
              menu={[
                {
                  node: 'item',
                  name: t('填入所有模型'),
                  onClick: () => handleInputChange('models', fullModels),
                },
                ...(inputs.type === 4 && isEdit
                  ? [
                      {
                        node: 'item',
                        name: t('Ollama 模型管理'),
                        onClick: () => setOllamaModalVisible(true),
                      },
                    ]
                  : []),
                { node: 'divider' },
                {
                  node: 'item',
                  name: t('复制所有模型'),
                  onClick: () => {
                    if (inputs.models.length === 0) {
                      showInfo(t('没有模型可以复制'));
                      return;
                    }
                    try {
                      copy(inputs.models.join(','));
                      showSuccess(t('模型列表已复制到剪贴板'));
                    } catch (error) {
                      showError(t('复制失败'));
                    }
                  },
                },
                {
                  node: 'item',
                  name: t('清除所有模型'),
                  type: 'danger',
                  onClick: () => handleInputChange('models', []),
                },
                ...(modelGroups && modelGroups.length > 0
                  ? [
                      { node: 'divider' },
                      ...modelGroups.map((group) => ({
                        node: 'item',
                        name: group.name,
                        onClick: () => {
                          let items = [];
                          try {
                            if (Array.isArray(group.items)) {
                              items = group.items;
                            } else if (typeof group.items === 'string') {
                              const parsed = JSON.parse(group.items || '[]');
                              if (Array.isArray(parsed)) items = parsed;
                            }
                          } catch {}
                          const current =
                            formApiRef.current?.getValue('models') ||
                            inputs.models ||
                            [];
                          const merged = Array.from(
                            new Set(
                              [...current, ...items]
                                .map((m) => (m || '').trim())
                                .filter(Boolean),
                            ),
                          );
                          handleInputChange('models', merged);
                        },
                      })),
                    ]
                  : []),
              ]}
            >
              <Button size='small' type='tertiary'>
                {t('更多')} <IconChevronDown size={12} />
              </Button>
            </Dropdown>
          </Space>
        }
      />

      {/* Custom Model Name - Core Config */}
      <Form.Input
        field='custom_model'
        label={t('自定义模型名称')}
        placeholder={t('输入自定义模型名称')}
        onChange={(value) => setCustomModel(value.trim())}
        value={customModel}
        suffix={
          <Button size='small' type='primary' onClick={addCustomModels}>
            {t('填入')}
          </Button>
        }
      />

      {/* Groups - Core Config */}
      <Form.Select
        field='groups'
        label={t('分组')}
        placeholder={t('请选择可以使用该渠道的分组')}
        multiple
        allowAdditions
        additionLabel={t('请在系统设置页面编辑分组倍率以添加新的分组：')}
        optionList={groupOptions}
        style={{ width: '100%' }}
        position='top'
        onChange={(value) => handleInputChange('groups', value)}
      />

      {/* Model Mapping - Core Config */}
      <JSONEditor
        key={`model_mapping-${isEdit ? channelId : 'new'}`}
        field='model_mapping'
        label={t('模型重定向')}
        placeholder={
          t(
            '此项可选，用于修改请求体中的模型名称，为一个 JSON 字符串，键为请求中模型名称，值为要替换的模型名称，例如：',
          ) + `\n${JSON.stringify(MODEL_MAPPING_EXAMPLE, null, 2)}`
        }
        value={inputs.model_mapping || ''}
        onChange={(value) => handleInputChange('model_mapping', value)}
        template={MODEL_MAPPING_EXAMPLE}
        templateLabel={t('填入模板')}
        editorType='keyValue'
        formApi={formApiRef.current}
        renderStringValueSuffix={({ pairKey, value }) => {
          if (!MODEL_FETCHABLE_CHANNEL_TYPES.has(inputs.type)) {
            return null;
          }
          const disabled = !String(pairKey ?? '').trim();
          return (
            <Tooltip content={t('选择模型')}>
              <Button
                type='tertiary'
                theme='borderless'
                size='small'
                icon={<IconSearch size={14} />}
                disabled={disabled}
                onClick={(e) => {
                  e.stopPropagation();
                  openModelMappingValueModal({
                    pairKey,
                    value,
                  });
                }}
              />
            </Tooltip>
          );
        }}
        extraText={t('键为请求中的模型名称，值为要替换的模型名称')}
      />

      {/* Auto Ban - Core Config */}
      <Form.Switch
        field='auto_ban'
        label={t('是否自动禁用')}
        checkedText={t('开')}
        uncheckedText={t('关')}
        onChange={(value) => setAutoBan(value)}
        extraText={t('仅当自动禁用开启时有效，关闭后不会自动禁用该渠道')}
        initValue={autoBan}
      />

      {/* Test Model - Core Config */}
      <Form.Input
        field='test_model'
        label={t('默认测试模型')}
        placeholder={t('不填则为模型列表第一个')}
        onChange={(value) => handleInputChange('test_model', value)}
        showClear
      />
    </Card>
  );
}
