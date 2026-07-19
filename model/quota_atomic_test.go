package model

import (
	"errors"
	"sync"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestAtomicUserQuotaReserveConcurrent(t *testing.T) {
	truncateTables(t)
	const workers = 500

	user := &User{Username: "atomic-user-quota", Password: "password", Quota: 100}
	require.NoError(t, DB.Create(user).Error)

	originalBatchSetting := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = originalBatchSetting })

	start := make(chan struct{})
	results := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			ready.Done()
			<-start
			results <- DecreaseUserQuota(user.Id, 60, false)
		}()
	}
	ready.Wait()
	close(start)

	var successes int
	var insufficient int
	for i := 0; i < workers; i++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrUserQuotaInsufficient):
			insufficient++
		default:
			require.NoError(t, err)
		}
	}

	var quota int
	require.NoError(t, DB.Model(&User{}).Where("id = ?", user.Id).Select("quota").Scan(&quota).Error)
	assert.Equal(t, 1, successes)
	assert.Equal(t, workers-1, insufficient)
	assert.Equal(t, 40, quota)
}

func TestAtomicTokenQuotaReserveConcurrent(t *testing.T) {
	truncateTables(t)
	const workers = 500

	token := &Token{UserId: 1, Key: "atomic-token-quota", Name: "atomic", RemainQuota: 100}
	require.NoError(t, DB.Create(token).Error)

	originalBatchSetting := common.BatchUpdateEnabled
	common.BatchUpdateEnabled = true
	t.Cleanup(func() { common.BatchUpdateEnabled = originalBatchSetting })

	start := make(chan struct{})
	results := make(chan error, workers)
	var ready sync.WaitGroup
	ready.Add(workers)
	for i := 0; i < workers; i++ {
		go func() {
			ready.Done()
			<-start
			results <- DecreaseTokenQuota(token.Id, token.Key, 60)
		}()
	}
	ready.Wait()
	close(start)

	var successes int
	var insufficient int
	for i := 0; i < workers; i++ {
		err := <-results
		switch {
		case err == nil:
			successes++
		case errors.Is(err, ErrTokenQuotaInsufficient):
			insufficient++
		default:
			require.NoError(t, err)
		}
	}

	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, 1, successes)
	assert.Equal(t, workers-1, insufficient)
	assert.Equal(t, 40, stored.RemainQuota)
	assert.Equal(t, 60, stored.UsedQuota)
}

func TestBatchStatisticsFlushFailureRetainsForRetry(t *testing.T) {
	originalDB := DB
	t.Cleanup(func() { DB = originalDB })

	for i := range batchUpdateStores {
		batchUpdateLocks[i].Lock()
		batchUpdateStores[i] = make(map[int]int)
		batchUpdateLocks[i].Unlock()
	}

	testDB, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	DB = testDB

	addNewRecord(BatchUpdateTypeUsedQuota, 99, 7)
	addNewRecord(BatchUpdateTypeRequestCount, 99, 1)
	batchUpdate()

	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	retainedUsedQuota := batchUpdateStores[BatchUpdateTypeUsedQuota][99]
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeRequestCount].Lock()
	retainedRequestCount := batchUpdateStores[BatchUpdateTypeRequestCount][99]
	batchUpdateLocks[BatchUpdateTypeRequestCount].Unlock()
	assert.Equal(t, 7, retainedUsedQuota)
	assert.Equal(t, 1, retainedRequestCount)

	require.NoError(t, testDB.AutoMigrate(&User{}))
	require.NoError(t, testDB.Create(&User{Id: 99, Username: "batch-retry", Password: "password"}).Error)
	batchUpdate()

	var stored User
	require.NoError(t, testDB.First(&stored, 99).Error)
	assert.Equal(t, 7, stored.UsedQuota)
	assert.Equal(t, 1, stored.RequestCount)
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Lock()
	assert.Empty(t, batchUpdateStores[BatchUpdateTypeUsedQuota])
	batchUpdateLocks[BatchUpdateTypeUsedQuota].Unlock()
	batchUpdateLocks[BatchUpdateTypeRequestCount].Lock()
	assert.Empty(t, batchUpdateStores[BatchUpdateTypeRequestCount])
	batchUpdateLocks[BatchUpdateTypeRequestCount].Unlock()
}
