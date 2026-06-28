package wallet

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/internal/platform/apperr"
)

func TestMemRepoBalanceOps(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()

	// 未知账户余额为 0。
	if b, _ := repo.Balance(ctx, 1, 1); b != 0 {
		t.Fatalf("zero-value balance = %v, want 0", b)
	}
	_ = repo.AddBalance(ctx, 1, 1, 40)
	_ = repo.AddBalance(ctx, 1, 1, 10)
	if b, _ := repo.Balance(ctx, 1, 1); b != 50 {
		t.Fatalf("balance = %v, want 50", b)
	}
	// 租户隔离：不同租户/用户互不影响。
	if b, _ := repo.Balance(ctx, 2, 1); b != 0 {
		t.Fatalf("other-tenant balance = %v, want 0", b)
	}
}

func TestMemRepoChargeBalanceConditional(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	_ = repo.AddBalance(ctx, 1, 1, 10)

	rem, err := repo.ChargeBalance(ctx, 1, 1, 4)
	if err != nil || rem != 6 {
		t.Fatalf("charge 4 -> (%v,%v), want (6,nil)", rem, err)
	}
	// 不足时拦截且余额不变。
	if _, err := repo.ChargeBalance(ctx, 1, 1, 6.5); !apperr.Is(err, CodeQuotaInsufficient) {
		t.Fatalf("expected insufficient, got %v", err)
	}
	if b, _ := repo.Balance(ctx, 1, 1); b != 6 {
		t.Fatalf("balance after failed charge = %v, want 6", b)
	}
}

func TestMemRepoGetRedemptionNotFound(t *testing.T) {
	repo := NewMemRepo()
	if _, err := repo.GetRedemption(context.Background(), 1, "nope"); !apperr.Is(err, CodeRedeemCodeInvalid) {
		t.Fatalf("expected REDEEM_CODE_INVALID, got %v", err)
	}
}

func TestMemRepoAddRedemptionDefaults(t *testing.T) {
	repo := NewMemRepo()
	c := &RedemptionCode{TenantID: 1, Code: "A", AmountUSD: 5}
	repo.AddRedemption(c)
	if c.ID == 0 {
		t.Fatal("AddRedemption should backfill ID")
	}
	got, err := repo.GetRedemption(context.Background(), 1, "A")
	if err != nil {
		t.Fatalf("GetRedemption: %v", err)
	}
	if got.Status != RedemptionEnabled {
		t.Fatalf("default status = %q, want enabled", got.Status)
	}
	if got.CreatedAt.IsZero() {
		t.Fatal("CreatedAt should be backfilled")
	}
}

func TestMemRepoUseRedemptionCAS(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	c := &RedemptionCode{TenantID: 1, Code: "A", AmountUSD: 5}
	repo.AddRedemption(c)

	ok, err := repo.UseRedemption(ctx, c.ID, 9, time.Now())
	if err != nil || !ok {
		t.Fatalf("first use -> (%v,%v), want (true,nil)", ok, err)
	}
	// 二次 CAS 失败（已非 enabled）。
	ok, err = repo.UseRedemption(ctx, c.ID, 9, time.Now())
	if err != nil || ok {
		t.Fatalf("second use -> (%v,%v), want (false,nil)", ok, err)
	}
	// 未知 id。
	if _, err := repo.UseRedemption(ctx, 999, 9, time.Now()); !apperr.Is(err, CodeRedeemCodeInvalid) {
		t.Fatalf("unknown id err = %v, want REDEEM_CODE_INVALID", err)
	}
}

// TestRedeemConcurrentSingleWinner：N goroutine 并发兑换同一码，仅 1 个成功，余额只入账一次。
func TestRedeemConcurrentSingleWinner(t *testing.T) {
	repo := NewMemRepo()
	pricing := newFakePricing(1.0)
	svc := NewService(repo, pricing, newFakeEarnings())
	repo.AddRedemption(&RedemptionCode{TenantID: 5, Code: "ONCE", AmountUSD: 25})

	ctx := contextWithTenant(5, 8)
	const workers = 200
	var (
		win       int64
		used      int64
		wg        sync.WaitGroup
		startGate = make(chan struct{})
	)
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			defer wg.Done()
			<-startGate
			err := svc.Redeem(ctx, 8, "ONCE")
			switch {
			case err == nil:
				atomic.AddInt64(&win, 1)
			case apperr.Is(err, CodeRedeemCodeUsed):
				atomic.AddInt64(&used, 1)
			default:
				t.Errorf("unexpected redeem error: %v", err)
			}
		}()
	}
	close(startGate)
	wg.Wait()

	if win != 1 {
		t.Fatalf("winners = %d, want 1", win)
	}
	if win+used != workers {
		t.Fatalf("accounted = %d, want %d", win+used, workers)
	}
	if b, _ := repo.Balance(ctx, 5, 8); b != 25 {
		t.Fatalf("balance credited %v, want exactly 25 (once)", b)
	}
}
