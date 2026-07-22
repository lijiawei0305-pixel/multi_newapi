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

import React, { useEffect, useState, useRef, useMemo } from 'react';
import { useTranslation } from 'react-i18next';
import {
  API,
  showError,
  showInfo,
  showSuccess,
  verifyJSON,
} from '../../../../helpers';
import { useIsMobile } from '../../../../hooks/common/useIsMobile';
import {
  CHANNEL_OPTIONS,
  MODEL_FETCHABLE_CHANNEL_TYPES,
} from '../../../../constants';
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
  Highlight,
  Input,
  Tooltip,
  Collapse,
  Dropdown,
} from '@douyinfe/semi-ui';
import {
  getChannelModels,
  copy,
  getChannelIcon,
  getModelCategories,
  selectFilter,
} from '../../../../helpers';
import ModelSelectModal from './ModelSelectModal';
import SingleModelSelectModal from './SingleModelSelectModal';
import OllamaModelModal from './OllamaModelModal';
import ParamOverrideEditorModal from './ParamOverrideEditorModal';
import JSONEditor from '../../../common/ui/JSONEditor';
import SecureVerificationModal from '../../../common/modals/SecureVerificationModal';
import StatusCodeRiskGuardModal from './StatusCodeRiskGuardModal';
import ChannelKeyDisplay from '../../../common/ui/ChannelKeyDisplay';
import { useSecureVerification } from '../../../../hooks/common/useSecureVerification';
import { parseChannelConnectionString } from '../../../../helpers/token';
import { createApiCalls } from '../../../../services/secureVerification';
import {
  collectInvalidStatusCodeEntries,
  collectNewDisallowedStatusCodeRedirects,
} from './statusCodeRiskGuard';
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
import {
  openHttpUrlInNewTab,
  openSameOriginPathInNewTab,
} from '../../../../helpers/safeNavigation';

import { EditChannelEditorContext } from './edit-channel/EditorContext';
import EditChannelEditorView from './edit-channel/EditorView';
import { useChannelSubmission } from './edit-channel/useChannelSubmission';
import { createChannelLoader } from './edit-channel/createChannelLoader';
import { useChannelViewOptions } from './edit-channel/useChannelViewOptions';

const { Text, Title } = Typography;

import {
  UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT,
  ADVANCED_SETTINGS_EXPANDED_KEY,
  PARAM_OVERRIDE_LEGACY_TEMPLATE,
  PARAM_OVERRIDE_OPERATIONS_TEMPLATE,
  DEPRECATED_DOUBAO_CODING_PLAN_BASE_URL,
} from './edit-channel/constants';
const EditChannelModal = (props) => {
  const { t } = useTranslation();
  const channelId = props.editingChannel.id;
  const isEdit = channelId !== undefined;
  const [loading, setLoading] = useState(isEdit);
  const isMobile = useIsMobile();
  const handleCancel = () => {
    props.handleClose();
  };
  const originInputs = {
    name: '',
    type: 1,
    key: '',
    openai_organization: '',
    max_input_tokens: 0,
    base_url: '',
    other: '',
    model_mapping: '',
    param_override: '',
    status_code_mapping: '',
    models: [],
    auto_ban: 1,
    test_model: '',
    groups: ['default'],
    priority: 0,
    weight: 0,
    tag: '',
    multi_key_mode: 'random',
    // 渠道额外设置的默认值
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    pass_through_body_enabled: false,
    system_prompt: '',
    system_prompt_override: false,
    settings: '',
    // 仅 Vertex: 密钥格式（存入 settings.vertex_key_type）
    vertex_key_type: 'json',
    // 仅 AWS: 密钥格式和区域（存入 settings.aws_key_type 和 settings.aws_region）
    aws_key_type: 'ak_sk',
    // 企业账户设置
    is_enterprise_account: false,
    // 字段透传控制默认值
    allow_service_tier: false,
    disable_store: false, // false = 允许透传（默认开启）
    allow_safety_identifier: false,
    allow_include_obfuscation: false,
    allow_inference_geo: false,
    allow_speed: false,
    claude_beta_query: false,
    upstream_model_update_check_enabled: false,
    upstream_model_update_auto_sync_enabled: false,
    upstream_model_update_last_check_time: 0,
    upstream_model_update_last_detected_models: [],
    upstream_model_update_ignored_models: '',
  };
  const [batch, setBatch] = useState(false);
  const [multiToSingle, setMultiToSingle] = useState(false);
  const [multiKeyMode, setMultiKeyMode] = useState('random');
  const [autoBan, setAutoBan] = useState(true);
  const [inputs, setInputs] = useState(originInputs);
  const [originModelOptions, setOriginModelOptions] = useState([]);
  const [modelOptions, setModelOptions] = useState([]);
  const [groupOptions, setGroupOptions] = useState([]);
  const [basicModels, setBasicModels] = useState([]);
  const [fullModels, setFullModels] = useState([]);
  const [modelGroups, setModelGroups] = useState([]);
  const [customModel, setCustomModel] = useState('');
  const [modelSearchValue, setModelSearchValue] = useState('');
  const [modalImageUrl, setModalImageUrl] = useState('');
  const [isModalOpenurl, setIsModalOpenurl] = useState(false);
  const [modelModalVisible, setModelModalVisible] = useState(false);
  const [fetchedModels, setFetchedModels] = useState([]);
  const [modelMappingValueModalVisible, setModelMappingValueModalVisible] =
    useState(false);
  const [modelMappingValueModalModels, setModelMappingValueModalModels] =
    useState([]);
  const [modelMappingValueKey, setModelMappingValueKey] = useState('');
  const [modelMappingValueSelected, setModelMappingValueSelected] =
    useState('');
  const [ollamaModalVisible, setOllamaModalVisible] = useState(false);
  const formApiRef = useRef(null);
  const [vertexKeys, setVertexKeys] = useState([]);
  const [vertexFileList, setVertexFileList] = useState([]);
  const vertexErroredNames = useRef(new Set()); // 避免重复报错
  const [isMultiKeyChannel, setIsMultiKeyChannel] = useState(false);
  const [channelSearchValue, setChannelSearchValue] = useState('');
  const [useManualInput, setUseManualInput] = useState(false); // 是否使用手动输入模式
  const [keyMode, setKeyMode] = useState('append'); // 密钥模式：replace（覆盖）或 append（追加）
  const [isEnterpriseAccount, setIsEnterpriseAccount] = useState(false); // 是否为企业账户
  const [doubaoApiEditUnlocked, setDoubaoApiEditUnlocked] = useState(false); // 豆包渠道自定义 API 地址隐藏入口
  const redirectModelList = useMemo(() => {
    const mapping = inputs.model_mapping;
    if (typeof mapping !== 'string') return [];
    const trimmed = mapping.trim();
    if (!trimmed) return [];
    try {
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        return [];
      }
      const values = Object.values(parsed)
        .map((value) => (typeof value === 'string' ? value.trim() : undefined))
        .filter((value) => value);
      return Array.from(new Set(values));
    } catch (error) {
      return [];
    }
  }, [inputs.model_mapping]);
  const redirectModelKeyList = useMemo(() => {
    const mapping = inputs.model_mapping;
    if (typeof mapping !== 'string') return [];
    const trimmed = mapping.trim();
    if (!trimmed) return [];
    try {
      const parsed = JSON.parse(trimmed);
      if (!parsed || typeof parsed !== 'object' || Array.isArray(parsed)) {
        return [];
      }
      const keys = Object.keys(parsed)
        .map((key) => key.trim())
        .filter((key) => key);
      return Array.from(new Set(keys));
    } catch (error) {
      return [];
    }
  }, [inputs.model_mapping]);
  const upstreamDetectedModels = useMemo(
    () =>
      Array.from(
        new Set(
          (inputs.upstream_model_update_last_detected_models || [])
            .map((model) => String(model || '').trim())
            .filter(Boolean),
        ),
      ),
    [inputs.upstream_model_update_last_detected_models],
  );
  const upstreamDetectedModelsPreview = useMemo(
    () =>
      upstreamDetectedModels.slice(0, UPSTREAM_DETECTED_MODEL_PREVIEW_LIMIT),
    [upstreamDetectedModels],
  );
  const upstreamDetectedModelsOmittedCount =
    upstreamDetectedModels.length - upstreamDetectedModelsPreview.length;
  const modelSearchMatchedCount = useMemo(() => {
    const keyword = modelSearchValue.trim();
    if (!keyword) {
      return modelOptions.length;
    }
    return modelOptions.reduce(
      (count, option) => count + (selectFilter(keyword, option) ? 1 : 0),
      0,
    );
  }, [modelOptions, modelSearchValue]);
  const modelSearchHintText = useMemo(() => {
    const keyword = modelSearchValue.trim();
    if (!keyword || modelSearchMatchedCount !== 0) {
      return '';
    }
    return t('未匹配到模型，按回车键可将「{{name}}」作为自定义模型名添加', {
      name: keyword,
    });
  }, [modelSearchMatchedCount, modelSearchValue, t]);
  const paramOverrideMeta = useMemo(() => {
    const raw =
      typeof inputs.param_override === 'string'
        ? inputs.param_override.trim()
        : '';
    if (!raw) {
      return {
        tagLabel: t('不更改'),
        tagColor: 'grey',
        preview: t('此项可选，用于覆盖请求参数。不支持覆盖 stream 参数'),
      };
    }
    if (!verifyJSON(raw)) {
      return {
        tagLabel: t('JSON格式错误'),
        tagColor: 'red',
        preview: raw,
      };
    }
    try {
      const parsed = JSON.parse(raw);
      const pretty = JSON.stringify(parsed, null, 2);
      if (
        parsed &&
        typeof parsed === 'object' &&
        !Array.isArray(parsed) &&
        Array.isArray(parsed.operations)
      ) {
        return {
          tagLabel: `${t('新格式模板')} (${parsed.operations.length})`,
          tagColor: 'cyan',
          preview: pretty,
        };
      }
      if (parsed && typeof parsed === 'object' && !Array.isArray(parsed)) {
        return {
          tagLabel: `${t('旧格式模板')} (${Object.keys(parsed).length})`,
          tagColor: 'blue',
          preview: pretty,
        };
      }
      return {
        tagLabel: t('自定义 JSON'),
        tagColor: 'orange',
        preview: pretty,
      };
    } catch (error) {
      return {
        tagLabel: t('JSON格式错误'),
        tagColor: 'red',
        preview: raw,
      };
    }
  }, [inputs.param_override, t]);
  const [isIonetChannel, setIsIonetChannel] = useState(false);
  const [ionetMetadata, setIonetMetadata] = useState(null);
  const [codexCredentialRefreshing, setCodexCredentialRefreshing] =
    useState(false);
  const [paramOverrideEditorVisible, setParamOverrideEditorVisible] =
    useState(false);

  // 密钥显示状态
  const [keyDisplayState, setKeyDisplayState] = useState({
    showModal: false,
    keyData: '',
  });

  // 专门的2FA验证状态（用于TwoFactorAuthModal）
  const [show2FAVerifyModal, setShow2FAVerifyModal] = useState(false);
  const [verifyCode, setVerifyCode] = useState('');

  useEffect(() => {
    if (!isEdit) {
      setIsIonetChannel(false);
      setIonetMetadata(null);
    }
  }, [isEdit]);

  const handleOpenIonetDeployment = () => {
    if (!ionetMetadata?.deployment_id) {
      return;
    }
    const targetUrl = `/console/deployment?deployment_id=${ionetMetadata.deployment_id}`;
    openSameOriginPathInNewTab(targetUrl);
  };
  const [verifyLoading, setVerifyLoading] = useState(false);
  const statusCodeRiskConfirmResolverRef = useRef(null);
  const [statusCodeRiskConfirmVisible, setStatusCodeRiskConfirmVisible] =
    useState(false);
  const [statusCodeRiskDetailItems, setStatusCodeRiskDetailItems] = useState(
    [],
  );

  // 剪贴板连接信息自动检测
  const [clipboardConfig, setClipboardConfig] = useState(null);

  // 高级设置折叠状态
  const [advancedSettingsOpen, setAdvancedSettingsOpen] = useState(false);
  const toggleAdvancedSettings = (open) => {
    setAdvancedSettingsOpen(open);
    localStorage.setItem(ADVANCED_SETTINGS_EXPANDED_KEY, String(open));
  };
  const formContainerRef = useRef(null);
  const doubaoApiClickCountRef = useRef(0);
  const initialBaseUrlRef = useRef('');
  const initialModelsRef = useRef([]);
  const initialModelMappingRef = useRef('');
  const initialStatusCodeMappingRef = useRef('');
  const doubaoCodingPlanDeprecationMessage =
    'Doubao Coding Plan 不再允许新增。根据火山方舟文档，Coding 套餐额度仅适用于 AI Coding 产品内调用，不适用于单独 API 调用；在非 AI Coding 产品中使用对应的 Base URL 和 API Key 可能被视为违规，并可能导致订阅停用或账号封禁。';
  const canKeepDeprecatedDoubaoCodingPlan =
    initialBaseUrlRef.current === DEPRECATED_DOUBAO_CODING_PLAN_BASE_URL;
  const doubaoCodingPlanOptionLabel = (
    <Tooltip content={doubaoCodingPlanDeprecationMessage} position='left'>
      <span className='inline-flex items-center gap-2'>
        <span>Doubao Coding Plan</span>
      </span>
    </Tooltip>
  );

  // 使用通用安全验证 Hook
  const {
    isModalVisible,
    verificationMethods,
    verificationState,
    withVerification,
    executeVerification,
    cancelVerification,
    setVerificationCode,
    switchVerificationMethod,
  } = useSecureVerification({
    onSuccess: (result) => {
      // 验证成功后显示密钥
      console.log('Verification success, result:', result);
      if (result && result.success && result.data?.key) {
        showSuccess(t('密钥获取成功'));
        setKeyDisplayState({
          showModal: true,
          keyData: result.data.key,
        });
      } else if (result && result.key) {
        // 直接返回了 key（没有包装在 data 中）
        showSuccess(t('密钥获取成功'));
        setKeyDisplayState({
          showModal: true,
          keyData: result.key,
        });
      }
    },
  });

  // 重置密钥显示状态
  const resetKeyDisplayState = () => {
    setKeyDisplayState({
      showModal: false,
      keyData: '',
    });
  };

  // 重置2FA验证状态
  const reset2FAVerifyState = () => {
    setShow2FAVerifyModal(false);
    setVerifyCode('');
    setVerifyLoading(false);
  };

  const handleApiConfigSecretClick = () => {
    if (inputs.type !== 45) return;
    const next = doubaoApiClickCountRef.current + 1;
    doubaoApiClickCountRef.current = next;
    if (next >= 10) {
      setDoubaoApiEditUnlocked((unlocked) => {
        if (!unlocked) {
          showInfo(t('已解锁豆包自定义 API 地址编辑'));
        }
        return true;
      });
    }
  };

  // 渠道额外设置状态
  const [channelSettings, setChannelSettings] = useState({
    force_format: false,
    thinking_to_content: false,
    proxy: '',
    pass_through_body_enabled: false,
    system_prompt: '',
  });
  const showApiConfigCard = true; // 控制是否显示 API 配置卡片
  const getInitValues = () => ({ ...originInputs });

  // 处理渠道额外设置的更新
  const handleChannelSettingsChange = (key, value) => {
    // 更新内部状态
    setChannelSettings((prev) => ({ ...prev, [key]: value }));

    // 同步更新到表单字段
    if (formApiRef.current) {
      formApiRef.current.setValue(key, value);
    }

    // 同步更新inputs状态
    setInputs((prev) => ({ ...prev, [key]: value }));

    // 生成setting JSON并更新
    const newSettings = { ...channelSettings, [key]: value };
    const settingsJson = JSON.stringify(newSettings);
    handleInputChange('setting', settingsJson);
  };

  const handleChannelOtherSettingsChange = (key, value) => {
    // 更新内部状态
    setChannelSettings((prev) => ({ ...prev, [key]: value }));

    // 同步更新到表单字段
    if (formApiRef.current) {
      formApiRef.current.setValue(key, value);
    }

    // 同步更新inputs状态
    setInputs((prev) => ({ ...prev, [key]: value }));

    // 需要更新settings，是一个json，例如{"azure_responses_version": "preview"}
    let settings = {};
    if (inputs.settings) {
      try {
        settings = JSON.parse(inputs.settings);
      } catch (error) {
        console.error('解析设置失败:', error);
      }
    }
    settings[key] = value;
    const settingsJson = JSON.stringify(settings);
    handleInputChange('settings', settingsJson);
  };

  const applyClipboardConfig = (config) => {
    if (!config) return;
    setInputs((prev) => ({
      ...prev,
      key: config.key,
      base_url: config.url,
    }));
    if (formApiRef.current) {
      formApiRef.current.setValue('key', config.key);
      formApiRef.current.setValue('base_url', config.url);
    }
    setClipboardConfig(null);
    showSuccess(t('连接信息已填入'));
  };

  const pasteFromClipboard = async () => {
    if (!navigator?.clipboard?.readText) {
      showError(t('无法读取剪贴板'));
      return;
    }
    try {
      const text = await navigator.clipboard.readText();
      const parsed = parseChannelConnectionString(text);
      if (parsed) {
        applyClipboardConfig(parsed);
      } else {
        showInfo(t('剪贴板中未检测到连接信息'));
      }
    } catch {
      showError(t('无法读取剪贴板'));
    }
  };

  const isIonetLocked = isIonetChannel && isEdit;

  const handleInputChange = (name, value) => {
    if (
      isIonetChannel &&
      isEdit &&
      ['type', 'key', 'base_url'].includes(name)
    ) {
      return;
    }
    if (formApiRef.current) {
      formApiRef.current.setValue(name, value);
    }
    if (name === 'models' && Array.isArray(value)) {
      value = Array.from(new Set(value.map((m) => (m || '').trim())));
    }

    if (name === 'base_url' && value.endsWith('/v1')) {
      Modal.confirm({
        title: '警告',
        content:
          '不需要在末尾加/v1，New API会自动处理，添加后可能导致请求失败，是否继续？',
        onOk: () => {
          setInputs((inputs) => ({ ...inputs, [name]: value }));
        },
      });
      return;
    }
    setInputs((inputs) => ({ ...inputs, [name]: value }));
    if (name === 'type') {
      let localModels = [];
      switch (value) {
        case 2:
          localModels = [
            'mj_imagine',
            'mj_variation',
            'mj_reroll',
            'mj_blend',
            'mj_upscale',
            'mj_describe',
            'mj_uploads',
          ];
          break;
        case 5:
          localModels = [
            'swap_face',
            'mj_imagine',
            'mj_video',
            'mj_edits',
            'mj_variation',
            'mj_reroll',
            'mj_blend',
            'mj_upscale',
            'mj_describe',
            'mj_zoom',
            'mj_shorten',
            'mj_modal',
            'mj_inpaint',
            'mj_custom_zoom',
            'mj_high_variation',
            'mj_low_variation',
            'mj_pan',
            'mj_uploads',
          ];
          break;
        case 36:
          localModels = ['suno_music', 'suno_lyrics'];
          break;
        case 45:
          localModels = getChannelModels(value);
          setInputs((prevInputs) => ({
            ...prevInputs,
            base_url: 'https://ark.cn-beijing.volces.com',
          }));
          break;
        default:
          localModels = getChannelModels(value);
          break;
      }
      if (inputs.models.length === 0) {
        setInputs((inputs) => ({ ...inputs, models: localModels }));
      }
      setBasicModels(localModels);

      // 重置手动输入模式状态
      setUseManualInput(false);

      if (value === 57) {
        setBatch(false);
        setMultiToSingle(false);
        setMultiKeyMode('random');
        setVertexKeys([]);
        setVertexFileList([]);
        if (formApiRef.current) {
          formApiRef.current.setValue('vertex_files', []);
        }
        setInputs((prev) => ({ ...prev, vertex_files: [] }));
      }
    }
    //setAutoBan
  };

  const formatJsonField = (fieldName) => {
    const rawValue = (inputs?.[fieldName] ?? '').trim();
    if (!rawValue) return;

    try {
      const parsed = JSON.parse(rawValue);
      handleInputChange(fieldName, JSON.stringify(parsed, null, 2));
    } catch (error) {
      showError(`${t('JSON格式错误')}: ${error.message}`);
    }
  };

  const formatUnixTime = (timestamp) => {
    const value = Number(timestamp || 0);
    if (!value) {
      return t('暂无');
    }
    return new Date(value * 1000).toLocaleString();
  };

  const copyParamOverrideJson = async () => {
    const raw =
      typeof inputs.param_override === 'string'
        ? inputs.param_override.trim()
        : '';
    if (!raw) {
      showInfo(t('暂无可复制 JSON'));
      return;
    }

    let content = raw;
    if (verifyJSON(raw)) {
      try {
        content = JSON.stringify(JSON.parse(raw), null, 2);
      } catch (error) {
        content = raw;
      }
    }

    const ok = await copy(content);
    if (ok) {
      showSuccess(t('参数覆盖 JSON 已复制'));
    } else {
      showError(t('复制失败'));
    }
  };

  const parseParamOverrideInput = () => {
    const raw =
      typeof inputs.param_override === 'string'
        ? inputs.param_override.trim()
        : '';
    if (!raw) return null;
    if (!verifyJSON(raw)) {
      throw new Error(t('当前参数覆盖不是合法的 JSON'));
    }
    return JSON.parse(raw);
  };

  const applyParamOverrideTemplate = (
    templateType = 'operations',
    applyMode = 'fill',
  ) => {
    try {
      const parsedCurrent = parseParamOverrideInput();
      if (templateType === 'legacy') {
        if (applyMode === 'fill') {
          handleInputChange(
            'param_override',
            JSON.stringify(PARAM_OVERRIDE_LEGACY_TEMPLATE, null, 2),
          );
          return;
        }
        const currentLegacy =
          parsedCurrent &&
          typeof parsedCurrent === 'object' &&
          !Array.isArray(parsedCurrent) &&
          !Array.isArray(parsedCurrent.operations)
            ? parsedCurrent
            : {};
        const merged = {
          ...PARAM_OVERRIDE_LEGACY_TEMPLATE,
          ...currentLegacy,
        };
        handleInputChange('param_override', JSON.stringify(merged, null, 2));
        return;
      }

      if (applyMode === 'fill') {
        handleInputChange(
          'param_override',
          JSON.stringify(PARAM_OVERRIDE_OPERATIONS_TEMPLATE, null, 2),
        );
        return;
      }
      const currentOperations =
        parsedCurrent &&
        typeof parsedCurrent === 'object' &&
        !Array.isArray(parsedCurrent) &&
        Array.isArray(parsedCurrent.operations)
          ? parsedCurrent.operations
          : [];
      const merged = {
        operations: [
          ...currentOperations,
          ...PARAM_OVERRIDE_OPERATIONS_TEMPLATE.operations,
        ],
      };
      handleInputChange('param_override', JSON.stringify(merged, null, 2));
    } catch (error) {
      showError(error.message || t('模板应用失败'));
    }
  };

  const clearParamOverride = () => {
    handleInputChange('param_override', '');
  };

  const loadChannel = createChannelLoader({
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
  });

  const fetchUpstreamModelList = async (name, options = {}) => {
    const silent = !!options.silent;
    // if (inputs['type'] !== 1) {
    //   showError(t('仅支持 OpenAI 接口格式'));
    //   return;
    // }
    setLoading(true);
    const models = [];
    let err = false;

    if (isEdit) {
      // 如果是编辑模式，使用已有的 channelId 获取模型列表
      const res = await API.get('/api/channel/fetch_models/' + channelId, {
        skipErrorHandler: true,
      });
      if (res && res.data && res.data.success) {
        models.push(...res.data.data);
      } else {
        err = true;
      }
    } else {
      // 如果是新建模式，通过后端代理获取模型列表
      if (!inputs?.['key']) {
        showError(t('请填写密钥'));
        err = true;
      } else {
        try {
          const res = await API.post(
            '/api/channel/fetch_models',
            {
              base_url: inputs['base_url'],
              type: inputs['type'],
              key: inputs['key'],
            },
            { skipErrorHandler: true },
          );

          if (res && res.data && res.data.success) {
            models.push(...res.data.data);
          } else {
            err = true;
          }
        } catch (error) {
          console.error('Error fetching models:', error);
          err = true;
        }
      }
    }

    if (!err) {
      const uniqueModels = Array.from(new Set(models));
      setFetchedModels(uniqueModels);
      if (!silent) {
        setModelModalVisible(true);
      }
      setLoading(false);
      return uniqueModels;
    } else {
      showError(t('获取模型列表失败'));
    }
    setLoading(false);
    return null;
  };

  const openModelMappingValueModal = async ({ pairKey, value }) => {
    const mappingKey = String(pairKey ?? '').trim();
    if (!mappingKey) return;

    if (!MODEL_FETCHABLE_CHANNEL_TYPES.has(inputs.type)) {
      return;
    }

    let modelsToUse = fetchedModels;
    if (!Array.isArray(modelsToUse) || modelsToUse.length === 0) {
      const fetched = await fetchUpstreamModelList('models', { silent: true });
      if (Array.isArray(fetched)) {
        modelsToUse = fetched;
      }
    }

    if (!Array.isArray(modelsToUse) || modelsToUse.length === 0) {
      showInfo(t('暂无模型'));
      return;
    }

    const normalizedModelsToUse = Array.from(
      new Set(
        modelsToUse.map((model) => String(model ?? '').trim()).filter(Boolean),
      ),
    );
    const currentValue = String(value ?? '').trim();

    setModelMappingValueModalModels(normalizedModelsToUse);
    setModelMappingValueKey(mappingKey);
    setModelMappingValueSelected(
      normalizedModelsToUse.includes(currentValue) ? currentValue : '',
    );
    setModelMappingValueModalVisible(true);
  };

  const fetchModels = async () => {
    try {
      let res = await API.get(`/api/channel/models`);
      const localModelOptions = res.data.data.map((model) => {
        const id = (model.id || '').trim();
        return {
          key: id,
          label: id,
          value: id,
        };
      });
      setOriginModelOptions(localModelOptions);
      setFullModels(res.data.data.map((model) => model.id));
      setBasicModels(
        res.data.data
          .filter((model) => {
            return model.id.startsWith('gpt-') || model.id.startsWith('text-');
          })
          .map((model) => model.id),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchGroups = async () => {
    try {
      let res = await API.get(`/api/group/`);
      if (res === undefined) {
        return;
      }
      setGroupOptions(
        res.data.data.map((group) => ({
          label: group,
          value: group,
        })),
      );
    } catch (error) {
      showError(error.message);
    }
  };

  const fetchModelGroups = async () => {
    try {
      const res = await API.get('/api/prefill_group?type=model');
      if (res?.data?.success) {
        setModelGroups(res.data.data || []);
      }
    } catch (error) {
      // ignore
    }
  };

  // 查看渠道密钥（透明验证）
  const handleShow2FAModal = async () => {
    try {
      // 使用 withVerification 包装，会自动处理需要验证的情况
      const result = await withVerification(
        createApiCalls.viewChannelKey(channelId),
        {
          title: t('查看渠道密钥'),
          description: t('为了保护账户安全，请验证您的身份。'),
          preferredMethod: 'passkey', // 优先使用 Passkey
        },
      );

      // 如果直接返回了结果（已验证），显示密钥
      if (result && result.success && result.data?.key) {
        showSuccess(t('密钥获取成功'));
        setKeyDisplayState({
          showModal: true,
          keyData: result.data.key,
        });
      }
    } catch (error) {
      console.error('Failed to view channel key:', error);
      showError(error.message || t('获取密钥失败'));
    }
  };

  const handleRefreshCodexCredential = async () => {
    if (!isEdit) return;

    setCodexCredentialRefreshing(true);
    try {
      const res = await API.post(
        `/api/channel/${channelId}/codex/refresh`,
        {},
        { skipErrorHandler: true },
      );
      if (!res?.data?.success) {
        throw new Error(res?.data?.message || 'Failed to refresh credential');
      }
      showSuccess(t('凭证已刷新'));
    } catch (error) {
      showError(error.message || t('刷新失败'));
    } finally {
      setCodexCredentialRefreshing(false);
    }
  };

  useEffect(() => {
    if (inputs.type !== 45) {
      doubaoApiClickCountRef.current = 0;
      setDoubaoApiEditUnlocked(false);
    }
  }, [inputs.type]);

  useEffect(() => {
    const modelMap = new Map();

    originModelOptions.forEach((option) => {
      const v = (option.value || '').trim();
      if (!modelMap.has(v)) {
        modelMap.set(v, option);
      }
    });

    inputs.models.forEach((model) => {
      const v = (model || '').trim();
      if (!modelMap.has(v)) {
        modelMap.set(v, {
          key: v,
          label: v,
          value: v,
        });
      }
    });

    const categories = getModelCategories(t);
    const optionsWithIcon = Array.from(modelMap.values()).map((opt) => {
      const modelName = opt.value;
      let icon = null;
      for (const [key, category] of Object.entries(categories)) {
        if (key !== 'all' && category.filter({ model_name: modelName })) {
          icon = category.icon;
          break;
        }
      }
      return {
        ...opt,
        label: (
          <span className='flex items-center gap-1'>
            {icon}
            {modelName}
          </span>
        ),
      };
    });

    setModelOptions(optionsWithIcon);
  }, [originModelOptions, inputs.models, t]);

  useEffect(() => {
    fetchModels().then();
    fetchGroups().then();
    if (!isEdit) {
      initialBaseUrlRef.current = '';
      setInputs(originInputs);
      if (formApiRef.current) {
        formApiRef.current.setValues(originInputs);
      }
      let localModels = getChannelModels(inputs.type);
      setBasicModels(localModels);
      setInputs((inputs) => ({ ...inputs, models: localModels }));
    }
  }, [props.editingChannel.id]);

  useEffect(() => {
    if (formApiRef.current) {
      formApiRef.current.setValues(inputs);
    }
  }, [inputs]);

  useEffect(() => {
    setModelSearchValue('');
    if (props.visible) {
      if (isEdit) {
        loadChannel();
      } else {
        formApiRef.current?.setValues(getInitValues());
        try {
          navigator?.clipboard
            ?.readText()
            ?.then((text) => {
              const parsed = parseChannelConnectionString(text);
              if (parsed) {
                setClipboardConfig(parsed);
              }
            })
            .catch(() => {});
        } catch {}
      }
      fetchModelGroups();
      // 重置手动输入模式状态
      setUseManualInput(false);
      // 编辑模式下恢复用户偏好，创建模式一律折叠
      setAdvancedSettingsOpen(
        isEdit &&
          localStorage.getItem(ADVANCED_SETTINGS_EXPANDED_KEY) === 'true',
      );
    } else {
      // 统一的模态框关闭重置逻辑
      resetModalState();
    }
  }, [props.visible, channelId]);

  useEffect(() => {
    if (!isEdit) {
      initialModelsRef.current = [];
      initialModelMappingRef.current = '';
      initialStatusCodeMappingRef.current = '';
    }
  }, [isEdit, props.visible]);

  useEffect(() => {
    return () => {
      if (statusCodeRiskConfirmResolverRef.current) {
        statusCodeRiskConfirmResolverRef.current(false);
        statusCodeRiskConfirmResolverRef.current = null;
      }
    };
  }, []);

  // 统一的模态框重置函数
  const resetModalState = () => {
    resolveStatusCodeRiskConfirm(false);
    formApiRef.current?.reset();
    // 重置渠道设置状态
    setChannelSettings({
      force_format: false,
      thinking_to_content: false,
      proxy: '',
      pass_through_body_enabled: false,
      system_prompt: '',
      system_prompt_override: false,
    });
    // 重置密钥模式状态
    setKeyMode('append');
    // 重置企业账户状态
    setIsEnterpriseAccount(false);
    // 重置豆包隐藏入口状态
    setDoubaoApiEditUnlocked(false);
    doubaoApiClickCountRef.current = 0;
    setModelSearchValue('');
    // 重置高级设置折叠状态
    setAdvancedSettingsOpen(false);
    // 清空表单中的key_mode字段
    if (formApiRef.current) {
      formApiRef.current.setValue('key_mode', undefined);
    }
    // 重置本地输入，避免下次打开残留上一次的 JSON 字段值
    setInputs(getInitValues());
    // 重置密钥显示状态
    resetKeyDisplayState();
    // 重置剪贴板检测状态
    setClipboardConfig(null);
  };

  const handleVertexUploadChange = ({ fileList }) => {
    vertexErroredNames.current.clear();
    (async () => {
      let validFiles = [];
      let keys = [];
      const errorNames = [];
      for (const item of fileList) {
        const fileObj = item.fileInstance;
        if (!fileObj) continue;
        try {
          const txt = await fileObj.text();
          keys.push(JSON.parse(txt));
          validFiles.push(item);
        } catch (err) {
          if (!vertexErroredNames.current.has(item.name)) {
            errorNames.push(item.name);
            vertexErroredNames.current.add(item.name);
          }
        }
      }

      // 非批量模式下只保留一个文件（最新选择的），避免重复叠加
      if (!batch && validFiles.length > 1) {
        validFiles = [validFiles[validFiles.length - 1]];
        keys = [keys[keys.length - 1]];
      }

      setVertexKeys(keys);
      setVertexFileList(validFiles);
      if (formApiRef.current) {
        formApiRef.current.setValue('vertex_files', validFiles);
      }
      setInputs((prev) => ({ ...prev, vertex_files: validFiles }));

      if (errorNames.length > 0) {
        showError(
          t('以下文件解析失败，已忽略：{{list}}', {
            list: errorNames.join(', '),
          }),
        );
      }
    })();
  };

  const { resolveStatusCodeRiskConfirm, submit } = useChannelSubmission({
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
  });

  // 密钥去重函数
  const deduplicateKeys = () => {
    const currentKey = formApiRef.current?.getValue('key') || inputs.key || '';

    if (!currentKey.trim()) {
      showInfo(t('请先输入密钥'));
      return;
    }

    // 按行分割密钥
    const keyLines = currentKey.split('\n');
    const beforeCount = keyLines.length;

    // 使用哈希表去重，保持原有顺序
    const keySet = new Set();
    const deduplicatedKeys = [];

    keyLines.forEach((line) => {
      const trimmedLine = line.trim();
      if (trimmedLine && !keySet.has(trimmedLine)) {
        keySet.add(trimmedLine);
        deduplicatedKeys.push(trimmedLine);
      }
    });

    const afterCount = deduplicatedKeys.length;
    const deduplicatedKeyText = deduplicatedKeys.join('\n');

    // 更新表单和状态
    if (formApiRef.current) {
      formApiRef.current.setValue('key', deduplicatedKeyText);
    }
    handleInputChange('key', deduplicatedKeyText);

    // 显示去重结果
    const message = t(
      '去重完成：去重前 {{before}} 个密钥，去重后 {{after}} 个密钥',
      {
        before: beforeCount,
        after: afterCount,
      },
    );

    if (beforeCount === afterCount) {
      showInfo(t('未发现重复密钥'));
    } else {
      showSuccess(message);
    }
  };

  const addCustomModels = () => {
    if (customModel.trim() === '') return;
    const modelArray = customModel.split(',').map((model) => model.trim());

    let localModels = [...inputs.models];
    let localModelOptions = [...modelOptions];
    const addedModels = [];

    modelArray.forEach((model) => {
      if (model && !localModels.includes(model)) {
        localModels.push(model);
        localModelOptions.push({
          key: model,
          label: model,
          value: model,
        });
        addedModels.push(model);
      }
    });

    setModelOptions(localModelOptions);
    setCustomModel('');
    handleInputChange('models', localModels);

    if (addedModels.length > 0) {
      showSuccess(
        t('已新增 {{count}} 个模型：{{list}}', {
          count: addedModels.length,
          list: addedModels.join(', '),
        }),
      );
    } else {
      showInfo(t('未发现新增模型'));
    }
  };

  const { batchExtra, channelOptionList, renderChannelOption } =
    useChannelViewOptions({
      batch,
      channelSearchValue,
      deduplicateKeys,
      formApiRef,
      handleInputChange,
      inputs,
      isEdit,
      isMultiKeyChannel,
      multiKeyMode,
      multiToSingle,
      setBatch,
      setInputs,
      setMultiKeyMode,
      setMultiToSingle,
      setUseManualInput,
      setVertexFileList,
      setVertexKeys,
      t,
      vertexFileList,
      vertexKeys,
    });

  const editorContextValue = {
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
  };

  return (
    <EditChannelEditorContext.Provider value={editorContextValue}>
      <EditChannelEditorView />
    </EditChannelEditorContext.Provider>
  );
};

export default EditChannelModal;
