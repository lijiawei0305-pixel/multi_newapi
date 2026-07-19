package service

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"

	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/ratio_setting"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

// ---------------------------------------------------------------------------
// 异步任务计费辅助函数
// ---------------------------------------------------------------------------

// taskIsSubscription 判断任务是否通过订阅计费。
func taskIsSubscription(task *model.Task) bool {
	return task.PrivateData.BillingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId > 0
}

func taskBillingRequestId(task *model.Task) string {
	if strings.TrimSpace(task.PrivateData.BillingRequestId) != "" {
		return strings.TrimSpace(task.PrivateData.BillingRequestId)
	}
	if task.ID > 0 {
		return fmt.Sprintf("task:%d", task.ID)
	}
	digest := sha256.Sum256([]byte(task.TaskID))
	return fmt.Sprintf("task:%x", digest)
}

func taskSubscriptionOccurredAt(task *model.Task) int64 {
	if task == nil {
		return 0
	}
	if task.PrivateData.SubscriptionOccurredAt > 0 {
		return task.PrivateData.SubscriptionOccurredAt
	}
	if task.SubmitTime > 0 {
		return task.SubmitTime
	}
	return task.CreatedAt
}

func midjourneySubscriptionOccurredAt(task *model.Midjourney) int64 {
	if task == nil {
		return 0
	}
	if task.SubscriptionOccurredAt > 0 {
		return task.SubscriptionOccurredAt
	}
	occurredAt := task.SubmitTime
	if occurredAt > 100_000_000_000 {
		occurredAt /= 1000
	}
	return occurredAt
}

func taskBillingAdjustment(task *model.Task, actualQuota int) (*model.BillingAdjustmentSpec, int) {
	balanceDelta := task.Quota - actualQuota
	if balanceDelta == 0 {
		return nil, 0
	}
	spec := &model.BillingAdjustmentSpec{
		RequestId: taskBillingRequestId(task),
		Operation: "final_adjustment",
	}
	if taskIsSubscription(task) {
		spec.SubscriptionId = task.PrivateData.SubscriptionId
		spec.SubscriptionResetEpoch = task.PrivateData.SubscriptionResetEpoch
		spec.SubscriptionOccurredAt = taskSubscriptionOccurredAt(task)
		spec.SubscriptionQuotaDelta = int64(-balanceDelta)
	} else {
		spec.UserId = task.UserId
		spec.UserQuotaDelta = balanceDelta
	}
	if task.PrivateData.TokenId > 0 {
		spec.TokenId = task.PrivateData.TokenId
		spec.TokenQuotaDelta = balanceDelta
	}
	return spec, balanceDelta
}

// taskBillingOther 从 task 的 BillingContext 构建日志 Other 字段。
func taskBillingOther(task *model.Task) map[string]interface{} {
	other := make(map[string]interface{})
	if bc := task.PrivateData.BillingContext; bc != nil {
		other["model_price"] = bc.ModelPrice
		if bc.ModelRatio > 0 {
			other["model_ratio"] = bc.ModelRatio
		}
		other["group_ratio"] = bc.GroupRatio
		if len(bc.OtherRatios) > 0 {
			for k, v := range bc.OtherRatios {
				other[k] = v
			}
		}
	}
	props := task.Properties
	if props.UpstreamModelName != "" && props.UpstreamModelName != props.OriginModelName {
		other["is_model_mapped"] = true
		other["upstream_model_name"] = props.UpstreamModelName
	}
	return other
}

// taskModelName 从 BillingContext 或 Properties 中获取模型名称。
func taskModelName(task *model.Task) string {
	if bc := task.PrivateData.BillingContext; bc != nil && bc.OriginModelName != "" {
		return bc.OriginModelName
	}
	return task.Properties.OriginModelName
}

// TransitionTaskWithBilling commits the terminal task CAS, lifecycle fact,
// exact account intent, and frozen commission payload together.
func TransitionTaskWithBilling(ctx context.Context, task *model.Task, fromStatus model.TaskStatus, actualQuota int, reason string) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("task is nil")
	}
	if actualQuota < 0 {
		return false, fmt.Errorf("actual task quota cannot be negative")
	}
	if task.PrivateData.BillingSource == BillingSourceFree {
		task.Quota = 0
		return task.UpdateWithStatus(fromStatus)
	}
	preConsumedQuota := task.Quota
	requestId := taskBillingRequestId(task)
	hasSubscriptionPreConsumeRecord := strings.TrimSpace(task.PrivateData.BillingRequestId) != ""
	subscriptionPreConsumeRequestId := ""
	if hasSubscriptionPreConsumeRecord {
		subscriptionPreConsumeRequestId = requestId
	}
	fundingSource := task.PrivateData.BillingSource
	if fundingSource == "" {
		fundingSource = BillingSourceWallet
	}
	if fundingSource == BillingSourceSubscription && task.PrivateData.SubscriptionId <= 0 {
		return false, errors.New("subscription task billing metadata is corrupt")
	}
	subscriptionOccurredAt := int64(0)
	if fundingSource == BillingSourceSubscription {
		subscriptionOccurredAt = taskSubscriptionOccurredAt(task)
		if subscriptionOccurredAt <= 0 {
			return false, errors.New("subscription task billing occurred time is missing")
		}
	}
	initialReservedQuota := preConsumedQuota
	commissionPolicy := ""
	if event, err := model.GetBillingSettlement(requestId, billingSettlementOperation); err == nil {
		initialReservedQuota = event.InitialReservedQuota
		commissionPolicy = event.CommissionPolicy
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	cancel := task.Status == model.TaskStatusFailure
	balanceDelta := preConsumedQuota - actualQuota
	var spec *model.BillingAdjustmentSpec
	adjustment := model.BillingAdjustmentSpec{RequestId: requestId, Operation: "task_final_adjustment"}
	if fundingSource == BillingSourceSubscription {
		adjustment.SubscriptionId = task.PrivateData.SubscriptionId
		adjustment.SubscriptionResetEpoch = task.PrivateData.SubscriptionResetEpoch
		adjustment.SubscriptionOccurredAt = subscriptionOccurredAt
		if cancel {
			if hasSubscriptionPreConsumeRecord {
				adjustment.SubscriptionRequestId = requestId
				adjustment.SubscriptionQuotaDelta = -int64(preConsumedQuota - initialReservedQuota)
			} else {
				adjustment.SubscriptionQuotaDelta = -int64(preConsumedQuota)
			}
		} else {
			adjustment.SubscriptionQuotaDelta = int64(actualQuota - preConsumedQuota)
		}
	} else if balanceDelta != 0 {
		adjustment.UserId = task.UserId
		adjustment.UserQuotaDelta = balanceDelta
	}
	if task.PrivateData.TokenId > 0 && balanceDelta != 0 {
		adjustment.TokenId = task.PrivateData.TokenId
		adjustment.TokenQuotaDelta = balanceDelta
	}
	if adjustment.UserQuotaDelta != 0 || adjustment.TokenQuotaDelta != 0 || adjustment.SubscriptionQuotaDelta != 0 || adjustment.SubscriptionRequestId != "" {
		spec = &adjustment
	}
	groupRatio := 0.0
	if task.PrivateData.BillingContext != nil {
		groupRatio = task.PrivateData.BillingContext.GroupRatio
	}
	var commission *model.BillingCommissionSnapshot
	var commissionErr error
	if !cancel {
		if commissionPolicy != "" {
			commission, commissionErr = materializeBillingCommissionPolicy(commissionPolicy, actualQuota, requestId, billingSettlementOperation)
		} else {
			commission, commissionErr = prepareBillingCommissionFields(task.UserId, actualQuota, requestId, billingSettlementOperation, fundingSource, task.Group, groupRatio)
		}
	}
	projection := taskAdjustmentBillingProjection(task, preConsumedQuota, actualQuota, reason)
	task.Quota = actualQuota
	won, err := model.UpdateTaskWithBillingSettlement(task, fromStatus, model.BillingSettlementSpec{
		RequestId: requestId, Operation: billingSettlementOperation, UserId: task.UserId,
		TokenId: task.PrivateData.TokenId, SubscriptionId: task.PrivateData.SubscriptionId,
		SubscriptionPreConsumeRequestId: subscriptionPreConsumeRequestId,
		SubscriptionResetEpoch:          task.PrivateData.SubscriptionResetEpoch,
		SubscriptionOccurredAt:          subscriptionOccurredAt,
		FundingSource:                   fundingSource, UsingGroup: task.Group, ChargedGroupRatio: groupRatio,
		ReservedQuota: preConsumedQuota, DeferCommission: true,
		CommissionPolicy: commissionPolicy,
	}, model.BillingSettlementTransition{
		RequestId: requestId, Operation: billingSettlementOperation, FinalQuota: actualQuota,
		ReleaseCommission: !cancel, Cancel: cancel, Commission: commission,
	}, spec, projection)
	if !won || (err != nil && !isBillingSettlementApplyPending(err)) {
		task.Quota = preConsumedQuota
		return won, err
	}
	if err != nil {
		return true, err
	}
	if commissionErr != nil {
		return true, fmt.Errorf("task billing commission resolution pending: %w", commissionErr)
	}
	if !cancel && fundingSource == BillingSourceWallet && actualQuota > 0 {
		if err := DispatchBillingCommission(requestId, billingSettlementOperation); err != nil {
			return true, err
		}
	}
	return true, nil
}

func isBillingSettlementApplyPending(err error) bool {
	var pending *model.BillingSettlementApplyPendingError
	return errors.As(err, &pending)
}

func CommitMidjourneySubmission(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, deferCommission bool) error {
	return commitMidjourneySubmission(relayInfo, task, deferCommission, nil)
}

func CommitMidjourneySubmissionWithProjection(c *gin.Context, relayInfo *relaycommon.RelayInfo, task *model.Midjourney, deferCommission bool) error {
	projection := midjourneyInitialBillingProjection(c, relayInfo, task)
	if projection != nil {
		requestId := billingRequestId(relayInfo.RequestId)
		projection.DependencyRequestId = requestId
		projection.DependencyOperation = billingSettlementOperation
		projection.ProjectionKey = model.BillingProjectionKey(requestId, billingSettlementOperation, "midjourney_initial")
		projection.LogRequestId = projectionLogRequestId(c, requestId, projection.ProjectionKey)
	}
	return commitMidjourneySubmission(relayInfo, task, deferCommission, projection)
}

func commitMidjourneySubmission(relayInfo *relaycommon.RelayInfo, task *model.Midjourney, deferCommission bool, projection *model.BillingProjectionSpec) error {
	if relayInfo == nil || task == nil {
		return errors.New("midjourney submission billing context is missing")
	}
	requestId := billingRequestId(relayInfo.RequestId)
	task.BillingRequestId = requestId
	task.SubscriptionResetEpoch = relayInfo.SubscriptionResetEpoch
	task.SubscriptionOccurredAt = relaySubscriptionOccurredAt(relayInfo)
	task.ChargedGroupRatio = relayInfo.PriceData.GroupRatioInfo.GroupRatio
	var commission *model.BillingCommissionSnapshot
	var commissionErr error
	if !deferCommission {
		commission, commissionErr = prepareBillingCommission(relayInfo, task.Quota, requestId, billingSettlementOperation)
	}
	err := model.InsertMidjourneyWithBillingSettlement(task, model.BillingSettlementTransition{
		RequestId: requestId, Operation: billingSettlementOperation, FinalQuota: task.Quota,
		ReleaseCommission: !deferCommission, Commission: commission,
	}, nil, projection)
	if err != nil {
		return err
	}
	if commissionErr != nil {
		return fmt.Errorf("midjourney commission resolution pending: %w", commissionErr)
	}
	if !deferCommission && task.BillingSource != BillingSourceSubscription && task.Quota > 0 {
		return DispatchBillingCommission(requestId, billingSettlementOperation)
	}
	return nil
}

// TransitionMidjourneyWithBilling atomically binds a Midjourney terminal CAS
// to either cancellation/refund or successful commission release.
func TransitionMidjourneyWithBilling(ctx context.Context, task *model.Midjourney, fromStatus string, reason string) (bool, error) {
	if task == nil {
		return false, fmt.Errorf("midjourney task is nil")
	}
	if task.BillingSource == BillingSourceFree {
		task.Quota = 0
		return task.UpdateWithStatus(fromStatus)
	}
	requestId := strings.TrimSpace(task.BillingRequestId)
	hasPreConsumeRecord := requestId != ""
	if requestId == "" {
		requestId = fmt.Sprintf("midjourney:%d", task.Id)
	}
	fundingSource := task.BillingSource
	if fundingSource == "" {
		fundingSource = BillingSourceWallet
	}
	if fundingSource == BillingSourceSubscription && task.SubscriptionId <= 0 {
		return false, errors.New("subscription midjourney billing metadata is corrupt")
	}
	subscriptionOccurredAt := int64(0)
	if fundingSource == BillingSourceSubscription {
		subscriptionOccurredAt = midjourneySubscriptionOccurredAt(task)
		if subscriptionOccurredAt <= 0 {
			return false, errors.New("subscription midjourney billing occurred time is missing")
		}
	}
	cancel := task.Status == "FAILURE"
	preConsumedQuota := task.Quota
	commissionPolicy := ""
	if event, err := model.GetBillingSettlement(requestId, billingSettlementOperation); err == nil {
		commissionPolicy = event.CommissionPolicy
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, err
	}
	var spec *model.BillingAdjustmentSpec
	if cancel && preConsumedQuota > 0 {
		adjustment := model.BillingAdjustmentSpec{RequestId: requestId, Operation: "midjourney_terminal"}
		if fundingSource == BillingSourceSubscription {
			adjustment.SubscriptionId = task.SubscriptionId
			adjustment.SubscriptionResetEpoch = task.SubscriptionResetEpoch
			adjustment.SubscriptionOccurredAt = subscriptionOccurredAt
			if hasPreConsumeRecord {
				adjustment.SubscriptionRequestId = requestId
			} else {
				adjustment.SubscriptionQuotaDelta = -int64(preConsumedQuota)
			}
		} else {
			adjustment.UserId = task.UserId
			adjustment.UserQuotaDelta = preConsumedQuota
		}
		if task.TokenId > 0 {
			adjustment.TokenId = task.TokenId
			adjustment.TokenQuotaDelta = preConsumedQuota
		}
		spec = &adjustment
	}
	var commission *model.BillingCommissionSnapshot
	var commissionErr error
	if !cancel {
		if commissionPolicy != "" {
			commission, commissionErr = materializeBillingCommissionPolicy(commissionPolicy, preConsumedQuota, requestId, billingSettlementOperation)
		} else {
			commission, commissionErr = prepareBillingCommissionFields(task.UserId, preConsumedQuota, requestId, billingSettlementOperation, fundingSource, task.Group, task.ChargedGroupRatio)
		}
	} else {
		task.Quota = 0
	}
	projection := (*model.BillingProjectionSpec)(nil)
	if cancel {
		projection = midjourneyTerminalBillingProjection(task, preConsumedQuota, reason)
	}
	subscriptionPreConsumeRequestId := ""
	if hasPreConsumeRecord && fundingSource == BillingSourceSubscription {
		subscriptionPreConsumeRequestId = requestId
	}
	won, err := model.UpdateMidjourneyWithBillingSettlement(task, fromStatus, model.BillingSettlementSpec{
		RequestId: requestId, Operation: billingSettlementOperation, UserId: task.UserId, TokenId: task.TokenId,
		SubscriptionId: task.SubscriptionId, SubscriptionPreConsumeRequestId: subscriptionPreConsumeRequestId,
		SubscriptionResetEpoch: task.SubscriptionResetEpoch, SubscriptionOccurredAt: subscriptionOccurredAt, FundingSource: fundingSource,
		UsingGroup: task.Group, ChargedGroupRatio: task.ChargedGroupRatio, ReservedQuota: preConsumedQuota, DeferCommission: true,
		CommissionPolicy: commissionPolicy,
	}, model.BillingSettlementTransition{
		RequestId: requestId, Operation: billingSettlementOperation, FinalQuota: task.Quota,
		ReleaseCommission: !cancel, Cancel: cancel, Commission: commission,
	}, spec, projection)
	if !won || (err != nil && !isBillingSettlementApplyPending(err)) {
		if cancel {
			task.Quota = preConsumedQuota
		}
		return won, err
	}
	if err != nil {
		return true, err
	}
	if cancel && spec != nil {
		logger.LogInfo(ctx, fmt.Sprintf("Midjourney task %s refunded after terminal failure", task.MjId))
	}
	if commissionErr != nil {
		return true, fmt.Errorf("midjourney commission resolution pending: %w", commissionErr)
	}
	if !cancel && fundingSource == BillingSourceWallet && preConsumedQuota > 0 {
		if err := DispatchBillingCommission(requestId, billingSettlementOperation); err != nil {
			return true, err
		}
	}
	return true, nil
}

// RefundTaskQuota is the idempotent compatibility entry point for a task that
// is already terminal. Production transitions should use
// TransitionTaskWithBilling so the status and intent commit atomically.
func RefundTaskQuota(ctx context.Context, task *model.Task, reason string) {
	quota := task.Quota
	if quota == 0 {
		return
	}
	spec, _ := taskBillingAdjustment(task, 0)
	if spec == nil {
		return
	}
	projection := taskAdjustmentBillingProjection(task, quota, 0, reason)
	if projection == nil {
		return
	}
	if err := model.ApplyBillingAdjustmentWithProjectionOnce(*spec, *projection); err != nil {
		logger.LogWarn(ctx, fmt.Sprintf("退还任务额度失败 task %s: %s", task.TaskID, err.Error()))
		return
	}
}

// RecalculateTaskQuota 通用的异步差额结算。
// actualQuota 是任务完成后的实际应扣额度，与预扣额度 (task.Quota) 做差额结算。
// reason 用于日志记录（例如 "token重算" 或 "adaptor调整"）。
func RecalculateTaskQuota(ctx context.Context, task *model.Task, actualQuota int, reason string) {
	if actualQuota <= 0 {
		return
	}
	preConsumedQuota := task.Quota
	quotaDelta := actualQuota - preConsumedQuota

	if quotaDelta == 0 {
		logger.LogInfo(ctx, fmt.Sprintf("任务 %s 预扣费准确（%s，%s）",
			task.TaskID, logger.LogQuota(actualQuota), reason))
		return
	}

	logger.LogInfo(ctx, fmt.Sprintf("任务 %s 差额结算：delta=%s（实际：%s，预扣：%s，%s）",
		task.TaskID,
		logger.LogQuota(quotaDelta),
		logger.LogQuota(actualQuota),
		logger.LogQuota(preConsumedQuota),
		reason,
	))

	spec, _ := taskBillingAdjustment(task, actualQuota)
	if spec == nil {
		return
	}
	projection := taskAdjustmentBillingProjection(task, preConsumedQuota, actualQuota, reason)
	if projection == nil {
		return
	}
	if err := model.ApplyBillingAdjustmentWithProjectionOnce(*spec, *projection); err != nil {
		logger.LogError(ctx, fmt.Sprintf("差额结算失败 task %s: %s", task.TaskID, err.Error()))
		return
	}
	preConsumedQuota = task.Quota
	task.Quota = actualQuota
	if task.ID > 0 {
		if err := model.DB.Model(&model.Task{}).Where("id = ?", task.ID).Update("quota", actualQuota).Error; err != nil {
			logger.LogWarn(ctx, fmt.Sprintf("更新任务实际额度失败 task %s: %s", task.TaskID, err.Error()))
		}
	}
}

// CalculateTaskQuotaByTokens snapshots the final token-based charge without
// mutating quota. The caller can bind the returned amount to a terminal CAS.
func CalculateTaskQuotaByTokens(task *model.Task, totalTokens int) (int, string, bool) {
	if totalTokens <= 0 {
		return 0, "", false
	}

	modelName := taskModelName(task)

	// 获取模型价格和倍率
	modelRatio, hasRatioSetting, _ := ratio_setting.GetModelRatio(modelName)
	// 只有配置了倍率(非固定价格)时才按 token 重新计费
	if !hasRatioSetting || modelRatio <= 0 {
		return 0, "", false
	}

	// 获取用户和组的倍率信息
	group := task.Group
	if group == "" {
		user, err := model.GetUserById(task.UserId, false)
		if err == nil {
			group = user.Group
		}
	}
	if group == "" {
		return 0, "", false
	}

	groupRatio := ratio_setting.GetGroupRatio(group)
	userGroupRatio, hasUserGroupRatio := ratio_setting.GetGroupGroupRatio(group, group)

	var finalGroupRatio float64
	if hasUserGroupRatio {
		finalGroupRatio = userGroupRatio
	} else {
		finalGroupRatio = groupRatio
	}

	// 计算 OtherRatios 乘积（视频折扣、时长等）
	otherMultiplier := 1.0
	if bc := task.PrivateData.BillingContext; bc != nil {
		for _, r := range bc.OtherRatios {
			if r != 1.0 && r > 0 {
				otherMultiplier *= r
			}
		}
	}

	// 计算实际应扣费额度: totalTokens * modelRatio * groupRatio * otherMultiplier
	actualQuota := int(float64(totalTokens) * modelRatio * finalGroupRatio * otherMultiplier)

	reason := fmt.Sprintf("token重算：tokens=%d, modelRatio=%.2f, groupRatio=%.2f, otherMultiplier=%.4f", totalTokens, modelRatio, finalGroupRatio, otherMultiplier)
	return actualQuota, reason, true
}

// RecalculateTaskQuotaByTokens 根据实际 token 消耗重新计费（异步差额结算）。
func RecalculateTaskQuotaByTokens(ctx context.Context, task *model.Task, totalTokens int) {
	actualQuota, reason, ok := CalculateTaskQuotaByTokens(task, totalTokens)
	if !ok {
		return
	}
	RecalculateTaskQuota(ctx, task, actualQuota, reason)
}
