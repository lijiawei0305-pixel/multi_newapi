# 子代理管理:一键开子域名 + 删除代理(含数据处理)设计(2026-07-04)

> 「子代理管理」页新增两组能力:①管理员为代理**一键开通子域名建站**(页面输入 label);②**删除代理**并妥善处理代理站数据。破坏性删除 + 真实用户数据,**落地前需用户确认三个决策点**(见 §四,均已给推荐值)。

## 现状(探子核实)

- **开子域名 = 纯 DB**:nginx `*.wedreamhub.com` 通配已反代到 app(`doc/domains-ssl.md` §6.1),开子域名只需往 `tenant_domains` 插一条 `<label>.wedreamhub.com`。`tenant_domains.tenant_id` 非唯一 → 一租户可多子域名。积木现成:`internal/tenant` 的 `CreateDomain`(gormrepo:162)、`slugValidator`(slug.go:37,DNS label + 保留词 www/api/admin/…)、Host 缓存失效(`Cache.Invalidate`)。`EnsureSubdomain`(service.go:53)只会派生 `<slug>`,不接受任意 label。
- **无删除能力**:仅有软删 `status=deleted`(`tenant` 状态机 active/suspended/deleted,deleted 为终态)。且**现有软删是漏的**:Host 解析(`GetTenantByDomain`,gormrepo:186)**不过滤 status** → 删了站点仍能打开、用户仍能登录充值;列表 `HandleAdminListAgents` 不过滤 status → 已删代理仍显示;子域名不回收。
- **全库零外键/级联**,一个代理站数据散在 ~25 张以 `tenant_id` 为键的表 + 终端用户(原生 `users` 表加了 `tenant_id` 列,身上挂 `quota` 余额/tokens/订阅/消费历史)。**硬删极危险**(手写 25 表删除器、漏表留孤儿、销毁真实用户钱与历史、不可逆)。

## 一、一键开子域名(admin)

**后端**:
- 新 service 方法 `AddSubdomain(ctx, tenantID int64, label string) error`(`internal/tenant/service.go`):
  1. `slugValidator.Validate(label)`(非法格式/保留词 → 拒);
  2. `domain = label + ".wedreamhub.com"`;全局查重 `GetTenantByDomain(domain)` 命中即拒(`DOMAIN_TAKEN`);
  3. **单主域名语义**:先删该租户现有 `is_primary` 的 `tenant_domains` 行(回收旧 label)→ `CreateDomain{TenantID, Domain, IsPrimary:true}`;
  4. `Cache.Invalidate(旧/新 domain)`。
- 新 admin 端点 `PUT /api/admin/agents/:id/domain` body `{ "label": "acme" }`(AdminAuth),在 `router/mt-router.go` 的 admin agents 组注册。
- `agentOut` 增加 `subdomain`(当前主子域名,取 `tenant_domains` primary)供 UI 回显。
- **不改 level**:开子域名只让该租户在此域名可达并渲染其站点(siteconfig 若已设则品牌化);等级(L0/L1 自助能力)与之正交,单独在编辑里改。

**前端**(`web/default/src/features/agents/`):
- 编辑抽屉「身份」区加一块**「子域名」**:输入框(label)+「开通/更新」按钮 + 当前子域名展示(`<label>.wedreamhub.com`,可复制/点击打开)。
- 调 `PUT /api/admin/agents/:id/domain`,成功 toast「子域名已开通」。W5 中文(defaultValue 兜底)。

## 二、删除代理(推荐:归档软删,不硬删)

**后端** `DELETE /api/admin/agents/:id`(AdminAuth)→ service `ArchiveAgent(ctx, tenantID)`:
1. **结算闸门**(决策③):`agent_wallets.withdrawable_balance + frozen_withdraw_amount > 0` → 拒删 `AGENT_HAS_UNSETTLED_BALANCE`(先打款/清零)。
2. 置 `tenants.status = deleted`(复用 `SetStatus`)。
3. **回收子域名**:删该租户全部 `tenant_domains` 行 + `Cache.Invalidate`;解绑自定义域名(复用现有强制解绑)。
4. **终端用户**(决策②·推荐迁回主站):`UPDATE users SET tenant_id = 0 WHERE tenant_id = <t>`——账号/余额/历史全留、并入主站继续用。
5. **数据留存**(决策①·推荐归档):订阅/消费/收益台账等**不删**,归档在已删租户下,可审计/可恢复。

**修补现有软删漏洞**(无论选哪个方案都要做,否则"删了还能访问"):
- `GetTenantByDomain` 增加 `tenants.status <> 'deleted'` 过滤(兜底:即便有残留域名也不解析);
- `HandleAdminListAgents` 默认隐藏 `deleted`(或标「已删除」灰条,加 `?include_deleted`);
- 买家/登录/充值路径补 status gate(deleted 租户拒绝下单/充值,与 /v1 一致)。

**前端**:行操作加**「删除」**(垃圾桶图标)+ 二次确认弹窗(中文警示:将归档该代理、回收子域名 `<x>`、其 N 名用户迁回主站;有未提现收益时先结清)。

## 三、硬删(方案 B,仅在决策①选"彻底抹除"时)

一个尽力而为的事务里,按 §现状 的全表清单逐表 `DELETE WHERE tenant_id=?` + 删 `tenant_domains` + 处理原生 `users`(删或 tenant_id=0)。**风险**:跨多 gormrepo 无单一事务、漏表留孤儿、销毁真实用户资产、不可逆——**不推荐对有真实用户/交易的代理用**。若选此项,建议仍先走结算闸门 + 保留期(软删 N 天后由后台任务硬删)。

## 四、待用户确认(落地前,均给推荐)

1. **删除语义**:归档软删(推荐,可恢复、留数据)/ 彻底硬删(不可逆、抹除)。→ 决定走 §二 还是 §三。
2. **终端用户去向**:迁回主站继续用(推荐,tenant_id=0)/ 一并停用(冻结登录)。
3. **未提现收益**:有余额先结清否则禁删(推荐)/ 直接删、收益作废。

（子域名侧默认:管理员输入任意 label、每代理一个主子域名、不改 level——如需"一代理多子域名"或"开子域名即升 L1"再说。）

## 五、落地顺序

1. 后端(子域名):`tenant` service `AddSubdomain` + admin 端点 + `agentOut.subdomain` + Host 缓存失效。
2. 后端(删除):`ArchiveAgent`(结算闸门 + status + 回收域名 + 用户迁移)+ `DELETE` 端点 + Host 解析/列表/买家路径 status 过滤。
3. 前端:编辑抽屉子域名块 + 行删除按钮 + 确认弹窗。W5 中文。
4. 门禁:`go build`/`go test`(tenant/mtwire/agent)、`tsgo -b`、`bun run build`;禁改锁定文件;显式 `git add`。部署后验证:开子域名后 `<label>.wedreamhub.com` 可解析到该租户;删除后站点 404/停用、用户迁主站、子域名释放、列表隐藏。
