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

import { API, getChannelModels, showError } from '../../../../../helpers';

export function createChannelLoader(context) {
  const {
    channelId,
    formApiRef,
    initialBaseUrlRef,
    initialModelMappingRef,
    initialModelsRef,
    initialStatusCodeMappingRef,
    setAdvancedSettingsOpen,
    setAutoBan,
    setBasicModels,
    setBatch,
    setChannelSettings,
    setInputs,
    setIonetMetadata,
    setIsEnterpriseAccount,
    setIsIonetChannel,
    setIsMultiKeyChannel,
    setLoading,
    setMultiKeyMode,
    setMultiToSingle,
  } = context;

  const loadChannel = async () => {
    setLoading(true);
    let res = await API.get(`/api/channel/${channelId}`);
    if (res === undefined) {
      return;
    }
    const { success, message, data } = res.data;
    if (success) {
      if (data.models === '') {
        data.models = [];
      } else {
        data.models = data.models.split(',');
      }
      if (data.group === '') {
        data.groups = [];
      } else {
        data.groups = data.group.split(',');
      }
      if (data.model_mapping !== '') {
        data.model_mapping = JSON.stringify(
          JSON.parse(data.model_mapping),
          null,
          2,
        );
      }
      const chInfo = data.channel_info || {};
      const isMulti = chInfo.is_multi_key === true;
      setIsMultiKeyChannel(isMulti);
      if (isMulti) {
        setBatch(true);
        setMultiToSingle(true);
        const modeVal = chInfo.multi_key_mode || 'random';
        setMultiKeyMode(modeVal);
        data.multi_key_mode = modeVal;
      } else {
        setBatch(false);
        setMultiToSingle(false);
      }
      // 解析渠道额外设置并合并到data中
      if (data.setting) {
        try {
          const parsedSettings = JSON.parse(data.setting);
          data.force_format = parsedSettings.force_format || false;
          data.thinking_to_content =
            parsedSettings.thinking_to_content || false;
          data.proxy = parsedSettings.proxy || '';
          data.pass_through_body_enabled =
            parsedSettings.pass_through_body_enabled || false;
          data.system_prompt = parsedSettings.system_prompt || '';
          data.system_prompt_override =
            parsedSettings.system_prompt_override || false;
        } catch (error) {
          console.error('解析渠道设置失败:', error);
          data.force_format = false;
          data.thinking_to_content = false;
          data.proxy = '';
          data.pass_through_body_enabled = false;
          data.system_prompt = '';
          data.system_prompt_override = false;
        }
      } else {
        data.force_format = false;
        data.thinking_to_content = false;
        data.proxy = '';
        data.pass_through_body_enabled = false;
        data.system_prompt = '';
        data.system_prompt_override = false;
      }

      if (data.settings) {
        try {
          const parsedSettings = JSON.parse(data.settings);
          data.azure_responses_version =
            parsedSettings.azure_responses_version || '';
          // 读取 Vertex 密钥格式
          data.vertex_key_type = parsedSettings.vertex_key_type || 'json';
          // 读取 AWS 密钥格式和区域
          data.aws_key_type = parsedSettings.aws_key_type || 'ak_sk';
          // 读取企业账户设置
          data.is_enterprise_account =
            parsedSettings.openrouter_enterprise === true;
          // 读取字段透传控制设置
          data.allow_service_tier = parsedSettings.allow_service_tier || false;
          data.disable_store = parsedSettings.disable_store || false;
          data.allow_safety_identifier =
            parsedSettings.allow_safety_identifier || false;
          data.allow_include_obfuscation =
            parsedSettings.allow_include_obfuscation || false;
          data.allow_inference_geo =
            parsedSettings.allow_inference_geo || false;
          data.allow_speed = parsedSettings.allow_speed || false;
          data.claude_beta_query = parsedSettings.claude_beta_query || false;
          data.upstream_model_update_check_enabled =
            parsedSettings.upstream_model_update_check_enabled === true;
          data.upstream_model_update_auto_sync_enabled =
            parsedSettings.upstream_model_update_auto_sync_enabled === true;
          data.upstream_model_update_last_check_time =
            Number(parsedSettings.upstream_model_update_last_check_time) || 0;
          data.upstream_model_update_last_detected_models = Array.isArray(
            parsedSettings.upstream_model_update_last_detected_models,
          )
            ? parsedSettings.upstream_model_update_last_detected_models
            : [];
          data.upstream_model_update_ignored_models = Array.isArray(
            parsedSettings.upstream_model_update_ignored_models,
          )
            ? parsedSettings.upstream_model_update_ignored_models.join(',')
            : '';
        } catch (error) {
          console.error('解析其他设置失败:', error);
          data.azure_responses_version = '';
          data.region = '';
          data.vertex_key_type = 'json';
          data.aws_key_type = 'ak_sk';
          data.is_enterprise_account = false;
          data.allow_service_tier = false;
          data.disable_store = false;
          data.allow_safety_identifier = false;
          data.allow_include_obfuscation = false;
          data.allow_inference_geo = false;
          data.allow_speed = false;
          data.claude_beta_query = false;
          data.upstream_model_update_check_enabled = false;
          data.upstream_model_update_auto_sync_enabled = false;
          data.upstream_model_update_last_check_time = 0;
          data.upstream_model_update_last_detected_models = [];
          data.upstream_model_update_ignored_models = '';
        }
      } else {
        // 兼容历史数据：老渠道没有 settings 时，默认按 json 展示
        data.vertex_key_type = 'json';
        data.aws_key_type = 'ak_sk';
        data.is_enterprise_account = false;
        data.allow_service_tier = false;
        data.disable_store = false;
        data.allow_safety_identifier = false;
        data.allow_include_obfuscation = false;
        data.allow_inference_geo = false;
        data.allow_speed = false;
        data.claude_beta_query = false;
        data.upstream_model_update_check_enabled = false;
        data.upstream_model_update_auto_sync_enabled = false;
        data.upstream_model_update_last_check_time = 0;
        data.upstream_model_update_last_detected_models = [];
        data.upstream_model_update_ignored_models = '';
      }

      if (
        data.type === 45 &&
        (!data.base_url ||
          (typeof data.base_url === 'string' && data.base_url.trim() === ''))
      ) {
        data.base_url = 'https://ark.cn-beijing.volces.com';
      }

      initialBaseUrlRef.current = data.base_url || '';
      setInputs(data);
      if (formApiRef.current) {
        formApiRef.current.setValues(data);
      }
      if (data.auto_ban === 0) {
        setAutoBan(false);
      } else {
        setAutoBan(true);
      }
      // 同步企业账户状态
      setIsEnterpriseAccount(data.is_enterprise_account || false);
      setBasicModels(getChannelModels(data.type));
      // 同步更新channelSettings状态显示
      setChannelSettings({
        force_format: data.force_format,
        thinking_to_content: data.thinking_to_content,
        proxy: data.proxy,
        pass_through_body_enabled: data.pass_through_body_enabled,
        system_prompt: data.system_prompt,
        system_prompt_override: data.system_prompt_override || false,
      });
      initialModelsRef.current = (data.models || [])
        .map((model) => (model || '').trim())
        .filter(Boolean);
      initialModelMappingRef.current = data.model_mapping || '';
      initialStatusCodeMappingRef.current = data.status_code_mapping || '';

      let parsedIonet = null;
      if (data.other_info) {
        try {
          const maybeMeta = JSON.parse(data.other_info);
          if (
            maybeMeta &&
            typeof maybeMeta === 'object' &&
            maybeMeta.source === 'ionet'
          ) {
            parsedIonet = maybeMeta;
          }
        } catch (error) {
          // ignore parse error
        }
      }
      const managedByIonet = !!parsedIonet;
      setIsIonetChannel(managedByIonet);
      setIonetMetadata(parsedIonet);

      // Smart expand: auto-open advanced settings if any advanced field has a value
      const hasAdvancedValues =
        (data.model_mapping && data.model_mapping.trim()) ||
        (data.param_override && data.param_override.trim()) ||
        (data.status_code_mapping && data.status_code_mapping.trim()) ||
        (data.header_override && data.header_override.trim()) ||
        (data.tag && data.tag.trim()) ||
        (data.remark && data.remark.trim()) ||
        (data.priority && data.priority !== 0) ||
        (data.weight && data.weight !== 0) ||
        (data.proxy && data.proxy.trim()) ||
        (data.system_prompt && data.system_prompt.trim()) ||
        data.thinking_to_content ||
        data.pass_through_body_enabled ||
        data.force_format ||
        data.claude_beta_query ||
        data.system_prompt_override;
      if (hasAdvancedValues) {
        setAdvancedSettingsOpen(true);
      }
    } else {
      showError(message);
    }
    setLoading(false);
  };
  return loadChannel;
}
