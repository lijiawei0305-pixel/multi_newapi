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
  Row,
  Select,
  Space,
  Switch,
  Tag,
  TextArea,
  Typography,
} from '@douyinfe/semi-ui';
import { IconDelete, IconPlus } from '@douyinfe/semi-icons';
import {
  CONDITION_MODE_OPTIONS,
  MODE_DESCRIPTIONS,
  MODE_META,
  OPERATION_MODE_OPTIONS,
  SYNC_TARGET_TYPE_OPTIONS,
  buildSyncTargetSpec,
  getModeFromLabel,
  getModeFromPlaceholder,
  getModePathLabel,
  getModePathPlaceholder,
  getModeToLabel,
  getModeToPlaceholder,
  getModeValueLabel,
  getModeValuePlaceholder,
  getOperationSummary,
  parseSyncTargetSpec,
} from './editorDomain';
import { useParamOverrideEditor } from './EditorContext';

const { Text } = Typography;

export default function ParamOverrideRuleDetails() {
  const {
    addCondition,
    addPruneRule,
    collapseAllSelectedConditions,
    duplicateOperation,
    expandAllSelectedConditions,
    formatSelectedOperationValueAsJson,
    handleConditionCollapseChange,
    pruneObjectsDraft,
    removeCondition,
    removeOperation,
    removePruneRule,
    returnErrorDraft,
    selectedConditionKeys,
    selectedOperation,
    selectedOperationIndex,
    setHeaderValueExampleVisible,
    t,
    updateCondition,
    updateOperation,
    updatePruneObjectsDraft,
    updatePruneRule,
    updateReturnErrorDraft,
    visualValidationError,
  } = useParamOverrideEditor();

  return (
    <Col xs={24} md={16}>
      {selectedOperation ? (
        (() => {
          const mode = selectedOperation.mode || 'set';
          const meta = MODE_META[mode] || MODE_META.set;
          const conditions = selectedOperation.conditions || [];
          const syncFromTarget =
            mode === 'sync_fields'
              ? parseSyncTargetSpec(selectedOperation.from)
              : null;
          const syncToTarget =
            mode === 'sync_fields'
              ? parseSyncTargetSpec(selectedOperation.to)
              : null;
          return (
            <Card
              className='!rounded-2xl !border-0'
              bodyStyle={{
                padding: 14,
                background: 'var(--semi-color-fill-0)',
              }}
            >
              <div className='flex items-center justify-between mb-3'>
                <Space>
                  <Tag color='blue'>{`#${selectedOperationIndex + 1}`}</Tag>
                  <Text strong>
                    {getOperationSummary(
                      selectedOperation,
                      selectedOperationIndex,
                    )}
                  </Text>
                </Space>
                <Space>
                  <Button
                    size='small'
                    type='tertiary'
                    onClick={() => duplicateOperation(selectedOperation.id)}
                  >
                    {t('复制')}
                  </Button>
                  <Button
                    size='small'
                    type='danger'
                    theme='borderless'
                    icon={<IconDelete />}
                    aria-label={t('删除规则')}
                    onClick={() => removeOperation(selectedOperation.id)}
                  />
                </Space>
              </div>

              <Row gutter={12}>
                <Col xs={24} md={8}>
                  <Text type='tertiary' size='small'>
                    {t('操作类型')}
                  </Text>
                  <Select
                    value={mode}
                    optionList={OPERATION_MODE_OPTIONS}
                    onChange={(nextMode) =>
                      updateOperation(selectedOperation.id, {
                        mode: nextMode,
                      })
                    }
                    style={{ width: '100%' }}
                  />
                </Col>
                {meta.path || meta.pathOptional ? (
                  <Col xs={24} md={16}>
                    <Text type='tertiary' size='small'>
                      {meta.pathOptional
                        ? t('目标路径（可选）')
                        : t(getModePathLabel(mode))}
                    </Text>
                    <Input
                      value={selectedOperation.path}
                      placeholder={getModePathPlaceholder(mode)}
                      onChange={(nextValue) =>
                        updateOperation(selectedOperation.id, {
                          path: nextValue,
                        })
                      }
                    />
                  </Col>
                ) : null}
              </Row>

              <Text type='tertiary' size='small' className='mt-1 block'>
                {MODE_DESCRIPTIONS[mode] || ''}
              </Text>
              <div className='mt-2'>
                <Text type='tertiary' size='small'>
                  {t('规则描述（可选）')}
                </Text>
                <Input
                  value={selectedOperation.description || ''}
                  placeholder={t('例如：清理工具参数，避免上游校验错误')}
                  onChange={(nextValue) =>
                    updateOperation(selectedOperation.id, {
                      description: nextValue || '',
                    })
                  }
                  maxLength={180}
                  showClear
                />
                <Text type='tertiary' size='small' className='mt-1 block'>
                  {`${String(selectedOperation.description || '').length}/180`}
                </Text>
              </div>

              {meta.value ? (
                mode === 'return_error' && returnErrorDraft ? (
                  <div
                    className='mt-2 rounded-xl p-3'
                    style={{
                      background: 'var(--semi-color-bg-1)',
                      border: '1px solid var(--semi-color-border)',
                    }}
                  >
                    <div className='flex items-center justify-between mb-2'>
                      <Text strong>{t('自定义错误响应')}</Text>
                      <Space spacing={6} align='center'>
                        <Text type='tertiary' size='small'>
                          {t('模式')}
                        </Text>
                        <Button
                          size='small'
                          type={
                            returnErrorDraft.simpleMode ? 'primary' : 'tertiary'
                          }
                          onClick={() =>
                            updateReturnErrorDraft(selectedOperation.id, {
                              simpleMode: true,
                            })
                          }
                        >
                          {t('简洁')}
                        </Button>
                        <Button
                          size='small'
                          type={
                            returnErrorDraft.simpleMode ? 'tertiary' : 'primary'
                          }
                          onClick={() =>
                            updateReturnErrorDraft(selectedOperation.id, {
                              simpleMode: false,
                            })
                          }
                        >
                          {t('高级')}
                        </Button>
                      </Space>
                    </div>

                    <Text type='tertiary' size='small'>
                      {t('错误消息（必填）')}
                    </Text>
                    <TextArea
                      value={returnErrorDraft.message}
                      autosize={{ minRows: 2, maxRows: 4 }}
                      placeholder={t('例如：该请求不满足准入策略')}
                      onChange={(nextValue) =>
                        updateReturnErrorDraft(selectedOperation.id, {
                          message: nextValue,
                        })
                      }
                    />

                    {returnErrorDraft.simpleMode ? (
                      <Text type='tertiary' size='small' className='mt-2 block'>
                        {t(
                          '简洁模式仅返回 message；状态码和错误类型将使用系统默认值。',
                        )}
                      </Text>
                    ) : (
                      <>
                        <Row gutter={12} style={{ marginTop: 10 }}>
                          <Col xs={24} md={8}>
                            <Text type='tertiary' size='small'>
                              {t('状态码')}
                            </Text>
                            <Input
                              value={String(returnErrorDraft.statusCode ?? '')}
                              placeholder='400'
                              onChange={(nextValue) =>
                                updateReturnErrorDraft(selectedOperation.id, {
                                  statusCode: parseInt(nextValue, 10) || 400,
                                })
                              }
                            />
                          </Col>
                          <Col xs={24} md={8}>
                            <Text type='tertiary' size='small'>
                              {t('错误代码（可选）')}
                            </Text>
                            <Input
                              value={returnErrorDraft.code}
                              placeholder='forced_bad_request'
                              onChange={(nextValue) =>
                                updateReturnErrorDraft(selectedOperation.id, {
                                  code: nextValue,
                                })
                              }
                            />
                          </Col>
                          <Col xs={24} md={8}>
                            <Text type='tertiary' size='small'>
                              {t('错误类型（可选）')}
                            </Text>
                            <Input
                              value={returnErrorDraft.type}
                              placeholder='invalid_request_error'
                              onChange={(nextValue) =>
                                updateReturnErrorDraft(selectedOperation.id, {
                                  type: nextValue,
                                })
                              }
                            />
                          </Col>
                        </Row>
                        <div className='mt-2 flex items-center gap-2'>
                          <Text type='tertiary' size='small'>
                            {t('重试建议')}
                          </Text>
                          <Button
                            size='small'
                            type={
                              returnErrorDraft.skipRetry
                                ? 'primary'
                                : 'tertiary'
                            }
                            onClick={() =>
                              updateReturnErrorDraft(selectedOperation.id, {
                                skipRetry: true,
                              })
                            }
                          >
                            {t('停止重试')}
                          </Button>
                          <Button
                            size='small'
                            type={
                              returnErrorDraft.skipRetry
                                ? 'tertiary'
                                : 'primary'
                            }
                            onClick={() =>
                              updateReturnErrorDraft(selectedOperation.id, {
                                skipRetry: false,
                              })
                            }
                          >
                            {t('允许重试')}
                          </Button>
                        </div>
                        <Space wrap style={{ marginTop: 8 }}>
                          <Tag
                            size='small'
                            color='grey'
                            className='cursor-pointer'
                            onClick={() =>
                              updateReturnErrorDraft(selectedOperation.id, {
                                statusCode: 400,
                                code: 'invalid_request',
                                type: 'invalid_request_error',
                              })
                            }
                          >
                            {t('参数错误')}
                          </Tag>
                          <Tag
                            size='small'
                            color='grey'
                            className='cursor-pointer'
                            onClick={() =>
                              updateReturnErrorDraft(selectedOperation.id, {
                                statusCode: 401,
                                code: 'unauthorized',
                                type: 'authentication_error',
                              })
                            }
                          >
                            {t('未授权')}
                          </Tag>
                          <Tag
                            size='small'
                            color='grey'
                            className='cursor-pointer'
                            onClick={() =>
                              updateReturnErrorDraft(selectedOperation.id, {
                                statusCode: 429,
                                code: 'rate_limited',
                                type: 'rate_limit_error',
                              })
                            }
                          >
                            {t('限流')}
                          </Tag>
                        </Space>
                      </>
                    )}
                  </div>
                ) : mode === 'prune_objects' && pruneObjectsDraft ? (
                  <div
                    className='mt-2 rounded-xl p-3'
                    style={{
                      background: 'var(--semi-color-bg-1)',
                      border: '1px solid var(--semi-color-border)',
                    }}
                  >
                    <div className='flex items-center justify-between mb-2'>
                      <Text strong>{t('对象清理规则')}</Text>
                      <Space spacing={6} align='center'>
                        <Text type='tertiary' size='small'>
                          {t('模式')}
                        </Text>
                        <Button
                          size='small'
                          type={
                            pruneObjectsDraft.simpleMode
                              ? 'primary'
                              : 'tertiary'
                          }
                          onClick={() =>
                            updatePruneObjectsDraft(selectedOperation.id, {
                              simpleMode: true,
                            })
                          }
                        >
                          {t('简洁')}
                        </Button>
                        <Button
                          size='small'
                          type={
                            pruneObjectsDraft.simpleMode
                              ? 'tertiary'
                              : 'primary'
                          }
                          onClick={() =>
                            updatePruneObjectsDraft(selectedOperation.id, {
                              simpleMode: false,
                            })
                          }
                        >
                          {t('高级')}
                        </Button>
                      </Space>
                    </div>

                    <Text type='tertiary' size='small'>
                      {t('类型（常用）')}
                    </Text>
                    <Input
                      value={pruneObjectsDraft.typeText}
                      placeholder='redacted_thinking'
                      onChange={(nextValue) =>
                        updatePruneObjectsDraft(selectedOperation.id, {
                          typeText: nextValue,
                        })
                      }
                    />

                    {pruneObjectsDraft.simpleMode ? (
                      <Text type='tertiary' size='small' className='mt-2 block'>
                        {t(
                          '简洁模式：按 type 全量清理对象，例如 redacted_thinking。',
                        )}
                      </Text>
                    ) : (
                      <>
                        <Row gutter={12} style={{ marginTop: 10 }}>
                          <Col xs={24} md={12}>
                            <Text type='tertiary' size='small'>
                              {t('逻辑')}
                            </Text>
                            <Select
                              value={pruneObjectsDraft.logic}
                              optionList={[
                                {
                                  label: t('全部满足（AND）'),
                                  value: 'AND',
                                },
                                {
                                  label: t('任一满足（OR）'),
                                  value: 'OR',
                                },
                              ]}
                              style={{ width: '100%' }}
                              onChange={(nextValue) =>
                                updatePruneObjectsDraft(selectedOperation.id, {
                                  logic: nextValue || 'AND',
                                })
                              }
                            />
                          </Col>
                          <Col xs={24} md={12}>
                            <Text type='tertiary' size='small'>
                              {t('递归策略')}
                            </Text>
                            <Space spacing={6} style={{ marginTop: 2 }}>
                              <Button
                                size='small'
                                type={
                                  pruneObjectsDraft.recursive
                                    ? 'primary'
                                    : 'tertiary'
                                }
                                onClick={() =>
                                  updatePruneObjectsDraft(
                                    selectedOperation.id,
                                    { recursive: true },
                                  )
                                }
                              >
                                {t('递归')}
                              </Button>
                              <Button
                                size='small'
                                type={
                                  pruneObjectsDraft.recursive
                                    ? 'tertiary'
                                    : 'primary'
                                }
                                onClick={() =>
                                  updatePruneObjectsDraft(
                                    selectedOperation.id,
                                    { recursive: false },
                                  )
                                }
                              >
                                {t('仅当前层')}
                              </Button>
                            </Space>
                          </Col>
                        </Row>

                        <div
                          className='mt-2 rounded-lg p-2'
                          style={{
                            background: 'var(--semi-color-fill-0)',
                          }}
                        >
                          <div className='flex items-center justify-between mb-2'>
                            <Text strong>{t('附加条件')}</Text>
                            <Button
                              size='small'
                              icon={<IconPlus />}
                              onClick={() => addPruneRule(selectedOperation.id)}
                            >
                              {t('新增条件')}
                            </Button>
                          </div>
                          {(pruneObjectsDraft.rules || []).length === 0 ? (
                            <Text type='tertiary' size='small'>
                              {t(
                                '未添加附加条件时，仅使用上方 type 进行清理。',
                              )}
                            </Text>
                          ) : (
                            <div className='flex flex-col gap-2'>
                              {(pruneObjectsDraft.rules || []).map(
                                (rule, ruleIndex) => (
                                  <div
                                    key={rule.id}
                                    className='rounded-lg p-2'
                                    style={{
                                      border:
                                        '1px solid var(--semi-color-border)',
                                      background: 'var(--semi-color-bg-0)',
                                    }}
                                  >
                                    <div className='flex items-center justify-between mb-2'>
                                      <Tag size='small'>
                                        {`R${ruleIndex + 1}`}
                                      </Tag>
                                      <Button
                                        size='small'
                                        type='danger'
                                        theme='borderless'
                                        icon={<IconDelete />}
                                        onClick={() =>
                                          removePruneRule(
                                            selectedOperation.id,
                                            rule.id,
                                          )
                                        }
                                      >
                                        {t('删除条件')}
                                      </Button>
                                    </div>
                                    <Row gutter={8}>
                                      <Col xs={24} md={9}>
                                        <Text type='tertiary' size='small'>
                                          {t('字段路径')}
                                        </Text>
                                        <Input
                                          value={rule.path}
                                          placeholder='type'
                                          onChange={(nextValue) =>
                                            updatePruneRule(
                                              selectedOperation.id,
                                              rule.id,
                                              { path: nextValue },
                                            )
                                          }
                                        />
                                      </Col>
                                      <Col xs={24} md={7}>
                                        <Text type='tertiary' size='small'>
                                          {t('匹配方式')}
                                        </Text>
                                        <Select
                                          value={rule.mode}
                                          optionList={CONDITION_MODE_OPTIONS}
                                          style={{
                                            width: '100%',
                                          }}
                                          onChange={(nextValue) =>
                                            updatePruneRule(
                                              selectedOperation.id,
                                              rule.id,
                                              { mode: nextValue },
                                            )
                                          }
                                        />
                                      </Col>
                                      <Col xs={24} md={8}>
                                        <Text type='tertiary' size='small'>
                                          {t('匹配值（可选）')}
                                        </Text>
                                        <Input
                                          value={rule.value_text}
                                          placeholder='redacted_thinking'
                                          onChange={(nextValue) =>
                                            updatePruneRule(
                                              selectedOperation.id,
                                              rule.id,
                                              {
                                                value_text: nextValue,
                                              },
                                            )
                                          }
                                        />
                                      </Col>
                                    </Row>
                                    <Space
                                      wrap
                                      spacing={8}
                                      style={{ marginTop: 8 }}
                                    >
                                      <Button
                                        size='small'
                                        type={
                                          rule.invert ? 'primary' : 'tertiary'
                                        }
                                        onClick={() =>
                                          updatePruneRule(
                                            selectedOperation.id,
                                            rule.id,
                                            {
                                              invert: !rule.invert,
                                            },
                                          )
                                        }
                                      >
                                        {t('条件取反')}
                                      </Button>
                                      <Button
                                        size='small'
                                        type={
                                          rule.pass_missing_key
                                            ? 'primary'
                                            : 'tertiary'
                                        }
                                        onClick={() =>
                                          updatePruneRule(
                                            selectedOperation.id,
                                            rule.id,
                                            {
                                              pass_missing_key:
                                                !rule.pass_missing_key,
                                            },
                                          )
                                        }
                                      >
                                        {t('字段缺失视为命中')}
                                      </Button>
                                    </Space>
                                  </div>
                                ),
                              )}
                            </div>
                          )}
                        </div>
                      </>
                    )}
                  </div>
                ) : (
                  <div className='mt-2'>
                    <div className='flex items-center justify-between gap-2'>
                      <Text type='tertiary' size='small'>
                        {t(getModeValueLabel(mode))}
                      </Text>
                      {mode === 'set_header' ? (
                        <Space spacing={6}>
                          <Button
                            size='small'
                            type='tertiary'
                            onClick={() => setHeaderValueExampleVisible(true)}
                          >
                            {t('查看 JSON 示例')}
                          </Button>
                          <Button
                            size='small'
                            type='tertiary'
                            onClick={formatSelectedOperationValueAsJson}
                          >
                            {t('格式化 JSON')}
                          </Button>
                        </Space>
                      ) : null}
                    </div>
                    {mode === 'set_header' ? (
                      <Text
                        type='tertiary'
                        size='small'
                        className='mt-1 mb-2 block'
                      >
                        {t(
                          '纯字符串会直接覆盖整条请求头，或者点击“查看 JSON 示例”按 token 规则处理。',
                        )}
                      </Text>
                    ) : null}
                    <TextArea
                      value={selectedOperation.value_text}
                      autosize={{ minRows: 1, maxRows: 4 }}
                      placeholder={getModeValuePlaceholder(mode)}
                      onChange={(nextValue) =>
                        updateOperation(selectedOperation.id, {
                          value_text: nextValue,
                        })
                      }
                    />
                  </div>
                )
              ) : null}

              {meta.keepOrigin ? (
                <div className='mt-2 flex items-center gap-2'>
                  <Switch
                    checked={Boolean(selectedOperation.keep_origin)}
                    checkedText={t('开')}
                    uncheckedText={t('关')}
                    onChange={(nextValue) =>
                      updateOperation(selectedOperation.id, {
                        keep_origin: nextValue,
                      })
                    }
                  />
                  <Text type='tertiary' size='small' className='leading-6'>
                    {t('保留原值（目标已有值时不覆盖）')}
                  </Text>
                </div>
              ) : null}

              {mode === 'sync_fields' ? (
                <div className='mt-2'>
                  <Text type='tertiary' size='small'>
                    {t('同步端点')}
                  </Text>
                  <Row gutter={12} style={{ marginTop: 6 }}>
                    <Col xs={24} md={12}>
                      <Text type='tertiary' size='small'>
                        {t('来源端点')}
                      </Text>
                      <div className='flex gap-2'>
                        <Select
                          value={syncFromTarget?.type || 'json'}
                          optionList={SYNC_TARGET_TYPE_OPTIONS}
                          style={{ width: 120 }}
                          onChange={(nextType) =>
                            updateOperation(selectedOperation.id, {
                              from: buildSyncTargetSpec(
                                nextType,
                                syncFromTarget?.key || '',
                              ),
                            })
                          }
                        />
                        <Input
                          value={syncFromTarget?.key || ''}
                          placeholder='session_id'
                          onChange={(nextKey) =>
                            updateOperation(selectedOperation.id, {
                              from: buildSyncTargetSpec(
                                syncFromTarget?.type || 'json',
                                nextKey,
                              ),
                            })
                          }
                        />
                      </div>
                    </Col>
                    <Col xs={24} md={12}>
                      <Text type='tertiary' size='small'>
                        {t('目标端点')}
                      </Text>
                      <div className='flex gap-2'>
                        <Select
                          value={syncToTarget?.type || 'json'}
                          optionList={SYNC_TARGET_TYPE_OPTIONS}
                          style={{ width: 120 }}
                          onChange={(nextType) =>
                            updateOperation(selectedOperation.id, {
                              to: buildSyncTargetSpec(
                                nextType,
                                syncToTarget?.key || '',
                              ),
                            })
                          }
                        />
                        <Input
                          value={syncToTarget?.key || ''}
                          placeholder='prompt_cache_key'
                          onChange={(nextKey) =>
                            updateOperation(selectedOperation.id, {
                              to: buildSyncTargetSpec(
                                syncToTarget?.type || 'json',
                                nextKey,
                              ),
                            })
                          }
                        />
                      </div>
                    </Col>
                  </Row>
                  <Space wrap style={{ marginTop: 8 }}>
                    <Tag
                      size='small'
                      color='cyan'
                      className='cursor-pointer'
                      onClick={() =>
                        updateOperation(selectedOperation.id, {
                          from: 'header:session_id',
                          to: 'json:prompt_cache_key',
                        })
                      }
                    >
                      {'header:session_id -> json:prompt_cache_key'}
                    </Tag>
                    <Tag
                      size='small'
                      color='cyan'
                      className='cursor-pointer'
                      onClick={() =>
                        updateOperation(selectedOperation.id, {
                          from: 'json:prompt_cache_key',
                          to: 'header:session_id',
                        })
                      }
                    >
                      {'json:prompt_cache_key -> header:session_id'}
                    </Tag>
                  </Space>
                </div>
              ) : meta.from || meta.to === false || meta.to ? (
                <Row gutter={12} style={{ marginTop: 8 }}>
                  {meta.from || meta.to === false ? (
                    <Col xs={24} md={12}>
                      <Text type='tertiary' size='small'>
                        {t(getModeFromLabel(mode))}
                      </Text>
                      <Input
                        value={selectedOperation.from}
                        placeholder={getModeFromPlaceholder(mode)}
                        onChange={(nextValue) =>
                          updateOperation(selectedOperation.id, {
                            from: nextValue,
                          })
                        }
                      />
                    </Col>
                  ) : null}
                  {meta.to || meta.to === false ? (
                    <Col xs={24} md={12}>
                      <Text type='tertiary' size='small'>
                        {t(getModeToLabel(mode))}
                      </Text>
                      <Input
                        value={selectedOperation.to}
                        placeholder={getModeToPlaceholder(mode)}
                        onChange={(nextValue) =>
                          updateOperation(selectedOperation.id, {
                            to: nextValue,
                          })
                        }
                      />
                    </Col>
                  ) : null}
                </Row>
              ) : null}

              <div
                className='mt-3 rounded-xl p-3'
                style={{
                  background: 'rgba(127, 127, 127, 0.08)',
                }}
              >
                <div className='flex items-center justify-between mb-2'>
                  <Space align='center'>
                    <Text>{t('条件规则')}</Text>
                    <Select
                      value={selectedOperation.logic || 'OR'}
                      optionList={[
                        {
                          label: t('满足任一条件（OR）'),
                          value: 'OR',
                        },
                        {
                          label: t('必须全部满足（AND）'),
                          value: 'AND',
                        },
                      ]}
                      size='small'
                      style={{ width: 180 }}
                      onChange={(nextValue) =>
                        updateOperation(selectedOperation.id, {
                          logic: nextValue,
                        })
                      }
                    />
                  </Space>
                  <Space spacing={6}>
                    <Button
                      size='small'
                      type='tertiary'
                      onClick={expandAllSelectedConditions}
                    >
                      {t('全部展开')}
                    </Button>
                    <Button
                      size='small'
                      type='tertiary'
                      onClick={collapseAllSelectedConditions}
                    >
                      {t('全部收起')}
                    </Button>
                    <Button
                      icon={<IconPlus />}
                      size='small'
                      onClick={() => addCondition(selectedOperation.id)}
                    >
                      {t('新增条件')}
                    </Button>
                  </Space>
                </div>

                {conditions.length === 0 ? (
                  <Text type='tertiary' size='small'>
                    {t('没有条件时，默认总是执行该操作。')}
                  </Text>
                ) : (
                  <Collapse
                    keepDOM
                    activeKey={selectedConditionKeys}
                    onChange={(activeKeys) =>
                      handleConditionCollapseChange(
                        selectedOperation.id,
                        activeKeys,
                      )
                    }
                  >
                    {conditions.map((condition, conditionIndex) => (
                      <Collapse.Panel
                        key={condition.id}
                        itemKey={condition.id}
                        header={
                          <Space spacing={8}>
                            <Tag size='small'>{`C${conditionIndex + 1}`}</Tag>
                            <Text type='tertiary' size='small'>
                              {condition.path || t('未设置路径')}
                            </Text>
                          </Space>
                        }
                      >
                        <div>
                          <div className='flex items-center justify-between mb-2'>
                            <Text type='tertiary' size='small'>
                              {t('条件项设置')}
                            </Text>
                            <Button
                              theme='borderless'
                              type='danger'
                              icon={<IconDelete />}
                              size='small'
                              onClick={() =>
                                removeCondition(
                                  selectedOperation.id,
                                  condition.id,
                                )
                              }
                            >
                              {t('删除条件')}
                            </Button>
                          </div>
                          <Row gutter={12}>
                            <Col xs={24} md={10}>
                              <Text type='tertiary' size='small'>
                                {t('字段路径')}
                              </Text>
                              <Input
                                value={condition.path}
                                placeholder='model'
                                onChange={(nextValue) =>
                                  updateCondition(
                                    selectedOperation.id,
                                    condition.id,
                                    { path: nextValue },
                                  )
                                }
                              />
                            </Col>
                            <Col xs={24} md={8}>
                              <Text type='tertiary' size='small'>
                                {t('匹配方式')}
                              </Text>
                              <Select
                                value={condition.mode}
                                optionList={CONDITION_MODE_OPTIONS}
                                onChange={(nextValue) =>
                                  updateCondition(
                                    selectedOperation.id,
                                    condition.id,
                                    { mode: nextValue },
                                  )
                                }
                                style={{ width: '100%' }}
                              />
                            </Col>
                            <Col xs={24} md={6}>
                              <Text type='tertiary' size='small'>
                                {t('匹配值')}
                              </Text>
                              <Input
                                value={condition.value_text}
                                placeholder='gpt'
                                onChange={(nextValue) =>
                                  updateCondition(
                                    selectedOperation.id,
                                    condition.id,
                                    { value_text: nextValue },
                                  )
                                }
                              />
                            </Col>
                          </Row>
                          <div className='mt-2 flex flex-wrap gap-3'>
                            <div className='flex items-center gap-2'>
                              <Text type='tertiary' size='small'>
                                {t('条件取反')}
                              </Text>
                              <Switch
                                checked={Boolean(condition.invert)}
                                checkedText={t('开')}
                                uncheckedText={t('关')}
                                onChange={(nextValue) =>
                                  updateCondition(
                                    selectedOperation.id,
                                    condition.id,
                                    { invert: nextValue },
                                  )
                                }
                              />
                            </div>
                            <div className='flex items-center gap-2'>
                              <Text type='tertiary' size='small'>
                                {t('字段缺失视为命中')}
                              </Text>
                              <Switch
                                checked={Boolean(condition.pass_missing_key)}
                                checkedText={t('开')}
                                uncheckedText={t('关')}
                                onChange={(nextValue) =>
                                  updateCondition(
                                    selectedOperation.id,
                                    condition.id,
                                    {
                                      pass_missing_key: nextValue,
                                    },
                                  )
                                }
                              />
                            </div>
                          </div>
                        </div>
                      </Collapse.Panel>
                    ))}
                  </Collapse>
                )}
              </div>
            </Card>
          );
        })()
      ) : (
        <Card
          className='!rounded-2xl !border-0'
          bodyStyle={{
            padding: 14,
            background: 'var(--semi-color-fill-0)',
          }}
        >
          <Text type='tertiary'>{t('请选择一条规则进行编辑。')}</Text>
        </Card>
      )}

      {visualValidationError ? (
        <Card
          className='!rounded-2xl !border-0 mt-3'
          bodyStyle={{
            padding: 12,
            background: 'var(--semi-color-fill-0)',
          }}
        >
          <Space>
            <Tag color='red'>{t('暂存错误')}</Tag>
            <Text type='danger'>{visualValidationError}</Text>
          </Space>
        </Card>
      ) : null}
    </Col>
  );
}
