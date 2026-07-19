package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"gorm.io/gorm"
)

type TaskSubmissionAcceptedResolution struct {
	ProviderTaskId string
	FinalQuota     int
	PublicResponse model.TaskSubmissionPublicResponse
}

func normalizeAcceptedResolution(input TaskSubmissionAcceptedResolution) (TaskSubmissionAcceptedResolution, error) {
	input.ProviderTaskId = strings.TrimSpace(input.ProviderTaskId)
	if input.ProviderTaskId == "" || len(input.ProviderTaskId) > 191 || strings.ContainsAny(input.ProviderTaskId, "\r\n\t") {
		return TaskSubmissionAcceptedResolution{}, errors.New("provider task id is invalid")
	}
	if input.FinalQuota < 0 || int64(input.FinalQuota) > 1_000_000_000_000 {
		return TaskSubmissionAcceptedResolution{}, errors.New("final quota is invalid")
	}
	if err := input.PublicResponse.Validate(); err != nil {
		return TaskSubmissionAcceptedResolution{}, err
	}
	canonicalHeaders := make(map[string][]string, len(input.PublicResponse.Headers))
	for name, values := range input.PublicResponse.Headers {
		canonicalHeaders[http.CanonicalHeaderKey(name)] = append([]string(nil), values...)
	}
	input.PublicResponse.Headers = canonicalHeaders
	return input, nil
}

func taskSubmissionResolutionAdjustment(event *model.BillingSettlementEvent, finalQuota int) *model.BillingAdjustmentSpec {
	if event == nil || finalQuota == event.ReservedQuota {
		return nil
	}
	delta := finalQuota - event.ReservedQuota
	adjustment := &model.BillingAdjustmentSpec{
		RequestId: event.RequestId, Operation: "task_submit_admin_resolve",
	}
	if event.FundingSource == BillingSourceSubscription {
		adjustment.SubscriptionId = event.SubscriptionId
		adjustment.SubscriptionResetEpoch = event.SubscriptionResetEpoch
		adjustment.SubscriptionOccurredAt = event.SubscriptionOccurredAt
		adjustment.SubscriptionQuotaDelta = int64(delta)
	} else {
		adjustment.UserId = event.UserId
		adjustment.UserQuotaDelta = -delta
	}
	if event.TokenId > 0 {
		adjustment.TokenId = event.TokenId
		adjustment.TokenQuotaDelta = -delta
	}
	return adjustment
}

func taskSubmissionResolutionRoot(row *model.TaskSubmissionRecovery, finalQuota int) (*model.BillingSettlementEvent, *model.BillingSettlementTransition, *model.BillingAdjustmentSpec, error) {
	event, err := model.GetBillingSettlement(row.RequestId, "request")
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if finalQuota != 0 || row.InitialQuota != 0 {
			return nil, nil, nil, errors.New("unbilled task submission must resolve with zero quota")
		}
		return nil, nil, nil, nil
	}
	if err != nil {
		return nil, nil, nil, err
	}
	if event.Status != model.BillingSettlementStatusReserved || event.UserId != row.UserId || event.TokenId != row.TokenId {
		return nil, nil, nil, errors.New("task submission billing root does not match the uncertain attempt")
	}
	transition := &model.BillingSettlementTransition{
		RequestId: row.RequestId, Operation: "request", FinalQuota: finalQuota, ReleaseCommission: false,
	}
	if row.Kind == model.TaskSubmissionKindMidjourney && row.Action == constant.MjActionSwapFace {
		transition.ReleaseCommission = true
		if event.FundingSource == BillingSourceWallet && finalQuota > 0 {
			var commission *model.BillingCommissionSnapshot
			if event.CommissionPolicy != "" {
				commission, err = materializeBillingCommissionPolicy(event.CommissionPolicy, finalQuota, row.RequestId, "request")
			} else {
				commission, err = prepareBillingCommissionFields(event.UserId, finalQuota, row.RequestId, "request",
					event.FundingSource, event.UsingGroup, event.ChargedGroupRatio)
			}
			if err != nil || commission == nil {
				if err == nil {
					err = errors.New("billing commission snapshot is unavailable")
				}
				return nil, nil, nil, err
			}
			transition.Commission = commission
		}
	}
	return event, transition, taskSubmissionResolutionAdjustment(event, finalQuota), nil
}

func taskSubmissionAdminProjection(row *model.TaskSubmissionRecovery, finalQuota int, acceptedAt time.Time, paid bool) *model.BillingProjectionSpec {
	operation := "request"
	dependencyType := model.BillingProjectionDependencySettlement
	if !paid {
		operation = row.Kind
		dependencyType = model.BillingProjectionDependencyTaskSubmission
	}
	projectionKey := model.BillingProjectionKey(row.RequestId, operation, "task_admin_recovery_initial")
	username, _ := model.GetUsernameById(row.UserId, false)
	tokenName := ""
	if row.TokenId > 0 {
		if token, err := model.GetTokenById(row.TokenId); err == nil {
			tokenName = token.Name
		}
	}
	modelName := row.Model
	if row.Kind == model.TaskSubmissionKindMidjourney {
		modelName = CovertMjpActionToModelName(row.Action)
	}
	other := map[string]interface{}{
		"is_task": true, "manual_recovery": true, "provider": row.Provider,
		"public_task_id": row.PublicTaskId, "request_path": row.Route,
	}
	return &model.BillingProjectionSpec{
		ProjectionKey: projectionKey, DependencyType: dependencyType,
		DependencyRequestId: row.RequestId, DependencyOperation: operation,
		LogEnabled: common.LogConsumeEnabled, LogUserId: row.UserId, LogUsername: username,
		LogCreatedAt: acceptedAt.Unix(), LogType: model.LogTypeConsume,
		LogContent:   "Administrator-confirmed asynchronous provider acceptance",
		LogTokenName: tokenName, LogModelName: modelName, LogQuota: finalQuota,
		LogChannelId: row.ChannelId, LogTokenId: row.TokenId, LogGroup: row.UsingGroup,
		LogRequestId: projectionLogRequestId(nil, row.RequestId, projectionKey), LogOther: common.MapToJsonStr(other),
		QuotaDataEnabled: common.DataExportEnabled && common.LogConsumeEnabled, QuotaDataNodeName: common.NodeName,
		UserId: row.UserId, UserUsedQuotaDelta: finalQuota, UserRequestDelta: 1,
		ChannelId: row.ChannelId, ChannelQuotaDelta: finalQuota,
	}
}

func resolvedTaskSubmissionPayload(row *model.TaskSubmissionRecovery, input TaskSubmissionAcceptedResolution) (model.TaskSubmissionCommitPayload, error) {
	event, transition, adjustment, err := taskSubmissionResolutionRoot(row, input.FinalQuota)
	if err != nil {
		return model.TaskSubmissionCommitPayload{}, err
	}
	acceptedAt := time.Now().UTC()
	if row.AttemptedAt != nil && !row.AttemptedAt.IsZero() {
		acceptedAt = row.AttemptedAt.UTC()
	}
	response := input.PublicResponse
	projection := taskSubmissionAdminProjection(row, input.FinalQuota, acceptedAt, event != nil)
	switch row.Kind {
	case model.TaskSubmissionKindTask:
		if !model.TaskSubmissionPublicResponseIdentifies(row.Kind, row.PublicTaskId, &response) {
			return model.TaskSubmissionCommitPayload{}, errors.New("public response does not contain the persisted public task id")
		}
		task := &model.Task{
			CreatedAt: acceptedAt.Unix(), UpdatedAt: acceptedAt.Unix(), TaskID: row.PublicTaskId,
			Platform: constant.TaskPlatform(row.Provider), UserId: row.UserId, Group: row.UsingGroup,
			ChannelId: row.ChannelId, Quota: input.FinalQuota, Action: row.Action,
			Status: model.TaskStatusNotStart, SubmitTime: acceptedAt.Unix(), Progress: "0%",
			Properties: model.Properties{Input: "", UpstreamModelName: row.Model, OriginModelName: row.Model},
			Data:       json.RawMessage([]byte(response.Body)),
		}
		privateData := model.TaskPrivateData{
			UpstreamTaskID: input.ProviderTaskId, BillingRequestId: row.RequestId,
			NodeName: common.NodeName, BillingContext: &model.TaskBillingContext{
				OriginModelName: row.Model, PerCallBilling: true,
			},
		}
		if event == nil {
			privateData.BillingSource = BillingSourceFree
		} else {
			privateData.BillingSource = event.FundingSource
			privateData.TokenId = event.TokenId
			privateData.SubscriptionId = event.SubscriptionId
			privateData.SubscriptionResetEpoch = event.SubscriptionResetEpoch
			privateData.SubscriptionOccurredAt = event.SubscriptionOccurredAt
		}
		return model.TaskSubmissionCommitPayload{
			Task: task, TaskPrivateData: &privateData, Transition: transition,
			Adjustment: adjustment, Projection: projection, PublicResponse: &response,
		}, nil
	case model.TaskSubmissionKindMidjourney:
		if !model.TaskSubmissionPublicResponseIdentifies(row.Kind, input.ProviderTaskId, &response) {
			return model.TaskSubmissionCommitPayload{}, errors.New("public response does not contain the provider task id")
		}
		task := &model.Midjourney{
			Code: 1, UserId: row.UserId, Action: row.Action, MjId: input.ProviderTaskId,
			Description: "Recovered from confirmed provider acceptance", SubmitTime: acceptedAt.UnixMilli(),
			Progress: "0%", ChannelId: row.ChannelId, Quota: input.FinalQuota,
			Group: row.UsingGroup, BillingRequestId: row.RequestId,
		}
		billing := &model.MidjourneySubmissionBilling{Group: row.UsingGroup, BillingRequestId: row.RequestId}
		if event == nil {
			task.BillingSource = BillingSourceFree
			billing.BillingSource = BillingSourceFree
		} else {
			task.TokenId = event.TokenId
			task.BillingSource = event.FundingSource
			task.SubscriptionId = event.SubscriptionId
			task.SubscriptionResetEpoch = event.SubscriptionResetEpoch
			task.SubscriptionOccurredAt = event.SubscriptionOccurredAt
			task.ChargedGroupRatio = event.ChargedGroupRatio
			billing.TokenId = event.TokenId
			billing.BillingSource = event.FundingSource
			billing.SubscriptionId = event.SubscriptionId
			billing.SubscriptionResetEpoch = event.SubscriptionResetEpoch
			billing.SubscriptionOccurredAt = event.SubscriptionOccurredAt
			billing.ChargedGroupRatio = event.ChargedGroupRatio
		}
		return model.TaskSubmissionCommitPayload{
			Midjourney: task, MidjourneyBilling: billing, Transition: transition,
			Adjustment: adjustment, Projection: projection, PublicResponse: &response,
		}, nil
	default:
		return model.TaskSubmissionCommitPayload{}, errors.New("task submission kind is invalid")
	}
}

func existingAcceptedResolutionMatches(row *model.TaskSubmissionRecovery, input TaskSubmissionAcceptedResolution) (bool, error) {
	if row.Status != model.TaskSubmissionStatusAccepted && row.Status != model.TaskSubmissionStatusCommitted {
		return false, nil
	}
	evidence, err := model.GetTaskSubmissionAcceptedEvidence(row)
	if err != nil {
		return false, err
	}
	if evidence.ProviderTaskId != input.ProviderTaskId {
		return false, errors.New("provider task id conflicts with the frozen acceptance")
	}
	if evidence.FinalQuota != input.FinalQuota {
		return false, errors.New("final quota conflicts with the frozen acceptance")
	}
	if evidence.PublicResponse.Status != input.PublicResponse.Status || evidence.PublicResponse.Body != input.PublicResponse.Body ||
		!reflect.DeepEqual(evidence.PublicResponse.Headers, input.PublicResponse.Headers) {
		return false, errors.New("public response conflicts with the frozen acceptance")
	}
	return true, nil
}

func applyAcceptedTaskSubmissionResolution(row *model.TaskSubmissionRecovery) (model.TaskSubmissionApplyResult, error) {
	result, err := model.ApplyAcceptedTaskSubmission(row.RequestId, row.Kind)
	if err != nil || !result.Committed || row.Kind != model.TaskSubmissionKindMidjourney || row.Action != constant.MjActionSwapFace {
		return result, err
	}
	event, settlementErr := model.GetBillingSettlement(row.RequestId, "request")
	if errors.Is(settlementErr, gorm.ErrRecordNotFound) {
		return result, nil
	}
	if settlementErr != nil {
		return result, settlementErr
	}
	if event.FundingSource == BillingSourceWallet && event.FinalQuota > 0 {
		if dispatchErr := DispatchBillingCommission(row.RequestId, "request"); dispatchErr != nil {
			return result, dispatchErr
		}
	}
	return result, nil
}

// ResolveUncertainTaskSubmissionAccepted turns an administrator-confirmed
// provider ACK into the same durable payload used by live submissions, then
// applies it exactly once. It never calls the provider.
func ResolveUncertainTaskSubmissionAccepted(requestId string, kind string, adminId int, input TaskSubmissionAcceptedResolution) (model.TaskSubmissionApplyResult, error) {
	if adminId <= 0 {
		return model.TaskSubmissionApplyResult{}, errors.New("task submission resolution administrator is invalid")
	}
	input, err := normalizeAcceptedResolution(input)
	if err != nil {
		return model.TaskSubmissionApplyResult{}, err
	}
	row, err := model.GetTaskSubmissionRecovery(requestId, kind)
	if err != nil {
		return model.TaskSubmissionApplyResult{}, err
	}
	if matched, matchErr := existingAcceptedResolutionMatches(row, input); matched || matchErr != nil {
		if matchErr != nil {
			return model.TaskSubmissionApplyResult{}, matchErr
		}
		if err := model.MarkTaskSubmissionResolvedAccepted(requestId, kind, adminId, input.ProviderTaskId); err != nil {
			return model.TaskSubmissionApplyResult{}, err
		}
		return applyAcceptedTaskSubmissionResolution(row)
	}
	if row.Status != model.TaskSubmissionStatusUncertain {
		return model.TaskSubmissionApplyResult{}, fmt.Errorf("task submission cannot be resolved accepted from status %s", row.Status)
	}
	payload, err := resolvedTaskSubmissionPayload(row, input)
	if err != nil {
		return model.TaskSubmissionApplyResult{}, err
	}
	if err := model.FreezeAcceptedTaskSubmissionResolved(requestId, kind, payload, adminId); err != nil {
		return model.TaskSubmissionApplyResult{}, err
	}
	return applyAcceptedTaskSubmissionResolution(row)
}
