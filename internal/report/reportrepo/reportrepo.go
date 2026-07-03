// Package reportrepo 是「财务报表」的聚合查询仓储（S1）：在 new-api 基座上用 raw
// db.Table()/Joins() 跨表做 SUM/GROUP BY，绝不 import 兄弟模块的 model 结构（升级 rebase 安全，
// 对齐 internal/mtwire/agent.go:46-48 约定）。四个数据透镜：
//
//	(a) 收益/分润 by source + 钱包余额   —— agent_earning_logs / agent_wallets        → ¥
//	(b) 充值/订单 paid/cost/spread       —— payment_orders / pending_subscription_orders
//	                                       JOIN mt_subscription_orders / 差价回退 ledger  → ¥
//	(c) 消耗成本 via logs→users.tenant_id —— 原生 logs JOIN users（无 tenant 列，靠 user_id 连） → ¥
//	(d) 提现 withdrawn/frozen/pending     —— agent_withdrawals / agent_wallets             → ¥
//
// 金额一律 float64（decimal(20,8)/(20,2) 存储，接口 float64 进出）；不在仓储侧四舍五入——由
// handler 边界 round(2)。时间：请求区间用 epoch 秒（int64）；DATETIME 台账表用 time.Unix(s,0).UTC()
// 比较，原生 logs.created_at 是 epoch 直接比。趋势按 UTC 日历分桶（day/week 周一/month）。
//
// 多租户口径（硬约束）：tenantID != nil → tenant_id = *tenantID（代理自助单租户）；
// tenantID == nil → 管理端跨租户，统一 tenant_id <> 0（排除主站/未归属）。
//
// 消耗透镜的 LOG_DB==DB 闸门（usage-logs-tenant-join）：同库才能 logs JOIN users；分库/ClickHouse
// 日志库降级为两步——先在主库 users 取 user_id→tenant_id 映射，再在 LOG_DB 按 user_id IN(...) 聚合、
// 在 Go 内合桶。MySQL 专用分桶 SQL 受 common.UsingMainDatabase/UsingLogDatabase(MySQL) 方言守卫，
// 其余方言（含 sqlite 单测）走「取行 + Go 分桶」路径。
package reportrepo

import (
	"context"
	"errors"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Repo 是财务报表聚合仓储。db 为主库（= model.DB）；日志查询按闸门走 model.LOG_DB。
type Repo struct {
	db *gorm.DB
}

// New 用已建立连接的 *gorm.DB（主库）构造仓储。
func New(db *gorm.DB) *Repo { return &Repo{db: db} }

// ============================================================================
// 结果结构（exportedSignatures：stage 2 据此装配 DTO）
// ============================================================================

// SourceSum 是「按收益来源汇总」的一行：source_type → Σ amount（¥）。
type SourceSum struct {
	SourceType string
	AmountCNY  float64
}

// WalletAgg 是钱包合计（代理=单行；管理端=跨租户 SUM）。api_balance 为 USD，余皆 ¥。
type WalletAgg struct {
	WithdrawableCNY float64
	FrozenCNY       float64
	TotalEarnedCNY  float64
	APIBalanceUSD   float64
}

// PaidCost 是订阅维度的 (零售实付, 代理成本) 对（¥）。
type PaidCost struct {
	PaidCNY float64
	CostCNY float64
}

// ConsumptionAgg 是消耗透镜的单值合计（按 scope 全量，不分租户）。
type ConsumptionAgg struct {
	UsedQuota   int64
	Calls       int64
	Tokens      int64
	UsedCostUSD float64
	UsedCostCNY float64
}

// ConsumptionTrendPoint 是消耗趋势的一个日历桶。
type ConsumptionTrendPoint struct {
	Bucket      string
	BucketTS    int64
	UsedQuota   int64
	Calls       int64
	Tokens      int64
	UsedCostCNY float64
}

// EarningsTrendPoint 是收益趋势的一个日历桶（跨来源合计 amount_cny）。
type EarningsTrendPoint struct {
	Bucket    string
	BucketTS  int64
	AmountCNY float64
}

// RechargeTrendPoint 是充值/订单趋势的一个日历桶。
type RechargeTrendPoint struct {
	Bucket                string
	BucketTS              int64
	RechargePaidCNY       float64
	SubscriptionPaidCNY   float64
	SubscriptionCostCNY   float64
	SubscriptionSpreadCNY float64
}

// WithdrawalsTrendPoint 是提现趋势的一个日历桶（按状态分列）。
type WithdrawalsTrendPoint struct {
	Bucket       string
	BucketTS     int64
	PendingCNY   float64
	WithdrawnCNY float64
	RejectedCNY  float64
}

// AgentRankRow 是管理端「按代理/租户排行」的一行（跨租户聚合，§1.3）。
type AgentRankRow struct {
	TenantID             int64
	AgentName            string
	OwnerUserID          int64
	OwnerUsername        string
	TotalEarnedCNY       float64
	ConsumeCommissionCNY float64
	RatioMarkupCNY       float64
	TokenplanSpreadCNY   float64
	ManualAdjustmentCNY  float64
	RechargePaidCNY      float64
	SubscriptionPaidCNY  float64
	SubscriptionCostCNY  float64
	ConsumptionUsedQuota int64
	ConsumptionCostCNY   float64
	WithdrawnCNY         float64
	PendingWithdrawCNY   float64
	WithdrawableCNY      float64
}

// EarningDetailRow 是收益明细一行（§1.4 earnings；reference = agent_earning_logs.source_id）。
type EarningDetailRow struct {
	TenantID   int64
	AgentName  string
	SourceType string
	AmountCNY  float64
	Reference  string
	CreatedAt  time.Time
}

// WithdrawalDetailRow 是提现明细一行（§1.4 withdrawals）。
type WithdrawalDetailRow struct {
	ID         int64
	TenantID   int64
	AgentName  string
	AmountCNY  float64
	Status     string
	CreatedAt  time.Time
	ReviewedAt *time.Time
}

// RechargeDetailRow 是充值/订单明细一行（§1.4 recharge）：充值单与套餐单合流，kind 区分。
type RechargeDetailRow struct {
	OrderNo           string
	TenantID          int64
	Kind              string // "recharge" | "subscription"
	Provider          string
	AmountUSD         float64
	ActualPaidCNY     float64
	AgentCostPriceCNY float64
	Status            string
	CreatedAt         time.Time
}

// ConsumptionDetailRow 是消耗明细一行（§1.4 consumption；按 tenant_id × model_name 聚合）。
type ConsumptionDetailRow struct {
	TenantID    int64
	AgentName   string
	ModelName   string
	Calls       int64
	Tokens      int64
	UsedQuota   int64
	UsedCostCNY float64
}

// ============================================================================
// 币种换算（读取运行期 LIVE 包变量；非正一律给 0）
// ============================================================================

// QuotaToUSD 把 quota 换算成 USD：quota / common.QuotaPerUnit（500000）。
func QuotaToUSD(quota int64) float64 {
	if quota <= 0 || common.QuotaPerUnit <= 0 {
		return 0
	}
	return float64(quota) / common.QuotaPerUnit
}

// QuotaToCNY 把 quota 换算成 ¥：QuotaToUSD × operation_setting.USDExchangeRate（默认 7.3）。
// 与 consumeCommissionCNY(ratio=1) 同口径，故报表消耗成本与 consume_commission 收益可对账。
func QuotaToCNY(quota int64) float64 {
	usd := QuotaToUSD(quota)
	rate := operation_setting.USDExchangeRate
	if usd <= 0 || rate <= 0 {
		return 0
	}
	return usd * rate
}

// ============================================================================
// 透镜 (a)：收益 by source + 钱包
// ============================================================================

// SummaryEarnings 按 source_type 汇总收益金额（¥）。代理=单租户，管理端=跨租户(<>0)。
func (r *Repo) SummaryEarnings(ctx context.Context, tenantID *int64, start, end int64) ([]SourceSum, error) {
	type sumRow struct {
		SourceType string
		Amount     float64
	}
	var rows []sumRow
	q := r.db.WithContext(ctx).Table("agent_earning_logs").
		Select("source_type, COALESCE(SUM(amount),0) AS amount").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Group("source_type").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]SourceSum, 0, len(rows))
	for _, x := range rows {
		out = append(out, SourceSum{SourceType: x.SourceType, AmountCNY: x.Amount})
	}
	return out, nil
}

// WalletTotals 返回钱包合计：代理=该租户单行（缺行返回零值，不报错）；管理端=跨租户 SUM(<>0)。
func (r *Repo) WalletTotals(ctx context.Context, tenantID *int64) (WalletAgg, error) {
	if tenantID != nil {
		var row struct {
			WithdrawableBalance  float64
			FrozenWithdrawAmount float64
			TotalEarned          float64
			ApiBalance           float64
		}
		err := r.db.WithContext(ctx).Table("agent_wallets").
			Select("withdrawable_balance, frozen_withdraw_amount, total_earned, api_balance").
			Where("tenant_id = ?", *tenantID).
			Take(&row).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return WalletAgg{}, nil
			}
			return WalletAgg{}, err
		}
		return WalletAgg{
			WithdrawableCNY: row.WithdrawableBalance,
			FrozenCNY:       row.FrozenWithdrawAmount,
			TotalEarnedCNY:  row.TotalEarned,
			APIBalanceUSD:   row.ApiBalance,
		}, nil
	}
	var row struct {
		Withdrawable float64
		Frozen       float64
		TotalEarned  float64
		ApiBalance   float64
	}
	err := r.db.WithContext(ctx).Table("agent_wallets").
		Select("COALESCE(SUM(withdrawable_balance),0) AS withdrawable, " +
			"COALESCE(SUM(frozen_withdraw_amount),0) AS frozen, " +
			"COALESCE(SUM(total_earned),0) AS total_earned, " +
			"COALESCE(SUM(api_balance),0) AS api_balance").
		Where("tenant_id <> 0").
		Scan(&row).Error // 聚合恒返一行，用 Scan（对齐 model.SumUsedQuota 习语）
	if err != nil {
		return WalletAgg{}, err
	}
	return WalletAgg{
		WithdrawableCNY: row.Withdrawable,
		FrozenCNY:       row.Frozen,
		TotalEarnedCNY:  row.TotalEarned,
		APIBalanceUSD:   row.ApiBalance,
	}, nil
}

// ============================================================================
// 透镜 (b)：充值/订单 paid/cost
// ============================================================================

// RechargePaid 按租户汇总钱包充值实付（¥）：payment_orders type='recharge' AND status='credited'。
func (r *Repo) RechargePaid(ctx context.Context, tenantID *int64, start, end int64) (map[int64]float64, error) {
	type paidRow struct {
		TenantID int64
		Paid     float64
	}
	var rows []paidRow
	q := r.db.WithContext(ctx).Table("payment_orders").
		Select("tenant_id, COALESCE(SUM(actual_paid),0) AS paid").
		Where("type = ? AND status = ?", "recharge", "credited").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Group("tenant_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]float64, len(rows))
	for _, x := range rows {
		out[x.TenantID] = x.Paid
	}
	return out, nil
}

// SubscriptionPaidCost 按租户汇总套餐订单的 (零售实付, 代理成本)（¥）：
// pending_subscription_orders JOIN mt_subscription_orders（o.status='activated'，按 o.created_at 计区间）。
func (r *Repo) SubscriptionPaidCost(ctx context.Context, tenantID *int64, start, end int64) (map[int64]PaidCost, error) {
	type pcRow struct {
		TenantID int64
		Paid     float64
		Cost     float64
	}
	var rows []pcRow
	q := r.db.WithContext(ctx).
		Table("pending_subscription_orders AS p").
		Select("p.tenant_id AS tenant_id, COALESCE(SUM(p.retail_price),0) AS paid, COALESCE(SUM(p.agent_cost_price),0) AS cost").
		Joins("JOIN mt_subscription_orders AS o ON o.order_no = p.order_id").
		Where("o.status = ?", "activated").
		Where("o.created_at >= ? AND o.created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "p.tenant_id", tenantID)
	if err := q.Group("p.tenant_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]PaidCost, len(rows))
	for _, x := range rows {
		out[x.TenantID] = PaidCost{PaidCNY: x.Paid, CostCNY: x.Cost}
	}
	return out, nil
}

// ============================================================================
// 透镜 (d)：提现 by status
// ============================================================================

// Withdrawals 按状态汇总提现金额（¥）：status ∈ pending/approved/rejected。
// frozen 口径由 WalletTotals.FrozenCNY 提供（= SUM(agent_wallets.frozen_withdraw_amount)）。
func (r *Repo) Withdrawals(ctx context.Context, tenantID *int64, start, end int64) (map[string]float64, error) {
	type stRow struct {
		Status string
		Amount float64
	}
	var rows []stRow
	q := r.db.WithContext(ctx).Table("agent_withdrawals").
		Select("status, COALESCE(SUM(amount),0) AS amount").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Group("status").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[string]float64, len(rows))
	for _, x := range rows {
		out[x.Status] = x.Amount
	}
	return out, nil
}

// ============================================================================
// 透镜 (c)：消耗成本（LOG_DB==DB 闸门）
// ============================================================================

// ConsumptionCost 返回 scope 全量消耗合计（不分租户）：used_quota/calls/tokens + 换算成本。
func (r *Repo) ConsumptionCost(ctx context.Context, tenantID *int64, start, end int64) (ConsumptionAgg, error) {
	var agg ConsumptionAgg
	if r.sameLogDB() {
		var row struct {
			UsedQuota int64
			Calls     int64
			Tokens    int64
		}
		q := r.logDB().WithContext(ctx).
			Table("logs AS l").
			Select("COALESCE(SUM(l.quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(l.prompt_tokens + l.completion_tokens),0) AS tokens").
			Joins("JOIN users AS u ON u.id = l.user_id").
			Where("l.type = ?", model.LogTypeConsume).
			Where("u.deleted_at IS NULL").
			Where("l.created_at >= ? AND l.created_at <= ?", start, end)
		q = applyTenantScope(q, "u.tenant_id", tenantID)
		if err := q.Scan(&row).Error; err != nil {
			return ConsumptionAgg{}, err
		}
		agg.UsedQuota, agg.Calls, agg.Tokens = row.UsedQuota, row.Calls, row.Tokens
	} else {
		ids, err := r.scopeUserIDs(ctx, tenantID)
		if err != nil {
			return ConsumptionAgg{}, err
		}
		if len(ids) == 0 {
			return ConsumptionAgg{}, nil
		}
		var row struct {
			UsedQuota int64
			Calls     int64
			Tokens    int64
		}
		if err := r.logDB().WithContext(ctx).
			Table("logs").
			Select("COALESCE(SUM(quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(prompt_tokens + completion_tokens),0) AS tokens").
			Where("type = ?", model.LogTypeConsume).
			Where("created_at >= ? AND created_at <= ?", start, end).
			Where("user_id IN ?", ids).
			Scan(&row).Error; err != nil {
			return ConsumptionAgg{}, err
		}
		agg.UsedQuota, agg.Calls, agg.Tokens = row.UsedQuota, row.Calls, row.Tokens
	}
	agg.UsedCostUSD = QuotaToUSD(agg.UsedQuota)
	agg.UsedCostCNY = QuotaToCNY(agg.UsedQuota)
	return agg, nil
}

// ConsumptionTrend 按日历桶聚合 scope 全量消耗。MySQL 同库走 DATE_FORMAT(FROM_UNIXTIME(...)) 分桶；
// 否则取行在 Go 内分桶（同库非 MySQL 单测 / 分库降级）。
func (r *Repo) ConsumptionTrend(ctx context.Context, tenantID *int64, start, end int64, granularity string) ([]ConsumptionTrendPoint, error) {
	granularity = normGranularity(granularity)
	type acc struct {
		quota, calls, tokens int64
	}
	buckets := map[string]*acc{}
	add := func(label string, q, c, t int64) {
		a := buckets[label]
		if a == nil {
			a = &acc{}
			buckets[label] = a
		}
		a.quota += q
		a.calls += c
		a.tokens += t
	}

	if r.sameLogDB() && common.UsingLogDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketEpoch("l.created_at", granularity)
		var rows []struct {
			Bucket    string
			UsedQuota int64
			Calls     int64
			Tokens    int64
		}
		q := r.logDB().WithContext(ctx).
			Table("logs AS l").
			Select(expr + " AS bucket, COALESCE(SUM(l.quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(l.prompt_tokens + l.completion_tokens),0) AS tokens").
			Joins("JOIN users AS u ON u.id = l.user_id").
			Where("l.type = ?", model.LogTypeConsume).
			Where("u.deleted_at IS NULL").
			Where("l.created_at >= ? AND l.created_at <= ?", start, end)
		q = applyTenantScope(q, "u.tenant_id", tenantID)
		if err := q.Group(expr).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			add(x.Bucket, x.UsedQuota, x.Calls, x.Tokens)
		}
	} else {
		ids, err := r.scopeUserIDs(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if len(ids) == 0 {
			return []ConsumptionTrendPoint{}, nil
		}
		var rows []logRow
		if err := r.logDB().WithContext(ctx).
			Table("logs").
			Select("created_at, quota, prompt_tokens, completion_tokens").
			Where("type = ?", model.LogTypeConsume).
			Where("created_at >= ? AND created_at <= ?", start, end).
			Where("user_id IN ?", ids).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			label, _ := bucketize(granularity, x.CreatedAt)
			add(label, x.Quota, 1, x.PromptTokens+x.CompletionTokens)
		}
	}

	out := make([]ConsumptionTrendPoint, 0, len(buckets))
	for _, label := range sortedStringKeys(buckets) {
		a := buckets[label]
		out = append(out, ConsumptionTrendPoint{
			Bucket:      label,
			BucketTS:    bucketStartTS(granularity, label),
			UsedQuota:   a.quota,
			Calls:       a.calls,
			Tokens:      a.tokens,
			UsedCostCNY: QuotaToCNY(a.quota),
		})
	}
	return out, nil
}

// ============================================================================
// 趋势：收益 / 充值 / 提现（主库 DATETIME 台账）
// ============================================================================

// TrendEarnings 按日历桶汇总收益（跨来源合计 amount_cny）。
func (r *Repo) TrendEarnings(ctx context.Context, tenantID *int64, start, end int64, granularity string) ([]EarningsTrendPoint, error) {
	granularity = normGranularity(granularity)
	sums, err := r.bucketedSumDatetime(ctx, "agent_earning_logs", "amount", "created_at", tenantID, start, end, granularity, nil)
	if err != nil {
		return nil, err
	}
	out := make([]EarningsTrendPoint, 0, len(sums))
	for _, label := range sortedStringKeys(sums) {
		out = append(out, EarningsTrendPoint{
			Bucket:    label,
			BucketTS:  bucketStartTS(granularity, label),
			AmountCNY: sums[label],
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

// TrendWithdrawals 按日历桶汇总提现（pending/approved→withdrawn/rejected）。
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
			WithdrawnCNY: st["approved"],
			RejectedCNY:  st["rejected"],
		})
	}
	return out, nil
}

// ============================================================================
// 管理端排行（跨租户 GROUP BY tenant_id；白名单排序 + 分页 + 名称解析）
// ============================================================================

// AgentRanking 装配各透镜的 per-tenant 聚合，合并后在 Go 内按白名单字段排序、分页，
// 再 JOIN tenants/users 解析 agent_name/owner。仅管理端（跨租户，tenant_id<>0）。
// total 为白名单全集大小（分页前）；sortBy 非白名单回退 total_earned_cny；order!='asc' 即 desc。
func (r *Repo) AgentRanking(ctx context.Context, start, end int64, sortBy, order string, page, pageSize int) ([]AgentRankRow, int64, error) {
	earn, err := r.earningsByTenant(ctx, start, end)
	if err != nil {
		return nil, 0, err
	}
	recharge, err := r.RechargePaid(ctx, nil, start, end)
	if err != nil {
		return nil, 0, err
	}
	subs, err := r.SubscriptionPaidCost(ctx, nil, start, end)
	if err != nil {
		return nil, 0, err
	}
	wd, err := r.withdrawByTenant(ctx, start, end)
	if err != nil {
		return nil, 0, err
	}
	cons, err := r.consumptionByTenant(ctx, start, end)
	if err != nil {
		return nil, 0, err
	}
	wallet, err := r.walletWithdrawableByTenant(ctx)
	if err != nil {
		return nil, 0, err
	}

	set := map[int64]struct{}{}
	for k := range earn {
		set[k] = struct{}{}
	}
	for k := range recharge {
		set[k] = struct{}{}
	}
	for k := range subs {
		set[k] = struct{}{}
	}
	for k := range wd {
		set[k] = struct{}{}
	}
	for k := range cons {
		set[k] = struct{}{}
	}
	for k := range wallet {
		set[k] = struct{}{}
	}
	delete(set, 0)
	ids := sortedInt64Set(set)
	metas := r.tenantNames(ctx, ids)

	rows := make([]AgentRankRow, 0, len(ids))
	for _, tid := range ids {
		e := earn[tid]
		pc := subs[tid]
		w := wd[tid]
		c := cons[tid]
		meta := metas[tid]
		rows = append(rows, AgentRankRow{
			TenantID:             tid,
			AgentName:            meta.Name,
			OwnerUserID:          meta.OwnerUserID,
			OwnerUsername:        meta.OwnerUsername,
			TotalEarnedCNY:       e.total,
			ConsumeCommissionCNY: e.consume,
			RatioMarkupCNY:       e.ratioMarkup,
			TokenplanSpreadCNY:   e.tokenplanSpread,
			ManualAdjustmentCNY:  e.manualAdj,
			RechargePaidCNY:      recharge[tid],
			SubscriptionPaidCNY:  pc.PaidCNY,
			SubscriptionCostCNY:  pc.CostCNY,
			ConsumptionUsedQuota: c.UsedQuota,
			ConsumptionCostCNY:   QuotaToCNY(c.UsedQuota),
			WithdrawnCNY:         w.withdrawn,
			PendingWithdrawCNY:   w.pending,
			WithdrawableCNY:      wallet[tid],
		})
	}

	total := int64(len(rows))
	sortRanking(rows, sortBy, order)
	return paginateSlice(rows, page, pageSize), total, nil
}

// ============================================================================
// 明细（分页）
// ============================================================================

// DetailEarnings 收益明细，按 created_at 倒序分页。reference = source_id。
func (r *Repo) DetailEarnings(ctx context.Context, tenantID *int64, start, end int64, page, pageSize int) ([]EarningDetailRow, int64, error) {
	where := func() *gorm.DB {
		q := r.db.WithContext(ctx).Table("agent_earning_logs").
			Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
		return applyTenantScope(q, "tenant_id", tenantID)
	}
	var total int64
	if err := where().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		TenantID   int64
		SourceType string
		Amount     float64
		SourceID   string
		CreatedAt  time.Time
	}
	if err := where().
		Select("tenant_id, source_type, amount, source_id, created_at").
		Order("created_at DESC").
		Limit(lim(pageSize)).Offset(off(page, pageSize)).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]EarningDetailRow, 0, len(rows))
	for _, x := range rows {
		out = append(out, EarningDetailRow{
			TenantID:   x.TenantID,
			SourceType: x.SourceType,
			AmountCNY:  x.Amount,
			Reference:  x.SourceID,
			CreatedAt:  x.CreatedAt,
		})
	}
	r.fillEarningNames(ctx, out)
	return out, total, nil
}

// DetailWithdrawals 提现明细，按 created_at 倒序分页。
func (r *Repo) DetailWithdrawals(ctx context.Context, tenantID *int64, start, end int64, page, pageSize int) ([]WithdrawalDetailRow, int64, error) {
	where := func() *gorm.DB {
		q := r.db.WithContext(ctx).Table("agent_withdrawals").
			Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
		return applyTenantScope(q, "tenant_id", tenantID)
	}
	var total int64
	if err := where().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	var rows []struct {
		ID         int64
		TenantID   int64
		Amount     float64
		Status     string
		CreatedAt  time.Time
		ReviewedAt *time.Time
	}
	if err := where().
		Select("id, tenant_id, amount, status, created_at, reviewed_at").
		Order("created_at DESC").
		Limit(lim(pageSize)).Offset(off(page, pageSize)).
		Scan(&rows).Error; err != nil {
		return nil, 0, err
	}
	out := make([]WithdrawalDetailRow, 0, len(rows))
	for _, x := range rows {
		out = append(out, WithdrawalDetailRow{
			ID:         x.ID,
			TenantID:   x.TenantID,
			AmountCNY:  x.Amount,
			Status:     x.Status,
			CreatedAt:  x.CreatedAt,
			ReviewedAt: x.ReviewedAt,
		})
	}
	ids := collectTenantIDs(out, func(w WithdrawalDetailRow) int64 { return w.TenantID })
	metas := r.tenantNames(ctx, ids)
	for i := range out {
		out[i].AgentName = metas[out[i].TenantID].Name
	}
	return out, total, nil
}

// DetailRecharge 充值/订单明细：payment_orders(type='recharge') 与 mt_subscription_orders 合流，
// 按 created_at 倒序，在 Go 内归并 + 分页（两表无法直接 SQL 交错分页）。
func (r *Repo) DetailRecharge(ctx context.Context, tenantID *int64, start, end int64, page, pageSize int) ([]RechargeDetailRow, int64, error) {
	var pRows []struct {
		OrderNo    string
		TenantID   int64
		Provider   string
		AmountUSD  float64
		ActualPaid float64
		Status     string
		CreatedAt  time.Time
	}
	pq := r.db.WithContext(ctx).Table("payment_orders").
		Where("type = ?", "recharge").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
	pq = applyTenantScope(pq, "tenant_id", tenantID)
	if err := pq.Select("order_no, tenant_id, provider, amount_usd, actual_paid, status, created_at").
		Scan(&pRows).Error; err != nil {
		return nil, 0, err
	}

	var sRows []struct {
		OrderNo        string
		TenantID       int64
		Provider       string
		AmountCNY      float64
		AgentCostPrice float64
		Status         string
		CreatedAt      time.Time
	}
	sq := r.db.WithContext(ctx).Table("mt_subscription_orders AS o").
		Joins("LEFT JOIN pending_subscription_orders AS p ON p.order_id = o.order_no").
		Where("o.created_at >= ? AND o.created_at <= ?", unixT(start), unixT(end))
	sq = applyTenantScope(sq, "o.tenant_id", tenantID)
	if err := sq.Select("o.order_no AS order_no, o.tenant_id AS tenant_id, o.provider AS provider, " +
		"o.amount_cny AS amount_cny, COALESCE(p.agent_cost_price,0) AS agent_cost_price, " +
		"o.status AS status, o.created_at AS created_at").
		Scan(&sRows).Error; err != nil {
		return nil, 0, err
	}

	all := make([]RechargeDetailRow, 0, len(pRows)+len(sRows))
	for _, p := range pRows {
		all = append(all, RechargeDetailRow{
			OrderNo: p.OrderNo, TenantID: p.TenantID, Kind: "recharge",
			Provider: p.Provider, AmountUSD: p.AmountUSD, ActualPaidCNY: p.ActualPaid,
			AgentCostPriceCNY: 0, Status: p.Status, CreatedAt: p.CreatedAt,
		})
	}
	for _, s := range sRows {
		all = append(all, RechargeDetailRow{
			OrderNo: s.OrderNo, TenantID: s.TenantID, Kind: "subscription",
			Provider: s.Provider, AmountUSD: 0, ActualPaidCNY: s.AmountCNY,
			AgentCostPriceCNY: s.AgentCostPrice, Status: s.Status, CreatedAt: s.CreatedAt,
		})
	}
	sort.SliceStable(all, func(i, j int) bool {
		if !all[i].CreatedAt.Equal(all[j].CreatedAt) {
			return all[i].CreatedAt.After(all[j].CreatedAt)
		}
		return all[i].OrderNo < all[j].OrderNo
	})
	total := int64(len(all))
	return paginateSlice(all, page, pageSize), total, nil
}

// DetailConsumption 消耗明细：按 tenant_id × model_name 聚合，used_quota 倒序分页（Go 内分页）。
func (r *Repo) DetailConsumption(ctx context.Context, tenantID *int64, start, end int64, page, pageSize int) ([]ConsumptionDetailRow, int64, error) {
	type consAcc struct {
		quota, calls, tokens int64
	}
	agg := map[int64]map[string]*consAcc{}
	add := func(tid int64, mname string, q, c, t int64) {
		if agg[tid] == nil {
			agg[tid] = map[string]*consAcc{}
		}
		a := agg[tid][mname]
		if a == nil {
			a = &consAcc{}
			agg[tid][mname] = a
		}
		a.quota += q
		a.calls += c
		a.tokens += t
	}

	if r.sameLogDB() {
		var rows []struct {
			TenantID  int64
			ModelName string
			UsedQuota int64
			Calls     int64
			Tokens    int64
		}
		q := r.logDB().WithContext(ctx).
			Table("logs AS l").
			Select("u.tenant_id AS tenant_id, l.model_name AS model_name, COALESCE(SUM(l.quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(l.prompt_tokens + l.completion_tokens),0) AS tokens").
			Joins("JOIN users AS u ON u.id = l.user_id").
			Where("l.type = ?", model.LogTypeConsume).
			Where("u.deleted_at IS NULL").
			Where("l.created_at >= ? AND l.created_at <= ?", start, end)
		q = applyTenantScope(q, "u.tenant_id", tenantID)
		if err := q.Group("u.tenant_id, l.model_name").Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		for _, x := range rows {
			add(x.TenantID, x.ModelName, x.UsedQuota, x.Calls, x.Tokens)
		}
	} else {
		u2t, err := r.scopeUserTenant(ctx, tenantID)
		if err != nil {
			return nil, 0, err
		}
		if len(u2t) == 0 {
			return []ConsumptionDetailRow{}, 0, nil
		}
		ids := make([]int64, 0, len(u2t))
		for id := range u2t {
			ids = append(ids, id)
		}
		var rows []struct {
			UserID    int64
			ModelName string
			UsedQuota int64
			Calls     int64
			Tokens    int64
		}
		if err := r.logDB().WithContext(ctx).
			Table("logs").
			Select("user_id, model_name, COALESCE(SUM(quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(prompt_tokens + completion_tokens),0) AS tokens").
			Where("type = ?", model.LogTypeConsume).
			Where("created_at >= ? AND created_at <= ?", start, end).
			Where("user_id IN ?", ids).
			Group("user_id, model_name").
			Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		for _, x := range rows {
			tid := u2t[x.UserID]
			if tid == 0 {
				continue
			}
			add(tid, x.ModelName, x.UsedQuota, x.Calls, x.Tokens)
		}
	}

	list := make([]ConsumptionDetailRow, 0, len(agg))
	for tid, models := range agg {
		for mname, a := range models {
			list = append(list, ConsumptionDetailRow{
				TenantID:    tid,
				ModelName:   mname,
				Calls:       a.calls,
				Tokens:      a.tokens,
				UsedQuota:   a.quota,
				UsedCostCNY: QuotaToCNY(a.quota),
			})
		}
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].UsedQuota != list[j].UsedQuota {
			return list[i].UsedQuota > list[j].UsedQuota
		}
		if list[i].TenantID != list[j].TenantID {
			return list[i].TenantID < list[j].TenantID
		}
		return list[i].ModelName < list[j].ModelName
	})
	total := int64(len(list))
	paged := paginateSlice(list, page, pageSize)
	ids := collectTenantIDs(paged, func(c ConsumptionDetailRow) int64 { return c.TenantID })
	metas := r.tenantNames(ctx, ids)
	for i := range paged {
		paged[i].AgentName = metas[paged[i].TenantID].Name
	}
	return paged, total, nil
}

// ============================================================================
// 内部：per-tenant 聚合（供 AgentRanking）
// ============================================================================

type earnAgg struct {
	total, consume, ratioMarkup, tokenplanSpread, manualAdj float64
}

func (r *Repo) earningsByTenant(ctx context.Context, start, end int64) (map[int64]earnAgg, error) {
	var rows []struct {
		TenantID   int64
		SourceType string
		Amount     float64
	}
	q := r.db.WithContext(ctx).Table("agent_earning_logs").
		Select("tenant_id, source_type, COALESCE(SUM(amount),0) AS amount").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end)).
		Where("tenant_id <> 0").
		Group("tenant_id, source_type")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int64]earnAgg{}
	for _, x := range rows {
		a := out[x.TenantID]
		a.total += x.Amount
		switch x.SourceType {
		case "consume_commission":
			a.consume += x.Amount
		case "ratio_markup":
			a.ratioMarkup += x.Amount
		case "tokenplan_spread":
			a.tokenplanSpread += x.Amount
		case "manual_adjustment":
			a.manualAdj += x.Amount
		}
		out[x.TenantID] = a
	}
	return out, nil
}

type wdAgg struct {
	withdrawn, pending float64
}

func (r *Repo) withdrawByTenant(ctx context.Context, start, end int64) (map[int64]wdAgg, error) {
	var rows []struct {
		TenantID int64
		Status   string
		Amount   float64
	}
	q := r.db.WithContext(ctx).Table("agent_withdrawals").
		Select("tenant_id, status, COALESCE(SUM(amount),0) AS amount").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end)).
		Where("tenant_id <> 0").
		Group("tenant_id, status")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := map[int64]wdAgg{}
	for _, x := range rows {
		a := out[x.TenantID]
		switch x.Status {
		case "approved":
			a.withdrawn += x.Amount
		case "pending":
			a.pending += x.Amount
		}
		out[x.TenantID] = a
	}
	return out, nil
}

func (r *Repo) walletWithdrawableByTenant(ctx context.Context) (map[int64]float64, error) {
	var rows []struct {
		TenantID            int64
		WithdrawableBalance float64
	}
	if err := r.db.WithContext(ctx).Table("agent_wallets").
		Select("tenant_id, withdrawable_balance").
		Where("tenant_id <> 0").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]float64, len(rows))
	for _, x := range rows {
		out[x.TenantID] = x.WithdrawableBalance
	}
	return out, nil
}

type tenantConsumption struct {
	UsedQuota int64
	Calls     int64
	Tokens    int64
}

// consumptionByTenant 跨租户(<>0)消耗聚合（管理端排行用）；遵循 LOG_DB==DB 闸门。
func (r *Repo) consumptionByTenant(ctx context.Context, start, end int64) (map[int64]tenantConsumption, error) {
	out := map[int64]tenantConsumption{}
	if r.sameLogDB() {
		var rows []struct {
			TenantID  int64
			UsedQuota int64
			Calls     int64
			Tokens    int64
		}
		q := r.logDB().WithContext(ctx).
			Table("logs AS l").
			Select("u.tenant_id AS tenant_id, COALESCE(SUM(l.quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(l.prompt_tokens + l.completion_tokens),0) AS tokens").
			Joins("JOIN users AS u ON u.id = l.user_id").
			Where("l.type = ?", model.LogTypeConsume).
			Where("u.deleted_at IS NULL").
			Where("u.tenant_id <> 0").
			Where("l.created_at >= ? AND l.created_at <= ?", start, end).
			Group("u.tenant_id")
		if err := q.Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			out[x.TenantID] = tenantConsumption{UsedQuota: x.UsedQuota, Calls: x.Calls, Tokens: x.Tokens}
		}
		return out, nil
	}
	u2t, err := r.scopeUserTenant(ctx, nil)
	if err != nil {
		return nil, err
	}
	if len(u2t) == 0 {
		return out, nil
	}
	ids := make([]int64, 0, len(u2t))
	for id := range u2t {
		ids = append(ids, id)
	}
	var rows []struct {
		UserID    int64
		UsedQuota int64
		Calls     int64
		Tokens    int64
	}
	if err := r.logDB().WithContext(ctx).
		Table("logs").
		Select("user_id, COALESCE(SUM(quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(prompt_tokens + completion_tokens),0) AS tokens").
		Where("type = ?", model.LogTypeConsume).
		Where("created_at >= ? AND created_at <= ?", start, end).
		Where("user_id IN ?", ids).
		Group("user_id").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		tid := u2t[x.UserID]
		if tid == 0 {
			continue
		}
		c := out[tid]
		c.UsedQuota += x.UsedQuota
		c.Calls += x.Calls
		c.Tokens += x.Tokens
		out[tid] = c
	}
	return out, nil
}

// ============================================================================
// 内部：分桶 SQL/Go 通用 + 名称解析 + LOG_DB 闸门
// ============================================================================

// bucketedSumDatetime 在主库 DATETIME 列上按日历桶 SUM(sumCol)，返回 label→sum。
// MySQL 用 DATE_FORMAT 在 DB 内 GROUP BY；其余方言取行在 Go 内分桶（UTC 一致）。
func (r *Repo) bucketedSumDatetime(ctx context.Context, table, sumCol, timeCol string, tenantID *int64, start, end int64, granularity string, extra func(*gorm.DB) *gorm.DB) (map[string]float64, error) {
	res := map[string]float64{}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketDatetime(timeCol, granularity)
		var rows []struct {
			Bucket string
			Val    float64
		}
		q := r.db.WithContext(ctx).Table(table).
			Select(expr + " AS bucket, COALESCE(SUM(" + sumCol + "),0) AS val").
			Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if extra != nil {
			q = extra(q)
		}
		if err := q.Group(expr).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			res[x.Bucket] = x.Val
		}
		return res, nil
	}
	var rows []struct {
		CreatedAt time.Time
		Val       float64
	}
	q := r.db.WithContext(ctx).Table(table).
		Select(timeCol + " AS created_at, " + sumCol + " AS val").
		Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if extra != nil {
		q = extra(q)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		label, _ := bucketize(granularity, x.CreatedAt.Unix())
		res[label] += x.Val
	}
	return res, nil
}

// bucketedStatusSum 在主库 DATETIME 列上按 (日历桶, status) SUM(amount)，返回 label→status→sum。
func (r *Repo) bucketedStatusSum(ctx context.Context, table, timeCol string, tenantID *int64, start, end int64, granularity string) (map[string]map[string]float64, error) {
	res := map[string]map[string]float64{}
	add := func(label, status string, v float64) {
		if res[label] == nil {
			res[label] = map[string]float64{}
		}
		res[label][status] += v
	}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketDatetime(timeCol, granularity)
		var rows []struct {
			Bucket string
			Status string
			Val    float64
		}
		q := r.db.WithContext(ctx).Table(table).
			Select(expr + " AS bucket, status, COALESCE(SUM(amount),0) AS val").
			Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if err := q.Group(expr + ", status").Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			add(x.Bucket, x.Status, x.Val)
		}
		return res, nil
	}
	var rows []struct {
		CreatedAt time.Time
		Status    string
		Amount    float64
	}
	q := r.db.WithContext(ctx).Table(table).
		Select(timeCol + " AS created_at, status, amount").
		Where(timeCol+" >= ? AND "+timeCol+" <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		label, _ := bucketize(granularity, x.CreatedAt.Unix())
		add(label, x.Status, x.Amount)
	}
	return res, nil
}

// bucketedSubPaidCost 在套餐 JOIN 上按日历桶 SUM(零售, 成本)（按 o.created_at），返回 label→[paid,cost]。
func (r *Repo) bucketedSubPaidCost(ctx context.Context, tenantID *int64, start, end int64, granularity string) (map[string][2]float64, error) {
	res := map[string][2]float64{}
	if common.UsingMainDatabase(common.DatabaseTypeMySQL) {
		expr := mysqlBucketDatetime("o.created_at", granularity)
		var rows []struct {
			Bucket string
			Paid   float64
			Cost   float64
		}
		q := r.db.WithContext(ctx).
			Table("pending_subscription_orders AS p").
			Select(expr + " AS bucket, COALESCE(SUM(p.retail_price),0) AS paid, COALESCE(SUM(p.agent_cost_price),0) AS cost").
			Joins("JOIN mt_subscription_orders AS o ON o.order_no = p.order_id").
			Where("o.status = ?", "activated").
			Where("o.created_at >= ? AND o.created_at <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "p.tenant_id", tenantID)
		if err := q.Group(expr).Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, x := range rows {
			res[x.Bucket] = [2]float64{x.Paid, x.Cost}
		}
		return res, nil
	}
	var rows []struct {
		CreatedAt time.Time
		Paid      float64
		Cost      float64
	}
	q := r.db.WithContext(ctx).
		Table("pending_subscription_orders AS p").
		Select("o.created_at AS created_at, p.retail_price AS paid, p.agent_cost_price AS cost").
		Joins("JOIN mt_subscription_orders AS o ON o.order_no = p.order_id").
		Where("o.status = ?", "activated").
		Where("o.created_at >= ? AND o.created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "p.tenant_id", tenantID)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, x := range rows {
		label, _ := bucketize(granularity, x.CreatedAt.Unix())
		cur := res[label]
		cur[0] += x.Paid
		cur[1] += x.Cost
		res[label] = cur
	}
	return res, nil
}

// tenantMeta 是 tenant_id → 展示名/owner 解析结果。
type tenantMeta struct {
	Name          string
	OwnerUserID   int64
	OwnerUsername string
}

// tenantNames 批量解析 tenant_id → (tenants.name, owner_user_id, users.username)。best-effort：
// 解析失败仅返回已得部分，缺失项为空串（报表不因名称缺失而失败）。
func (r *Repo) tenantNames(ctx context.Context, ids []int64) map[int64]tenantMeta {
	out := map[int64]tenantMeta{}
	if len(ids) == 0 {
		return out
	}
	var trows []struct {
		ID          int64
		Name        string
		OwnerUserID int64
	}
	if err := r.db.WithContext(ctx).Table("tenants").
		Select("id, name, owner_user_id").
		Where("id IN ?", ids).
		Scan(&trows).Error; err != nil {
		return out
	}
	ownerIDs := make([]int64, 0, len(trows))
	for _, t := range trows {
		out[t.ID] = tenantMeta{Name: t.Name, OwnerUserID: t.OwnerUserID}
		if t.OwnerUserID > 0 {
			ownerIDs = append(ownerIDs, t.OwnerUserID)
		}
	}
	if len(ownerIDs) > 0 {
		var urows []struct {
			ID       int64
			Username string
		}
		if err := r.db.WithContext(ctx).Table("users").
			Select("id, username").
			Where("id IN ?", ownerIDs).
			Scan(&urows).Error; err == nil {
			uname := make(map[int64]string, len(urows))
			for _, u := range urows {
				uname[u.ID] = u.Username
			}
			for tid, m := range out {
				m.OwnerUsername = uname[m.OwnerUserID]
				out[tid] = m
			}
		}
	}
	return out
}

// fillEarningNames 为收益明细批量回填 agent_name。
func (r *Repo) fillEarningNames(ctx context.Context, rows []EarningDetailRow) {
	ids := collectTenantIDs(rows, func(e EarningDetailRow) int64 { return e.TenantID })
	metas := r.tenantNames(ctx, ids)
	for i := range rows {
		rows[i].AgentName = metas[rows[i].TenantID].Name
	}
}

// scopeUserIDs 取 scope 内 user_id 集（分库降级用）：代理=tenant_id=tid；管理端=tenant_id<>0。
func (r *Repo) scopeUserIDs(ctx context.Context, tenantID *int64) ([]int64, error) {
	q := r.db.WithContext(ctx).Table("users").Where("deleted_at IS NULL")
	q = applyTenantScope(q, "tenant_id", tenantID)
	var ids []int64
	if err := q.Pluck("id", &ids).Error; err != nil {
		return nil, err
	}
	return ids, nil
}

// CountTenantUsers 返回归属某租户的下级用户数（软删除排除），与 scopeUserIDs 同过滤口径
// （tenant_id=? AND deleted_at IS NULL）。供 admin 代理升档决策指标（无现成 per-agent 用户计数聚合，
// 故补此一条薄查询）。
func (r *Repo) CountTenantUsers(ctx context.Context, tenantID int64) (int64, error) {
	var n int64
	if err := r.db.WithContext(ctx).Table("users").
		Where("tenant_id = ? AND deleted_at IS NULL", tenantID).
		Count(&n).Error; err != nil {
		return 0, err
	}
	return n, nil
}

// scopeUserTenant 取 scope 内 user_id→tenant_id 映射（分库降级用）。
func (r *Repo) scopeUserTenant(ctx context.Context, tenantID *int64) (map[int64]int64, error) {
	var rows []struct {
		ID       int64
		TenantID int64
	}
	q := r.db.WithContext(ctx).Table("users").
		Select("id, tenant_id").
		Where("deleted_at IS NULL")
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[int64]int64, len(rows))
	for _, x := range rows {
		m[x.ID] = x.TenantID
	}
	return m, nil
}

// logDB 返回日志库句柄：分库（LOG_DB!=DB）走 model.LOG_DB，否则走主库（含单测 globals 未初始化）。
func (r *Repo) logDB() *gorm.DB {
	if model.LOG_DB != nil && model.DB != nil && model.LOG_DB != model.DB {
		return model.LOG_DB
	}
	return r.db
}

// sameLogDB 报告日志库与主库是否同库（可 logs JOIN users）。globals 未初始化（单测）视为同库。
func (r *Repo) sameLogDB() bool {
	return !(model.LOG_DB != nil && model.DB != nil && model.LOG_DB != model.DB)
}

// ============================================================================
// 内部：纯函数辅助（分桶 / 方言表达式 / 作用域 / 排序 / 分页）
// ============================================================================

// unixT 把 epoch 秒转为 UTC time.Time（用于 DATETIME 台账列的区间比较）。
func unixT(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

// applyTenantScope 加租户作用域：tenantID!=nil → col=*tid；否则管理端 col<>0（排除主站/未归属）。
func applyTenantScope(q *gorm.DB, col string, tenantID *int64) *gorm.DB {
	if tenantID != nil {
		return q.Where(col+" = ?", *tenantID)
	}
	return q.Where(col + " <> 0")
}

// normGranularity 归一化粒度；非法回退 day（handler 另行校验并回 REPORT_GRANULARITY_INVALID）。
func normGranularity(g string) string {
	switch g {
	case "day", "week", "month":
		return g
	default:
		return "day"
	}
}

// bucketize 返回 UTC 日历桶标签 + 桶起始 epoch。week 以周一为界（UTC）。
func bucketize(granularity string, ts int64) (string, int64) {
	t := time.Unix(ts, 0).UTC()
	switch granularity {
	case "month":
		start := time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, time.UTC)
		return start.Format("2006-01"), start.Unix()
	case "week":
		offset := (int(t.Weekday()) + 6) % 7 // 周一=0 … 周日=6
		day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		start := day.AddDate(0, 0, -offset)
		return start.Format("2006-01-02"), start.Unix()
	default: // day
		start := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		return start.Format("2006-01-02"), start.Unix()
	}
}

// bucketStartTS 把 SQL 产出的桶标签解析回 UTC 起始 epoch（与 bucketize 同口径，跨方言一致）。
func bucketStartTS(granularity, label string) int64 {
	if granularity == "month" {
		if t, err := time.ParseInLocation("2006-01", label, time.UTC); err == nil {
			return t.Unix()
		}
		return 0
	}
	// day 与 week（week 标签即周一日期）
	if t, err := time.ParseInLocation("2006-01-02", label, time.UTC); err == nil {
		return t.Unix()
	}
	return 0
}

// mysqlBucketDatetime 构造 MySQL DATETIME 列的桶标签表达式。
func mysqlBucketDatetime(col, granularity string) string {
	switch granularity {
	case "month":
		return "DATE_FORMAT(" + col + ", '%Y-%m')"
	case "week":
		return "DATE_FORMAT(DATE_SUB(" + col + ", INTERVAL WEEKDAY(" + col + ") DAY), '%Y-%m-%d')"
	default:
		return "DATE_FORMAT(" + col + ", '%Y-%m-%d')"
	}
}

// mysqlBucketEpoch 构造 MySQL epoch 列的桶标签表达式（先 FROM_UNIXTIME 再走 DATETIME 口径）。
func mysqlBucketEpoch(col, granularity string) string {
	return mysqlBucketDatetime("FROM_UNIXTIME("+col+")", granularity)
}

// logRow 是分库降级路径下从 LOG_DB 取回的最小日志行。
type logRow struct {
	CreatedAt        int64
	Quota            int64
	PromptTokens     int64
	CompletionTokens int64
}

// lim 归一化页大小（<=0 → 20）。
func lim(pageSize int) int {
	if pageSize <= 0 {
		return 20
	}
	return pageSize
}

// off 计算 OFFSET（page<=0 视为 1）。
func off(page, pageSize int) int {
	if page <= 0 {
		page = 1
	}
	return (page - 1) * lim(pageSize)
}

// paginateSlice 在已排序切片上做内存分页。
func paginateSlice[T any](rows []T, page, pageSize int) []T {
	size := lim(pageSize)
	offset := off(page, pageSize)
	if offset >= len(rows) {
		return []T{}
	}
	end := offset + size
	if end > len(rows) {
		end = len(rows)
	}
	return rows[offset:end]
}

// collectTenantIDs 收集切片中去重、非零的 tenant_id（保序）。
func collectTenantIDs[T any](rows []T, get func(T) int64) []int64 {
	seen := map[int64]struct{}{}
	out := make([]int64, 0, len(rows))
	for _, row := range rows {
		id := get(row)
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// sortedStringKeys 返回 map 的升序键（day/week/month 标签按字典序即时间序）。
func sortedStringKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// sortedInt64Set 返回 set 的升序键。
func sortedInt64Set(set map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(set))
	for k := range set {
		ids = append(ids, k)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

// sortRanking 按白名单字段排序（稳定，租户号升序为同值兜底）；order!='asc' 即降序。
func sortRanking(rows []AgentRankRow, sortBy, order string) {
	less := rankLess(sortBy)
	asc := order == "asc"
	sort.SliceStable(rows, func(i, j int) bool {
		if asc {
			return less(rows[i], rows[j])
		}
		return less(rows[j], rows[i])
	})
}

// rankLess 返回升序比较器；sortBy 非白名单回退 total_earned_cny。
func rankLess(sortBy string) func(a, b AgentRankRow) bool {
	switch sortBy {
	case "recharge_paid_cny":
		return func(a, b AgentRankRow) bool { return a.RechargePaidCNY < b.RechargePaidCNY }
	case "subscription_paid_cny":
		return func(a, b AgentRankRow) bool { return a.SubscriptionPaidCNY < b.SubscriptionPaidCNY }
	case "consumption_cost_cny":
		return func(a, b AgentRankRow) bool { return a.ConsumptionCostCNY < b.ConsumptionCostCNY }
	case "consumption_used_quota":
		return func(a, b AgentRankRow) bool { return a.ConsumptionUsedQuota < b.ConsumptionUsedQuota }
	case "withdrawn_cny":
		return func(a, b AgentRankRow) bool { return a.WithdrawnCNY < b.WithdrawnCNY }
	case "pending_withdraw_cny":
		return func(a, b AgentRankRow) bool { return a.PendingWithdrawCNY < b.PendingWithdrawCNY }
	case "withdrawable_cny":
		return func(a, b AgentRankRow) bool { return a.WithdrawableCNY < b.WithdrawableCNY }
	default: // total_earned_cny
		return func(a, b AgentRankRow) bool { return a.TotalEarnedCNY < b.TotalEarnedCNY }
	}
}
