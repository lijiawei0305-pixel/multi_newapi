package service

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	mysqlDriver "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

const (
	SubscriptionPlanErrorCodeManagedByTokenPlan   = "TOKEN_PLAN_MANAGED"
	SubscriptionPlanErrorCodeOwnershipUnavailable = "PLAN_OWNERSHIP_UNAVAILABLE"
	ownershipFailureReasonContextCanceled         = "context_canceled"
	ownershipFailureReasonDeadlineExceeded        = "deadline_exceeded"
	ownershipFailureReasonMappingTableMissing     = "mapping_table_missing"
	ownershipFailureReasonPermissionDenied        = "permission_denied"
	ownershipFailureReasonDatabaseUnavailable     = "database_unavailable"
	ownershipFailureReasonQueryFailed             = "query_failed"
	mysqlErrorCommandDenied                       = 1142
	mysqlErrorTableDoesNotExist                   = 1146
	postgresSQLStateInsufficientPrivilege         = "42501"
	postgresSQLStateUndefinedTable                = "42P01"
)

var (
	// ErrTokenPlanManagedSubscriptionPlan protects a native billing plan whose
	// lifecycle belongs to Token Plans rather than the native plan admin API.
	ErrTokenPlanManagedSubscriptionPlan = errors.New("subscription plan is managed by Token Plans")
	// ErrSubscriptionPlanOwnershipUnavailable keeps native plan writes
	// fail-closed when ownership cannot be established reliably.
	ErrSubscriptionPlanOwnershipUnavailable = errors.New("subscription plan ownership is unavailable")
)

// GetTokenPlanManagedSubscriptionPlanIDs returns native plans managed by Token
// Plans and normalizes ownership-store failures for safe operational logging.
func GetTokenPlanManagedSubscriptionPlanIDs(ctx context.Context, db *gorm.DB, nativePlanIDs []int) (map[int]struct{}, error) {
	if db == nil {
		logger.LogError(ctx, fmt.Sprintf(
			"subscription plan ownership check failed plan_count=%d reason=%s error_type=nil_database",
			len(nativePlanIDs),
			ownershipFailureReasonDatabaseUnavailable,
		))
		return nil, ErrSubscriptionPlanOwnershipUnavailable
	}

	managed, err := model.GetTokenPlanManagedSubscriptionPlanIDs(ctx, db, nativePlanIDs)
	if err != nil {
		planID := 0
		if len(nativePlanIDs) == 1 {
			planID = nativePlanIDs[0]
		}
		logger.LogError(ctx, fmt.Sprintf(
			"subscription plan ownership check failed plan_count=%d plan_id=%d reason=%s error_type=%T",
			len(nativePlanIDs),
			planID,
			subscriptionPlanOwnershipFailureReason(err),
			err,
		))
		return nil, ErrSubscriptionPlanOwnershipUnavailable
	}
	return managed, nil
}

// EnsureSubscriptionPlanAdminMutable rejects native-admin writes for plans
// referenced by the tokenplan-to-native bridge. Callers must run this check in
// the same transaction as the subsequent write.
func EnsureSubscriptionPlanAdminMutable(ctx context.Context, tx *gorm.DB, planID int) error {
	managed, err := GetTokenPlanManagedSubscriptionPlanIDs(ctx, tx, []int{planID})
	if err != nil {
		return err
	}
	if _, exists := managed[planID]; exists {
		return ErrTokenPlanManagedSubscriptionPlan
	}
	return nil
}

func subscriptionPlanOwnershipFailureReason(err error) string {
	if errors.Is(err, context.Canceled) {
		return ownershipFailureReasonContextCanceled
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return ownershipFailureReasonDeadlineExceeded
	}

	var mysqlError *mysqlDriver.MySQLError
	if errors.As(err, &mysqlError) {
		switch mysqlError.Number {
		case mysqlErrorTableDoesNotExist:
			return ownershipFailureReasonMappingTableMissing
		case mysqlErrorCommandDenied:
			return ownershipFailureReasonPermissionDenied
		}
	}

	var sqlStateError interface{ SQLState() string }
	if errors.As(err, &sqlStateError) {
		switch sqlStateError.SQLState() {
		case postgresSQLStateUndefinedTable:
			return ownershipFailureReasonMappingTableMissing
		case postgresSQLStateInsufficientPrivilege:
			return ownershipFailureReasonPermissionDenied
		}
	}

	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "no such table"),
		strings.Contains(message, "unknown table"),
		strings.Contains(message, "undefined table"),
		strings.Contains(message, "doesn't exist"),
		strings.Contains(message, "relation") && strings.Contains(message, "does not exist"):
		return ownershipFailureReasonMappingTableMissing
	case strings.Contains(message, "permission denied"),
		strings.Contains(message, "access denied"),
		strings.Contains(message, "not authorized"),
		strings.Contains(message, "command denied"):
		return ownershipFailureReasonPermissionDenied
	case strings.Contains(message, "connection refused"),
		strings.Contains(message, "bad connection"),
		strings.Contains(message, "database is closed"),
		strings.Contains(message, "server closed"):
		return ownershipFailureReasonDatabaseUnavailable
	default:
		return ownershipFailureReasonQueryFailed
	}
}
