# 域名 · SSL

> 由 `CLAUDE.md` 路由表指向。改 wildcard / 自定义域名绑定 / HTTPS 证书前先读此文。
> 本文记录**已落地实现**（2026-07，代理商自定义域名绑定）；权威需求见 `newapi-multitenant-development-plan.md` §6。

## §6.1 一期 wildcard 二级域名（已上线）

- 主站域名 `wedreamhub.com`；每个租户自动获得 `<slug>.wedreamhub.com`（`tenant.TenantService.Create` 写入 `tenant_domains`，`is_primary=true`）。
- Nginx：`*.wedreamhub.com` 通配 vhost 反代到多租户栈 `127.0.0.1:3100`，`proxy_set_header Host $host`（保留 Host 供后端按 Host 解析租户）。
- 保留子域（slug 不可占用）：`www/api/admin/root/dashboard/static/cdn/status/support`（见 `internal/tenant/slug.go`）。

## §6.2 自定义域名（OEM）架构

代理在自有域名上跑自己的站点 = 让一个租户在**独立表** `tenant_custom_domains` 里再挂 1 条任意域名，
经所有权验证 + 证书就绪后才生效。**与 wildcard 分表**（决策见 [[custom-domain-decisions]]）：

- `tenant_domains`（wildcard 二级域名热路径）保持原样、零改动。
- `tenant_custom_domains`：`tenant_id`(唯一，每租户 1 个) / `domain`(唯一) / `status` / `verify_token` / `cert_status` / `cert_expires_at` / `last_error`。
- **安全红线**：只有 `status='active'` 的自定义域名参与解析。该不变量由独立表 + 显式 `WHERE status='active'` 双重保证，wildcard 查询路径不受影响。

代码：`internal/tenant/custom_domain.go`（领域服务 + 状态机 + 保留/格式纯函数）、`internal/tenant/gormrepo/gormrepo.go`（`customDomainRow` + 两段 `GetTenantByDomain`）、`internal/mtwire/custom_domain.go`（HTTP）。

## §6.3 绑定流程与状态机

状态机：`pending_dns` → `verifying` → `dns_verified` → `active`；任一步失败 → `failed`(+`last_error`)。

1. **绑定** `POST /api/tenant/custom-domain {domain}`（代理自助，owner 维度）：校验格式 → 拒保留域名（`wedreamhub.com` 及任意子域）→ 查每租户上限（已绑则 `DOMAIN_LIMIT`）→ 全局查重（`DOMAIN_TAKEN`）→ 生成 `verify_token` → 落 `pending_dns`（**不进解析**）。返回需添加的 **A 记录**（→ 主站 IP `MT_SITE_IP`）+ **TXT 记录**（`_newapi-verify.<域名> = <token>`）。
2. **校验** `POST /api/tenant/custom-domain/verify`：后端 `net.LookupTXT` 查 `_newapi-verify.<域名>`，匹配 token → `dns_verified`（= 待发证信号）；否则 → `failed`（`DNS_VERIFY_FAILED`）。
3. **签发**（异步，服务器脚本）：见 §6.5。证书就绪后回写 → `active` + 失效 Host 缓存。
4. **查询** `GET /api/tenant/custom-domain`：返回当前绑定 + 状态 + `verify_token` + A/TXT 指引 + `last_error`（未绑定 `{bound:false}`）。
5. **解绑** `DELETE /api/tenant/custom-domain`：删记录 + 失效 Host 缓存。

错误码（`internal/tenant/errors.go`，模块前缀惯例，非规格里的 `ERR_*`）：`DOMAIN_INVALID` / `DOMAIN_RESERVED` / `DOMAIN_TAKEN` / `DOMAIN_LIMIT` / `DNS_VERIFY_FAILED` / `CUSTOM_DOMAIN_NOT_FOUND` / `DOMAIN_STATUS_INVALID`。

越权：代理自助端点挂 `UserAuth + AgentOwnerAuth`，租户一律取 `agentTenantID(c)`（直读 DB 校验 owner），A 代理拿不到/改不动 B 的绑定。

## §6.4 请求路由（按 Host 解析租户）

`gormrepo.GetTenantByDomain(host)`：先查 `tenant_domains`（wildcard），未命中再查 `tenant_custom_domains WHERE domain=? AND status='active'`；均未命中 `ErrTenantNotFound`。
`ResolveByHost` 走 `MemCache`（命中即返回，未命中回源 + 写缓存；负结果不缓存）。`TenantMiddleware` 注入租户，未知 Host 放行（由各 handler 自判缺租户）。
**写后失效**（`Cache.Invalidate`）：解绑、证书回写转 active 时清对应 Host 缓存。

### 主站 / 代理站 / 站点未开通（三态）

「无租户命中」不再一律回落主站，而由 `tenant.IsMainSiteHost(host)`（`internal/tenant/resolver.go`）再分两类，`/api/tenant/current`（`HandleTenantCurrent`）据此返回：

| Host | 分类 | 后端响应 | 前端渲染 |
| --- | --- | --- | --- |
| 命中 `tenant_domains` / active 自定义域名 | **代理站** | 200 + 品牌 | 代理品牌 |
| `www.wedreamhub.com`、apex `wedreamhub.com`、localhost/裸 IP/无关域（直连/调试） | **主站** | 404 `TENANT_NOT_FOUND` | 主站（前端 no-op） |
| `*.wedreamhub.com` 下未注册子域（非 www、无对应租户） | **站点未开通** | 404 `SITE_NOT_ACTIVATED` | 全屏「站点未开通」页（`features/errors/site-not-activated.tsx`） |

- 前端 `lib/tenant.ts:resolveTenant()` 用 `getApiErrorCode` 区分 `SITE_NOT_ACTIVATED`；`__root.tsx` 命中 `not-activated` 时渲染 `SiteNotActivated` 替代 `Outlet`，与 `useTenantBrand` 共用 `['tenant-resolution']` query（每会话一次调用）。
- **安全意义**：野域名 / 未注册子域指向平台 IP 时，只见中性「未开通」页，**不再泄露主站控制台**；主站严格限 www/apex（+ 直连兜底）。
- **apex 规范化**：`wedreamhub.com` 由独立 nginx server 块 301 → `www.wedreamhub.com`（`deploy/nginx/apex-redirect.wedreamhub.com.conf`，需证书 SAN 含 apex，或 CF 侧重定向规则二选一）。
- **保留词交互**：`www` 等在 `slug.go` 保留词表（代理不可占用）；其中仅 `www`（+ apex）渲染主站，其余保留词子域（api 走独立 3000 vhost 不经此逻辑；admin/status 等若被直接访问）落「站点未开通」。

## §6.5 HTTPS 证书（acme.sh + 服务器脚本）

异步签发，不在请求里同步做。Go 侧只写状态（`dns_verified` 即"待发证"信号）+ 两个内网端点；实际签发由服务器脚本消费。

- 内网端点（共享密钥 `X-Internal-Secret` = `MT_INTERNAL_SECRET`，deny-by-default；Nginx 另以 `location ^~ /api/internal/ { return 404; }` 拒公网）：
  - `GET /api/internal/domain/pending-cert` → 待发证域名列表；
  - `POST /api/internal/domain/cert-issued {domain,cert_status,expires_at}` → 转 active + 失效缓存。
- 脚本（`scripts/`，详见 `scripts/README.md`）：
  - `install-acme.sh`：装 acme.sh（默认 CA Let's Encrypt）+ webroot + systemd timer（每 2 分钟）。
  - `domain-cert-loop.sh`：轮询 `pending-cert` → 逐个 `issue-cert.sh`，含 LE 限流指数退避（fail 次数 → 2^n 分钟封顶 60）。
  - `issue-cert.sh <域名>`：写 HTTP-only vhost + reload（供 HTTP-01）→ `acme.sh --issue --webroot --keylength ec-256` → 装证书（`--reloadcmd` 续期自动 reload）→ 写 80→443 完整 vhost + reload → 回写 `cert-issued`。
- **每域名一个 vhost 文件**写入宝塔 vhost 目录 `/www/server/panel/vhost/nginx/custom_<域名>.conf`，反代 `127.0.0.1:3100`、`Host $host`、放行 `/.well-known/acme-challenge/`、独立 LE 证书。**不声明 `default_server`**，与宝塔现有配置零冲突（精确 server_name 优先匹配）。
- 前置（代理侧）：A 记录直连 `64.90.4.114`（**不要套 Cloudflare 代理**，否则 HTTP-01 取不到 challenge）。

## §6.6 OEM 品牌（自定义域名隐藏主站品牌）

接通既有 `internal/siteconfig` 包（GORM 落地 `tenant_site_configs` + `tenant_assets`）：

- 代理自助：`GET/PUT /api/tenant/site-config`（站名/主题色限色板/`brand_hidden`）+ `POST /api/tenant/site-config/logo`（multipart，限 jpg/png/webp ≤2MB，内联为 `data:` URL 存 `logo_url`，免对象存储；切对象存储仅换 `siteconfig.Blob` 实现）。
- `GET /api/tenant/current`（公开）合并装修：返回 `site_name`(已配置则用之，否则租户名) / `logo_url` / `theme_color` / `brand_hidden`。前端在自定义域名（`brand_hidden=true`）隐藏主站 logo/名称、显示代理品牌。

## 注意 / 风险（§13.7）

- **未配置证书的兜底**：域名在 `active` 前绝不解析命中（安全红线，已单测 + E2E）；前端 active 前显示"待生效"。
- **续期**：acme.sh 自带 cron + `--reloadcmd` 自动 reload；DB `cert_expires_at` 在续期后不自动刷新（仅 `issue-cert.sh` 回写时刷新），属可接受的展示滞后。
- **宝塔边界**：手写 vhost 与宝塔共存（主 nginx.conf `include vhost/nginx/*.conf`）；勿在宝塔面板编辑这些自定义域名站点，避免覆盖。
- 部署细节与踩坑见 `RETRO.md`。
