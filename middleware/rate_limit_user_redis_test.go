package middleware

import (
	"context"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v8"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func withCriticalRedisLimit(t *testing.T, maxRequests int, durationSeconds int64, keyExpiration time.Duration) (*miniredis.Miniredis, *redis.Client) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClient(&redis.Options{Addr: server.Addr()})
	previous := struct {
		enabled       bool
		maxRequests   int
		duration      int64
		redisEnabled  bool
		redisClient   *redis.Client
		keyExpiration time.Duration
	}{
		common.CriticalRateLimitEnable,
		common.CriticalRateLimitNum,
		common.CriticalRateLimitDuration,
		common.RedisEnabled,
		common.RDB,
		common.RateLimitKeyExpirationDuration,
	}

	common.CriticalRateLimitEnable = true
	common.CriticalRateLimitNum = maxRequests
	common.CriticalRateLimitDuration = durationSeconds
	common.RedisEnabled = true
	common.RDB = client
	common.RateLimitKeyExpirationDuration = keyExpiration
	t.Cleanup(func() {
		common.CriticalRateLimitEnable = previous.enabled
		common.CriticalRateLimitNum = previous.maxRequests
		common.CriticalRateLimitDuration = previous.duration
		common.RedisEnabled = previous.redisEnabled
		common.RDB = previous.redisClient
		common.RateLimitKeyExpirationDuration = previous.keyExpiration
		_ = client.Close()
	})
	return server, client
}

func TestCriticalUserRateLimit_RedisConcurrentRequestsDoNotExceedMaximum(t *testing.T) {
	const (
		maxRequests   = 5
		totalRequests = 100
		userID        = 910101
	)
	_, client := withCriticalRedisLimit(t, maxRequests, 600, 20*time.Minute)
	handler := CriticalUserRateLimit()

	start := make(chan struct{})
	statuses := make(chan int, totalRequests)
	var waitGroup sync.WaitGroup
	waitGroup.Add(totalRequests)
	for i := 0; i < totalRequests; i++ {
		go func() {
			defer waitGroup.Done()
			<-start
			ctx := runLimiter(handler, userID, "1.1.1.1")
			if !ctx.IsAborted() {
				statuses <- http.StatusOK
				return
			}
			statuses <- ctx.Writer.Status()
		}()
	}
	close(start)
	waitGroup.Wait()
	close(statuses)

	allowed := 0
	limited := 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Errorf("unexpected limiter status: %d", status)
		}
	}
	assert.Equal(t, maxRequests, allowed)
	assert.Equal(t, totalRequests-maxRequests, limited)
	length, err := client.LLen(context.Background(), "rateLimit:CT:user:910101").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(maxRequests), length)
}

func TestCriticalRateLimit_RedisConcurrentRequestsDoNotExceedMaximum(t *testing.T) {
	const (
		maxRequests   = 5
		totalRequests = 100
		clientIP      = "198.51.100.23"
	)
	_, client := withCriticalRedisLimit(t, maxRequests, 600, 20*time.Minute)
	handler := CriticalRateLimit()

	start := make(chan struct{})
	statuses := make(chan int, totalRequests)
	var waitGroup sync.WaitGroup
	waitGroup.Add(totalRequests)
	for i := 0; i < totalRequests; i++ {
		go func() {
			defer waitGroup.Done()
			<-start
			ctx := runLimiter(handler, 0, clientIP)
			if !ctx.IsAborted() {
				statuses <- http.StatusOK
				return
			}
			statuses <- ctx.Writer.Status()
		}()
	}
	close(start)
	waitGroup.Wait()
	close(statuses)

	allowed := 0
	limited := 0
	for status := range statuses {
		switch status {
		case http.StatusOK:
			allowed++
		case http.StatusTooManyRequests:
			limited++
		default:
			t.Errorf("unexpected limiter status: %d", status)
		}
	}
	assert.Equal(t, maxRequests, allowed)
	assert.Equal(t, totalRequests-maxRequests, limited)
	length, err := client.LLen(context.Background(), "rateLimit:CT"+clientIP).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(maxRequests), length)
}

func TestCriticalUserRateLimit_RedisBucketTTLNeverShortensConfiguredWindow(t *testing.T) {
	server, _ := withCriticalRedisLimit(t, 1, 60, time.Second)
	handler := CriticalUserRateLimit()
	const userID = 910102

	assert.False(t, runLimiter(handler, userID, "1.1.1.1").IsAborted())
	server.FastForward(2 * time.Second)
	assert.True(t, server.Exists("rateLimit:CT:user:910102"))

	limited := runLimiter(handler, userID, "1.1.1.1")
	require.True(t, limited.IsAborted())
	assert.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())
}

func TestCriticalUserRateLimit_RedisExpirationRestoresCapacity(t *testing.T) {
	server, _ := withCriticalRedisLimit(t, 1, 2, 2*time.Second)
	handler := CriticalUserRateLimit()
	const userID = 910201

	assert.False(t, runLimiter(handler, userID, "1.1.1.1").IsAborted())
	limited := runLimiter(handler, userID, "1.1.1.1")
	require.True(t, limited.IsAborted())
	assert.Equal(t, http.StatusTooManyRequests, limited.Writer.Status())

	server.FastForward(3 * time.Second)
	assert.False(t, server.Exists("rateLimit:CT:user:910201"))
	assert.False(t, runLimiter(handler, userID, "1.1.1.1").IsAborted())
}

func TestCriticalUserRateLimit_RedisWindowExpirationRestoresCapacity(t *testing.T) {
	_, client := withCriticalRedisLimit(t, 1, 2, 20*time.Minute)
	handler := CriticalUserRateLimit()
	const (
		userID = 910202
		key    = "rateLimit:CT:user:910202"
	)

	ctx := context.Background()
	require.NoError(t, client.LPush(ctx, key, time.Now().Add(-3*time.Second).Format(timeFormat)).Err())
	require.NoError(t, client.Expire(ctx, key, 20*time.Minute).Err())
	assert.False(t, runLimiter(handler, userID, "1.1.1.1").IsAborted())
	length, err := client.LLen(ctx, key).Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), length)
}

func TestCriticalUserRateLimit_RedisErrorFailsClosed(t *testing.T) {
	_, client := withCriticalRedisLimit(t, 1, 600, 20*time.Minute)
	handler := CriticalUserRateLimit()
	require.NoError(t, client.Close())

	ctx := runLimiter(handler, 910301, "1.1.1.1")
	require.True(t, ctx.IsAborted())
	assert.Equal(t, http.StatusInternalServerError, ctx.Writer.Status())
}

func TestCriticalUserRateLimit_InvalidRedisConfigurationFailsClosed(t *testing.T) {
	t.Run("nil client", func(t *testing.T) {
		_, _ = withCriticalRedisLimit(t, 1, 600, 20*time.Minute)
		common.RDB = nil
		handler := CriticalUserRateLimit()

		ctx := runLimiter(handler, 910302, "1.1.1.1")
		require.True(t, ctx.IsAborted())
		assert.Equal(t, http.StatusInternalServerError, ctx.Writer.Status())
	})

	t.Run("non-positive window", func(t *testing.T) {
		_, _ = withCriticalRedisLimit(t, 1, 0, 20*time.Minute)
		handler := CriticalUserRateLimit()

		ctx := runLimiter(handler, 910303, "1.1.1.1")
		require.True(t, ctx.IsAborted())
		assert.Equal(t, http.StatusInternalServerError, ctx.Writer.Status())
	})

	t.Run("non-positive key expiration", func(t *testing.T) {
		_, _ = withCriticalRedisLimit(t, 1, 600, 0)
		handler := CriticalUserRateLimit()

		ctx := runLimiter(handler, 910304, "1.1.1.1")
		require.True(t, ctx.IsAborted())
		assert.Equal(t, http.StatusInternalServerError, ctx.Writer.Status())
	})
}

func TestCriticalUserRateLimit_RedisUsersHaveIndependentBuckets(t *testing.T) {
	_, client := withCriticalRedisLimit(t, 1, 600, 20*time.Minute)
	handler := CriticalUserRateLimit()
	const userA, userB = 910401, 910402

	assert.False(t, runLimiter(handler, userA, "1.1.1.1").IsAborted())
	limitedA := runLimiter(handler, userA, "2.2.2.2")
	require.True(t, limitedA.IsAborted())
	assert.Equal(t, http.StatusTooManyRequests, limitedA.Writer.Status())
	assert.False(t, runLimiter(handler, userB, "1.1.1.1").IsAborted())

	ctx := context.Background()
	userALength, err := client.LLen(ctx, "rateLimit:CT:user:910401").Result()
	require.NoError(t, err)
	userBLength, err := client.LLen(ctx, "rateLimit:CT:user:910402").Result()
	require.NoError(t, err)
	assert.Equal(t, int64(1), userALength)
	assert.Equal(t, int64(1), userBLength)
}
