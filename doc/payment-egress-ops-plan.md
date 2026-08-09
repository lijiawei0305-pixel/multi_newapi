# 支付出站（微信/支付宝）基础设施整改方案

> **状态：proposed — 默认不执行**  
> 配套 PAY-LAT-02。应用端缓解已落地；**出口时变抖动的根因需运维/云厂商处理**。  
> 不得在未授权下修改生产 DNS、防火墙、路由、NAT、EIP、代理或微信商户配置。

## 1. 问题结论（已核实）

- 非固定「坏 IP」：同一微信 A 记录在不同时刻 TCP/TLS 成功或超时。
- 主域名 `api.mch.weixin.qq.com` 与备域名 `api2.mch.weixin.qq.com` 在当前出口均抖动。
- 应用侧已：缩短同步预算、结构化 outcome、查单恢复、独立 TLS 安全 client、有界查单并发。
- **不得**宣称 PAY-LAT-02 网络根因已关闭，直至出口达标。

## 2. 对比测量计划（同一 app 镜像）

在**当前出口**与**候选合规亚洲/优质 BGP/专用 NAT 出口**上，用**同一 Docker 镜像**跑：

| 项 | 方法 |
| --- | --- |
| 目标 | `api.mch.weixin.qq.com`、`api2.mch.weixin.qq.com`（仅 hostname，不固定 IP） |
| DNS | dig A/AAAA + TTL；记录时间与源公网 IP |
| TCP/TLS | 带 SNI + 系统 CA 的 openssl/curl；记录冷连接 vs 复用 |
| HTTPS | `curl --http1.1` / HTTP2 最小 GET（非 Prepay 签名） |
| 采样 | 跨高峰/低峰至少 3 个时段；每时段 ≥30 次；**低样本不得报 P95** |
| 对比 | 宿主机 vs app 容器 |

## 3. 云厂商/ISP 工单证据包

每次失败样例保留：

1. 时间（含时区）
2. 源公网 IP / NAT
3. 目标 hostname
4. 当时 DNS A/AAAA/TTL
5. TCP/TLS/HTTPS 成功率与耗时分布
6. traceroute / mtr
7. 宿主机与容器对照

## 4. Payment 专用 egress（若采用）

**必须先安全与合规批准。** 设计约束：

- 端到端 TLS；**禁止** TLS MITM / 代理 CA
- 仅允许官方支付 hostname:443 白名单
- 不记录支付 body/签名/密钥
- 高可用 + 监控 + 明确回滚
- HTTPS CONNECT 可接受；明文旁路不可

## 5. 明确禁止

- 固定微信解析 IP / `/etc/hosts` / 私有 DNS 写死
- 用 IP 替换 URL、关闭 SNI/hostname/证书校验
- `curl -k`、Clash/消费级 VPN 作为生产支付路径
- 未授权改生产路由/NAT/区域

## 6. 切换门槛（需用户授权）

输出后再等授权：变更列表、风险、成本、回滚步骤、所需云权限。

## 7. Go / No-Go（出口）

| 指标 | 建议门禁 |
| --- | --- |
| TLS 握手成功率（主+备，跨时段） | 冷握手成功率 ≥ 99% |
| 冷连接 TLS P95 | < 2s |
| 复用连接 TTFB P95 | < 500ms |
| 真实签名 Prepay | **另需授权** 后测，不得用 curl 代替 |

### 7.1 应用内 SLI 契约（2026-08-09）

与 `internal/payment/realpay` 实现对齐（见 `doc/specs/2026-08-09-payment-tls-alert-audit-for-grok.md`）：

| 项 | 规则 |
| --- | --- |
| TLS 样本 | **仅 cold handshake**：`TLSHandshakeDone(err==nil)`=成功，`err!=nil` 或 start 未完成=失败；**reused / DNS/TCP 未达 TLS 不进分母** |
| 滚动窗口 | **30 分钟**；窗口内有效 cold TLS 样本 **≥30** 才 `available`；不足 → `n/a`，**不告警** |
| 成功率门禁 | `available && rate < 0.99` 记为坏窗口 |
| 告警状态机 | 连续 **2** 个坏窗口 → 发一次 degraded；持续异常 **6 小时** reminder 一次；恢复发一次 recovery；**不改**全局 Dedup TTL |
| 严重度 | 仅 warm/query、无逻辑 Prepay 样本 → **Warning**；有真实 prepay_sync/bg 样本 → **Critical**；breaker open **独立 Critical** |
| Prepay P95 | 按**逻辑** createPay 记一次（sync/bg）；样本 &lt;5 → `n/a (n=k)`，禁止展示 `-1` |
| 连接复用率 | 仅 `payment_http`（tracingRoundTripper）权威；warm 外层不双计 |
| 备域保温 | `api2.mch.weixin.qq.com` 探测必须真实访问备域（`preferBackup=true`） |

未达标：**应用端已缓解；PAY-LAT-02 基础设施根因仍未关闭。** 邮件不再轰炸 ≠ 出口网络已修好。
