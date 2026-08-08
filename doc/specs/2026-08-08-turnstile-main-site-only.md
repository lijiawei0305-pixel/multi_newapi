# Turnstile 方案 A：仅主站校验 + 自有自定义域可扩展

> **日期**：2026-08-08
> **背景**：OEM 自定义域（如 `test1.ccchhxx.top`）上 Cloudflare Turnstile 因 **Site Key 主机名白名单** 未包含该域而失败；全局开关会拖垮所有代理站登录。
> **决策**：方案 A — 默认**只保护主站**；平台以后自有的自定义入口域可通过 env 白名单扩展。

---

## 1. 行为（代码已落地）

| Host 类型 | 示例 | 全局 Turnstile 开时 |
| --- | --- | --- |
| 主站 apex / www | `wedreamhub.com` / `www.wedreamhub.com` | ✅ 显示组件 + 后端强制校验 |
| 平台子域（代理） | `test1.wedreamhub.com` | ❌ 不显示、不校验 |
| OEM 自定义域 | `test1.ccchhxx.top` | ❌ 不显示、不校验 |
| 平台自有扩展域 | 见 `TURNSTILE_EXTRA_HOSTS` | ✅ 与主站相同（须先在 CF 加主机名） |

实现：

- `common.TurnstileAppliesToHost`（`common/turnstile_host.go`）
- `middleware.TurnstileCheck`：仅当 `TurnstileCheckEnabled && TurnstileAppliesToHost(Host)` 才校验
- `GET /api/status` 的 `turnstile_check`：**按请求 Host** 计算（前端靠此决定是否渲染组件）

---

## 2. 现在如何「只开主站」（运维步骤）

### 2.1 Cloudflare Turnstile 控制台

1. 打开对应 **Site Key**（与后台 `TurnstileSiteKey` 一致）。
2. **Hostname Management** 仅保留主站相关，例如：
   - `wedreamhub.com`
   - `www.wedreamhub.com`
3. **不要**把每个代理的 OEM 域都加进白名单（不可规模化）。

### 2.2 应用后台

1. 确保已填 **Site Key / Secret Key**。
2. 打开 **TurnstileCheckEnabled**（系统设置 → 认证/安全 → Turnstile）。
3. 部署含本改动的版本后：
   - 访问 `https://www.wedreamhub.com/login` → 应出现机器人验证。
   - 访问 `https://test1.wedreamhub.com/login` 或 `https://test1.ccchhxx.top/login` → **无**验证框，可直接登录。

### 2.3 线上状态（2026-08-08 已落地）

| 步骤 | 状态 |
| --- | --- |
| Host-aware 代码部署 | ✅ 已 build 上线 |
| `TurnstileCheckEnabled` | ✅ `true` |
| `www.wedreamhub.com` → `turnstile_check` | ✅ `true`（主站开） |
| `test1.wedreamhub.com` → `turnstile_check` | ✅ `false`（代理子域关） |
| `test1.ccchhxx.top` → `turnstile_check` | ✅ `false`（OEM 自定义域关） |

**含义**：绑定/开通代理子域名后，**无需额外操作**，该子域登录默认不跑 Turnstile；主站继续有机器人验证。

---

## 3. 以后想给「我们自己部署的自定义域名」也开 Turnstile

适用：平台自己的入口域（不是每个代理随便绑的 OEM 域），例如 `portal.ourbrand.com`。

### 步骤（两处都要做）

**① Cloudflare Turnstile**

- 在同一 Site Key 的 Hostname 列表中 **新增** 该 FQDN（如 `portal.ourbrand.com`）。
- 保存后等 CF 侧生效（通常很快）。

**② 应用环境变量**

在服务器 `.env` / compose 增加（逗号分隔，可多个）：

```bash
TURNSTILE_EXTRA_HOSTS=portal.ourbrand.com
# 多个：
# TURNSTILE_EXTRA_HOSTS=portal.ourbrand.com,login.ourbrand.com
```

然后 **重启 app** 使进程读到 env。

**③ 验收**

```bash
# 主站仍为 true
curl -sS -H 'Host: www.wedreamhub.com' http://127.0.0.1:3100/api/status | jq '.data.turnstile_check'

# 扩展域为 true（开关已开时）
curl -sS -H 'Host: portal.ourbrand.com' http://127.0.0.1:3100/api/status | jq '.data.turnstile_check'

# 普通代理 OEM 域仍为 false
curl -sS -H 'Host: test1.ccchhxx.top' http://127.0.0.1:3100/api/status | jq '.data.turnstile_check'
```

浏览器打开扩展域登录页，应出现 Turnstile 且能通过。

### 不要做的事

| 反模式 | 原因 |
| --- | --- |
| 只改 env、不改 CF 主机名 | 组件仍报错 / 校验失败 |
| 只改 CF、不改 env | 前端不渲染、后端不校验（该 Host 仍被策略跳过） |
| 把所有代理 OEM 域写进 `TURNSTILE_EXTRA_HOSTS` | 运维爆炸；与方案 A 目标相反 |

---

## 4. 与「代理自助自定义域名」的关系

| 能力 | 谁用 | Turnstile |
| --- | --- | --- |
| 平台子域 | 代理站默认 | 默认关 |
| 代理 OEM 自定义域 | 代理自助绑定 | 默认关 |
| 平台自有品牌域 | 运营/运维配置 | 主站默认开 + EXTRA 可选开 |

代理站安全仍靠：限流、鉴权、会话、风控等；不依赖 CF 机器人框。

---

## 5. 回滚

- 关后台 `TurnstileCheckEnabled` → 全站都不校验（与现网临时处置相同）。
- 或清空 `TURNSTILE_EXTRA_HOSTS` 并重启 → 仅主站受保护。

---

## 6. 修订

| 日期 | 说明 |
| --- | --- |
| 2026-08-08 | 方案 A 代码 + 运维手册；EXTRA_HOSTS 扩展自有自定义域 |
