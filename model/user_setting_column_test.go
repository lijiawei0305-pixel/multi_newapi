package model

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression: concurrent setting writes must not roll back used_quota / quota / request_count
// (UpdateSelf billing lost-update, 2026-08-04).
func TestUpdateUserSettingColumnDoesNotTouchBillingCounters(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username:     "setting-col-user",
		Password:     "password-long",
		Quota:        1_000_000,
		UsedQuota:    500_000,
		RequestCount: 42,
		Setting:      `{"language":"zh"}`,
	}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, UpdateUserSettingColumn(user.Id, `{"language":"en","sidebar_modules":"{}"}`))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, 1_000_000, got.Quota)
	assert.Equal(t, 500_000, got.UsedQuota)
	assert.Equal(t, 42, got.RequestCount)
	assert.Contains(t, got.Setting, `"language":"en"`)
}

func TestUpdateUserSettingColumnRejectsOversizedPayload(t *testing.T) {
	truncateTables(t)

	user := &User{Username: "setting-oversized", Password: "password-long", Setting: `{}`}
	require.NoError(t, DB.Create(user).Error)

	oversized := make([]byte, MaxUserSettingBytes+1)
	for i := range oversized {
		oversized[i] = 'A'
	}
	err := UpdateUserSettingColumn(user.Id, string(oversized))
	require.Error(t, err)

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, `{}`, got.Setting)
}

func TestUpdateUserAccessTokenColumnDoesNotTouchBillingCounters(t *testing.T) {
	truncateTables(t)

	token := "tok_access_token_column_01"
	user := &User{
		Username:     "access-tok-user",
		Password:     "password-long",
		Quota:        9_000,
		UsedQuota:    1_234,
		RequestCount: 7,
	}
	require.NoError(t, DB.Create(user).Error)

	require.NoError(t, UpdateUserAccessTokenColumn(user.Id, token))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	require.NotNil(t, got.AccessToken)
	assert.Equal(t, token, *got.AccessToken)
	assert.Equal(t, 9_000, got.Quota)
	assert.Equal(t, 1_234, got.UsedQuota)
	assert.Equal(t, 7, got.RequestCount)
}

// Simulates the production race: setting save concurrent with atomic used_quota increments.
// Before the fix, User.Update full-row write rolled used_quota back to the T0 snapshot.
func TestSettingWriteConcurrentWithUsedQuotaIncrement(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username:     "race-setting-billing",
		Password:     "password-long",
		Quota:        10_000_000,
		UsedQuota:    0,
		RequestCount: 0,
		Setting:      `{"language":"zh"}`,
	}
	require.NoError(t, DB.Create(user).Error)

	const billingWorkers = 50
	const settingWorkers = 50
	const quotaPerBill = 100

	start := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(billingWorkers + settingWorkers)

	for i := 0; i < billingWorkers; i++ {
		go func() {
			defer wg.Done()
			<-start
			updateUserUsedQuotaAndRequestCount(user.Id, quotaPerBill, 1)
		}()
	}
	for i := 0; i < settingWorkers; i++ {
		i := i
		go func() {
			defer wg.Done()
			<-start
			// Mimic UpdateSelf: read snapshot, change setting, write setting only.
			var snap User
			_ = DB.First(&snap, user.Id).Error
			setting := fmt.Sprintf(`{"language":"zh","sidebar_modules":"%d"}`, i)
			_ = UpdateUserSettingColumn(snap.Id, setting)
		}()
	}

	close(start)
	wg.Wait()

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, billingWorkers*quotaPerBill, got.UsedQuota,
		"used_quota must accumulate fully; lost updates indicate full-row overwrite regression")
	assert.Equal(t, billingWorkers, got.RequestCount)
	assert.Equal(t, 10_000_000, got.Quota)
}

// Hardening: even legacy User.Update(false) after loading a full row must not
// clobber concurrent billing counters (defense in depth for OAuth/bind paths).
func TestUserUpdateOmitsBillingCounters(t *testing.T) {
	truncateTables(t)

	user := &User{
		Username:     "update-omit-billing",
		Password:     "password-long",
		DisplayName:  "old",
		Quota:        77_000,
		UsedQuota:    12_000,
		RequestCount: 3,
	}
	require.NoError(t, DB.Create(user).Error)

	// Concurrent billing after T0 read would be lost if Update wrote used_quota.
	loaded, err := GetUserById(user.Id, false)
	require.NoError(t, err)
	updateUserUsedQuotaAndRequestCount(user.Id, 5_000, 2)

	loaded.DisplayName = "new-name"
	// Stale snapshot still carries UsedQuota=12000 — must not be written back.
	require.NoError(t, loaded.Update(false))

	var got User
	require.NoError(t, DB.First(&got, user.Id).Error)
	assert.Equal(t, "new-name", got.DisplayName)
	assert.Equal(t, 77_000, got.Quota)
	assert.Equal(t, 17_000, got.UsedQuota)
	assert.Equal(t, 5, got.RequestCount)
}
