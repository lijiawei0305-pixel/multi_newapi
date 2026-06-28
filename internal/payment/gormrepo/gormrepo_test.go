package gormrepo

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/payment"
)

// newTestRepo 用纯 Go sqlite(:memory:) 建一个隔离的支付订单仓储。
// 单连接：:memory: 每连接独立库，限 1 连接保证并发用例命中同一库（对齐 model/tokenplan 测试约定）。
func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatalf("db handle: %v", err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := AutoMigrate(db); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return New(db)
}

func createdOrder(no string) *payment.PayOrder {
	return &payment.PayOrder{
		OrderNo: no, Type: payment.OrderTypeRecharge, TenantID: 7, UserID: 42,
		Provider: payment.ProviderWxpay, AmountUSD: 10, ActualPaid: 73, Status: payment.OrderCreated,
	}
}

func TestGormCreateAndGet(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if err := r.Create(ctx, createdOrder("RCG-1")); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := r.GetByOrderNo(ctx, "RCG-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.TenantID != 7 || got.UserID != 42 || got.AmountUSD != 10 || got.Status != payment.OrderCreated {
		t.Fatalf("roundtrip mismatch: %+v", got)
	}
}

func TestGormCreateDuplicateRejected(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if err := r.Create(ctx, createdOrder("RCG-dup")); err != nil {
		t.Fatalf("first create: %v", err)
	}
	err := r.Create(ctx, createdOrder("RCG-dup"))
	if err != payment.ErrOrderDuplicate {
		t.Fatalf("duplicate create err = %v, want ErrOrderDuplicate", err)
	}
}

func TestGormGetNotFound(t *testing.T) {
	r := newTestRepo(t)
	if _, err := r.GetByOrderNo(context.Background(), "missing"); err != payment.ErrOrderNotFound {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

// TestGormCASConcurrentSingleWinner 并发 created→paid CAS：恰好一个胜者（强幂等地基）。
func TestGormCASConcurrentSingleWinner(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	if err := r.Create(ctx, createdOrder("RCG-cas")); err != nil {
		t.Fatalf("create: %v", err)
	}

	const n = 50
	var winners int64
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			ok, err := r.CompareAndSetStatus(ctx, "RCG-cas", payment.OrderCreated, payment.OrderPaid)
			if err != nil {
				t.Errorf("cas: %v", err)
				return
			}
			if ok {
				atomic.AddInt64(&winners, 1)
			}
		}()
	}
	wg.Wait()
	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}
	got, _ := r.GetByOrderNo(ctx, "RCG-cas")
	if got.Status != payment.OrderPaid {
		t.Fatalf("status = %q, want paid", got.Status)
	}
}

// TestGormCASNotFound 对不存在订单做 CAS → ErrOrderNotFound。
func TestGormCASNotFound(t *testing.T) {
	r := newTestRepo(t)
	ok, err := r.CompareAndSetStatus(context.Background(), "nope", payment.OrderCreated, payment.OrderPaid)
	if ok || err != payment.ErrOrderNotFound {
		t.Fatalf("got ok=%v err=%v, want false/ErrOrderNotFound", ok, err)
	}
}
