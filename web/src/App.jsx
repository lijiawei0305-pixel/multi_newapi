import React, { useEffect, useState } from 'react';
import { Layout } from '@douyinfe/semi-ui';
import TopBar from './components/TopBar.jsx';
import BrandHome from './pages/BrandHome.jsx';
import Wallet from './pages/Wallet.jsx';
import { apiFetch } from './lib/api.js';
import { DEFAULT_BRAND } from './lib/brand.js';

const { Content } = Layout;

// 轻量 hash 路由：#/ → home，#/wallet → wallet。
// 选 hash 而非 history：dist 由后端静态托管，任何路径只会落到 index.html，hash 不需要后端配 rewrite。
function parseHash() {
  const h = (window.location.hash || '').replace(/^#/, '');
  if (h === '/wallet' || h.startsWith('/wallet')) return 'wallet';
  return 'home';
}

function useHashRoute() {
  const [route, setRoute] = useState(parseHash());
  useEffect(() => {
    const onHash = () => setRoute(parseHash());
    window.addEventListener('hashchange', onHash);
    return () => window.removeEventListener('hashchange', onHash);
  }, []);
  return route;
}

export default function App() {
  const route = useHashRoute();

  // 租户品牌：进站拉一次，TopBar 与首页共用 (status: loading | ready | error)
  const [tenantState, setTenantState] = useState({ status: 'loading' });

  useEffect(() => {
    let alive = true;
    apiFetch('/api/tenant/current').then((r) => {
      if (!alive) return;
      if (r.ok) {
        setTenantState({ status: 'ready', tenant: r.data || {} });
      } else {
        setTenantState({ status: 'error', code: r.code, message: r.message });
      }
    });
    return () => {
      alive = false;
    };
  }, []);

  // 顶栏品牌：识别成功用租户品牌，否则回退主站默认 (演示多租户换肤)
  const brand =
    tenantState.status === 'ready'
      ? { ...DEFAULT_BRAND, ...(tenantState.tenant || {}) }
      : DEFAULT_BRAND;

  return (
    <Layout style={{ minHeight: '100vh', background: '#f8fafc' }}>
      <TopBar brand={brand} route={route} />
      <Content style={{ padding: 24, display: 'flex', justifyContent: 'center' }}>
        <div style={{ width: '100%', maxWidth: 720 }}>
          {route === 'wallet' ? (
            <Wallet brand={brand} />
          ) : (
            <BrandHome state={tenantState} />
          )}
        </div>
      </Content>
    </Layout>
  );
}
