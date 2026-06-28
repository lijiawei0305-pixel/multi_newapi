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

// NewGateway 组装支付网关。sinks 按订单类型映射入账目标（recharge→Wallet、subscription→TokenPlan）。
func NewGateway(repo OrderRepo, sdk PaySDK, sinks map[OrderType]OrderSink, opts ...Option) *Gateway {
	g := &Gateway{
		repo:       repo,
		sdk:        sdk,
		sinks:      sinks,
		newOrderNo: defaultOrderNo,
		now:        time.Now,
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
//	校验入参 → 生成唯一 order_no → 回填 notify_url → SDK 下单拿支付凭据 → 落库（order_no 唯一）
//
// 真实实现应「先落 created 订单再调 SDK」并将下单失败置 failed/删除，放进同一事务；
// 本轮内存假实现按序执行（见报告 TODO）。
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

	cred, err := g.sdk.CreatePay(ctx, PayRequest{
		Provider:   in.Provider,
		OrderNo:    o.OrderNo,
		AmountUSD:  in.AmountUSD,
		ActualPaid: in.ActualPaid,
		Subject:    in.Subject,
		NotifyURL:  o.NotifyURL,
	})
	if err != nil {
		return nil, err // SDK 下单失败原样上浮
	}
	o.PayURL = cred.PayURL

	if err := g.repo.Create(ctx, o); err != nil {
		return nil, err // PAY_ORDER_DUPLICATE（order_no 唯一约束冲突）
	}
	return o, nil
}

// defaultOrderNo 生成全局唯一订单号：PAY + 纳秒时间(base36) + 6 字节随机(hex)。
func defaultOrderNo() string {
	var b [6]byte
	_, _ = rand.Read(b[:]) // crypto/rand 失败概率可忽略；退化为纯时间序仍唯一性极高
	return "PAY" + strconv.FormatInt(time.Now().UnixNano(), 36) + hex.EncodeToString(b[:])
}
