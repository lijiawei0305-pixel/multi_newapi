package promotion

import (
	"context"
	"sync"
	"time"
)

// MemRepo 是 PromotionRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
// 真实 GORM 实现（迁移 + prefix 唯一约束 + 原子 registered_count++ + scopeByTenant）顺延（见报告 TODO）。
type MemRepo struct {
	mu            sync.RWMutex
	channels      map[int64]Channel
	prefixIndex   map[string]int64      // prefix -> channelID（业务唯一键）
	codeIndex     map[string]int64      // channel_code -> channelID（归属查询）
	attributions  map[int64]Attribution // userID -> 归属记录
	nextChannelID int64
	now           func() time.Time
}

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		channels:     make(map[int64]Channel),
		prefixIndex:  make(map[string]int64),
		codeIndex:    make(map[string]int64),
		attributions: make(map[int64]Attribution),
		now:          time.Now,
	}
}

func (r *MemRepo) CreateChannel(_ context.Context, c *Channel) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.prefixIndex[c.Prefix]; ok {
		return ErrChannelPrefixDup
	}
	r.nextChannelID++
	c.ID = r.nextChannelID
	ts := r.now()
	if c.CreatedAt.IsZero() {
		c.CreatedAt = ts
	}
	c.UpdatedAt = ts
	r.channels[c.ID] = *c
	r.prefixIndex[c.Prefix] = c.ID
	r.codeIndex[c.ChannelCode] = c.ID
	return nil
}

func (r *MemRepo) GetChannelByCode(_ context.Context, code string) (*Channel, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	id, ok := r.codeIndex[code]
	if !ok {
		return nil, ErrChannelNotFound
	}
	c := r.channels[id]
	cp := c
	return &cp, nil
}

func (r *MemRepo) IncrRegisteredCount(_ context.Context, channelID int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	c, ok := r.channels[channelID]
	if !ok {
		return ErrChannelNotFound
	}
	c.RegisteredCount++
	c.UpdatedAt = r.now()
	r.channels[channelID] = c
	return nil
}

func (r *MemRepo) CreateAttribution(_ context.Context, a *Attribution) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = r.now()
	}
	r.attributions[a.UserID] = *a
	return nil
}

// GetAttribution 读取用户的归属记录（供单测断言；非 PromotionRepo 接口方法）。
func (r *MemRepo) GetAttribution(_ context.Context, userID int64) (*Attribution, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.attributions[userID]
	if !ok {
		return nil, false
	}
	cp := a
	return &cp, true
}
