package quota

import (
	"context"
	"testing"
)

// fakeSource 验证一个实现满足 Source 契约（编译期 + 运行期）。
type fakeSource struct {
	bal float64
}

func (f *fakeSource) Charge(_ context.Context, cost float64) (Receipt, error) {
	f.bal -= cost
	return Receipt{Kind: KindWallet, ChargedUSD: cost, RemainingUSD: f.bal}, nil
}
func (f *fakeSource) Balance(_ context.Context) (float64, error) { return f.bal, nil }

// 编译期断言：fakeSource 实现 Source。
var _ Source = (*fakeSource)(nil)

func TestReceiptAndSourceContract(t *testing.T) {
	var s Source = &fakeSource{bal: 100}
	r, err := s.Charge(context.Background(), 30)
	if err != nil {
		t.Fatal(err)
	}
	if r.Kind != KindWallet || r.ChargedUSD != 30 || r.RemainingUSD != 70 {
		t.Fatalf("unexpected receipt: %+v", r)
	}
	if b, _ := s.Balance(context.Background()); b != 70 {
		t.Fatalf("balance = %v", b)
	}
}
