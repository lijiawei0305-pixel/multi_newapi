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
  Col,
  Collapse,
  Input,
  Modal,
  Row,
  Select,
  Space,
  Switch,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconMenu, IconPlus } from '@douyinfe/semi-icons';
import {
  CONDITION_MODE_OPTIONS,
  FIELD_GUIDE_TARGET_OPTIONS,
  HEADER_VALUE_JSONC_EXAMPLE,
  LEGACY_TEMPLATE,
  MODE_DESCRIPTIONS,
  MODE_META,
  OPERATION_MODE_LABEL_MAP,
  OPERATION_MODE_OPTIONS,
  OPERATION_TEMPLATE,
  SYNC_TARGET_TYPE_OPTIONS,
  TEMPLATE_GROUP_OPTIONS,
  buildSyncTargetSpec,
  getModeFromLabel,
  getModeFromPlaceholder,
  getModePathLabel,
  getModePathPlaceholder,
  getModeToLabel,
  getModeToPlaceholder,
  getModeValueLabel,
  getModeValuePlaceholder,
  getOperationModeTagColor,
  getOperationSummary,
  parseSyncTargetSpec,
} from './editorDomain';
import { useParamOverrideEditor } from './EditorContext';
import ParamOverrideAuxiliaryModals from './AuxiliaryModals';
import ParamOverrideRulesNavigator from './RulesNavigator';
import ParamOverrideRuleDetails from './RuleDetails';

const { Text } = Typography;

export default function ParamOverrideEditorView() {
  const {
    addCondition,
    addOperation,
    addPruneRule,
    appendTemplateFromLibrary,
    applyBuiltinField,
    collapseAllSelectedConditions,
    copyBuiltinField,
    dragOverOperationId,
    dragOverPosition,
    draggedOperationId,
    duplicateOperation,
    editMode,
    expandAllSelectedConditions,
    fieldGuideActionLabel,
    fieldGuideFieldCount,
    fieldGuideKeyword,
    fieldGuideTarget,
    fieldGuideVisible,
    fillTemplateFromLibrary,
    filteredFieldGuideSections,
    filteredOperations,
    formatJson,
    formatSelectedOperationValueAsJson,
    handleConditionCollapseChange,
    handleJsonChange,
    handleOperationDragOver,
    handleOperationDragStart,
    handleOperationDrop,
    handleSave,
    headerValueExampleVisible,
    jsonError,
    jsonText,
    legacyValue,
    onCancel,
    operationCount,
    operationSearch,
    operations,
    pruneObjectsDraft,
    removeCondition,
    removeOperation,
    removePruneRule,
    resetEditorState,
    resetOperationDragState,
    returnErrorDraft,
    selectedConditionKeys,
    selectedOperation,
    selectedOperationId,
    selectedOperationIndex,
    setFieldGuideKeyword,
    setFieldGuideTarget,
    setFieldGuideVisible,
    setHeaderValueExampleVisible,
    setLegacyValue,
    setOperationSearch,
    setSelectedOperationId,
    setTemplateGroupKey,
    setTemplatePresetKey,
    switchToJsonMode,
    switchToVisualMode,
    t,
    templateGroupKey,
    templatePresetKey,
    templatePresetOptions,
    topOperationModes,
    updateCondition,
    updateOperation,
    updatePruneObjectsDraft,
    updatePruneRule,
    updateReturnErrorDraft,
    visible,
    visualMode,
    visualValidationError,
  } = useParamOverrideEditor();

  return (
    <>
      <Modal
        title={t('参数覆盖')}
        visible={visible}
        width={1120}
        bodyStyle={{ maxHeight: '76vh', overflowY: 'auto', paddingTop: 10 }}
        onCancel={onCancel}
        onOk={handleSave}
        okText={t('保存')}
        cancelText={t('取消')}
      >
        <Space vertical align='start' spacing={14} style={{ width: '100%' }}>
          <Card
            className='!rounded-xl !border-0 w-full'
            bodyStyle={{
              padding: 12,
              background: 'var(--semi-color-fill-0)',
            }}
          >
            <div className='flex items-start justify-between gap-3'>
              <Space wrap spacing={8}>
                <Tag color='grey'>{t('编辑方式')}</Tag>
                <Button
                  type={editMode === 'visual' ? 'primary' : 'tertiary'}
                  onClick={switchToVisualMode}
                >
                  {t('可视化')}
                </Button>
                <Button
                  type={editMode === 'json' ? 'primary' : 'tertiary'}
                  onClick={switchToJsonMode}
                >
                  {t('JSON 文本')}
                </Button>
                <Tag color='grey'>{t('模板')}</Tag>
                <Select
                  value={templateGroupKey}
                  optionList={TEMPLATE_GROUP_OPTIONS}
                  onChange={(nextValue) =>
                    setTemplateGroupKey(nextValue || 'basic')
                  }
                  style={{ width: 120 }}
                />
                <Select
                  value={templatePresetKey}
                  optionList={templatePresetOptions}
                  onChange={(nextValue) =>
                    setTemplatePresetKey(nextValue || 'operations_default')
                  }
                  style={{ width: 260 }}
                />
                <Button onClick={fillTemplateFromLibrary}>
                  {t('填充模板')}
                </Button>
                <Button type='tertiary' onClick={appendTemplateFromLibrary}>
                  {t('追加模板')}
                </Button>
                <Button type='tertiary' onClick={resetEditorState}>
                  {t('重置')}
                </Button>
              </Space>
            </div>
          </Card>

          {editMode === 'visual' ? (
            <div style={{ width: '100%' }}>
              {visualMode === 'legacy' ? (
                <Card
                  className='!rounded-2xl !border-0'
                  bodyStyle={{
                    padding: 14,
                    background: 'var(--semi-color-fill-0)',
                  }}
                >
                  <Text className='mb-2 block'>{t('旧格式（JSON 对象）')}</Text>
                  <TextArea
                    value={legacyValue}
                    autosize={{ minRows: 10, maxRows: 20 }}
                    placeholder={JSON.stringify(LEGACY_TEMPLATE, null, 2)}
                    onChange={(nextValue) => setLegacyValue(nextValue)}
                    showClear
                  />
                  <Text type='tertiary' size='small' className='mt-2 block'>
                    {t('这里直接编辑 JSON 对象。适合简单覆盖参数的场景。')}
                  </Text>
                </Card>
              ) : (
                <div>
                  <div className='flex items-center justify-between mb-3'>
                    <Space>
                      <Text>{t('新格式（规则 + 条件）')}</Text>
                      <Tag color='cyan'>{`${t('规则')}: ${operationCount}`}</Tag>
                    </Space>
                    <Button icon={<IconPlus />} onClick={addOperation}>
                      {t('新增规则')}
                    </Button>
                  </div>

                  <Row gutter={12}>
                    <ParamOverrideRulesNavigator />
                    <ParamOverrideRuleDetails />
                  </Row>
                </div>
              )}
            </div>
          ) : (
            <div style={{ width: '100%' }}>
              <Space style={{ marginBottom: 8 }} wrap>
                <Button onClick={formatJson}>{t('格式化')}</Button>
                <Tag color='grey'>{t('高级文本编辑')}</Tag>
              </Space>
              <TextArea
                value={jsonText}
                autosize={{ minRows: 18, maxRows: 28 }}
                onChange={(nextValue) => handleJsonChange(nextValue ?? '')}
                placeholder={JSON.stringify(OPERATION_TEMPLATE, null, 2)}
                showClear
              />
              <Text type='tertiary' size='small' className='mt-2 block'>
                {t('直接编辑 JSON 文本，保存时会校验格式。')}
              </Text>
              {jsonError ? (
                <Text className='text-red-500 text-xs mt-2'>{jsonError}</Text>
              ) : null}
            </div>
          )}
        </Space>
      </Modal>

      <ParamOverrideAuxiliaryModals />
    </>
  );
}
