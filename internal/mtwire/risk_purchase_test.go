package mtwire

import (
	"context"
	"errors"
	"testing"

	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// newTrialRiskEngine 构造一个真实 risk.Engine（内存 KV，Trial 终身去重），
// 供适配器端到端测试——不 mock 被测逻辑，直接验证限购行为。
func newTrialRiskEngine() risk.RiskEngine {
	return risk.NewEngine(risk.NewMemKVCache(nil))
}

// Trial 同一用户二次购买应被拒（用户维去重）。
func TestTokenplanRiskAdapter_TrialSameUser_SecondRejected(t *testing.T) {
	a := tokenplanRiskAdapter{eng: newTrialRiskEngine()}
	check := tokenplan.PurchaseLimitCheck{UserID: 1001, PlanID: 1, PlanCode: "trial"}

	if err := a.CheckPurchaseLimit(context.Background(), check); err != nil {
		t.Fatalf("首次购买 Trial 应放行，得 %v", err)
	}
	err := a.CheckPurchaseLimit(context.Background(), check)
	if !errors.Is(err, risk.ErrPurchaseLimitExceeded) {
		t.Fatalf("同用户二次购买 Trial 应 PURCHASE_LIMIT_EXCEEDED，得 %v", err)
	}
}

// Trial 不同用户但同设备指纹应被拒（设备维去重，证明 WithPurchaseIdentity 已注入）。
func TestTokenplanRiskAdapter_TrialSameDevice_DifferentUser_Rejected(t *testing.T) {
	a := tokenplanRiskAdapter{eng: newTrialRiskEngine()}
	first := tokenplan.PurchaseLimitCheck{UserID: 2001, PlanID: 1, PlanCode: "trial", DeviceID: "dev-XYZ"}
	second := tokenplan.PurchaseLimitCheck{UserID: 2002, PlanID: 1, PlanCode: "trial", DeviceID: "dev-XYZ"}

	if err := a.CheckPurchaseLimit(context.Background(), first); err != nil {
		t.Fatalf("首个用户购买 Trial 应放行，得 %v", err)
	}
	err := a.CheckPurchaseLimit(context.Background(), second)
	if !errors.Is(err, risk.ErrPurchaseLimitExceeded) {
		t.Fatalf("同设备换用户购买 Trial 应被拒（设备维），得 %v", err)
	}
}

// 非 Trial 档不限购：同一用户可重复购买。
func TestTokenplanRiskAdapter_NonTrial_NotLimited(t *testing.T) {
	a := tokenplanRiskAdapter{eng: newTrialRiskEngine()}
	check := tokenplan.PurchaseLimitCheck{UserID: 3001, PlanID: 2, PlanCode: "mini"}

	for i := 0; i < 3; i++ {
		if err := a.CheckPurchaseLimit(context.Background(), check); err != nil {
			t.Fatalf("非 Trial 档第 %d 次购买应放行，得 %v", i+1, err)
		}
	}
}

// 引擎为 nil（Redis 关闭时的装配）应优雅放行，不回归。
func TestTokenplanRiskAdapter_NilEngine_Allows(t *testing.T) {
	a := tokenplanRiskAdapter{eng: nil}
	check := tokenplan.PurchaseLimitCheck{UserID: 4001, PlanID: 1, PlanCode: "trial"}

	if err := a.CheckPurchaseLimit(context.Background(), check); err != nil {
		t.Fatalf("nil 引擎应放行，得 %v", err)
	}
}
