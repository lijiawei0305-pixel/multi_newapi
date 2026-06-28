package tenant

import (
	"context"
	"sync"
)

// MemCache 是 Cache 的并发安全内存假实现（Host->Tenant）。
// 存取均做值拷贝，避免调用方与缓存共享可变指针。
// 真实 Redis 适配（序列化 + TTL + 写后失效）顺延（见报告 TODO）。
type MemCache struct {
	mu   sync.RWMutex
	data map[string]Tenant
}

// NewMemCache 构造空缓存。
func NewMemCache() *MemCache {
	return &MemCache{data: make(map[string]Tenant)}
}

func (c *MemCache) Get(_ context.Context, host string) (*Tenant, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	t, ok := c.data[host]
	if !ok {
		return nil, false
	}
	cp := t
	return &cp, true
}

func (c *MemCache) Set(_ context.Context, host string, t *Tenant) {
	if t == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[host] = *t
}

// Invalidate 主动失效一个 Host 缓存（对应 §6.3 写后失效；本轮预留，未接中间件）。
func (c *MemCache) Invalidate(_ context.Context, host string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.data, host)
}
