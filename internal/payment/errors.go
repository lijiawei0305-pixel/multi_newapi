package payment

import (
	"net/http"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

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
)
