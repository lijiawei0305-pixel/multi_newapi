package mtwire

// 充值 / 支付装配（Phase 2 · 目标③，支付重构后）。
//
// 架构：充值入 new-api **原生 quota**（$1 = QuotaPerUnit，IncreaseUserQuota）。微信/支付宝下单经
// 进程内 providerManager（inProcessPaySDK）直连真实平台，回调直达主站 /api/pay/*/notify（见
// payment_inprocess.go）。本文件是主站侧装配：
//   - HandleWalletRecharge 下单：校验 → 算实付¥ → 经 RechargeGateway 落库 RCG 订单 + 进程内下单 → 返支付凭据；
//   - rechargeQuotaSink     入账：RCG 订单 → 原生 quota（$1 = QuotaPerUnit）。
//
// 金额可信：以**库内订单金额**入账，绝不信回调报文金额（回调仅作反篡改校验，见 payment.CreditPaidOrder）。

import (
	"context"
	"fmt"
	"math"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/internal/tenant"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/setting/operation_setting"
)

// minRechargeUSD 充值美元最低额（对齐 operation_setting.MinTopUp 口径、UI $1 下限）。
const minRechargeUSD = 1.0

// 充值/支付相关错误码（沿用模块前缀约定）。
var (
	errRechargeAmountTooSmall  = apperr.New("RECHARGE_AMOUNT_TOO_SMALL", fmt.Sprintf("充值金额最低 $%g", minRechargeUSD), http.StatusBadRequest)
	errRechargeUnauthenticated = apperr.New("RECHARGE_UNAUTHENTICATED", "登录态缺失", http.StatusUnauthorized)
)

// rechargeConfig 是主站侧充值装配参数。
type rechargeConfig struct {
	notifyBaseURL string // 异步回调公网基址（回填订单 notify_url）；缺省取 system_setting.ServerAddress
}

// loadRechargeConfig 读取充值装配参数（notify 基址优先 MT_PAY_NOTIFY_BASE，缺省 system_setting.ServerAddress）。
func loadRechargeConfig() rechargeConfig {
	return rechargeConfig{
		notifyBaseURL: resolveNotifyBase(),
	}
}

// ---- 入账分发目标（OrderSink 实现）----

// rechargeQuotaSink 把充值订单入账到 new-api 原生 quota（$1 = QuotaPerUnit）。
// 幂等由上游 CreditPaidOrder 的 created→paid CAS 保证：OnPaid 仅被首个推进者调用一次。
type rechargeQuotaSink struct{}

func (rechargeQuotaSink) OnPaid(_ context.Context, o payment.PaidOrder) error {
	q := rechargeQuota(o.AmountUSD)
	if q <= 0 {
		return nil
	}
	// TODO(recharge_spread / 口径未决)：充值差价分润 = 用户实付¥ − 代理成本¥。当前单一汇率模型下，
	// 用户实付 = AmountUSD × USDExchangeRate（= 主站标准价），代理无独立「充值成本/加价率」字段，
	// 故差价恒为 0、暂不入账。待数据模型补充 agent 充值加价/成本率后，在此按
	// (o.ActualPaid − agentRechargeCostCNY) 经 AgentEarnings.AddEarning(source=recharge_spread,
	// SourceID=o.OrderNo) 幂等落账（须先有 agent_profile）。详见报告「风险/未决」。
	// db=true：同步落 users.quota + 异步刷新额度缓存（与 EpayNotify/Stripe 入账一致）。
	return model.IncreaseUserQuota(int(o.UserID), q, true)
}

// rechargeQuota 把充值美元额折算为 new-api 内部 quota 单位（$1 = common.QuotaPerUnit）。
func rechargeQuota(amountUSD float64) int {
	if amountUSD <= 0 {
		return 0
	}
	return int(amountUSD * common.QuotaPerUnit)
}

// actualPaidCNY 计算用户实付人民币 = 美元额 × 汇率（收益币种；差价基准）。
func actualPaidCNY(amountUSD, rate float64) float64 {
	return amountUSD * rate
}

// amountToleranceCNY 金额比对容差（元）：≤1 分视为相等，吸收浮点/汇率取整噪声。
const amountToleranceCNY = 0.011

// amountMatchesCNY 报告回调实付金额是否与库内订单金额一致（反篡改）。
// paidCNY<=0 视为「调用方未提供」（如对账兜底主动查单），跳过比对。
func amountMatchesCNY(paidCNY, orderCNY float64) bool {
	if paidCNY <= 0 {
		return true
	}
	return math.Abs(paidCNY-orderCNY) <= amountToleranceCNY
}

// 说明：tokenplan 套餐订单（SUB 前缀）的激活由 App.ActivatePaidTokenplanOrder
// （internal/mtwire/subscription_bridge.go）提供，回调按前缀分发调用（见 payment_inprocess.go）。
// SUB 订单存于 mt_subscription_orders，不在本模块 payment_orders（RCG 充值订单）中。

// ---- HTTP 处理器 ----

// rechargeRequest 是 POST /api/tenant/wallet/recharge 入参。
type rechargeRequest struct {
	AmountUSD float64 `json:"amount_usd"`
	Provider  string  `json:"provider"` // wxpay | alipay
}

// HandleWalletRecharge POST /api/tenant/wallet/recharge —— 钱包充值下单。需 UserAuth + Host 租户。
//
// 流程：校验 amount_usd≥1 与渠道 → 实付¥=usd×汇率 → RechargeGateway.CreateOrder（落库 RCG 订单 +
// 进程内向平台下单拿支付凭据）→ 返回 {order_no, amount_*, provider, pay:{wxpay_qr|alipay_url}}。
func (a *App) HandleWalletRecharge(c *gin.Context) {
	t := tenantFrom(c)
	if t == nil {
		respondErr(c, tenant.ErrTenantNotFound)
		return
	}
	userID := int64(c.GetInt("id"))
	if userID <= 0 {
		respondErr(c, errRechargeUnauthenticated)
		return
	}
	var body rechargeRequest
	if err := c.ShouldBindJSON(&body); err != nil {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	provider := payment.Provider(body.Provider)
	if !provider.Valid() {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	if body.AmountUSD < minRechargeUSD {
		respondErr(c, errRechargeAmountTooSmall)
		return
	}
	// 渠道必须可用（enabled 且凭据齐全，进程内判断）方可下单。
	if err := a.ensureProviderUsable(reqCtx(c), provider); err != nil {
		respondErr(c, err)
		return
	}
	if a.RechargeGateway == nil {
		respondErr(c, apperr.New("RECHARGE_UNAVAILABLE", "充值服务未装配", http.StatusServiceUnavailable))
		return
	}

	actualPaid := actualPaidCNY(body.AmountUSD, operation_setting.USDExchangeRate)
	order, err := a.RechargeGateway.CreateOrder(reqCtx(c), payment.OrderInput{
		Type:       payment.OrderTypeRecharge,
		TenantID:   t.ID,
		UserID:     userID,
		Provider:   provider,
		AmountUSD:  body.AmountUSD,
		ActualPaid: actualPaid,
		Subject:    fmt.Sprintf("钱包充值 $%g", body.AmountUSD),
	})
	if err != nil {
		respondErr(c, err)
		return
	}

	pay := gin.H{}
	switch provider {
	case payment.ProviderWxpay:
		pay["wxpay_qr"] = order.PayURL // 前端渲染二维码
	case payment.ProviderAlipay:
		pay["alipay_url"] = order.PayURL // 前端跳转
	}
	respondOK(c, gin.H{
		"order_no":   order.OrderNo,
		"amount_usd": body.AmountUSD,
		"amount_cny": actualPaid,
		"provider":   string(provider),
		"pay":        pay,
	})
}
