package authservice

import (
	"bytes"
	"context"
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/payment/realpay"
)

// internalSecretHeader 必须与主站 internal/mtwire.internalSecretHeader 一致。
const internalSecretHeader = "X-Internal-Secret"

// Server 是 auth-service 的 HTTP 服务。
//
// 两种形态：
//   - mock（cfg.Mock=true）：复用 internal/payment 的 Gateway(MemRepo + StubPaySDK + forwarder Sink)：
//     下单落 MemRepo + 返 mock 确认页；确认页合成合法签名回调 → Gateway 验签/幂等 → forward 主站。
//   - 真实（cfg.Mock=false）：用 realpay 适配真实微信/支付宝 V3。回调走**无状态 verify-and-forward**
//     （realpay.VerifyNotify → forwarder.OnPaid 直接回调主站），不依赖本地 MemRepo——
//     主站持久订单 + created→paid→credited CAS 是唯一幂等 + 金额权威，故 auth-service 重启不丢单。
type Server struct {
	cfg    Config
	gw     *payment.Gateway    // mock 模式：状态机编排
	repo   payment.OrderRepo   // mock 模式：落单/读单（确认页/查单）
	sdk    *payment.StubPaySDK // mock 模式：合成 mock 回调
	real   *realpay.SDK        // 真实模式：微信/支付宝 V3 适配器
	fwd    *forwarder          // 入账转发器（两种模式共用）
	client *http.Client
}

// NewServer 组装服务。真实模式装配 realpay（凭据/密钥文件错误会返回 error）。
func NewServer(cfg Config) (*Server, error) {
	client := &http.Client{Timeout: 8 * time.Second}
	fwd := &forwarder{
		callbackURL: cfg.Internal.CallbackURL,
		secret:      cfg.Internal.SharedSecret,
		client:      client,
	}
	s := &Server{cfg: cfg, fwd: fwd, client: client}

	if cfg.Mock {
		repo := payment.NewMemRepo()
		sdk := payment.NewStubPaySDK(cfg.SignSecret)
		sinks := map[payment.OrderType]payment.OrderSink{
			payment.OrderTypeRecharge:     fwd,
			payment.OrderTypeSubscription: fwd,
		}
		s.repo = repo
		s.sdk = sdk
		s.gw = payment.NewGateway(repo, sdk, sinks)
		return s, nil
	}

	real, err := buildRealSDK(cfg)
	if err != nil {
		return nil, fmt.Errorf("auth-service: build real pay sdk: %w", err)
	}
	s.real = real
	return s, nil
}

// buildRealSDK 从 Config 映射出 realpay.Config（读取支付宝私钥/公钥文件内容）。
func buildRealSDK(cfg Config) (*realpay.SDK, error) {
	rc := realpay.Config{}
	if cfg.wxpayConfigured() {
		rc.Wxpay = realpay.WxpayConfig{
			AppID:          cfg.Wxpay.AppID,
			MchID:          cfg.Wxpay.MchID,
			APIv3Key:       cfg.Wxpay.APIv3Key,
			CertSerialNo:   cfg.Wxpay.CertSerialNo,
			PrivateKeyPath: cfg.Wxpay.PrivateKeyPath, // realpay 内部经 utils.LoadPrivateKeyWithPath 读取
		}
	}
	if cfg.alipayConfigured() {
		priv, err := os.ReadFile(cfg.Alipay.PrivateKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read alipay private key %q: %w", cfg.Alipay.PrivateKeyPath, err)
		}
		pub, err := os.ReadFile(cfg.Alipay.AlipayPublicKeyPath)
		if err != nil {
			return nil, fmt.Errorf("read alipay public key %q: %w", cfg.Alipay.AlipayPublicKeyPath, err)
		}
		rc.Alipay = realpay.AlipayConfig{
			AppID:           cfg.Alipay.AppID,
			PrivateKey:      string(priv),
			AlipayPublicKey: string(pub),
			SellerID:        cfg.Alipay.SellerID,
			ReturnURL:       cfg.Alipay.ReturnURL,
			IsProduction:    !cfg.Alipay.Sandbox,
		}
	}
	return realpay.New(context.Background(), rc)
}

// Router 返回挂载全部路由的 http.Handler（Go 1.22+ 方法模式）。
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/healthz", s.handleHealth)
	mux.HandleFunc("POST /auth/order", s.handleCreateOrder)
	mux.HandleFunc("GET /auth/order/status", s.handleOrderStatus)
	mux.HandleFunc("GET /auth/mock/pay", s.handleMockPayPage)
	mux.HandleFunc("POST /auth/mock/confirm", s.handleMockConfirm)
	// 历史独立服务的微信回调（当前生产回调由主 API 的 /api/pay/wechat/notify 承载）。
	// （nginx ^~ /pay/ 反代到此）。同时保留 /auth/wxpay/notify 兼容既有 mock 自检/单测。
	mux.HandleFunc("POST /pay/wxpay/notify", s.handleWxpayNotify)
	mux.HandleFunc("POST /auth/wxpay/notify", s.handleWxpayNotify)
	mux.HandleFunc("POST /auth/alipay/notify", s.handleAlipayNotify)
	return mux
}

// ---- 下单 ----

type createOrderRequest struct {
	OrderNo   string  `json:"order_no"`
	Provider  string  `json:"provider"`
	AmountCNY float64 `json:"amount_cny"`
	AmountUSD float64 `json:"amount_usd"`
	Subject   string  `json:"subject"`
	NotifyURL string  `json:"notify_url"`
}

// handleCreateOrder POST /auth/order —— 主站下单。
//   - 真实模式：调 realpay.CreatePay 取真实支付凭据（微信 code_url / 支付宝跳转 URL），无状态。
//   - mock 模式：落 created 订单（MemRepo）+ 返回 mock 确认页 URL。
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := common.DecodeJson(r.Body, &req); err != nil || req.OrderNo == "" {
		writeJSON(w, http.StatusBadRequest, jsonObj{"success": false, "message": "bad request"})
		return
	}
	provider := payment.Provider(req.Provider)
	if !provider.Valid() {
		writeJSON(w, http.StatusBadRequest, jsonObj{"success": false, "message": "invalid provider"})
		return
	}

	if !s.cfg.Mock {
		if !s.real.Available(provider) {
			writeJSON(w, http.StatusBadRequest, jsonObj{"success": false, "message": "provider not configured: " + string(provider)})
			return
		}
		payURL, err := s.real.CreatePay(r.Context(), provider, req.OrderNo, req.Subject, req.AmountCNY, req.NotifyURL)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, jsonObj{"success": false, "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, jsonObj{
			"success": true,
			"data":    jsonObj{"pay_url": payURL, "provider": string(provider), "mock": false},
		})
		return
	}

	// mock 模式：落 created 订单 + 返回 mock 确认页 URL。
	now := time.Now()
	order := &payment.PayOrder{
		OrderNo:    req.OrderNo,
		Type:       orderTypeFromNo(req.OrderNo),
		Provider:   provider,
		AmountUSD:  req.AmountUSD,
		ActualPaid: req.AmountCNY,
		Subject:    req.Subject,
		Status:     payment.OrderCreated,
		NotifyURL:  req.NotifyURL,
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	if err := s.repo.Create(r.Context(), order); err != nil {
		// 已存在即幂等返回同一支付凭据（下单可重入）。
		if err != payment.ErrOrderDuplicate {
			writeJSON(w, http.StatusBadGateway, jsonObj{"success": false, "message": err.Error()})
			return
		}
	}
	writeJSON(w, http.StatusOK, jsonObj{
		"success": true,
		"data": jsonObj{
			"pay_url":  s.payURL(req.OrderNo),
			"provider": string(provider),
			"mock":     true,
		},
	})
}

// payURL 拼装 mock 支付确认页 URL（微信→二维码内容、支付宝→跳转目标，均指向此页）。
func (s *Server) payURL(orderNo string) string {
	return strings.TrimRight(s.cfg.Server.PublicBaseURL, "/") + "/auth/mock/pay?order=" + orderNo
}

// ---- mock 支付确认页（仅 mock 模式）----

// handleMockPayPage GET /auth/mock/pay?order=XXX —— 极简确认页（mock 专用）。
func (s *Server) handleMockPayPage(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Mock {
		http.NotFound(w, r)
		return
	}
	orderNo := r.URL.Query().Get("order")
	order, err := s.repo.GetByOrderNo(r.Context(), orderNo)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, mockPayPageHTML,
		html.EscapeString(orderNo),
		html.EscapeString(string(order.Provider)),
		order.ActualPaid,
		order.AmountUSD,
		html.EscapeString(orderNo),
	)
}

// handleMockConfirm POST /auth/mock/confirm (order=XXX) —— 用户「确认支付」（mock 专用）。
// 合成一条带正确签名的平台回调，复用真实回调路径（验签→幂等→调主站入账）。
func (s *Server) handleMockConfirm(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Mock {
		http.NotFound(w, r)
		return
	}
	_ = r.ParseForm()
	orderNo := r.FormValue("order")
	if orderNo == "" {
		orderNo = r.URL.Query().Get("order")
	}
	order, err := s.repo.GetByOrderNo(r.Context(), orderNo)
	if err != nil {
		http.Error(w, "order not found", http.StatusNotFound)
		return
	}
	// 合成合法签名回调（=支付平台异步通知报文）。金额回传仅供对账，主站以库内订单金额入账。
	raw := s.sdk.Encode(payment.CallbackInfo{
		Provider:   order.Provider,
		OrderNo:    orderNo,
		Success:    true,
		PaidAmount: order.ActualPaid,
		TxnID:      "MOCK-" + orderNo,
	})
	if err := s.processNotify(r.Context(), order.Provider, raw); err != nil {
		writeJSON(w, http.StatusBadGateway, jsonObj{"success": false, "message": err.Error()})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = fmt.Fprintf(w, mockPaidPageHTML, html.EscapeString(orderNo))
}

// ---- 平台异步回调 ----

func (s *Server) handleWxpayNotify(w http.ResponseWriter, r *http.Request) {
	s.handleNotify(w, r, payment.ProviderWxpay)
}

func (s *Server) handleAlipayNotify(w http.ResponseWriter, r *http.Request) {
	s.handleNotify(w, r, payment.ProviderAlipay)
}

// handleNotify 据模式分发：真实→verify-and-forward；mock→Gateway 验签+幂等+forward。
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request, provider payment.Provider) {
	if !s.cfg.Mock {
		s.handleRealNotify(w, r, provider)
		return
	}
	// mock：读原文交给 Gateway（StubPaySDK 验签 → created→paid CAS → forward）。
	raw := readBody(r)
	if err := s.processNotify(r.Context(), provider, raw); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.ackNotify(w, provider)
}

// handleRealNotify 真实模式无状态回调：realpay 验签解析 → 成功则直接 forward 主站（主站幂等 + 金额校验）。
// 不读本地订单、不做本地 CAS：主站 payment_orders 的 created→paid→credited 是唯一幂等权威，
// 故平台重推/auth-service 重启都不会重复入账或丢单。
func (s *Server) handleRealNotify(w http.ResponseWriter, r *http.Request, provider payment.Provider) {
	info, err := s.real.VerifyNotify(r.Context(), provider, r)
	if err != nil {
		// 验签失败/报文非法/商户不符 → 400，让平台按策略重试（幂等已保证不重复入账）。
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if info.Success {
		po := payment.PaidOrder{
			OrderNo:    info.OrderNo,
			Type:       orderTypeFromNo(info.OrderNo),
			Provider:   provider,
			ActualPaid: info.PaidAmount, // 元；主站据此与库内订单金额比对（反篡改）
			Reference:  info.TxnID,
			PaidAt:     time.Now(),
		}
		if err := s.fwd.OnPaid(r.Context(), po); err != nil {
			// 入账失败（含金额不符被主站拒）→ 非 2xx，平台按策略重推；主站幂等吸收重复 forward。
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
	}
	// 平台期望的成功 ack（即便交易失败/关闭也 ack，停止无谓重推；不 forward 即不入账）。
	s.ackNotify(w, provider)
}

// ackNotify 返回各平台期望的成功应答，避免持续重推。
func (s *Server) ackNotify(w http.ResponseWriter, provider payment.Provider) {
	switch provider {
	case payment.ProviderWxpay:
		writeJSON(w, http.StatusOK, jsonObj{"code": "SUCCESS", "message": "OK"})
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("success")) // 支付宝要求返回 "success"
	}
}

// processNotify 复用 Gateway 的回调编排（mock 专用：验签 → created→paid CAS 幂等 → forwarder 调主站）。
func (s *Server) processNotify(ctx context.Context, provider payment.Provider, raw []byte) error {
	switch provider {
	case payment.ProviderWxpay:
		return s.gw.HandleWxpay(ctx, raw)
	case payment.ProviderAlipay:
		return s.gw.HandleAlipay(ctx, raw)
	default:
		return fmt.Errorf("unknown provider %q", provider)
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	wx, ali := s.providerAvailability()
	writeJSON(w, http.StatusOK, jsonObj{
		"success": true,
		"mock":    s.cfg.Mock,
		// providers = 是否已配置真实凭据（供主站决定渠道是否可下单）。
		"providers": jsonObj{"wxpay": wx, "alipay": ali},
	})
}

// providerAvailability 报告各支付渠道真实凭据是否齐全：
//   - mock 模式：两者都视为可用（true，下单走 mock 确认页，不需真实凭据）；
//   - 真实模式：据 realpay SDK 是否装配该渠道（s.real.Available）；s.real 为 nil 时一律 false。
func (s *Server) providerAvailability() (wxpay, alipay bool) {
	if s.cfg.Mock {
		return true, true
	}
	if s.real == nil {
		return false, false
	}
	return s.real.Available(payment.ProviderWxpay), s.real.Available(payment.ProviderAlipay)
}

// handleOrderStatus GET /auth/order/status?order_no=XXX[&provider=YYY] —— 主站对账查单。
// 内网鉴权（共享密钥头）；公网经 nginx 拒绝该路径。
//   - 真实模式：经 realpay 向微信/支付宝主动查单（需 provider 参数）。
//   - mock 模式：读本地 MemRepo 订单状态。
//
// 供主站 RCG/SUB 卡单对账：查到 paid → 补入账/补激活（回调丢失时的兜底）。
func (s *Server) handleOrderStatus(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get(internalSecretHeader) != s.cfg.Internal.SharedSecret {
		writeJSON(w, http.StatusUnauthorized, jsonObj{"success": false, "message": "unauthorized"})
		return
	}
	orderNo := r.URL.Query().Get("order_no")

	if !s.cfg.Mock {
		provider := payment.Provider(r.URL.Query().Get("provider"))
		if !provider.Valid() {
			writeJSON(w, http.StatusBadRequest, jsonObj{"success": false, "message": "provider required"})
			return
		}
		paid, err := s.real.QueryOrder(r.Context(), provider, orderNo)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, jsonObj{"success": false, "message": err.Error()})
			return
		}
		writeJSON(w, http.StatusOK, jsonObj{
			"success": true,
			"data":    jsonObj{"order_no": orderNo, "paid": paid},
		})
		return
	}

	order, err := s.repo.GetByOrderNo(r.Context(), orderNo)
	if err != nil {
		writeJSON(w, http.StatusNotFound, jsonObj{"success": false, "message": "order not found"})
		return
	}
	// paid|credited 都代表平台已收款（credited=主站已入账，paid=已收款待入账/在途）。
	paid := order.Status == payment.OrderPaid || order.Status == payment.OrderCredited
	writeJSON(w, http.StatusOK, jsonObj{
		"success": true,
		"data":    jsonObj{"order_no": order.OrderNo, "paid": paid, "status": string(order.Status)},
	})
}

// ---- forwarder：OrderSink 实现，验签+幂等通过后回调主站内网入账端点 ----

type forwarder struct {
	callbackURL string
	secret      string
	client      *http.Client
}

var _ payment.OrderSink = (*forwarder)(nil)

// OnPaid 把「已支付订单」回调主站 /api/internal/order/paid（共享密钥头）。
// 透传 paid_amount（元）+ provider 供主站反篡改金额校验；入账金额仍以主站库内订单为准。
// 失败返回 error → 调用方（mock Gateway 回滚；真实 handler 返回非 2xx）据此让平台重推/重试。
func (f *forwarder) OnPaid(ctx context.Context, o payment.PaidOrder) error {
	body, _ := common.Marshal(jsonObj{
		"order_no":    o.OrderNo,
		"txn_id":      o.Reference,
		"paid_amount": o.ActualPaid,
		"provider":    string(o.Provider),
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.callbackURL, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(internalSecretHeader, f.secret)
	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("call main site: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("main site credit failed: HTTP %d", resp.StatusCode)
	}
	return nil
}

// ---- 小工具 ----

type jsonObj = map[string]any

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = common.EncodeJson(w, v)
}

func readBody(r *http.Request) []byte {
	if r.Body == nil {
		return nil
	}
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(io.LimitReader(r.Body, 1<<20)) // 1MiB 上限，防滥用
	return buf.Bytes()
}

// orderTypeFromNo 据订单号前缀推断类型（SUB→套餐，其余→充值）；仅用于 Sink 选择，两 Sink 同为 forwarder。
func orderTypeFromNo(orderNo string) payment.OrderType {
	if strings.HasPrefix(orderNo, payment.OrderNoPrefixSubscription) {
		return payment.OrderTypeSubscription
	}
	return payment.OrderTypeRecharge
}

const mockPayPageHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<title>模拟支付确认</title></head>
<body style="font-family:system-ui;max-width:480px;margin:48px auto;padding:0 16px">
<h2>模拟支付确认（mock）</h2>
<p>订单号：<code>%s</code></p>
<p>渠道：<b>%s</b></p>
<p>应付：<b>&yen;%.2f</b>（$%.2f 充值额度）</p>
<form method="post" action="/auth/mock/confirm">
  <input type="hidden" name="order" value="%s">
  <button type="submit" data-testid="mock-pay-confirm"
    style="padding:12px 24px;font-size:16px;background:#07c160;color:#fff;border:0;border-radius:8px;cursor:pointer">
    确认已支付
  </button>
</form>
<p style="color:#888;margin-top:24px">真实模式下此页由微信/支付宝收银台替代。</p>
</body></html>`

const mockPaidPageHTML = `<!doctype html><html lang="zh"><head><meta charset="utf-8">
<title>支付成功</title></head>
<body style="font-family:system-ui;max-width:480px;margin:48px auto;padding:0 16px">
<h2 data-testid="mock-paid">支付成功 ✓</h2>
<p>订单 <code>%s</code> 已入账，请返回页面查看余额。</p>
</body></html>`
