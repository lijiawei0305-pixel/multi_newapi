import React from 'react';
import { Card, Button, Typography, Spin, Avatar } from '@douyinfe/semi-ui';
import { DEFAULT_BRAND, ERROR_COPY, softBg, initialOf } from '../lib/brand.js';

const { Title, Text } = Typography;

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

// 品牌首页 (#/)：loading / ready / error 三态，复用 Slice 1 渲染
export default function BrandHome({ state }) {
  if (state.status === 'loading') {
    return (
      <div data-testid="tenant-loading" style={{ textAlign: 'center', padding: '64px 0' }}>
        <Spin size="large" />
        <div style={{ marginTop: 12, color: '#94a3b8' }}>正在识别站点…</div>
      </div>
    );
  }
  if (state.status === 'error') {
    return <ErrorState code={state.code} message={state.message} />;
  }
  return <BrandCard tenant={state.tenant || {}} />;
}
