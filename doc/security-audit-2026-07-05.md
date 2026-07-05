# 安全与代码检查报告 — newapi628

- **日期**：2026-07-05
- **检查对象**：newapi628 多租户代理分销平台（fork 自 QuantumNous/new-api）
- **检查范围**：本 fork **自有代码**（`internal/` 20 包 43K 行、`router/mt-router.go`、前端 fork features）+ **线上数据库**（`new-api-test`）+ **服务器基础设施**（64.90.4.114）。**未**重审上游 new-api 原生代码（上游已经充分验证）。
- **方法**：4 路并行专项审计（租户隔离/资金正确性/支付回调/前端）+ 直接执行的 DB 完整性核对、SQL 注入扫描、密钥卫生、服务器端口暴露探测。**所有 Medium 及以上发现均已逐一读源码/查库二次核实。**

---

## 一、总体结论

**架构与资金核心扎实，未发现 Critical/High 级代码漏洞。** 本 fork 在多租户隔离、防重复入账、防超支、支付防伪造上做得**明显高于一般水平**：

- 幂等靠**数据库级 UNIQUE 约束**（不是仅靠应用层判断，从根上杜绝并发重复入账）；
- 余额扣减/提现全程 **条件 UPDATE / 状态 CAS**（无超支、无双花、无重复解冻）；
- 支付回调**先验签再结算、失败即 fail-closed**、金额以库内订单为准；
- 租户隔离**无越权 IDOR、不信任客户端传入的 tenant_id**；
- 金额一律 `decimal`（非 float），钱包台账与余额**逐分精确对平**。

**唯一需要立即处理的是 1 个服务器基础设施暴露风险（H1，与代码无关）**，其余为 3 个 Medium 代码/合规问题 + 若干 Low/加固项。

### 严重度分布

| 级别 | 数量 | 概述 |
| --- | --- | --- |
| 🔴 High | 1 | 宝塔 host MySQL(:3306) 暴露公网（**fork 自有数据不受影响**） |
| 🟠 Medium | 5 | 套餐分润可能永久漏记；停用代理仍可访问控制台；充值页全英文(违反 W5)；重置密码静默失败；面板/FTP 端口暴露 |
| 🟡 Low | 10 | 见第四节（多为幂等时序加固、admin 跨租户口径、W5 零星漏译等） |
| ⚪ 加固/信息 | 若干 | 废单清理、冗余索引、运维遗留等 |

---

## ★ 修复进度（2026-07-05 更新）

**已在服务器直接修复并验证生效：**
- **H1（2026-07-05 更正：实为误报，撤销）** ❌ 原判「3306/MySQL 暴露公网」**不成立**。真实网络边界是**雨云云厂商防火墙（白名单模式）**，只放行 `22/8889/443/80/5522`，**3306/888(phpMyAdmin)/21(FTP) 早已在云层被拦、从未暴露公网**。误判根因：仅从服务器 OS 层判断（iptables `policy accept` + mysqld 绑 `*:3306`），且外部 nc 探测被网络代理干扰（全端口假阳性），**未能看到云厂商防火墙这一外层**。→ H1 撤销；我加的 iptables 3306 DROP 为**冗余但无害**（云层已拦），留作纵深防御即可。**真正残留仅 :8889 宝塔面板**（安全入口已开）——可选把云防火墙里 8889 的源地址限到固定 IP；另 :22 无服务监听，可删该白名单条目。
- **L7** ✅ api vhost 加 `location ^~ /api/internal/ { return 404; }`；`nginx -t` 通过 + reload；实测 `/api/internal/*`→404、`/api/status`→200。

**已改代码 + 本地验证（`go build`、`go test`（含 `-race`）、前端 `typecheck` 全绿）—— 待部署：**
- **M1** ✅ 套餐分润移出 `if created` 门控（幂等，重试补记）；新增回归测试 `TestActivateEarningRetryAfterFailure`（旧代码必挂）+ 修正测试假实现的幂等建模。
- **M2** ✅ `TenantByOwner` 加 `status<>deleted/suspended` + `ORDER BY id ASC`；新增测试 `TestTenantByOwner_ExcludesDeletedAndSuspended`。
- **M3** ✅ 充值卡/二维码/hook 共 9 处文案补中文 `defaultValue`（不动锁定的 locale 文件）。
- **M4** ✅ 重置密码补 `else` + `catch` 失败提示。
- **L3** ✅ 订阅监控改按 `tenantFrom` 判隔离（修自定义域名越权列全租户）。
- **L4** ✅ 修正 mt-router 违规日志的误导注释（对齐「主站统一管控」实现）。
- **L8（部分）** ✅ `agent-plans-admin` Code / `forgot-password` Email 补中文。

> 代码修复**未部署**：遵「先不推、等同学一起合并部署」的既定安排，未单独覆盖服务器（避免又被 rsync 回退 / 与锁定的支付 WIP 冲突）。一句话即部署，或并入统一的 git main 部署。

**需拍板 / 暂缓（附原因）：**
- **M5**（面板/FTP 暴露）— ⚠️ 需固定管理 IP：贸然封 :8889 会把自己挡在宝塔面板外。给 IP（或说明是否用 FTP）再封。
- **L1**（AGT CAS 时序）— 暂缓：朴素「CAS 前置」反而引入「已 activated 却未开通」更坏失败态；正确修法需事务化重构，且当前幂等不可利用。
- **L2**（对账不覆盖 AGT）— 暂缓：需新增查单对账逻辑 + 集成测试；属「欠开通」非欺诈，建议独立跟进。
- **L5**（AGT 建代理原子性）— 暂缓：需跨接口事务重构；当前首次收益 upsert 自愈。
- **L6**（manual_adjustment 负值下限）— 暂缓：无实时写入方、且负值为设计允许；建管理员调账端点时一并做。
- **L9**（wxpay 诊断日志）/ **L8 其余**（system-settings 英文）— 暂缓：属本会话**锁定的支付 WIP / system-settings 文件**，不擅动。
- **L10**（未接线服务层）— 暂缓：无实时调用方。

---

## 二、优先处理清单（按建议顺序）

| # | 级别 | 问题 | 位置 | 一句话修法 |
| --- | --- | --- | --- | --- |
| **H1** | 🔴 | MySQL 3306 暴露公网（防火墙未拦） | 服务器 `mysqld.service` | 绑定 `127.0.0.1` 或防火墙仅放行本机 |
| **M1** | 🟠 | 套餐差价分润在偶发 DB 失败后**永久漏记** | `internal/tokenplan/subscription.go:139` | 分润 `AddEarning` 移出 `if created` 门控 |
| **M2** | 🟠 | 停用/删除的代理仍能访问全部代理控制台 | `internal/tenant/gormrepo/gormrepo.go:305` | `TenantByOwner` 加 `status<>deleted/suspended` 过滤 |
| **M3** | 🟠 | 微信/支付宝充值页对买家**全英文**（违反 W5） | `features/wallet/components/tenant-recharge-card.tsx` 等 3 文件 | 9 个 `t()` 加 `defaultValue:'中文'` |
| **M4** | 🟠 | 重置密码链接失效时**无任何提示**（像死按钮） | `features/auth/reset-password-confirm/index.tsx:66` | 补 `else` 分支 toast 报错 |
| **M5** | 🟠 | 宝塔面板(:8889)/FTP(:21)/:888 暴露公网 | 服务器防火墙 | 限来源 IP，FTP 不用则关 |
| L1–L10 | 🟡 | 见第四节 | — | — |

---

## 三、已核实**无问题**的部分（检查覆盖面）

> 这些是本次重点验证、确认**安全/正确**的关键路径，列出以证明覆盖度。

**资金正确性**
- 收益幂等：`agent_earning_logs.idem_key` **UNIQUE** + `ON CONFLICT DO NOTHING`，钱包仅首次插入时入账、台账+钱包同一事务 —— 结构上杜绝 recharge/consume/tokenplan/markup 的重复入账。
- 充值双保险：CAS `created→paid` **叠加** `mt_recharge_credit_ledger(order_no 主键)` 唯一台账，回调与对账并发也不会双记。
- 提现状态机：`创建` 条件 UPDATE `WHERE withdrawable_balance>=amount`（无超支、行锁串行化）；`approve→paid` CAS；拒绝仅解冻一次、mark-paid 仅扣冻结一次、`approved` 只能转 `paid` —— 无双花、无重复解冻。
- 钱包/兑换：`ChargeBalance` 条件 UPDATE 防超支；`RedeemCode` 单赢家 CAS；建码扣额+批量插入同一事务。
- L0/L1 分润按等级互斥，订阅桶消耗**两侧都不重复计**；倍率下限读写共享同一 floor，基数一致。
- 钱包台账 ↔ 收益逐分精确对平（tenant 2：`total_earned`=`SUM(earnings)`=`withdrawable`，漂移 `0.00000000`）。

**支付回调安全**
- 结算前**强制验签**：`handlePayNotify` 先 `providerVerifyNotify`，`err!=nil||info==nil` 即 ack 失败、零结算；微信 `ParseNotifyRequest`（验签+AES-GCM 解密）、支付宝 `DecodeNotification`（内部 `VerifySign`）。
- **未配置=fail-closed**：无凭据 → `realpay.New` 报错 → 验签返回错误 → ack 失败，无 fail-open。
- 金额与库内订单交叉核对（`ErrAmountMismatch`）；入账的 user/tenant/amount 取自**库内订单**，回调体只贡献交易号，无法改写。
- 对账循环向渠道 `QueryOrder` **重新验证**后才入账，master-only + `sync.Once` + 原子重入标志，与实时回调不会双记。
- 商户私钥/APIv3 key 从 DB option 读取、注入 SDK、按指纹缓存，**从不落日志**。

**租户隔离与鉴权**
- **无跨租户 IDOR**：ticket/moderation/redemption/promotion/withdrawal 的读写全部 `WHERE ...=? AND tenant_id/user_id=?`，越权收敛为 `NotFound`。
- **不信任客户端 tenant_id**：全 `internal/mtwire` 无 handler 把请求体/query 的 `tenant_id/owner` 当权威；仅作 admin/agent 列表的可选筛选（且 agent 侧仍受 `agentTenantID` 约束）。
- `InternalSecretAuth` 用 `subtle.ConstantTimeCompare` + 空密钥默认拒绝；`RequireAgentLevel` 路由门禁 + handler 内 `ensureAgentLevel` 双保险、每请求读实时等级（降级即时生效）。

**代码与密钥卫生**
- **无 SQL 注入**：动态片段（`granularity`）走 `switch` 白名单，列名/表名/索引 DDL 均为开发者字面量，请求值全走 GORM `?` 占位符。
- **无入库密钥**：`.env`/`*.pem`/`*.key`/私钥均 gitignore，源码无硬编码凭据。
- **无 XSS**：`features/` 下无 `dangerouslySetInnerHTML`/`innerHTML`/`eval`；唯一富文本渲染器经 `DOMPurify.sanitize()`；违禁词/用户文本以纯 JSX 文本渲染（React 自动转义）。
- 前端鉴权门控 fail-closed（`agent-context` 出错默认关闭）、菜单隐藏与路由守卫无口径漂移；`target=_blank` 均配 `rel=noopener`；充值返现卡金额求和逻辑正确（两个独立台账相加，非重复计）。

**数据库完整性**（线上直查）
- 金额列全 `decimal`（无 float 漂移）；`tenants.slug`/`tenant_domains.domain`/`promotion.code`/`redemption(tenant,code)` 均 UNIQUE（防子域/域名/邀请码/兑换码冲突劫持）。
- **零**孤儿租户引用、**零**负余额、**零**残留删除租户(tokendream/tenant 1)引用（上次删除干净）。
- 无卡单：`payment_orders` 无 `pending` 滞留（均为 credited/cancelled/failed 终态）。

**服务器**
- 应用仅绑 `127.0.0.1:3100`（nft 显式 drop 外部访问）；**fork 的 Docker MySQL 未发布端口**（真实数据不暴露）；Redis 未 host 暴露；磁盘 27%；近期日志无 panic。

---

## 四、问题详述与修复方案

### 🔴 H1 — 宝塔 host MySQL(:3306) 暴露公网

**证据**：`mysqld`（`/system.slice/mysqld.service`，`/www/server/mysql`，socket `/tmp/mysql.sock`）监听 `*:3306`（全网卡）；防火墙 INPUT 链 `policy accept`、仅显式放行 5522，netfilter 与宝塔防火墙**均无** 3306 的 DROP 规则（`nft ... | grep -c 3306` = 0）。→ **3306 网络可从公网抵达**。

**影响**：这是**宝塔面板自带的 host MySQL**，**fork 应用并不使用它**（fork 用的是 Docker 容器内 MySQL，`PortBindings={}` 未发布端口，真实租户/支付/用户数据**不在此列、不受影响**）。但一个对公网开放的数据库服务本身即高危攻击面：协议层 CVE、口令爆破、若存在 `%` 远程用户或弱口令则可被直接接管，进而作为跳板。root 需要口令（`Access denied ... using password: NO`），但无法从内部确认是否存在远程用户/口令强度。

**修法**（任选其一，建议都做）：
1. **绑定本机**：宝塔 MySQL 配置 `bind-address = 127.0.0.1`（它只需本机被宝塔面板访问），重启 MySQL。因为 fork 用 Docker MySQL，此举**对业务零影响**。
2. **防火墙拦截**：宝塔面板 → 安全 → 系统防火墙，3306 改为仅放行 `127.0.0.1`；或 `nft add rule inet filter INPUT tcp dport 3306 ip saddr != 127.0.0.1 drop`。
3. **核实授权**：登录后 `SELECT user,host FROM mysql.user;` 删除任何 `host='%'`/非本机用户；确认 root 强口令。

### 🟠 M1 — 套餐差价分润在偶发 DB 失败后永久漏记

**位置**：`internal/tokenplan/subscription.go:135-153`（`ActivateFromPayment`）
**问题**：`ActivateFromOrder`（建订阅行，事务 A）与 `AddEarning`（记代理差价，事务 B）**分属两个事务**，且分润被 `if created` 门控：

```go
created, err := s.subs.ActivateFromOrder(ctx, sub)   // 事务A：提交订阅行
if created {
    if spread := tokenplanSpread(...); spread > 0 {
        if err := s.earnings.AddEarning(...); err != nil { return nil, err } // 事务B：可能失败
    }
}
```

**失败场景**：事务 A 提交后、事务 B（AddEarning）因瞬时 DB 错误/崩溃失败 → 订单 `settled=false`，对账 `ReconcileStuckSubscriptions` 重新驱动 → 再次进入 `ActivateFromPayment`，但此时订阅行已存在，`ActivateFromOrder` 返回 `created=false` → **AddEarning 被永久跳过**，随后 `subscription_bridge.go:338` 置 `settled=true` 永久关闭订单，**代理差价分润再也不会补记**。对账"重复调用也只入账一次"的设计，无法挽回"失败过一次"的收益。

**修法**：把 `AddEarning` 移出 `if created` 门控——它本身按 `(tenant, tokenplan_spread, orderID)` 幂等（`idem_key` UNIQUE），每次重试都调用是安全的、最终会成功：

```go
created, err := s.subs.ActivateFromOrder(ctx, sub)
if err != nil { return nil, err }
_ = created
if spread := tokenplanSpread(pp.RetailPrice, pp.AgentCostPrice); spread > 0 {
    if err := s.earnings.AddEarning(ctx, EarningEntry{ /* ... */ }); err != nil {
        return nil, err // settled 保持 false，等对账重驱动
    }
}
```

### 🟠 M2 — 停用/删除的代理仍能访问全部代理控制台

**位置**：`internal/tenant/gormrepo/gormrepo.go:305-312`（`TenantByOwner`）
**问题**：鉴权路径 `AgentOwnerAuthByUser`（`agent.go:403`）与前端门控 `callerOwnedTenant` 都靠 `TenantByOwner` 反查租户，而它 `Take(&row, "owner_user_id = ?", ...)` **无状态过滤、无排序**。租户"删除"是软删（`status='deleted'`，非 gorm `DeletedAt`），`HandleAdminDeleteAgent` **不清空** `owner_user_id`；停用是 `status='suspended'`。对照：购买链路的 `agentTenantByOwner`（`agent_plan_bridge.go:273`）**显式**过滤 `status<>'deleted'` 并 `ORDER BY id ASC`——安全关键的鉴权路径反而没有。

**影响**：管理员停用一个违规代理（租户→`suspended`）但保留其用户账号后，该用户**仍能通过鉴权**、继续操作全部代理自助端点（改用户层级/组倍率、建兑换码、发提现、站点装修、自定义域名、违禁词等）。管理员的停用/删除对"控制台撤权"**形同虚设**。（仅限其**自己**的租户，非跨租户，故 Medium；且提现仍需管理员审核兜底。）

**修法**：`TenantByOwner` 对齐 `agentTenantByOwner`——

```go
err := r.db.WithContext(ctx).
    Where("owner_user_id = ? AND status <> ? AND status <> ?",
        ownerUserID, string(tenant.StatusDeleted), string(tenant.StatusSuspended)).
    Order("id ASC").Limit(1).Take(&row).Error
```

并（可选）在 `HandleAdminDeleteAgent` 里清空 `owner_user_id`。

### 🟠 M3 — 微信/支付宝充值页对买家全英文（违反 W5）

**位置**：`features/wallet/components/tenant-recharge-card.tsx:125/133/165-168`、`features/wallet/hooks/use-tenant-recharge.ts:128`、`features/wallet/components/dialogs/recharge-qr-dialog.tsx:58/61/65/83/96`
**问题**：新落地的进程内微信/支付宝充值流程，全部 9 个文案 `t('...')` 的 key **在 `zh.json` 与 `en.json` 中都不存在**（已实测 `grep` 命中数=0），且无 `defaultValue`。`fallbackLng:'en'` 下 i18next 回落为**直接显示英文 key** —— 买家在**真实付款**页面看到的是端到端英文，违反 W5「前端一律中文」。

**修法**：这 3 个文件**不在锁定集**（锁定前端仅 `system-settings/*` + locale 文件），可直接给每个 `t()` 补中文 `defaultValue`（即时渲染中文、不动锁定的 `zh.json`）：

```tsx
t('Recharge (WeChat / Alipay)', { defaultValue: '充值（微信 / 支付宝）' })
t('Amount (USD), minimum ${{amount}}', { defaultValue: '金额（美元），最低 ${{amount}}', amount })
t('Scan to pay with WeChat', { defaultValue: '请使用微信扫码支付' })
// …其余同理
```

日后 `zh.json` 解锁再补正式条目（会透明覆盖 `defaultValue`）。

### 🟠 M4 — 重置密码链接失效时无任何提示

**位置**：`features/auth/reset-password-confirm/index.tsx:66-83`
**问题**：`api.post('/api/user/reset', {...}, { skipBusinessError: true })` 关闭了全局 axios 失败 toast，代码却**只有 `if (res?.data?.success)` 没有 `else`**。token 失效/过期时，`finally` 里 `setLoading(false)`、按钮复位，用户得到**零反馈**，像点了个死按钮。（其余流程都有显式失败分支或依赖未跳过的拦截器，此处是唯一例外。）

**修法**：补 else 分支，或去掉 `skipBusinessError`：

```tsx
if (res?.data?.success) { /* ... */ }
else { toast.error(res?.data?.message || t('Reset failed, link may be invalid or expired',
        { defaultValue: '重置失败，链接可能已失效或过期' })) }
```

### 🟠 M5 — 宝塔面板/FTP/888 端口暴露公网

**证据**：`ss` 显示 `0.0.0.0:21`(FTP)、`0.0.0.0:888`、`0.0.0.0:8889`(宝塔面板) 监听，防火墙 INPUT `policy accept` 未拦。
**影响**：FTP 明文传输且是爆破目标；宝塔面板对公网暴露，若口令弱/有 CVE 则等于整机失守。
**修法**：宝塔面板 → 安全，把 8889/888/21 改为**仅放行你的固定 IP**（或走 SSH 隧道/VPN 访问面板）；FTP 若不用直接停用。

---

## 四·补 · 🟡 Low / 加固项

| # | 级别 | 问题 | 位置 | 修法 |
| --- | --- | --- | --- | --- |
| **L1** | 🟡 | 代理套餐激活先做副作用再 CAS（与 RCG/SUB 范式不一致；当前因幂等 upsert **不可利用**） | `internal/mtwire/agent_plan_bridge.go:144` 早于 `:150` | 先 `pending→activated` CAS，胜出者才 `provisionAgentFromOrder` |
| **L2** | 🟡 | 对账循环**不覆盖 AGT**（代理套餐）订单：AGT 回调丢失则买家未开通（欠开通，非欺诈） | `runReconcileAll` | 把 AGT 纳入对账重驱动 |
| **L3** | 🟡 | `HandleAdminListSubscriptions` 把**代理自定义域名**当主站 → 返回全租户（当前仅全局 admin 可触发） | `internal/mtwire/http.go:575` | 按解析出的 tenant 判定，而非 `IsMainSiteHost(Host)` |
| **L4** | 🟡 | `HandleAdminListViolations` 恒返全租户，与路由注释「按 Host 隔离」矛盾 | `internal/mtwire/moderation_http.go:198` | 代理域按 tenant 收敛，或改正注释 |
| **L5** | 🟡 | AGT 新建代理非原子：建租户后崩溃、重试走已存在分支会跳过 `EnsureWallet`（首次收益 upsert 自愈） | `agent_plan_bridge.go:168-240` | 建代理链路包一个事务 |
| **L6** | 🟡 | `manual_adjustment` 允许负收益且无余额下限（**当前无实时写入方**，仅潜在） | `internal/agent/model.go:119` | 暴露 admin 调账端点前，`AppendEarning` 加 `>=0` 下限 |
| **L7** | 🟡 | `api.` nginx vhost 缺 `/api/internal/ {return 404;}`（应用有 InternalSecret 兜底，仅纵深防御缺口） | 宝塔 nginx `api-443-*.conf` | 镜像 wildcard vhost 加同款 404 块 |
| **L8** | 🟡 | 另有 ~7 处零星硬编码英文（`agent-plans-admin`、`performance-section` Goroutines、`forgot-password` Email 等） | 见第五节 | 补 `defaultValue:'中文'` |
| **L9** | 🟡 | 微信验签失败时的临时诊断日志（**不泄密**，仅公开请求头；CLAUDE.md 已挂待撤） | `internal/payment/realpay/wxpay.go:108-115` | 按既有待办撤除 |
| **L10** | 🟡 | 未接线的 `wallet.NewService`/`billing.NewService` 若将来接线有幂等/跨事务缺口（**当前无调用方**） | `internal/wallet/service.go:63` 等 | 接线前补 `SourceID` + 共享事务 |

**信息/加固（非缺陷）**
- `mt_subscription_orders` 有 5 条 `pending` 废单（「下单没付」，无害）→ 可加 TTL 定时清理保持整洁。
- `top_ups` 同时有 `trade_no` UNIQUE 与冗余 `idx_top_ups_trade_no`（浪费写入，可删其一）。
- `header-navigation-section.tsx:172/184` 的 `defaultValue` 是英文（当前被 zh.json 覆盖，无害，但偏离 W5 兜底范式）。
- `DiscountRatio` 无上限、`HandleWalletRecharge` 无金额上限——均**管理员侧**设置且下游数学 `clamp≥0`，不产生负分润，仅记录。

---

## 五、运维遗留（跨切面，务必知悉）

**Mac ↔ 服务器 支付代码分叉**：据项目记忆，正确的支付代码（微信**公钥模式**）已入 git `main`（commit `199211b`），但**服务器上运行的可能仍是被回退成「平台证书模式」的旧微信代码**（会 404 `RESOURCE_NOT_EXISTS`）。本报告审计的是 **Mac 工作区源码（=正确版本）**，故"支付回调健全"的结论针对的是**正确代码**；上线前请确认**部署的二进制与 git main 一致**（按记忆：统一从 git main 部署，勿再各自 rsync 过期副本），否则线上微信支付仍会 404。此为已知运维项，非本次新发现的代码缺陷。

---

## 六、检查项一览（覆盖清单）

- [x] 租户隔离 / 跨租户 IDOR / 客户端 tenant_id 信任 —— **无越权**
- [x] 鉴权门禁（owner/level/admin/internal-secret）—— 健全（M2 撤权缺口除外）
- [x] 资金：重复入账 / 超支 / 双花 / 幂等 / decimal —— **DB 级防护**（M1 分润时序除外）
- [x] 支付回调：验签 / 防重放 / 金额篡改 / 订单归属 / 对账竞态 —— **健全**
- [x] SQL 注入 —— **无**
- [x] XSS / 前端密钥 / 开放重定向 —— **无**
- [x] 前端鉴权门控 / 错误处理 / 金额展示 —— 健全（M4 静默失败除外）
- [x] W5 中文合规 —— **M3 充值页全英文 + L8 零星漏译**
- [x] 数据库完整性（对平/孤儿/负值/卡单/删除残留）—— **优秀**
- [x] 服务器端口暴露 / 防火墙 / 密钥卫生 / 磁盘 —— **H1/M5 端口暴露待处理**，其余健全

> 生成方式：4 路专项审计（opus×3 + sonnet×1）+ 直接库/服务器核查；Medium 及以上均已读源码二次确认。
