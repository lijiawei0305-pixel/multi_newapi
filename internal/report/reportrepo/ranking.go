package reportrepo

import (
	"context"

	"github.com/QuantumNous/new-api/model"
)

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
		case "paid":
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
