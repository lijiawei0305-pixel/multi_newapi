package model

import (
	"errors"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestSaveQuotaDataCacheRetainsFailedEntryAndMergesNewIncrement(t *testing.T) {
	originalDB := DB
	CacheQuotaDataLock.Lock()
	originalCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB = originalDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = originalCache
		CacheQuotaDataLock.Unlock()
	})

	testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usedata.db")), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, testDB.AutoMigrate(&QuotaData{}))
	require.NoError(t, ensureQuotaDataBucketUniqueIndex(testDB, false))
	DB = testDB

	const createdAt = int64(3600)
	successRow := QuotaData{
		UserID:    1,
		Username:  "saved-user",
		ModelName: "saved-model",
		CreatedAt: createdAt,
		UseGroup:  "default",
	}
	retryRow := QuotaData{
		UserID:    2,
		Username:  "retry-user",
		ModelName: "retry-model",
		CreatedAt: createdAt,
		UseGroup:  "default",
	}
	require.NoError(t, testDB.Create(&successRow).Error)
	require.NoError(t, testDB.Create(&retryRow).Error)

	LogQuotaData(QuotaDataLogParams{
		UserID:    successRow.UserID,
		Username:  successRow.Username,
		ModelName: successRow.ModelName,
		CreatedAt: createdAt,
		UseGroup:  successRow.UseGroup,
		Quota:     10,
		TokenUsed: 4,
	})
	LogQuotaData(QuotaDataLogParams{
		UserID:    retryRow.UserID,
		Username:  retryRow.Username,
		ModelName: retryRow.ModelName,
		CreatedAt: createdAt,
		UseGroup:  retryRow.UseGroup,
		Quota:     20,
		TokenUsed: 8,
	})

	injectedError := errors.New("injected first quota-data flush failure")
	upsertStarted := make(chan struct{})
	releaseUpsert := make(chan struct{})
	var failFirstRetryUpsert atomic.Bool
	failFirstRetryUpsert.Store(true)
	const callbackName = "test:fail-first-quota-data-upsert"
	require.NoError(t, testDB.Callback().Create().Before("gorm:create").Register(callbackName, func(tx *gorm.DB) {
		quotaData, ok := tx.Statement.Dest.(*QuotaData)
		if !ok || quotaData.Username != retryRow.Username || !failFirstRetryUpsert.CompareAndSwap(true, false) {
			return
		}
		close(upsertStarted)
		<-releaseUpsert
		tx.AddError(injectedError)
	}))
	t.Cleanup(func() {
		testDB.Callback().Create().Remove(callbackName)
	})

	flushDone := make(chan struct{})
	go func() {
		SaveQuotaDataCache()
		close(flushDone)
	}()

	select {
	case <-upsertStarted:
	case <-time.After(5 * time.Second):
		close(releaseUpsert)
		<-flushDone
		require.FailNow(t, "quota-data flush did not reach the injected upsert")
	}

	cacheLockAvailable := CacheQuotaDataLock.TryLock()
	if cacheLockAvailable {
		CacheQuotaDataLock.Unlock()
	} else {
		close(releaseUpsert)
		<-flushDone
		require.FailNow(t, "quota-data flush held the ingestion lock during database I/O")
	}

	LogQuotaData(QuotaDataLogParams{
		UserID:    retryRow.UserID,
		Username:  retryRow.Username,
		ModelName: retryRow.ModelName,
		CreatedAt: createdAt,
		UseGroup:  retryRow.UseGroup,
		Quota:     3,
		TokenUsed: 2,
	})
	close(releaseUpsert)
	<-flushDone

	CacheQuotaDataLock.Lock()
	require.Len(t, CacheQuotaData, 1)
	var retained QuotaData
	for _, quotaData := range CacheQuotaData {
		retained = *quotaData
	}
	CacheQuotaDataLock.Unlock()
	assert.Equal(t, retryRow.Username, retained.Username)
	assert.Equal(t, 2, retained.Count)
	assert.Equal(t, 23, retained.Quota)
	assert.Equal(t, 10, retained.TokenUsed)

	var storedSuccess QuotaData
	require.NoError(t, testDB.First(&storedSuccess, successRow.Id).Error)
	assert.Equal(t, 1, storedSuccess.Count)
	assert.Equal(t, 10, storedSuccess.Quota)
	assert.Equal(t, 4, storedSuccess.TokenUsed)

	var storedRetry QuotaData
	require.NoError(t, testDB.First(&storedRetry, retryRow.Id).Error)
	assert.Zero(t, storedRetry.Count)
	assert.Zero(t, storedRetry.Quota)
	assert.Zero(t, storedRetry.TokenUsed)

	SaveQuotaDataCache()
	require.NoError(t, testDB.First(&storedSuccess, successRow.Id).Error)
	assert.Equal(t, 1, storedSuccess.Count, "a successful first-flush item must not be replayed")
	require.NoError(t, testDB.First(&storedRetry, retryRow.Id).Error)
	assert.Equal(t, 2, storedRetry.Count)
	assert.Equal(t, 23, storedRetry.Quota)
	assert.Equal(t, 10, storedRetry.TokenUsed)

	CacheQuotaDataLock.Lock()
	assert.Empty(t, CacheQuotaData)
	CacheQuotaDataLock.Unlock()
}

func TestSaveQuotaDataCacheRetainsCreateAndUpdateFailures(t *testing.T) {
	originalDB := DB
	CacheQuotaDataLock.Lock()
	originalCache := CacheQuotaData
	CacheQuotaData = make(map[string]*QuotaData)
	CacheQuotaDataLock.Unlock()
	t.Cleanup(func() {
		DB = originalDB
		CacheQuotaDataLock.Lock()
		CacheQuotaData = originalCache
		CacheQuotaDataLock.Unlock()
	})

	for _, operation := range []string{"create", "update"} {
		t.Run(operation, func(t *testing.T) {
			CacheQuotaDataLock.Lock()
			CacheQuotaData = make(map[string]*QuotaData)
			CacheQuotaDataLock.Unlock()

			testDB, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "usedata.db")), &gorm.Config{})
			require.NoError(t, err)
			require.NoError(t, testDB.AutoMigrate(&QuotaData{}))
			require.NoError(t, ensureQuotaDataBucketUniqueIndex(testDB, false))
			DB = testDB

			quotaData := QuotaData{
				UserID:    1,
				Username:  operation + "-retry-user",
				ModelName: operation + "-retry-model",
				CreatedAt: 3600,
				UseGroup:  "default",
			}
			if operation == "update" {
				require.NoError(t, testDB.Create(&quotaData).Error)
			}

			var failFirstOperation atomic.Bool
			failFirstOperation.Store(true)
			callbackName := "test:fail-first-quota-data-" + operation
			injectFailure := func(tx *gorm.DB) {
				if failFirstOperation.CompareAndSwap(true, false) {
					tx.AddError(errors.New("injected first quota-data " + operation + " failure"))
				}
			}
			require.NoError(t, testDB.Callback().Create().Before("gorm:create").Register(callbackName, injectFailure))
			t.Cleanup(func() { testDB.Callback().Create().Remove(callbackName) })

			LogQuotaData(QuotaDataLogParams{
				UserID:    quotaData.UserID,
				Username:  quotaData.Username,
				ModelName: quotaData.ModelName,
				CreatedAt: quotaData.CreatedAt,
				UseGroup:  quotaData.UseGroup,
				Quota:     12,
				TokenUsed: 5,
			})
			SaveQuotaDataCache()

			CacheQuotaDataLock.Lock()
			require.Len(t, CacheQuotaData, 1)
			CacheQuotaDataLock.Unlock()

			SaveQuotaDataCache()
			var stored QuotaData
			require.NoError(t, testDB.Where("username = ?", quotaData.Username).First(&stored).Error)
			assert.Equal(t, 1, stored.Count)
			assert.Equal(t, 12, stored.Quota)
			assert.Equal(t, 5, stored.TokenUsed)
			CacheQuotaDataLock.Lock()
			assert.Empty(t, CacheQuotaData)
			CacheQuotaDataLock.Unlock()
		})
	}
}
