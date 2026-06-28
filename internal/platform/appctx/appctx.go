// Package appctx 承载请求级的租户/用户身份（Principal），供全模块统一读取。
// 多租户隔离的根：Repo 层据此 scopeByTenant。详见 doc/detailed-design.md §1.3。
package appctx

import "context"

// Role 角色枚举。
type Role string

const (
	RoleAdmin      Role = "admin"
	RoleAgentOwner Role = "agent_owner"
	RoleUser       Role = "user"
)

// Principal 是请求级身份。
type Principal struct {
	UserID   int64
	TenantID int64
	Role     Role
}

func (p Principal) IsAdmin() bool      { return p.Role == RoleAdmin }
func (p Principal) IsAgentOwner() bool { return p.Role == RoleAgentOwner }

type ctxKey struct{}

// WithPrincipal 返回携带 Principal 的新 context。
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom 读取 context 中的 Principal；不存在返回 ok=false。
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// TenantID 便捷读取当前租户；不存在返回 0。
func TenantID(ctx context.Context) int64 {
	if p, ok := PrincipalFrom(ctx); ok {
		return p.TenantID
	}
	return 0
}
