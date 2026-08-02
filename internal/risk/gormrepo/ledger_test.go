package gormrepo

import (
	"context"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/QuantumNous/new-api/internal/risk"
)

func newTestRepo(t *testing.T) *Repo {
	t.Helper()
	// 每测独立 :memory:，避免 cache=shared 污染
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{
		TranslateError: true,
	})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, AutoMigrate(db))
	return New(db)
}

func TestClaimTrial_UserDedupAndSurvives(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	require.NoError(t, r.ClaimTrial(ctx, 7, risk.PurchaseIdentity{}, 0, now))
	require.ErrorIs(t, r.ClaimTrial(ctx, 7, risk.PurchaseIdentity{}, 0, now), risk.ErrPurchaseLimitExceeded)
}

func TestClaimTrial_DeviceTTLSelfHeals(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	ttl := time.Hour
	require.NoError(t, r.ClaimTrial(ctx, 1, risk.PurchaseIdentity{DeviceID: "dev-x"}, ttl, now))
	require.ErrorIs(t, r.ClaimTrial(ctx, 2, risk.PurchaseIdentity{DeviceID: "dev-x"}, ttl, now), risk.ErrPurchaseLimitExceeded)
	// 过期后其他用户可占
	later := now.Add(2 * time.Hour)
	require.NoError(t, r.ClaimTrial(ctx, 3, risk.PurchaseIdentity{DeviceID: "dev-x"}, ttl, later))
}

func TestClaimTrial_LoserRollsBackPartialDims(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	// A 占 device
	require.NoError(t, r.ClaimTrial(ctx, 7, risk.PurchaseIdentity{DeviceID: "dev-1"}, time.Hour, now))
	// B 同设备应拒，且 B 的 user 行不得残留
	require.ErrorIs(t, r.ClaimTrial(ctx, 8, risk.PurchaseIdentity{DeviceID: "dev-1"}, time.Hour, now), risk.ErrPurchaseLimitExceeded)
	// B 换设备应成功
	require.NoError(t, r.ClaimTrial(ctx, 8, risk.PurchaseIdentity{DeviceID: "dev-2"}, time.Hour, now))
}

func TestReleaseTrial_AndReclaim(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	now := time.Unix(1_700_000_000, 0)
	require.NoError(t, r.ClaimTrial(ctx, 7, risk.PurchaseIdentity{DeviceID: "d"}, time.Hour, now))
	res, err := r.ReleaseTrial(ctx, 7, risk.PurchaseIdentity{DeviceID: "d"}, false)
	require.NoError(t, err)
	require.Contains(t, res.Released, "user")
	require.Contains(t, res.Released, "device")
	require.NoError(t, r.ClaimTrial(ctx, 7, risk.PurchaseIdentity{DeviceID: "d"}, time.Hour, now))
}

func TestClaimPlan_LimitAndRollback(t *testing.T) {
	r := newTestRepo(t)
	ctx := context.Background()
	require.NoError(t, r.ClaimPlan(ctx, 9, 1, 2))
	require.NoError(t, r.ClaimPlan(ctx, 9, 1, 2))
	require.ErrorIs(t, r.ClaimPlan(ctx, 9, 1, 2), risk.ErrPurchaseLimitExceeded)
	require.NoError(t, r.RollbackPlan(ctx, 9, 1))
	require.NoError(t, r.ClaimPlan(ctx, 9, 1, 2))
}
