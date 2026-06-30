package mtwire

// 支付渠道可用性（Phase 2 支付重构后）。
//
// 「买家能否用某渠道支付」由单一进程内判断决定：configured = enabled(setting.*Enabled) 且凭据齐全。
// 不再有独立的管理员启用开关表（mt_payment_provider_settings 已退役）；启用开关即 setting.*Enabled，
// 凭据 / 开关均由 setting 包（DB option，Builder A 管理）持有。configured 由 providerManager.Configured
// 进程内同步判断，不再查 auth-service /auth/healthz。
//
//   - 下单（HandleWalletRecharge）经 ensureProviderUsable 校验该渠道 configured；
//   - 买家可用渠道（HandleTenantRechargeMethods）= 各 provider where Configured。

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
)

// 支付渠道相关错误码（沿用模块前缀约定）。
var (
	// errProviderInvalid provider 参数非 wxpay/alipay。
	errProviderInvalid = apperr.New("PROVIDER_INVALID", "未知支付渠道", http.StatusBadRequest)
	// errProviderDisabled 渠道未启用（setting.*Enabled=false）或未配置（凭据缺失），不可下单。
	errProviderDisabled = apperr.New("PROVIDER_DISABLED", "该支付渠道未启用或未配置", http.StatusBadRequest)
)

// providerConfigured 报告渠道是否可用（enabled && 凭据齐全，进程内判断）；单测替换为桩。
// providerMgr 未装配（如单测直构 App）时返回 false。
var providerConfigured = func(a *App, provider payment.Provider) bool {
	if a.providerMgr == nil {
		return false
	}
	return a.providerMgr.Configured(provider)
}

// ensureProviderUsable 校验某渠道可下单：configured（enabled 且凭据齐全）。否则 errProviderDisabled。
func (a *App) ensureProviderUsable(_ context.Context, provider payment.Provider) error {
	if !providerConfigured(a, provider) {
		return errProviderDisabled
	}
	return nil
}

// HandleTenantRechargeMethods GET /api/tenant/wallet/recharge/methods —— 当前租户买家可用充值渠道。
// 需 UserAuth + Host 租户。可用 = Configured（enabled && 凭据齐全，进程内判断）。
// 两渠道均不可用时返回空列表（前端据此隐藏整卡）。
func (a *App) HandleTenantRechargeMethods(c *gin.Context) {
	if tenantFrom(c) == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	methods := make([]string, 0, 2)
	if providerConfigured(a, payment.ProviderWxpay) {
		methods = append(methods, string(payment.ProviderWxpay))
	}
	if providerConfigured(a, payment.ProviderAlipay) {
		methods = append(methods, string(payment.ProviderAlipay))
	}
	respondOK(c, gin.H{"methods": methods})
}
