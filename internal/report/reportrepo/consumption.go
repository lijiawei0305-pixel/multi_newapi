package reportrepo

import (
	"context"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
)

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
			Select(expr+" AS bucket, COALESCE(SUM(l.quota),0) AS used_quota, COUNT(*) AS calls, COALESCE(SUM(l.prompt_tokens + l.completion_tokens),0) AS tokens").
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

// 注：财务报表 v3 管理端总览的「主站/代理站钱包消耗」曾复用 ConsumptionByTenant（logs 全量消耗、钱包+
// 套餐桶混合的上界）；本次改为专门的钱包消耗台账聚合 WalletConsumptionByTenant（wallet_consume.go，纯钱包
// 桶），口径精确后该导出包装已无调用方，随之删除。内部 consumptionByTenant（AgentRanking 排行用，全量消耗
// 口径，见 ConsumptionCost/consumption 透镜）保留不变。
