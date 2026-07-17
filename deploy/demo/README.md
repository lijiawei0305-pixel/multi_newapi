# deploy/demo — 演示手册（Phase 1 交付）

> 平台已正式上线：**https://tokendream.wedreamhub.com**（Cloudflare Full strict）。
> 本目录提供 **一条命令跑通主流程** 的自动化演示 `demo.sh` 与 **三视角浏览器走查** 指引。
> 账目对账见 [`../ops/reconcile.sh`](../ops/reconcile.sh)。
>
> **运行位置**：`demo.sh` 在**服务器**跑（`ssh newapi628`）—— 它直连回环端口与 mysql 容器。
> **安全护栏**：`newapi_test` 自 2026-07-03 单栈收敛后是【唯一现网 / 生产栈】（原 `newapi_YFNf` 已删除）；`guard_target`（旧名 `guard_not_prod`）确认目标确为它、挡拼写误配。⚠ `demo.sh` 会在生产上造演示数据，仅限受控演示时运行。

---

## 1. 演示账号

> ⚠️ **以下为演示口令，部署后务必改密**（管理后台「用户管理」改密码，或 `demo.sh` 用环境变量覆盖）。

| 角色 | 用户名 | 口令 | 身份 | 说明 |
| --- | --- | --- | --- | --- |
| 主站管理员 | `admin` | （你设的密码，**不入库**；跑 demo 时 `export ADMIN_PASS=…`） | root（user_id=1） | 全局：套餐/代理/提现审核 |
| 代理 owner | `demoagent` | `demoagent123` | 租户 `tokendream`（id=2，owns tenant 1） | 分销：上架/收益/提现/自助 |
| 终端用户 | `chanuser1` | `chanuser123` | 归属 tokendream（user_id=3） | 买套餐 / 充值 / 调 /v1 |

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
| 完成支付 | 购买弹出支付页 | mock 模式点 **「确认已支付」**（真实模式为微信/支付宝收银台） |
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
| 提现审核 | `/withdrawals` | 待审提现 → **通过**（扣冻结、线下打款）/ **拒绝**（解冻退回） |

---

## 3. 跑 `demo.sh`（自动化演示）

```bash
ssh newapi628
cd /root/newapi-test/deploy/demo
./demo.sh
```

逐步打印「步骤 → 结果」，覆盖 **8 步主流程**（任一步失败 `set -e` 退出，退出码非 0）：

| 步 | 动作 | 关键打印 |
| --- | --- | --- |
| ① | `chanuser1` 登录 | user_id、归属租户 |
| ② | 买 mini 套餐 | `order_no`（SUB…）、应付 ¥119、`pay_url` |
| ③ | auth-service `/auth/mock/confirm` 确认 | HTTP 200 |
| ④ | 校验激活 + 分润 | 原生订阅 `active`、`amount_total`、代理 `tokenplan_spread ¥23.80` |
| ⑤ | `/v1` 调用 `gpt-5.4-mini` | 模型回复、`logs.billing_source = subscription` |
| ⑥ | `demoagent` 查收益 / 申请提现 | 可提现/冻结/累计、提现单 id |
| ⑦ | `admin` 审核通过 | 金额守恒：`total_earned = withdrawable + frozen + Σapproved` |
| ⑧ | 充值 $1（mock） | quota Δ=500000、RCG 订单 `credited` |

**可覆盖参数**（环境变量）：`PLAN_ID`(默认 2=mini)、`WITHDRAW_CNY`(默认 10)、`RECHARGE_USD`(默认 1)、
`BUYER_USER/BUYER_PASS`、`AGENT_*`、`ADMIN_*`、`PLAN_MODEL`(默认 gpt-5.4-mini)。

```bash
PLAN_ID=3 WITHDRAW_CNY=20 ./demo.sh        # 改买 solo、提现 ¥20
ADMIN_PASS='改后的口令' ./demo.sh          # 部署改密后
```

**幂等友好**：每次跑都是新订单 / 新充值（金额累加，无害）；演示 API Key 按名复用、不重复建。

### 预期结果（mini 套餐，全绿）
```
▶ 步骤 1  终端用户「chanuser1」登录
   ✓ 登录成功（user_id=3）
     归属租户                 TokenDream（slug=tokendream）
▶ 步骤 2  购买套餐（plan_id=2）→ 下单
   ✓ 下单成功
     order_no                 SUB…
     应付金额                 ¥119
▶ 步骤 4  校验：原生订阅 active + 代理「demoagent」得 tokenplan_spread
   ✓ SUB 订单 activated → 原生订阅 id=… status=active
     amount_total             110000000 quota（= $220.0 额度上限）
   ✓ 代理获得 tokenplan_spread = ¥23.80（零售价 − 代理成本价）
▶ 步骤 5  …
     billing_source           subscription
   ✓ 计费来源 = subscription（走订阅桶扣减，未动钱包余额）
▶ 步骤 7  …
   ✓ 金额守恒成立：total_earned = withdrawable + frozen + Σapproved
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
| 登录失败 | 口令是否被改（部署后默认密已变）；`Host` 头是否 `tokendream.wedreamhub.com` |
| 步骤 ③/⑧ HTTP≠200 | auth-service 是否健康：`curl 127.0.0.1:8180/auth/healthz` |
| 步骤 ⑤ `/v1` 失败 | 上游渠道是否可达（`channels` 表）；`.env` 的 `UPSTREAM_*` 是否配置 |
| `billing_source≠subscription` | 该用户是否有 active 原生订阅（步骤 ②③ 是否成功） |
| 找不到 mysql 容器 | 测试栈是否启动：`docker compose -p newapi_test ps` |
