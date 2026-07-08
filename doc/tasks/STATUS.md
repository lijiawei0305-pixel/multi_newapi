# 📍 现状 · 唯一权威（STATUS）

> **本文件是项目当前状态的唯一权威来源。** 盘自 ground truth：git（244 提交，6/28→7/7）+ 后端路由表（`router/mt-router.go`）+ 前端路由树 + 部署实况，**不是计划口吻**。
> **取代**：`doc/tasks/progress.md`、`doc/tasks/phase2.md`、`doc/tasks/00~13-*.md`（均降级为**历史存档**，勿再据以判断现状）。
> **需求权威**仍是 [`doc/proposal.md`](../proposal.md) v2.0；跨模块设计见 [`doc/detailed-design.md`](../detailed-design.md) 及各专题 doc。
> **最后核实**：2026-07-07（ground-truth 逐项核对；纠正了"违禁词/recharge_spread/整体进度"等多处旧文档误差）。

---

## 一句话

**已建成并上线的多租户代理分销平台。** 三域名 `api` / `www` / `tokendream`.wedreamhub.com 走 fork 栈 `newapi_test`（`127.0.0.1:3100`），live。proposal 的核心能力**几乎全部落地 + 有 UI**；真正剩余的很少（见 §三）。

## 部署实况

- 服务器 `64.90.4.114`｜宝塔 Docker｜**单栈 `newapi_test`**（app + redis + `mysql:8.2`，DB `new-api-test`），监听 `127.0.0.1:3100`。
- 三域名：CF（**Origin CA 证书**，到 2041）→ 宝塔 nginx → 3100。原 stock 栈 `newapi_YFNf`（:3000）已于 07-03 删除。
- 代码在 **git `main`**；服务器源码为 **rsync 非 git 副本**；部署 = 定向覆盖改动文件 + 容器重建（**非**整树 deploy.sh）。今日部署二进制含下述全部特性。
- `main` 当前**领先 `origin/main` 18 个提交**（bulb-orbit + 支付修复 + 今日风控/文档，未 push；服务器部署走 rsync 不依赖 push）。

---

## 一、已建成 & 上线（按能力域）

**多租户 & 域名**：Host 三态路由（主站 www / 代理子域 / 未开通提示）、`*.wedreamhub.com` 通配；代理自定义域名绑定 + DNS 验证 + **证书自动签发**（CertificateSeal 动效）；admin 跨租户域名管理（查看/强制解绑）。

**身份 & 鉴权**：new-api 原生用户体系（注册/登录/OAuth/OTP/找回）；casbin 角色；`access_token`（需 `Authorization` + `New-Api-User` **双 header**）；代理 owner 校验 `AgentOwnerAuth`。

**代理体系**：分级 **L0 普通 / L1 独立 / API**（`can_api`）；底价 `BottomPriceRatio`、折扣系数（全线批发）、分级分润（L0 佣金 XOR L1 加价）；子代理管理（设代理 / 一键开子域 / 删除归档软删 + 迁用户回主站 / 升档指标）；**代理套餐 P1–P5**（开通 + 到期降级）；代理自助全套 —— 套餐上架改价（保护线）、推广渠道、兑换码（原生 quota 预扣 + CAS 单赢家）、我的用户、用户组倍率（floor 校验）、违禁词库、工单、收益、提现、收款账户、装修（logo/site-config）、档位倍率；代理加盟落地页（复刻 /affiliate）。

**计费**：复用 new-api 原生 quota；**双桶**（订阅桶 subscription + 钱包桶 wallet）独立计量、不回退；分组倍率**单点解析**（`relay/helper/price.go`，含租户 `model_groups` 覆盖，预扣与结算共用、两桶一致）；高额预扣；消耗分润 `consume_commission`；套餐差价 `tokenplan_spread`。

**支付**：进程内 **realpay SDK**（微信公钥模式 / 支付宝，official 标识）、DB 凭据表单配置、改单门、Epay 路径防御；**卡单对账 ①–⑤**（定时 5min 扫 + 心跳 + 历史 UI + 支付概览 4 态）；微信/支付宝回调 `/notify`。

**tokenplan 套餐**：6 档；月度封顶计量（原子、并发不击穿）；购买 → 激活**原生订阅**；管理 CRUD；代理上架改价；**Trial 限购 live**（用户维；设备/实名维后端就绪、前端未上送指纹故休眠）。

**钱包 / 充值 / 提现**：充值 → 原生 quota（强幂等）；兑换码；邀请返现；提现闭环（申请 / 审核 / 驳回理由 / 收款账户 / 标记已打款）。

**财务 & 统计**：财务报表 v3（管理员 6 卡 + 净收入趋势 / 代理自助，双报表）；钱包 vs 套餐消耗台账区分；订阅监控（跨租户 + 满额分级**显示** warn/critical/exhausted）。

**风控**：租户状态校验；**原生速率限制已启用 live**（1000 成功/分/用户）；IP allowlist 原生（`token.AllowIps` 经 `auth.go` 强制 + keys 页 UI）；Trial 限购 live。（`internal/risk` 的 RPM/IP/并发为 fork 前 superseded 死代码，勿重造 —— 见 [[risk-hardening-native-supersedes]]。）

**违禁词屏蔽（6e — ✅ 已建成，非待办）**：`/v1` 转发前扫描用户输入（`agenthook.ScanUserInput`）；违规日志；管理员词库 CRUD + 全站基础库；代理自建词库；违规审阅；表 `moderation_banned_words` / `moderation_content_violations`；前端 4 页（moderation-words / moderation-violations / my-moderation / my-violations）。

**工单**：三端（用户 / 代理 / 管理员）支持工单，权限隔离。

**前端**：new-api 新版 **default 主题**（固化进 seed）+ 我们增量 ~40 页；全中文 i18n；东八区时间；`/docs` 文档页；页眉游乐园 / 代理加盟；bulb 粒子灯泡 logo。

**运维**：`deploy/ops/deploy.sh`（预检 / tag / 备份 / 上传 / 重建 / 健康轮询 / 失败回滚）；DB 备份；回滚镜像；对账定时（master-only, 5min）。

---

## 二、数据表（我们新增，46 张，节选）

`tenants` `tenant_users` `tenant_domains` `tenant_custom_domains` `tenant_site_configs` `tenant_token_plans` `tenant_groups` `tenant_billing_logs` `agent_profiles` `agent_wallets` `agent_earning_logs` `agent_withdrawals` `agent_promotion_channels` `agent_redemption_codes` `agent_promotion_attributions` `agent_plans` `token_plans` `tokenplan_subscriptions` `subscription_usage_logs` `mt_subscription_orders` `pending_subscription_orders` `mt_native_subscription_plans` `mt_agent_plan_orders` `mt_agent_memberships` `mt_recharge_credit_ledger` `mt_wallet_consume_log` `payment_orders` `reconcile_runs` `reconcile_heartbeat` `moderation_banned_words` `moderation_content_violations` `support_tickets` `support_ticket_messages` `model_groups` `user_balances` …

---

## 三、真正剩余 / 可选（很少）

**三期(P3 · 品牌营销+国际化续订,2026-07-08 对账,详见 [acceptance.md](../acceptance.md) Part 3)**：≈ **完成一半**——P3-DOM-01 域名/SSL ✅ 超规格(自助绑定+自动签发;剩证书到期监控)；P3-FE-01 营销化 🟡 大半(划线/角标/推荐已渲染+theme_preset 模板)；P3-I18N-01 多语言 🟡 大半(6 语言+切换 UI;剩覆盖审计)；**P3-RNW-01 续订自动化 ⬜ 未启动**(且微信/支付宝自动扣款需代扣签约资质,外部依赖)；**P3-CUR-01 多币种 ⬜ 未启动**。

- ~~7c-2 满额提醒(用户侧)~~ → **✅ 已落地（2026-07-07）**：`SubscriptionUsageBanner` 全局横幅——套餐用量 ≥80% 黄条 /≥100% 红条，可关闭（按 订阅×档位×计费周期 记忆，跨周期/升档自动重弹），CTA 跳 `/plans`；纯前端读原生订阅快照（spec `doc/specs/2026-07-07-subscription-usage-banner.md`，提交 e85afe5..447bed9，SDD 终审 Ready-to-merge）。**服务端主动推送（NoteUsage→AlertSink 邮件/webhook）仍未做、可选**。
- **8c 运维零头** —— 迁移版本化、监控告警渠道接线。
- **8a CF「Full (strict)」模式** —— 源站已具 Origin CA 证书；仅差在**你的 CF 面板**切模式。
- **Trial 设备/实名维** —— 需前端购买请求上送 `device_id`/`real_name_id`（用户维已挡住主要滥用）。
- **vhost 注释清理** —— 3 份配置注释过期（"自签"/`newapi_YFNf`/3000），无功能影响。

## 四、外部阻塞 —— 2026-07-07 均已由用户解除 ✅

- ~~微信/支付宝真实商户凭据~~ → **凭据已配**（后台表单）。真实支付可进行小额沙箱验收（尚未由我独立复核，需要可协助）。
- ~~gemini 渠道换上游~~ → **已换 base_url + key**。此前 "no channel under default" 应已消除（尚未由我独立复核，可发一笔测试确认）。

## 五、已明确不做

- **recharge_spread 充值差价分润** —— 代理收益已由 `consume_commission`（消耗分润）+ `tokenplan_spread`（套餐差价）+ 批发折扣系数 + 提现 覆盖；充值时不再单独抽差价。

---

## 六、文档地图

| 文档 | 角色 |
| --- | --- |
| **`STATUS.md`（本文）** | **现状唯一权威** |
| [`proposal.md`](../proposal.md) | 需求权威 v2.0 |
| [`detailed-design.md`](../detailed-design.md)、`billing.md`、`architecture.md` 等专题 doc | 模块/跨模块设计 |
| [存档] `progress.md`、`phase2.md`、`00~13-*.md` | 历史里程碑，**勿据以判断现状** |
