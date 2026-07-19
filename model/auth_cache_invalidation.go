package model

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
)

const (
	authCacheInvalidationPending = "pending"
	authCacheInvalidationApplied = "applied"
	authCacheInvalidationBatch   = 100
)

// AuthCacheInvalidation is the durable half of the authorization revocation
// protocol. The database mutation and this pending row commit together. Redis
// is fenced before that commit, and this row lets any instance finish releasing
// the fence after a crash or a transient post-commit Redis failure.
type AuthCacheInvalidation struct {
	Id           int    `json:"id"`
	CacheKey     string `json:"cache_key" gorm:"type:varchar(191);not null;index"`
	AuthVersion  int64  `json:"auth_version" gorm:"type:bigint;not null"`
	Status       string `json:"status" gorm:"type:varchar(16);not null;index"`
	AttemptCount int    `json:"attempt_count" gorm:"not null"`
	LastError    string `json:"last_error" gorm:"type:varchar(512)"`
	CreatedAt    int64  `json:"created_at" gorm:"type:bigint;not null"`
	UpdatedAt    int64  `json:"updated_at" gorm:"type:bigint;not null;index"`
}

var (
	authCacheFenceHookMu sync.RWMutex
	authCacheFenceHook   func(string)
)

func authCacheFenceKey(cacheKey string) string {
	return "auth-cache:fence:" + common.GenerateHMAC(cacheKey)
}

func authCacheFenceTTL() time.Duration {
	cacheTTL := time.Duration(common.RedisKeyCacheSeconds()) * time.Second
	if cacheTTL < time.Minute {
		cacheTTL = time.Minute
	}
	return 2*cacheTTL + time.Minute
}

func authCacheFenceToken(row *AuthCacheInvalidation) string {
	return strconv.Itoa(row.Id) + ":" + strconv.FormatInt(row.AuthVersion, 10)
}

func isAuthCacheFenced(cacheKey string) (bool, error) {
	if !common.RedisEnabled {
		return false, nil
	}
	if common.RDB == nil {
		return false, errors.New("Redis is enabled but the client is not initialized")
	}
	count, err := common.RDB.Exists(context.Background(), authCacheFenceKey(cacheKey)).Result()
	if err != nil {
		return false, err
	}
	return count != 0, nil
}

func prepareAuthCacheInvalidationTx(tx *gorm.DB, cacheKey string, authVersion int64) error {
	now := common.GetTimestamp()
	row := &AuthCacheInvalidation{
		CacheKey:    cacheKey,
		AuthVersion: authVersion,
		Status:      authCacheInvalidationPending,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := tx.Create(row).Error; err != nil {
		return err
	}
	if !common.RedisEnabled {
		// With Redis disabled, authorization reads use the database. Keep the
		// row pending so a future Redis-enabled start clears any old cache first.
		return nil
	}
	if err := common.RedisFenceVersionedHash(
		cacheKey,
		authCacheGenerationKey(cacheKey),
		authCacheFenceKey(cacheKey),
		authCacheFenceToken(row),
		authCacheFenceTTL(),
	); err != nil {
		return fmt.Errorf("establish authorization cache fence: %w", err)
	}

	authCacheFenceHookMu.RLock()
	hook := authCacheFenceHook
	authCacheFenceHookMu.RUnlock()
	if hook != nil {
		hook(cacheKey)
	}
	return nil
}

func prepareUserAuthCacheInvalidationTx(tx *gorm.DB, userId int) error {
	result := tx.Unscoped().Model(&User{}).Where("id = ?", userId).
		UpdateColumn("auth_version", gorm.Expr("auth_version + ?", 1))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	var authVersion int64
	if err := tx.Unscoped().Model(&User{}).Where("id = ?", userId).
		Select("auth_version").Scan(&authVersion).Error; err != nil {
		return err
	}
	return prepareAuthCacheInvalidationTx(tx, getUserCacheKey(userId), authVersion)
}

func prepareTokenAuthCacheInvalidationTx(tx *gorm.DB, token *Token) error {
	if token == nil || token.Id <= 0 || token.Key == "" {
		return errors.New("token identity is incomplete for authorization cache invalidation")
	}
	result := tx.Unscoped().Model(&Token{}).Where("id = ?", token.Id).
		UpdateColumn("auth_version", gorm.Expr("auth_version + ?", 1))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	if err := tx.Unscoped().Model(&Token{}).Where("id = ?", token.Id).
		Select("auth_version").Scan(&token.AuthVersion).Error; err != nil {
		return err
	}
	return prepareAuthCacheInvalidationTx(tx, getTokenCacheKey(token.Key), token.AuthVersion)
}

// ApplyAuthCacheInvalidation repeats the generation advance and hash deletion
// before releasing the matching fence. It is safe for multiple instances and
// repeated delivery: an older event cannot remove a newer event's fence.
func ApplyAuthCacheInvalidation(id int) error {
	if !common.RedisEnabled {
		return nil
	}
	var row AuthCacheInvalidation
	if err := DB.First(&row, id).Error; err != nil {
		return err
	}
	if row.Status == authCacheInvalidationApplied {
		return nil
	}
	if row.Status != authCacheInvalidationPending {
		return fmt.Errorf("authorization cache invalidation %d has invalid status %q", row.Id, row.Status)
	}

	if err := common.RedisReleaseVersionedHashFence(
		row.CacheKey,
		authCacheGenerationKey(row.CacheKey),
		authCacheFenceKey(row.CacheKey),
		authCacheFenceToken(&row),
	); err != nil {
		lastError := err.Error()
		if len(lastError) > 512 {
			lastError = lastError[:512]
		}
		_ = DB.Model(&AuthCacheInvalidation{}).
			Where("id = ? AND status = ?", row.Id, authCacheInvalidationPending).
			Updates(map[string]interface{}{
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"last_error":    lastError,
				"updated_at":    common.GetTimestamp(),
			}).Error
		return err
	}

	result := DB.Model(&AuthCacheInvalidation{}).
		Where("id = ? AND status = ?", row.Id, authCacheInvalidationPending).
		Updates(map[string]interface{}{
			"status":        authCacheInvalidationApplied,
			"attempt_count": gorm.Expr("attempt_count + 1"),
			"last_error":    "",
			"updated_at":    common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var current AuthCacheInvalidation
		if err := DB.Select("status").First(&current, row.Id).Error; err != nil {
			return err
		}
		if current.Status != authCacheInvalidationApplied {
			return errors.New("authorization cache invalidation state changed concurrently")
		}
	}
	return nil
}

func flushAuthCacheInvalidations(cacheKey string) error {
	if !common.RedisEnabled {
		return nil
	}
	var rows []AuthCacheInvalidation
	if err := DB.Where("cache_key = ? AND status = ?", cacheKey, authCacheInvalidationPending).
		Order("id ASC").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return invalidateAuthCache(cacheKey)
	}
	for _, row := range rows {
		if err := ApplyAuthCacheInvalidation(row.Id); err != nil {
			return err
		}
	}
	return nil
}

// ReconcilePendingAuthCacheInvalidations replays durable invalidations left by
// a process crash, a Redis-disabled period, or a transient post-commit failure.
func ReconcilePendingAuthCacheInvalidations(limit int) (int, error) {
	if !common.RedisEnabled {
		return 0, nil
	}
	if limit <= 0 {
		limit = authCacheInvalidationBatch
	}
	var rows []AuthCacheInvalidation
	if err := DB.Where("status = ?", authCacheInvalidationPending).
		Order("id ASC").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for _, row := range rows {
		if err := ApplyAuthCacheInvalidation(row.Id); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

// InitAuthCacheInvalidationReconciler must run after both the main database and
// Redis are initialized. The synchronous first pass prevents a restarted
// instance from serving against Redis before committed revocations are replayed.
func InitAuthCacheInvalidationReconciler() error {
	if !common.RedisEnabled {
		return nil
	}
	for {
		applied, err := ReconcilePendingAuthCacheInvalidations(authCacheInvalidationBatch)
		if err != nil {
			return err
		}
		if applied < authCacheInvalidationBatch {
			break
		}
	}
	gopool.Go(func() {
		ticker := time.NewTicker(time.Second)
		defer ticker.Stop()
		for range ticker.C {
			if _, err := ReconcilePendingAuthCacheInvalidations(authCacheInvalidationBatch); err != nil {
				common.SysLog("failed to reconcile authorization cache invalidations: " + err.Error())
			}
		}
	})
	return nil
}

func setAuthCacheFenceHookForTest(hook func(string)) func() {
	authCacheFenceHookMu.Lock()
	previous := authCacheFenceHook
	authCacheFenceHook = hook
	authCacheFenceHookMu.Unlock()
	return func() {
		authCacheFenceHookMu.Lock()
		authCacheFenceHook = previous
		authCacheFenceHookMu.Unlock()
	}
}
