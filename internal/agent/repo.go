package agent

import (
	"context"
	"sync"
	"time"
)

// agentRecord 是代理资料的内部存储结构（按 userID）。
type agentRecord struct {
	t         AgentType
	params    AgentParams
	updatedAt time.Time
}

// MemRepo 是 AgentRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测，
// 也可作为 cmd/main 早期装配桩。真实 GORM 实现（迁移 + scopeByTenant + 条件 UPDATE）顺延（见报告 TODO）。
//
// 单 mutex 守护全部状态：AppendEarning / CreateWithdrawal / ResolveWithdrawal 在锁内完成
// 读-改-写，等价于 detailed-design §6.2 的原子条件更新，保证 -race 下的幂等与金额守恒。
type MemRepo struct {
	mu          sync.Mutex
	profiles    map[int64]agentRecord  // userID -> 代理资料
	wallets     map[int64]*AgentWallet // tenantID -> 钱包
	earnings    []EarningEntry         // 收益日志（追加）
	seenEarning map[string]bool        // 幂等键 -> 已入账
	withdrawals map[int64]*Withdrawal  // id -> 提现单
	nextWID     int64
	now         func() time.Time
}

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		profiles:    make(map[int64]agentRecord),
		wallets:     make(map[int64]*AgentWallet),
		seenEarning: make(map[string]bool),
		withdrawals: make(map[int64]*Withdrawal),
		now:         time.Now,
	}
}

// walletRef 返回 tenantID 的钱包指针（不存在则惰性创建）。调用方须持有 mu。
func (r *MemRepo) walletRef(tenantID int64) *AgentWallet {
	w := r.wallets[tenantID]
	if w == nil {
		w = &AgentWallet{TenantID: tenantID, UpdatedAt: r.now()}
		r.wallets[tenantID] = w
	}
	return w
}

// SeedAPIBalance 为测试 / 早期装配设置某租户的 API 额度。
// 非 AgentRepo 接口方法：API 额度由 Wallet/Billing 维护，本模块只读展示（见报告 TODO）。
func (r *MemRepo) SeedAPIBalance(tenantID int64, amount float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.walletRef(tenantID)
	w.APIBalance = amount
	w.UpdatedAt = r.now()
}

func (r *MemRepo) SetAgentType(_ context.Context, userID int64, t AgentType, p AgentParams) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.profiles[userID] = agentRecord{t: t, params: p, updatedAt: r.now()}
	return nil
}

func (r *MemRepo) GetAgentType(_ context.Context, userID int64) (AgentType, AgentParams, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.profiles[userID]
	if !ok {
		return "", AgentParams{}, false, nil
	}
	return rec.t, rec.params, true, nil
}

func (r *MemRepo) GetWallet(_ context.Context, tenantID int64) (*AgentWallet, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cp := *r.walletRef(tenantID) // 返回副本，避免外部改动泄漏到内部状态
	return &cp, nil
}

func (r *MemRepo) AppendEarning(_ context.Context, e EarningEntry) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := e.IdempotencyKey()
	if r.seenEarning[key] {
		return false, nil // 幂等：同来源不重复入账
	}
	r.seenEarning[key] = true
	if e.CreatedAt.IsZero() {
		e.CreatedAt = r.now()
	}
	r.earnings = append(r.earnings, e)
	w := r.walletRef(e.TenantID)
	if e.UserID != 0 {
		w.UserID = e.UserID
	}
	w.WithdrawableBalance += e.Amount
	w.TotalEarned += e.Amount
	w.UpdatedAt = r.now()
	return true, nil
}

func (r *MemRepo) CreateWithdrawal(_ context.Context, wd *Withdrawal) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w := r.walletRef(wd.TenantID)
	if wd.Amount <= 0 || wd.Amount > w.WithdrawableBalance {
		return ErrWithdrawInsufficient
	}
	// 原子冻结：可提现 → 冻结（金额守恒：withdrawable + frozen 不变）。
	w.WithdrawableBalance -= wd.Amount
	w.FrozenWithdrawAmount += wd.Amount
	if wd.UserID != 0 {
		w.UserID = wd.UserID
	}
	ts := r.now()
	w.UpdatedAt = ts
	r.nextWID++
	wd.ID = r.nextWID
	wd.Status = WithdrawPending
	wd.CreatedAt = ts
	wd.UpdatedAt = ts
	stored := *wd // 内部存独立副本，与调用方返回值解耦
	r.withdrawals[wd.ID] = &stored
	return nil
}

func (r *MemRepo) GetWithdrawal(_ context.Context, id int64) (*Withdrawal, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	wd, ok := r.withdrawals[id]
	if !ok {
		return nil, ErrWithdrawNotFound
	}
	cp := *wd
	return &cp, nil
}

func (r *MemRepo) ResolveWithdrawal(_ context.Context, id int64, target WithdrawStatus, remark string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	wd, ok := r.withdrawals[id]
	if !ok {
		return ErrWithdrawNotFound
	}
	if !wd.Status.CanTransitionTo(target) {
		return ErrWithdrawNotPending
	}
	w := r.walletRef(wd.TenantID)
	switch target {
	case WithdrawApproved:
		// 线下打款：扣减冻结，资金离开系统。
		w.FrozenWithdrawAmount -= wd.Amount
	case WithdrawRejected:
		// 解冻退回：冻结 → 可提现（金额守恒：withdrawable + frozen 复原）。
		w.FrozenWithdrawAmount -= wd.Amount
		w.WithdrawableBalance += wd.Amount
	default:
		return ErrWithdrawNotPending
	}
	ts := r.now()
	w.UpdatedAt = ts
	wd.Status = target
	wd.Remark = remark
	wd.UpdatedAt = ts
	wd.ReviewedAt = ts
	return nil
}
