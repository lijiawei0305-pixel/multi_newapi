package tokenplan

import (
	"context"
	"sort"
	"sync"
	"time"
)

// listingKey 是 (tenant_id, plan_id) 上架记录唯一键（对应 UNIQUE(tenant_id, plan_id)）。
type listingKey struct {
	tenantID int64
	planID   int64
}

// MemRepo 是 PlanRepo + SubscriptionRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
//
// 关键点：Meter（月度计量）与 ActivateFromOrder（幂等激活）在同一把互斥锁下完成
// 「读-判定-写」，**模拟 detailed-design §6.2 的原子条件 UPDATE**，高并发下零穿透/零重复。
// 真实 GORM 实现（条件 UPDATE / 行锁、scopeByTenant、迁移）顺延（见报告 TODO）。
type MemRepo struct {
	mu sync.Mutex

	plans      map[int64]*Plan
	nextPlanID int64

	listings      map[listingKey]*TenantPlan
	nextListingID int64

	subs        map[int64]*Subscription
	subsByOrder map[string]int64 // source_order_id -> subID（幂等）
	nextSubID   int64

	pending map[string]*PendingPurchase // order_id -> 购买意图

	usageLogs   []UsageLog
	nextUsageID int64
}

// 编译期断言：MemRepo 同时实现 PlanRepo 与 SubscriptionRepo。
var (
	_ PlanRepo         = (*MemRepo)(nil)
	_ SubscriptionRepo = (*MemRepo)(nil)
)

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		plans:       make(map[int64]*Plan),
		listings:    make(map[listingKey]*TenantPlan),
		subs:        make(map[int64]*Subscription),
		subsByOrder: make(map[string]int64),
		pending:     make(map[string]*PendingPurchase),
	}
}

// ---- PlanRepo ----

func (r *MemRepo) CreatePlan(_ context.Context, p *Plan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextPlanID++
	p.ID = r.nextPlanID
	now := time.Now()
	if p.CreatedAt.IsZero() {
		p.CreatedAt = now
	}
	p.UpdatedAt = now
	cp := *p
	r.plans[p.ID] = &cp
	return nil
}

func (r *MemRepo) UpdatePlan(_ context.Context, id int64, in PlanInput) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cur, ok := r.plans[id]
	if !ok {
		return ErrPlanNotFound
	}
	updated := in.toPlan()
	updated.ID = id
	updated.CreatedAt = cur.CreatedAt
	updated.UpdatedAt = time.Now()
	r.plans[id] = updated
	return nil
}

func (r *MemRepo) GetPlan(_ context.Context, id int64) (*Plan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.plans[id]
	if !ok {
		return nil, ErrPlanNotFound
	}
	cp := *p
	return &cp, nil
}

func (r *MemRepo) ListPlans(_ context.Context) ([]Plan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Plan, 0, len(r.plans))
	for _, p := range r.plans {
		out = append(out, *p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Sort != out[j].Sort {
			return out[i].Sort < out[j].Sort
		}
		return out[i].ID < out[j].ID
	})
	return out, nil
}

func (r *MemRepo) GetListing(_ context.Context, tenantID, planID int64) (*TenantPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	tp, ok := r.listings[listingKey{tenantID, planID}]
	if !ok {
		return nil, nil // 未上架不是错误
	}
	cp := *tp
	return &cp, nil
}

func (r *MemRepo) UpsertListing(_ context.Context, tp *TenantPlan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := listingKey{tp.TenantID, tp.PlanID}
	now := time.Now()
	if cur, ok := r.listings[key]; ok {
		cur.Enabled = tp.Enabled
		cur.RetailPrice = tp.RetailPrice
		cur.UpdatedAt = now
		tp.ID = cur.ID
		tp.CreatedAt = cur.CreatedAt
		tp.UpdatedAt = now
		return nil
	}
	r.nextListingID++
	tp.ID = r.nextListingID
	tp.CreatedAt = now
	tp.UpdatedAt = now
	cp := *tp
	r.listings[key] = &cp
	return nil
}

func (r *MemRepo) ListListings(_ context.Context, tenantID int64) ([]TenantPlan, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]TenantPlan, 0)
	for _, tp := range r.listings {
		if tp.TenantID == tenantID {
			out = append(out, *tp)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].PlanID < out[j].PlanID })
	return out, nil
}

// ---- SubscriptionRepo ----

func (r *MemRepo) SavePendingPurchase(_ context.Context, p *PendingPurchase) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *p
	r.pending[p.OrderID] = &cp
	return nil
}

func (r *MemRepo) GetPendingPurchase(_ context.Context, orderID string) (*PendingPurchase, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	p, ok := r.pending[orderID]
	if !ok {
		return nil, ErrSubscriptionNotFound
	}
	cp := *p
	return &cp, nil
}

// ActivateFromOrder 幂等创建：临界区内按 SourceOrderID 去重，模拟唯一约束 + 行锁。
func (r *MemRepo) ActivateFromOrder(_ context.Context, sub *Subscription) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sub.SourceOrderID != "" {
		if id, ok := r.subsByOrder[sub.SourceOrderID]; ok {
			*sub = *r.subs[id] // 回填既有实例，调用方据此返回幂等结果
			return false, nil
		}
	}
	r.nextSubID++
	sub.ID = r.nextSubID
	cp := *sub
	r.subs[sub.ID] = &cp
	if sub.SourceOrderID != "" {
		r.subsByOrder[sub.SourceOrderID] = sub.ID
	}
	return true, nil
}

// GetActiveByUser 返回用户 active 订阅（按 now 惰性过期）；无则 (nil, nil)。
func (r *MemRepo) GetActiveByUser(_ context.Context, userID int64, now time.Time) (*Subscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	// 选最近创建的 active 实例，保证多实例时确定性。
	var found *Subscription
	for _, s := range r.subs {
		if s.UserID != userID || s.Status != SubActive {
			continue
		}
		if s.isExpiredAt(now) { // 惰性过期：读时顺手翻为 expired
			s.Status = SubExpired
			s.UpdatedAt = now
			continue
		}
		if found == nil || s.ID > found.ID {
			found = s
		}
	}
	if found == nil {
		return nil, nil
	}
	cp := *found
	return &cp, nil
}

func (r *MemRepo) GetByID(_ context.Context, id int64) (*Subscription, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.subs[id]
	if !ok {
		return nil, ErrSubscriptionNotFound
	}
	cp := *s
	return &cp, nil
}

// Meter 原子条件累加（detailed-design §6.2）：临界区内「读-判定-写」。
//
//	active 且未过期 且 used+cost<=month_limit → used+=cost，写计量日志，返回 nil
//	used+cost>month_limit                     → 置 exhausted，返回 ErrSubscriptionExhausted（整笔拒绝）
//	now>=expire_at                            → 置 expired，返回 ErrSubscriptionExpired
//	非 active（含 refunded）                   → 按既有终态返回对应错误
func (r *MemRepo) Meter(_ context.Context, subID int64, costUSD float64, now time.Time) (float64, error) {
	if !validAmount(costUSD) || costUSD < 0 {
		return 0, ErrAmountInvalid
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.subs[subID]
	if !ok {
		return 0, ErrSubscriptionNotFound
	}
	// 惰性过期优先于额度判定。
	if s.Status == SubActive && s.isExpiredAt(now) {
		s.Status = SubExpired
		s.UpdatedAt = now
	}
	switch s.Status {
	case SubExpired, SubRefunded:
		// refunded 一期不可达 GetActive（不会被选桶），直接 Meter 视为不可计量。
		return s.UsedUSD, ErrSubscriptionExpired
	case SubExhausted:
		return s.UsedUSD, ErrSubscriptionExhausted
	case SubActive:
		if s.UsedUSD+costUSD > s.MonthLimitUSD { // 0 行 → 超额 → 置 exhausted（整笔拒绝）
			s.Status = SubExhausted
			s.UpdatedAt = now
			return s.UsedUSD, ErrSubscriptionExhausted
		}
		s.UsedUSD += costUSD
		s.UpdatedAt = now
		r.nextUsageID++
		r.usageLogs = append(r.usageLogs, UsageLog{
			ID:              r.nextUsageID,
			SubscriptionID:  s.ID,
			TenantID:        s.TenantID,
			UserID:          s.UserID,
			UpstreamCostUSD: costUSD,
			CreatedAt:       now,
		})
		return s.UsedUSD, nil
	default:
		return s.UsedUSD, ErrSubscriptionNotFound
	}
}

// ---- 测试/装配辅助 ----

// UsageLogs 返回某订阅的计量日志副本（断言计量记录用）。
func (r *MemRepo) UsageLogs(subID int64) []UsageLog {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]UsageLog, 0)
	for _, l := range r.usageLogs {
		if l.SubscriptionID == subID {
			out = append(out, l)
		}
	}
	return out
}
