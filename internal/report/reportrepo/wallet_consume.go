package reportrepo

// 钱包消耗台账（mt_wallet_consume_log）读侧聚合：财务报表 v3「钱包消耗」口径的精确数据源
// （doc/finance-model-report-v3.md §二 + §落地改动）。
//
// 为什么单独一张台账、而不是继续从 logs 里挖：单次消耗事件的资金来源是「单一」的——要么全走钱包桶
// （users.quota），要么全走套餐(订阅)桶（见 service/billing_session.go：一个 BillingSession 只绑定一个
// FundingSource；订阅桶不足时在*预扣*阶段整单回退钱包，绝不在结算内把一次消费劈成两半，
// PostConsumeUserSubscriptionDelta 甚至在超额时直接报错而非溢出到钱包）。故「钱包消耗」= billingSource==
// wallet 的事件的全额消耗额度。写入侧在结算收口 internal/mtwire.creditConsumeCommission
// （agenthook.ConsumeCommission，两个结算点 service/quota.go 与 service/text_quota.go 都经过它）按
// billingSource 甄别后落 mt_wallet_consume_log，与代理分润入账同一租户口径（users.tenant_id）。
// 本文件只做读侧聚合（原生 db.Table()，不 import 兄弟模块 model 结构，同本包一贯约定）。
//
// 租户口径与消耗透镜一致：wallet_quota 按写入时解析的 tenant_id（= users.tenant_id）归属；管理端跨租户
// 聚合排除 tenant_id=0（主站未归属，与 applyTenantScope 同约定），代理端按单租户过滤。台账落主库
// （非 LOG_DB），created_at 为 DATETIME，区间比较用 unixT（同 agent_earning_logs）。换算 quota→¥ 复用
// QuotaToCNY（与 consume_commission 同口径，可对账）。

import "context"

// WalletConsumption 返回 scope 内钱包桶消耗额度合计（quota 单位）：代理=该租户；管理端(tenantID==nil)=
// 跨租户(<>0)。由 handler 经 QuotaToCNY 换算为 ¥（agentFinanceOverviewOut.ApikeyConsumptionCNY 的精确来源）。
func (r *Repo) WalletConsumption(ctx context.Context, tenantID *int64, start, end int64) (int64, error) {
	var row struct{ Quota int64 }
	q := r.db.WithContext(ctx).Table("mt_wallet_consume_log").
		Select("COALESCE(SUM(wallet_quota),0) AS quota").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end))
	q = applyTenantScope(q, "tenant_id", tenantID)
	if err := q.Scan(&row).Error; err != nil {
		return 0, err
	}
	return row.Quota, nil
}

// WalletConsumptionByTenant 按租户聚合钱包桶消耗额度（quota 单位，跨租户 tenant_id<>0），供财务报表 v3
// 管理端总览做主站/代理站二分（splitByPlatform，见 internal/mtwire/report.go adminFinanceOverview）。
// 与 EarningsByTenant / ConsumptionCost 同风格：仓储只按 tenant 聚合，主站/代理站的口径判定留给上层。
func (r *Repo) WalletConsumptionByTenant(ctx context.Context, start, end int64) (map[int64]int64, error) {
	var rows []struct {
		TenantID int64
		Quota    int64
	}
	q := r.db.WithContext(ctx).Table("mt_wallet_consume_log").
		Select("tenant_id, COALESCE(SUM(wallet_quota),0) AS quota").
		Where("created_at >= ? AND created_at <= ?", unixT(start), unixT(end)).
		Where("tenant_id <> 0").
		Group("tenant_id")
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[int64]int64, len(rows))
	for _, x := range rows {
		out[x.TenantID] = x.Quota
	}
	return out, nil
}
