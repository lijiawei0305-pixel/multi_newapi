package mtwire

import (
	"context"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/types"
)

// tenantStatusChecker 实现 risk.StatusChecker：按租户 status 把关。
// new-api 原生已校验用户/Token 状态；这里补「整租户被禁用(suspended/deleted)」这一多租户维度。
// tenant_id<=0（主站/无租户）或查不到 → 放行（旁路安全，绝不误杀正常请求）。
type tenantStatusChecker struct{ db *gorm.DB }

func (s tenantStatusChecker) Active(ctx context.Context, p *appctx.Principal) (bool, error) {
	if p == nil || p.TenantID <= 0 {
		return true, nil
	}
	var row struct{ Status string }
	if err := s.db.WithContext(ctx).Table("tenants").
		Select("status").Where("id = ?", p.TenantID).Take(&row).Error; err != nil {
		return true, nil // 查不到不阻断
	}
	return row.Status == "active", nil // tenant.StatusActive
}

// checkCallHook 是 agenthook.CheckCall 的实现：/v1 转发前调用风控（本轮 RPM 限流 + 租户状态）。
// best-effort：装配缺失/内部异常一律放行；仅确切命中限流/状态时返回 4xx 拦截（429/403）。
func (a *App) checkCallHook(ctx context.Context, userID, tokenID int64, model, clientIP, requestID string) *types.NewAPIError {
	defer func() { _ = recover() }()
	if a.RiskEngine == nil {
		return nil
	}
	p := &appctx.Principal{UserID: userID, TenantID: a.userTenantID(ctx, userID), Role: appctx.RoleUser}
	if err := a.RiskEngine.CheckCall(ctx, p, risk.CallContext{
		Model:     model,
		Endpoint:  "/v1",
		ClientIP:  clientIP,
		RequestID: requestID,
	}); err != nil {
		// risk 错误自带 HTTP 码（RATE_LIMITED=429 / IP_NOT_ALLOWED·STATUS_FORBIDDEN=403），透传给 relay。
		return types.NewErrorWithStatusCode(err, types.ErrorCodeAccessDenied, apperr.HTTPStatusOf(err))
	}
	return nil
}
