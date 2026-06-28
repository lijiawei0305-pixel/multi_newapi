package payment

import (
	"context"
	"sync"
)

// fakeSink 是 OrderSink 的并发安全内存假实现，记录收到的 PaidOrder 并可注入错误。
type fakeSink struct {
	mu     sync.Mutex
	orders []PaidOrder
	err    error // 可注入：覆盖 OnPaid 失败/回滚分支
}

func newFakeSink() *fakeSink { return &fakeSink{} }

func (f *fakeSink) OnPaid(_ context.Context, o PaidOrder) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.err != nil {
		return f.err
	}
	f.orders = append(f.orders, o)
	return nil
}

func (f *fakeSink) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.orders)
}

func (f *fakeSink) last() (PaidOrder, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.orders) == 0 {
		return PaidOrder{}, false
	}
	return f.orders[len(f.orders)-1], true
}

// fakeSDK 是 PaySDK 的可控假实现：用于精确驱动 CreatePay 错误与 Verify 结果，
// 不依赖真实签名（签名/验签的真路径由 StubPaySDK 在 sdk_test 覆盖）。
type fakeSDK struct {
	createErr error
	payURL    string
	verifyFn  func(provider Provider, raw []byte) (*CallbackInfo, error)
}

func (f *fakeSDK) CreatePay(_ context.Context, req PayRequest) (*PayCredential, error) {
	if f.createErr != nil {
		return nil, f.createErr
	}
	url := f.payURL
	if url == "" {
		url = "https://pay.fake/" + req.OrderNo
	}
	return &PayCredential{PayURL: url}, nil
}

func (f *fakeSDK) Verify(_ context.Context, provider Provider, raw []byte) (*CallbackInfo, error) {
	return f.verifyFn(provider, raw)
}

// okVerify 返回一个把 raw 直接当作 order_no、交易成功的验签函数（驱动入账分发）。
func okVerify() func(Provider, []byte) (*CallbackInfo, error) {
	return func(p Provider, raw []byte) (*CallbackInfo, error) {
		return &CallbackInfo{Provider: p, OrderNo: string(raw), Success: true, TxnID: "txn-" + string(raw)}, nil
	}
}
