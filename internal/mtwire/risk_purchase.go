package mtwire

import (
	"context"

	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// tokenplanRiskAdapter 把 tokenplan 的限购契约桥接到真实 risk.Engine：
// 翻译 PurchaseLimitCheck → risk.Plan，并把实名/设备维度经 context 注入
// （risk.WithPurchaseIdentity），供引擎做 Trial 三维去重（用户∪实名∪设备各 1 次）。
// eng 为 nil（Redis 关闭、未装配风控）时放行，保持无风控时的既有行为、不回归。
type tokenplanRiskAdapter struct {
	eng risk.RiskEngine
}

// 编译期断言：满足 tokenplan 的消费者接口。
var _ tokenplan.RiskEngine = tokenplanRiskAdapter{}

func (a tokenplanRiskAdapter) CheckPurchaseLimit(ctx context.Context, in tokenplan.PurchaseLimitCheck) error {
	if a.eng == nil {
		return nil // 无风控引擎（Redis 关）→ 放行，不回归
	}
	// 实名/设备维度经请求级 context 传入（引擎 CheckPurchaseLimit 签名仅含 userID）。
	ctx = risk.WithPurchaseIdentity(ctx, risk.PurchaseIdentity{
		RealNameID: in.RealNameID,
		DeviceID:   in.DeviceID,
	})
	return a.eng.CheckPurchaseLimit(ctx, in.UserID, risk.Plan{
		ID:   in.PlanID,
		Code: in.PlanCode,
		// Trial 由 code 判定（Plan 无独立布尔，见 tokenplan/model.go 约定）；
		// 非 Trial 档 PerUserLimit=0 表示不限购（现口径，proposal §2.4）。
		Trial: in.PlanCode == "trial",
	})
}
