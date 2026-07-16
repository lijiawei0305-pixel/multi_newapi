package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// 本文件覆盖**生产 /v1 订阅计费真正走的**原生计量路径：
//   PreConsumeUserSubscription / PostConsumeUserSubscriptionDelta / RefundSubscriptionPreConsume。
// 这是 internal/tokenplan/gormrepo.Meter（未装配的死代码，写恒零死列 used_usd）在生产中的
// 真实替身——真实用量记在 user_subscriptions.amount_used（int64 quota 单位）。
// 全程 int64 精确算术（无 decimal/float 参与），故不存在「恰好用满」的浮点舍入问题；
// 这些断言把「耗尽/未过期恰好用满/已满/无订阅/过期/负数/幂等/多订阅择桶」逐一钉死在生产算法上。

// meterEnsure 确保幂等预扣台账表存在（TestMain 的 AutoMigrate 列表未含它）。
func meterEnsure(t *testing.T) {
	t.Helper()
	require.NoError(t, DB.AutoMigrate(&SubscriptionPreConsumeRecord{}))
	t.Cleanup(func() {
		DB.Exec("DELETE FROM subscription_pre_consume_records")
		DB.Exec("DELETE FROM user_subscriptions")
		DB.Exec("DELETE FROM subscription_plans")
		for id := 1; id <= 64; id++ {
			InvalidateSubscriptionPlanCache(id)
		}
	})
}

// meterPlan 落一个套餐（total=0 即不限量），并清缓存保证按 id 回读到最新。
func meterPlan(t *testing.T, planID int, total int64, resetPeriod string) {
	t.Helper()
	if resetPeriod == "" {
		resetPeriod = SubscriptionResetNever
	}
	require.NoError(t, DB.Create(&SubscriptionPlan{
		Id:               planID,
		Title:            "meter-test",
		TotalAmount:      total,
		QuotaResetPeriod: resetPeriod,
		DurationUnit:     "month",
		DurationValue:    1,
		Enabled:          true,
	}).Error)
	InvalidateSubscriptionPlanCache(planID)
}

// meterSub 落一条订阅；endOffsetSec 相对 now（正=未来 active，负=已过期）。
func meterSub(t *testing.T, subID, planID, userID int, total, used int64, endOffsetSec int64) {
	t.Helper()
	now := GetDBTimestamp()
	require.NoError(t, DB.Create(&UserSubscription{
		Id:            subID,
		UserId:        userID,
		PlanId:        planID,
		AmountTotal:   total,
		AmountUsed:    used,
		StartTime:     now - 3600,
		EndTime:       now + endOffsetSec,
		Status:        "active",
		Source:        "order",
		LastResetTime: now - 3600,
		NextResetTime: 0,
	}).Error)
}

func meterUsed(t *testing.T, subID int) int64 {
	t.Helper()
	var sub UserSubscription
	require.NoError(t, DB.Where("id = ?", subID).First(&sub).Error)
	return sub.AmountUsed
}

// ---- PreConsumeUserSubscription ----

// 恰好用满：remain==amount 必须放行（used 精确落到 total），验证 `remain < amount` 的边界方向。
func TestPreConsume_ExactlyFull_Allowed(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 90, 86400)

	res, err := PreConsumeUserSubscription("req-exact", 100, "gpt-4o", 0, 10)
	require.NoError(t, err)
	assert.Equal(t, int64(10), res.PreConsumed)
	assert.Equal(t, int64(90), res.AmountUsedBefore)
	assert.Equal(t, int64(100), res.AmountUsedAfter)
	assert.Equal(t, int64(100), meterUsed(t, 1))
}

// 差一个单位即拒（耗尽），返回 "subscription quota insufficient"；used 不动。
func TestPreConsume_OverByOne_Insufficient(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 95, 86400)

	_, err := PreConsumeUserSubscription("req-over", 100, "gpt-4o", 0, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")
	assert.Equal(t, int64(95), meterUsed(t, 1))
}

// 已满（used==total）再来一单位即拒。
func TestPreConsume_AlreadyExhausted(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 100, 86400)

	_, err := PreConsumeUserSubscription("req-full", 100, "gpt-4o", 0, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "insufficient")
}

// 不限量套餐（total=0）：跳过额度门，任意消费放行。
func TestPreConsume_Unlimited_Allowed(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 0, "")
	meterSub(t, 1, 1, 100, 0, 999999, 86400)

	res, err := PreConsumeUserSubscription("req-unl", 100, "gpt-4o", 0, 5000)
	require.NoError(t, err)
	assert.Equal(t, int64(5000), res.PreConsumed)
	assert.Equal(t, int64(999999+5000), meterUsed(t, 1))
}

// 已过期订阅（end_time<=now）不被选中 → "no active subscription"（原生路径无独立 EXPIRED 码）。
func TestPreConsume_Expired_NoActive(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 0, -100) // 已过期

	_, err := PreConsumeUserSubscription("req-exp", 100, "gpt-4o", 0, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active subscription")
}

// 无任何订阅 → "no active subscription"。
func TestPreConsume_NoSubscription(t *testing.T) {
	meterEnsure(t)
	_, err := PreConsumeUserSubscription("req-none", 100, "gpt-4o", 0, 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active subscription")
}

// 参数校验：amount<=0 / userId<=0 / requestId 空。
func TestPreConsume_InvalidArgs(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 0, 86400)

	_, err := PreConsumeUserSubscription("req-neg", 100, "gpt-4o", 0, -5)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "amount")

	_, err = PreConsumeUserSubscription("req-zero", 100, "gpt-4o", 0, 0)
	require.Error(t, err)

	_, err = PreConsumeUserSubscription("req-uid", 0, "gpt-4o", 0, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "userId")

	_, err = PreConsumeUserSubscription("  ", 100, "gpt-4o", 0, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "requestId")
}

// 幂等：同 requestId 重放不重复扣，used 只前进一次。
func TestPreConsume_Idempotent(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 10, 86400)

	res1, err := PreConsumeUserSubscription("req-idem", 100, "gpt-4o", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, int64(30), meterUsed(t, 1))

	res2, err := PreConsumeUserSubscription("req-idem", 100, "gpt-4o", 0, 20)
	require.NoError(t, err)
	assert.Equal(t, res1.PreConsumed, res2.PreConsumed)
	assert.Equal(t, int64(30), meterUsed(t, 1), "重放不得二次扣减")
}

// 多订阅择桶：耗尽的（end 更早）跳过，选下一个有余量的。
func TestPreConsume_MultiSub_SkipsExhausted(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 100, 3600)  // 已满，end 更早（先被排序命中）
	meterSub(t, 2, 1, 100, 100, 0, 86400)   // 有余量，end 更晚

	res, err := PreConsumeUserSubscription("req-multi", 100, "gpt-4o", 0, 30)
	require.NoError(t, err)
	assert.Equal(t, 2, res.UserSubscriptionId, "应跳过已满订阅选下一个")
	assert.Equal(t, int64(100), meterUsed(t, 1), "已满订阅不受影响")
	assert.Equal(t, int64(30), meterUsed(t, 2))
}

// ---- PostConsumeUserSubscriptionDelta ----

func TestPostConsume_PositiveWithinTotal(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 50, 86400)

	require.NoError(t, PostConsumeUserSubscriptionDelta(1, 30))
	assert.Equal(t, int64(80), meterUsed(t, 1))
}

// 负 delta（退款）夹到 0，不会为负。
func TestPostConsume_NegativeClampsZero(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 10, 86400)

	require.NoError(t, PostConsumeUserSubscriptionDelta(1, -50))
	assert.Equal(t, int64(0), meterUsed(t, 1))
}

// 结算 delta 冲破 total（AmountTotal>0）→ 报错且不落库（防止 amount_used 越顶）。
func TestPostConsume_OverTotalRejected(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 90, 86400)

	err := PostConsumeUserSubscriptionDelta(1, 20)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeds total")
	assert.Equal(t, int64(90), meterUsed(t, 1), "越顶结算不得落库")
}

// 不限量（total=0）无天花板，任意正 delta 落库。
func TestPostConsume_UnlimitedNoCeiling(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 0, "")
	meterSub(t, 1, 1, 100, 0, 100, 86400)

	require.NoError(t, PostConsumeUserSubscriptionDelta(1, 9999))
	assert.Equal(t, int64(100+9999), meterUsed(t, 1))
}

func TestPostConsume_ZeroAndInvalid(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 40, 86400)

	require.NoError(t, PostConsumeUserSubscriptionDelta(1, 0)) // no-op
	assert.Equal(t, int64(40), meterUsed(t, 1))

	err := PostConsumeUserSubscriptionDelta(0, 10)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid")
}

// ---- RefundSubscriptionPreConsume ----

// 预扣后退款：used 回到原值；重复退款幂等（不多退）。
func TestRefundPreConsume_Idempotent(t *testing.T) {
	meterEnsure(t)
	meterPlan(t, 1, 100, "")
	meterSub(t, 1, 1, 100, 100, 10, 86400)

	_, err := PreConsumeUserSubscription("req-refund", 100, "gpt-4o", 0, 30)
	require.NoError(t, err)
	assert.Equal(t, int64(40), meterUsed(t, 1))

	require.NoError(t, RefundSubscriptionPreConsume("req-refund"))
	assert.Equal(t, int64(10), meterUsed(t, 1), "退款应回到预扣前")

	require.NoError(t, RefundSubscriptionPreConsume("req-refund"))
	assert.Equal(t, int64(10), meterUsed(t, 1), "重复退款不得多退")
}
