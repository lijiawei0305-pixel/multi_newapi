package reportrepo

import (
	"context"
	"sort"

	"gorm.io/gorm"
)

// ============================================================================
// 趋势：收益 / 充值 / 提现（主库 DATETIME 台账）
// ============================================================================

// TrendEarnings 按日历桶汇总收益（跨来源合计 amount_cny），另拆出 tokenplan_spread / (ratio_markup+
// consume_commission) 两条按天子序列（字段口径镜像 agentFinanceOverviewOut，供代理「我的收益」3 线
// 趋势图 —— 套餐可提现/apikey消费可提现/总和，总和由 mtwire 层或前端相加）。三个 bucketedSumDatetime
// 调用共享同一张表/时间列，只是 extra 过滤不同，与 TrendRecharge 的 tokenplan_spread 子查询同款写法。
func (r *Repo) TrendEarnings(ctx context.Context, tenantID *int64, start, end int64, granularity string) ([]EarningsTrendPoint, error) {
	granularity = normGranularity(granularity)
	sums, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", tenantID, start, end, granularity, nil)
	if err != nil {
		return nil, err
	}
	tokenplanWithdrawable, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", tenantID, start, end, granularity, func(q *gorm.DB) *gorm.DB {
		return q.Where("source_type = ?", "tokenplan_spread")
	})
	if err != nil {
		return nil, err
	}
	consumptionWithdrawable, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", tenantID, start, end, granularity, func(q *gorm.DB) *gorm.DB {
		return q.Where("source_type IN ?", []string{"ratio_markup", "consume_commission"})
	})
	if err != nil {
		return nil, err
	}

	set := map[string]struct{}{}
	for k := range sums {
		set[k] = struct{}{}
	}
	for k := range tokenplanWithdrawable {
		set[k] = struct{}{}
	}
	for k := range consumptionWithdrawable {
		set[k] = struct{}{}
	}
	labels := make([]string, 0, len(set))
	for k := range set {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	out := make([]EarningsTrendPoint, 0, len(labels))
	for _, label := range labels {
		out = append(out, EarningsTrendPoint{
			Bucket:                     label,
			BucketTS:                   bucketStartTS(granularity, label),
			AmountCNY:                  sums[label],
			TokenplanWithdrawableCNY:   tokenplanWithdrawable[label],
			ConsumptionWithdrawableCNY: consumptionWithdrawable[label],
		})
	}
	return out, nil
}

// TrendRecharge 按日历桶汇总充值实付 + 套餐(实付/成本) + 套餐差价回退（tokenplan_spread）。
func (r *Repo) TrendRecharge(ctx context.Context, tenantID *int64, start, end int64, granularity string) ([]RechargeTrendPoint, error) {
	granularity = normGranularity(granularity)
	paid, err := r.bucketedSumDatetime(ctx, "payment_orders", "actual_paid", "created_at", tenantID, start, end, granularity, func(q *gorm.DB) *gorm.DB {
		return q.Where("type = ? AND status = ?", "recharge", "credited")
	})
	if err != nil {
		return nil, err
	}
	subPC, err := r.bucketedSubPaidCost(ctx, tenantID, start, end, granularity)
	if err != nil {
		return nil, err
	}
	spread, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", tenantID, start, end, granularity, func(q *gorm.DB) *gorm.DB {
		return q.Where("source_type = ?", "tokenplan_spread")
	})
	if err != nil {
		return nil, err
	}

	set := map[string]struct{}{}
	for k := range paid {
		set[k] = struct{}{}
	}
	for k := range subPC {
		set[k] = struct{}{}
	}
	for k := range spread {
		set[k] = struct{}{}
	}
	labels := make([]string, 0, len(set))
	for k := range set {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	out := make([]RechargeTrendPoint, 0, len(labels))
	for _, label := range labels {
		pc := subPC[label]
		out = append(out, RechargeTrendPoint{
			Bucket:                label,
			BucketTS:              bucketStartTS(granularity, label),
			RechargePaidCNY:       paid[label],
			SubscriptionPaidCNY:   pc[0],
			SubscriptionCostCNY:   pc[1],
			SubscriptionSpreadCNY: spread[label],
		})
	}
	return out, nil
}

// TrendWithdrawals 按日历桶汇总提现（pending/paid→withdrawn/rejected；paid=真正打款出账）。
func (r *Repo) TrendWithdrawals(ctx context.Context, tenantID *int64, start, end int64, granularity string) ([]WithdrawalsTrendPoint, error) {
	granularity = normGranularity(granularity)
	m, err := r.bucketedStatusSum(ctx, "agent_withdrawals", "created_at", tenantID, start, end, granularity)
	if err != nil {
		return nil, err
	}
	out := make([]WithdrawalsTrendPoint, 0, len(m))
	for _, label := range sortedStringKeys(m) {
		st := m[label]
		out = append(out, WithdrawalsTrendPoint{
			Bucket:       label,
			BucketTS:     bucketStartTS(granularity, label),
			PendingCNY:   st["pending"],
			WithdrawnCNY: st["paid"],
			RejectedCNY:  st["rejected"],
		})
	}
	return out, nil
}

// NetIncomeTrend 按日历桶汇总管理端「净收入」两条子序列（¥），供财务报表管理端净收入趋势图
// （3 线：套餐净 / api净 / 两者之和；「总净」由 mtwire/前端相加，不入 wire）。口径与 report.go
// adminFinanceOverview 完全一致，保证趋势区间合计能与 6 卡对账：
//
//	套餐净 = 订阅实付(全站含主站) − 套餐返现(仅代理,排除平台租户)   ⇒ Σ = 卡1+卡2 − 卡5
//	api净  = 钱包消耗(全站含主站) − api返现(仅代理,排除平台租户)    ⇒ Σ = 卡3+卡4 − 卡6
//
// 「排除平台租户」镜像 adminFinanceOverview 的 splitByPlatform(..., platformID) 取代理侧：
// tenant_id<>platformID 的过滤叠加在 applyTenantScope 的 tenant_id<>0 之上（extra 闭包内追加）。
// 桶标签取四个 map 并集后 sort.Strings（镜像 TrendEarnings/TrendRecharge 写法）。
func (r *Repo) NetIncomeTrend(ctx context.Context, start, end int64, granularity string, platformID int64) ([]NetIncomeTrendPoint, error) {
	granularity = normGranularity(granularity)

	// 订阅实付（全站含主站，取 paid 分量）：= 卡1+卡2 的按天分解。
	subPC, err := r.bucketedSubPaidCost(ctx, nil, start, end, granularity)
	if err != nil {
		return nil, err
	}
	// 套餐返现（仅代理，排除平台租户）：Σ source_type=tokenplan_spread AND tenant_id<>platformID = 卡5 按天分解。
	tokenplanRebate, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", nil, start, end, granularity, func(q *gorm.DB) *gorm.DB {
		return q.Where("source_type = ? AND tenant_id <> ?", "tokenplan_spread", platformID)
	})
	if err != nil {
		return nil, err
	}
	// 钱包消耗额度（全站含主站，quota 单位）：经 QuotaToCNY 换算 = 卡3+卡4 按天分解（与 WalletConsumption 同口径）。
	walletQuota, err := r.bucketedSumDatetime(ctx, "mt_wallet_consume_log", "wallet_quota", "created_at", nil, start, end, granularity, nil)
	if err != nil {
		return nil, err
	}
	// api 返现（仅代理，排除平台租户）：Σ source_type IN (ratio_markup,consume_commission) AND tenant_id<>platformID = 卡6 按天分解。
	apiRebate, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", nil, start, end, granularity, func(q *gorm.DB) *gorm.DB {
		return q.Where("source_type IN ? AND tenant_id <> ?", []string{"ratio_markup", "consume_commission"}, platformID)
	})
	if err != nil {
		return nil, err
	}

	set := map[string]struct{}{}
	for k := range subPC {
		set[k] = struct{}{}
	}
	for k := range tokenplanRebate {
		set[k] = struct{}{}
	}
	for k := range walletQuota {
		set[k] = struct{}{}
	}
	for k := range apiRebate {
		set[k] = struct{}{}
	}
	labels := make([]string, 0, len(set))
	for k := range set {
		labels = append(labels, k)
	}
	sort.Strings(labels)

	out := make([]NetIncomeTrendPoint, 0, len(labels))
	for _, label := range labels {
		pc := subPC[label] // [paid, cost]
		walletConsumptionCNY := QuotaToCNY(int64(walletQuota[label]))
		out = append(out, NetIncomeTrendPoint{
			Bucket:          label,
			BucketTS:        bucketStartTS(granularity, label),
			TokenplanNetCNY: pc[0] - tokenplanRebate[label],
			ApiNetCNY:       walletConsumptionCNY - apiRebate[label],
		})
	}
	return out, nil
}
