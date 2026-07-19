package service

import (
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

func ReturnPreConsumedQuota(c *gin.Context, relayInfo *relaycommon.RelayInfo) error {
	if relayInfo == nil || relayInfo.FinalPreConsumedQuota == 0 {
		return nil
	}
	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费额度 %s", relayInfo.UserId, logger.FormatQuota(relayInfo.FinalPreConsumedQuota)))
	requestId := billingRequestId(relayInfo.RequestId)
	relayInfo.RequestId = requestId
	spec := model.BillingAdjustmentSpec{
		RequestId: requestId, Operation: "legacy_preconsume_refund",
		UserId: relayInfo.UserId, UserQuotaDelta: relayInfo.FinalPreConsumedQuota,
	}
	if !relayInfo.IsPlayground {
		spec.TokenId = relayInfo.TokenId
		spec.TokenQuotaDelta = relayInfo.FinalPreConsumedQuota
	}
	var err error
	for attempt := 0; attempt < 3; attempt++ {
		err = model.FinalizeBillingSettlement(model.BillingSettlementTransition{
			RequestId: requestId, Operation: billingSettlementOperation, FinalQuota: 0, Cancel: true,
		}, &spec)
		if err == nil {
			relayInfo.FinalPreConsumedQuota = 0
			return nil
		}
		var pending *model.BillingSettlementApplyPendingError
		var recoveryPending *model.BillingTerminalRecoveryPendingError
		if errors.As(err, &pending) || errors.As(err, &recoveryPending) {
			relayInfo.FinalPreConsumedQuota = 0
			common.SysLog("legacy pre-consume refund is durable and pending replay: " + err.Error())
			return nil
		}
		if attempt < 2 {
			time.Sleep(time.Duration(attempt+1) * 100 * time.Millisecond)
		}
	}
	common.SysLog("error return pre-consumed quota after retries: " + err.Error())
	return err
}

// PreConsumeQuota checks if the user has enough quota to pre-consume.
// It returns the pre-consumed quota if successful, or an error if not.
func PreConsumeQuota(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
	if err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	if userQuota <= 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}
	if userQuota-preConsumedQuota < 0 {
		return types.NewErrorWithStatusCode(fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
	}

	relayInfo.UserQuota = userQuota
	relayInfo.BillingSource = BillingSourceWallet
	if err := freezeBillingCommissionPolicy(relayInfo); err != nil {
		return types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
	}
	requestId := billingRequestId(relayInfo.RequestId)
	relayInfo.RequestId = requestId
	if preConsumedQuota > 0 {
		spec := model.BillingAdjustmentSpec{
			RequestId:      requestId,
			Operation:      "legacy_preconsume",
			UserId:         relayInfo.UserId,
			UserQuotaDelta: -preConsumedQuota,
		}
		if !relayInfo.IsPlayground {
			spec.TokenId = relayInfo.TokenId
			spec.TokenQuotaDelta = -preConsumedQuota
		}
		settlement := model.BillingSettlementSpec{
			RequestId: requestId, Operation: billingSettlementOperation, UserId: relayInfo.UserId,
			FundingSource: BillingSourceWallet, UsingGroup: relayInfo.UsingGroup,
			ChargedGroupRatio: relayInfo.PriceData.GroupRatioInfo.GroupRatio, ReservedQuota: preConsumedQuota,
			DeferCommission:  relayInfo.DeferBillingCommission,
			CommissionPolicy: relayInfo.BillingCommissionPolicy,
		}
		if !relayInfo.IsPlayground {
			settlement.TokenId = relayInfo.TokenId
		}
		var reserveErr error
		if relayInfo.TaskSubmissionRecoveryPrepared {
			reserveErr = model.ReserveTaskSubmissionBillingSettlementImmediate(relayInfo.TaskSubmissionRecoveryKind, spec, settlement)
		} else {
			reserveErr = model.ReserveBillingSettlementImmediate(spec, settlement)
		}
		if reserveErr != nil {
			err := reserveErr
			if err == model.ErrTokenQuotaInsufficient {
				return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
			}
			if err == model.ErrUserQuotaInsufficient {
				return types.NewErrorWithStatusCode(
					fmt.Errorf("预扣费额度失败, 用户额度已被其他请求占用"),
					types.ErrorCodeInsufficientUserQuota,
					http.StatusForbidden,
					types.ErrOptionWithSkipRetry(),
					types.ErrOptionWithNoRecordErrorLog(),
				)
			}
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		logger.LogInfo(c, fmt.Sprintf("用户 %d 预扣费 %s, 预扣费后剩余额度: %s", relayInfo.UserId, logger.FormatQuota(preConsumedQuota), logger.FormatQuota(userQuota-preConsumedQuota)))
	} else {
		settlement := model.BillingSettlementSpec{
			RequestId: requestId, Operation: billingSettlementOperation, UserId: relayInfo.UserId,
			FundingSource: BillingSourceWallet, UsingGroup: relayInfo.UsingGroup,
			ChargedGroupRatio: relayInfo.PriceData.GroupRatioInfo.GroupRatio, ReservedQuota: 0,
			DeferCommission:  relayInfo.DeferBillingCommission,
			CommissionPolicy: relayInfo.BillingCommissionPolicy,
		}
		if !relayInfo.IsPlayground {
			settlement.TokenId = relayInfo.TokenId
		}
		var reserveErr error
		if relayInfo.TaskSubmissionRecoveryPrepared {
			reserveErr = model.EnsureTaskSubmissionBillingSettlementReserved(relayInfo.TaskSubmissionRecoveryKind, settlement)
		} else {
			reserveErr = model.EnsureBillingSettlementReserved(settlement)
		}
		if reserveErr != nil {
			return types.NewError(reserveErr, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
	}
	relayInfo.FinalPreConsumedQuota = preConsumedQuota
	return nil
}
