package wallet

import (
	"context"
	"sync"
	"time"
)

// balKey 是用户余额的复合主键（多租户隔离）。
type balKey struct {
	tenantID int64
	userID   int64
}

// codeKey 是兑换码的租户内唯一定位键。
type codeKey struct {
	tenantID int64
	code     string
}

// MemRepo 是 WalletRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
//
// 关键点：余额扣减（ChargeBalance）与兑换码翻牌（UseRedemption）在同一把互斥锁下完成
// 「读-判定-写」，**模拟 detailed-design §6.2 的原子条件 UPDATE**，高并发下零穿透/零透支。
// 生产 GORM 实现（条件 UPDATE / 行锁、scopeByTenant、迁移）位于 gormrepo 子包。
type MemRepo struct {
	mu        sync.Mutex
	balances  map[balKey]float64
	codes     map[int64]*RedemptionCode
	codeIndex map[codeKey]int64
	nextID    int64
	now       func() time.Time
}

// 编译期断言：MemRepo 实现 WalletRepo。
var _ WalletRepo = (*MemRepo)(nil)

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		balances:  make(map[balKey]float64),
		codes:     make(map[int64]*RedemptionCode),
		codeIndex: make(map[codeKey]int64),
		now:       time.Now,
	}
}

func (r *MemRepo) Balance(_ context.Context, tenantID, userID int64) (float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.balances[balKey{tenantID, userID}], nil
}

func (r *MemRepo) AddBalance(_ context.Context, tenantID, userID int64, deltaUSD float64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.balances[balKey{tenantID, userID}] += deltaUSD
	return nil
}

// ChargeBalance 原子条件扣减：临界区内「读-判定-写」，仅当 balance-cost>=0 才扣减。
func (r *MemRepo) ChargeBalance(_ context.Context, tenantID, userID int64, costUSD float64) (float64, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := balKey{tenantID, userID}
	cur := r.balances[key]
	if cur-costUSD < 0 { // 余额不足 → 0 行受影响 → 拦截
		return cur, ErrQuotaInsufficient
	}
	cur -= costUSD
	r.balances[key] = cur
	return cur, nil
}

// AddRedemption 预置一张兑换码（模拟代理建码「从自己额度预扣」）。回填 c.ID。
// 供 main 真实建码流程与单测共用；并发安全。
func (r *MemRepo) AddRedemption(c *RedemptionCode) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextID++
	c.ID = r.nextID
	if c.Status == "" {
		c.Status = RedemptionEnabled
	}
	if c.CreatedAt.IsZero() {
		c.CreatedAt = r.now()
	}
	cp := *c
	r.codes[c.ID] = &cp
	r.codeIndex[codeKey{c.TenantID, c.Code}] = c.ID
}

func (r *MemRepo) GetRedemption(_ context.Context, tenantID int64, code string) (*RedemptionCode, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.codeIndex[codeKey{tenantID, code}]
	if !ok {
		return nil, ErrRedeemCodeInvalid
	}
	cp := *r.codes[id]
	return &cp, nil
}

// UseRedemption 原子 CAS：临界区内仅当状态仍为 enabled 才翻为 used。
// 状态非 enabled（已被并发用掉/禁用）→ ok=false，调用方据此返回 REDEEM_CODE_USED。
func (r *MemRepo) UseRedemption(_ context.Context, id, userID int64, now time.Time) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.codes[id]
	if !ok {
		return false, ErrRedeemCodeInvalid
	}
	if c.Status != RedemptionEnabled {
		return false, nil // CAS 失败：已非 enabled
	}
	c.Status = RedemptionUsed
	c.UsedByUserID = userID
	c.UsedAt = now
	return true, nil
}
