package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/pkg/cachex"
	"github.com/go-redis/redis/v8"
	"github.com/samber/hot"
	"gorm.io/gorm"
)

const (
	subscriptionPlanCacheNamespace     = "new-api:subscription_plan:v1"
	subscriptionPlanInfoCacheNamespace = "new-api:subscription_plan_info:v2"
	subscriptionPlanGenerationPrefix   = "new-api:subscription_plan_generation:v1:"
	subscriptionPlanInfoGenerationKey  = "new-api:subscription_plan_info_generation:v2"
	subscriptionPlanCacheOpTimeout     = 2 * time.Second
)

var (
	subscriptionPlanCacheOnce     sync.Once
	subscriptionPlanInfoCacheOnce sync.Once

	subscriptionPlanCache     *cachex.HybridCache[SubscriptionPlan]
	subscriptionPlanInfoCache *cachex.HybridCache[SubscriptionPlanInfo]

	// Cache entries are addressed by a monotonically increasing generation.
	// A reader may finish an old database read after an administrator changes a
	// plan, but it can then only populate the old generation, which is no longer
	// reachable. The lock makes the same guarantee for the in-memory cache; Redis
	// stores the generations so the guarantee also spans multiple instances.
	subscriptionPlanCacheGenerationMu sync.RWMutex
	subscriptionPlanCacheGenerations  = make(map[int]uint64)
	subscriptionPlanInfoGeneration    uint64

	// subscriptionPlanCacheBeforeFillHook is a deterministic test seam for the
	// database-read/cache-fill race. Production leaves it nil.
	subscriptionPlanCacheBeforeFillHook func(int)
)

func subscriptionPlanCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_TTL", 300)
	if ttlSeconds <= 0 {
		ttlSeconds = 300
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanInfoCacheTTL() time.Duration {
	ttlSeconds := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_TTL", 120)
	if ttlSeconds <= 0 {
		ttlSeconds = 120
	}
	return time.Duration(ttlSeconds) * time.Second
}

func subscriptionPlanCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_CACHE_CAP", 5000)
	if capacity <= 0 {
		capacity = 5000
	}
	return capacity
}

func subscriptionPlanInfoCacheCapacity() int {
	capacity := common.GetEnvOrDefault("SUBSCRIPTION_PLAN_INFO_CACHE_CAP", 10000)
	if capacity <= 0 {
		capacity = 10000
	}
	return capacity
}

func getSubscriptionPlanCache() *cachex.HybridCache[SubscriptionPlan] {
	subscriptionPlanCacheOnce.Do(func() {
		ttl := subscriptionPlanCacheTTL()
		subscriptionPlanCache = cachex.NewHybridCache[SubscriptionPlan](cachex.HybridCacheConfig[SubscriptionPlan]{
			Namespace: cachex.Namespace(subscriptionPlanCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlan]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlan] {
				return hot.NewHotCache[string, SubscriptionPlan](hot.LRU, subscriptionPlanCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanCache
}

func getSubscriptionPlanInfoCache() *cachex.HybridCache[SubscriptionPlanInfo] {
	subscriptionPlanInfoCacheOnce.Do(func() {
		ttl := subscriptionPlanInfoCacheTTL()
		subscriptionPlanInfoCache = cachex.NewHybridCache[SubscriptionPlanInfo](cachex.HybridCacheConfig[SubscriptionPlanInfo]{
			Namespace: cachex.Namespace(subscriptionPlanInfoCacheNamespace),
			Redis:     common.RDB,
			RedisEnabled: func() bool {
				return common.RedisEnabled && common.RDB != nil
			},
			RedisCodec: cachex.JSONCodec[SubscriptionPlanInfo]{},
			Memory: func() *hot.HotCache[string, SubscriptionPlanInfo] {
				return hot.NewHotCache[string, SubscriptionPlanInfo](hot.LRU, subscriptionPlanInfoCacheCapacity()).
					WithTTL(ttl).
					WithJanitor().
					Build()
			},
		})
	})
	return subscriptionPlanInfoCache
}

func subscriptionPlanCacheKey(id int) string {
	if id <= 0 {
		return ""
	}
	return strconv.Itoa(id)
}

func subscriptionPlanVersionedCacheKey(id int, generation uint64) string {
	return fmt.Sprintf("%s:g%d", subscriptionPlanCacheKey(id), generation)
}

func subscriptionPlanInfoVersionedCacheKey(userSubscriptionId int, generation uint64) string {
	return fmt.Sprintf("g%d:sub:%d", generation, userSubscriptionId)
}

func subscriptionPlanGenerationKey(id int) string {
	return subscriptionPlanGenerationPrefix + strconv.Itoa(id)
}

// The generation readers must be called while holding
// subscriptionPlanCacheGenerationMu for reading or writing.
func currentSubscriptionPlanCacheGenerationLocked(planId int) (uint64, error) {
	if !common.RedisEnabled {
		return subscriptionPlanCacheGenerations[planId], nil
	}
	if common.RDB == nil {
		return 0, errors.New("Redis is enabled but the client is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionPlanCacheOpTimeout)
	defer cancel()
	generation, err := common.RDB.Get(ctx, subscriptionPlanGenerationKey(planId)).Uint64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read subscription plan cache generation: %w", err)
	}
	return generation, nil
}

func currentSubscriptionPlanInfoCacheGenerationLocked() (uint64, error) {
	if !common.RedisEnabled {
		return subscriptionPlanInfoGeneration, nil
	}
	if common.RDB == nil {
		return 0, errors.New("Redis is enabled but the client is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionPlanCacheOpTimeout)
	defer cancel()
	generation, err := common.RDB.Get(ctx, subscriptionPlanInfoGenerationKey).Uint64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read subscription plan info cache generation: %w", err)
	}
	return generation, nil
}

// InvalidateSubscriptionPlanCache advances tombstone generations instead of
// merely deleting cache entries. Deletion alone is racy: a cache-miss reader
// can read the old database row, observe the deletion, and refill stale data.
// Callers must handle the returned error because a failed Redis increment means
// other instances may still consider the old generation current.
func InvalidateSubscriptionPlanCache(planId int) error {
	if planId <= 0 {
		return errors.New("invalid subscription plan id")
	}
	subscriptionPlanCacheGenerationMu.Lock()
	defer subscriptionPlanCacheGenerationMu.Unlock()
	if !common.RedisEnabled {
		subscriptionPlanCacheGenerations[planId]++
		subscriptionPlanInfoGeneration++
		return nil
	}
	if common.RDB == nil {
		return errors.New("invalidate subscription plan cache: Redis is enabled but the client is not initialized")
	}
	ctx, cancel := context.WithTimeout(context.Background(), subscriptionPlanCacheOpTimeout)
	defer cancel()
	if _, err := common.RDB.TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		pipe.Incr(ctx, subscriptionPlanGenerationKey(planId))
		pipe.Incr(ctx, subscriptionPlanInfoGenerationKey)
		return nil
	}); err != nil {
		return fmt.Errorf("invalidate subscription plan cache: %w", err)
	}
	return nil
}

func GetSubscriptionPlanById(id int) (*SubscriptionPlan, error) {
	return getSubscriptionPlanByIdTx(nil, id)
}

func getSubscriptionPlanByIdTx(tx *gorm.DB, id int) (*SubscriptionPlan, error) {
	if id <= 0 {
		return nil, errors.New("invalid plan id")
	}
	if tx != nil {
		var plan SubscriptionPlan
		if err := tx.Where("id = ?", id).First(&plan).Error; err != nil {
			return nil, err
		}
		plan.NormalizeDefaults()
		return &plan, nil
	}

	// Capture the generation before touching the database. Holding the read lock
	// makes cache hits linearizable with an in-process invalidation. Redis-backed
	// hits are checked against the generation a second time so another instance's
	// invalidation cannot leave a stale hit looking current.
	subscriptionPlanCacheGenerationMu.RLock()
	generation, generationErr := currentSubscriptionPlanCacheGenerationLocked(id)
	if generationErr == nil {
		key := subscriptionPlanVersionedCacheKey(id, generation)
		cached, found, cacheErr := getSubscriptionPlanCache().Get(key)
		if cacheErr == nil && found {
			currentGeneration := generation
			if common.RedisEnabled {
				currentGeneration, generationErr = currentSubscriptionPlanCacheGenerationLocked(id)
			}
			if generationErr == nil && currentGeneration == generation {
				subscriptionPlanCacheGenerationMu.RUnlock()
				cached.NormalizeDefaults()
				return &cached, nil
			}
		}
	}
	subscriptionPlanCacheGenerationMu.RUnlock()

	var plan SubscriptionPlan
	if err := DB.Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	plan.NormalizeDefaults()
	if subscriptionPlanCacheBeforeFillHook != nil {
		subscriptionPlanCacheBeforeFillHook(id)
	}

	// Only the generation captured before the database read may be populated. If
	// it changed, an administrator invalidated this read and the result must not
	// be inserted into the current cache generation.
	if generationErr == nil {
		subscriptionPlanCacheGenerationMu.RLock()
		currentGeneration, currentErr := currentSubscriptionPlanCacheGenerationLocked(id)
		if currentErr == nil && currentGeneration == generation {
			key := subscriptionPlanVersionedCacheKey(id, generation)
			if err := getSubscriptionPlanCache().SetWithTTL(key, plan, subscriptionPlanCacheTTL()); err != nil {
				common.SysLog("failed to populate subscription plan cache: " + err.Error())
			}
		}
		subscriptionPlanCacheGenerationMu.RUnlock()
	}
	return &plan, nil
}

func getSubscriptionPlanByIdForUpdateTx(tx *gorm.DB, id int) (*SubscriptionPlan, error) {
	if tx == nil {
		return nil, errors.New("tx is nil")
	}
	if id <= 0 {
		return nil, errors.New("invalid plan id")
	}
	var plan SubscriptionPlan
	if err := lockForUpdate(tx).Where("id = ?", id).First(&plan).Error; err != nil {
		return nil, err
	}
	plan.NormalizeDefaults()
	return &plan, nil
}

type SubscriptionPlanInfo struct {
	PlanId    int
	PlanTitle string
}

func GetSubscriptionPlanInfoByUserSubscriptionId(userSubscriptionId int) (*SubscriptionPlanInfo, error) {
	if userSubscriptionId <= 0 {
		return nil, errors.New("invalid userSubscriptionId")
	}

	subscriptionPlanCacheGenerationMu.RLock()
	infoGeneration, generationErr := currentSubscriptionPlanInfoCacheGenerationLocked()
	if generationErr == nil {
		cacheKey := subscriptionPlanInfoVersionedCacheKey(userSubscriptionId, infoGeneration)
		cached, found, cacheErr := getSubscriptionPlanInfoCache().Get(cacheKey)
		if cacheErr == nil && found {
			currentInfoGeneration := infoGeneration
			if common.RedisEnabled {
				currentInfoGeneration, generationErr = currentSubscriptionPlanInfoCacheGenerationLocked()
			}
			if generationErr == nil && currentInfoGeneration == infoGeneration {
				subscriptionPlanCacheGenerationMu.RUnlock()
				return &cached, nil
			}
		}
	}
	subscriptionPlanCacheGenerationMu.RUnlock()

	var sub UserSubscription
	if err := DB.Where("id = ?", userSubscriptionId).First(&sub).Error; err != nil {
		return nil, err
	}
	planTitle := sub.PlanTitle
	if strings.TrimSpace(planTitle) == "" && sub.BenefitSnapshotVersion == 0 {
		// This fallback is only for rows created before benefit snapshots existed.
		// Startup migration fills all legacy rows whose catalog plan still exists.
		plan, err := getSubscriptionPlanByIdTx(nil, sub.PlanId)
		if err != nil {
			return nil, err
		}
		planTitle = plan.Title
	}
	info := &SubscriptionPlanInfo{
		PlanId:    sub.PlanId,
		PlanTitle: planTitle,
	}
	if generationErr == nil {
		subscriptionPlanCacheGenerationMu.RLock()
		currentInfoGeneration, currentErr := currentSubscriptionPlanInfoCacheGenerationLocked()
		if currentErr == nil && currentInfoGeneration == infoGeneration {
			cacheKey := subscriptionPlanInfoVersionedCacheKey(userSubscriptionId, infoGeneration)
			if err := getSubscriptionPlanInfoCache().SetWithTTL(cacheKey, *info, subscriptionPlanInfoCacheTTL()); err != nil {
				common.SysLog("failed to populate subscription plan info cache: " + err.Error())
			}
		}
		subscriptionPlanCacheGenerationMu.RUnlock()
	}
	return info, nil
}
