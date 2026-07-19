package model

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"time"

	clickhouseclient "github.com/ClickHouse/clickhouse-go/v2"
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingProjectionStatusPending = "pending"
	BillingProjectionStatusApplied = "applied"

	BillingProjectionDependencySettlement     = "settlement"
	BillingProjectionDependencyAdjustment     = "adjustment"
	BillingProjectionDependencyTaskSubmission = "task_submission"
)

// BillingProjectionOutbox is the exact, durable payload for the rebuildable
// log and aggregate projections that follow a settlement fact. It is inserted
// in the same transaction as the task/Midjourney lifecycle transition.
type BillingProjectionOutbox struct {
	Id                  int    `json:"id"`
	ProjectionKey       string `json:"projection_key" gorm:"type:varchar(64);not null;uniqueIndex"`
	DependencyType      string `json:"dependency_type" gorm:"type:varchar(16);not null"`
	DependencyRequestId string `json:"dependency_request_id" gorm:"type:varchar(128);not null;index:idx_billing_projection_dependency,priority:1"`
	DependencyOperation string `json:"dependency_operation" gorm:"type:varchar(64);not null;index:idx_billing_projection_dependency,priority:2"`
	LogEnabled          bool   `json:"log_enabled" gorm:"not null"`
	LogUserId           int    `json:"log_user_id"`
	LogUsername         string `json:"log_username" gorm:"type:varchar(64)"`
	LogCreatedAt        int64  `json:"log_created_at" gorm:"type:bigint"`
	LogType             int    `json:"log_type"`
	// These projection fields are intentionally unbounded. In particular,
	// expression metadata and provider task payloads can exceed MySQL TEXT's
	// 64 KiB limit; a plain string maps to LONGTEXT on MySQL and TEXT elsewhere.
	LogContent           string    `json:"log_content"`
	LogTokenName         string    `json:"log_token_name" gorm:"type:varchar(64)"`
	LogModelName         string    `json:"log_model_name" gorm:"type:varchar(128)"`
	LogQuota             int       `json:"log_quota"`
	LogPromptTokens      int       `json:"log_prompt_tokens"`
	LogCompletionTokens  int       `json:"log_completion_tokens"`
	LogUseTime           int       `json:"log_use_time"`
	LogIsStream          bool      `json:"log_is_stream" gorm:"not null"`
	LogChannelId         int       `json:"log_channel_id"`
	LogTokenId           int       `json:"log_token_id"`
	LogGroup             string    `json:"log_group" gorm:"type:varchar(64)"`
	LogIp                string    `json:"log_ip" gorm:"type:varchar(64)"`
	LogRequestId         string    `json:"log_request_id" gorm:"type:varchar(64)"`
	LogUpstreamRequestId string    `json:"log_upstream_request_id" gorm:"type:varchar(128)"`
	LogOther             string    `json:"log_other"`
	QuotaDataEnabled     bool      `json:"quota_data_enabled" gorm:"not null"`
	QuotaDataNodeName    string    `json:"quota_data_node_name" gorm:"type:varchar(64)"`
	QuotaDataTokenUsed   int       `json:"quota_data_token_used"`
	UserId               int       `json:"user_id" gorm:"index"`
	UserUsedQuotaDelta   int       `json:"user_used_quota_delta"`
	UserRequestDelta     int       `json:"user_request_delta"`
	ChannelId            int       `json:"channel_id" gorm:"index"`
	ChannelQuotaDelta    int       `json:"channel_quota_delta"`
	Status               string    `json:"status" gorm:"type:varchar(16);not null;index"`
	AttemptCount         int       `json:"attempt_count" gorm:"not null"`
	LastError            string    `json:"last_error" gorm:"type:varchar(512);not null"`
	CreatedAt            time.Time `json:"created_at" gorm:"not null"`
	UpdatedAt            time.Time `json:"updated_at" gorm:"not null;index"`
}

type BillingProjectionSpec struct {
	ProjectionKey        string
	DependencyType       string
	DependencyRequestId  string
	DependencyOperation  string
	LogEnabled           bool
	LogUserId            int
	LogUsername          string
	LogCreatedAt         int64
	LogType              int
	LogContent           string
	LogTokenName         string
	LogModelName         string
	LogQuota             int
	LogPromptTokens      int
	LogCompletionTokens  int
	LogUseTime           int
	LogIsStream          bool
	LogChannelId         int
	LogTokenId           int
	LogGroup             string
	LogIp                string
	LogRequestId         string
	LogUpstreamRequestId string
	LogOther             string
	QuotaDataEnabled     bool
	QuotaDataNodeName    string
	QuotaDataTokenUsed   int
	UserId               int
	UserUsedQuotaDelta   int
	UserRequestDelta     int
	ChannelId            int
	ChannelQuotaDelta    int
}

func (s BillingProjectionSpec) normalized() BillingProjectionSpec {
	s.ProjectionKey = strings.TrimSpace(s.ProjectionKey)
	s.DependencyType = strings.TrimSpace(s.DependencyType)
	if s.DependencyType == "" {
		s.DependencyType = BillingProjectionDependencySettlement
	}
	s.DependencyRequestId = strings.TrimSpace(s.DependencyRequestId)
	s.DependencyOperation = strings.TrimSpace(s.DependencyOperation)
	s.LogUsername = truncateBillingProjectionField(s.LogUsername, 64)
	s.LogTokenName = truncateBillingProjectionField(s.LogTokenName, 64)
	s.LogModelName = truncateBillingProjectionField(s.LogModelName, 128)
	s.LogGroup = truncateBillingProjectionField(s.LogGroup, 64)
	s.LogIp = truncateBillingProjectionField(s.LogIp, 64)
	s.LogRequestId = truncateBillingProjectionField(s.LogRequestId, 64)
	s.LogUpstreamRequestId = truncateBillingProjectionField(s.LogUpstreamRequestId, 128)
	s.QuotaDataNodeName = truncateBillingProjectionField(s.QuotaDataNodeName, 64)
	return s
}

func truncateBillingProjectionField(value string, maxRunes int) string {
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes])
}

func BillingProjectionKey(requestId string, operation string, kind string) string {
	raw := strings.TrimSpace(requestId) + "\x00" + strings.TrimSpace(operation) + "\x00" + strings.TrimSpace(kind)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
}

func (s BillingProjectionSpec) validate() error {
	s = s.normalized()
	if s.ProjectionKey == "" || len(s.ProjectionKey) > 64 || s.DependencyRequestId == "" || len(s.DependencyRequestId) > 128 ||
		s.DependencyOperation == "" || len(s.DependencyOperation) > 64 {
		return errors.New("billing projection identity is invalid")
	}
	if s.DependencyType != BillingProjectionDependencySettlement && s.DependencyType != BillingProjectionDependencyAdjustment &&
		s.DependencyType != BillingProjectionDependencyTaskSubmission {
		return errors.New("billing projection dependency is invalid")
	}
	if s.UserUsedQuotaDelta < 0 || s.UserRequestDelta < 0 || s.ChannelQuotaDelta < 0 {
		return errors.New("billing projection aggregate delta is invalid")
	}
	if (s.UserUsedQuotaDelta != 0 || s.UserRequestDelta != 0) && s.UserId <= 0 {
		return errors.New("billing projection user is invalid")
	}
	if s.ChannelQuotaDelta != 0 && s.ChannelId <= 0 {
		return errors.New("billing projection channel is invalid")
	}
	if s.LogEnabled {
		if s.LogUserId <= 0 || (s.LogType != LogTypeConsume && s.LogType != LogTypeRefund) || s.LogCreatedAt <= 0 || s.LogQuota < 0 ||
			s.LogPromptTokens < 0 || s.LogCompletionTokens < 0 || s.LogUseTime < 0 {
			return errors.New("billing projection log is invalid")
		}
	}
	if s.QuotaDataEnabled {
		if !s.LogEnabled || s.LogType != LogTypeConsume || s.QuotaDataTokenUsed < 0 {
			return errors.New("billing projection quota data is invalid")
		}
	}
	if !s.LogEnabled && s.UserUsedQuotaDelta == 0 && s.UserRequestDelta == 0 && s.ChannelQuotaDelta == 0 {
		return errors.New("billing projection has no effect")
	}
	return nil
}

func validateSettlementBillingProjection(spec *BillingProjectionSpec, transition BillingSettlementTransition) error {
	if spec == nil {
		return nil
	}
	normalized := spec.normalized()
	if normalized.DependencyType != BillingProjectionDependencySettlement ||
		normalized.DependencyRequestId != strings.TrimSpace(transition.RequestId) ||
		normalized.DependencyOperation != strings.TrimSpace(transition.Operation) {
		return errors.New("billing projection does not match settlement transition")
	}
	return normalized.validate()
}

func billingProjectionMatches(row *BillingProjectionOutbox, spec BillingProjectionSpec) bool {
	spec = spec.normalized()
	return row.DependencyType == spec.DependencyType && row.DependencyRequestId == spec.DependencyRequestId &&
		row.DependencyOperation == spec.DependencyOperation && row.LogEnabled == spec.LogEnabled &&
		row.LogUserId == spec.LogUserId && row.LogUsername == spec.LogUsername && row.LogCreatedAt == spec.LogCreatedAt &&
		row.LogType == spec.LogType && row.LogContent == spec.LogContent && row.LogTokenName == spec.LogTokenName &&
		row.LogModelName == spec.LogModelName && row.LogQuota == spec.LogQuota && row.LogPromptTokens == spec.LogPromptTokens &&
		row.LogCompletionTokens == spec.LogCompletionTokens && row.LogUseTime == spec.LogUseTime && row.LogIsStream == spec.LogIsStream && row.LogChannelId == spec.LogChannelId &&
		row.LogTokenId == spec.LogTokenId && row.LogGroup == spec.LogGroup && row.LogIp == spec.LogIp &&
		row.LogRequestId == spec.LogRequestId && row.LogUpstreamRequestId == spec.LogUpstreamRequestId && row.LogOther == spec.LogOther &&
		row.QuotaDataEnabled == spec.QuotaDataEnabled && row.QuotaDataNodeName == spec.QuotaDataNodeName && row.QuotaDataTokenUsed == spec.QuotaDataTokenUsed &&
		row.UserId == spec.UserId && row.UserUsedQuotaDelta == spec.UserUsedQuotaDelta && row.UserRequestDelta == spec.UserRequestDelta &&
		row.ChannelId == spec.ChannelId && row.ChannelQuotaDelta == spec.ChannelQuotaDelta
}

func ensureBillingProjectionPendingTx(tx *gorm.DB, spec BillingProjectionSpec) (*BillingProjectionOutbox, error) {
	if tx == nil {
		return nil, errors.New("billing projection transaction is nil")
	}
	spec = spec.normalized()
	if err := spec.validate(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	row := &BillingProjectionOutbox{
		ProjectionKey: spec.ProjectionKey, DependencyType: spec.DependencyType,
		DependencyRequestId: spec.DependencyRequestId, DependencyOperation: spec.DependencyOperation, LogEnabled: spec.LogEnabled,
		LogUserId: spec.LogUserId, LogUsername: spec.LogUsername, LogCreatedAt: spec.LogCreatedAt, LogType: spec.LogType,
		LogContent: spec.LogContent, LogTokenName: spec.LogTokenName, LogModelName: spec.LogModelName, LogQuota: spec.LogQuota,
		LogPromptTokens: spec.LogPromptTokens, LogCompletionTokens: spec.LogCompletionTokens, LogUseTime: spec.LogUseTime, LogIsStream: spec.LogIsStream,
		LogChannelId: spec.LogChannelId, LogTokenId: spec.LogTokenId, LogGroup: spec.LogGroup, LogIp: spec.LogIp,
		LogRequestId: spec.LogRequestId, LogUpstreamRequestId: spec.LogUpstreamRequestId, LogOther: spec.LogOther,
		QuotaDataEnabled: spec.QuotaDataEnabled, QuotaDataNodeName: spec.QuotaDataNodeName, QuotaDataTokenUsed: spec.QuotaDataTokenUsed,
		UserId: spec.UserId, UserUsedQuotaDelta: spec.UserUsedQuotaDelta, UserRequestDelta: spec.UserRequestDelta,
		ChannelId: spec.ChannelId, ChannelQuotaDelta: spec.ChannelQuotaDelta,
		Status: BillingProjectionStatusPending, CreatedAt: now, UpdatedAt: now,
	}
	if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "projection_key"}}, DoNothing: true}).Create(row).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("projection_key = ?", spec.ProjectionKey).First(row).Error; err != nil {
		return nil, err
	}
	if !billingProjectionMatches(row, spec) {
		return nil, errors.New("billing projection retry does not match persisted payload")
	}
	return row, nil
}

func billingProjectionLogMatches(log *Log, row *BillingProjectionOutbox) bool {
	return log.UserId == row.LogUserId && log.Username == row.LogUsername && log.CreatedAt == row.LogCreatedAt &&
		log.Type == row.LogType && log.Content == row.LogContent && log.TokenName == row.LogTokenName &&
		log.ModelName == row.LogModelName && log.Quota == row.LogQuota && log.ChannelId == row.LogChannelId &&
		log.TokenId == row.LogTokenId && log.Group == row.LogGroup && log.Ip == row.LogIp &&
		log.RequestId == row.LogRequestId && log.UpstreamRequestId == row.LogUpstreamRequestId && log.Other == row.LogOther &&
		log.PromptTokens == row.LogPromptTokens && log.CompletionTokens == row.LogCompletionTokens && log.UseTime == row.LogUseTime && log.IsStream == row.LogIsStream
}

const billingProjectionLogLookupCondition = "created_at = ? AND projection_key = ?"

func billingProjectionLogLookup(logDB *gorm.DB, row *BillingProjectionOutbox) *gorm.DB {
	return logDB.Where(billingProjectionLogLookupCondition, row.LogCreatedAt, row.ProjectionKey)
}

func persistBillingProjectionLog(tx *gorm.DB, row *BillingProjectionOutbox) error {
	if !row.LogEnabled {
		return nil
	}
	logDB := LOG_DB
	if logDB == nil {
		return errors.New("billing projection log database is unavailable")
	}
	if LOG_DB == DB {
		logDB = tx
	}
	var existing Log
	result := billingProjectionLogLookup(logDB, row).Limit(1).Find(&existing)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected > 0 {
		if !billingProjectionLogMatches(&existing, row) {
			return errors.New("billing projection log payload mismatch")
		}
		return nil
	}
	key := row.ProjectionKey
	logRow := &Log{
		UserId: row.LogUserId, Username: row.LogUsername, CreatedAt: row.LogCreatedAt, Type: row.LogType,
		Content: row.LogContent, TokenName: row.LogTokenName, ModelName: row.LogModelName, Quota: row.LogQuota,
		PromptTokens: row.LogPromptTokens, CompletionTokens: row.LogCompletionTokens, UseTime: row.LogUseTime, IsStream: row.LogIsStream,
		ChannelId: row.LogChannelId, TokenId: row.LogTokenId, Group: row.LogGroup, Ip: row.LogIp,
		RequestId: row.LogRequestId, UpstreamRequestId: row.LogUpstreamRequestId, Other: row.LogOther,
		ProjectionKey: &key,
	}
	var create *gorm.DB
	if common.UsingLogDatabase(common.DatabaseTypeClickHouse) {
		create = createClickHouseBillingProjectionLog(logDB, logRow, row.ProjectionKey)
	} else {
		create = logDB.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "projection_key"}}, DoNothing: true}).Create(logRow)
	}
	if create.Error != nil {
		return create.Error
	}
	result = billingProjectionLogLookup(logDB, row).Limit(1).Find(&existing)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 || !billingProjectionLogMatches(&existing, row) {
		return errors.New("billing projection log persistence could not be verified")
	}
	return nil
}

// createClickHouseBillingProjectionLog gives the ClickHouse sink its own
// idempotency identity. The main outbox claim serializes live workers, while
// the stable insertion token covers the ambiguous case where ClickHouse
// durably accepts a block but the client never receives its acknowledgement.
func createClickHouseBillingProjectionLog(logDB *gorm.DB, logRow *Log, projectionKey string) *gorm.DB {
	ctx := clickhouseclient.Context(context.Background(), clickhouseclient.WithSettings(clickhouseclient.Settings{
		"insert_deduplicate":         1,
		"insert_deduplication_token": projectionKey,
	}))
	return logDB.WithContext(ctx).Create(logRow)
}

func applyBillingProjectionQuotaDataTx(tx *gorm.DB, row *BillingProjectionOutbox) error {
	if !row.QuotaDataEnabled {
		return nil
	}
	createdAt := row.LogCreatedAt - (row.LogCreatedAt % 3600)
	return upsertQuotaData(tx, QuotaData{
		UserID: row.LogUserId, Username: row.LogUsername, ModelName: row.LogModelName,
		CreatedAt: createdAt, UseGroup: row.LogGroup, TokenID: row.LogTokenId, ChannelID: row.LogChannelId,
		NodeName: row.QuotaDataNodeName, TokenUsed: row.QuotaDataTokenUsed, Count: 1, Quota: row.LogQuota,
	})
}

// ApplyBillingProjection applies the log sink and aggregate counters exactly
// once. The main outbox row is locked while projecting. For an external log
// database, projection_key is the stable sink idempotency key: relational
// sinks enforce it with a unique index, while ClickHouse receives the same
// insertion deduplication token on every retry. Exact readback also rejects
// payload drift. User/channel counters and outbox status roll back together in
// the main transaction.
func ApplyBillingProjection(projectionKey string) error {
	projectionKey = strings.TrimSpace(projectionKey)
	if projectionKey == "" {
		return errors.New("billing projection key is empty")
	}
	err := DB.Transaction(func(tx *gorm.DB) error {
		// This must be the first database operation in the transaction. The
		// real increment obtains SQLite's write lock and a row lock on
		// MySQL/PostgreSQL before any external log sink is inspected. A
		// no-op/timestamp claim is insufficient because MySQL can report zero
		// changed rows for it.
		claim := tx.Model(&BillingProjectionOutbox{}).
			Where("projection_key = ? AND status = ?", projectionKey, BillingProjectionStatusPending).
			UpdateColumn("attempt_count", gorm.Expr("attempt_count + 1"))
		if claim.Error != nil {
			return claim.Error
		}
		query := tx.Where("projection_key = ?", projectionKey)
		if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
			query = query.Clauses(clause.Locking{Strength: "UPDATE"})
		}
		var row BillingProjectionOutbox
		if err := query.First(&row).Error; err != nil {
			return err
		}
		if claim.RowsAffected == 0 && row.Status == BillingProjectionStatusApplied {
			return nil
		}
		if claim.RowsAffected != 1 {
			return fmt.Errorf("billing projection could not be claimed from status: %s", row.Status)
		}
		if row.Status == BillingProjectionStatusApplied {
			return nil
		}
		if row.Status != BillingProjectionStatusPending {
			return fmt.Errorf("billing projection status is invalid: %s", row.Status)
		}
		switch row.DependencyType {
		case BillingProjectionDependencySettlement:
			var settlement BillingSettlementEvent
			if err := tx.Where("request_id = ? AND operation = ?", row.DependencyRequestId, row.DependencyOperation).First(&settlement).Error; err != nil {
				return err
			}
			if settlement.FinancialStatus != BillingSettlementFinancialApplied {
				return errors.New("billing projection settlement financial application is pending")
			}
			if settlement.Status != BillingSettlementStatusDeferred && settlement.Status != BillingSettlementStatusFinalized && settlement.Status != BillingSettlementStatusCancelled {
				return errors.New("billing projection settlement lifecycle is not terminal or deferred")
			}
		case BillingProjectionDependencyAdjustment:
			var adjustment BillingAdjustmentIntent
			if err := tx.Where("request_id = ? AND operation = ?", row.DependencyRequestId, row.DependencyOperation).First(&adjustment).Error; err != nil {
				return err
			}
			if adjustment.Status != BillingAdjustmentStatusApplied {
				return errors.New("billing projection adjustment financial application is pending")
			}
		case BillingProjectionDependencyTaskSubmission:
			var submission TaskSubmissionRecovery
			if err := tx.Where("request_id = ? AND kind = ?", row.DependencyRequestId, row.DependencyOperation).First(&submission).Error; err != nil {
				return err
			}
			if submission.Status != TaskSubmissionStatusCommitted {
				return errors.New("billing projection task submission is pending")
			}
		default:
			return fmt.Errorf("billing projection dependency is invalid: %s", row.DependencyType)
		}
		if err := persistBillingProjectionLog(tx, &row); err != nil {
			return err
		}
		if err := applyBillingProjectionQuotaDataTx(tx, &row); err != nil {
			return err
		}
		if row.UserUsedQuotaDelta != 0 || row.UserRequestDelta != 0 {
			// Usage facts outlive account deletion. Update soft-deleted users when
			// the row still exists; a hard-deleted dimension must not poison the
			// durable log/outbox forever (the outbox itself remains the ledger).
			result := tx.Unscoped().Model(&User{}).Where("id = ?", row.UserId).Updates(map[string]interface{}{
				"used_quota":    gorm.Expr("used_quota + ?", row.UserUsedQuotaDelta),
				"request_count": gorm.Expr("request_count + ?", row.UserRequestDelta),
			})
			if result.Error != nil {
				return result.Error
			}
		}
		if row.ChannelQuotaDelta != 0 {
			// Channels can be hard-deleted after an async task is accepted. Match
			// the legacy counter behavior by treating a missing dimension as a
			// terminal no-op while retaining the durable log fact.
			result := tx.Model(&Channel{}).Where("id = ?", row.ChannelId).
				Update("used_quota", gorm.Expr("used_quota + ?", row.ChannelQuotaDelta))
			if result.Error != nil {
				return result.Error
			}
		}
		statusResult := tx.Model(&BillingProjectionOutbox{}).
			Where("id = ? AND status = ?", row.Id, BillingProjectionStatusPending).
			Updates(map[string]interface{}{"status": BillingProjectionStatusApplied, "last_error": "", "updated_at": time.Now().UTC()})
		if statusResult.Error != nil {
			return statusResult.Error
		}
		if statusResult.RowsAffected != 1 {
			return errors.New("billing projection state changed concurrently")
		}
		return nil
	})
	if err != nil {
		_ = DB.Model(&BillingProjectionOutbox{}).
			Where("projection_key = ? AND status = ?", projectionKey, BillingProjectionStatusPending).
			Updates(map[string]interface{}{
				"attempt_count": gorm.Expr("attempt_count + 1"),
				"last_error":    truncateBillingRefundError(err.Error()),
				"updated_at":    time.Now().UTC(),
			}).Error
	}
	return err
}

// ApplyBillingAdjustmentWithProjectionOnce is the compatibility path for
// asynchronous adjustments that predate BillingSettlementEvent. The exact
// financial intent and projection payload are committed together; financial
// application is replayable, and a projection failure remains in the outbox.
func ApplyBillingAdjustmentWithProjectionOnce(adjustment BillingAdjustmentSpec, projection BillingProjectionSpec) error {
	adjustment.RequestId = strings.TrimSpace(adjustment.RequestId)
	adjustment.Operation = strings.TrimSpace(adjustment.Operation)
	projection.DependencyType = BillingProjectionDependencyAdjustment
	projection.DependencyRequestId = adjustment.RequestId
	projection.DependencyOperation = adjustment.Operation
	if err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := ensureBillingAdjustmentPendingTx(tx, adjustment); err != nil {
			return err
		}
		_, err := ensureBillingProjectionPendingTx(tx, projection)
		return err
	}); err != nil {
		return err
	}
	if err := ApplyBillingAdjustment(adjustment.RequestId, adjustment.Operation); err != nil {
		return err
	}
	applyBillingProjectionBestEffort(&projection)
	return nil
}

func ReconcilePendingBillingProjections(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []BillingProjectionOutbox
	if err := DB.Where("status = ?", BillingProjectionStatusPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for i := range rows {
		if err := ApplyBillingProjection(rows[i].ProjectionKey); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

func applyBillingProjectionBestEffort(spec *BillingProjectionSpec) {
	if spec == nil {
		return
	}
	if err := ApplyBillingProjection(spec.ProjectionKey); err != nil {
		common.SysLog("billing projection remains pending: " + err.Error())
	}
}
