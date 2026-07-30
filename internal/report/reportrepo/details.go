package reportrepo

import (
	"context"
	"sort"
	"time"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/model"
)

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
	unitsColumn, hasUnits, err := r.agentMoneyUnitColumn("agent_earning_logs", "amount")
	if err != nil {
		return nil, 0, err
	}
	if hasUnits {
		var rows []struct {
			TenantID   int64
			SourceType string
			Amount     *int64
			SourceID   string
			CreatedAt  time.Time
		}
		if err := where().
			Select("tenant_id, source_type, " + unitsColumn + " AS amount, source_id, created_at").
			Order("created_at DESC").
			Limit(lim(pageSize)).Offset(off(page, pageSize)).
			Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		out := make([]EarningDetailRow, 0, len(rows))
		for _, row := range rows {
			if row.Amount == nil {
				return nil, 0, validateReportMoneyUnitCount("agent_earning_logs", unitsColumn, 1, 0)
			}
			out = append(out, EarningDetailRow{
				TenantID:   row.TenantID,
				SourceType: row.SourceType,
				AmountCNY:  reportMoneyFromUnits(*row.Amount),
				Reference:  row.SourceID,
				CreatedAt:  row.CreatedAt,
			})
		}
		r.fillEarningNames(ctx, out)
		return out, total, nil
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
	unitsColumn, hasUnits, err := r.agentMoneyUnitColumn("agent_withdrawals", "amount")
	if err != nil {
		return nil, 0, err
	}
	if hasUnits {
		var rows []struct {
			ID         int64
			TenantID   int64
			Amount     *int64
			Status     string
			CreatedAt  time.Time
			ReviewedAt *time.Time
		}
		if err := where().
			Select("id, tenant_id, " + unitsColumn + " AS amount, status, created_at, reviewed_at").
			Order("created_at DESC").
			Limit(lim(pageSize)).Offset(off(page, pageSize)).
			Scan(&rows).Error; err != nil {
			return nil, 0, err
		}
		out := make([]WithdrawalDetailRow, 0, len(rows))
		for _, row := range rows {
			if row.Amount == nil {
				return nil, 0, validateReportMoneyUnitCount("agent_withdrawals", unitsColumn, 1, 0)
			}
			out = append(out, WithdrawalDetailRow{
				ID:         row.ID,
				TenantID:   row.TenantID,
				AmountCNY:  reportMoneyFromUnits(*row.Amount),
				Status:     row.Status,
				CreatedAt:  row.CreatedAt,
				ReviewedAt: row.ReviewedAt,
			})
		}
		ids := collectTenantIDs(out, func(w WithdrawalDetailRow) int64 { return w.TenantID })
		metas := r.tenantNames(ctx, ids)
		for i := range out {
			out[i].AgentName = metas[out[i].TenantID].Name
		}
		return out, total, nil
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
