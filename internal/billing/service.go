package billing

import (
	"context"

	"github.com/QuantumNous/new-api/internal/platform/quota"
)

// defaultMultiplier 是计费倍率默认值 x1（上游成本价直计，不叠分组倍率，见 §7 默认假设 #2）。
const defaultMultiplier = 1.0

// service 是 BillingService 的实现。所有依赖均为本包声明的消费者接口，便于全 mock 单测。
type service struct {
	catalog  ModelCatalog
	router   quota.Router
	logs     CallLogWriter
	earnings EarningSink
}

// 编译期确认 service 满足 BillingService。
var _ BillingService = (*service)(nil)

// NewService 构造 BillingService。
//   - catalog：模型官方价（算上游成本）
//   - router：双桶路由（见 NewQuotaRouter）
//   - logs：计费日志落库
//   - earnings：消耗分润入账
func NewService(catalog ModelCatalog, router quota.Router, logs CallLogWriter, earnings EarningSink) BillingService {
	return &service{catalog: catalog, router: router, logs: logs, earnings: earnings}
}

// Charge 执行一次调用计费，编排顺序（doc/detailed-design.md §2.5 / §3.1）：
//
//	算上游成本 → 选桶 → 原子扣减 → 写计费日志 → 触发消耗分润。
//
// 错误传播：ModelCatalog/选桶/扣减任一失败均原样上浮、不吞码；其中桶扣减失败
// （QUOTA_INSUFFICIENT / SUBSCRIPTION_EXHAUSTED / SUBSCRIPTION_EXPIRED）时
// 不写日志、不分润、不回退到其它桶（独立计量不回退，§8.1）。
//
// TODO(txn)：扣减+日志+分润应在同一事务内（platform/txn.WithTx，§6.1）。本轮未接真实
// 事务，故日志/分润写失败仅上浮错误；真实实现下该错误会触发整笔回滚。
func (s *service) Charge(ctx context.Context, req ChargeRequest) (*BillingResult, error) {
	inPrice, outPrice, err := s.catalog.Price(ctx, req.Model)
	if err != nil {
		return nil, err // 模型未定价/不可用，原样上浮
	}

	upstreamCost := float64(req.PromptTokens)*inPrice + float64(req.CompletionTokens)*outPrice
	mult := req.Multiplier
	if mult <= 0 {
		mult = defaultMultiplier
	}
	charged := upstreamCost * mult

	src, err := s.router.Select(ctx, req.UserID, req.TenantID)
	if err != nil {
		return nil, err // 选桶失败，原样上浮
	}

	receipt, err := src.Charge(ctx, charged)
	if err != nil {
		// 桶不足/超额/过期：原样上浮，不回退、不写日志、不分润。
		return nil, err
	}

	grossProfit := receipt.ChargedUSD - upstreamCost

	// 写计费日志（与扣费、分润同一事务 —— 真实 txn 见上方 TODO）。
	if err := s.logs.Write(ctx, CallLogEntry{
		RequestID:        req.RequestID,
		TenantID:         req.TenantID,
		UserID:           req.UserID,
		Model:            req.Model,
		PromptTokens:     req.PromptTokens,
		CompletionTokens: req.CompletionTokens,
		BucketKind:       receipt.Kind,
		UpstreamCostUSD:  upstreamCost,
		ChargedUSD:       receipt.ChargedUSD,
		GrossProfitUSD:   grossProfit,
		GroupKey:         req.GroupKey,
	}); err != nil {
		return nil, err
	}

	// 触发消耗分润：以本次消耗额为基数，Agent 侧按分润比例换算入账。
	if err := s.earnings.AddEarning(ctx, EarningEntry{
		TenantID:  req.TenantID,
		UserID:    req.UserID,
		Source:    SourceConsumeCommission,
		AmountUSD: receipt.ChargedUSD,
		Model:     req.Model,
		RefKey:    req.RequestID,
	}); err != nil {
		return nil, err
	}

	return &BillingResult{
		BucketKind:      receipt.Kind,
		UpstreamCostUSD: upstreamCost,
		ChargedUSD:      receipt.ChargedUSD,
		GrossProfitUSD:  grossProfit,
		RemainingUSD:    receipt.RemainingUSD,
	}, nil
}
