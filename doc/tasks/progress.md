# 📋 总体进度看板 — New API 多租户代理分销平台

> **依据**：[proposal.md](../proposal.md) v2.0 ｜ [detailed-design.md](../detailed-design.md) ｜ 任务文件见本目录 `NN-<module>.md`
> **部署目标（已核实）**：服务器 `64.90.4.114`，宝塔 Docker 栈 `newapi_YFNf`（new-api + redis + mysql:8.2），域名 `wedreamhub.com`。
> **排期方针**：proposal §10.5 **方案 A** —— 核心多租户 MVP 先行，tokenplan 第二批，现实工期 ≈ 4.5–5 天。
> **图例**：`- [ ]` 未完成 ｜ `- [x]` 已完成 ｜ 🔴 阻塞 ｜ 🟡 进行中。每个模块的子任务在各自文件内勾选。

---

## 一、推荐构建顺序（按依赖分层）

> 同一 Wave 内可并行；后一 Wave 依赖前一 Wave 的接口。

- 🟡 **Wave 0 · 基建**：[00-infra](00-infra.md) — ✅ go module · platform(apperr/appctx) · 包骨架 · git；⏳ fork new-api · 迁移 · 镜像 · 宝塔网关
- 🟡 **Wave 1 · 基础层**：[01-tenant](01-tenant.md) ＋ [02-identity](02-identity.md) ＋ [04-pricing](04-pricing.md) — ✅ 接口+领域逻辑+单测（`go test -race` 绿，覆盖率 98/100/100%）；⏳ 迁移 · GORM repo · handler · 集成
- 🟡 **Wave 2 · 领域核心**：[03-agent](03-agent.md) ＋ [05-billing](05-billing.md) ＋ [06-wallet](06-wallet.md) — ✅ 接口+领域逻辑+单测（99/100/98%，含 `-race` 并发不透支与幂等）；⏳ 迁移 · GORM · handler · 集成
- 🟡 **Wave 3 · 入口（核心 MVP 收口）**：[11-relay](11-relay.md) ＋ [09-promotion](09-promotion.md) ＋ [10-siteconfig](10-siteconfig.md) ＋ [12-stats](12-stats.md) — ✅ 接口+领域逻辑+单测（100/100/98.9/100%）；⏳ Gin handler · 迁移 · GORM · E2E　← 核心 MVP 逻辑层已就绪
- 🟡 **Wave 4 · tokenplan 批次**：[08-payment](08-payment.md) ＋ [07-tokenplan](07-tokenplan.md) ＋ [13-risk](13-risk.md)（限购/满额）— ✅ 接口+领域逻辑+单测（payment 99.2 / tokenplan 95.5 / risk 97.3%，含 Meter 并发不击穿/激活幂等/回调幂等/限购单赢家）；⏳ 迁移·GORM·handler·E2E

---

## 二、模块整体进度

| 状态 | 模块 | 任务文件 | 依赖 | 备注 |
| --- | --- | --- | --- | --- |
| 🟡 | 🏗️ Infra & Foundation | [00-infra](00-infra.md) | — | go module/platform/骨架/git ✓；fork·迁移·镜像·网关 ⏳ |
| 🟡 | 🏢 Tenant 多租户基础 | [01-tenant](01-tenant.md) | infra | 接口+逻辑+单测 ✓ 98%；迁移·GORM·中间件 ⏳ |
| 🟡 | 🔐 Identity & Access | [02-identity](02-identity.md) | tenant | 接口+守卫+单测 ✓ 100%；迁移·GORM·中间件 ⏳ |
| 🟡 | 💱 Pricing & CostGuard | [04-pricing](04-pricing.md) | infra | 纯逻辑+单测 ✓ 100%；迁移·GORM·集成 ⏳ |
| 🟡 | 🧑‍💼 Agent 代理体系 | [03-agent](03-agent.md) | pricing | 接口+逻辑+单测 ✓ 99%；迁移·GORM·handler ⏳ |
| 🟡 | 🧮 Billing & Quota | [05-billing](05-billing.md) | pricing, agent | 双桶路由+计费+单测 ✓ 100%；迁移·txn·E2E ⏳ |
| 🟡 | 👛 Wallet & Recharge | [06-wallet](06-wallet.md) | pricing, agent, payment | 充值/兑换/WalletQuota+并发 ✓ 98%；迁移·GORM·handler ⏳ |
| 🟡 | 🎟️ TokenPlan 套餐 ★ | [07-tokenplan](07-tokenplan.md) | pricing, payment, risk, agent | 核心逻辑+计量+幂等+单测 ✓ 95.5%；迁移·GORM·handler·E2E ⏳ |
| 🟡 | 💳 Payment 支付回调 | [08-payment](08-payment.md) | — (被 wallet/tokenplan 注入) | 下单+回调幂等+分发+单测 ✓ 99.2%；真实SDK 进程内已落地(realpay)；真实凭据(后台表单)·迁移 ⏳ |
| 🟡 | 📣 Promotion 推广归属 | [09-promotion](09-promotion.md) | tenant | 渠道+归属+单测 ✓ 100%；迁移·GORM·handler ⏳ |
| 🟡 | 🎨 SiteConfig 装修 | [10-siteconfig](10-siteconfig.md) | tenant | 校验+装修+上传安全+单测 ✓ 99%；迁移·Blob·handler·前端 ⏳ |
| 🟡 | 🚦 RelayGateway 中继 | [11-relay](11-relay.md) | identity, billing, risk | 接口+编排+全mock单测 ✓ 100%；handler·UpstreamPool·E2E ⏳ |
| 🟡 | 📊 Stats 统计看板 | [12-stats](12-stats.md) | billing, tokenplan | 聚合+满额预警+隔离+单测 ✓ 100%；迁移·GORM·handler ⏳ |
| 🟡 | 🛡️ RiskControl 风控 | [13-risk](13-risk.md) | infra | 逻辑+单测 ✓ 97.3%；接入(07-07)：Trial限购 E2E✓、RPM/IP/并发**原生覆盖**、满额告警可选（详见 [phase2 §3 7c](phase2.md)） |

进度：🟢 **14/14 模块逻辑层完成**（Wave 0–4 全绿）—— 全部 `go test -race -cover` 通过，平均覆盖率 **~98.7%**（apperr/appctx/pricing/identity/billing/promotion/relay/stats 100%，payment 99.2，agent 99.2，siteconfig 98.9，tenant/wallet 98，risk 97.3，tokenplan 95.5）。
> ⏳ **集成层待办（统一收尾）**：① fork `QuantumNous/new-api` 合并基座；② 迁移 + GORM 真实 Repo + Redis；③ Gin handler + `cmd/main` 装配（含组装层适配器）；④ 部署独立测试栈 + E2E；⑤ 前端对接 `doc/api-contract.md`。完成后 🟡 → ✅。

> **本轮进展（2026-06-28，Wave 0–1 启动）**：Master-Worker 自动化已跑通首批 —— Wave 0 引导（Go module + `platform` 包 + 13 包骨架 + git）已提交 `7f1f7a2`；3 个 Worker 子 Agent 并行完成 tenant/identity/pricing 的接口+领域逻辑+单测（TDD，纯标准库，依赖倒置可独立单测）。Master 已独立复跑质量门（gofmt/build/vet/test-race）全绿后才更新本看板。
> **Wave 2 完成（2026-06-28）**：新增共享契约 `platform/quota`（QuotaSource/Router/Receipt）；3 个 Worker 并行完成 agent/billing/wallet（接口+领域逻辑+单测，含 `-race` 并发不透支/幂等/兑换码单赢家）。Master 已独立复跑质量门全绿。
> **组装层 TODO（记录，留待集成）**：`wallet.EarningEntry` 与 `agent.EarningEntry` 字段不同，`cmd/main` 需薄适配器映射（`Reference→agent.SourceID` 非空做幂等键）并处理 USD↔¥ 单位；billing 的 `WalletSourceFactory/SubscriptionSourceFactory` 在 main 注入 wallet/tokenplan 的桶实现。
> **Wave 3+4 完成（2026-06-28）**：relay/promotion/siteconfig/stats + payment/tokenplan/risk 全部逻辑层完成并验收。tokenplan 实现 month_limit 原子计量（500 goroutine 不击穿）、激活幂等、Trial 三维限购单赢家、回调幂等。**至此 14 个后端模块逻辑层全部跑绿。**
> **★ Slice 1 完成（2026-06-28）· 集成纵切打通**：Master 调度 后端 Worker(gin+GORM tenant)+前端 Worker(React/Semi 品牌页)，过**预上传 gate**(`scripts/preflight.sh`)→ tar 上传服务器 → 构建镜像 → `newapi_test` 栈(127.0.0.1:3100, DB `new-api-test`, 独立 redis, **不碰现网**) → 真实 MySQL→GORM→解析→`/api/tenant/current` → **playwright-cli 浏览器冒烟全过**（demo 租户品牌渲染 ✓、未知域名 404 站点未开通 ✓）。**Mac 代码→服务器构建→DB→端点→浏览器 全链路打通。** 提交 `637526a`。
> **★ Slice 2 完成（2026-06-28）· 身份与钱包 + 真实域名**：① 接入真实域名 **`https://tokendream.wedreamhub.com`**（CF 代理，源站宝塔 nginx vhost → 测试栈 3100，CF Full 用源站自签 443，详见 RETRO）；② 后端 Worker：identity 鉴权中间件（Bearer/Cookie→Principal）+ Wallet GORM（余额/兑换/充值-stub）+ dev-login + seed `tokendream` 租户/用户/钱包/兑换码；前端 Worker：hash 路由 + 钱包页（uiux §3.3）。③ **浏览器 E2E 全过（真实域名）**：TokenDream 品牌渲染 ✓、登录 ✓、余额 $110 ✓、兑换 WELCOME10→+10 ✓、再兑 `REDEEM_CODE_USED` ✓、未登录 401 ✓、充值 stub ✓、紫色主题换肤 ✓。提交 `637526a`/`d8fb903`。
> **★ Slice 3 完成（2026-06-28）· tokenplan 套餐**（核心增量）：后端 Worker：tokenplan GORM（4+1 表，原子 Meter/幂等激活）+ 9 端点（套餐列表/购买/我的套餐/管理员 CRUD/代理上架改价）+ seed 6 档套餐 + admin/agent 角色用户 + stub 支付（同步激活）+ Trial 限购（user 维）；前端 Worker：购买页（6 营销卡片，uiux §4.1）+ 我的套餐（进度条）。**浏览器 E2E 全过（真实域名）**：6 档卡片营销渲染 ✓（Trial ¥6.90/-99%/$80…Max ¥8999/$25000，Pro 推荐角标+描边）、购买 Mini 激活 ✓、我的套餐显示 trial+mini ✓、Trial 再购 `PURCHASE_LIMIT_EXCEEDED` ✓、user 调 admin 403 ✓。提交 `c9082f2`。
> **★★ Slice 4 完成（2026-06-28）· 中继计费 + 真实上游 —— 4 个纵切全部打通！**：后端 Worker：真实 UpstreamPool（转发 `codexapis.com/v1`，Key 仅存服务器 `.env`）+ `POST /v1/chat/completions` 接 **billing.QuotaRouter 双桶路由**（有 active 套餐→tokenplan 套餐桶 Meter / 否则 wallet 钱包桶，**独立计量不回退**）+ 计费日志落库 + `GET /api/tenant/billing-logs`；前端 Worker：Playground（模型/提示词/发送 + 用量 + 计费日志 + 双桶账号切换）。**E2E 全过（真实域名 + 真实模型 gpt-5.4-mini）**：套餐桶(demo@td)扣 $0.0039 used↑、钱包桶(payg)余额 50→49.996、`X-TD-Bucket` 头正确、浏览器 Playground 实时回复+用量 ✓。提交 `3473184`。
>
> **🎯 集成里程碑**：tenant→identity→wallet→tokenplan→relay 全链路在测试栈 `https://tokendream.wedreamhub.com` 上以真实 GORM/MySQL/Redis/上游 跑通；核心差异化（tokenplan 套餐桶 vs 钱包桶 独立计量）以真实模型验证。**Master-Worker + 预上传 gate + tar 上传 + 后台构建 + playwright E2E** 全流程成熟。
> **下一步（可选方向）**：① 把复用的 new-api 基座（用户体系/渠道池/真实模型价表/支付回调）正式 merge，替换各 stub；② 管理端/代理端 UI（套餐 CRUD、子代理、提现审核）；③ 预扣计费 + 多档风控 + 真实支付；④ 正式栈灰度（`*.wedreamhub.com` 通配 + Origin CA 证书）。问题持续记 `RETRO.md`。
>
> **Phase 2（已确认 4 大目标）详细计划见 [phase2.md](phase2.md)**：4 目标 → 可执行 slices（5a–8c）+ 依赖排期 + 待替换 stub 清单 + 开工前决策点。

---

## 三、第一阶段里程碑验收（核心 + tokenplan）

> 完整 33 项见 [proposal.md §18](../proposal.md)；以下为关键验收门。

**核心多租户 MVP**
- [ ] 用户注册/登录；管理员可将用户设为 普通/OEM/API 代理
- [ ] `*.wedreamhub.com` 二级域名正确路由到租户；未开通显示提示
- [ ] 代理可配站点基础信息（Logo/Favicon/Hero/标题/公告/客服/页脚）；主题色限色板
- [ ] 代理可配用户分组倍率，且**不能低于主站保护线**
- [ ] 代理可建推广渠道/兑换码；用户经链接/域名注册归属正确
- [ ] 终端用户可充值得 API 额度；充值差价计入代理可提现余额
- [ ] 终端用户可建 Token、调用 `/v1/*`、按租户扣费、写使用日志
- [ ] 消耗分润正确计入代理收益；代理可提现、管理员可审核
- [ ] 管理员看全局统计；代理只看本站；**跨租户访问被拦截**
- [ ] 微信 `/api/pay/wechat/notify`、支付宝 `/api/pay/alipay/notify` 正确入账（或管理员人工入账兜底）

**tokenplan 套餐**
- [ ] 管理员可 CRUD 六档套餐（Trial~Max，售价/原价/月限额/成本价/保护线）
- [ ] 代理可上架/退出 tokenplan，并在**保护线内**改零售价
- [ ] 用户可在 new-api 风格购买页购买套餐，生成 30 天订阅
- [ ] 套餐内调用按 month_limit 月度封顶；超额/到期拦截重购、**不回退钱包**
- [ ] 套餐计量**独立于钱包**；并发不击穿月限额
- [ ] tokenplan 收益（差价/分润）入代理可提现余额
- [ ] **Trial 限购防刷**生效；满额逼近预警可见

**交付物**
- [ ] 第一阶段接口文档 ｜ - [ ] 部署/回滚说明 ｜ - [ ] 演示账号与步骤

---

## 四、待确认事项（不阻塞起步，编码前定）

- [ ] #2 `x1` = 上游成本价 ×1.0、不叠分组倍率（当前默认：是）
- [ ] #4 Trial 限购口径 = 用户∪实名∪设备 各 1 次（当前默认）
- [ ] #5 套餐**退款**策略（按比例/全额/不可退）
- [ ] #6 套餐内是否允许**分模型差异计量**（一期默认统一 ×1.0）

---

## 五、约定

- 每个子任务的「✅ 测试标准」达成才勾选；**先写 `port.go` 接口与单测骨架（TDD）再实现**。
- 模块间只依赖接口（消费者定义接口），单测用 mock 依赖，确保可独立验证。
- 完成一个 Wave 后做一次跨模块联调（E2E）再进入下一 Wave。
