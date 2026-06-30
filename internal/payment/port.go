package payment

import (
	"context"
	"time"
)

// --- 对外接口（detailed-design §2.8 的 Go 签名）---

// PaymentGateway 负责下单：落库支付订单（order_no 唯一）、回填 notify_url、返回支付凭据。
type PaymentGateway interface {
	// CreateOrder 创建支付订单。type=recharge|subscription，强制绑 tenant_id+user_id。
	CreateOrder(ctx context.Context, in OrderInput) (*PayOrder, error)
}

// CallbackHandler 处理支付平台异步回调（部署于 auth-service，经 Nginx /pay /auth 转发）。
// 流程：验签 → 幂等（order_no 已 paid/credited 则短路成功）→ 按 type 分发到 OrderSink。
type CallbackHandler interface {
	// HandleWxpay 处理微信回调原文。错签 → PAY_SIGN_INVALID；未知单 → ORDER_NOT_FOUND。
	HandleWxpay(ctx context.Context, raw []byte) error
	// HandleAlipay 处理支付宝回调原文。
	HandleAlipay(ctx context.Context, raw []byte) error
}

// --- 消费者定义的依赖接口（本包声明，cmd/main 装配具体实现；detailed-design §1.4）---

// OrderSink 接收一笔已支付订单并完成入账，由 Wallet / TokenPlan 实现并在 main 按 type 注入：
//   - recharge     → Wallet.Credit（钱包充值，触发充值差价收益，§3.3）
//   - subscription → TokenPlan.ActivateFromPayment（幂等激活订阅，§3.2）
//
// Payment 不 import Wallet/TokenPlan，仅调 OrderSink（detailed-design §2.8）。
type OrderSink interface {
	OnPaid(ctx context.Context, order PaidOrder) error
}

// PaySDK 抽象支付平台 SDK（下单 + 验签）。本轮提供 std-lib HMAC 占位实现 StubPaySDK；
// 真实微信/支付宝 SDK 适配器顺延（见报告 TODO），不引入外部依赖。
type PaySDK interface {
	// CreatePay 向支付平台下单，返回支付凭据（跳转/二维码）。
	CreatePay(ctx context.Context, req PayRequest) (*PayCredential, error)
	// Verify 校验回调原文签名并解析订单信息：
	//   验签失败 → ErrSignInvalid；报文非法 → ErrCallbackInvalid。
	Verify(ctx context.Context, provider Provider, raw []byte) (*CallbackInfo, error)
}

// OrderRepo 是支付订单持久化抽象。本轮提供并发安全内存假实现（MemRepo）；
// 真实 GORM 实现（order_no 唯一索引、scopeByTenant、状态机条件 UPDATE、迁移）顺延（见报告 TODO）。
type OrderRepo interface {
	// Create 落库新订单；order_no 已存在返回 ErrOrderDuplicate。
	Create(ctx context.Context, o *PayOrder) error
	// GetByOrderNo 按订单号查；不存在返回 ErrOrderNotFound。
	GetByOrderNo(ctx context.Context, orderNo string) (*PayOrder, error)
	// CompareAndSetStatus 原子 CAS：仅当当前状态等于 from 才置为 to。
	//   ok=true  迁移成功（本次为推进者）。
	//   ok=false 当前状态非 from（已被并发推进 / 已终态）→ 调用方据此幂等短路。
	// 订单不存在返回 ErrOrderNotFound。
	CompareAndSetStatus(ctx context.Context, orderNo string, from, to OrderStatus) (ok bool, err error)
	// ListByStatus 返回处于 status 且 UpdatedAt 早于 before 的订单（对账兜底扫描用）。
	// before 过滤掉刚占位、可能仍在入账的在途订单，避免与正常回调竞态。
	ListByStatus(ctx context.Context, status OrderStatus, before time.Time) ([]*PayOrder, error)
}
