package identity

import (
	"context"
	"sync"
)

// MemTokenStore 是 TokenStore 的内存假实现（本轮使用；GORM 实现见 TODO）。
// 并发安全，可作为 cmd/main 早期装配桩，也支撑 -race 下的并发用例。
type MemTokenStore struct {
	mu     sync.RWMutex
	byHash map[string]TokenRecord
}

// NewMemTokenStore 构造空的内存令牌库。
func NewMemTokenStore() *MemTokenStore {
	return &MemTokenStore{byHash: make(map[string]TokenRecord)}
}

// Put 以原始令牌注册一条记录（内部按 hex(sha256) 存储，不留明文）。
func (s *MemTokenStore) Put(rawToken string, rec TokenRecord) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.byHash[HashToken(rawToken)] = rec
}

// FindByHash 实现 TokenStore：按 hash 反查，返回内部记录的副本。
func (s *MemTokenStore) FindByHash(ctx context.Context, tokenHash string) (*TokenRecord, bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rec, ok := s.byHash[tokenHash]
	if !ok {
		return nil, false, nil
	}
	out := rec // 拷贝，避免外部改动泄漏到内部 map
	return &out, true, nil
}

// MemTenantStatusChecker 是 TenantStatusChecker 的内存假实现。
type MemTenantStatusChecker struct {
	mu     sync.RWMutex
	status map[int64]TenantStatus
}

// NewMemTenantStatusChecker 构造空的内存租户状态表。
func NewMemTenantStatusChecker() *MemTenantStatusChecker {
	return &MemTenantStatusChecker{status: make(map[int64]TenantStatus)}
}

// Set 设置某租户状态。
func (c *MemTenantStatusChecker) Set(tenantID int64, s TenantStatus) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.status[tenantID] = s
}

// StatusOf 实现 TenantStatusChecker。
func (c *MemTenantStatusChecker) StatusOf(ctx context.Context, tenantID int64) (TenantStatus, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.status[tenantID]
	if !ok {
		return "", false, nil
	}
	return s, true, nil
}
