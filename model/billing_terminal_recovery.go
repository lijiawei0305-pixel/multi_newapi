package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingTerminalRecoveryStatusPending = "pending"
	BillingTerminalRecoveryStatusApplied = "applied"

	BillingTerminalRecoveryPhaseDeferred  = "deferred"
	BillingTerminalRecoveryPhaseFinalized = "finalized"
	BillingTerminalRecoveryPhaseCancelled = "cancelled"
)

// BillingTerminalRecovery freezes the exact terminal settlement intent before
// the lifecycle transition is attempted. This closes the accepted-response
// gap where a transient main-database failure could otherwise leave only the
// old reservation and make the known upstream cost refundable.
type BillingTerminalRecovery struct {
	Id          int    `json:"id"`
	RecoveryKey string `json:"recovery_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	RequestId   string `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_billing_terminal_recovery_identity,priority:1"`
	Operation   string `json:"operation" gorm:"type:varchar(64);not null;uniqueIndex:idx_billing_terminal_recovery_identity,priority:2"`
	Phase       string `json:"phase" gorm:"type:varchar(16);not null;uniqueIndex:idx_billing_terminal_recovery_identity,priority:3"`
	PayloadHash string `json:"payload_hash" gorm:"type:varchar(64);not null"`
	// Leave the string type dialect-neutral: GORM maps an unbounded string to
	// LONGTEXT on MySQL and TEXT on PostgreSQL/SQLite. A MySQL TEXT column is
	// limited to 64 KiB and cannot safely hold a frozen terminal projection.
	Payload      string     `json:"payload" gorm:"not null"`
	Status       string     `json:"status" gorm:"type:varchar(16);not null;index"`
	AttemptCount int        `json:"attempt_count" gorm:"not null"`
	LastError    string     `json:"last_error" gorm:"type:varchar(512);not null"`
	AppliedAt    *time.Time `json:"applied_at"`
	CreatedAt    time.Time  `json:"created_at" gorm:"not null"`
	UpdatedAt    time.Time  `json:"updated_at" gorm:"not null;index"`
}

// BillingTerminalRecoveryPayload is private durable state. Settlement is
// present only for legacy/no-session paths whose lifecycle root must be
// created together with the terminal transition during recovery.
type BillingTerminalRecoveryPayload struct {
	Settlement *BillingSettlementSpec      `json:"settlement,omitempty"`
	Transition BillingSettlementTransition `json:"transition"`
	Adjustment *BillingAdjustmentSpec      `json:"adjustment,omitempty"`
	Projection *BillingProjectionSpec      `json:"projection,omitempty"`
}

// BillingTerminalRecoveryPendingError means the exact terminal intent is
// durable, but its lifecycle transition has not committed yet. Accepted
// upstream work must not be refunded or retried when callers receive it.
type BillingTerminalRecoveryPendingError struct {
	RecoveryKey string
	Err         error
}

func (e *BillingTerminalRecoveryPendingError) Error() string {
	return "billing terminal recovery is pending: " + e.Err.Error()
}

func (e *BillingTerminalRecoveryPendingError) Unwrap() error { return e.Err }

func billingTerminalRecoveryPhase(transition BillingSettlementTransition) string {
	if transition.Cancel {
		return BillingTerminalRecoveryPhaseCancelled
	}
	if !transition.ReleaseCommission {
		return BillingTerminalRecoveryPhaseDeferred
	}
	return BillingTerminalRecoveryPhaseFinalized
}

func BillingTerminalRecoveryKey(requestId string, operation string, phases ...string) string {
	phase := BillingTerminalRecoveryPhaseFinalized
	if len(phases) > 0 && strings.TrimSpace(phases[0]) != "" {
		phase = strings.TrimSpace(phases[0])
	}
	raw := strings.TrimSpace(requestId) + "\x00" + strings.TrimSpace(operation) + "\x00" + phase
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func normalizeBillingTerminalRecoveryPayload(payload BillingTerminalRecoveryPayload) BillingTerminalRecoveryPayload {
	payload.Transition.RequestId = strings.TrimSpace(payload.Transition.RequestId)
	payload.Transition.Operation = strings.TrimSpace(payload.Transition.Operation)
	if payload.Settlement != nil {
		settlement := *payload.Settlement
		settlement.RequestId = strings.TrimSpace(settlement.RequestId)
		settlement.Operation = strings.TrimSpace(settlement.Operation)
		settlement.SubscriptionPreConsumeRequestId = strings.TrimSpace(settlement.SubscriptionPreConsumeRequestId)
		payload.Settlement = &settlement
	}
	if payload.Adjustment != nil {
		adjustment := *payload.Adjustment
		adjustment.RequestId = strings.TrimSpace(adjustment.RequestId)
		adjustment.Operation = strings.TrimSpace(adjustment.Operation)
		adjustment.SubscriptionRequestId = strings.TrimSpace(adjustment.SubscriptionRequestId)
		payload.Adjustment = &adjustment
	}
	if payload.Projection != nil {
		projection := payload.Projection.normalized()
		payload.Projection = &projection
	}
	return payload
}

func validateBillingTerminalRecoveryPayload(payload BillingTerminalRecoveryPayload) error {
	transition := payload.Transition
	if transition.RequestId == "" || len(transition.RequestId) > 128 || transition.Operation == "" || len(transition.Operation) > 64 || transition.FinalQuota < 0 {
		return errors.New("billing terminal recovery transition is invalid")
	}
	if payload.Settlement != nil {
		if err := payload.Settlement.validate(); err != nil {
			return err
		}
		if payload.Settlement.RequestId != transition.RequestId || payload.Settlement.Operation != transition.Operation {
			return errors.New("billing terminal recovery settlement identity mismatch")
		}
	}
	if payload.Adjustment != nil {
		adjustment := *payload.Adjustment
		// Legacy subscription settlements bind their immutable occurrence time
		// from the lifecycle root inside transitionBillingSettlementTx. Validate
		// the remaining shape without inventing that value in the frozen payload.
		if adjustment.SubscriptionQuotaDelta != 0 && adjustment.SubscriptionResetEpoch == 0 && adjustment.SubscriptionOccurredAt == 0 {
			adjustment.SubscriptionOccurredAt = 1
		}
		if err := adjustment.validate(); err != nil {
			return err
		}
	}
	return validateSettlementBillingProjection(payload.Projection, transition)
}

func decodeBillingTerminalRecovery(row *BillingTerminalRecovery) (*BillingTerminalRecoveryPayload, error) {
	if row == nil || row.Payload == "" {
		return nil, errors.New("billing terminal recovery payload is empty")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(row.Payload)))
	if hash != row.PayloadHash {
		return nil, errors.New("billing terminal recovery payload hash mismatch")
	}
	var payload BillingTerminalRecoveryPayload
	if err := common.UnmarshalJsonStr(row.Payload, &payload); err != nil {
		return nil, err
	}
	payload = normalizeBillingTerminalRecoveryPayload(payload)
	if err := validateBillingTerminalRecoveryPayload(payload); err != nil {
		return nil, err
	}
	if payload.Transition.RequestId != row.RequestId || payload.Transition.Operation != row.Operation {
		return nil, errors.New("billing terminal recovery row identity mismatch")
	}
	if billingTerminalRecoveryPhase(payload.Transition) != row.Phase {
		return nil, errors.New("billing terminal recovery phase mismatch")
	}
	return &payload, nil
}

func freezeBillingTerminalRecovery(payload BillingTerminalRecoveryPayload) (string, error) {
	payload = normalizeBillingTerminalRecoveryPayload(payload)
	if err := validateBillingTerminalRecoveryPayload(payload); err != nil {
		return "", err
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return "", err
	}
	phase := billingTerminalRecoveryPhase(payload.Transition)
	recoveryKey := BillingTerminalRecoveryKey(payload.Transition.RequestId, payload.Transition.Operation, phase)
	payloadHash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	now := time.Now().UTC()
	row := &BillingTerminalRecovery{
		RecoveryKey: recoveryKey, RequestId: payload.Transition.RequestId, Operation: payload.Transition.Operation, Phase: phase,
		PayloadHash: payloadHash, Payload: string(encoded), Status: BillingTerminalRecoveryStatusPending,
		LastError: "", CreatedAt: now, UpdatedAt: now,
	}
	err = DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "recovery_key"}}, DoNothing: true,
		}).Create(row).Error; err != nil {
			return err
		}
		var persisted BillingTerminalRecovery
		if err := tx.Where("recovery_key = ?", recoveryKey).First(&persisted).Error; err != nil {
			return err
		}
		if persisted.RequestId != payload.Transition.RequestId || persisted.Operation != payload.Transition.Operation || persisted.Phase != phase ||
			persisted.PayloadHash != payloadHash || persisted.Payload != string(encoded) {
			return errors.New("billing terminal recovery retry does not match persisted payload")
		}
		if persisted.Status != BillingTerminalRecoveryStatusPending && persisted.Status != BillingTerminalRecoveryStatusApplied {
			return fmt.Errorf("billing terminal recovery status is invalid: %s", persisted.Status)
		}
		return nil
	})
	return recoveryKey, err
}

func loadBillingTerminalRecoveryTx(tx *gorm.DB, recoveryKey string, lock bool) (*BillingTerminalRecovery, error) {
	query := tx.Where("recovery_key = ?", recoveryKey)
	if lock && !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row BillingTerminalRecovery
	if err := query.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

// ApplyBillingTerminalRecovery commits the terminal settlement fact, exact
// projection outbox, and recovery applied marker in one main-database
// transaction. Financial and sink application remain replayable follow-ups.
func ApplyBillingTerminalRecovery(recoveryKey string) error {
	recoveryKey = strings.TrimSpace(recoveryKey)
	if recoveryKey == "" || len(recoveryKey) > 64 {
		return errors.New("billing terminal recovery key is invalid")
	}
	var event *BillingSettlementEvent
	var projection *BillingProjectionSpec
	err := DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&BillingTerminalRecovery{}).
			Where("recovery_key = ? AND status = ?", recoveryKey, BillingTerminalRecoveryStatusPending).
			UpdateColumn("attempt_count", gorm.Expr("attempt_count + 1"))
		if claim.Error != nil {
			return claim.Error
		}
		row, err := loadBillingTerminalRecoveryTx(tx, recoveryKey, true)
		if err != nil {
			return err
		}
		payload, err := decodeBillingTerminalRecovery(row)
		if err != nil {
			return err
		}
		projection = payload.Projection
		if claim.RowsAffected == 0 && row.Status == BillingTerminalRecoveryStatusApplied {
			event, err = lockBillingSettlementTx(tx, payload.Transition.RequestId, payload.Transition.Operation)
			return err
		}
		if claim.RowsAffected != 1 || row.Status != BillingTerminalRecoveryStatusPending {
			return fmt.Errorf("billing terminal recovery could not be claimed from status: %s", row.Status)
		}
		if payload.Settlement != nil {
			if _, err := ensureBillingSettlementReservedTx(tx, *payload.Settlement); err != nil {
				return err
			}
		}
		event, err = transitionBillingSettlementTx(tx, payload.Transition, payload.Adjustment)
		if err != nil {
			return err
		}
		if projection != nil {
			if _, err := ensureBillingProjectionPendingTx(tx, *projection); err != nil {
				return err
			}
		}
		appliedAt := time.Now().UTC()
		result := tx.Model(&BillingTerminalRecovery{}).
			Where("id = ? AND status = ?", row.Id, BillingTerminalRecoveryStatusPending).
			Updates(map[string]interface{}{
				"status": BillingTerminalRecoveryStatusApplied, "applied_at": appliedAt,
				"last_error": "", "updated_at": appliedAt,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("billing terminal recovery state changed concurrently")
		}
		return nil
	})
	if err != nil {
		_ = DB.Model(&BillingTerminalRecovery{}).
			Where("recovery_key = ? AND status = ?", recoveryKey, BillingTerminalRecoveryStatusPending).
			Updates(map[string]interface{}{
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"last_error":    truncateBillingRefundError(err.Error()), "updated_at": time.Now().UTC(),
			}).Error
		return &BillingTerminalRecoveryPendingError{RecoveryKey: recoveryKey, Err: err}
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return &BillingSettlementApplyPendingError{Err: err}
	}
	applyBillingProjectionBestEffort(projection)
	return nil
}

func GetBillingTerminalRecovery(requestId string, operation string, phases ...string) (*BillingTerminalRecovery, error) {
	var row BillingTerminalRecovery
	err := DB.Where("recovery_key = ?", BillingTerminalRecoveryKey(requestId, operation, phases...)).First(&row).Error
	return &row, err
}

func ReconcilePendingBillingTerminalRecoveries(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []BillingTerminalRecovery
	if err := DB.Where("status = ?", BillingTerminalRecoveryStatusPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for i := range rows {
		err := ApplyBillingTerminalRecovery(rows[i].RecoveryKey)
		if err == nil {
			applied++
			continue
		}
		var financialPending *BillingSettlementApplyPendingError
		if errors.As(err, &financialPending) {
			// The journal itself was applied. Settlement reconciliation owns the
			// now-durable financial follow-up.
			applied++
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return applied, firstErr
}
