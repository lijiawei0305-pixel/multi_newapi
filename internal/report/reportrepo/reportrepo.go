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
// 代理收益/钱包/提现聚合优先读取 1e-8 BIGINT units 权威列，先按整数求和再在返回边界换成
// float64；仅为旧 schema/隔离测试保留 decimal 镜像回退。其余既有金额仍按原表 decimal 聚合。
// 仓储侧不做 2 位四舍五入——由 handler 边界 round(2)。时间：请求区间用 epoch 秒（int64）；
// DATETIME 台账表用 time.Unix(s,0).UTC() 比较，原生 logs.created_at 是 epoch 直接比。趋势按 UTC
// 日历分桶（day/week 周一/month）。
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
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// Repo 是财务报表聚合仓储。db 为主库（= model.DB）；日志查询按闸门走 model.LOG_DB。
type Repo struct {
	db               *gorm.DB
	moneyUnitColumns sync.Map
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

// EarningsTrendPoint 是收益趋势的一个日历桶（跨来源合计 amount_cny）；另按 v3 总览同款口径
// （agentFinanceOverviewOut）拆出两条可提现子序列，供代理「我的收益」3 线趋势图使用：
// TokenplanWithdrawableCNY = Σ source_type=tokenplan_spread；
// ConsumptionWithdrawableCNY = Σ source_type IN (ratio_markup, consume_commission)。
// 两者之和 <= AmountCNY（AmountCNY 还含 tokenplan_commission/manual_adjustment 等其余来源）。
type EarningsTrendPoint struct {
	Bucket                     string
	BucketTS                   int64
	AmountCNY                  float64
	TokenplanWithdrawableCNY   float64
	ConsumptionWithdrawableCNY float64
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

// NetIncomeTrendPoint 是管理端「净收入」趋势的一个日历桶（¥）：套餐净收入 / api净收入两条子序列
// （第三条「总净收入」= 两者之和，由 mtwire/前端相加，不入 wire）。口径见 NetIncomeTrend。
type NetIncomeTrendPoint struct {
	Bucket          string
	BucketTS        int64
	TokenplanNetCNY float64
	ApiNetCNY       float64
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
	unitsColumn, hasUnits, err := r.agentMoneyUnitColumn("agent_earning_logs", "amount")
	if err != nil {
		return nil, err
	}
	if hasUnits {
		var rows []struct {
			SourceType string
			Amount     int64
			RowCount   int64
			UnitCount  int64
		}
		q := r.db.WithContext(ctx).Table("agent_earning_logs").
			Select("source_type, COALESCE(SUM("+unitsColumn+"),0) AS amount, "+
				"COUNT(*) AS row_count, COUNT("+unitsColumn+") AS unit_count").
			Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if err := q.Group("source_type").Scan(&rows).Error; err != nil {
			return nil, err
		}
		out := make([]SourceSum, 0, len(rows))
		for _, row := range rows {
			if err := validateReportMoneyUnitCount("agent_earning_logs", unitsColumn, row.RowCount, row.UnitCount); err != nil {
				return nil, err
			}
			out = append(out, SourceSum{SourceType: row.SourceType, AmountCNY: reportMoneyFromUnits(row.Amount)})
		}
		return out, nil
	}

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
	_, hasUnits, err := r.agentMoneyUnitColumn("agent_wallets", "withdrawable_balance")
	if err != nil {
		return WalletAgg{}, err
	}
	if tenantID != nil {
		if hasUnits {
			var row struct {
				WithdrawableBalance  *int64
				FrozenWithdrawAmount *int64
				TotalEarned          *int64
				ApiBalance           *int64
			}
			err := r.db.WithContext(ctx).Table("agent_wallets").
				Select("withdrawable_balance_units AS withdrawable_balance, "+
					"frozen_withdraw_amount_units AS frozen_withdraw_amount, "+
					"total_earned_units AS total_earned, api_balance_units AS api_balance").
				Where("tenant_id = ?", *tenantID).
				Take(&row).Error
			if err != nil {
				if errors.Is(err, gorm.ErrRecordNotFound) {
					return WalletAgg{}, nil
				}
				return WalletAgg{}, err
			}
			for column, value := range map[string]*int64{
				"withdrawable_balance_units":   row.WithdrawableBalance,
				"frozen_withdraw_amount_units": row.FrozenWithdrawAmount,
				"total_earned_units":           row.TotalEarned,
				"api_balance_units":            row.ApiBalance,
			} {
				if value == nil {
					return WalletAgg{}, validateReportMoneyUnitCount("agent_wallets", column, 1, 0)
				}
			}
			return WalletAgg{
				WithdrawableCNY: reportMoneyFromUnits(*row.WithdrawableBalance),
				FrozenCNY:       reportMoneyFromUnits(*row.FrozenWithdrawAmount),
				TotalEarnedCNY:  reportMoneyFromUnits(*row.TotalEarned),
				APIBalanceUSD:   reportMoneyFromUnits(*row.ApiBalance),
			}, nil
		}

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
	if hasUnits {
		var row struct {
			Withdrawable      int64
			Frozen            int64
			TotalEarned       int64
			ApiBalance        int64
			RowCount          int64
			WithdrawableCount int64
			FrozenCount       int64
			TotalEarnedCount  int64
			APIBalanceCount   int64
		}
		err := r.db.WithContext(ctx).Table("agent_wallets").
			Select("COALESCE(SUM(withdrawable_balance_units),0) AS withdrawable, " +
				"COALESCE(SUM(frozen_withdraw_amount_units),0) AS frozen, " +
				"COALESCE(SUM(total_earned_units),0) AS total_earned, " +
				"COALESCE(SUM(api_balance_units),0) AS api_balance, " +
				"COUNT(*) AS row_count, COUNT(withdrawable_balance_units) AS withdrawable_count, " +
				"COUNT(frozen_withdraw_amount_units) AS frozen_count, COUNT(total_earned_units) AS total_earned_count, " +
				"COUNT(api_balance_units) AS api_balance_count").
			Where("tenant_id <> 0").
			Scan(&row).Error
		if err != nil {
			return WalletAgg{}, err
		}
		for column, count := range map[string]int64{
			"withdrawable_balance_units":   row.WithdrawableCount,
			"frozen_withdraw_amount_units": row.FrozenCount,
			"total_earned_units":           row.TotalEarnedCount,
			"api_balance_units":            row.APIBalanceCount,
		} {
			if err := validateReportMoneyUnitCount("agent_wallets", column, row.RowCount, count); err != nil {
				return WalletAgg{}, err
			}
		}
		return WalletAgg{
			WithdrawableCNY: reportMoneyFromUnits(row.Withdrawable),
			FrozenCNY:       reportMoneyFromUnits(row.Frozen),
			TotalEarnedCNY:  reportMoneyFromUnits(row.TotalEarned),
			APIBalanceUSD:   reportMoneyFromUnits(row.ApiBalance),
		}, nil
	}

	var row struct {
		Withdrawable float64
		Frozen       float64
		TotalEarned  float64
		ApiBalance   float64
	}
	err = r.db.WithContext(ctx).Table("agent_wallets").
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

// TenantEarningAgg 是按租户聚合的收益分桶（¥）。财务报表 v3 管理端总览
// （doc/finance-model-report-v3.md §二，internal/mtwire/report.go handleFinanceSummary 的
// adminFinanceOverview）与 AgentRanking 排行共用同一底座数据——见 EarningsByTenant。
type TenantEarningAgg struct {
	TotalCNY             float64
	ConsumeCommissionCNY float64
	RatioMarkupCNY       float64
	TokenplanSpreadCNY   float64
	ManualAdjustmentCNY  float64
}

// EarningsByTenant 按租户聚合收益分桶（跨租户 tenant_id<>0）：total + 四个具名来源分桶
// （consume_commission/ratio_markup/tokenplan_spread/manual_adjustment）。是 earningsByTenant
// （AgentRanking 内部用）的导出版本，供财务报表 v3 管理端总览做「主站/代理站」二分求和
// （按 tenant_id 是否为平台租户，见调用方 internal/mtwire/report.go）。两者查询相同、结果一一对应，
// 刻意不合并成一个方法：AgentRanking 早于本次改动、已有测试锁定其内部字段名，改法用「新增导出
// 包装」而非「重命名后更新旧测试」，最小化本次改动半径。
func (r *Repo) EarningsByTenant(ctx context.Context, start, end int64) (map[int64]TenantEarningAgg, error) {
	agg, err := r.earningsByTenant(ctx, start, end)
	if err != nil {
		return nil, err
	}
	out := make(map[int64]TenantEarningAgg, len(agg))
	for tid, a := range agg {
		out[tid] = TenantEarningAgg{
			TotalCNY:             a.total,
			ConsumeCommissionCNY: a.consume,
			RatioMarkupCNY:       a.ratioMarkup,
			TokenplanSpreadCNY:   a.tokenplanSpread,
			ManualAdjustmentCNY:  a.manualAdj,
		}
	}
	return out, nil
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

// Withdrawals 按状态汇总提现金额（¥）：status ∈ pending/approved/paid/rejected（withdrawn 口径=paid，即真正打款出账）。
// frozen 口径由 WalletTotals.FrozenCNY 提供（= SUM(agent_wallets.frozen_withdraw_amount)）。
func (r *Repo) Withdrawals(ctx context.Context, tenantID *int64, start, end int64) (map[string]float64, error) {
	unitsColumn, hasUnits, err := r.agentMoneyUnitColumn("agent_withdrawals", "amount")
	if err != nil {
		return nil, err
	}
	if hasUnits {
		var rows []struct {
			Status    string
			Amount    int64
			RowCount  int64
			UnitCount int64
		}
		q := r.db.WithContext(ctx).Table("agent_withdrawals").
			Select("status, COALESCE(SUM("+unitsColumn+"),0) AS amount, "+
				"COUNT(*) AS row_count, COUNT("+unitsColumn+") AS unit_count").
			Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
		q = applyTenantScope(q, "tenant_id", tenantID)
		if err := q.Group("status").Scan(&rows).Error; err != nil {
			return nil, err
		}
		out := make(map[string]float64, len(rows))
		for _, row := range rows {
			if err := validateReportMoneyUnitCount("agent_withdrawals", unitsColumn, row.RowCount, row.UnitCount); err != nil {
				return nil, err
			}
			out[row.Status] = reportMoneyFromUnits(row.Amount)
		}
		return out, nil
	}

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
