package mtwire

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
)

// TestUsdToQuotaRound 锁定统一「美元→原生 quota」换算核心的取整语义与精度。
// 这是充值(截断)、订阅(进位)、分销(截断)三处换算收敛后的唯一实现，必须保证：
//   - roundDown 截断、roundUp 进位、非正额度一律归零；
//   - 内部走 decimal 精确乘法（非 float64）——杜绝 int(0.29×QuotaPerUnit) 这类浮点误差少算 1 quota。
func TestUsdToQuotaRound(t *testing.T) {
	per := int64(common.QuotaPerUnit) // 500000

	// 非正额度：两种取整都归零。
	for _, r := range []rounding{roundDown, roundUp} {
		if got := usdToQuotaRound(0, r); got != 0 {
			t.Errorf("usdToQuotaRound(0, %v) = %d, want 0", r, got)
		}
		if got := usdToQuotaRound(-5, r); got != 0 {
			t.Errorf("usdToQuotaRound(-5, %v) = %d, want 0", r, got)
		}
	}

	// 整额：两种取整一致，等于 usd×per。
	if got := usdToQuotaRound(1, roundDown); got != per {
		t.Errorf("usdToQuotaRound(1, roundDown) = %d, want %d", got, per)
	}
	if got := usdToQuotaRound(20, roundUp); got != 20*per {
		t.Errorf("usdToQuotaRound(20, roundUp) = %d, want %d", got, 20*per)
	}

	// 精确性：0.29 × 500000 = 145000（恰整数）。老的 int(0.29*500000) 因 float64 误差得 144999——
	// 核心必须用 decimal 精确乘法拿到 145000，否则该断言失败即暴露又退回了浮点乘法。
	if got := usdToQuotaRound(0.29, roundDown); got != 145000 {
		t.Errorf("usdToQuotaRound(0.29, roundDown) = %d, want 145000 (精确 decimal，非浮点截断的 144999)", got)
	}

	// 取整方向：0.000001 × 500000 = 0.5 → 截断=0、进位=1。
	const half = 0.000001
	if got := usdToQuotaRound(half, roundDown); got != 0 {
		t.Errorf("usdToQuotaRound(%g, roundDown) = %d, want 0 (截断 0.5→0)", half, got)
	}
	if got := usdToQuotaRound(half, roundUp); got != 1 {
		t.Errorf("usdToQuotaRound(%g, roundUp) = %d, want 1 (进位 0.5→1)", half, got)
	}
}
