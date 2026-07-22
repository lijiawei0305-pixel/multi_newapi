package model

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	BillingSettlementStatusReserved  = "reserved"
	BillingSettlementStatusDeferred  = "deferred"
	BillingSettlementStatusFinalized = "finalized"
	BillingSettlementStatusCancelled = "cancelled"

	BillingSettlementFinancialPending = "pending"
	BillingSettlementFinancialApplied = "applied"

	BillingCommissionStatusBlocked           = "blocked"
	BillingCommissionStatusResolutionPending = "resolution_pending"
	BillingCommissionStatusPending           = "pending"
	BillingCommissionStatusDispatched        = "dispatched"
	BillingCommissionStatusNotApplicable     = "not_applicable"
)

// BillingSettlementEvent is created before upstream work. Final account
// intent, terminal lifecycle state, and immutable commission payload are later
// committed together. Account application and commission dispatch are
// independently replayable after that fact commit.
type BillingSettlementEvent struct {
	Id                              int     `json:"id"`
	RequestId                       string  `json:"request_id" gorm:"type:varchar(128);not null;uniqueIndex:idx_billing_settlement_request_operation,priority:1"`
	Operation                       string  `json:"operation" gorm:"type:varchar(64);not null;uniqueIndex:idx_billing_settlement_request_operation,priority:2"`
	UserId                          int     `json:"user_id" gorm:"not null;index"`
	TokenId                         int     `json:"token_id" gorm:"index"`
	SubscriptionId                  int     `json:"subscription_id" gorm:"index"`
	SubscriptionPreConsumeRequestId string  `json:"subscription_pre_consume_request_id" gorm:"type:varchar(128)"`
	SubscriptionResetEpoch          int64   `json:"subscription_reset_epoch" gorm:"type:bigint;not null"`
	SubscriptionOccurredAt          int64   `json:"subscription_occurred_at" gorm:"type:bigint;not null;default:0"`
	FundingSource                   string  `json:"funding_source" gorm:"type:varchar(20);not null"`
	UsingGroup                      string  `json:"using_group" gorm:"type:varchar(64);not null"`
	ChargedGroupRatio               float64 `json:"charged_group_ratio" gorm:"type:decimal(20,8);not null"`
	InitialReservedQuota            int     `json:"initial_reserved_quota" gorm:"not null"`
	ReservedQuota                   int     `json:"reserved_quota" gorm:"not null"`
	FinalQuota                      int     `json:"final_quota" gorm:"not null"`
	DeferCommission                 bool    `json:"defer_commission" gorm:"not null"`
	CommissionPolicy                string  `json:"commission_policy" gorm:"type:text"`
	Status                          string  `json:"status" gorm:"type:varchar(16);not null;index"`
	FinancialStatus                 string  `json:"financial_status" gorm:"type:varchar(16);not null;index"`
	CommissionStatus                string  `json:"commission_status" gorm:"type:varchar(20);not null;index"`

	TerminalReleaseCommission        bool   `json:"terminal_release_commission" gorm:"not null"`
	TerminalCancel                   bool   `json:"terminal_cancel" gorm:"not null"`
	AdjustmentRequestId              string `json:"adjustment_request_id" gorm:"type:varchar(128)"`
	AdjustmentOperation              string `json:"adjustment_operation" gorm:"type:varchar(64)"`
	AdjustmentUserId                 int    `json:"adjustment_user_id"`
	AdjustmentTokenId                int    `json:"adjustment_token_id"`
	AdjustmentSubscriptionId         int    `json:"adjustment_subscription_id"`
	AdjustmentSubscriptionRequestId  string `json:"adjustment_subscription_request_id" gorm:"type:varchar(128)"`
	AdjustmentSubscriptionResetEpoch int64  `json:"adjustment_subscription_reset_epoch" gorm:"type:bigint;not null"`
	AdjustmentSubscriptionOccurredAt int64  `json:"adjustment_subscription_occurred_at" gorm:"type:bigint;not null;default:0"`
	AdjustmentUserQuotaDelta         int    `json:"adjustment_user_quota_delta"`
	AdjustmentTokenQuotaDelta        int    `json:"adjustment_token_quota_delta"`
	AdjustmentSubscriptionQuotaDelta int64  `json:"adjustment_subscription_quota_delta" gorm:"type:bigint"`

	CommissionSourceId          string     `json:"commission_source_id" gorm:"type:varchar(128)"`
	CommissionOccurredAt        *time.Time `json:"commission_occurred_at"`
	CommissionWalletTenantId    int64      `json:"commission_wallet_tenant_id"`
	CommissionWalletUserId      int64      `json:"commission_wallet_user_id"`
	CommissionWalletQuota       int64      `json:"commission_wallet_quota"`
	CommissionEarningApplicable bool       `json:"commission_earning_applicable"`
	CommissionEarningTenantId   int64      `json:"commission_earning_tenant_id"`
	CommissionEarningUserId     int64      `json:"commission_earning_user_id"`
	CommissionEarningSourceType string     `json:"commission_earning_source_type" gorm:"type:varchar(32)"`
	CommissionEarningAmount     float64    `json:"commission_earning_amount" gorm:"type:decimal(20,8)"`
	CommissionEarningRemark     string     `json:"commission_earning_remark" gorm:"type:varchar(255)"`

	LastError  string     `json:"last_error" gorm:"type:varchar(512);not null"`
	TerminalAt *time.Time `json:"terminal_at" gorm:"index"`
	CreatedAt  time.Time  `json:"created_at" gorm:"not null"`
	UpdatedAt  time.Time  `json:"updated_at" gorm:"not null;index"`
}

type BillingSettlementSpec struct {
	RequestId                       string
	Operation                       string
	UserId                          int
	TokenId                         int
	SubscriptionId                  int
	SubscriptionPreConsumeRequestId string
	SubscriptionResetEpoch          int64
	SubscriptionOccurredAt          int64
	FundingSource                   string
	UsingGroup                      string
	ChargedGroupRatio               float64
	ReservedQuota                   int
	DeferCommission                 bool
	CommissionPolicy                string
}

type BillingCommissionSnapshot struct {
	SourceId          string
	OccurredAt        time.Time
	WalletTenantId    int64
	WalletUserId      int64
	WalletQuota       int64
	EarningApplicable bool
	EarningTenantId   int64
	EarningUserId     int64
	EarningSourceType string
	EarningAmount     float64
	EarningRemark     string
}

type BillingSettlementTransition struct {
	RequestId         string
	Operation         string
	FinalQuota        int
	ReleaseCommission bool
	Cancel            bool
	Commission        *BillingCommissionSnapshot
}

type BillingCommissionEvent struct {
	RequestId string
	Operation string
	Snapshot  BillingCommissionSnapshot
}

type BillingCommissionResolutionEvent struct {
	RequestId         string
	Operation         string
	UserId            int
	FinalQuota        int
	FundingSource     string
	UsingGroup        string
	ChargedGroupRatio float64
	CommissionPolicy  string
}

// BillingSettlementApplyPendingError means the terminal fact and account
// intent are durable, but the account mutation still needs reconciliation.
// Callers must not refund/rewrite the already-finalized lifecycle.
type BillingSettlementApplyPendingError struct{ Err error }

func (e *BillingSettlementApplyPendingError) Error() string {
	return "billing settlement financial application is pending: " + e.Err.Error()
}

func (e *BillingSettlementApplyPendingError) Unwrap() error { return e.Err }

func normalizeSettlementMoney(value float64) float64 {
	return math.Round(value*1e8) / 1e8
}

func (s BillingSettlementSpec) validate() error {
	s.RequestId = strings.TrimSpace(s.RequestId)
	s.Operation = strings.TrimSpace(s.Operation)
	if s.RequestId == "" || len(s.RequestId) > 128 {
		return errors.New("billing settlement request id is invalid")
	}
	if s.Operation == "" || len(s.Operation) > 64 {
		return errors.New("billing settlement operation is invalid")
	}
	if s.UserId <= 0 || s.TokenId < 0 || s.SubscriptionId < 0 || s.ReservedQuota < 0 {
		return errors.New("billing settlement reservation is invalid")
	}
	if s.FundingSource != "wallet" && s.FundingSource != "subscription" {
		return errors.New("billing settlement funding source is invalid")
	}
	if s.FundingSource == "wallet" && (s.SubscriptionId != 0 || s.SubscriptionResetEpoch != 0 || s.SubscriptionOccurredAt != 0 || strings.TrimSpace(s.SubscriptionPreConsumeRequestId) != "") {
		return errors.New("wallet billing settlement has subscription id")
	}
	if s.FundingSource == "subscription" && s.SubscriptionId <= 0 {
		return errors.New("subscription billing settlement id is missing")
	}
	if s.FundingSource == "subscription" && s.SubscriptionOccurredAt <= 0 {
		return errors.New("subscription billing settlement occurred time is missing")
	}
	if len(strings.TrimSpace(s.SubscriptionPreConsumeRequestId)) > 128 {
		return errors.New("billing settlement subscription pre-consume request id is invalid")
	}
	if s.SubscriptionResetEpoch < 0 {
		return errors.New("billing settlement subscription reset epoch is invalid")
	}
	if s.SubscriptionOccurredAt < 0 {
		return errors.New("billing settlement subscription occurred time is invalid")
	}
	if len(s.UsingGroup) > 64 || math.IsNaN(s.ChargedGroupRatio) || math.IsInf(s.ChargedGroupRatio, 0) {
		return errors.New("billing settlement commission context is invalid")
	}
	if len(s.CommissionPolicy) > 4096 {
		return errors.New("billing settlement commission policy is too large")
	}
	return nil
}

func billingSettlementMatches(event *BillingSettlementEvent, spec BillingSettlementSpec) bool {
	return event.UserId == spec.UserId && event.TokenId == spec.TokenId && event.SubscriptionId == spec.SubscriptionId &&
		event.SubscriptionPreConsumeRequestId == strings.TrimSpace(spec.SubscriptionPreConsumeRequestId) && event.SubscriptionResetEpoch == spec.SubscriptionResetEpoch &&
		(event.SubscriptionOccurredAt == spec.SubscriptionOccurredAt || event.SubscriptionOccurredAt == 0) && event.FundingSource == spec.FundingSource &&
		event.UsingGroup == spec.UsingGroup && event.ChargedGroupRatio == normalizeSettlementMoney(spec.ChargedGroupRatio) &&
		event.InitialReservedQuota == spec.ReservedQuota && event.DeferCommission == spec.DeferCommission &&
		event.CommissionPolicy == spec.CommissionPolicy
}

func ensureBillingSettlementReservedTx(tx *gorm.DB, spec BillingSettlementSpec) (*BillingSettlementEvent, error) {
	if err := spec.validate(); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	event := &BillingSettlementEvent{
		RequestId:                       strings.TrimSpace(spec.RequestId),
		Operation:                       strings.TrimSpace(spec.Operation),
		UserId:                          spec.UserId,
		TokenId:                         spec.TokenId,
		SubscriptionId:                  spec.SubscriptionId,
		SubscriptionPreConsumeRequestId: strings.TrimSpace(spec.SubscriptionPreConsumeRequestId),
		SubscriptionResetEpoch:          spec.SubscriptionResetEpoch,
		SubscriptionOccurredAt:          spec.SubscriptionOccurredAt,
		FundingSource:                   spec.FundingSource,
		UsingGroup:                      spec.UsingGroup,
		ChargedGroupRatio:               normalizeSettlementMoney(spec.ChargedGroupRatio),
		InitialReservedQuota:            spec.ReservedQuota,
		ReservedQuota:                   spec.ReservedQuota,
		FinalQuota:                      spec.ReservedQuota,
		DeferCommission:                 spec.DeferCommission,
		CommissionPolicy:                spec.CommissionPolicy,
		Status:                          BillingSettlementStatusReserved,
		FinancialStatus:                 BillingSettlementFinancialApplied,
		CommissionStatus:                BillingCommissionStatusBlocked,
		CreatedAt:                       now,
		UpdatedAt:                       now,
	}
	if err := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "request_id"}, {Name: "operation"}},
		DoNothing: true,
	}).Create(event).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("request_id = ? AND operation = ?", event.RequestId, event.Operation).First(event).Error; err != nil {
		return nil, err
	}
	if !billingSettlementMatches(event, spec) {
		return nil, errors.New("billing settlement retry does not match persisted reservation")
	}
	if event.SubscriptionOccurredAt == 0 && spec.SubscriptionOccurredAt > 0 {
		result := tx.Model(&BillingSettlementEvent{}).
			Where("id = ? AND subscription_occurred_at = 0", event.Id).
			UpdateColumn("subscription_occurred_at", spec.SubscriptionOccurredAt)
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected == 1 {
			event.SubscriptionOccurredAt = spec.SubscriptionOccurredAt
		} else {
			var current BillingSettlementEvent
			query := tx.Select("subscription_occurred_at").Where("id = ?", event.Id)
			if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
				query = query.Clauses(clause.Locking{Strength: "UPDATE"})
			}
			if err := query.First(&current).Error; err != nil {
				return nil, err
			}
			if current.SubscriptionOccurredAt != spec.SubscriptionOccurredAt {
				return nil, errors.New("billing settlement retry does not match persisted subscription occurred time")
			}
			event.SubscriptionOccurredAt = current.SubscriptionOccurredAt
		}
	}
	return event, nil
}

func EnsureBillingSettlementReservedTx(tx *gorm.DB, spec BillingSettlementSpec) error {
	if tx == nil {
		return errors.New("billing settlement transaction is nil")
	}
	_, err := ensureBillingSettlementReservedTx(tx, spec)
	return err
}

func EnsureBillingSettlementReserved(spec BillingSettlementSpec) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		_, err := ensureBillingSettlementReservedTx(tx, spec)
		return err
	})
}

func EnsureTaskSubmissionBillingSettlementReserved(kind string, spec BillingSettlementSpec) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		if _, err := lockPreparingTaskSubmissionTx(tx, spec.RequestId, kind); err != nil {
			return err
		}
		_, err := ensureBillingSettlementReservedTx(tx, spec)
		return err
	})
}

func validateSettlementReservationAdjustment(userId int, tokenId int, subscriptionId int, subscriptionResetEpoch int64, fundingSource string, delta int, adjustment BillingAdjustmentSpec) error {
	if delta <= 0 {
		return errors.New("billing settlement reserve delta must be positive")
	}
	if fundingSource == "wallet" {
		if adjustment.UserId != userId || adjustment.UserQuotaDelta != -delta || adjustment.SubscriptionId != 0 ||
			adjustment.SubscriptionResetEpoch != 0 || adjustment.SubscriptionOccurredAt != 0 ||
			adjustment.SubscriptionQuotaDelta != 0 || strings.TrimSpace(adjustment.SubscriptionRequestId) != "" {
			return errors.New("wallet reserve adjustment mismatch")
		}
	} else {
		if adjustment.UserId != 0 || adjustment.UserQuotaDelta != 0 || adjustment.SubscriptionId != subscriptionId ||
			adjustment.SubscriptionResetEpoch != subscriptionResetEpoch || adjustment.SubscriptionOccurredAt <= 0 ||
			adjustment.SubscriptionQuotaDelta != int64(delta) || strings.TrimSpace(adjustment.SubscriptionRequestId) != "" {
			return errors.New("subscription reserve adjustment mismatch")
		}
	}
	if tokenId > 0 {
		if adjustment.TokenId != tokenId || adjustment.TokenQuotaDelta != -delta {
			return errors.New("token reserve adjustment mismatch")
		}
	} else if adjustment.TokenId != 0 || adjustment.TokenQuotaDelta != 0 {
		return errors.New("unexpected token reserve adjustment")
	}
	return nil
}

// ReserveBillingSettlementImmediate is used only before upstream work; a
// failed reserve rolls back both account mutation and lifecycle root.
func ReserveBillingSettlementImmediate(adjustment BillingAdjustmentSpec, settlement BillingSettlementSpec) error {
	return reserveBillingSettlementImmediate("", adjustment, settlement)
}

func ReserveTaskSubmissionBillingSettlementImmediate(kind string, adjustment BillingAdjustmentSpec, settlement BillingSettlementSpec) error {
	return reserveBillingSettlementImmediate(kind, adjustment, settlement)
}

func reserveBillingSettlementImmediate(kind string, adjustment BillingAdjustmentSpec, settlement BillingSettlementSpec) error {
	if strings.TrimSpace(adjustment.RequestId) != strings.TrimSpace(settlement.RequestId) {
		return errors.New("billing settlement reserve request id mismatch")
	}
	if err := settlement.validate(); err != nil {
		return err
	}
	var applied BillingAdjustmentIntent
	err := DB.Transaction(func(tx *gorm.DB) error {
		if kind != "" {
			if _, err := lockPreparingTaskSubmissionTx(tx, settlement.RequestId, kind); err != nil {
				return err
			}
		}
		event, err := ensureBillingSettlementReservedTx(tx, settlement)
		if err != nil {
			return err
		}
		bound, err := bindSettlementAdjustment(event, &adjustment)
		if err != nil {
			return err
		}
		if err := validateSettlementReservationAdjustment(event.UserId, event.TokenId, event.SubscriptionId, event.SubscriptionResetEpoch, event.FundingSource, event.ReservedQuota, *bound); err != nil {
			return err
		}
		intent, err := ensureBillingAdjustmentPendingTx(tx, *bound)
		if err != nil {
			return err
		}
		applied = *intent
		if err := applyBillingAdjustmentTx(tx, &applied); err != nil {
			return err
		}
		if applied.SubscriptionEpochOutcome != "" {
			return errors.New("billing settlement cannot reserve against an expired subscription period")
		}
		return nil
	})
	if err != nil {
		return err
	}
	refreshBillingAdjustmentCaches(applied)
	return nil
}

func ReserveBillingSettlementAdditionalImmediate(requestId string, operation string, targetQuota int, adjustment BillingAdjustmentSpec) error {
	requestId = strings.TrimSpace(requestId)
	operation = strings.TrimSpace(operation)
	if requestId == "" || operation == "" || targetQuota < 0 || strings.TrimSpace(adjustment.RequestId) != requestId {
		return errors.New("billing settlement additional reserve key is invalid")
	}
	var applied BillingAdjustmentIntent
	refreshCache := false
	err := DB.Transaction(func(tx *gorm.DB) error {
		event, err := lockBillingSettlementTx(tx, requestId, operation)
		if err != nil {
			return err
		}
		if event.Status != BillingSettlementStatusReserved || event.FinancialStatus != BillingSettlementFinancialApplied {
			return errors.New("billing settlement cannot reserve after finalization")
		}
		bound, err := bindSettlementAdjustment(event, &adjustment)
		if err != nil {
			return err
		}
		if targetQuota < event.ReservedQuota {
			return errors.New("billing settlement reserve target regressed below persisted reservation")
		}
		if targetQuota == event.ReservedQuota {
			if err := bound.validate(); err != nil {
				return err
			}
			var existing BillingAdjustmentIntent
			if err := tx.Where("request_id = ? AND operation = ?", strings.TrimSpace(adjustment.RequestId), strings.TrimSpace(adjustment.Operation)).First(&existing).Error; err != nil {
				return errors.New("billing settlement reserve replay is missing its adjustment")
			}
			if !billingAdjustmentMatches(&existing, *bound) || existing.Status != BillingAdjustmentStatusApplied {
				return errors.New("billing settlement reserve replay adjustment mismatch")
			}
			return nil
		}
		delta := targetQuota - event.ReservedQuota
		if err := validateSettlementReservationAdjustment(event.UserId, event.TokenId, event.SubscriptionId, event.SubscriptionResetEpoch, event.FundingSource, delta, *bound); err != nil {
			return err
		}
		intent, err := ensureBillingAdjustmentPendingTx(tx, *bound)
		if err != nil {
			return err
		}
		applied = *intent
		if err := applyBillingAdjustmentTx(tx, &applied); err != nil {
			return err
		}
		if applied.SubscriptionEpochOutcome != "" {
			return errors.New("billing settlement cannot reserve against an expired subscription period")
		}
		refreshCache = true
		result := tx.Model(&BillingSettlementEvent{}).
			Where("id = ? AND status = ? AND financial_status = ? AND reserved_quota = ?", event.Id, BillingSettlementStatusReserved, BillingSettlementFinancialApplied, event.ReservedQuota).
			Updates(map[string]interface{}{"reserved_quota": targetQuota, "final_quota": targetQuota, "updated_at": time.Now().UTC()})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("billing settlement reservation changed concurrently")
		}
		return nil
	})
	if err != nil {
		return err
	}
	if refreshCache {
		refreshBillingAdjustmentCaches(applied)
	}
	return nil
}

func lockBillingSettlementTx(tx *gorm.DB, requestId string, operation string) (*BillingSettlementEvent, error) {
	query := tx.Where("request_id = ? AND operation = ?", strings.TrimSpace(requestId), strings.TrimSpace(operation))
	if !common.UsingMainDatabase(common.DatabaseTypeSQLite) {
		query = query.Clauses(clause.Locking{Strength: "UPDATE"})
	}
	var event BillingSettlementEvent
	if err := query.First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// bindSettlementAdjustment copies the immutable subscription period boundary
// from the lifecycle root into every direct subscription mutation. Legacy roots
// created before this field existed use their durable creation time.
func bindSettlementAdjustment(event *BillingSettlementEvent, adjustment *BillingAdjustmentSpec) (*BillingAdjustmentSpec, error) {
	if adjustment == nil {
		return nil, nil
	}
	bound := *adjustment
	if event == nil {
		return nil, errors.New("billing settlement root is missing")
	}
	if event.FundingSource != "subscription" {
		if bound.SubscriptionOccurredAt != 0 {
			return nil, errors.New("wallet billing settlement has subscription occurred time")
		}
		return &bound, nil
	}
	occurredAt := event.SubscriptionOccurredAt
	if occurredAt == 0 && !event.CreatedAt.IsZero() {
		occurredAt = event.CreatedAt.Unix()
	}
	if occurredAt <= 0 {
		return nil, errors.New("subscription billing settlement occurred time is missing")
	}
	if bound.SubscriptionOccurredAt != 0 && bound.SubscriptionOccurredAt != occurredAt {
		return nil, errors.New("subscription billing adjustment occurred time does not match settlement")
	}
	bound.SubscriptionOccurredAt = occurredAt
	return &bound, nil
}

func fillSettlementAdjustment(event *BillingSettlementEvent, adjustment *BillingAdjustmentSpec) {
	event.AdjustmentRequestId = ""
	event.AdjustmentOperation = ""
	event.AdjustmentUserId = 0
	event.AdjustmentTokenId = 0
	event.AdjustmentSubscriptionId = 0
	event.AdjustmentSubscriptionRequestId = ""
	event.AdjustmentSubscriptionResetEpoch = 0
	event.AdjustmentSubscriptionOccurredAt = 0
	event.AdjustmentUserQuotaDelta = 0
	event.AdjustmentTokenQuotaDelta = 0
	event.AdjustmentSubscriptionQuotaDelta = 0
	if adjustment == nil {
		return
	}
	event.AdjustmentRequestId = strings.TrimSpace(adjustment.RequestId)
	event.AdjustmentOperation = strings.TrimSpace(adjustment.Operation)
	event.AdjustmentUserId = adjustment.UserId
	event.AdjustmentTokenId = adjustment.TokenId
	event.AdjustmentSubscriptionId = adjustment.SubscriptionId
	event.AdjustmentSubscriptionRequestId = strings.TrimSpace(adjustment.SubscriptionRequestId)
	event.AdjustmentSubscriptionResetEpoch = adjustment.SubscriptionResetEpoch
	event.AdjustmentSubscriptionOccurredAt = adjustment.SubscriptionOccurredAt
	event.AdjustmentUserQuotaDelta = adjustment.UserQuotaDelta
	event.AdjustmentTokenQuotaDelta = adjustment.TokenQuotaDelta
	event.AdjustmentSubscriptionQuotaDelta = adjustment.SubscriptionQuotaDelta
}

func fillSettlementCommission(event *BillingSettlementEvent, snapshot *BillingCommissionSnapshot) {
	event.CommissionSourceId = ""
	event.CommissionOccurredAt = nil
	event.CommissionWalletTenantId = 0
	event.CommissionWalletUserId = 0
	event.CommissionWalletQuota = 0
	event.CommissionEarningApplicable = false
	event.CommissionEarningTenantId = 0
	event.CommissionEarningUserId = 0
	event.CommissionEarningSourceType = ""
	event.CommissionEarningAmount = 0
	event.CommissionEarningRemark = ""
	if snapshot == nil {
		return
	}
	event.CommissionSourceId = snapshot.SourceId
	occurredAt := snapshot.OccurredAt.UTC().Truncate(time.Millisecond)
	event.CommissionOccurredAt = &occurredAt
	event.CommissionWalletTenantId = snapshot.WalletTenantId
	event.CommissionWalletUserId = snapshot.WalletUserId
	event.CommissionWalletQuota = snapshot.WalletQuota
	event.CommissionEarningApplicable = snapshot.EarningApplicable
	event.CommissionEarningTenantId = snapshot.EarningTenantId
	event.CommissionEarningUserId = snapshot.EarningUserId
	event.CommissionEarningSourceType = snapshot.EarningSourceType
	event.CommissionEarningAmount = normalizeSettlementMoney(snapshot.EarningAmount)
	event.CommissionEarningRemark = snapshot.EarningRemark
}

func validateCommissionSnapshot(event *BillingSettlementEvent, transition BillingSettlementTransition) error {
	needsSnapshot := event.FundingSource == "wallet" && transition.FinalQuota > 0 && !transition.Cancel && transition.ReleaseCommission
	if !needsSnapshot {
		if transition.Commission != nil {
			return errors.New("billing settlement commission snapshot is not applicable")
		}
		return nil
	}
	if transition.Commission == nil {
		// The final charge still commits when agent configuration resolution is
		// temporarily unavailable. A durable resolution_pending state is retried.
		return nil
	}
	s := transition.Commission
	if s.SourceId != BillingCommissionSourceId(event.RequestId, event.Operation) || len(s.SourceId) > 128 || s.OccurredAt.IsZero() || s.WalletQuota < 0 ||
		math.IsNaN(s.EarningAmount) || math.IsInf(s.EarningAmount, 0) || len(s.EarningRemark) > 255 {
		return errors.New("billing settlement commission snapshot is invalid")
	}
	if s.WalletTenantId > 0 {
		if s.WalletUserId != int64(event.UserId) || s.WalletQuota != int64(transition.FinalQuota) {
			return errors.New("billing settlement wallet consumption snapshot is inconsistent")
		}
	} else if s.WalletUserId != 0 || s.WalletQuota != 0 {
		return errors.New("billing settlement wallet snapshot has no tenant")
	}
	if s.EarningApplicable && (s.EarningTenantId <= 0 || s.EarningTenantId != s.WalletTenantId || s.EarningUserId <= 0 || s.EarningAmount <= 0 ||
		(s.EarningSourceType != "consume_commission" && s.EarningSourceType != "ratio_markup")) {
		return errors.New("billing settlement earning snapshot is invalid")
	}
	if len(s.EarningSourceType) > 32 {
		return errors.New("billing settlement earning source is too long")
	}
	if !s.EarningApplicable && (s.EarningTenantId != 0 || s.EarningUserId != 0 || s.EarningSourceType != "" || s.EarningAmount != 0 || s.EarningRemark != "") {
		return errors.New("billing settlement non-applicable earning snapshot is not empty")
	}
	return nil
}

func settlementTerminalMatches(event *BillingSettlementEvent, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec) bool {
	if event.FinalQuota != transition.FinalQuota || event.TerminalCancel != transition.Cancel ||
		event.TerminalReleaseCommission != transition.ReleaseCommission {
		return false
	}
	copy := *event
	fillSettlementAdjustment(&copy, adjustment)
	if event.AdjustmentRequestId != copy.AdjustmentRequestId || event.AdjustmentOperation != copy.AdjustmentOperation ||
		event.AdjustmentUserId != copy.AdjustmentUserId || event.AdjustmentTokenId != copy.AdjustmentTokenId ||
		event.AdjustmentSubscriptionId != copy.AdjustmentSubscriptionId ||
		event.AdjustmentSubscriptionRequestId != copy.AdjustmentSubscriptionRequestId ||
		(event.AdjustmentSubscriptionResetEpoch != copy.AdjustmentSubscriptionResetEpoch) ||
		(event.AdjustmentSubscriptionOccurredAt != copy.AdjustmentSubscriptionOccurredAt && event.AdjustmentSubscriptionOccurredAt != 0) ||
		event.AdjustmentUserQuotaDelta != copy.AdjustmentUserQuotaDelta ||
		event.AdjustmentTokenQuotaDelta != copy.AdjustmentTokenQuotaDelta ||
		event.AdjustmentSubscriptionQuotaDelta != copy.AdjustmentSubscriptionQuotaDelta {
		return false
	}
	fillSettlementCommission(&copy, transition.Commission)
	return settlementCommissionMatches(event, &copy)
}

func settlementCommissionMatches(event *BillingSettlementEvent, expected *BillingSettlementEvent) bool {
	return event.CommissionSourceId == expected.CommissionSourceId &&
		settlementTimesEqual(event.CommissionOccurredAt, expected.CommissionOccurredAt) &&
		event.CommissionWalletTenantId == expected.CommissionWalletTenantId &&
		event.CommissionWalletUserId == expected.CommissionWalletUserId &&
		event.CommissionWalletQuota == expected.CommissionWalletQuota &&
		event.CommissionEarningApplicable == expected.CommissionEarningApplicable &&
		event.CommissionEarningTenantId == expected.CommissionEarningTenantId &&
		event.CommissionEarningUserId == expected.CommissionEarningUserId &&
		event.CommissionEarningSourceType == expected.CommissionEarningSourceType &&
		event.CommissionEarningAmount == expected.CommissionEarningAmount &&
		event.CommissionEarningRemark == expected.CommissionEarningRemark
}

func settlementTimesEqual(left *time.Time, right *time.Time) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return left.Equal(*right)
}

func validateSettlementAdjustment(event *BillingSettlementEvent, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec) error {
	previousQuota := event.ReservedQuota
	if event.Status == BillingSettlementStatusDeferred {
		previousQuota = event.FinalQuota
	}
	delta := transition.FinalQuota - previousQuota
	if transition.Cancel && transition.FinalQuota != 0 {
		return errors.New("cancelled billing settlement final quota must be zero")
	}
	if event.FundingSource == "wallet" {
		if delta == 0 {
			if adjustment != nil {
				return errors.New("wallet billing settlement has unnecessary adjustment")
			}
			return nil
		}
		if adjustment == nil || adjustment.UserId != event.UserId || adjustment.UserQuotaDelta != -delta ||
			adjustment.SubscriptionId != 0 || adjustment.SubscriptionResetEpoch != 0 || adjustment.SubscriptionOccurredAt != 0 ||
			adjustment.SubscriptionQuotaDelta != 0 || strings.TrimSpace(adjustment.SubscriptionRequestId) != "" {
			return errors.New("wallet billing settlement adjustment does not match quota delta")
		}
	} else {
		if event.SubscriptionId <= 0 {
			return errors.New("subscription billing settlement id is missing")
		}
		expectedSubscriptionDelta := int64(delta)
		expectedRequestId := ""
		if transition.Cancel {
			expectedRequestId = event.SubscriptionPreConsumeRequestId
			if expectedRequestId == "" {
				expectedSubscriptionDelta = -int64(previousQuota)
			} else {
				expectedSubscriptionDelta = -int64(previousQuota - event.InitialReservedQuota)
			}
		}
		needsAdjustment := expectedSubscriptionDelta != 0 || expectedRequestId != "" || (event.TokenId > 0 && delta != 0)
		if !needsAdjustment {
			if adjustment != nil {
				return errors.New("subscription billing settlement has unnecessary adjustment")
			}
			return nil
		}
		if adjustment == nil || adjustment.UserId != 0 || adjustment.UserQuotaDelta != 0 || adjustment.SubscriptionId != event.SubscriptionId || adjustment.SubscriptionResetEpoch != event.SubscriptionResetEpoch || adjustment.SubscriptionOccurredAt <= 0 ||
			adjustment.SubscriptionQuotaDelta != expectedSubscriptionDelta ||
			strings.TrimSpace(adjustment.SubscriptionRequestId) != expectedRequestId {
			return errors.New("subscription billing settlement adjustment does not match quota delta")
		}
	}
	if adjustment == nil {
		return errors.New("billing settlement quota delta requires adjustment")
	}
	if event.TokenId > 0 {
		if adjustment.TokenId != event.TokenId || adjustment.TokenQuotaDelta != -delta {
			return errors.New("billing settlement token adjustment does not match quota delta")
		}
	} else if adjustment.TokenId != 0 || adjustment.TokenQuotaDelta != 0 {
		return errors.New("billing settlement unexpectedly adjusts a token")
	}
	return nil
}

// transitionBillingSettlementTx persists the terminal fact and pending account
// intent, but deliberately does not apply the account mutation. Thus apply
// failure cannot erase the already-known external cost or commission payload.
func transitionBillingSettlementTx(tx *gorm.DB, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec) (*BillingSettlementEvent, error) {
	if transition.FinalQuota < 0 {
		return nil, errors.New("billing settlement final quota is invalid")
	}
	event, err := lockBillingSettlementTx(tx, transition.RequestId, transition.Operation)
	if err != nil {
		return nil, err
	}
	adjustment, err = bindSettlementAdjustment(event, adjustment)
	if err != nil {
		return nil, err
	}
	targetStatus := BillingSettlementStatusFinalized
	if transition.Cancel {
		targetStatus = BillingSettlementStatusCancelled
	} else if !transition.ReleaseCommission {
		targetStatus = BillingSettlementStatusDeferred
	}
	if event.Status == targetStatus {
		if !settlementTerminalMatches(event, transition, adjustment) {
			return nil, errors.New("billing settlement terminal replay payload mismatch")
		}
		return event, nil
	}
	if event.Status == BillingSettlementStatusCancelled || event.Status == BillingSettlementStatusFinalized {
		return nil, fmt.Errorf("billing settlement is already terminal: %s", event.Status)
	}
	if event.FinancialStatus == BillingSettlementFinancialPending {
		return nil, errors.New("billing settlement has a pending prior adjustment")
	}
	if !transition.Cancel {
		if !event.DeferCommission && !transition.ReleaseCommission {
			return nil, errors.New("non-deferred billing settlement must release commission")
		}
	}
	if err := validateCommissionSnapshot(event, transition); err != nil {
		return nil, err
	}
	if err := validateSettlementAdjustment(event, transition, adjustment); err != nil {
		return nil, err
	}
	if adjustment != nil {
		if _, err := ensureBillingAdjustmentPendingTx(tx, *adjustment); err != nil {
			return nil, err
		}
	}
	event.FinalQuota = transition.FinalQuota
	event.Status = targetStatus
	event.TerminalCancel = transition.Cancel
	event.TerminalReleaseCommission = transition.ReleaseCommission
	event.FinancialStatus = BillingSettlementFinancialApplied
	if adjustment != nil {
		event.FinancialStatus = BillingSettlementFinancialPending
	}
	event.CommissionStatus = BillingCommissionStatusNotApplicable
	if !transition.Cancel && !transition.ReleaseCommission {
		event.CommissionStatus = BillingCommissionStatusBlocked
	} else if !transition.Cancel && transition.ReleaseCommission && event.FundingSource == "wallet" && transition.FinalQuota > 0 {
		if transition.Commission == nil {
			event.CommissionStatus = BillingCommissionStatusResolutionPending
		} else if adjustment == nil {
			event.CommissionStatus = BillingCommissionStatusPending
		} else {
			event.CommissionStatus = BillingCommissionStatusBlocked
		}
	}
	fillSettlementAdjustment(event, adjustment)
	fillSettlementCommission(event, transition.Commission)
	event.UpdatedAt = time.Now().UTC()
	terminalAt := event.UpdatedAt
	event.TerminalAt = &terminalAt
	updates := map[string]interface{}{
		"final_quota": event.FinalQuota, "status": event.Status, "financial_status": event.FinancialStatus,
		"commission_status": event.CommissionStatus, "terminal_cancel": event.TerminalCancel,
		"terminal_release_commission": event.TerminalReleaseCommission,
		"adjustment_request_id":       event.AdjustmentRequestId, "adjustment_operation": event.AdjustmentOperation,
		"adjustment_user_id": event.AdjustmentUserId, "adjustment_token_id": event.AdjustmentTokenId,
		"adjustment_subscription_id":          event.AdjustmentSubscriptionId,
		"adjustment_subscription_request_id":  event.AdjustmentSubscriptionRequestId,
		"adjustment_subscription_reset_epoch": event.AdjustmentSubscriptionResetEpoch,
		"adjustment_subscription_occurred_at": event.AdjustmentSubscriptionOccurredAt,
		"adjustment_user_quota_delta":         event.AdjustmentUserQuotaDelta,
		"adjustment_token_quota_delta":        event.AdjustmentTokenQuotaDelta,
		"adjustment_subscription_quota_delta": event.AdjustmentSubscriptionQuotaDelta,
		"commission_source_id":                event.CommissionSourceId, "commission_wallet_tenant_id": event.CommissionWalletTenantId,
		"commission_occurred_at":    event.CommissionOccurredAt,
		"commission_wallet_user_id": event.CommissionWalletUserId, "commission_wallet_quota": event.CommissionWalletQuota,
		"commission_earning_applicable":  event.CommissionEarningApplicable,
		"commission_earning_tenant_id":   event.CommissionEarningTenantId,
		"commission_earning_user_id":     event.CommissionEarningUserId,
		"commission_earning_source_type": event.CommissionEarningSourceType,
		"commission_earning_amount":      event.CommissionEarningAmount,
		"commission_earning_remark":      event.CommissionEarningRemark,
		"last_error":                     "", "updated_at": event.UpdatedAt,
		"terminal_at": event.TerminalAt,
	}
	result := tx.Model(&BillingSettlementEvent{}).
		Where("id = ? AND status IN ?", event.Id, []string{BillingSettlementStatusReserved, BillingSettlementStatusDeferred}).
		Updates(updates)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected != 1 {
		return nil, errors.New("billing settlement state changed concurrently")
	}
	return event, nil
}

func adjustmentSpecFromSettlement(event *BillingSettlementEvent) *BillingAdjustmentSpec {
	if event.AdjustmentRequestId == "" || event.AdjustmentOperation == "" {
		return nil
	}
	occurredAt := event.AdjustmentSubscriptionOccurredAt
	if occurredAt == 0 && event.FundingSource == "subscription" {
		occurredAt = event.SubscriptionOccurredAt
		if occurredAt == 0 && !event.CreatedAt.IsZero() {
			occurredAt = event.CreatedAt.Unix()
		}
	}
	return &BillingAdjustmentSpec{
		RequestId: event.AdjustmentRequestId, Operation: event.AdjustmentOperation,
		UserId: event.AdjustmentUserId, TokenId: event.AdjustmentTokenId,
		SubscriptionId:         event.AdjustmentSubscriptionId,
		SubscriptionRequestId:  event.AdjustmentSubscriptionRequestId,
		SubscriptionResetEpoch: event.AdjustmentSubscriptionResetEpoch,
		SubscriptionOccurredAt: occurredAt,
		UserQuotaDelta:         event.AdjustmentUserQuotaDelta, TokenQuotaDelta: event.AdjustmentTokenQuotaDelta,
		SubscriptionQuotaDelta: event.AdjustmentSubscriptionQuotaDelta,
	}
}

func markBillingSettlementFinancialApplied(requestId string, operation string) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		event, err := lockBillingSettlementTx(tx, requestId, operation)
		if err != nil {
			return err
		}
		if event.FinancialStatus == BillingSettlementFinancialApplied {
			return nil
		}
		if event.FinancialStatus != BillingSettlementFinancialPending {
			return fmt.Errorf("billing settlement financial status is invalid: %s", event.FinancialStatus)
		}
		var intent BillingAdjustmentIntent
		if err := tx.Where("request_id = ? AND operation = ?", event.AdjustmentRequestId, event.AdjustmentOperation).First(&intent).Error; err != nil {
			return err
		}
		if intent.Status != BillingAdjustmentStatusApplied {
			return errors.New("billing settlement adjustment is not applied")
		}
		commissionStatus := event.CommissionStatus
		if event.Status == BillingSettlementStatusFinalized && event.TerminalReleaseCommission && event.FundingSource == "wallet" && event.FinalQuota > 0 && event.CommissionStatus == BillingCommissionStatusBlocked {
			commissionStatus = BillingCommissionStatusPending
		}
		result := tx.Model(&BillingSettlementEvent{}).Where("id = ? AND financial_status = ?", event.Id, BillingSettlementFinancialPending).
			Updates(map[string]interface{}{
				"financial_status":  BillingSettlementFinancialApplied,
				"commission_status": commissionStatus,
				"last_error":        "", "updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			return nil
		}
		var current BillingSettlementEvent
		if err := tx.Where("id = ?", event.Id).First(&current).Error; err != nil {
			return err
		}
		if current.FinancialStatus != BillingSettlementFinancialApplied {
			return errors.New("billing settlement financial state changed concurrently")
		}
		return nil
	})
}

func applyBillingSettlementFinancial(event *BillingSettlementEvent) error {
	if event == nil || event.FinancialStatus == BillingSettlementFinancialApplied {
		return nil
	}
	adjustment := adjustmentSpecFromSettlement(event)
	if adjustment == nil {
		return errors.New("billing settlement pending financial adjustment is missing")
	}
	if err := ApplyBillingAdjustment(adjustment.RequestId, adjustment.Operation); err != nil {
		MarkBillingSettlementError(event.RequestId, event.Operation, err)
		return err
	}
	return markBillingSettlementFinancialApplied(event.RequestId, event.Operation)
}

func FinalizeBillingSettlement(transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec) error {
	return FinalizeBillingSettlementWithProjection(transition, adjustment, nil)
}

// FinalizeBillingSettlementWithProjection first freezes the accepted
// synchronous request's exact terminal intent in an independent transaction.
// The terminal lifecycle fact, projection outbox, and recovery applied marker
// are then committed together and can be replayed without calling upstream.
func FinalizeBillingSettlementWithProjection(transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) error {
	recoveryKey, err := freezeBillingTerminalRecovery(BillingTerminalRecoveryPayload{
		Transition: transition, Adjustment: adjustment, Projection: projection,
	})
	if err != nil {
		common.SysLog("critical: unable to freeze synchronous billing terminal intent: " + err.Error())
		return err
	}
	return ApplyBillingTerminalRecovery(recoveryKey)
}

func CreateAndFinalizeBillingSettlement(settlement BillingSettlementSpec, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec) error {
	return createAndFinalizeBillingSettlementWithProjection(settlement, transition, adjustment, nil)
}

func createAndFinalizeBillingSettlementWithProjection(settlement BillingSettlementSpec, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) error {
	if err := validateSettlementBillingProjection(projection, transition); err != nil {
		return err
	}
	var event *BillingSettlementEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		if _, err := ensureBillingSettlementReservedTx(tx, settlement); err != nil {
			return err
		}
		var err error
		event, err = transitionBillingSettlementTx(tx, transition, adjustment)
		if err != nil {
			return err
		}
		if projection != nil {
			_, err = ensureBillingProjectionPendingTx(tx, *projection)
		}
		return err
	})
	if err != nil {
		return err
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return &BillingSettlementApplyPendingError{Err: err}
	}
	applyBillingProjectionBestEffort(projection)
	return nil
}

func CreateAndFinalizeBillingSettlementWithProjection(settlement BillingSettlementSpec, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) error {
	recoveryKey, err := freezeBillingTerminalRecovery(BillingTerminalRecoveryPayload{
		Settlement: &settlement, Transition: transition, Adjustment: adjustment, Projection: projection,
	})
	if err != nil {
		common.SysLog("critical: unable to freeze synchronous billing terminal intent: " + err.Error())
		return err
	}
	return ApplyBillingTerminalRecovery(recoveryKey)
}

// InsertTaskWithBillingSettlement commits local task durability and the
// deferred settlement fact in one transaction. Account application happens
// after commit and remains replayable if it fails.
func InsertTaskWithBillingSettlement(task *Task, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) error {
	if task == nil {
		return errors.New("billing settlement task is nil")
	}
	if err := validateSettlementBillingProjection(projection, transition); err != nil {
		return err
	}
	var event *BillingSettlementEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		var err error
		event, err = transitionBillingSettlementTx(tx, transition, adjustment)
		if err != nil {
			return err
		}
		if projection != nil {
			_, err = ensureBillingProjectionPendingTx(tx, *projection)
		}
		return err
	})
	if err != nil {
		task.ID = 0
		return err
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return &BillingSettlementApplyPendingError{Err: err}
	}
	applyBillingProjectionBestEffort(projection)
	return nil
}

func InsertMidjourneyWithBillingSettlement(task *Midjourney, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) error {
	if task == nil {
		return errors.New("billing settlement midjourney task is nil")
	}
	if err := validateSettlementBillingProjection(projection, transition); err != nil {
		return err
	}
	var event *BillingSettlementEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(task).Error; err != nil {
			return err
		}
		var err error
		event, err = transitionBillingSettlementTx(tx, transition, adjustment)
		if err != nil {
			return err
		}
		if projection != nil {
			_, err = ensureBillingProjectionPendingTx(tx, *projection)
		}
		return err
	})
	if err != nil {
		task.Id = 0
		return err
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return &BillingSettlementApplyPendingError{Err: err}
	}
	applyBillingProjectionBestEffort(projection)
	return nil
}

func GetBillingSettlement(requestId string, operation string) (*BillingSettlementEvent, error) {
	var event BillingSettlementEvent
	if err := DB.Where("request_id = ? AND operation = ?", strings.TrimSpace(requestId), strings.TrimSpace(operation)).First(&event).Error; err != nil {
		return nil, err
	}
	return &event, nil
}

// UpdateTaskWithBillingSettlement binds the terminal task CAS and settlement
// transition. For legacy task rows, it also creates the missing deferred root
// in the same transaction before transitioning it.
func UpdateTaskWithBillingSettlement(task *Task, fromStatus TaskStatus, legacyRoot BillingSettlementSpec, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) (bool, error) {
	if task == nil {
		return false, errors.New("billing settlement task is nil")
	}
	if err := validateSettlementBillingProjection(projection, transition); err != nil {
		return false, err
	}
	won := false
	var event *BillingSettlementEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(task).Where("status = ?", fromStatus).Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		won = true
		if _, err := lockBillingSettlementTx(tx, transition.RequestId, transition.Operation); err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if _, err := ensureBillingSettlementReservedTx(tx, legacyRoot); err != nil {
				return err
			}
		}
		var err error
		event, err = transitionBillingSettlementTx(tx, transition, adjustment)
		if err != nil {
			return err
		}
		if projection != nil {
			_, err = ensureBillingProjectionPendingTx(tx, *projection)
		}
		return err
	})
	if err != nil {
		return false, err
	}
	if !won {
		return false, nil
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return true, &BillingSettlementApplyPendingError{Err: err}
	}
	applyBillingProjectionBestEffort(projection)
	return true, nil
}

func UpdateMidjourneyWithBillingSettlement(task *Midjourney, fromStatus string, legacyRoot BillingSettlementSpec, transition BillingSettlementTransition, adjustment *BillingAdjustmentSpec, projection *BillingProjectionSpec) (bool, error) {
	if task == nil {
		return false, errors.New("billing settlement midjourney task is nil")
	}
	if err := validateSettlementBillingProjection(projection, transition); err != nil {
		return false, err
	}
	won := false
	var event *BillingSettlementEvent
	err := DB.Transaction(func(tx *gorm.DB) error {
		result := tx.Model(task).Where("status = ?", fromStatus).Select("*").Updates(task)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return nil
		}
		won = true
		if _, err := lockBillingSettlementTx(tx, transition.RequestId, transition.Operation); err != nil {
			if !errors.Is(err, gorm.ErrRecordNotFound) {
				return err
			}
			if _, err := ensureBillingSettlementReservedTx(tx, legacyRoot); err != nil {
				return err
			}
		}
		var err error
		event, err = transitionBillingSettlementTx(tx, transition, adjustment)
		if err != nil {
			return err
		}
		if projection != nil {
			_, err = ensureBillingProjectionPendingTx(tx, *projection)
		}
		return err
	})
	if err != nil {
		return false, err
	}
	if !won {
		return false, nil
	}
	if err := applyBillingSettlementFinancial(event); err != nil {
		return true, &BillingSettlementApplyPendingError{Err: err}
	}
	applyBillingProjectionBestEffort(projection)
	return true, nil
}

func BillingCommissionSourceId(requestId string, operation string) string {
	key := strings.TrimSpace(requestId) + "\x00" + strings.TrimSpace(operation)
	return fmt.Sprintf("%x", sha256.Sum256([]byte(key)))
}

func billingCommissionSnapshotFromEvent(event *BillingSettlementEvent) BillingCommissionSnapshot {
	var occurredAt time.Time
	if event.CommissionOccurredAt != nil {
		occurredAt = *event.CommissionOccurredAt
	}
	return BillingCommissionSnapshot{
		SourceId: event.CommissionSourceId, OccurredAt: occurredAt, WalletTenantId: event.CommissionWalletTenantId,
		WalletUserId: event.CommissionWalletUserId, WalletQuota: event.CommissionWalletQuota,
		EarningApplicable: event.CommissionEarningApplicable, EarningTenantId: event.CommissionEarningTenantId,
		EarningUserId: event.CommissionEarningUserId, EarningSourceType: event.CommissionEarningSourceType,
		EarningAmount: event.CommissionEarningAmount, EarningRemark: event.CommissionEarningRemark,
	}
}

func ListPendingBillingCommissionResolutions(limit int) ([]BillingCommissionResolutionEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []BillingSettlementEvent
	if err := DB.Where("commission_status = ? AND financial_status = ?", BillingCommissionStatusResolutionPending, BillingSettlementFinancialApplied).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]BillingCommissionResolutionEvent, 0, len(rows))
	for i := range rows {
		events = append(events, BillingCommissionResolutionEvent{
			RequestId: rows[i].RequestId, Operation: rows[i].Operation, UserId: rows[i].UserId,
			FinalQuota: rows[i].FinalQuota, FundingSource: rows[i].FundingSource,
			UsingGroup: rows[i].UsingGroup, ChargedGroupRatio: rows[i].ChargedGroupRatio, CommissionPolicy: rows[i].CommissionPolicy,
		})
	}
	return events, nil
}

func AttachBillingCommissionSnapshot(requestId string, operation string, snapshot BillingCommissionSnapshot) error {
	return DB.Transaction(func(tx *gorm.DB) error {
		event, err := lockBillingSettlementTx(tx, requestId, operation)
		if err != nil {
			return err
		}
		transition := BillingSettlementTransition{
			RequestId: event.RequestId, Operation: event.Operation, FinalQuota: event.FinalQuota,
			ReleaseCommission: true, Commission: &snapshot,
		}
		if err := validateCommissionSnapshot(event, transition); err != nil {
			return err
		}
		expected := *event
		fillSettlementCommission(&expected, &snapshot)
		if event.CommissionStatus == BillingCommissionStatusPending || event.CommissionStatus == BillingCommissionStatusDispatched {
			if !settlementCommissionMatches(event, &expected) {
				return errors.New("billing commission resolution replay payload mismatch")
			}
			return nil
		}
		if event.CommissionStatus != BillingCommissionStatusResolutionPending || event.FinancialStatus != BillingSettlementFinancialApplied || event.Status != BillingSettlementStatusFinalized {
			return fmt.Errorf("billing commission cannot be resolved from state %s/%s/%s", event.Status, event.FinancialStatus, event.CommissionStatus)
		}
		result := tx.Model(&BillingSettlementEvent{}).
			Where("id = ? AND commission_status = ? AND financial_status = ?", event.Id, BillingCommissionStatusResolutionPending, BillingSettlementFinancialApplied).
			Updates(map[string]interface{}{
				"commission_status":    BillingCommissionStatusPending,
				"commission_source_id": expected.CommissionSourceId, "commission_occurred_at": expected.CommissionOccurredAt,
				"commission_wallet_tenant_id": expected.CommissionWalletTenantId, "commission_wallet_user_id": expected.CommissionWalletUserId,
				"commission_wallet_quota": expected.CommissionWalletQuota, "commission_earning_applicable": expected.CommissionEarningApplicable,
				"commission_earning_tenant_id": expected.CommissionEarningTenantId, "commission_earning_user_id": expected.CommissionEarningUserId,
				"commission_earning_source_type": expected.CommissionEarningSourceType, "commission_earning_amount": expected.CommissionEarningAmount,
				"commission_earning_remark": expected.CommissionEarningRemark, "last_error": "", "updated_at": time.Now().UTC(),
			})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return errors.New("billing commission resolution state changed concurrently")
		}
		return nil
	})
}

func GetBillingCommissionEvent(requestId string, operation string) (*BillingCommissionEvent, error) {
	var event BillingSettlementEvent
	if err := DB.Where("request_id = ? AND operation = ?", requestId, operation).First(&event).Error; err != nil {
		return nil, err
	}
	if event.CommissionStatus != BillingCommissionStatusPending && event.CommissionStatus != BillingCommissionStatusDispatched {
		return nil, fmt.Errorf("billing commission is not dispatchable: %s", event.CommissionStatus)
	}
	return &BillingCommissionEvent{RequestId: event.RequestId, Operation: event.Operation, Snapshot: billingCommissionSnapshotFromEvent(&event)}, nil
}

func ListPendingBillingCommissionEvents(limit int) ([]BillingCommissionEvent, error) {
	if limit <= 0 {
		limit = 200
	}
	var rows []BillingSettlementEvent
	if err := DB.Where("commission_status = ?", BillingCommissionStatusPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	events := make([]BillingCommissionEvent, 0, len(rows))
	for i := range rows {
		events = append(events, BillingCommissionEvent{RequestId: rows[i].RequestId, Operation: rows[i].Operation, Snapshot: billingCommissionSnapshotFromEvent(&rows[i])})
	}
	return events, nil
}

func MarkBillingCommissionDispatched(requestId string, operation string) error {
	result := DB.Model(&BillingSettlementEvent{}).
		Where("request_id = ? AND operation = ? AND commission_status = ?", requestId, operation, BillingCommissionStatusPending).
		Updates(map[string]interface{}{"commission_status": BillingCommissionStatusDispatched, "last_error": "", "updated_at": time.Now().UTC()})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		var event BillingSettlementEvent
		if err := DB.Where("request_id = ? AND operation = ?", requestId, operation).First(&event).Error; err != nil {
			return err
		}
		if event.CommissionStatus != BillingCommissionStatusDispatched {
			return errors.New("billing commission state changed concurrently")
		}
	}
	return nil
}

func MarkBillingSettlementError(requestId string, operation string, settlementErr error) {
	if settlementErr == nil {
		return
	}
	_ = DB.Model(&BillingSettlementEvent{}).Where("request_id = ? AND operation = ?", requestId, operation).
		Updates(map[string]interface{}{"last_error": truncateBillingRefundError(settlementErr.Error()), "updated_at": time.Now().UTC()}).Error
}

func ReconcilePendingBillingSettlements(limit int) (int, error) {
	if limit <= 0 {
		limit = 100
	}
	var rows []BillingSettlementEvent
	if err := DB.Where("financial_status = ?", BillingSettlementFinancialPending).
		Order("updated_at asc, id asc").Limit(limit).Find(&rows).Error; err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for i := range rows {
		if err := applyBillingSettlementFinancial(&rows[i]); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

type BillingSettlementStaleCounts struct {
	Reserved          int64
	Deferred          int64
	FinancialPending  int64
	CommissionPending int64
}

func CountStaleBillingSettlements(reservedAge time.Duration, deferredAge time.Duration, followupAge time.Duration) (BillingSettlementStaleCounts, error) {
	if reservedAge <= 0 {
		reservedAge = time.Hour
	}
	if deferredAge <= 0 {
		deferredAge = 24 * time.Hour
	}
	if followupAge <= 0 {
		followupAge = 10 * time.Minute
	}
	now := time.Now().UTC()
	counts := BillingSettlementStaleCounts{}
	queries := []struct {
		destination *int64
		query       *gorm.DB
	}{
		{&counts.Reserved, DB.Model(&BillingSettlementEvent{}).Where("status = ? AND created_at < ?", BillingSettlementStatusReserved, now.Add(-reservedAge))},
		{&counts.Deferred, DB.Model(&BillingSettlementEvent{}).Where("status = ? AND COALESCE(terminal_at, created_at) < ?", BillingSettlementStatusDeferred, now.Add(-deferredAge))},
		{&counts.FinancialPending, DB.Model(&BillingSettlementEvent{}).Where("financial_status = ? AND COALESCE(terminal_at, created_at) < ?", BillingSettlementFinancialPending, now.Add(-followupAge))},
		{&counts.CommissionPending, DB.Model(&BillingSettlementEvent{}).Where("commission_status IN ? AND COALESCE(terminal_at, created_at) < ?", []string{BillingCommissionStatusResolutionPending, BillingCommissionStatusPending}, now.Add(-followupAge))},
	}
	for _, item := range queries {
		if err := item.query.Count(item.destination).Error; err != nil {
			return BillingSettlementStaleCounts{}, err
		}
	}
	return counts, nil
}
