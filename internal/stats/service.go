package stats

import (
	"context"

	"newapi-mt/internal/platform/appctx"
)

// DefaultAlertThreshold 是满额预警默认阈值：used_usd/month_limit ≥ 0.8（80%）即预警。
// 对应 doc/tasks/12-stats.md A 节与 doc/proposal.md §2.4「满额逼近监控/告警」。
const DefaultAlertThreshold = 0.8

// service 是 StatsService 的实现。依赖均为本包声明的只读消费者接口，便于全 mock / 内存假实现单测。
type service struct {
	billing   BillingReader
	subs      SubscriptionReader
	threshold float64 // 满额预警阈值，(0,1]
}

// 编译期确认 service 满足 StatsService。
var _ StatsService = (*service)(nil)

// NewService 用默认预警阈值（0.8）构造 StatsService。
func NewService(billing BillingReader, subs SubscriptionReader) StatsService {
	return NewServiceWithThreshold(billing, subs, DefaultAlertThreshold)
}

// NewServiceWithThreshold 用自定义满额预警阈值构造 StatsService。
// threshold 必须落在 (0,1]；非法值（≤0 或 >1）回退为 DefaultAlertThreshold。
func NewServiceWithThreshold(billing BillingReader, subs SubscriptionReader, threshold float64) StatsService {
	if threshold <= 0 || threshold > 1 {
		threshold = DefaultAlertThreshold
	}
	return &service{billing: billing, subs: subs, threshold: threshold}
}

// AdminOverview 聚合全站每租户计费汇总：租户数 = 行数；活跃数 = Active 行数；
// 调用量/扣费/代理收益 = 各行求和。读取失败原样上浮（不吞码）。
func (s *service) AdminOverview(ctx context.Context) (*AdminStats, error) {
	rows, err := s.billing.TenantBillings(ctx)
	if err != nil {
		return nil, err
	}
	out := &AdminStats{}
	for _, r := range rows {
		out.TotalTenants++
		if r.Active {
			out.ActiveTenants++
		}
		out.TotalCalls += r.Calls
		out.TotalChargedUSD += r.ChargedUSD
		out.TotalEarningUSD += r.EarningUSD
	}
	return out, nil
}

// TenantOverview 返回单租户统计，严格限定本租户范围（隔离）。
//
// 防御性隔离（defense-in-depth，§1.3）：若 ctx 携带非管理员 Principal 且其 TenantID
// 与请求的 tenantID 不一致 → ErrCrossTenant。管理员（或无 Principal 的内部调用）放行；
// 真实数据隔离仍由底层 Reader 的 scopeByTenant 强制（见 reader.go / 报告 TODO）。
func (s *service) TenantOverview(ctx context.Context, tenantID int64) (*TenantStats, error) {
	if p, ok := appctx.PrincipalFrom(ctx); ok && !p.IsAdmin() && p.TenantID != tenantID {
		return nil, ErrCrossTenant
	}
	r, err := s.billing.TenantBillingByID(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	return &TenantStats{
		TenantID:   tenantID,
		UserCount:  r.UserCount,
		TokenCount: r.TokenCount,
		Calls:      r.Calls,
		RevenueUSD: r.RevenueUSD,
		EarningUSD: r.EarningUSD,
	}, nil
}

// SubscriptionAlerts 扫描全站 active 订阅，输出 used/limit ≥ 阈值者。读取失败原样上浮。
// 结果按相同输入稳定（顺序由 Reader 决定），未命中返回 nil 切片。
func (s *service) SubscriptionAlerts(ctx context.Context) ([]SubAlert, error) {
	subs, err := s.subs.ActiveSubscriptions(ctx)
	if err != nil {
		return nil, err
	}
	var alerts []SubAlert
	for _, u := range subs {
		ratio := usageRatio(u.UsedUSD, u.MonthLimitUSD)
		if ratio >= s.threshold {
			alerts = append(alerts, SubAlert{
				SubscriptionID: u.SubscriptionID,
				TenantID:       u.TenantID,
				UserID:         u.UserID,
				PlanCode:       u.PlanCode,
				UsedUSD:        u.UsedUSD,
				MonthLimitUSD:  u.MonthLimitUSD,
				Ratio:          ratio,
			})
		}
	}
	return alerts, nil
}

// usageRatio 计算 used/limit，并安全处理非法/零月限额：
//   - limit > 0  → used/limit；
//   - limit ≤ 0 且 used > 0 → 1.0（限额缺失但已有消耗，保守视为已满 → 触发告警，防巨亏）；
//   - limit ≤ 0 且 used ≤ 0 → 0（无消耗无风险）。
//
// 避免除零产生 +Inf/NaN，保证 SubAlert.Ratio 始终为有限值。
func usageRatio(used, limit float64) float64 {
	if limit > 0 {
		return used / limit
	}
	if used > 0 {
		return 1.0
	}
	return 0
}
