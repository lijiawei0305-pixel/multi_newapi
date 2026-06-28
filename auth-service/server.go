package authservice

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/internal/payment"
)

// internalSecretHeader 必须与主站 internal/mtwire.internalSecretHeader 一致。
const internalSecretHeader = "X-Internal-Secret"

// Server 是 auth-service 的 HTTP 服务。
//
// 复用 internal/payment 的状态机蓝本：Gateway(MemRepo + StubPaySDK + forwarder Sink)。
//   - 下单（/auth/order）：据主站给定 order_no 落一条 created 订单（MemRepo）+ 返回 mock 支付凭据；
//   - 回调（/auth/{wxpay,alipay}/notify）：Gateway 验签 → created→paid CAS 幂等 → forwarder 调主站入账；
//   - mock 确认页（/auth/mock/pay|confirm）：用户「支付」→ 合成合法签名回调 → 复用同一回调路径。
type Server struct {
	cfg    Config
	gw     *payment.Gateway
	repo   payment.OrderRepo   // 直接落单/读单（确认页展示）
	sdk    *payment.StubPaySDK // 合成 mock 回调（与 gw 验签同密钥）
	client *http.Client
}

// NewServer 组装服务：Gateway 的两个 Sink 都指向 forwarder（统一回调主站，主站按前缀分发入账）。
func NewServer(cfg Config) *Server {
	repo := payment.NewMemRepo()
	sdk := payment.NewStubPaySDK(cfg.SignSecret)
	client := &http.Client{Timeout: 8 * time.Second}
	fwd := &forwarder{
		callbackURL: cfg.Internal.CallbackURL,
		secret:      cfg.Internal.SharedSecret,
		client:      client,
	}
	sinks := map[payment.OrderType]payment.OrderSink{
		payment.OrderTypeRecharge:     fwd,
		payment.OrderTypeSubscription: fwd,
	}
	gw := payment.NewGateway(repo, sdk, sinks)
	return &Server{cfg: cfg, gw: gw, repo: repo, sdk: sdk, client: client}
}

// Router 返回挂载全部路由的 http.Handler（Go 1.22+ 方法模式）。
func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/healthz", s.handleHealth)
	mux.HandleFunc("POST /auth/order", s.handleCreateOrder)
	mux.HandleFunc("GET /auth/mock/pay", s.handleMockPayPage)
	mux.HandleFunc("POST /auth/mock/confirm", s.handleMockConfirm)
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

// handleCreateOrder POST /auth/order —— 主站下单。落 created 订单 + 返回 mock 支付凭据（pay_url）。
func (s *Server) handleCreateOrder(w http.ResponseWriter, r *http.Request) {
	var req createOrderRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.OrderNo == "" {
		writeJSON(w, http.StatusBadRequest, jsonObj{"success": false, "message": "bad request"})
		return
	}
	provider := payment.Provider(req.Provider)
	if !provider.Valid() {
		writeJSON(w, http.StatusBadRequest, jsonObj{"success": false, "message": "invalid provider"})
		return
	}

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
			"mock":     s.cfg.Mock,
		},
	})
}

// payURL 拼装 mock 支付确认页 URL（微信→二维码内容、支付宝→跳转目标，均指向此页）。
func (s *Server) payURL(orderNo string) string {
	return strings.TrimRight(s.cfg.Server.PublicBaseURL, "/") + "/auth/mock/pay?order=" + orderNo
}

// ---- mock 支付确认页 ----

// handleMockPayPage GET /auth/mock/pay?order=XXX —— 极简确认页（mock 专用）。
func (s *Server) handleMockPayPage(w http.ResponseWriter, r *http.Request) {
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

// handleMockConfirm POST /auth/mock/confirm (order=XXX) —— 用户「确认支付」。
// 合成一条带正确签名的平台回调，复用真实回调路径（验签→幂等→调主站入账）。
func (s *Server) handleMockConfirm(w http.ResponseWriter, r *http.Request) {
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

// ---- 平台异步回调（真实模式下由微信/支付宝 POST；mock 由确认页合成）----

func (s *Server) handleWxpayNotify(w http.ResponseWriter, r *http.Request) {
	s.handleNotify(w, r, payment.ProviderWxpay)
}

func (s *Server) handleAlipayNotify(w http.ResponseWriter, r *http.Request) {
	s.handleNotify(w, r, payment.ProviderAlipay)
}

// handleNotify 读原文交给 Gateway（验签→幂等→forward）；据渠道返回平台期望的 ack。
func (s *Server) handleNotify(w http.ResponseWriter, r *http.Request, provider payment.Provider) {
	raw := readBody(r)
	if err := s.processNotify(r.Context(), provider, raw); err != nil {
		// 验签失败/未知单等：返回 400，让平台按其策略重试（幂等已保证不重复入账）。
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// 平台期望的成功 ack（避免持续重推）。
	switch provider {
	case payment.ProviderWxpay:
		writeJSON(w, http.StatusOK, jsonObj{"code": "SUCCESS", "message": "OK"})
	default:
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("success")) // 支付宝要求返回 "success"
	}
}

// processNotify 复用 Gateway 的回调编排（验签 → created→paid CAS 幂等 → forwarder 调主站）。
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
	writeJSON(w, http.StatusOK, jsonObj{"success": true, "mock": s.cfg.Mock})
}

// ---- forwarder：OrderSink 实现，验签+幂等通过后回调主站内网入账端点 ----

type forwarder struct {
	callbackURL string
	secret      string
	client      *http.Client
}

var _ payment.OrderSink = (*forwarder)(nil)

// OnPaid 把「已支付订单」回调主站 /api/internal/order/paid（共享密钥头）。失败返回 error，
// Gateway 据此回滚 paid→created，待平台重推/重试再 forward（不丢账、不重复入账）。
func (f *forwarder) OnPaid(ctx context.Context, o payment.PaidOrder) error {
	body, _ := json.Marshal(jsonObj{"order_no": o.OrderNo, "txn_id": o.Reference})
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
	_ = json.NewEncoder(w).Encode(v)
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
