package model

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingRefundStatusPending  = "pending"
	BillingRefundStatusRefunded = "refunded"
)

// BillingRefundIntent is the durable, idempotent compensation record for a
// relay billing request. The unique request/operation pair lets retry workers
// safely replay a failed refund after a process restart.
type BillingRefundIntent struct {
	Id                    int    `json:"id"`
	RequestId             string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_billing_refund_request_operation,priority:1"`
	Operation             string `json:"operation" gorm:"type:varchar(64);not null;uniqueIndex:idx_billing_refund_request_operation,priority:2"`
	UserId                int    `json:"user_id" gorm:"index"`
	TokenId               int    `json:"token_id" gorm:"index"`
	UserQuota             int    `json:"user_quota" gorm:"not null"`
	TokenQuota            int    `json:"token_quota" gorm:"not null"`
	SubscriptionRequestId string `json:"subscription_request_id" gorm:"type:varchar(128)"`
	SubscriptionId        int    `json:"subscription_id" gorm:"index"`
	SubscriptionQuota     int64  `json:"subscription_quota" gorm:"type:bigint;not null"`
	Status                string `json:"status" gorm:"type:varchar(16);not null;index"`
	LastError             string `json:"last_error" gorm:"type:varchar(512)"`
	CreatedAt             int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt             int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

type BillingRefundSpec struct {
	RequestId             string
	Operation             string
	UserId                int
	TokenId               int
	UserQuota             int
	TokenQuota            int
	SubscriptionRequestId string
	SubscriptionId        int
	SubscriptionQuota     int64
}

func (i *BillingRefundIntent) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	i.CreatedAt = now
	i.UpdatedAt = now
	return nil
}

func (i *BillingRefundIntent) BeforeUpdate(_ *gorm.DB) error {
	i.UpdatedAt = common.GetTimestamp()
	return nil
}

func (s BillingRefundSpec) validate() error {
	if strings.TrimSpace(s.RequestId) == "" || len(strings.TrimSpace(s.RequestId)) > 128 {
		return errors.New("billing refund request id is invalid")
	}
	if strings.TrimSpace(s.Operation) == "" || len(strings.TrimSpace(s.Operation)) > 64 {
		return errors.New("billing refund operation is invalid")
	}
	if s.UserQuota < 0 || s.TokenQuota < 0 || s.SubscriptionQuota < 0 {
		return errors.New("billing refund quota cannot be negative")
	}
	if s.UserQuota > 0 && s.UserId <= 0 {
		return errors.New("billing refund user id is invalid")
	}
	if s.TokenQuota > 0 && s.TokenId <= 0 {
		return errors.New("billing refund token id is invalid")
	}
	if s.SubscriptionQuota > 0 && s.SubscriptionId <= 0 {
		return errors.New("billing refund subscription id is invalid")
	}
	if len(strings.TrimSpace(s.SubscriptionRequestId)) > 128 {
		return errors.New("billing refund subscription request id is invalid")
	}
	if s.UserQuota > 0 && (s.SubscriptionQuota > 0 || strings.TrimSpace(s.SubscriptionRequestId) != "") {
		return errors.New("billing refund cannot compensate wallet and subscription funding together")
	}
	if s.UserQuota == 0 && s.TokenQuota == 0 && s.SubscriptionQuota == 0 && strings.TrimSpace(s.SubscriptionRequestId) == "" {
		return errors.New("billing refund has no compensation")
	}
	return nil
}

func billingRefundMatches(intent *BillingRefundIntent, spec BillingRefundSpec) bool {
	return intent.UserId == spec.UserId &&
		intent.TokenId == spec.TokenId &&
		intent.UserQuota == spec.UserQuota &&
		intent.TokenQuota == spec.TokenQuota &&
		intent.SubscriptionRequestId == spec.SubscriptionRequestId &&
		intent.SubscriptionId == spec.SubscriptionId &&
		intent.SubscriptionQuota == spec.SubscriptionQuota
}

// EnsureBillingRefundPending persists the compensation before any quota is
// returned. A repeated operation must carry exactly the same amounts.
func EnsureBillingRefundPending(spec BillingRefundSpec) error {
	if err := spec.validate(); err != nil {
		return err
	}
	intent := &BillingRefundIntent{
		RequestId:             strings.TrimSpace(spec.RequestId),
		Operation:             strings.TrimSpace(spec.Operation),
		UserId:                spec.UserId,
		TokenId:               spec.TokenId,
		UserQuota:             spec.UserQuota,
		TokenQuota:            spec.TokenQuota,
		SubscriptionRequestId: strings.TrimSpace(spec.SubscriptionRequestId),
		SubscriptionId:        spec.SubscriptionId,
		SubscriptionQuota:     spec.SubscriptionQuota,
		Status:                BillingRefundStatusPending,
	}
	result := DB.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "request_id"}, {Name: "operation"}},
		DoNothing: true,
	}).Create(intent)
	if result.Error != nil {
		return result.Error
	}
	var existing BillingRefundIntent
	if err := DB.Where("request_id = ? AND operation = ?", intent.RequestId, intent.Operation).
		First(&existing).Error; err != nil {
		return err
	}
	if !billingRefundMatches(&existing, spec) {
		return errors.New("billing refund retry does not match the persisted compensation")
	}
	return nil
}

func refundSubscriptionPreConsumeTx(tx *gorm.DB, requestId string) error {
	_, err := refundSubscriptionPreConsumeForEpochTx(tx, requestId)
	return err
}

func refundSubscriptionPreConsumeForEpochTx(tx *gorm.DB, requestId string) (bool, error) {
	if strings.TrimSpace(requestId) == "" {
		return false, nil
	}
	var record SubscriptionPreConsumeRecord
	if err := lockForUpdate(tx).Where("request_id = ?", requestId).First(&record).Error; err != nil {
		return false, err
	}
	if record.Status == "refunded" {
		return false, nil
	}
	skipped := false
	if record.PreConsumed > 0 {
		var err error
		skipped, err = postConsumeUserSubscriptionDeltaForEpochTx(tx, record.UserSubscriptionId, -record.PreConsumed, record.ResetEpoch, record.CreatedAt)
		if err != nil {
			return false, err
		}
	}
	record.Status = "refunded"
	return skipped, tx.Save(&record).Error
}

// ApplyBillingRefund applies every quota return and the pending->refunded
// transition in one transaction. A committed retry therefore observes the
// terminal state instead of adding quota twice.
func ApplyBillingRefund(requestId string, operation string) error {
	requestId = strings.TrimSpace(requestId)
	operation = strings.TrimSpace(operation)
	if requestId == "" || operation == "" {
		return errors.New("billing refund key is empty")
	}

	var applied BillingRefundIntent
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Where("request_id = ? AND operation = ?", requestId, operation)
		if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&applied).Error; err != nil {
			return err
		}
		if applied.Status == BillingRefundStatusRefunded {
			return nil
		}
		if applied.Status != BillingRefundStatusPending {
			return fmt.Errorf("billing refund has invalid status %q", applied.Status)
		}

		if applied.UserQuota > 0 {
			result := tx.Unscoped().Model(&User{}).Where("id = ?", applied.UserId).
				Update("quota", gorm.Expr("quota + ?", applied.UserQuota))
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("billing refund user does not exist")
			}
		}
		if applied.TokenQuota > 0 {
			result := tx.Unscoped().Model(&Token{}).
				Where("id = ? AND used_quota >= ?", applied.TokenId, applied.TokenQuota).
				Updates(map[string]interface{}{
					"remain_quota": gorm.Expr("remain_quota + ?", applied.TokenQuota),
					"used_quota":   gorm.Expr("used_quota - ?", applied.TokenQuota),
				})
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("billing refund token does not exist or its usage is inconsistent")
			}
		}
		if err := refundSubscriptionPreConsumeTx(tx, applied.SubscriptionRequestId); err != nil {
			return err
		}
		if applied.SubscriptionQuota > 0 {
			// BillingRefundIntent predates explicit subscription reset epochs. Its
			// creation time is the only durable period boundary available for
			// legacy pending refunds: once the subscription has reset after that
			// time, the refund belongs to the expired period and must not reduce
			// usage in the current period.
			if _, err := postConsumeUserSubscriptionDeltaForEpochTx(
				tx,
				applied.SubscriptionId,
				-applied.SubscriptionQuota,
				0,
				applied.CreatedAt,
			); err != nil {
				return err
			}
		}

		result := tx.Model(&BillingRefundIntent{}).
			Where("id = ? AND status = ?", applied.Id, BillingRefundStatusPending).
			Updates(map[string]interface{}{
				"status":     BillingRefundStatusRefunded,
				"last_error": "",
				"updated_at": common.GetTimestamp(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("billing refund state changed concurrently")
		}
		applied.Status = BillingRefundStatusRefunded
		return nil
	})
	if err != nil {
		_ = DB.Model(&BillingRefundIntent{}).
			Where("request_id = ? AND operation = ? AND status = ?", requestId, operation, BillingRefundStatusPending).
			Updates(map[string]interface{}{
				"last_error": truncateBillingRefundError(err.Error()),
				"updated_at": common.GetTimestamp(),
			}).Error
		return err
	}

	// Redis is a cache for these quota fields. Refresh it only after the DB
	// transaction commits; a cache failure cannot roll back durable money.
	if applied.UserId > 0 && common.RedisEnabled {
		gopool.Go(func() {
			if _, err := GetUserQuota(applied.UserId, true); err != nil {
				common.SysLog("failed to refresh user quota cache after billing refund: " + err.Error())
			}
		})
	}
	if applied.TokenId > 0 && common.RedisEnabled {
		gopool.Go(func() {
			if _, err := GetTokenById(applied.TokenId); err != nil {
				common.SysLog("failed to refresh token quota cache after billing refund: " + err.Error())
			}
		})
	}
	return nil
}

func truncateBillingRefundError(message string) string {
	const maxLength = 512
	if len(message) <= maxLength {
		return message
	}
	return message[:maxLength]
}

func RefundBillingQuotaOnce(spec BillingRefundSpec) error {
	if err := EnsureBillingRefundPending(spec); err != nil {
		return err
	}
	return ApplyBillingRefund(spec.RequestId, spec.Operation)
}

func ReconcilePendingBillingRefunds(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var intents []BillingRefundIntent
	if err := DB.Where("status = ?", BillingRefundStatusPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&intents).Error; err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for _, intent := range intents {
		if err := ApplyBillingRefund(intent.RequestId, intent.Operation); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

func InitBillingRefundReconciler() {
	run := func() {
		applied, err := ReconcilePendingBillingRefunds(100)
		if err != nil {
			common.SysLog("failed to reconcile pending billing refunds: " + err.Error())
			return
		}
		if applied > 0 {
			common.SysLog(fmt.Sprintf("reconciled %d pending billing refunds", applied))
		}
	}
	run()
	gopool.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			run()
		}
	})
}
