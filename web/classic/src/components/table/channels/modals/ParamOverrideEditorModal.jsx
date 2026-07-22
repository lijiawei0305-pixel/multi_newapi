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

import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { useTranslation } from 'react-i18next';
import { copy, showError, showSuccess, verifyJSON } from '../../../../helpers';
import {
  BUILTIN_FIELD_SECTIONS,
  MODE_META,
  TEMPLATE_PRESET_CONFIG,
  buildConditionPayload,
  buildPruneObjectsValueText,
  buildReturnErrorValueText,
  buildSyncTargetSpec,
  createDefaultCondition,
  createDefaultOperation,
  isOperationBlank,
  normalizeOperation,
  normalizePruneRule,
  parseInitialState,
  parseLooseValue,
  parsePruneObjectsDraft,
  parseReturnErrorDraft,
  reorderOperations,
  validateOperations,
} from './param-override/editorDomain';
import { ParamOverrideEditorContext } from './param-override/EditorContext';
import ParamOverrideEditorView from './param-override/EditorView';

const ParamOverrideEditorModal = ({ visible, value, onSave, onCancel }) => {
  const { t } = useTranslation();

  const [editMode, setEditMode] = useState('visual');
  const [visualMode, setVisualMode] = useState('operations');
  const [legacyValue, setLegacyValue] = useState('');
  const [operations, setOperations] = useState([createDefaultOperation()]);
  const [jsonText, setJsonText] = useState('');
  const [jsonError, setJsonError] = useState('');
  const [operationSearch, setOperationSearch] = useState('');
  const [selectedOperationId, setSelectedOperationId] = useState('');
  const [expandedConditionMap, setExpandedConditionMap] = useState({});
  const [draggedOperationId, setDraggedOperationId] = useState('');
  const [dragOverOperationId, setDragOverOperationId] = useState('');
  const [dragOverPosition, setDragOverPosition] = useState('before');
  const [templateGroupKey, setTemplateGroupKey] = useState('basic');
  const [templatePresetKey, setTemplatePresetKey] =
    useState('operations_default');
  const [headerValueExampleVisible, setHeaderValueExampleVisible] =
    useState(false);
  const [fieldGuideVisible, setFieldGuideVisible] = useState(false);
  const [fieldGuideTarget, setFieldGuideTarget] = useState('path');
  const [fieldGuideKeyword, setFieldGuideKeyword] = useState('');

  useEffect(() => {
    if (!visible) return;
    const nextState = parseInitialState(value);
    setEditMode(nextState.editMode);
    setVisualMode(nextState.visualMode);
    setLegacyValue(nextState.legacyValue);
    setOperations(nextState.operations);
    setJsonText(nextState.jsonText);
    setJsonError(nextState.jsonError);
    setOperationSearch('');
    setSelectedOperationId(nextState.operations[0]?.id || '');
    setExpandedConditionMap({});
    setDraggedOperationId('');
    setDragOverOperationId('');
    setDragOverPosition('before');
    if (nextState.visualMode === 'legacy') {
      setTemplateGroupKey('basic');
      setTemplatePresetKey('legacy_default');
    } else {
      setTemplateGroupKey('basic');
      setTemplatePresetKey('operations_default');
    }
    setHeaderValueExampleVisible(false);
    setFieldGuideVisible(false);
    setFieldGuideTarget('path');
    setFieldGuideKeyword('');
  }, [visible, value]);

  useEffect(() => {
    if (operations.length === 0) {
      setSelectedOperationId('');
      return;
    }
    if (!operations.some((item) => item.id === selectedOperationId)) {
      setSelectedOperationId(operations[0].id);
    }
  }, [operations, selectedOperationId]);

  const templatePresetOptions = useMemo(
    () =>
      Object.entries(TEMPLATE_PRESET_CONFIG)
        .filter(([, config]) => config.group === templateGroupKey)
        .map(([value, config]) => ({
          value,
          label: config.label,
        })),
    [templateGroupKey],
  );

  useEffect(() => {
    if (templatePresetOptions.length === 0) return;
    const exists = templatePresetOptions.some(
      (item) => item.value === templatePresetKey,
    );
    if (!exists) {
      setTemplatePresetKey(templatePresetOptions[0].value);
    }
  }, [templatePresetKey, templatePresetOptions]);

  const operationCount = useMemo(
    () => operations.filter((item) => !isOperationBlank(item)).length,
    [operations],
  );

  const filteredOperations = useMemo(() => {
    const keyword = operationSearch.trim().toLowerCase();
    if (!keyword) return operations;
    return operations.filter((operation) => {
      const searchableText = [
        operation.description,
        operation.mode,
        operation.path,
        operation.from,
        operation.to,
        operation.value_text,
      ]
        .filter(Boolean)
        .join(' ')
        .toLowerCase();
      return searchableText.includes(keyword);
    });
  }, [operationSearch, operations]);

  const selectedOperation = useMemo(
    () => operations.find((operation) => operation.id === selectedOperationId),
    [operations, selectedOperationId],
  );

  const selectedOperationIndex = useMemo(
    () =>
      operations.findIndex((operation) => operation.id === selectedOperationId),
    [operations, selectedOperationId],
  );

  const returnErrorDraft = useMemo(() => {
    if (
      !selectedOperation ||
      (selectedOperation.mode || '') !== 'return_error'
    ) {
      return null;
    }
    return parseReturnErrorDraft(selectedOperation.value_text);
  }, [selectedOperation]);

  const pruneObjectsDraft = useMemo(() => {
    if (
      !selectedOperation ||
      (selectedOperation.mode || '') !== 'prune_objects'
    ) {
      return null;
    }
    return parsePruneObjectsDraft(selectedOperation.value_text);
  }, [selectedOperation]);

  const topOperationModes = useMemo(() => {
    const counts = operations.reduce((acc, operation) => {
      const mode = operation.mode || 'set';
      acc[mode] = (acc[mode] || 0) + 1;
      return acc;
    }, {});
    return Object.entries(counts)
      .sort((a, b) => b[1] - a[1])
      .slice(0, 4);
  }, [operations]);

  const buildOperationsJson = useCallback(
    (sourceOperations, options = {}) => {
      const { validate = true } = options;
      const filteredOps = sourceOperations.filter(
        (item) => !isOperationBlank(item),
      );
      if (filteredOps.length === 0) return '';

      if (validate) {
        const message = validateOperations(filteredOps, t);
        if (message) {
          throw new Error(message);
        }
      }

      const payloadOps = filteredOps.map((operation) => {
        const mode = operation.mode || 'set';
        const meta = MODE_META[mode] || MODE_META.set;
        const descriptionValue = String(operation.description || '').trim();
        const pathValue = operation.path.trim();
        const fromValue = operation.from.trim();
        const toValue = operation.to.trim();
        const payload = { mode };
        if (descriptionValue) {
          payload.description = descriptionValue;
        }
        if (meta.path) {
          payload.path = pathValue;
        }
        if (meta.pathOptional && pathValue) {
          payload.path = pathValue;
        }
        if (meta.value) {
          payload.value = parseLooseValue(operation.value_text);
        }
        if (meta.keepOrigin && operation.keep_origin) {
          payload.keep_origin = true;
        }
        if (meta.from) {
          payload.from = fromValue;
        }
        if (!meta.to && operation.to.trim()) {
          payload.to = toValue;
        }
        if (meta.to) {
          payload.to = toValue;
        }
        if (meta.pathAlias) {
          if (!payload.from && pathValue) {
            payload.from = pathValue;
          }
          if (!payload.to && pathValue) {
            payload.to = pathValue;
          }
        }

        const conditions = (operation.conditions || [])
          .map(buildConditionPayload)
          .filter(Boolean);

        if (conditions.length > 0) {
          payload.conditions = conditions;
          payload.logic = operation.logic === 'AND' ? 'AND' : 'OR';
        }

        return payload;
      });

      return JSON.stringify({ operations: payloadOps }, null, 2);
    },
    [t],
  );

  const buildVisualJson = useCallback(() => {
    if (visualMode === 'legacy') {
      const trimmed = legacyValue.trim();
      if (!trimmed) return '';
      if (!verifyJSON(trimmed)) {
        throw new Error(t('参数覆盖必须是合法的 JSON 格式！'));
      }
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        throw new Error(t('旧格式必须是 JSON 对象'));
      }
      return JSON.stringify(parsed, null, 2);
    }
    return buildOperationsJson(operations, { validate: true });
  }, [buildOperationsJson, legacyValue, operations, t, visualMode]);

  const switchToJsonMode = () => {
    if (editMode === 'json') return;
    try {
      setJsonText(buildVisualJson());
      setJsonError('');
    } catch (error) {
      showError(error.message);
      if (visualMode === 'legacy') {
        setJsonText(legacyValue);
      } else {
        setJsonText(buildOperationsJson(operations, { validate: false }));
      }
      setJsonError(error.message || t('参数配置有误'));
    }
    setEditMode('json');
  };

  const switchToVisualMode = () => {
    if (editMode === 'visual') return;
    const trimmed = jsonText.trim();
    if (!trimmed) {
      const fallback = createDefaultOperation();
      setVisualMode('operations');
      setOperations([fallback]);
      setSelectedOperationId(fallback.id);
      setLegacyValue('');
      setJsonError('');
      setEditMode('visual');
      return;
    }
    if (!verifyJSON(trimmed)) {
      showError(t('参数覆盖必须是合法的 JSON 格式！'));
      return;
    }
    const parsed = JSON.parse(trimmed);
    if (
      parsed &&
      typeof parsed === 'object' &&
      !Array.isArray(parsed) &&
      Array.isArray(parsed.operations)
    ) {
      const nextOperations =
        parsed.operations.length > 0
          ? parsed.operations.map(normalizeOperation)
          : [createDefaultOperation()];
      setVisualMode('operations');
      setOperations(nextOperations);
      setSelectedOperationId(nextOperations[0]?.id || '');
      setLegacyValue('');
      setJsonError('');
      setEditMode('visual');
      setTemplateGroupKey('basic');
      setTemplatePresetKey('operations_default');
      return;
    }
    if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
      const fallback = createDefaultOperation();
      setVisualMode('legacy');
      setLegacyValue(JSON.stringify(parsed, null, 2));
      setOperations([fallback]);
      setSelectedOperationId(fallback.id);
      setJsonError('');
      setEditMode('visual');
      setTemplateGroupKey('basic');
      setTemplatePresetKey('legacy_default');
      return;
    }
    showError(t('参数覆盖必须是合法的 JSON 对象'));
  };

  const fillLegacyTemplate = (legacyPayload) => {
    const text = JSON.stringify(legacyPayload, null, 2);
    const fallback = createDefaultOperation();
    setVisualMode('legacy');
    setLegacyValue(text);
    setOperations([fallback]);
    setSelectedOperationId(fallback.id);
    setExpandedConditionMap({});
    setJsonText(text);
    setJsonError('');
    setEditMode('visual');
  };

  const fillOperationsTemplate = (operationsPayload) => {
    const nextOperations = (operationsPayload || []).map(normalizeOperation);
    const finalOperations =
      nextOperations.length > 0 ? nextOperations : [createDefaultOperation()];
    setVisualMode('operations');
    setOperations(finalOperations);
    setSelectedOperationId(finalOperations[0]?.id || '');
    setExpandedConditionMap({});
    setJsonText(
      JSON.stringify({ operations: operationsPayload || [] }, null, 2),
    );
    setJsonError('');
    setEditMode('visual');
  };

  const appendLegacyTemplate = (legacyPayload) => {
    let parsedCurrent = {};
    if (visualMode === 'legacy') {
      const trimmed = legacyValue.trim();
      if (trimmed) {
        if (!verifyJSON(trimmed)) {
          showError(t('当前旧格式 JSON 不合法，无法追加模板'));
          return;
        }
        const parsed = JSON.parse(trimmed);
        if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
          showError(t('当前旧格式不是 JSON 对象，无法追加模板'));
          return;
        }
        parsedCurrent = parsed;
      }
    }

    const merged = {
      ...(legacyPayload || {}),
      ...parsedCurrent,
    };
    const text = JSON.stringify(merged, null, 2);
    const fallback = createDefaultOperation();
    setVisualMode('legacy');
    setLegacyValue(text);
    setOperations([fallback]);
    setSelectedOperationId(fallback.id);
    setExpandedConditionMap({});
    setJsonText(text);
    setJsonError('');
    setEditMode('visual');
  };

  const appendOperationsTemplate = (operationsPayload) => {
    const appended = (operationsPayload || []).map(normalizeOperation);
    const existing =
      visualMode === 'operations'
        ? operations.filter((item) => !isOperationBlank(item))
        : [];
    const nextOperations = [...existing, ...appended];
    setVisualMode('operations');
    setOperations(nextOperations.length > 0 ? nextOperations : appended);
    setSelectedOperationId(nextOperations[0]?.id || appended[0]?.id || '');
    setExpandedConditionMap({});
    setLegacyValue('');
    setJsonError('');
    setEditMode('visual');
    setJsonText('');
  };

  const clearValue = () => {
    const fallback = createDefaultOperation();
    setVisualMode('operations');
    setLegacyValue('');
    setOperations([fallback]);
    setSelectedOperationId(fallback.id);
    setExpandedConditionMap({});
    setJsonText('');
    setJsonError('');
    setTemplateGroupKey('basic');
    setTemplatePresetKey('operations_default');
  };

  const getSelectedTemplatePreset = () =>
    TEMPLATE_PRESET_CONFIG[templatePresetKey] ||
    TEMPLATE_PRESET_CONFIG.operations_default;

  const fillTemplateFromLibrary = () => {
    const preset = getSelectedTemplatePreset();
    if (preset.kind === 'legacy') {
      fillLegacyTemplate(preset.payload || {});
      return;
    }
    fillOperationsTemplate(preset.payload?.operations || []);
  };

  const appendTemplateFromLibrary = () => {
    const preset = getSelectedTemplatePreset();
    if (preset.kind === 'legacy') {
      appendLegacyTemplate(preset.payload || {});
      return;
    }
    appendOperationsTemplate(preset.payload?.operations || []);
  };

  const resetEditorState = () => {
    clearValue();
    setEditMode('visual');
  };

  const applyBuiltinField = (fieldKey, target = 'path') => {
    if (!selectedOperation) {
      showError(t('请先选择一条规则'));
      return;
    }
    const mode = selectedOperation.mode || 'set';
    const meta = MODE_META[mode] || MODE_META.set;
    if (
      target === 'path' &&
      (meta.path || meta.pathOptional || meta.pathAlias)
    ) {
      updateOperation(selectedOperation.id, { path: fieldKey });
      return;
    }
    if (
      target === 'from' &&
      (meta.from || meta.pathAlias || mode === 'sync_fields')
    ) {
      updateOperation(selectedOperation.id, {
        from:
          mode === 'sync_fields'
            ? buildSyncTargetSpec('json', fieldKey)
            : fieldKey,
      });
      return;
    }
    if (target === 'to' && (meta.to || mode === 'sync_fields')) {
      updateOperation(selectedOperation.id, {
        to:
          mode === 'sync_fields'
            ? buildSyncTargetSpec('json', fieldKey)
            : fieldKey,
      });
      return;
    }
    showError(t('当前规则不支持写入到该位置'));
  };

  const openFieldGuide = (target = 'path') => {
    setFieldGuideTarget(target);
    setFieldGuideVisible(true);
  };

  const copyBuiltinField = async (fieldKey) => {
    const ok = await copy(fieldKey);
    if (ok) {
      showSuccess(t('已复制字段：{{name}}', { name: fieldKey }));
    } else {
      showError(t('复制失败'));
    }
  };

  const filteredFieldGuideSections = useMemo(() => {
    const keyword = fieldGuideKeyword.trim().toLowerCase();
    if (!keyword) {
      return BUILTIN_FIELD_SECTIONS;
    }
    return BUILTIN_FIELD_SECTIONS.map((section) => ({
      ...section,
      fields: section.fields.filter((field) =>
        [field.key, field.label, field.tip]
          .filter(Boolean)
          .join(' ')
          .toLowerCase()
          .includes(keyword),
      ),
    })).filter((section) => section.fields.length > 0);
  }, [fieldGuideKeyword]);

  const fieldGuideActionLabel = useMemo(() => {
    if (fieldGuideTarget === 'from') return t('填入来源');
    if (fieldGuideTarget === 'to') return t('填入目标');
    return t('填入路径');
  }, [fieldGuideTarget, t]);

  const fieldGuideFieldCount = useMemo(
    () =>
      filteredFieldGuideSections.reduce(
        (total, section) => total + section.fields.length,
        0,
      ),
    [filteredFieldGuideSections],
  );

  const updateOperation = (operationId, patch) => {
    setOperations((prev) =>
      prev.map((item) =>
        item.id === operationId ? { ...item, ...patch } : item,
      ),
    );
  };

  const formatSelectedOperationValueAsJson = useCallback(() => {
    if (!selectedOperation) return;
    const raw = String(selectedOperation.value_text || '').trim();
    if (!raw) return;
    if (!verifyJSON(raw)) {
      showError(t('当前值不是合法 JSON，无法格式化'));
      return;
    }
    try {
      updateOperation(selectedOperation.id, {
        value_text: JSON.stringify(JSON.parse(raw), null, 2),
      });
      showSuccess(t('JSON 已格式化'));
    } catch (error) {
      showError(t('当前值不是合法 JSON，无法格式化'));
    }
  }, [selectedOperation, t, updateOperation]);

  const updateReturnErrorDraft = (operationId, draftPatch = {}) => {
    const current = operations.find((item) => item.id === operationId);
    if (!current) return;
    const draft = parseReturnErrorDraft(current.value_text);
    const nextDraft = { ...draft, ...draftPatch };
    updateOperation(operationId, {
      value_text: buildReturnErrorValueText(nextDraft),
    });
  };

  const updatePruneObjectsDraft = (operationId, updater) => {
    const current = operations.find((item) => item.id === operationId);
    if (!current) return;
    const draft = parsePruneObjectsDraft(current.value_text);
    const nextDraft =
      typeof updater === 'function'
        ? updater(draft)
        : { ...draft, ...(updater || {}) };
    updateOperation(operationId, {
      value_text: buildPruneObjectsValueText(nextDraft),
    });
  };

  const addPruneRule = (operationId) => {
    updatePruneObjectsDraft(operationId, (draft) => ({
      ...draft,
      simpleMode: false,
      rules: [...(draft.rules || []), normalizePruneRule({})],
    }));
  };

  const updatePruneRule = (operationId, ruleId, patch) => {
    updatePruneObjectsDraft(operationId, (draft) => ({
      ...draft,
      rules: (draft.rules || []).map((rule) =>
        rule.id === ruleId ? { ...rule, ...patch } : rule,
      ),
    }));
  };

  const removePruneRule = (operationId, ruleId) => {
    updatePruneObjectsDraft(operationId, (draft) => ({
      ...draft,
      rules: (draft.rules || []).filter((rule) => rule.id !== ruleId),
    }));
  };

  const addOperation = () => {
    const created = createDefaultOperation();
    setOperations((prev) => [...prev, created]);
    setSelectedOperationId(created.id);
  };

  const resetOperationDragState = useCallback(() => {
    setDraggedOperationId('');
    setDragOverOperationId('');
    setDragOverPosition('before');
  }, []);

  const moveOperation = useCallback(
    (sourceId, targetId, position = 'before') => {
      if (!sourceId || !targetId || sourceId === targetId) {
        return;
      }
      setOperations((prev) =>
        reorderOperations(prev, sourceId, targetId, position),
      );
      setSelectedOperationId(sourceId);
    },
    [],
  );

  const handleOperationDragStart = useCallback((event, operationId) => {
    setDraggedOperationId(operationId);
    setSelectedOperationId(operationId);
    event.dataTransfer.effectAllowed = 'move';
    event.dataTransfer.setData('text/plain', operationId);
  }, []);

  const handleOperationDragOver = useCallback(
    (event, operationId) => {
      event.preventDefault();
      if (!draggedOperationId || draggedOperationId === operationId) {
        return;
      }
      const rect = event.currentTarget.getBoundingClientRect();
      const position =
        event.clientY - rect.top > rect.height / 2 ? 'after' : 'before';
      setDragOverOperationId(operationId);
      setDragOverPosition(position);
      event.dataTransfer.dropEffect = 'move';
    },
    [draggedOperationId],
  );

  const handleOperationDrop = useCallback(
    (event, operationId) => {
      event.preventDefault();
      const sourceId =
        draggedOperationId || event.dataTransfer.getData('text/plain');
      const position =
        dragOverOperationId === operationId ? dragOverPosition : 'before';
      moveOperation(sourceId, operationId, position);
      resetOperationDragState();
    },
    [
      dragOverOperationId,
      dragOverPosition,
      draggedOperationId,
      moveOperation,
      resetOperationDragState,
    ],
  );

  const duplicateOperation = (operationId) => {
    let insertedId = '';
    setOperations((prev) => {
      const index = prev.findIndex((item) => item.id === operationId);
      if (index < 0) return prev;
      const source = prev[index];
      const cloned = normalizeOperation({
        description: source.description,
        path: source.path,
        mode: source.mode,
        value: parseLooseValue(source.value_text),
        keep_origin: source.keep_origin,
        from: source.from,
        to: source.to,
        logic: source.logic,
        conditions: (source.conditions || []).map((condition) => ({
          path: condition.path,
          mode: condition.mode,
          value: parseLooseValue(condition.value_text),
          invert: condition.invert,
          pass_missing_key: condition.pass_missing_key,
        })),
      });
      insertedId = cloned.id;
      const next = [...prev];
      next.splice(index + 1, 0, cloned);
      return next;
    });
    if (insertedId) {
      setSelectedOperationId(insertedId);
    }
  };

  const removeOperation = (operationId) => {
    setOperations((prev) => {
      if (prev.length <= 1) return [createDefaultOperation()];
      return prev.filter((item) => item.id !== operationId);
    });
    setExpandedConditionMap((prev) => {
      if (!Object.prototype.hasOwnProperty.call(prev, operationId)) {
        return prev;
      }
      const next = { ...prev };
      delete next[operationId];
      return next;
    });
  };

  const addCondition = (operationId) => {
    const createdCondition = createDefaultCondition();
    setOperations((prev) =>
      prev.map((operation) =>
        operation.id === operationId
          ? {
              ...operation,
              conditions: [...(operation.conditions || []), createdCondition],
            }
          : operation,
      ),
    );
    setExpandedConditionMap((prev) => ({
      ...prev,
      [operationId]: [...(prev[operationId] || []), createdCondition.id],
    }));
  };

  const updateCondition = (operationId, conditionId, patch) => {
    setOperations((prev) =>
      prev.map((operation) => {
        if (operation.id !== operationId) return operation;
        return {
          ...operation,
          conditions: (operation.conditions || []).map((condition) =>
            condition.id === conditionId
              ? { ...condition, ...patch }
              : condition,
          ),
        };
      }),
    );
  };

  const removeCondition = (operationId, conditionId) => {
    setOperations((prev) =>
      prev.map((operation) => {
        if (operation.id !== operationId) return operation;
        return {
          ...operation,
          conditions: (operation.conditions || []).filter(
            (condition) => condition.id !== conditionId,
          ),
        };
      }),
    );
    setExpandedConditionMap((prev) => ({
      ...prev,
      [operationId]: (prev[operationId] || []).filter(
        (id) => id !== conditionId,
      ),
    }));
  };

  const selectedConditionKeys = useMemo(
    () => expandedConditionMap[selectedOperationId] || [],
    [expandedConditionMap, selectedOperationId],
  );

  const handleConditionCollapseChange = useCallback(
    (operationId, activeKeys) => {
      const keys = (
        Array.isArray(activeKeys) ? activeKeys : [activeKeys]
      ).filter(Boolean);
      setExpandedConditionMap((prev) => ({
        ...prev,
        [operationId]: keys,
      }));
    },
    [],
  );

  const expandAllSelectedConditions = useCallback(() => {
    if (!selectedOperationId || !selectedOperation) return;
    setExpandedConditionMap((prev) => ({
      ...prev,
      [selectedOperationId]: (selectedOperation.conditions || []).map(
        (condition) => condition.id,
      ),
    }));
  }, [selectedOperation, selectedOperationId]);

  const collapseAllSelectedConditions = useCallback(() => {
    if (!selectedOperationId) return;
    setExpandedConditionMap((prev) => ({
      ...prev,
      [selectedOperationId]: [],
    }));
  }, [selectedOperationId]);

  const handleJsonChange = (nextValue) => {
    setJsonText(nextValue);
    const trimmed = String(nextValue || '').trim();
    if (!trimmed) {
      setJsonError('');
      return;
    }
    if (!verifyJSON(trimmed)) {
      setJsonError(t('JSON格式错误'));
      return;
    }
    setJsonError('');
  };

  const formatJson = () => {
    const trimmed = jsonText.trim();
    if (!trimmed) return;
    if (!verifyJSON(trimmed)) {
      showError(t('参数覆盖必须是合法的 JSON 格式！'));
      return;
    }
    setJsonText(JSON.stringify(JSON.parse(trimmed), null, 2));
    setJsonError('');
  };

  const visualValidationError = useMemo(() => {
    if (editMode !== 'visual') {
      return '';
    }
    try {
      buildVisualJson();
      return '';
    } catch (error) {
      return error?.message || t('参数配置有误');
    }
  }, [buildVisualJson, editMode, t]);

  const handleSave = () => {
    try {
      let result = '';
      if (editMode === 'json') {
        const trimmed = jsonText.trim();
        if (!trimmed) {
          result = '';
        } else {
          if (!verifyJSON(trimmed)) {
            throw new Error(t('参数覆盖必须是合法的 JSON 格式！'));
          }
          result = JSON.stringify(JSON.parse(trimmed), null, 2);
        }
      } else {
        result = buildVisualJson();
      }
      onSave?.(result);
    } catch (error) {
      showError(error.message);
    }
  };

  const editorContextValue = {
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
  };

  return (
    <ParamOverrideEditorContext.Provider value={editorContextValue}>
      <ParamOverrideEditorView />
    </ParamOverrideEditorContext.Provider>
  );
};

export default ParamOverrideEditorModal;
