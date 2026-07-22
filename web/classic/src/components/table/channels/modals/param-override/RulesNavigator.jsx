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
import { Card, Col, Input, Space, Tag, Typography } from '@douyinfe/semi-ui';
import { IconMenu } from '@douyinfe/semi-icons';
import {
  OPERATION_MODE_LABEL_MAP,
  getOperationModeTagColor,
  getOperationSummary,
} from './editorDomain';
import { useParamOverrideEditor } from './EditorContext';

const { Text } = Typography;

export default function ParamOverrideRulesNavigator() {
  const {
    dragOverOperationId,
    dragOverPosition,
    draggedOperationId,
    filteredOperations,
    handleOperationDragOver,
    handleOperationDragStart,
    handleOperationDrop,
    operationCount,
    operationSearch,
    operations,
    resetOperationDragState,
    selectedOperationId,
    setOperationSearch,
    setSelectedOperationId,
    t,
    topOperationModes,
  } = useParamOverrideEditor();

  return (
    <Col xs={24} md={8}>
      <Card
        className='!rounded-2xl !border-0 h-full'
        bodyStyle={{
          padding: 12,
          background: 'var(--semi-color-fill-0)',
          display: 'flex',
          flexDirection: 'column',
          gap: 10,
          minHeight: 520,
        }}
      >
        <div className='flex items-center justify-between'>
          <Text strong>{t('规则导航')}</Text>
          <Tag color='grey'>{`${operationCount}/${operations.length}`}</Tag>
        </div>

        {topOperationModes.length > 0 ? (
          <Space wrap spacing={6}>
            {topOperationModes.map(([mode, count]) => (
              <Tag
                key={`mode_stat_${mode}`}
                size='small'
                color={getOperationModeTagColor(mode)}
              >
                {`${OPERATION_MODE_LABEL_MAP[mode] || mode} · ${count}`}
              </Tag>
            ))}
          </Space>
        ) : null}

        <Input
          value={operationSearch}
          placeholder={t('搜索规则（描述 / 类型 / 路径 / 来源 / 目标）')}
          onChange={(nextValue) => setOperationSearch(nextValue || '')}
          showClear
        />

        <div
          className='overflow-auto'
          style={{ flex: 1, minHeight: 320, paddingRight: 2 }}
        >
          {filteredOperations.length === 0 ? (
            <Text type='tertiary' size='small'>
              {t('没有匹配的规则')}
            </Text>
          ) : (
            <div
              style={{
                display: 'flex',
                flexDirection: 'column',
                gap: 8,
                width: '100%',
              }}
            >
              {filteredOperations.map((operation) => {
                const index = operations.findIndex(
                  (item) => item.id === operation.id,
                );
                const isActive = operation.id === selectedOperationId;
                const isDragging = operation.id === draggedOperationId;
                const isDropTarget =
                  operation.id === dragOverOperationId &&
                  draggedOperationId &&
                  draggedOperationId !== operation.id;
                return (
                  <div
                    key={operation.id}
                    role='button'
                    tabIndex={0}
                    draggable={operations.length > 1}
                    onClick={() => setSelectedOperationId(operation.id)}
                    onDragStart={(event) =>
                      handleOperationDragStart(event, operation.id)
                    }
                    onDragOver={(event) =>
                      handleOperationDragOver(event, operation.id)
                    }
                    onDrop={(event) => handleOperationDrop(event, operation.id)}
                    onDragEnd={resetOperationDragState}
                    onKeyDown={(event) => {
                      if (event.key === 'Enter' || event.key === ' ') {
                        event.preventDefault();
                        setSelectedOperationId(operation.id);
                      }
                    }}
                    className='w-full rounded-xl px-3 py-3 cursor-pointer transition-colors'
                    style={{
                      background: isActive
                        ? 'var(--semi-color-primary-light-default)'
                        : 'var(--semi-color-bg-2)',
                      border: isActive
                        ? '1px solid var(--semi-color-primary)'
                        : '1px solid var(--semi-color-border)',
                      opacity: isDragging ? 0.6 : 1,
                      boxShadow: isDropTarget
                        ? dragOverPosition === 'after'
                          ? 'inset 0 -3px 0 var(--semi-color-primary)'
                          : 'inset 0 3px 0 var(--semi-color-primary)'
                        : 'none',
                    }}
                  >
                    <div className='flex items-start justify-between gap-2'>
                      <div className='flex items-start gap-2 min-w-0'>
                        <div
                          className='flex-shrink-0'
                          style={{
                            color: 'var(--semi-color-text-2)',
                            cursor: operations.length > 1 ? 'grab' : 'default',
                            marginTop: 1,
                          }}
                        >
                          <IconMenu />
                        </div>
                        <div className='min-w-0'>
                          <Text strong>{`#${index + 1}`}</Text>
                          <Text
                            type='tertiary'
                            size='small'
                            className='block mt-1'
                          >
                            {getOperationSummary(operation, index)}
                          </Text>
                          {String(operation.description || '').trim() ? (
                            <Text
                              type='tertiary'
                              size='small'
                              className='block mt-1'
                              style={{
                                lineHeight: 1.5,
                                wordBreak: 'break-word',
                                overflow: 'hidden',
                                display: '-webkit-box',
                                WebkitLineClamp: 2,
                                WebkitBoxOrient: 'vertical',
                              }}
                            >
                              {operation.description}
                            </Text>
                          ) : null}
                        </div>
                      </div>
                      <Tag size='small' color='grey'>
                        {(operation.conditions || []).length}
                      </Tag>
                    </div>
                    <Space spacing={6} style={{ marginTop: 8 }}>
                      <Tag
                        size='small'
                        color={getOperationModeTagColor(
                          operation.mode || 'set',
                        )}
                      >
                        {OPERATION_MODE_LABEL_MAP[operation.mode || 'set'] ||
                          operation.mode ||
                          'set'}
                      </Tag>
                      <Text type='tertiary' size='small'>
                        {t('条件数')}
                      </Text>
                    </Space>
                  </div>
                );
              })}
            </div>
          )}
        </div>
      </Card>
    </Col>
  );
}
