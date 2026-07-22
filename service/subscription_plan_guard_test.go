package service

import (
	"context"
	"errors"
	"fmt"
	"testing"

	mysqlDriver "github.com/go-sql-driver/mysql"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/assert"
)

func TestSubscriptionPlanOwnershipFailureReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "request canceled", err: context.Canceled, want: ownershipFailureReasonContextCanceled},
		{name: "deadline exceeded", err: context.DeadlineExceeded, want: ownershipFailureReasonDeadlineExceeded},
		{
			name: "mysql table missing code",
			err: fmt.Errorf("wrapped query error: %w", &mysqlDriver.MySQLError{
				Number:  mysqlErrorTableDoesNotExist,
				Message: "opaque server message",
			}),
			want: ownershipFailureReasonMappingTableMissing,
		},
		{
			name: "mysql select denied code",
			err: &mysqlDriver.MySQLError{
				Number:  mysqlErrorCommandDenied,
				Message: "opaque server message",
			},
			want: ownershipFailureReasonPermissionDenied,
		},
		{
			name: "postgres undefined table sqlstate",
			err:  fmt.Errorf("wrapped query error: %w", &pgconn.PgError{Code: postgresSQLStateUndefinedTable, Message: "opaque server message"}),
			want: ownershipFailureReasonMappingTableMissing,
		},
		{
			name: "postgres insufficient privilege sqlstate",
			err:  &pgconn.PgError{Code: postgresSQLStateInsufficientPrivilege, Message: "opaque server message"},
			want: ownershipFailureReasonPermissionDenied,
		},
		{name: "sqlite table missing", err: errors.New("no such table: mt_native_subscription_plans"), want: ownershipFailureReasonMappingTableMissing},
		{name: "mysql textual table missing", err: errors.New("Error 1146: Table 'new_api.mt_native_subscription_plans' doesn't exist"), want: ownershipFailureReasonMappingTableMissing},
		{name: "postgres textual table missing", err: errors.New(`relation "mt_native_subscription_plans" does not exist`), want: ownershipFailureReasonMappingTableMissing},
		{name: "textual permission denied", err: errors.New("permission denied for table mt_native_subscription_plans"), want: ownershipFailureReasonPermissionDenied},
		{name: "mysql textual command denied", err: errors.New("SELECT command denied to user for table 'mt_native_subscription_plans'"), want: ownershipFailureReasonPermissionDenied},
		{name: "database unavailable", err: errors.New("driver: bad connection"), want: ownershipFailureReasonDatabaseUnavailable},
		{name: "other query failure", err: errors.New("unexpected query failure"), want: ownershipFailureReasonQueryFailed},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, subscriptionPlanOwnershipFailureReason(test.err))
		})
	}
}
