package model

import (
	"fmt"
	"hash/fnv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
)

const authCacheGenerationShardCount = 256

type authCacheGenerationSnapshot struct {
	key   string
	value int64
	valid bool
}

var (
	authCacheFillHookMu sync.RWMutex
	authCacheFillHook   func(string, bool)
	authCacheFillWG     sync.WaitGroup
)

// authCacheGenerationKey maps every authentication cache key to one of a
// bounded set of generation counters. Invalidations on the same shard can
// conservatively discard an unrelated in-flight fill, but can never allow a
// stale fill. The fixed shard set avoids one permanent Redis tombstone per
// historical user or token.
func authCacheGenerationKey(cacheKey string) string {
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(cacheKey))
	shard := hasher.Sum32() % authCacheGenerationShardCount
	return fmt.Sprintf("auth-cache:generation:%03d", shard)
}

func snapshotAuthCacheGeneration(cacheKey string) (authCacheGenerationSnapshot, error) {
	if !common.RedisEnabled {
		return authCacheGenerationSnapshot{}, nil
	}
	generationKey := authCacheGenerationKey(cacheKey)
	generation, err := common.RedisGetVersionedHashGeneration(generationKey)
	if err != nil {
		return authCacheGenerationSnapshot{}, err
	}
	return authCacheGenerationSnapshot{key: generationKey, value: generation, valid: true}, nil
}

func fillAuthCacheIfCurrent(cacheKey string, snapshot authCacheGenerationSnapshot, obj interface{}) error {
	if !common.RedisEnabled || !snapshot.valid {
		return nil
	}

	authCacheFillHookMu.RLock()
	hook := authCacheFillHook
	authCacheFillHookMu.RUnlock()
	if hook != nil {
		hook(cacheKey, true)
	}

	_, err := common.RedisHSetObjIfGeneration(
		cacheKey,
		snapshot.key,
		snapshot.value,
		obj,
		time.Duration(common.RedisKeyCacheSeconds())*time.Second,
	)
	if hook != nil {
		hook(cacheKey, false)
	}
	return err
}

func scheduleAuthCacheFill(fill func()) {
	authCacheFillWG.Add(1)
	gopool.Go(func() {
		defer authCacheFillWG.Done()
		fill()
	})
}

func waitForAuthCacheFillsForTest() {
	authCacheFillWG.Wait()
}

func invalidateAuthCache(cacheKey string) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisInvalidateVersionedHash(cacheKey, authCacheGenerationKey(cacheKey))
}

func incrementAuthCacheField(cacheKey string, field string, delta int64) error {
	if !common.RedisEnabled {
		return nil
	}
	return common.RedisHIncrByAndInvalidateVersionedHash(
		cacheKey,
		authCacheGenerationKey(cacheKey),
		field,
		delta,
	)
}

func setAuthCacheFillHookForTest(hook func(string, bool)) func() {
	authCacheFillHookMu.Lock()
	previous := authCacheFillHook
	authCacheFillHook = hook
	authCacheFillHookMu.Unlock()
	return func() {
		authCacheFillHookMu.Lock()
		authCacheFillHook = previous
		authCacheFillHookMu.Unlock()
	}
}
