# 支付系统延迟审计、整改方案与最终 Review 手册

> - 审计日期：2026-08-06
> - 审计范围：default 前端钱包充值、官方微信/支付宝下单、支付回调、余额入账、缓存一致性、订单轮询与兜底对账
> - 暂不处理：当前已知的 `403 NO_AUTH` 商户权限问题
> - 操作边界：本次仅做代码审计、定向测试和现网只读核验，未发起真实支付，未修改生产配置或生产数据

## 1. 文档目的

本文档用于：

1. 记录本次支付链路审计已经确认的问题和证据。
2. 区分“二维码生成慢”和“支付后余额显示慢”两个独立问题。
3. 给出按优先级分阶段实施的整改方案。
4. 定义实施完成后的测试、验收和最终 Review 操作。
5. 防止后续实现者只优化 UI 文案、盲目增加超时，或根据通用文章重建一套与现有代码重复的支付系统。

## 2. 执行摘要

当前两个用户问题有不同根因：

### 2.1 二维码出现慢

二维码组件本身不是主要耗时。真实链路是：

```text
用户点击充值
  → 前端仅让按钮旋转
  → POST /api/tenant/wallet/recharge
  → 本地写 payment_orders
  → 同步调用微信 Native Prepay
  → 最多 3 × 8 秒重试
  → 回填 pay_url
  → 前端收到响应
  → 设置 qrState
  → 此时才打开 Dialog 并渲染二维码
```

因此，支付机构网络耗时全部暴露成“按钮长时间旋转”。前端没有 `creating` 状态，也没有先打开弹窗。

现网只读网络检查还确认：服务器解析到的三个微信支付公网 IP 中，两条连接或 TLS 超过 10 秒失败，只有一条约 2.8 秒成功。代码层增加同步重试可以提高部分成功率，但也会把单次用户等待放大到约 24.6 秒。

### 2.2 付款后余额显示慢

后端状态机定义：

```text
created   待支付
paid      支付机构已确认，本站正在入账
credited  余额与账单已完成
failed    失败
```

但当前状态接口把 `paid` 和 `credited` 都映射为 `paid:true`。前端一见 `paid:true` 就停止轮询、关闭二维码、显示成功，并且只刷新一次用户余额。

这形成明确竞态：

```text
订单刚进入 paid
  → 前端认为已经到账
  → 前端请求 /api/user/self
  → quota 事务可能尚未提交
  → 前端读到旧余额
  → 前端已经停止轮询
  → 页面长期保留旧余额
```

如果支付回调完全失达，后端目前最早约 5 分钟、最坏接近 10 分钟才主动查单；前端却在 3 分钟后停止轮询。这是另一条可直接解释“余额很久以后才变化”的路径。

### 2.3 数据库入账不是当前已证实的主要慢点

现网三笔可关联的历史成功订单中：

| 阶段 | 抽样结果 |
| --- | --- |
| 本地订单创建 → 额度台账写入 | 约 8.0s、13.4s、26.0s，包含用户扫码与付款时间 |
| 额度台账写入 → 订单 credited | 约 6–7ms |

当前没有证据表明 ledger、quota 和 top-up 数据库事务本身是主要耗时。真正缺失的是 `callback_received_at`、`provider_paid_at`、`credited_at` 以及分段日志，因此无法从历史数据还原“用户付款到本站收到回调”的准确耗时。

## 3. 审计基线与方法

### 3.1 代码基线

- 本地审计基线：`main@9c2e71a`
- 现网应用版本：`d9134cb-20260805-005036`
- 现网版本是本地 HEAD 的祖先；其后两次提交与本支付链路无关，因此本次读取的支付代码可以代表当前线上实现。

### 3.2 已执行的检查

1. 阅读根 `AGENTS.md` 与 `web/default/AGENTS.md`。
2. 审计 default 前端钱包、二维码弹窗、轮询、用户状态和账单历史。
3. 审计 Go 路由、Handler、Gateway、支付 SDK、回调、入账事务、缓存与对账任务。
4. 检查 MySQL 线上表结构、订单状态分布和历史时间戳。
5. 检查 app、MySQL、Redis 运行状态和当前版本。
6. 从服务器及 app 容器测试微信支付域名 DNS、TCP、TLS 与总耗时。
7. 运行定向 Go 测试：

```bash
go test ./internal/payment/... ./internal/mtwire \
  -run 'Test(RechargeStatus|RechargeQuotaSink|CreditPaidOrder|RetryTransient|WechatNotify)' \
  -count=1
```

定向测试通过。测试通过只说明当前行为被测试覆盖，不代表当前状态契约是正确的；例如现有测试恰好锁定了 `paid → paid:true` 这一错误语义。

## 4. 当前真实架构

### 4.1 下单链路

```text
TenantRechargeCard
  → useTenantRecharge.submit
  → POST /api/tenant/wallet/recharge
  → apiBase 基础中间件
  → TenantMiddleware
  → UserAuth
  → CriticalUserRateLimit
  → HandleWalletRecharge
  → RechargeGateway.CreateOrder
      → INSERT payment_orders(status=created)
      → providerManager / realpay SDK
      → 微信 Native Prepay
      → UPDATE payment_orders.pay_url
  → 返回 wxpay_qr
  → setQrState
  → RechargeQrDialog
  → QRCodeSVG
```

关键文件：

- `web/default/src/features/wallet/components/tenant-recharge-card.tsx`
- `web/default/src/features/wallet/hooks/use-tenant-recharge.ts`
- `web/default/src/features/wallet/api.ts`
- `internal/mtwire/recharge.go`
- `internal/payment/gateway.go`
- `internal/payment/realpay/wxpay.go`
- `internal/payment/realpay/retry.go`

### 4.2 回调与入账链路

```text
微信回调 /api/pay/wechat/notify
  → 公开路由，无 UserAuth
  → 通用 GlobalAPIRateLimit
  → AnonymousRequestBodyLimit
  → 微信验签与通知解密
  → 读取本地订单
  → created/failed → paid CAS
  → rechargeQuotaSink.OnPaid
      → INSERT mt_recharge_credit_ledger
      → users.quota 原子自增
      → INSERT top_ups
      → COMMIT
  → Redis 用户缓存失效
  → paid → credited CAS
  → HTTP 200 SUCCESS
```

关键文件：

- `router/mt-router.go`
- `internal/mtwire/payment_inprocess.go`
- `internal/payment/credit.go`
- `internal/mtwire/recharge.go`
- `internal/payment/gormrepo/gormrepo.go`
- `model/user_cache.go`

### 4.3 前端到账发现链路

```text
二维码显示
  → 每 3 秒 GET /api/tenant/wallet/recharge/status
  → 第一次查询也要等待 3 秒
  → paid:true
  → stopPolling
  → 关闭二维码
  → window.dispatchEvent(mt:tenant-recharge-paid)
  → Wallet 额外 GET /api/user/self
  → 只更新 Wallet 组件本地 user state
```

Dashboard 使用持久化的 Zustand `auth.user`，因此 Wallet 和 Dashboard 当前不是同一个余额事实源。

## 5. 已确认问题总表

| 编号 | 级别 | 问题 | 用户影响 | 是否已从代码确认 |
| --- | --- | --- | --- | --- |
| PAY-LAT-01 | P0 | 弹窗在下单完成后才打开 | 用户长时间只看到按钮旋转 | 是 |
| PAY-LAT-02 | P0 | 服务器到微信部分 IP 网络严重不稳定 | Prepay 可能数秒至超时 | 现网只读实测 |
| PAY-STA-01 | P0 | `paid` 与 `credited` 被前端合并 | 提前停止轮询，余额可能长期显示旧值 | 是 |
| PAY-REC-01 | P0 | 前端 3 分钟停止，后端兜底首次约 5–10 分钟 | 回调失达后用户必然先看到超时 | 是 |
| PAY-UI-01 | P1 | Wallet 与 Dashboard 使用不同余额状态 | 跨页仍显示旧余额 | 是 |
| PAY-CBK-01 | P1 | 回调继承通用 Redis/IP 限流 | Redis 故障或集中出口高峰可能阻断合法回调 | 架构风险已确认 |
| PAY-IDEM-01 | P1 | 只有入账幂等，没有创建请求幂等 | 重复点击、多标签、超时重试创建多单 | 是 |
| PAY-TXN-01 | P1 | 订单状态、余额事务、缓存和终态分成多个提交边界 | 崩溃时可能卡 paid 或缓存旧 | 是 |
| PAY-CACHE-01 | P1 | 重跑命中已有 ledger 后可能跳过缓存失效 | DB 已到账但 API quota 缓存可旧至 TTL | 是 |
| PAY-REC-02 | P1 | 对账查询无 SQL ORDER BY/LIMIT，缺复合索引 | 订单增长后扫描变慢或饥饿 | 是 |
| PAY-FACT-01 | P1 | provider txn ID 未持久化，金额缺失时可跳过校验 | 审计和支付事实约束不足 | 是 |
| PAY-OBS-01 | P1 | 无成功回调日志和分段时钟 | 无法准确判断慢在哪一段 | 是 |
| PAY-EXT-01 | P1 | Epay/Stripe 等把打开收银台当成流程成功 | 真正付款后没有持续刷新 | 是 |
| PAY-DB-01 | P2 | PostgreSQL 重复 ledger 唯一冲突处理不安全 | PostgreSQL 恢复路径可能无法推进 credited | 是 |

## 6. 详细问题与对应解决方案

### 6.1 PAY-LAT-01：弹窗开启时序错误

#### 问题

前端只有拿到 `wxpay_qr` 后才设置 `qrState`，而弹窗的 `open` 完全依赖 `qrState !== null`。现有 Dialog 无法表达“二维码生成中”。

#### 解决方案

建立显式前端状态机：

```text
idle
  → creating
  → pending
  → paid_processing
  → credited

异常：
creating_error | failed | expired | poll_timeout
```

点击时同步执行：

1. 保存金额与渠道。
2. 设置 `dialogOpen=true`。
3. 设置 `phase=creating`。
4. 异步发起创建订单请求。
5. 响应后在同一 Dialog 原地切换为二维码。

Dialog 必须包含：

- `DialogTitle` 与 `DialogDescription`
- `aria-live` 状态区域
- Spinner 或 Skeleton
- 金额、渠道和明确提示
- 错误态与原地重试
- 真实 `expires_at` 倒计时

`dialogOpen` 与 `activeOrder` 必须分离。关闭弹窗只能隐藏 UI，不能停止仍未终结的订单监控。

### 6.2 PAY-LAT-02：微信出站网络不稳定

#### 问题

当前同步 Prepay 最多允许约 24.6 秒。现网测试显示 DNS 返回的多个微信 IP 可达性差异巨大。

#### 解决方案

代码层：

1. 即使 Prepay 实际需要 20 秒，也立即显示 `creating` 弹窗。
2. 为 DNS、connect、TLS、TTFB、每次 provider attempt 增加分段指标。
3. 审核 `http.Transport` 的连接复用、`TLSHandshakeTimeout`、`IdleConnTimeout`、`MaxIdleConnsPerHost`。
4. 不继续盲目增加同步 timeout 或重试次数。
5. 不硬编码当前可用 IP，不绕过 TLS 主机验证。

基础设施层：

1. 将支付出站放到对微信链路稳定的合规区域或网络。
2. 如使用受控出站代理，必须评估支付数据安全、证书验证和合规要求。
3. 部署验收必须在服务器或 app 容器内测试，不能只看本机结果。
4. 本机 DNS 返回 `198.18.x.x` 时按项目说明视为 Clash/sing-box fake-ip。

### 6.3 PAY-STA-01：把入账处理中误判为到账完成

#### 问题

`OrderPaid` 是内部入账处理中间态，`OrderCredited` 才代表钱包完成。但状态接口和前端使用了含义模糊的 `paid` 布尔值。

#### 解决方案

状态接口增加无歧义字段：

```json
{
  "order_no": "RCG...",
  "status": "paid",
  "provider_paid": true,
  "credited": false,
  "amount_cny": 10.00,
  "amount_usd": 1.3698,
  "credited_quota": 684900,
  "current_quota": 10684900,
  "expires_at": "2026-08-06T13:42:00+08:00",
  "provider_paid_at": "2026-08-06T13:28:01+08:00",
  "credited_at": null,
  "updated_at": "2026-08-06T13:28:01+08:00"
}
```

规则：

1. `provider_paid=true` 表示支付机构已确认。
2. `credited=true` 或 `status=credited` 才表示用户余额完成。
3. 先盘点旧 `paid` 字段的所有消费者，再决定兼容策略。
4. default 前端只能依据 `credited` 完成流程。
5. `paid_processing` 时继续轮询并显示“支付已确认，余额入账中”。
6. 当前锁定 `paid → paid:true → 完成` 的测试必须重写。

### 6.4 PAY-REC-01：前端轮询和后端兜底时间不匹配

#### 问题

前端 180 秒停止；后端订单满 5 分钟后才进入每 5 分钟一次的扫描。

#### 解决方案

增加持久化的订单级快速补偿：

1. 主路径仍为支付机构回调。
2. 约 5 秒、30 秒、60 秒主动查单。
3. 后续按退避策略低频查询到 `expires_at`。
4. 当前 5 分钟扫描保留为最终扫尾。
5. 使用 `next_query_at`、`query_attempts` 等持久字段，进程重启后可恢复。
6. 查询任务必须在数据库中 `ORDER BY next_query_at,id LIMIT N`。
7. 每笔 provider 查询有独立 timeout，并使用有界并发。
8. 查询失败是 unknown；只有明确未支付且已过期才允许置 failed。
9. 浏览器仍只查询本站，不得直接请求微信。

前端轮询改为：

1. QR 返回后立即查询一次。
2. 之后约每 2 秒串行查询。
3. 任意时刻最多一个请求。
4. 使用 AbortSignal/请求 timeout。
5. `paid_processing` 继续。
6. `credited/failed/expired` 才停止。
7. 使用 `refetchOnWindowFocus` 恢复检查。
8. 关闭 Dialog 后 `activeOrder` 继续监控。

禁止继续使用 `setInterval(async ...)`。

### 6.5 PAY-UI-01：余额存在多个前端事实源

#### 问题

- Wallet：组件本地 `user` state。
- Dashboard：持久化 Zustand `auth.user`。
- 账单历史：另一个独立请求状态。

#### 解决方案

建立共享 current-user query 或统一 store action：

1. `credited` 时使用状态接口返回的权威 `current_quota` 立即更新 UI。
2. 同步更新 Wallet、`auth.user` 和持久化快照。
3. 随后 invalidate/refetch `/api/user/self` 做校验。
4. invalidate 账单历史。
5. 保留旧余额展示，后台刷新时不要重新显示整卡 Skeleton。
6. 移除 `mt:tenant-recharge-paid` 这种 window CustomEvent 桥接。

### 6.6 PAY-TXN-01 / PAY-CACHE-01：提交边界与缓存恢复窗口

#### 问题

当前依次发生：

```text
事务 A：created/failed → paid
事务 B：ledger + users.quota + top_ups
Redis：缓存失效
事务 C：paid → credited
```

可能的异常：

- A 后崩溃：订单 paid，余额未加。
- B 后崩溃：余额已加，缓存未失效。
- B 后、C 前崩溃：余额已加，订单仍 paid。
- 重跑命中已有 ledger 时提前返回，缓存失效可能永远不再执行，只能等 TTL。

#### 解决方案

优先引入清晰的 transactional settlement repository，在短数据库事务内完成：

1. 条件认领订单。
2. 保存支付机构事实。
3. 插入幂等 ledger。
4. 原子增加 `users.quota`。
5. 写入 `top_ups`。
6. 将订单推进 `credited` 并写 `credited_at`。

如果本阶段无法安全完成跨库事务重构，则必须至少：

1. 不再把 `paid` 对外当成完成。
2. 处理 Sink 失败后回滚 CAS 的错误，不能忽略。
3. 将 stuck-paid 恢复缩短到秒级。
4. ledger 已存在时仍能补订单终态。
5. ledger 已存在时仍能补缓存失效。

缓存建议使用 durable invalidation outbox：

1. 入账事务内写以 `order_no` 唯一的 outbox。
2. DB commit 后同步尝试一次短超时缓存失效。
3. Redis 失败不能回滚真实资金。
4. 失败必须持久化重试并告警。
5. 重复执行只做幂等删除，不能重复增加 quota。
6. 状态接口的 `current_quota` 直接读取主库权威值。

### 6.7 PAY-CBK-01：支付回调依赖通用 Redis 限流

#### 问题

支付回调没有 `UserAuth`，这一点正确。但它继承了 `apiBase` 的 `GlobalAPIRateLimit`：

- 按来源 IP 共用桶。
- Redis 错误时 fail-closed。
- 高峰超额时在验签前返回 429。

支付平台回调可能来自有限出口 IP，且合法财务回调不应把通用 Redis 限流作为入账可用性的单点。

#### 解决方案

将支付回调挂到独立基础中间件链：

1. 保留 RouteTag、请求日志、BodyStorageCleanup。
2. 保留 AnonymousRequestBodyLimit。
3. 保留严格验签。
4. 不继承普通用户共用的 GlobalAPIRateLimit。
5. 如需保护，使用专用 webhook 并发或高容量限流策略。
6. 专用保护不得因为非资金 Redis 故障直接阻断合法回调。

回调 ACK 契约保持渠道差异：

- 微信成功：HTTP 200。
- 支付宝成功：纯文本 `success`。
- 入账失败：返回非成功，让平台重试。

不要盲目统一为 HTTP 204。

### 6.8 PAY-IDEM-01：创建订单没有业务幂等

#### 问题

`order_no`、ledger 和 top-up 唯一约束保护的是重复回调，不保护重复点击。每次请求都会生成新 `order_no` 并调用支付机构。

#### 解决方案

推荐使用客户端支付意图幂等键：

1. 前端每次真实支付意图生成 opaque idempotency key。
2. 网络重试保持同一个 key。
3. `payment_orders` 增加 nullable `idempotency_key` 和唯一索引。
4. 唯一冲突后按 tenant/user 校验归属，不能泄露其他订单。
5. 未过期、待支付且已有 `pay_url` 时返回原订单和二维码。
6. 首请求仍在 creating 时返回同一订单的 creating 状态。
7. failed、credited 或过期订单不得错误复用。
8. 旧客户端可增加短窗口 pending 复用，但不能只依赖“两分钟时间桶”。

保留现有唯一约束：

- `payment_orders.order_no`
- `mt_recharge_credit_ledger.order_no`
- `top_ups.trade_no`

### 6.9 PAY-FACT-01：支付机构事实约束不足

#### 问题

当前：

- provider transaction ID 只在内存传递。
- 回调金额缺失或解析失败可能变成 0，而 0 会跳过金额比较。
- 主动查单只返回 bool。
- provider 与本地订单 provider 未形成数据库级绑定检查。

#### 解决方案

新增：

- `provider_transaction_id`
- `provider_paid_at`
- `credited_at`
- `callback_received_at`
- `expires_at`

建立 `(provider, provider_transaction_id)` 唯一约束，历史未支付行使用 NULL，不得统一回填空字符串。

成功支付事实必须验证：

1. 非空本地订单号。
2. 本地 provider 与回调 provider 一致。
3. 非空支付机构交易号。
4. 完整匹配的 appid/mchid 或 app_id/seller_id。
5. 有效正金额。
6. 金额与本地订单一致。

人民币金额逐步迁移为 `int64` 分进行精确比较。不得直接破坏现有报表使用的 decimal 字段，必须设计兼容迁移。

主动查单返回结构化结果，而不是 bool：

```text
provider
order_no
status
transaction_id
paid_amount_fen
paid_at
```

主动查单补账必须经过同一套支付事实校验。

### 6.10 PAY-REC-02：对账扫描不可扩展

#### 问题

`ListByStatus` 没有 SQL `ORDER BY` 和 `LIMIT`，先把全部订单读进内存，再在 Go 循环中截断 200 条。

#### 解决方案

1. Repo 接口接受 `limit` 和稳定游标。
2. SQL 层使用 `ORDER BY updated_at,id LIMIT ?`。
3. 增加 `(status,updated_at,id)` 或 `(status,next_query_at,id)` 复合索引。
4. stuck-paid 与 created-query 使用独立队列或公平游标。
5. provider 查询使用有界并发。
6. 支付恢复任务不要被无关 billing reconciliation 长时间阻塞。
7. 监控 backlog count 和 oldest age。

### 6.11 PAY-OBS-01：缺少分段观测

#### 解决方案

后端至少记录：

下单：

- `request_received`
- `local_order_created`
- `sdk_get_done`
- `provider_attempt_start/end`
- `pay_url_persisted`
- `response_sent`

到账：

- `notify_received`
- `signature_verified`
- `order_claimed`
- `ledger_transaction_committed`
- `cache_invalidated`
- `order_credited`
- `ack_sent`

指标：

- create total/provider/local DB histogram
- provider attempt count 和 failure reason
- notify verify failure
- credit failure
- duplicate callback
- cache invalidation failure
- paid→credited duration
- reconcile backlog 与 oldest age
- callback received→credited→ack duration

日志应带 `request_id`、`order_no`、`provider`、状态迁移和 `duration_ms`。不得记录完整二维码、私钥、APIv3 key、签名或完整回调 body。

前端 User Timing：

- `recharge_click`
- `dialog_open`
- `create_request_start`
- `create_response`
- `qr_rendered`
- `provider_paid_seen`
- `credited_seen`
- `balance_rendered`

### 6.12 PAY-EXT-01：其他支付渠道成功语义错误

Epay、Stripe、Creem、Waffo、Waffo Pancake 当前存在“打开收银台后立即刷新一次余额”的问题。

整改规则：

1. 创建订单或打开收银台只能表示 `order_created/redirect_created`。
2. 不能显示“付款成功”或“余额到账”。
3. 有订单号的渠道接入统一状态查询。
4. 暂时没有状态接口的渠道，至少在 window focus、返回钱包页时重新查询用户和订单。
5. 真正付款后必须有持续发现机制，不能只在跳转前刷新一次。

### 6.13 PAY-DB-01：PostgreSQL 重复 ledger 路径

PostgreSQL 中，事务内普通 INSERT 命中唯一冲突会让整个事务进入 aborted 状态。捕获错误后返回 nil 不能像 MySQL 一样继续正常提交。

整改：

1. 使用跨库安全的 GORM `OnConflict`、条件 CAS 或事务外预检查配合数据库唯一约束。
2. 不使用 MySQL 专属函数或 PostgreSQL 专属部分索引。
3. 在 SQLite、MySQL、PostgreSQL 中验证重复回调、崩溃恢复和唯一约束。

## 7. 目标架构

```text
用户点击
  → 立即打开 Dialog(creating)
  → 创建请求携带 idempotency key
  → 本地创建/复用订单
  → 同步向支付机构下单
  → 返回 QR + expires_at
  → Dialog 原地显示 QR(pending)
  → 前端串行轮询本站状态

主路径：
支付机构回调
  → 独立 webhook 中间件链
  → 验签与完整支付事实校验
  → 短数据库结算事务
  → credited
  → HTTP 200 ACK
  → 缓存 outbox 最终完成

快速补偿：
5s / 30s / 60s 持久化主动查单
  → 结构化支付事实
  → 同一结算服务

最终兜底：
5 分钟低频全局扫描

前端：
created/pending       等待支付
paid                 支付已确认、继续等待入账
credited             更新共享余额、停止轮询
failed/expired        明确终态
```

第一阶段不需要 WebSocket/SSE。两秒左右的本站轮询已经足够；先把状态语义、回调和共享余额修正确。

## 8. 建议实施顺序

### 阶段 A：用户立即可感知的 P0

1. 点击即打开 creating Dialog。
2. 明确前端状态机。
3. 前端只在 credited 时完成。
4. 轮询改为串行、立即首查、可取消。
5. Wallet 与 Dashboard 使用同一余额事实源。
6. 补前端行为测试和 User Timing。

### 阶段 B：到账可靠性 P0/P1

1. 状态 API 增加无歧义字段和权威余额。
2. 修复回调中间件链，不依赖通用 Redis 限流。
3. 缩短 stuck-paid 恢复时间。
4. 实现 5s/30s/60s 持久化查单。
5. 增加成功回调日志和阶段指标。

### 阶段 C：资金正确性与幂等

1. 创建 idempotency key。
2. provider transaction ID 持久化与唯一约束。
3. 精确金额字段与迁移。
4. 收敛结算事务或完善补偿不变量。
5. durable cache invalidation outbox。
6. 三数据库兼容测试。

### 阶段 D：其他渠道统一

1. Epay/Stripe/Creem/Waffo 状态语义统一。
2. 统一 current-user 与账单刷新。
3. 统一监控和性能指标。

### 阶段 E：基础设施

1. 根据分段指标确认微信网络 P95/P99。
2. 调整支付出站区域或合规网络。
3. 不用代码重试掩盖不可用网络路径。

## 9. 测试与验收要求

### 9.1 后端确定性测试

1. `status=paid` 不报告 credited。
2. `status=credited` 返回最新 quota。
3. 阻塞 OnPaid 时状态保持 processing。
4. OnPaid 失败时不得出现用户可见成功。
5. 20 个并发重复回调只增加一次 quota。
6. ledger 和 top-up 各一条。
7. provider transaction ID 关联不同订单时拒绝。
8. provider、商户、交易号、金额缺失时拒绝。
9. 金额差一分钱时拒绝。
10. 任一数据库步骤失败时验证事务不变量。
11. Redis 失败后 outbox 能最终失效缓存且不重复入账。
12. fake clock 验证 5s/30s/60s 查单。
13. 大量 created 废单不会饿死后续已付款订单。
14. 同 idempotency key 并发请求只创建一个本地订单，fake SDK 只调用一次。
15. SQLite、MySQL、PostgreSQL 验证迁移、CAS、唯一约束和重复 ledger。
16. 回调不会因为普通 GlobalAPIRateLimit 桶耗尽而在验签前返回 429。

禁止使用 sleep、随机输入、虚假压力循环或只证明代码运行的测试。

### 9.2 前端确定性测试

1. 创建 Promise 未 resolve 时 Dialog 已可见。
2. 同一 Dialog 从 creating 原地切换为 QR。
3. 快速双击只发送一个 POST。
4. 任意时刻最多一个状态请求。
5. paid_processing 不关弹窗、不显示成功。
6. credited 只执行一次 toast 和一次余额更新。
7. 重复 credited 响应不重复副作用。
8. 关闭 Dialog 后 active order 仍被监控。
9. Wallet 与 Dashboard 余额同步。
10. failed、expired、网络错误和请求挂起有明确 UI。
11. 旧金额响应不得覆盖新金额。
12. 打开 Stripe/Epay 收银台不等于付款完成。

### 9.3 性能目标

| 指标 | 目标 |
| --- | --- |
| 点击 → Dialog 显示 | P95 < 100ms |
| 创建响应 → QR 本地渲染 | P95 < 50ms |
| 回调收到 → DB credited | P95 < 1s |
| credited → 页面显示新余额 | P95 < 2.5s |
| 正常支付 → 页面显示 | 目标 P95 < 3s，支付机构回调延迟单独统计 |
| 回调失达 → 主动查单补账 | P95 < 60s |
| 回调总处理与 ACK | P95 < 5s |

当前网络条件下，微信 Prepay P95 < 1.5s 不能仅靠前端代码承诺，必须单独列为基础设施整改指标。

## 10. 最终 Review 操作

本节用于 Grok 或其他实现者完成修改后的最终代码审查。Review 默认只读；发现问题先形成 findings，不要在 Review 阶段顺手扩大修改范围。

### 10.1 Review 前置

1. 读取：
   - 根 `AGENTS.md`
   - `web/default/AGENTS.md`
   - 本文档
2. 记录当前分支、HEAD 和工作区状态。
3. 区分用户原有修改与本次支付修改。
4. 确认没有执行部署、真实支付或生产写操作。
5. 确认 `403 NO_AUTH` 没有被混入本次整改。

```bash
git branch --show-current
git log -1 --oneline
git status --short
git diff --stat
```

### 10.2 Diff 范围 Review

检查：

1. 是否只修改支付相关文件、必要的共享用户状态和 i18n 文件。
2. 是否误改 `web/classic`。
3. 是否覆盖工作区已有修改。
4. 是否修改受保护的项目/组织标识。
5. 是否引入不必要的新依赖。
6. 是否新增单调用者机械 helper、深层嵌套或重复状态源。
7. 是否有临时调试日志、完整 QR、回调原文或密钥输出。

```bash
git diff --name-status
git diff --check
git diff -- router internal/payment internal/mtwire model web/default/src/features/wallet web/default/src/stores
```

### 10.3 架构与 API Review

逐项确认：

- [ ] 点击后 Dialog 立即打开，不等待创建接口。
- [ ] Dialog 能表达 creating、pending、paid_processing、credited、failed、expired。
- [ ] `dialogOpen` 与 `activeOrder` 分离。
- [ ] 前端只有 credited 才显示到账成功。
- [ ] paid_processing 会继续轮询。
- [ ] 状态接口仍只查询本站。
- [ ] 旧 `paid` 字段兼容策略经过消费者盘点。
- [ ] 状态接口保持用户与租户隔离。
- [ ] current_quota 来自主库权威值。
- [ ] Wallet 和 Dashboard 使用同一余额事实源。
- [ ] Epay/Stripe 打开收银台不再被视为付款成功。
- [ ] 不使用 `setInterval(async ...)`。
- [ ] 状态查询不会重叠，能取消旧请求。

### 10.4 资金正确性 Review

- [ ] 回调验签在任何入账前完成。
- [ ] 本地 provider 与回调 provider 匹配。
- [ ] 商户、应用、交易号和金额完整校验。
- [ ] 金额使用整数分或等价的精确方式比较。
- [ ] provider transaction ID 已持久化并唯一。
- [ ] 重复回调不会重复增加 quota。
- [ ] ledger、quota、top-up 和订单终态满足文档定义的不变量。
- [ ] 任一步失败不会形成“订单成功但余额未加”的静默状态。
- [ ] Redis 失败不回滚真实资金，也不会永久保留旧缓存。
- [ ] 重复 ledger 恢复路径仍会修复订单终态和缓存。
- [ ] SUB/AGT 支付分发未被 RCG 改动破坏。

### 10.5 数据库与迁移 Review

- [ ] SQLite、MySQL、PostgreSQL 均可迁移。
- [ ] 新增唯一字段对历史行使用 NULL 或安全回填。
- [ ] 没有把旧行全部回填为空字符串造成唯一冲突。
- [ ] 没有 MySQL 专属函数、PostgreSQL 专属操作符或 SQLite 不支持的 ALTER。
- [ ] PostgreSQL 唯一冲突不会令事务永久 aborted。
- [ ] `payment_orders` 有正确的幂等、provider txn 和调度索引。
- [ ] 对账查询在 SQL 层 ORDER BY + LIMIT。
- [ ] 没有新增会反复触发 AutoMigrate 的布尔 default tag。
- [ ] 金额迁移没有破坏现有报表字段。

建议检查最终表意图：

```text
payment_orders:
  order_no UNIQUE
  idempotency_key UNIQUE NULLABLE
  provider_transaction_id NULLABLE
  UNIQUE(provider, provider_transaction_id)
  expires_at
  provider_paid_at
  credited_at
  callback_received_at
  next_query_at
  query_attempts
  INDEX(status, next_query_at, id)

mt_recharge_credit_ledger:
  order_no PRIMARY KEY

top_ups:
  trade_no UNIQUE
```

### 10.6 Webhook 可用性与安全 Review

- [ ] 微信/支付宝回调没有 UserAuth。
- [ ] 回调保留 body size limit。
- [ ] 回调保留严格签名验证。
- [ ] 回调不再依赖普通用户共用 Redis 限流。
- [ ] 专用防护不会轻易拒绝合法平台出口高峰。
- [ ] 微信成功 ACK 仍为 HTTP 200。
- [ ] 支付宝成功 ACK 仍为纯文本 success。
- [ ] 入账失败返回平台可重试的失败响应。
- [ ] 回调响应前没有邮件、报表、画像或其他无关慢任务。
- [ ] 回调 P95 能在 5 秒内完成。

### 10.7 缓存与前端状态 Review

- [ ] DB commit 后缓存立即失效或进入 durable outbox。
- [ ] outbox 重启后可恢复。
- [ ] outbox 重复消费不会重复加余额。
- [ ] `/api/user/self`、API quota 缓存和页面显示最终一致。
- [ ] credited 后同步更新 current-user query 与 Zustand。
- [ ] localStorage 不长期保留旧 quota。
- [ ] 账单历史在到账后刷新。
- [ ] 页面 focus/return 会恢复未完成订单检查。

### 10.8 可观测性 Review

- [ ] 下单有 local DB、SDK、provider、pay_url 回填分段时长。
- [ ] 每次 provider attempt 可区分。
- [ ] 成功和失败回调都有精简日志。
- [ ] 有 callback verify、DB credit、cache、ACK 分段时长。
- [ ] 有 paid→credited duration。
- [ ] 有 reconcile backlog 和 oldest age。
- [ ] 前端有 click→dialog→QR→credited→balance User Timing。
- [ ] 指标标签没有 user_id/order_no 等高基数值。
- [ ] 日志未泄露敏感支付数据。

### 10.9 自动化验证

后端至少执行：

```bash
gofmt -w <本次修改的 Go 文件>
go test ./internal/payment/... ./internal/mtwire ./router ./model
go test ./...
```

前端执行：

```bash
cd /Users/cc/newapi628/web/default
bun run i18n:sync
bun run test
bun run typecheck
bun run lint
bun run format:check
bun run build
```

额外检查：

```bash
git diff --check
git status --short
```

Review 报告必须记录每条命令是否实际运行、退出码和失败原因，不能只写“测试通过”。

### 10.10 Staging 与现网只读验收

任何真实支付验收前，先完成只读检查：

1. 版本与 readiness。
2. 回调域名、HTTPS 和 Nginx 生效配置。
3. 微信 API DNS/TLS 出站。
4. payment_orders 卡单和对账 backlog。
5. Redis、MySQL 健康与慢查询。

DNS/HTTPS 检查遵循项目规则：

```bash
dig <callback-domain> +short
dig <callback-domain> @1.1.1.1 +short
ssh <server> "dig <callback-domain> +short"
dig +trace <callback-domain>
```

微信出站检查必须在服务器或 app 容器执行；不得用本机 `198.18.x.x` fake-ip 作结论。

真实小额支付只能在用户明确授权后执行。验收时记录：

```text
T0 用户点击
T1 Dialog 显示
T2 后端收到创建请求
T3 本地订单创建
T4 开始请求微信
T5 微信返回
T6 前端收到创建响应
T7 QR 渲染完成

P0 用户完成支付
P1 本站收到回调
P2 DB credited
P3 前端查到 credited
P4 页面显示新余额
P5 Dashboard 显示相同余额
```

完成后只读核对：

- payment_orders 最终为 credited。
- ledger 恰好一条。
- top_up 恰好一条。
- users.quota 只增加一次。
- provider transaction ID 唯一。
- 重复回调不会重复入账。
- 对账脚本无 paid 卡单。

不得手工 UPDATE 订单状态或余额来“修复”验收结果。

### 10.11 Go / No-Go 门槛

以下任一项失败即 No-Go：

1. paid 中间态仍会让前端显示成功。
2. 重复回调可能重复增加 quota。
3. provider transaction ID 未形成唯一约束。
4. PostgreSQL 重复 ledger 路径失败。
5. 回调仍可能被普通 GlobalAPIRateLimit 在验签前拒绝。
6. 回调失达后首次补账仍只能等待 5–10 分钟。
7. Wallet 与 Dashboard 余额不一致。
8. 任一支持数据库迁移失败。
9. 后端或前端验证命令失败。
10. 日志泄露二维码、签名、密钥或完整回调。
11. 没有明确回滚方案。
12. 现网微信出站网络仍严重不稳定，却把项目标记为“二维码性能已完全解决”。

### 10.12 回滚门槛

上线后出现以下任一情况应立即停用受影响支付渠道并执行受支持的版本回滚：

- 重复入账或金额不一致。
- 回调验签失败率异常升高。
- credited 订单无 ledger/top-up。
- ledger 已存在但 quota 未增加。
- 回调 ACK P95 超过 5 秒。
- 大量订单卡在 paid。
- 新迁移导致数据库错误或锁等待异常。
- 前端把 pending/paid 显示为成功。

回滚不能删除支付订单或台账。回滚后先查询支付机构真实订单，再运行只读对账；不得直接改订单状态。

## 11. 最终 Review 输出模板

```markdown
# 支付整改最终 Review

## 结论
PASS / PASS WITH FOLLOW-UPS / NO-GO

## 基线
- Branch:
- Commit:
- Diff files:
- Reviewer:
- Review time:

## Findings
### P0
- [文件:行] 问题、影响、证据、建议

### P1
- ...

### P2
- ...

## 需求逐项验证
- 即时弹窗：
- paid/credited 语义：
- 余额单一事实源：
- 创建幂等：
- 回调幂等：
- 快速查单：
- 缓存 outbox：
- 三库兼容：
- Webhook 可用性：
- 可观测性：

## 实际执行的命令
- command:
  - exit:
  - result:

## 数据库迁移 Review
- 字段：
- 索引：
- 历史数据兼容：
- SQLite：
- MySQL：
- PostgreSQL：

## Staging / 现网只读证据
- 版本：
- DNS/TLS：
- 回调：
- T0–T7：
- P0–P5：

## 未解决风险
- ...

## Go / No-Go
- 结论：
- 理由：
- 回滚触发条件：
```

## 12. 给最终 Reviewer 的提示词

```text
请对 /Users/cc/newapi628 的支付整改做严格、只读优先的最终 Review。

先完整阅读：
1. AGENTS.md
2. web/default/AGENTS.md
3. doc/payment-system-latency-audit-and-remediation.md

忽略已知 403 NO_AUTH，不要把 Review 转向商户权限配置。不得修改或删除受保护的 new-api / QuantumNous 信息，不得部署、发起真实支付或修改生产数据。

Review 顺序：
1. 确认 git 基线、工作区已有修改和本次 diff 范围。
2. 重建当前真实调用链，不接受实现者的口头描述。
3. 逐项核对即时弹窗、状态机、paid/credited、共享余额、轮询生命周期。
4. 审核回调验签、金额/provider/txn 校验、重复回调幂等和结算提交边界。
5. 审核缓存 outbox、崩溃恢复、快速主动查单和 5 分钟最终兜底。
6. 审核回调是否仍受普通 GlobalAPIRateLimit/Redis 故障影响。
7. 审核 idempotency_key、provider_transaction_id、时间字段和索引迁移。
8. 验证 SQLite、MySQL、PostgreSQL 兼容，特别检查 PostgreSQL 唯一冲突后的事务状态。
9. 审核前端 i18n、Base UI Dialog 可访问性、React Query/轮询不重叠、Wallet/Dashboard 状态一致。
10. 审核日志与指标，确认不记录敏感支付数据或高基数指标标签。
11. 运行文档规定的后端和前端验证命令。
12. 只读检查服务器版本、DNS/TLS、回调与卡单；本机 198.18.x.x 必须视为 fake-ip。

输出 findings-first Review：
- 按 P0/P1/P2 排序；
- 每条给出文件和行号、真实影响、复现或证据、建议；
- 区分已证实问题与待线上验证；
- 最后给 PASS / PASS WITH FOLLOW-UPS / NO-GO；
- 任一资金幂等、金额校验、三库迁移或 paid/credited 问题未解决时必须 NO-GO。

Review 默认不要修改代码。只有用户明确要求“修复 Review findings”后才实施修改。
```

## 13. 明确不应做的事情

1. 不要只增加按钮动画或延长 loading 文案。
2. 不要把同步重试从 3 次继续无上限增加。
3. 不要让浏览器直接请求微信/支付宝。
4. 不要因为回调有签名就取消请求体限制。
5. 不要给支付回调增加用户登录认证。
6. 不要把支付机构返回页面或 redirect 当作支付成功。
7. 不要用前端传来的“成功”直接加余额。
8. 不要手工修改订单状态绕过回调。
9. 不要用 Redis 锁代替数据库唯一约束和资金台账。
10. 不要仅依赖两分钟时间桶作为创建幂等。
11. 不要在回调 ACK 前发送邮件、生成报表或做无关统计。
12. 不要硬编码当前可访问的微信 IP。
13. 不要声称数据库入账慢，除非新增分段数据能够证明。
14. 不要把 UI 立即响应和真实微信网络性能混为同一个指标。

## 14. 尚需实现后验证的事项

以下内容当前缺少完整线上数据，实施后必须通过新观测补齐：

1. 用户付款时间到微信首次回调时间。
2. Cloudflare/WAF 是否曾阻断支付回调。
3. 微信回调源 IP 分布及高峰量。
4. 回调 P95/P99 验签、DB、缓存和 ACK 耗时。
5. MySQL 用户行锁等待是否在高并发时放大入账时间。
6. Redis 命令超时对 API quota 可见性的实际影响。
7. 微信出站网络改善后的 DNS/TCP/TLS/TTFB 分位数。
8. 其他支付渠道是否都能取得稳定的订单状态和交易号。
