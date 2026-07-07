# 设计:套餐满额续费提醒横幅（SubscriptionUsageBanner）

- **日期**：2026-07-07
- **状态**：设计已通过评审，待写实现计划
- **类型**：前端小功能（纯前端，零后端改动）
- **来源**：7c-2「满额提醒」的**用户侧**落地（brainstorming 2026-07-07；proposal §7「满额逼近预警可见」）

## 目标

用户活跃 tokenplan 套餐用量接近/达月度上限时，在控制台**任意已登录页顶部**显示可关闭横幅，一键跳购买页 `/plans`，促用户续费/加购。

## 非目标（YAGNI）

- **不动后端**：不接 `risk.NoteUsage/AlertSink`、不加通知表/端点（留给日后若要邮件/webhook 再上）。
- **不做邮件/站外推送**：本次仅站内（用户已选「站内全局提醒」）。
- **不覆盖钱包低余额**：本功能只针对套餐满额（月度上限）；钱包充值提醒是另一回事。
- **不做运营侧告警**：管理员/代理已有订阅监控页分级显示，不重复。

## 设计

### 组件与放置
- 新组件 `SubscriptionUsageBanner`，挂进 authenticated 布局顶部（`web/default/src/routes/_authenticated/route.tsx` 顶部固定区 / app-header 下方），使任意已登录页可见。

### 数据源
- 读**用户自己的活跃订阅**（与「我的套餐」进度条同源），react-query 拉取（复用现有 hooks，带缓存，不额外增加请求频率）。
- ⚠️ 代码中存在两套 DTO：买家前端 `userSubscriptionSchema`(`amount_used`/`amount_total`) 与管理端监控 `adminSubOut`(`used_usd`/`limit_usd`/`usage_pct`/`alert_level`，服务端已算分档)。**具体调哪个端点、用哪套字段，在实现计划阶段确认**——优先复用「我的套餐」已消费的那套，避免新增请求；若买家端点已返 `usage_pct`/`alert_level` 则直接复用、`computeUsageAlert` 退化为读取。

### 判定（纯函数 `computeUsageAlert`，可单测）
- 输入：活跃订阅列表。
- 对每条算 `ratio = used / limit`（limit ≤ 0 视为无限额 → 跳过该条）。
- 取**最高 ratio** 那条为「最紧急」，输出 `{ level, ratio, planName }`：
  - `0.8 ≤ ratio < 1.0` → `level = 'warn'`
  - `ratio ≥ 1.0` → `level = 'exhausted'`
  - 其它 → `level = 'none'`
- 无活跃套餐 / 全部无限额 → `none`。

### 渲染
- `warn` → 🟡 黄条：「你的套餐「{planName}」已用 {round(ratio×100)}%，快用完了 · 去续费」
- `exhausted` → 🔴 红条：「套餐「{planName}」已用尽，续费或换套餐以继续使用」
- CTA「去续费」按钮 → `navigate('/plans')`。
- `none` → 不渲染。

### 关闭行为
- 横幅带 × 关闭。
- 关闭状态存 `localStorage`，键 = `subUsageDismiss:{subscriptionId}:{level}`。
- 渲染前查键：已关同 (订阅, 档位) → 不显示。
- **跨档升级重弹**：`warn` 关过后升到 `exhausted` 是不同键 → 会重新显示（关键节点不漏）。
- 新计费周期 used 归零 → ratio 回落，下次接近再走一遍（旧 dismiss 键随新档位自然不再命中）。

### 边界 / 错误
- 拉取失败、加载中、无数据 → **静默不显示**（绝不打扰或闪烁）。

### i18n
- 全中文（W5）。文案用 `t('key', { defaultValue: '中文' })` 兜底，避免动可能被锁定的 `zh.json`。

## 涉及文件（预估）
- 新增 `web/default/src/features/subscriptions/lib/usage-alert.ts`（纯函数 `computeUsageAlert` + 类型）。
- 新增 `web/default/src/features/subscriptions/components/subscription-usage-banner.tsx`（组件）。
- 改 `web/default/src/routes/_authenticated/route.tsx`（挂载）。
- 复用现有 subscriptions `api.ts`/`types.ts`。

## 测试策略
- `computeUsageAlert` 纯函数：多组 (used, limit) → 断言 level/ratio/planName；覆盖 多订阅取最高、无限额跳过、无订阅→none、边界 0.8 与 1.0。
- ⚠️ Mac 无前端测试运行器（见记忆 [[frontend-build-env-gotchas]]）：纯函数逻辑自审 + 服务器构建时验；视觉用 playwright 在服务器核（黄/红/不显示/关闭不再弹/升档重弹/CTA 跳转）。

## 开放问题
- 无（阈值 80% / 100%、两档、关闭策略均已定）。
