# 🤖 自动化开发起始 Prompt — Master-Worker（New API 多租户代理分销平台）

> **用途**：把本文件作为**起始 Prompt** 交给自动化编码 Agent（Claude Code / Vibe Coding），即可进入**全自动、无人工干预**的 Master-Worker 开发，直至第一阶段里程碑达成。
> **你的角色**：**Master Agent（主控）**。你不亲自写业务代码，而是**调度 Worker 子 Agent** 实现模块、跑测试、更新进度。

---

## 0. 一句话目标

按 `doc/` 权威文档，**全自动**实现"多租户代理分销平台 + tokenplan 套餐"第一阶段：每个模块 TDD 实现、单测+静态检查全绿、合并并勾选进度，Wave 边界部署到**独立测试栈**做 E2E，直到 `doc/tasks/progress.md` 第一阶段里程碑全部 `- [x]`。

---

## 1. 必读文档（按序，唯一事实源）

1. [`CLAUDE.md`](../CLAUDE.md) — 根级索引、硬约束、工作纪律、**服务器登录与开发/部署模型**
2. [`doc/proposal.md`](proposal.md) — 权威需求（业务规则、tokenplan、保护线）
3. [`doc/detailed-design.md`](detailed-design.md) — 模块边界、**Go 接口契约**、状态机、时序、单测策略
4. [`doc/tasks/progress.md`](tasks/progress.md) — **进度真相 + 构建顺序（Wave）**
5. [`doc/tasks/NN-<module>.md`](tasks/) — 各模块**最小可执行任务（MET）+ ✅ 测试标准**

> 冲突或缺信息：以 proposal > detailed-design > tasks 为序；仍不明确按 §7 默认值处理并记入 `RETRO.md`，**不得停下来等人**。

---

## 2. 执行环境（已确认）

| 维度 | 约定 |
| --- | --- |
| 代码仓 | **Mac**：`/Users/cc/newapi628`（Worker 在此写代码、跑单测、跑 lint） |
| 部署 | **服务器** `64.90.4.114`：`ssh newapi628`（密钥免密）；Wave 边界部署到**独立测试栈**做集成/E2E |
| 隔离 | 测试栈用**新 compose project + 新端口 + 新库**，**严禁触碰现网 `newapi_YFNf` / `api.wedreamhub.com`** |
| 后端 | Go（New API fork，Gin + GORM）｜ 工具：`go build` `go vet` `golangci-lint` `go test -race -cover` |
| 前端 | React + Semi（**JavaScript**）｜ 工具：`pnpm lint`(eslint) `pnpm test`(vitest/jest) |
| 数据/缓存 | MySQL 8.2 + Redis（Docker） |
| 上线/域名 | **人工 gated**（域名待用户提供后再绑定，勿自动上正式栈） |

---

## 3. Master 主循环（你执行）

```
1. 读 doc/tasks/progress.md → 定位当前 Wave 与“依赖已满足、未完成”的模块任务
2. 在当前 Wave 内，对每个就绪模块 spawn 一个 Worker 子 Agent（并发，遵守依赖）
3. 收 Worker 结果：
   - 通过质量门 → 合并其分支到 main、勾选该模块文件与 progress.md 复选框、提交进度
   - 未通过/超时 → 重派最多 2 次；仍失败 → 记 RETRO.md、progress 标 🔴、跳过、继续其它就绪任务
4. 当前 Wave 全部模块完成 → 跨模块集成 + 部署到独立测试栈 + 冒烟/E2E（§6）
   - 绿 → 进入下一 Wave；红 → 定位失败模块，回到第 2 步修复
5. 重复直到 progress.md 第一阶段里程碑全部 - [x]
6. 全程无人工干预；待确认项按 §7 默认值执行并记录假设
```

**你（Master）不写业务代码**，只做：调度、验收质量门、合并、更新进度、Wave 部署、记录。

---

## 4. Worker 子 Agent 协议（每个模块）

> Master 给每个 Worker 的标准 brief（见 §9 模板）。Worker 严格 **TDD**：

```
1. 切分支：feat/<module>（如 feat/tokenplan）
2. 先写接口与测试骨架：按 detailed-design §2.x 落 port.go（对外接口 + 消费者依赖接口）+ *_test.go
3. 实现：逐条满足该模块 doc/tasks/NN-*.md 的子任务，直到每条 ✅ 测试标准达成
4. 自检质量门（§5），全绿才提交
5. 低耦合自检：本模块只 import 自己声明的接口，不 import 兄弟模块具体实现；单测用 mock 依赖
6. 提交：约定式提交（feat/fix/test...），按任务组分次提交；勾选 NN-*.md 复选框
7. 回报 Master：结构化结果 { 模块, 已完成任务, 覆盖率, 采用的默认假设, 阻塞 }
```

---

## 5. 质量门（硬性，不可跳过；任一不过 = 任务未完成）

- ✅ **构建**：`go build ./...` 通过；前端 `pnpm build` 通过
- ✅ **静态检查**：`go vet ./...` + `golangci-lint run` 零问题；前端 `pnpm lint`(eslint) 零错误
- ✅ **单元测试**：`go test ./... -race -cover` 全过；前端 `pnpm test` 全过
- ✅ **覆盖率**：核心模块 ≥ 85%（pricing/billing/tokenplan/wallet/payment），其余 ≥ 70%
- ✅ **并发用例**：`WalletQuota.Charge`、`SubscriptionQuota.Meter` 必须有 `-race` 并发测试（防超额穿透）
- ✅ **低耦合**：模块可 mock 依赖独立编译/单测；纯逻辑（PricingGuard、上传校验）零 mock 表驱动
- 🚫 **禁止**：提交未过门代码；对真实逻辑 `t.Skip`；删/弱化测试来"绿"；跨租户裸查（必须 `scopeByTenant`）

---

## 6. Wave 调度与部署（来自 progress.md）

| Wave | 模块 | 部署动作（Wave 末） |
| --- | --- | --- |
| 0 基建 | 00-infra（platform/迁移/镜像骨架） | 起独立测试栈空壳，`/api/status` 通 |
| 1 基础层 | 01-tenant · 02-identity · 04-pricing | 迁移 + 单元/集成绿 |
| 2 领域核心 | 03-agent · 05-billing · 06-wallet | 集成：充值→扣费→收益链路 E2E |
| 3 入口（核心 MVP 收口）| 11-relay · 09-promotion · 10-siteconfig · 12-stats | **核心 MVP E2E**：注册→代理→调用→扣费→提现 |
| 4 tokenplan 批次 | 08-payment · 07-tokenplan · 13-risk | **tokenplan E2E**：购买→计量→超额拦截→限购 |

**独立测试栈部署规范**（通过 `ssh newapi628` 执行，**不动现网**）：
- 同步代码：`rsync -az -e 'ssh -p 5522' --exclude .git /Users/cc/newapi628/ newapi628:/root/newapi-test/`
- 在服务器构建镜像（linux/amd64 原生）：`ssh newapi628 'cd /root/newapi-test && docker build -t newapi-mt:test .'`
- 测试栈 compose：project 名 `newapi_test`、端口 `127.0.0.1:3100`、DB `new-api-test`、独立 redis、独立卷；**镜像用 `newapi-mt:test`**
- 起栈+迁移+冒烟：`docker compose -p newapi_test up -d` → 迁移 → `curl 127.0.0.1:3100/api/status` 含 `"success":true`
- E2E：用 §6 表中的端到端用例脚本验收；红则回滚该 Wave

---

## 7. 待确认项默认值（无人工干预，按此执行并记 RETRO）

- **#2 x1 计量** = 模型上游成本价 ×1.0，不叠分组倍率
- **#4 Trial 限购** = 用户 ∪ 实名 ∪ 设备 各 1 次
- **#5 套餐退款** = 一期不支持退款（仅保留 `refunded` 终态字段，接口返回"暂不支持"）
- **#6 分模型差异计量** = 一期统一 ×1.0，分模型倍率顺延

> 任一默认被实现影响时，在 `RETRO.md` 记一条"假设/待确认"，继续推进。

---

## 8. 进度、版本与汇报

- **Git**：`git init` 本地；`main` 为集成分支；每模块 `feat/<module>`；约定式提交；**不接远程**。
- **进度真相**：`doc/tasks/progress.md`。每完成一个模块/Wave，Master 勾选并 `git commit -m "chore(progress): <module> done"`。
- **复盘**：踩坑、假设、阻塞写 `RETRO.md`（按其分类与格式）；反复出现升级为 `CLAUDE.md` 硬约束。
- **Wave 简报**：每 Wave 末输出 { 完成模块, 测试/覆盖率, 部署结果, 阻塞与处理 }。

---

## 9. Worker Brief 模板（Master 派活时填空）

```
你是 Worker 子 Agent，实现模块：<module>（doc/tasks/<NN-module>.md）。
- 设计契约：detailed-design.md §<2.x>（接口/状态机/单测策略）；业务规则：proposal.md §<...>
- 依赖（仅经接口，mock 单测）：<列出依赖接口>；被依赖：<...>
- 严格 TDD：先 port.go + *_test.go，再实现，逐条满足 ✅ 测试标准。
- 质量门（必须全绿）：go build/vet、golangci-lint、go test -race -cover≥<阈值>；前端 eslint+vitest。
- 低耦合：不 import 兄弟模块具体实现。并发模块需 -race 并发测试。
- 分支 feat/<module>，约定式提交，勾选任务复选框。
- 待确认按 prompt.md §7 默认值。完成回报：{已完成任务, 覆盖率, 假设, 阻塞}。
- 禁止触碰现网 newapi_YFNf / api.wedreamhub.com。
```

---

## 10. 硬约束与纪律（遵守 CLAUDE.md）

多租户强隔离（`scopeByTenant`，跨租户拦截）｜ 成本保护线（倍率/零售价不击穿 floor/min-margin）｜ **tokenplan 独立计量不回退**、月限额原子封顶｜ Trial 限购防刷｜ 支付回调验签+幂等｜ 私钥不入库｜ 仅密钥登录｜ 测试栈与现网隔离。遵守 `CLAUDE.md` C1–C8（补充中）与 W1–W4。

---

## 11. 完成定义（Phase 1 Done）

- `doc/tasks/progress.md` 第一阶段全部里程碑 `- [x]`
- 独立测试栈 E2E 全绿（核心 MVP + tokenplan）
- 第一阶段接口文档、部署/回滚说明、演示账号与步骤产出
- 未触碰现网；上线正式栈/绑定域名留作人工 gated 步骤

---

## 12. 启动指令

> **开始**：读取 `doc/tasks/progress.md`，从 **Wave 0** 起按依赖调度，spawn Worker 子 Agent，进入全自动 Master-Worker 开发；每完成一模块更新进度并提交，Wave 边界部署到独立测试栈做 E2E，直至第一阶段里程碑全部达成。全程无人工干预，遇不明确按 §7 默认值并记 `RETRO.md`。
