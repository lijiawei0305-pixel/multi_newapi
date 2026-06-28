package tenant

import (
	"context"
	"strings"

	"newapi-mt/internal/platform/appctx"
)

type hostResolver struct {
	repo  TenantRepo
	cache Cache
}

// NewResolver 组装 TenantResolver（缓存 + 回源）。
func NewResolver(repo TenantRepo, cache Cache) TenantResolver {
	return &hostResolver{repo: repo, cache: cache}
}

// ResolveByHost 归一化 Host 后三分支解析：命中缓存 -> 直接返回；
// 未命中 -> 回源 Repo 并写缓存；Repo 未找到 -> 返回 ErrTenantNotFound（不缓存负结果）。
func (r *hostResolver) ResolveByHost(ctx context.Context, host string) (*Tenant, error) {
	h := normalizeHost(host)
	if h == "" {
		return nil, ErrTenantNotFound
	}
	if t, ok := r.cache.Get(ctx, h); ok {
		return t, nil
	}
	t, err := r.repo.GetTenantByDomain(ctx, h)
	if err != nil {
		return nil, err // 含 ErrTenantNotFound
	}
	r.cache.Set(ctx, h, t)
	return t, nil
}

// normalizeHost 归一化 Host 头：去空白、转小写、去端口、去末尾点。
// 一期仅处理二级域名，未特殊处理 IPv6 字面量。
func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	if i := strings.LastIndex(h, ":"); i >= 0 {
		if isAllDigits(h[i+1:]) {
			h = h[:i]
		}
	}
	h = strings.TrimSuffix(h, ".")
	return h
}

func isAllDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// ContextWithTenant 把已解析租户的 TenantID 注入请求 context（复用 appctx.Principal），
// 供下游 Repo 做 scopeByTenant。这是中间件解析 Host 后的注入点（中间件本身见报告 TODO）。
// UserID/Role 由后续鉴权中间件补齐，这里保留已存在的 Principal 字段。
func ContextWithTenant(ctx context.Context, t *Tenant) context.Context {
	p, _ := appctx.PrincipalFrom(ctx)
	p.TenantID = t.ID
	return appctx.WithPrincipal(ctx, p)
}
