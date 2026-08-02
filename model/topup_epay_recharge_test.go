package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestRechargeEpay_IdempotentAndAmountGuard locks the epay credit path:
// pending→success+quota once; second call is no-op; money mismatch rejects.
func TestRechargeEpay_IdempotentAndAmountGuard(t *testing.T) {
	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&User{}, &TopUp{}))

	originalDB := DB
	DB = testDB
	t.Cleanup(func() { DB = originalDB })

	user := &User{Username: "epay-user", Password: "x", Quota: 0, Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(user).Error)

	tradeNo := "EpayTestTrade001"
	topUp := &TopUp{
		UserId:          user.Id,
		Amount:          1,
		Money:           1.00,
		TradeNo:         tradeNo,
		PaymentMethod:   "alipay",
		PaymentProvider: PaymentProviderEpay,
		CreateTime:      common.GetTimestamp(),
		Status:          common.TopUpStatusPending,
	}
	require.NoError(t, topUp.Insert())

	// Money mismatch → fail, still pending
	err = RechargeEpay(tradeNo, "9.99", "alipay", "127.0.0.1")
	require.Error(t, err)

	var reloaded TopUp
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&reloaded).Error)
	require.Equal(t, common.TopUpStatusPending, reloaded.Status)

	// Correct money → success + quota
	require.NoError(t, RechargeEpay(tradeNo, "1.00", "alipay", "127.0.0.1"))
	require.NoError(t, DB.Where("trade_no = ?", tradeNo).First(&reloaded).Error)
	require.Equal(t, common.TopUpStatusSuccess, reloaded.Status)

	var u User
	require.NoError(t, DB.First(&u, user.Id).Error)
	wantQuota := int(common.QuotaPerUnit) // Amount=1
	require.Equal(t, wantQuota, u.Quota)

	// Idempotent second call: no double credit
	require.NoError(t, RechargeEpay(tradeNo, "1.00", "alipay", "127.0.0.1"))
	require.NoError(t, DB.First(&u, user.Id).Error)
	require.Equal(t, wantQuota, u.Quota)
}
