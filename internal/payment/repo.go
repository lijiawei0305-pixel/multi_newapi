package payment

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MemRepo 是 OrderRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
//
// 关键点：订单号唯一插入（Create）与状态机迁移（CompareAndSetStatus）在同一把互斥锁下完成
// 「读-判定-写」，**模拟 detailed-design §6.2 的唯一约束 + 状态机条件 UPDATE**。
type MemRepo struct {
	mu     sync.Mutex
	orders map[string]*PayOrder // key: order_no
	now    func() time.Time
}

// 编译期断言：MemRepo 实现 OrderRepo。
var _ OrderRepo = (*MemRepo)(nil)

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		orders: make(map[string]*PayOrder),
		now:    time.Now,
	}
}

// Create 落库新订单；order_no / idempotency_key 冲突 → ErrOrderDuplicate。
func (r *MemRepo) Create(_ context.Context, o *PayOrder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orders[o.OrderNo]; ok {
		return ErrOrderDuplicate
	}
	if o.IdempotencyKey != "" {
		for _, existing := range r.orders {
			if existing.IdempotencyKey == o.IdempotencyKey {
				return ErrOrderDuplicate
			}
		}
	}
	cp := *o
	r.orders[o.OrderNo] = &cp
	return nil
}

// SetPayURL 回填 PayURL。
func (r *MemRepo) SetPayURL(ctx context.Context, orderNo, payURL string) error {
	return r.SetPayURLFenced(ctx, orderNo, payURL, "", nil)
}

// SetPayURLFenced 内存版条件写回。
func (r *MemRepo) SetPayURLFenced(_ context.Context, orderNo, payURL, claimToken string, allowedStates []CreateState) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return ErrOrderNotFound
	}
	if o.Status != OrderCreated {
		return ErrOrderNotFound
	}
	if claimToken != "" && o.QueryClaimToken != claimToken && o.RecoveryClaimToken != claimToken {
		return ErrOrderNotFound
	}
	if len(allowedStates) > 0 {
		okState := false
		for _, s := range allowedStates {
			if o.CreateState == s || (o.CreateState == "" && s == CreateStateLocalCreated) {
				okState = true
				break
			}
		}
		if !okState {
			return ErrOrderNotFound
		}
	}
	o.PayURL = payURL
	o.CreateState = CreateStateCredentialReady
	o.UpdatedAt = r.now()
	return nil
}

// GetByOrderNo 返回订单快照拷贝。
func (r *MemRepo) GetByOrderNo(_ context.Context, orderNo string) (*PayOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return nil, ErrOrderNotFound
	}
	cp := *o
	return &cp, nil
}

// GetByIdempotencyKey 按幂等键查。
func (r *MemRepo) GetByIdempotencyKey(_ context.Context, key string) (*PayOrder, error) {
	if key == "" {
		return nil, ErrOrderNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.orders {
		if o.IdempotencyKey == key {
			cp := *o
			return &cp, nil
		}
	}
	return nil, ErrOrderNotFound
}

// GetByProviderTxn 按渠道+交易号查。
func (r *MemRepo) GetByProviderTxn(_ context.Context, provider Provider, txnID string) (*PayOrder, error) {
	if txnID == "" {
		return nil, ErrOrderNotFound
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, o := range r.orders {
		if o.Provider == provider && o.ProviderTransactionID == txnID {
			cp := *o
			return &cp, nil
		}
	}
	return nil, ErrOrderNotFound
}

// CompareAndSetStatus 原子 CAS。
func (r *MemRepo) CompareAndSetStatus(_ context.Context, orderNo string, from, to OrderStatus) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != from {
		return false, nil
	}
	o.Status = to
	o.UpdatedAt = r.now()
	if to == OrderCredited {
		o.CreditedAt = r.now()
		o.NextQueryAt = time.Time{}
	}
	if to == OrderFailed {
		o.NextQueryAt = time.Time{}
	}
	return true, nil
}

// ListByStatus 扫描并截断。
func (r *MemRepo) ListByStatus(_ context.Context, status OrderStatus, before time.Time, limit int) ([]*PayOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*PayOrder
	for _, o := range r.orders {
		if o.Status == status && o.UpdatedAt.Before(before) {
			cp := *o
			out = append(out, &cp)
		}
	}
	sortOrdersByUpdated(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ListDueForQuery 返回 due 的 created 订单。
// ListPendingPrepay 内存版：local_created + 无 pay_url + 未过期 + 无有效 lease。
func (r *MemRepo) ListPendingPrepay(_ context.Context, now time.Time, limit int) ([]*PayOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*PayOrder
	for _, o := range r.orders {
		if o.Status != OrderCreated {
			continue
		}
		cs := o.CreateState
		if cs == "" {
			cs = CreateStateLocalCreated
		}
		if cs != CreateStateLocalCreated {
			continue
		}
		if strings.TrimSpace(o.PayURL) != "" {
			continue
		}
		if !o.ExpiresAt.IsZero() && !o.ExpiresAt.After(now) {
			continue
		}
		if !o.RecoveryClaimUntil.IsZero() && o.RecoveryClaimUntil.After(now) {
			continue
		}
		if !o.QueryClaimUntil.IsZero() && o.QueryClaimUntil.After(now) {
			continue
		}
		cp := *o
		out = append(out, &cp)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out, nil
}

func (r *MemRepo) ListDueForQuery(_ context.Context, now time.Time, limit int) ([]*PayOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*PayOrder
	for _, o := range r.orders {
		if o.Status == OrderCreated && !o.NextQueryAt.IsZero() && !o.NextQueryAt.After(now) {
			cp := *o
			out = append(out, &cp)
		}
	}
	// ORDER BY next_query_at, order_no
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].NextQueryAt.Before(out[i].NextQueryAt) ||
				(out[j].NextQueryAt.Equal(out[i].NextQueryAt) && out[j].OrderNo < out[i].OrderNo) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// SavePaymentFacts 内存版支付事实。
func (r *MemRepo) SavePaymentFacts(_ context.Context, orderNo string, facts PaymentFacts) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return ErrOrderNotFound
	}
	if facts.ProviderTransactionID != "" {
		for no, other := range r.orders {
			if no != orderNo && other.ProviderTransactionID == facts.ProviderTransactionID &&
				other.Provider == o.Provider {
				return ErrProviderTxnConflict
			}
		}
		o.ProviderTransactionID = facts.ProviderTransactionID
	}
	if !facts.ProviderPaidAt.IsZero() {
		o.ProviderPaidAt = facts.ProviderPaidAt
	}
	if !facts.CallbackReceivedAt.IsZero() && o.CallbackReceivedAt.IsZero() {
		o.CallbackReceivedAt = facts.CallbackReceivedAt
	}
	if facts.ClearNextQuery {
		o.NextQueryAt = time.Time{}
	}
	o.UpdatedAt = r.now()
	return nil
}

// MarkCredited paid→credited。
func (r *MemRepo) MarkCredited(_ context.Context, orderNo string, at time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != OrderPaid {
		return false, nil
	}
	if at.IsZero() {
		at = r.now()
	}
	o.Status = OrderCredited
	o.CreditedAt = at
	o.NextQueryAt = time.Time{}
	o.UpdatedAt = r.now()
	return true, nil
}

// ScheduleNextQuery 更新调度。
func (r *MemRepo) ScheduleNextQuery(_ context.Context, orderNo string, nextAt time.Time, attempts int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return ErrOrderNotFound
	}
	o.NextQueryAt = nextAt
	o.QueryAttempts = attempts
	o.UpdatedAt = r.now()
	return nil
}

// ClaimForQuery 内存版：有效 Prepay lease 互斥；lease 过期后允许 Query（含 prepay_inflight 崩溃恢复）。
func (r *MemRepo) ClaimForQuery(_ context.Context, orderNo string, now, leaseUntil time.Time) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return "", false, ErrOrderNotFound
	}
	if o.Status != OrderCreated {
		return "", false, nil
	}
	if !o.NextQueryAt.IsZero() && o.NextQueryAt.After(now) {
		return "", false, nil
	}
	if !o.QueryClaimUntil.IsZero() && o.QueryClaimUntil.After(now) {
		return "", false, nil
	}
	// 有效 operation lease 互斥；过期后允许 Query-first 恢复
	if !o.RecoveryClaimUntil.IsZero() && o.RecoveryClaimUntil.After(now) {
		return "", false, nil
	}
	token := fmt.Sprintf("q-%d", now.UnixNano())
	o.QueryClaimToken = token
	o.QueryClaimUntil = leaseUntil
	o.UpdatedAt = r.now()
	return token, true, nil
}

// ClaimForPrepay 内存版：仅 local_created|empty 可认领；prepay_unknown 必须先经 Query NOT_EXIST 重置。
func (r *MemRepo) ClaimForPrepay(_ context.Context, orderNo string, now, leaseUntil time.Time) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return "", false, ErrOrderNotFound
	}
	if o.Status != OrderCreated {
		return "", false, nil
	}
	cs := o.CreateState
	if cs == "" {
		cs = CreateStateLocalCreated
	}
	if cs != CreateStateLocalCreated {
		return "", false, nil
	}
	if !o.QueryClaimUntil.IsZero() && o.QueryClaimUntil.After(now) {
		return "", false, nil
	}
	if !o.RecoveryClaimUntil.IsZero() && o.RecoveryClaimUntil.After(now) {
		return "", false, nil
	}
	token := fmt.Sprintf("p-%d", now.UnixNano())
	o.CreateState = CreateStatePrepayInflight
	o.RecoveryClaimToken = token
	o.RecoveryClaimUntil = leaseUntil
	o.NextQueryAt = leaseUntil
	o.UpdatedAt = r.now()
	return token, true, nil
}

// FinishPrepayFenced 内存版。
func (r *MemRepo) FinishPrepayFenced(_ context.Context, orderNo, token string, to CreateState, payURL string, nextQueryAt time.Time, errorClass, stage string, createAttempts int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != OrderCreated || o.RecoveryClaimToken != token {
		return false, nil
	}
	o.RecoveryClaimToken = ""
	o.RecoveryClaimUntil = time.Time{}
	o.CreateState = to
	if payURL != "" {
		o.PayURL = payURL
	}
	if !nextQueryAt.IsZero() {
		o.NextQueryAt = nextQueryAt
	} else if to == CreateStateDefinitiveReject {
		o.NextQueryAt = time.Time{}
	}
	if errorClass != "" {
		o.LastErrorClass = errorClass
		o.LastNetworkStage = stage
		o.CreateAttempts = createAttempts
	}
	if to == CreateStateDefinitiveReject {
		o.Status = OrderFailed
		o.NextQueryAt = time.Time{}
	}
	o.UpdatedAt = r.now()
	return true, nil
}

// FinishQueryFenced 内存版 fenced 完成。
func (r *MemRepo) FinishQueryFenced(_ context.Context, orderNo, token string, nextAt time.Time, attempts int, createState CreateState, tradeState string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != OrderCreated || o.QueryClaimToken != token {
		return false, nil
	}
	o.QueryClaimToken = ""
	o.QueryClaimUntil = time.Time{}
	o.QueryAttempts = attempts
	o.NextQueryAt = nextAt
	if createState != "" {
		o.CreateState = createState
	}
	if tradeState != "" {
		o.ProviderTradeState = tradeState
	}
	o.UpdatedAt = r.now()
	return true, nil
}

// ReleaseQueryClaim 内存版释放。
func (r *MemRepo) ReleaseQueryClaim(_ context.Context, orderNo, token string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return nil
	}
	if o.QueryClaimToken == token {
		o.QueryClaimToken = ""
		o.QueryClaimUntil = time.Time{}
		o.UpdatedAt = r.now()
	}
	return nil
}

// TransitionCreateState 内存版 CAS create_state。
func (r *MemRepo) TransitionCreateState(_ context.Context, orderNo string, from, to CreateState) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != OrderCreated {
		return false, nil
	}
	cur := o.CreateState
	if cur == "" {
		cur = CreateStateLocalCreated
	}
	if cur != from {
		return false, nil
	}
	o.CreateState = to
	o.UpdatedAt = r.now()
	return true, nil
}

// TransitionCreateStateFenced 内存版。
func (r *MemRepo) TransitionCreateStateFenced(_ context.Context, orderNo, token string, from, to CreateState) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != OrderCreated || o.RecoveryClaimToken != token {
		return false, nil
	}
	cur := o.CreateState
	if cur == "" {
		cur = CreateStateLocalCreated
	}
	if cur != from {
		return false, nil
	}
	o.CreateState = to
	o.UpdatedAt = r.now()
	return true, nil
}

// ScheduleNextQueryFenced 内存版。
func (r *MemRepo) ScheduleNextQueryFenced(_ context.Context, orderNo, token string, nextAt time.Time, attempts int) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != OrderCreated {
		return false, nil
	}
	if o.QueryClaimToken != token && o.RecoveryClaimToken != token {
		return false, nil
	}
	o.NextQueryAt = nextAt
	o.QueryAttempts = attempts
	o.UpdatedAt = r.now()
	return true, nil
}

// RecordCreateFailure 记录创建失败观测。
func (r *MemRepo) RecordCreateFailure(_ context.Context, orderNo string, errorClass, stage string, createAttempts int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return ErrOrderNotFound
	}
	o.LastErrorClass = errorClass
	o.LastNetworkStage = stage
	o.CreateAttempts = createAttempts
	o.UpdatedAt = r.now()
	return nil
}

func sortOrdersByUpdated(out []*PayOrder) {
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].UpdatedAt.Before(out[i].UpdatedAt) ||
				(out[j].UpdatedAt.Equal(out[i].UpdatedAt) && out[j].OrderNo < out[i].OrderNo) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
}
