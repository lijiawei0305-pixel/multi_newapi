# deploy/demo — 演示手册（Phase 1 交付）

> 平台已正式上线：**https://tokendream.wedreamhub.com**（Cloudflare Full strict）。
> 本目录提供 **一次真实支付跑通主流程** 的交互式演示 `demo.sh` 与 **三视角浏览器走查** 指引。
> 账目对账见 [`../ops/reconcile.sh`](../ops/reconcile.sh)。
>
> **运行位置**：`demo.sh` 在**服务器**跑（`ssh newapi628`）—— 它直连回环端口与 mysql 容器。
> **安全护栏**：`newapi_test` 自 2026-07-03 单栈收敛后是【唯一现网 / 生产栈】（原 `newapi_YFNf` 已删除）；`guard_target`（旧名 `guard_not_prod`）确认目标确为它、挡拼写误配。⚠ `demo.sh` 会创建真实订单、要求真实微信/支付宝付款并改变余额，默认拒绝运行；仅限受控验收，且必须显式设置 `ALLOW_REAL_PAYMENT_DEMO=1`。

---

## 1. 演示账号

> 仓库不提供任何默认口令。生产环境请由管理员手工创建/管理账号，并在运行脚本时从密钥管理器或交互输入注入 `ADMIN_PASS`、`AGENT_PASS`、`BUYER_PASS`。自动创建 `demoagent` 默认关闭，且生产环境禁止启用；仅本地 development 可同时设置 `MT_ENABLE_DEMO_AGENT_SEED=true` 与一个 16-20 字符的新 `MT_DEMO_AGENT_PASSWORD`。

| 角色 | 用户名 | 口令 | 身份 | 说明 |
| --- | --- | --- | --- | --- |
| 主站管理员 | `admin` | `ADMIN_PASS`（安全注入） | root（user_id=1） | 全局：套餐/代理/提现审核 |
| 代理 owner | `demoagent` | `AGENT_PASS`（安全注入） | 租户 `tokendream`（id=2，owns tenant 1） | 分销：上架/收益/提现/自助 |
| 终端用户 | `chanuser1` | `BUYER_PASS`（安全注入） | 归属 tokendream（user_id=3） | 买套餐 / 充值 / 调 /v1 |

- 多租户入口域名：**tokendream.wedreamhub.com**（代理 `demoagent` 的站点）。
- 金额口径：`*_cny`=人民币，`*_usd`/quota=美元（**$1 = 500000 quota**），汇率 7.3。

---

## 2. 浏览器主流程（三视角）

> 登录页：`https://tokendream.wedreamhub.com/sign-in`。登录后切换账号请先右上角登出。
> 下列路径均为 `https://tokendream.wedreamhub.com<路径>`。

### 2.1 买家视角（`chanuser1`）
| 步骤 | 路径 | 点哪 / 看什么 |
| --- | --- | --- |
| 浏览套餐 | `/plans` | 6 档套餐卡（零售价 / 原价划线 / 折扣角标 / 推荐高亮 / 月限额）；点 **mini「立即购买」** |
| 完成支付 | 购买弹出支付页 | 使用主站进程内微信/支付宝 SDK 返回的二维码或收银台真实付款 |
| 我的订阅 | `/plans` 下方 / `/subscriptions` | 看新订阅 **active** + 用量进度条（月限额 $220） |
| 充值 | `/wallet` | 输入 $1 → 选微信 → 确认支付 → 余额 quota +500000 |
| 调用 API | `/keys` → 复制 Key；`/playground` | 用 Key 调 `gpt-5.4-mini`，消耗从**订阅桶**扣减 |

### 2.2 代理视角（`demoagent`）
| 步骤 | 路径 | 点哪 / 看什么 |
| --- | --- | --- |
| 我的收益 | `/agent-earnings` | 顶部卡：可提现 / 冻结 / 累计；明细见 `tokenplan_spread`、`consume_commission` |
| 申请提现 | `/agent-earnings`（提现入口） | 输入金额 → 提交 → 可提现↓、冻结↑（待管理员审核） |
| 套餐上架改价 | `/agent-listings` | 调整零售价（受成本保护线约束，低于击穿线被拒） |
| 推广渠道 | `/promotion-channels` | 建渠道码；经此码注册的用户归属本代理 |
| 我的用户 | `/my-users` | 看下级终端用户列表 |
| 用户组倍率 | `/my-groups` | 设分组倍率（floor 校验） |
| 兑换码 | `/redemption-codes` | 批量生成兑换码（代理 quota 预扣） |

### 2.3 管理视角（`admin`）
| 步骤 | 路径 | 点哪 / 看什么 |
| --- | --- | --- |
| 套餐管理 | `/token-plans` | 6 档套餐 CRUD（售价/原价/月限额/成本/保护线/排序/上下架） |
| 子代理管理 | `/agents` | 设代理（普通/OEM/API + 成本价/折扣/分润/等级）、改代理 |
| 订阅监控 | `/subscription-monitor` | 当前租户订阅 + 用量 + 满额预警分级（warn/critical/exhausted） |
| 提现审核 | `/withdrawals` | 待审提现 → **通过**（仍保持冻结）→ 完成线下打款后 **标记已打款**（扣冻结）；或 **拒绝**（解冻退回） |

---

## 3. 跑 `demo.sh`（交互式真实支付演示）

```bash
ssh newapi628
cd /root/newapi-test/deploy/demo
read -rsp '管理员口令: ' ADMIN_PASS; export ADMIN_PASS; printf '\n'
read -rsp '代理口令: ' AGENT_PASS; export AGENT_PASS; printf '\n'
read -rsp '买家口令: ' BUYER_PASS; export BUYER_PASS; printf '\n'
ALLOW_REAL_PAYMENT_DEMO=1 PAY_PROVIDER=wxpay ./demo.sh
```

逐步打印「步骤 → 结果」，覆盖 **8 步主流程**（任一步失败 `set -e` 退出，退出码非 0）：

| 步 | 动作 | 关键打印 |
| --- | --- | --- |
| ① | `chanuser1` 登录 | user_id、归属租户 |
| ② | 买 mini 套餐 | `order_no`（SUB…）、应付 ¥119、`pay_url` |
| ③ | 使用真实微信/支付宝完成付款 | 主站回调验签后 SUB 进入 `activated` |
| ④ | 校验激活 + 分润 | 原生订阅 `active`、`amount_total`、代理 `tokenplan_spread ¥23.80` |
| ⑤ | `/v1` 调用 `gpt-5.4-mini` | 模型回复、`logs.billing_source = subscription` |
| ⑥ | `demoagent` 查收益 / 申请提现 | 可提现/冻结/累计、提现单 id |
| ⑦ | `admin` 审核通过后标记已打款 | 金额守恒：`total_earned = withdrawable + frozen + Σpaid` |
| ⑧ | 真实充值 $1 | quota Δ=500000、RCG 订单 `credited` |

**可覆盖参数**（环境变量）：`PLAN_ID`(默认 2=mini)、`WITHDRAW_CNY`(默认 10)、`RECHARGE_USD`(默认 1)、
`PAY_PROVIDER`(`wxpay`/`alipay`)、`PAYMENT_WAIT_SECONDS`(默认 300)、`BUYER_USER/BUYER_PASS`、
`AGENT_*`、`ADMIN_*`、`PLAN_MODEL`(默认 gpt-5.4-mini)。三项 `*_PASS` 均无默认值；`ALLOW_REAL_PAYMENT_DEMO=1` 是必需的真实资金确认开关。

```bash
ALLOW_REAL_PAYMENT_DEMO=1 PLAN_ID=3 WITHDRAW_CNY=20 ./demo.sh
ALLOW_REAL_PAYMENT_DEMO=1 PAY_PROVIDER=alipay ./demo.sh
```

**资金提醒**：每次跑都会新建套餐、充值和提现记录，真实金额与余额会累加；脚本不会伪造回调。演示 API Key 仅按名复用、不重复建。

### 预期结果（mini 套餐，全绿）
```
▶ 步骤 1  终端用户「chanuser1」登录
   ✓ 登录成功（user_id=3）
     归属租户                 TokenDream（slug=tokendream）
▶ 步骤 2  购买套餐（plan_id=2）→ 下单
   ✓ 下单成功
     order_no                 SUB…
     应付金额                 ¥119
▶ 步骤 3  完成真实付款并等待可信回调激活
   ✓ 支付事实已验签并幂等激活（SUB status=activated）
▶ 步骤 4  校验：原生订阅 active + 代理「demoagent」得 tokenplan_spread
   ✓ SUB 订单 activated → 原生订阅 id=… status=active
     amount_total             110000000 quota（= $220.0 额度上限）
   ✓ 代理获得 tokenplan_spread = ¥23.80（零售价 − 代理成本价）
▶ 步骤 5  …
     billing_source           subscription
   ✓ 计费来源 = subscription（走订阅桶扣减，未动钱包余额）
▶ 步骤 7  …
   ✓ 提现已审核并标记已打款
   ✓ 金额守恒成立：total_earned = withdrawable + frozen + Σpaid
▶ 步骤 8  …
   ✓ quota 增量 = 500000（= $1 × 500000，符合 $1=500k quota 口径）
══════ 演示全部 8 步通过 ══════
```

---

## 4. 跑对账（`reconcile.sh`）

演示后可核对账目一致性（**只读**，逐项 PASS/FAIL + 汇总，退出码=FAIL 数）：

```bash
cd /root/newapi-test/deploy/ops && ./reconcile.sh
```

校验：充值无卡单 / order_no 唯一、每个激活套餐单 ↔ 原生订阅+台账、分润求和==钱包累计、
提现冻结/金额守恒、无孤儿越权账。详见脚本头注释。

---

## 5. 排障

| 现象 | 排查 |
| --- | --- |
| 登录失败 | 三项 `*_PASS` 是否已从正确的密钥来源注入；`Host` 头是否 `tokendream.wedreamhub.com` |
| 步骤 ③/⑧ 等待超时 | 在支付商户后台确认订单事实；检查主站支付配置、回调公网基址、`/health/ready` 与支付对账页，勿伪造回调 |
| 步骤 ⑤ `/v1` 失败 | 上游渠道是否可达（`channels` 表）；`.env` 的 `UPSTREAM_*` 是否配置 |
| `billing_source≠subscription` | 该用户是否有 active 原生订阅（步骤 ②③ 是否成功） |
| 找不到 mysql 容器 | 测试栈是否启动：`docker compose -p newapi_test ps` |
