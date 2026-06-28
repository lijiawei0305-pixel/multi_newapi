# 📋 总体进度看板 — New API 多租户代理分销平台

> **依据**：[proposal.md](../proposal.md) v2.0 ｜ [detailed-design.md](../detailed-design.md) ｜ 任务文件见本目录 `NN-<module>.md`
> **部署目标（已核实）**：服务器 `64.90.4.114`，宝塔 Docker 栈 `newapi_YFNf`（new-api + redis + mysql:8.2），域名 `wedreamhub.com`。
> **排期方针**：proposal §10.5 **方案 A** —— 核心多租户 MVP 先行，tokenplan 第二批，现实工期 ≈ 4.5–5 天。
> **图例**：`- [ ]` 未完成 ｜ `- [x]` 已完成 ｜ 🔴 阻塞 ｜ 🟡 进行中。每个模块的子任务在各自文件内勾选。

---

## 一、推荐构建顺序（按依赖分层）

> 同一 Wave 内可并行；后一 Wave 依赖前一 Wave 的接口。

- [ ] **Wave 0 · 基建**：[00-infra](00-infra.md)（platform 包 / 迁移 / 自定义镜像 / 宝塔网关域名）
- [ ] **Wave 1 · 基础层**：[01-tenant](01-tenant.md) ＋ [02-identity](02-identity.md) ＋ [04-pricing](04-pricing.md)
- [ ] **Wave 2 · 领域核心**：[03-agent](03-agent.md) ＋ [05-billing](05-billing.md) ＋ [06-wallet](06-wallet.md)
- [ ] **Wave 3 · 入口（核心 MVP 收口）**：[11-relay](11-relay.md) ＋ [09-promotion](09-promotion.md) ＋ [10-siteconfig](10-siteconfig.md) ＋ [12-stats](12-stats.md)　← 至此核心 MVP 可演示（方案 A 第一批 ≈3 天）
- [ ] **Wave 4 · tokenplan 批次**：[08-payment](08-payment.md) ＋ [07-tokenplan](07-tokenplan.md) ＋ [13-risk](13-risk.md)（限购/满额）　← 方案 A 第二批 ≈+1.5–2 天

---

## 二、模块整体进度

| 状态 | 模块 | 任务文件 | 依赖 | 备注 |
| --- | --- | --- | --- | --- |
| - [ ] | 🏗️ Infra & Foundation | [00-infra](00-infra.md) | — | fork new-api、迁移、镜像、网关 |
| - [ ] | 🏢 Tenant 多租户基础 | [01-tenant](01-tenant.md) | infra | 根基，Host→租户 |
| - [ ] | 🔐 Identity & Access | [02-identity](02-identity.md) | tenant | Token 绑租户、守卫 |
| - [ ] | 💱 Pricing & CostGuard | [04-pricing](04-pricing.md) | infra | 纯逻辑，保护线 |
| - [ ] | 🧑‍💼 Agent 代理体系 | [03-agent](03-agent.md) | pricing | 钱包/收益/提现 |
| - [ ] | 🧮 Billing & Quota | [05-billing](05-billing.md) | pricing, agent | 双桶路由中枢 |
| - [ ] | 👛 Wallet & Recharge | [06-wallet](06-wallet.md) | pricing, agent, payment | WalletQuota |
| - [ ] | 🎟️ TokenPlan 套餐 ★ | [07-tokenplan](07-tokenplan.md) | pricing, payment, risk, agent | 核心增量，SubscriptionQuota |
| - [ ] | 💳 Payment 支付回调 | [08-payment](08-payment.md) | — (被 wallet/tokenplan 注入) | auth-service 新增 |
| - [ ] | 📣 Promotion 推广归属 | [09-promotion](09-promotion.md) | tenant | 渠道/归属 |
| - [ ] | 🎨 SiteConfig 装修 | [10-siteconfig](10-siteconfig.md) | tenant | 一期数据层，二期前端 |
| - [ ] | 🚦 RelayGateway 中继 | [11-relay](11-relay.md) | identity, billing, risk | /v1/* 编排 |
| - [ ] | 📊 Stats 统计看板 | [12-stats](12-stats.md) | billing, tokenplan | 含满额预警 |
| - [ ] | 🛡️ RiskControl 风控 | [13-risk](13-risk.md) | infra | 限购/限流/告警 |

进度：**0 / 14 模块完成**

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
- [ ] 微信 `/pay/wxpay/notify`、支付宝 `/auth/alipay/notify` 正确入账（或管理员人工入账兜底）

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
