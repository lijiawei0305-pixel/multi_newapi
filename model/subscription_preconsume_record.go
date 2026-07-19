package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// SubscriptionPreConsumeRecord stores idempotent pre-consume operations per request.
type SubscriptionPreConsumeRecord struct {
	Id                 int    `json:"id"`
	RequestId          string `json:"request_id" gorm:"type:varchar(128);uniqueIndex"`
	UserId             int    `json:"user_id" gorm:"index"`
	UserSubscriptionId int    `json:"user_subscription_id" gorm:"index"`
	PreConsumed        int64  `json:"pre_consumed" gorm:"type:bigint;not null;default:0"`
	ResetEpoch         int64  `json:"reset_epoch" gorm:"type:bigint;not null;default:0"`
	TokenId            int    `json:"token_id" gorm:"index"`
	TokenAmount        int    `json:"token_amount" gorm:"not null;default:0"`
	ClaimId            string `json:"claim_id" gorm:"type:varchar(64);not null;default:''"`
	Status             string `json:"status" gorm:"type:varchar(32);index"` // consumed/refunded
	CreatedAt          int64  `json:"created_at" gorm:"bigint"`
	UpdatedAt          int64  `json:"updated_at" gorm:"bigint;index"`
}

func (r *SubscriptionPreConsumeRecord) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	r.CreatedAt = now
	r.UpdatedAt = now
	return nil
}

func (r *SubscriptionPreConsumeRecord) BeforeUpdate(_ *gorm.DB) error {
	r.UpdatedAt = common.GetTimestamp()
	return nil
}

// CleanupSubscriptionPreConsumeRecords removes old idempotency records while
// retaining every row still referenced by pending money work or an active
// settlement lifecycle.
func CleanupSubscriptionPreConsumeRecords(olderThanSeconds int64) (int64, error) {
	if olderThanSeconds <= 0 {
		olderThanSeconds = 7 * 24 * 3600
	}
	cutoff := GetDBTimestamp() - olderThanSeconds
	pendingRefunds := DB.Model(&BillingRefundIntent{}).Select("subscription_request_id").
		Where("status = ? AND subscription_request_id <> ''", BillingRefundStatusPending)
	pendingAdjustments := DB.Model(&BillingAdjustmentIntent{}).Select("subscription_request_id").
		Where("status = ? AND subscription_request_id <> ''", BillingAdjustmentStatusPending)
	activeSettlements := DB.Model(&BillingSettlementEvent{}).Select("subscription_pre_consume_request_id").
		Where("status IN ? AND subscription_pre_consume_request_id <> ''", []string{BillingSettlementStatusReserved, BillingSettlementStatusDeferred})
	result := DB.Where("updated_at < ?", cutoff).
		Where("request_id NOT IN (?)", pendingRefunds).
		Where("request_id NOT IN (?)", pendingAdjustments).
		Where("request_id NOT IN (?)", activeSettlements).
		Delete(&SubscriptionPreConsumeRecord{})
	return result.RowsAffected, result.Error
}
