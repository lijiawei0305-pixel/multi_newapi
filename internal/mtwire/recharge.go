package mtwire

// 充值 / 支付装配（Phase 2 · 目标③）。
//
// 架构：充值入 new-api **原生 quota**（$1 = QuotaPerUnit，IncreaseUserQuota），
// 微信/支付宝走**独立 auth-service**（原生只有 epay 子渠道、无官方 V3）。本文件是主站侧装配：
//   - HandleWalletRecharge   下单：校验 → 算实付¥ → 经 RechargeGateway 落库 RCG 订单 + 调 auth-service 下单 → 返支付凭据；
//   - HandleInternalOrderPaid 入账：仅内网 + 共享密钥；据 order_no 走强幂等状态机入账（RCG→加额度 / SUB→激活套餐）；
//   - authServiceClient       实现 payment.PaySDK.CreatePay（HTTP 调 auth-service）；主站不验签（验签在 auth-service）。
//
// 金额可信：以**库内订单金额**入账，绝不信回调报文金额；/api/internal/* 仅内网 + 共享密钥（见 nginx deny）。

import (
	"bytes"
	"context"
	"crypto/hmac"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"time"

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

// internalSecretHeader 是 auth-service → 主站内部入账接口的共享密钥请求头。
const internalSecretHeader = "X-Internal-Secret"

// 充值/支付相关错误码（沿用模块前缀约定）。
var (
	errRechargeAmountTooSmall  = apperr.New("RECHARGE_AMOUNT_TOO_SMALL", fmt.Sprintf("充值金额最低 $%g", minRechargeUSD), http.StatusBadRequest)
	errRechargeUnauthenticated = apperr.New("RECHARGE_UNAUTHENTICATED", "登录态缺失", http.StatusUnauthorized)
	errInternalUnauthorized    = apperr.New("INTERNAL_UNAUTHORIZED", "内部接口鉴权失败", http.StatusUnauthorized)
	errVerifyUnsupported       = apperr.New("PAY_VERIFY_UNSUPPORTED", "主站不处理支付平台验签", http.StatusNotImplemented)
)

// rechargeConfig 是主站侧充值装配参数（从环境变量读取，缺省给开发值）。
type rechargeConfig struct {
	authServiceURL string // auth-service 内网基址（CreateOrder 下单）
	internalSecret string // /api/internal/* 共享密钥（与 auth-service 同值）
	notifyBaseURL  string // 异步回调公网基址（回填订单 notify_url；mock 可空）
}

// loadRechargeConfig 读取充值装配参数。
func loadRechargeConfig() rechargeConfig {
	return rechargeConfig{
		authServiceURL: envOr("MT_AUTH_SERVICE_URL", "http://auth-service:8080"),
		internalSecret: envOr("MT_INTERNAL_SECRET", "dev-internal-secret-change-me"),
		notifyBaseURL:  os.Getenv("MT_PAY_NOTIFY_BASE"),
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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

// 说明：tokenplan 套餐订单（SUB 前缀）的激活由 Track 1 的 App.ActivatePaidTokenplanOrder
// （internal/mtwire/subscription_bridge.go）提供，本入口按前缀分发调用（见 HandleInternalOrderPaid）。
// SUB 订单存于 Track 1 的 mt_subscription_orders，不在本模块 payment_orders（RCG 充值订单）中。

// checkInternalSecret 常量时间比对内部共享密钥（防时序侧信道）。
func (a *App) checkInternalSecret(provided string) bool {
	want := a.rechargeCfg.internalSecret
	if want == "" || provided == "" {
		return false
	}
	return hmac.Equal([]byte(provided), []byte(want))
}

// ---- HTTP 处理器 ----

// rechargeRequest 是 POST /api/tenant/wallet/recharge 入参。
type rechargeRequest struct {
	AmountUSD float64 `json:"amount_usd"`
	Provider  string  `json:"provider"` // wxpay | alipay
}

// HandleWalletRecharge POST /api/tenant/wallet/recharge —— 钱包充值下单。需 UserAuth + Host 租户。
//
// 流程：校验 amount_usd≥1 与渠道 → 实付¥=usd×汇率 → RechargeGateway.CreateOrder（落库 RCG 订单 +
// 调 auth-service 下单拿支付凭据）→ 返回 {order_no, amount_*, provider, pay:{wxpay_qr|alipay_url}}。
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

// internalOrderPaidRequest 是 POST /api/internal/order/paid 入参（auth-service 验签后调）。
type internalOrderPaidRequest struct {
	OrderNo string `json:"order_no"`
	TxnID   string `json:"txn_id"` // 平台交易号（审计/收益引用；不作金额来源）
}

// HandleInternalOrderPaid POST /api/internal/order/paid —— 内网入账（仅 auth-service 调）。
//
// 鉴权：共享密钥头 + nginx 拒绝公网访问 /api/internal/*。入账按 order_no 走强幂等状态机：
// 据库内订单类型分发（RCG→原生 quota；SUB→tokenplan 激活），金额一律以库内订单为准。
func (a *App) HandleInternalOrderPaid(c *gin.Context) {
	if !a.checkInternalSecret(c.GetHeader(internalSecretHeader)) {
		respondErr(c, errInternalUnauthorized)
		return
	}
	var body internalOrderPaidRequest
	if err := c.ShouldBindJSON(&body); err != nil || body.OrderNo == "" {
		respondErr(c, payment.ErrOrderInvalid)
		return
	}
	ctx := c.Request.Context()

	// 按订单号前缀分发（各自走自身的强幂等路径）：
	//   - SUB → Track 1 tokenplan 桥接激活（读 mt_subscription_orders，pending→activated CAS）；
	//   - 其余（RCG 等）→ 本模块充值入账（读 payment_orders，created→paid→credited CAS）。
	var err error
	if IsSubscriptionOrderNo(body.OrderNo) {
		err = a.ActivatePaidTokenplanOrder(ctx, body.OrderNo)
	} else if a.RechargeGateway != nil {
		err = a.RechargeGateway.CreditPaidOrder(ctx, body.OrderNo, body.TxnID)
	} else {
		err = apperr.New("RECHARGE_UNAVAILABLE", "充值服务未装配", http.StatusServiceUnavailable)
	}
	if err != nil {
		respondErr(c, err)
		return
	}
	respondOK(c, gin.H{"order_no": body.OrderNo, "status": "credited"})
}

// ---- auth-service 客户端（payment.PaySDK 实现）----

// authServiceClient 把「向支付平台下单」委托给独立 auth-service（HTTP）。
// 主站只用 CreatePay；Verify 不在主站执行（auth-service 完成平台验签后回调 /api/internal/order/paid）。
type authServiceClient struct {
	baseURL string
	http    *http.Client
}

// 编译期断言：authServiceClient 实现 payment.PaySDK。
var _ payment.PaySDK = (*authServiceClient)(nil)

func newAuthServiceClient(baseURL string) *authServiceClient {
	return &authServiceClient{
		baseURL: baseURL,
		http:    &http.Client{Timeout: 8 * time.Second},
	}
}

// authOrderRequest / authOrderResponse 是与 auth-service /auth/order 的契约。
type authOrderRequest struct {
	OrderNo   string  `json:"order_no"`
	Provider  string  `json:"provider"`
	AmountCNY float64 `json:"amount_cny"`
	AmountUSD float64 `json:"amount_usd"`
	Subject   string  `json:"subject"`
	NotifyURL string  `json:"notify_url"`
}

type authOrderResponse struct {
	Success bool `json:"success"`
	Data    struct {
		PayURL string `json:"pay_url"`
	} `json:"data"`
	Message string `json:"message"`
}

// CreatePay 调 auth-service 下单，返回支付凭据（mock：占位二维码内容/确认页 URL）。
func (c *authServiceClient) CreatePay(ctx context.Context, req payment.PayRequest) (*payment.PayCredential, error) {
	payload, _ := json.Marshal(authOrderRequest{
		OrderNo:   req.OrderNo,
		Provider:  string(req.Provider),
		AmountCNY: req.ActualPaid,
		AmountUSD: req.AmountUSD,
		Subject:   req.Subject,
		NotifyURL: req.NotifyURL,
	})
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/auth/order", bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, apperr.New("AUTH_SERVICE_UNREACHABLE", "支付服务暂不可用", http.StatusBadGateway).Wrap(err)
	}
	defer resp.Body.Close()
	var out authOrderResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, apperr.New("AUTH_SERVICE_BAD_RESPONSE", "支付服务响应异常", http.StatusBadGateway).Wrap(err)
	}
	if resp.StatusCode != http.StatusOK || !out.Success || out.Data.PayURL == "" {
		return nil, apperr.New("AUTH_SERVICE_CREATE_FAILED", "支付下单失败", http.StatusBadGateway)
	}
	return &payment.PayCredential{PayURL: out.Data.PayURL}, nil
}

// Verify 主站不验签（验签在 auth-service）。返回 PAY_VERIFY_UNSUPPORTED。
func (c *authServiceClient) Verify(_ context.Context, _ payment.Provider, _ []byte) (*payment.CallbackInfo, error) {
	return nil, errVerifyUnsupported
}
