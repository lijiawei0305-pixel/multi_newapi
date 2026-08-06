# 支付整改验收清单（§10.10）

> 配套 `doc/payment-system-latency-audit-and-remediation.md`。  
> **真实小额支付必须由用户明确授权后执行**；本清单默认只做只读检查与代码验证。

## 1. 代码与自动化（本地 / CI）

```bash
# 后端
gofmt -w internal/payment internal/mtwire router/mt-router.go
go test ./internal/payment/... ./internal/mtwire ./router -count=1

# 前端
cd web/default
bun run typecheck
bunx oxlint src/features/wallet
bun run test -- src/features/wallet/lib/recharge-status.test.ts src/features/wallet/lib/recharge-behavior.test.ts
```

## 2. 现网只读（服务器，无真实支付）

登录：`ssh newapi628`（或 `ssh -i ~/.ssh/newapi628_ed25519 -p 5522 root@64.90.4.114`）

```bash
# 版本与 readiness
curl -sS http://127.0.0.1:3100/api/status | head -c 500

# 微信 API 出站（必须在服务器或 app 容器内；禁止用本机 198.18.x.x fake-ip 下结论）
dig api.mch.weixin.qq.com +short
# 对每个解析 IP 测 TCP/TLS 耗时（示例）
for ip in $(dig +short api.mch.weixin.qq.com | head -5); do
  echo "=== $ip ==="
  timeout 15 bash -c "time openssl s_client -connect ${ip}:443 -servername api.mch.weixin.qq.com </dev/null 2>/dev/null | head -3"
done

# 卡单只读
docker exec newapi_test-mysql-1 mysql -uroot -p"$MYSQL_ROOT_PASSWORD" new-api-test -e \
  "SELECT status, COUNT(*) c FROM payment_orders GROUP BY status;
   SELECT order_no,status,updated_at FROM payment_orders WHERE status='paid' LIMIT 20;"
```

记录：微信 DNS 结果、各 IP TLS 是否 <3s、paid 卡单数。

## 3. 真实小额支付时序表（需用户授权）

授权后用 ¥1 或最低档，记录：

```text
T0 用户点击
T1 Dialog 显示（目标 P95 < 100ms）
T2 后端收到创建请求
T3 本地订单创建（status=created, next_query_at≈+5s）
T4 开始请求微信
T5 微信返回 code_url
T6 前端收到创建响应
T7 QR 渲染完成

P0 用户完成支付
P1 本站收到回调
P2 DB credited（ledger 1 条 + top_up 1 条 + quota 一次）
P3 前端查到 credited
P4 页面显示新余额
P5 Dashboard 显示相同余额
```

完成后只读核对（**禁止手工 UPDATE 订单/余额**）：

- [ ] `payment_orders.status = credited`
- [ ] `mt_recharge_credit_ledger` 恰 1 行
- [ ] `top_ups` 恰 1 行（同 trade_no）
- [ ] `users.quota` 只增加一次
- [ ] `provider_transaction_id` 非空且唯一
- [ ] 重复回调不重复入账
- [ ] 无 paid 卡单残留

## 4. 本阶段已实现能力速查

| 能力 | 状态 |
| --- | --- |
| creating Dialog 立即打开 | 已实现 |
| credited 才完成前端流程 | 已实现 |
| status: provider_paid / credited / current_quota | 已实现 |
| idempotency_key + 意图校验 | 已实现（Phase D） |
| actual_paid_fen + 分比对 | 已实现 |
| provider_transaction_id 真实值（禁 query/reconcile） | 已实现（Phase D） |
| QueryResult 结构化查单入账 | 已实现（Phase D） |
| credited-before-ledger 竞态修复 | 已实现（Phase D） |
| PostgreSQL OnConflict 幂等台账 + outbox 同事务 | 已实现（Phase D） |
| outcome_unknown 返回 order_no + 前端复用 key | 已实现（Phase D） |
| 主备域 request-local（非永久粘滞） | 已实现（Phase D） |
| 支付 HTTP 拒绝 redirect + 强制 TLS 校验 | 已实现（Phase D） |
| APIError 脱敏 | 已实现（Phase D） |
| legacy cancelled → 可信已付恢复 | 已实现（Phase D） |
| 查单 claim/lease | 已实现（Phase D） |
| next_query_at 5s/30s/60s | 已实现 |
| 缓存失效 outbox | 已实现 |
| 回调脱离 GlobalAPIRateLimit | 已实现 |
| Epay/Stripe/Creem/Waffo 不 toast 付款成功 | 已实现 |
| 微信出站网络整改 | **proposed，未执行** |
| 真实签名 Prepay / ¥1 E2E | **未执行，待用户授权** |
| 生产部署 | **Phase D 门禁未 GO 前禁止** |

## 5. Phase D 结论（2026-08-06）

- 应用端资金正确性与恢复状态机已按 P0 清单收口；定向 `go test` / `-race` 通过。
- **不得**宣称 PAY-LAT-02 微信出口根因已关闭；真实 Prepay P95 **未测**。
- 完整独立 Review + preflight + 生产只读基线通过前：**NO-GO 部署**。

## 6. Phase E / F 进展 — 仍为 **NO-GO / 非 GO-TO-RC**

### Phase F 已落地（代码）

| 项 | 状态 |
| --- | --- |
| F1 ledger 整数 quota+fen 比较；decimal(20,8)；q<=0 报错 | **已修** |
| F1 Prepay 前 CAS create_state→prepay_inflight；fenced SetPayURL | **已修** |
| F1 USERPAYING 不关单；REFUND/UNKNOWN 隔离 | **已修** |
| F1 QueryPaidOK 禁止空绑定/空 currency | **已修**（支付宝 AuthorityVerified） |
| F1 SQLite 并发测 TempDir 隔离；-count=10 绿 | **已修** |
| F2 AutoCloseReplace feature flag **默认关** | **已修**（`PAY_AUTO_CLOSE_REPLACE`） |
| F2 CreateReplacement 完整事务 | **未完成**（骨架 + 明确不部分执行） |
| F3 版本化 migrate 包 | **骨架** `internal/payment/migrate`；未接 deploy/CI 真库 |
| F3 MySQL/PG 外部方言 CI | **opt-in skip 入口**；**未实际执行真库** |
| 前端 active_order_no 跟随 + 支付宝单次跳转 | **已修** |

### 诚实结论

- **禁止 GO-TO-RC**：CreateReplacement 未闭环；MySQL/PG 真库并发未跑。
- **禁止部署 / 生产 migration / 生产 CloseOrder / 真实 Prepay**。
- **PAY_AUTO_CLOSE_REPLACE 默认关闭**；compose 显式 `false`。
- **自动 close / replacement 延后到独立阶段**：本阶段半截 recovery 已移除；
  若进程收到 `PAY_AUTO_CLOSE_REPLACE=true` 则启动失败（`FEATURE_NOT_IMPLEMENTED`），
  绝不调用 `CloseOrder`。不得开启「只执行半截」的 feature flag。
- **PAY-LAT-02 出口根因仍未关闭**；不得宣称 Prepay P95 达标。
