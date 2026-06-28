import React, { useCallback, useEffect, useState } from 'react';
import { Card, Button, Typography, Spin, Input, Toast } from '@douyinfe/semi-ui';
import { apiFetch } from '../lib/api.js';
import { softBg } from '../lib/brand.js';

const { Title, Text } = Typography;

// 充值金额预设 (单位 ¥，呼应 uiux §3.3「充值实付 = CNY ¥」)；下单 stub，Slice 3 接真实支付
const RECHARGE_PRESETS = [73, 146, 365, 730, 1460, 3650];

// 兑换码错误码 → 文案 (api-contract §1.1 Wallet 段)
const REDEEM_COPY = {
  REDEEM_CODE_INVALID: '兑换码无效，请检查后重试',
  REDEEM_CODE_USED: '兑换码已被使用',
  WALLET_AMOUNT_INVALID: '兑换金额非法',
  UNAUTHORIZED: '登录已失效，请重新「演示登录」后再试',
  TOKEN_INVALID: '登录已失效，请重新「演示登录」后再试',
};

// USD 额度格式化（额度/计量统一 USD，呼应 api-contract §1）
function fmtUsd(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '0.00';
  return n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

// 统计卡（左标签 + 大号粗体数字，uiux §1 令牌）
function StatCard({ label, value, unit, testid, accent }) {
  return (
    <Card
      style={{
        flex: 1,
        minWidth: 160,
        borderRadius: 12,
        border: '1px solid #e5e7eb',
        boxShadow: '0 1px 3px rgba(15,23,42,0.06)',
      }}
      bodyStyle={{ padding: 16 }}
    >
      <Text type="tertiary" style={{ fontSize: 13 }}>
        {label}
      </Text>
      <div style={{ marginTop: 6, display: 'flex', alignItems: 'baseline', gap: 6 }}>
        <span
          data-testid={testid}
          style={{ fontSize: 26, fontWeight: 700, color: accent || '#0f172a', lineHeight: 1.1 }}
        >
          {value}
        </span>
        {unit && (
          <span style={{ fontSize: 12, fontWeight: 600, color: '#94a3b8' }}>{unit}</span>
        )}
      </div>
    </Card>
  );
}

// 登录态：未登录 → 演示登录 (POST /api/dev/login {username:"demo@td"})
function LoginPanel({ themeColor, onLogin, busy }) {
  return (
    <div data-testid="wallet-login">
      <Card
        style={{
          borderRadius: 12,
          border: '1px solid #e5e7eb',
          borderTop: `4px solid ${themeColor}`,
          textAlign: 'center',
          padding: '32px 0',
        }}
      >
        <div style={{ fontSize: 48, lineHeight: 1 }}>🔐</div>
        <Title heading={4} style={{ marginTop: 16, marginBottom: 8 }}>
          钱包需登录后查看
        </Title>
        <Text type="tertiary">开发演示环境可一键登录体验充值 / 兑换流程。</Text>
        <div style={{ marginTop: 20 }}>
          <Button
            data-testid="wallet-login-btn"
            theme="solid"
            loading={busy}
            onClick={onLogin}
            style={{ background: '#000', color: '#fff', borderRadius: 8, minWidth: 140 }}
          >
            演示登录
          </Button>
        </div>
      </Card>
    </div>
  );
}

export default function Wallet({ brand }) {
  const themeColor = (brand && brand.theme_color) || '#0f172a';

  // status: loading | login | ready | error
  const [status, setStatus] = useState('loading');
  const [wallet, setWallet] = useState(null);
  const [errCode, setErrCode] = useState('');

  const [loginBusy, setLoginBusy] = useState(false);

  const [rechargeBusy, setRechargeBusy] = useState(0); // 进行中的金额 (0 = 空闲)
  const [rechargeResult, setRechargeResult] = useState(null);

  const [code, setCode] = useState('');
  const [redeemBusy, setRedeemBusy] = useState(false);
  const [redeemResult, setRedeemResult] = useState(null);

  // 拉钱包：200 → ready；401 → login；其它 → error
  const loadWallet = useCallback(async () => {
    setStatus('loading');
    const r = await apiFetch('/api/tenant/wallet');
    if (r.ok) {
      setWallet(r.data || {});
      setStatus('ready');
    } else if (r.status === 401 || r.code === 'UNAUTHORIZED' || r.code === 'TOKEN_INVALID') {
      setStatus('login');
    } else {
      setErrCode(r.code || 'INTERNAL');
      setStatus('error');
    }
  }, []);

  useEffect(() => {
    loadWallet();
  }, [loadWallet]);

  // 演示登录
  const devLogin = useCallback(async () => {
    setLoginBusy(true);
    const r = await apiFetch('/api/dev/login', { method: 'POST', body: { username: 'demo@td' } });
    setLoginBusy(false);
    if (r.ok) {
      Toast.success('演示登录成功');
      loadWallet();
    } else {
      Toast.error('演示登录失败：' + (r.message || r.code));
      setErrCode(r.code || 'INTERNAL');
      setStatus('error');
    }
  }, [loadWallet]);

  // 充值下单 (stub)：POST /wallet/recharge {amount} → 展示 order_no
  const recharge = useCallback(async (amount) => {
    setRechargeBusy(amount);
    setRechargeResult(null);
    const r = await apiFetch('/api/tenant/wallet/recharge', { method: 'POST', body: { amount } });
    setRechargeBusy(0);
    if (r.ok) {
      const orderNo = (r.data && (r.data.order_no || r.data.orderNo)) || '(无单号)';
      setRechargeResult({ ok: true, amount, orderNo });
    } else if (r.status === 401 || r.code === 'UNAUTHORIZED' || r.code === 'TOKEN_INVALID') {
      setStatus('login');
    } else {
      setRechargeResult({ ok: false, amount, code: r.code, message: r.message });
      Toast.error('下单失败：' + (r.message || r.code));
    }
  }, []);

  // 兑换码：POST /wallet/redeem {code} → 成功刷新余额 / 失败按 code 文案
  const redeem = useCallback(async () => {
    const c = (code || '').trim();
    if (!c) {
      setRedeemResult({ ok: false, message: '请输入兑换码' });
      return;
    }
    setRedeemBusy(true);
    setRedeemResult(null);
    const r = await apiFetch('/api/tenant/wallet/redeem', { method: 'POST', body: { code: c } });
    setRedeemBusy(false);
    if (r.ok) {
      const credited = r.data && (r.data.amount_usd != null ? r.data.amount_usd : null);
      setRedeemResult({
        ok: true,
        message: credited != null ? `兑换成功，入账 ${fmtUsd(credited)} USD` : '兑换成功，额度已入账',
      });
      setCode('');
      loadWallet(); // 刷新余额
    } else if (r.status === 401 || r.code === 'UNAUTHORIZED' || r.code === 'TOKEN_INVALID') {
      setStatus('login');
    } else {
      const msg = REDEEM_COPY[r.code] || r.message || '兑换失败';
      setRedeemResult({ ok: false, code: r.code, message: msg });
    }
  }, [code, loadWallet]);

  if (status === 'loading') {
    return (
      <div data-testid="wallet-loading" style={{ textAlign: 'center', padding: '64px 0' }}>
        <Spin size="large" />
        <div style={{ marginTop: 12, color: '#94a3b8' }}>正在加载钱包…</div>
      </div>
    );
  }

  if (status === 'login') {
    return <LoginPanel themeColor={themeColor} onLogin={devLogin} busy={loginBusy} />;
  }

  if (status === 'error') {
    return (
      <div data-testid="wallet-error">
        <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', textAlign: 'center', padding: '32px 0' }}>
          <div style={{ fontSize: 48, lineHeight: 1 }}>⚠️</div>
          <Title heading={4} style={{ marginTop: 16, marginBottom: 8 }}>
            钱包加载失败
          </Title>
          <Text type="tertiary">请稍后重试。</Text>
          <div style={{ marginTop: 12, color: '#cbd5e1', fontSize: 12 }}>code：{errCode}</div>
          <div style={{ marginTop: 16 }}>
            <Button theme="outline" onClick={loadWallet} style={{ borderColor: themeColor, color: themeColor, borderRadius: 8 }}>
              重试
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  // ready
  const balance = wallet && wallet.balance_usd != null ? wallet.balance_usd : 0;

  return (
    <div data-testid="wallet-page" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {/* 统计卡组 (uiux §3.3：当前余额 / 总用量 / API 请求；本切片仅余额为真实数据) */}
      <div style={{ display: 'flex', gap: 16, flexWrap: 'wrap' }}>
        <StatCard label="当前余额" value={`$${fmtUsd(balance)}`} unit="USD 额度" testid="wallet-balance" accent={themeColor} />
        <StatCard label="总用量" value="—" unit="USD" testid="wallet-usage" />
        <StatCard label="API 请求" value="—" testid="wallet-requests" />
      </div>

      {/* 添加资金 */}
      <Card
        title="添加资金"
        style={{ borderRadius: 12, border: '1px solid #e5e7eb', boxShadow: '0 1px 3px rgba(15,23,42,0.06)' }}
      >
        <Text type="tertiary" style={{ fontSize: 13 }}>
          选择充值金额（单位 ¥）。下单为 stub，<b>Slice 3 接真实支付</b>（支付宝 / 微信）。
        </Text>
        <div style={{ marginTop: 16, display: 'flex', gap: 12, flexWrap: 'wrap' }}>
          {RECHARGE_PRESETS.map((amt) => (
            <Button
              key={amt}
              data-testid={`recharge-${amt}`}
              theme="outline"
              loading={rechargeBusy === amt}
              disabled={rechargeBusy !== 0 && rechargeBusy !== amt}
              onClick={() => recharge(amt)}
              style={{ borderColor: themeColor, color: themeColor, borderRadius: 8, minWidth: 92 }}
            >
              ¥{amt}
            </Button>
          ))}
        </div>

        {rechargeResult && (
          <div
            data-testid="recharge-result"
            style={{
              marginTop: 16,
              padding: '10px 14px',
              borderRadius: 10,
              background: rechargeResult.ok ? softBg(themeColor) : 'rgba(220,38,38,0.08)',
              color: rechargeResult.ok ? themeColor : '#dc2626',
              fontSize: 13,
            }}
          >
            {rechargeResult.ok ? (
              <>
                已为 <b>¥{rechargeResult.amount}</b> 创建订单：
                <span data-testid="recharge-order-no" style={{ fontWeight: 700 }}>
                  {rechargeResult.orderNo}
                </span>
                <div style={{ marginTop: 4, color: '#94a3b8' }}>
                  支付下单 stub，Slice 3 接真实支付。
                </div>
              </>
            ) : (
              <>下单失败（{rechargeResult.code}）：{rechargeResult.message || '请稍后重试'}</>
            )}
          </div>
        )}
      </Card>

      {/* 兑换码 */}
      <Card
        title="兑换码"
        style={{ borderRadius: 12, border: '1px solid #e5e7eb', boxShadow: '0 1px 3px rgba(15,23,42,0.06)' }}
      >
        <div style={{ display: 'flex', gap: 12, alignItems: 'center', flexWrap: 'wrap' }}>
          <Input
            data-testid="redeem-input"
            value={code}
            onChange={setCode}
            onEnterPress={redeem}
            placeholder="输入兑换码"
            style={{ maxWidth: 320, flex: 1, minWidth: 200 }}
          />
          <Button
            data-testid="redeem-btn"
            theme="solid"
            loading={redeemBusy}
            onClick={redeem}
            style={{ background: '#000', color: '#fff', borderRadius: 8 }}
          >
            兑换额度
          </Button>
        </div>

        {redeemResult && (
          <div
            data-testid="redeem-result"
            style={{
              marginTop: 14,
              padding: '8px 12px',
              borderRadius: 8,
              fontSize: 13,
              background: redeemResult.ok ? softBg(themeColor) : 'rgba(220,38,38,0.08)',
              color: redeemResult.ok ? themeColor : '#dc2626',
              fontWeight: 600,
            }}
          >
            {redeemResult.message}
            {redeemResult.code ? `（${redeemResult.code}）` : ''}
          </div>
        )}
      </Card>
    </div>
  );
}
