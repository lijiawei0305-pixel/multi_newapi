package mtwire

// 用户缓存失效 durable outbox（PAY-CACHE-01 / PAY-TXN-01）。
//
// 入账 DB 事务提交后同步尝试 InvalidateUserCache；失败则依赖本表重试。
// 重复消费只做幂等删除缓存，不重复增加 quota。
//
// Phase F：next_attempt_at + claim token/lease + 有界退避 + poison 隔离 + 完成清理。

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
)

const (
	outboxMaxAttempts   = 20
	outboxClaimLease    = 30 * time.Second
	outboxDoneRetention = 7 * 24 * time.Hour
)

// cacheInvalidationOutboxRow 以 order_no 唯一，避免重复入队。
type cacheInvalidationOutboxRow struct {
	OrderNo       string     `gorm:"column:order_no;primaryKey;type:varchar(64)"`
	UserID        int64      `gorm:"column:user_id;not null;index"`
	CreatedAt     time.Time  `gorm:"column:created_at"`
	DoneAt        *time.Time `gorm:"column:done_at"`
	Attempts      int        `gorm:"column:attempts;not null;default:0"`
	NextAttemptAt *time.Time `gorm:"column:next_attempt_at;index:idx_mt_outbox_sched,priority:2"`
	ClaimToken    string     `gorm:"column:claim_token;type:varchar(64)"`
	ClaimUntil    *time.Time `gorm:"column:claim_until"`
	PoisonedAt    *time.Time `gorm:"column:poisoned_at;index:idx_mt_outbox_sched,priority:1"`
}

func (cacheInvalidationOutboxRow) TableName() string { return "mt_user_cache_invalidation_outbox" }

func migrateCacheInvalidationOutbox(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	return db.AutoMigrate(&cacheInvalidationOutboxRow{})
}

// enqueueCacheInvalidationOutbox best-effort 写入 outbox。事务外调用。
func enqueueCacheInvalidationOutbox(db *gorm.DB, orderNo string, userID int64) {
	if db == nil || orderNo == "" || userID <= 0 {
		return
	}
	if err := enqueueCacheInvalidationOutboxTx(db, orderNo, userID); err != nil {
		common.SysLog("cache outbox enqueue failed order=" + orderNo + ": " + err.Error())
	}
}

// enqueueCacheInvalidationOutboxTx 在给定 DB/tx 内写入 outbox（与资金同事务时使用）。
// OnConflict 后必须读回并验证 user_id，不能 DoNothing 后当成功。
func enqueueCacheInvalidationOutboxTx(db *gorm.DB, orderNo string, userID int64) error {
	if db == nil || orderNo == "" || userID <= 0 {
		return nil
	}
	now := time.Now()
	row := cacheInvalidationOutboxRow{
		OrderNo:       orderNo,
		UserID:        userID,
		CreatedAt:     now,
		NextAttemptAt: &now,
	}
	err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(&row).Error
	if err != nil {
		return err
	}
	// 无论 insert 或 conflict：读回验证 user_id
	var existing cacheInvalidationOutboxRow
	if rerr := db.Where("order_no = ?", orderNo).Take(&existing).Error; rerr != nil {
		return rerr
	}
	if existing.UserID != userID {
		return fmt.Errorf("cache outbox user_id mismatch order=%s have=%d want=%d", orderNo, existing.UserID, userID)
	}
	return nil
}

// markCacheInvalidationOutboxDone 标记已完成（同步失效成功时）。
func markCacheInvalidationOutboxDone(db *gorm.DB, orderNo string) {
	if db == nil || orderNo == "" {
		return
	}
	now := time.Now()
	_ = db.Model(&cacheInvalidationOutboxRow{}).
		Where("order_no = ? AND done_at IS NULL", orderNo).
		Updates(map[string]any{"done_at": now, "claim_token": "", "claim_until": nil}).Error
}

func outboxBackoff(attempts int) time.Duration {
	// 有界指数退避：2^min(attempts,8) 秒，上限 15 分钟
	shift := attempts
	if shift > 8 {
		shift = 8
	}
	d := time.Duration(1<<uint(shift)) * time.Second
	if d > 15*time.Minute {
		d = 15 * time.Minute
	}
	return d
}

func newOutboxClaimToken() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// processCacheInvalidationOutbox 带 claim/lease 的调度重试（对账循环调用）。
func processCacheInvalidationOutbox(ctx context.Context, db *gorm.DB, limit int) {
	if db == nil {
		return
	}
	if limit <= 0 {
		limit = 100
	}
	now := time.Now()
	// 清理过期完成记录
	cutoff := now.Add(-outboxDoneRetention)
	_ = db.WithContext(ctx).
		Where("done_at IS NOT NULL AND done_at < ?", cutoff).
		Delete(&cacheInvalidationOutboxRow{}).Error

	var rows []cacheInvalidationOutboxRow
	if err := db.WithContext(ctx).
		Where("done_at IS NULL AND poisoned_at IS NULL").
		Where("next_attempt_at IS NULL OR next_attempt_at <= ?", now).
		Where("claim_until IS NULL OR claim_until <= ?", now).
		Order("next_attempt_at ASC, order_no ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		logger.LogWarn(ctx, "cache outbox list failed: "+err.Error())
		return
	}
	for _, row := range rows {
		token := newOutboxClaimToken()
		lease := now.Add(outboxClaimLease)
		res := db.WithContext(ctx).Model(&cacheInvalidationOutboxRow{}).
			Where("order_no = ? AND done_at IS NULL AND poisoned_at IS NULL", row.OrderNo).
			Where("claim_until IS NULL OR claim_until <= ?", now).
			Updates(map[string]any{
				"claim_token": token,
				"claim_until": lease,
			})
		if res.Error != nil || res.RowsAffected != 1 {
			continue
		}
		if err := model.InvalidateUserCache(int(row.UserID)); err != nil {
			attempts := row.Attempts + 1
			next := now.Add(outboxBackoff(attempts))
			upd := map[string]any{
				"attempts":        attempts,
				"next_attempt_at": next,
				"claim_token":     "",
				"claim_until":     nil,
			}
			if attempts >= outboxMaxAttempts {
				upd["poisoned_at"] = now
				logger.LogWarn(ctx, "cache outbox poisoned order="+row.OrderNo)
			} else {
				logger.LogWarn(ctx, "cache outbox invalidate failed order="+row.OrderNo+": "+err.Error())
			}
			_ = db.Model(&cacheInvalidationOutboxRow{}).
				Where("order_no = ? AND claim_token = ?", row.OrderNo, token).
				Updates(upd).Error
			continue
		}
		done := time.Now()
		_ = db.Model(&cacheInvalidationOutboxRow{}).
			Where("order_no = ? AND claim_token = ?", row.OrderNo, token).
			Updates(map[string]any{
				"done_at":     done,
				"attempts":    row.Attempts + 1,
				"claim_token": "",
				"claim_until": nil,
			}).Error
	}
}
