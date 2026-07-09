package mtwire

import (
	"context"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
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

// checkCallHook 是 agenthook.CheckCall 的实现：/v1 转发前调用风控。两层解耦：
//  1. 租户状态（整租户 suspended/deleted 拦截）——纯 DB 点查，**与 Redis 无关**，任何时候都强制
//     （fail-closed：确切非 active 即 403）。"停用某代理→切断其名下用户 /v1"由此保证，不因
//     Redis 关/宕而静默失效（历史 bug：此校验曾绑死在仅 Redis 时装配的 RiskEngine 上）。
//  2. RPM 固定窗口限流——需 Redis 共享计数，故仅 RiskEngine 装配（Redis 就绪）时运行。
//
// best-effort：panic / DB 查询错误一律放行（绝不误杀正常流量，故 recover 记日志而非静默）；
// 仅确切命中状态/限流时返回 4xx 拦截（403/429）。
func (a *App) checkCallHook(ctx context.Context, userID, tokenID int64, model, clientIP, requestID string) *types.NewAPIError {
	defer func() {
		if r := recover(); r != nil {
			common.SysError("mtwire: checkCallHook panic recovered")
		}
	}()
	p := &appctx.Principal{UserID: userID, TenantID: a.userTenantID(ctx, userID), Role: appctx.RoleUser}
	// 1) 租户状态：DB-only，Redis 无关 → 恒强制。Active 在查不到/出错时返回 true（旁路，绝不误杀），
	//    仅确切 status != active 才返回 false → 复用 risk.ErrStatusForbidden（403）透传给 relay。
	if active, err := (tenantStatusChecker{db: a.DB}).Active(ctx, p); err == nil && !active {
		return types.NewErrorWithStatusCode(risk.ErrStatusForbidden, types.ErrorCodeAccessDenied, apperr.HTTPStatusOf(risk.ErrStatusForbidden))
	}
	// 2) RPM 限流：需共享计数 → 仅 Redis 装配时运行；未装配即到此为止（状态已在上方恒强制）。
	if a.RiskEngine == nil {
		return nil
	}
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
