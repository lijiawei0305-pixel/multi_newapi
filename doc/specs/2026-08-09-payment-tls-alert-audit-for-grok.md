# 支付 TLS 告警重复邮件审计与修复交接（供 Grok）

> 日期：2026-08-09
> 级别：严重（告警口径失真 + 重复邮件；同时存在真实微信支付出口抖动）
> 本文用途：给 Grok 直接据此修改代码、补测试。本文不代表已经修复或部署。
> 用户现象：反复收到主题为「支付 TLS 成功率低于门禁」的邮件，例如：
> `tls_success_rate=0.955（门禁≥0.99）；cold+reused 样本不足时不报。conn_reuse_rate=0.417 prepay_sync_p95_ms=-1 prepay_bg_p95_ms=-1`

---

## 1. 结论

这不是单纯的误报，也不能据此直接认定“真实业务 Prepay TLS 成功率只有 95.5%”。

同时成立的事实：

1. **现网微信支付出口确有明显抖动。** 保温探测日志中存在大量连接超时、TLS 4 秒超时和 TTFB 超时，不能通过删除告警或关闭 TLS 校验掩盖。
2. **当前 `tls_success_rate` 算法口径错误。** TLS 已明确失败的请求可能被记为 TLS 成功；DNS/TCP 阶段失败、TLS 根本没开始的请求反而被记为 TLS 失败；复用连接也被当作一次 TLS 成功。
3. **这封邮件并不代表真实 Prepay。** 本次现网采样周期内真实 `prepay`/`prepay_bg` HTTP 样本均为 0，邮件完全由保温探测和查单流量触发。
4. **`prepay_*_p95_ms=-1` 表示样本少于 5，不是耗时为负，也不是独立故障。** 邮件没有把它格式化为 `n/a`，容易误导。
5. **后台 Prepay 指标接线缺失。** 微信 `createPay` 不区分同步/后台预算，一律写 `operation=prepay`，所以 `prepay_bg_p95_ms` 在现有路径下不会正确产生。
6. **告警会按固定 30 分钟反复发送。** 全量对账每 5 分钟调用一次 SLI 告警；相同 DedupKey 仅抑制 30 分钟。指标持续低于阈值时，每过 30 分钟就会重发，没有状态迁移、恢复通知或更长提醒周期。
7. **微信备域保温实际没有访问备域。** `api2.mch.weixin.qq.com` 探测请求被 `hostRewriteTransport` 改回主域；日志外层显示备域，内层实际请求始终是主域。
8. **连接复用率也被重复/矛盾计数。** 同一次保温既记一条 `payment_http`，又记一条 `payment_warm`；支付宝外层保温永远记录 `conn_reused=false`，而内层 HTTP 几乎全为 `true`。

因此应当同时修复“指标真实性”和“告警生命周期”，不能只把阈值从 0.99 调低，也不能只延长全局告警去重时间。

---

## 2. 现网只读证据

### 2.1 环境

- 线上 app 版本：`9ed52da-20260806-223418`
- 本轮进程启动时间：2026-08-09 02:30:13（Asia/Taipei）
- 日志检查方式：只读读取 `newapi_test-app-1` 从本轮启动至检查时的 `payment_http` / `payment_warm` 日志。
- 未修改服务器、容器、网络、DNS、支付配置或告警设置。

### 2.2 与邮件数值完全对应的观测

检查时汇总约为：

| 项目 | 数量 |
| --- | ---: |
| `payment_http` 总数 | 2455 |
| `operation=warm` | 2376 |
| `operation=query` | 79 |
| `operation=prepay` | 0 |
| `operation=prepay_bg` | 0 |
| HTTP 日志成功 | 2148 |
| HTTP 日志失败 | 307 |
| `conn_reused=true` | 1410 |
| 冷连接 | 1045 |
| 日志明确为 `stage=tls` 的失败 | 181 |
| 失败且 `tls_ms=not_run` | 111 |

当前代码仅把“失败 + 非复用 + `tlsOK=false`”记为 `TLSFail`。`tlsOK` 又通过“TLS start/done 时间都存在”判断，而没有检查 `TLSHandshakeDone` 的 `err`。

因此：

```text
现有口径 TLSFail = 111
现有口径 TLSOK   = 2455 - 111 = 2344
2344 / 2455      = 0.954786...
邮件格式化       = 0.955
```

这证明邮件数值来自当前代码口径，而非独立监控系统。

关键反例：181 条日志已经明确 `stage=tls` 且失败，但因为 `TLSHandshakeDone` 即使收到错误也写入了结束时间，它们反而进入 `TLSOK`。相反，111 条 TLS 尚未运行的 DNS/TCP/上下文失败进入 `TLSFail`。

### 2.3 `p95=-1` 的真实含义

当前 `p95Ms` 在样本少于 5 时返回 `-1`。本轮进程真实 Prepay HTTP 样本为 0，所以：

```text
prepay_sync_p95_ms=-1
prepay_bg_p95_ms=-1
```

属于“无足够样本”，不是业务延迟故障。TLS 告警仍然触发，是因为 TLS 分母主要由每分钟保温探测填满，两个门槛互相独立。

### 2.4 备域保温失效

本轮检查：

| 观测 | 数量 |
| --- | ---: |
| 外层 `payment_warm host=api2.mch.weixin.qq.com` | 792 |
| 内层 `payment_http host=api2.mch.weixin.qq.com` | 0 |

原因：`defaultProbe` 给所有微信探测都写 `preferBackup=false`；`hostRewriteTransport` 看到备域 URL 后又把它改回主域。结果是“主域探测两次”，备域没有真正建连或保温。

### 2.5 支付宝复用率矛盾

本轮支付宝保温约 792 次：

- 内层 `payment_http` 几乎全部 `conn_reused=true`；
- 外层 `payment_warm` 全部 `conn_reused=false`；
- 原因是 `SignedProbe` 分支提前返回，没有把内层 trace 结果带回 `WarmResult`，但 `RecordWarm` 仍把缺失信息当作 cold。

近同时刻用当前公式重建：

```text
(HTTPReused + WarmReused) / (HTTPCount + WarmCount) ≈ 0.417
```

与邮件 `conn_reuse_rate=0.417` 一致。该值混合了双重计数和“未知当 false”，不能视为可信连接复用 SLI。

### 2.6 保温耗时恒为 0

现网 `payment_warm` 日志的 `duration_ms` 恒为 0，但内层 HTTP 明明耗时数百毫秒至数秒。

`defaultProbe` 使用未命名返回值，并在 defer 中修改局部变量：

```go
res := WarmResult{...}
defer func() { res.Duration = time.Since(start) }()
return res
```

`return res` 先复制返回值，defer 再修改局部 `res`，修改不会进入已复制的返回值。该缺陷不直接触发本邮件，但会污染保温延迟观测。

---

## 3. 代码根因

### F1（严重）：TLS 结果只看回调时间，不看握手错误

文件：`internal/payment/realpay/httpclient.go`

当前 `TLSHandshakeDone` 丢弃 `error`，失败路径又用以下条件判断：

```go
tlsOK := !snap.tlsStart.IsZero() && !snap.tlsDone.IsZero() || snap.reused
```

问题：

- TLS 握手回调“结束”不代表成功；回调会携带握手错误。
- 复用连接没有发生新 TLS 握手，不应作为一次 TLS 成功样本。
- TLS 尚未开始的 DNS/TCP 失败不应进入 TLS 成功率分母。

### F2（严重）：`RecordHTTP` 把所有冷连接失败粗分成 TLS 失败

文件：`internal/payment/realpay/metrics.go`

当前逻辑：

```go
if tlsOK {
    tlsOK++
} else if !ok && !reused {
    tlsFail++
}
```

这会把 DNS、connect、context cancel 等失败写入 `tlsFail`，与字段名和告警主题不符。

### F3（严重）：TLS 告警由进程启动以来累计值驱动，最少只需 5 个样本

文件：`internal/payment/realpay/metrics.go`

- 计数器从进程启动后一直累计，没有时间窗口。
- `tlsTotal >= 5` 就开始判断 99% 门禁。
- 一个失败样本在总样本达到 100 前都会让成功率低于 99%。
- 旧故障会长时间拖低累计值；大量旧成功也可能掩盖最新故障。
- 重启会清零，告警行为受发布时间影响。

这与 `doc/payment-egress-ops-plan.md` 的“跨时段、每时段至少 30 次、低样本不得报”并不一致。

### F4（严重）：告警没有状态机，固定每 30 分钟重发

调用链：

```text
reconcile loop（每 5 分钟）
  -> alertReconcileHealth
  -> alertPaymentSLI
  -> AlertSink.Dispatch(DedupKey="payment_tls_sli")
  -> 默认 Dedup TTL 30 分钟
```

持续低于门禁时，邮件最多每 30 分钟一次；进程重启后内存去重状态丢失，可立即再发。没有：

- healthy -> degraded 状态迁移；
- 连续窗口确认；
- degraded -> recovered 恢复通知；
- 独立的 payment reminder 周期；
- warm-only 与真实 Prepay 失败的严重度区分。

### F5（高）：同步/后台 Prepay 指标未正确分类

文件：`internal/payment/realpay/wxpay.go`

无论 `BudgetSync` 还是 `BudgetBackground`，都写：

```go
ctxKeyPayOperation{} = "prepay"
```

所以所有微信 HTTP attempt 都进入 `prepaySyncDurations`，`prepayBgDurations` 没有正常生产调用来源。

此外，当前 P95 统计的是 HTTP attempt（对冲时可能一个逻辑下单产生多个 attempt），而不是一次逻辑 Prepay 的端到端耗时。文件中已有 `RecordPrepayDuration(bg, d)`，但没有任何生产调用者。

### F6（高）：微信备域探测被重写回主域

文件：`internal/payment/realpay/warmer.go`、`httpclient.go`

`BuildWxWarmTargets` 虽返回主/备两个 URL，但 `defaultProbe` 对两者都设置 `preferBackup=false`。备域探测经过 `hostRewriteTransport` 后变成主域。

### F7（高）：连接复用率双重计数且缺失值按 false

文件：`internal/payment/realpay/metrics.go`、`warmer.go`

- 每个保温请求在 `tracingRoundTripper` 记一次 HTTP reuse/cold；`RecordWarm` 又记一次。
- 支付宝 `SignedProbe` 外层拿不到 trace，仍被记为 cold。
- 指标把 warm 与业务请求混为一个总率，无法回答“业务 Prepay 是否复用连接”。

### F8（中）：告警正文缺少诊断信息，`-1` 对人不友好

当前邮件没有提供：

- 窗口起止时间；
- TLS 成功/失败/未尝试样本数；
- provider、主域/备域拆分；
- warm/query/prepay 拆分；
- 是否仅为 warm-only；
- 连续异常窗口数；
- 上次通知和恢复状态。

`-1` 应显示为 `n/a (n=0)`，而不是像真实毫秒值一样输出。

---

## 4. Grok 修改要求

### 4.1 必须修：建立真实 TLS 观测语义

建议在 trace 中保存明确三态，而不是一个推断布尔值：

```text
not_attempted  TLSHandshakeStart 未发生（DNS/TCP/复用连接）
success        TLSHandshakeDone(err == nil)
failure        TLSHandshakeDone(err != nil)，或 start 后超时未正常完成
```

要求：

1. `TLSHandshakeDone` 必须保存 `err`/成功状态。
2. DNS/TCP 失败不进入 TLS 分母，另按 stage 统计。
3. 复用连接不进入“本次 TLS 握手成功率”分母；复用健康另看 HTTP/TTFB/请求成功率。
4. 日志 `stage=tls` 的失败必须进入 TLSFail，不得进入 TLSOK。
5. 不能关闭证书、hostname、SNI 校验，不能使用 `InsecureSkipVerify`、固定微信 IP 或 `curl -k`。

### 4.2 必须修：指标分维度且使用滚动窗口

至少区分：

- provider：wxpay / alipay；
- operation：warm / query / prepay_sync / prepay_bg；
- host：微信 primary / backup；
- connection：cold handshake / reused；
- stage：dns / connect / tls / ttfb / response。

告警不得继续使用“进程启动以来累计 + 5 个样本”。推荐契约：

1. 使用明确滚动时间窗（建议 30 分钟）或带时间戳的有界样本。
2. TLS 门禁只看 cold TLS handshake observations。
3. 单窗口至少 30 个有效 cold TLS 样本；若团队坚持把 99% 当硬门禁，建议至少 100 个样本，或按运维方案采用“3 个时段 × 每时段 ≥30”的连续确认。
4. 样本不足返回 `available=false` + `sample_count`，不要用数值 `-1` 混入正常指标。
5. 当前窗口与历史窗口分开，避免旧故障永久拖累，也避免旧成功掩盖当前崩溃。

阈值与最小样本若最终选用不同数值，必须在代码、测试和 `payment-egress-ops-plan.md` 中保持一致，不得只改邮件文字。

### 4.3 必须修：支付专用告警状态机

不得通过修改 `alert.DefaultDedupTTL` 影响对账失败、软删用户入账等其它严重告警。

建议 payment TLS 告警使用独立状态：

```text
insufficient -> 不发
healthy      -> 不发
degraded 第 1 个窗口 -> 记录，不发或发 Warning
degraded 连续达到门槛 -> 发一次 degraded 告警
持续 degraded -> 不每 30 分钟发；可每 6 小时提醒一次
recovered -> 发一次恢复通知并重置状态
```

严重度建议：

- 仅 warm 探测异常、没有真实 Prepay 样本：`Warning`；
- 真实 `prepay_sync/prepay_bg` 连续失败、breaker open、或支付订单进入 unknown：`Critical`；
- breaker 告警继续独立，不与 TLS warm 告警合并。

如果不做完整状态机，最低要求也应是 payment 专用冷却时间 + 连续窗口门槛 + recovery 通知，而不是继续依赖通用 30 分钟 Dedup。

### 4.4 必须修：Prepay P95 按逻辑操作记录

1. 根据 `budgetModeFrom(ctx)` 标记 `prepay_sync` 或 `prepay_bg`。
2. 使用已有 `RecordPrepayDuration(bg, duration)` 在一次逻辑 `createPay` 结束时只记录一次。
3. HTTP attempt/hedge 耗时保留为 attempt 指标，但不要与逻辑 Prepay P95 混用或重复写入。
4. 邮件输出样本数：`prepay_sync_p95_ms=n/a (n=0)`。
5. 支付宝 `TradePagePay` 是本地生成跳转 URL，不要伪装成网络 Prepay TLS 样本。

### 4.5 必须修：保温主/备域与连接复用

1. 备域 target 必须真正访问 `api2.mch.weixin.qq.com`；可根据 target host 设置 `preferBackup=true`，或让显式 warm URL 绕过主备重写。
2. 测试必须断言外层 target 与内层实际请求 hostname 一致。
3. 连接复用只保留一个权威采集点，建议使用 `tracingRoundTripper`；`RecordWarm` 不再重复贡献 reuse/cold 总数。
4. “拿不到 trace”必须表示 unknown，而不是 false/cold。
5. 修复 `WarmResult.Duration` 恒为 0；不要依赖修改未命名返回局部值的 defer。

### 4.6 必须改进：邮件正文

推荐正文至少包含：

```text
window=2026-08-09T15:00:00+08:00..15:30:00+08:00
provider=wxpay host=primary|backup
tls_success_rate=...
tls_ok=... tls_fail=... tls_not_attempted=...
warm_ok=... warm_fail=...
prepay_sync_p95_ms=n/a prepay_sync_samples=0
prepay_bg_p95_ms=n/a prepay_bg_samples=0
consecutive_bad_windows=...
severity_reason=warm_only|business_prepay|breaker
```

避免继续写“cold+reused 样本”，因为 reused 请求没有发生本次 TLS handshake。

---

## 5. 必须补的回归测试

新测试遵守项目规则：Go 使用 `testify/require` 做 setup/fatal、`testify/assert` 做值断言；确定性输入，不使用随机、sleep、真实公网或只为覆盖率的循环。

### `internal/payment/realpay/httpclient` / metrics

1. `TLSHandshakeDone(err != nil)` -> `TLSFail +1`、`TLSOK` 不增加。
2. TLS 成功 -> `TLSOK +1`。
3. DNS/connect 失败且 TLS 未开始 -> TLS 样本总数不增加。
4. reused 请求 -> TLS handshake 样本不增加，但 reuse 指标增加。
5. 日志/指标对同一 trace 的 stage 语义一致。
6. 低于最小样本 -> snapshot 明确 unavailable，告警不触发。
7. 滚动窗口淘汰过期样本，旧失败不永久拖低新窗口。
8. provider/operation/host 分桶互不串样本。

### Prepay

1. `BudgetSync` 只写一条逻辑 `prepay_sync` duration。
2. `BudgetBackground` 只写一条逻辑 `prepay_bg` duration。
3. hedged 两个 HTTP attempt 仍只产生一个逻辑 Prepay duration。
4. 0–4 个样本显示 unavailable/n/a，不输出负毫秒。

### Warmer

1. primary target 实际请求 primary。
2. backup target 实际请求 backup，不被重写回 primary。
3. Alipay SignedProbe 未提供 reuse 时为 unknown，不记 cold。
4. 一个 warm request 不在 HTTP + warm 两处重复贡献连接复用率。
5. `WarmResult.Duration > 0` 使用注入时钟或确定性计时 seam 验证，禁止 sleep/timing 比大小的脆弱测试。

### Alert

1. warm-only + 样本不足 -> 不发 Critical。
2. 第一个坏窗口不满足连续门槛 -> 不发或只 Warning（按最终契约）。
3. 连续坏窗口达到门槛 -> 只发一次 degraded。
4. 持续坏但未到 reminder 周期 -> 不重复。
5. reminder 到期 -> 只提醒一次。
6. 恢复 -> 发一次 recovery；后续 healthy 不重复。
7. breaker open 仍立即 Critical，不受 TLS warm-only 降级影响。
8. 不修改全局 Dedup TTL，不破坏既有对账告警去重测试。

---

## 6. 验收标准

### 6.1 代码/单测

```bash
go test ./internal/payment/realpay ./internal/mtwire ./internal/alert -count=1
go test -race ./internal/payment/realpay ./internal/mtwire ./internal/alert -count=1
go vet ./internal/payment/realpay ./internal/mtwire ./internal/alert
```

随后运行：

```bash
go test ./... -count=1
```

### 6.2 观测契约

- 明确 TLS 失败日志不能被计入 TLSOK。
- connect/DNS 失败不能被计入 TLSFail。
- reused 不进入 TLS handshake 成功率分母。
- 主/备域都有真实内层 hostname 样本。
- 无真实 Prepay 时显示 `n/a (n=0)`，且 warm-only 不发 Critical。
- 持续异常不会每 30 分钟重复邮件；恢复后只通知一次。
- 告警中样本数能人工反算成功率。

### 6.3 线上发布后只读验证

1. 核对 app 版本和启动时间，等待滚动窗口达到最小样本。
2. 汇总 `payment_http` / `payment_warm`，按 provider/operation/host/stage 对账。
3. 证明备域内层日志真的出现 `host=api2.mch.weixin.qq.com`。
4. 在没有真实下单时，Prepay P95 为 n/a 且不发 Critical。
5. 若微信 warm TLS 仍大量超时，应保留 Warning 并继续执行出口网络整改；不得把“邮件不再轰炸”误写成“网络已修复”。

---

## 7. 建议修改文件

| 文件 | 预期修改 |
| --- | --- |
| `internal/payment/realpay/httpclient.go` | 保存 TLSHandshakeDone error/三态；正确区分 stage；不把 reused 当新 TLS 成功 |
| `internal/payment/realpay/metrics.go` | 分维度滚动窗口、样本数/available、去除双计数、逻辑 Prepay P95 |
| `internal/payment/realpay/wxpay.go` | sync/bg operation 分类；逻辑 Prepay duration 只记一次 |
| `internal/payment/realpay/warmer.go` | 备域真实访问、unknown reuse、Duration 修复 |
| `internal/mtwire/reconcile_orchestrate.go` | 连续窗口/状态迁移/恢复/专用 reminder；邮件正文改进 |
| `internal/alert/*` | 原则上不改全局 TTL；仅在确需通用 per-alert cooldown 能力时扩展且保护所有既有契约 |
| `doc/payment-egress-ops-plan.md` | 统一窗口、最小样本、严重度与恢复规则 |
| 对应 `*_test.go` | 补齐第 5 节行为级回归测试 |

---

## 8. 不要做

- 不要仅把 0.99 改成更低数值来“消警”。
- 不要删除真实出口抖动的观测和告警。
- 不要全局关闭 AlertSink 或拉长全局 Dedup TTL，避免掩盖支付卡单/对账失败。
- 不要关闭 TLS 证书、hostname、SNI 校验。
- 不要固定微信 IP、写 `/etc/hosts`、使用消费级代理/VPN 或 `curl -k`。
- 不要把 `-1` 当真实耗时参与比较或继续直接展示给用户。
- 不要用只断言私有计数器、日志出现、随机循环或 sleep 的低价值测试替代行为契约。
- 不要宣称“告警修复”等于“微信出口网络根因关闭”。

---

## 9. 给 Grok 的一句话任务

> 修复支付 SLI 的采样真实性与告警生命周期：TLS 只统计真实 cold handshake 成败，排除 DNS/TCP/reused；主备域真实分开保温；Prepay sync/bg 按逻辑操作记录 P95；使用有最小样本和连续窗口的滚动指标；warm-only 降为 Warning，Critical 保留给真实 Prepay/breaker；异常只在状态迁移及长周期 reminder 通知，恢复发一次 recovery，并补齐确定性行为测试。
