package tokenplan

import (
	"context"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/quota"
)

// quotaFactory 是 SubscriptionQuotaFactory 的实现，为某 Principal/用户绑定其 active 套餐桶。
type quotaFactory struct {
	repo  SubscriptionRepo
	clock Clock
}

// 编译期断言：工厂满足契约、套餐桶满足共享 quota.Source。
var (
	_ SubscriptionQuotaFactory = (*quotaFactory)(nil)
	_ quota.Source             = (*subscriptionQuota)(nil)
)

// NewQuotaFactory 构造 SubscriptionQuotaFactory。clock 为 nil 时回退真实时钟。
func NewQuotaFactory(repo SubscriptionRepo, clock Clock) SubscriptionQuotaFactory {
	return &quotaFactory{repo: repo, clock: orSystemClock(clock)}
}

// For 匹配 detailed-design §2.7：有 active 套餐→(套餐桶, true)，否则 (nil, false)。
// 无 ctx 入参（对齐设计签名），活跃判定用 context.Background()。
func (f *quotaFactory) For(p appctx.Principal) (quota.Source, bool) {
	sub, err := f.repo.GetActiveByUser(context.Background(), p.UserID, f.clock.Now())
	if err != nil || sub == nil {
		return nil, false
	}
	return f.bind(sub), true
}

// SubscriptionSource 匹配 billing.SubscriptionSourceFactory：返回绑定到用户 active 订阅的桶。
// 选桶器（QuotaRouter）只在 HasActive 为真时调用；竞态下 active 已消失则返回 SUBSCRIPTION_EXPIRED。
func (f *quotaFactory) SubscriptionSource(ctx context.Context, userID, _ int64) (quota.Source, error) {
	sub, err := f.repo.GetActiveByUser(ctx, userID, f.clock.Now())
	if err != nil {
		return nil, err
	}
	if sub == nil {
		return nil, ErrSubscriptionExpired
	}
	return f.bind(sub), nil
}

// bind 绑定一个套餐额度桶到具体订阅实例（月限额取购买快照）。
func (f *quotaFactory) bind(sub *Subscription) *subscriptionQuota {
	return &subscriptionQuota{
		repo:       f.repo,
		clock:      f.clock,
		subID:      sub.ID,
		monthLimit: sub.MonthLimitUSD,
	}
}

// subscriptionQuota 是 quota.Source 的套餐实现：月限额即额度，扣减走 Meter 的原子条件 UPDATE。
type subscriptionQuota struct {
	repo       SubscriptionRepo
	clock      Clock
	subID      int64
	monthLimit float64 // 购买快照，作 RemainingUSD 基准
}

// Charge 适配 Meter：原子 used_usd += cost；超额/过期由 Repo 上浮
// SUBSCRIPTION_EXHAUSTED / SUBSCRIPTION_EXPIRED（独立计量不回退）。
// 成功返回 quota.Receipt{Kind: subscription, ChargedUSD, RemainingUSD = month_limit − used}。
func (q *subscriptionQuota) Charge(ctx context.Context, costUSD float64) (quota.Receipt, error) {
	if !validAmount(costUSD) || costUSD < 0 {
		return quota.Receipt{}, ErrAmountInvalid
	}
	used, err := q.repo.Meter(ctx, q.subID, costUSD, q.clock.Now())
	if err != nil {
		return quota.Receipt{}, err // SUBSCRIPTION_EXHAUSTED / SUBSCRIPTION_EXPIRED
	}
	return quota.Receipt{
		Kind:         quota.KindSubscription,
		ChargedUSD:   costUSD,
		RemainingUSD: remaining(q.monthLimit, used),
	}, nil
}

// Balance 返回当前月额度剩余（month_limit − used，下限 0）。
func (q *subscriptionQuota) Balance(ctx context.Context) (float64, error) {
	sub, err := q.repo.GetByID(ctx, q.subID)
	if err != nil {
		return 0, err
	}
	return remaining(q.monthLimit, sub.UsedUSD), nil
}
