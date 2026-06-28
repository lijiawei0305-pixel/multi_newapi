import React, { useCallback, useEffect, useMemo, useState } from 'react';
import { Card, Button, Typography, Spin, Toast, Progress } from '@douyinfe/semi-ui';
import { apiFetch } from '../lib/api.js';
import { softBg } from '../lib/brand.js';

const { Title, Text } = Typography;

// ───────────────────────── 文案映射 (api-contract §1.1，按 code 分支，勿仅依赖 message) ─────────────────────────

// 购买失败 code → 文案 (TokenPlan / Billing 段)
const PURCHASE_COPY = {
  PURCHASE_LIMIT_EXCEEDED: '每人限购一次',
  PLAN_DISABLED: '该套餐已停用',
  PLAN_NOT_LISTED: '该套餐尚未上架',
  PLAN_NOT_FOUND: '套餐不存在或已下架',
  QUOTA_INSUFFICIENT: '余额不足，请先充值',
  RETAIL_BELOW_MIN: '套餐定价异常，请联系站点',
  UNAUTHORIZED: '登录已失效，请重新「演示登录」后再试',
  TOKEN_INVALID: '登录已失效，请重新「演示登录」后再试',
};

// 支付方式 → 文案 (购买响应 pay.method)
const PAY_METHOD = { wxpay: '微信支付', wechat: '微信支付', alipay: '支付宝' };

// 订阅状态 → 视觉 (api-contract §3 Subscription：active|exhausted|expired|refunded)
const SUB_STATUS = {
  active: { label: '使用中', color: '#16a34a', ended: false },
  exhausted: { label: '已用尽', color: '#dc2626', ended: true },
  expired: { label: '已到期', color: '#64748b', ended: true },
  refunded: { label: '已退款', color: '#64748b', ended: false },
};

// ───────────────────────── 工具 ─────────────────────────

function num(v) {
  if (v == null || v === '') return undefined;
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
}

// USD 额度格式化 (额度/计量统一 USD，呼应 api-contract §1)
function fmtUsd(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '0.00';
  return n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

// ¥ 售价格式化 (充值/收益/售价统一 ¥)；整数不带小数，含小数才保留两位
function fmtCny(v) {
  const n = Number(v);
  if (!Number.isFinite(n)) return '0';
  return Number.isInteger(n)
    ? n.toLocaleString('en-US')
    : n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

// 到期日格式化：兼容 ISO 字符串 / unix 秒 / unix 毫秒
function fmtDate(v) {
  if (v == null || v === '') return '—';
  let d;
  if (typeof v === 'number' || /^\d+$/.test(String(v))) {
    const n = Number(v);
    d = new Date(n < 1e12 ? n * 1000 : n); // 秒 vs 毫秒
  } else {
    d = new Date(v);
  }
  if (isNaN(d.getTime())) return String(v);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, '0');
  const day = String(d.getDate()).padStart(2, '0');
  return `${y}-${m}-${day}`;
}

// 信封解包：兼容裸数组 / {data:[...]} / {plans|subscriptions|items|list|records:[...]}
function asList(data) {
  if (Array.isArray(data)) return data;
  if (!data || typeof data !== 'object') return [];
  return data.plans || data.subscriptions || data.items || data.list || data.records || [];
}

// Plan 归一化 (api-contract §3 Plan)：字段名多源兜底，营销字段齐全
function normPlan(p) {
  const retail = num(p.retail_price_cny) ?? num(p.retail_price) ?? num(p.retail) ?? num(p.base_price_cny) ?? 0;
  const anchor = num(p.anchor_price_cny) ?? num(p.anchor_price) ?? num(p.anchor) ?? 0;
  const monthLimit = num(p.month_limit_usd) ?? num(p.month_limit) ?? 0;
  const multiplier = num(p.multiplier) ?? num(p.ratio) ?? 1;
  const validDays = num(p.valid_days) ?? num(p.valid_day) ?? 30;
  const code = String(p.code ?? p.plan_code ?? p.id ?? '');
  return {
    id: p.id ?? p.plan_id ?? code,
    code,
    name: p.name || p.plan_name || code || '套餐',
    retail,
    anchor,
    monthLimit,
    multiplier,
    validDays,
    discount_label: p.discount_label || p.discount || '',
    is_recommended: !!(p.is_recommended ?? p.recommended),
    badge: p.badge || '',
    sort: num(p.sort) ?? 0,
  };
}

// Subscription 归一化 (api-contract §3 Subscription)
function normSub(s) {
  const limit = num(s.month_limit_usd) ?? num(s.month_limit) ?? 0;
  const used = num(s.used_usd) ?? num(s.used) ?? 0;
  const remaining = num(s.remaining_usd) ?? Math.max(0, limit - used);
  return {
    id: s.id,
    plan_code: String(s.plan_code ?? s.code ?? s.plan_id ?? ''),
    plan_name: s.plan_name || s.name || '',
    month_limit_usd: limit,
    used_usd: used,
    remaining_usd: remaining,
    status: s.status || 'active',
    start_at: s.start_at,
    expire_at: s.expire_at ?? s.expired_at ?? s.expire_time,
  };
}

// ───────────────────────── 小组件 ─────────────────────────

function SectionHeader({ icon, title, subtitle }) {
  return (
    <div style={{ marginBottom: 14 }}>
      <div style={{ display: 'flex', alignItems: 'center', gap: 8 }}>
        <span style={{ fontSize: 18, lineHeight: 1 }}>{icon}</span>
        <Title heading={4} style={{ margin: 0 }}>
          {title}
        </Title>
      </div>
      {subtitle && (
        <Text type="tertiary" style={{ fontSize: 13 }}>
          {subtitle}
        </Text>
      )}
    </div>
  );
}

// 套餐卡 (uiux §4.1)：名称 + ¥售价大号 + 划线原价 + 折扣角标 + 月限额 + 倍率 + 推荐高亮 + 购买(黑/主题色)
function PlanCard({ plan, themeColor, onBuy, busy, disabled }) {
  const accent = themeColor || '#0f172a';
  const rec = plan.is_recommended || !!plan.badge; // 推荐 或 自带角标 → 高亮
  const recLabel = plan.badge || '推荐';

  const specRow = (label, value) => (
    <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'baseline' }}>
      <span style={{ color: '#94a3b8' }}>{label}</span>
      <span style={{ color: '#334155', fontWeight: 600 }}>{value}</span>
    </div>
  );

  return (
    <Card
      data-testid={`plan-card-${plan.code}`}
      style={{
        position: 'relative',
        height: '100%',
        borderRadius: 14,
        border: rec ? `2px solid ${accent}` : '1px solid #e5e7eb',
        boxShadow: rec ? `0 10px 28px ${softBg(accent)}` : '0 1px 3px rgba(15,23,42,0.06)',
        overflow: 'hidden',
      }}
      bodyStyle={{ padding: 20, height: '100%', display: 'flex', flexDirection: 'column', gap: 14 }}
    >
      {/* 推荐 / 角标 (右上角条) */}
      {rec && (
        <div
          data-testid={`plan-recommended-${plan.code}`}
          style={{
            position: 'absolute',
            top: 0,
            right: 0,
            background: accent,
            color: '#fff',
            fontSize: 12,
            fontWeight: 700,
            padding: '3px 12px',
            borderBottomLeftRadius: 12,
            letterSpacing: 0.5,
          }}
        >
          {recLabel}
        </div>
      )}

      {/* 套餐名 */}
      <div style={{ paddingRight: 56 }}>
        <Title heading={5} style={{ margin: 0, color: '#0f172a' }}>
          {plan.name}
        </Title>
      </div>

      {/* 价格区：¥售价大号 + 折扣角标，下方划线原价 */}
      <div>
        <div style={{ display: 'flex', alignItems: 'baseline', gap: 8, flexWrap: 'wrap' }}>
          <span
            data-testid={`plan-price-${plan.code}`}
            style={{ fontSize: 34, fontWeight: 800, color: '#0f172a', lineHeight: 1 }}
          >
            ¥{fmtCny(plan.retail)}
          </span>
          {plan.discount_label && (
            <span
              data-testid={`plan-discount-${plan.code}`}
              style={{
                background: '#ef4444',
                color: '#fff',
                fontSize: 12,
                fontWeight: 700,
                padding: '2px 8px',
                borderRadius: 999,
                lineHeight: 1.4,
              }}
            >
              {plan.discount_label}
            </span>
          )}
        </div>
        {plan.anchor > plan.retail && (
          <div style={{ marginTop: 6 }}>
            <span style={{ textDecoration: 'line-through', color: '#94a3b8', fontSize: 14 }}>
              ¥{fmtCny(plan.anchor)}
            </span>
            <span style={{ marginLeft: 6, color: '#cbd5e1', fontSize: 12 }}>原价</span>
          </div>
        )}
      </div>

      {/* 规格区 */}
      <div style={{ display: 'flex', flexDirection: 'column', gap: 7, fontSize: 13, paddingTop: 4, borderTop: '1px dashed #eef2f7' }}>
        {specRow('月度额度', `$${fmtUsd(plan.monthLimit)} USD`)}
        {specRow('计费倍率', `x${Number(plan.multiplier)}`)}
        {specRow('有效期', `${plan.validDays} 天`)}
      </div>

      {/* 购买按钮 (推荐用主题色，其余黑底白字) */}
      <Button
        data-testid={`plan-buy-${plan.code}`}
        block
        theme="solid"
        loading={busy}
        disabled={disabled}
        onClick={() => onBuy(plan)}
        style={{
          marginTop: 'auto',
          background: rec ? accent : '#000',
          color: '#fff',
          borderRadius: 10,
          height: 40,
          fontWeight: 600,
        }}
      >
        购买
      </Button>
    </Card>
  );
}

// 我的套餐卡 (uiux §4.2)：状态 + 进度条 used/limit + 剩余 + 到期；用尽/到期 → 醒目「重购」
function SubCard({ sub, themeColor, onRebuy, rebuyBusy, canRebuy }) {
  const accent = themeColor || '#0f172a';
  const meta = SUB_STATUS[sub.status] || { label: sub.status || '未知', color: '#64748b', ended: false };
  const limit = sub.month_limit_usd;
  const used = sub.used_usd;
  const percent = limit > 0 ? Math.min(100, Math.round((used / limit) * 100)) : 0;
  const barColor = meta.ended ? '#ef4444' : accent;

  return (
    <Card
      data-testid={`sub-card-${sub.plan_code}`}
      style={{
        height: '100%',
        borderRadius: 12,
        border: '1px solid #e5e7eb',
        boxShadow: '0 1px 3px rgba(15,23,42,0.06)',
      }}
      bodyStyle={{ padding: 18, height: '100%', display: 'flex', flexDirection: 'column', gap: 12 }}
    >
      {/* 头：套餐名 + 状态 pill */}
      <div style={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 8 }}>
        <Title heading={5} style={{ margin: 0 }}>
          {sub.plan_name || sub.plan_code}
        </Title>
        <span
          data-testid={`sub-status-${sub.plan_code}`}
          style={{
            display: 'inline-flex',
            alignItems: 'center',
            gap: 6,
            fontSize: 12,
            fontWeight: 600,
            color: meta.color,
            background: `${meta.color}14`,
            padding: '3px 10px',
            borderRadius: 999,
            whiteSpace: 'nowrap',
          }}
        >
          <span style={{ width: 6, height: 6, borderRadius: '50%', background: meta.color }} />
          {meta.label}
        </span>
      </div>

      {/* 进度条 used/limit */}
      <div data-testid={`sub-progress-${sub.plan_code}`} data-percent={percent}>
        <Progress percent={percent} stroke={barColor} showInfo={false} aria-label={`${percent}%`} />
        <div style={{ marginTop: 6, display: 'flex', justifyContent: 'space-between', fontSize: 12, color: '#64748b' }}>
          <span>
            已用 <b style={{ color: '#334155' }}>${fmtUsd(used)}</b> / ${fmtUsd(limit)} USD
          </span>
          <span>{percent}%</span>
        </div>
      </div>

      {/* 剩余 + 到期 */}
      <div style={{ display: 'flex', justifyContent: 'space-between', fontSize: 13 }}>
        <span style={{ color: '#94a3b8' }}>
          剩余 <b style={{ color: accent }}>${fmtUsd(sub.remaining_usd)}</b>
        </span>
        <span style={{ color: '#94a3b8' }}>到期 {fmtDate(sub.expire_at)}</span>
      </div>

      {/* 用尽/到期 → 醒目重购 (不自动用钱包，独立计量) */}
      {meta.ended && (
        <Button
          data-testid={`sub-rebuy-${sub.plan_code}`}
          block
          theme="solid"
          loading={rebuyBusy}
          disabled={!canRebuy}
          onClick={onRebuy}
          style={{ marginTop: 'auto', background: accent, color: '#fff', borderRadius: 10, fontWeight: 600 }}
        >
          {canRebuy ? '重新购买' : '套餐已下架'}
        </Button>
      )}
    </Card>
  );
}

// 登录态：未登录 → 演示登录 (沿用钱包页 401 回落 + POST /api/dev/login)
function LoginPanel({ themeColor, onLogin, busy }) {
  return (
    <div data-testid="plans-login">
      <Card
        style={{
          borderRadius: 12,
          border: '1px solid #e5e7eb',
          borderTop: `4px solid ${themeColor}`,
          textAlign: 'center',
          padding: '32px 0',
        }}
      >
        <div style={{ fontSize: 48, lineHeight: 1 }}>🎁</div>
        <Title heading={4} style={{ marginTop: 16, marginBottom: 8 }}>
          套餐购买需登录后查看
        </Title>
        <Text type="tertiary">开发演示环境可一键登录体验选购 / 我的套餐流程。</Text>
        <div style={{ marginTop: 20 }}>
          <Button
            data-testid="plans-login-btn"
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

// ───────────────────────── 主页面 ─────────────────────────

export default function Plans({ brand }) {
  const themeColor = (brand && brand.theme_color) || '#0f172a';

  // status: loading | login | ready | error
  const [status, setStatus] = useState('loading');
  const [plans, setPlans] = useState([]);
  const [subs, setSubs] = useState([]);
  const [errCode, setErrCode] = useState('');

  const [loginBusy, setLoginBusy] = useState(false);
  const [buyingId, setBuyingId] = useState(null); // 进行中的套餐 id (null = 空闲)
  const [purchaseResult, setPurchaseResult] = useState(null);

  // code → plan，供「重购」按已知 code 回购
  const planByCode = useMemo(() => {
    const m = {};
    plans.forEach((p) => {
      m[p.code] = p;
    });
    return m;
  }, [plans]);

  // 仅刷新「我的套餐」(购买后不必重拉套餐列表)
  const loadSubs = useCallback(async () => {
    const r = await apiFetch('/api/tenant/subscriptions');
    if (r.ok) setSubs(asList(r.data).map(normSub));
  }, []);

  // 首屏：并行拉套餐 + 订阅；套餐 401 → 整页 login；套餐失败 → error
  const load = useCallback(async () => {
    setStatus('loading');
    const [pr, sr] = await Promise.all([
      apiFetch('/api/tenant/token-plans'),
      apiFetch('/api/tenant/subscriptions'),
    ]);
    if (pr.status === 401 || pr.code === 'UNAUTHORIZED' || pr.code === 'TOKEN_INVALID') {
      setStatus('login');
      return;
    }
    if (!pr.ok) {
      setErrCode(pr.code || 'INTERNAL');
      setStatus('error');
      return;
    }
    setPlans(asList(pr.data).map(normPlan).sort((a, b) => a.sort - b.sort));
    setSubs(sr.ok ? asList(sr.data).map(normSub) : []);
    setStatus('ready');
  }, []);

  useEffect(() => {
    load();
  }, [load]);

  // 演示登录
  const devLogin = useCallback(async () => {
    setLoginBusy(true);
    const r = await apiFetch('/api/dev/login', { method: 'POST', body: { username: 'demo@td' } });
    setLoginBusy(false);
    if (r.ok) {
      Toast.success('演示登录成功');
      load();
    } else {
      Toast.error('演示登录失败：' + (r.message || r.code));
      setErrCode(r.code || 'INTERNAL');
      setStatus('error');
    }
  }, [load]);

  // 购买：POST /token-plans/:id/purchase → 成功 Toast + 刷新我的套餐；失败按 code
  const buy = useCallback(
    async (plan) => {
      setBuyingId(plan.id);
      setPurchaseResult(null);
      const r = await apiFetch(`/api/tenant/token-plans/${encodeURIComponent(plan.id)}/purchase`, {
        method: 'POST',
        body: {},
      });
      setBuyingId(null);
      if (r.ok) {
        const orderNo = (r.data && (r.data.order_no || r.data.orderNo)) || '(无单号)';
        const pay = (r.data && r.data.pay) || {};
        Toast.success(`已为「${plan.name}」创建订单`);
        setPurchaseResult({
          ok: true,
          planName: plan.name,
          orderNo,
          payMethod: pay.method,
          validDays: (r.data && (r.data.valid_days || r.data.validDays)) || plan.validDays,
        });
        loadSubs();
      } else if (r.status === 401 || r.code === 'UNAUTHORIZED' || r.code === 'TOKEN_INVALID') {
        setStatus('login');
      } else {
        const msg = PURCHASE_COPY[r.code] || r.message || '购买失败，请稍后重试';
        Toast.error(msg);
        setPurchaseResult({ ok: false, planName: plan.name, code: r.code, message: msg });
      }
    },
    [loadSubs]
  );

  // 重购：按 plan_code 找回当前在售套餐再购
  const rebuy = useCallback(
    (sub) => {
      const p = planByCode[sub.plan_code];
      if (p) buy(p);
      else Toast.error('该套餐已下架，无法重购');
    },
    [planByCode, buy]
  );

  if (status === 'loading') {
    return (
      <div data-testid="plans-loading" style={{ textAlign: 'center', padding: '64px 0' }}>
        <Spin size="large" />
        <div style={{ marginTop: 12, color: '#94a3b8' }}>正在加载套餐…</div>
      </div>
    );
  }

  if (status === 'login') {
    return <LoginPanel themeColor={themeColor} onLogin={devLogin} busy={loginBusy} />;
  }

  if (status === 'error') {
    return (
      <div data-testid="plans-error">
        <Card style={{ borderRadius: 12, border: '1px solid #e5e7eb', textAlign: 'center', padding: '32px 0' }}>
          <div style={{ fontSize: 48, lineHeight: 1 }}>⚠️</div>
          <Title heading={4} style={{ marginTop: 16, marginBottom: 8 }}>
            套餐加载失败
          </Title>
          <Text type="tertiary">请稍后重试。</Text>
          <div style={{ marginTop: 12, color: '#cbd5e1', fontSize: 12 }}>code：{errCode}</div>
          <div style={{ marginTop: 16 }}>
            <Button theme="outline" onClick={load} style={{ borderColor: themeColor, color: themeColor, borderRadius: 8 }}>
              重试
            </Button>
          </div>
        </Card>
      </div>
    );
  }

  // ready
  return (
    <div data-testid="plans-page" style={{ display: 'flex', flexDirection: 'column', gap: 28 }}>
      {/* ───── 选购套餐 ───── */}
      <section>
        <SectionHeader icon="🛒" title="选购套餐" subtitle="独立月度额度，购买即享；划线价为原价，限时超值。" />

        {purchaseResult && (
          <div
            data-testid="purchase-result"
            style={{
              marginBottom: 16,
              padding: '10px 14px',
              borderRadius: 10,
              fontSize: 13,
              background: purchaseResult.ok ? softBg(themeColor) : 'rgba(220,38,38,0.08)',
              color: purchaseResult.ok ? themeColor : '#dc2626',
            }}
          >
            {purchaseResult.ok ? (
              <>
                已为 <b>{purchaseResult.planName}</b> 创建订单：
                <span data-testid="purchase-order-no" style={{ fontWeight: 700 }}>
                  {purchaseResult.orderNo}
                </span>
                {purchaseResult.payMethod && (
                  <>（{PAY_METHOD[purchaseResult.payMethod] || purchaseResult.payMethod}）</>
                )}
                <div style={{ marginTop: 4, color: '#94a3b8' }}>
                  支付成功后激活 {purchaseResult.validDays} 天套餐；额度独立计量，不占用钱包余额。
                </div>
              </>
            ) : (
              <>
                购买失败（{purchaseResult.code}）：{purchaseResult.message}
              </>
            )}
          </div>
        )}

        {plans.length === 0 ? (
          <Card style={{ borderRadius: 12, border: '1px dashed #e5e7eb', textAlign: 'center', padding: '28px 0' }}>
            <Text type="tertiary">暂无可购套餐</Text>
          </Card>
        ) : (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(260px, 1fr))',
              gap: 16,
              alignItems: 'stretch',
            }}
          >
            {plans.map((p) => (
              <PlanCard
                key={p.code || p.id}
                plan={p}
                themeColor={themeColor}
                onBuy={buy}
                busy={buyingId === p.id}
                disabled={buyingId !== null && buyingId !== p.id}
              />
            ))}
          </div>
        )}
      </section>

      {/* ───── 我的套餐 ───── */}
      <section data-testid="my-subs">
        <SectionHeader icon="📦" title="我的套餐" subtitle="独立计量；用尽 / 到期请重购，不自动扣钱包。" />

        {subs.length === 0 ? (
          <Card
            data-testid="my-subs-empty"
            style={{ borderRadius: 12, border: '1px dashed #e5e7eb', textAlign: 'center', padding: '32px 0' }}
          >
            <div style={{ fontSize: 40, lineHeight: 1 }}>🗂️</div>
            <Title heading={5} style={{ marginTop: 12, marginBottom: 4 }}>
              暂无套餐
            </Title>
            <Text type="tertiary">从上方选购一档套餐，开启独立月度额度。</Text>
          </Card>
        ) : (
          <div
            style={{
              display: 'grid',
              gridTemplateColumns: 'repeat(auto-fill, minmax(280px, 1fr))',
              gap: 16,
              alignItems: 'stretch',
            }}
          >
            {subs.map((s, i) => (
              <SubCard
                key={`${s.plan_code}-${s.id ?? i}`}
                sub={s}
                themeColor={themeColor}
                onRebuy={() => rebuy(s)}
                rebuyBusy={buyingId !== null && planByCode[s.plan_code] && buyingId === planByCode[s.plan_code].id}
                canRebuy={!!planByCode[s.plan_code]}
              />
            ))}
          </div>
        )}
      </section>
    </div>
  );
}
