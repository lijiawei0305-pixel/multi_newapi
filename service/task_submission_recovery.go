package service

import (
	"errors"
	"fmt"

	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/gin-gonic/gin"
)

// TaskSubmissionCommitOutcome separates durable upstream acceptance from the
// best-effort local apply. Once Durable is true the caller must return the
// provider's successful response and must never retry or refund.
type TaskSubmissionCommitOutcome struct {
	Durable   bool
	Committed bool
}

func CommitTaskSubmissionWithRecovery(c *gin.Context, relayInfo *relaycommon.RelayInfo, task *model.Task, actualQuota int) (TaskSubmissionCommitOutcome, error) {
	return CommitTaskSubmissionWithRecoveryAndResponse(c, relayInfo, task, actualQuota, nil)
}

func CommitTaskSubmissionWithRecoveryAndResponse(c *gin.Context, relayInfo *relaycommon.RelayInfo, task *model.Task, actualQuota int, publicResponse *model.TaskSubmissionPublicResponse) (TaskSubmissionCommitOutcome, error) {
	if relayInfo == nil || task == nil || actualQuota < 0 {
		return TaskSubmissionCommitOutcome{}, errors.New("task submission billing context is invalid")
	}
	requestId := billingRequestId(relayInfo.RequestId)
	relayInfo.RequestId = requestId

	var session *BillingSession
	if relayInfo.Billing != nil {
		var ok bool
		session, ok = relayInfo.Billing.(*BillingSession)
		if !ok {
			return TaskSubmissionCommitOutcome{}, fmt.Errorf("task submission requires a durable billing session: %T", relayInfo.Billing)
		}
		session.mu.Lock()
		defer session.mu.Unlock()
		if session.settled {
			row, err := model.GetTaskSubmissionRecovery(requestId, model.TaskSubmissionKindTask)
			if err == nil && row.Status == model.TaskSubmissionStatusCommitted {
				task.ID = row.LocalTaskId
				return TaskSubmissionCommitOutcome{Durable: true, Committed: true}, nil
			}
			return TaskSubmissionCommitOutcome{}, errors.New("billing task submission is already settled")
		}
	} else if actualQuota != 0 {
		return TaskSubmissionCommitOutcome{}, errors.New("non-zero task submission requires a billing session")
	}

	var (
		adjustment *model.BillingAdjustmentSpec
		delta      int
		transition *model.BillingSettlementTransition
	)
	if session != nil {
		var err error
		adjustment, delta, err = session.settlementAdjustment(actualQuota, "task_submit_settle")
		if err != nil {
			return TaskSubmissionCommitOutcome{}, err
		}
		transition = &model.BillingSettlementTransition{
			RequestId: requestId, Operation: billingSettlementOperation, FinalQuota: actualQuota,
			ReleaseCommission: false,
		}
		task.PrivateData.BillingSource = relayInfo.BillingSource
		if relayInfo.IsPlayground {
			task.PrivateData.TokenId = 0
		}
	} else {
		task.PrivateData.BillingSource = BillingSourceFree
		relayInfo.BillingSource = BillingSourceFree
	}
	originalPostDelta := relayInfo.SubscriptionPostDelta
	if session != nil && session.funding.Source() == BillingSourceSubscription {
		relayInfo.SubscriptionPostDelta += int64(delta)
	}
	projection := taskInitialBillingProjection(c, relayInfo, task)
	relayInfo.SubscriptionPostDelta = originalPostDelta
	projectionKeyOperation := billingSettlementOperation
	if session == nil {
		projectionKeyOperation = model.TaskSubmissionKindTask
	}
	if projection != nil {
		projection.DependencyRequestId = requestId
		projection.DependencyOperation = billingSettlementOperation
		projection.ProjectionKey = model.BillingProjectionKey(requestId, projectionKeyOperation, "task_initial")
		projection.LogRequestId = projectionLogRequestId(c, requestId, projection.ProjectionKey)
		projection.LogQuota = actualQuota
		projection.UserUsedQuotaDelta = actualQuota
		projection.ChannelQuotaDelta = actualQuota
		if session == nil {
			projection.DependencyType = model.BillingProjectionDependencyTaskSubmission
			projection.DependencyOperation = model.TaskSubmissionKindTask
		}
	}
	task.PrivateData.BillingRequestId = requestId
	taskCopy := *task
	privateCopy := task.PrivateData
	payload := model.TaskSubmissionCommitPayload{
		Task: &taskCopy, TaskPrivateData: &privateCopy, Transition: transition,
		Adjustment: adjustment, Projection: projection, PublicResponse: publicResponse,
	}
	if err := model.FreezeAcceptedTaskSubmission(requestId, model.TaskSubmissionKindTask, payload); err != nil {
		return TaskSubmissionCommitOutcome{}, err
	}
	relayInfo.TaskSubmissionRecoveryProtected = true
	outcome := TaskSubmissionCommitOutcome{Durable: true}
	applyResult, applyErr := model.ApplyAcceptedTaskSubmission(requestId, model.TaskSubmissionKindTask)
	if applyResult.Committed {
		outcome.Committed = true
		task.ID = applyResult.LocalTaskId
		if session != nil {
			session.fundingSettled = true
			session.settled = true
			if session.funding.Source() == BillingSourceSubscription {
				relayInfo.SubscriptionPostDelta += int64(delta)
			}
		}
	}
	return outcome, applyErr
}

func CommitMidjourneySubmissionWithRecovery(c *gin.Context, relayInfo *relaycommon.RelayInfo, task *model.Midjourney, deferCommission bool) (TaskSubmissionCommitOutcome, error) {
	return CommitMidjourneySubmissionWithRecoveryAndResponse(c, relayInfo, task, deferCommission, nil)
}

func CommitMidjourneySubmissionWithRecoveryAndResponse(c *gin.Context, relayInfo *relaycommon.RelayInfo, task *model.Midjourney, deferCommission bool, publicResponse *model.TaskSubmissionPublicResponse) (TaskSubmissionCommitOutcome, error) {
	if relayInfo == nil || task == nil || task.Quota < 0 {
		return TaskSubmissionCommitOutcome{}, errors.New("midjourney submission billing context is invalid")
	}
	requestId := billingRequestId(relayInfo.RequestId)
	relayInfo.RequestId = requestId
	task.BillingRequestId = requestId
	task.SubscriptionResetEpoch = relayInfo.SubscriptionResetEpoch
	task.SubscriptionOccurredAt = relaySubscriptionOccurredAt(relayInfo)
	task.ChargedGroupRatio = relayInfo.PriceData.GroupRatioInfo.GroupRatio
	billed := relayInfo.BillingSource == BillingSourceWallet || relayInfo.BillingSource == BillingSourceSubscription
	if task.Quota > 0 && !billed {
		return TaskSubmissionCommitOutcome{}, errors.New("non-zero midjourney submission has no billing settlement root")
	}
	if billed {
		task.BillingSource = relayInfo.BillingSource
		if relayInfo.IsPlayground {
			task.TokenId = 0
		}
	} else {
		task.BillingSource = BillingSourceFree
		relayInfo.BillingSource = BillingSourceFree
		task.SubscriptionId = 0
		task.SubscriptionResetEpoch = 0
		task.SubscriptionOccurredAt = 0
	}

	projection := midjourneyInitialBillingProjection(c, relayInfo, task)
	if projection != nil {
		projection.DependencyRequestId = requestId
		projection.DependencyOperation = billingSettlementOperation
		projection.ProjectionKey = model.BillingProjectionKey(requestId, billingSettlementOperation, "midjourney_initial")
		projection.LogRequestId = projectionLogRequestId(c, requestId, projection.ProjectionKey)
		if !billed {
			projection.DependencyType = model.BillingProjectionDependencyTaskSubmission
			projection.DependencyOperation = model.TaskSubmissionKindMidjourney
			projection.ProjectionKey = model.BillingProjectionKey(requestId, model.TaskSubmissionKindMidjourney, "midjourney_initial")
		}
	}

	var (
		transition    *model.BillingSettlementTransition
		commissionErr error
	)
	if billed {
		var commission *model.BillingCommissionSnapshot
		if task.Quota > 0 && !deferCommission {
			commission, commissionErr = prepareBillingCommission(relayInfo, task.Quota, requestId, billingSettlementOperation)
		}
		transition = &model.BillingSettlementTransition{
			RequestId: requestId, Operation: billingSettlementOperation, FinalQuota: task.Quota,
			ReleaseCommission: !deferCommission, Commission: commission,
		}
	}
	taskCopy := *task
	billing := &model.MidjourneySubmissionBilling{
		TokenId: task.TokenId, Group: task.Group, ChargedGroupRatio: task.ChargedGroupRatio,
		BillingSource: task.BillingSource, SubscriptionId: task.SubscriptionId,
		SubscriptionResetEpoch: task.SubscriptionResetEpoch, SubscriptionOccurredAt: task.SubscriptionOccurredAt,
		BillingRequestId: requestId,
	}
	payload := model.TaskSubmissionCommitPayload{
		Midjourney: &taskCopy, MidjourneyBilling: billing, Transition: transition, Projection: projection,
		PublicResponse: publicResponse,
	}
	if err := model.FreezeAcceptedTaskSubmission(requestId, model.TaskSubmissionKindMidjourney, payload); err != nil {
		return TaskSubmissionCommitOutcome{}, err
	}
	relayInfo.TaskSubmissionRecoveryProtected = true
	outcome := TaskSubmissionCommitOutcome{Durable: true}
	applyResult, applyErr := model.ApplyAcceptedTaskSubmission(requestId, model.TaskSubmissionKindMidjourney)
	if applyResult.Committed {
		outcome.Committed = true
		task.Id = int(applyResult.LocalTaskId)
		if billed {
			relayInfo.FinalPreConsumedQuota = 0
		}
	}
	if applyErr != nil {
		return outcome, applyErr
	}
	if commissionErr != nil {
		return outcome, fmt.Errorf("midjourney commission resolution pending: %w", commissionErr)
	}
	if outcome.Committed && billed && task.Quota > 0 && !deferCommission && task.BillingSource == BillingSourceWallet {
		if err := DispatchBillingCommission(requestId, billingSettlementOperation); err != nil {
			return outcome, err
		}
	}
	return outcome, nil
}
