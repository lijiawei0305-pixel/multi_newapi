# 套餐满额续费提醒横幅 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 用户活跃套餐用量接近/达月度上限时，在控制台任意已登录页顶部显示可关闭横幅，一键跳 `/plans` 促续费。

**Architecture:** 纯前端。一个纯函数 `computeUsageAlert` 从用户活跃订阅算出告警档位，一个 `SubscriptionUsageBanner` 组件用 react-query 拉 `getSelfSubscriptions()`、渲染 shadcn `Alert`、localStorage 记关闭态，挂进 `AuthenticatedLayout` 的内容区顶部。零后端改动；`risk.NoteUsage/AlertSink` 保持休眠。

**Tech Stack:** React 19 + TanStack Router(`useNavigate`) + TanStack Query(`useQuery`) + react-i18next(`t`) + shadcn/ui(`Alert`/`Button`) + Tailwind v4。

## Global Constraints

- **前端文案一律中文（W5）**：用 `t('English key', { defaultValue: '中文' })` 兜底，不动 `zh.json`。
- **无本地前端测试运行器**（Mac 缺 `@tanstack` 等、无 vitest；见记忆 `frontend-build-env-gotchas`）：纯函数测试文件照写（供日后运行器接入），但**本地不跑**；行为验证一律走**服务器 Docker 构建（catch 类型/构建错）+ playwright 视觉**。这是本项目既有前端验证模式。
- **部署/构建在服务器**（W4）：Mac 只编辑；构建/E2E 在 `64.90.4.114` 栈 `newapi_test`（定向覆盖改动文件 + 容器重建，非整树 deploy.sh）。
- **数据源既定**：`getSelfSubscriptions()` → `GET /api/subscription/self`，返 `ApiResponse<UserSubscriptionRecord[]>`，每项 `.subscription` 含 `amount_used`/`amount_total`/`status`/`id`。订阅**无套餐名字段** → 横幅用通用文案「你的套餐」。
- **阈值**：`ratio ≥ 1.0` 或 `status==='exhausted'` → exhausted（红）；`ratio ≥ 0.8` → warn（黄）；否则不显示。`status==='expired'`（时间到期，非满额）与 `amount_total ≤ 0`（无限额）跳过。

---

### Task 1: 纯函数 `computeUsageAlert`

**Files:**
- Create: `web/default/src/features/subscriptions/lib/usage-alert.ts`
- Test: `web/default/src/features/subscriptions/lib/usage-alert.test.ts`

**Interfaces:**
- Consumes: `UserSubscriptionRecord` / `UserSubscription`（`web/default/src/features/subscriptions/types.ts`，含 `id/status/amount_used/amount_total`）。
- Produces:
  - `type UsageLevel = 'none' | 'warn' | 'exhausted'`
  - `interface UsageAlert { level: UsageLevel; ratio: number; subscriptionId: number }`
  - `function computeUsageAlert(records: UserSubscriptionRecord[] | undefined): UsageAlert`

- [ ] **Step 1: 写测试文件（本地不跑，编码意图）**

```ts
// web/default/src/features/subscriptions/lib/usage-alert.test.ts
import { describe, it, expect } from 'vitest'
import { computeUsageAlert } from './usage-alert'
import type { UserSubscriptionRecord } from '../types'

const rec = (o: Partial<UserSubscriptionRecord['subscription']>): UserSubscriptionRecord =>
  ({ subscription: {
    id: 1, user_id: 1, plan_id: 1, status: 'active',
    start_time: 0, end_time: 0, amount_total: 100, amount_used: 0, ...o,
  } })

describe('computeUsageAlert', () => {
  it('低于 80% → none', () => {
    expect(computeUsageAlert([rec({ amount_used: 50 })]).level).toBe('none')
  })
  it('恰好 80% → warn', () => {
    const r = computeUsageAlert([rec({ amount_used: 80 })])
    expect(r.level).toBe('warn'); expect(r.ratio).toBeCloseTo(0.8)
  })
  it('达到 100% → exhausted', () => {
    expect(computeUsageAlert([rec({ amount_used: 100 })]).level).toBe('exhausted')
  })
  it('status=exhausted 即使 ratio 略低也算 exhausted', () => {
    expect(computeUsageAlert([rec({ status: 'exhausted', amount_used: 99 })]).level).toBe('exhausted')
  })
  it('多订阅取最严重那条并带其 id', () => {
    const r = computeUsageAlert([rec({ id: 1, amount_used: 50 }), rec({ id: 2, amount_used: 90 })])
    expect(r.level).toBe('warn'); expect(r.subscriptionId).toBe(2)
  })
  it('status=expired 跳过（时间到期非满额）', () => {
    expect(computeUsageAlert([rec({ status: 'expired', amount_used: 100 })]).level).toBe('none')
  })
  it('amount_total<=0（无限额）跳过', () => {
    expect(computeUsageAlert([rec({ amount_total: 0, amount_used: 100 })]).level).toBe('none')
  })
  it('空/undefined → none', () => {
    expect(computeUsageAlert([]).level).toBe('none')
    expect(computeUsageAlert(undefined).level).toBe('none')
  })
})
```

- [ ] **Step 2: 写实现**

```ts
// web/default/src/features/subscriptions/lib/usage-alert.ts
import type { UserSubscriptionRecord } from '../types'

export type UsageLevel = 'none' | 'warn' | 'exhausted'

export interface UsageAlert {
  level: UsageLevel
  ratio: number
  subscriptionId: number
}

const WARN_THRESHOLD = 0.8
const RANK: Record<UsageLevel, number> = { none: 0, warn: 1, exhausted: 2 }

/**
 * 从用户活跃订阅算出满额告警档位。取最严重的一条（exhausted>warn>none）。
 * 规则：status=expired（时间到期，非满额）与 amount_total<=0（无限额）跳过；
 * ratio>=1 或 status=exhausted → exhausted；ratio>=0.8 → warn；否则 none。
 */
export function computeUsageAlert(
  records: UserSubscriptionRecord[] | undefined
): UsageAlert {
  let best: UsageAlert = { level: 'none', ratio: 0, subscriptionId: 0 }
  for (const r of records ?? []) {
    const s = r?.subscription
    if (!s) continue
    if (s.status === 'expired') continue
    if (!(s.amount_total > 0)) continue
    const ratio = s.amount_used / s.amount_total
    const level: UsageLevel =
      ratio >= 1 || s.status === 'exhausted'
        ? 'exhausted'
        : ratio >= WARN_THRESHOLD
          ? 'warn'
          : 'none'
    if (
      RANK[level] > RANK[best.level] ||
      (RANK[level] === RANK[best.level] && ratio > best.ratio)
    ) {
      best = { level, ratio, subscriptionId: s.id }
    }
  }
  return best
}
```

- [ ] **Step 3: 本地类型自检（尽力）+ 逻辑自审**

Run: `cd web/default && npx tsc --noEmit -p tsconfig.json 2>&1 | grep usage-alert || echo "no usage-alert type errors"`
Expected: 无 usage-alert 相关类型错（若本地 tsc 因缺依赖整体失败，忽略非本文件报错；逻辑正确性靠 Task 3 playwright 端到端验）。

- [ ] **Step 4: Commit**

```bash
git add web/default/src/features/subscriptions/lib/usage-alert.ts web/default/src/features/subscriptions/lib/usage-alert.test.ts
git commit -m "feat(subscriptions): computeUsageAlert 纯函数——套餐满额档位判定 + 单测"
```

---

### Task 2: `SubscriptionUsageBanner` 组件

**Files:**
- Create: `web/default/src/features/subscriptions/components/subscription-usage-banner.tsx`

**Interfaces:**
- Consumes: `computeUsageAlert`（Task 1）、`getSelfSubscriptions`（`../api`）、`Alert`/`AlertTitle`/`AlertDescription`（`@/components/ui/alert`）、`Button`（`@/components/ui/button`）、`useNavigate`（`@tanstack/react-router`）、`useQuery`（`@tanstack/react-query`）、`useTranslation`。
- Produces: `export function SubscriptionUsageBanner(): JSX.Element | null`

- [ ] **Step 1: 写组件**

```tsx
// web/default/src/features/subscriptions/components/subscription-usage-banner.tsx
import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { useNavigate } from '@tanstack/react-router'
import { useTranslation } from 'react-i18next'
import { AlertTriangle, X } from 'lucide-react'
import { cn } from '@/lib/utils'
import { Alert, AlertDescription } from '@/components/ui/alert'
import { Button } from '@/components/ui/button'
import { getSelfSubscriptions } from '../api'
import { computeUsageAlert } from '../lib/usage-alert'

export function SubscriptionUsageBanner() {
  const { t } = useTranslation()
  const navigate = useNavigate()
  const [dismissedTick, setDismissedTick] = useState(0) // 触发重渲染
  const { data } = useQuery({
    queryKey: ['self-subscriptions'],
    queryFn: getSelfSubscriptions,
    staleTime: 60_000, // 每页挂载共享缓存，避免频繁重拉
  })

  const alert = computeUsageAlert(data?.data)
  if (alert.level === 'none') return null

  const dismissKey = `subUsageDismiss:${alert.subscriptionId}:${alert.level}`
  if (typeof localStorage !== 'undefined' && localStorage.getItem(dismissKey)) {
    return null
  }
  void dismissedTick // 关闭后 state 变化触发重渲染 → 上面 localStorage 命中 → 隐藏

  const pct = Math.round(alert.ratio * 100)
  const exhausted = alert.level === 'exhausted'

  const dismiss = () => {
    try {
      localStorage.setItem(dismissKey, '1')
    } catch {
      /* localStorage 不可用则仅本次隐藏 */
    }
    setDismissedTick((n) => n + 1)
  }

  return (
    <Alert
      variant={exhausted ? 'destructive' : 'default'}
      className={cn(
        'flex items-center gap-3 rounded-none border-x-0 border-t-0',
        !exhausted &&
          'border-yellow-500/50 text-yellow-800 dark:text-yellow-300 [&>svg]:text-yellow-600'
      )}
    >
      <AlertTriangle className='h-4 w-4 shrink-0' />
      <AlertDescription className='flex-1'>
        {exhausted
          ? t('subUsage.exhausted', {
              defaultValue: '你的套餐额度已用尽，续费或换套餐以继续使用。',
            })
          : t('subUsage.warn', {
              defaultValue: '你的套餐已用 {{pct}}%，快用完了，建议尽快续费。',
              pct,
            })}
      </AlertDescription>
      <Button
        size='sm'
        variant={exhausted ? 'secondary' : 'default'}
        onClick={() => navigate({ to: '/plans' })}
      >
        {t('subUsage.cta', { defaultValue: '去续费' })}
      </Button>
      <Button
        size='icon'
        variant='ghost'
        className='h-6 w-6 shrink-0'
        aria-label={t('subUsage.dismiss', { defaultValue: '关闭' })}
        onClick={dismiss}
      >
        <X className='h-4 w-4' />
      </Button>
    </Alert>
  )
}
```

- [ ] **Step 2: 本地类型自检（尽力）**

Run: `cd web/default && npx tsc --noEmit -p tsconfig.json 2>&1 | grep subscription-usage-banner || echo "no banner type errors"`
Expected: 无本组件类型错（整体 tsc 若因环境失败，只看本文件行）。

- [ ] **Step 3: Commit**

```bash
git add web/default/src/features/subscriptions/components/subscription-usage-banner.tsx
git commit -m "feat(subscriptions): SubscriptionUsageBanner 组件——满额续费提醒(黄/红两档+可关闭+跳/plans)"
```

---

### Task 3: 挂载进布局 + 服务器构建 + playwright 端到端验

**Files:**
- Modify: `web/default/src/components/layout/components/authenticated-layout.tsx:44-53`（`SidebarInset` 内容区顶部插横幅）

**Interfaces:**
- Consumes: `SubscriptionUsageBanner`（Task 2）。

- [ ] **Step 1: 挂载横幅（内容区顶部，随内容滚、不遮侧栏）**

在 `authenticated-layout.tsx` 顶部 import 区加：
```tsx
import { SubscriptionUsageBanner } from '@/features/subscriptions/components/subscription-usage-banner'
```
把 `SidebarInset` 内的 children 前插入横幅：
```tsx
            <SidebarInset
              className={cn(
                '@container/content',
                'h-[calc(100svh-var(--app-header-height,0px))]',
                'min-h-0 overflow-hidden',
                'peer-data-[variant=inset]:h-[calc(100svh-var(--app-header-height,0px)-(var(--spacing)*4))]'
              )}
            >
              <SubscriptionUsageBanner />
              {props.children ?? <AnimatedOutlet />}
            </SidebarInset>
```

- [ ] **Step 2: Commit**

```bash
git add web/default/src/components/layout/components/authenticated-layout.tsx
git commit -m "feat(subscriptions): 挂载满额续费横幅进 AuthenticatedLayout 内容区顶部"
```

- [ ] **Step 3: 定向部署到服务器测试栈（构建即是构建验证——catch 类型/构建错）**

```bash
# Mac：把 3 个改动文件 scp 到服务器非 git 副本
scp web/default/src/features/subscriptions/lib/usage-alert.ts \
    web/default/src/features/subscriptions/components/subscription-usage-banner.tsx \
    newapi628:/root/newapi-test/web/default/src/features/subscriptions/
# ↑ lib/ 与 components/ 子目录分别放；按实际路径两条 scp：
scp web/default/src/features/subscriptions/lib/usage-alert.ts newapi628:/root/newapi-test/web/default/src/features/subscriptions/lib/
scp web/default/src/features/subscriptions/components/subscription-usage-banner.tsx newapi628:/root/newapi-test/web/default/src/features/subscriptions/components/
scp web/default/src/components/layout/components/authenticated-layout.tsx newapi628:/root/newapi-test/web/default/src/components/layout/components/
# 服务器：容器重建（前端在镜像内 vite build，构建失败即类型/语法错）
ssh newapi628 "docker tag newapi_test-app:latest newapi_test-app:prev-usage-banner; cd /root/newapi-test && docker compose -p newapi_test --env-file /root/newapi-test/.env -f /root/newapi-test/deploy/docker-compose.test.yml up -d --build app 2>&1 | tail -5"
```
Expected: 构建 `BUILD_EXIT=0`、容器 Started；`curl -s --retry 8 --retry-delay 2 http://127.0.0.1:3100/api/status -o /dev/null -w 'HTTP=%{http_code}'` = 200。

- [ ] **Step 4: playwright 视觉验（服务器侧，用完即关——W6）**

用 playwright 登录测试栈买家账号（如 `agentdemo.wedreamhub.com` 下 user9 或有活跃套餐的账号），核对：
1. 有活跃套餐且用量 ≥80% → 顶部出现黄条「已用 X%」+「去续费」。
2. 用量达 100%/exhausted → 红条「已用尽」。
3. 点「去续费」→ 跳 `/plans`。
4. 点 × 关闭 → 横幅消失；刷新/换页仍不再出现（同档位 localStorage 生效）。
5. 无活跃套餐/用量 <80% 的账号 → 无横幅。
（造数据：可临时把某活跃订阅 `amount_used` 改到 ≥80%/100% 验档位，验完复原——参照 Trial E2E 的可逆改法。）

验证完毕：`playwright-cli close` / 关无头进程，`ps` 复核零残留（W6）。

- [ ] **Step 5: 回写 STATUS + 提交**

把 `doc/tasks/STATUS.md` §三「7c-2 满额主动推送」标注：用户侧站内横幅已落地（本方案）；服务端 NoteUsage/AlertSink 仍休眠（邮件/webhook 留待日后）。
```bash
git add doc/tasks/STATUS.md
git commit -m "docs(status): 满额续费横幅(用户侧)已落地"
```

---

## Self-Review

**Spec coverage：** 目标(横幅促续费)=Task2/3；分档(80/100)=Task1；数据源(getSelfSubscriptions)=Task2；CTA跳/plans=Task2；关闭+跨档重弹(localStorage 键含 id+level)=Task2；边界(拉取失败/无数据静默)=`computeUsageAlert`→none + `data?.data` 兜底=Task1/2；i18n 中文=Task2 全 `t(defaultValue)`；放置(任意登录页顶部)=Task3。✅ 全覆盖。

**偏离 spec 记录：** spec 文案含「套餐「{planName}」」，但 `/api/subscription/self` DTO 无套餐名字段 → 改用通用「你的套餐」（YAGNI，不为横幅额外拉套餐名映射）。

**Placeholder scan：** 无 TBD/TODO；每步含真实代码/命令。测试因"无本地运行器"不本地跑，已在 Global Constraints 显式说明并用服务器构建+playwright 替代（非占位，是本项目既有模式）。

**Type consistency：** `computeUsageAlert(records)→UsageAlert{level,ratio,subscriptionId}` 在 Task1 定义、Task2 消费一致；`getSelfSubscriptions()→ApiResponse<UserSubscriptionRecord[]>`、`data?.data` 取数组一致；`UsageLevel` 三值全程一致。

**待实现时确认（非阻塞）：** `/api/subscription/self` 是否只返活跃订阅（若含历史，`status==='expired'` 跳过已兜底）；`status` 枚举确切字符串（'active'/'exhausted'/'expired'——playwright 造数时核对，computeUsageAlert 只依赖 'expired'/'exhausted' 两个特判，其余走 ratio，鲁棒）。
