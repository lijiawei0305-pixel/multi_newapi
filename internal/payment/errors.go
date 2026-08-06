package payment

import (
	"errors"
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

// ErrOrderNotExist 网关主动查单返回「订单不存在」（微信 ORDER_NOT_EXIST / 支付宝 TRADE_NOT_EXIST）：
// 该订单在网关侧从未创建或已被清除——终态，永不会被支付。对账据此（配合超时兜底）安全过期，区别于
// 可重试的瞬时查单错误（网络/超时/限流）。realpay 适配器以 %w 包裹返回，调用方用 errors.Is 判定。
var ErrOrderNotExist = errors.New("payment: order not exist at gateway")

// 本模块错误码命名空间（detailed-design §2.8 / §6.4）。
//
// 设计文档 §2.8 / tasks/08-payment.md 将「错签」写作 SIGN_INVALID（无前缀），
// 本实现按本仓库「模块前缀」约定（apperr 包注释、wallet 的 WALLET_*）统一加 PAY_ 前缀，
// 并遵循本轮任务书的 PAY_SIGN_INVALID。订单类错误沿用文档的 ORDER_* 命名。
// 该取舍已在交付报告 ④ 标注，便于 Master 统一裁决。
const (
	// CodeSignInvalid 回调验签失败（伪造 / 篡改）。
	CodeSignInvalid = "PAY_SIGN_INVALID"
	// CodeCallbackInvalid 回调原文无法解析（缺字段 / 非法格式）。
	CodeCallbackInvalid = "PAY_CALLBACK_INVALID"
	// CodeOrderNotFound order_no 未找到。
	CodeOrderNotFound = "ORDER_NOT_FOUND"
	// CodeOrderAlreadyPaid 订单已 paid/credited —— 幂等短路，回调对外返回成功（HTTP 200）。
	CodeOrderAlreadyPaid = "ORDER_ALREADY_PAID"
	// CodeOrderDuplicate 下单时 order_no 唯一约束冲突。
	CodeOrderDuplicate = "PAY_ORDER_DUPLICATE"
	// CodeOrderInvalid 下单入参非法（未绑 tenant/user、类型/渠道/金额非法等）。
	CodeOrderInvalid = "PAY_ORDER_INVALID"
	// CodeOrderTypeUnknown 订单类型无对应 OrderSink（防御；正常不达）。
	CodeOrderTypeUnknown = "PAY_ORDER_TYPE_UNKNOWN"
	// CodeAmountMismatch 回调实付金额与库内订单金额不一致（疑似篡改）—— 拒绝入账。
	CodeAmountMismatch = "PAY_AMOUNT_MISMATCH"
	// CodeProviderTxnConflict 同一支付机构交易号已绑定其他订单。
	CodeProviderTxnConflict = "PAY_PROVIDER_TXN_CONFLICT"
	// CodePaymentFactInvalid 支付事实不完整（缺交易号/金额/渠道不一致等）。
	CodePaymentFactInvalid = "PAY_FACT_INVALID"
	// CodeCreateOutcomeUnknown 平台下单结果未知（可能已送达）；订单保持 created，须查单恢复。
	CodeCreateOutcomeUnknown = "PAY_CREATE_UNKNOWN"
	// CodePayURLPersist 平台已返回凭据但本站落库失败。
	CodePayURLPersist = "PAY_URL_PERSIST_FAILED"
	// CodePayURLMissing 订单存在但无可用二维码，且无法安全恢复。
	CodePayURLMissing = "PAY_URL_MISSING"
	// CodeIdempotencyConflict 同一幂等键但支付意图（金额/渠道等）不一致。
	CodeIdempotencyConflict = "PAY_IDEMPOTENCY_CONFLICT"
)

var (
	// ErrSignInvalid 回调验签失败。
	ErrSignInvalid = apperr.New(CodeSignInvalid, "支付回调验签失败", http.StatusBadRequest)
	// ErrCallbackInvalid 回调原文无法解析。
	ErrCallbackInvalid = apperr.New(CodeCallbackInvalid, "支付回调报文非法", http.StatusBadRequest)
	// ErrOrderNotFound 未知订单号。
	ErrOrderNotFound = apperr.New(CodeOrderNotFound, "支付订单不存在", http.StatusNotFound)
	// ErrOrderAlreadyPaid 订单已处理（幂等短路对应错误码；handler 短路时对外返回成功而非该错误）。
	ErrOrderAlreadyPaid = apperr.New(CodeOrderAlreadyPaid, "订单已入账", http.StatusOK)
	// ErrOrderDuplicate order_no 唯一约束冲突。
	ErrOrderDuplicate = apperr.New(CodeOrderDuplicate, "订单号重复", http.StatusConflict)
	// ErrOrderInvalid 下单入参非法。
	ErrOrderInvalid = apperr.New(CodeOrderInvalid, "下单参数非法", http.StatusBadRequest)
	// ErrOrderTypeUnknown 订单类型无对应入账分发目标。
	ErrOrderTypeUnknown = apperr.New(CodeOrderTypeUnknown, "未知订单类型", http.StatusInternalServerError)
	// ErrAmountMismatch 回调实付金额与库内订单金额不一致（反篡改）。
	ErrAmountMismatch = apperr.New(CodeAmountMismatch, "支付金额与订单不一致", http.StatusBadRequest)
	// ErrProviderTxnConflict 支付机构交易号与其它订单冲突。
	ErrProviderTxnConflict = apperr.New(CodeProviderTxnConflict, "支付机构交易号冲突", http.StatusConflict)
	// ErrPaymentFactInvalid 支付事实校验失败。
	ErrPaymentFactInvalid = apperr.New(CodePaymentFactInvalid, "支付事实不完整或无效", http.StatusBadRequest)
	// ErrCreateOutcomeUnknown 下单结果未知（不标 failed）；HTTP 202，响应体须带 order_no。
	ErrCreateOutcomeUnknown = apperr.New(CodeCreateOutcomeUnknown, "支付订单确认中，请稍后查询状态", http.StatusAccepted)
	// ErrIdempotencyConflict 同一幂等键但支付意图字段不一致。
	ErrIdempotencyConflict = apperr.New(CodeIdempotencyConflict, "幂等键与支付意图不一致", http.StatusConflict)
	// ErrPayURLPersist 二维码落库失败。
	ErrPayURLPersist = apperr.New(CodePayURLPersist, "支付凭据保存失败，请重试", http.StatusServiceUnavailable)
	// ErrPayURLMissing 无可用二维码。
	ErrPayURLMissing = apperr.New(CodePayURLMissing, "支付二维码不可用，请重新发起或联系客服", http.StatusConflict)
)
