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
	"gorm.io/gorm/schema"
)

const (
	TaskSubmissionKindTask       = "task"
	TaskSubmissionKindMidjourney = "midjourney"

	TaskSubmissionStatusPreparing = "preparing"
	TaskSubmissionStatusUncertain = "uncertain"
	TaskSubmissionStatusAccepted  = "accepted"
	TaskSubmissionStatusCommitted = "committed"
	TaskSubmissionStatusAborted   = "aborted"
)

// TaskSubmissionRecovery is the durable boundary around an asynchronous
// provider submission. A row exists before the request is sent. Once the
// provider ACK is known, Payload contains the exact local task, settlement,
// adjustment, and projection facts needed to finish without calling upstream
// again.
type TaskSubmissionRecovery struct {
	Id                     int                   `json:"id"`
	RequestId              string                `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_task_submission_request_kind,priority:1"`
	Kind                   string                `json:"kind" gorm:"type:varchar(16);not null;uniqueIndex:idx_task_submission_request_kind,priority:2"`
	Status                 string                `json:"status" gorm:"type:varchar(16);not null;index"`
	IdempotencyFingerprint *string               `json:"idempotency_fingerprint,omitempty" gorm:"type:char(64)"`
	RequestFingerprint     string                `json:"request_fingerprint" gorm:"type:char(64)"`
	UserId                 int                   `json:"user_id" gorm:"index"`
	TokenId                int                   `json:"token_id" gorm:"index"`
	TenantId               int64                 `json:"tenant_id" gorm:"type:bigint;index"`
	Host                   string                `json:"host" gorm:"type:varchar(255)"`
	Route                  string                `json:"route" gorm:"type:varchar(255)"`
	Method                 string                `json:"method" gorm:"type:varchar(8)"`
	ChannelId              int                   `json:"channel_id" gorm:"index"`
	Provider               string                `json:"provider" gorm:"type:varchar(64)"`
	Model                  string                `json:"model" gorm:"type:varchar(191)"`
	PublicTaskId           string                `json:"public_task_id" gorm:"type:varchar(191)"`
	Action                 string                `json:"action" gorm:"type:varchar(64)"`
	UsingGroup             string                `json:"using_group" gorm:"type:varchar(64)"`
	InitialQuota           int                   `json:"initial_quota"`
	AttemptedAt            *time.Time            `json:"attempted_at" gorm:"index"`
	Resolution             string                `json:"resolution" gorm:"type:varchar(16)"`
	ResolvedBy             int                   `json:"resolved_by"`
	ResolvedAt             *time.Time            `json:"resolved_at" gorm:"index"`
	ProviderTaskId         string                `json:"provider_task_id" gorm:"type:varchar(191)"`
	PayloadHash            string                `json:"payload_hash" gorm:"type:varchar(64);not null"`
	Payload                TaskSubmissionPayload `json:"payload" gorm:"not null"`
	LocalTaskId            int64                 `json:"local_task_id" gorm:"type:bigint;not null"`
	AttemptCount           int                   `json:"attempt_count" gorm:"not null"`
	LastError              string                `json:"last_error" gorm:"type:varchar(512);not null"`
	AcceptedAt             *time.Time            `json:"accepted_at"`
	CommittedAt            *time.Time            `json:"committed_at"`
	CreatedAt              time.Time             `json:"created_at" gorm:"not null"`
	UpdatedAt              time.Time             `json:"updated_at" gorm:"not null;index"`
}

type TaskSubmissionPayload string

func (TaskSubmissionPayload) GormDataType() string {
	return "text"
}

func (TaskSubmissionPayload) GormDBDataType(db *gorm.DB, _ *schema.Field) string {
	switch db.Dialector.Name() {
	case "mysql":
		return "LONGTEXT"
	default:
		return "TEXT"
	}
}

// MidjourneySubmissionBilling contains fields intentionally hidden from the
// public Midjourney JSON representation but required by the local DB row.
type MidjourneySubmissionBilling struct {
	TokenId                int     `json:"token_id"`
	Group                  string  `json:"group"`
	ChargedGroupRatio      float64 `json:"charged_group_ratio"`
	BillingSource          string  `json:"billing_source"`
	SubscriptionId         int     `json:"subscription_id"`
	SubscriptionResetEpoch int64   `json:"subscription_reset_epoch"`
	SubscriptionOccurredAt int64   `json:"subscription_occurred_at"`
	BillingRequestId       string  `json:"billing_request_id"`
}

// TaskSubmissionCommitPayload is private durable state, never an API response.
// Task.PrivateData uses its existing JSON contract explicitly because Task's
// public JSON representation deliberately omits it.
type TaskSubmissionCommitPayload struct {
	Task              *Task                         `json:"task,omitempty"`
	TaskPrivateData   *TaskPrivateData              `json:"task_private_data,omitempty"`
	Midjourney        *Midjourney                   `json:"midjourney,omitempty"`
	MidjourneyBilling *MidjourneySubmissionBilling  `json:"midjourney_billing,omitempty"`
	Transition        *BillingSettlementTransition  `json:"transition,omitempty"`
	Adjustment        *BillingAdjustmentSpec        `json:"adjustment,omitempty"`
	Projection        *BillingProjectionSpec        `json:"projection,omitempty"`
	PublicResponse    *TaskSubmissionPublicResponse `json:"public_response,omitempty"`
}

type TaskSubmissionApplyResult struct {
	Committed   bool
	LocalTaskId int64
	RequestId   string
	Operation   string
}

func normalizeTaskSubmissionIdentity(requestId string, kind string) (string, string, error) {
	requestId = strings.TrimSpace(requestId)
	kind = strings.TrimSpace(kind)
	if requestId == "" || len(requestId) > 128 {
		return "", "", errors.New("task submission recovery request id is invalid")
	}
	if kind != TaskSubmissionKindTask && kind != TaskSubmissionKindMidjourney {
		return "", "", errors.New("task submission recovery kind is invalid")
	}
	return requestId, kind, nil
}

func loadTaskSubmissionRecoveryTx(tx *gorm.DB, requestId string, kind string, lock bool) (*TaskSubmissionRecovery, error) {
	query := tx.Where("request_id = ? AND kind = ?", requestId, kind)
	if lock && !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var row TaskSubmissionRecovery
	if err := query.First(&row).Error; err != nil {
		return nil, err
	}
	return &row, nil
}

func lockPreparingTaskSubmissionTx(tx *gorm.DB, requestId string, kind string) (*TaskSubmissionRecovery, error) {
	if common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		claim := tx.Model(&TaskSubmissionRecovery{}).
			Where("request_id = ? AND kind = ? AND status = ?", requestId, kind, TaskSubmissionStatusPreparing).
			UpdateColumn("updated_at", time.Now().UTC())
		if claim.Error != nil {
			return nil, claim.Error
		}
		if claim.RowsAffected != 1 {
			row, err := loadTaskSubmissionRecoveryTx(tx, requestId, kind, false)
			if err != nil {
				return nil, err
			}
			return nil, fmt.Errorf("task submission recovery is not preparing: %s", row.Status)
		}
	}
	row, err := loadTaskSubmissionRecoveryTx(tx, requestId, kind, true)
	if err != nil {
		return nil, err
	}
	if row.Status != TaskSubmissionStatusPreparing {
		return nil, fmt.Errorf("task submission recovery is not preparing: %s", row.Status)
	}
	if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		if err := tx.Model(&TaskSubmissionRecovery{}).Where("id = ?", row.Id).
			UpdateColumn("updated_at", time.Now().UTC()).Error; err != nil {
			return nil, err
		}
	}
	return row, nil
}

func EnsureTaskSubmissionPreparing(requestId string, kind string) (*TaskSubmissionRecovery, error) {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := &TaskSubmissionRecovery{
		RequestId: requestId, Kind: kind, Status: TaskSubmissionStatusPreparing,
		PayloadHash: "", Payload: "", LastError: "", CreatedAt: now, UpdatedAt: now,
	}
	if err := DB.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "request_id"}, {Name: "kind"}}, DoNothing: true,
	}).Create(row).Error; err != nil {
		return nil, err
	}
	return loadTaskSubmissionRecoveryTx(DB, requestId, kind, false)
}

func GetTaskSubmissionRecovery(requestId string, kind string) (*TaskSubmissionRecovery, error) {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return nil, err
	}
	return loadTaskSubmissionRecoveryTx(DB, requestId, kind, false)
}

func updateTaskSubmissionState(requestId string, kind string, from string, to string) error {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return err
	}
	result := DB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ? AND status = ?", requestId, kind, from).
		Updates(map[string]interface{}{"status": to, "last_error": "", "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	row, loadErr := loadTaskSubmissionRecoveryTx(DB, requestId, kind, false)
	if loadErr != nil {
		return loadErr
	}
	if row.Status == to {
		return nil
	}
	return fmt.Errorf("task submission recovery cannot move from %s to %s; current status is %s", from, to, row.Status)
}

func MarkTaskSubmissionUncertain(requestId string, kind string) error {
	return updateTaskSubmissionState(requestId, kind, TaskSubmissionStatusPreparing, TaskSubmissionStatusUncertain)
}

func MarkTaskSubmissionRejected(requestId string, kind string) error {
	return updateTaskSubmissionState(requestId, kind, TaskSubmissionStatusUncertain, TaskSubmissionStatusPreparing)
}

// AbortTaskSubmission is intentionally restricted to preparing. An uncertain
// request may already have been accepted upstream and must never auto-refund.
func AbortTaskSubmission(requestId string, kind string) error {
	return updateTaskSubmissionState(requestId, kind, TaskSubmissionStatusPreparing, TaskSubmissionStatusAborted)
}

func taskSubmissionCancellationAdjustment(event *BillingSettlementEvent) *BillingAdjustmentSpec {
	if event == nil || event.ReservedQuota == 0 && event.SubscriptionPreConsumeRequestId == "" {
		return nil
	}
	adjustment := &BillingAdjustmentSpec{
		RequestId: event.RequestId, Operation: "task_submission_cancel",
	}
	if event.FundingSource == "wallet" {
		adjustment.UserId = event.UserId
		adjustment.UserQuotaDelta = event.ReservedQuota
	} else {
		adjustment.SubscriptionId = event.SubscriptionId
		adjustment.SubscriptionRequestId = event.SubscriptionPreConsumeRequestId
		adjustment.SubscriptionResetEpoch = event.SubscriptionResetEpoch
		adjustment.SubscriptionOccurredAt = event.SubscriptionOccurredAt
		if event.SubscriptionPreConsumeRequestId == "" {
			adjustment.SubscriptionQuotaDelta = -int64(event.ReservedQuota)
		} else {
			adjustment.SubscriptionQuotaDelta = -int64(event.ReservedQuota - event.InitialReservedQuota)
		}
	}
	if event.TokenId > 0 && event.ReservedQuota > 0 {
		adjustment.TokenId = event.TokenId
		adjustment.TokenQuotaDelta = event.ReservedQuota
	}
	return adjustment
}

// AbortPreparingTaskSubmission atomically freezes the settlement cancellation
// (when pre-consume already happened) and marks the unsent recovery row aborted.
// It must never be called for uncertain or accepted rows.
func AbortPreparingTaskSubmission(requestId string, kind string) error {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return err
	}
	var event *BillingSettlementEvent
	err = DB.Transaction(func(tx *gorm.DB) error {
		row, loadErr := loadTaskSubmissionRecoveryTx(tx, requestId, kind, false)
		if loadErr != nil {
			return loadErr
		}
		if row.Status == TaskSubmissionStatusAborted {
			return nil
		}
		row, loadErr = lockPreparingTaskSubmissionTx(tx, requestId, kind)
		if loadErr != nil {
			return loadErr
		}

		settlement, settlementErr := lockBillingSettlementTx(tx, requestId, "request")
		if settlementErr != nil && !errors.Is(settlementErr, gorm.ErrRecordNotFound) {
			return settlementErr
		}
		if settlementErr == nil {
			if settlement.Status != BillingSettlementStatusReserved {
				return fmt.Errorf("pre-send billing settlement is not reserved: %s", settlement.Status)
			}
			event, settlementErr = transitionBillingSettlementTx(tx, BillingSettlementTransition{
				RequestId: requestId, Operation: "request", FinalQuota: 0, Cancel: true,
			}, taskSubmissionCancellationAdjustment(settlement))
			if settlementErr != nil {
				return settlementErr
			}
		}
		now := time.Now().UTC()
		result := tx.Model(&TaskSubmissionRecovery{}).
			Where("id = ? AND status = ?", row.Id, TaskSubmissionStatusPreparing).
			Updates(map[string]interface{}{"status": TaskSubmissionStatusAborted, "last_error": "", "updated_at": now})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("task submission recovery state changed while aborting")
		}
		return nil
	})
	if err != nil || event == nil {
		return err
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return &BillingSettlementApplyPendingError{Err: err}
	}
	return nil
}

func ReconcileStalePreparingTaskSubmissions(olderThan time.Duration, limit int) (int, error) {
	if olderThan <= 0 {
		olderThan = 10 * time.Minute
	}
	if limit <= 0 {
		limit = 100
	}
	var rows []TaskSubmissionRecovery
	if err := DB.Where("status = ? AND updated_at < ?", TaskSubmissionStatusPreparing, time.Now().UTC().Add(-olderThan)).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	aborted := 0
	var firstErr error
	for i := range rows {
		err := AbortPreparingTaskSubmission(rows[i].RequestId, rows[i].Kind)
		var pending *BillingSettlementApplyPendingError
		if err == nil || errors.As(err, &pending) {
			aborted++
			continue
		}
		if firstErr == nil {
			firstErr = err
		}
	}
	return aborted, firstErr
}

func CountStaleUncertainTaskSubmissions(olderThan time.Duration) (int64, error) {
	if olderThan <= 0 {
		olderThan = 10 * time.Minute
	}
	var count int64
	err := DB.Model(&TaskSubmissionRecovery{}).
		Where("status = ? AND updated_at < ?", TaskSubmissionStatusUncertain, time.Now().UTC().Add(-olderThan)).
		Count(&count).Error
	return count, err
}

func validateTaskSubmissionPayload(kind string, requestId string, payload *TaskSubmissionCommitPayload) error {
	if payload == nil {
		return errors.New("task submission recovery payload is missing")
	}
	switch kind {
	case TaskSubmissionKindTask:
		if payload.Task == nil || payload.TaskPrivateData == nil || payload.Midjourney != nil || payload.MidjourneyBilling != nil || payload.Task.ID != 0 || payload.Task.TaskID == "" {
			return errors.New("task submission recovery task payload is invalid")
		}
	case TaskSubmissionKindMidjourney:
		if payload.Midjourney == nil || payload.MidjourneyBilling == nil || payload.Task != nil || payload.TaskPrivateData != nil || payload.Midjourney.Id != 0 || payload.Midjourney.MjId == "" {
			return errors.New("task submission recovery midjourney payload is invalid")
		}
	}
	if payload.PublicResponse != nil {
		if err := payload.PublicResponse.Validate(); err != nil {
			return err
		}
		publicTaskId := taskSubmissionPublicTaskId(kind, payload)
		if !TaskSubmissionPublicResponseIdentifies(kind, publicTaskId, payload.PublicResponse) {
			return errors.New("task submission public response does not identify the public task")
		}
	}
	if payload.Transition != nil {
		if strings.TrimSpace(payload.Transition.RequestId) != requestId || strings.TrimSpace(payload.Transition.Operation) == "" {
			return errors.New("task submission recovery settlement identity is invalid")
		}
		if err := validateSettlementBillingProjection(payload.Projection, *payload.Transition); err != nil {
			return err
		}
		return nil
	}
	if payload.Adjustment != nil {
		return errors.New("unbilled task submission cannot contain a billing adjustment")
	}
	if payload.Projection == nil {
		return nil
	}
	projection := payload.Projection.normalized()
	if projection.DependencyType != BillingProjectionDependencyTaskSubmission ||
		projection.DependencyRequestId != requestId || projection.DependencyOperation != kind {
		return errors.New("unbilled task submission projection identity is invalid")
	}
	return projection.validate()
}

func validateTaskSubmissionSettlementRoot(kind string, payload *TaskSubmissionCommitPayload, settlement *BillingSettlementEvent) error {
	if payload == nil || payload.Transition == nil {
		return nil
	}
	if settlement == nil {
		return errors.New("task submission settlement root is unavailable")
	}
	if settlement.Status != BillingSettlementStatusReserved || settlement.UserId <= 0 {
		return errors.New("task submission settlement root is not reserved")
	}
	var userId, tokenId, subscriptionId int
	fundingSource := ""
	switch kind {
	case TaskSubmissionKindTask:
		userId = payload.Task.UserId
		tokenId = payload.TaskPrivateData.TokenId
		subscriptionId = payload.TaskPrivateData.SubscriptionId
		fundingSource = payload.TaskPrivateData.BillingSource
		if payload.Transition.FinalQuota != payload.Task.Quota {
			return errors.New("task submission settlement quota does not match task")
		}
	case TaskSubmissionKindMidjourney:
		userId = payload.Midjourney.UserId
		tokenId = payload.MidjourneyBilling.TokenId
		subscriptionId = payload.MidjourneyBilling.SubscriptionId
		fundingSource = payload.MidjourneyBilling.BillingSource
		if payload.Transition.FinalQuota != payload.Midjourney.Quota {
			return errors.New("midjourney submission settlement quota does not match task")
		}
	}
	if fundingSource == "" {
		fundingSource = "wallet"
	}
	if settlement.UserId != userId || settlement.TokenId != tokenId || settlement.SubscriptionId != subscriptionId || settlement.FundingSource != fundingSource {
		return errors.New("task submission settlement identity or funding source does not match")
	}
	return nil
}

func FreezeAcceptedTaskSubmission(requestId string, kind string, payload TaskSubmissionCommitPayload) error {
	return freezeAcceptedTaskSubmission(requestId, kind, payload, 0)
}

func FreezeAcceptedTaskSubmissionResolved(requestId string, kind string, payload TaskSubmissionCommitPayload, adminId int) error {
	if adminId <= 0 {
		return errors.New("task submission resolution administrator is invalid")
	}
	return freezeAcceptedTaskSubmission(requestId, kind, payload, adminId)
}

func freezeAcceptedTaskSubmission(requestId string, kind string, payload TaskSubmissionCommitPayload, adminId int) error {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return err
	}
	if err := validateTaskSubmissionPayload(kind, requestId, &payload); err != nil {
		return err
	}
	encoded, err := common.Marshal(payload)
	if err != nil {
		return err
	}
	hash := fmt.Sprintf("%x", sha256.Sum256(encoded))
	now := time.Now().UTC()
	err = DB.Transaction(func(tx *gorm.DB) error {
		current, loadErr := loadTaskSubmissionRecoveryTx(tx, requestId, kind, true)
		if loadErr != nil {
			return loadErr
		}
		if current.Status == TaskSubmissionStatusAccepted || current.Status == TaskSubmissionStatusCommitted {
			if current.PayloadHash != hash || current.Payload != TaskSubmissionPayload(encoded) {
				return errors.New("task submission ACK retry does not match persisted payload")
			}
			if adminId > 0 {
				return tx.Model(&TaskSubmissionRecovery{}).Where("id = ? AND (resolution IS NULL OR resolution = '' OR resolution = ?)",
					current.Id, TaskSubmissionResolutionAccepted).Updates(map[string]interface{}{
					"resolution": TaskSubmissionResolutionAccepted, "resolved_by": adminId, "resolved_at": now,
					"provider_task_id": taskSubmissionProviderTaskId(kind, &payload), "updated_at": now,
				}).Error
			}
			return nil
		}
		if current.Status != TaskSubmissionStatusPreparing && current.Status != TaskSubmissionStatusUncertain {
			return fmt.Errorf("task submission ACK cannot be frozen from status %s", current.Status)
		}
		updates := map[string]interface{}{
			"status": TaskSubmissionStatusAccepted, "payload": TaskSubmissionPayload(encoded), "payload_hash": hash,
			"provider_task_id": taskSubmissionProviderTaskId(kind, &payload),
			"accepted_at":      now, "last_error": "", "updated_at": now,
		}
		if adminId > 0 {
			updates["resolution"] = TaskSubmissionResolutionAccepted
			updates["resolved_by"] = adminId
			updates["resolved_at"] = now
		}
		result := tx.Model(&TaskSubmissionRecovery{}).
			Where("id = ? AND status = ?", current.Id, current.Status).
			Updates(updates)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("task submission recovery state changed while freezing ACK")
		}
		if payload.Transition != nil {
			settlement, settlementErr := lockBillingSettlementTx(tx, payload.Transition.RequestId, payload.Transition.Operation)
			if settlementErr != nil {
				return fmt.Errorf("task submission settlement root is unavailable: %w", settlementErr)
			}
			if err := validateTaskSubmissionSettlementRoot(kind, &payload, settlement); err != nil {
				return err
			}
		}
		return nil
	})
	return err
}

func decodeTaskSubmissionPayload(row *TaskSubmissionRecovery) (*TaskSubmissionCommitPayload, error) {
	if row == nil || row.Payload == "" {
		return nil, errors.New("task submission recovery payload is empty")
	}
	payloadHash := fmt.Sprintf("%x", sha256.Sum256([]byte(string(row.Payload))))
	if row.PayloadHash == "" || payloadHash != row.PayloadHash {
		return nil, errors.New("task submission recovery payload hash mismatch")
	}
	var payload TaskSubmissionCommitPayload
	if err := common.UnmarshalJsonStr(string(row.Payload), &payload); err != nil {
		return nil, err
	}
	if err := validateTaskSubmissionPayload(row.Kind, row.RequestId, &payload); err != nil {
		return nil, err
	}
	return &payload, nil
}

func restoreMidjourneyBilling(task *Midjourney, billing *MidjourneySubmissionBilling) {
	task.TokenId = billing.TokenId
	task.Group = billing.Group
	task.ChargedGroupRatio = billing.ChargedGroupRatio
	task.BillingSource = billing.BillingSource
	task.SubscriptionId = billing.SubscriptionId
	task.SubscriptionResetEpoch = billing.SubscriptionResetEpoch
	task.SubscriptionOccurredAt = billing.SubscriptionOccurredAt
	task.BillingRequestId = billing.BillingRequestId
}

// ApplyAcceptedTaskSubmission performs only main-database work in the commit
// transaction. Financial application and projection sinks remain durable
// follow-ups and are invoked immediately after the transaction.
func ApplyAcceptedTaskSubmission(requestId string, kind string) (TaskSubmissionApplyResult, error) {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return TaskSubmissionApplyResult{}, err
	}
	result := TaskSubmissionApplyResult{RequestId: requestId}
	var event *BillingSettlementEvent
	var projection *BillingProjectionSpec
	err = DB.Transaction(func(tx *gorm.DB) error {
		claim := tx.Model(&TaskSubmissionRecovery{}).
			Where("request_id = ? AND kind = ? AND status = ?", requestId, kind, TaskSubmissionStatusAccepted).
			UpdateColumn("attempt_count", gorm.Expr("attempt_count + 1"))
		if claim.Error != nil {
			return claim.Error
		}
		row, loadErr := loadTaskSubmissionRecoveryTx(tx, requestId, kind, true)
		if loadErr != nil {
			return loadErr
		}
		if row.Status == TaskSubmissionStatusCommitted {
			result.Committed = true
			result.LocalTaskId = row.LocalTaskId
			return nil
		}
		if claim.RowsAffected != 1 || row.Status != TaskSubmissionStatusAccepted {
			return fmt.Errorf("task submission recovery cannot be applied from status %s", row.Status)
		}
		payload, decodeErr := decodeTaskSubmissionPayload(row)
		if decodeErr != nil {
			return decodeErr
		}
		if payload.Transition != nil {
			result.Operation = strings.TrimSpace(payload.Transition.Operation)
		}
		switch kind {
		case TaskSubmissionKindTask:
			task := *payload.Task
			task.PrivateData = *payload.TaskPrivateData
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
			result.LocalTaskId = task.ID
		case TaskSubmissionKindMidjourney:
			task := *payload.Midjourney
			restoreMidjourneyBilling(&task, payload.MidjourneyBilling)
			if err := tx.Create(&task).Error; err != nil {
				return err
			}
			result.LocalTaskId = int64(task.Id)
		}
		if payload.Transition != nil {
			event, err = transitionBillingSettlementTx(tx, *payload.Transition, payload.Adjustment)
			if err != nil {
				return err
			}
		}
		projection = payload.Projection
		if projection != nil {
			if _, err := ensureBillingProjectionPendingTx(tx, *projection); err != nil {
				return err
			}
		}
		committedAt := time.Now().UTC()
		state := tx.Model(&TaskSubmissionRecovery{}).
			Where("id = ? AND status = ?", row.Id, TaskSubmissionStatusAccepted).
			Updates(map[string]interface{}{
				"status": TaskSubmissionStatusCommitted, "local_task_id": result.LocalTaskId,
				"committed_at": committedAt, "last_error": "", "updated_at": committedAt,
			})
		if state.Error != nil {
			return state.Error
		}
		if state.RowsAffected != 1 {
			return errors.New("task submission recovery state changed concurrently")
		}
		result.Committed = true
		return nil
	})
	if err != nil {
		_ = DB.Model(&TaskSubmissionRecovery{}).
			Where("request_id = ? AND kind = ? AND status = ?", requestId, kind, TaskSubmissionStatusAccepted).
			Updates(map[string]interface{}{
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"last_error":    truncateBillingRefundError(err.Error()), "updated_at": time.Now().UTC(),
			}).Error
		if row, loadErr := GetTaskSubmissionRecovery(requestId, kind); loadErr == nil && row.Status == TaskSubmissionStatusCommitted {
			result.Committed = true
			result.LocalTaskId = row.LocalTaskId
		}
		return result, err
	}
	if !result.Committed {
		return result, nil
	}
	if event != nil {
		if err := applyBillingSettlementFinancial(event); err != nil {
			return result, &BillingSettlementApplyPendingError{Err: err}
		}
	}
	applyBillingProjectionBestEffort(projection)
	return result, nil
}

func ReconcileAcceptedTaskSubmissions(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []TaskSubmissionRecovery
	if err := DB.Where("status = ?", TaskSubmissionStatusAccepted).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	committed := 0
	var firstErr error
	for i := range rows {
		result, err := ApplyAcceptedTaskSubmission(rows[i].RequestId, rows[i].Kind)
		if result.Committed {
			committed++
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return committed, firstErr
}
