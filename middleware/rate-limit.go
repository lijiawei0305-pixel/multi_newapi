package middleware

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/gin-gonic/gin"
)

var timeFormat = "2006-01-02T15:04:05.000Z"

var inMemoryRateLimiter common.InMemoryRateLimiter

var defNext = func(c *gin.Context) {
	c.Next()
}

// Keep the existing list-backed sliding-window representation so rate-limit
// keys created by earlier versions remain usable during a rolling deploy. The
// whole read/decide/write sequence must stay in one script: separate LLEN and
// LPUSH calls let concurrent requests all observe spare capacity and pass.
const redisRateLimitScript = `
local max_requests = tonumber(ARGV[1])
local now = ARGV[2]
local cutoff = ARGV[3]
local expiration_ms = tonumber(ARGV[4])

if not max_requests or max_requests <= 0 then
  return redis.error_reply("invalid rate limit maximum")
end
if not expiration_ms then
  return redis.error_reply("invalid rate limit expiration")
end

local length = redis.call("LLEN", KEYS[1])
if length < max_requests then
  redis.call("LPUSH", KEYS[1], now)
  redis.call("PEXPIRE", KEYS[1], expiration_ms)
  return 1
end

local oldest = redis.call("LINDEX", KEYS[1], -1)
if not oldest or not string.match(oldest, "^%d%d%d%d%-%d%d%-%d%dT%d%d:%d%d:%d%d%.%d%d%dZ$") then
  return redis.error_reply("invalid rate limit timestamp")
end

if oldest > cutoff then
  redis.call("PEXPIRE", KEYS[1], expiration_ms)
  return 0
end

redis.call("LPUSH", KEYS[1], now)
redis.call("LTRIM", KEYS[1], 0, max_requests - 1)
redis.call("PEXPIRE", KEYS[1], expiration_ms)
return 1
`

func redisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := "rateLimit:" + mark + c.ClientIP()
	redisSlidingWindowRateLimiter(c, maxRequestNum, duration, key)
}

func memoryRateLimiter(c *gin.Context, maxRequestNum int, duration int64, mark string) {
	key := mark + c.ClientIP()
	if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
		return
	}
}

func rateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			redisRateLimiter(c, maxRequestNum, duration, mark)
		}
	} else {
		// It's safe to call multi times.
		inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
		return func(c *gin.Context) {
			memoryRateLimiter(c, maxRequestNum, duration, mark)
		}
	}
}

func GlobalWebRateLimit() func(c *gin.Context) {
	if common.GlobalWebRateLimitEnable {
		return rateLimitFactory(common.GlobalWebRateLimitNum, common.GlobalWebRateLimitDuration, "GW")
	}
	return defNext
}

func GlobalAPIRateLimit() func(c *gin.Context) {
	if common.GlobalApiRateLimitEnable {
		return rateLimitFactory(common.GlobalApiRateLimitNum, common.GlobalApiRateLimitDuration, "GA")
	}
	return defNext
}

func CriticalRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return rateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

// CriticalUserRateLimit 是 CriticalRateLimit 的**按认证用户计桶**版本，用于**已过 UserAuth**的
// money 端点（购买/充值/兑换/提现）。CriticalRateLimit 按 c.ClientIP() 计桶，而 gin 默认信任
// 0.0.0.0/0（全仓未 SetTrustedProxies）→ ClientIP() 取攻击者自填的最左 X-Forwarded-For → 登录态
// 攻击者每请求换一个 XFF 即落进全新桶，闸门失效（audit 2026-07-17 · High）。改按 user_id 计桶后
// 与伪造 XFF 完全无关。**必须挂在 UserAuth 之后**（无 user id 时 fail-closed 返回 401）。
// 复用 CriticalRateLimit 的开关与额度（CRITICAL_RATE_LIMIT*），语义与上游同类「印钞端点」对齐。
func CriticalUserRateLimit() func(c *gin.Context) {
	if common.CriticalRateLimitEnable {
		return userRateLimitFactory(common.CriticalRateLimitNum, common.CriticalRateLimitDuration, "CT")
	}
	return defNext
}

func DownloadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.DownloadRateLimitNum, common.DownloadRateLimitDuration, "DW")
}

func UploadRateLimit() func(c *gin.Context) {
	return rateLimitFactory(common.UploadRateLimitNum, common.UploadRateLimitDuration, "UP")
}

// userRateLimitFactory creates a rate limiter keyed by authenticated user ID
// instead of client IP, making it resistant to proxy rotation attacks.
// Must be used AFTER authentication middleware (UserAuth).
func userRateLimitFactory(maxRequestNum int, duration int64, mark string) func(c *gin.Context) {
	if common.RedisEnabled {
		return func(c *gin.Context) {
			userId := c.GetInt("id")
			if userId == 0 {
				c.Status(http.StatusUnauthorized)
				c.Abort()
				return
			}
			key := fmt.Sprintf("rateLimit:%s:user:%d", mark, userId)
			userRedisRateLimiter(c, maxRequestNum, duration, key)
		}
	}
	// It's safe to call multi times.
	inMemoryRateLimiter.Init(common.RateLimitKeyExpirationDuration)
	return func(c *gin.Context) {
		userId := c.GetInt("id")
		if userId == 0 {
			c.Status(http.StatusUnauthorized)
			c.Abort()
			return
		}
		key := fmt.Sprintf("%s:user:%d", mark, userId)
		if !inMemoryRateLimiter.Request(key, maxRequestNum, duration) {
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		}
	}
}

// userRedisRateLimiter is like redisRateLimiter but accepts a pre-built key
// (to support user-ID-based keys).
func userRedisRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	redisSlidingWindowRateLimiter(c, maxRequestNum, duration, key)
}

func redisSlidingWindowRateLimiter(c *gin.Context, maxRequestNum int, duration int64, key string) {
	_, allowed, err := reserveRedisSlidingWindow(c.Request.Context(), key, maxRequestNum, duration)
	if err != nil {
		fmt.Println(err.Error())
		c.Status(http.StatusInternalServerError)
		c.Abort()
		return
	}
	if !allowed {
		c.Status(http.StatusTooManyRequests)
		c.Abort()
	}
}

// reserveRedisSlidingWindow atomically consumes one list-backed slot and
// returns the exact legacy-format timestamp stored in Redis. Callers that are
// reserving capacity for an outcome-dependent limit may release that value if
// the request later fails. Keeping the value as the historical timestamp
// format preserves mixed-version compatibility with older readers.
func reserveRedisSlidingWindow(ctx context.Context, key string, maxRequestNum int, duration int64) (string, bool, error) {
	rdb := common.RDB
	if rdb == nil || maxRequestNum <= 0 || duration <= 0 || common.RateLimitKeyExpirationDuration <= 0 {
		return "", false, fmt.Errorf("Redis rate limiter configuration or client is invalid")
	}
	if duration > int64((time.Duration(1<<63-1))/time.Second) {
		return "", false, fmt.Errorf("Redis rate limiter window is too large")
	}
	windowDuration := time.Duration(duration) * time.Second
	keyExpiration := common.RateLimitKeyExpirationDuration
	if keyExpiration < windowDuration {
		// The bucket must outlive the configured sliding window. Otherwise a
		// quiet period equal to the shorter global TTL would erase still-live
		// requests and restore capacity earlier than the endpoint promises.
		keyExpiration = windowDuration
	}

	// Keep the historical local-wall-clock encoding during rolling upgrades.
	// Existing keys were written with time.Now().Format(timeFormat), whose
	// layout contains a literal Z despite the configured Asia/Shanghai zone.
	// Switching those keys to UTC would make old entries appear eight hours in
	// the future and repeated 429s would continuously extend their TTL.
	now := time.Now()
	result, err := rdb.Eval(
		ctx,
		redisRateLimitScript,
		[]string{key},
		maxRequestNum,
		now.Format(timeFormat),
		now.Add(-windowDuration).Format(timeFormat),
		keyExpiration.Milliseconds(),
	).Int()
	if err != nil {
		return "", false, err
	}

	switch result {
	case 1:
		return now.Format(timeFormat), true, nil
	case 0:
		return "", false, nil
	default:
		return "", false, fmt.Errorf("unexpected Redis rate limiter result: %d", result)
	}
}

// SearchRateLimit returns a per-user rate limiter for search endpoints.
// Configurable via SEARCH_RATE_LIMIT_ENABLE / SEARCH_RATE_LIMIT / SEARCH_RATE_LIMIT_DURATION.
func SearchRateLimit() func(c *gin.Context) {
	if !common.SearchRateLimitEnable {
		return defNext
	}
	return userRateLimitFactory(common.SearchRateLimitNum, common.SearchRateLimitDuration, "SR")
}
