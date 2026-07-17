package mtwire

import (
	"context"
	"math"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/alert"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/model"
)

// fakeAlerter 是 alert.AlertSink 的测试桩，记录被分发的告警供断言（命中软删用户入账时应发 Critical）。
type fakeAlerter struct{ alerts []alert.Alert }

func (f *fakeAlerter) Dispatch(_ context.Context, a alert.Alert) error {
	f.alerts = append(f.alerts, a)
	return nil
}

func TestRechargeQuota(t *testing.T) {
	per := int(common.QuotaPerUnit)
	cases := []struct {
		usd  float64
		want int
	}{
		{1, per},       // $1 = QuotaPerUnit
		{10, 10 * per}, // $10
		{0, 0},         // 零额
		{-5, 0},        // 负额防御
		{2.5, 5 * per / 2},
	}
	for _, c := range cases {
		if got := rechargeQuota(c.usd); got != c.want {
			t.Errorf("rechargeQuota(%g) = %d, want %d", c.usd, got, c.want)
		}
	}
}

func TestActualPaidCNY(t *testing.T) {
	if got := actualPaidCNY(10, 7.3); got != 73 {
		t.Fatalf("actualPaidCNY(10, 7.3) = %g, want 73", got)
	}
	if got := actualPaidCNY(1, 7.3); got != 7.3 {
		t.Fatalf("actualPaidCNY(1, 7.3) = %g, want 7.3", got)
	}
}

// TestResolveRechargeAmount 锁定充值金额口径归一 + 「所见即所付」：
// 人民币口径下实付精确到该¥（不由 usd×rate 反算引入浮点误差），usd=cny/rate；美元口径保持旧行为。
func TestResolveRechargeAmount(t *testing.T) {
	// 人民币口径：选 ¥100 → 实付正好 ¥100（所见即所付），usd = 100/7.3。
	usd, paid, cny := resolveRechargeAmount(0, 100, 7.3)
	if !cny {
		t.Fatal("amount_cny>0 应判为人民币口径")
	}
	if paid != 100 {
		t.Errorf("actualPaid = %v, want 100（实付精确到分，所见即所付）", paid)
	}
	if want := 100.0 / 7.3; math.Abs(usd-want) > 1e-9 {
		t.Errorf("usd = %v, want %v（cny/rate）", usd, want)
	}

	// 美元口径（旧）：usd 原样，实付 = usd×rate。
	usd2, paid2, cny2 := resolveRechargeAmount(10, 0, 7.3)
	if cny2 {
		t.Fatal("amount_cny=0 应判为美元口径")
	}
	if usd2 != 10 {
		t.Errorf("usd = %v, want 10", usd2)
	}
	if paid2 != 73 {
		t.Errorf("actualPaid = %v, want 73（10×7.3）", paid2)
	}

	// rate<=0 兜底为 1：人民币口径下 usd==cny==实付，不除零/不为负。
	usd3, paid3, _ := resolveRechargeAmount(0, 50, 0)
	if usd3 != 50 || paid3 != 50 {
		t.Errorf("rate<=0 兜底: usd=%v paid=%v, want 50/50", usd3, paid3)
	}
}

// rechargeTestUser 映射到 users 表的最简结构，供入账幂等测试更新额度，避免引入 model.User 的全部
// 字段/迁移复杂度。含 DeletedAt：model.User 是软删除模型，OnPaid 内 tx.Model(&model.User{}).Update
// 会带 WHERE deleted_at IS NULL，故最简表也需该列，否则 sqlite 报 "no such column: deleted_at"。
type rechargeTestUser struct {
	Id        int            `gorm:"primaryKey"`
	Quota     int            `gorm:"column:quota"`
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (rechargeTestUser) TableName() string { return "users" }

// TestRechargeQuotaSinkIdempotent 锁定审计 C1 双扣修复：OnPaid 对同一 order_no 重复调用
// （回调重推 / 对账 ReconcileStuckPaid 重跑 / 崩溃恢复）**只入账一次**——额度只加一次、台账只一条。
func TestRechargeQuotaSinkIdempotent(t *testing.T) {
	// OnPaid 成功后调 model.InvalidateUserCache；无 Redis 的单测环境下关闭缓存（否则 RedisDelKey 空指针）。
	// 生产为 RedisEnabled + 有效客户端，缓存失效正常；此处只验 DB 层入账幂等。
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&rechargeTestUser{}, &model.TopUp{}); err != nil {
		t.Fatalf("migrate users: %v", err)
	}
	if err := migrateRechargeLedger(db); err != nil {
		t.Fatalf("migrate ledger: %v", err)
	}
	if err := db.Create(&rechargeTestUser{Id: 42, Quota: 0}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	sink := rechargeQuotaSink{db: db}
	order := payment.PaidOrder{OrderNo: "RCG-idem", TenantID: 1, UserID: 42, AmountUSD: 10}
	want := rechargeQuota(10) // 10 × QuotaPerUnit

	// 连续 3 次入账（模拟回调重推 / 对账重跑 / 崩溃恢复）。
	for i := 1; i <= 3; i++ {
		if err := sink.OnPaid(context.Background(), order); err != nil {
			t.Fatalf("OnPaid #%d: %v", i, err)
		}
	}

	var u rechargeTestUser
	if err := db.First(&u, 42).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.Quota != want {
		t.Fatalf("quota = %d after 3× OnPaid, want %d (credited exactly once)", u.Quota, want)
	}
	var ledgerRows int64
	if err := db.Model(&rechargeCreditLedgerRow{}).Where("order_no = ?", order.OrderNo).Count(&ledgerRows).Error; err != nil {
		t.Fatalf("count ledger: %v", err)
	}
	if ledgerRows != 1 {
		t.Fatalf("ledger rows = %d, want 1 (idempotent)", ledgerRows)
	}
}

// TestRechargeQuotaSinkWritesTopUpRow 锁定「MT 充值账单历史不可见」的修复：微信/支付宝充值经
// OnPaid 首次入账时，需在同一事务内补写一条 status=success 的 model.TopUp，使
// GetUserTopUps（GET /api/user/topup/self，钱包「账单历史」弹窗的数据源）能看到这笔充值——
// 此前 OnPaid 只写台账 + users.quota，从不写 top_ups，充值成功但历史「查无此单」。
// 同时验证幂等：重复调用（回调重推 / 对账 ReconcileStuckPaid 重跑）不得写出第二条 TopUp。
func TestRechargeQuotaSinkWritesTopUpRow(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&rechargeTestUser{}, &model.TopUp{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := migrateRechargeLedger(db); err != nil {
		t.Fatalf("migrate ledger: %v", err)
	}
	if err := db.Create(&rechargeTestUser{Id: 77, Quota: 0}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}

	sink := rechargeQuotaSink{db: db}
	order := payment.PaidOrder{
		OrderNo:    "RCG-topup-visible",
		TenantID:   1,
		UserID:     77,
		Provider:   payment.ProviderWxpay,
		AmountUSD:  10,
		ActualPaid: 73, // 10 × 7.3 示例汇率，用户实付¥
	}

	if err := sink.OnPaid(context.Background(), order); err != nil {
		t.Fatalf("OnPaid #1: %v", err)
	}

	var rows []model.TopUp
	if err := db.Where("trade_no = ?", order.OrderNo).Find(&rows).Error; err != nil {
		t.Fatalf("query top_ups: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("top_ups rows for %s = %d, want 1 (OnPaid should write a completed TopUp row)", order.OrderNo, len(rows))
	}
	got := rows[0]
	if got.UserId != int(order.UserID) {
		t.Errorf("UserId = %d, want %d", got.UserId, order.UserID)
	}
	if got.Status != common.TopUpStatusSuccess {
		t.Errorf("Status = %q, want %q", got.Status, common.TopUpStatusSuccess)
	}
	if got.Amount != 10 {
		t.Errorf("Amount = %d, want 10 (USD, matches o.AmountUSD)", got.Amount)
	}
	if got.Money != order.ActualPaid {
		t.Errorf("Money = %v, want %v (¥ actually paid, matches o.ActualPaid)", got.Money, order.ActualPaid)
	}
	if got.PaymentMethod != "wxpay_official" {
		t.Errorf("PaymentMethod = %q, want %q", got.PaymentMethod, "wxpay_official")
	}
	if got.PaymentProvider != "wxpay" {
		t.Errorf("PaymentProvider = %q, want %q", got.PaymentProvider, "wxpay")
	}
	if got.CreateTime == 0 || got.CompleteTime == 0 {
		t.Errorf("CreateTime/CompleteTime should be set, got create=%d complete=%d", got.CreateTime, got.CompleteTime)
	}

	// 重复入账（模拟回调重推 / 对账重跑）：台账幂等短路，不应再写第二条 TopUp。
	for i := 0; i < 2; i++ {
		if err := sink.OnPaid(context.Background(), order); err != nil {
			t.Fatalf("OnPaid retry #%d: %v", i, err)
		}
	}
	var count int64
	if err := db.Model(&model.TopUp{}).Where("trade_no = ?", order.OrderNo).Count(&count).Error; err != nil {
		t.Fatalf("count top_ups: %v", err)
	}
	if count != 1 {
		t.Fatalf("top_ups rows after retries = %d, want 1 (idempotent, no duplicate write)", count)
	}
}

// TestRechargeQuotaSinkSoftDeletedUserStillCredited 锁定审计 #11 修复的核心：用户在回调落地前被
// 管理员/风控**软删**（users.deleted_at 置位），充值入账**必须**仍把额度落到该用户行（钱不丢、可恢复），
// 而非旧行为——tx.Model(&User{}) 的软删作用域使 UPDATE 匹配 0 行、静默提交、订单推进 credited、永久丢账。
// 同时验证：命中软删用户必经 AlertSink 发一条 Critical 告警知会风控；且整条链幂等（重跑不双扣、不重复告警）。
func TestRechargeQuotaSinkSoftDeletedUserStillCredited(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&rechargeTestUser{}, &model.TopUp{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := migrateRechargeLedger(db); err != nil {
		t.Fatalf("migrate ledger: %v", err)
	}
	if err := db.Create(&rechargeTestUser{Id: 42, Quota: 0}).Error; err != nil {
		t.Fatalf("seed user: %v", err)
	}
	// 关键：回调落地前用户被软删。GORM 软删置 deleted_at → 作用域内 UPDATE 匹配 0 行（正是丢账触发条件）。
	if err := db.Delete(&rechargeTestUser{}, 42).Error; err != nil {
		t.Fatalf("soft-delete user: %v", err)
	}

	al := &fakeAlerter{}
	sink := rechargeQuotaSink{db: db, alerter: al}
	order := payment.PaidOrder{OrderNo: "RCG-softdel", TenantID: 1, UserID: 42, Provider: payment.ProviderWxpay, AmountUSD: 10, ActualPaid: 73}
	want := rechargeQuota(10)

	if err := sink.OnPaid(context.Background(), order); err != nil {
		t.Fatalf("OnPaid on soft-deleted user should credit (not error), got: %v", err)
	}

	// 额度已 Unscoped 落到软删用户行（钱不丢、可恢复）——需 Unscoped 才读得到软删行。
	var u rechargeTestUser
	if err := db.Unscoped().First(&u, 42).Error; err != nil {
		t.Fatalf("reload (unscoped) user: %v", err)
	}
	if u.Quota != want {
		t.Fatalf("soft-deleted user quota = %d, want %d (credited on their row, not silently dropped)", u.Quota, want)
	}
	// 台账 1 条（幂等键就位）+ TopUp 1 条（账单历史可见）。
	var ledgerRows, topupRows int64
	db.Model(&rechargeCreditLedgerRow{}).Where("order_no = ?", order.OrderNo).Count(&ledgerRows)
	db.Model(&model.TopUp{}).Where("trade_no = ?", order.OrderNo).Count(&topupRows)
	if ledgerRows != 1 || topupRows != 1 {
		t.Fatalf("ledger=%d topup=%d, want 1/1 (credited + auditable)", ledgerRows, topupRows)
	}
	// 风控告警：命中软删用户必发一条 Critical。
	if len(al.alerts) != 1 || al.alerts[0].Level != alert.LevelCritical {
		t.Fatalf("alerts = %+v, want exactly 1 Critical (risk notified)", al.alerts)
	}

	// 幂等：重复入账（回调重推 / 对账重跑）不再加额度、不再重复告警（台账短路，credited 保持 false）。
	for i := 0; i < 2; i++ {
		if err := sink.OnPaid(context.Background(), order); err != nil {
			t.Fatalf("OnPaid retry #%d: %v", i, err)
		}
	}
	if err := db.Unscoped().First(&u, 42).Error; err != nil {
		t.Fatalf("reload user: %v", err)
	}
	if u.Quota != want {
		t.Fatalf("quota after retries = %d, want %d (credited exactly once)", u.Quota, want)
	}
	if len(al.alerts) != 1 {
		t.Fatalf("alerts after retries = %d, want 1 (idempotent, no re-alert)", len(al.alerts))
	}
}

// TestRechargeQuotaSinkMissingUser 锁定「目标用户行真的不存在」的 fail-loud：连 Unscoped 都 0 行匹配时
// 必须返回 errRechargeUserMissing 使整个事务回滚（台账 / quota / TopUp 一并撤销），订单不推进 credited，
// 由回调重推 + 对账兜底 + 告警接手——绝不静默把这笔钱吞进「已入账」的假象。
func TestRechargeQuotaSinkMissingUser(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{TranslateError: true})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.AutoMigrate(&rechargeTestUser{}, &model.TopUp{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := migrateRechargeLedger(db); err != nil {
		t.Fatalf("migrate ledger: %v", err)
	}
	// 不 seed 任何用户：UserID=999 行根本不存在。

	sink := rechargeQuotaSink{db: db}
	order := payment.PaidOrder{OrderNo: "RCG-ghost", TenantID: 1, UserID: 999, Provider: payment.ProviderWxpay, AmountUSD: 10, ActualPaid: 73}

	if err := sink.OnPaid(context.Background(), order); err != errRechargeUserMissing {
		t.Fatalf("OnPaid for missing user = %v, want errRechargeUserMissing (fail-loud, not silent credit)", err)
	}
	// 整事务回滚：台账与 TopUp 均未落库 → 订单不推进 credited、回调 ack 非 SUCCESS → 平台重推/对账兜底。
	var ledgerRows, topupRows int64
	db.Model(&rechargeCreditLedgerRow{}).Where("order_no = ?", order.OrderNo).Count(&ledgerRows)
	db.Model(&model.TopUp{}).Where("trade_no = ?", order.OrderNo).Count(&topupRows)
	if ledgerRows != 0 || topupRows != 0 {
		t.Fatalf("ledger=%d topup=%d after missing-user credit, want 0/0 (rolled back, no phantom credit)", ledgerRows, topupRows)
	}
}
