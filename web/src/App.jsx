import React, { useEffect, useState } from 'react';
import { Layout, Avatar, Card, Button, Typography, Spin } from '@douyinfe/semi-ui';

const { Header, Content } = Layout;
const { Title, Text } = Typography;

// 主站默认品牌 (租户未配置 / 未识别时回退) — 呼应 api-contract §5.4 "未配置回退主站默认"
const DEFAULT_BRAND = {
  site_name: 'newapi628',
  theme_color: '#0f172a',
  logo_url: '',
  footer_text: '',
  slug: '',
};

// 错误码 → 文案 (api-contract §1.1，前端按 code 做文案/分支，勿仅依赖 message)
const ERROR_COPY = {
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
function softBg(hex) {
  const m = /^#?([0-9a-fA-F]{6})$/.exec(hex || '');
  if (!m) return 'rgba(15,23,42,0.08)';
  const n = parseInt(m[1], 16);
  const r = (n >> 16) & 255;
  const g = (n >> 8) & 255;
  const b = n & 255;
  return `rgba(${r},${g},${b},0.12)`;
}

function initialOf(name) {
  return (name || '?').trim().slice(0, 1).toUpperCase();
}

// 顶栏 (~56px)：Logo + 品牌名 + 黑色主按钮示意 (uiux.md §1)
function TopBar({ brand }) {
  const { site_name: siteName, logo_url: logoUrl, theme_color: themeColor } = brand;
  return (
    <Header
      data-testid="tenant-topbar"
      style={{
        height: 56,
        background: '#ffffff',
        borderBottom: '1px solid #e5e7eb',
        display: 'flex',
        alignItems: 'center',
        justifyContent: 'space-between',
        padding: '0 24px',
      }}
    >
      <div style={{ display: 'flex', alignItems: 'center', gap: 12 }}>
        <Avatar
          size="extra-small"
          src={logoUrl || undefined}
          alt={siteName}
          style={{ background: logoUrl ? undefined : themeColor, color: '#fff' }}
        >
          {!logoUrl && initialOf(siteName)}
        </Avatar>
        <span style={{ fontWeight: 600, fontSize: 16, color: '#0f172a' }}>{siteName}</span>
      </div>
      <Button
        theme="solid"
        style={{ background: '#000', color: '#fff', borderRadius: 8 }}
      >
        控制台
      </Button>
    </Header>
  );
}

// 成功：品牌头 + "租户管道已打通" (Slice 1 冒烟断言点)
function BrandCard({ tenant }) {
  const siteName = tenant.site_name || DEFAULT_BRAND.site_name;
  const themeColor = tenant.theme_color || DEFAULT_BRAND.theme_color;
  const logoUrl = tenant.logo_url || '';
  const slug = tenant.slug || '';
  const footerText = tenant.footer_text || '';

  return (
    <div data-testid="tenant-brand">
      <Card
        style={{
          borderRadius: 12,
          border: '1px solid #e5e7eb',
          borderTop: `4px solid ${themeColor}`,
          boxShadow: '0 1px 3px rgba(15,23,42,0.06)',
        }}
      >
        {/* 品牌头 */}
        <div style={{ display: 'flex', alignItems: 'center', gap: 16 }}>
          <Avatar
            size="large"
            src={logoUrl || undefined}
            alt={siteName}
            style={{ background: logoUrl ? undefined : themeColor, color: '#fff' }}
          >
            {!logoUrl && initialOf(siteName)}
          </Avatar>
          <div>
            <Title heading={3} style={{ margin: 0 }}>
              {siteName}
            </Title>
            <Text type="tertiary">
              slug：<span data-testid="tenant-slug">{slug || '(主站默认)'}</span>
            </Text>
          </div>
        </div>

        {/* 管道打通强调块 (主题色强调) */}
        <div
          data-testid="tenant-ready"
          style={{
            marginTop: 20,
            padding: '10px 14px',
            borderRadius: 10,
            background: softBg(themeColor),
            color: themeColor,
            fontWeight: 600,
            display: 'inline-block',
          }}
        >
          租户管道已打通 ✅ slug={slug || '(default)'}
        </div>

        {/* 设计令牌示意：黑色主按钮 + 主题色次级描边按钮 */}
        <div style={{ marginTop: 24, display: 'flex', gap: 12 }}>
          <Button theme="solid" style={{ background: '#000', color: '#fff', borderRadius: 8 }}>
            主操作（黑底白字）
          </Button>
          <Button
            theme="outline"
            style={{ borderColor: themeColor, color: themeColor, borderRadius: 8 }}
          >
            次级操作
          </Button>
        </div>

        {/* 页脚 */}
        <div
          data-testid="tenant-footer"
          style={{
            marginTop: 24,
            paddingTop: 16,
            borderTop: '1px solid #e5e7eb',
            color: '#94a3b8',
            fontSize: 13,
          }}
        >
          {footerText || `© ${siteName}`}
        </div>
      </Card>
    </div>
  );
}

// 失败：空状态 (TENANT_NOT_FOUND → site-unbound)
function ErrorState({ code, message }) {
  const isUnbound = code === 'TENANT_NOT_FOUND';
  const copy =
    ERROR_COPY[code] || {
      title: '加载失败',
      desc: message || '无法获取站点信息，请稍后重试。',
      icon: '⚠️',
    };
  return (
    <div data-testid={isUnbound ? 'site-unbound' : 'tenant-error'}>
      <Card
        style={{
          borderRadius: 12,
          border: '1px solid #e5e7eb',
          textAlign: 'center',
          padding: '32px 0',
        }}
      >
        <div style={{ fontSize: 48, lineHeight: 1 }}>{copy.icon}</div>
        <Title heading={4} style={{ marginTop: 16, marginBottom: 8 }}>
          {copy.title}
        </Title>
        <Text type="tertiary">{copy.desc}</Text>
        <div style={{ marginTop: 12, color: '#cbd5e1', fontSize: 12 }}>code：{code}</div>
      </Card>
    </div>
  );
}

export default function App() {
  // status: loading | ready | error
  const [state, setState] = useState({ status: 'loading' });

  useEffect(() => {
    // 透传 ?host= 便于测试栈按租户取数 (本地代理 → 后端按 Host 识别租户)
    const qs = window.location.search.includes('host=') ? window.location.search : '';
    let alive = true;

    fetch('/api/tenant/current' + qs, { headers: { Accept: 'application/json' } })
      .then(async (resp) => {
        let body = {};
        try {
          body = await resp.json();
        } catch (_e) {
          body = {};
        }
        if (!alive) return;
        if (resp.ok) {
          // 成功信封：{ data: {...} } 或裸资源对象，两种都兼容
          const tenant = body && body.data ? body.data : body || {};
          setState({ status: 'ready', tenant });
        } else {
          const code = (body && body.code) || 'INTERNAL';
          setState({ status: 'error', code, message: body && body.message });
        }
      })
      .catch((err) => {
        if (!alive) return;
        setState({
          status: 'error',
          code: 'NETWORK',
          message: String((err && err.message) || err),
        });
      });

    return () => {
      alive = false;
    };
  }, []);

  // 顶栏品牌：识别成功用租户品牌，否则回退主站默认 (演示多租户换肤)
  const brand =
    state.status === 'ready'
      ? { ...DEFAULT_BRAND, ...(state.tenant || {}) }
      : DEFAULT_BRAND;

  return (
    <Layout style={{ minHeight: '100vh', background: '#f8fafc' }}>
      <TopBar brand={brand} />
      <Content style={{ padding: 24, display: 'flex', justifyContent: 'center' }}>
        <div style={{ width: '100%', maxWidth: 720 }}>
          {state.status === 'loading' && (
            <div data-testid="tenant-loading" style={{ textAlign: 'center', padding: '64px 0' }}>
              <Spin size="large" />
              <div style={{ marginTop: 12, color: '#94a3b8' }}>正在识别站点…</div>
            </div>
          )}
          {state.status === 'ready' && <BrandCard tenant={state.tenant || {}} />}
          {state.status === 'error' && (
            <ErrorState code={state.code} message={state.message} />
          )}
        </div>
      </Content>
    </Layout>
  );
}
