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
