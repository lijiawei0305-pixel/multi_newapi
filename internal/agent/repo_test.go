package agent

import (
	"context"
	"testing"
)

func TestMemRepo_GetAgentTypeNotFound(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	if _, found, err := repo.GetAgentType(ctx, 123); err != nil || found {
		t.Fatalf("GetAgentType(unknown): found=%v err=%v, want false/nil", found, err)
	}
}

func TestMemRepo_SeedAPIBalanceReflectedInWallet(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	repo.SeedAPIBalance(7, 250)

	w, err := repo.GetWallet(ctx, 7)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if w.APIBalance != 250 {
		t.Fatalf("APIBalance = %v, want 250", w.APIBalance)
	}
}

func TestMemRepo_GetWithdrawalNotFound(t *testing.T) {
	ctx := context.Background()
	if _, err := NewMemRepo().GetWithdrawal(ctx, 1); err == nil {
		t.Fatal("expected ErrWithdrawNotFound, got nil")
	} else {
		assertCode(t, err, CodeWithdrawNotFound)
	}
}

// TestMemRepo_GetWalletReturnsCopy 确认返回值是副本，外部改动不污染内部状态。
func TestMemRepo_GetWalletReturnsCopy(t *testing.T) {
	ctx := context.Background()
	repo := NewMemRepo()
	repo.SeedAPIBalance(1, 10)

	w1, _ := repo.GetWallet(ctx, 1)
	w1.APIBalance = 9999 // 改动副本

	w2, _ := repo.GetWallet(ctx, 1)
	if w2.APIBalance != 10 {
		t.Fatalf("internal wallet leaked: APIBalance = %v, want 10", w2.APIBalance)
	}
}
