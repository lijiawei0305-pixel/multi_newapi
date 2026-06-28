package stats

import (
	"context"
	"sort"
	"sync"
)

// MemBillingReader 是 BillingReader 的并发安全内存假实现，用于本轮纯逻辑开发、单测，
// 以及 cmd/main 在真实只读仓储就绪前的临时装配。
// 真实只读 GORM 实现（tenant_billing_logs/agent_earning_logs 聚合 + scopeByTenant 强制隔离）
// 顺延（见报告 TODO）。
type MemBillingReader struct {
	mu   sync.RWMutex
	rows map[int64]TenantBilling // tenantID -> 该租户计费汇总
}

// 编译期确认 MemBillingReader 满足 BillingReader。
var _ BillingReader = (*MemBillingReader)(nil)

// NewMemBillingReader 构造空的内存计费读模型。
func NewMemBillingReader() *MemBillingReader {
	return &MemBillingReader{rows: make(map[int64]TenantBilling)}
}

// Put 写入/覆盖某租户的计费汇总（按 TenantBilling.TenantID 索引）。
func (m *MemBillingReader) Put(b TenantBilling) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.rows[b.TenantID] = b
}

// TenantBillings 返回全站每租户一行的汇总，按 TenantID 升序（稳定，便于断言/展示）。
func (m *MemBillingReader) TenantBillings(_ context.Context) ([]TenantBilling, error) {
	m.mu.RLock()
	out := make([]TenantBilling, 0, len(m.rows))
	for _, b := range m.rows {
		out = append(out, b)
	}
	m.mu.RUnlock()
	sort.Slice(out, func(i, j int) bool { return out[i].TenantID < out[j].TenantID })
	return out, nil
}

// TenantBillingByID 返回指定租户的汇总（隔离：仅按该 tenantID 命中）。
// 未命中时返回回填了 TenantID 的零值汇总（看板显示 0），不报错。
func (m *MemBillingReader) TenantBillingByID(_ context.Context, tenantID int64) (TenantBilling, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	b, ok := m.rows[tenantID]
	if !ok {
		return TenantBilling{TenantID: tenantID}, nil
	}
	return b, nil
}

// MemSubscriptionReader 是 SubscriptionReader 的并发安全内存假实现。
// 真实只读实现（user_subscriptions 中 status=active 的用量投影）顺延（见报告 TODO）。
type MemSubscriptionReader struct {
	mu   sync.RWMutex
	subs []SubscriptionUsage
}

// 编译期确认 MemSubscriptionReader 满足 SubscriptionReader。
var _ SubscriptionReader = (*MemSubscriptionReader)(nil)

// NewMemSubscriptionReader 构造空的内存订阅读模型。
func NewMemSubscriptionReader() *MemSubscriptionReader {
	return &MemSubscriptionReader{}
}

// Add 追加一条 active 订阅用量投影。
func (m *MemSubscriptionReader) Add(u SubscriptionUsage) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.subs = append(m.subs, u)
}

// ActiveSubscriptions 返回全部 active 订阅用量的拷贝（避免外部共享底层切片）。
func (m *MemSubscriptionReader) ActiveSubscriptions(_ context.Context) ([]SubscriptionUsage, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]SubscriptionUsage, len(m.subs))
	copy(out, m.subs)
	return out, nil
}
