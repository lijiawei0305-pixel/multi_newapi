package model

import (
	"sync"
	"sync/atomic"
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

func closeRedisAfterAuthFence(t *testing.T, server *miniredis.Miniredis, targetKey string) {
	t.Helper()
	var once sync.Once
	restore := setAuthCacheFenceHookForTest(func(cacheKey string) {
		if cacheKey == targetKey {
			once.Do(server.Close)
		}
	})
	t.Cleanup(restore)
}

func restartAuthCacheTestRedis(t *testing.T, server *miniredis.Miniredis) {
	t.Helper()
	require.NoError(t, server.Restart())
	require.NoError(t, common.RDB.Ping(t.Context()).Err())
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

func TestTokenDisableFenceRejectsStaleCacheAfterPostCommitRedisFailure(t *testing.T) {
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
	cacheKey := getTokenCacheKey(token.Key)
	staleToken := token
	staleToken.Clean()
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleToken, time.Minute))

	closeRedisAfterAuthFence(t, server, cacheKey)
	token.Status = common.TokenStatusDisabled
	err := token.Update()
	require.Error(t, err, "a committed token disable must surface a Redis invalidation failure")
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusDisabled, stored.Status)
	assert.EqualValues(t, 1, stored.AuthVersion)

	var pending AuthCacheInvalidation
	require.NoError(t, DB.Where("cache_key = ?", cacheKey).First(&pending).Error)
	assert.Equal(t, authCacheInvalidationPending, pending.Status)

	restartAuthCacheTestRedis(t, server)
	// Emulate a stale hash held by another instance. The durable fence was
	// established before commit, so Redis recovery must not make it authoritative.
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleToken, time.Minute))
	reloaded, err := GetTokenByKey(token.Key, false)
	require.NoError(t, err)
	assert.Equal(t, common.TokenStatusDisabled, reloaded.Status)
	waitForAuthCacheFillsForTest()

	applied, err := ReconcilePendingAuthCacheInvalidations(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	assert.False(t, server.Exists(cacheKey))
	assert.False(t, server.Exists(authCacheFenceKey(cacheKey)))
	require.NoError(t, DB.First(&pending, pending.Id).Error)
	assert.Equal(t, authCacheInvalidationApplied, pending.Status)
}

func TestTokenDeleteFenceRejectsStaleCacheAfterPostCommitRedisFailure(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	token := Token{
		Id:          82201,
		UserId:      82200,
		Key:         "tokendeletefailure",
		Status:      common.TokenStatusEnabled,
		ExpiredTime: -1,
		RemainQuota: 100,
	}
	require.NoError(t, DB.Create(&token).Error)
	cacheKey := getTokenCacheKey(token.Key)
	staleToken := token
	staleToken.Clean()
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleToken, time.Minute))

	closeRedisAfterAuthFence(t, server, cacheKey)
	require.Error(t, token.Delete())
	require.ErrorIs(t, DB.First(&Token{}, token.Id).Error, gorm.ErrRecordNotFound)

	restartAuthCacheTestRedis(t, server)
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleToken, time.Minute))
	_, err := GetTokenByKey(token.Key, false)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	waitForAuthCacheFillsForTest()

	applied, err := ReconcilePendingAuthCacheInvalidations(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	assert.False(t, server.Exists(cacheKey))
}

func TestTokenDisableRollsBackWhenPreCommitFenceCannotBeEstablished(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	token := Token{Id: 82301, UserId: 82300, Key: "prefencefailure", Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&token).Error)

	server.Close()
	token.Status = common.TokenStatusDisabled
	require.Error(t, token.Update())
	var stored Token
	require.NoError(t, DB.First(&stored, token.Id).Error)
	assert.Equal(t, common.TokenStatusEnabled, stored.Status)
	assert.Zero(t, stored.AuthVersion)
	var count int64
	require.NoError(t, DB.Model(&AuthCacheInvalidation{}).Count(&count).Error)
	assert.Zero(t, count)
}

func TestNewerAuthorizationFenceSurvivesOlderOutboxReplay(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{Id: 82401, Username: "ordered-fence", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	staleUser := *user.ToBaseUser()
	cacheKey := getUserCacheKey(user.Id)

	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("status", common.UserStatusDisabled).Error; err != nil {
			return err
		}
		return prepareUserAuthCacheInvalidationTx(tx, user.Id)
	}))
	require.NoError(t, DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&User{}).Where("id = ?", user.Id).Update("group", "restricted").Error; err != nil {
			return err
		}
		return prepareUserAuthCacheInvalidationTx(tx, user.Id)
	}))

	var rows []AuthCacheInvalidation
	require.NoError(t, DB.Where("cache_key = ?", cacheKey).Order("id ASC").Find(&rows).Error)
	require.Len(t, rows, 2)
	require.NoError(t, ApplyAuthCacheInvalidation(rows[0].Id))
	assert.True(t, server.Exists(authCacheFenceKey(cacheKey)), "an older retry must not clear the newer transaction's fence")

	require.NoError(t, common.RedisHSetObj(cacheKey, &staleUser, time.Minute))
	reloaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusDisabled, reloaded.Status)
	assert.Equal(t, "restricted", reloaded.Group)
	waitForAuthCacheFillsForTest()

	require.NoError(t, ApplyAuthCacheInvalidation(rows[1].Id))
	assert.False(t, server.Exists(authCacheFenceKey(cacheKey)))
	assert.False(t, server.Exists(cacheKey))
}

func TestHealthyAuthorizationCacheHitDoesNotQueryDatabase(t *testing.T) {
	truncateTables(t)
	useAuthCacheTestRedis(t)
	user := User{Id: 82501, Username: "cache-only-hit", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, common.RedisHSetObj(getUserCacheKey(user.Id), user.ToBaseUser(), time.Minute))

	const callbackName = "test:count_auth_cache_db_queries"
	var queryCount atomic.Int64
	require.NoError(t, DB.Callback().Query().Before("gorm:query").Register(callbackName, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "users" {
			queryCount.Add(1)
		}
	}))
	t.Cleanup(func() {
		require.NoError(t, DB.Callback().Query().Remove(callbackName))
	})

	loaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, user.Username, loaded.Username)
	assert.Zero(t, queryCount.Load(), "a healthy unfenced cache hit must not perform an unconditional database validation")
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

func TestUserDisableFenceRejectsStaleCacheAfterPostCommitRedisFailure(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{Id: 83101, Username: "disable-cache-error", Status: common.UserStatusEnabled, Group: "default"}
	require.NoError(t, DB.Create(&user).Error)
	cacheKey := getUserCacheKey(user.Id)
	staleUser := *user.ToBaseUser()
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleUser, time.Minute))

	closeRedisAfterAuthFence(t, server, cacheKey)
	user.Status = common.UserStatusDisabled
	require.Error(t, user.Update(false))
	var stored User
	require.NoError(t, DB.First(&stored, user.Id).Error)
	assert.Equal(t, common.UserStatusDisabled, stored.Status)
	assert.EqualValues(t, 1, stored.AuthVersion)

	restartAuthCacheTestRedis(t, server)
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleUser, time.Minute))
	reloaded, err := GetUserCache(user.Id)
	require.NoError(t, err)
	assert.Equal(t, common.UserStatusDisabled, reloaded.Status)
	waitForAuthCacheFillsForTest()

	applied, err := ReconcilePendingAuthCacheInvalidations(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	assert.False(t, server.Exists(cacheKey))
}

func TestHardDeleteUserFenceRejectsStaleCacheAfterPostCommitRedisFailure(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{Id: 83201, Username: "hard-delete-cache-error", Status: common.UserStatusEnabled}
	token := Token{Id: 83202, UserId: user.Id, Key: "cacheerror", Status: common.TokenStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)
	require.NoError(t, DB.Create(&token).Error)
	cacheKey := getUserCacheKey(user.Id)
	staleUser := *user.ToBaseUser()
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleUser, time.Minute))

	closeRedisAfterAuthFence(t, server, cacheKey)
	err := HardDeleteUserById(user.Id)
	require.Error(t, err, "a committed hard delete must surface a Redis invalidation failure")
	require.ErrorIs(t, DB.Unscoped().First(&User{}, user.Id).Error, gorm.ErrRecordNotFound)

	restartAuthCacheTestRedis(t, server)
	require.NoError(t, common.RedisHSetObj(cacheKey, &staleUser, time.Minute))
	_, err = GetUserCache(user.Id)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	waitForAuthCacheFillsForTest()

	applied, err := ReconcilePendingAuthCacheInvalidations(10)
	require.NoError(t, err)
	assert.Equal(t, 1, applied)
	assert.False(t, server.Exists(cacheKey))
}

func TestHardDeleteUserRollsBackWhenPreCommitFenceCannotBeEstablished(t *testing.T) {
	truncateTables(t)
	server := useAuthCacheTestRedis(t)
	user := User{Id: 83301, Username: "hard-delete-pre-fence", Status: common.UserStatusEnabled}
	require.NoError(t, DB.Create(&user).Error)

	server.Close()
	require.Error(t, HardDeleteUserById(user.Id))
	require.NoError(t, DB.Unscoped().First(&User{}, user.Id).Error)
	var count int64
	require.NoError(t, DB.Model(&AuthCacheInvalidation{}).Count(&count).Error)
	assert.Zero(t, count)
}
