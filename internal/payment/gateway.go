package payment

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"strconv"
	"time"
)

// Gateway 同时实现 PaymentGateway（下单）与 CallbackHandler（回调入账分发）。
// 部署于 auth-service 单进程；所有依赖以接口注入，便于单测（mock SDK/Sink/Repo）。
type Gateway struct {
	repo       OrderRepo
	sdk        PaySDK
	sinks      map[OrderType]OrderSink // 按订单类型分发的入账目标（main 注入）
	notifyBase string                  // notify_url 前缀（公网回调基址）
	newOrderNo func() string           // 订单号生成器（可注入，便于测试唯一冲突）
	now        func() time.Time
	logf       func(format string, args ...any) // 异常观测钩子（默认 no-op；main 注入 SysLog），本包保持纯净
}

// 编译期断言：Gateway 实现两个对外接口。
var (
	_ PaymentGateway  = (*Gateway)(nil)
	_ CallbackHandler = (*Gateway)(nil)
)

// Option 是 Gateway 的可选配置。
type Option func(*Gateway)

// WithNotifyBaseURL 设置异步回调地址前缀（如 https://api.example.com）。
func WithNotifyBaseURL(base string) Option { return func(g *Gateway) { g.notifyBase = base } }

// WithClock 注入时钟（测试用）。
func WithClock(now func() time.Time) Option { return func(g *Gateway) { g.now = now } }

// WithOrderNoFunc 注入订单号生成器（测试用，可制造唯一冲突）。
func WithOrderNoFunc(fn func() string) Option { return func(g *Gateway) { g.newOrderNo = fn } }

// WithErrorLogf 注入异常观测钩子（如 common.SysLog 包装）。用于把「入账成功后状态推进失败」
// 等静默异常上报，替代原先 `_, _ =` 的吞错；默认 no-op，保持本包无外部日志依赖。
func WithErrorLogf(fn func(format string, args ...any)) Option {
	return func(g *Gateway) {
		if fn != nil {
			g.logf = fn
		}
	}
}

// NewGateway 组装支付网关。sinks 按订单类型映射入账目标（recharge→Wallet、subscription→TokenPlan）。
func NewGateway(repo OrderRepo, sdk PaySDK, sinks map[OrderType]OrderSink, opts ...Option) *Gateway {
	g := &Gateway{
		repo:       repo,
		sdk:        sdk,
		sinks:      sinks,
		newOrderNo: defaultOrderNo,
		now:        time.Now,
		logf:       func(string, ...any) {}, // 默认 no-op；main 经 WithErrorLogf 注入 SysLog
	}
	for _, opt := range opts {
		opt(g)
	}
	return g
}

// notifyURL 拼接某渠道的完整回调地址。
func (g *Gateway) notifyURL(p Provider) string {
	return g.notifyBase + p.NotifyPath()
}

// CreateOrder 下单（detailed-design §2.8 / tasks/08-payment.md）：
//
//	校验入参 → 生成唯一 order_no → 回填 notify_url → **先落 created 订单** → 向平台下单拿支付凭据 → 回填 PayURL
//
// 顺序修正（审计 M3）：先落库再向平台下单。避免「平台已建单、本地无记录」的孤儿单——
// 若本地 Create 失败，直接返回错误、根本不向平台建单；若平台下单失败，本地 created 单置 failed
// （终态，不被对账反复查单）。本地存在而平台无单是无害的（用户拿不到支付凭据、不会去付）。
func (g *Gateway) CreateOrder(ctx context.Context, in OrderInput) (*PayOrder, error) {
	if err := in.validate(); err != nil {
		return nil, err // PAY_ORDER_INVALID
	}
	now := g.now()
	o := &PayOrder{
		OrderNo:    g.newOrderNo(),
		Type:       in.Type,
		TenantID:   in.TenantID,
		UserID:     in.UserID,
		Provider:   in.Provider,
		AmountUSD:  in.AmountUSD,
		ActualPaid: in.ActualPaid,
		GroupID:    in.GroupID,
		PlanID:     in.PlanID,
		Subject:    in.Subject,
		Reference:  in.Reference,
		Status:     OrderCreated,
		NotifyURL:  g.notifyURL(in.Provider),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	// 先落 created 订单（本地权威）。失败则根本不向平台建单，无孤儿。
	if err := g.repo.Create(ctx, o); err != nil {
		return nil, err // PAY_ORDER_DUPLICATE（order_no 唯一约束冲突）
	}

	cred, err := g.sdk.CreatePay(ctx, PayRequest{
		Provider:   in.Provider,
		OrderNo:    o.OrderNo,
		AmountUSD:  in.AmountUSD,
		ActualPaid: in.ActualPaid,
		Subject:    in.Subject,
		NotifyURL:  o.NotifyURL,
	})
	if err != nil {
		// 平台下单失败 → 本地 created 单置 failed（终态），避免被 ReconcileStuckCreated 反复查单。
		if ok, csErr := g.repo.CompareAndSetStatus(ctx, o.OrderNo, OrderCreated, OrderFailed); csErr != nil || !ok {
			g.logf("payment: create %s: CreatePay failed and mark-failed failed (ok=%v err=%v)", o.OrderNo, ok, csErr)
		}
		return nil, err // SDK 下单失败原样上浮
	}
	o.PayURL = cred.PayURL

	// 回填支付凭据。失败不阻断（PayURL 已在内存返回给前端）；仅观测。
	if err := g.repo.SetPayURL(ctx, o.OrderNo, o.PayURL); err != nil {
		g.logf("payment: create %s: persist pay_url failed: %v", o.OrderNo, err)
	}
	return o, nil
}

// GetByOrderNo 按订单号查单笔订单快照（只读，不改变状态机）。供 order-status 端点（扫码支付后
// 前端轮询探活）与其他需要直读单笔订单的调用方使用。不存在时原样上浮 repo 的 ErrOrderNotFound。
func (g *Gateway) GetByOrderNo(ctx context.Context, orderNo string) (*PayOrder, error) {
	return g.repo.GetByOrderNo(ctx, orderNo)
}

// defaultOrderNo 生成全局唯一订单号：PAY + 纳秒时间(base36) + 6 字节随机(hex)。
func defaultOrderNo() string {
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand 失败概率可忽略；退化为纯时间序仍唯一性极高
	return "PAY" + strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(b[:])
}
