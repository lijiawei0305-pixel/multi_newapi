package model

import (
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

func ensureSubscriptionPaymentReceiptIndexes(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("subscription payment receipt index migration requires database")
	}
	// A short-lived pre-release schema used GORM's default index name and made
	// order_id unique. Drop only that known legacy index; the new explicitly
	// named lookup index is non-unique and allows duplicate-payment evidence.
	const legacyOrderIndex = "idx_subscription_payment_receipts_order_id"
	if db.Migrator().HasIndex(&SubscriptionPaymentReceipt{}, legacyOrderIndex) {
		if err := db.Migrator().DropIndex(&SubscriptionPaymentReceipt{}, legacyOrderIndex); err != nil {
			return err
		}
	}
	const orderLookupIndex = "idx_subscription_payment_receipts_order_lookup"
	if db.Migrator().HasIndex(&SubscriptionPaymentReceipt{}, orderLookupIndex) {
		return nil
	}
	return db.Migrator().CreateIndex(&SubscriptionPaymentReceipt{}, orderLookupIndex)
}

func backfillSubscriptionPaymentReceiptMetadata() error {
	const batchSize = 500
	lastId := 0
	for {
		var receipts []SubscriptionPaymentReceipt
		if err := DB.Select("id", "order_id", "receipt_key", "provider", "provider_transaction_id", "claim_id", "disposition").
			Where("id > ? AND (claim_id IS NULL OR claim_id = '' OR disposition IS NULL OR disposition = '' OR receipt_key LIKE ?)", lastId, "%:%").
			Order("id asc").Limit(batchSize).Find(&receipts).Error; err != nil {
			return err
		}
		if len(receipts) == 0 {
			return nil
		}
		orderIds := make([]int, 0, len(receipts))
		for _, receipt := range receipts {
			lastId = receipt.Id
			orderIds = append(orderIds, receipt.OrderId)
		}
		var orders []SubscriptionOrder
		if err := DB.Select("id", "status").Where("id IN ?", orderIds).Find(&orders).Error; err != nil {
			return err
		}
		orderStatus := make(map[int]string, len(orders))
		for _, order := range orders {
			orderStatus[order.Id] = order.Status
		}
		if err := DB.Transaction(func(tx *gorm.DB) error {
			for _, receipt := range receipts {
				updates := map[string]interface{}{}
				expectedKey, keyErr := subscriptionReceiptKey(receipt.Provider, receipt.ProviderTransactionId)
				if keyErr != nil {
					return keyErr
				}
				if receipt.ReceiptKey != expectedKey {
					var existingCount int64
					if err := tx.Model(&SubscriptionPaymentReceipt{}).
						Where("receipt_key = ? AND id <> ?", expectedKey, receipt.Id).
						Count(&existingCount).Error; err != nil {
						return err
					}
					if existingCount == 0 {
						updates["receipt_key"] = expectedKey
					} else {
						updates["receipt_key"] = fmt.Sprintf("%x", common.Sha256Raw([]byte(fmt.Sprintf("legacy-conflict\x00%d\x00%s", receipt.Id, receipt.ReceiptKey))))
						updates["reason"] = "legacy receipt key conflicts with canonical provider transaction hash"
					}
				}
				if strings.TrimSpace(receipt.ClaimId) == "" {
					updates["claim_id"] = fmt.Sprintf("legacy-%d", receipt.Id)
				}
				if strings.TrimSpace(receipt.Disposition) == "" {
					disposition := SubscriptionReceiptDispositionReconciliation
					if orderStatus[receipt.OrderId] == common.TopUpStatusSuccess {
						disposition = SubscriptionReceiptDispositionFulfilled
					}
					updates["disposition"] = disposition
				}
				if len(updates) > 0 {
					if err := tx.Model(&SubscriptionPaymentReceipt{}).Where("id = ?", receipt.Id).UpdateColumns(updates).Error; err != nil {
						return err
					}
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

func backfillSubscriptionOrderReviewMetadata() error {
	const batchSize = 500
	lastId := 0
	now := GetDBTimestamp()
	for {
		var orders []SubscriptionOrder
		if err := DB.Select("id", "create_time", "complete_time").
			Where("id > ? AND status = ? AND (review_status IS NULL OR review_status = '')", lastId, SubscriptionOrderStatusReconciliationRequired).
			Order("id asc").Limit(batchSize).Find(&orders).Error; err != nil {
			return err
		}
		if len(orders) == 0 {
			return nil
		}
		if err := DB.Transaction(func(tx *gorm.DB) error {
			for _, order := range orders {
				lastId = order.Id
				reviewTime := order.CompleteTime
				if reviewTime <= 0 {
					reviewTime = order.CreateTime
				}
				if reviewTime <= 0 {
					reviewTime = now
				}
				if err := tx.Model(&SubscriptionOrder{}).
					Where("id = ? AND (review_status IS NULL OR review_status = '')", order.Id).
					UpdateColumns(map[string]interface{}{
						"review_status":                 SubscriptionOrderReviewRequired,
						"first_review_time":             reviewTime,
						"review_time":                   reviewTime,
						"review_previous_status":        "legacy_unknown",
						"review_previous_complete_time": order.CompleteTime,
					}).Error; err != nil {
					return err
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
}

// backfillUserSubscriptionBenefitSnapshots freezes the best information that
// still exists for legacy entitlements. Rows whose catalog plan was already
// deleted deliberately stay at version 0; runtime reset logic treats their
// missing policy as "never" instead of silently borrowing another plan.
func backfillUserSubscriptionBenefitSnapshots() error {
	const batchSize = 500
	lastId := 0
	missingPlanCount := 0
	now := GetDBTimestamp()
	for {
		var subscriptions []UserSubscription
		if err := DB.Select("id", "plan_id", "start_time", "end_time", "last_reset_time", "next_reset_time").
			Where("benefit_snapshot_version = ? AND id > ?", 0, lastId).
			Order("id asc").Limit(batchSize).Find(&subscriptions).Error; err != nil {
			return err
		}
		if len(subscriptions) == 0 {
			break
		}

		planIds := make([]int, 0, len(subscriptions))
		seenPlanIds := make(map[int]struct{}, len(subscriptions))
		for _, subscription := range subscriptions {
			lastId = subscription.Id
			if _, seen := seenPlanIds[subscription.PlanId]; !seen && subscription.PlanId > 0 {
				seenPlanIds[subscription.PlanId] = struct{}{}
				planIds = append(planIds, subscription.PlanId)
			}
		}
		var plans []SubscriptionPlan
		if len(planIds) > 0 {
			if err := DB.Where("id IN ?", planIds).Find(&plans).Error; err != nil {
				return err
			}
		}
		plansById := make(map[int]SubscriptionPlan, len(plans))
		for _, plan := range plans {
			plansById[plan.Id] = plan
		}

		if err := DB.Transaction(func(tx *gorm.DB) error {
			for _, subscription := range subscriptions {
				plan, ok := plansById[subscription.PlanId]
				if !ok {
					missingPlanCount++
					if subscription.NextResetTime != 0 {
						if err := tx.Model(&UserSubscription{}).Where("id = ?", subscription.Id).
							UpdateColumn("next_reset_time", 0).Error; err != nil {
							return err
						}
					}
					continue
				}
				resetPeriod := NormalizeResetPeriod(plan.QuotaResetPeriod)
				customSeconds := plan.QuotaResetCustomSeconds
				if resetPeriod == SubscriptionResetCustom && customSeconds <= 0 {
					resetPeriod = SubscriptionResetNever
					customSeconds = 0
				}
				lastResetTime := subscription.LastResetTime
				nextResetTime := int64(0)
				if resetPeriod != SubscriptionResetNever {
					baseUnix := lastResetTime
					if baseUnix <= 0 {
						baseUnix = subscription.StartTime
					}
					if baseUnix <= 0 {
						baseUnix = now
					}
					nextResetTime = calcNextResetTimeForPolicy(time.Unix(baseUnix, 0), resetPeriod, customSeconds, subscription.EndTime)
					if nextResetTime > 0 && lastResetTime <= 0 {
						lastResetTime = baseUnix
					}
				}
				result := tx.Model(&UserSubscription{}).
					Where("id = ? AND benefit_snapshot_version = ?", subscription.Id, 0).
					UpdateColumns(map[string]interface{}{
						"benefit_snapshot_version":   SubscriptionOrderSnapshotVersion,
						"plan_title":                 plan.Title,
						"quota_reset_period":         resetPeriod,
						"quota_reset_custom_seconds": customSeconds,
						"last_reset_time":            lastResetTime,
						"next_reset_time":            nextResetTime,
					})
				if result.Error != nil {
					return result.Error
				}
			}
			return nil
		}); err != nil {
			return err
		}
	}
	if missingPlanCount > 0 {
		common.SysLog(fmt.Sprintf("subscription benefit snapshot migration skipped %d rows whose plans no longer exist", missingPlanCount))
	}
	return nil
}
