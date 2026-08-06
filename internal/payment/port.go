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

// CallbackHandler 处理当前 API 进程收到的支付平台异步回调。
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

// PaySDK 抽象支付平台 SDK（下单 + 验签）；生产装配由进程内支付适配器实现。
type PaySDK interface {
	// CreatePay 向支付平台下单，返回支付凭据（跳转/二维码）。
	CreatePay(ctx context.Context, req PayRequest) (*PayCredential, error)
	// Verify 校验回调原文签名并解析订单信息：
	//   验签失败 → ErrSignInvalid；报文非法 → ErrCallbackInvalid。
	Verify(ctx context.Context, provider Provider, raw []byte) (*CallbackInfo, error)
}

// OrderRepo 是支付订单持久化抽象；生产装配使用 GORM 仓储，测试可使用内存实现。
type OrderRepo interface {
	// Create 落库新订单；order_no 或 idempotency_key 唯一冲突返回 ErrOrderDuplicate。
	Create(ctx context.Context, o *PayOrder) error
	// GetByOrderNo 按订单号查；不存在返回 ErrOrderNotFound。
	GetByOrderNo(ctx context.Context, orderNo string) (*PayOrder, error)
	// GetByIdempotencyKey 按幂等键查；不存在返回 ErrOrderNotFound。key 空串不得调用。
	GetByIdempotencyKey(ctx context.Context, key string) (*PayOrder, error)
	// GetByProviderTxn 按 (provider, provider_transaction_id) 查；不存在返回 ErrOrderNotFound。
	GetByProviderTxn(ctx context.Context, provider Provider, txnID string) (*PayOrder, error)
	// CompareAndSetStatus 原子 CAS：仅当当前状态等于 from 才置为 to。
	//   ok=true  迁移成功（本次为推进者）。
	//   ok=false 当前状态非 from（已被并发推进 / 已终态）→ 调用方据此幂等短路。
	// 订单不存在返回 ErrOrderNotFound。
	CompareAndSetStatus(ctx context.Context, orderNo string, from, to OrderStatus) (ok bool, err error)
	// ListByStatus 返回处于 status 且 UpdatedAt 早于 before 的订单（对账兜底扫描用）。
	// before 过滤掉刚占位、可能仍在入账的在途订单，避免与正常回调竞态。
	// limit>0 时在 SQL/存储层 ORDER BY updated_at,id 并 LIMIT（PAY-REC-02）；limit<=0 表示不截断。
	ListByStatus(ctx context.Context, status OrderStatus, before time.Time, limit int) ([]*PayOrder, error)
	// ListDueForQuery 返回 status=created 且 next_query_at<=now 的待主动查单订单（ORDER BY next_query_at,id）。
	ListDueForQuery(ctx context.Context, now time.Time, limit int) ([]*PayOrder, error)
	// SetPayURL 回填支付凭据 PayURL（下单改为「先落 created 订单、再向平台下单」后，
	// 拿到凭据回填；订单不存在返回 ErrOrderNotFound）。
	SetPayURL(ctx context.Context, orderNo, payURL string) error
	// SetPayURLFenced 条件写回 PayURL（防 closed/replaced 迟到 Prepay 写回）。
	SetPayURLFenced(ctx context.Context, orderNo, payURL, claimToken string, allowedStates []CreateState) error
	// SavePaymentFacts 持久化支付机构事实与时间戳（txn/paid_at/callback_at）；幂等覆盖空字段。
	// 若 (provider,txn) 已被其它 order_no 占用 → ErrProviderTxnConflict。
	SavePaymentFacts(ctx context.Context, orderNo string, facts PaymentFacts) error
	// MarkCredited 将订单置 credited 并写 credited_at（仅当当前 paid）。
	MarkCredited(ctx context.Context, orderNo string, at time.Time) (ok bool, err error)
	// ScheduleNextQuery 更新 next_query_at 与 query_attempts。
	ScheduleNextQuery(ctx context.Context, orderNo string, nextAt time.Time, attempts int) error
	// ClaimForQuery 查单租约：排除 prepay_inflight 与有效 operation lease。
	ClaimForQuery(ctx context.Context, orderNo string, now, leaseUntil time.Time) (token string, ok bool, err error)
	// ClaimForPrepay 认领唯一 Prepay 操作：local_created|prepay_unknown → prepay_inflight + token。
	// 同时把 next_query_at 推到 leaseUntil（≥ Prepay budget+grace），禁止 5s 内 Query 抢跑。
	ClaimForPrepay(ctx context.Context, orderNo string, now, leaseUntil time.Time) (token string, ok bool, err error)
	// FinishPrepayFenced token 持有者写回 create_state / pay_url 调度；返回 applied。
	FinishPrepayFenced(ctx context.Context, orderNo, token string, to CreateState, payURL string, nextQueryAt time.Time, errorClass, stage string, createAttempts int) (applied bool, err error)
	// FinishQueryFenced 仅 token 持有者可写回调度/create_state；返回 applied。
	FinishQueryFenced(ctx context.Context, orderNo, token string, nextAt time.Time, attempts int, createState CreateState, tradeState string) (applied bool, err error)
	// ReleaseQueryClaim 异常释放 claim。
	ReleaseQueryClaim(ctx context.Context, orderNo, token string) error
	// TransitionCreateState CAS create_state（仅 status=created）。
	TransitionCreateState(ctx context.Context, orderNo string, from, to CreateState) (ok bool, err error)
	// TransitionCreateStateFenced 带 operation token 的 CAS。
	TransitionCreateStateFenced(ctx context.Context, orderNo, token string, from, to CreateState) (applied bool, err error)
	// RecordCreateFailure 记录创建失败观测字段（不改变 status，除非 status 非空）。
	RecordCreateFailure(ctx context.Context, orderNo string, errorClass, stage string, createAttempts int) error
	// ScheduleNextQueryFenced 带 token 的调度写回。
	ScheduleNextQueryFenced(ctx context.Context, orderNo, token string, nextAt time.Time, attempts int) (applied bool, err error)
}

// PaymentFacts 是回调/查单确认后写入订单的支付机构事实。
type PaymentFacts struct {
	Provider              Provider // 用于 (provider, txn) 复合唯一冲突检测；零值时回退订单自身 provider
	ProviderTransactionID string
	ProviderPaidAt        time.Time
	CallbackReceivedAt    time.Time // 零值=本次不更新；仅真实回调路径写入
	ClearNextQuery        bool      // true 时清空 next_query_at（已付无需再查）
}
