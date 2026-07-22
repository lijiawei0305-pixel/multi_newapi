package model

import (
	"context"
	"errors"
	"time"

	"gorm.io/gorm"
)

const (
	SubscriptionPlanManagerNative    = "native_subscription"
	SubscriptionPlanManagerTokenPlan = "token_plan"
)

var errSubscriptionPlanOwnershipDatabaseUnavailable = errors.New("subscription plan ownership database is unavailable")

// TokenPlanNativeSubscriptionPlan maps one token-plan product to the native
// subscription plan that backs its billing lifecycle.
type TokenPlanNativeSubscriptionPlan struct {
	TokenPlanID  int64     `gorm:"column:token_plan_id;primaryKey"`
	NativePlanID int64     `gorm:"column:native_plan_id;not null;index"`
	CreatedAt    time.Time `gorm:"column:created_at"`
}

func (TokenPlanNativeSubscriptionPlan) TableName() string {
	return "mt_native_subscription_plans"
}

// GetTokenPlanManagedSubscriptionPlanIDs returns the subset of native plan IDs
// whose lifecycle is owned by Token Plans.
func GetTokenPlanManagedSubscriptionPlanIDs(ctx context.Context, db *gorm.DB, nativePlanIDs []int) (map[int]struct{}, error) {
	managed := make(map[int]struct{})
	if len(nativePlanIDs) == 0 {
		return managed, nil
	}
	if db == nil {
		return nil, errSubscriptionPlanOwnershipDatabaseUnavailable
	}

	var mappings []TokenPlanNativeSubscriptionPlan
	err := db.WithContext(ctx).
		Select("native_plan_id").
		Where("native_plan_id IN ?", nativePlanIDs).
		Find(&mappings).Error
	if err != nil {
		return nil, err
	}
	for _, mapping := range mappings {
		managed[int(mapping.NativePlanID)] = struct{}{}
	}
	return managed, nil
}
