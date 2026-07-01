package mtwire

import (
	"context"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
)

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
	if err := db.AutoMigrate(&rechargeTestUser{}); err != nil {
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
