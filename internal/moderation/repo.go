package moderation

import (
	"context"
	"sort"
	"sync"
	"time"
)

// MemRepo 是 BannedWordRepo + ViolationSink 的并发安全内存假实现，作为单测默认替身。
// 真实持久化见 gormrepo 子包。
type MemRepo struct {
	mu         sync.RWMutex
	words      map[int64]BannedWord
	nextWordID int64
	violations []ViolationEvent
	nextVID    int64
	now        func() time.Time
}

// NewMemRepo 构造内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{words: map[int64]BannedWord{}, now: time.Now}
}

// ListWords 返回 tenant_id 恰等于 tenantID 的全部词（含禁用），按 ID 升序。
func (r *MemRepo) ListWords(ctx context.Context, tenantID int64) ([]BannedWord, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]BannedWord, 0)
	for _, w := range r.words {
		if w.TenantID == tenantID {
			out = append(out, w)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// UpsertWord 新增（ID=0，回填 ID 与 CreatedAt）或按 ID 更新。
func (r *MemRepo) UpsertWord(ctx context.Context, w *BannedWord) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if w.ID == 0 {
		r.nextWordID++
		w.ID = r.nextWordID
		if w.CreatedAt.IsZero() {
			w.CreatedAt = r.now()
		}
	}
	r.words[w.ID] = *w
	return nil
}

// DeleteWord 删除某租户名下指定词；跨租户视为不存在。
func (r *MemRepo) DeleteWord(ctx context.Context, tenantID, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	w, ok := r.words[id]
	if !ok || w.TenantID != tenantID {
		return ErrWordNotFound
	}
	delete(r.words, id)
	return nil
}

// Record 落库一条违规事件（回填 ID 与 CreatedAt）。
func (r *MemRepo) Record(ctx context.Context, ev *ViolationEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextVID++
	ev.ID = r.nextVID
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = r.now()
	}
	r.violations = append(r.violations, *ev)
	return nil
}

// ListForAdmin 按租户 + 过滤条件查违规日志，按 created_at 倒序（最新在前）。
func (r *MemRepo) ListForAdmin(ctx context.Context, tenantID int64, f ViolationFilter) ([]ViolationEvent, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]ViolationEvent, 0)
	for i := len(r.violations) - 1; i >= 0; i-- {
		ev := r.violations[i]
		if ev.TenantID != tenantID {
			continue
		}
		if f.UserID != 0 && ev.UserID != f.UserID {
			continue
		}
		if !f.Since.IsZero() && ev.CreatedAt.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && ev.CreatedAt.After(f.Until) {
			continue
		}
		out = append(out, ev)
	}
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			return []ViolationEvent{}, nil
		}
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

var (
	_ BannedWordRepo = (*MemRepo)(nil)
	_ ViolationSink  = (*MemRepo)(nil)
)
