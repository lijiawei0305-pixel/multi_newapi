package payment

import (
	"context"
	"sync"
	"time"
)

// MemRepo 是 OrderRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
//
// 关键点：订单号唯一插入（Create）与状态机迁移（CompareAndSetStatus）在同一把互斥锁下完成
// 「读-判定-写」，**模拟 detailed-design §6.2 的唯一约束 + 状态机条件 UPDATE**：
// 高并发重复回调下，created→paid 的 CAS 只有一个胜者，从而不重复入账。
// 真实 GORM 实现（order_no 唯一索引、行锁/条件 UPDATE、scopeByTenant、迁移）顺延（见报告 TODO）。
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

// Create 落库新订单；order_no 已存在视为唯一约束冲突 → ErrOrderDuplicate。
func (r *MemRepo) Create(_ context.Context, o *PayOrder) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.orders[o.OrderNo]; ok {
		return ErrOrderDuplicate
	}
	cp := *o // 防御性拷贝，避免外部持有引用后篡改库内状态
	r.orders[o.OrderNo] = &cp
	return nil
}

// SetPayURL 回填 PayURL；订单不存在 → ErrOrderNotFound。
func (r *MemRepo) SetPayURL(_ context.Context, orderNo, payURL string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return ErrOrderNotFound
	}
	o.PayURL = payURL
	o.UpdatedAt = r.now()
	return nil
}

// GetByOrderNo 返回订单快照拷贝；不存在 → ErrOrderNotFound。
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

// CompareAndSetStatus 原子 CAS：临界区内「读当前状态-判定-写」。
// 仅当当前状态 == from 才置为 to 并刷新 UpdatedAt，返回 ok=true；
// 否则 ok=false（状态已被并发推进 / 已终态）。订单不存在 → ErrOrderNotFound。
func (r *MemRepo) CompareAndSetStatus(_ context.Context, orderNo string, from, to OrderStatus) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	o, ok := r.orders[orderNo]
	if !ok {
		return false, ErrOrderNotFound
	}
	if o.Status != from {
		return false, nil // CAS 失败：当前状态非预期 from
	}
	o.Status = to
	o.UpdatedAt = r.now()
	return true, nil
}

// ListByStatus 返回处于 status 且 UpdatedAt 早于 before 的订单快照拷贝（对账兜底扫描用）。
func (r *MemRepo) ListByStatus(_ context.Context, status OrderStatus, before time.Time) ([]*PayOrder, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []*PayOrder
	for _, o := range r.orders {
		if o.Status == status && o.UpdatedAt.Before(before) {
			cp := *o
			out = append(out, &cp)
		}
	}
	return out, nil
}
