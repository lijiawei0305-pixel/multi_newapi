package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

func newPostConsumeBillingError(err error) *types.NewAPIError {
	common.SysError(fmt.Sprintf("accepted upstream billing terminal state is unknown: error_type=%T", err))
	return types.NewErrorWithStatusCode(errors.New("upstream accepted but billing terminal state is unknown; do not retry"), types.ErrorCodeUpdateDataError, http.StatusInternalServerError, types.ErrOptionWithSkipRetry())
}

func MarkUpstreamAccepted(ctx *gin.Context) {
	if ctx != nil {
		common.SetContextKey(ctx, constant.ContextKeyUpstreamAccepted, true)
	}
}

func IsUpstreamAccepted(ctx *gin.Context) bool {
	return ctx != nil && common.GetContextKeyBool(ctx, constant.ContextKeyUpstreamAccepted)
}

func MarkBillingTerminalAttempted(ctx *gin.Context) {
	if ctx != nil {
		common.SetContextKey(ctx, constant.ContextKeyBillingTerminalAttempted, true)
	}
}

func IsBillingTerminalAttempted(ctx *gin.Context) bool {
	return ctx != nil && common.GetContextKeyBool(ctx, constant.ContextKeyBillingTerminalAttempted)
}

func billingSettlementFactCommitted(requestId string, operation string) bool {
	event, err := model.GetBillingSettlement(requestId, operation)
	if err != nil || (event.Status != model.BillingSettlementStatusFinalized && event.Status != model.BillingSettlementStatusCancelled) {
		return false
	}
	return true
}

func billingProjectionFactCommitted(projection *model.BillingProjectionSpec) bool {
	if projection == nil || !billingSettlementFactCommitted(projection.DependencyRequestId, projection.DependencyOperation) {
		return false
	}
	var count int64
	if err := model.DB.Model(&model.BillingProjectionOutbox{}).
		Where("projection_key = ?", projection.ProjectionKey).Count(&count).Error; err != nil {
		return false
	}
	return count == 1
}

func settleSynchronousBillingProjection(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, projection *model.BillingProjectionSpec) *types.NewAPIError {
	MarkUpstreamAccepted(ctx)
	err := SettleBillingWithProjection(ctx, relayInfo, actualQuota, projection)
	if err == nil {
		return nil
	}
	var recoveryPending *model.BillingTerminalRecoveryPendingError
	if errors.As(err, &recoveryPending) {
		common.SysLog("synchronous billing terminal intent is durable and pending replay: " + err.Error())
		return nil
	}
	if billingProjectionFactCommitted(projection) {
		common.SysLog("synchronous billing terminal fact committed with durable follow-up pending: " + err.Error())
		return nil
	}
	if projection == nil && billingSettlementFactCommitted(relayInfo.RequestId, billingSettlementOperation) {
		common.SysLog("synchronous billing terminal fact committed without projection effects; durable financial follow-up pending: " + err.Error())
		return nil
	}
	return newPostConsumeBillingError(err)
}

// FinalizeAcceptedBillingFailure conservatively commits the immutable reserved
// quota when an upstream success response cannot be parsed or safely delivered
// before actual-usage settlement starts. It never overwrites an actual-quota
// terminal attempt, even when that attempt returned a hard error.
func FinalizeAcceptedBillingFailure(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, apiErr *types.NewAPIError) *types.NewAPIError {
	if apiErr == nil || relayInfo == nil || !IsUpstreamAccepted(ctx) || IsBillingTerminalAttempted(ctx) {
		return apiErr
	}

	common.SysLog(fmt.Sprintf(
		"accepted upstream response unavailable; applying conservative settlement: request_id=%s error_code=%s error_type=%T",
		relayInfo.RequestId, apiErr.GetErrorCode(), apiErr.Err,
	))
	safeErr := types.NewErrorWithStatusCode(
		errors.New("upstream accepted; response unavailable; do not retry/contact admin"),
		types.ErrorCodeBadResponseBody,
		http.StatusBadGateway,
		types.ErrOptionWithSkipRetry(),
	)
	reservedQuota := relayInfo.FinalPreConsumedQuota
	if relayInfo.Billing != nil {
		reservedQuota = relayInfo.Billing.GetPreConsumedQuota()
	}
	useTimeSeconds := 0
	if !relayInfo.StartTime.IsZero() {
		useTimeSeconds = max(int(time.Since(relayInfo.StartTime).Seconds()), 0)
	}
	errorCode := string(apiErr.GetErrorCode())
	other := map[string]interface{}{
		"billing_fallback":  "reserved_quota",
		"upstream_accepted": true,
	}
	if errorCode != "" {
		other["response_error_code"] = errorCode
	}
	tokenName := ""
	if ctx != nil {
		tokenName = ctx.GetString("token_name")
	}
	params := model.RecordConsumeLogParams{
		ChannelId: relayInfo.ChannelId, ModelName: relayInfo.OriginModelName,
		TokenName: tokenName,
		Quota:     reservedQuota, Content: "upstream accepted; response processing failed; settled reserved quota",
		TokenId: relayInfo.TokenId, UseTimeSeconds: useTimeSeconds, IsStream: relayInfo.IsStream,
		Group: relayInfo.UsingGroup, Other: other,
	}
	projection := consumeBillingProjection(ctx, relayInfo, billingSettlementOperation, "sync_accepted_failure", params, true)
	err := SettleBillingWithProjection(ctx, relayInfo, reservedQuota, projection)
	if err == nil {
		return safeErr
	}
	var recoveryPending *model.BillingTerminalRecoveryPendingError
	if errors.As(err, &recoveryPending) {
		common.SysLog("accepted response fallback settlement is durable and pending replay")
		return safeErr
	}
	if billingProjectionFactCommitted(projection) || (projection == nil && billingSettlementFactCommitted(relayInfo.RequestId, billingSettlementOperation)) {
		common.SysLog("accepted response fallback settlement committed with durable follow-up pending")
		return safeErr
	}
	return newPostConsumeBillingError(err)
}

func stageSynchronousSubscriptionProjection(relayInfo *relaycommon.RelayInfo, actualQuota int) int64 {
	previous := relayInfo.SubscriptionPostDelta
	if relayInfo.BillingSource != BillingSourceSubscription {
		return previous
	}
	preConsumed := relayInfo.FinalPreConsumedQuota
	if session, ok := relayInfo.Billing.(*BillingSession); ok {
		preConsumed = session.GetPreConsumedQuota()
	}
	relayInfo.SubscriptionPostDelta += int64(actualQuota - preConsumed)
	return previous
}

func consumeBillingProjection(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, dependencyOperation string, kind string, params model.RecordConsumeLogParams, countUsage bool) *model.BillingProjectionSpec {
	if relayInfo == nil {
		return nil
	}
	requestId := billingRequestId(relayInfo.RequestId)
	relayInfo.RequestId = requestId
	projectionKey := model.BillingProjectionKey(requestId, dependencyOperation, kind)
	logEnabled := common.LogConsumeEnabled
	if !logEnabled && !countUsage {
		return nil
	}
	createdAt := common.GetTimestamp()
	username, upstreamRequestId := "", ""
	if ctx != nil {
		username = ctx.GetString("username")
		upstreamRequestId = ctx.GetString(common.UpstreamRequestIdKey)
	}
	promptTokens := max(params.PromptTokens, 0)
	completionTokens := max(params.CompletionTokens, 0)
	useTime := max(params.UseTimeSeconds, 0)
	userQuotaDelta, requestDelta, channelQuotaDelta := 0, 0, 0
	if countUsage {
		userQuotaDelta = params.Quota
		requestDelta = 1
		if relayInfo.ChannelId > 0 {
			channelQuotaDelta = params.Quota
		}
	}
	return &model.BillingProjectionSpec{
		ProjectionKey: projectionKey, DependencyRequestId: requestId, DependencyOperation: strings.TrimSpace(dependencyOperation),
		LogEnabled: logEnabled, LogUserId: relayInfo.UserId, LogUsername: username, LogCreatedAt: createdAt,
		LogType: model.LogTypeConsume, LogContent: params.Content, LogTokenName: params.TokenName,
		LogModelName: params.ModelName, LogQuota: params.Quota, LogPromptTokens: promptTokens,
		LogCompletionTokens: completionTokens, LogUseTime: useTime, LogIsStream: params.IsStream,
		LogChannelId: params.ChannelId, LogTokenId: params.TokenId, LogGroup: params.Group,
		LogIp: projectionClientIP(ctx, relayInfo.UserId), LogRequestId: projectionLogRequestId(ctx, requestId, projectionKey),
		LogUpstreamRequestId: upstreamRequestId, LogOther: common.MapToJsonStr(params.Other),
		QuotaDataEnabled: logEnabled && common.DataExportEnabled, QuotaDataNodeName: common.NodeName,
		QuotaDataTokenUsed: promptTokens + completionTokens,
		UserId:             relayInfo.UserId, UserUsedQuotaDelta: userQuotaDelta, UserRequestDelta: requestDelta,
		ChannelId: relayInfo.ChannelId, ChannelQuotaDelta: channelQuotaDelta,
	}
}
