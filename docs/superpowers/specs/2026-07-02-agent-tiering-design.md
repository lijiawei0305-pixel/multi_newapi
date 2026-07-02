# 代理分层(level 档位驱动)设计 spec

- **日期**: 2026-07-02
- **归属**: 我(Claude)实现(与"对账记录"并行分工)
- **状态**: 设计已批准,待实现

> 本文自包含。核心:把"三类代理静态标签"改为"**等级驱动的能力门禁**" —— 普通代理是基础档,管理员判断"干得好"后手动升档,才解锁独立站点(子域名 + 自定义域名 + 品牌/个性化)。

## 1. 背景(代码盘查结论)

- 三类代理 `type` = `normal`/`oem`/`api`(`internal/agent/model.go:11-20`,存 `agent_profiles.type`,该表以 `tenant_id` 为主键、与 tenant 1:1)。
- **`type` 什么都不 gate** —— 全仓库 grep `type=="oem"/"api"` 零结果。所有代理能力(自定义域名、品牌页、用户组倍率、兑换码、推广、分润…)都按"是不是租户 owner"(`AgentOwnerAuth` / `is_agent_owner`)开,与类型无关 → **每个代理都已拥有完整 OEM 套件**;三类纯装饰。
- `api` 是**死值**(除 `.Valid()` 外无任何代码引用)。
- `agent_profiles.level`(int)字段**已存在、管理员可编辑、已持久化,但从不被读来 gate 任何东西**(inert)—— 现成的空挂钩。
- **子域名** `<slug>.wedreamhub.com` 在 `tenant.Create`(`internal/mtwire/agent.go:475-484`)对**每个**代理自动发。
- 命名坑:代理 `level`(本设计要用)≠ 用户 `tier`(下级用户分组 default/vip,`distribution.go` 在用)。

## 2. 目标(用户愿景)

最普通的代理是基础档;**"干得好"(管理员判断)才升档解锁"独立开展"** —— 子域名 + 自定义域名 + 品牌/个性化。

## 3. 已确认的决策

| 维度 | 定论 |
|---|---|
| **模型** | `level` 档位驱动(0 普通 / 1 独立);**删** `type` 字段/枚举/下拉框,只剩档位 |
| **升档** | 纯管理员手动(页面展示充值/分润/下级数辅助判断) |
| **L0 普通** | 无子域名,靠主站推广链接拉人;有 用户组倍率/兑换码/推广/下级/分润/tokenplan |
| **L1 独立** | 管理员升档时**发子域名** + 解锁自定义域名 + 品牌/个性化 |
| **API** | 移出范围,只建 `can_api` 占位字段(开放API 当独立后续项目) |
| **迁移** | 现有代理全置 L1(不拉黑现有站点) |

## 4. 能力 → 档位 gate 矩阵

| 能力 | L0 普通 | L1 独立 | 改动点 |
|---|:--:|:--:|---|
| 用户组倍率 / 兑换码 / 推广 / 下级 / 分润 / tokenplan | ✅ | ✅ | 不变(仅 owner) |
| **子域名** `<slug>.wedreamhub.com` | ❌ | ✅ 升档时发 | 改 `agent.go:475`/`tenant.Create` |
| **自定义域名绑定** | ❌ | ✅ | 后端路由 + 前端守卫 gate `level≥1` |
| **品牌 / 个性化**(site-branding) | ❌ | ✅ | 同上 |
| 开放 API | `can_api`(本期恒关) | `can_api`(本期恒关) | 正交占位,不 gate |

## 5. 设计

### 5.1 数据模型
- `agent_profiles.level`(已存在,int):**0=普通 / 1=独立**。能力 gate 的唯一真相。
- 新增 `agent_profiles.can_api`(bool,默认 false):占位,本期不 gate 任何东西。
- **删除** `agent_profiles.type` 列 + `AgentType` 枚举(`model.go:11-30`)+ 前端 `agentTypeValues`(`web/default/src/features/agents/types.ts:33-34`)及所有引用。
- **迁移**:现有 `agent_profiles` 全部 `level=1`;删 `type` 列。

### 5.2 后端
1. `internal/agent`:`AgentParams` 已有 `Level`;加 `CanAPI`。删 `Type`/`AgentType`/`SetAgentType` 中与 type 相关逻辑。加 gate helper `AgentLevel(ctx, tenantID) (int, error)`。
2. **子域名 gate**:改 `agent.go` 创建流程 —— L0 **不发**子域名(创建 tenant 时不做子域名 host 映射);管理员 `PATCH /api/admin/agents/:id` 把 `level 0→1` 时**才** provision `<slug>.wedreamhub.com`。
3. **能力 gate**:`router/mt-router.go` 的 `agentSelf` 组里 —— 自定义域名(`:104-107`)+ site-branding(`:109-111`)那几条,加 `RequireAgentLevel(1)`;`custom_domain.go` / `siteconfig.go` handler 入口再校验 level(纵深)。
4. **迁移脚本**:现有 agent_profiles 全 `level=1`;删 type 列(`internal/agent/gormrepo` 迁移 + 建 `can_api`)。
5. 验证 L0 靠主站推广链接(`HandleAgentCreateChannel` / 推广归属)拉下级这条路通(无子域名也能归属)。

### 5.3 前端
1. admin 代理抽屉(`web/default/src/features/agents/components/agent-mutate-drawer.tsx:252-296`):删 `Normal/OEM/API` 选择器 → 换**档位选择(普通/独立)**;旁边展示该代理关键指标(充值/分润/下级数,辅助管理员判断升不升;缺接口则补)。
2. 列表 badge(`agents-columns.tsx:64-89`)由 `level` 派生显示("普通代理"/"独立代理")。
3. 删前端 `agentTypeValues` 枚举及引用。
4. 代理自助侧栏 + 路由守卫:`web/default/src/hooks/use-sidebar-data.ts:157-214` 里 Custom Domain / Site Branding 从 `agentOwnerOnly` 收紧为 `owner && level>=1`;路由守卫(`routes/_authenticated/custom-domain/index.tsx`、`site-branding/index.tsx`)非达标重定向"未解锁"提示页/403。
5. `/api/tenant/agent-context`(`web/default/src/lib/agent-context.ts` → 后端 handler)响应在 `{is_agent_owner}` 上加 `{level}`(+ `can_api` 占位),供前端 gate。

## 6. 测试(TDD,先写测试)
- level gate:L0 拒 / L1 过 自定义域名 + site-branding(后端)。
- 升档 L0→L1 发子域名;L0 创建无子域名。
- 迁移:现有代理全 `level=1`。
- `can_api` 占位不影响任何 gate。
- 删 `type` 后创建/编辑代理正常(无残留引用)。

## 7. 验证
- 服务器构建 + 部署测试栈(`newapi_test`、3100;**构建在服务器**)。
- **Playwright E2E**:admin 升某代理档 → 该代理登录看到子域名/自定义域名/品牌页解锁;L0 代理看不到这些、只有推广链接可用。CF/Turnstile 挡则**临时关 → 测 → 开回**(位置动前先确认)。

## 8. 风险 / 注意
- 子域名收回要**改租户创建流程**(不是加个 if),需验证 tenant 解析对"无子域名 tenant"不炸(现子域名是 tenant 的 host 解析键之一)。
- 命名坑重申:代理 `level`(本设计)≠ 用户 `tier`(下级分组,在用)。
- 与伙伴 reconcile 并行:本设计动 `internal/agent` / `internal/tenant` / `internal/siteconfig` / `custom_domain.go` / 前端 `agents`;reconcile 动 mtwire `reconcile_*` + payment。`router/mt-router.go` 两边都改(不同路由组,冲突可控)。
- `type` 删除是破坏性 schema 变更 —— 迁移前确认没有其他代码/报表依赖 `agent_profiles.type`。

## 9. 金流模型 v2(三线分离 · 差异化只在消耗计费层;L0 提成 / L1 差价)

> **本节取代 v1("L0 提成+9折 / L1 差价"版本)。** v1 的"邀请 9 折"已明确移出本轮范围(§9.5、§9.8)。
> v2 的核心变化:不再是"L0/L1 互斥选一种赚钱方式"这一句话能概括的——而是**三条钱线各自独立结算**(§9.1),差异化经营的口子**只开在"模型消耗"这一条线**,充值与 tokenplan 两条线保持现状、本轮不动。L0/L1 的"二选一"只发生在**消耗线内部**(提成 vs 差价,§9.9)。
> 以下内容为已确认设计(2026-07-02 与用户对齐),**如实写出**,含一处已知与"谁设谁担"表述有出入的地方(§9.6 末尾显式标注,未静默假设)。
>
> **2026-07-02 追加 3 处 v3 修订(用户再次确认,原地编辑,非整体重写;详见各小节)**:① §9.6.1(新增)—— 代理可自设「per-tenant vip 力度」覆盖(与既有代理设模型分组卖价"同一套机制",平行开在层级轴),解决了 §9.6 原先标注的"谁设谁担"差距;② §9.4 差价入账公式相应改为按**实际生效的组合倍率**(`chargedGroupRatio`,含代理自设 vip)计算,代理由此真正为自己给出的 vip 折扣买单(§9.6 的"记账推导"随之改写,§9.8/§9.9/§9.10 同步勘误);③ §9.11(新增)—— 邀请链接折扣率字段预留(schema-only,本轮不接入计费)。

### 9.1 三条钱线(分离,互不混算)

1. **充值**:1 元 = 1 美元,全平台统一汇率,**不因代理分层而不同**(现有单一汇率;`recharge_spread` 现有 0 值不变;本轮不动)。
2. **Tokenplan(套餐)**:代理自主定零售价,与管理员定价无关(现有 `Retail.SetListing` / `tokenplan_spread` 通路;本轮不动)。
3. **模型消耗(调用消耗)**:本轮唯一的差异化战场——四档价格阶梯(§9.3)+ 差价入账(§9.4),下同各节详述。

### 9.2 消耗计费公式(每次调用)

```
charge = token数 × ModelRatio × 分组倍率 × 用户层级优惠
```

- **`ModelRatio`**:管理员按模型设的基准倍率(分组倍率=1 时即"平台直客价"的那一档)。
- **`分组倍率`**:对某个代理的用户而言 = **该代理的卖价**(代理经既有 `HandleAgentSetGroupRatio` → `tenant_groups` 设定的 per-tenant 模型分组倍率);未设覆盖则为平台基准(`GroupRatio[分组]`,**已全平台重置为 1**)。
- **`用户层级优惠`**:vip 折扣(既有 2D 用户层级:`default`=1、`vip`<1),经 `resolveModelGroup2D` 的 `tier` 分量。

**此公式已是现状实现**——`internal/mtwire/modelgroup.go` `resolveModelGroup2D`:`ratio = tier(用户层级) × modelFactor(分组倍率,命中代理覆盖用覆盖值/否则平台基准)`,`relay/helper/price.go` `HandleGroupRatio` 已接入 `/v1` 计费热路径。**v2 完全不改计费/扣费路径本身**,只改"平台和代理之间怎么分这笔已经收上来的钱"(§9.3-§9.5)。

### 9.3 四档价格阶梯(消耗计费,新增的分账依据)

```
平台成本(cost)  <  代理底价(admin-set,逐代理)  <  代理卖价(agent-set,≥ 底价)  <  平台直客价(ModelRatio × 1)
```

- **平台赚**:`底价 − cost`(代理活跃贡献的批发差,不因代理是否设卖价而变化——只要代理在跑量)。
- **代理赚**:`卖价 − 底价`(代理自主加价空间,§9.4;仅 L1)。
- 卖价的地板(下限)本轮从"全局 `modelGroupBaseline`"改为"**该代理自己的底价**"——见 §9.7 新字段。

### 9.4 L1 高级代理 —— 差价入账

- **卖价**:沿用既有 `HandleAgentSetGroupRatio` → `tenant_groups`(不改端点、不改存储结构);地板本轮从"全局基准"改为"**该代理自己的底价**"(§9.7)。
- 每次其用户消耗,记入代理钱包(**2026-07-02 v3 修订公式**——原公式漏了层级优惠,见下方"为什么改公式"):

  ```
  markup = rawUnits × (chargedGroupRatio − 底价)
         = chargedQuota − rawUnits × 底价
  ```

  其中:
  - `chargedGroupRatio` = 本次计费**实际生效**的组合倍率(= 层级优惠 × 分组倍率,即 `tier × modelFactor`;`tier` 现在可能是该代理自设的 vip 力度覆盖,§9.6.1)——直接取自结算时 `relayInfo.PriceData.GroupRatioInfo.GroupRatio`,不重新查一遍"当前"倍率(避免与代理事后改卖价/vip 力度的竞态);
  - `rawUnits = chargedQuota ÷ chargedGroupRatio`,精确反推出与任何折扣无关的"原始消耗"(token数 × ModelRatio);
  - `chargedQuota` = 用户本次实付的计费额(quota 单位);
  - markup 结构上 clamp ≥ 0(§9.9 红线③,同时是 §9.6.1 floor 承诺的结算侧兜底)。

  收益来源 `SourceRatioMarkup`(🆕),requestID 幂等(§9.9)。

- **为什么改公式(v2 → v3)**:v2 的 `markup = (卖价 − 底价) × token数 × ModelRatio` 里,"卖价"是代理设的分组倍率本身、不含层级优惠——所以无论该用户是不是 vip、折扣多深,代理挣到的差价完全不变。这在 v2 阶段是刻意的(v2 还没有"代理自设 vip 力度"这个机制,vip 折扣只可能来自平台全局倍率,§9.6 因而如实记录"平台兜底"的结果,并标注为一处需用户确认的差距)。**现在(§9.6.1)代理可以自己设 vip 力度了**,如果公式还按老口径算,代理自己决定打折、成本却让平台兜底,直接违反"谁设谁担"。新公式把不含层级的"卖价"换成实际生效的 `chargedGroupRatio`,代理自设的折扣因而如实体现在自己的差价里——折扣越深,这一单赚得越少,直到跌到地板(§9.9 红线③兜底,不会倒扣)。
- **验证(用户确认的例子)**:底价 0.5、卖价 0.8、代理自设 vip 力度 0.9 → vip 用户实付 `0.8×0.9=0.72`;代理这一单赚 `0.72−0.5=0.22`(同一用户不打折时 `chargedGroupRatio=0.8`,代理赚 `0.8−0.5=0.3`)。折扣越深、代理赚得越少,正是"谁设谁担"要的效果。
- **无**提成(`consume_commission`/`tokenplan_commission`)——与 L0 在消耗线上互斥,按 level 二选一(§9.9)。

### 9.5 L0 普通代理 —— 提成(返点,公式不变)

- **不能**自设卖价:`HandleAgentSetGroupRatio` gate `level≥1`(已实现 `RequireAgentLevel`/`ensureAgentLevel`,Task 3;L0 调用 → 403 `AGENT_LEVEL_LOCKED`)。
- 其邀请用户按**平台直客价**付费(分组倍率 = 平台基准,无代理覆盖可用)。
- L0 赚:既有 `consume_commission`(`commission_ratio × 官方原价消耗额`),**公式不变、不改动**。
- **本轮明确不做**"邀请用户 9 折"(v1 遗留概念,已确认移出范围——不新建 `invite_discount_rate` 配置项,不建折扣退款通路,不改 `creditConsumeCommission` 的 L0 分支之外的任何用户侧退款逻辑)。

### 9.6 vip 优惠与"谁设谁担"

- vip 折扣就是 §9.2 的"用户层级优惠"因子(`GroupRatio['vip']`,全局配置,`default`=1、`vip`<1)——**完全复用现状,本轮不改倍率解析路径**。
- **主站 vip**:管理员设全局 vip 倍率;主站(非代理)用户命中该层级 → 折扣成本由**平台**承担(无代理在场,天然如此,无需额外设计)。
- **代理下级 vip**:代理经既有 `HandleAgentSetUserTier`(`PUT /api/tenant/users/:id/tier`)把自己名下用户标记为 vip——这是代理现有的"决定谁享受折扣"权限,**本轮不新增、直接复用**。折扣的**倍率本身**仍是同一份全局 `GroupRatio['vip']`:当前代码没有"代理自设一份独立于平台的 vip 倍率"的机制(`resolveModelGroup2D` 只在 `usingGroup`(模型分组)维度查 `tenant_groups` 覆盖,从不在 `userGroup`(层级)维度查——即没有"层级覆盖"这个概念)。
- **v2 记账推导(2026-07-02 前;以下保留作历史对照,§9.6.1 起为 v3 修订)**:`SourceRatioMarkup` 的 `token数×ModelRatio` 由「实际计费额 ÷ 实际生效的分组倍率(tier×分组倍率,来自结算时的 `relayInfo.PriceData.GroupRatioInfo.GroupRatio`)」精确反推——vip 折扣已经被这一步除干净。也就是说(**v2 阶段**):代理每单位原始消耗换来的差价与该用户是否 vip 无关;vip 折扣造成的收入缺口,结构上始终由平台侧兜底(平台"底价层"的实收变少),不论这个 vip 用户是被代理动过层级、还是主站原生 vip 用户。
- **✅ 已解决("whoever sets it bears it"差距,2026-07-02 用户确认)**:v2 曾标注一处已知差距——若要"代理把自己名下用户设为 vip 时,折扣成本从代理自己的差价里扣、绝不侵蚀平台的底价层"这条更强的保证,需要 (a) 一个代理可自设、独立于平台全局 vip 倍率的新机制,且 (b) §9.4 的差价公式相应改用实际生效的组合倍率而非裸卖价。**这两者本轮都已确认要做**:分别是下方 §9.6.1(新增的 per-tenant 层级覆盖)与 §9.4 已改写的公式。上一条"v2 记账推导"描述的"平台兜底"结果,**只在代理没有自设 vip 覆盖时依然成立**(§9.6.1 的"Default=1"分支);一旦代理自设覆盖,折扣成本转为由代理自己的差价吸收,不再由平台兜底。主站直客(无代理)的 vip 折扣不受任何影响,继续 100% 由平台承担(无代理参与)。

### 9.6.1 代理自设 vip 力度(per-tenant 层级覆盖,2026-07-02 confirmed,解决 §9.6 差距)

> Change 1(用户确认)。与既有"代理自设模型分组卖价"(`HandleAgentSetGroupRatio` → `tenant_groups`,只在**模型分组轴**生效)**同一套机制**,新开在**层级轴**——两条轴现在都支持"代理可自设 per-tenant 覆盖,命中则代替平台基准"。

- **两个人设、两条线、不重叠**:
  - **平台(管理员)**:既有全局 `GroupRatio['vip']`——只对**主站直客**(无代理归属,`tenant_id=0`)生效,折扣成本 100% 平台承担。本轮不改这条线。
  - **代理(🆕)**:per-tenant 层级覆盖——只对**该代理自己的下级用户**生效,折扣成本由**该代理自己的差价**吸收(§9.4 修订后的公式)。默认值 = **1(不打折)**,不是平台基准;代理不主动设,其下级 vip 用户按原价付费(见下方"No overlap")。
  - **No overlap**:代理下级用户的层级折扣**只能**来自该代理自己的覆盖,**绝不**回退平台全局 vip 倍率(即便代理没设覆盖,也不会"顺便"吃到平台的 0.8);主站直客的层级折扣**只能**来自平台全局,代理侧的覆盖对主站直客零影响。每一方只对"自己的用户"设折扣、只为"自己的用户"担成本。
  - **level 门禁**:设覆盖的写端点 `level>=1`(复用 `ensureAgentLevel`,同 `HandleAgentSetGroupRatio`,Task 3 既有机制,不新建 gate)。**L0 代理不能自设层级覆盖**——L0 下级用户被 `HandleAgentSetUserTier`(既有、不受 level 门禁、本轮不改)标记为 vip 后,层级倍率解析进入"代理下级用户但未配置覆盖"分支;**本轮实现选择让这种情况回退平台全局(与今天完全一致,对 L0 零行为变化)**,而不是套用"No overlap"变成 1。**⚠️ 这是一处需要用户确认的范围判断,不是不言自明的推论**:另一种同样自洽的替代方案是"No overlap"不分 level、对所有代理下级用户一律生效(L0 下级 vip 用户也会从"吃平台折扣"变成"tier=1 无折扣")。两者是产品决策,本节按"对 L0 零影响"的保守方案如实写出,不擅自选另一种。

- **存储:与卖价覆盖同一张表、不同命名空间**——复用既有 `tenant_groups`(`UNIQUE(tenant_id, group_name)`)+ `UpsertGroup`/`LookupEnabledGroupRatio`(不建新表、不建新方法),但 `group_name` 一律加 `tier:` 前缀(如 `tier:vip`)写入/查询——模型分组轴(`HandleAgentSetGroupRatio`)用裸名(如 `claude-kiro`)写入同一张表。两条轴共享同一张表 + 同一个 unique key 空间,前缀是零成本的永久隔离,不依赖"没人把模型分组取名叫 vip"这种命名约定。

- **设端点**:新增 `PUT /api/tenant/tier-ratio/:tier`(镜像 `HandleAgentSetGroupRatio`,同一文件 `internal/mtwire/distribution.go`),`level>=1` 门禁;`tier` 必须是"可代理覆盖层级"(`allowedAgentTiers` 去掉 `default`——今仅 `vip`)且不能是已登记模型分组名(双向互斥校验,防两条轴串号),否则复用既有 `AGENT_TIER_INVALID`(`errAgentTierInvalid`,`HandleAgentSetUserTier` 已在用同一错误码语义)。配套 `GET /api/tenant/tier-ratio` 列出当前状态(未覆盖展示 1,不是平台参考值——避免 UI 暗示"没设=用平台的")。

- **读端(计费热路径)**:`resolveModelGroup2D`(`internal/mtwire/modelgroup.go`)的层级轴从"直接 `groupRatioOf(userGroup)`"改为经新函数 `resolveTierRatio` 决定:非"可代理覆盖层级"(如 `default`)、或用户不归属任何 L1 代理(主站直客 / L0 代理下级)→ 平台全局 `groupRatioOf(userGroup)`(既有行为完全不变);归属 L1 代理且该层级有 enabled 覆盖 → 用覆盖值;归属 L1 代理但未配置 → 1。**唯一发生数值变化的组合是"L1 代理 + 该代理下级用户 + 可覆盖层级(vip) + 未设覆盖"**(v2 时值为平台基准如 0.8,v3 起变为 1)。

- **floor(卖价×vip 力度 ≥ 该代理底价)**:**结构性、强制性的保护落在结算/入账那一端**——§9.4 修订后的公式直接用 `chargedGroupRatio`(已经把代理自设 vip 力度乘进去的实际生效倍率)与该代理"底价"比较(复用 `consumeFloorRatio` 的比较对象,§9.7);一旦 `chargedGroupRatio ≤ 底价`,markup clamp 到 0(§9.9 红线③),绝不倒扣——这个 clamp 本来就是 §9.7 为"管理员事后调底价"这类配置漂移设的防线,vip 力度导致的"实际卖价"下探是**同一类漂移**,天然被同一道防线接住,不需要另开一条判断逻辑。**本轮不在"设 vip 力度"这个写端点做预防性的跨模型分组穷举校验**(即不会在代理设 vip 力度时,反过来检查该代理名下所有已设卖价的模型分组是否都还压得住地板)——这与"管理员改底价"端点本身也不做这种穷举校验是同一个先例(两者是同一类"两个独立旋钮、谁后设谁可能打破对方前提"的问题,本项目现有选择是"结算时兜底,不在设置时穷举预防")。这是本节一处未跑穷举预防校验的已知留白,不是遗漏,已在配套报告中标出供确认。

- **部署顺序提醒**:§9.4 的公式修订(Task 13)本身,在本节机制(Task 16)上线前,就已经让"任何降低 chargedGroupRatio 的层级优惠"影响代理差价——而 Task 16 上线前,L1 代理下级 vip 用户唯一可能吃到的层级优惠仍是**平台全局** vip 倍率。也就是说,若两者分开上线,会有一段过渡期让代理差价随平台调整全局 vip 折扣而波动——这不是"谁设谁担",方向反了。**两者逻辑上是一对,应在同一次上线中一起部署**;分开上线前须与用户确认过渡期是否可接受。

### 9.7 新增字段:底价倍率(BottomPriceRatio)

- `AgentParams` 新增 `BottomPriceRatio float64`——管理员按代理设,**独立于**既有 `PackageDiscount`/`DiscountFloor`(那两个字段是 tokenplan 套餐折扣的既有保护线,服务 §9.1 第②条,v2 不动、不复用、不混淆)。
- 语义:`0` = 未配置 → 消耗计费地板回退平台基准 `modelGroupBaseline(group)`(与引入本字段前的历史行为完全一致,安全默认,不改变任何未配置代理的现状行为);`>0` = 管理员为该代理设的消耗计费底价倍率——同时是 (a) `HandleAgentSetGroupRatio` 卖价下限、(b) `SourceRatioMarkup` 差价入账公式里的减数。**(a)(b) 必须用同一口径**(同一个 `consumeFloorRatio` 辅助函数),否则会出现"卖价被下限挡住却在差价入账时按 0 底价把整单倍率差都算成代理利润"的记账错误(严重程度:平台侧无兜底、代理侧超额得利)。
- 跨全部模型分组统一一个比例(不像卖价那样逐分组单独设)——底价语义是"对这个代理的批发折扣比例",而它相乘的对象(`ModelRatio`)已经是逐模型定价,无需再逐分组重复设底价。
- **与 §9.6.1 的关系(v3 追加)**:底价还兼任 §9.6.1 vip 力度 floor 比较的减数——`chargedGroupRatio(含代理自设 vip)− 底价`,由 §9.4 修订后的公式与 §9.9 红线③的 clamp 共同兜底,不需要为 vip 力度另设一套地板字段/口径。

### 9.8 复用 vs 新建

- ✅ **复用**(不改动或只读引用):充值统一汇率 · tokenplan 零售定价通路 · 消耗计费公式本身(`resolveModelGroup2D`/`resolveTenantGroupRatio`/`modelGroupBaseline`)· `HandleAgentSetGroupRatio` 端点与 `tenant_groups` 存储 · L0 `consume_commission`/`tokenplan_commission` 公式 · `RequireAgentLevel`/`ensureAgentLevel`(level gate,Task 3)· `HandleAgentSetUserTier`(代理选谁享受 vip)· `AgentRepo.AppendEarning` 的 requestID 幂等机制 · `EarningSource` 枚举模式与 `AgentParams.Validate()` 的错误码约定。
- 🆕 **新建**:`AgentParams.BottomPriceRatio` 字段(§9.7)· `consumeFloorRatio` 统一地板口径函数 · `HandleAgentSetGroupRatio`/`HandleAgentListGroups` 地板改按代理底价 · `SourceRatioMarkup` 收益来源常量 · `creditRatioMarkup` 差价入账实现 · `creditConsumeCommission` 按档二选一 dispatcher(拆出 `creditL0Commission`)· `agenthook.ConsumeCommission` 钩子签名扩展(新增 `usingGroup`+`chargedGroupRatio` 两个参数,供差价入账精确反推 token×ModelRatio,详见实现任务"为什么必须改签名")。
- 🆕 **新建**(追加,v3,2026-07-02):代理自设独立于平台的 per-tenant vip 层级覆盖(§9.6.1)——新端点 `HandleAgentSetTierRatio`/`HandleAgentListTierRatios`、`resolveModelGroup2D` 层级轴新增的 tenant_groups 查询(`resolveTierRatio`)、`tier:` 前缀命名空间隔离;§9.4 差价入账公式相应改用 `chargedGroupRatio`。*(历史注:此项 v2 版本曾列在下一条"明确不做"里,标注为"需用户后续确认是否要做,本轮不擅自实现"——2026-07-02 已确认要做,移至本条,不再属于"不做"范围。)*
- ❌ **明确不做**(本轮移出范围,不要静默漏做也不要意外做了):v1 的"邀请 9 折"整体废弃,不建折扣退款通路、不改任何用户侧 quota 退款逻辑(**注意区分**:§9.11 新增的按渠道 `discount_rate` 预留字段,只是 schema 占位,billing 本轮不读,不等于重新引入 v1 机制——两者不是一回事)。

### 9.9 按档 gate + 幂等 / 红线

- `HandleAgentSetGroupRatio` gate `level≥1`(L0 调用 → 403 `AGENT_LEVEL_LOCKED`,复用 Task 3 既有中间件/helper,不新建 gate 机制)。
- `creditConsumeCommission` 按 `level` 严格二选一:`level==0` → `creditL0Commission`(提成);`level≥1` → `creditRatioMarkup`(差价)。同一次消耗**绝不**同时产生 `consume_commission`/`tokenplan_commission` 与 `ratio_markup` 两类收益条目。
- **billing 红线**(本节任何新代码路径必须满足,验收硬指标):
  1. requestID 强幂等——落在既有 `AgentRepo.AppendEarning` 的 `(tenant_id, source_type, source_id)` 唯一索引上,同一来源重复调用不重复入账;
  2. best-effort + `defer recover()`——失败(含任何新增查询/计算失败)绝不阻断用户请求或核心 quota 扣减,与现有 `creditConsumeCommission` 的既有姿态一致,不降低现有保护;
  3. 差价 `markupQuota` 结构上恒 `≥ 0`——`卖价 ≥ 底价` 由 §9.7 的统一地板口径在"设卖价"时前置校验(`RATIO_BELOW_FLOOR`);`creditRatioMarkup` 在入账时**再做一次防御性复核**,且(v3 起)复核对象是**实际生效的组合倍率 `chargedGroupRatio`**(含代理自设 vip 力度,§9.6.1)而不是裸卖价——`chargedGroupRatio ≤ 底价` 就跳过入账。这一次复核同时兜住两类"设置时刻互相看不见"的漂移:① 管理员在代理已设好卖价之后才上调该代理底价;② 代理在已设好卖价之后才自设(或调深)vip 力度,导致某些 vip 用户的实付价跌破底价。两者是同一类问题、同一处代码兜底,不为②另开分支。复核不通过则跳过入账(不 panic、不倒扣、不阻断)。

### 9.10 测试(TDD,覆盖点清单)

- **底价倍率**:AgentParams/DTO/gormrepo 全链路 round-trip;`0` 回退平台基准、`>0` 生效为地板;`Validate()` 不因新增字段破坏既有"全零合法"契约(现有 `TestAgentParams_Validate` 的 `"all zero ok"` 用例必须继续通过)。
- **地板**:`HandleAgentSetGroupRatio` 用"该代理底价"(而非全局基准)当下限,击穿仍返回 400 `RATIO_BELOW_FLOOR`;`HandleAgentListGroups` 展示的 `Floor` 字段与实际生效下限一致(`PlatformRatio` 保持平台基准语义、与 `Floor` 分离);未配置底价的代理,地板行为与"本字段引入前"完全一致(不得让既有测试的期望值变化)。
- **gate**:L0 调 `HandleAgentSetGroupRatio` → 403 `AGENT_LEVEL_LOCKED`,且不落 `tenant_groups`;L1 → 通过(既有校验——下限/仅模型分组——照常生效)。同样,L0 调 §9.6.1 新增的 `HandleAgentSetTierRatio` → 403 `AGENT_LEVEL_LOCKED`。
- **差价入账**:`rawUnits × (chargedGroupRatio − 底价)` 数值精确(v3 修订公式,§9.4)——含 vip 场景验证该差价**随代理自设的 vip 力度成比例收缩**(不再是"与用户层级无关",那是 v2 的旧断言,已被 §9.6.1 取代);未设卖价覆盖 → 不入账(不得把"未覆盖"当"隐式底价"来算差价);requestID 重放不双记;`SourceRatioMarkup` 与 `consume_commission`/`tokenplan_commission` 按档严格互斥(同一次消耗的收益台账里只应出现其中一种)。
- **幂等 + 隔离**(红线直接对应的测试):钩子内部 panic 有兜底、不阻断调用方；主站用户 / 未归属用户(`tenant_id=0`)在任意档位逻辑下都不产生任何收益条目、不报错。
- **vip 安全**:构造 `chargedGroupRatio`(含 vip 力度)跌破底价的场景,验证 `creditRatioMarkup` 的结果绝不因此变负(结构性证明,而非仅个别用例断言,覆盖 §9.9 红线③的复核);验证"代理自担"这条 v3 新行为——同一代理、同样的原始消耗(rawUnits 相同),vip 用户(代理自设力度生效)比 default 用户产生**更小**的差价,且缩小幅度与代理自设的 vip 力度精确对应(不是 v2 的"platform 兜底、代理不受影响");主站直客(无代理)的 vip 折扣继续不产生任何代理收益条目(无代理参与,平台 100% 承担,不变)。
- **层级覆盖(§9.6.1 新增)**:`resolveModelGroup2D` 的层级轴——L1 代理下级 vip 用户未配置覆盖 → 1(不回退平台全局);已配置 → 用覆盖值;主站直客与 L0 代理下级两种情况数值上与"本节引入前"完全一致(不得让既有测试的期望值变化,除已显式更新的那一个用例);`tenant_groups` 的 `tier:` 前缀命名空间与模型分组轴互不串号(同一租户同时设卖价覆盖与 vip 力度覆盖,两条轴互不干扰)。

### 9.11 邀请折扣(discount_rate):按渠道预留,本轮不激活

> Change 3(用户确认)。**只建 schema/设计占位,不接入计费**——与 §9.5 末尾"本轮明确不做邀请 9 折"**不矛盾**:那条禁止的是"v1 式、全局统一的邀请折扣退款通路";这里预留的是"按渠道各自独立、未来才可能启用"的一个字段,今天不产生任何折扣、不产生任何退款,billing 完全不读它。

- **字段**:`promotion.Channel`(`internal/promotion/model.go`)新增 `DiscountRate float64`,随渠道一起持久化(`agent_promotion_channels` 表新列 `discount_rate`,`gormrepo.channelRow`);默认 **1.0 = 不打折**。新建渠道(`promotionService.CreateChannel`)固定写入 1.0——本轮**不**暴露任何设置该值的入口(不加建渠道参数、不加改渠道端点),纯预留。
- **为什么现在就要建字段,而不是等激活了再加**:同一代理名下的多条推广/邀请链接(`agent_promotion_channels`,一个代理可建多条渠道)未来要能各自配不同折扣力度——这个"按渠道"的粒度如果不趁现在(建渠道 schema 的同一轮)加进表结构,以后要补一次破坏性迁移;现在加一列默认值安全(`not null default 1`,不改变任何现有行为),成本远低于以后回填。
- **谁设、谁担(占位,留待激活时决策,本轮不预判)**:参照 §9.6.1 vip 力度的"同一套机制、谁设谁担"范式——邀请折扣如果未来激活,大概率也应该是"哪一方能设某条渠道的折扣,就由哪一方的差价/提成吸收成本"(如代理自己的推广渠道打折,成本从代理自己的收益里扣;若开放平台给主站自己的推广渠道设折扣,则平台自担)。**这只是留一个自洽的类比供未来参考,不是本轮决策**——具体"谁能设"、"折扣如何影响计费"、"是否需要新的 `SourceXxx` 收益类型"等,本轮完全不设计,留待用户明确要激活时再单独立项。
- **billing 现状(明确)**:计费公式(§9.2)、四档价格阶梯(§9.3)、差价入账(§9.4)均**不读** `DiscountRate`;它今天只是一列会随渠道一起被创建/查询/列表返回的静态数据,任何 `/v1` 请求路径都不会查询这张表来影响价格。
