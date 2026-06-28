package relay

import (
	"context"
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// Gateway 是 /v1/* 中继编排器：鉴权 → 租户活跃 → 风控 → 模型权限 → 转发 → 扣费 → 返回。
// 纯编排、不含业务规则，全部依赖均为本包消费者接口，便于「全 mock」独立单测（detailed-design §2.11）。
type Gateway struct {
	auth     Authenticator
	access   AccessGuard
	risk     RiskEngine
	models   ModelPermission
	billing  Billing
	upstream UpstreamPool
}

// NewGateway 装配中继编排器。各依赖由 cmd/main 注入兄弟模块实现（依赖倒置，§1.4）。
func NewGateway(auth Authenticator, access AccessGuard, risk RiskEngine, models ModelPermission, billing Billing, upstream UpstreamPool) *Gateway {
	return &Gateway{
		auth:     auth,
		access:   access,
		risk:     risk,
		models:   models,
		billing:  billing,
		upstream: upstream,
	}
}

// Handle 编排一次 /v1/* 调用（ChatCompletions / Completions / Embeddings 共用），
// 顺序（detailed-design §3.1，桶路由下沉至 Billing.Charge 内部）：
//
//	鉴权 → 租户活跃 → 风控 → 模型权限 → 转发 → 计费扣费 → 返回。
//
// 短路语义：任一步失败立即返回、后续步骤不执行（鉴权失败不进风控、风控失败不转发）。
// 计费失败（桶不足/超额/过期）原样上浮、**不回退**（独立计量，§8.1）；上游转发失败
// 标准化为 UPSTREAM_ERROR。错误一律透传依赖错误码，relay 不吞码（§6.4）。
//
// TODO(handler)：Gin 薄封装 RelayHandler.ChatCompletions/Completions/Embeddings 与
// GET /v1/models 路由本轮不做——后续从 *gin.Context 解析为 RelayRequest、调用本方法、
// 再把 RelayResponse 回写客户端。真实 UpstreamPool（复用 new-api 转发）亦待接入。
func (g *Gateway) Handle(ctx context.Context, req RelayRequest) (RelayResponse, error) {
	// 1) 鉴权：原始令牌 → Principal（契约保证成功时 p 非 nil，参考 identity.authenticator）。
	p, err := g.auth.AuthenticateToken(ctx, req.RawToken)
	if err != nil {
		return RelayResponse{}, err
	}

	// 2) 租户活跃校验（冻结/删除租户在此拦截）。
	if err := g.access.RequireTenantActive(ctx, p.TenantID); err != nil {
		return RelayResponse{}, err
	}

	// 3) 调用前风控（状态 / RPM / IP / 并发）。
	if err := g.risk.CheckCall(ctx, p, CallContext{
		Model:     req.Model,
		Endpoint:  req.Endpoint,
		ClientIP:  req.ClientIP,
		RequestID: req.RequestID,
	}); err != nil {
		return RelayResponse{}, err
	}

	// 4) 模型权限（Token model_allowlist），不允许 → MODEL_NOT_ALLOWED。
	if err := g.models.Check(ctx, p, req.Model); err != nil {
		return RelayResponse{}, err
	}

	// 5) 转发上游 → resp + usage。
	resp, usage, err := g.upstream.Forward(ctx, req)
	if err != nil {
		return RelayResponse{}, upstreamError(err)
	}

	// 6) 计费扣费（内部选桶/倍率/日志/分润）。失败原样上浮、不回退。
	if _, err := g.billing.Charge(ctx, ChargeRequest{
		RequestID:        req.RequestID,
		UserID:           p.UserID,
		TenantID:         p.TenantID,
		Model:            req.Model,
		PromptTokens:     usage.PromptTokens,
		CompletionTokens: usage.CompletionTokens,
	}); err != nil {
		return RelayResponse{}, err
	}

	// 7) 返回上游响应。
	return resp, nil
}

// upstreamError 把上游转发的底层错误标准化为 UPSTREAM_ERROR（502）；
// 若已是携带稳定错误码的 AppError 则原样透传，不二次包裹（避免覆盖更具体的码）。
func upstreamError(err error) error {
	var ae *apperr.AppError
	if errors.As(err, &ae) {
		return err
	}
	return apperr.New(CodeUpstreamError, "上游转发失败", http.StatusBadGateway).Wrap(err)
}
