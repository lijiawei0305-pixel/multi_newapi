// 品牌常量与小工具 — 主站默认回退 / 错误码文案 / 主题色派生 (App.jsx 与各页共用)
// 呼应 api-contract §5.4「未配置回退主站默认」、§1.1 错误码注册表。

// 主站默认品牌 (租户未配置 / 未识别时回退)
export const DEFAULT_BRAND = {
  site_name: 'newapi628',
  theme_color: '#0f172a',
  logo_url: '',
  footer_text: '',
  slug: '',
};

// 错误码 → 文案 (api-contract §1.1，前端按 code 做文案/分支，勿仅依赖 message)
export const ERROR_COPY = {
  TENANT_NOT_FOUND: {
    title: '站点未开通 / 未绑定',
    desc: '当前域名尚未绑定到任何站点，或站点尚未开通。请联系主站管理员或代理商。',
    icon: '🚧',
  },
  TENANT_SUSPENDED: {
    title: '站点已停用',
    desc: '该站点已被暂停，暂时无法访问。',
    icon: '⛔',
  },
  TENANT_INACTIVE: {
    title: '站点未激活',
    desc: '该租户尚未激活，请联系管理员。',
    icon: '⏳',
  },
};

// 主题色 → 浅色背景 (用于强调块底色)
export function softBg(hex) {
  const m = /^#?([0-9a-fA-F]{6})$/.exec(hex || '');
  if (!m) return 'rgba(15,23,42,0.08)';
  const n = parseInt(m[1], 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return `rgba(${r},${g},${b},0.12)`;
}

export function initialOf(name) {
  return (name || '?').trim().slice(0, 1).toUpperCase();
}
