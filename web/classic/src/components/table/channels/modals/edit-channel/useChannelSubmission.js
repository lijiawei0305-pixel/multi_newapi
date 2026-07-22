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

import { Modal } from '@douyinfe/semi-ui';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  verifyJSON,
} from '../../../../../helpers';
import {
  collectInvalidStatusCodeEntries,
  collectNewDisallowedStatusCodeRedirects,
} from '../statusCodeRiskGuard';

export function useChannelSubmission(context) {
  const {
    batch,
    channelId,
    formApiRef,
    handleInputChange,
    initialModelMappingRef,
    initialModelsRef,
    initialStatusCodeMappingRef,
    inputs,
    isEdit,
    isMultiKeyChannel,
    keyMode,
    multiKeyMode,
    multiToSingle,
    originInputs,
    props,
    setInputs,
    setStatusCodeRiskConfirmVisible,
    setStatusCodeRiskDetailItems,
    statusCodeRiskConfirmResolverRef,
    t,
    useManualInput,
    vertexFileList,
    vertexKeys,
  } = context;

  const confirmMissingModelMappings = (missingModels) =>
    new Promise((resolve) => {
      const modal = Modal.confirm({
        title: t('模型未加入列表，可能无法调用'),
        content: (
          <div className='text-sm leading-6'>
            <div>
              {t(
                '模型重定向里的下列模型尚未添加到“模型”列表，调用时会因为缺少可用模型而失败：',
              )}
            </div>
            <div className='font-mono text-xs break-all text-red-600 mt-1'>
              {missingModels.join(', ')}
            </div>
            <div className='mt-2'>
              {t(
                '你可以在“自定义模型名称”处手动添加它们，然后点击填入后再提交，或者直接使用下方操作自动处理。',
              )}
            </div>
          </div>
        ),
        centered: true,
        footer: (
          <Space align='center' className='w-full justify-end'>
            <Button
              type='tertiary'
              onClick={() => {
                modal.destroy();
                resolve('cancel');
              }}
            >
              {t('返回修改')}
            </Button>
            <Button
              type='primary'
              theme='light'
              onClick={() => {
                modal.destroy();
                resolve('submit');
              }}
            >
              {t('直接提交')}
            </Button>
            <Button
              type='primary'
              theme='solid'
              onClick={() => {
                modal.destroy();
                resolve('add');
              }}
            >
              {t('添加后提交')}
            </Button>
          </Space>
        ),
      });
    });

  const resolveStatusCodeRiskConfirm = (confirmed) => {
    setStatusCodeRiskConfirmVisible(false);
    setStatusCodeRiskDetailItems([]);
    if (statusCodeRiskConfirmResolverRef.current) {
      statusCodeRiskConfirmResolverRef.current(confirmed);
      statusCodeRiskConfirmResolverRef.current = null;
    }
  };

  const confirmStatusCodeRisk = (detailItems) =>
    new Promise((resolve) => {
      statusCodeRiskConfirmResolverRef.current = resolve;
      setStatusCodeRiskDetailItems(detailItems);
      setStatusCodeRiskConfirmVisible(true);
    });

  const hasModelConfigChanged = (normalizedModels, modelMappingStr) => {
    if (!isEdit) return true;
    const initialModels = initialModelsRef.current;
    if (normalizedModels.length !== initialModels.length) {
      return true;
    }
    for (let i = 0; i < normalizedModels.length; i++) {
      if (normalizedModels[i] !== initialModels[i]) {
        return true;
      }
    }
    const normalizedMapping = (modelMappingStr || '').trim();
    const initialMapping = (initialModelMappingRef.current || '').trim();
    return normalizedMapping !== initialMapping;
  };

  const submit = async () => {
    const formValues = formApiRef.current ? formApiRef.current.getValues() : {};
    let localInputs = { ...formValues };
    localInputs.param_override = inputs.param_override;

    if (localInputs.type === 57) {
      if (batch) {
        showInfo(t('Codex 渠道不支持批量创建'));
        return;
      }

      const rawKey = (localInputs.key || '').trim();
      if (!isEdit && rawKey === '') {
        showInfo(t('请输入密钥！'));
        return;
      }

      if (rawKey !== '') {
        if (!verifyJSON(rawKey)) {
          showInfo(t('密钥必须是合法的 JSON 格式！'));
          return;
        }
        try {
          const parsed = JSON.parse(rawKey);
          if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
            showInfo(t('密钥必须是 JSON 对象'));
            return;
          }
          const accessToken = String(parsed.access_token || '').trim();
          const accountId = String(parsed.account_id || '').trim();
          if (!accessToken) {
            showInfo(t('密钥 JSON 必须包含 access_token'));
            return;
          }
          if (!accountId) {
            showInfo(t('密钥 JSON 必须包含 account_id'));
            return;
          }
          localInputs.key = JSON.stringify(parsed);
        } catch (error) {
          showInfo(t('密钥必须是合法的 JSON 格式！'));
          return;
        }
      }
    }

    if (localInputs.type === 41) {
      const keyType = localInputs.vertex_key_type || 'json';
      if (keyType === 'api_key') {
        // 直接作为普通字符串密钥处理
        if (!isEdit && (!localInputs.key || localInputs.key.trim() === '')) {
          showInfo(t('请输入密钥！'));
          return;
        }
      } else {
        // JSON 服务账号密钥
        if (useManualInput) {
          if (localInputs.key && localInputs.key.trim() !== '') {
            try {
              const parsedKey = JSON.parse(localInputs.key);
              localInputs.key = JSON.stringify(parsedKey);
            } catch (err) {
              showError(t('密钥格式无效，请输入有效的 JSON 格式密钥'));
              return;
            }
          } else if (!isEdit) {
            showInfo(t('请输入密钥！'));
            return;
          }
        } else {
          // 文件上传模式
          let keys = vertexKeys;
          if (keys.length === 0 && vertexFileList.length > 0) {
            try {
              const parsed = await Promise.all(
                vertexFileList.map(async (item) => {
                  const fileObj = item.fileInstance;
                  if (!fileObj) return null;
                  const txt = await fileObj.text();
                  return JSON.parse(txt);
                }),
              );
              keys = parsed.filter(Boolean);
            } catch (err) {
              showError(t('解析密钥文件失败: {{msg}}', { msg: err.message }));
              return;
            }
          }
          if (keys.length === 0) {
            if (!isEdit) {
              showInfo(t('请上传密钥文件！'));
              return;
            } else {
              delete localInputs.key;
            }
          } else {
            localInputs.key = batch
              ? JSON.stringify(keys)
              : JSON.stringify(keys[0]);
          }
        }
      }
    }

    // 如果是编辑模式且 key 为空字符串，避免提交空值覆盖旧密钥
    if (isEdit && (!localInputs.key || localInputs.key.trim() === '')) {
      delete localInputs.key;
    }
    delete localInputs.vertex_files;

    if (!isEdit && (!localInputs.name || !localInputs.key)) {
      showInfo(t('请填写渠道名称和渠道密钥！'));
      return;
    }
    if (!Array.isArray(localInputs.models) || localInputs.models.length === 0) {
      showInfo(t('请至少选择一个模型！'));
      return;
    }
    if (
      localInputs.type === 45 &&
      (!localInputs.base_url || localInputs.base_url.trim() === '')
    ) {
      showInfo(t('请输入API地址！'));
      return;
    }
    const hasModelMapping =
      typeof localInputs.model_mapping === 'string' &&
      localInputs.model_mapping.trim() !== '';
    let parsedModelMapping = null;
    if (hasModelMapping) {
      if (!verifyJSON(localInputs.model_mapping)) {
        showInfo(t('模型映射必须是合法的 JSON 格式！'));
        return;
      }
      try {
        parsedModelMapping = JSON.parse(localInputs.model_mapping);
      } catch (error) {
        showInfo(t('模型映射必须是合法的 JSON 格式！'));
        return;
      }
    }

    const normalizedModels = (localInputs.models || [])
      .map((model) => (model || '').trim())
      .filter(Boolean);
    localInputs.models = normalizedModels;

    if (
      parsedModelMapping &&
      typeof parsedModelMapping === 'object' &&
      !Array.isArray(parsedModelMapping)
    ) {
      const modelSet = new Set(normalizedModels);
      const missingModels = Object.keys(parsedModelMapping)
        .map((key) => (key || '').trim())
        .filter((key) => key && !modelSet.has(key));
      const shouldPromptMissing =
        missingModels.length > 0 &&
        hasModelConfigChanged(normalizedModels, localInputs.model_mapping);
      if (shouldPromptMissing) {
        const confirmAction = await confirmMissingModelMappings(missingModels);
        if (confirmAction === 'cancel') {
          return;
        }
        if (confirmAction === 'add') {
          const updatedModels = Array.from(
            new Set([...normalizedModels, ...missingModels]),
          );
          localInputs.models = updatedModels;
          handleInputChange('models', updatedModels);
        }
      }
    }

    const invalidStatusCodeEntries = collectInvalidStatusCodeEntries(
      localInputs.status_code_mapping,
    );
    if (invalidStatusCodeEntries.length > 0) {
      showError(
        `${t('状态码复写包含无效的状态码')}: ${invalidStatusCodeEntries.join(', ')}`,
      );
      return;
    }

    const riskyStatusCodeRedirects = collectNewDisallowedStatusCodeRedirects(
      initialStatusCodeMappingRef.current,
      localInputs.status_code_mapping,
    );
    if (riskyStatusCodeRedirects.length > 0) {
      const confirmed = await confirmStatusCodeRisk(riskyStatusCodeRedirects);
      if (!confirmed) {
        return;
      }
    }

    if (localInputs.base_url && localInputs.base_url.endsWith('/')) {
      localInputs.base_url = localInputs.base_url.slice(
        0,
        localInputs.base_url.length - 1,
      );
    }
    if (localInputs.type === 18 && localInputs.other === '') {
      localInputs.other = 'v2.1';
    }

    // 生成渠道额外设置JSON
    const channelExtraSettings = {
      force_format: localInputs.force_format || false,
      thinking_to_content: localInputs.thinking_to_content || false,
      proxy: localInputs.proxy || '',
      pass_through_body_enabled: localInputs.pass_through_body_enabled || false,
      system_prompt: localInputs.system_prompt || '',
      system_prompt_override: localInputs.system_prompt_override || false,
    };
    localInputs.setting = JSON.stringify(channelExtraSettings);

    // 处理 settings 字段（包括企业账户设置和字段透传控制）
    let settings = {};
    if (localInputs.settings) {
      try {
        settings = JSON.parse(localInputs.settings);
      } catch (error) {
        console.error('解析settings失败:', error);
      }
    }

    // type === 20: 设置企业账户标识，无论是true还是false都要传到后端
    if (localInputs.type === 20) {
      settings.openrouter_enterprise =
        localInputs.is_enterprise_account === true;
    }

    // type === 33 (AWS): 保存 aws_key_type 到 settings
    if (localInputs.type === 33) {
      settings.aws_key_type = localInputs.aws_key_type || 'ak_sk';
    }

    // type === 41 (Vertex): 始终保存 vertex_key_type 到 settings，避免编辑时被重置
    if (localInputs.type === 41) {
      settings.vertex_key_type = localInputs.vertex_key_type || 'json';
    } else if ('vertex_key_type' in settings) {
      delete settings.vertex_key_type;
    }

    // type === 1 (OpenAI) 或 type === 14 (Claude): 设置字段透传控制（显式保存布尔值）
    if (localInputs.type === 1 || localInputs.type === 14) {
      settings.allow_service_tier = localInputs.allow_service_tier === true;
      // 仅 OpenAI 渠道需要 store / safety_identifier / include_obfuscation
      if (localInputs.type === 1) {
        settings.disable_store = localInputs.disable_store === true;
        settings.allow_safety_identifier =
          localInputs.allow_safety_identifier === true;
        settings.allow_include_obfuscation =
          localInputs.allow_include_obfuscation === true;
      }
      if (localInputs.type === 14) {
        settings.allow_inference_geo = localInputs.allow_inference_geo === true;
        settings.allow_speed = localInputs.allow_speed === true;
        settings.claude_beta_query = localInputs.claude_beta_query === true;
      }
    }

    settings.upstream_model_update_check_enabled =
      localInputs.upstream_model_update_check_enabled === true;
    settings.upstream_model_update_auto_sync_enabled =
      settings.upstream_model_update_check_enabled &&
      localInputs.upstream_model_update_auto_sync_enabled === true;
    settings.upstream_model_update_ignored_models = Array.from(
      new Set(
        String(localInputs.upstream_model_update_ignored_models || '')
          .split(',')
          .map((model) => model.trim())
          .filter(Boolean),
      ),
    );
    if (
      !Array.isArray(settings.upstream_model_update_last_detected_models) ||
      !settings.upstream_model_update_check_enabled
    ) {
      settings.upstream_model_update_last_detected_models = [];
    }
    if (typeof settings.upstream_model_update_last_check_time !== 'number') {
      settings.upstream_model_update_last_check_time = 0;
    }

    localInputs.settings = JSON.stringify(settings);

    // 清理不需要发送到后端的字段
    delete localInputs.force_format;
    delete localInputs.thinking_to_content;
    delete localInputs.proxy;
    delete localInputs.pass_through_body_enabled;
    delete localInputs.system_prompt;
    delete localInputs.system_prompt_override;
    delete localInputs.is_enterprise_account;
    // 顶层的 vertex_key_type 不应发送给后端
    delete localInputs.vertex_key_type;
    // 顶层的 aws_key_type 不应发送给后端
    delete localInputs.aws_key_type;
    // 清理字段透传控制的临时字段
    delete localInputs.allow_service_tier;
    delete localInputs.disable_store;
    delete localInputs.allow_safety_identifier;
    delete localInputs.allow_include_obfuscation;
    delete localInputs.allow_inference_geo;
    delete localInputs.allow_speed;
    delete localInputs.claude_beta_query;
    delete localInputs.upstream_model_update_check_enabled;
    delete localInputs.upstream_model_update_auto_sync_enabled;
    delete localInputs.upstream_model_update_last_check_time;
    delete localInputs.upstream_model_update_last_detected_models;
    delete localInputs.upstream_model_update_ignored_models;

    let res;
    localInputs.auto_ban = localInputs.auto_ban ? 1 : 0;
    localInputs.models = localInputs.models.join(',');
    localInputs.group = (localInputs.groups || []).join(',');

    let mode = 'single';
    if (batch) {
      mode = multiToSingle ? 'multi_to_single' : 'batch';
    }

    if (isEdit) {
      res = await API.put(`/api/channel/`, {
        ...localInputs,
        id: parseInt(channelId),
        key_mode: isMultiKeyChannel ? keyMode : undefined, // 只在多key模式下传递
      });
    } else {
      res = await API.post(`/api/channel/`, {
        mode: mode,
        multi_key_mode: mode === 'multi_to_single' ? multiKeyMode : undefined,
        channel: localInputs,
      });
    }
    const { success, message } = res.data;
    if (success) {
      if (isEdit) {
        showSuccess(t('渠道更新成功！'));
      } else {
        showSuccess(t('渠道创建成功！'));
        setInputs(originInputs);
      }
      props.refresh();
      props.handleClose();
    } else {
      showError(message);
    }
  };
  return { resolveStatusCodeRiskConfirm, submit };
}
