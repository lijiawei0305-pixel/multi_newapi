# CLAUDE.md — newapi628 多租户代理分销平台

> **根级索引（每会话载入）。** 本文件是项目"驾驶舱"，每个会话自动加载，必须保持精简。
> 规则：
> 1. 本文件**只放**项目定位、路由表、硬约束、工作纪律四类内容；任何细节一律下沉到 `doc/`。
> 2. 动手前先看下方「路由」表，**按需读取**对应子文档，不要凭记忆改代码。
> 3. 本文件是**棘轮**——硬约束与纪律只进不退；删除/放宽任何一条都必须经用户确认。
> 4. 复盘与踩坑记录见 `RETRO.md`；反复出现的坑应升级为下方「部署硬约束」或「工作纪律」。

---

## 项目定位

基于 **New API**（One API 衍生）二次开发的**多租户代理分销平台**，对标 **TOKEN HUB v1.0**。

- **架构**：单套后端 + 多租户隔离 + 统一 API 网关。**不是**给每个代理部署一套独立 New API。
- **主站**：统一管控上游渠道、模型、支付、计费、风控、系统配置与管理员能力。
- **代理商**（普通 / OEM / API 三类）：在主站体系内获得分销、品牌定制、用户组倍率、兑换码、开放 API 等能力，管理自己的下级用户与收益。
- **终端用户**：经主站、代理域名或推广链接注册，归属到对应代理商名下。
- **栈**：Go 1.21+ ｜ Node.js 18+ ｜ MySQL 8.0+ ｜ Nginx 1.18+ ｜ Docker Compose 部署。
- **路线**：第一期 3 天全栈可演示 MVP → 第二期前端品牌化 / OEM / 自定义域名。

> 完整业务定义见 [`doc/proposal.md`](doc/proposal.md)（**权威需求文档 v2.0，完整替代版**，含 tokenplan 套餐、可行性分析、技术选型、二期预留）。
> `newapi-multitenant-development-plan.md` 为初版设计稿，已被 proposal 取代，仅作历史参考；`TOKEN HUB 文档.md` 为对标基准。

---

## 路由（动手前按需读）

> 改某模块前，先读它对应的文档；改完若踩坑，记入 `RETRO.md`，必要时升级为硬约束。
> 所有子文档位于 `doc/`，与本文件同级。

| 模块 | 何时读（动手前） | 文档 | 需求章节 |
| --- | --- | --- | --- |
| 架构 · 多租户识别 · 隔离 | 改 Host 识别 / 请求路由 / 网关分层 / 越权与数据隔离 | [`doc/architecture.md`](doc/architecture.md) | §2 §6.4 §11 |
| 数据模型 · 迁移 | 改表结构 / 加字段 / 写迁移 | [`doc/data-model.md`](doc/data-model.md) | §4 |
| 计费 · 倍率 · 收益 · 支付 | 改扣费 / 用户组倍率 / 充值差价 / 消耗分润 / 成本保护 / 提现 / 支付回调 | [`doc/billing.md`](doc/billing.md) | proposal §7 |
| tokenplan 套餐 | 改套餐定义 / 月度计量 / 购买 / 到期 / 代理上架改价 / 限购防刷 | [`doc/proposal.md`](doc/proposal.md) §8 | proposal §8 §2.4 |
| 详细设计（跨模块） | 写代码前看模块边界 / Go 接口契约 / 数据流时序 / 单测策略 | [`doc/detailed-design.md`](doc/detailed-design.md) | 全模块 |
| 任务与进度（开发跟踪） | 认领任务 / 看构建顺序 / 勾选进度 / 查模块 MET 与验收标准 | [`doc/tasks/progress.md`](doc/tasks/progress.md) | 14 模块 |
| 自动化开发起始 Prompt | 启动 Master-Worker 全自动开发 / 查质量门与部署规范 | [`doc/prompt.md`](doc/prompt.md) | 全流程 |
| 代理 · 租户管理 | 改代理类型 / 等级 / 钱包 / 推广 / 兑换码 / 站点配置 | [`doc/agent-tenant.md`](doc/agent-tenant.md) | §3 §10.1 §10.2 |
| 域名 · SSL | 改 wildcard / 自定义域名绑定 / HTTPS 证书 | [`doc/domains-ssl.md`](doc/domains-ssl.md) | §6 |
| 渠道 · 模型 · 中继转发 | 改上游渠道 / 模型映射 / 统一网关 / 限流风控 / 调用入口 | [`doc/relay-channels.md`](doc/relay-channels.md) | §2 §10.7 |
| 前端 · OEM 品牌装修 | 改主站 / 代理站前端 / 装修配置 / 上传安全 | [`doc/frontend.md`](doc/frontend.md) | §9 |
| 部署 · Nginx · 运维 | 改部署 / 反向代理 / 证书 / 备份 / 回调转发 | [`doc/deployment.md`](doc/deployment.md) | §12 |

---

## 服务器与部署（本项目唯一环境）

> **开发/部署模型**：Mac 端**只做代码编辑与调试**；**完整构建、迁移、部署、集成与 E2E 全部在服务器**进行。域名待用户提供后再绑定。

**登录**（已配置密钥，免密码）：
- 一键：`ssh newapi628`
- 等价：`ssh -i ~/.ssh/newapi628_ed25519 -p 5522 root@64.90.4.114`
- 私钥在 Mac `~/.ssh/newapi628_ed25519`（**严禁入库**）；服务器已改为仅密钥登录。

**服务器现状**（已核实）：
- `64.90.4.114` ｜ Debian 12 ｜ 宝塔面板（:8889）｜ Docker 29 + Compose v2
- new-api 现以宝塔 Docker 应用 `newapi_YFNf` 运行：`calciumion/new-api:latest` + `redis` + `mysql:8.2`（DB=`new-api`），监听 `127.0.0.1:3000`
- 域名 `wedreamhub.com`，`api.wedreamhub.com` 已（宝塔 nginx）反代到 3000；**无 auth-service（待建）**
- compose 路径：`/www/dk_project/dk_app/newapi/newapi_YFNf/`

> 部署细节见 [`doc/tasks/00-infra.md`](doc/tasks/00-infra.md) 与 [`doc/deployment.md`](doc/deployment.md)。

---

## 部署硬约束（C1–C8，违反即错）

> 硬约束 = "违反即错"的红线，等价于编译错误。动手前必读；一旦违反必须回退重做。
> **本节为占位骨架**——请逐条把 `TODO` 替换为真实约束。素材来源：需求文档 §11 权限隔离、§12 部署、§13 风险，以及 `RETRO.md` 中复现的坑。
> 每条统一格式：**约束（一句话祈使）** ｜ 为什么（根因/事故） ｜ 正确做法。

| 编号 | 约束 | 为什么 | 正确做法 |
| --- | --- | --- | --- |
| **C1** | TODO | TODO | TODO |
| **C2** | TODO | TODO | TODO |
| **C3** | TODO | TODO | TODO |
| **C4** | TODO | TODO | TODO |
| **C5** | TODO | TODO | TODO |
| **C6** | TODO | TODO | TODO |
| **C7** | TODO | TODO | TODO |
| **C8** | TODO | TODO | TODO |

---

## 工作纪律（本项目硬性）

> 本项目的硬性协作纪律。违反不一定导致程序报错，但会导致返工或沟通成本。
> **以下 W1–W3 来自你在本次对话中的明确要求，已固化；W4 起为占位，请按需补充。**

- **W1 — 不明确就提问，不要猜测意图。** 任何存在歧义、需要技术决策或缺少前提的地方，必须先向用户提问确认，再动手；不臆测需求。
- **W2 — 维护 `RETRO.md` 复盘日志。** 踩坑/反复出现的困难按其维护规则记录（现象、根因、是否解决、解决/规避方案）；未解决标 `[未解决]`，已解决标 `[已解决]`。
- **W3 — 棘轮升级。** `RETRO.md` 中已固化为规则的经验，升级为本文件「部署硬约束」或本节纪律，并在 `RETRO.md` 标注"已升级为规则"及位置。
- **W4 — Mac 只调试、部署在服务器。** 本项目唯一环境是服务器 `64.90.4.114`（见上「服务器与部署」）；Mac 仅代码编辑/调试，构建/迁移/集成/部署/E2E 一律在服务器执行；私钥不入库。
- **W5 —** TODO（例如：改 `doc/` 路由文档与代码同步更新）。
- **W6 —** TODO（按需补充）。
