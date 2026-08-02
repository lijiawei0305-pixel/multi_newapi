package risk

import (
	"context"
	"strconv"
	"sync"
	"time"
)

// MemPurchaseLedger 是 PurchaseLedger 的进程内实现，供单测与无 DB 场景验证
// 「台账权威、KV 可抹掉」语义。生产用 gormrepo。
type MemPurchaseLedger struct {
	mu    sync.Mutex
	rows  map[string]memClaim // key = scope + "\x00" + claimKey
	clock Clock               // 可选；nil 时用 now 参数（ClaimTrial 传入）
}

type memClaim struct {
	owner     int64
	count     int
	expiresAt int64 // 0 = 永不过期
}

// 编译期断言。
var _ PurchaseLedger = (*MemPurchaseLedger)(nil)

// NewMemPurchaseLedger 构造空台账。
func NewMemPurchaseLedger() *MemPurchaseLedger {
	return &MemPurchaseLedger{rows: map[string]memClaim{}}
}

func memKey(scope, claimKey string) string { return scope + "\x00" + claimKey }

// ClaimTrial 事务式多维占用（进程内锁模拟）。
func (m *MemPurchaseLedger) ClaimTrial(_ context.Context, userID int64, pi PurchaseIdentity, deviceTTL time.Duration, now time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	nowUnix := now.Unix()
	m.purgeExpiredLocked(nowUnix)

	type dim struct {
		scope, key string
		ttl        time.Duration
	}
	dims := []dim{{ScopeTrialUser, strconv.FormatInt(userID, 10), 0}}
	if pi.RealNameID != "" {
		dims = append(dims, dim{ScopeTrialRealname, pi.RealNameID, 0})
	}
	if pi.DeviceID != "" {
		dims = append(dims, dim{ScopeTrialDevice, pi.DeviceID, deviceTTL})
	}

	// 预检：任一占用即拒（不写部分）
	for _, d := range dims {
		if c, ok := m.rows[memKey(d.scope, d.key)]; ok {
			if c.expiresAt > 0 && c.expiresAt <= nowUnix {
				delete(m.rows, memKey(d.scope, d.key))
				continue
			}
			return ErrPurchaseLimitExceeded
		}
	}
	// 写入
	for _, d := range dims {
		var exp int64
		if d.ttl > 0 {
			exp = now.Add(d.ttl).Unix()
		}
		m.rows[memKey(d.scope, d.key)] = memClaim{owner: userID, count: 1, expiresAt: exp}
	}
	return nil
}

func (m *MemPurchaseLedger) purgeExpiredLocked(nowUnix int64) {
	for k, c := range m.rows {
		if c.expiresAt > 0 && c.expiresAt <= nowUnix {
			delete(m.rows, k)
		}
	}
}

// ReleaseTrial 释放台账行。
func (m *MemPurchaseLedger) ReleaseTrial(_ context.Context, userID int64, pi PurchaseIdentity, force bool) (TrialReleaseResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	res := TrialReleaseResult{}
	// user 维：按 userID 建键，恒删
	uk := memKey(ScopeTrialUser, strconv.FormatInt(userID, 10))
	delete(m.rows, uk)
	res.Released = append(res.Released, "user")

	shared := []struct{ dim, id, scope string }{
		{"realname", pi.RealNameID, ScopeTrialRealname},
		{"device", pi.DeviceID, ScopeTrialDevice},
	}
	for _, s := range shared {
		if s.id == "" {
			continue
		}
		k := memKey(s.scope, s.id)
		c, ok := m.rows[k]
		if !ok {
			continue
		}
		if c.owner != userID {
			if !force {
				res.Skipped = append(res.Skipped, s.dim)
				continue
			}
			// force：仅遗留/脏值可绕过——内存台账无遗留 "1"，另一真实用户一律拒
			res.Skipped = append(res.Skipped, s.dim)
			continue
		}
		delete(m.rows, k)
		res.Released = append(res.Released, s.dim)
	}
	return res, nil
}

// ClaimPlan 计数 +1。
func (m *MemPurchaseLedger) ClaimPlan(_ context.Context, planID, userID int64, limit int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := memKey(ScopePlan, planClaimKey(planID, userID))
	c := m.rows[k]
	if c.count >= limit {
		return ErrPurchaseLimitExceeded
	}
	c.count++
	c.owner = userID
	m.rows[k] = c
	return nil
}

// RollbackPlan 计数 -1。
func (m *MemPurchaseLedger) RollbackPlan(_ context.Context, planID, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	k := memKey(ScopePlan, planClaimKey(planID, userID))
	c, ok := m.rows[k]
	if !ok {
		return nil
	}
	c.count--
	if c.count <= 0 {
		delete(m.rows, k)
		return nil
	}
	m.rows[k] = c
	return nil
}

// ReleasePlan 整行删除。
func (m *MemPurchaseLedger) ReleasePlan(_ context.Context, planID, userID int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.rows, memKey(ScopePlan, planClaimKey(planID, userID)))
	return nil
}

// Scope 常量与 gormrepo 对齐。
const (
	ScopeTrialUser     = "trial_user"
	ScopeTrialRealname = "trial_realname"
	ScopeTrialDevice   = "trial_device"
	ScopePlan          = "plan"
)

func planClaimKey(planID, userID int64) string {
	return strconv.FormatInt(planID, 10) + ":" + strconv.FormatInt(userID, 10)
}
