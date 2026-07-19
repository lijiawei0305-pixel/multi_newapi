package model

import (
	"fmt"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
)

func getTokenCacheKey(key string) string {
	return fmt.Sprintf("token:%s", common.GenerateHMAC(key))
}

func cacheFillToken(token Token, generation authCacheGenerationSnapshot) error {
	key := getTokenCacheKey(token.Key)
	token.Clean()
	return fillAuthCacheIfCurrent(key, generation, &token)
}

func cacheDeleteToken(key string) error {
	return invalidateAuthCache(getTokenCacheKey(key))
}

func cacheIncrTokenQuota(key string, increment int64) error {
	return incrementAuthCacheField(getTokenCacheKey(key), constant.TokenFiledRemainQuota, increment)
}

func cacheDecrTokenQuota(key string, decrement int64) error {
	return cacheIncrTokenQuota(key, -decrement)
}

// CacheGetTokenByKey 从缓存中获取 token，如果缓存中不存在，则从数据库中获取
func cacheGetTokenByKey(key string) (*Token, error) {
	if !common.RedisEnabled {
		return nil, fmt.Errorf("redis is not enabled")
	}
	var token Token
	err := common.RedisHGetObj(getTokenCacheKey(key), &token)
	if err != nil {
		return nil, err
	}
	token.Key = key
	return &token, nil
}
