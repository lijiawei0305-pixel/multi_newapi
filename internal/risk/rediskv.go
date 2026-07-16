package risk

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
)

// RedisKVCache 用 go-redis/v8 实现 KVCache：限流计数与限购去重走 Redis 原子命令
// （INCR/SETNX/EXPIRE），保证多副本下固定窗口计数一致、限购单赢家。
// 与内存假实现 MemKVCache 满足同一契约（port.go），由 mtwire 在 Redis 启用时注入替换。
type RedisKVCache struct {
	rdb *redis.Client
}

// NewRedisKVCache 用已连接的 *redis.Client（common.RDB）构造。
func NewRedisKVCache(rdb *redis.Client) *RedisKVCache { return &RedisKVCache{rdb: rdb} }

var _ KVCache = (*RedisKVCache)(nil)

// Incr 原子自增并返回新值（key 不存在视为 0→1）。
func (r *RedisKVCache) Incr(ctx context.Context, key string) (int64, error) {
	return r.rdb.Incr(ctx, key).Result()
}

// Get 读取值；redis.Nil（不存在/已过期）映射为 found=false（不视为错误）。
func (r *RedisKVCache) Get(ctx context.Context, key string) (string, bool, error) {
	v, err := r.rdb.Get(ctx, key).Result()
	if err == redis.Nil {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return v, true, nil
}

// SetNX 仅当 key 不存在时写入。ttl<=0 → 不过期（go-redis 以 0 duration 表示无 TTL）。
func (r *RedisKVCache) SetNX(ctx context.Context, key, val string, ttl time.Duration) (bool, error) {
	if ttl < 0 {
		ttl = 0
	}
	return r.rdb.SetNX(ctx, key, val, ttl).Result()
}

// Expire 为已存在的 key 设过期；ttl<=0 视为不设置（与 KVCache 契约一致）。
func (r *RedisKVCache) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if ttl <= 0 {
		return nil
	}
	return r.rdb.Expire(ctx, key, ttl).Err()
}

// Del 删除给定 key（DEL 幂等：不存在的 key 计 0 不报错）。空列表 no-op（go-redis 空 keys 会 panic，故短路）。
func (r *RedisKVCache) Del(ctx context.Context, keys ...string) error {
	if len(keys) == 0 {
		return nil
	}
	return r.rdb.Del(ctx, keys...).Err()
}
