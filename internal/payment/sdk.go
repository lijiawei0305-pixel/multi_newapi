package payment

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
)

// StubPaySDK 是 PaySDK 的占位实现，仅依赖 Go 标准库（crypto/hmac + sha256）。
//
// 它用 HMAC-SHA256（共享密钥）模拟支付平台的签名/验签语义，使下单与「验签 → 入账」
// 全链路可在无外部依赖下端到端跑通与单测；**它不是生产用微信/支付宝 SDK**。
// 真实 wxpay/alipay SDK 适配器（证书、RSA/V3 验签、对账）顺延（见报告 TODO）。
type StubPaySDK struct {
	secret []byte
}

// 编译期断言：StubPaySDK 实现 PaySDK。
var _ PaySDK = (*StubPaySDK)(nil)

// NewStubPaySDK 用共享密钥构造占位 SDK。
func NewStubPaySDK(secret string) *StubPaySDK {
	return &StubPaySDK{secret: []byte(secret)}
}

// stubPayload 是占位回调原文的线格式（JSON）。
type stubPayload struct {
	OrderNo string  `json:"order_no"`
	Success bool    `json:"success"`
	Amount  float64 `json:"amount"`
	TxnID   string  `json:"txn_id"`
	Sign    string  `json:"sign"`
}

// sign 对回调字段计算 HMAC-SHA256 十六进制签名。
func (s *StubPaySDK) sign(p stubPayload) string {
	mac := hmac.New(sha256.New, s.secret)
	// 规范化拼接，避免浮点格式歧义。
	canonical := strings.Join([]string{
		p.OrderNo,
		strconv.FormatBool(p.Success),
		strconv.FormatFloat(p.Amount, 'f', -1, 64),
		p.TxnID,
	}, "|")
	mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

// CreatePay 占位下单：直接返回确定性的占位支付链接（不访问网络）。
func (s *StubPaySDK) CreatePay(_ context.Context, req PayRequest) (*PayCredential, error) {
	return &PayCredential{
		PayURL: "https://pay.stub/" + string(req.Provider) + "/" + req.OrderNo,
	}, nil
}

// Verify 校验回调签名并解析：报文非法 → ErrCallbackInvalid；验签失败 → ErrSignInvalid。
func (s *StubPaySDK) Verify(_ context.Context, provider Provider, raw []byte) (*CallbackInfo, error) {
	var p stubPayload
	if err := json.Unmarshal(raw, &p); err != nil || p.OrderNo == "" {
		return nil, ErrCallbackInvalid
	}
	want := s.sign(p)
	if !hmac.Equal([]byte(want), []byte(p.Sign)) { // 常量时间比较
		return nil, ErrSignInvalid
	}
	return &CallbackInfo{
		Provider:   provider,
		OrderNo:    p.OrderNo,
		Success:    p.Success,
		PaidAmount: p.Amount,
		TxnID:      p.TxnID,
	}, nil
}

// Encode 生成一条带正确签名的占位回调原文（供 main 自检 / 单测构造合法回调）。
func (s *StubPaySDK) Encode(info CallbackInfo) []byte {
	p := stubPayload{
		OrderNo: info.OrderNo,
		Success: info.Success,
		Amount:  info.PaidAmount,
		TxnID:   info.TxnID,
	}
	p.Sign = s.sign(p)
	b, _ := json.Marshal(p)
	return b
}
