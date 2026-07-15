// @ts-nocheck
/* WeDream 统一 3D 场景 —— 配置:模型元数据 / logo 素材 / 两条轨道。
   MODELS、LOGOS 由脚本从 orbit.ts 原样迁移(值一字不改);ORBITS 为本次新增。
   speed 正负号在实现期对着截图校正(内前方左→右 / 外前方右→左);
   radius/tilt 为初值,塞进 65% 盒子时按截图微调。 */

export const LOGOS: Record<string, string> = {
  openai: `<svg viewBox="0 0 24 24"><path fill="#ECF3FF" d="M22.2819 9.8211a5.9847 5.9847 0 0 0-.5157-4.9108 6.0462 6.0462 0 0 0-6.5098-2.9A6.0651 6.0651 0 0 0 4.9807 4.1818a5.9847 5.9847 0 0 0-3.9977 2.9 6.0462 6.0462 0 0 0 .7427 7.0966 5.98 5.98 0 0 0 .511 4.9107 6.051 6.051 0 0 0 6.5146 2.9001A5.9847 5.9847 0 0 0 13.2599 24a6.0557 6.0557 0 0 0 5.7718-4.2058 5.9894 5.9894 0 0 0 3.9977-2.9001 6.0557 6.0557 0 0 0-.7475-7.073zm-9.022 12.6081a4.4755 4.4755 0 0 1-2.8764-1.0408l.1419-.0804 4.7783-2.7582a.7948.7948 0 0 0 .3927-.6813v-6.7369l2.02 1.1686a.071.071 0 0 1 .038.052v5.5826a4.504 4.504 0 0 1-4.4945 4.4944zm-9.6607-4.1254a4.4708 4.4708 0 0 1-.5346-3.0137l.142.0852 4.783 2.7582a.7712.7712 0 0 0 .7806 0l5.8428-3.3685v2.3324a.0804.0804 0 0 1-.0332.0615L9.74 19.9502a4.4992 4.4992 0 0 1-6.1408-1.6464zM2.3408 7.8956a4.485 4.485 0 0 1 2.3655-1.9728V11.6a.7664.7664 0 0 0 .3879.6765l5.8144 3.3543-2.0201 1.1685a.0757.0757 0 0 1-.071 0l-4.8303-2.7865A4.504 4.504 0 0 1 2.3408 7.8956zm16.5963 3.8558L13.1038 8.364 15.1192 7.2a.0757.0757 0 0 1 .071 0l4.8303 2.7913a4.4944 4.4944 0 0 1-.6765 8.1042v-5.6772a.79.79 0 0 0-.407-.667zm2.0107-3.0231l-.142-.0852-4.7735-2.7818a.7759.7759 0 0 0-.7854 0L9.409 9.2297V6.8974a.0662.0662 0 0 1 .0284-.0615l4.8303-2.7866a4.4992 4.4992 0 0 1 6.6802 4.66zM8.3065 12.863l-2.02-1.1638a.0804.0804 0 0 1-.038-.0567V6.0742a4.4992 4.4992 0 0 1 7.3757-3.4537l-.142.0805L8.704 5.459a.7948.7948 0 0 0-.3927.6813zm1.0976-2.3654l2.602-1.4998 2.6069 1.4998v2.9994l-2.5974 1.4997-2.6067-1.4997z"/></svg>`,
  anthropic: `<svg viewBox="0 0 24 24"><path fill="#D97757" fill-rule="evenodd" d="M17.3041 3.541h-3.6718l6.696 16.918H24Zm-10.6082 0L0 20.459h3.7442l1.3693-3.5527h7.0052l1.3693 3.5527h3.7442L10.5363 3.541Zm-.3712 10.2232 2.2914-5.9456 2.2914 5.9456Z"/></svg>`,
  gemini: `<svg viewBox="0 0 24 24"><defs><linearGradient id="__id__g" x1="1" y1="4" x2="22" y2="21" gradientUnits="userSpaceOnUse"><stop offset="0" stop-color="#559BFA"/><stop offset=".55" stop-color="#8A7CF8"/><stop offset="1" stop-color="#C96BF0"/></linearGradient></defs><path fill="url(#__id__g)" d="M12 0c.53 6.9 5.1 11.47 12 12-6.9.53-11.47 5.1-12 12-.53-6.9-5.1-11.47-12-12C6.9 11.47 11.47 6.9 12 0Z"/></svg>`,
  deepseek: `<svg viewBox="0 0 24 24"><path fill="#5B78FF" d="M23.748 4.482c-.254-.124-.364.113-.512.234-.051.039-.094.09-.137.136-.372.397-.806.657-1.373.626-.829-.046-1.537.214-2.163.848-.133-.782-.575-1.248-1.247-1.548-.352-.156-.708-.311-.955-.65-.172-.241-.219-.51-.305-.774-.055-.16-.11-.323-.293-.35-.2-.031-.278.136-.356.276-.313.572-.434 1.202-.422 1.84.027 1.436.633 2.58 1.838 3.393.137.093.172.187.129.323-.082.28-.18.552-.266.833-.055.179-.137.217-.329.14a5.526 5.526 0 0 1-1.736-1.18c-.857-.828-1.631-1.742-2.597-2.458a11.365 11.365 0 0 0-.689-.471c-.985-.957.13-1.743.388-1.836.27-.098.093-.432-.779-.428-.872.004-1.67.295-2.687.684a3.055 3.055 0 0 1-.465.137 9.597 9.597 0 0 0-2.883-.102c-1.885.21-3.39 1.102-4.497 2.623C.082 8.606-.231 10.684.152 12.85c.403 2.284 1.569 4.175 3.36 5.653 1.858 1.533 3.997 2.284 6.438 2.14 1.482-.085 3.133-.284 4.994-1.86.47.234.962.327 1.78.397.63.059 1.236-.03 1.705-.128.735-.156.684-.837.419-.961-2.155-1.004-1.682-.595-2.113-.926 1.096-1.296 2.746-2.642 3.392-7.003.05-.347.007-.565 0-.845-.004-.17.035-.237.23-.256a4.173 4.173 0 0 0 1.545-.475c1.396-.763 1.96-2.015 2.093-3.517.02-.23-.004-.467-.247-.588zM11.581 18c-2.089-1.642-3.102-2.183-3.52-2.16-.392.024-.321.471-.235.763.09.288.207.486.371.739.114.167.192.416-.113.603-.673.416-1.842-.14-1.897-.167-1.361-.802-2.5-1.86-3.301-3.307-.774-1.393-1.224-2.887-1.298-4.482-.02-.386.093-.522.477-.592a4.696 4.696 0 0 1 1.529-.039c2.132.312 3.946 1.265 5.468 2.774.868.86 1.525 1.887 2.202 2.891.72 1.066 1.494 2.082 2.48 2.914.348.292.625.514.891.677-.802.09-2.14.11-3.054-.614zm1-6.44a.306.306 0 0 1 .415-.287.302.302 0 0 1 .2.288.306.306 0 0 1-.31.307.303.303 0 0 1-.304-.308zm3.11 1.596c-.2.081-.399.151-.59.16a1.245 1.245 0 0 1-.798-.254c-.274-.23-.47-.358-.552-.758a1.73 1.73 0 0 1 .016-.588c.07-.327-.008-.537-.239-.727-.187-.156-.426-.199-.688-.199a.559.559 0 0 1-.254-.078c-.11-.054-.2-.19-.114-.358.028-.054.16-.186.192-.21.356-.202.767-.136 1.146.016.353.144.62.409 1.004.781.393.45.462.576.685.914.176.265.336.537.445.848.067.195-.019.354-.253.453z"/></svg>`,
  xai: `<svg viewBox="0 0 24 24"><path fill="#F2F6FC" d="m3.005 8.858 8.783 12.544h3.904L6.908 8.858zM6.905 15.825 3 21.402h3.907l1.951-2.788zM16.585 2l-6.75 9.64 1.953 2.79L20.492 2zM17.292 7.965v13.437h3.2V3.395z"/></svg>`,
  qwen: `<img src="/lp-assets/logos/qwen.png" alt="" draggable="false">`,
  doubao: `<img src="/lp-assets/logos/doubao.png" alt="" draggable="false">`,
  kimi: `<img src="/lp-assets/logos/kimi.png" alt="" draggable="false">`,
  glm_chatglm: `<img src="/lp-assets/logos/glm_chatglm.png" alt="" draggable="false">`,
  minimax: `<img src="/lp-assets/logos/minimax.png" alt="" draggable="false">`,
}

export const MODELS: Record<string, any> = {
  openai: { name: 'GPT Series', provider: 'OPENAI', desc: 'Multimodal flagship — top-tier general intelligence and tool use, with the most mature ecosystem.', scene: 'General chat, agents, multimodal understanding and generation.', telemetry: 'Multimodal pipeline online', color: '#10d075' },
  anthropic: { name: 'Claude Series', provider: 'ANTHROPIC', desc: 'The benchmark for coding and complex reasoning; reliable long context and excellent safety alignment.', scene: 'Coding agents, long-document analysis, serious writing.', telemetry: '200K context active', color: '#d97757' },
  gemini: { name: 'Gemini Series', provider: 'GOOGLE DEEPMIND', desc: 'Natively multimodal engine with million-token context and strong video/audio understanding.', scene: 'Multimodal analysis, Q&A over very large corpora.', telemetry: '1M token pipeline', color: '#9020f0' },
  deepseek: { name: 'DeepSeek Series', provider: 'DEEPSEEK', desc: 'Best value in reasoning and code; open and strong at mathematics.', scene: 'Cost-effective reasoning, code generation, math problem solving.', telemetry: 'Reasoning chain active', color: '#4fa3ff' },
  xai: { name: 'Grok Series', provider: 'XAI', desc: 'Plugged into real-time information, with a distinct style and fast-improving reasoning.', scene: 'Real-time information Q&A, trend analysis.', telemetry: 'Real-time retrieval in sync', color: '#00a0ff' },
  qwen: { name: 'Qwen Series', provider: 'Alibaba Cloud', desc: 'Domestic open-source flagship — strong at code and multilingual tasks, with the broadest size lineup.', scene: 'Chinese conversation, code generation, structured output.', telemetry: 'Full size lineup online', color: '#6b6dff' },
  minimax: { name: 'MiniMax Series', provider: 'MINIMAX', desc: 'Long context and multimodality in step, with outstanding speech synthesis.', scene: 'Long-form processing, voice applications, character dialogue.', telemetry: 'Million-scale context', color: '#b987ff' },
  doubao: { name: 'Doubao Large Model', provider: 'ByteDance', desc: 'High concurrency at low cost, great everyday Chinese conversation, proven at scale.', scene: 'Large-scale consumer apps, smart customer service, translation.', telemetry: 'Volcano Engine pipeline', color: '#39c5ff' },
  kimi: { name: 'Kimi Series', provider: 'Moonshot AI', desc: 'A pioneer of ultra-long context — great for web and document digests, with open-source reasoning models.', scene: 'Long-document reading, research digests, deep reasoning.', telemetry: 'Ultra-long context active', color: '#6c7cff' },
  glm_chatglm: { name: 'GLM Series', provider: 'Zhipu AI', desc: 'Tsinghua-rooted technology with strong agent and coding ability; open and accessible.', scene: 'Agent development, coding assistance, academic research.', telemetry: 'Agent pipeline active', color: '#6f7bff' },
}

export type OrbitCfg = {
  ring: 'inner' | 'outer'
  color: string
  radius: number
  tubeRadius: number
  opacity: number
  tilt: [number, number, number]
  wobbleDeg: number
  wobblePeriod: number
  wobblePhase: number
  speed: number
  wakeStrength: number
  wakeFalloff: number
  keys: string[]
}

export const ORBITS: OrbitCfg[] = [
  {
    ring: 'inner', color: '#8fd8ff', radius: 1.0, tubeRadius: 0.0028, opacity: 0.55, tilt: [0.5, 0, 0.12],
    wobbleDeg: 4, wobblePeriod: 18, wobblePhase: 0, speed: 0.1,
    wakeStrength: 0.9, wakeFalloff: 5.0, keys: ['openai', 'anthropic', 'gemini', 'xai'],
  },
  {
    ring: 'outer', color: '#5f7cff', radius: 1.32, tubeRadius: 0.0018, opacity: 0.3, tilt: [0.72, 0, -0.38],
    wobbleDeg: 6, wobblePeriod: 18, wobblePhase: Math.PI, speed: -0.075,
    wakeStrength: 0.6, wakeFalloff: 6.0, keys: ['deepseek', 'qwen', 'minimax', 'doubao', 'kimi', 'glm_chatglm'],
  },
]
