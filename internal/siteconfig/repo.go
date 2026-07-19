package siteconfig

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// MemRepo 是 SiteConfigRepo 的并发安全内存假实现，用于本轮纯逻辑开发与单测。
// 生产 GORM 实现（迁移 + tenant_id scope + 唯一约束）位于 gormrepo 子包。
type MemRepo struct {
	mu        sync.RWMutex
	configs   map[int64]SiteConfig
	assets    map[int64]Asset
	nextAsset int64
	now       func() time.Time
}

// NewMemRepo 构造空的内存仓储。
func NewMemRepo() *MemRepo {
	return &MemRepo{
		configs: make(map[int64]SiteConfig),
		assets:  make(map[int64]Asset),
		now:     time.Now,
	}
}

// GetConfig 读取租户配置；未配置返回 found=false。
func (r *MemRepo) GetConfig(_ context.Context, tenantID int64) (*SiteConfig, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	cfg, ok := r.configs[tenantID]
	if !ok {
		return nil, false, nil
	}
	cp := cfg
	cp.EnabledModules = append([]string(nil), cfg.EnabledModules...)
	return &cp, true, nil
}

// UpsertConfig 按 TenantID 写入/更新配置；保留首建的 CreatedAt，刷新 UpdatedAt。
func (r *MemRepo) UpsertConfig(_ context.Context, cfg *SiteConfig) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	ts := r.now()
	cp := *cfg
	if stored, ok := r.configs[cfg.TenantID]; ok {
		cp.CreatedAt = stored.CreatedAt
	} else if cp.CreatedAt.IsZero() {
		cp.CreatedAt = ts
	}
	cp.UpdatedAt = ts
	cp.EnabledModules = append([]string(nil), cfg.EnabledModules...)
	r.configs[cfg.TenantID] = cp
	return nil
}

// CreateAsset 记录素材并回填 a.ID。
func (r *MemRepo) CreateAsset(_ context.Context, a *Asset) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.nextAsset++
	a.ID = r.nextAsset
	if a.Status == "" {
		a.Status = AssetStatusActive
	}
	if a.CreatedAt.IsZero() {
		a.CreatedAt = r.now()
	}
	r.assets[a.ID] = *a
	return nil
}

// GetAsset 按 ID 读取素材；不存在返回 ErrAssetNotFound。
func (r *MemRepo) GetAsset(_ context.Context, id int64) (*Asset, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	a, ok := r.assets[id]
	if !ok {
		return nil, ErrAssetNotFound
	}
	cp := a
	return &cp, nil
}

// SetAssetStatus 更新素材状态；不存在返回 ErrAssetNotFound。
func (r *MemRepo) SetAssetStatus(_ context.Context, id int64, s AssetStatus) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.assets[id]
	if !ok {
		return ErrAssetNotFound
	}
	a.Status = s
	r.assets[id] = a
	return nil
}

// MemBlob 是 Blob 的并发安全内存假实现。真实对象存储适配顺延（见报告 TODO）。
type MemBlob struct {
	mu      sync.RWMutex
	baseURL string
	objs    map[string][]byte
}

// NewMemBlob 构造空对象存储；baseURL 作为返回 URL 的前缀。
func NewMemBlob(baseURL string) *MemBlob {
	return &MemBlob{baseURL: baseURL, objs: make(map[string][]byte)}
}

// Put 以 key 存储对象（值拷贝），返回 baseURL/key。
func (b *MemBlob) Put(_ context.Context, key string, data []byte, _ string) (string, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.objs[key] = append([]byte(nil), data...)
	return fmt.Sprintf("%s/%s", b.baseURL, key), nil
}

// Delete 删除对象。
func (b *MemBlob) Delete(_ context.Context, key string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.objs, key)
	return nil
}

// Get 读取对象（仅供测试/校验"下架后不可访问"）；不存在返回 ok=false。
func (b *MemBlob) Get(key string) ([]byte, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	d, ok := b.objs[key]
	if !ok {
		return nil, false
	}
	return append([]byte(nil), d...), true
}
