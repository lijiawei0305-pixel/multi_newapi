package model

import (
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func useAuthCacheTestRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})

	previousClient := common.RDB
	previousEnabled := common.RedisEnabled
	previousFrequency := common.SyncFrequency
	common.RDB = client
	common.RedisEnabled = true
	common.SyncFrequency = 60
	t.Cleanup(func() {
		waitForAuthCacheFillsForTest()
		common.RDB = previousClient
		common.RedisEnabled = previousEnabled
		common.SyncFrequency = previousFrequency
		require.NoError(t, client.Close())
	})
	return server
}

func installAuthCacheFillBarrier(t *testing.T, targetKey string) (<-chan struct{}, func(), <-chan struct{}) {
	t.Helper()
	entered := make(chan struct{})
	release := make(chan struct{})
	finished := make(chan struct{})
	var enteredOnce sync.Once
	var releaseOnce sync.Once
	var finishedOnce sync.Once

	restore := setAuthCacheFillHookForTest(func(cacheKey string, before bool) {
		if cacheKey != targetKey {
			return
		}
		if before {
			enteredOnce.Do(func() {
				close(entered)
				<-release
			})
			return
		}
		finishedOnce.Do(func() { close(finished) })
	})
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	t.Cleanup(func() {
		unblock()
		restore()
	})
	return entered, unblock, finished
}

func waitForAuthCacheBarrier(t *testing.T, signal <-chan struct{}, message string) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(3 * time.Second):
		require.FailNow(t, message)
	}
}

func TestUserCacheGenerationRejectsStaleDatabaseFillAfterDisable(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{
		Id:       81001,
		Username: "generation-user",
		Status:   common.UserStatusEnabled,
		Group:    "default",
		Quota:    100,
	}
	require.NoError(t, DB.Create(&user).Error)

	cacheKey := getUserCacheKey(user.Id)
	entered, release, finished := installAuthCacheFillBarrier(t, cacheKey)
	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	require.Equal(t, common.UserStatusEnabled, loaded.Status)
	waitForAuthCacheBarrier(t, entered, "stale user fill did not reach the controlled barrier")

	user.Status = common.UserStatusDisabled
	require.NoError(t, user.Update(false))
	release()
	waitForAuthCacheBarrier(t, finished, "stale user fill did not finish after release")

	assert.False(t, server.Exists(cacheKey), "a pre-disable DB read must not republish the enabled user")
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, stored.Status)
}

func TestTokenCacheGenerationRejectsStaleDatabaseFillAfterDisable(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	token := Token{
		Id:             82001,
		UserId:         82000,
		Key:            "generationtoken",
		Name:           "generation token",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: false,
	}
	require.NoError(t, DB.Create(&token).Error)

	cacheKey := getTokenCacheKey(token.Key)
	entered, release, finished := installAuthCacheFillBarrier(t, cacheKey)
	loaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	require.Equal(t, common.TokenStatusEnabled, loaded.Status)
	waitForAuthCacheBarrier(t, entered, "stale token fill did not reach the controlled barrier")

	token.Status = common.TokenStatusDisabled
	require.NoError(t, token.Update())
	release()
	waitForAuthCacheBarrier(t, finished, "stale token fill did not finish after release")

	assert.False(t, server.Exists(cacheKey), "a pre-disable DB read must not republish the enabled token")
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
}

func TestTokenDisableSurfacesPostCommitRedisInvalidationFailure(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	token := Token{
		Id:             82101,
		UserId:         82100,
		Key:            "tokeninvalidateerror",
		Status:         common.TokenStatusEnabled,
		ExpiredTime:    -1,
		RemainQuota:    100,
		UnlimitedQuota: false,
	}
	require.NoError(t, DB.Create(&token).Error)

	server.Close()
	token.Status = common.TokenStatusDisabled
	err := token.Update()
	require.Error(t, err, "a committed token disable must surface a Redis invalidation failure")
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
}

func TestHardDeleteUserImmediatelyInvalidatesUserAndTokenCaches(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{Id: 83001, Username: "hard-delete-user", Status: common.UserStatusEnabled, Group: "default"}
	token := Token{
		Id:          83002,
		UserId:      user.Id,
		Key:         "harddeletetoken",
		Name:        "hard delete token",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: 100,
	}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	userCacheKey := getUserCacheKey(user.Id)
	tokenCacheKey := getTokenCacheKey(token.Key)
	require.NoError(t, common.RedisHSetObj(userCacheKey, user.ToBaseUser(), time.Minute))
	cachedToken := token
	cachedToken.Clean()
	require.NoError(t, common.RedisHSetObj(tokenCacheKey, &cachedToken, time.Minute))
	require.True(t, server.Exists(userCacheKey))
	require.True(t, server.Exists(tokenCacheKey))

	require.NoError(t, HardDeleteUserById(user.Id))
	assert.False(t, server.Exists(userCacheKey))
	assert.False(t, server.Exists(tokenCacheKey))
	require.ErrorIs(t, DB.Unscoped().First(&User{}, user.Id).Error, gorm.ErrRecordNotFound)
}

func TestHardDeleteUserSurfacesPostCommitRedisInvalidationFailure(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{Id: 83101, Username: "hard-delete-cache-error", Status: common.UserStatusEnabled}
	token := Token{Id: 83102, UserId: user.Id, Key: "cacheerror", Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)

	server.Close()
	err := HardDeleteUserById(user.Id)
	require.Error(t, err, "a committed hard delete must surface a Redis invalidation failure")
	require.ErrorIs(t, DB.Unscoped().First(&User{}, user.Id).Error, gorm.ErrRecordNotFound)
}
