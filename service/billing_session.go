package service

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"

	"github.com/gin-gonic/gin"
)

// ---------------------------------------------------------------------------
// BillingSession — 统一计费会话
// ---------------------------------------------------------------------------

// BillingSession 封装单次请求的预扣费/结算/退款生命周期。
// 实现 relaycommon.BillingSettler 接口。
type BillingSession struct {
	relayInfo        *relaycommon.RelayInfo
	funding          FundingSource
	preConsumedQuota int  // 实际预扣额度
	tokenConsumed    int  // 令牌额度实际扣减量
	extraReserved    int  // 发送前补充预扣的额度（订阅退款时需要单独回滚）
	fundingSettled   bool // funding.Settle 已成功，资金来源已提交
	settled          bool // Settle 全部完成（资金 + 令牌）
	refunded         bool // Refund 已调用
	billingRequestId string
	mu               sync.Mutex
}

func relaySubscriptionOccurredAt(info *relaycommon.RelayInfo) int64 {
	if info == nil {
		return 0
	}
	if info.SubscriptionOccurredAt > 0 {
		return info.SubscriptionOccurredAt
	}
	if !info.StartTime.IsZero() {
		return info.StartTime.Unix()
	}
	return common.GetTimestamp()
}

func (s *BillingSession) settlementSpec(reservedQuota int) model.BillingSettlementSpec {
	tokenId := s.relayInfo.TokenId
	if s.relayInfo.IsPlayground {
		tokenId = 0
	}
	spec := model.BillingSettlementSpec{
		RequestId: s.billingRequestId, Operation: billingSettlementOperation,
		UserId: s.relayInfo.UserId, TokenId: tokenId, FundingSource: s.funding.Source(),
		UsingGroup: s.relayInfo.UsingGroup, ChargedGroupRatio: s.relayInfo.PriceData.GroupRatioInfo.GroupRatio,
		ReservedQuota: reservedQuota, DeferCommission: s.relayInfo.TaskRelayInfo != nil || s.relayInfo.DeferBillingCommission,
		CommissionPolicy: s.relayInfo.BillingCommissionPolicy,
	}
	if funding, ok := s.funding.(*SubscriptionFunding); ok {
		spec.SubscriptionId = funding.subscriptionId
		spec.SubscriptionPreConsumeRequestId = funding.requestId
		spec.SubscriptionResetEpoch = funding.resetEpoch
		spec.SubscriptionOccurredAt = funding.occurredAt
	}
	return spec
}

func (s *BillingSession) settlementAdjustment(actualQuota int, operation string) (*model.BillingAdjustmentSpec, int, error) {
	delta := actualQuota - s.preConsumedQuota
	if delta == 0 {
		return nil, 0, nil
	}
	spec := model.BillingAdjustmentSpec{RequestId: s.billingRequestId, Operation: operation}
	switch funding := s.funding.(type) {
	case *WalletFunding:
		spec.UserId = funding.userId
		spec.UserQuotaDelta = -delta
	case *SubscriptionFunding:
		spec.SubscriptionId = funding.subscriptionId
		spec.SubscriptionResetEpoch = funding.resetEpoch
		spec.SubscriptionOccurredAt = funding.occurredAt
		spec.SubscriptionQuotaDelta = int64(delta)
	default:
		return nil, 0, fmt.Errorf("unsupported funding source: %s", s.funding.Source())
	}
	if !s.relayInfo.IsPlayground {
		spec.TokenId = s.relayInfo.TokenId
		spec.TokenQuotaDelta = -delta
	}
	return &spec, delta, nil
}

// Settle persists the final charge fact before applying any account delta.
// A failed account application remains pending and is reconciled after restart.
func (s *BillingSession) Settle(actualQuota int) error {
	return s.settle(actualQuota, nil)
}

func (s *BillingSession) SettleWithProjection(actualQuota int, projection *model.BillingProjectionSpec) error {
	return s.settle(actualQuota, projection)
}

func (s *BillingSession) settle(actualQuota int, projection *model.BillingProjectionSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return nil
	}
	if actualQuota < 0 {
		return errors.New("billing settlement actual quota cannot be negative")
	}
	adjustment, delta, err := s.settlementAdjustment(actualQuota, "request_settle")
	if err != nil {
		return err
	}
	releaseCommission := s.relayInfo.TaskRelayInfo == nil
	var commission *model.BillingCommissionSnapshot
	var commissionPrepareErr error
	if releaseCommission {
		commission, commissionPrepareErr = prepareBillingCommission(s.relayInfo, actualQuota, s.billingRequestId, billingSettlementOperation)
		if commissionPrepareErr != nil {
			common.SysLog("failed to prepare billing commission snapshot; durable resolution is pending: " + commissionPrepareErr.Error())
		}
	}
	err = model.FinalizeBillingSettlementWithProjection(model.BillingSettlementTransition{
		RequestId: s.billingRequestId, Operation: billingSettlementOperation, FinalQuota: actualQuota,
		ReleaseCommission: releaseCommission, Commission: commission,
	}, adjustment, projection)
	if err != nil {
		var pending *model.BillingSettlementApplyPendingError
		var recoveryPending *model.BillingTerminalRecoveryPendingError
		if errors.As(err, &pending) || errors.As(err, &recoveryPending) {
			s.fundingSettled = true
			s.settled = true
			if s.funding.Source() == BillingSourceSubscription {
				s.relayInfo.SubscriptionPostDelta += int64(delta)
			}
		}
		return err
	}
	s.fundingSettled = true
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	s.settled = true
	if commissionPrepareErr != nil {
		return fmt.Errorf("billing commission resolution pending: %w", commissionPrepareErr)
	}
	if releaseCommission && s.funding.Source() == BillingSourceWallet && actualQuota > 0 {
		return DispatchBillingCommission(s.billingRequestId, billingSettlementOperation)
	}
	return nil
}

// CommitTaskSubmission binds the local task row to the deferred billing fact.
// It is called only after the upstream accepted the task.
func (s *BillingSession) CommitTaskSubmission(task *model.Task, actualQuota int) error {
	return s.commitTaskSubmission(task, actualQuota, nil)
}

func (s *BillingSession) commitTaskSubmission(task *model.Task, actualQuota int, projection *model.BillingProjectionSpec) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled {
		return errors.New("billing task submission is already settled")
	}
	if actualQuota < 0 || task == nil {
		return errors.New("billing task submission is invalid")
	}
	adjustment, delta, err := s.settlementAdjustment(actualQuota, "task_submit_settle")
	if err != nil {
		return err
	}
	task.PrivateData.BillingRequestId = s.billingRequestId
	err = model.InsertTaskWithBillingSettlement(task, model.BillingSettlementTransition{
		RequestId: s.billingRequestId, Operation: billingSettlementOperation, FinalQuota: actualQuota,
		ReleaseCommission: false,
	}, adjustment, projection)
	if err != nil {
		var pending *model.BillingSettlementApplyPendingError
		if errors.As(err, &pending) {
			s.fundingSettled = true
			s.settled = true
			if s.funding.Source() == BillingSourceSubscription {
				s.relayInfo.SubscriptionPostDelta += int64(delta)
			}
		}
		return err
	}
	s.fundingSettled = true
	s.settled = true
	if s.funding.Source() == BillingSourceSubscription {
		s.relayInfo.SubscriptionPostDelta += int64(delta)
	}
	return nil
}

func CommitTaskSubmission(relayInfo *relaycommon.RelayInfo, task *model.Task, actualQuota int) error {
	if relayInfo == nil || task == nil {
		return errors.New("task submission billing context is missing")
	}
	session, ok := relayInfo.Billing.(*BillingSession)
	if !ok {
		return errors.New("task submission requires a billing session")
	}
	return session.CommitTaskSubmission(task, actualQuota)
}

// CommitTaskSubmissionWithProjection freezes the initial task log and usage
// counters before committing the task. The task row, terminal settlement fact,
// and projection outbox therefore become durable in one transaction.
func CommitTaskSubmissionWithProjection(c *gin.Context, relayInfo *relaycommon.RelayInfo, task *model.Task, actualQuota int) error {
	if relayInfo == nil || task == nil {
		return errors.New("task submission billing context is missing")
	}
	session, ok := relayInfo.Billing.(*BillingSession)
	if !ok {
		return errors.New("task submission requires a billing session")
	}
	projection := taskInitialBillingProjection(c, relayInfo, task)
	if projection != nil {
		projection.DependencyRequestId = session.billingRequestId
		projection.DependencyOperation = billingSettlementOperation
		projection.ProjectionKey = model.BillingProjectionKey(session.billingRequestId, billingSettlementOperation, "task_initial")
		projection.LogRequestId = projectionLogRequestId(c, session.billingRequestId, projection.ProjectionKey)
		projection.LogQuota = actualQuota
		projection.UserUsedQuotaDelta = actualQuota
		projection.ChannelQuotaDelta = actualQuota
	}
	return session.commitTaskSubmission(task, actualQuota, projection)
}

// Refund records lifecycle cancellation and compensation together. A failed
// account application remains pending; the cancellation itself is not lost.
func (s *BillingSession) Refund(c *gin.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.settled || s.refunded || !s.needsRefundLocked() {
		return nil
	}

	logger.LogInfo(c, fmt.Sprintf("用户 %d 请求失败, 返还预扣费（token_quota=%s, funding=%s）",
		s.relayInfo.UserId,
		logger.FormatQuota(s.tokenConsumed),
		s.funding.Source(),
	))

	var adjustment *model.BillingAdjustmentSpec
	spec := model.BillingAdjustmentSpec{RequestId: s.billingRequestId, Operation: "request_cancel"}
	switch funding := s.funding.(type) {
	case *WalletFunding:
		if s.preConsumedQuota > 0 {
			spec.UserId = funding.userId
			spec.UserQuotaDelta = s.preConsumedQuota
		}
	case *SubscriptionFunding:
		spec.SubscriptionId = funding.subscriptionId
		spec.SubscriptionRequestId = funding.requestId
		spec.SubscriptionResetEpoch = funding.resetEpoch
		spec.SubscriptionOccurredAt = funding.occurredAt
		spec.SubscriptionQuotaDelta = -int64(s.extraReserved)
	default:
		return fmt.Errorf("unsupported funding source: %s", s.funding.Source())
	}
	if !s.relayInfo.IsPlayground && s.preConsumedQuota > 0 {
		spec.TokenId = s.relayInfo.TokenId
		spec.TokenQuotaDelta = s.preConsumedQuota
	}
	if spec.UserQuotaDelta != 0 || spec.TokenQuotaDelta != 0 || spec.SubscriptionQuotaDelta != 0 || spec.SubscriptionRequestId != "" {
		adjustment = &spec
	}
	err := model.FinalizeBillingSettlement(model.BillingSettlementTransition{
		RequestId: s.billingRequestId, Operation: billingSettlementOperation, FinalQuota: 0, Cancel: true,
	}, adjustment)
	if err != nil {
		var pending *model.BillingSettlementApplyPendingError
		var recoveryPending *model.BillingTerminalRecoveryPendingError
		if errors.As(err, &pending) || errors.As(err, &recoveryPending) {
			s.fundingSettled = true
			s.refunded = true
		}
		return err
	}
	s.fundingSettled = true
	s.refunded = true
	return nil
}

// NeedsRefund 返回是否存在需要退还的预扣状态。
func (s *BillingSession) NeedsRefund() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.needsRefundLocked()
}

func (s *BillingSession) needsRefundLocked() bool {
	if s.settled || s.refunded || s.fundingSettled {
		// fundingSettled 时资金来源已提交结算，不能再退预扣费
		return false
	}
	if s.tokenConsumed > 0 {
		return true
	}
	if wallet, ok := s.funding.(*WalletFunding); ok && wallet.consumed > 0 {
		return true
	}
	// 订阅可能在 tokenConsumed=0 时仍预扣了额度
	if sub, ok := s.funding.(*SubscriptionFunding); ok && sub.preConsumed > 0 {
		return true
	}
	return false
}

// GetPreConsumedQuota 返回实际预扣的额度。
func (s *BillingSession) GetPreConsumedQuota() int {
	return s.preConsumedQuota
}

func (s *BillingSession) Reserve(targetQuota int) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.settled || s.refunded || targetQuota <= s.preConsumedQuota {
		return nil
	}

	delta := targetQuota - s.preConsumedQuota
	if delta <= 0 {
		return nil
	}

	if funding, ok := s.funding.(*WalletFunding); ok {
		spec := model.BillingAdjustmentSpec{
			RequestId:      s.billingRequestId,
			Operation:      fmt.Sprintf("request_reserve_%d", targetQuota),
			UserId:         funding.userId,
			UserQuotaDelta: -delta,
		}
		if !s.relayInfo.IsPlayground {
			spec.TokenId = s.relayInfo.TokenId
			spec.TokenQuotaDelta = -delta
		}
		if err := model.ReserveBillingSettlementAdditionalImmediate(s.billingRequestId, billingSettlementOperation, targetQuota, spec); err != nil {
			return err
		}
		funding.consumed += delta
	} else if funding, ok := s.funding.(*SubscriptionFunding); ok {
		spec := model.BillingAdjustmentSpec{
			RequestId:              s.billingRequestId,
			Operation:              fmt.Sprintf("request_reserve_%d", targetQuota),
			SubscriptionId:         funding.subscriptionId,
			SubscriptionResetEpoch: funding.resetEpoch,
			SubscriptionOccurredAt: funding.occurredAt,
			SubscriptionQuotaDelta: int64(delta),
		}
		if !s.relayInfo.IsPlayground {
			spec.TokenId = s.relayInfo.TokenId
			spec.TokenQuotaDelta = -delta
		}
		if err := model.ReserveBillingSettlementAdditionalImmediate(s.billingRequestId, billingSettlementOperation, targetQuota, spec); err != nil {
			return err
		}
	} else {
		return fmt.Errorf("unsupported funding source: %s", s.funding.Source())
	}

	s.preConsumedQuota += delta
	s.tokenConsumed += delta
	s.extraReserved += delta
	s.syncRelayInfo()
	return nil
}

// ---------------------------------------------------------------------------
// PreConsume — 统一预扣费入口
// ---------------------------------------------------------------------------

// preConsume 执行预扣费：令牌预扣 -> 资金来源预扣。
// 任一步骤失败时原子回滚已完成的步骤。
func (s *BillingSession) preConsume(c *gin.Context, quota int) *types.NewAPIError {
	effectiveQuota := quota

	if effectiveQuota > 0 {
		logger.LogInfo(c, fmt.Sprintf("用户 %d 需要预扣费 %s (funding=%s)", s.relayInfo.UserId, logger.FormatQuota(effectiveQuota), s.funding.Source()))
	}
	if funding, ok := s.funding.(*WalletFunding); ok && effectiveQuota > 0 {
		spec := model.BillingAdjustmentSpec{
			RequestId:      s.billingRequestId,
			Operation:      "request_preconsume",
			UserId:         funding.userId,
			UserQuotaDelta: -effectiveQuota,
		}
		if !s.relayInfo.IsPlayground {
			spec.TokenId = s.relayInfo.TokenId
			spec.TokenQuotaDelta = -effectiveQuota
		}
		var reserveErr error
		if s.relayInfo.TaskSubmissionRecoveryPrepared {
			reserveErr = model.ReserveTaskSubmissionBillingSettlementImmediate(s.relayInfo.TaskSubmissionRecoveryKind, spec, s.settlementSpec(effectiveQuota))
		} else {
			reserveErr = model.ReserveBillingSettlementImmediate(spec, s.settlementSpec(effectiveQuota))
		}
		if reserveErr != nil {
			err := reserveErr
			if errors.Is(err, model.ErrTokenQuotaInsufficient) {
				return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
			}
			if errors.Is(err, model.ErrUserQuotaInsufficient) {
				return types.NewErrorWithStatusCode(
					fmt.Errorf("用户额度不足"),
					types.ErrorCodeInsufficientUserQuota,
					http.StatusForbidden,
					types.ErrOptionWithSkipRetry(),
					types.ErrOptionWithNoRecordErrorLog(),
				)
			}
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		funding.consumed = effectiveQuota
		s.tokenConsumed = effectiveQuota
		s.preConsumedQuota = effectiveQuota
		s.syncRelayInfo()
		return nil
	}
	if funding, ok := s.funding.(*SubscriptionFunding); ok && effectiveQuota > 0 {
		tokenId := 0
		tokenAmount := 0
		if !s.relayInfo.IsPlayground {
			tokenId = s.relayInfo.TokenId
			tokenAmount = effectiveQuota
		}
		var reserveErr error
		if s.relayInfo.TaskSubmissionRecoveryPrepared {
			reserveErr = funding.PreConsumeTaskSubmissionWithTokenAndSettlement(s.relayInfo.TaskSubmissionRecoveryKind, tokenId, tokenAmount, s.settlementSpec(effectiveQuota))
		} else {
			reserveErr = funding.PreConsumeWithTokenAndSettlement(tokenId, tokenAmount, s.settlementSpec(effectiveQuota))
		}
		if reserveErr != nil {
			err := reserveErr
			if errors.Is(err, model.ErrTokenQuotaInsufficient) {
				return types.NewErrorWithStatusCode(err, types.ErrorCodePreConsumeTokenQuotaFailed, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
			}
			errMsg := err.Error()
			if strings.Contains(errMsg, "no active subscription") || strings.Contains(errMsg, "subscription quota insufficient") {
				return types.NewErrorWithStatusCode(fmt.Errorf("订阅额度不足或未配置订阅: %s", errMsg), types.ErrorCodeInsufficientUserQuota, http.StatusForbidden, types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
			}
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		s.tokenConsumed = effectiveQuota
		s.preConsumedQuota = effectiveQuota
		s.syncRelayInfo()
		return nil
	}

	if effectiveQuota == 0 {
		var reserveErr error
		if s.relayInfo.TaskSubmissionRecoveryPrepared {
			reserveErr = model.EnsureTaskSubmissionBillingSettlementReserved(s.relayInfo.TaskSubmissionRecoveryKind, s.settlementSpec(0))
		} else {
			reserveErr = model.EnsureBillingSettlementReserved(s.settlementSpec(0))
		}
		if reserveErr != nil {
			err := reserveErr
			return types.NewError(err, types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
		}
		s.preConsumedQuota = 0
		s.syncRelayInfo()
		return nil
	}
	return types.NewError(fmt.Errorf("unsupported funding source: %s", s.funding.Source()), types.ErrorCodeUpdateDataError, types.ErrOptionWithSkipRetry())
}

// syncRelayInfo 将 BillingSession 的状态同步到 RelayInfo 的兼容字段上。
func (s *BillingSession) syncRelayInfo() {
	info := s.relayInfo
	info.FinalPreConsumedQuota = s.preConsumedQuota
	info.BillingSource = s.funding.Source()

	if sub, ok := s.funding.(*SubscriptionFunding); ok {
		info.SubscriptionId = sub.subscriptionId
		info.SubscriptionResetEpoch = sub.resetEpoch
		info.SubscriptionOccurredAt = sub.occurredAt
		info.SubscriptionPreConsumed = sub.preConsumed + int64(s.extraReserved)
		info.SubscriptionPostDelta = 0
		info.SubscriptionAmountTotal = sub.AmountTotal
		info.SubscriptionAmountUsedAfterPreConsume = sub.AmountUsedAfter + int64(s.extraReserved)
		info.SubscriptionPlanId = sub.PlanId
		info.SubscriptionPlanTitle = sub.PlanTitle
	} else {
		info.SubscriptionId = 0
		info.SubscriptionResetEpoch = 0
		info.SubscriptionOccurredAt = 0
		info.SubscriptionPreConsumed = 0
	}
}

// ---------------------------------------------------------------------------
// NewBillingSession 工厂 — 根据计费偏好创建会话并处理回退
// ---------------------------------------------------------------------------

// NewBillingSession 根据用户计费偏好创建 BillingSession，处理 subscription_first / wallet_first 的回退。
func NewBillingSession(c *gin.Context, relayInfo *relaycommon.RelayInfo, preConsumedQuota int) (*BillingSession, *types.NewAPIError) {
	if relayInfo == nil {
		return nil, types.NewError(fmt.Errorf("relayInfo is nil"), types.ErrorCodeInvalidRequest, types.ErrOptionWithSkipRetry())
	}

	pref := common.NormalizeBillingPreference(relayInfo.UserSetting.BillingPreference)
	requestId := billingRequestId(relayInfo.RequestId)
	if strings.TrimSpace(relayInfo.RequestId) == "" {
		relayInfo.RequestId = requestId
	}

	// 钱包路径需要先检查用户额度
	tryWallet := func() (*BillingSession, *types.NewAPIError) {
		userQuota, err := model.GetUserQuota(relayInfo.UserId, false)
		if err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if userQuota <= 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("用户额度不足, 剩余额度: %s", logger.FormatQuota(userQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		if userQuota-preConsumedQuota < 0 {
			return nil, types.NewErrorWithStatusCode(
				fmt.Errorf("预扣费额度失败, 用户剩余额度: %s, 需要预扣费额度: %s", logger.FormatQuota(userQuota), logger.FormatQuota(preConsumedQuota)),
				types.ErrorCodeInsufficientUserQuota, http.StatusForbidden,
				types.ErrOptionWithSkipRetry(), types.ErrOptionWithNoRecordErrorLog())
		}
		relayInfo.UserQuota = userQuota
		relayInfo.BillingSource = BillingSourceWallet
		if err := freezeBillingCommissionPolicy(relayInfo); err != nil {
			return nil, types.NewError(err, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}

		session := &BillingSession{
			relayInfo:        relayInfo,
			funding:          &WalletFunding{userId: relayInfo.UserId},
			billingRequestId: requestId,
		}
		if apiErr := session.preConsume(c, preConsumedQuota); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	trySubscription := func() (*BillingSession, *types.NewAPIError) {
		relayInfo.BillingSource = BillingSourceSubscription
		relayInfo.BillingCommissionPolicy = ""
		subConsume := int64(preConsumedQuota)
		if subConsume <= 0 {
			subConsume = 1
		}
		session := &BillingSession{
			relayInfo:        relayInfo,
			billingRequestId: requestId,
			funding: &SubscriptionFunding{
				requestId:  relayInfo.RequestId,
				userId:     relayInfo.UserId,
				modelName:  relayInfo.OriginModelName,
				amount:     subConsume,
				occurredAt: relaySubscriptionOccurredAt(relayInfo),
			},
		}
		// 必须传 subConsume 而非 preConsumedQuota，保证 SubscriptionFunding.amount、
		// preConsume 参数和 FinalPreConsumedQuota 三者一致，避免订阅多扣费。
		if apiErr := session.preConsume(c, int(subConsume)); apiErr != nil {
			return nil, apiErr
		}
		return session, nil
	}

	switch pref {
	case "subscription_only":
		return trySubscription()
	case "wallet_only":
		return tryWallet()
	case "wallet_first":
		session, err := tryWallet()
		if err != nil {
			if err.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				return trySubscription()
			}
			return nil, err
		}
		return session, nil
	case "subscription_first":
		fallthrough
	default:
		hasSub, subCheckErr := model.HasActiveUserSubscription(relayInfo.UserId)
		if subCheckErr != nil {
			return nil, types.NewError(subCheckErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
		}
		if !hasSub {
			return tryWallet()
		}
		session, apiErr := trySubscription()
		if apiErr != nil {
			if apiErr.GetErrorCode() == types.ErrorCodeInsufficientUserQuota {
				// 仅当用户的活跃订阅允许钱包回退时才回退到钱包，否则返回订阅额度不足错误
				allowOverflow, overflowErr := model.UserActiveSubscriptionsAllowWalletOverflow(relayInfo.UserId)
				if overflowErr != nil {
					return nil, types.NewError(overflowErr, types.ErrorCodeQueryDataError, types.ErrOptionWithSkipRetry())
				}
				if allowOverflow {
					return tryWallet()
				}
				return nil, apiErr
			}
			return nil, apiErr
		}
		return session, nil
	}
}

func billingRequestId(requestId string) string {
	requestId = strings.TrimSpace(requestId)
	if requestId != "" {
		return requestId
	}
	return common.GetUUID()
}
