package model

import (
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	TaskSubmissionResolutionAccepted = "accepted"
	TaskSubmissionResolutionRejected = "rejected"

	TaskSubmissionPublicResponseMaxBytes = 1 << 20
)

var (
	ErrTaskSubmissionIdempotencyPayloadMismatch = errors.New("idempotency key was already used with a different request")
	ErrTaskSubmissionIdempotencyInProgress      = errors.New("idempotent task submission is still in progress")
	ErrTaskSubmissionReplayUnavailable          = errors.New("idempotent task submission response is unavailable")
)

const taskSubmissionIdempotencyIndexName = "idx_task_submission_idempotency_fingerprint"

// EnsureTaskSubmissionIdempotencyUniqueIndex is deliberately separate from
// AutoMigrate. SQLite cannot add a UNIQUE column to a populated table, so the
// nullable column is migrated first and this single-column index is added in a
// second, idempotent step on all three supported dialects.
func EnsureTaskSubmissionIdempotencyUniqueIndex(db *gorm.DB) error {
	if db == nil {
		return errors.New("task submission idempotency migration database is nil")
	}
	if db.Migrator().HasIndex(&TaskSubmissionRecovery{}, taskSubmissionIdempotencyIndexName) {
		return nil
	}
	err := db.Exec("CREATE UNIQUE INDEX " + taskSubmissionIdempotencyIndexName + " ON task_submission_recoveries (idempotency_fingerprint)").Error
	if err != nil && db.Migrator().HasIndex(&TaskSubmissionRecovery{}, taskSubmissionIdempotencyIndexName) {
		return nil
	}
	return err
}

// TaskSubmissionClaimSpec contains only authenticated identity and one-way
// request material. IdempotencyKey is used to derive a versioned digest and is
// never written to the database.
type TaskSubmissionClaimSpec struct {
	RequestId          string
	Kind               string
	IdempotencyKey     string
	RequestFingerprint string
	UserId             int
	TokenId            int
	TenantId           int64
	Host               string
	Route              string
	Method             string
	PublicTaskId       string
}

type TaskSubmissionClaimResult struct {
	Recovery *TaskSubmissionRecovery
	Owned    bool
}

type TaskSubmissionAttemptMetadata struct {
	UserId       int
	TokenId      int
	ChannelId    int
	Provider     string
	Model        string
	PublicTaskId string
	Action       string
	UsingGroup   string
	InitialQuota int
}

// TaskSubmissionPublicResponse is the exact, client-visible success response
// frozen with a provider ACK. Headers have already passed a strict allowlist;
// credentials, cookies, hop-by-hop fields, and raw upstream headers are never
// persisted.
type TaskSubmissionPublicResponse struct {
	Status  int                 `json:"status"`
	Headers map[string][]string `json:"headers,omitempty"`
	Body    string              `json:"body"`
}

type TaskSubmissionRecoveryView struct {
	RequestId      string     `json:"request_id"`
	Kind           string     `json:"kind"`
	Status         string     `json:"status"`
	UserId         int        `json:"user_id"`
	TokenId        int        `json:"token_id"`
	TenantId       int64      `json:"tenant_id"`
	Host           string     `json:"host"`
	Route          string     `json:"route"`
	Method         string     `json:"method"`
	ChannelId      int        `json:"channel_id"`
	Provider       string     `json:"provider"`
	Model          string     `json:"model"`
	PublicTaskId   string     `json:"public_task_id"`
	ProviderTaskId string     `json:"provider_task_id,omitempty"`
	Action         string     `json:"action"`
	InitialQuota   int        `json:"initial_quota"`
	AttemptCount   int        `json:"attempt_count"`
	HasError       bool       `json:"has_error"`
	AttemptedAt    *time.Time `json:"attempted_at,omitempty"`
	AcceptedAt     *time.Time `json:"accepted_at,omitempty"`
	CommittedAt    *time.Time `json:"committed_at,omitempty"`
	Resolution     string     `json:"resolution,omitempty"`
	ResolvedBy     int        `json:"resolved_by,omitempty"`
	ResolvedAt     *time.Time `json:"resolved_at,omitempty"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
}

type TaskSubmissionAcceptedEvidence struct {
	ProviderTaskId string
	FinalQuota     int
	PublicResponse *TaskSubmissionPublicResponse
}

func validTaskSubmissionIdempotencyKey(key string) bool {
	if len(key) < 1 || len(key) > 128 {
		return false
	}
	for i := 0; i < len(key); i++ {
		ch := key[i]
		if ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' {
			continue
		}
		switch ch {
		case '.', '_', '~', ':', '-':
			continue
		default:
			return false
		}
	}
	return true
}

func taskSubmissionIdentityFingerprint(spec TaskSubmissionClaimSpec) string {
	parts := []string{
		"v1", strconv.FormatInt(spec.TenantId, 10), spec.Host, strconv.Itoa(spec.UserId), strconv.Itoa(spec.TokenId),
		strings.ToUpper(spec.Method), spec.Route, spec.IdempotencyKey,
	}
	var material strings.Builder
	for _, part := range parts {
		material.WriteString(strconv.Itoa(len(part)))
		material.WriteByte(':')
		material.WriteString(part)
	}
	digest := sha256.Sum256([]byte(material.String()))
	return fmt.Sprintf("%x", digest)
}

// TaskSubmissionTenantID reads the fork's authoritative user attribution.
// Startup migration guarantees users.tenant_id exists before relay traffic;
// query/schema failures are fail-closed so an identity is never silently
// moved into the platform tenant. Request headers never participate.
func TaskSubmissionTenantID(userId int) (int64, error) {
	if userId <= 0 {
		return 0, errors.New("task submission user is invalid")
	}
	var row struct {
		TenantId sql.NullInt64 `gorm:"column:tenant_id"`
	}
	if err := DB.Table("users").Select("tenant_id").Where("id = ?", userId).Take(&row).Error; err != nil {
		return 0, err
	}
	if !row.TenantId.Valid {
		return 0, nil
	}
	if row.TenantId.Int64 < 0 {
		return 0, errors.New("task submission tenant is invalid")
	}
	return row.TenantId.Int64, nil
}

func normalizeTaskSubmissionClaim(spec TaskSubmissionClaimSpec) (TaskSubmissionClaimSpec, *string, error) {
	requestId, kind, err := normalizeTaskSubmissionIdentity(spec.RequestId, spec.Kind)
	if err != nil {
		return TaskSubmissionClaimSpec{}, nil, err
	}
	spec.RequestId = requestId
	spec.Kind = kind
	spec.Method = strings.ToUpper(strings.TrimSpace(spec.Method))
	spec.Host = strings.ToLower(strings.TrimSpace(spec.Host))
	spec.Route = strings.TrimSpace(spec.Route)
	spec.PublicTaskId = strings.TrimSpace(spec.PublicTaskId)
	if spec.UserId <= 0 || spec.TokenId < 0 || spec.TenantId < 0 || spec.Host == "" || len(spec.Host) > 255 ||
		spec.Method == "" || len(spec.Method) > 8 || spec.Route == "" || len(spec.Route) > 255 {
		return TaskSubmissionClaimSpec{}, nil, errors.New("task submission authenticated identity is invalid")
	}
	if len(spec.PublicTaskId) > 191 || len(spec.RequestFingerprint) != sha256.Size*2 {
		return TaskSubmissionClaimSpec{}, nil, errors.New("task submission request fingerprint is invalid")
	}
	if spec.IdempotencyKey == "" {
		return spec, nil, nil
	}
	if !validTaskSubmissionIdempotencyKey(spec.IdempotencyKey) {
		return TaskSubmissionClaimSpec{}, nil, errors.New("Idempotency-Key must be 1-128 ASCII letters, digits, or . _ ~ : -")
	}
	fingerprint := taskSubmissionIdentityFingerprint(spec)
	return spec, &fingerprint, nil
}

// ClaimTaskSubmission creates the provider-call owner before any upstream
// request. A concurrent request with the same authenticated idempotency
// identity can observe the row, but can never become a second owner.
func ClaimTaskSubmission(spec TaskSubmissionClaimSpec) (TaskSubmissionClaimResult, error) {
	spec, idempotencyFingerprint, err := normalizeTaskSubmissionClaim(spec)
	if err != nil {
		return TaskSubmissionClaimResult{}, err
	}
	now := time.Now().UTC()
	row := &TaskSubmissionRecovery{
		RequestId: spec.RequestId, Kind: spec.Kind, Status: TaskSubmissionStatusPreparing,
		IdempotencyFingerprint: idempotencyFingerprint, RequestFingerprint: spec.RequestFingerprint,
		UserId: spec.UserId, TokenId: spec.TokenId, TenantId: spec.TenantId, Host: spec.Host,
		Route: spec.Route, Method: spec.Method,
		PublicTaskId: spec.PublicTaskId, PayloadHash: "", Payload: "", LastError: "", Resolution: "",
		CreatedAt: now, UpdatedAt: now,
	}
	created := false
	err = DB.Transaction(func(tx *gorm.DB) error {
		create := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(row)
		if create.Error != nil {
			return create.Error
		}
		created = create.RowsAffected == 1
		if created {
			return nil
		}
		row = &TaskSubmissionRecovery{}
		query := tx
		if idempotencyFingerprint != nil {
			query = query.Where("idempotency_fingerprint = ?", *idempotencyFingerprint)
		} else {
			query = query.Where("request_id = ? AND kind = ?", spec.RequestId, spec.Kind)
		}
		if err := query.First(row).Error; err != nil {
			return err
		}
		if row.Kind != spec.Kind || row.UserId != spec.UserId || row.TokenId != spec.TokenId ||
			row.TenantId != spec.TenantId || row.Host != spec.Host || row.Route != spec.Route ||
			row.Method != spec.Method || row.RequestFingerprint != spec.RequestFingerprint {
			return ErrTaskSubmissionIdempotencyPayloadMismatch
		}
		return nil
	})
	if err != nil {
		return TaskSubmissionClaimResult{}, err
	}
	return TaskSubmissionClaimResult{Recovery: row, Owned: created}, nil
}

// LookupTaskSubmissionClaim is the read-only fast path used before reserving
// local response-spool capacity. It is only meaningful for an explicit client
// idempotency key. A miss is never treated as a claim; the later insert remains
// the sole ownership decision.
func LookupTaskSubmissionClaim(spec TaskSubmissionClaimSpec) (*TaskSubmissionRecovery, bool, error) {
	spec, idempotencyFingerprint, err := normalizeTaskSubmissionClaim(spec)
	if err != nil {
		return nil, false, err
	}
	if idempotencyFingerprint == nil {
		return nil, false, nil
	}
	var row TaskSubmissionRecovery
	err = DB.Where("idempotency_fingerprint = ?", *idempotencyFingerprint).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	if row.Kind != spec.Kind || row.UserId != spec.UserId || row.TokenId != spec.TokenId ||
		row.TenantId != spec.TenantId || row.Host != spec.Host || row.Route != spec.Route ||
		row.Method != spec.Method || row.RequestFingerprint != spec.RequestFingerprint {
		return nil, true, ErrTaskSubmissionIdempotencyPayloadMismatch
	}
	return &row, true, nil
}

func normalizeTaskSubmissionAttemptMetadata(metadata TaskSubmissionAttemptMetadata) (TaskSubmissionAttemptMetadata, error) {
	metadata.Provider = strings.TrimSpace(metadata.Provider)
	metadata.Model = strings.TrimSpace(metadata.Model)
	metadata.PublicTaskId = strings.TrimSpace(metadata.PublicTaskId)
	metadata.Action = strings.TrimSpace(metadata.Action)
	metadata.UsingGroup = strings.TrimSpace(metadata.UsingGroup)
	if metadata.UserId <= 0 || metadata.TokenId < 0 || metadata.ChannelId <= 0 || metadata.InitialQuota < 0 {
		return TaskSubmissionAttemptMetadata{}, errors.New("task submission attempt identity is invalid")
	}
	if metadata.Provider == "" || len(metadata.Provider) > 64 || len(metadata.Model) > 191 ||
		len(metadata.PublicTaskId) > 191 || len(metadata.Action) > 64 || len(metadata.UsingGroup) > 64 {
		return TaskSubmissionAttemptMetadata{}, errors.New("task submission attempt metadata is invalid")
	}
	return metadata, nil
}

// MarkTaskSubmissionUncertainWithMetadata persists the safe investigation
// envelope in the same state transition that closes the provider-send gate.
func MarkTaskSubmissionUncertainWithMetadata(requestId string, kind string, metadata TaskSubmissionAttemptMetadata) error {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return err
	}
	metadata, err = normalizeTaskSubmissionAttemptMetadata(metadata)
	if err != nil {
		return err
	}
	now := time.Now().UTC()
	result := DB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ? AND status = ? AND user_id = ? AND token_id = ?", requestId, kind, TaskSubmissionStatusPreparing, metadata.UserId, metadata.TokenId).
		Updates(map[string]interface{}{
			"status": TaskSubmissionStatusUncertain, "channel_id": metadata.ChannelId,
			"provider": metadata.Provider, "model": metadata.Model, "public_task_id": metadata.PublicTaskId,
			"action": metadata.Action, "using_group": metadata.UsingGroup, "initial_quota": metadata.InitialQuota,
			"attempted_at": now, "last_error": "", "updated_at": now,
		})
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
	return fmt.Errorf("task submission cannot begin provider attempt from status %s", row.Status)
}

func taskSubmissionResponseHeaderAllowed(name string) bool {
	switch http.CanonicalHeaderKey(strings.TrimSpace(name)) {
	case "Content-Type", "Content-Language", "Location", "Retry-After", "X-Request-Id", "X-Oneapi-Request-Id", "X-New-Api-Other-Ratios":
		return true
	default:
		return false
	}
}

func (response *TaskSubmissionPublicResponse) Validate() error {
	if response == nil {
		return errors.New("task submission public response is missing")
	}
	if response.Status < http.StatusOK || response.Status >= http.StatusMultipleChoices {
		return errors.New("task submission public response status is invalid")
	}
	if len(response.Body) == 0 || len(response.Body) > TaskSubmissionPublicResponseMaxBytes || !utf8.ValidString(response.Body) {
		return errors.New("task submission public response body is invalid")
	}
	var decoded interface{}
	if err := common.Unmarshal([]byte(response.Body), &decoded); err != nil {
		return errors.New("task submission public response must be valid JSON")
	}
	seenHeaders := make(map[string]struct{}, len(response.Headers))
	for name, values := range response.Headers {
		canonicalName := http.CanonicalHeaderKey(strings.TrimSpace(name))
		if _, duplicate := seenHeaders[canonicalName]; duplicate {
			return errors.New("task submission public response contains duplicate canonical headers")
		}
		seenHeaders[canonicalName] = struct{}{}
		if !taskSubmissionResponseHeaderAllowed(name) || len(values) == 0 || len(values) > 8 {
			return errors.New("task submission public response header is not allowed")
		}
		totalBytes := 0
		for _, value := range values {
			totalBytes += len(value)
			if len(value) > 1024 || strings.IndexFunc(value, func(r rune) bool {
				return r < 0x20 || r == 0x7f
			}) >= 0 {
				return errors.New("task submission public response header value is invalid")
			}
		}
		if totalBytes > 4096 {
			return errors.New("task submission public response header values are too large")
		}
	}
	return nil
}

func TaskSubmissionPublicResponseIdentifies(kind string, expectedTaskId string, response *TaskSubmissionPublicResponse) bool {
	expectedTaskId = strings.TrimSpace(expectedTaskId)
	if response == nil || expectedTaskId == "" {
		return false
	}
	var object map[string]interface{}
	if err := common.Unmarshal([]byte(response.Body), &object); err != nil {
		return false
	}
	keys := []string{"id", "task_id", "data"}
	if kind == TaskSubmissionKindMidjourney {
		keys = []string{"result"}
	}
	for _, key := range keys {
		if value, ok := object[key].(string); ok && value == expectedTaskId {
			return true
		}
	}
	return false
}

func GetTaskSubmissionPublicResponse(row *TaskSubmissionRecovery) (*TaskSubmissionPublicResponse, error) {
	if row == nil || row.Status != TaskSubmissionStatusAccepted && row.Status != TaskSubmissionStatusCommitted {
		return nil, ErrTaskSubmissionReplayUnavailable
	}
	payload, err := decodeTaskSubmissionPayload(row)
	if err != nil {
		return nil, err
	}
	if payload.PublicResponse == nil {
		return nil, ErrTaskSubmissionReplayUnavailable
	}
	if err := payload.PublicResponse.Validate(); err != nil {
		return nil, err
	}
	copyResponse := *payload.PublicResponse
	copyResponse.Headers = make(map[string][]string, len(payload.PublicResponse.Headers))
	for name, values := range payload.PublicResponse.Headers {
		copyResponse.Headers[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	return &copyResponse, nil
}

func GetTaskSubmissionAcceptedEvidence(row *TaskSubmissionRecovery) (TaskSubmissionAcceptedEvidence, error) {
	response, err := GetTaskSubmissionPublicResponse(row)
	if err != nil {
		return TaskSubmissionAcceptedEvidence{}, err
	}
	payload, err := decodeTaskSubmissionPayload(row)
	if err != nil {
		return TaskSubmissionAcceptedEvidence{}, err
	}
	finalQuota := 0
	if payload.Transition != nil {
		finalQuota = payload.Transition.FinalQuota
	} else if row.Kind == TaskSubmissionKindTask && payload.Task != nil {
		finalQuota = payload.Task.Quota
	} else if row.Kind == TaskSubmissionKindMidjourney && payload.Midjourney != nil {
		finalQuota = payload.Midjourney.Quota
	}
	return TaskSubmissionAcceptedEvidence{
		ProviderTaskId: taskSubmissionProviderTaskId(row.Kind, payload), FinalQuota: finalQuota, PublicResponse: response,
	}, nil
}

func taskSubmissionRecoveryView(row TaskSubmissionRecovery) TaskSubmissionRecoveryView {
	return TaskSubmissionRecoveryView{
		RequestId: row.RequestId, Kind: row.Kind, Status: row.Status,
		UserId: row.UserId, TokenId: row.TokenId, TenantId: row.TenantId, Host: row.Host,
		Route: row.Route, Method: row.Method,
		ChannelId: row.ChannelId, Provider: row.Provider, Model: row.Model, PublicTaskId: row.PublicTaskId,
		ProviderTaskId: row.ProviderTaskId, Action: row.Action, InitialQuota: row.InitialQuota,
		AttemptCount: row.AttemptCount, HasError: row.LastError != "", AttemptedAt: row.AttemptedAt,
		AcceptedAt: row.AcceptedAt, CommittedAt: row.CommittedAt, Resolution: row.Resolution,
		ResolvedBy: row.ResolvedBy, ResolvedAt: row.ResolvedAt, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func ListTaskSubmissionRecoveries(status string, kind string, limit int, beforeId int) ([]TaskSubmissionRecoveryView, error) {
	status = strings.TrimSpace(status)
	kind = strings.TrimSpace(kind)
	if status != "" && status != TaskSubmissionStatusPreparing && status != TaskSubmissionStatusUncertain &&
		status != TaskSubmissionStatusAccepted && status != TaskSubmissionStatusCommitted && status != TaskSubmissionStatusAborted {
		return nil, errors.New("task submission recovery status filter is invalid")
	}
	if kind != "" && kind != TaskSubmissionKindTask && kind != TaskSubmissionKindMidjourney {
		return nil, errors.New("task submission recovery kind filter is invalid")
	}
	if limit <= 0 || limit > 100 {
		return nil, errors.New("task submission recovery limit must be between 1 and 100")
	}
	query := DB.Model(&TaskSubmissionRecovery{})
	if status != "" {
		query = query.Where("status = ?", status)
	}
	if kind != "" {
		query = query.Where("kind = ?", kind)
	}
	if beforeId > 0 {
		query = query.Where("id < ?", beforeId)
	}
	var rows []TaskSubmissionRecovery
	if err := query.Order("id desc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	views := make([]TaskSubmissionRecoveryView, 0, len(rows))
	for _, row := range rows {
		views = append(views, taskSubmissionRecoveryView(row))
	}
	return views, nil
}

func GetTaskSubmissionRecoveryView(requestId string, kind string) (TaskSubmissionRecoveryView, error) {
	row, err := GetTaskSubmissionRecovery(requestId, kind)
	if err != nil {
		return TaskSubmissionRecoveryView{}, err
	}
	return taskSubmissionRecoveryView(*row), nil
}

func ListStaleUncertainTaskSubmissionRequestIDs(olderThan time.Duration, limit int) ([]string, error) {
	if olderThan <= 0 {
		olderThan = 10 * time.Minute
	}
	if limit <= 0 || limit > 20 {
		limit = 10
	}
	var ids []string
	err := DB.Model(&TaskSubmissionRecovery{}).
		Where("status = ? AND updated_at < ?", TaskSubmissionStatusUncertain, time.Now().UTC().Add(-olderThan)).
		Order("updated_at asc, id asc").Limit(limit).Pluck("request_id", &ids).Error
	return ids, err
}

// CleanupTerminalTaskSubmissionRecoveries bounds durable response retention.
// Only terminal rows are eligible; preparing, uncertain, and accepted rows are
// recovery authority and are never deleted by this job.
func CleanupTerminalTaskSubmissionRecoveries(retention time.Duration, limit int) (int64, error) {
	if retention < 24*time.Hour {
		return 0, errors.New("task submission idempotency retention must be at least 24 hours")
	}
	if limit <= 0 || limit > 1000 {
		limit = 100
	}
	cutoff := time.Now().UTC().Add(-retention)
	pendingProjection := DB.Model(&BillingProjectionOutbox{}).
		Select("1").
		Where("dependency_type = ?", BillingProjectionDependencyTaskSubmission).
		Where("dependency_request_id = task_submission_recoveries.request_id").
		Where("dependency_operation = task_submission_recoveries.kind").
		Where("status <> ?", BillingProjectionStatusApplied)
	var ids []int
	if err := DB.Model(&TaskSubmissionRecovery{}).
		Where("status IN ? AND updated_at < ?", []string{TaskSubmissionStatusCommitted, TaskSubmissionStatusAborted}, cutoff).
		Where("NOT EXISTS (?)", pendingProjection).
		Order("updated_at asc, id asc").Limit(limit).Pluck("id", &ids).Error; err != nil {
		return 0, err
	}
	if len(ids) == 0 {
		return 0, nil
	}
	result := DB.Where("id IN ? AND status IN ? AND updated_at < ?", ids,
		[]string{TaskSubmissionStatusCommitted, TaskSubmissionStatusAborted}, cutoff).Delete(&TaskSubmissionRecovery{})
	return result.RowsAffected, result.Error
}

func ResolveUncertainTaskSubmissionRejected(requestId string, kind string, adminId int) error {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return err
	}
	if adminId <= 0 {
		return errors.New("task submission resolution administrator is invalid")
	}
	var event *BillingSettlementEvent
	err = DB.Transaction(func(tx *gorm.DB) error {
		row, loadErr := loadTaskSubmissionRecoveryTx(tx, requestId, kind, true)
		if loadErr != nil {
			return loadErr
		}
		if row.Status == TaskSubmissionStatusAborted && row.Resolution == TaskSubmissionResolutionRejected {
			return nil
		}
		if row.Status != TaskSubmissionStatusUncertain {
			return fmt.Errorf("task submission cannot be resolved rejected from status %s", row.Status)
		}
		settlement, settlementErr := lockBillingSettlementTx(tx, requestId, "request")
		if settlementErr != nil && !errors.Is(settlementErr, gorm.ErrRecordNotFound) {
			return settlementErr
		}
		if settlementErr == nil {
			if settlement.Status != BillingSettlementStatusReserved {
				return fmt.Errorf("task submission settlement cannot be cancelled from status %s", settlement.Status)
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
			Where("id = ? AND status = ?", row.Id, TaskSubmissionStatusUncertain).
			Updates(map[string]interface{}{
				"status": TaskSubmissionStatusAborted, "resolution": TaskSubmissionResolutionRejected,
				"resolved_by": adminId, "resolved_at": now, "last_error": "", "updated_at": now,
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("task submission state changed while resolving rejection")
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

func MarkTaskSubmissionResolvedAccepted(requestId string, kind string, adminId int, providerTaskId string) error {
	requestId, kind, err := normalizeTaskSubmissionIdentity(requestId, kind)
	if err != nil {
		return err
	}
	providerTaskId = strings.TrimSpace(providerTaskId)
	if adminId <= 0 || providerTaskId == "" || len(providerTaskId) > 191 || strings.ContainsAny(providerTaskId, "\r\n\t") {
		return errors.New("task submission accepted resolution identity is invalid")
	}
	now := time.Now().UTC()
	result := DB.Model(&TaskSubmissionRecovery{}).
		Where("request_id = ? AND kind = ? AND status IN ? AND (resolution IS NULL OR resolution = '' OR (resolution = ? AND provider_task_id = ?))",
			requestId, kind, []string{TaskSubmissionStatusAccepted, TaskSubmissionStatusCommitted}, TaskSubmissionResolutionAccepted, providerTaskId).
		Updates(map[string]interface{}{
			"resolution": TaskSubmissionResolutionAccepted, "resolved_by": adminId,
			"resolved_at": now, "provider_task_id": providerTaskId, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 1 {
		return nil
	}
	row, loadErr := GetTaskSubmissionRecovery(requestId, kind)
	if loadErr != nil {
		return loadErr
	}
	if row.Resolution == TaskSubmissionResolutionAccepted && row.ProviderTaskId == providerTaskId {
		return nil
	}
	return fmt.Errorf("task submission accepted resolution conflicts with status %s", row.Status)
}

func taskSubmissionProviderTaskId(kind string, payload *TaskSubmissionCommitPayload) string {
	if payload == nil {
		return ""
	}
	if kind == TaskSubmissionKindTask && payload.TaskPrivateData != nil {
		return strings.TrimSpace(payload.TaskPrivateData.UpstreamTaskID)
	}
	if kind == TaskSubmissionKindMidjourney && payload.Midjourney != nil {
		return strings.TrimSpace(payload.Midjourney.MjId)
	}
	return ""
}

func taskSubmissionPublicTaskId(kind string, payload *TaskSubmissionCommitPayload) string {
	if payload == nil {
		return ""
	}
	if kind == TaskSubmissionKindTask && payload.Task != nil {
		return strings.TrimSpace(payload.Task.TaskID)
	}
	if kind == TaskSubmissionKindMidjourney && payload.Midjourney != nil {
		return strings.TrimSpace(payload.Midjourney.MjId)
	}
	return ""
}

func sortedTaskSubmissionHeaderNames(headers map[string][]string) []string {
	names := make([]string, 0, len(headers))
	for name := range headers {
		names = append(names, http.CanonicalHeaderKey(name))
	}
	sort.Strings(names)
	return names
}
