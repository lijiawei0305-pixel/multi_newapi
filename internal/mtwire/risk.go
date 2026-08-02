package mtwire

import (
	"context"
	"errors"

	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/appctx"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/types"
)

// tenantStatusChecker 实现 risk.StatusChecker：按租户 status 把关。
// new-api 原生已校验用户/Token 状态；这里补「整租户被禁用(suspended/deleted)」这一多租户维度。
// tenant_id<=0（主站/无租户）→ 放行。已知 tenant_id 但行不存在 → 视为非 active（fail-closed）。
// 基础设施错误（DB 抖动）→ 返回 error，由 checkCallHook fail-open 放行并记日志（不误杀正常流量）。
type tenantStatusChecker struct{ db *gorm.DB }

func (s tenantStatusChecker) Active(ctx context.Context, p *appctx.Principal) (bool, error) {
	if p == nil || p.TenantID <= 0 {
		return true, nil
	}
	var row struct{ Status string }
	if err := s.db.WithContext(ctx).Table("tenants").
		Select("status").Where("id = ?", p.TenantID).Take(&row).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			// 请求带了明确 tenant_id，但库中无此租户 → 不放行（防止已删租户继续打 /v1）
			return false, nil
		}
		// 基础设施错误：交上层 fail-open，绝不因 DB 抖动误杀
		return true, err
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
	if active, err := (tenantStatusChecker{db: a.DB}).Active(ctx, p); err != nil {
		// DB 等基础设施错误：fail-open 放行，但必须留痕（便于区分「租户停用」与「查状态失败」）。
		common.SysError("mtwire: tenant status check error (fail-open): " + err.Error())
	} else if !active {
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
		// CheckCall 会混返两类 error，必须分流处理：
		//  ① 确切策略拒绝——均为 *apperr.AppError，自带 4xx（RATE_LIMITED=429 / IP_NOT_ALLOWED·STATUS_FORBIDDEN=403）。
		//  ② 基础设施抖动——Redis INCR / IP·RPM 后端 / 状态点查失败，是裸 Go error（非 AppError）。
		// 契约（见上方 §38-39）：仅确切命中状态/限流才返回 4xx 拦截；基础设施错误一律放行（best-effort，绝不误杀正常流量）。
		// 陷阱：apperr.HTTPStatusOf 对非 AppError 归一为 500 —— 若原样透传，Redis 一宕机每个 /v1 请求都变 500
		// （fail-closed，恰与契约相反；客户端 codex/cursor 把 5xx 当临时错误狂重试，把故障放大成全站雪崩）。
		// 故按 HTTP 码分流：仅 4xx（确切策略）拦截；其余（基础设施错误 → 归一 500）记日志后 fail-open 放行。
		if status := apperr.HTTPStatusOf(err); status >= 400 && status < 500 {
			return types.NewErrorWithStatusCode(err, types.ErrorCodeAccessDenied, status)
		}
		// 基础设施错误：留痕后放行。补上这行日志（此前排障会先误查上游渠道，因为没有任何一行说「风控因 Redis 不可用」）。
		common.SysError("mtwire: checkCallHook 风控基础设施错误(如 Redis 不可用)，已 fail-open 放行以免误杀 /v1 流量: " + err.Error())
		return nil
	}
	return nil
}
