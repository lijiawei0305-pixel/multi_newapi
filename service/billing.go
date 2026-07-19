package service

import (
	"fmt"
	"time"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/internal/platform/agenthook"
	"github.com/QuantumNous/new-api/logger"
	"github.com/QuantumNous/new-api/model"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
)

const billingSettlementOperation = "request"

func prepareBillingCommission(relayInfo *relaycommon.RelayInfo, quota int, requestId string, operation string) (*model.BillingCommissionSnapshot, error) {
	if relayInfo == nil || relayInfo.BillingSource == BillingSourceSubscription || quota <= 0 {
		return nil, nil
	}
	if relayInfo.BillingCommissionPolicy != "" {
		return materializeBillingCommissionPolicy(relayInfo.BillingCommissionPolicy, quota, requestId, operation)
	}
	return prepareBillingCommissionFields(relayInfo.UserId, quota, requestId, operation, relayInfo.BillingSource,
		relayInfo.UsingGroup, relayInfo.PriceData.GroupRatioInfo.GroupRatio)
}

func freezeBillingCommissionPolicy(relayInfo *relaycommon.RelayInfo) error {
	if relayInfo == nil || relayInfo.BillingSource == BillingSourceSubscription {
		return nil
	}
	policy := agenthook.CommissionPolicy{OccurredAt: time.Now().UTC().Truncate(time.Millisecond)}
	if agenthook.PrepareConsumeCommissionPolicy != nil {
		var err error
		policy, err = agenthook.PrepareConsumeCommissionPolicy(int64(relayInfo.UserId), relayInfo.BillingSource, relayInfo.UsingGroup, relayInfo.PriceData.GroupRatioInfo.GroupRatio)
		if err != nil {
			return err
		}
	}
	payload, err := common.Marshal(policy)
	if err != nil {
		return err
	}
	relayInfo.BillingCommissionPolicy = string(payload)
	return nil
}

func materializeBillingCommissionPolicy(policyJSON string, quota int, requestId string, operation string) (*model.BillingCommissionSnapshot, error) {
	if quota <= 0 {
		return nil, nil
	}
	var policy agenthook.CommissionPolicy
	if err := common.UnmarshalJsonStr(policyJSON, &policy); err != nil {
		return nil, err
	}
	sourceId := model.BillingCommissionSourceId(requestId, operation)
	if agenthook.MaterializeConsumeCommissionPolicy == nil {
		return &model.BillingCommissionSnapshot{SourceId: sourceId, OccurredAt: policy.OccurredAt}, nil
	}
	snapshot, err := agenthook.MaterializeConsumeCommissionPolicy(policy, int64(quota), sourceId)
	if err != nil {
		return nil, err
	}
	return billingCommissionSnapshotFromHook(snapshot), nil
}

func billingCommissionSnapshotFromHook(snapshot agenthook.CommissionSnapshot) *model.BillingCommissionSnapshot {
	return &model.BillingCommissionSnapshot{
		SourceId: snapshot.SourceID, OccurredAt: snapshot.OccurredAt,
		WalletTenantId: snapshot.WalletTenantID, WalletUserId: snapshot.WalletUserID, WalletQuota: snapshot.WalletQuota,
		EarningApplicable: snapshot.EarningApplicable, EarningTenantId: snapshot.EarningTenantID,
		EarningUserId: snapshot.EarningUserID, EarningSourceType: snapshot.EarningSourceType,
		EarningAmount: snapshot.EarningAmount, EarningRemark: snapshot.EarningRemark,
	}
}

func prepareBillingCommissionFields(userId int, quota int, requestId string, operation string, billingSource string, usingGroup string, chargedGroupRatio float64) (*model.BillingCommissionSnapshot, error) {
	if billingSource == BillingSourceSubscription || quota <= 0 {
		return nil, nil
	}
	sourceId := model.BillingCommissionSourceId(requestId, operation)
	if agenthook.PrepareConsumeCommission == nil {
		return &model.BillingCommissionSnapshot{SourceId: sourceId, OccurredAt: time.Now().UTC().Truncate(time.Millisecond)}, nil
	}
	snapshot, err := agenthook.PrepareConsumeCommission(
		int64(userId), int64(quota), sourceId, billingSource, usingGroup, chargedGroupRatio,
	)
	if err != nil {
		return nil, err
	}
	if snapshot.SourceID == "" {
		snapshot.SourceID = sourceId
	}
	if snapshot.OccurredAt.IsZero() {
		snapshot.OccurredAt = time.Now().UTC().Truncate(time.Millisecond)
	}
	return billingCommissionSnapshotFromHook(snapshot), nil
}

func DispatchBillingCommission(requestId string, operation string) error {
	if agenthook.PersistConsumeCommission == nil {
		return model.MarkBillingCommissionDispatched(requestId, operation)
	}
	event, err := model.GetBillingCommissionEvent(requestId, operation)
	if err != nil {
		return err
	}
	s := event.Snapshot
	err = agenthook.PersistConsumeCommission(agenthook.CommissionSnapshot{
		SourceID: s.SourceId, OccurredAt: s.OccurredAt,
		WalletTenantID: s.WalletTenantId, WalletUserID: s.WalletUserId, WalletQuota: s.WalletQuota,
		EarningApplicable: s.EarningApplicable, EarningTenantID: s.EarningTenantId,
		EarningUserID: s.EarningUserId, EarningSourceType: s.EarningSourceType,
		EarningAmount: s.EarningAmount, EarningRemark: s.EarningRemark,
	})
	if err != nil {
		model.MarkBillingSettlementError(requestId, operation, err)
		return err
	}
	return model.MarkBillingCommissionDispatched(requestId, operation)
}

const (
	BillingSourceWallet       = "wallet"
	BillingSourceSubscription = "subscription"
	BillingSourceFree         = "free"
)

// PreConsumeBilling 根据用户计费偏好创建 BillingSession 并执行预扣费。
// 会话存储在 relayInfo.Billing 上，供后续 Settle / Refund 使用。
func PreConsumeBilling(c *gin.Context, preConsumedQuota int, relayInfo *relaycommon.RelayInfo) *types.NewAPIError {
	session, apiErr := NewBillingSession(c, relayInfo, preConsumedQuota)
	if apiErr != nil {
		return apiErr
	}
	relayInfo.Billing = session
	return nil
}

// ---------------------------------------------------------------------------
// SettleBilling — 后结算辅助函数
// ---------------------------------------------------------------------------

// SettleBilling 执行计费结算。如果 RelayInfo 上有 BillingSession 则通过 session 结算，
// 否则回退到旧的 PostConsumeQuota 路径（兼容按次计费等场景）。
func SettleBilling(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int) error {
	return SettleBillingWithProjection(ctx, relayInfo, actualQuota, nil)
}

func SettleBillingWithProjection(ctx *gin.Context, relayInfo *relaycommon.RelayInfo, actualQuota int, projection *model.BillingProjectionSpec) error {
	MarkBillingTerminalAttempted(ctx)
	if relayInfo.Billing != nil {
		preConsumed := relayInfo.Billing.GetPreConsumedQuota()
		delta := actualQuota - preConsumed

		if delta > 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后补扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else if delta < 0 {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费后返还扣费：%s（实际消耗：%s，预扣费：%s）",
				logger.FormatQuota(-delta),
				logger.FormatQuota(actualQuota),
				logger.FormatQuota(preConsumed),
			))
		} else {
			logger.LogInfo(ctx, fmt.Sprintf("预扣费与实际消耗一致，无需调整：%s（按次计费）",
				logger.FormatQuota(actualQuota),
			))
		}

		var err error
		if session, ok := relayInfo.Billing.(*BillingSession); ok {
			if projection != nil {
				projection.DependencyRequestId = session.billingRequestId
				projection.DependencyOperation = billingSettlementOperation
			}
			err = session.SettleWithProjection(actualQuota, projection)
		} else if projection != nil {
			return fmt.Errorf("billing session does not support durable projection: %T", relayInfo.Billing)
		} else {
			err = relayInfo.Billing.Settle(actualQuota)
		}
		if err != nil {
			return err
		}

		// 发送额度通知（订阅计费使用订阅剩余额度）
		if actualQuota != 0 {
			if relayInfo.BillingSource == BillingSourceSubscription {
				checkAndSendSubscriptionQuotaNotify(relayInfo)
			} else {
				checkAndSendQuotaNotify(relayInfo, actualQuota-preConsumed, preConsumed)
			}
		}
		return nil
	}

	// 回退：无 BillingSession 时使用旧路径
	quotaDelta := actualQuota - relayInfo.FinalPreConsumedQuota
	if quotaDelta != 0 || projection != nil {
		return PostConsumeQuotaWithProjectionOperation(relayInfo, quotaDelta, relayInfo.FinalPreConsumedQuota, quotaDelta != 0, "legacy_settle", projection)
	}
	return nil
}
