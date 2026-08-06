package mtwire

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/internal/platform/apperr"
	"github.com/QuantumNous/new-api/model"
)

// openIsolatedSQLite 每测独立文件库 + 多连接，禁止固定 shared memory 名导致 DDL 冲突。
func openIsolatedSQLite(t *testing.T) *gorm.DB {
	t.Helper()
	path := filepath.Join(t.TempDir(), "pay.db")
	// busy_timeout 提升并发稳定性；cache=private 避免跨测污染
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(8)
	sqlDB.SetMaxIdleConns(4)
	return db
}

func migrateRechargeTestDB(t *testing.T, db *gorm.DB) {
	t.Helper()
	require.NoError(t, db.AutoMigrate(&rechargeTestUser{}, &model.TopUp{}))
	require.NoError(t, migrateRechargeLedger(db))
	require.NoError(t, migrateCacheInvalidationOutbox(db))
}

// TestRechargeOnPaidConcurrentOnce 32 并发 OnPaid 只加一次 quota。
func TestRechargeOnPaidConcurrentOnce(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db := openIsolatedSQLite(t)
	migrateRechargeTestDB(t, db)
	require.NoError(t, db.Create(&rechargeTestUser{Id: 42, Quota: 0}).Error)

	sink := rechargeQuotaSink{db: db}
	order := payment.PaidOrder{
		OrderNo: "RCG-conc-32", RootOrderNo: "RCG-conc-32",
		TenantID: 1, UserID: 42, AmountUSD: 10.5, ActualPaid: 76.65, ActualPaidFen: 7665,
		Provider: payment.ProviderWxpay,
	}
	want := rechargeQuota(10.5)

	const n = 32
	var wg sync.WaitGroup
	var okCount atomic.Int32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := sink.OnPaid(context.Background(), order); err == nil {
				okCount.Add(1)
			}
		}()
	}
	wg.Wait()
	assert.Equal(t, int32(n), okCount.Load(), "all calls should succeed (idempotent)")

	var u rechargeTestUser
	require.NoError(t, db.First(&u, 42).Error)
	assert.Equal(t, want, u.Quota, "quota credited exactly once")

	var ledgerN, topN, outN int64
	require.NoError(t, db.Model(&rechargeCreditLedgerRow{}).Where("order_no = ?", order.OrderNo).Count(&ledgerN).Error)
	require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", order.OrderNo).Count(&topN).Error)
	require.NoError(t, db.Model(&cacheInvalidationOutboxRow{}).Where("order_no = ?", order.OrderNo).Count(&outN).Error)
	assert.Equal(t, int64(1), ledgerN)
	assert.Equal(t, int64(1), topN)
	assert.Equal(t, int64(1), outN)
}

// TestRechargeLedgerInvariantMismatch 同 order_no 不同意图拒绝。
func TestRechargeLedgerInvariantMismatch(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db := openIsolatedSQLite(t)
	migrateRechargeTestDB(t, db)
	require.NoError(t, db.Create(&rechargeTestUser{Id: 7, Quota: 0}).Error)

	sink := rechargeQuotaSink{db: db}
	o1 := payment.PaidOrder{
		OrderNo: "RCG-inv", RootOrderNo: "RCG-inv", TenantID: 1, UserID: 7,
		AmountUSD: 10, ActualPaid: 73, ActualPaidFen: 7300, Provider: payment.ProviderWxpay,
	}
	require.NoError(t, sink.OnPaid(context.Background(), o1))

	o2 := payment.PaidOrder{
		OrderNo: "RCG-inv", RootOrderNo: "RCG-inv", TenantID: 1, UserID: 7,
		AmountUSD: 20, ActualPaid: 146, ActualPaidFen: 14600, Provider: payment.ProviderWxpay,
	}
	err := sink.OnPaid(context.Background(), o2)
	require.Error(t, err)
	assert.Equal(t, "RECHARGE_LEDGER_INVARIANT", apperr.CodeOf(err))
}

// TestRechargeNonIntegerUSDCrashReplay sink 后 MarkCredited 前崩溃重放：只加一次 quota。
func TestRechargeNonIntegerUSDCrashReplay(t *testing.T) {
	prevRedis := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prevRedis })

	db := openIsolatedSQLite(t)
	migrateRechargeTestDB(t, db)
	require.NoError(t, db.Create(&rechargeTestUser{Id: 99, Quota: 0}).Error)

	sink := rechargeQuotaSink{db: db}
	// 非整数 USD：10.25 → 易触发 float 比较问题
	o := payment.PaidOrder{
		OrderNo: "RCG-frac", RootOrderNo: "RCG-frac", TenantID: 1, UserID: 99,
		AmountUSD: 10.25, ActualPaid: 74.825, ActualPaidFen: 7483, Provider: payment.ProviderWxpay,
	}
	want := rechargeQuota(10.25)
	require.NoError(t, sink.OnPaid(context.Background(), o))
	// 模拟 MarkCredited 前崩溃后重放
	require.NoError(t, sink.OnPaid(context.Background(), o))
	require.NoError(t, sink.OnPaid(context.Background(), o))

	var u rechargeTestUser
	require.NoError(t, db.First(&u, 99).Error)
	assert.Equal(t, want, u.Quota)
}
