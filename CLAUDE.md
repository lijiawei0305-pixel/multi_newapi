# CLAUDE.md — newapi628 多租户代理分销平台

> **根级索引（每会话载入）。** 本文件是项目"驾驶舱"，每个会话自动加载，必须保持精简。
> 规则：
> 1. 本文件**只放**项目定位、路由表、硬约束、工作纪律四类内容；任何细节一律下沉到 `doc/`。
> 2. 动手前先看下方「路由」表，**按需读取**对应子文档，不要凭记忆改代码。
> 3. 本文件是**棘轮**——硬约束与纪律只进不退；删除/放宽任何一条都必须经用户确认。
> 4. 复盘与踩坑记录见 `RETRO.md`；反复出现的坑应升级为下方「部署硬约束」或「工作纪律」。

---

## 项目定位

基于 **New API**（基线仓库 [`github.com/QuantumNous/new-api`](https://github.com/QuantumNous/new-api)，One API 衍生；Docker 镜像 `calciumion/new-api`）二次开发的**多租户代理分销平台**，对标 **TOKEN HUB v1.0**。

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
| **现状（唯一权威 · 先看这个）** | 查"什么已建成 / 已上线 / 真正剩余" | [`doc/tasks/STATUS.md`](doc/tasks/STATUS.md) | 全部 |
| 任务与进度（历史存档） | 早期里程碑 / Slice 笔记（**勿据以判断现状**，看 STATUS） | [`doc/tasks/progress.md`](doc/tasks/progress.md)、[`phase2.md`](doc/tasks/phase2.md) | — |
| 验收标准（三期 · 质量门） | 验收某期/某项 / 查 12 道通用技术门与阈值 / 定义完成(DoD) / 写演示判据 | [`doc/acceptance.md`](doc/acceptance.md) | proposal §17 §18 |
| 自动化开发起始 Prompt | 启动 Master-Worker 全自动开发 / 查质量门与部署规范 | [`doc/prompt.md`](doc/prompt.md) | 全流程 |
| API 契约（前后端对齐） | 前端对接 / 加改端点 / 查错误码注册表 / 对象字段 | [`doc/api-contract.md`](doc/api-contract.md) | proposal §10 §13 |
| UIUX 改造规格（对标 TOKEN HUB） | 还原界面 / 逐页规格 / 设计令牌 / tokenplan 页 / 侧栏 IA | [`doc/uiux.md`](doc/uiux.md) | proposal §9 |
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

**服务器现状**（已核实 · 2026-07-03 收敛为**单栈**）：
- `64.90.4.114` ｜ Debian 12 ｜ 宝塔面板（:8889）｜ Docker 29 + Compose v2
- **唯一 newapi = fork 栈 `newapi_test`**（`newapi_test-app` 本机构建 + `redis` + `mysql:8.2`，DB=`new-api-test`），监听 `127.0.0.1:3100`；含微信/支付宝真实 SDK（进程内 `realpay`）+ 全部多租户功能。
- **三域名全走 fork**：`api` / `www` / `tokendream`.wedreamhub.com 经宝塔 nginx 反代到 3100（`*.wedreamhub.com` 通配 → 3100；`api` 显式 config 已由 3000 改指 3100）。`/auth/` → 8180（mock 支付页，退役中）。
- **原 stock 栈 `newapi_YFNf`（原版 `calciumion/new-api`，DB `new-api`，:3000）已于 2026-07-03 删除**（空壳：0 渠道 / 0 token / 无 /v1 流量）。回滚料：DB 备份 `/root/stock-newapi-backup.sql`、nginx 备份 `…/api-443-to-origin.wedreamhub.com.conf.bak-before-consolidate`。
- fork 源码/构建：服务器 `/root/newapi-test/`（**非 git 副本**，rsync 自 Mac）+ `deploy/ops/deploy.sh`；compose `deploy/docker-compose.test.yml`（`docker compose -p newapi_test --env-file /root/newapi-test/.env -f …`）。

> 部署细节见 [`doc/tasks/00-infra.md`](doc/tasks/00-infra.md) 与 [`doc/deployment.md`](doc/deployment.md)。

---

## 部署硬约束（C1–C8，违反即错）

> 硬约束 = "违反即错"的红线，等价于编译错误。动手前必读；一旦违反必须回退重做。
> **本节为占位骨架**——请逐条把 `TODO` 替换为真实约束。素材来源：需求文档 §11 权限隔离、§12 部署、§13 风险，以及 `RETRO.md` 中复现的坑。
> 每条统一格式：**约束（一句话祈使）** ｜ 为什么（根因/事故） ｜ 正确做法。

| 编号 | 约束 | 为什么 | 正确做法 |
| --- | --- | --- | --- |
| **C1** | 任何密钥/凭据的**字面量**都不得写进受 git 跟踪的文件（含 compose 的 `${VAR:-默认}` 内联默认、生成物如 `repomix-output.xml`）；密钥只存服务器 `.env`(600)，仓库仅 `${VAR}` 引用且用 **`:?` fail-closed**（缺失即拒绝部署）。签名密钥与加密/HMAC 密钥须**分权**（`SESSION_SECRET`≠`CRYPTO_SECRET`）。 | `docker-compose.test.yml` 曾把 48 字符会话/加密根密钥内联成 git 字面量且从未轮换，服务器 `.env` 未覆盖 → 线上逐字节在用该公开密钥，任一仓库读者可离线伪造 `role:100` cookie 免密全站 root（RETRO 2026-07-16 · Critical）。 | 去内联默认改 `:?`；两把密钥分开；服务器 `.env` 用 `openssl rand -hex 32` 各生成随机值；新增/改 compose 或 `.env.example` 前 `git grep` 确认无真值明文。违反即回退重做。 |
| **C2** | 任何拿到 `*gin.Engine` 的自研路由装配点（`SetMtRouter` 等）新增 `/api/**` 路由，**必须挂在与 `apiRouter` 同基础链的 `/api` 基组**（`apiBase := engine.Group("/api")` + `.Use(RouteTag/gzip/BodyStorageCleanup/GlobalAPIRateLimit)`），**严禁直接 `engine.Group("/api/…")`**；公开回调补 `AnonymousRequestBodyLimit`、money/兑换/提现端点补 `CriticalRateLimit()`（对齐上游同类端点）。 | gin 的 `Group()` 创建时快照父链、`.Use()` 不按路径前缀继承：自研组直接挂 engine 即 apiRouter 的**兄弟组**，全站默认全局限流(360/180s)+关键限流(20/20min)+体积门对自研 `/api/**` **一条都不生效** → 兑换码可爆破入账、支付回调可 1GB body OOM、pending 订单可无限造（RETRO 三·「mt-router 旁路 /api 分组中间件」· Critical）。 | 新增自研 `/api` 路由前先确认挂在 `apiBase` 下；改完 `git grep 'router.Group("/api'` 应只剩 `apiBase := router.Group("/api")` 一处；公开/money 端点逐个核对已挂上游对等中间件。违反即回退重做。 |
| **C3** | 凡**代理可写、且最终可能到达 HTML/JS sink** 的自由文本站点配置字段（Footer/公告等），**必须在 `siteconfig.ValidatePatch` 逐字段列举校验**（长度上限 + 危险 HTML 黑名单，命中即拒），**且渲染端必须 `DOMPurify.sanitize` 后再 `dangerouslySetInnerHTML`**——两层都要。`ValidatePatch` 是**默认拒绝**：未列举=按不安全处理，绝不默认放行。 | `ValidatePatch` 逐字段白名单校验，`Footer` 漏进校验表 → `applyPatch` 原样落库 → 公开无鉴权 `GET /api/tenant/current` 下发 → `footer.tsx:208` 裸 `dangerouslySetInnerHTML` 无 DOMPurify → level≥1 代理填 `<img onerror>` 即在其名下所有终端用户页面执行、接管账号；payload 进 localStorage 服务端删库仍复现（RETRO 三·「代理自配页脚未净化」· Critical）。 | 新增此类字段时：① `ValidatePatch` 加该字段校验分支（对齐 `validateFooter`：`MaxFooterBytes` 长度门 + `footerDangerRe` 危险模式，返 `FOOTER_INVALID` 类错误码）；② 渲染端复用 `dompurify`（已内置 3.4.11，见 `html-content.tsx`）消毒。改完 `git grep dangerouslySetInnerHTML` 逐处确认已消毒或为静态常量。违反即回退重做。 |
| **C4** | 任何「终身/不可逆」的风控去重或限购键，其**权威**必须有**持久后备**（DB 台账，或至少挂持久卷 + 开 AOF 的 Redis），且必须同时提供**释放原语 + 带鉴权与审计的后台释放通道**——严禁「只增不删（`Incr/SetNX` 无 `Del`）＋ 只活在无持久卷的 Redis」。 | Trial 终身限购（`PurchaseDedupTTL=0` 永久键）此前双向都坏：① `KVCache` 无 `Del` 原语 → 下单即消耗、用户犹豫关单即永久占用、后台零手段（客服只能直连无密码/无卷/无审计 Redis 删键）；② redis 容器无 `volumes:`、限购在 DB 无后备台账 → `compose down` / 镜像重拉即全站限购一次性归零、历史买家可再薅、无日志无告警。关键不对称：余额丢 Redis 只丢 ≤5s（MySQL 后备），限购丢 Redis 丢全部（RETRO 2026-07-16 · Critical）。 | 补 `Del` 原语（三实现齐补）+ `PurchaseLimitAdmin` 后台释放契约 + AdminAuth 端点（挂 `apiBase`，`common.SysLog` 审计留痕）；redis 挂 named volume `:/data` + `--appendonly yes`。新增此类"不可逆键"时先问：权威有无持久后备？能否被审计地释放？两者缺一即回退重做。更强根治（消耗移到支付成功后 / DB 成权威后备）按需另起。 |
| **C5** | 任何**承载金额的订单类型**（RCG 充值 / SUB 套餐 / AGT 代理套餐 / 未来新增），落地时**必须同时接入全部对账保护层**——① 进 `runReconcileAll` 的主动查单对账（已付补激活 / 确认未付超时置终态 / 瞬时错误留待重试，绝不误杀已付单）② 卡单在 `/api/admin/reconcile/stuck` 可见 ③ 失败进 `alertReconcileHealth` 告警——**严禁只接「支付回调」这一条主链**。 | AGT（全站最贵 SKU，¥990–9990）落地时只接了「回调→`ActivatePaidAgentPlanOrder`」，从未进对账编排：主动查单/超时终态/卡单可见/告警**四层全缺**。真实失败场景——api_v3_key 轮换配错致 24h 回调全部验签失败——RCG/SUB 被对账循环持续查单救回并在卡单页可见、失败即告警，而 AGT 静默永停 pending、任何页面看不到、无告警，唯一发现途径是手工 `SELECT * FROM mt_agent_plan_orders`（RETRO 2026-07-16 · 结构性缺口）。 | 新增付费订单类型前先问三连：它进 `runReconcileAll` 了吗？`/stuck` 看得到吗？失败会告警吗？镜像已有 `sub_reconcile.go` 补一条 seam（`reconcileXxxFn`）+ 终态常量 + `listStuckXxx` + 折进 stuck/failed 计数与历史 detail；三缺一即回退补齐。同属「清单挑选式遗漏」家族（C2/C3/C6 的漏登记模式）。 |
| **C6** | 任何**上线制品**必须携带**可溯源且可重建**的版本身份，且**回滚兜底路径必须真实可达**——① 真实版本串（git 短 SHA + `-dirty` + 时间戳）烤进二进制（`common.Version`）与前端，`/api/status` 如实回报；② 部署清单（SHA/dirty/branch/tag/时间/部署人）烤进镜像（`/deploy-manifest.json`）；③ 实际部署的源码树归档留存（≥N 份），令无 `.git` 也可重建；④ 严禁推荐服务器上不存在的路径（如 `--git`→`git checkout` 于非 git 副本）作为回滚兜底，回滚深度须＞1（镜像 `:prev` 秒级 + 源码归档多版本）。 | `VERSION` 是 0 字节且已跟踪 → `Dockerfile:39` 的 `-X '…Version=$(cat VERSION)'` 注入空串、连源码默认 `v0.0.0` 都被覆盖 → 线上 `/api/status version=""`；`deploy.sh` 打包 Mac 脏工作树（非任何 git ref）、`git tag` 却打在 HEAD 上 → tag 指向的代码从未上线；服务器用 `--exclude='./.git'` 上传 → 无 `.git`，而 `rollback.sh --git` 恰好 `git checkout` 且在镜像回滚失败时**主动推荐**它 → 兜底必败；`:latest`→`:prev` 每次覆盖 → 回滚深度恒为 1。三条溯源路径同断：出计费错账时无法回答「跑的是哪份代码」、也无法重建或安全回退（RETRO 2026-07-16 · #12 · 结构性缺口）。 | 版本串在 `deploy.sh` 由 git 计算、部署时盖进服务器 `VERSION`+`deploy-manifest.json`（**解包后、构建前**）；`Dockerfile` 三处 `$(cat VERSION)` 加空串兜底（回落 `v0.0.0-unknown`）；源码 tar 归档到 `/root/deploy-archives/src-<ts>.tgz`（+ `.version`/`.manifest.json` sidecar，留 7 份）；`rollback.sh` 删除必败的 `--git`、改为 `--to <ts>`（归档重建）+ `--list`，保留 `:prev` 秒级；部署尾自检 `/api/status` 报告版本==盖入版本，不等即告警。新增任何「上线制品/部署路径」时先问三连：版本可溯源吗？能无 .git 重建吗？回滚兜底真能跑通吗？三缺一即回退重做。同属「清单挑选式遗漏」家族（对齐 C2/C3/C5）。 |
| **C7** | 任何 `internal/**` 生产包**必须能从唯一装配入口** `SetMtRouter → internal/mtwire`（`wire.go`）的 import 图可达（`go list -deps ./router/` 可验证）；**暂不接线的包严禁配备「看起来像正主」的 `doc.go` + 全绿测试**——要么在 `doc.go` 顶部**显式标注「未装配·休眠·勿挂错」**，要么**发现即删不留**（git history 即归档）。判死活只认 Go 工具链的 import 图（`go list -deps` / 0 非测试 importer + 无 `_ "…"` 空白 import），**绝不信 doc.go / 绿测试 / 「本轮使用」注释这类表象**；已装配的活模块反而应补 `doc.go`（对齐当前无 doc.go 的活模块 mtwire）。 | `internal/relay` / `identity` / `billing` 三整包 + `wallet` / `stats` 服务层（1652 行生产码，占 internal 5.8%）配了 1730 行**全绿测试** + `doc.go`（「详见 detailed-design」暗示正主），却 **0 装配点可达**；最大的活模块 mtwire 反而**无** doc.go → 死码比活码更「正规」，骗过两次：① 违禁词接入时差点把钩子挂到休眠的 `relay.Gateway`（RETRO 处置**只写规则、未删码** → 地雷仍在，且 identity/billing 同款却无任何 RETRO）；② 直接酿成生产缺陷 **P2-BRK-02**——未装配的 tokenplan 计费桶 `Meter` 永不执行 → `tokenplan_subscriptions.used_usd` 恒 0 死列 → 读侧被迫 JOIN 原生表绕过 → 绕过表达式复制到 2 包 → 一处读死列致告警谓词恒假、告警恒空。且 STATUS.md 把**活着**的 `internal/risk` 误判为死码「勿重造」、对**真死**的 relay/billing 只字未提，死活恰好反了（RETRO 2026-07-16 · Critical 结构性缺口）。 | 新增 `internal/**` 包时先问：它在 `go list -deps ./router/` 里吗？不在就要么标「休眠」、要么别建。发现「有 doc.go/绿测试但 0 importer」的包，**别只写 RETRO——当场删**（本次已删 relay/identity/billing 整包 + wallet/stats 死层、剪其死接口/耦合测试、纠正 STATUS.md 死活反转、给活模块 stats/wallet 的 doc.go 加实况说明）。跨分支 merge 后 push main 前，容器跑 `go build ./... && go vet ./... && go test ./internal/...` 兜住误删（本机无 Go，见 [[local-no-build-toolchain]]）。与 C2/C3/C5/C6 的「清单挑选式**遗漏**」互补——本条治「多余的死**登记**」。违反即回退重做。 |
| **C8** | 行级锁必须用 GORM v2 的 `clause.Locking`，且**统一走 `model.lockForUpdate(tx)` 助手**（内置 SQLite 方言跳过）；**严禁** GORM v1 的 `Set("gorm:query_option", "FOR UPDATE")` 写法；`model/lock_guard_test.go` 三层守卫（MySQL DryRun 断言真出 FOR UPDATE ／ 钉死「老写法被 v2 静默忽略」根因 ／ 全仓 `*.go` 字面量封禁）**不得删除或绕过**。 | 本仓 `gorm.io/gorm v1.25.2` 即 GORM v2——v2 已移除 `gorm:query_option` 并**静默忽略**（gorm 源码 0 处读取该 key，不报错不告警），全仓曾有 17 处「加锁读」实为普通快照读：REPEATABLE READ 下「读→算→写回绝对值」构成丢失更新——3 个并发请求各读 amount_used=0、各写回 40，终值 40 而非 120，套餐超用已交付却不计量、上限失效；同款空锁覆盖订阅计量×9/充值×6/兑换码/邀请划转，且 SQLite 上的全绿计量测试为空锁路径盖绿章（audit 2026-07-17 · 头号 Critical）。 | 新增「加锁读」一律 `lockForUpdate(tx)`（`model/lock.go`）；改锁相关代码后跑 `go test ./model/ -run 'LockForUpdate\|LegacyQueryOption'`；SQLite 无行锁语义（glebarez 会静默剥除 Locking 子句）——并发正确性回归必须上服务器 MySQL 栈；额度计数器根治（原子条件 UPDATE 消灭 read-modify-write）按需另起。违反即回退重做。 |

---

## 工作纪律（本项目硬性）

> 本项目的硬性协作纪律。违反不一定导致程序报错，但会导致返工或沟通成本。
> **以下 W1–W3 来自你在本次对话中的明确要求，已固化；W4 起为占位，请按需补充。**

- **W1 — 不明确就提问，不要猜测意图。** 任何存在歧义、需要技术决策或缺少前提的地方，必须先向用户提问确认，再动手；不臆测需求。
- **W2 — 维护 `RETRO.md` 复盘日志。** 踩坑/反复出现的困难按其维护规则记录（现象、根因、是否解决、解决/规避方案）；未解决标 `[未解决]`，已解决标 `[已解决]`。
- **W3 — 棘轮升级。** `RETRO.md` 中已固化为规则的经验，升级为本文件「部署硬约束」或本节纪律，并在 `RETRO.md` 标注"已升级为规则"及位置。
- **W4 — Mac 只调试、部署在服务器。** 本项目唯一环境是服务器 `64.90.4.114`（见上「服务器与部署」）；Mac 仅代码编辑/调试，构建/迁移/集成/部署/E2E 一律在服务器执行；私钥不入库。
- **W5 — 前端页面文字一律用中文。** 任何面向用户的界面文案（标签 / 按钮 / 提示 / 表头 / 菜单 / toast / 错误码展示文案等）必须是中文，不留英文。i18n 以 `zh.json` 为准；当 `zh.json` 被并行工作区占用不可改时，用 `t('English Key', { defaultValue: '中文' })` 兜底（即时渲染中文、不动锁定文件，日后补 locale 条目会透明覆盖）。新增/改动任何前端前，务必核对最终**显示**出来的是中文。
- **W6 — 浏览器/本地服务进程用完即关（用户警告 2026-07-06）。** Playwright 浏览器（`playwright-cli close` / `kill-all`）、无头 Chrome（`--headless` 截图/调试进程）、本地调试 HTTP 服务（如 `python3 -m http.server`）等，验证一结束**立即关闭**，并用 `ps` 复核零残留；严禁留后台常驻——闲置进程持续消耗 Mac 性能且毫无用处。
- **W7 —** TODO（按需补充）。

---

## 待办看板（用户介入 / 明日继续）

> 「精简」原则的临时例外（用户要求置顶可见）；完成即移除/下沉到 `doc/tasks/phase2.md`。细节见 phase2.md ③ 与 `doc/detailed-design.md`。

- ✅ **[已查清 · 无 bug] 微信/支付宝回调其实一直正常** —— 一度以为「付款成功但回调验签 `PAY_SIGN_INVALID`、套餐卡 `pending` 不激活」。**2026-07-03 用商户平台截图 + DB 交叉核实,坐实是误判**：商户中心仅 **3 笔真实付款**（全「买家已支付」）—— `SUB6169CC`（¥6.90 套餐→已激活 sub 18，已迁主站平台租户 4）、`RCGdjoko3sig…`（¥1 充值 user6）/`RCGdjokuo7yr…`（¥1 充值 user15）**均已 `credited` + 进 `top_ups`（账单历史可见）**，全链路走通。所谓「卡住」的 `SUBE6212E88`（创建于真实付款前 46 秒、**商户平台查无此付款**）及 DB 里 24 笔 pending（12 微信 +12 支付宝）**全是「下单没付」的废单**（`pending`＝未付款，非回调失败）。⇒ **微信/支付宝支付无需修,勿再追此「bug」**。`internal/payment/realpay/wxpay.go` 里那段临时诊断（只在测试栈服务器、未入库）**可撤** —— 它只在回调验签失败时触发,而回调从未失败过。
- 🔴 **[需你提供] 微信/支付宝商户凭据** —— 接真实支付的**唯一外部阻塞**（真实 SDK 已落地：主站进程内 `internal/payment/realpay` + `internal/mtwire/payment_inprocess.go`，凭据存 DB；充值/购买闭环已 E2E 通过）。需：微信 `mch_id`/`app_id`/`api_v3_key`/商户私钥 `apiclient_key.pem`/微信支付公钥+`pub_key_id`；支付宝 `app_id`/应用私钥/应用公钥证书/支付宝公钥证书/根证书。拿到后→后台「系统设置 → 支付 → 微信/支付宝 选项卡」填表单并启用（**单门**：配好即在用户充值页与套餐购买页对买家显示，无需改配置文件/环境变量）→沙箱小额验收（入账侧零改）。
- 🔴 **[需你后续 · 我以后改] gemini 换上游** —— gemini 渠道（测试栈 channel **id4**，type=24 Google Gemini，分组 `gemini`）上游不出请求 → new-api 跨组回退、报 `no available channel … under group default`。**已逐层验证：token 组=gemini、可用组校验含 gemini、渠道启用、路由 enabled、已配价——分组/调用都没错，纯上游渠道问题。** 换法：控制台「渠道管理」→ gemini 渠道 → 编辑 → 改 **base_url + key**（换成能分发 gemini 的上游）→ 保存（分组/路由/倍率/可选全不动）。换好后若仍回退 default，叫我加调试日志精确定位。
- ✅ **①+② 买家页端到端闭环（完成）** —— 购买 snake_case + 走 auth-service mock：购买→mock 支付页→确认→激活原生订阅→代理分润(¥23.8)→**/v1 走订阅桶**，全链路 E2E 过。
- ✅ **[已建成 · 非待办] 违禁词屏蔽** —— relay 转发前扫描用户输入（`agenthook.ScanUserInput`）+ 违规日志 + 管理员/代理词库 CRUD + 全站基础库 + 违规审阅 + 4 前端页，均已落地（表 `moderation_banned_words`/`moderation_content_violations`）。规格 `doc/detailed-design.md` §2.14。~~"明日新功能"~~ 系旧文档误记（2026-07-07 核实纠正）。
- ✅ **[已上线并验证 2026-07-17] 限流安全修复 + 8a 源站锁 CF-only + C1 密钥轮换**（版本 `019b915-dirty-20260717-191443`）—— ① money 端点（购买×2/充值/兑换/提现共 5 处）限流改**按认证用户计桶** `CriticalUserRateLimit`，免疫伪造 XFF 绕过；② 全局 `gin.TrustedPlatform=CF-Connecting-IP` 取真实客户端 IP（线上双验证：本机带头压过伪造 XFF ✅、Mac 经 CF 记真实 IP 179.255.144.58 ✅，非静默空操作）；③ **8a 源站侧完成**——两个手动 vhost（api-443/wildcard）注入 `nginx allow CF段+本机; deny all`（include `/www/server/nginx/conf/cloudflare-only.conf`，备份 `/root/nginx-8a-backup-20260717-192209`），三角验证经 CF 200 / 直连绕 CF 403 / 本机 200，**api 回调域名不受影响**；④ **C1 遗留补齐**——服务器 `.env` 补随机 `SESSION_SECRET`/`CRYPTO_SECRET`（各 `openssl rand -hex 32`、分权），旧泄露密钥作废（代价：全站登出一次，已用户确认）。
- 真正剩余（少）：7c-2 满额主动推送（可选，需渠道）｜ 8c 运维零头 ｜ **8a 仅剩 CF 面板侧**：源站锁 CF-only 已完成 ✅，仅需**你在 CF 面板把 SSL 模式设为 Full (strict)**（证书已是 CF Origin CA，源站已只认 CF 回源）｜ 真实支付（待你给商户凭据）｜ gemini 换上游。（7c 风控、6b 代理管理 UI 均已完成。）**完整现状见 [STATUS.md](doc/tasks/STATUS.md)。**
