import React from 'react';
import { Layout, Avatar, Button } from '@douyinfe/semi-ui';
import { initialOf } from '../lib/brand.js';

const { Header } = Layout;

// 顶栏导航项（hash 锚点导航：浏览器更新 hash → App 监听 hashchange 切页；不触碰 ?host= search）
function NavLink({ to, active, themeColor, testid, children }) {
  return (
    <a
      href={to}
      data-testid={testid}
      style={{
        textDecoration: 'none',
        fontSize: 14,
        fontWeight: active ? 600 : 500,
        color: active ? themeColor : '#475569',
        padding: '4px 2px',
        borderBottom: active ? `2px solid ${themeColor}` : '2px solid transparent',
        transition: 'color .15s',
      }}
    >
      {children}
    </a>
  );
}

// 顶栏 (~56px)：Logo + 品牌名 + 导航(主页·钱包) + 黑色主按钮 (uiux.md §1)
export default function TopBar({ brand, route }) {
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

      <div style={{ display: 'flex', alignItems: 'center', gap: 24 }}>
        <nav style={{ display: 'flex', alignItems: 'center', gap: 20 }}>
          <NavLink to="#/" active={route === 'home'} themeColor={themeColor} testid="nav-home">
            主页
          </NavLink>
          <NavLink
            to="#/playground"
            active={route === 'playground'}
            themeColor={themeColor}
            testid="nav-playground"
          >
            游乐场
          </NavLink>
          <NavLink
            to="#/plans"
            active={route === 'plans'}
            themeColor={themeColor}
            testid="nav-plans"
          >
            套餐
          </NavLink>
          <NavLink
            to="#/wallet"
            active={route === 'wallet'}
            themeColor={themeColor}
            testid="nav-wallet"
          >
            钱包
          </NavLink>
        </nav>
        <Button
          theme="solid"
          style={{ background: '#000', color: '#fff', borderRadius: 8 }}
        >
          控制台
        </Button>
      </div>
    </Header>
  );
}
