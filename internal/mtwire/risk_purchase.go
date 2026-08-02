package mtwire

import (
	"context"

	"github.com/QuantumNous/new-api/internal/risk"
	"github.com/QuantumNous/new-api/internal/tokenplan"
)

// tokenplanRiskAdapter 把 tokenplan 的限购契约桥接到真实 risk.Engine：
// 翻译 PurchaseLimitCheck → risk.Plan，并把实名/设备维度经 context 注入
// （risk.WithPurchaseIdentity），供引擎做 Trial 三维去重（用户∪实名∪设备各 1 次）。
// 生产 wire 始终注入带 PurchaseLedger 的 Engine（DB 权威）；eng 为 nil 仅测试桩。
type tokenplanRiskAdapter struct {
	eng risk.RiskEngine
}

// 编译期断言：满足 tokenplan 的消费者接口。
var _ tokenplan.RiskEngine = tokenplanRiskAdapter{}

func (a tokenplanRiskAdapter) CheckPurchaseLimit(ctx context.Context, in tokenplan.PurchaseLimitCheck) error {
	if a.eng == nil {
		return nil // 测试桩未注入引擎 → 放行
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

// ReleasePurchaseClaim 归还本次已通过 CheckPurchaseLimit 的限购占用（补偿：下单/支付凭据创建
// 失败时调用）。否则 Trial 终身键（PurchaseDedupTTL=0）永久泄漏——用户没付款、订单没成，却再也
// 买不了 Trial（audit F2）。best-effort：释放失败仅经 SysLog 留痕，仍可经后台 ReleaseTrialLimit 兜底。
func (a tokenplanRiskAdapter) ReleasePurchaseClaim(ctx context.Context, in tokenplan.PurchaseLimitCheck) error {
	if a.eng == nil {
		return nil // 无风控引擎（Redis 关）→ 无键可释放
	}
	if in.PlanCode != "trial" {
		compensator, ok := a.eng.(risk.PurchaseLimitCompensator)
		if !ok || compensator == nil {
			return nil
		}
		return compensator.RollbackPurchaseLimit(ctx, in.PlanID, in.UserID)
	}
	admin, ok := a.eng.(risk.PurchaseLimitAdmin)
	if !ok || admin == nil {
		return nil
	}
	_, err := admin.ReleaseTrialLimit(ctx, in.UserID, risk.PurchaseIdentity{
		RealNameID: in.RealNameID,
		DeviceID:   in.DeviceID,
	}, false) // force=false：只删值==userID 的键，绝不误删他人/赢家占用
	return err
}
