package common

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
)

var redisVersionedHashFillScript = redis.NewScript(`
local generation = redis.call("GET", KEYS[1])
if not generation then
  generation = "0"
end
if generation ~= ARGV[1] then
  return 0
end
redis.call("HSET", KEYS[2], unpack(ARGV, 3))
local expiration = tonumber(ARGV[2])
if expiration and expiration > 0 then
  redis.call("PEXPIRE", KEYS[2], expiration)
end
return 1
`)

var redisVersionedHashInvalidateScript = redis.NewScript(`
redis.call("INCR", KEYS[1])
redis.call("DEL", KEYS[2])
return 1
`)

var redisVersionedHashIncrScript = redis.NewScript(`
redis.call("INCR", KEYS[1])
if redis.call("EXISTS", KEYS[2]) == 1 then
  redis.call("HINCRBY", KEYS[2], ARGV[1], ARGV[2])
end
return 1
`)

var redisVersionedHashFenceScript = redis.NewScript(`
redis.call("SET", KEYS[3], ARGV[1], "PX", ARGV[2])
redis.call("INCR", KEYS[1])
redis.call("DEL", KEYS[2])
return 1
`)

var redisVersionedHashReleaseFenceScript = redis.NewScript(`
local fence = redis.call("GET", KEYS[3])
if fence and fence ~= ARGV[1] then
  return 0
end
redis.call("INCR", KEYS[1])
redis.call("DEL", KEYS[2])
if fence == ARGV[1] then
  redis.call("DEL", KEYS[3])
end
return 1
`)

var redisVersionedHashFencedFillScript = redis.NewScript(`
if redis.call("EXISTS", KEYS[3]) == 1 then
  return 0
end
local generation = redis.call("GET", KEYS[1])
if not generation then
  generation = "0"
end
if generation ~= ARGV[1] then
  return 0
end
redis.call("HSET", KEYS[2], unpack(ARGV, 3))
local expiration = tonumber(ARGV[2])
if expiration and expiration > 0 then
  redis.call("PEXPIRE", KEYS[2], expiration)
end
return 1
`)

// RedisGetVersionedHashGeneration snapshots the generation used to guard a
// subsequent database-backed cache fill. A missing shard starts at generation
// zero. Callers must skip the fill when this function returns an error.
func RedisGetVersionedHashGeneration(generationKey string) (int64, error) {
	if RDB == nil {
		return 0, errors.New("Redis client is not initialized")
	}
	value, err := RDB.Get(context.Background(), generationKey).Result()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	generation, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid Redis cache generation: %w", err)
	}
	return generation, nil
}

// RedisHSetObjIfGeneration publishes a database result only when no cache
// invalidation sharing generationKey has happened since the caller's snapshot.
// The generation comparison and HSET are one Redis script, so separate service
// instances using the same Redis cannot resurrect a pre-mutation read.
func RedisHSetObjIfGeneration(
	key string,
	generationKey string,
	expectedGeneration int64,
	obj interface{},
	expiration time.Duration,
) (bool, error) {
	if RDB == nil {
		return false, errors.New("Redis client is not initialized")
	}
	data, err := redisHashData(obj)
	if err != nil {
		return false, err
	}
	if len(data) == 0 {
		return false, errors.New("cannot cache an empty Redis hash")
	}

	args := make([]interface{}, 0, 2+len(data)*2)
	args = append(args, strconv.FormatInt(expectedGeneration, 10), expiration.Milliseconds())
	for field, value := range data {
		args = append(args, field, value)
	}

	result, err := redisVersionedHashFillScript.Run(
		context.Background(),
		RDB,
		[]string{generationKey, key},
		args...,
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// RedisHSetObjIfGenerationUnlessFenced is the authorization-cache variant of
// RedisHSetObjIfGeneration. A durable revocation transaction establishes the
// fence before committing its database change. While that fence exists, no
// database read that raced with the transaction may republish an authorization
// hash, even if its generation snapshot was otherwise current.
func RedisHSetObjIfGenerationUnlessFenced(
	key string,
	generationKey string,
	fenceKey string,
	expectedGeneration int64,
	obj interface{},
	expiration time.Duration,
) (bool, error) {
	if RDB == nil {
		return false, errors.New("Redis client is not initialized")
	}
	data, err := redisHashData(obj)
	if err != nil {
		return false, err
	}
	if len(data) == 0 {
		return false, errors.New("cannot cache an empty Redis hash")
	}

	args := make([]interface{}, 0, 2+len(data)*2)
	args = append(args, strconv.FormatInt(expectedGeneration, 10), expiration.Milliseconds())
	for field, value := range data {
		args = append(args, field, value)
	}

	result, err := redisVersionedHashFencedFillScript.Run(
		context.Background(),
		RDB,
		[]string{generationKey, key, fenceKey},
		args...,
	).Int64()
	if err != nil {
		return false, err
	}
	return result == 1, nil
}

// RedisInvalidateVersionedHash advances the shared generation before deleting
// the cached value. Both operations are atomic in Redis.
func RedisInvalidateVersionedHash(key string, generationKey string) error {
	if RDB == nil {
		return errors.New("Redis client is not initialized")
	}
	return redisVersionedHashInvalidateScript.Run(
		context.Background(),
		RDB,
		[]string{generationKey, key},
	).Err()
}

// RedisFenceVersionedHash makes an authorization cache key unavailable before
// its database mutation commits. The fence, generation advance, and hash
// deletion are one Redis operation. The fence TTL must outlive the hash TTL so
// an abandoned fence can expire without allowing a pre-mutation value back in.
func RedisFenceVersionedHash(
	key string,
	generationKey string,
	fenceKey string,
	fenceToken string,
	fenceTTL time.Duration,
) error {
	if RDB == nil {
		return errors.New("Redis client is not initialized")
	}
	if fenceTTL <= 0 {
		return errors.New("Redis authorization fence TTL must be positive")
	}
	return redisVersionedHashFenceScript.Run(
		context.Background(),
		RDB,
		[]string{generationKey, key, fenceKey},
		fenceToken,
		fenceTTL.Milliseconds(),
	).Err()
}

// RedisReleaseVersionedHashFence performs the post-commit half of the
// authorization revocation protocol. A retry for an older transaction cannot
// clear a newer transaction's fence because the opaque fence token must match.
// If an abandoned fence has already expired, the generation/delete step is
// still repeated before the key becomes usable again.
func RedisReleaseVersionedHashFence(
	key string,
	generationKey string,
	fenceKey string,
	fenceToken string,
) error {
	if RDB == nil {
		return errors.New("Redis client is not initialized")
	}
	return redisVersionedHashReleaseFenceScript.Run(
		context.Background(),
		RDB,
		[]string{generationKey, key, fenceKey},
		fenceToken,
	).Err()
}

// RedisHIncrByAndInvalidateVersionedHash advances the generation and applies a
// quota delta to an existing cached hash in the same Redis operation. If the
// cache is cold, only the generation advances; the next guarded fill reloads
// the authoritative database value.
func RedisHIncrByAndInvalidateVersionedHash(
	key string,
	generationKey string,
	field string,
	delta int64,
) error {
	if RDB == nil {
		return errors.New("Redis client is not initialized")
	}
	return redisVersionedHashIncrScript.Run(
		context.Background(),
		RDB,
		[]string{generationKey, key},
		field,
		delta,
	).Err()
}
