export type Orbit = 'intl' | 'domestic'

export interface Model {
  id: string
  name: string
  provider: string
  desc: string
  scene: string
  telemetry: string
  brandColor: string
  orbit: Orbit
  logo: string
}

export const DEFAULT_MODEL: Model = {
  id: 'core', name: 'WeDream 核心', provider: '系统运行中',
  desc: '中央智能核心，环绕的卫星代表已接入的大模型，实时互联。', scene: '——',
  telemetry: '核心负载 100%', brandColor: '#00f0ff', orbit: 'intl', logo: '',
}

export const MODELS: readonly Model[] = [
  // —— 国际轨（12）——
  { id: 'openai', name: 'GPT 系列', provider: 'OPENAI', desc: '多模态旗舰，通用智能与工具调用能力顶尖，生态最成熟。', scene: '通用对话、智能体、多模态理解与生成。', telemetry: '多模态管线在线', brandColor: '#10d075', orbit: 'intl', logo: 'openai.png' },
  { id: 'anthropic', name: 'Claude 系列', provider: 'ANTHROPIC', desc: '代码与复杂推理标杆，长上下文稳定可靠，安全对齐出色。', scene: '编程智能体、长文档分析、严肃写作。', telemetry: '200K 上下文激活', brandColor: '#d97757', orbit: 'intl', logo: 'anthropic.png' },
  { id: 'gemini', name: 'Gemini 系列', provider: 'GOOGLE DEEPMIND', desc: '原生多模态引擎，百万级 token 上下文，视频音频理解强。', scene: '多模态分析、超长资料库问答。', telemetry: '1M token 管线', brandColor: '#9020f0', orbit: 'intl', logo: 'gemini.png' },
  { id: 'meta', name: 'Llama 系列', provider: 'META AI', desc: '开源权重旗舰，社区生态庞大，可完全私有化部署。', scene: '私有化部署、定制微调、开源研究。', telemetry: '开源权重可用', brandColor: '#0596ff', orbit: 'intl', logo: 'meta.png' },
  { id: 'mistral', name: 'Mistral 系列', provider: 'MISTRAL AI', desc: '欧洲开源新锐，小模型效率极高，MoE 架构先行者。', scene: '低成本推理、边缘部署、多语种应用。', telemetry: 'MoE 引擎在线', brandColor: '#ff7000', orbit: 'intl', logo: 'mistral.png' },
  { id: 'deepseek', name: 'DeepSeek 系列', provider: 'DEEPSEEK', desc: '推理与代码性价比之王，开源开放，数学推理尤强。', scene: '高性价比推理、代码生成、数学解题。', telemetry: '推理链激活', brandColor: '#4fa3ff', orbit: 'intl', logo: 'deepseek.png' },
  { id: 'xai', name: 'Grok 系列', provider: 'XAI', desc: '接入实时资讯流，风格鲜明，推理能力快速迭代。', scene: '实时信息问答、热点分析。', telemetry: '实时检索同步', brandColor: '#00a0ff', orbit: 'intl', logo: 'xai.png' },
  { id: 'cohere', name: 'Command 系列', provider: 'COHERE', desc: '企业级检索与嵌入见长，RAG 工具链完善，多语种企业部署。', scene: '企业知识库、语义搜索、RAG 应用。', telemetry: 'RAG 管线在线', brandColor: '#ff7759', orbit: 'intl', logo: 'cohere.png' },
  { id: 'midjourney', name: 'Midjourney', provider: 'MIDJOURNEY', desc: '顶级艺术风格图像生成，美学表现力公认最强。', scene: '概念设计、海报插画、艺术创作。', telemetry: '渲染农场在线', brandColor: '#9bb5ff', orbit: 'intl', logo: 'midjourney.png' },
  { id: 'stability', name: 'Stable Diffusion 系列', provider: 'STABILITY AI', desc: '开源图像生成标杆，插件生态丰富，可本地部署。', scene: '可控图像生成、二次开发、本地出图。', telemetry: '扩散管线就绪', brandColor: '#b266ff', orbit: 'intl', logo: 'stability.png' },
  { id: 'huggingface', name: '开源模型枢纽', provider: 'HUGGING FACE', desc: '全球最大开源模型社区，数十万模型即取即用。', scene: '开源模型试用、推理 API、数据集。', telemetry: 'Hub 已连接', brandColor: '#ffd21e', orbit: 'intl', logo: 'huggingface.png' },
  { id: 'perplexity', name: 'Sonar 系列', provider: 'PERPLEXITY', desc: 'AI 原生搜索引擎，答案附引用来源，实时联网。', scene: '联网问答、资料调研、事实核查。', telemetry: '联网检索激活', brandColor: '#2bb8ce', orbit: 'intl', logo: 'perplexity.png' },
  // —— 国产轨（12）——
  { id: 'qwen', name: '通义千问系列', provider: '阿里云', desc: '国产开源旗舰，代码与多语种能力强，模型尺寸谱系最全。', scene: '中文对话、代码生成、结构化输出。', telemetry: '全尺寸谱系在线', brandColor: '#6b6dff', orbit: 'domestic', logo: 'qwen.png' },
  { id: 'minimax', name: 'MiniMax 系列', provider: 'MINIMAX', desc: '长上下文与多模态并进，语音合成表现出色。', scene: '长文处理、语音应用、角色对话。', telemetry: '百万级上下文', brandColor: '#b987ff', orbit: 'domestic', logo: 'minimax.png' },
  { id: 'doubao', name: '豆包大模型', provider: '字节跳动', desc: '高并发低成本，中文日常对话体验佳，规模化验证充分。', scene: '大规模 C 端应用、智能客服、翻译。', telemetry: '火山引擎管线', brandColor: '#39c5ff', orbit: 'domestic', logo: 'doubao.png' },
  { id: 'stepfun', name: 'Step 系列', provider: '阶跃星辰', desc: '多模态理解见长，万亿参数 MoE 路线探索者。', scene: '图文理解、多模态创作。', telemetry: '多模态管线在线', brandColor: '#92a2ff', orbit: 'domestic', logo: 'stepfun.png' },
  { id: 'kimi', name: 'Kimi 系列', provider: '月之暗面', desc: '超长上下文先行者，网页与文档整理利器，推理模型开源。', scene: '长文档阅读、资料汇总、深度推理。', telemetry: '超长上下文激活', brandColor: '#6c7cff', orbit: 'domestic', logo: 'kimi.png' },
  { id: 'huawei_pangu', name: '盘古大模型', provider: '华为云', desc: '行业大模型深耕，政企场景与昇腾算力生态深度结合。', scene: '政企行业方案、私有云部署。', telemetry: '昇腾集群在线', brandColor: '#ef3340', orbit: 'domestic', logo: 'huawei_pangu.png' },
  { id: 'baidu_wenxin', name: '文心大模型', provider: '百度', desc: '中文知识增强路线，检索增强与插件生态成熟。', scene: '中文创作、企业应用、搜索增强。', telemetry: '知识增强激活', brandColor: '#2d78ff', orbit: 'domestic', logo: 'baidu_wenxin.png' },
  { id: 'zeroone_ai', name: 'Yi 系列', provider: '零一万物', desc: '中英双语开源佳作，长文本与多模态兼备。', scene: '双语应用、开源定制。', telemetry: '双语管线在线', brandColor: '#61d3ff', orbit: 'domestic', logo: 'zeroone_ai.png' },
  { id: 'tencent_hunyuan', name: '混元大模型', provider: '腾讯', desc: '全链路自研，文生图与视频多模态齐全，微信生态天然接入。', scene: '内容创作、腾讯生态应用。', telemetry: '多模态就绪', brandColor: '#25d6ff', orbit: 'domestic', logo: 'tencent_hunyuan.png' },
  { id: 'baichuan_ai', name: 'Baichuan 系列', provider: '百川智能', desc: '中文开源先锋，医疗等垂直领域持续深化。', scene: '中文垂直领域、开源部署。', telemetry: '垂直增强在线', brandColor: '#2de2a0', orbit: 'domestic', logo: 'baichuan_ai.png' },
  { id: 'glm_chatglm', name: 'GLM 系列', provider: '智谱 AI', desc: '清华系技术底蕴，Agent 与代码能力强，开源开放。', scene: '智能体开发、代码辅助、学术研究。', telemetry: 'Agent 管线激活', brandColor: '#6f7bff', orbit: 'domestic', logo: 'glm_chatglm.png' },
  { id: 'iflytek_spark', name: '星火大模型', provider: '科大讯飞', desc: '语音交互天然优势，教育医疗行业落地深。', scene: '语音助手、教育应用、办公纪要。', telemetry: '语音引擎在线', brandColor: '#ff395d', orbit: 'domestic', logo: 'iflytek_spark.png' },
]

const BY_ID = new Map(MODELS.map(m => [m.id, m]))
export function getModel(id: string): Model | undefined { return BY_ID.get(id) }
export function orbitModels(orbit: Orbit): Model[] { return MODELS.filter(m => m.orbit === orbit) }
