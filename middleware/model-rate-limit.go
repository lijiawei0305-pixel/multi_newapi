package middleware

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/common/limiter"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/setting"

	"github.com/gin-gonic/gin"
)

const (
	ModelRequestRateLimitCountMark        = "MRRL"
	ModelRequestRateLimitSuccessCountMark = "MRRLS"
)

type modelSuccessMemoryBucket struct {
	reservations []int64
	expiresAt    int64
}

// modelSuccessMemoryReservations gives the in-memory path the same
// reserve-before-dispatch/release-on-failure semantics as Redis. The previous
// *_check bucket counted every attempt and never consulted the actual success
// bucket, so its behavior diverged from both the option name and Redis.
type modelSuccessMemoryReservationStore struct {
	mu          sync.Mutex
	buckets     map[string]modelSuccessMemoryBucket
	lastCleanup int64
}

var modelSuccessMemoryReservations modelSuccessMemoryReservationStore

func (s *modelSuccessMemoryReservationStore) reserve(key string, maxCount int, duration time.Duration) (int64, bool) {
	now := time.Now().UnixMilli()
	durationMillis := duration.Milliseconds()
	cutoff := now - durationMillis

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.buckets == nil {
		s.buckets = make(map[string]modelSuccessMemoryBucket)
	}
	if s.lastCleanup == 0 || now-s.lastCleanup >= time.Minute.Milliseconds() {
		for bucketKey, bucket := range s.buckets {
			if bucket.expiresAt <= now {
				delete(s.buckets, bucketKey)
			}
		}
		s.lastCleanup = now
	}

	bucket := s.buckets[key]
	firstLive := 0
	for firstLive < len(bucket.reservations) && bucket.reservations[firstLive] <= cutoff {
		firstLive++
	}
	if firstLive > 0 {
		bucket.reservations = append([]int64(nil), bucket.reservations[firstLive:]...)
	}
	if len(bucket.reservations) >= maxCount {
		bucket.expiresAt = bucket.reservations[len(bucket.reservations)-1] + durationMillis
		s.buckets[key] = bucket
		return 0, false
	}
	bucket.reservations = append(bucket.reservations, now)
	bucket.expiresAt = now + durationMillis
	s.buckets[key] = bucket
	return now, true
}

func (s *modelSuccessMemoryReservationStore) release(key string, reservation int64, duration time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	bucket, ok := s.buckets[key]
	if !ok {
		return
	}
	for index := len(bucket.reservations) - 1; index >= 0; index-- {
		if bucket.reservations[index] != reservation {
			continue
		}
		bucket.reservations = append(bucket.reservations[:index], bucket.reservations[index+1:]...)
		if len(bucket.reservations) == 0 {
			delete(s.buckets, key)
		} else {
			bucket.expiresAt = bucket.reservations[len(bucket.reservations)-1] + duration.Milliseconds()
			s.buckets[key] = bucket
		}
		return
	}
}

// Redis限流处理器
func redisRateLimitHandler(durationMinutes, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		const maxInt64 = int64(^uint64(0) >> 1)
		if durationMinutes <= 0 || int64(durationMinutes) > maxInt64/60 || totalMaxCount < 0 || successMaxCount < 0 {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_config_invalid")
			return
		}
		duration := int64(durationMinutes) * 60
		if totalMaxCount == 0 && successMaxCount == 0 {
			c.Next()
			return
		}
		if duration <= 0 || common.RDB == nil {
			abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_config_invalid")
			return
		}
		userId := strconv.Itoa(c.GetInt("id"))
		ctx := c.Request.Context()
		rdb := common.RDB

		// 1. 检查总请求数限制并记录总请求（0 表示不限制）。
		if totalMaxCount > 0 {
			if int64(totalMaxCount) > maxInt64/duration {
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_config_invalid")
				return
			}
			totalKey := fmt.Sprintf("rateLimit:%s", userId)
			tb := limiter.New(ctx, rdb)
			allowed, err := tb.Allow(
				ctx,
				totalKey,
				limiter.WithCapacity(int64(totalMaxCount)*duration),
				limiter.WithRate(int64(totalMaxCount)),
				limiter.WithRequested(duration),
			)

			if err != nil {
				fmt.Println("检查总请求数限制失败:", err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}

			if !allowed {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到总请求数限制：%d分钟内最多请求%d次，包括失败次数，请检查您的请求是否正确", durationMinutes, totalMaxCount))
				return
			}
		}

		// 2. 成功数必须在进入下游前原子预占，否则空桶并发请求会全部
		// 通过。失败响应释放同一时间戳；成功响应保留预占作为计数。
		successKey := fmt.Sprintf("rateLimit:%s:%s", ModelRequestRateLimitSuccessCountMark, userId)
		reservation := ""
		if successMaxCount > 0 {
			var allowed bool
			var err error
			reservation, allowed, err = reserveRedisSlidingWindow(ctx, successKey, successMaxCount, duration)
			if err != nil {
				fmt.Println("检查成功请求数限制失败:", err.Error())
				abortWithOpenAiMessage(c, http.StatusInternalServerError, "rate_limit_check_failed")
				return
			}
			if !allowed {
				abortWithOpenAiMessage(c, http.StatusTooManyRequests, fmt.Sprintf("您已达到请求数限制：%d分钟内最多请求%d次", durationMinutes, successMaxCount))
				return
			}
		}

		completed := false
		defer func() {
			if reservation == "" || (completed && c.Writer.Status() < http.StatusBadRequest) {
				return
			}
			releaseCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := rdb.LRem(releaseCtx, successKey, 1, reservation).Err(); err != nil {
				fmt.Println("释放失败请求的成功限流预占失败:", err.Error())
			}
		}()

		c.Next()
		completed = true
	}
}

// 内存限流处理器
func memoryRateLimitHandler(durationMinutes, totalMaxCount, successMaxCount int) gin.HandlerFunc {
	return func(c *gin.Context) {
		const maxInt64 = int64(^uint64(0) >> 1)
		if durationMinutes <= 0 || int64(durationMinutes) > maxInt64/60 || totalMaxCount < 0 || successMaxCount < 0 {
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
		duration := int64(durationMinutes) * 60
		if duration <= 0 || duration > int64((time.Duration(1<<63-1))/time.Second) {
			c.Status(http.StatusInternalServerError)
			c.Abort()
			return
		}
		window := time.Duration(duration) * time.Second
		inMemoryRateLimiter.Init(window)

		userId := strconv.Itoa(c.GetInt("id"))
		totalKey := ModelRequestRateLimitCountMark + userId
		successKey := ModelRequestRateLimitSuccessCountMark + userId

		// 1. 检查总请求数限制（当totalMaxCount为0时跳过）
		if totalMaxCount > 0 && !inMemoryRateLimiter.Request(totalKey, totalMaxCount, duration) {
			c.Status(http.StatusTooManyRequests)
			c.Abort()
			return
		}

		// 2. 与 Redis 相同：成功数先预占，失败时精确释放。
		reservation := int64(0)
		if successMaxCount > 0 {
			var allowed bool
			reservation, allowed = modelSuccessMemoryReservations.reserve(successKey, successMaxCount, window)
			if !allowed {
				c.Status(http.StatusTooManyRequests)
				c.Abort()
				return
			}
		}

		completed := false
		defer func() {
			if reservation == 0 || (completed && c.Writer.Status() < http.StatusBadRequest) {
				return
			}
			modelSuccessMemoryReservations.release(successKey, reservation, window)
		}()

		// 3. 处理请求；成功保留预占，失败或 panic 释放。
		c.Next()
		completed = true
	}
}

// ModelRequestRateLimit 模型请求限流中间件
func ModelRequestRateLimit() func(c *gin.Context) {
	return func(c *gin.Context) {
		// 在每个请求时检查是否启用限流
		config := setting.GetModelRequestRateLimitConfig()
		if !config.Enabled {
			c.Next()
			return
		}

		// 本请求的标量与分组规则必须来自同一个不可变快照。
		totalMaxCount := config.Count
		successMaxCount := config.SuccessCount

		// 获取分组
		group := common.GetContextKeyString(c, constant.ContextKeyTokenGroup)
		if group == "" {
			group = common.GetContextKeyString(c, constant.ContextKeyUserGroup)
		}

		//获取分组的限流配置
		groupTotalCount, groupSuccessCount, found := config.GroupLimit(group)
		if found {
			totalMaxCount = groupTotalCount
			successMaxCount = groupSuccessCount
		}

		// 根据存储类型选择并执行限流处理器
		if common.RedisEnabled {
			redisRateLimitHandler(config.DurationMinutes, totalMaxCount, successMaxCount)(c)
		} else {
			memoryRateLimitHandler(config.DurationMinutes, totalMaxCount, successMaxCount)(c)
		}
	}
}
