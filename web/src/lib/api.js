// 统一 fetch 封装 (api-contract §1)：
//  - credentials:'include' → 始终带 cookie 做会话鉴权
//  - ?host= 透传 → 测试栈下后端可按 Host 识别租户 (生产用真实 Host，无需透传)
//  - 统一信封解析 → 成功 { data } 或裸资源对象；失败读 code (§1.1)，勿仅依赖 message
// 返回结构：{ ok, status, data?, code?, message? }

// 透传当前页 ?host=（hash 路由下 search 仍在 # 前，可直接读 window.location.search）
function withHost(path) {
  const search = window.location.search || '';
  if (!search.includes('host=')) return path;
  const host = new URLSearchParams(search).get('host');
  if (!host) return path;
  const sep = path.includes('?') ? '&' : '?';
  return `${path}${sep}host=${encodeURIComponent(host)}`;
}

export async function apiFetch(path, { method = 'GET', body } = {}) {
  let resp;
  try {
    resp = await fetch(withHost(path), {
      method,
      credentials: 'include', // cookie 鉴权
      headers: {
        Accept: 'application/json',
        ...(body !== undefined ? { 'Content-Type': 'application/json' } : {}),
      },
      ...(body !== undefined ? { body: JSON.stringify(body) } : {}),
    });
  } catch (err) {
    return { ok: false, status: 0, code: 'NETWORK', message: String((err && err.message) || err) };
  }

  let payload = {};
  try {
    payload = await resp.json();
  } catch (_e) {
    payload = {};
  }

  if (resp.ok) {
    // 成功信封：{ data: {...} } 或裸资源对象，两种都兼容
    const data = payload && payload.data ? payload.data : payload || {};
    return { ok: true, status: resp.status, data };
  }
  return {
    ok: false,
    status: resp.status,
    code: (payload && payload.code) || 'INTERNAL',
    message: (payload && payload.message) || '',
  };
}
