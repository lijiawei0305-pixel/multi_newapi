import React, { useCallback, useEffect, useState } from 'react';
import { Card, Button, Typography, Select, TextArea, Input, Spin, Toast } from '@douyinfe/semi-ui';
import { apiFetch, getPlaygroundAuth, setPlaygroundAuth } from '../lib/api.js';
import { softBg } from '../lib/brand.js';

const { Title, Text } = Typography;

// ───────────────────────── 常量 ─────────────────────────

// 上游模型（默认 mini）。/v1/chat/completions 的 model 字段直传后端。
const DEFAULT_MODEL = 'gpt-5.4-mini';
const MODELS = [
  { value: 'gpt-5.4-mini', label: 'gpt-5.4-mini（默认 · 轻量）' },
  { value: 'gpt-5.4', label: 'gpt-5.4（标准）' },
  { value: 'gpt-5.5', label: 'gpt-5.5（强力）' },
];

// 单次回复上限（适中，演示足够、控成本）
const MAX_TOKENS = 256;

// 演示账号：demo@td 有套餐→走套餐桶；payg@tokendream 无套餐→走钱包桶（演示双桶计费）
const DEMO_ACCOUNTS = [
  {
    username: 'demo@td',
    label: '套餐演示账号',
    bucket: '套餐桶',
    desc: '有 active 套餐 → 走套餐桶（独立月度额度计量）',
    testid: 'pg-login-btn',
  },
  {
    username: 'payg@tokendream',
    label: '钱包演示账号',
    bucket: '钱包桶',
    desc: '无套餐 → 走钱包桶（扣 USD 余额）',
    testid: 'pg-login-payg',
  },
];

// 调用失败 code → 文案（api-contract §1.1 Billing/Relay/Risk 段；按 code 分支，勿仅依赖 message）
const CHAT_ERROR_COPY = {
  QUOTA_INSUFFICIENT: { title: '钱包余额不足', desc: '当前走钱包计费，余额不足。请先到「钱包」充值或兑换额度后重试。' },
  SUBSCRIPTION_EXHAUSTED: { title: '套餐额度已用尽', desc: '本月套餐额度已用尽。请到「套餐」页重新购买（不会自动扣用钱包余额）。' },
  SUBSCRIPTION_EXPIRED: { title: '套餐已到期', desc: '当前套餐已到期。请到「套餐」页重新购买后再调用。' },
  MODEL_NOT_ALLOWED: { title: '模型不被允许', desc: '当前密钥不允许调用所选模型，请更换模型或密钥。' },
  RATE_LIMITED: { title: '请求过于频繁', desc: '触发限流，请稍后再试。' },
  IP_NOT_ALLOWED: { title: 'IP 不被允许', desc: '当前 IP 不在允许范围内。' },
  STATUS_FORBIDDEN: { title: '状态禁止调用', desc: '当前账号 / 密钥状态不允许调用。' },
  UPSTREAM_ERROR: { title: '上游渠道异常', desc: '上游模型服务返回错误，请稍后重试。' },
  INTERNAL: { title: '服务异常', desc: '服务器内部错误，请稍后重试。' },
};

// 计费来源（计费日志 source/bucket）→ 中文桶名
const BUCKET_COPY = { subscription: '套餐', plan: '套餐', tokenplan: '套餐', sub: '套餐', wallet: '钱包', balance: '钱包', payg: '钱包' };

const CARD_STYLE = { borderRadius: 12, border: '1px solid #e5e7eb', boxShadow: '0 1px 3px rgba(15,23,42,0.06)' };
const LABEL_STYLE = { fontSize: 13, fontWeight: 600, color: '#475569', marginBottom: 6 };

// ───────────────────────── 工具 ─────────────────────────

function num(v) {
  const n = Number(v);
  return Number.isFinite(n) ? n : undefined;
}

// USD 扣费格式化：极小额（< 0.01）保留更多位，避免显示成 0.00
function fmtCharge(v) {
  const n = Number(v);
  if (!Number.isFinite(n) || n === 0) return '0';
  if (n < 0.01) return n.toFixed(6).replace(/0+$/, '').replace(/\.$/, '');
  return n.toLocaleString('en-US', { minimumFractionDigits: 2, maximumFractionDigits: 4 });
}

// 计费来源原始值 → 桶名（兜底子串匹配）
function bucketLabel(raw) {
  const s = String(raw || '').toLowerCase();
  if (!s) return '—';
  if (BUCKET_COPY[s]) return BUCKET_COPY[s];
  if (s.includes('sub') || s.includes('plan') || s.includes('token')) return '套餐';
  if (s.includes('wallet') || s.includes('bal') || s.includes('pay')) return '钱包';
  return raw;
}

// 信封解包：兼容裸数组 / {data:[...]} / {logs|items|list|records:[...]}
function asList(data) {
  if (Array.isArray(data)) return data;
  if (!data || typeof data !== 'object') return [];
  return data.logs || data.items || data.list || data.records || data.data || [];
}

// 计费日志归一化（字段名多源兜底；api-contract 未固定 BillingLog 字段）
function normLog(l, i) {
  const prompt = num(l.prompt_tokens) ?? num(l.promptTokens) ?? num(l.input_tokens) ?? 0;
  const completion = num(l.completion_tokens) ?? num(l.completionTokens) ?? num(l.output_tokens) ?? 0;
  const total = num(l.total_tokens) ?? prompt + completion;
  const charge = num(l.amount_usd) ?? num(l.cost_usd) ?? num(l.quota_usd) ?? num(l.quota) ?? 0;
  const rawSrc = l.bucket ?? l.source ?? l.source_type ?? l.billing_source ?? '';
  return {
    id: l.id ?? l.log_id ?? i,
    model: l.model_name || l.model || l.modelName || '—',
    prompt,
    completion,
    total,
    charge,
    source: bucketLabel(rawSrc),
  };
}

// usage 取值：兼容 OpenAI 字段名；total 缺失时由 prompt+completion 兜底
function usageVal(u, kind) {
  if (!u) return '—';
  const keys = {
    prompt: ['prompt_tokens', 'promptTokens', 'input_tokens'],
    completion: ['completion_tokens', 'completionTokens', 'output_tokens'],
    total: ['total_tokens', 'totalTokens'],
  }[kind];
  for (const k of keys) if (u[k] != null) return u[k];
  if (kind === 'total') {
    const p = usageVal(u, 'prompt');
    const c = usageVal(u, 'completion');
    if (p !== '—' && c !== '—') return Number(p) + Number(c);
  }
  return '—';
}

// ───────────────────────── 小组件 ─────────────────────────

// 桶徽标：套餐=主题色，钱包=蓝灰
function BucketTag({ source }) {
  const isPlan = source === '套餐';
  const color = isPlan ? '#16a34a' : '#2563eb';
  return (
    <span
      style={{
        display: 'inline-flex',
        alignItems: 'center',
        gap: 5,
        fontSize: 12,
        fontWeight: 600,
        color,
        background: `${color}14`,
        padding: '2px 9px',
        borderRadius: 999,
        whiteSpace: 'nowrap',
      }}
    >
      <span style={{ width: 6, height: 6, borderRadius: '50%', background: color }} />
      {source}
    </span>
  );
}

// 登录态：演示账号快捷登录（两个桶）+ 自定义账号（沿用钱包页 401 回落风格）
function LoginPanel({ accent, onLogin, busy }) {
  const [custom, setCustom] = useState('');
  const customName = custom.trim();
  return (
    <div data-testid="playground-login">
      <Card style={{ ...CARD_STYLE, borderTop: `4px solid ${accent}` }}>
        <div style={{ textAlign: 'center' }}>
          <div style={{ fontSize: 44, lineHeight: 1 }}>🎮</div>
          <Title heading={4} style={{ marginTop: 12, marginBottom: 4 }}>
            游乐场需登录后使用
          </Title>
          <Text type="tertiary">选择一个演示账号一键登录，体验「套餐桶 / 钱包桶」双路计费。</Text>
        </div>

        <div style={{ display: 'flex', flexDirection: 'column', gap: 12, marginTop: 18 }}>
          {DEMO_ACCOUNTS.map((a) => (
            <div
              key={a.username}
              style={{
                display: 'flex',
                alignItems: 'center',
                justifyContent: 'space-between',
                gap: 12,
                padding: '12px 14px',
                border: '1px solid #e5e7eb',
                borderRadius: 10,
                flexWrap: 'wrap',
              }}
            >
              <div style={{ minWidth: 0 }}>
                <div style={{ fontWeight: 600, color: '#0f172a' }}>
                  {a.label}
                  <span style={{ color: '#94a3b8', fontWeight: 400 }}> · {a.username}</span>
                </div>
                <div style={{ fontSize: 12, color: '#94a3b8', marginTop: 2 }}>{a.desc}</div>
              </div>
              <Button
                data-testid={a.testid}
                theme="solid"
                loading={busy === a.username}
                disabled={!!busy && busy !== a.username}
                onClick={() => onLogin(a.username)}
                style={{ background: '#000', color: '#fff', borderRadius: 8, minWidth: 118 }}
              >
                登录（{a.bucket}）
              </Button>
            </div>
          ))}
        </div>

        <div
          style={{
            marginTop: 16,
            paddingTop: 16,
            borderTop: '1px dashed #e5e7eb',
            display: 'flex',
            gap: 10,
            alignItems: 'center',
            flexWrap: 'wrap',
          }}
        >
          <Text type="tertiary" style={{ fontSize: 13 }}>
            或自定义账号：
          </Text>
          <Input
            data-testid="pg-login-username"
            value={custom}
            onChange={setCustom}
            onEnterPress={() => customName && onLogin(customName)}
            placeholder="username（如 demo@td）"
            style={{ maxWidth: 240, flex: 1, minWidth: 180 }}
          />
          <Button
            data-testid="pg-login-custom"
            theme="outline"
            loading={!!customName && busy === customName}
            disabled={!customName}
            onClick={() => onLogin(customName)}
            style={{ borderColor: accent, color: accent, borderRadius: 8 }}
          >
            登录
          </Button>
        </div>
      </Card>
    </div>
  );
}

// 调用错误面板：按 code 文案 + 对应 CTA（余额不足→充值 / 套餐用尽→重购）
function ErrorPanel({ error, accent }) {
  const copy = CHAT_ERROR_COPY[error.code] || { title: '调用失败', desc: error.message || '请稍后重试。' };
  const isQuota = error.code === 'QUOTA_INSUFFICIENT';
  const isSub = error.code === 'SUBSCRIPTION_EXHAUSTED' || error.code === 'SUBSCRIPTION_EXPIRED';
  return (
    <div data-testid="pg-error">
      <Card style={{ borderRadius: 12, border: '1px solid #fecaca', background: 'rgba(220,38,38,0.04)' }}>
        <div style={{ display: 'flex', gap: 12, alignItems: 'flex-start' }}>
          <span style={{ fontSize: 22, lineHeight: 1 }}>⚠️</span>
          <div style={{ flex: 1, minWidth: 0 }}>
            <div style={{ fontWeight: 700, color: '#dc2626' }}>{copy.title}</div>
            <div style={{ color: '#7f1d1d', fontSize: 13, marginTop: 4 }}>{copy.desc}</div>
            <div data-testid="pg-error-code" style={{ color: '#cbd5e1', fontSize: 12, marginTop: 6 }}>
              code：{error.code || 'INTERNAL'}
              {error.message ? ` · ${error.message}` : ''}
            </div>
            {(isQuota || isSub) && (
              <div style={{ marginTop: 10 }}>
                <Button
                  theme="solid"
                  size="small"
                  onClick={() => {
                    window.location.hash = isQuota ? '#/wallet' : '#/plans';
                  }}
                  style={{ background: accent, color: '#fff', borderRadius: 8 }}
                >
                  {isQuota ? '前往充值' : '前往重购套餐'}
                </Button>
              </div>
            )}
          </div>
        </div>
      </Card>
    </div>
  );
}

// 计费日志表：模型 / tokens(入/出) / 扣费(USD) / 来源(套餐|钱包)
function LogTable({ logs }) {
  if (!logs || logs.length === 0) {
    return (
      <div data-testid="pg-log" style={{ padding: '20px 16px', color: '#94a3b8', fontSize: 13, textAlign: 'center' }}>
        暂无调用记录，发送一次对话后这里会显示模型 / tokens / 扣费 / 来源。
      </div>
    );
  }
  const cols = '1.4fr 1fr 1fr 0.9fr';
  return (
    <div data-testid="pg-log">
      <div
        style={{
          display: 'grid',
          gridTemplateColumns: cols,
          fontSize: 12,
          color: '#94a3b8',
          padding: '10px 16px',
          borderBottom: '1px solid #f1f5f9',
        }}
      >
        <span>模型</span>
        <span>tokens（入/出）</span>
        <span>扣费（USD）</span>
        <span>来源</span>
      </div>
      {logs.map((l, i) => (
        <div
          key={l.id ?? i}
          data-testid={`pg-log-row-${i}`}
          style={{
            display: 'grid',
            gridTemplateColumns: cols,
            alignItems: 'center',
            fontSize: 13,
            color: '#334155',
            padding: '10px 16px',
            borderBottom: '1px solid #f8fafc',
          }}
        >
          <span style={{ fontWeight: 600, overflow: 'hidden', textOverflow: 'ellipsis', whiteSpace: 'nowrap' }}>{l.model}</span>
          <span>
            {l.prompt}/{l.completion}
          </span>
          <span>${fmtCharge(l.charge)}</span>
          <span>
            <BucketTag source={l.source} />
          </span>
        </div>
      ))}
    </div>
  );
}

// ───────────────────────── 主页面 ─────────────────────────

export default function Playground({ brand }) {
  const accent = (brand && brand.theme_color) || '#0f172a';

  // 登录态（内存令牌，跨路由 remount 保留）：{ apiToken, username } | null
  const [auth, setAuth] = useState(() => getPlaygroundAuth());
  const [loginBusy, setLoginBusy] = useState(''); // 正在登录的 username（'' = 空闲）

  const [model, setModel] = useState(DEFAULT_MODEL);
  const [prompt, setPrompt] = useState('');
  const [sending, setSending] = useState(false);

  const [response, setResponse] = useState(null); // 助手回复文本
  const [usage, setUsage] = useState(null); // { prompt_tokens, completion_tokens, total_tokens }
  const [error, setError] = useState(null); // { code, message }
  const [logs, setLogs] = useState([]);

  const bucketHint = auth ? (DEMO_ACCOUNTS.find((a) => a.username === auth.username) || {}).bucket : undefined;

  // 拉近几条计费日志（cookie 鉴权 /api/tenant/*；dev-login 同时建立了会话）
  const loadLogs = useCallback(async () => {
    const r = await apiFetch('/api/tenant/billing-logs?page=1&page_size=5');
    if (r.ok) setLogs(asList(r.data).slice(0, 5).map(normLog));
  }, []);

  // 首屏：若已有内存令牌，拉一次日志
  useEffect(() => {
    if (getPlaygroundAuth()) loadLogs();
    // 仅挂载时执行
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // 演示登录：POST /api/dev/login {username} → 取 api_token 存内存（供 /v1 用），并建立会话
  const doLogin = useCallback(
    async (username) => {
      if (!username) return;
      setLoginBusy(username);
      const r = await apiFetch('/api/dev/login', { method: 'POST', body: { username } });
      setLoginBusy('');
      if (r.ok) {
        const d = r.data || {};
        const token = d.api_token || d.apiToken || d.access_token || d.token || d.key;
        if (!token) {
          Toast.error('登录成功但未返回 api_token，无法调用 /v1');
          return;
        }
        const next = { apiToken: token, username };
        setPlaygroundAuth(next);
        setAuth(next);
        setResponse(null);
        setUsage(null);
        setError(null);
        Toast.success(`已登录：${username}`);
        loadLogs();
      } else {
        Toast.error('演示登录失败：' + (r.message || r.code));
      }
    },
    [loadLogs]
  );

  // 切换账号 / 登出：清内存令牌与上次会话
  const logout = useCallback(() => {
    setPlaygroundAuth(null);
    setAuth(null);
    setResponse(null);
    setUsage(null);
    setError(null);
    setLogs([]);
  }, []);

  // 发送：POST /v1/chat/completions，Bearer <api_token>
  const send = useCallback(async () => {
    const text = prompt.trim();
    if (!text) {
      Toast.warning('请输入提示词');
      return;
    }
    if (!auth) return;
    setSending(true);
    setError(null);
    setResponse(null);
    setUsage(null);
    const r = await apiFetch('/v1/chat/completions', {
      method: 'POST',
      headers: { Authorization: `Bearer ${auth.apiToken}` },
      body: { model, messages: [{ role: 'user', content: text }], max_tokens: MAX_TOKENS },
    });
    setSending(false);
    if (r.ok) {
      const d = r.data || {};
      const content = (d.choices && d.choices[0] && d.choices[0].message && d.choices[0].message.content) || '';
      setResponse(content || '(空回复)');
      setUsage(d.usage || null);
      loadLogs(); // 刷新计费日志，演示扣费来源
    } else if (r.status === 401 || r.code === 'UNAUTHORIZED' || r.code === 'TOKEN_INVALID') {
      Toast.error('登录已失效，请重新登录');
      logout();
    } else {
      setError({ code: r.code, message: r.message });
    }
  }, [prompt, model, auth, loadLogs, logout]);

  // 未登录 → 登录面板
  if (!auth) {
    return <LoginPanel accent={accent} onLogin={doLogin} busy={loginBusy} />;
  }

  // 已登录 → 游乐场
  return (
    <div data-testid="playground-page" style={{ display: 'flex', flexDirection: 'column', gap: 16 }}>
      {/* 账号条 */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', gap: 12, flexWrap: 'wrap' }}>
        <div data-testid="pg-account" style={{ display: 'inline-flex', alignItems: 'center', gap: 8, fontSize: 13, color: '#334155' }}>
          <span style={{ width: 8, height: 8, borderRadius: '50%', background: '#16a34a' }} />
          已登录 <b>{auth.username}</b>
          {bucketHint && (
            <span style={{ color: accent, background: softBg(accent), borderRadius: 999, padding: '2px 10px', fontWeight: 600 }}>
              预计走 {bucketHint}
            </span>
          )}
        </div>
        <Button data-testid="pg-switch" theme="borderless" onClick={logout} style={{ color: '#64748b' }}>
          切换账号
        </Button>
      </div>

      {/* 对话卡 */}
      <Card title="AI 对话" style={CARD_STYLE}>
        <div style={{ display: 'flex', flexDirection: 'column', gap: 14 }}>
          <div>
            <div style={LABEL_STYLE}>模型</div>
            <div data-testid="pg-model">
              <Select value={model} onChange={setModel} style={{ width: 260, maxWidth: '100%' }} aria-label="模型选择">
                {MODELS.map((m) => (
                  <Select.Option key={m.value} value={m.value} data-testid={`pg-model-opt-${m.value}`}>
                    {m.label}
                  </Select.Option>
                ))}
              </Select>
            </div>
          </div>

          <div>
            <div style={LABEL_STYLE}>提示词</div>
            <TextArea
              data-testid="pg-prompt"
              value={prompt}
              onChange={setPrompt}
              autosize={{ minRows: 3, maxRows: 8 }}
              placeholder="输入你的问题，例如：用一句话介绍多租户代理分销平台"
              onKeyDown={(e) => {
                if ((e.metaKey || e.ctrlKey) && e.key === 'Enter') {
                  e.preventDefault();
                  send();
                }
              }}
            />
          </div>

          <div style={{ display: 'flex', alignItems: 'center', gap: 12, flexWrap: 'wrap' }}>
            <Button
              data-testid="pg-send"
              theme="solid"
              loading={sending}
              disabled={!prompt.trim()}
              onClick={send}
              style={{ background: '#000', color: '#fff', borderRadius: 8, minWidth: 120 }}
            >
              发送
            </Button>
            <Text type="tertiary" style={{ fontSize: 12 }}>
              ⌘ / Ctrl + Enter 发送 · 单次最多 {MAX_TOKENS} tokens
            </Text>
          </div>
        </div>
      </Card>

      {/* 调用错误（按 code 文案 + CTA） */}
      {error && <ErrorPanel error={error} accent={accent} />}

      {/* 助手回复 + 本次用量 */}
      {(sending || response != null) && (
        <Card title="助手回复" style={CARD_STYLE}>
          {sending ? (
            <div style={{ textAlign: 'center', padding: '24px 0' }}>
              <Spin />
              <div style={{ marginTop: 10, color: '#94a3b8', fontSize: 13 }}>模型生成中…</div>
            </div>
          ) : (
            <>
              <div
                data-testid="pg-response"
                style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-word', color: '#0f172a', fontSize: 14, lineHeight: 1.7 }}
              >
                {response}
              </div>
              {usage && (
                <div
                  data-testid="pg-usage"
                  style={{
                    marginTop: 14,
                    paddingTop: 12,
                    borderTop: '1px dashed #eef2f7',
                    fontSize: 13,
                    color: '#64748b',
                  }}
                >
                  本次用量：输入 <b style={{ color: '#334155' }}>{usageVal(usage, 'prompt')}</b> · 输出{' '}
                  <b style={{ color: '#334155' }}>{usageVal(usage, 'completion')}</b> · 合计{' '}
                  <b style={{ color: accent }}>{usageVal(usage, 'total')}</b> tokens
                </div>
              )}
            </>
          )}
        </Card>
      )}

      {/* 最近调用 · 计费日志（演示扣费来源 套餐|钱包） */}
      <Card title="最近调用 · 计费日志" style={CARD_STYLE} bodyStyle={{ padding: 0 }}>
        <LogTable logs={logs} />
      </Card>
    </div>
  );
}
