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
  SideSheet,
  Space,
  Spin,
  Button,
  Typography,
  Checkbox,
  Banner,
  Modal,
  ImagePreview,
  Card,
  Tag,
  Avatar,
  Form,
  Row,
  Col,
  Tooltip,
  Collapse,
  Dropdown,
} from '@douyinfe/semi-ui';
import {
  IconSave,
  IconClose,
  IconServer,
  IconSetting,
  IconCode,
  IconCopy,
  IconGlobe,
  IconBolt,
  IconSearch,
  IconChevronDown,
} from '@douyinfe/semi-icons';
import { showError, showSuccess } from '../../../../../helpers';
import { MODEL_FETCHABLE_CHANNEL_TYPES } from '../../../../../constants';
import ModelSelectModal from '../ModelSelectModal';
import SingleModelSelectModal from '../SingleModelSelectModal';
import OllamaModelModal from '../OllamaModelModal';
import ParamOverrideEditorModal from '../ParamOverrideEditorModal';
import JSONEditor from '../../../../common/ui/JSONEditor';
import SecureVerificationModal from '../../../../common/modals/SecureVerificationModal';
import StatusCodeRiskGuardModal from '../StatusCodeRiskGuardModal';
import ChannelKeyDisplay from '../../../../common/ui/ChannelKeyDisplay';
import { useEditChannelEditor } from './EditorContext';
import AdvancedSettingsFields from './AdvancedSettingsFields';
import ChannelCoreSettings from './ChannelCoreSettings';

const { Text, Title } = Typography;
import {
  MODEL_MAPPING_EXAMPLE,
  STATUS_CODE_MAPPING_EXAMPLE,
  REGION_EXAMPLE,
  DEPRECATED_DOUBAO_CODING_PLAN_BASE_URL,
  type2secretPrompt,
} from './constants';

export default function EditChannelEditorView() {
  const {
    addCustomModels,
    advancedSettingsOpen,
    applyClipboardConfig,
    applyParamOverrideTemplate,
    autoBan,
    basicModels,
    batch,
    batchExtra,
    canKeepDeprecatedDoubaoCodingPlan,
    cancelVerification,
    channelId,
    channelOptionList,
    clearParamOverride,
    clipboardConfig,
    codexCredentialRefreshing,
    copyParamOverrideJson,
    customModel,
    doubaoApiEditUnlocked,
    doubaoCodingPlanOptionLabel,
    executeVerification,
    fetchUpstreamModelList,
    fetchedModels,
    formApiRef,
    formContainerRef,
    formatJsonField,
    formatUnixTime,
    fullModels,
    groupOptions,
    handleApiConfigSecretClick,
    handleCancel,
    handleChannelOtherSettingsChange,
    handleChannelSettingsChange,
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
    isMobile,
    isModalOpenurl,
    isModalVisible,
    isMultiKeyChannel,
    keyDisplayState,
    keyMode,
    loading,
    modalImageUrl,
    modelGroups,
    modelMappingValueKey,
    modelMappingValueModalModels,
    modelMappingValueModalVisible,
    modelMappingValueSelected,
    modelModalVisible,
    modelOptions,
    modelSearchHintText,
    multiToSingle,
    ollamaModalVisible,
    openModelMappingValueModal,
    originInputs,
    paramOverrideEditorVisible,
    paramOverrideMeta,
    pasteFromClipboard,
    props,
    redirectModelKeyList,
    redirectModelList,
    renderChannelOption,
    resetKeyDisplayState,
    resolveStatusCodeRiskConfirm,
    setAdvancedSettingsOpen,
    setAutoBan,
    setBatch,
    setChannelSearchValue,
    setClipboardConfig,
    setCustomModel,
    setInputs,
    setIsEnterpriseAccount,
    setIsModalOpenurl,
    setKeyMode,
    setModelMappingValueModalVisible,
    setModelModalVisible,
    setModelSearchValue,
    setMultiKeyMode,
    setOllamaModalVisible,
    setParamOverrideEditorVisible,
    setUseManualInput,
    setVerificationCode,
    setVertexFileList,
    setVertexKeys,
    showApiConfigCard,
    statusCodeRiskConfirmVisible,
    statusCodeRiskDetailItems,
    submit,
    switchVerificationMethod,
    t,
    toggleAdvancedSettings,
    upstreamDetectedModels,
    upstreamDetectedModelsOmittedCount,
    upstreamDetectedModelsPreview,
    useManualInput,
    verificationMethods,
    verificationState,
    vertexFileList,
  } = useEditChannelEditor();

  return (
    <>
      <SideSheet
        placement={isEdit ? 'right' : 'left'}
        title={
          <div className='flex items-center justify-between w-full'>
            <Space>
              <Tag color='blue' shape='circle'>
                {isEdit ? t('编辑') : t('新建')}
              </Tag>
              <Title heading={4} className='m-0'>
                {isEdit ? t('更新渠道信息') : t('创建新的渠道')}
              </Title>
            </Space>
            {!isEdit && (
              <Button
                size='small'
                type='tertiary'
                className='ec-dbcd0a3c01b55203 shrink-0'
                icon={<IconBolt />}
                onClick={pasteFromClipboard}
              >
                {t('从剪贴板粘贴配置')}
              </Button>
            )}
          </div>
        }
        bodyStyle={{ padding: '0' }}
        visible={props.visible}
        width={isMobile ? '100%' : 600}
        footer={
          <div className='flex justify-end items-center gap-2'>
            <Button
              theme='solid'
              onClick={() => formApiRef.current?.submitForm()}
              icon={<IconSave />}
            >
              {t('提交')}
            </Button>
            <Button
              theme='light'
              type='primary'
              onClick={handleCancel}
              icon={<IconClose />}
            >
              {t('取消')}
            </Button>
          </div>
        }
        closeIcon={null}
        onCancel={() => handleCancel()}
      >
        <Form
          key={isEdit ? 'edit' : 'new'}
          initValues={originInputs}
          getFormApi={(api) => (formApiRef.current = api)}
          onSubmit={submit}
        >
          {() => {
            const advancedSettingsContent = <AdvancedSettingsFields />;

            return (
              <>
                <Spin spinning={loading}>
                  <div className='p-2 space-y-3' ref={formContainerRef}>
                    {!isEdit && clipboardConfig && (
                      <Banner
                        type='info'
                        className='ec-dbcd0a3c01b55203'
                        description={
                          <div className='flex items-center justify-between gap-2'>
                            <span>{t('检测到剪贴板中的连接信息')}</span>
                            <div className='flex gap-1'>
                              <Button
                                size='small'
                                theme='solid'
                                type='primary'
                                onClick={() =>
                                  applyClipboardConfig(clipboardConfig)
                                }
                              >
                                {t('自动填入')}
                              </Button>
                              <Button
                                size='small'
                                type='tertiary'
                                onClick={() => setClipboardConfig(null)}
                              >
                                {t('忽略')}
                              </Button>
                            </div>
                          </div>
                        }
                      />
                    )}
                    {/* Core Configuration Card - Always Visible */}
                    <ChannelCoreSettings />

                    {/* Advanced Settings Toggle / Collapse */}
                    {isMobile ? (
                      <Collapse
                        activeKey={advancedSettingsOpen ? ['advanced'] : []}
                        onChange={(keys) =>
                          toggleAdvancedSettings(keys.includes('advanced'))
                        }
                      >
                        <Collapse.Panel
                          header={
                            <div className='flex items-center gap-2'>
                              <IconSetting size={16} />
                              <Text className='font-medium'>
                                {t('高级设置')}
                              </Text>
                            </div>
                          }
                          itemKey='advanced'
                        >
                          {advancedSettingsContent}
                        </Collapse.Panel>
                      </Collapse>
                    ) : (
                      /* Desktop: toggle button to open side panel */
                      <div
                        className='flex items-center justify-between p-3 rounded-xl cursor-pointer transition-colors hover:bg-gray-50'
                        style={{
                          backgroundColor: advancedSettingsOpen
                            ? 'var(--semi-color-primary-light-default)'
                            : 'var(--semi-color-fill-0)',
                          border: '1px solid var(--semi-color-fill-2)',
                        }}
                        onClick={() =>
                          toggleAdvancedSettings(!advancedSettingsOpen)
                        }
                      >
                        <div className='flex items-center gap-2'>
                          <IconSetting size={16} />
                          <Text className='font-medium'>{t('高级设置')}</Text>
                        </div>
                        <div
                          className='flex items-center gap-1 text-sm'
                          style={{ color: 'var(--semi-color-primary)' }}
                        >
                          <Text
                            size='small'
                            style={{ color: 'var(--semi-color-primary)' }}
                          >
                            {advancedSettingsOpen
                              ? t('收起')
                              : isEdit
                                ? t('向左展开')
                                : t('向右展开')}
                          </Text>
                          <IconChevronDown
                            size={14}
                            style={{
                              transform: advancedSettingsOpen
                                ? 'rotate(180deg)'
                                : isEdit
                                  ? 'rotate(90deg)'
                                  : 'rotate(-90deg)',
                              transition: 'transform 0.2s',
                            }}
                          />
                        </div>
                      </div>
                    )}
                  </div>
                </Spin>

                {/* Desktop: Advanced Settings Side Panel - rendered inside Form tree */}
                {!isMobile && advancedSettingsOpen && (
                  <div
                    className='fixed top-0 h-full overflow-y-auto z-[999] semi-sidesheet-inner'
                    style={{
                      width: 600,
                      [isEdit ? 'right' : 'left']: 600,
                      backgroundColor: 'var(--semi-color-bg-0)',
                      borderLeft: isEdit
                        ? 'none'
                        : '1px solid var(--semi-color-border)',
                      borderRight: isEdit
                        ? '1px solid var(--semi-color-border)'
                        : 'none',
                      animation: `slideIn${isEdit ? 'Left' : 'Right'} 0.3s ease-out`,
                    }}
                  >
                    <div className='semi-sidesheet-header'>
                      <div className='semi-sidesheet-title'>
                        <Space>
                          <Tag color='cyan' shape='circle'>
                            {t('高级')}
                          </Tag>
                          <Title heading={4} className='m-0'>
                            {t('高级设置')}
                          </Title>
                        </Space>
                      </div>
                      <Button
                        className='semi-sidesheet-close'
                        type='tertiary'
                        theme='borderless'
                        icon={<IconClose />}
                        size='small'
                        onClick={() => setAdvancedSettingsOpen(false)}
                      />
                    </div>
                    <div className='semi-sidesheet-body' style={{ padding: 0 }}>
                      <div className='p-2 space-y-3'>
                        <Card className='!rounded-2xl shadow-sm border-0'>
                          <div className='flex items-center mb-4'>
                            <Avatar
                              size='small'
                              color='orange'
                              className='mr-2 shadow-md'
                            >
                              <IconSetting size={16} />
                            </Avatar>
                            <div>
                              <Text className='text-lg font-medium'>
                                {t('高级设置')}
                              </Text>
                              <div className='text-xs text-gray-600'>
                                {t('渠道的高级配置选项')}
                              </div>
                            </div>
                          </div>
                          {advancedSettingsContent}
                        </Card>
                      </div>
                    </div>
                  </div>
                )}
              </>
            );
          }}
        </Form>

        <ImagePreview
          src={modalImageUrl}
          visible={isModalOpenurl}
          onVisibleChange={(visible) => setIsModalOpenurl(visible)}
        />
      </SideSheet>
      <StatusCodeRiskGuardModal
        visible={statusCodeRiskConfirmVisible}
        detailItems={statusCodeRiskDetailItems}
        onCancel={() => resolveStatusCodeRiskConfirm(false)}
        onConfirm={() => resolveStatusCodeRiskConfirm(true)}
      />
      {/* 使用通用安全验证模态框 */}
      <SecureVerificationModal
        visible={isModalVisible}
        verificationMethods={verificationMethods}
        verificationState={verificationState}
        onVerify={executeVerification}
        onCancel={cancelVerification}
        onCodeChange={setVerificationCode}
        onMethodSwitch={switchVerificationMethod}
        title={verificationState.title}
        description={verificationState.description}
      />

      {/* 使用ChannelKeyDisplay组件显示密钥 */}
      <Modal
        title={
          <div className='flex items-center'>
            <div className='w-8 h-8 rounded-full bg-green-100 dark:bg-green-900 flex items-center justify-center mr-3'>
              <svg
                className='w-4 h-4 text-green-600 dark:text-green-400'
                fill='currentColor'
                viewBox='0 0 20 20'
              >
                <path
                  fillRule='evenodd'
                  d='M5 9V7a5 5 0 0110 0v2a2 2 0 012 2v5a2 2 0 01-2 2H5a2 2 0 01-2-2v-5a2 2 0 012-2zm8-2v2H7V7a3 3 0 016 0z'
                  clipRule='evenodd'
                />
              </svg>
            </div>
            {t('渠道密钥信息')}
          </div>
        }
        visible={keyDisplayState.showModal}
        onCancel={resetKeyDisplayState}
        footer={
          <Button type='primary' onClick={resetKeyDisplayState}>
            {t('完成')}
          </Button>
        }
        width={700}
        style={{ maxWidth: '90vw' }}
      >
        <ChannelKeyDisplay
          keyData={keyDisplayState.keyData}
          showSuccessIcon={true}
          successText={t('密钥获取成功')}
          showWarning={true}
          warningText={t(
            '请妥善保管密钥信息，不要泄露给他人。如有安全疑虑，请及时更换密钥。',
          )}
        />
      </Modal>

      <ParamOverrideEditorModal
        visible={paramOverrideEditorVisible}
        value={inputs.param_override || ''}
        onCancel={() => setParamOverrideEditorVisible(false)}
        onSave={(nextValue) => {
          handleInputChange('param_override', nextValue);
          setParamOverrideEditorVisible(false);
        }}
      />

      <ModelSelectModal
        visible={modelModalVisible}
        models={fetchedModels}
        selected={inputs.models}
        redirectModels={redirectModelList}
        redirectSourceModels={redirectModelKeyList}
        onConfirm={(selectedModels) => {
          handleInputChange('models', selectedModels);
          showSuccess(t('模型列表已更新'));
          setModelModalVisible(false);
        }}
        onCancel={() => setModelModalVisible(false)}
      />

      <SingleModelSelectModal
        visible={modelMappingValueModalVisible}
        models={modelMappingValueModalModels}
        selected={modelMappingValueSelected}
        onConfirm={(selectedModel) => {
          const modelName = String(selectedModel ?? '').trim();
          if (!modelName) {
            showError(t('请先选择模型！'));
            return;
          }

          const mappingKey = String(modelMappingValueKey ?? '').trim();
          if (!mappingKey) {
            setModelMappingValueModalVisible(false);
            return;
          }

          let parsed = {};
          const currentMapping = inputs.model_mapping;
          if (typeof currentMapping === 'string' && currentMapping.trim()) {
            try {
              parsed = JSON.parse(currentMapping);
            } catch (error) {
              parsed = {};
            }
          } else if (
            currentMapping &&
            typeof currentMapping === 'object' &&
            !Array.isArray(currentMapping)
          ) {
            parsed = currentMapping;
          }
          if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            parsed = {};
          }

          parsed[mappingKey] = modelName;
          const nextMapping = JSON.stringify(parsed, null, 2);
          handleInputChange('model_mapping', nextMapping);
          if (formApiRef.current) {
            formApiRef.current.setValue('model_mapping', nextMapping);
          }
          setModelMappingValueModalVisible(false);
        }}
        onCancel={() => setModelMappingValueModalVisible(false)}
      />

      <OllamaModelModal
        visible={ollamaModalVisible}
        onCancel={() => setOllamaModalVisible(false)}
        channelId={channelId}
        channelInfo={inputs}
        onModelsUpdate={(options = {}) => {
          // 当模型更新后，重新获取模型列表以更新表单
          fetchUpstreamModelList('models', { silent: !!options.silent });
        }}
        onApplyModels={({ mode, modelIds } = {}) => {
          if (!Array.isArray(modelIds) || modelIds.length === 0) {
            return;
          }
          const existingModels = Array.isArray(inputs.models)
            ? inputs.models.map(String)
            : [];
          const incoming = modelIds.map(String);
          const nextModels = Array.from(
            new Set([...existingModels, ...incoming]),
          );

          handleInputChange('models', nextModels);
          if (formApiRef.current) {
            formApiRef.current.setValue('models', nextModels);
          }
          showSuccess(t('模型列表已追加更新'));
        }}
      />
    </>
  );
}
