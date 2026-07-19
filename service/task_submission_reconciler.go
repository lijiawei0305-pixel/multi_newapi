package service

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/model"
	"github.com/bytedance/gopkg/util/gopool"
)

var taskSubmissionReconcilerOnce sync.Once

func taskSubmissionIdempotencyRetention() time.Duration {
	hours := common.GetEnvOrDefault("TASK_SUBMISSION_IDEMPOTENCY_RETENTION_HOURS", 7*24)
	if hours < 24 {
		hours = 24
	}
	return time.Duration(hours) * time.Hour
}

func reconcilePendingBillingCommissions(limit int) (int, error) {
	resolutions, err := model.ListPendingBillingCommissionResolutions(limit)
	if err != nil {
		return 0, err
	}
	applied := 0
	var firstErr error
	for _, event := range resolutions {
		var snapshot *model.BillingCommissionSnapshot
		if event.CommissionPolicy != "" {
			snapshot, err = materializeBillingCommissionPolicy(event.CommissionPolicy, event.FinalQuota, event.RequestId, event.Operation)
		} else {
			snapshot, err = prepareBillingCommissionFields(event.UserId, event.FinalQuota, event.RequestId, event.Operation,
				event.FundingSource, event.UsingGroup, event.ChargedGroupRatio)
		}
		if err == nil && snapshot == nil {
			err = fmt.Errorf("billing commission resolution produced no snapshot")
		}
		if err == nil {
			err = model.AttachBillingCommissionSnapshot(event.RequestId, event.Operation, *snapshot)
		}
		if err != nil {
			model.MarkBillingSettlementError(event.RequestId, event.Operation, err)
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}

	pending, listErr := model.ListPendingBillingCommissionEvents(limit)
	if listErr != nil {
		if firstErr != nil {
			return applied, firstErr
		}
		return applied, listErr
	}
	for _, event := range pending {
		if err := DispatchBillingCommission(event.RequestId, event.Operation); err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		applied++
	}
	return applied, firstErr
}

// InitTaskSubmissionRecoveryReconciler owns accepted submission replay, safe
// stale-preparing cancellation, uncertain-state alerts, and commission
// follow-ups. The existing billing reconciler remains the single owner of
// adjustment, settlement, and projection replay.
func InitTaskSubmissionRecoveryReconciler() {
	taskSubmissionReconcilerOnce.Do(func() {
		run := func() {
			// At one run every 30 seconds, a 1,000-row batch can retire roughly
			// 2.8 million terminal submissions per day and avoids a permanent
			// retention backlog on busy task gateways.
			if _, err := model.CleanupTerminalTaskSubmissionRecoveries(taskSubmissionIdempotencyRetention(), 1000); err != nil {
				common.SysError(fmt.Sprintf("terminal task submission retention cleanup failed: error_type=%T", err))
			}
			if _, err := model.ReconcileStalePreparingTaskSubmissions(10*time.Minute, 100); err != nil {
				common.SysError(fmt.Sprintf("stale pre-send task submission cancellation failed: error_type=%T", err))
			}
			if _, err := model.ReconcileAcceptedTaskSubmissions(100); err != nil {
				common.SysError(fmt.Sprintf("task submission replay failed: error_type=%T", err))
			}
			if uncertain, err := model.CountStaleUncertainTaskSubmissions(10 * time.Minute); err != nil {
				common.SysError(fmt.Sprintf("uncertain task submission inspection failed: error_type=%T", err))
			} else if uncertain > 0 {
				requestIds, listErr := model.ListStaleUncertainTaskSubmissionRequestIDs(10*time.Minute, 10)
				if listErr != nil {
					common.SysError(fmt.Sprintf("uncertain task submission request-id listing failed: error_type=%T", listErr))
				}
				common.SysError(fmt.Sprintf("critical: %d task submissions have uncertain upstream acceptance and require manual investigation; request_ids=%s",
					uncertain, strings.Join(requestIds, ",")))
			}
			if _, err := reconcilePendingBillingCommissions(100); err != nil {
				common.SysError(fmt.Sprintf("task submission commission follow-up failed: error_type=%T", err))
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
	})
}
