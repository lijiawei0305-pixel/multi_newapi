package identity

import (
	"context"
	"net/http"

	"newapi-mt/internal/platform/appctx"
	"newapi-mt/internal/platform/apperr"
)

type accessGuard struct {
	tenants TenantStatusChecker
}

// NewAccessGuard 用给定 TenantStatusChecker 构造 AccessGuard。
func NewAccessGuard(tenants TenantStatusChecker) AccessGuard {
	return &accessGuard{tenants: tenants}
}

func (g *accessGuard) RequireAdmin(p *appctx.Principal) error {
	if p != nil && p.IsAdmin() {
		return nil
	}
	return apperr.New(CodeForbiddenAdmin, "需要管理员权限", http.StatusForbidden)
}

func (g *accessGuard) RequireTenantOwner(p *appctx.Principal, tenantID int64) error {
	if p != nil {
		// 管理员为超级用户，可跨租户操作；管理员接口另以 RequireAdmin 单独鉴权，
		// 二者不混用（proposal §14）。默认假设：admin 可通过本守卫。
		if p.IsAdmin() {
			return nil
		}
		// 否则必须是该租户的 owner：角色为 agent_owner 且租户匹配。
		if p.Role == appctx.RoleAgentOwner && p.TenantID == tenantID {
			return nil
		}
	}
	return apperr.New(CodeForbiddenTenant, "无权访问该租户资源", http.StatusForbidden)
}

func (g *accessGuard) RequireTenantActive(ctx context.Context, tenantID int64) error {
	status, found, err := g.tenants.StatusOf(ctx, tenantID)
	if err != nil {
		return err // 基础设施错误，原样上浮
	}
	if !found || status != TenantStatusActive {
		return apperr.New(CodeTenantInactive, "租户未激活或已被冻结", http.StatusForbidden)
	}
	return nil
}
