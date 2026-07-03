# 对账记录(reconcile-history)设计 spec

- **日期**: 2026-07-02
- **归属**: 交接伙伴实现(与"代理分层"并行分工)
- **状态**: 设计已批准,待实现
- **前置**: 需先熟悉现有支付对账代码(`internal/mtwire/reconcile_loop.go`、`internal/payment/reconcile.go`、`internal/mtwire/sub_reconcile.go`)

> 本文自包含 —— 阅读者无需本设计对话的上下文。

## 1. 背景

支付卡单对账兜底(reconcile)已上线:每 5 分钟(仅 master 节点,`sync.Once`+`IsMasterNode`+`gopool.Go`+ ticker + `atomic.Bool` 防重入)扫卡单并幂等补单。当前架构有 **三条路径**(见 `reconcile_loop.go` 的 `runReconcileOnce`):

1. **RCG-paid**(`payment.Gateway.ReconcileStuckPaid`):卡在 `paid` 未 `credited`(入账进程在 OnPaid 完成与 CAS 之间崩溃的缺口)→ 重跑 `OnPaid`(强幂等,不双扣)→ CAS `paid→credited`。
2. **RCG-created**(`payment.Gateway.ReconcileStuckCreated`):卡在 `created`(平台回调从未成功送达)→ 用 `providerMgr.QueryOrder` **主动查平台** → 已付则 `CreditPaidOrder`(paidAmount=0 跳过金额比对)。带 `maxAge`(26h)/`limit`(200)。
3. **SUB**(`App.ReconcileStuckSubscriptions`):`pending` 套餐单查单补激活;外加 `activated && settled=false` 补驱动步骤③(分润/记录,M1 修复)。

**痛点**:
- 对账结果只进日志、**不持久化** —— 管理员在「支付对账」前端页看不到"它在跑、跑了啥"。
- **手动「立即对账」按钮(`HandleAdminRunReconcile`)只跑 ①③,漏了 ②**,与定时不一致(drift)。

## 2. 目标

管理员在「支付对账」页能:
1. **随时确认对账在正常运行**(即使一直没卡单)—— 心跳。
2. 看到"**真干了事**"的历史运行(补单/激活/失败)+ 所有手动触发记录。
3. 手动「立即对账」时跑 **全 3 条**、与定时一致。

## 3. 已确认的决策

- **覆盖 = 三条全覆盖 + 修手动 drift**:定时与手动统一走**一个入口**,都跑 ①②③;手动补上漏跑的 ②。
- **记录策略 = 心跳 + 实事记录**:
  - **心跳**:每轮定时都更新(最近对账时间/今日轮次/状态),顶部常显 → 解决"健康系统历史为空、看不出在跑"的困惑。
  - **历史列表**:只记"**有实事**"的运行(任一 `scanned>0` 或有补单/激活/失败)+ **所有手动触发**(手动总记)。避免每 5min 空跑刷屏。
- 小选择默认:①心跳用**单行表** ②心跳数据**并进 `/stuck` 响应**(省一次请求) ③历史行**可展开明细**。

## 4. 设计

### 4.1 数据模型
- `reconcile_runs`:`id · ran_at(index) · trigger varchar(8)(cron|manual) · summary varchar(255)(三类计数) · detail text(JSON:三类 order_no 明细)`。
- `reconcile_heartbeat` **单行表**:`id=1 · last_run_at · last_trigger · today_date · today_runs · last_stuck_count · last_failed_count`,每轮 upsert(`today_runs` 跨天重置)。
- 迁移:`internal/mtwire/wire.go` 的 `Migrate()` 追加 `migrateReconcileRuns` + `migrateReconcileHeartbeat`(仿现有 `migrateSubscriptionBridge`)。

### 4.2 编排(统一入口)
- 新增 `func (a *App) runReconcileAll(ctx, before time.Time, trigger string) (paid, created payment.ReconcileResult, sub ReconcileSubResult)`:依次跑
  - `RechargeGateway.ReconcileStuckPaid(ctx, before)`
  - `RechargeGateway.ReconcileStuckCreated(ctx, before, reconcileCreatedMaxAge, reconcileCreatedLimit, query)`(`query` 用 `providerMgr.QueryOrder`)
  - `ReconcileStuckSubscriptions(ctx, before)`
  - 聚合三结果;每轮更新心跳;`trigger=="manual" || 任一有实事` 时落一条 `reconcile_runs`(best-effort,写库失败仅放弃记录、不影响对账)。
- `runReconcileOnce`(cron)→ 改为调 `runReconcileAll(ctx, before, "cron")`,保留现有日志。
- `HandleAdminRunReconcile`(manual)→ 改为调 **同一个** `runReconcileAll(ctx, before, "manual")` → **自动补上漏跑的 ②**,修 drift。

### 4.3 Admin API(`/api/admin/reconcile`,`AdminAuth`)
- `GET /stuck`(现有,保留)→ 响应体加心跳字段(前端顶部心跳条用)。
- `POST /run`(改造)→ 走统一入口、跑全 3 条、记历史。
- **新增 `GET /history?limit=50`** → 倒序返回 `reconcile_runs`(带展开明细)。
- 路由注册见 `router/mt-router.go` 的 `adminReconcileGroup`(现有 `/stuck`、`/run`,加 `/history`)。

### 4.4 前端(`web/default/src/features/payment-reconcile/`,复用现有页)
- 顶部**心跳条**:`最近对账 X 前 · 今日 N 轮 · 状态:正常 / 有卡单 / 有失败`(黄/红提示)。
- 底部**「对账记录」表**:`时间 | 触发(定时/手动) | 摘要`;行**可展开**看明细(哪些 order_no 补了/激活了/失败了)。
- `api.ts` 加 `listHistory`;i18n en/zh。

### 4.5 起点:parked WIP 分支(参考,勿直接 merge)
分支 `wip/reconcile-history`(commit `e5b6c9b`)有一版半成品可**参考**:
- `internal/mtwire/reconcile_history.go`:`reconcileRunRow` + `migrateReconcileRuns` + `reconcileBoth`(**旧版:只编排 ①③ 两条,需扩到三条 + 加心跳**)+ `recordReconcileRun` + `listReconcileRuns`。
- `reconcile_loop.go` / `wire.go` 的接线改动 diff。
> ⚠️ 该分支基于**合并前的旧基线**,与现支付/对账代码重叠,**不能直接 merge**(会冲突)。当参考、在现基线上重做。

## 5. 测试(TDD,先写测试)
- `runReconcileAll` 三路径聚合正确(providerMgr 查单打桩)。
- 记录策略:空跑不记 / 有实事记 / 手动总记。
- 心跳 upsert(`today_runs` 跨天重置)。
- `GET /history` 倒序、limit 生效。
- `POST /run` 修 drift 后结果**含 ②(created)**。

## 6. 验证
- 服务器构建 + 部署测试栈(项目名 `newapi_test`、端口 3100;**构建在服务器**,`--env-file /root/newapi-test/.env`)。
- **Playwright E2E**:登录 admin → 开「支付对账」页 → 见心跳 → 点「立即对账」→ 结果入历史 →(可选:造一笔 created/paid 卡单 → 手动/等定时 → 验证补单入历史 + 心跳变化)。若 CF/Turnstile 挡登录,**临时关闭 → 测 → 测完开回**(关/开位置动前先跟负责人确认)。

## 7. 注意
- 与"代理分层"并行开发:文件重叠小,但 `router/mt-router.go` 两边都会改(不同路由组,冲突可控)。
- 心跳"状态"语义:`有失败`=最近一轮 `Failed>0`;`有卡单`=当前 `/stuck` 非空且早于阈值;否则 `正常`。
