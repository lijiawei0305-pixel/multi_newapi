package risk

import (
	"context"
	"sync"
	"time"
)

// kvEntry 是 MemKVCache 的内部条目。set 标识键存在（计数键或值键统一用 set 表达）。
type kvEntry struct {
	num      int64     // Incr 计数
	val      string    // SetNX 写入值
	expireAt time.Time // 过期时刻；零值表示不过期
}

// MemKVCache 是 KVCache 的并发安全内存假实现（限流计数 / 限购去重）。
// 过期按注入时钟惰性回收，保证与限流窗口/去重 TTL 的确定性单测一致。
// 真实 Redis 适配（INCR/EXPIRE/SET NX/TTL）顺延（见报告 TODO）。
type MemKVCache struct {
	mu    sync.Mutex
	data  map[string]kvEntry
	clock Clock
}

// 编译期断言：*MemKVCache 满足 KVCache 契约。
var _ KVCache = (*MemKVCache)(nil)

// NewMemKVCache 构造空缓存。clock 为 nil 时用系统时钟。
func NewMemKVCache(clock Clock) *MemKVCache {
	if clock == nil {
		clock = systemClock{}
	}
	return &MemKVCache{data: make(map[string]kvEntry), clock: clock}
}

// live 返回未过期的条目；已过期则删除并返回 ok=false。调用方须持有 mu。
func (c *MemKVCache) live(key string) (kvEntry, bool) {
	e, ok := c.data[key]
	if !ok {
		return kvEntry{}, false
	}
	if !e.expireAt.IsZero() && !c.clock.Now().Before(e.expireAt) {
		delete(c.data, key)
		return kvEntry{}, false
	}
	return e, true
}

// Incr 原子自增并返回新值；不存在则从 0→1（不改动既有过期）。
func (c *MemKVCache) Incr(_ context.Context, key string) (int64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.live(key)
	if !ok {
		e = kvEntry{}
	}
	e.num++
	c.data[key] = e
	return e.num, nil
}

// Get 读取 SetNX 写入的值；found=false 表示不存在或已过期。
func (c *MemKVCache) Get(_ context.Context, key string) (string, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.live(key)
	if !ok {
		return "", false, nil
	}
	return e.val, true, nil
}

// SetNX 仅当 key 不存在（或已过期）时写入；ttl<=0 表示不过期。返回是否写入成功。
func (c *MemKVCache) SetNX(_ context.Context, key, val string, ttl time.Duration) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.live(key); ok {
		return false, nil
	}
	e := kvEntry{val: val}
	if ttl > 0 {
		e.expireAt = c.clock.Now().Add(ttl)
	}
	c.data[key] = e
	return true, nil
}

// Expire 为已存在的 key 设置/刷新过期；ttl<=0 清除过期。key 不存在则 no-op。
func (c *MemKVCache) Expire(_ context.Context, key string, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.live(key)
	if !ok {
		return nil
	}
	if ttl > 0 {
		e.expireAt = c.clock.Now().Add(ttl)
	} else {
		e.expireAt = time.Time{}
	}
	c.data[key] = e
	return nil
}
