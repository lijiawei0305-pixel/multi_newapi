package mtwire

// 支付真库 opt-in harness（发布门禁须显式提供 DSN 并 executed）。
//
//	TEST_PAYMENT_DB_ALLOW_DROP=1
//	PAYMENT_MYSQL_DSN=.../pay_dialect_test?...
//	PAYMENT_MYSQL_CFR_DSN=...clientFoundRows=true...
//	PAYMENT_PG_DSN=...

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/payment"
	"github.com/QuantumNous/new-api/model"
)

func requirePaymentExternalAllow(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_PAYMENT_DB_ALLOW_DROP") != "1" {
		t.Skip("TEST_PAYMENT_DB_ALLOW_DROP=1 required")
	}
}

func assertDSNIsTestDB(t *testing.T, dsn string) {
	t.Helper()
	if !strings.Contains(dsn, "_test") && !strings.Contains(dsn, "test") {
		t.Fatalf("DSN must target a *test* database, got redacted length=%d", len(dsn))
	}
}

func openDialect(t *testing.T, kind, dsn string) *gorm.DB {
	t.Helper()
	var dialector gorm.Dialector
	switch kind {
	case "mysql":
		dialector = mysql.Open(dsn)
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		t.Fatalf("unknown dialect %s", kind)
	}
	db, err := gorm.Open(dialector, &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(16)
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db
}

func setupPaymentFundTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	// drop only our tables
	_ = db.Migrator().DropTable(&rechargeCreditLedgerRow{}, &cacheInvalidationOutboxRow{}, &model.TopUp{}, &rechargeTestUser{})
	require.NoError(t, db.AutoMigrate(&rechargeTestUser{}, &model.TopUp{}))
	require.NoError(t, migrateRechargeLedger(db))
	require.NoError(t, migrateCacheInvalidationOutbox(db))
}

func runConcurrentOnPaidOnce(t *testing.T, db *gorm.DB) {
	t.Helper()
	prev := common.RedisEnabled
	common.RedisEnabled = false
	t.Cleanup(func() { common.RedisEnabled = prev })

	require.NoError(t, db.Create(&rechargeTestUser{Id: 1001, Quota: 0}).Error)
	sink := rechargeQuotaSink{db: db}
	o := payment.PaidOrder{
		OrderNo: "RCG-ext-conc", RootOrderNo: "RCG-ext-conc",
		TenantID: 1, UserID: 1001, AmountUSD: 10.25, ActualPaid: 74.825, ActualPaidFen: 7483,
		Provider: payment.ProviderWxpay,
	}
	want := rechargeQuota(10.25)
	const n = 32
	var wg sync.WaitGroup
	var okN atomic.Int32
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			if err := sink.OnPaid(context.Background(), o); err == nil {
				okN.Add(1)
			}
		}()
	}
	wg.Wait()
	require.Equal(t, int32(n), okN.Load())
	var u rechargeTestUser
	require.NoError(t, db.First(&u, 1001).Error)
	assert.Equal(t, want, u.Quota)
	var ln, tn int64
	require.NoError(t, db.Model(&rechargeCreditLedgerRow{}).Where("order_no = ?", o.OrderNo).Count(&ln).Error)
	require.NoError(t, db.Model(&model.TopUp{}).Where("trade_no = ?", o.OrderNo).Count(&tn).Error)
	assert.Equal(t, int64(1), ln)
	assert.Equal(t, int64(1), tn)
}

func TestPaymentExternalDialectSQLiteFile(t *testing.T) {
	// 始终执行：多连接文件库
	path := filepath.Join(t.TempDir(), "pay_ext.db")
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", path)
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{TranslateError: true})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(16)
	setupPaymentFundTables(t, db)
	runConcurrentOnPaidOnce(t, db)
}

func TestPaymentExternalDialectMySQL(t *testing.T) {
	requirePaymentExternalAllow(t)
	dsn := os.Getenv("PAYMENT_MYSQL_DSN")
	if dsn == "" {
		t.Skip("PAYMENT_MYSQL_DSN not set")
	}
	assertDSNIsTestDB(t, dsn)
	db := openDialect(t, "mysql", dsn)
	setupPaymentFundTables(t, db)
	runConcurrentOnPaidOnce(t, db)
}

func TestPaymentExternalDialectMySQLClientFoundRows(t *testing.T) {
	requirePaymentExternalAllow(t)
	dsn := os.Getenv("PAYMENT_MYSQL_CFR_DSN")
	if dsn == "" {
		// allow reusing PAYMENT_MYSQL_DSN with clientFoundRows
		dsn = os.Getenv("PAYMENT_MYSQL_DSN")
		if dsn == "" {
			t.Skip("PAYMENT_MYSQL_CFR_DSN not set")
		}
		if !strings.Contains(dsn, "clientFoundRows") {
			if strings.Contains(dsn, "?") {
				dsn += "&clientFoundRows=true"
			} else {
				dsn += "?clientFoundRows=true"
			}
		}
	}
	assertDSNIsTestDB(t, dsn)
	db := openDialect(t, "mysql", dsn)
	setupPaymentFundTables(t, db)
	runConcurrentOnPaidOnce(t, db)
}

func TestPaymentExternalDialectPostgres(t *testing.T) {
	requirePaymentExternalAllow(t)
	dsn := os.Getenv("PAYMENT_PG_DSN")
	if dsn == "" {
		t.Skip("PAYMENT_PG_DSN not set")
	}
	assertDSNIsTestDB(t, dsn)
	db := openDialect(t, "postgres", dsn)
	setupPaymentFundTables(t, db)
	runConcurrentOnPaidOnce(t, db)
}
