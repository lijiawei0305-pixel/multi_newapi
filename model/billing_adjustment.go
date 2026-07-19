package model

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/bytedance/gopkg/util/gopool"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingAdjustmentStatusPending = "pending"
	BillingAdjustmentStatusApplied = "applied"
)

var errLegacySubscriptionOccurrenceUnknown = errors.New("legacy subscription occurrence unknown")

// BillingAdjustmentIntent is a durable, idempotent, multi-account quota
// mutation. Positive wallet/token deltas return quota, negative deltas consume
// quota; positive subscription deltas consume subscription allowance.
type BillingAdjustmentIntent struct {
	Id                       int    `json:"id"`
	RequestId                string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_billing_adjustment_request_operation,priority:1"`
	Operation                string `json:"operation" gorm:"type:varchar(64);not null;uniqueIndex:idx_billing_adjustment_request_operation,priority:2"`
	UserId                   int    `json:"user_id" gorm:"index"`
	TokenId                  int    `json:"token_id" gorm:"index"`
	SubscriptionId           int    `json:"subscription_id" gorm:"index"`
	SubscriptionRequestId    string `json:"subscription_request_id" gorm:"type:varchar(128)"`
	SubscriptionResetEpoch   int64  `json:"subscription_reset_epoch" gorm:"type:bigint;not null"`
	SubscriptionOccurredAt   int64  `json:"subscription_occurred_at" gorm:"type:bigint;not null;default:0"`
	SubscriptionEpochOutcome string `json:"subscription_epoch_outcome" gorm:"type:varchar(32);not null"`
	UserQuotaDelta           int    `json:"user_quota_delta" gorm:"not null"`
	TokenQuotaDelta          int    `json:"token_quota_delta" gorm:"not null"`
	SubscriptionQuotaDelta   int64  `json:"subscription_quota_delta" gorm:"type:bigint;not null"`
	Status                   string `json:"status" gorm:"type:varchar(16);not null;index"`
	LastError                string `json:"last_error" gorm:"type:varchar(512)"`
	CreatedAt                int64  `json:"created_at" gorm:"bigint;not null"`
	UpdatedAt                int64  `json:"updated_at" gorm:"bigint;not null;index"`
}

type BillingAdjustmentSpec struct {
	RequestId              string
	Operation              string
	UserId                 int
	TokenId                int
	SubscriptionId         int
	SubscriptionRequestId  string
	SubscriptionResetEpoch int64
	SubscriptionOccurredAt int64
	UserQuotaDelta         int
	TokenQuotaDelta        int
	SubscriptionQuotaDelta int64
}

func (i *BillingAdjustmentIntent) BeforeCreate(_ *gorm.DB) error {
	now := common.GetTimestamp()
	i.CreatedAt = now
	i.UpdatedAt = now
	return nil
}

func (i *BillingAdjustmentIntent) BeforeUpdate(_ *gorm.DB) error {
	i.UpdatedAt = common.GetTimestamp()
	return nil
}

func (s BillingAdjustmentSpec) validate() error {
	s.RequestId = strings.TrimSpace(s.RequestId)
	s.Operation = strings.TrimSpace(s.Operation)
	if s.RequestId == "" || len(s.RequestId) > 128 {
		return errors.New("billing adjustment request id is invalid")
	}
	if s.Operation == "" || len(s.Operation) > 64 {
		return errors.New("billing adjustment operation is invalid")
	}
	if s.UserQuotaDelta != 0 && s.UserId <= 0 {
		return errors.New("billing adjustment user id is invalid")
	}
	if s.TokenQuotaDelta != 0 && s.TokenId <= 0 {
		return errors.New("billing adjustment token id is invalid")
	}
	if s.SubscriptionQuotaDelta != 0 && s.SubscriptionId <= 0 {
		return errors.New("billing adjustment subscription id is invalid")
	}
	if len(strings.TrimSpace(s.SubscriptionRequestId)) > 128 {
		return errors.New("billing adjustment subscription request id is invalid")
	}
	if s.SubscriptionResetEpoch < 0 {
		return errors.New("billing adjustment subscription reset epoch is invalid")
	}
	if s.SubscriptionOccurredAt < 0 {
		return errors.New("billing adjustment subscription occurred time is invalid")
	}
	if s.SubscriptionQuotaDelta != 0 && s.SubscriptionResetEpoch == 0 && s.SubscriptionOccurredAt == 0 {
		return errors.New("billing adjustment subscription occurred time is missing")
	}
	if s.UserQuotaDelta != 0 && (s.SubscriptionQuotaDelta != 0 || strings.TrimSpace(s.SubscriptionRequestId) != "") {
		return errors.New("billing adjustment cannot mutate wallet and subscription funding together")
	}
	if s.UserQuotaDelta != 0 && s.TokenQuotaDelta != 0 &&
		(s.UserQuotaDelta > 0) != (s.TokenQuotaDelta > 0) {
		return errors.New("billing adjustment wallet and token deltas have incompatible directions")
	}
	if s.SubscriptionQuotaDelta != 0 && s.TokenQuotaDelta != 0 &&
		(s.SubscriptionQuotaDelta > 0) == (s.TokenQuotaDelta > 0) {
		return errors.New("billing adjustment subscription and token deltas have incompatible directions")
	}
	if strings.TrimSpace(s.SubscriptionRequestId) != "" && s.TokenQuotaDelta < 0 {
		return errors.New("billing adjustment subscription refund and token delta have incompatible directions")
	}
	if s.UserQuotaDelta == 0 && s.TokenQuotaDelta == 0 && s.SubscriptionQuotaDelta == 0 && strings.TrimSpace(s.SubscriptionRequestId) == "" {
		return errors.New("billing adjustment has no quota delta")
	}
	return nil
}

func billingAdjustmentMatches(intent *BillingAdjustmentIntent, spec BillingAdjustmentSpec) bool {
	return intent.UserId == spec.UserId &&
		intent.TokenId == spec.TokenId &&
		intent.SubscriptionId == spec.SubscriptionId &&
		intent.SubscriptionRequestId == strings.TrimSpace(spec.SubscriptionRequestId) &&
		intent.SubscriptionResetEpoch == spec.SubscriptionResetEpoch &&
		(intent.SubscriptionOccurredAt == spec.SubscriptionOccurredAt || intent.SubscriptionOccurredAt == 0) &&
		intent.UserQuotaDelta == spec.UserQuotaDelta &&
		intent.TokenQuotaDelta == spec.TokenQuotaDelta &&
		intent.SubscriptionQuotaDelta == spec.SubscriptionQuotaDelta
}

func ensureBillingAdjustmentPendingTx(tx *gorm.DB, spec BillingAdjustmentSpec) (*BillingAdjustmentIntent, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	intent := &BillingAdjustmentIntent{
		RequestId:              strings.TrimSpace(spec.RequestId),
		Operation:              strings.TrimSpace(spec.Operation),
		UserId:                 spec.UserId,
		TokenId:                spec.TokenId,
		SubscriptionId:         spec.SubscriptionId,
		SubscriptionRequestId:  strings.TrimSpace(spec.SubscriptionRequestId),
		SubscriptionResetEpoch: spec.SubscriptionResetEpoch,
		SubscriptionOccurredAt: spec.SubscriptionOccurredAt,
		UserQuotaDelta:         spec.UserQuotaDelta,
		TokenQuotaDelta:        spec.TokenQuotaDelta,
		SubscriptionQuotaDelta: spec.SubscriptionQuotaDelta,
		Status:                 BillingAdjustmentStatusPending,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "request_id"}, {Name: "operation"}},
		DoNothing: true,
	}).Create(intent)
	if result.Error != nil {
		return nil, result.Error
	}
	if err := tx.Where("request_id = ? AND operation = ?", intent.RequestId, intent.Operation).
		First(intent).Error; err != nil {
		return nil, err
	}
	if !billingAdjustmentMatches(intent, spec) {
		return nil, errors.New("billing adjustment retry does not match the persisted mutation")
	}
	if intent.SubscriptionOccurredAt == 0 && spec.SubscriptionOccurredAt > 0 {
		result := tx.Model(&BillingAdjustmentIntent{}).
			Where("id = ? AND subscription_occurred_at = 0", intent.Id).
			Update("subscription_occurred_at", spec.SubscriptionOccurredAt)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			intent.SubscriptionOccurredAt = spec.SubscriptionOccurredAt
		} else {
			var current BillingAdjustmentIntent
			query := tx.Select("subscription_occurred_at").Where("id = ?", intent.Id)
			if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.First(&current).Error; err != nil {
				return nil, err
			}
			if current.SubscriptionOccurredAt != spec.SubscriptionOccurredAt {
				return nil, errors.New("billing adjustment retry does not match the persisted subscription occurred time")
			}
			intent.SubscriptionOccurredAt = current.SubscriptionOccurredAt
		}
	}
	return intent, nil
}

// EnsureBillingAdjustmentPending persists an adjustment before it is applied.
func EnsureBillingAdjustmentPending(spec BillingAdjustmentSpec) error {
	_, err := ensureBillingAdjustmentPendingTx(DB, spec)
	return err
}

// EnsureBillingAdjustmentPendingTx binds an adjustment to a caller-owned
// transaction, for example the same transaction that moves an async task to a
// terminal status.
func EnsureBillingAdjustmentPendingTx(tx *gorm.DB, spec BillingAdjustmentSpec) error {
	if tx == nil {
		return errors.New("billing adjustment transaction is nil")
	}
	_, err := ensureBillingAdjustmentPendingTx(tx, spec)
	return err
}

func applyBillingAdjustmentTx(tx *gorm.DB, intent *BillingAdjustmentIntent) error {
	if intent.Status == BillingAdjustmentStatusApplied {
		return nil
	}
	if intent.Status != BillingAdjustmentStatusPending {
		return fmt.Errorf("billing adjustment has invalid status %q", intent.Status)
	}

	if intent.UserQuotaDelta != 0 {
		query := tx.Unscoped().Model(&User{}).Where("id = ?", intent.UserId)
		if intent.UserQuotaDelta < 0 {
			amount := -intent.UserQuotaDelta
			query = query.Where("quota >= ?", amount)
		}
		result := query.Update("quota", gorm.Expr("quota + ?", intent.UserQuotaDelta))
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			if intent.UserQuotaDelta < 0 {
				return ErrUserQuotaInsufficient
			}
			return errors.New("billing adjustment user does not exist")
		}
	}

	if intent.TokenQuotaDelta != 0 {
		var token Token
		err := tx.Unscoped().Where("id = ?", intent.TokenId).First(&token).Error
		if err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return errors.New("billing adjustment token does not exist")
			}
			return err
		}
		query := tx.Unscoped().Model(&Token{}).Where("id = ?", intent.TokenId)
		if intent.TokenQuotaDelta < 0 {
			amount := -intent.TokenQuotaDelta
			query = query.Where("unlimited_quota = ? OR remain_quota >= ?", true, amount)
		} else {
			query = query.Where("used_quota >= ?", intent.TokenQuotaDelta)
		}
		result := query.Updates(map[string]interface{}{
			"remain_quota":  gorm.Expr("remain_quota + ?", intent.TokenQuotaDelta),
			"used_quota":    gorm.Expr("used_quota - ?", intent.TokenQuotaDelta),
			"accessed_time": common.GetTimestamp(),
		})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			if intent.TokenQuotaDelta < 0 {
				return ErrTokenQuotaInsufficient
			}
			return errors.New("billing adjustment token does not exist or its usage is inconsistent")
		}
	}

	epochSkipped := false
	if intent.SubscriptionQuotaDelta != 0 {
		occurredAt := intent.SubscriptionOccurredAt
		if occurredAt == 0 && intent.SubscriptionResetEpoch == 0 {
			var err error
			occurredAt, err = legacyBillingAdjustmentSubscriptionOccurredAtTx(tx, intent)
			if err != nil {
				return err
			}
			result := tx.Model(&BillingAdjustmentIntent{}).
				Where("id = ? AND subscription_occurred_at = 0", intent.Id).
				UpdateColumn("subscription_occurred_at", occurredAt)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected != 1 {
				return errors.New("legacy billing adjustment occurrence changed concurrently")
			}
			intent.SubscriptionOccurredAt = occurredAt
		}
		skipped, err := postConsumeUserSubscriptionDeltaForEpochTx(tx, intent.SubscriptionId, intent.SubscriptionQuotaDelta, intent.SubscriptionResetEpoch, occurredAt)
		if err != nil {
			return err
		}
		epochSkipped = epochSkipped || skipped
	}
	if intent.SubscriptionRequestId != "" {
		skipped, err := refundSubscriptionPreConsumeForEpochTx(tx, intent.SubscriptionRequestId)
		if err != nil {
			return err
		}
		epochSkipped = epochSkipped || skipped
	}
	epochOutcome := ""
	if epochSkipped {
		epochOutcome = "expired_period_skipped"
		if intent.SubscriptionQuotaDelta > 0 {
			epochOutcome = "expired_period_debt_absorbed"
		}
	}

	result := tx.Model(&BillingAdjustmentIntent{}).
		Where("id = ? AND status = ?", intent.Id, BillingAdjustmentStatusPending).
		Updates(map[string]interface{}{
			"status": BillingAdjustmentStatusApplied, "subscription_epoch_outcome": epochOutcome,
			"last_error": "", "updated_at": common.GetTimestamp(),
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return errors.New("billing adjustment state changed concurrently")
	}
	intent.Status = BillingAdjustmentStatusApplied
	intent.SubscriptionEpochOutcome = epochOutcome
	return nil
}

func legacyBillingAdjustmentSubscriptionOccurredAtTx(tx *gorm.DB, intent *BillingAdjustmentIntent) (int64, error) {
	if tx == nil || intent == nil || intent.SubscriptionId <= 0 {
		return 0, errLegacySubscriptionOccurrenceUnknown
	}

	var settlements []BillingSettlementEvent
	if err := tx.Where("adjustment_request_id = ? AND adjustment_operation = ?", intent.RequestId, intent.Operation).
		Order("id ASC").Limit(2).Find(&settlements).Error; err != nil {
		return 0, err
	}
	if len(settlements) == 1 && settlements[0].FundingSource == "subscription" && settlements[0].SubscriptionId == intent.SubscriptionId {
		occurredAt := settlements[0].SubscriptionOccurredAt
		if occurredAt == 0 && !settlements[0].CreatedAt.IsZero() {
			occurredAt = settlements[0].CreatedAt.Unix()
		}
		if occurredAt > 0 {
			return occurredAt, nil
		}
	}
	if len(settlements) > 1 {
		return 0, errLegacySubscriptionOccurrenceUnknown
	}

	recordRequestIds := []string{strings.TrimSpace(intent.SubscriptionRequestId)}
	if requestId := strings.TrimSpace(intent.RequestId); requestId != "" && requestId != recordRequestIds[0] {
		recordRequestIds = append(recordRequestIds, requestId)
	}
	for _, requestId := range recordRequestIds {
		if requestId == "" {
			continue
		}
		var record SubscriptionPreConsumeRecord
		result := tx.Where("request_id = ?", requestId).Limit(1).Find(&record)
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected == 1 && record.UserSubscriptionId == intent.SubscriptionId && record.CreatedAt > 0 {
			return record.CreatedAt, nil
		}
	}

	if taskId, ok := legacyBillingNumericId(intent.RequestId, "task:"); ok {
		var task Task
		result := tx.Where("id = ?", taskId).Limit(1).Find(&task)
		if result.Error != nil {
			return 0, result.Error
		}
		if result.RowsAffected == 1 && task.PrivateData.SubscriptionId == intent.SubscriptionId &&
			(task.PrivateData.BillingSource == "" || task.PrivateData.BillingSource == "subscription") {
			occurredAt := task.PrivateData.SubscriptionOccurredAt
			if occurredAt == 0 {
				occurredAt = task.SubmitTime
				if occurredAt > 100_000_000_000 {
					occurredAt /= 1000
				}
			}
			if occurredAt == 0 {
				occurredAt = task.CreatedAt
			}
			if occurredAt > 0 {
				return occurredAt, nil
			}
		}
	}

	var midjourneyTasks []Midjourney
	if taskId, ok := legacyBillingNumericId(intent.RequestId, "midjourney:"); ok {
		if err := tx.Where("id = ?", taskId).Limit(2).Find(&midjourneyTasks).Error; err != nil {
			return 0, err
		}
	} else if strings.TrimSpace(intent.RequestId) != "" {
		if err := tx.Where("billing_request_id = ?", strings.TrimSpace(intent.RequestId)).Order("id ASC").Limit(2).Find(&midjourneyTasks).Error; err != nil {
			return 0, err
		}
	}
	if len(midjourneyTasks) == 1 && midjourneyTasks[0].SubscriptionId == intent.SubscriptionId &&
		(midjourneyTasks[0].BillingSource == "" || midjourneyTasks[0].BillingSource == "subscription") {
		occurredAt := midjourneyTasks[0].SubscriptionOccurredAt
		if occurredAt == 0 {
			occurredAt = midjourneyTasks[0].SubmitTime
			if occurredAt > 100_000_000_000 {
				occurredAt /= 1000
			}
		}
		if occurredAt > 0 {
			return occurredAt, nil
		}
	}

	return 0, errLegacySubscriptionOccurrenceUnknown
}

func legacyBillingNumericId(requestId string, prefix string) (int64, bool) {
	requestId = strings.TrimSpace(requestId)
	if !strings.HasPrefix(requestId, prefix) {
		return 0, false
	}
	id, err := strconv.ParseInt(strings.TrimPrefix(requestId, prefix), 10, 64)
	return id, err == nil && id > 0
}

func refreshBillingAdjustmentCaches(intent BillingAdjustmentIntent) {
	if !common.RedisEnabled {
		return
	}
	if intent.UserId > 0 && intent.UserQuotaDelta != 0 {
		gopool.Go(func() {
			if _, err := GetUserQuota(intent.UserId, true); err != nil {
				common.SysLog("failed to refresh user quota cache after billing adjustment: " + err.Error())
			}
		})
	}
	if intent.TokenId > 0 && intent.TokenQuotaDelta != 0 {
		gopool.Go(func() {
			if _, err := GetTokenById(intent.TokenId); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
				common.SysLog("failed to refresh token quota cache after billing adjustment: " + err.Error())
			}
		})
	}
}

// ApplyBillingAdjustment atomically applies every account delta and marks the
// durable intent applied. Replays are no-ops.
func ApplyBillingAdjustment(requestId string, operation string) error {
	requestId = strings.TrimSpace(requestId)
	operation = strings.TrimSpace(operation)
	if requestId == "" || operation == "" {
		return errors.New("billing adjustment key is empty")
	}

	var applied BillingAdjustmentIntent
	err := DB.Transaction(func(tx *gorm.DB) error {
		query := tx.Where("request_id = ? AND operation = ?", requestId, operation)
		if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		if err := query.First(&applied).Error; err != nil {
			return err
		}
		return applyBillingAdjustmentTx(tx, &applied)
	})
	if err != nil {
		if errors.Is(err, errLegacySubscriptionOccurrenceUnknown) && applied.LastError != errLegacySubscriptionOccurrenceUnknown.Error() {
			common.SysError(fmt.Sprintf(
				"CRITICAL: legacy subscription occurrence unknown; billing adjustment remains pending id=%d subscription_id=%d",
				applied.Id, applied.SubscriptionId,
			))
		}
		_ = DB.Model(&BillingAdjustmentIntent{}).
			Where("request_id = ? AND operation = ? AND status = ?", requestId, operation, BillingAdjustmentStatusPending).
			Updates(map[string]interface{}{
				"last_error": truncateBillingRefundError(err.Error()),
				"updated_at": common.GetTimestamp(),
			}).Error
		return err
	}
	refreshBillingAdjustmentCaches(applied)
	return nil
}

// ApplyBillingAdjustmentOnce persists first, then applies. A failure remains
// pending for reconciliation because the external cost may already exist.
func ApplyBillingAdjustmentOnce(spec BillingAdjustmentSpec) error {
	if err := EnsureBillingAdjustmentPending(spec); err != nil {
		return err
	}
	return ApplyBillingAdjustment(spec.RequestId, spec.Operation)
}

// ApplyBillingAdjustmentImmediateOnce is for pre-request reservations. The
// intent and all quota mutations commit together; on insufficient quota the
// whole transaction rolls back instead of leaving a debt for later top-ups.
func ApplyBillingAdjustmentImmediateOnce(spec BillingAdjustmentSpec) error {
	var applied BillingAdjustmentIntent
	err := DB.Transaction(func(tx *gorm.DB) error {
		intent, err := ensureBillingAdjustmentPendingTx(tx, spec)
		if err != nil {
			return err
		}
		applied = *intent
		return applyBillingAdjustmentTx(tx, &applied)
	})
	if err != nil {
		return err
	}
	refreshBillingAdjustmentCaches(applied)
	return nil
}

func ReconcilePendingBillingAdjustments(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var intents []BillingAdjustmentIntent
	if err := DB.Where("status = ?", BillingAdjustmentStatusPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&intents).Error; err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for _, intent := range intents {
		if err := ApplyBillingAdjustment(intent.RequestId, intent.Operation); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

func InitBillingAdjustmentReconciler() {
	run := func() {
		recovered, recoveryErr := ReconcilePendingBillingTerminalRecoveries(100)
		if recoveryErr != nil {
			common.SysLog("failed to reconcile pending billing terminal recoveries: " + recoveryErr.Error())
		} else if recovered > 0 {
			common.SysLog(fmt.Sprintf("reconciled %d pending billing terminal recoveries", recovered))
		}
		applied, err := ReconcilePendingBillingAdjustments(100)
		if err != nil {
			common.SysLog("failed to reconcile pending billing adjustments: " + err.Error())
		} else if applied > 0 {
			common.SysLog(fmt.Sprintf("reconciled %d pending billing adjustments", applied))
		}
		settled, settlementErr := ReconcilePendingBillingSettlements(100)
		if settlementErr != nil {
			common.SysLog("failed to reconcile pending billing settlements: " + settlementErr.Error())
		} else if settled > 0 {
			common.SysLog(fmt.Sprintf("reconciled %d pending billing settlements", settled))
		}
		projected, projectionErr := ReconcilePendingBillingProjections(100)
		if projectionErr != nil {
			common.SysLog("failed to reconcile pending billing projections: " + projectionErr.Error())
		} else if projected > 0 {
			common.SysLog(fmt.Sprintf("reconciled %d pending billing projections", projected))
		}
		stale, staleErr := CountStaleBillingSettlements(time.Hour, 24*time.Hour, 10*time.Minute)
		if staleErr != nil {
			common.SysLog("failed to inspect stale billing settlements: " + staleErr.Error())
		} else if stale.Reserved+stale.Deferred+stale.FinancialPending+stale.CommissionPending > 0 {
			common.SysLog(fmt.Sprintf("critical: stale billing settlements reserved=%d deferred=%d financial_pending=%d commission_pending=%d",
				stale.Reserved, stale.Deferred, stale.FinancialPending, stale.CommissionPending))
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

// UpdateTaskWithBillingAdjustment commits a terminal task CAS and its pending
// financial mutation in one transaction. A process crash can therefore leave
// neither an unrefunded terminal task nor a mutation without the status change.
func UpdateTaskWithBillingAdjustment(task *Task, fromStatus TaskStatus, spec *BillingAdjustmentSpec) (bool, error) {
	if task == nil {
		return false, errors.New("task is nil")
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(task).Where("status = ?", fromStatus).Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		won = true
		if spec != nil {
			if _, err := ensureBillingAdjustmentPendingTx(tx, *spec); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return won, err
}

// UpdateMidjourneyWithBillingAdjustment provides the same terminal-CAS
// guarantee for the legacy Midjourney task table.
func UpdateMidjourneyWithBillingAdjustment(task *Midjourney, fromStatus string, spec *BillingAdjustmentSpec) (bool, error) {
	if task == nil {
		return false, errors.New("midjourney task is nil")
	}
	won := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(task).Where("status = ?", fromStatus).Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		won = true
		if spec != nil {
			if _, err := ensureBillingAdjustmentPendingTx(tx, *spec); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	return won, err
}
