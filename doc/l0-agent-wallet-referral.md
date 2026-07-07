# L0 代理钱包页「邀请返现」面板设计(2026-07-04)

> 用户成为「普通代理(L0/初级)」后,在**主站钱包页**出现一块「邀请返现」面板——机制同 L1 独立站代理(靠邀请链接拉人、按下级消费/购买返现),只是 L0 没独立站,所以挂在钱包里。经互动确认:①删掉钱包页原生 affiliate 卡 ②只给 L0 看。**返现机制已存在(consume_commission + tokenplan_spread 自动入代理钱包)、所需端点 L0 全可调,本功能几乎纯前端 UI,零后端改动。**

## 一、判定与位置

- 位置:`web/default/src/features/wallet/index.tsx`,在原 `AffiliateRewardsCard` 处。
- **删掉原生 affiliate 卡**(`components/affiliate-rewards-card.tsx` + `hooks/use-affiliate.ts`,new-api 原生按充值返现,与代理返现两套、会混淆)——所有用户不再看到。
- 新增 `<AgentReferralRewardsCard>`,仅 `is_agent_owner && level===0`(`agentContextQueryOptions`,`lib/agent-context.ts`)时渲染;普通用户/L1 不显示。

## 二、面板内容

1. **邀请链接**:`GET /api/tenant/promotion/channels`——有渠道显示第一条 `${origin}/sign-up?channel=<code>` + 复制;无渠道显示「生成邀请链接」按钮 → `POST /api/tenant/promotion/channels {name:"默认邀请"}` → 刷新(不自动偷偷建)。
2. **统计(¥ 均 round2)**:
   | 指标 | 数据源(`GET /api/tenant/finance/summary` → `data.overview`,除注明) |
   | --- | --- |
   | 邀请总人数 | 各渠道 `registered_count` 之和(来自 channels) |
   | 被邀请 API 消费 | `apikey_consumption_cny` |
   | 被邀请套餐购买 | `tokenplan_revenue_cny` |
   | 总返现(= 两部分) | `consumption_withdrawable_cny` + `tokenplan_withdrawable_cny` |
   | 可提现 | `GET /api/tenant/earnings` → `withdrawable_cny`(两种返现都入此钱包) |
3. **绑卡 + 提现 + 历史**:复用 `features/agent-earnings/components/` 的 `PayoutAccountCard`+`PayoutAccountDialog`(`GET/PUT /api/tenant/payout-account`)、`WithdrawDialog`(`POST /api/tenant/withdrawals`)、`MyWithdrawalsTable`(`GET /api/tenant/withdrawals`)。

## 三、复用清单(全现成,L0 可调)

- 身份门控 `lib/agent-context.ts`(`agentContextQueryOptions`)。
- 邀请链接:`features/promotion-channels/api.ts`(list/create channel)+ 其 `signupLink()` 拼接思路。
- 数据:`features/financial-report/api.ts` `getTenantFinanceSummary`;`features/agent-earnings/api.ts` `getTenantEarnings`/`getPayoutAccount`/`getMyWithdrawals`/`requestWithdrawal`。
- 组件:`agent-earnings/components/` 的 `PayoutAccountCard`/`PayoutAccountDialog`/`WithdrawDialog`/`MyWithdrawalsTable`(几乎零改)。

## 四、口径(确认)

- 邀请总人数 = 经邀请渠道注册数之和(L0 通常即下级用户数)。
- 被邀请消费 = `apikey_consumption_cny`(纯钱包 API 消费,不含套餐);被邀请套餐 = `tokenplan_revenue_cny`(下级为套餐支付总额)。
- 总返现 = 消耗返现(`consumption_withdrawable_cny`)+ 套餐返现(`tokenplan_withdrawable_cny`);可提现 = 代理钱包总可提现 `withdrawable_cny`。
- **统计窗口(2026-07-07 修正)**:消费/购买/返现三项取**近 365 天**——后端 `parseTimeRange` 区间上限 366 天,超出报 `STATS_RANGE_INVALID`「统计范围非法」(首个真实 L0 jia 踩雷,原实现传了 3650 天)。邀请人数(channels 累加)与可提现(钱包状态)为累计值,面板已加口径注脚。

## 五、落地

1. 新 `features/wallet/components/agent-referral-rewards-card.tsx`(拉 agent-context/channels/finance-summary/earnings/payout/withdrawals,组织为面板;复用 agent-earnings 组件)。
2. `wallet/index.tsx`:删 `AffiliateRewardsCard` 渲染+import,新增 `AgentReferralRewardsCard`(内部 L0 门控,非 L0 返 null)。
3. 删 `wallet/components/affiliate-rewards-card.tsx` + `wallet/hooks/use-affiliate.ts`(grep 确认无其它引用再删)。
4. W5 全中文。门禁:`tsgo -b` 干净、`bun run build` 成功、无 `consumptionCaveat`。**本地自测,不擅自部署**。
