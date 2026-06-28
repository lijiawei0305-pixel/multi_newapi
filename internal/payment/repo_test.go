package payment

import (
	"context"
	"testing"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestMemRepoCreateAndGet(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()
	o := &PayOrder{OrderNo: "PAY1", Type: OrderTypeRecharge, TenantID: 1, UserID: 2, Status: OrderCreated}

	if err := r.Create(ctx, o); err != nil {
		t.Fatalf("Create: %v", err)
	}
	// 重复 order_no → 唯一约束冲突。
	if got := apperr.CodeOf(r.Create(ctx, o)); got != CodeOrderDuplicate {
		t.Fatalf("duplicate Create code = %q, want %q", got, CodeOrderDuplicate)
	}

	got, err := r.GetByOrderNo(ctx, "PAY1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TenantID != 1 || got.UserID != 2 || got.Status != OrderCreated {
		t.Fatalf("got = %+v", got)
	}
	// 返回拷贝：外部篡改不影响库内。
	got.Status = OrderCredited
	again, _ := r.GetByOrderNo(ctx, "PAY1")
	if again.Status != OrderCreated {
		t.Fatalf("repo snapshot mutated externally: %v", again.Status)
	}

	if _, err := r.GetByOrderNo(ctx, "NOPE"); apperr.CodeOf(err) != CodeOrderNotFound {
		t.Fatalf("missing Get code = %q, want %q", apperr.CodeOf(err), CodeOrderNotFound)
	}
}

func TestMemRepoCompareAndSetStatus(t *testing.T) {
	ctx := context.Background()
	r := NewMemRepo()
	_ = r.Create(ctx, &PayOrder{OrderNo: "PAY1", Status: OrderCreated})

	// from 不匹配 → ok=false，无错误，状态不变。
	ok, err := r.CompareAndSetStatus(ctx, "PAY1", OrderPaid, OrderCredited)
	if err != nil || ok {
		t.Fatalf("mismatch CAS: ok=%v err=%v, want ok=false err=nil", ok, err)
	}

	// created→paid 成功。
	ok, err = r.CompareAndSetStatus(ctx, "PAY1", OrderCreated, OrderPaid)
	if err != nil || !ok {
		t.Fatalf("created→paid: ok=%v err=%v", ok, err)
	}
	// 再次 created→paid → 已非 created → ok=false（幂等基石）。
	ok, _ = r.CompareAndSetStatus(ctx, "PAY1", OrderCreated, OrderPaid)
	if ok {
		t.Fatal("second created→paid must fail (idempotency)")
	}

	// 订单不存在 → ErrOrderNotFound。
	_, err = r.CompareAndSetStatus(ctx, "NOPE", OrderCreated, OrderPaid)
	if apperr.CodeOf(err) != CodeOrderNotFound {
		t.Fatalf("missing CAS code = %q, want %q", apperr.CodeOf(err), CodeOrderNotFound)
	}
}
