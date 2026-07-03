# 📋 Phase 2 计划 — 产品化（用户确认的 4 大目标）

> **承接**：Phase 1 已完成 14 模块逻辑层 + 4 个集成纵切（tenant/identity/wallet/tokenplan/relay 在测试栈 `tokendream.wedreamhub.com` 真实跑通）。
> **本阶段**：把"集成纵切的 stub/薄实现"替换为生产级，并补管理端 UI、计费硬化、正式上线。
> **方式不变**：Master-Worker（前/后端 Worker）+ 预上传 gate(`scripts/preflight.sh`) + tar 上传 + 后台构建 + playwright E2E + 测试栈隔离（不碰现网）。问题记 `RETRO.md`。

---

## 0. 待替换的 Phase 1 stub/薄实现（清单）
| 现状（Phase 1） | 目标（Phase 2） |
| --- | --- |
| `dev-login`（明文 Token 假登录） | 真实用户体系：注册/登录/会话（复用 new-api users + casbin 角色） |
| 固定模型价（`MODEL_PRICE_*` 占位） | 真实分模型定价 + 上游渠道池（多渠道/分组倍率） |
| 支付 stub（下单即激活/recharge 假订单） | 真实支付回调（微信/支付宝）→ 幂等入账/激活 |
| 薄 `cmd/server` + 薄 React（brand/wallet/plans/playground） | new-api 基座（含 users/channels/relay/新版前端） + 我们的增量层 |
| 自签证书（CF Full） | `*.wedreamhub.com` 通配 + CF Origin CA（Full strict） |
| seed 演示数据（demo/payg/tokendream） | 真实开站流程（管理员设代理→开站→配置） |

---

## 1. 目标①：正式 merge new-api 基座（基础，**先做**）

> **决策已定：A 全量 fork**（new-api 整库作基座 + 我们的 `internal/` 增量 wire 进去 + 前端切 new-api 新版前端）。在分支 `phase2-newapi-fork` 进行（main 保留 Slices 1–4）。
> **✅ Stage 0 完成（2026-06-28）**：仓库已变成 new-api fork —— new-api 源码作基座、我们 14 模块 `internal/` 并入同一模块（`github.com/QuantumNous/new-api/internal`，77 文件 rename、`go build ./internal/...` 绿）、`cmd/`+薄 web 移除、go.mod 采用 new-api、compose 改 new-api env。**已在测试栈构建+运行**：`/api/status` success、新版前端 200（origin + CF `https://tokendream.wedreamhub.com`）。提交 `fd17844`（2268 files）。
> **下一步（5a/6a 并行）**：5a 把多租户+身份 wire 进 new-api `router.SetRouter`；6a 在 new-api 新版前端加 tokenplan 套餐 CRUD UI。
- [ ] **5a 用户/会话/角色**：接 new-api users + session + casbin；替换 dev-login；保留多租户 `tenant_id` 维度与 Host 解析
- [ ] **5b 渠道池 + 模型价**：接 new-api channels/abilities + 模型价表；relay 走真实渠道分发（替换单一上游直连）；分组倍率接 pricing
- [ ] **5c 支付回调**：接 new-api 支付（或独立 auth-service）/pay/、/auth/ 回调；幂等入账 → wallet.Credit / tokenplan 激活
- [ ] **5d 前端基座**：决策后——切 new-api 新版前端（按 uiux 加多租户换肤+tokenplan 页）或续用我们的薄前端

## 2. 目标②：管理端 / 代理端 UI（**可并行**，后端端点已就绪）
> **✅ 5a/6a + 补6a 完成（2026-06-28）**：5a 多租户+tokenplan 路由 wire 进 new-api（`model.DB`+`UserAuth`/`AdminAuth`）；6a 在 new-api **新版前端(default 主题)** 加三页，全 E2E 通过（截图确认）：
> - **管理「套餐管理」**：`/token-plans`，CRUD 6 档（售价/原价/月限额/成本/保护线/排序/状态）。
> - **买家「套餐购买」**：`/plans`，6 套餐卡片（零售价/原价划线/折扣角标/推荐高亮/月限额）+「我的订阅」用量进度。
> - **管理「订阅监控」**：`/subscription-monitor`，订阅表+用量+满额预警分级(warn/critical/exhausted)。
> 后端配套：buyer/admin 端点 snake_case DTO（含 `id`）、`GET /api/admin/subscriptions`(按租户)、tokenplan `ListSubscriptionsByTenant`。
> **关键**：`theme.frontend=default` 已**固化进 mtwire seed**（首次初始化设置，持久化值已确认；new-api 默认 classic，我们页面在 default——见 RETRO）。
- [x] **6a tokenplan 套餐 CRUD UI**（管理员）—— 列表+新建+编辑+上下架
- [x] **6a+ 买家套餐购买页 + 管理订阅监控页** —— 卡片购买流（真实支付待目标③）+ 满额预警分级
- [x] **6b 子代理管理 UI** + 后端（设代理 普通/OEM/API、成本价/折扣/分润/等级）—— 见下「代理核心闭环」
- [x] **6c 提现审核 UI** + 后端（管理员通过/拒绝 + 代理端申请）—— 见下「代理核心闭环」

> **✅ 代理核心闭环完成（2026-06-28，测试栈 E2E 全过）**：决策=代理=User+Tenant 1:1。
> - **数据模型**：`tenants.owner_user_id` + 原生 `users.tenant_id`（幂等 raw ALTER，不改 new-api 源）+ agent 4 表 + `internal/agent/gormrepo`（替换 MemRepo，分润幂等 idem_key、提现条件UPDATE/CAS）。
> - **端点**：`POST/GET/PATCH /api/admin/agents`、`GET/POST /api/tenant/withdrawals`、`GET /api/tenant/earnings`、`GET /api/admin/withdrawals` + approve/reject；`AgentOwnerAuth`（直读 DB owner 校验）。
> - **分润落账**：`tokenplan_spread`（换掉 noop）+ `consume_commission`（挂原生 `PostConsumeQuota` 单点覆盖两桶、幂等 RequestId、旁路化）。
> - **UI**（web/default）：管理「子代理管理」`/agents` + 「提现审核」`/withdrawals`；代理「我的收益」`/agent-earnings`。
> - **E2E 实测**：seed `demoagent`/`demoagent123`=tokendream owner；购买 lite→demoagent 得 `tokenplan_spread ¥179.8`（幂等不双计）；申请提现¥100→可提现79.8/冻结100→admin 通过→冻结0（金额守恒）；三页浏览器渲染确认。
> - **遗留**：`consume_commission`/`recharge_spread` 未端到端实测（需子用户真实 /v1 调用 + recharge 口径未决）；推广渠道码归属、代理自助(用户组/兑换码/套餐上架 UI-04)、设代理 admin UI 的"建租户"完整流 待补。
- [x] **6d 代理自助分销（P1-UI-04，测试栈 E2E 全过）**：套餐上架改价(保护线)/推广渠道/兑换码(代理 quota 预扣+用户兑换单赢家)/我的用户/用户组倍率(floor 校验) 5 页 + 端点（新表 `agent_promotion_channels`/`agent_redemption_codes`/`tenant_groups`，全 `AgentOwnerAuth`+scopeByTenant）。遗留（2026-06-30 复核）：用户组倍率作用于 /v1 计费 ✅ 已接（2D 解析器 relay 生效）、注册经渠道码归属 ✅ 已接（AttributeRegistration hook）；**真缺**：recharge_spread 充值差价分润（缺 agent 成本价/加价率字段、口径未决）、代理装修配置 UI（siteconfig 后端在、缺端点+前端页→二期 P2-UI-01）
- [ ] **6e 违禁词屏蔽（Phase 2 新增功能）** —— relay hook 扫用户消息→提醒/拦截 + 违规日志；管理员词库 CRUD + 违规审阅。规格见 `doc/detailed-design.md` §2.14（含开放问题，实现前先与用户确认）

## 3. 目标③：计费硬化（**架构已定：复用 new-api 原生计费 + 桥接我们的套餐**）
> **✅ 大头已通（2026-06-28，mock 全程 E2E 验证）**：
> - **侦察定论**：new-api 原生已自带 quota/原生订阅/双资金来源(钱包桶+订阅桶)/**7a 高额预扣**/真实分模型定价(model_ratio)/多渠道池/流式。我们 `internal/` 双桶曾是 dead code。决策=复用原生、桥接我们的套餐。
> - **Track1 套餐桥接**：tokenplan 购买"支付成功"→ `ActivatePaidTokenplanOrder` 激活**原生 `UserSubscription`**（month_limit_usd×500000→额度上限，valid_days→期）；/v1 默认 `subscription_first` 自动按订阅桶计量；幂等。**实测**：购买 solo→原生订阅 amount_total=280000000($560)、active、30d ✓。我们表改名 `tokenplan_subscriptions`（避撞原生，见 RETRO）。
> - **Track2 充值/支付**：真实支付改为**主站进程内真实 SDK**（`internal/payment/realpay` + `internal/mtwire/payment_inprocess.go`，微信/支付宝凭据存 DB、后台「支付」选项卡表单填写并启用，已落地；早期独立 auth-service mock 已退役）+ `POST /api/tenant/wallet/recharge` + 内网 `POST /api/internal/order/paid`（共享密钥、强幂等、按 order_no 前缀 RCG/SUB 分发）→ 原生 `IncreaseUserQuota`。**实测**：充$1→确认→quota +500000、重复确认 Δ=0（强幂等）✓。充值 UI 进 web/default。
- [x] **7a 预扣**：复用原生 `BillingSession.preConsume`（转发前预扣、扣不动即拒）—— 零新增；流式按增量结算(`Reserve`)待补
- [x] **7d 支付基建(mock)**：auth-service 微信/支付宝下单+回调 + 充值→原生quota + tokenplan购买→激活原生订阅，**强幂等全通**
- 🟡 **7d′ 真实凭据（代码已就绪，待凭据+沙箱）**：真实 V3 SDK（`wechatpay-go`/`smartwalle/alipay`）已落地于主站**进程内** `internal/payment/realpay`（+ `internal/mtwire/payment_inprocess.go`，凭据存 DB、后台「系统设置 → 支付」选项卡表单填写并启用）；待补=沙箱→小额真单验收。**唯一阻塞=你提供微信/支付宝商户凭据**。（注：早期独立 auth-service 版实现在 `500L` 分支 提交 `493b830`，已被进程内版取代。）
- [ ] **7b 真实分模型定价**：原生 model_ratio 本就生效；待校准我们套餐桶与分组倍率/成本保护线口径
- [ ] 🟡 **7c 多档风控（核心已上线）**：✅ 真实 Redis RPM 限流接 relay（`RISK_DEFAULT_RPM`、超限 429、压测原子无超发，提交 cd1cf95）+ 租户状态校验；**待补**：并发/IP allowlist 完整接线、Trial 三维限购、满额分级告警
- [x] **遗留接线 ①②③④ 完成**：①购买响应 snake_case ✅ ②tokenplan 购买走 auth-service mock(全链路 E2E) ✅ ③代理差价/分润落账(`tokenplan_spread`+`consume_commission` 真实 /v1 E2E) ✅ ④ **RCG/SUB 'paid'卡单对账兜底** ✅：`ReconcileStuckPaid`(扫 RCG paid→重跑 OnPaid 幂等→credited)+`ReconcileStuckSubscriptions`(扫 SUB pending→查 auth-service `/auth/order/status`→已付补激活)；`StartReconcileLoop` 5min 定时扫(master-only)，线上日志确认在跑；TDD 全测，提交 35c5098/c6f8e39/bd25efa/69d2136

## 4. 目标④：正式上线（**最后**，灰度）
- [ ] **8a 域名/证书**：`*.wedreamhub.com` 通配 vhost + CF Origin CA 证书（Full strict）；主站 `www/admin/api` + 代理泛子域
- [ ] **8b 灰度切流**：测试栈验证 → 正式栈（独立于现网 `newapi_YFNf` 或择机替换）→ 小流量灰度
- [ ] **8c 运维**：备份/回滚脚本、迁移版本化、监控/告警、`docker compose` 一键启停

---

## 排期建议（依赖关系）
```
① new-api 基座(5a→5b→5c)  ─┬─→ ③ 计费硬化(7a-7d 依赖渠道池/支付)
                            └─→ ④ 正式上线(8a-8c 最后)
② 管理端 UI(6a-6d) ── 可与①并行（端点已就绪，先接现有薄前端或随①切基座）
```
**建议起点**：①-5a（用户/会话/角色）—— 它解开"真实登录"，是 ②③④ 的前提；或并行先做 ②-6a（套餐 CRUD UI，纯前端接已就绪端点，快速可见）。

## 决策点（开工前需定）
1. **new-api merge 方式**：A 全量 fork 作基座（复用最大、重构大、前端切 new-api 新版前端）｜ B 增量替换 stub（保留现架构、按需移植 new-api 能力）｜ C 混合（后端渐进并入 new-api 服务，前端先续用我们的薄前端按 uiux 建，择机再切）。
2. **正式栈关系**：新建独立正式栈，还是择机替换现网 `newapi_YFNf`（`api.wedreamhub.com`）。
3. **起步顺序**：先 ①-5a 打地基，还是并行先出 ②-6a 管理 UI。
