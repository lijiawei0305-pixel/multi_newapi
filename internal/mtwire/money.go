package mtwire

// 货币换算收敛点：所有「美元额 → new-api 原生 quota 单位」的换算都必须经由本文件的
// usdToQuotaRound，杜绝散落在 recharge/subscription/distribution 各处、口径(QuotaPerUnit)
// 与取整策略各自为政、改一漏二的维护发散。各域按语义显式声明取整方向（见文件末尾三个 adapter）。

import (
	"github.com/QuantumNous/new-api/common"
	"github.com/shopspring/decimal"
)

// rounding 声明美元→quota 的取整方向。金额换算不接受隐式取整——调用方必须显式选一个。
type rounding int

const (
	roundDown rounding = iota // 截断（向零取整）：充值、分销建码/兑换用；同一函数保证预扣与入账严格守恒。
	roundUp                   // 进位（向上取整）：订阅额度上限用，对齐原生 calcSubscriptionBalanceQuota 的 decimal.Ceil。
)

// usdToQuotaRound 把美元额精确折算为 new-api 内部 quota 单位（$1 = common.QuotaPerUnit）。
// 唯一实现：走 decimal 精确乘法（非 float64，避免 0.29×QuotaPerUnit 这类浮点误差少算 1 quota），
// 按 r 取整。非正额度返回 0。
func usdToQuotaRound(usd float64, r rounding) int64 {
	if usd <= 0 {
		return 0
	}
	q := decimal.NewFromFloat(usd).Mul(decimal.NewFromFloat(common.QuotaPerUnit))
	if r == roundUp {
		q = q.Ceil()
	} else {
		q = q.Truncate(0)
	}
	return q.IntPart()
}
